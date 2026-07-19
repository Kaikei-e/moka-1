//! per-IP レート制限の純ロジック(host API 非依存 — native `cargo test` で回す)。
//!
//! key はホストが偽装不能な形で再発行する `x-real-ip`(クライアントの値は常に上書きされる
//! — Plecto server/headers.rs)。バケット実体はホスト側(manifest の `[filter.ratelimit]`)に
//! あり、フィルタは (key, cost) を渡して結果を判定に写像するだけ。

/// `host-ratelimit::try-acquire` の結果のうち判定に要る部分。
#[derive(Debug, Clone, Copy)]
pub struct AcquireOutcome {
    pub allowed: bool,
    pub retry_after_ms: u64,
}

/// フィルタの判定(WIT の request-decision に写像される)。
#[derive(Debug, PartialEq, Eq)]
pub enum LimitVerdict {
    /// トークン取得成功 → %continue
    Allow,
    /// バケット枯渇 → 429 + Retry-After(秒、切り上げ)
    Deny { retry_after_secs: u64 },
    /// x-real-ip が無い/読めない → 403(Fail-closed: key 無しで素通ししない)
    Forbidden,
}

/// ヘッダ列から `x-real-ip` の値を取り出す(UTF-8 で読めない値は無い扱い)。
pub fn client_ip<'a>(headers: &[(&'a str, &'a [u8])]) -> Option<&'a str> {
    headers
        .iter()
        .find(|(name, _)| name.eq_ignore_ascii_case("x-real-ip"))
        .and_then(|(_, value)| std::str::from_utf8(value).ok())
        .map(str::trim)
        .filter(|v| !v.is_empty())
}

/// 判定本体。`acquire` は host-ratelimit 呼び出し(テストではフェイクを注入)。
pub fn verdict(ip: Option<&str>, acquire: impl FnOnce(&str) -> AcquireOutcome) -> LimitVerdict {
    let Some(ip) = ip else {
        return LimitVerdict::Forbidden;
    };
    let outcome = acquire(ip);
    if outcome.allowed {
        LimitVerdict::Allow
    } else {
        LimitVerdict::Deny {
            retry_after_secs: retry_after_secs(outcome.retry_after_ms),
        }
    }
}

/// retry-after-ms を Retry-After ヘッダ用の秒に切り上げる。
pub fn retry_after_secs(ms: u64) -> u64 {
    ms.div_ceil(1000)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn allows_when_the_bucket_grants() {
        let v = verdict(Some("203.0.113.7"), |key| {
            assert_eq!(key, "203.0.113.7", "the client IP is the bucket key");
            AcquireOutcome {
                allowed: true,
                retry_after_ms: 0,
            }
        });
        assert_eq!(v, LimitVerdict::Allow);
    }

    #[test]
    fn denies_with_retry_after_rounded_up() {
        let v = verdict(Some("203.0.113.7"), |_| AcquireOutcome {
            allowed: false,
            retry_after_ms: 1500,
        });
        assert_eq!(
            v,
            LimitVerdict::Deny {
                retry_after_secs: 2
            }
        );
    }

    #[test]
    fn missing_ip_is_forbidden_fail_closed() {
        assert_eq!(
            verdict(None, |_| unreachable!("acquire must not run without a key")),
            LimitVerdict::Forbidden
        );
    }

    #[test]
    fn extracts_x_real_ip_case_insensitively() {
        let headers: &[(&str, &[u8])] = &[("accept", b"*/*"), ("X-Real-IP", b"198.51.100.4")];
        assert_eq!(client_ip(headers), Some("198.51.100.4"));
    }

    #[test]
    fn missing_or_unreadable_ip_yields_none() {
        let empty: &[(&str, &[u8])] = &[("accept", b"*/*")];
        assert_eq!(client_ip(empty), None);

        let invalid_utf8: &[(&str, &[u8])] = &[("x-real-ip", &[0xff, 0xfe])];
        assert_eq!(client_ip(invalid_utf8), None);

        let blank: &[(&str, &[u8])] = &[("x-real-ip", b"  ")];
        assert_eq!(client_ip(blank), None);
    }

    #[test]
    fn rounds_retry_after_to_whole_seconds() {
        assert_eq!(retry_after_secs(0), 0);
        assert_eq!(retry_after_secs(1), 1);
        assert_eq!(retry_after_secs(999), 1);
        assert_eq!(retry_after_secs(1000), 1);
        assert_eq!(retry_after_secs(1001), 2);
        assert_eq!(retry_after_secs(60_000), 60);
    }
}
