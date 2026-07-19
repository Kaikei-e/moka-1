//! ratelimit — moka-1 のレート制限フィルタ(Plecto ドッグフーディング Phase 2)。
//!
//! ホストが偽装不能な形で再発行する `x-real-ip` を key に、host-native トークンバケット
//! (`host-ratelimit`)を 1 リクエスト = cost 1 で消費する。バケット仕様(容量・補充)は
//! manifest の `[filter.ratelimit]` にありオペレータが所有する — フィルタ 1 枚 = バケット
//! 1 種なので、別レート(例: ログイン経路の厳しめ制限)は同じ wasm を別 id で
//! `[[filter]]` 登録して実現する(manifest.tmpl.toml の ratelimit / ratelimit-auth)。
//!
//! - 枯渇 → 429 + Retry-After(retry-after-ms の切り上げ秒)
//! - `x-real-ip` 欠落 → 403(Fail-closed: key 無しで素通ししない)
//!
//! 判定ロジックは `limit` モジュールの純関数(native `cargo test` 対象)。

pub mod limit;

#[cfg(target_arch = "wasm32")]
mod filter {
    // wit-bindgen が record を多数の core-wasm ABI 引数に展開するため、生成コードが
    // clippy::too_many_arguments に触れる。生成コードのみが対象の allow
    #![allow(clippy::too_many_arguments)]

    wit_bindgen::generate!({
        path: "../wit",
        world: "filter",
    });

    use self::plecto::filter::host_log;
    use self::plecto::filter::host_ratelimit;
    use self::plecto::filter::types::Header;
    use crate::limit::{self, AcquireOutcome, LimitVerdict};

    struct RateLimit;

    impl Guest for RateLimit {
        fn init() {
            // バケットはホスト側(manifest)にあり、このフィルタに初期化すべき状態は無い
        }

        fn on_request(req: HttpRequest) -> RequestDecision {
            let headers: Vec<(&str, &[u8])> = req
                .headers
                .iter()
                .map(|h| (h.name.as_str(), h.value.as_slice()))
                .collect();
            let verdict = limit::verdict(limit::client_ip(&headers), |key| {
                let acquire = host_ratelimit::try_acquire(key, 1);
                AcquireOutcome {
                    allowed: acquire.allowed,
                    retry_after_ms: acquire.retry_after_ms,
                }
            });
            match verdict {
                LimitVerdict::Allow => RequestDecision::Continue,
                LimitVerdict::Deny { retry_after_secs } => {
                    RequestDecision::ShortCircuit(too_many_requests(retry_after_secs))
                }
                LimitVerdict::Forbidden => {
                    // ホストは常に x-real-ip を再発行するので、ここに来るのは構成異常。
                    // 素通しせず遮断し、原因調査のためログだけ残す(Fail-closed)
                    host_log::log(
                        host_log::Level::Warn,
                        "ratelimit: x-real-ip missing/unreadable — denying (fail-closed)",
                    );
                    RequestDecision::ShortCircuit(forbidden())
                }
            }
        }

        fn on_response(_req: HttpRequest, _resp: HttpResponse) -> ResponseDecision {
            ResponseDecision::Continue
        }
    }

    fn header(name: &str, value: &[u8]) -> Header {
        Header {
            name: name.to_string(),
            value: value.to_vec(),
        }
    }

    fn too_many_requests(retry_after_secs: u64) -> HttpResponse {
        HttpResponse {
            status: 429,
            headers: vec![
                header("retry-after", retry_after_secs.to_string().as_bytes()),
                header("content-type", b"application/json"),
                header("cache-control", b"no-store"),
            ],
            body: b"{\"error\":\"rate limited\"}".to_vec(),
        }
    }

    fn forbidden() -> HttpResponse {
        HttpResponse {
            status: 403,
            headers: vec![
                header("content-type", b"application/json"),
                header("cache-control", b"no-store"),
            ],
            body: b"{\"error\":\"client address unavailable\"}".to_vec(),
        }
    }

    export!(RateLimit);
}
