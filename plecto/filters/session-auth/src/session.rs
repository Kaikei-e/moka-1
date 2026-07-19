//! セッション cookie 検証の純ロジック(host API 非依存 — native `cargo test` で回す)。
//!
//! cookie 契約(moka-core 側と合意済み、変更禁止 — ADR00021):
//!   値 = `v1.<exp_unix_ms>.<base64url_nopad(HMAC-SHA256(secret, "v1." + exp_unix_ms))>`
//!   検証 = 形式パース → HMAC 検証(定数時間比較)→ `exp_unix_ms > now_ms`
//!
//! 鍵は [filter.config] の `hmac_key` 文字列(trim 後)の UTF-8 バイト列を **そのまま** HMAC
//! 鍵として使う(hex デコードしない)。moka-core 側も secret ファイル内容を同じ扱いにする契約。

use base64::Engine as _;
use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use hmac::{Hmac, Mac};
use sha2::Sha256;

type HmacSha256 = Hmac<Sha256>;

/// cookie 名の既定値([filter.config] の `session_cookie` で上書き可)。
pub const DEFAULT_COOKIE_NAME: &str = "moka_session";

/// cookie 値の版プレフィックス。HMAC の署名対象にも含まれる。
const VERSION_PREFIX: &str = "v1.";

/// 検証失敗時にクライアントへ返す遮断の種別(Fail-closed)。
#[derive(Debug, PartialEq, Eq)]
pub enum Denial {
    /// ブラウザのページ遷移(GET + Accept: text/html)→ 302 でログイン画面へ
    Redirect,
    /// それ以外(API 呼び出し等)→ 401
    Unauthorized,
}

/// リクエスト全体を検証する。有効な署名 cookie があれば `Ok(())`、なければ遮断種別を返す。
/// `headers` は (名前, 値バイト列) の列。名前照合はすべて ASCII case-insensitive。
pub fn authorize(
    method: &str,
    headers: &[(&str, &[u8])],
    cookie_name: &str,
    key: &[u8],
    now_ms: u64,
) -> Result<(), Denial> {
    let authenticated = headers
        .iter()
        .filter(|(name, _)| name.eq_ignore_ascii_case("cookie"))
        .filter_map(|(_, value)| std::str::from_utf8(value).ok())
        .flat_map(|header_value| cookie_values(header_value, cookie_name))
        .any(|value| verify_cookie_value(value, key, now_ms));
    if authenticated {
        return Ok(());
    }
    Err(denial_for(method, headers))
}

/// 1 本の Cookie ヘッダ値から `name` に一致する cookie の値を列挙する。
/// 同名 cookie が複数あってもすべて候補にする(どれか 1 つでも HMAC が通れば有効)。
fn cookie_values<'a>(header_value: &'a str, name: &'a str) -> impl Iterator<Item = &'a str> {
    header_value.split(';').filter_map(move |pair| {
        let (n, v) = pair.split_once('=')?;
        if n.trim().eq_ignore_ascii_case(name) {
            Some(v.trim())
        } else {
            None
        }
    })
}

/// cookie 値 1 つを検証する: 形式パース → HMAC(定数時間比較)→ 有効期限。
fn verify_cookie_value(value: &str, key: &[u8], now_ms: u64) -> bool {
    // 形式パース: "v1." + exp(10進数字のみ)+ "." + base64url_nopad(署名)
    let Some(rest) = value.strip_prefix(VERSION_PREFIX) else {
        return false;
    };
    let Some((exp_str, sig_b64)) = rest.split_once('.') else {
        return false;
    };
    if exp_str.is_empty() || !exp_str.bytes().all(|b| b.is_ascii_digit()) {
        return false;
    }
    let Ok(exp_ms) = exp_str.parse::<u64>() else {
        return false; // u64 を超える桁数など
    };
    let Ok(sig) = URL_SAFE_NO_PAD.decode(sig_b64) else {
        return false;
    };

    // HMAC 検証: 署名対象は受信した文字列そのまま("v1." + exp 部分文字列)。
    // 比較は Mac::verify_slice(定数時間)。
    // unwrap 不変条件: HMAC は任意長の鍵を受けるため new_from_slice は失敗しない
    let mut mac = HmacSha256::new_from_slice(key).expect("HMAC accepts any key length");
    mac.update(VERSION_PREFIX.as_bytes());
    mac.update(exp_str.as_bytes());
    if mac.verify_slice(&sig).is_err() {
        return false;
    }

    // 有効期限: exp は「まだ先」でなければならない(等しい = 失効)
    exp_ms > now_ms
}

/// 検証失敗時の遮断種別: Accept に text/html を含む GET だけログイン画面へ 302、他は 401。
fn denial_for(method: &str, headers: &[(&str, &[u8])]) -> Denial {
    if method != "GET" {
        return Denial::Unauthorized;
    }
    let wants_html = headers
        .iter()
        .filter(|(name, _)| name.eq_ignore_ascii_case("accept"))
        .filter_map(|(_, value)| std::str::from_utf8(value).ok())
        .any(accepts_html);
    if wants_html {
        Denial::Redirect
    } else {
        Denial::Unauthorized
    }
}

/// Accept ヘッダ値が text/html を含むか(大文字小文字は無視)。
fn accepts_html(accept: &str) -> bool {
    accept.to_ascii_lowercase().contains("text/html")
}

#[cfg(test)]
mod tests {
    use super::*;

    const KEY: &[u8] = b"test-hmac-key-0123456789abcdef";
    const NOW_MS: u64 = 1_800_000_000_000;

    /// テスト用に契約どおりの cookie 値を作る(exp の 10 進表記に署名)。
    fn signed_value(exp_ms: u64, key: &[u8]) -> String {
        let msg = format!("v1.{exp_ms}");
        let mut mac = HmacSha256::new_from_slice(key).unwrap();
        mac.update(msg.as_bytes());
        let sig = URL_SAFE_NO_PAD.encode(mac.finalize().into_bytes());
        format!("{msg}.{sig}")
    }

    fn cookie_header(value: &str) -> String {
        format!("moka_session={value}")
    }

    /// moka-core(internal/auth/session_test.go)と共有する相互運用 test vector。
    /// 発行側・検証側どちらかの契約が変わればここが割れる
    #[test]
    fn accepts_the_cross_implementation_test_vector() {
        let value = "v1.1752900000000.70IQ73QEImdzelmgC936H0Hp499_n5NpPISpN9s4CnI";
        let key = b"test-secret-32bytes-aaaaaaaaaaaa";
        assert!(verify_cookie_value(value, key, 1_752_899_999_999));
        // 契約どおり exp と同時刻以降は失効
        assert!(!verify_cookie_value(value, key, 1_752_900_000_000));
    }

    #[test]
    fn accepts_a_valid_unexpired_cookie() {
        let value = signed_value(NOW_MS + 60_000, KEY);
        let header = cookie_header(&value);
        let headers: &[(&str, &[u8])] = &[("cookie", header.as_bytes())];
        assert_eq!(
            authorize("GET", headers, DEFAULT_COOKIE_NAME, KEY, NOW_MS),
            Ok(())
        );
    }

    #[test]
    fn rejects_an_expired_cookie() {
        let value = signed_value(NOW_MS - 1, KEY);
        let header = cookie_header(&value);
        let headers: &[(&str, &[u8])] = &[("cookie", header.as_bytes())];
        assert!(authorize("POST", headers, DEFAULT_COOKIE_NAME, KEY, NOW_MS).is_err());
    }

    #[test]
    fn rejects_exp_equal_to_now() {
        // 契約は exp > now の厳密比較。等しい場合は失効扱い
        let value = signed_value(NOW_MS, KEY);
        let header = cookie_header(&value);
        let headers: &[(&str, &[u8])] = &[("cookie", header.as_bytes())];
        assert!(authorize("POST", headers, DEFAULT_COOKIE_NAME, KEY, NOW_MS).is_err());
    }

    #[test]
    fn rejects_a_cookie_signed_with_another_key() {
        let value = signed_value(NOW_MS + 60_000, b"another-key");
        let header = cookie_header(&value);
        let headers: &[(&str, &[u8])] = &[("cookie", header.as_bytes())];
        assert!(authorize("POST", headers, DEFAULT_COOKIE_NAME, KEY, NOW_MS).is_err());
    }

    #[test]
    fn rejects_a_tampered_expiry() {
        // 有効な署名の exp 部分だけ延長 → HMAC 不一致
        let valid = signed_value(NOW_MS + 60_000, KEY);
        let sig = valid.rsplit_once('.').unwrap().1;
        let tampered = format!("v1.{}.{}", NOW_MS + 999_999_000, sig);
        let header = cookie_header(&tampered);
        let headers: &[(&str, &[u8])] = &[("cookie", header.as_bytes())];
        assert!(authorize("POST", headers, DEFAULT_COOKIE_NAME, KEY, NOW_MS).is_err());
    }

    #[test]
    fn rejects_malformed_values() {
        for bad in [
            "",
            "v1.",
            "v2.123.AAAA",                  // 版違い
            "v1.123",                       // 署名欠落
            "v1..AAAA",                     // exp 空
            "v1.12a3.AAAA",                 // exp に非数字
            "v1.+123.AAAA",                 // 符号は数字でない
            "v1.99999999999999999999.AAAA", // u64 超過
            "v1.123.@@@@",                  // base64url 不正
            "v1.123.AAAA",                  // 署名長不正(HMAC 不一致)
        ] {
            let header = cookie_header(bad);
            let headers: &[(&str, &[u8])] = &[("cookie", header.as_bytes())];
            assert!(
                authorize("POST", headers, DEFAULT_COOKIE_NAME, KEY, NOW_MS).is_err(),
                "should reject {bad:?}"
            );
        }
    }

    #[test]
    fn rejects_when_cookie_header_is_absent() {
        let headers: &[(&str, &[u8])] = &[("accept", b"application/json")];
        assert!(authorize("POST", headers, DEFAULT_COOKIE_NAME, KEY, NOW_MS).is_err());
    }

    #[test]
    fn finds_the_session_cookie_among_others() {
        let value = signed_value(NOW_MS + 60_000, KEY);
        let header = format!("theme=dark; moka_session={value} ; lang=ja");
        let headers: &[(&str, &[u8])] = &[("Cookie", header.as_bytes())];
        assert_eq!(
            authorize("GET", headers, DEFAULT_COOKIE_NAME, KEY, NOW_MS),
            Ok(())
        );
    }

    #[test]
    fn checks_every_cookie_header_and_every_duplicate() {
        // 無効な同名 cookie が先にあっても、有効な 1 つがどこかにあれば通す
        let valid = signed_value(NOW_MS + 60_000, KEY);
        let first = cookie_header("v1.1.AAAA");
        let second = format!("a=b; moka_session={valid}");
        let headers: &[(&str, &[u8])] =
            &[("cookie", first.as_bytes()), ("cookie", second.as_bytes())];
        assert_eq!(
            authorize("GET", headers, DEFAULT_COOKIE_NAME, KEY, NOW_MS),
            Ok(())
        );
    }

    #[test]
    fn honours_a_custom_cookie_name() {
        let value = signed_value(NOW_MS + 60_000, KEY);
        let header = format!("my_session={value}");
        let headers: &[(&str, &[u8])] = &[("cookie", header.as_bytes())];
        assert_eq!(authorize("GET", headers, "my_session", KEY, NOW_MS), Ok(()));
        // 既定名では見つからない
        assert!(authorize("GET", headers, DEFAULT_COOKIE_NAME, KEY, NOW_MS).is_err());
    }

    #[test]
    fn html_get_is_redirected_to_login() {
        let headers: &[(&str, &[u8])] = &[("accept", b"text/html,application/xhtml+xml;q=0.9")];
        assert_eq!(
            authorize("GET", headers, DEFAULT_COOKIE_NAME, KEY, NOW_MS),
            Err(Denial::Redirect)
        );
    }

    #[test]
    fn accept_matching_is_case_insensitive() {
        let headers: &[(&str, &[u8])] = &[("Accept", b"TEXT/HTML")];
        assert_eq!(
            authorize("GET", headers, DEFAULT_COOKIE_NAME, KEY, NOW_MS),
            Err(Denial::Redirect)
        );
    }

    #[test]
    fn non_html_get_gets_401() {
        let headers: &[(&str, &[u8])] = &[("accept", b"application/json")];
        assert_eq!(
            authorize("GET", headers, DEFAULT_COOKIE_NAME, KEY, NOW_MS),
            Err(Denial::Unauthorized)
        );
    }

    #[test]
    fn get_without_accept_gets_401() {
        let headers: &[(&str, &[u8])] = &[];
        assert_eq!(
            authorize("GET", headers, DEFAULT_COOKIE_NAME, KEY, NOW_MS),
            Err(Denial::Unauthorized)
        );
    }

    #[test]
    fn html_post_gets_401_not_redirect() {
        let headers: &[(&str, &[u8])] = &[("accept", b"text/html")];
        assert_eq!(
            authorize("POST", headers, DEFAULT_COOKIE_NAME, KEY, NOW_MS),
            Err(Denial::Unauthorized)
        );
    }

    #[test]
    fn cookie_with_non_utf8_bytes_is_ignored() {
        let headers: &[(&str, &[u8])] = &[("cookie", &[0xff, 0xfe, 0x3d, 0x3b])];
        assert!(authorize("POST", headers, DEFAULT_COOKIE_NAME, KEY, NOW_MS).is_err());
    }
}
