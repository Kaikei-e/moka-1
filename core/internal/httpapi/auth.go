package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/Kaikei-e/moka-1/core/internal/auth"
)

// Authenticator はパスキー認証ユースケースの消費側ポート(具象は *auth.Service、main で注入)。
// Begin* の戻り値は go-webauthn の CredentialCreation / CredentialAssertion をそのまま
// JSON 化して返す前提の opaque な値(httpapi は中身に関知しない)。
// Finish* が返す cookie は ADR00021 の署名セッション cookie(発行は auth の責務)。
type Authenticator interface {
	Registered(ctx context.Context) (bool, error)
	BeginRegistration(ctx context.Context) (any, error)
	FinishRegistration(ctx context.Context, body io.Reader) (*http.Cookie, error)
	BeginLogin(ctx context.Context) (any, error)
	FinishLogin(ctx context.Context, body io.Reader) (*http.Cookie, error)
	// ListPasskeys は管理画面向けの登録済み資格情報一覧を返す。
	ListPasskeys(ctx context.Context) ([]auth.PasskeySummary, error)
	// DeletePasskey は id の資格情報をハード削除する(ErrPasskeyNotFound は 404 に写像)。
	DeletePasskey(ctx context.Context, id int64) error
	// Logout はセッション cookie を失効させる応答を返す(失敗しない — ステートレス設計)。
	Logout() *http.Cookie
}

// handleAuthStatus は GET /api/v1/auth/status。パスキー登録済みかを返す
// (web がブートストラップ画面/ログイン画面のどちらを出すか判定に使う)。
func handleAuthStatus(a Authenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		registered, err := a.Registered(r.Context())
		if err != nil {
			writeAuthError(w, err, http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"registered": registered})
	}
}

// handleRegisterBegin は POST /api/v1/auth/register/begin。未登録のときだけ
// CredentialCreation を返す(登録済みなら 409 — ADR00021 ブートストラップは最初の登録で閉じる)。
func handleRegisterBegin(a Authenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		options, err := a.BeginRegistration(r.Context())
		if err != nil {
			writeAuthError(w, err, http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, options)
	}
}

// handleRegisterFinish は POST /api/v1/auth/register/finish。attestation 検証成功で
// 資格情報を保存し 201。登録成功はログイン扱いなのでセッション cookie も発行する。
func handleRegisterFinish(a Authenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := a.FinishRegistration(r.Context(), r.Body)
		if err != nil {
			writeAuthError(w, err, http.StatusBadRequest)
			return
		}
		http.SetCookie(w, cookie)
		writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
	}
}

// handleLoginBegin は POST /api/v1/auth/login/begin。登録済みのときだけ
// CredentialAssertion を返す(未登録なら 404)。
func handleLoginBegin(a Authenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		options, err := a.BeginLogin(r.Context())
		if err != nil {
			writeAuthError(w, err, http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, options)
	}
}

// handleLoginFinish は POST /api/v1/auth/login/finish。assertion 検証成功で
// sign counter を追記し、セッション cookie を発行する。検証失敗は 401。
func handleLoginFinish(a Authenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := a.FinishLogin(r.Context(), r.Body)
		if err != nil {
			writeAuthError(w, err, http.StatusUnauthorized)
			return
		}
		http.SetCookie(w, cookie)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// handleListPasskeys は GET /api/v1/auth/passkeys。パスキー管理画面向けの一覧。
func handleListPasskeys(a Authenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := a.ListPasskeys(r.Context())
		if err != nil {
			writeAuthError(w, err, http.StatusBadRequest)
			return
		}
		if list == nil {
			list = []auth.PasskeySummary{} // JSON では null でなく [] を返す
		}
		writeJSON(w, http.StatusOK, map[string][]auth.PasskeySummary{"passkeys": list})
	}
}

// handleDeletePasskey は DELETE /api/v1/auth/passkeys/{id}。ハード削除(ADR00019 と
// 同じ流儀)。最後の1本を消すことも許す — パスキーが1本も無い状態はブートストラップを
// 再び開くので、鍵を失っても自分自身で再登録して復旧できる(ADR00021)。
func handleDeletePasskey(a Authenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid passkey id")
			return
		}
		if err := a.DeletePasskey(r.Context(), id); err != nil {
			writeAuthError(w, err, http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleLogout は POST /api/v1/auth/logout。セッション cookie を失効させる。
// moka-core はセッションストアを持たないステートレス設計(ADR00021)なので、
// 認証状態に関わらず常に 200(失敗しない)。
func handleLogout(a Authenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, a.Logout())
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// writeAuthError は auth ドメインの sentinel を HTTP ステータスへ写像する。
// ceremonyFailedStatus は ErrCeremonyFailed の写像先 — 登録(400: クライアントの
// attestation が不正)とログイン(401: 認証失敗)でだけ意味が分かれる。
func writeAuthError(w http.ResponseWriter, err error, ceremonyFailedStatus int) {
	switch {
	case errors.Is(err, auth.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "auth unavailable")
	case errors.Is(err, auth.ErrAlreadyRegistered):
		writeError(w, http.StatusConflict, "passkey already registered")
	case errors.Is(err, auth.ErrNotRegistered):
		writeError(w, http.StatusNotFound, "no passkey registered")
	case errors.Is(err, auth.ErrNoCeremony):
		writeError(w, http.StatusBadRequest, "no pending ceremony")
	case errors.Is(err, auth.ErrPasskeyNotFound):
		writeError(w, http.StatusNotFound, "passkey not found")
	case errors.Is(err, auth.ErrCeremonyFailed):
		if ceremonyFailedStatus == http.StatusUnauthorized {
			writeError(w, http.StatusUnauthorized, "authentication failed")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid webauthn response")
	default:
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}
