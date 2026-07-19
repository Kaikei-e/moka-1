// Package auth はパスキー(WebAuthn)の儀式と HMAC 署名 cookie のステートレス
// セッション発行を担う(ADR00021)。検証は Plecto のセッション認証フィルタが同じ
// 共有シークレットで行い、moka-core 側はセッションストアを持たない。
// DB・HTTP ハンドラの具象は知らない(依存は消費側 interface 経由 — clean-architecture)。
package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// ドメイン境界の sentinel。httpapi がステータスコードへ写像する。
var (
	// ErrUnavailable はセッションシークレット未設定等で認証機能が使えない場合(503)。
	// 認証はエッジ(Plecto フィルタ)で担保されるため、core 全体は落とさない(fail-soft)。
	ErrUnavailable = errors.New("auth unavailable")
	// ErrAlreadyRegistered はパスキー登録済みの再登録要求(409)。
	// 登録はパスキーが1本も無いときだけ開放する(ADR00021 ブートストラップ)。
	ErrAlreadyRegistered = errors.New("passkey already registered")
	// ErrNotRegistered はパスキー未登録でのログイン要求(404)。
	ErrNotRegistered = errors.New("no passkey registered")
	// ErrNoCeremony は begin 無し・期限切れの finish 要求(400)。
	ErrNoCeremony = errors.New("no pending ceremony")
	// ErrCeremonyFailed は WebAuthn 応答のパース・検証失敗
	// (登録 400 / ログイン 401 に写像)。
	ErrCeremonyFailed = errors.New("webauthn ceremony failed")
)

// 単一ユーザー前提(ADR00021)の固定ユーザー。WebAuthn の user handle は
// 資格情報に焼き込まれるので、この値は登録後に変えてはならない。
const (
	ownerUserID      = "moka-owner"
	ownerName        = "owner"
	ownerDisplayName = "moka owner"
	rpDisplayName    = "moka"
)

// challengeTTL は begin/finish 間の challenge の生存時間(in-memory、単一プロセス前提)。
const challengeTTL = 5 * time.Minute

// 既定の RP 設定(env WEBAUTHN_RP_ID / WEBAUTHN_ORIGIN で上書き)。
const (
	DefaultRPID   = "localhost"
	DefaultOrigin = "https://localhost"
)

// CredentialStore はパスキー資格情報と assertion イベントの永続化ポート
// (消費側定義 — 具象は *store.Store)。すべて INSERT/SELECT のみ(ADR00002)。
type CredentialStore interface {
	// ListPasskeyCredentials は全資格情報を返す。sign counter は auth_assertions の
	// 最新イベントから導出済みの値が Authenticator.SignCount に入る。
	ListPasskeyCredentials(ctx context.Context) ([]webauthn.Credential, error)
	// InsertPasskeyCredential は登録儀式で得た資格情報を追記する。
	InsertPasskeyCredential(ctx context.Context, cred webauthn.Credential) error
	// InsertAuthAssertion はログイン成功の事実(sign counter 込み)を追記する。
	// credentialID は WebAuthn の credential ID(passkey_credentials.credential_id)。
	InsertAuthAssertion(ctx context.Context, credentialID []byte, signCount uint32) error
}

// Config は Service の設定。ゼロ値のフィールドには既定値が入る。
type Config struct {
	// RPID は WebAuthn の Relying Party ID(既定 "localhost"、env WEBAUTHN_RP_ID)。
	RPID string
	// Origin はブラウザ側儀式の origin(既定 "https://localhost"、env WEBAUTHN_ORIGIN)。
	Origin string
	// SecretFile はセッション HMAC 鍵ファイルのパス(env SESSION_HMAC_KEY_FILE)。
	// 空・読めない場合は認証機能だけ無効化される(fail-soft — 他の API は通常起動)。
	SecretFile string
	// Now は時計の注入点(テスト用)。nil なら time.Now。
	Now func() time.Time
}

// Service はパスキー認証ユースケース。httpapi.Authenticator を満たす(main で注入)。
type Service struct {
	web    *webauthn.WebAuthn
	store  CredentialStore
	secret []byte
	now    func() time.Time
	logger *slog.Logger

	// disabled が非 nil なら全メソッドが ErrUnavailable を返す(fail-soft)。
	disabled error

	// begin/finish 間の challenge 保持(単一ユーザーなので儀式種別ごとに1枠)。
	mu         sync.Mutex
	ceremonies map[ceremonyKind]pendingCeremony
}

type ceremonyKind string

const (
	ceremonyRegister ceremonyKind = "register"
	ceremonyLogin    ceremonyKind = "login"
)

type pendingCeremony struct {
	session webauthn.SessionData
	expires time.Time
}

// NewService は Service を組み立てる。シークレットが読めない・RP 設定が不正でも
// エラーは返さず「認証だけ無効」の Service を返す(fail-soft — 呼び出し側は落ちない)。
func NewService(cfg Config, store CredentialStore, logger *slog.Logger) *Service {
	s := &Service{
		store:      store,
		now:        cfg.Now,
		logger:     logger,
		ceremonies: make(map[ceremonyKind]pendingCeremony),
	}
	if s.now == nil {
		s.now = time.Now
	}

	rpID := cfg.RPID
	if rpID == "" {
		rpID = DefaultRPID
	}
	origin := cfg.Origin
	if origin == "" {
		origin = DefaultOrigin
	}

	secret, err := readSecretFile(cfg.SecretFile)
	if err != nil {
		s.disabled = err
		logger.Warn("auth disabled: session hmac key unavailable", "err", err.Error())
		return s
	}
	s.secret = secret

	web, err := webauthn.New(&webauthn.Config{
		RPDisplayName: rpDisplayName,
		RPID:          rpID,
		RPOrigins:     []string{origin},
	})
	if err != nil {
		s.disabled = fmt.Errorf("init webauthn (rp_id=%s origin=%s): %w", rpID, origin, err)
		logger.Warn("auth disabled: invalid webauthn config", "err", err.Error())
		return s
	}
	s.web = web
	return s
}

// Registered はパスキーが1本以上登録済みかを返す(httpapi.Authenticator)。
func (s *Service) Registered(ctx context.Context) (bool, error) {
	user, err := s.user(ctx)
	if err != nil {
		return false, err
	}
	return len(user.creds) > 0, nil
}

// BeginRegistration は登録儀式を開始し CredentialCreation を返す。
// パスキーが既に有れば ErrAlreadyRegistered(ブートストラップは最初の登録で閉じる)。
func (s *Service) BeginRegistration(ctx context.Context) (any, error) {
	user, err := s.user(ctx)
	if err != nil {
		return nil, err
	}
	if len(user.creds) > 0 {
		return nil, ErrAlreadyRegistered
	}

	options, session, err := s.web.BeginRegistration(user)
	if err != nil {
		return nil, fmt.Errorf("begin registration: %w", err)
	}
	s.putCeremony(ceremonyRegister, session)
	return options, nil
}

// FinishRegistration は attestation を検証して資格情報を保存し、
// セッション cookie を返す(登録成功 = ログイン扱い)。
func (s *Service) FinishRegistration(ctx context.Context, body io.Reader) (*http.Cookie, error) {
	user, err := s.user(ctx)
	if err != nil {
		return nil, err
	}
	if len(user.creds) > 0 {
		return nil, ErrAlreadyRegistered
	}
	session, ok := s.takeCeremony(ceremonyRegister)
	if !ok {
		return nil, ErrNoCeremony
	}

	parsed, err := protocol.ParseCredentialCreationResponseBody(body)
	if err != nil {
		return nil, fmt.Errorf("parse attestation response: %w: %w", ErrCeremonyFailed, err)
	}
	cred, err := s.web.CreateCredential(user, session, parsed)
	if err != nil {
		return nil, fmt.Errorf("verify attestation: %w: %w", ErrCeremonyFailed, err)
	}

	if err := s.store.InsertPasskeyCredential(ctx, *cred); err != nil {
		return nil, fmt.Errorf("insert passkey credential: %w", err)
	}
	s.logger.Info("passkey registered", "credential_id", fmt.Sprintf("%x", cred.ID))
	return newSessionCookie(s.secret, s.now()), nil
}

// BeginLogin はログイン儀式を開始し CredentialAssertion を返す。
// パスキーが未登録なら ErrNotRegistered。
func (s *Service) BeginLogin(ctx context.Context) (any, error) {
	user, err := s.user(ctx)
	if err != nil {
		return nil, err
	}
	if len(user.creds) == 0 {
		return nil, ErrNotRegistered
	}

	options, session, err := s.web.BeginLogin(user)
	if err != nil {
		return nil, fmt.Errorf("begin login: %w", err)
	}
	s.putCeremony(ceremonyLogin, session)
	return options, nil
}

// FinishLogin は assertion を検証し、sign counter を auth_assertions へ追記して
// セッション cookie を返す(資格情報行は UPDATE しない — ADR00021/ADR00002)。
func (s *Service) FinishLogin(ctx context.Context, body io.Reader) (*http.Cookie, error) {
	user, err := s.user(ctx)
	if err != nil {
		return nil, err
	}
	if len(user.creds) == 0 {
		return nil, ErrNotRegistered
	}
	session, ok := s.takeCeremony(ceremonyLogin)
	if !ok {
		return nil, ErrNoCeremony
	}

	parsed, err := protocol.ParseCredentialRequestResponseBody(body)
	if err != nil {
		return nil, fmt.Errorf("parse assertion response: %w: %w", ErrCeremonyFailed, err)
	}
	cred, err := s.web.ValidateLogin(user, session, parsed)
	if err != nil {
		return nil, fmt.Errorf("verify assertion: %w: %w", ErrCeremonyFailed, err)
	}
	if cred.Authenticator.CloneWarning {
		// sign counter 非同期のオーセンティケータ(iCloud Keychain 等は常に0)では
		// 検知が効かない(ADR00021 の既知の制約)。ブロックせず記録だけ残す。
		s.logger.Warn("authenticator clone warning", "sign_count", cred.Authenticator.SignCount)
	}

	if err := s.store.InsertAuthAssertion(ctx, cred.ID, cred.Authenticator.SignCount); err != nil {
		return nil, fmt.Errorf("insert auth assertion: %w", err)
	}
	return newSessionCookie(s.secret, s.now()), nil
}

// user は保存済み資格情報を束ねた固定ユーザーを返す(認証無効時は ErrUnavailable)。
func (s *Service) user(ctx context.Context) (*singleUser, error) {
	if s.disabled != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, s.disabled)
	}
	creds, err := s.store.ListPasskeyCredentials(ctx)
	if err != nil {
		return nil, fmt.Errorf("list passkey credentials: %w", err)
	}
	return &singleUser{creds: creds}, nil
}

func (s *Service) putCeremony(kind ceremonyKind, session *webauthn.SessionData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ceremonies[kind] = pendingCeremony{session: *session, expires: s.now().Add(challengeTTL)}
}

// takeCeremony は保留中の儀式を取り出して消す(challenge は1回で消費 — リプレイ不可)。
func (s *Service) takeCeremony(kind ceremonyKind) (webauthn.SessionData, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending, ok := s.ceremonies[kind]
	if !ok {
		return webauthn.SessionData{}, false
	}
	delete(s.ceremonies, kind)
	if s.now().After(pending.expires) {
		return webauthn.SessionData{}, false
	}
	return pending.session, true
}

// singleUser は webauthn.User を満たす固定の単一ユーザー(ADR00021)。
type singleUser struct {
	creds []webauthn.Credential
}

func (u *singleUser) WebAuthnID() []byte                         { return []byte(ownerUserID) }
func (u *singleUser) WebAuthnName() string                       { return ownerName }
func (u *singleUser) WebAuthnDisplayName() string                { return ownerDisplayName }
func (u *singleUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }
