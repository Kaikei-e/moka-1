# E2E テスト(Hurl + Playwright)

外側から検証する2層(tdd-workflow Phase 0):

- **Hurl** — moka-core の HTTP API(`e2e/hurl/core/`)
- **Playwright** — moka-web の UI からバックエンドまで(`web/tests/e2e/`)

E2E 関連のコンテナ設定は **compose.e2e.yaml(オーバーレイ)に分離**されており、
本番の compose.yaml には含まれない。通常の `docker compose up -d` では e2e-fixtures は存在せず、
moka-core / moka-web のホスト公開も無い。

## 前提

- [Hurl](https://hurl.dev/) がホストにインストールされていること
- Playwright は `cd web && pnpm install && pnpm exec playwright install chromium`
- **フレッシュ DB で走らせる**(201 / 記事件数 / 空状態のアサーションは残留データで壊れる)。
  Hurl と Playwright はどちらも登録シナリオを含むため、**それぞれ別のフレッシュ DB で**実行する
- moka-core は `e2e-db`(compose.e2e.yaml 専用サービス・専用ボリューム `e2e-db-data`)に接続する。
  **本番の `db` / `db-data` には一切書き込まない** — `down -v` で本番の購読データを巻き込む事故がない

## フレッシュ状態にする(e2e-db だけをリセット、本番 db-data には触れない)

```bash
docker compose -f compose.yaml -f compose.e2e.yaml rm -f -s e2e-db e2e-migrate 2>/dev/null || true
docker volume rm moka_e2e-db-data 2>/dev/null || true
```

## Hurl(API 層)

```bash
# 1. e2e-db をフレッシュにする(上記)

# 2. e2e オーバーレイ込みで起動(fixture 配信 nginx + e2e-db + プライベート IP 許可 + 8080 公開)
docker compose -f compose.yaml -f compose.e2e.yaml up -d --build --wait moka-core e2e-fixtures

# 3. Hurl 実行(DB 依存シナリオなので --jobs 1)。要約系(summarize.hurl)は e2e-llm-mock に対して走る
hurl --test --jobs 1 \
  --variable host=http://localhost:8080 \
  --variable fixture_url=http://e2e-fixtures/feed.xml \
  e2e/hurl/core/feeds_and_articles.hurl e2e/hurl/core/summarize.hurl
```

## Playwright(UI 層)

```bash
# 1. e2e-db をフレッシュにする(上記)

# 2. moka-web 込みで起動(3000 公開 + ORIGIN 上書きは compose.e2e.yaml)
docker compose -f compose.yaml -f compose.e2e.yaml up -d --build --wait moka-core moka-web e2e-fixtures

# 3. Playwright 実行(workers=1 固定、playwright.config.ts)
cd web && pnpm test:e2e
```

## 片付け(e2e で足したサービス・公開ポートを残さない)

```bash
docker compose -f compose.yaml -f compose.e2e.yaml stop e2e-db e2e-migrate e2e-llm-mock e2e-fixtures moka-core moka-web
docker compose -f compose.yaml -f compose.e2e.yaml rm -f e2e-db e2e-migrate e2e-llm-mock e2e-fixtures
```

`down`(オーバーレイ全体)は本番の `plecto` / `llm` / `db` / `migrate` も一緒に落ちるので、
通常はここまでの `stop` + `rm` に留める。本番スタックごと畳みたい時だけ
`docker compose -f compose.yaml -f compose.e2e.yaml down`(`-v` は付けない — 付けると本番 `db-data` も消える)。

## 構成メモ

- `MOKA_FEED_ALLOW_PRIVATE=true`、`127.0.0.1:8080:8080` / `127.0.0.1:3000:3000` の公開、
  `ORIGIN=http://localhost:3000`、`DATABASE_URL`(e2e-db 向け)は compose.e2e.yaml のみ。
  本番デフォルトは SSRF ガード有効・moka-core / moka-web 非公開(トラフィックは Plecto 経由)・`db` 接続
- `e2e-db` / `e2e-migrate` / `e2e-llm-mock` は compose.e2e.yaml でしか定義されない(常駐サービス
  5 上限の勘定外、tenets §2-3 — e2e-fixtures と同じ扱い)。本番の `db` / `migrate` / `llm` も
  e2e 実行時に依存関係上一緒に起動しうるが、moka-core は接続しないので触れられない
- `e2e-llm-mock`(`e2e/mock-llm/mock_llm.py`)は本物の llm(iGPU Vulkan passthrough が必要)の
  代わりに決定的な要約文字列を返す OpenAI 互換モック。GitHub-hosted runner に GPU が無いための
  代替で、moka-core → LLM クライアント → DB保存 → API → UI の配線は実コードのまま検証する
  (推論品質そのものは eval/ の管轄。fail-soft 設計により moka-core は本番では `llm` に依存しないが、
  e2e 限定で `LLM_BASE_URL` を e2e-llm-mock へ向けている)
- nginx は静的ファイルに ETag / Last-Modified を自動付与するので、再登録シナリオが
  条件付き GET(304)の経路を実際に通る
- Playwright がフォームに入れる fixture URL は moka-core が docker ネットワーク内で解決する
  (ブラウザからは触らない)ので、ホスト側の名前解決は不要
- CI では Hurl に `--report-junit reports/junit.xml` を付ける
