# plecto/ — moka-1 のエッジ

[Plecto](https://github.com/Kaikei-e/Plecto) のドッグフーディング(tenets §1.2 / §3.5)。
外向きの複雑さ(TLS、HTTP/1·2·3、ルーティング、将来の認証・レート制限)をすべてここに押し出す。

## 現在地: Phase 1(素通し)

TLS 終端 + path ルーティングのみ。`/api/*` → moka-core、それ以外 → moka-web。
WASM フィルタ(認証 / レート制限)は Phase 2 で `filters/` に生える。

## ファイル

| ファイル | 役割 |
|---|---|
| `manifest.toml` | ルート・upstream・TLS・listen の宣言。コンテナ内 `/run/plecto/` に ro マウント |
| `certs/` | 自己署名証明書のマウントポイント(named volume で上書き、compose の `plecto-certs-init` が生成) |

公式イメージ `ghcr.io/kaikei-e/plecto` を compose.yaml から直接参照する(digest pin)。
moka-1 側に Dockerfile は無い — ADR00005 の Revisit Trigger が発火し、ソースビルド運用から移行した。

## 運用

```bash
curl -sk https://localhost/            # エッジ経由(自己署名なので -k)
curl -s  http://localhost:9099/readyz  # admin: readiness
curl -s  http://localhost:9099/metrics | grep '^plecto_'

docker compose kill -s HUP plecto      # manifest.toml 編集後のゼロダウンタイム反映
docker compose logs plecto -f          # JSON 構造化ログ + アクセスログ
```

公式イメージは distroless(シェル/openssl 無し)で、`plecto` CLI にも moka-core の
`moka healthz` に相当する自己プローブ用サブコマンドが無いため、compose の `healthcheck:` は
張っていない。上記の `curl` で手動確認するか、`docker compose logs plecto` で起動ログを見る。

## upstream 契約

- 各サービスは **GET /healthz に 200** を返す(通るまでルーティングに入らない = それまで 503)
- upstream へは平文 HTTP/1.1(TLS はエッジで終端)
- `/api` prefix は strip しない — moka-core は `/api/...` のパスで受ける

## Plecto の pin 更新

`compose.yaml` の `plecto.image` のタグと digest を進めて `docker compose pull plecto && docker compose up -d plecto`。
タグは可変(実例: `v0.1.0` が同じ番号のまま指す commit を差し替えたことがある)なので、
`docker buildx imagetools inspect ghcr.io/kaikei-e/plecto:<tag>` で digest を確認してから pin する。
踏んだ問題は回避せず Plecto 側に issue/ADR を起票する(ドッグフーディングの本義)。
