package auth

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/require"
)

// virtualAuthenticator は WebAuthn 儀式のテスト用オーセンティケータ。
// ES256 (P-256) 鍵で attestation("none" 形式)と assertion を実際に組み立てて署名する
// — go-webauthn の検証経路を本物の暗号で通すため(モックで素通しさせない)。
type virtualAuthenticator struct {
	t      *testing.T
	key    *ecdsa.PrivateKey
	credID []byte
	rpID   string
	origin string
}

func newVirtualAuthenticator(t *testing.T, rpID, origin string) *virtualAuthenticator {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return &virtualAuthenticator{
		t:      t,
		key:    key,
		credID: []byte("virtual-authenticator-cred-1"),
		rpID:   rpID,
		origin: origin,
	}
}

// attestationJSON は register/finish に渡す本文(navigator.credentials.create の応答相当)。
func (a *virtualAuthenticator) attestationJSON(challenge []byte) []byte {
	a.t.Helper()

	clientData := a.clientDataJSON("webauthn.create", challenge)
	attObj, err := cbor.Marshal(map[string]any{
		"fmt":      "none",
		"attStmt":  map[string]any{},
		"authData": a.authData(0x45, 0, true), // UP|UV|AT
	})
	require.NoError(a.t, err)

	return a.marshalBody(map[string]any{
		"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientData),
		"attestationObject": base64.RawURLEncoding.EncodeToString(attObj),
	})
}

// assertionJSON は login/finish に渡す本文(navigator.credentials.get の応答相当)。
func (a *virtualAuthenticator) assertionJSON(challenge []byte, signCount uint32) []byte {
	a.t.Helper()

	clientData := a.clientDataJSON("webauthn.get", challenge)
	authData := a.authData(0x05, signCount, false) // UP|UV
	clientDataHash := sha256.Sum256(clientData)
	digest := sha256.Sum256(append(append([]byte{}, authData...), clientDataHash[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	require.NoError(a.t, err)

	return a.marshalBody(map[string]any{
		"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientData),
		"authenticatorData": base64.RawURLEncoding.EncodeToString(authData),
		"signature":         base64.RawURLEncoding.EncodeToString(sig),
	})
}

func (a *virtualAuthenticator) clientDataJSON(ceremony string, challenge []byte) []byte {
	return fmt.Appendf(nil, `{"type":%q,"challenge":%q,"origin":%q}`,
		ceremony, base64.RawURLEncoding.EncodeToString(challenge), a.origin)
}

func (a *virtualAuthenticator) marshalBody(response map[string]any) []byte {
	a.t.Helper()
	body, err := json.Marshal(map[string]any{
		"id":       base64.RawURLEncoding.EncodeToString(a.credID),
		"rawId":    base64.RawURLEncoding.EncodeToString(a.credID),
		"type":     "public-key",
		"response": response,
	})
	require.NoError(a.t, err)
	return body
}

// authData は authenticator data を組み立てる: rpIdHash(32) | flags(1) | signCount(4)
// [| AAGUID(16) | credIDLen(2) | credID | COSE 公開鍵](attested の場合のみ)。
func (a *virtualAuthenticator) authData(flags byte, signCount uint32, attested bool) []byte {
	a.t.Helper()

	var buf bytes.Buffer
	rpIDHash := sha256.Sum256([]byte(a.rpID))
	buf.Write(rpIDHash[:])
	buf.WriteByte(flags)
	require.NoError(a.t, binary.Write(&buf, binary.BigEndian, signCount))
	if attested {
		buf.Write(make([]byte, 16))                                                       // AAGUID(全ゼロ)
		require.NoError(a.t, binary.Write(&buf, binary.BigEndian, uint16(len(a.credID)))) //nolint:gosec // 固定の短い credID
		buf.Write(a.credID)
		buf.Write(a.cosePublicKey())
	}
	return buf.Bytes()
}

// cosePublicKey は EC2/P-256/ES256 の COSE_Key(CBOR)を返す。
func (a *virtualAuthenticator) cosePublicKey() []byte {
	a.t.Helper()
	// PublicKey.Bytes() は非圧縮形式 0x04 || X(32) || Y(32)
	point, err := a.key.PublicKey.Bytes()
	require.NoError(a.t, err)
	require.Len(a.t, point, 65)
	key, err := cbor.Marshal(map[int]any{
		1:  2,  // kty: EC2
		3:  -7, // alg: ES256
		-1: 1,  // crv: P-256
		-2: point[1:33],
		-3: point[33:65],
	})
	require.NoError(a.t, err)
	return key
}
