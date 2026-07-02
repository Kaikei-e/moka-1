# syntax=docker/dockerfile:1
# moka-core: Go 単一バイナリ(API + 常駐エージェント)
FROM golang:1.26.4-trixie AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -o /out/moka ./cmd/moka

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/moka /moka
EXPOSE 8080
# healthcheck は `moka healthz` サブコマンド(distroless に curl は無い)
ENTRYPOINT ["/moka"]
