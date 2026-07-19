package auth

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCredStore は CredentialStore のテストフェイク。
type fakeCredStore struct {
	creds      []webauthn.Credential
	listErr    error
	insertErr  error
	assertErr  error
	inserted   []webauthn.Credential
	assertions []assertionRecord
}

type assertionRecord struct {
	credentialID []byte
	signCount    uint32
}

func (f *fakeCredStore) ListPasskeyCredentials(context.Context) ([]webauthn.Credential, error) {
	return f.creds, f.listErr
}

func (f *fakeCredStore) InsertPasskeyCredential(_ context.Context, cred webauthn.Credential) error {
	if f.insertErr != nil {
		return f.insertErr
	}
	f.inserted = append(f.inserted, cred)
	f.creds = append(f.creds, cred)
	return nil
}

func (f *fakeCredStore) InsertAuthAssertion(_ context.Context, credentialID []byte, signCount uint32) error {
	if f.assertErr != nil {
		return f.assertErr
	}
	f.assertions = append(f.assertions, assertionRecord{credentialID: credentialID, signCount: signCount})
	return nil
}

// newTestService は test vector と同じ鍵の secret ファイルを作って Service を組み立てる。
func newTestService(t *testing.T, store CredentialStore, now func() time.Time) *Service {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session_hmac_key")
	// 契約: ファイル内容は前後空白を trim した文字列の UTF-8 バイトが HMAC 鍵
	// (hex/base64 デコードはしない)。末尾改行込みで書いて trim を検証する。
	require.NoError(t, os.WriteFile(path, []byte(testSecret+"\n"), 0o600))
	return NewService(
		Config{SecretFile: path, Now: now},
		store,
		slog.New(slog.DiscardHandler),
	)
}

// registrationChallenge は register/begin の戻りから challenge バイト列を取り出す。
func registrationChallenge(t *testing.T, options any) []byte {
	t.Helper()
	creation, ok := options.(*protocol.CredentialCreation)
	require.True(t, ok, "options should be *protocol.CredentialCreation, got %T", options)
	return creation.Response.Challenge
}

// loginChallenge は login/begin の戻りから challenge バイト列を取り出す。
func loginChallenge(t *testing.T, options any) []byte {
	t.Helper()
	assertion, ok := options.(*protocol.CredentialAssertion)
	require.True(t, ok, "options should be *protocol.CredentialAssertion, got %T", options)
	return assertion.Response.Challenge
}

// register は登録儀式一式を回すテストヘルパ(begin → 仮想オーセンティケータ → finish)。
func register(t *testing.T, svc *Service, va *virtualAuthenticator) {
	t.Helper()
	options, err := svc.BeginRegistration(t.Context())
	require.NoError(t, err)
	_, err = svc.FinishRegistration(t.Context(), bytes.NewReader(va.attestationJSON(registrationChallenge(t, options))))
	require.NoError(t, err)
}

func TestServiceUnavailable(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	assertAllUnavailable := func(t *testing.T, svc *Service) {
		t.Helper()
		_, err := svc.Registered(t.Context())
		require.ErrorIs(t, err, ErrUnavailable)
		_, err = svc.BeginRegistration(t.Context())
		require.ErrorIs(t, err, ErrUnavailable)
		_, err = svc.FinishRegistration(t.Context(), bytes.NewReader(nil))
		require.ErrorIs(t, err, ErrUnavailable)
		_, err = svc.BeginLogin(t.Context())
		require.ErrorIs(t, err, ErrUnavailable)
		_, err = svc.FinishLogin(t.Context(), bytes.NewReader(nil))
		require.ErrorIs(t, err, ErrUnavailable)
	}

	t.Run("unset secret file disables every auth method", func(t *testing.T) {
		t.Parallel()

		svc := NewService(Config{}, &fakeCredStore{}, logger)
		assertAllUnavailable(t, svc)
	})

	t.Run("unreadable secret file disables every auth method", func(t *testing.T) {
		t.Parallel()

		svc := NewService(Config{SecretFile: filepath.Join(t.TempDir(), "missing")}, &fakeCredStore{}, logger)
		assertAllUnavailable(t, svc)
	})

	t.Run("empty secret file disables every auth method", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "empty")
		require.NoError(t, os.WriteFile(path, []byte("\n"), 0o600))
		svc := NewService(Config{SecretFile: path}, &fakeCredStore{}, logger)
		assertAllUnavailable(t, svc)
	})
}

func TestServiceRegistered(t *testing.T) {
	t.Parallel()

	t.Run("no credentials means not registered", func(t *testing.T) {
		t.Parallel()

		svc := newTestService(t, &fakeCredStore{}, nil)
		registered, err := svc.Registered(t.Context())
		require.NoError(t, err)
		assert.False(t, registered)
	})

	t.Run("a stored credential means registered", func(t *testing.T) {
		t.Parallel()

		svc := newTestService(t, &fakeCredStore{creds: []webauthn.Credential{{ID: []byte("c")}}}, nil)
		registered, err := svc.Registered(t.Context())
		require.NoError(t, err)
		assert.True(t, registered)
	})

	t.Run("store failure propagates", func(t *testing.T) {
		t.Parallel()

		svc := newTestService(t, &fakeCredStore{listErr: assert.AnError}, nil)
		_, err := svc.Registered(t.Context())
		assert.ErrorIs(t, err, assert.AnError)
	})
}

func TestServiceRegistration(t *testing.T) {
	t.Parallel()

	t.Run("full ceremony stores the credential and issues a session cookie", func(t *testing.T) {
		t.Parallel()

		// 発行時刻を固定すると cookie 値は session_test.go の test vector に一致する
		issued := time.UnixMilli(testExpUnixMilli).Add(-30 * 24 * time.Hour)
		store := &fakeCredStore{}
		svc := newTestService(t, store, func() time.Time { return issued })
		va := newVirtualAuthenticator(t, "localhost", "https://localhost")

		options, err := svc.BeginRegistration(t.Context())
		require.NoError(t, err)

		cookie, err := svc.FinishRegistration(t.Context(),
			bytes.NewReader(va.attestationJSON(registrationChallenge(t, options))))
		require.NoError(t, err)

		require.Len(t, store.inserted, 1)
		assert.Equal(t, va.credID, store.inserted[0].ID)
		assert.NotEmpty(t, store.inserted[0].PublicKey)
		assert.Equal(t, "moka_session", cookie.Name)
		assert.Equal(t, testSessionValue, cookie.Value)
		assert.Empty(t, store.assertions, "registration itself must not append an assertion event")
	})

	t.Run("begin is refused once a passkey exists (bootstrap closes)", func(t *testing.T) {
		t.Parallel()

		svc := newTestService(t, &fakeCredStore{creds: []webauthn.Credential{{ID: []byte("c")}}}, nil)
		_, err := svc.BeginRegistration(t.Context())
		assert.ErrorIs(t, err, ErrAlreadyRegistered)
	})

	t.Run("finish is refused once a passkey exists", func(t *testing.T) {
		t.Parallel()

		svc := newTestService(t, &fakeCredStore{creds: []webauthn.Credential{{ID: []byte("c")}}}, nil)
		_, err := svc.FinishRegistration(t.Context(), bytes.NewReader([]byte(`{}`)))
		assert.ErrorIs(t, err, ErrAlreadyRegistered)
	})

	t.Run("finish without begin returns ErrNoCeremony", func(t *testing.T) {
		t.Parallel()

		svc := newTestService(t, &fakeCredStore{}, nil)
		_, err := svc.FinishRegistration(t.Context(), bytes.NewReader([]byte(`{}`)))
		assert.ErrorIs(t, err, ErrNoCeremony)
	})

	t.Run("expired challenge returns ErrNoCeremony", func(t *testing.T) {
		t.Parallel()

		now := time.Now()
		store := &fakeCredStore{}
		svc := newTestService(t, store, func() time.Time { return now })
		va := newVirtualAuthenticator(t, "localhost", "https://localhost")

		options, err := svc.BeginRegistration(t.Context())
		require.NoError(t, err)

		now = now.Add(6 * time.Minute) // challengeTTL(5分)超過
		_, err = svc.FinishRegistration(t.Context(),
			bytes.NewReader(va.attestationJSON(registrationChallenge(t, options))))
		assert.ErrorIs(t, err, ErrNoCeremony)
	})

	t.Run("wrong challenge in attestation returns ErrCeremonyFailed", func(t *testing.T) {
		t.Parallel()

		store := &fakeCredStore{}
		svc := newTestService(t, store, nil)
		va := newVirtualAuthenticator(t, "localhost", "https://localhost")

		_, err := svc.BeginRegistration(t.Context())
		require.NoError(t, err)

		_, err = svc.FinishRegistration(t.Context(),
			bytes.NewReader(va.attestationJSON([]byte("wrong-challenge-wrong-challenge!"))))
		require.ErrorIs(t, err, ErrCeremonyFailed)
		assert.Empty(t, store.inserted)
	})

	t.Run("malformed body returns ErrCeremonyFailed", func(t *testing.T) {
		t.Parallel()

		svc := newTestService(t, &fakeCredStore{}, nil)
		_, err := svc.BeginRegistration(t.Context())
		require.NoError(t, err)

		_, err = svc.FinishRegistration(t.Context(), bytes.NewReader([]byte(`not json`)))
		assert.ErrorIs(t, err, ErrCeremonyFailed)
	})
}

func TestServiceLogin(t *testing.T) {
	t.Parallel()

	t.Run("begin without a passkey returns ErrNotRegistered", func(t *testing.T) {
		t.Parallel()

		svc := newTestService(t, &fakeCredStore{}, nil)
		_, err := svc.BeginLogin(t.Context())
		assert.ErrorIs(t, err, ErrNotRegistered)
	})

	t.Run("finish without a passkey returns ErrNotRegistered", func(t *testing.T) {
		t.Parallel()

		svc := newTestService(t, &fakeCredStore{}, nil)
		_, err := svc.FinishLogin(t.Context(), bytes.NewReader([]byte(`{}`)))
		assert.ErrorIs(t, err, ErrNotRegistered)
	})

	t.Run("finish without begin returns ErrNoCeremony", func(t *testing.T) {
		t.Parallel()

		svc := newTestService(t, &fakeCredStore{creds: []webauthn.Credential{{ID: []byte("c")}}}, nil)
		_, err := svc.FinishLogin(t.Context(), bytes.NewReader([]byte(`{}`)))
		assert.ErrorIs(t, err, ErrNoCeremony)
	})

	t.Run("full ceremony appends the sign count and issues a session cookie", func(t *testing.T) {
		t.Parallel()

		store := &fakeCredStore{}
		svc := newTestService(t, store, nil)
		va := newVirtualAuthenticator(t, "localhost", "https://localhost")
		register(t, svc, va)

		options, err := svc.BeginLogin(t.Context())
		require.NoError(t, err)

		cookie, err := svc.FinishLogin(t.Context(),
			bytes.NewReader(va.assertionJSON(loginChallenge(t, options), 5)))
		require.NoError(t, err)

		// sign counter は資格情報行の UPDATE でなく assertion イベントの追記(ADR00021)
		require.Len(t, store.assertions, 1)
		assert.Equal(t, va.credID, store.assertions[0].credentialID)
		assert.Equal(t, uint32(5), store.assertions[0].signCount)
		assert.Equal(t, "moka_session", cookie.Name)
		assert.Regexp(t, `^v1\.\d+\.[A-Za-z0-9_-]{43}$`, cookie.Value)
	})

	t.Run("tampered signature returns ErrCeremonyFailed without an assertion event", func(t *testing.T) {
		t.Parallel()

		store := &fakeCredStore{}
		svc := newTestService(t, store, nil)
		va := newVirtualAuthenticator(t, "localhost", "https://localhost")
		register(t, svc, va)

		options, err := svc.BeginLogin(t.Context())
		require.NoError(t, err)

		body := va.assertionJSON(loginChallenge(t, options), 5)
		// 署名(base64url)の一部を破壊する
		tampered := bytes.Replace(body, []byte(`"signature":"`), []byte(`"signature":"AAAA`), 1)
		_, err = svc.FinishLogin(t.Context(), bytes.NewReader(tampered))
		require.ErrorIs(t, err, ErrCeremonyFailed)
		assert.Empty(t, store.assertions)
	})

	t.Run("a fresh challenge is required per ceremony (challenge is consumed)", func(t *testing.T) {
		t.Parallel()

		store := &fakeCredStore{}
		svc := newTestService(t, store, nil)
		va := newVirtualAuthenticator(t, "localhost", "https://localhost")
		register(t, svc, va)

		options, err := svc.BeginLogin(t.Context())
		require.NoError(t, err)

		body := va.assertionJSON(loginChallenge(t, options), 5)
		_, err = svc.FinishLogin(t.Context(), bytes.NewReader(body))
		require.NoError(t, err)

		// 同じ応答の再送(リプレイ)は begin し直さない限り拒否される
		_, err = svc.FinishLogin(t.Context(), bytes.NewReader(body))
		assert.ErrorIs(t, err, ErrNoCeremony)
	})
}
