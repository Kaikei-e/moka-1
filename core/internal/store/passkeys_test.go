package store

import (
	"testing"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// samplePasskey は roundtrip 検証用の代表的な資格情報(transports/flags/aaguid 込み)。
func samplePasskey() webauthn.Credential {
	return webauthn.Credential{
		ID:        []byte("credential-id-1"),
		PublicKey: []byte{0xa5, 0x01, 0x02},
		// Type と Format の両方を持つ(v0.17 の CreateCredential が返す形。Format が
		// 空だと Credential.UnmarshalJSON の後方互換マイグレーションが Type を動かす)
		AttestationType:   "none",
		AttestationFormat: "none",
		Transport: []protocol.AuthenticatorTransport{
			protocol.Internal, protocol.Hybrid,
		},
		Flags: webauthn.CredentialFlags{
			UserPresent:    true,
			UserVerified:   true,
			BackupEligible: true,
			BackupState:    false,
		},
		Authenticator: webauthn.Authenticator{
			AAGUID:     []byte("0123456789abcdef"),
			SignCount:  42, // meta には保存されない(auth_assertions から導出)
			Attachment: protocol.Platform,
		},
	}
}

func TestPasskeyMetaRoundtrip(t *testing.T) {
	t.Parallel()

	t.Run("meta keeps transports, flags and aaguid but not id, key or sign count", func(t *testing.T) {
		t.Parallel()

		meta, err := passkeyMeta(samplePasskey())
		require.NoError(t, err)

		// カラムが真実のフィールドは meta に複製しない
		assert.NotContains(t, meta, `"credential-id-1"`)
		restored, err := passkeyFromRow([]byte("credential-id-1"), []byte{0xa5, 0x01, 0x02}, []byte(meta), 7)
		require.NoError(t, err)

		want := samplePasskey()
		want.Authenticator.SignCount = 7 // sign counter は行でなくイベント由来
		assert.Equal(t, want, restored)
	})

	t.Run("zero-value credential survives the roundtrip", func(t *testing.T) {
		t.Parallel()

		cred := webauthn.Credential{ID: []byte("id"), PublicKey: []byte("pk")}
		meta, err := passkeyMeta(cred)
		require.NoError(t, err)

		restored, err := passkeyFromRow([]byte("id"), []byte("pk"), []byte(meta), 0)
		require.NoError(t, err)
		assert.Equal(t, cred, restored)
	})

	t.Run("empty meta still yields id, key and sign count", func(t *testing.T) {
		t.Parallel()

		restored, err := passkeyFromRow([]byte("id"), []byte("pk"), nil, 3)
		require.NoError(t, err)
		assert.Equal(t, []byte("id"), restored.ID)
		assert.Equal(t, []byte("pk"), restored.PublicKey)
		assert.Equal(t, uint32(3), restored.Authenticator.SignCount)
	})

	t.Run("malformed meta returns an error", func(t *testing.T) {
		t.Parallel()

		_, err := passkeyFromRow([]byte("id"), []byte("pk"), []byte("{not json"), 0)
		assert.Error(t, err)
	})
}
