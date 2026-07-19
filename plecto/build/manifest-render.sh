#!/bin/sh
# plecto-manifest-render — one-shot ジョブ本体(compose.yaml から実行、alpine 内で走る)。
#
# manifest.tmpl.toml のプレースホルダを埋めて、plecto が読む完成 manifest を
# plecto-runtime volume(/out/manifest.toml)にレンダリングする:
#   __SESSION_HMAC_KEY__   ← Docker secret session_hmac_key(ADR00021 の共有シークレット)
#   __SESSION_AUTH_DIGEST__ / __RATELIMIT_DIGEST__
#                          ← plecto-filters-build が書いた OCI image-manifest digest
#
# Plecto の [filter.config] はインライン文字列のみでファイル間接参照・env 展開が無いため、
# ファイルベース Docker secrets(ADR00003)とはこのジョブで橋渡しする(Phase 2 フィールド
# レポート draft_0004 参照 — Plecto 側へ改善提案予定)。
# 置換値は事前に文字クラス検証する(TOML 文字列へエスケープ無しで埋めるため、fail-closed)。
set -eu

TEMPLATE=/in/manifest.tmpl.toml
OUT=/out
SECRET=/run/secrets/session_hmac_key

fail() {
  echo "ERROR: $1" >&2
  exit 1
}

[ -f "$TEMPLATE" ] || fail "template not mounted at $TEMPLATE"
[ -f "$SECRET" ] || fail "secret not mounted at $SECRET (secrets/README.md の手順で secrets/session_hmac_key.txt を作成すること)"

# 秘密値: 末尾改行だけ許容して除去。TOML basic string に安全な文字のみ・32 文字以上を強制
# (openssl rand -hex 32 はこれを満たす)
HMAC_KEY="$(head -n1 "$SECRET" | tr -d '\r\n')"
printf '%s' "$HMAC_KEY" | grep -Eq '^[A-Za-z0-9_-]{32,}$' \
  || fail "session_hmac_key must be >=32 chars of [A-Za-z0-9_-] (use: openssl rand -hex 32)"

read_digest() { # read_digest <name>
  digest_file="$OUT/filters/$1.digest"
  [ -f "$digest_file" ] || fail "$digest_file not found (run plecto-filters-build first)"
  digest="$(cat "$digest_file")"
  printf '%s' "$digest" | grep -Eq '^sha256:[0-9a-f]{64}$' \
    || fail "$digest_file has malformed digest: $digest"
  printf '%s' "$digest"
}

SESSION_AUTH_DIGEST="$(read_digest session-auth)"
RATELIMIT_DIGEST="$(read_digest ratelimit)"

TMP="$OUT/.manifest.toml.tmp"
sed \
  -e "s|__SESSION_HMAC_KEY__|$HMAC_KEY|" \
  -e "s|__SESSION_AUTH_DIGEST__|$SESSION_AUTH_DIGEST|" \
  -e "s|__RATELIMIT_DIGEST__|$RATELIMIT_DIGEST|" \
  "$TEMPLATE" > "$TMP"

grep -q '__' "$TMP" && fail "unreplaced placeholder remains in rendered manifest"

# plecto は distroless nonroot(uid 65532)。manifest には共有シークレットが含まれるので
# 所有者のみ読み取り可に絞る。mv で差し替え(SIGHUP リロードが中途半端な内容を読まない)
chown 65532:65532 "$TMP"
chmod 0600 "$TMP"
mv "$TMP" "$OUT/manifest.toml"

# /run/plecto を ro でマウントする plecto コンテナ内で、certs 用 named volume の
# mountpoint が実在するように空ディレクトリを掘っておく(Phase 1 field report §3.3 の罠)
mkdir -p "$OUT/certs"

echo "plecto-manifest-render: done (manifest.toml rendered)" >&2
