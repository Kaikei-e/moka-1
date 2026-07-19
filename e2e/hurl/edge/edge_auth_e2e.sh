#!/usr/bin/env bash
# エッジ(Plecto Phase 2)のセッション認証 + レート制限の実走検証(edge_auth.hurl)。
#
# 前提: 本番スタックのエッジ側が Phase 2 の配線で起動済みであること —
#   docker compose run --rm plecto-filters-build
#   docker compose run --rm plecto-manifest-render
#   docker compose up -d --wait plecto moka-web
# (secrets/session_hmac_key.txt / secrets/plecto_filter_signing_key.pem が必要 —
#  secrets/README.md。自己署名証明書なので hurl は --insecure)
#
# セッション cookie は moka-core と同じ契約(ADR00021 / core/internal/auth/session.go:
#   v1.<exp_unix_ms>.<base64url_nopad(HMAC-SHA256(trim(secret), "v1."+exp_unix_ms))>)で
# openssl により鋳造する。フィルタ側の検証(署名・期限)だけを相手にするので moka-core は不要。
#
# 末尾のバーストシナリオが per-IP の /auth バケット(capacity 10, refill 1/s)を空にするため、
# 連続実行する場合は 10 秒ほど空けること。
# リポジトリルートから実行すること: bash e2e/hurl/edge/edge_auth_e2e.sh
set -euo pipefail

EDGE_HOST="${EDGE_HOST:-https://localhost}"
KEY_FILE="${SESSION_HMAC_KEY_FILE:-secrets/session_hmac_key.txt}"

if [ ! -s "$KEY_FILE" ]; then
  echo "edge_auth_e2e: $KEY_FILE not found (see secrets/README.md)" >&2
  exit 1
fi

# 鍵の契約: trim 後の文字列の UTF-8 バイト列をそのまま HMAC 鍵にする(hex デコードしない)
key=$(tr -d '[:space:]' < "$KEY_FILE")

# base64url_nopad(HMAC-SHA256(key, payload)) — cookie 契約の署名部
sign() {
  printf '%s' "$1" | openssl dgst -sha256 -hmac "$key" -binary \
    | openssl base64 -A | tr '+/' '-_' | tr -d '='
}

now_ms=$(($(date +%s) * 1000))
valid_payload="v1.$((now_ms + 3600 * 1000))"   # 1時間先まで有効
expired_payload="v1.$((now_ms - 3600 * 1000))" # 1時間前に失効(署名自体は正しい)

valid_cookie="${valid_payload}.$(sign "$valid_payload")"
expired_cookie="${expired_payload}.$(sign "$expired_payload")"
# 改竄: 有効な署名を別ペイロードに付け替える(署名検証で必ず落ちる)
tampered_cookie="${valid_payload}.$(sign "$expired_payload")"

hurl --test --jobs 1 --insecure \
  --variable edge_host="$EDGE_HOST" \
  --variable valid_cookie="$valid_cookie" \
  --variable expired_cookie="$expired_cookie" \
  --variable tampered_cookie="$tampered_cookie" \
  e2e/hurl/edge/edge_auth.hurl

echo "edge_auth_e2e: ok"
