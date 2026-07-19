//! session-auth — moka-1 のセッション認証フィルタ(Plecto ドッグフーディング Phase 2)。
//!
//! ADR00021: 認証の儀式(パスキー登録・ログイン)は moka-core の仕事。このフィルタは
//! moka-core が発行した HMAC 署名 cookie の**検証だけ**を担う。セッションはステートレス
//! (host-kv 不使用)なので、必要なのは共有シークレット 1 つだけ。
//!
//! - シークレットは `[filter.config]` の `hmac_key`(plecto-manifest-render ジョブが
//!   Docker secret `session_hmac_key` から注入)。欠落は `init` の panic = ロード失敗
//!   (`isolation = "trusted"` 前提 — Plecto ADR 000066)
//! - 検証失敗は Fail-closed: Accept に text/html を含む GET は 302 → /auth/login、
//!   それ以外は 401 + `WWW-Authenticate: Session`
//! - 成功は `%continue`(単一ユーザー運用なのでヘッダ stamp は不要)
//!
//! 判定ロジックは `session` モジュールの純関数(native `cargo test` 対象)。この lib.rs は
//! WIT 境界への写像だけを行う。

pub mod session;

#[cfg(target_arch = "wasm32")]
mod filter {
    // wit-bindgen が record を多数の core-wasm ABI 引数に展開するため、生成コードが
    // clippy::too_many_arguments に触れる。生成コードのみが対象の allow
    #![allow(clippy::too_many_arguments)]

    wit_bindgen::generate!({
        path: "../wit",
        world: "filter",
    });

    use self::plecto::filter::host_clock;
    use self::plecto::filter::host_config;
    use self::plecto::filter::host_log;
    use self::plecto::filter::types::Header;
    use crate::session::{self, Denial};
    use std::cell::RefCell;

    /// ログイン画面のパス(/auth prefix は session-auth の掛からないルート — manifest 参照)。
    const LOGIN_PATH: &str = "/auth/login";

    /// `init` で読んだ設定。trusted isolation ではインスタンスがプールされるため、
    /// リクエスト毎の host-config 呼び出しを避けられる(filter-jwt と同じ形)。
    struct Config {
        hmac_key: Vec<u8>,
        cookie_name: String,
    }

    thread_local! {
        static CONFIG: RefCell<Option<Config>> = const { RefCell::new(None) };
    }

    struct SessionAuth;

    impl Guest for SessionAuth {
        fn init() {
            // 必須 config の欠落は明示的に失敗させる(panic = trap)。trusted isolation では
            // ロード時に 1 インスタンスが eager-build されるため、ここで落ちれば
            // リクエスト時でなく manifest 適用時に fail-closed で発覚する(ADR 000066)
            let hmac_key = match host_config::get("hmac_key") {
                Some(v) if !v.trim().is_empty() => v.trim().as_bytes().to_vec(),
                _ => panic!(
                    "session-auth: [filter.config] hmac_key is required \
                     (injected by plecto-manifest-render; requires isolation = \"trusted\")"
                ),
            };
            let cookie_name = match host_config::get("session_cookie") {
                Some(v) if !v.trim().is_empty() => v.trim().to_string(),
                _ => session::DEFAULT_COOKIE_NAME.to_string(),
            };
            CONFIG.with(|c| {
                *c.borrow_mut() = Some(Config {
                    hmac_key,
                    cookie_name,
                })
            });
            host_log::log(host_log::Level::Info, "session-auth: init ok");
        }

        fn on_request(req: HttpRequest) -> RequestDecision {
            CONFIG.with(|c| {
                let config = c.borrow();
                // unwrap 不変条件: init が成功していなければこのインスタンスは存在しない
                let config = config.as_ref().expect("init ran before on-request");

                let headers: Vec<(&str, &[u8])> = req
                    .headers
                    .iter()
                    .map(|h| (h.name.as_str(), h.value.as_slice()))
                    .collect();
                match session::authorize(
                    &req.method,
                    &headers,
                    &config.cookie_name,
                    &config.hmac_key,
                    host_clock::now_ms(),
                ) {
                    Ok(()) => RequestDecision::Continue,
                    Err(Denial::Redirect) => RequestDecision::ShortCircuit(redirect_to_login()),
                    Err(Denial::Unauthorized) => RequestDecision::ShortCircuit(unauthorized()),
                }
            })
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

    /// ブラウザのページ遷移はログイン画面へ(302)。認証応答はキャッシュさせない。
    fn redirect_to_login() -> HttpResponse {
        HttpResponse {
            status: 302,
            headers: vec![
                header("location", LOGIN_PATH.as_bytes()),
                header("cache-control", b"no-store"),
            ],
            body: Vec::new(),
        }
    }

    /// ページ遷移以外は 401(RFC 9110: challenge は WWW-Authenticate で示す)。
    fn unauthorized() -> HttpResponse {
        HttpResponse {
            status: 401,
            headers: vec![
                header("www-authenticate", b"Session"),
                header("content-type", b"application/json"),
                header("cache-control", b"no-store"),
            ],
            body: b"{\"error\":\"session required\"}".to_vec(),
        }
    }

    export!(SessionAuth);
}
