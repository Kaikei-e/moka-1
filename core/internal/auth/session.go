package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// セッション cookie 契約(ADR00021 — Plecto のセッション認証フィルタと共有。変更禁止):
//
//	名前: moka_session
//	値:   v1.<exp_unix_ms>.<base64url_nopad(HMAC-SHA256(secret, "v1."+exp_unix_ms))>
//	属性: HttpOnly; Secure; SameSite=Lax; Path=/; Max-Age=2592000(30日)
//	exp = 発行時刻 + 30日
const (
	// SessionCookieName はセッション cookie の名前。
	SessionCookieName = "moka_session"
	sessionVersion    = "v1"
	sessionTTL        = 30 * 24 * time.Hour
)

// signSessionValue は cookie 値を組み立てる。署名対象はバージョン込みの
// "v1.<exp_unix_ms>"(署名だけ切り出して別ペイロードに付け替えられないように)。
func signSessionValue(secret []byte, expUnixMilli int64) string {
	payload := sessionVersion + "." + strconv.FormatInt(expUnixMilli, 10)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// newSessionCookie は発行時刻 issued から30日有効なセッション cookie を作る。
func newSessionCookie(secret []byte, issued time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    signSessionValue(secret, issued.Add(sessionTTL).UnixMilli()),
		Path:     "/",
		MaxAge:   int(sessionTTL / time.Second),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
}

// readSecretFile は SESSION_HMAC_KEY_FILE の指すファイルから HMAC 鍵を読む
// (POSTGRES_PASSWORD_FILE と同じファイルベース Docker secrets 流儀 — ADR00003)。
// 契約(Plecto フィルタと共有): ファイル内容の前後空白を trim した文字列の UTF-8
// バイトをそのまま鍵にする。hex/base64 デコードはしない(`openssl rand -hex 32` で
// 作った鍵は 64 文字の hex 文字列そのものがバイト列になる)。
func readSecretFile(path string) ([]byte, error) {
	if path == "" {
		return nil, fmt.Errorf("session hmac key file not configured (SESSION_HMAC_KEY_FILE)")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read session hmac key file %s: %w", path, err)
	}
	secret := strings.TrimSpace(string(raw))
	if secret == "" {
		return nil, fmt.Errorf("session hmac key file %s is empty", path)
	}
	return []byte(secret), nil
}
