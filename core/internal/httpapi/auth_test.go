package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kaikei-e/moka-1/core/internal/auth"
)

// fakeAuthenticator は Authenticator のテストフェイク。ゼロ値は「未登録・儀式成功」。
type fakeAuthenticator struct {
	registered    func(ctx context.Context) (bool, error)
	beginReg      func(ctx context.Context) (any, error)
	finishReg     func(ctx context.Context, body io.Reader) (*http.Cookie, error)
	beginLogin    func(ctx context.Context) (any, error)
	finishLogin   func(ctx context.Context, body io.Reader) (*http.Cookie, error)
	listPasskeys  func(ctx context.Context) ([]auth.PasskeySummary, error)
	deletePasskey func(ctx context.Context, id int64) error
	logout        func() *http.Cookie
}

func (f *fakeAuthenticator) Registered(ctx context.Context) (bool, error) {
	if f.registered == nil {
		return false, nil
	}
	return f.registered(ctx)
}

func (f *fakeAuthenticator) BeginRegistration(ctx context.Context) (any, error) {
	if f.beginReg == nil {
		return map[string]any{"publicKey": map[string]any{}}, nil
	}
	return f.beginReg(ctx)
}

func (f *fakeAuthenticator) FinishRegistration(ctx context.Context, body io.Reader) (*http.Cookie, error) {
	if f.finishReg == nil {
		return testSessionCookie(), nil
	}
	return f.finishReg(ctx, body)
}

func (f *fakeAuthenticator) BeginLogin(ctx context.Context) (any, error) {
	if f.beginLogin == nil {
		return map[string]any{"publicKey": map[string]any{}}, nil
	}
	return f.beginLogin(ctx)
}

func (f *fakeAuthenticator) FinishLogin(ctx context.Context, body io.Reader) (*http.Cookie, error) {
	if f.finishLogin == nil {
		return testSessionCookie(), nil
	}
	return f.finishLogin(ctx, body)
}

func (f *fakeAuthenticator) ListPasskeys(ctx context.Context) ([]auth.PasskeySummary, error) {
	if f.listPasskeys == nil {
		return nil, nil
	}
	return f.listPasskeys(ctx)
}

func (f *fakeAuthenticator) DeletePasskey(ctx context.Context, id int64) error {
	if f.deletePasskey == nil {
		return nil
	}
	return f.deletePasskey(ctx, id)
}

func (f *fakeAuthenticator) Logout() *http.Cookie {
	if f.logout == nil {
		return clearedSessionCookie()
	}
	return f.logout()
}

// clearedSessionCookie は Logout フェイクの既定戻り値(cookie 契約どおりの失効 cookie)。
func clearedSessionCookie() *http.Cookie {
	return &http.Cookie{
		Name:     "moka_session",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
}

// testSessionCookie は cookie 契約(ADR00021)どおりの Set-Cookie をフェイクが返すための
// 見本。値の正しさ(HMAC)は auth パッケージのテストが担保する — ここでは属性の透過のみ見る。
func testSessionCookie() *http.Cookie {
	return &http.Cookie{
		Name:     "moka_session",
		Value:    "v1.1752900000000.70IQ73QEImdzelmgC936H0Hp499_n5NpPISpN9s4CnI",
		Path:     "/",
		MaxAge:   2592000,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
}

func TestHandleAuthStatus(t *testing.T) {
	t.Parallel()

	t.Run("unregistered instance reports registered=false", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/status", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"registered":false}`, rec.Body.String())
	})

	t.Run("registered instance reports registered=true", func(t *testing.T) {
		t.Parallel()

		a := &fakeAuthenticator{registered: func(context.Context) (bool, error) { return true, nil }}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/status", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"registered":true}`, rec.Body.String())
	})

	t.Run("missing session secret returns 503", func(t *testing.T) {
		t.Parallel()

		a := &fakeAuthenticator{registered: func(context.Context) (bool, error) { return false, auth.ErrUnavailable }}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/status", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})

	t.Run("store failure returns 500", func(t *testing.T) {
		t.Parallel()

		a := &fakeAuthenticator{registered: func(context.Context) (bool, error) { return false, assert.AnError }}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/status", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("POST returns 405", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/status", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	})
}

func TestHandleRegisterBegin(t *testing.T) {
	t.Parallel()

	t.Run("unregistered instance returns creation options as-is", func(t *testing.T) {
		t.Parallel()

		a := &fakeAuthenticator{beginReg: func(context.Context) (any, error) {
			return map[string]any{"publicKey": map[string]any{"challenge": "abc"}}, nil
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/register/begin", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"publicKey":{"challenge":"abc"}}`, rec.Body.String())
	})

	t.Run("already registered returns 409 (bootstrap closed)", func(t *testing.T) {
		t.Parallel()

		a := &fakeAuthenticator{beginReg: func(context.Context) (any, error) { return nil, auth.ErrAlreadyRegistered }}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/register/begin", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusConflict, rec.Code)
	})

	t.Run("missing session secret returns 503", func(t *testing.T) {
		t.Parallel()

		a := &fakeAuthenticator{beginReg: func(context.Context) (any, error) { return nil, auth.ErrUnavailable }}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/register/begin", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})

	t.Run("GET returns 405", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/register/begin", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	})
}

func TestHandleRegisterFinish(t *testing.T) {
	t.Parallel()

	t.Run("successful registration returns 201 with session cookie (login included)", func(t *testing.T) {
		t.Parallel()

		var gotBody string
		a := &fakeAuthenticator{finishReg: func(_ context.Context, body io.Reader) (*http.Cookie, error) {
			b, err := io.ReadAll(body)
			require.NoError(t, err)
			gotBody = string(b)
			return testSessionCookie(), nil
		}}
		req := httptest.NewRequestWithContext(t.Context(),
			http.MethodPost, "/api/v1/auth/register/finish", strings.NewReader(`{"id":"cred"}`))
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusCreated, rec.Code)
		assert.JSONEq(t, `{"ok":true}`, rec.Body.String())
		assert.JSONEq(t, `{"id":"cred"}`, gotBody)
		// cookie 契約(ADR00021): HttpOnly; Secure; SameSite=Lax; Path=/; Max-Age=2592000
		assert.Equal(t,
			"moka_session=v1.1752900000000.70IQ73QEImdzelmgC936H0Hp499_n5NpPISpN9s4CnI; "+
				"Path=/; Max-Age=2592000; HttpOnly; Secure; SameSite=Lax",
			rec.Header().Get("Set-Cookie"))
	})

	t.Run("already registered returns 409 without cookie", func(t *testing.T) {
		t.Parallel()

		a := &fakeAuthenticator{finishReg: func(context.Context, io.Reader) (*http.Cookie, error) {
			return nil, auth.ErrAlreadyRegistered
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/register/finish", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Empty(t, rec.Header().Get("Set-Cookie"))
	})

	t.Run("finish without begin returns 400", func(t *testing.T) {
		t.Parallel()

		a := &fakeAuthenticator{finishReg: func(context.Context, io.Reader) (*http.Cookie, error) {
			return nil, auth.ErrNoCeremony
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/register/finish", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("invalid attestation returns 400", func(t *testing.T) {
		t.Parallel()

		a := &fakeAuthenticator{finishReg: func(context.Context, io.Reader) (*http.Cookie, error) {
			return nil, auth.ErrCeremonyFailed
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/register/finish", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("missing session secret returns 503", func(t *testing.T) {
		t.Parallel()

		a := &fakeAuthenticator{finishReg: func(context.Context, io.Reader) (*http.Cookie, error) {
			return nil, auth.ErrUnavailable
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/register/finish", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})

	t.Run("store failure returns 500", func(t *testing.T) {
		t.Parallel()

		a := &fakeAuthenticator{finishReg: func(context.Context, io.Reader) (*http.Cookie, error) {
			return nil, assert.AnError
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/register/finish", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestHandleLoginBegin(t *testing.T) {
	t.Parallel()

	t.Run("registered instance returns assertion options as-is", func(t *testing.T) {
		t.Parallel()

		a := &fakeAuthenticator{beginLogin: func(context.Context) (any, error) {
			return map[string]any{"publicKey": map[string]any{"challenge": "xyz"}}, nil
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login/begin", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"publicKey":{"challenge":"xyz"}}`, rec.Body.String())
	})

	t.Run("unregistered instance returns 404", func(t *testing.T) {
		t.Parallel()

		a := &fakeAuthenticator{beginLogin: func(context.Context) (any, error) { return nil, auth.ErrNotRegistered }}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login/begin", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("missing session secret returns 503", func(t *testing.T) {
		t.Parallel()

		a := &fakeAuthenticator{beginLogin: func(context.Context) (any, error) { return nil, auth.ErrUnavailable }}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login/begin", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})
}

func TestHandleLoginFinish(t *testing.T) {
	t.Parallel()

	t.Run("successful assertion returns 200 with session cookie", func(t *testing.T) {
		t.Parallel()

		a := &fakeAuthenticator{}
		req := httptest.NewRequestWithContext(t.Context(),
			http.MethodPost, "/api/v1/auth/login/finish", strings.NewReader(`{"id":"cred"}`))
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"ok":true}`, rec.Body.String())
		assert.Equal(t,
			"moka_session=v1.1752900000000.70IQ73QEImdzelmgC936H0Hp499_n5NpPISpN9s4CnI; "+
				"Path=/; Max-Age=2592000; HttpOnly; Secure; SameSite=Lax",
			rec.Header().Get("Set-Cookie"))
	})

	t.Run("failed assertion returns 401 without cookie", func(t *testing.T) {
		t.Parallel()

		a := &fakeAuthenticator{finishLogin: func(context.Context, io.Reader) (*http.Cookie, error) {
			return nil, auth.ErrCeremonyFailed
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login/finish", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.Empty(t, rec.Header().Get("Set-Cookie"))
	})

	t.Run("finish without begin returns 400", func(t *testing.T) {
		t.Parallel()

		a := &fakeAuthenticator{finishLogin: func(context.Context, io.Reader) (*http.Cookie, error) {
			return nil, auth.ErrNoCeremony
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login/finish", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("unregistered instance returns 404", func(t *testing.T) {
		t.Parallel()

		a := &fakeAuthenticator{finishLogin: func(context.Context, io.Reader) (*http.Cookie, error) {
			return nil, auth.ErrNotRegistered
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login/finish", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("missing session secret returns 503", func(t *testing.T) {
		t.Parallel()

		a := &fakeAuthenticator{finishLogin: func(context.Context, io.Reader) (*http.Cookie, error) {
			return nil, auth.ErrUnavailable
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login/finish", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})
}

func TestHandleListPasskeys(t *testing.T) {
	t.Parallel()

	t.Run("returns the list as-is", func(t *testing.T) {
		t.Parallel()

		created := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
		lastUsed := created.Add(time.Hour)
		a := &fakeAuthenticator{listPasskeys: func(context.Context) ([]auth.PasskeySummary, error) {
			return []auth.PasskeySummary{
				{ID: 1, CreatedAt: created, LastUsedAt: &lastUsed},
				{ID: 2, CreatedAt: created, LastUsedAt: nil},
			}, nil
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/passkeys", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t,
			`{"passkeys":[`+
				`{"id":1,"created_at":"2026-07-19T00:00:00Z","last_used_at":"2026-07-19T01:00:00Z"},`+
				`{"id":2,"created_at":"2026-07-19T00:00:00Z","last_used_at":null}`+
				`]}`,
			rec.Body.String())
	})

	t.Run("empty list is [] not null", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/passkeys", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: &fakeAuthenticator{}}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"passkeys":[]}`, rec.Body.String())
	})

	t.Run("missing session secret returns 503", func(t *testing.T) {
		t.Parallel()

		a := &fakeAuthenticator{listPasskeys: func(context.Context) ([]auth.PasskeySummary, error) {
			return nil, auth.ErrUnavailable
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/passkeys", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})

	t.Run("store failure returns 500", func(t *testing.T) {
		t.Parallel()

		a := &fakeAuthenticator{listPasskeys: func(context.Context) ([]auth.PasskeySummary, error) {
			return nil, assert.AnError
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/passkeys", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestHandleDeletePasskey(t *testing.T) {
	t.Parallel()

	t.Run("existing passkey deletes with 204", func(t *testing.T) {
		t.Parallel()

		var gotID int64
		a := &fakeAuthenticator{deletePasskey: func(_ context.Context, id int64) error {
			gotID = id
			return nil
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/api/v1/auth/passkeys/7", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNoContent, rec.Code)
		assert.Equal(t, int64(7), gotID)
	})

	t.Run("non-numeric id returns 400", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/api/v1/auth/passkeys/abc", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: &fakeAuthenticator{}}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("unknown id returns 404", func(t *testing.T) {
		t.Parallel()

		a := &fakeAuthenticator{deletePasskey: func(context.Context, int64) error {
			return auth.ErrPasskeyNotFound
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/api/v1/auth/passkeys/404", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("missing session secret returns 503", func(t *testing.T) {
		t.Parallel()

		a := &fakeAuthenticator{deletePasskey: func(context.Context, int64) error {
			return auth.ErrUnavailable
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/api/v1/auth/passkeys/1", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})

	t.Run("store failure returns 500", func(t *testing.T) {
		t.Parallel()

		a := &fakeAuthenticator{deletePasskey: func(context.Context, int64) error {
			return assert.AnError
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/api/v1/auth/passkeys/1", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: a}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestHandleLogout(t *testing.T) {
	t.Parallel()

	t.Run("clears the session cookie and returns ok", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/logout", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{auth: &fakeAuthenticator{}}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"ok":true}`, rec.Body.String())
		setCookie := rec.Header().Get("Set-Cookie")
		assert.Contains(t, setCookie, "moka_session=")
		assert.Contains(t, setCookie, "Max-Age=0")
	})
}
