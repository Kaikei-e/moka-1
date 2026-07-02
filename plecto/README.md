# plecto/ — moka-1 のエッジ

[Plecto](https://github.com/Kaikei-e/Plecto) のドッグフーディング(tenets §1.2 / §3.5)。
外向きの複雑さ(TLS、HTTP/1·2·3、ルーティング、将来の認証・レート制限)をすべてここに押し出す。

## 現在地: Phase 1(素通し)

TLS 終端 + path ルーティングのみ。`/api/*` → moka-core、それ以外 → moka-web。
WASM フィルタ(認証 / レート制限)は Phase 2 で `filters/` に生える。

## ファイル

| ファイル | 役割 |
|---|---|
| `Dockerfile` | 公式イメージ未配布のため、pin したコミットからソースビルド(`PLECTO_REF`) |
| `manifest.toml` | ルート・upstream・TLS の宣言。コンテナ内 `/run/plecto/` に ro マウント |
| `entrypoint.sh` | 起動時に自己署名証明書を生成(named volume に永続化)→ `plecto` 起動 |

## 運用

```bash
curl -sk https://localhost/            # エッジ経由(自己署名なので -k)
curl -s  http://localhost:9099/readyz  # admin: readiness
curl -s  http://localhost:9099/metrics | grep '^plecto_'

docker compose kill -s HUP plecto      # manifest.toml 編集後のゼロダウンタイム反映
docker compose logs plecto -f          # JSON 構造化ログ + アクセスログ
```

## upstream 契約

- 各サービスは **GET /healthz に 200** を返す(通るまでルーティングに入らない = それまで 503)
- upstream へは平文 HTTP/1.1(TLS はエッジで終端)
- `/api` prefix は strip しない — moka-core は `/api/...` のパスで受ける

## Plecto の pin 更新

`Dockerfile` の `PLECTO_REF` を進めて `docker compose build plecto`。
踏んだ問題は回避せず Plecto 側に issue/ADR を起票する(ドッグフーディングの本義)。
