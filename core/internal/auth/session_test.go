package auth

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// testSecret / testExpUnixMilli は Plecto のセッション認証フィルタ側テストと
// 突き合わせるための共有 test vector(ADR00021 — 両者が同じ契約で署名/検証する)。
//
//	secret = "test-secret-32bytes-aaaaaaaaaaaa"
//	exp    = 1752900000000 (unix ms)
//	value  = "v1.1752900000000.70IQ73QEImdzelmgC936H0Hp499_n5NpPISpN9s4CnI"
const (
	testSecret        = "test-secret-32bytes-aaaaaaaaaaaa"
	testExpUnixMilli  = int64(1752900000000)
	testSessionValue  = "v1.1752900000000.70IQ73QEImdzelmgC936H0Hp499_n5NpPISpN9s4CnI"
	testSessionMaxAge = 2592000 // 30日(秒)
)

func TestSignSessionValue(t *testing.T) {
	t.Parallel()

	t.Run("known key and exp produce the shared test vector", func(t *testing.T) {
		t.Parallel()

		// value = "v1." + exp_unix_ms + "." + base64url_nopad(HMAC-SHA256(secret, "v1."+exp_unix_ms))
		assert.Equal(t, testSessionValue, signSessionValue([]byte(testSecret), testExpUnixMilli))
	})

	t.Run("different keys produce different signatures", func(t *testing.T) {
		t.Parallel()

		assert.NotEqual(t,
			signSessionValue([]byte(testSecret), testExpUnixMilli),
			signSessionValue([]byte("another-secret"), testExpUnixMilli))
	})
}

func TestNewSessionCookie(t *testing.T) {
	t.Parallel()

	t.Run("cookie follows the ADR00021 contract exactly", func(t *testing.T) {
		t.Parallel()

		// exp = 発行時刻 + 30日。発行時刻を exp - 30日に固定すると値は test vector に一致する。
		issued := time.UnixMilli(testExpUnixMilli).Add(-30 * 24 * time.Hour)
		c := newSessionCookie([]byte(testSecret), issued)

		assert.Equal(t,
			"moka_session="+testSessionValue+"; Path=/; Max-Age=2592000; HttpOnly; Secure; SameSite=Lax",
			c.String())
	})

	t.Run("cookie attributes are set individually", func(t *testing.T) {
		t.Parallel()

		c := newSessionCookie([]byte(testSecret), time.UnixMilli(testExpUnixMilli))
		assert.Equal(t, "moka_session", c.Name)
		assert.Equal(t, "/", c.Path)
		assert.Equal(t, testSessionMaxAge, c.MaxAge)
		assert.True(t, c.HttpOnly)
		assert.True(t, c.Secure)
		assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
	})
}

// TestClearSessionCookie はログアウト用のセッション失効 cookie(ADR00021 の cookie 属性は
// 保ったまま、値を空にして即時削除させる)。moka-core はセッションストアを持たない
// ステートレス設計なので、ログアウトは「この cookie を消せ」という応答を返すだけでよい。
func TestClearSessionCookie(t *testing.T) {
	t.Parallel()

	c := clearSessionCookie()
	assert.Equal(t, "moka_session", c.Name)
	assert.Empty(t, c.Value)
	assert.Equal(t, "/", c.Path)
	assert.Negative(t, c.MaxAge, "MaxAge < 0 は net/http の即時削除シグナル(Max-Age: 0 として送出される)")
	assert.True(t, c.HttpOnly)
	assert.True(t, c.Secure)
	assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
	assert.Equal(t, "moka_session=; Path=/; Max-Age=0; HttpOnly; Secure; SameSite=Lax", c.String())
}
