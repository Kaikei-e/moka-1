#!/bin/bash
# plecto-filters-build — one-shot ジョブ本体(compose.yaml から実行、rust イメージ内で走る)。
#
# WASM フィルタ 2 枚(session-auth / ratelimit)を
#   cargo build (wasm32-unknown-unknown) → wasm-tools component new → wasi:* import ゼロ検査
#   → ECDSA P-256 署名(Docker secret の dev 鍵)→ 署名済みローカル OCI image-layout 生成
# まで行い、plecto-runtime volume(/out)に出力する。digest は /out/filters/<name>.digest に
# 書き、後続の plecto-manifest-render が manifest の digest pin に埋め込む。
#
# Plecto はフィルタを「署名済み OCI image-layout + digest pin + [trust] 公開鍵」でしか
# ロードしない(fail-closed、Plecto ADR 000006/000007)。レイアウトの構造は
# Plecto crates/control/src/oci.rs(write_layout)と同一:
#   blobs/sha256/<hex> に config({}) / component / 署名 / SBOM / SBOM 署名 / image manifest、
#   index.json が image manifest を指し、その digest が manifest の pin になる。
# 署名は cosign 既定と同じ ECDSA P-256 + SHA-256 の raw DER(openssl dgst -sign で生成可能)。
# SBOM は component の sha256 を subject に持つ最小の in-toto statement(host の
# SBOM↔component binding 検査を満たす — dev_signer.rs bound_sbom と同形)。
set -euo pipefail

SRC=/src            # ./plecto/filters(ro)
OUT=/out            # plecto-runtime named volume
CACHE=/cache        # plecto-build-cache named volume(cargo registry / target / wasm-tools)
SIGNING_KEY=/run/secrets/plecto_filter_signing_key

# cargo のレジストリ・ビルド生成物は named volume に置き、2 回目以降を高速化する。
# rustup(ツールチェーン本体)はイメージ側のまま — イメージ pin 更新で入れ替わるべきもの
export CARGO_HOME="$CACHE/cargo"
export CARGO_TARGET_DIR="$CACHE/target"

# wasm-tools はプリビルドバイナリを pin + sha256 検証で取得(cargo install はコンパイルに
# 数分かかるため)。バージョン更新時は 2 行セットで進めること
WASM_TOOLS_VERSION=1.253.0
WASM_TOOLS_SHA256=4e2898f7ca3bd0536218ed9b7b36ff7b86954c57ae0e6272fde69728cbe01088

[ -f "$SIGNING_KEY" ] || {
  echo "ERROR: signing key secret not mounted at $SIGNING_KEY" >&2
  echo "  (secrets/README.md の手順で secrets/plecto_filter_signing_key.pem を作成すること)" >&2
  exit 1
}

# --- 1. ツールチェーン整備 ------------------------------------------------------------
rustup target add wasm32-unknown-unknown

install_wasm_tools() {
  local dir="$CACHE/bin" tar="wasm-tools-${WASM_TOOLS_VERSION}-x86_64-linux.tar.gz"
  if [ -x "$dir/wasm-tools" ] && "$dir/wasm-tools" --version | grep -q "wasm-tools $WASM_TOOLS_VERSION"; then
    return
  fi
  echo "fetching wasm-tools $WASM_TOOLS_VERSION" >&2
  mkdir -p "$dir"
  local tmp
  tmp="$(mktemp -d)"
  curl -fsSL --retry 3 -o "$tmp/$tar" \
    "https://github.com/bytecodealliance/wasm-tools/releases/download/v${WASM_TOOLS_VERSION}/${tar}"
  echo "$WASM_TOOLS_SHA256  $tmp/$tar" | sha256sum -c - >/dev/null
  tar -xzf "$tmp/$tar" -C "$tmp"
  install -m 0755 "$tmp/wasm-tools-${WASM_TOOLS_VERSION}-x86_64-linux/wasm-tools" "$dir/wasm-tools"
  rm -rf "$tmp"
}
install_wasm_tools
export PATH="$CACHE/bin:$PATH"

# --- 2. ビルド + component 化 ---------------------------------------------------------
# ソースは ro マウント。--locked で Cargo.lock(コミット済み)からの逸脱を拒否する
cargo build --manifest-path "$SRC/Cargo.toml" --locked \
  --target wasm32-unknown-unknown --release

WORK="$(mktemp -d)"

componentize() { # componentize <crate_name> -> $WORK/<crate>.component.wasm
  local crate="$1"
  wasm-tools component new \
    "$CARGO_TARGET_DIR/wasm32-unknown-unknown/release/${crate}.wasm" \
    -o "$WORK/${crate}.component.wasm"
  # deny-by-default の検査: import は plecto:filter/* のみ、wasi:* が 1 つでもあれば失敗
  # (examples/filters/*/build.sh と同じゲート)
  local wit
  wit="$(wasm-tools component wit "$WORK/${crate}.component.wasm")"
  if printf '%s' "$wit" | grep -q 'wasi:'; then
    echo "ERROR: ${crate} component imports wasi:* (must be zero-WASI):" >&2
    printf '%s\n' "$wit" | grep 'wasi:' >&2
    exit 1
  fi
  if printf '%s' "$wit" | grep 'import' | grep -qv 'plecto:filter/'; then
    echo "ERROR: ${crate} component has imports outside plecto:filter/*:" >&2
    printf '%s\n' "$wit" | grep 'import' >&2
    exit 1
  fi
}

# --- 3. 署名 + OCI image-layout -------------------------------------------------------
sha256_hex() { sha256sum "$1" | cut -d' ' -f1; }

write_blob() { # write_blob <layout_dir> <file> -> hex を stdout に返す
  local layout="$1" file="$2" hex
  hex="$(sha256_hex "$file")"
  cp "$file" "$layout/blobs/sha256/$hex"
  printf '%s' "$hex"
}

descriptor() { # descriptor <media_type> <hex> <size> — OCI descriptor JSON(改行なし)
  printf '{"mediaType":"%s","digest":"sha256:%s","size":%s}' "$1" "$2" "$3"
}

package_filter() { # package_filter <crate_name> <out_name>
  local crate="$1" name="$2"
  local component="$WORK/${crate}.component.wasm"
  local layout="$OUT/filters/.tmp-${name}"
  rm -rf "$layout"
  mkdir -p "$layout/blobs/sha256"

  # 署名(cosign sign-blob 相当): ECDSA P-256 + SHA-256、raw DER
  openssl dgst -sha256 -sign "$SIGNING_KEY" -out "$WORK/${name}.sig" "$component"
  # SBOM: component の sha256 を subject に束縛した最小 in-toto statement
  printf '{"_type":"https://in-toto.io/Statement/v1","subject":[{"name":"filter","digest":{"sha256":"%s"}}],"predicateType":"https://cyclonedx.org/bom","predicate":{}}' \
    "$(sha256_hex "$component")" > "$WORK/${name}.sbom"
  openssl dgst -sha256 -sign "$SIGNING_KEY" -out "$WORK/${name}.sbom.sig" "$WORK/${name}.sbom"

  printf '{"imageLayoutVersion":"1.0.0"}' > "$layout/oci-layout"
  printf '{}' > "$WORK/config.json"

  local config_hex component_hex sig_hex sbom_hex sbom_sig_hex
  config_hex="$(write_blob "$layout" "$WORK/config.json")"
  component_hex="$(write_blob "$layout" "$component")"
  sig_hex="$(write_blob "$layout" "$WORK/${name}.sig")"
  sbom_hex="$(write_blob "$layout" "$WORK/${name}.sbom")"
  sbom_sig_hex="$(write_blob "$layout" "$WORK/${name}.sbom.sig")"

  # image manifest(layer の並びと mediaType は Plecto oci.rs と同一)
  printf '{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":%s,"layers":[%s,%s,%s,%s]}' \
    "$(descriptor application/vnd.wasm.config.v0+json "$config_hex" "$(stat -c%s "$WORK/config.json")")" \
    "$(descriptor application/wasm "$component_hex" "$(stat -c%s "$component")")" \
    "$(descriptor application/vnd.plecto.signature "$sig_hex" "$(stat -c%s "$WORK/${name}.sig")")" \
    "$(descriptor application/vnd.plecto.sbom "$sbom_hex" "$(stat -c%s "$WORK/${name}.sbom")")" \
    "$(descriptor application/vnd.plecto.sbom.signature "$sbom_sig_hex" "$(stat -c%s "$WORK/${name}.sbom.sig")")" \
    > "$WORK/${name}.manifest.json"
  local manifest_hex
  manifest_hex="$(write_blob "$layout" "$WORK/${name}.manifest.json")"
  printf '{"schemaVersion":2,"manifests":[%s]}' \
    "$(descriptor application/vnd.oci.image.manifest.v1+json "$manifest_hex" "$(stat -c%s "$WORK/${name}.manifest.json")")" \
    > "$layout/index.json"

  # 差し替えは最後に一括で(plecto は SIGHUP まで旧 manifest+旧 digest を読むので、
  # 旧レイアウトを消すのは新レイアウトが完成してから)
  rm -rf "$OUT/filters/$name"
  mv "$layout" "$OUT/filters/$name"
  printf 'sha256:%s' "$manifest_hex" > "$OUT/filters/${name}.digest"
  echo "packaged $name -> sha256:$manifest_hex" >&2
}

mkdir -p "$OUT/filters" "$OUT/keys" "$OUT/certs"
componentize session_auth
componentize ratelimit
package_filter session_auth session-auth
package_filter ratelimit ratelimit

# [trust] が参照する公開鍵(SPKI PEM)を秘密鍵から導出して runtime volume へ
openssl pkey -in "$SIGNING_KEY" -pubout -out "$OUT/keys/filter-signer.pub"

# plecto コンテナは distroless nonroot(uid 65532)で読む。全部読めるようにしておく
# (manifest.toml の所有権・権限は plecto-manifest-render が絞る)
chmod -R a+rX "$OUT/filters" "$OUT/keys"
rm -rf "$WORK"
echo "plecto-filters-build: done" >&2
