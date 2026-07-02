# syntax=docker/dockerfile:1
# Plecto エッジ(ドッグフーディング Phase 1 — tenets §3.5)。
# 公式コンテナイメージは未配布のため、コミットを pin してソースからビルドする。
# pin の更新は意図的に行い、踏んだ問題は Plecto 側に issue/ADR 起票する。

ARG PLECTO_REPO=https://github.com/Kaikei-e/Plecto
ARG PLECTO_REF=a3c0c85af6809a968c22a44dc647a69aad741500

FROM rust:1.96.1-slim-trixie AS build
# cmake / build-essential: 依存の aws-lc-sys(rustls バックエンド)のネイティブビルドに必要
RUN apt-get update && apt-get install -y --no-install-recommends \
        git ca-certificates build-essential cmake perl pkg-config \
    && rm -rf /var/lib/apt/lists/*
ARG PLECTO_REPO
ARG PLECTO_REF
RUN git clone --filter=blob:none ${PLECTO_REPO} /src \
    && git -C /src checkout ${PLECTO_REF}
# Rust workspace はリポジトリの plecto/ サブディレクトリ
WORKDIR /src/plecto
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/src/plecto/target \
    cargo build --release -p plecto-server --bin plecto \
    && install -m 0755 target/release/plecto /usr/local/bin/plecto

FROM debian:trixie-slim
# openssl: entrypoint の開発用自己署名証明書生成 / curl: compose healthcheck
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates openssl curl \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /usr/local/bin/plecto /usr/local/bin/plecto
COPY entrypoint.sh /usr/local/bin/plecto-entrypoint

# 443/tcp: HTTP/1.1 + HTTP/2 (TLS) / 443/udp: HTTP/3 (QUIC) / 9099: admin
EXPOSE 443/tcp 443/udp 9099/tcp
ENTRYPOINT ["/usr/local/bin/plecto-entrypoint"]
