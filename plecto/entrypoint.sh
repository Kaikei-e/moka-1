#!/bin/sh
# One-kick(tenets §2-1): TLS 証明書が無ければ自己署名で生成してから plecto を起動する。
# 証明書は named volume(compose の plecto-certs)に永続化され、再起動では再生成しない。
# 本物の証明書に差し替える場合は同じパス(certs/moka.crt, certs/moka.key)に置くだけでよい。
set -eu

DEPLOY_DIR=/run/plecto
CERT_DIR="${DEPLOY_DIR}/certs"
LISTEN_ADDR="${PLECTO_LISTEN_ADDR:-0.0.0.0:443}"

if [ ! -s "${CERT_DIR}/moka.crt" ] || [ ! -s "${CERT_DIR}/moka.key" ]; then
    echo "generating self-signed dev certificate in ${CERT_DIR}" >&2
    mkdir -p "${CERT_DIR}"
    openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes \
        -keyout "${CERT_DIR}/moka.key" -out "${CERT_DIR}/moka.crt" \
        -days 825 -subj "/CN=moka.localhost" \
        -addext "subjectAltName=DNS:moka.localhost,DNS:localhost,IP:127.0.0.1"
fi

# usage: plecto <manifest.toml> [listen_addr]
# SIGHUP でホットリロード、SIGTERM で graceful shutdown(Plecto ADR 000039)
exec plecto "${DEPLOY_DIR}/manifest.toml" "${LISTEN_ADDR}"
