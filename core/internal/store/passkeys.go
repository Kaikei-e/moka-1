package store

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/Kaikei-e/moka-1/core/internal/auth"
)

// passkey_credentials / auth_assertions は INSERT-only(ADR00002/ADR00021)。
// sign counter は資格情報行を UPDATE せず、auth_assertions の最新イベントから導出する。
// meta JSONB には webauthn.Credential の付帯情報(transports / flags / aaguid /
// attestation 等)を格納し、credential_id / public_key / sign counter はカラムが真実。

// ListPasskeyCredentials は全資格情報を返す(auth.CredentialStore)。
// Authenticator.SignCount には各資格情報の最新 assertion イベントの値が入る(無ければ 0)。
func (s *Store) ListPasskeyCredentials(ctx context.Context) ([]webauthn.Credential, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT c.credential_id, c.public_key, c.meta,
		        COALESCE((SELECT a.sign_count FROM auth_assertions a
		                  WHERE a.credential_id = c.id
		                  ORDER BY a.asserted_at DESC, a.id DESC LIMIT 1), 0) AS sign_count
		 FROM passkey_credentials c
		 ORDER BY c.id`,
	)
	if err != nil {
		return nil, fmt.Errorf("select passkey credentials: %w", err)
	}
	defer rows.Close()

	var creds []webauthn.Credential
	for rows.Next() {
		var (
			credentialID, publicKey, meta []byte
			signCount                     int64
		)
		if err := rows.Scan(&credentialID, &publicKey, &meta, &signCount); err != nil {
			return nil, fmt.Errorf("scan passkey credential: %w", err)
		}
		cred, err := passkeyFromRow(credentialID, publicKey, meta, signCount)
		if err != nil {
			return nil, fmt.Errorf("decode passkey credential: %w", err)
		}
		creds = append(creds, cred)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate passkey credentials: %w", err)
	}
	return creds, nil
}

// InsertPasskeyCredential は登録儀式で得た資格情報を追記する(auth.CredentialStore)。
func (s *Store) InsertPasskeyCredential(ctx context.Context, cred webauthn.Credential) error {
	meta, err := passkeyMeta(cred)
	if err != nil {
		return fmt.Errorf("encode passkey meta: %w", err)
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO passkey_credentials (credential_id, public_key, meta) VALUES ($1, $2, $3::jsonb)`,
		cred.ID, cred.PublicKey, meta,
	)
	if err != nil {
		return fmt.Errorf("insert passkey credential: %w", err)
	}
	return nil
}

// InsertAuthAssertion はログイン成功の事実(sign counter 込み)を追記する
// (auth.CredentialStore)。credentialID は WebAuthn の credential ID(BYTEA)で、
// FK は副問い合わせで解決する。
func (s *Store) InsertAuthAssertion(ctx context.Context, credentialID []byte, signCount uint32) error {
	tag, err := s.pool.Exec(ctx,
		`INSERT INTO auth_assertions (credential_id, sign_count)
		 SELECT id, $2 FROM passkey_credentials WHERE credential_id = $1`,
		credentialID, int64(signCount),
	)
	if err != nil {
		return fmt.Errorf("insert auth assertion: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("insert auth assertion: credential %x not found", credentialID)
	}
	return nil
}

// ListPasskeySummaries は管理画面向けの人間可読な一覧を返す(auth.CredentialStore)。
// DB 行 id と、auth_assertions の最新イベント時刻(未使用なら NULL)を持つ。
func (s *Store) ListPasskeySummaries(ctx context.Context) ([]auth.PasskeySummary, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT c.id, c.created_at,
		        (SELECT a.asserted_at FROM auth_assertions a
		         WHERE a.credential_id = c.id
		         ORDER BY a.asserted_at DESC, a.id DESC LIMIT 1) AS last_used_at
		 FROM passkey_credentials c
		 ORDER BY c.id`,
	)
	if err != nil {
		return nil, fmt.Errorf("select passkey summaries: %w", err)
	}
	defer rows.Close()

	var summaries []auth.PasskeySummary
	for rows.Next() {
		var (
			id         int64
			createdAt  time.Time
			lastUsedAt *time.Time
		)
		if err := rows.Scan(&id, &createdAt, &lastUsedAt); err != nil {
			return nil, fmt.Errorf("scan passkey summary: %w", err)
		}
		summaries = append(summaries, auth.PasskeySummary{ID: id, CreatedAt: createdAt, LastUsedAt: lastUsedAt})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate passkey summaries: %w", err)
	}
	return summaries, nil
}

// DeletePasskeyCredential は DB 行 id で資格情報を物理削除する(auth.CredentialStore)。
// ハード削除 — ADR00019 と同じ流儀。auth_assertions は既存の FK ON DELETE CASCADE で
// 一括して消える(passkey_credentials 自体は INSERT-only ではない — リソース、ADR00021)。
func (s *Store) DeletePasskeyCredential(ctx context.Context, id int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM passkey_credentials WHERE id = $1`, id)
	if err != nil {
		return false, fmt.Errorf("delete passkey credential %d: %w", id, err)
	}
	return tag.RowsAffected() > 0, nil
}

// passkeyMeta は webauthn.Credential の付帯情報を meta JSONB 用の JSON にする。
// カラムが真実の credential_id / public_key と、イベント由来の sign counter は複製しない。
// CredentialFlags の非公開 raw バイトは JSON に出ないため roundtrip で落ちるが、
// 検証(go-webauthn)は公開 bool しか見ないので実害はない。
func passkeyMeta(cred webauthn.Credential) (string, error) {
	cred.ID = nil
	cred.PublicKey = nil
	cred.Authenticator.SignCount = 0
	b, err := json.Marshal(cred)
	if err != nil {
		return "", fmt.Errorf("marshal credential meta: %w", err)
	}
	return string(b), nil
}

// passkeyFromRow は DB 行を webauthn.Credential へ復元する(passkeyMeta の逆写像)。
func passkeyFromRow(credentialID, publicKey, meta []byte, signCount int64) (webauthn.Credential, error) {
	var cred webauthn.Credential
	if len(meta) > 0 {
		if err := json.Unmarshal(meta, &cred); err != nil {
			return webauthn.Credential{}, fmt.Errorf("unmarshal credential meta: %w", err)
		}
	}
	cred.ID = credentialID
	cred.PublicKey = publicKey
	if signCount < 0 || signCount > math.MaxUint32 {
		return webauthn.Credential{}, fmt.Errorf("sign count %d out of uint32 range", signCount)
	}
	cred.Authenticator.SignCount = uint32(signCount)
	return cred, nil
}
