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

# 3. Hurl 実行(DB 依存シナリオなので --jobs 1)。要約系(summarize.hurl)・Q&A(qa.hurl)は
# e2e-llm-mock に対して走る。search.hurl / qa.hurl は「limit=1 で先頭記事」「フィクスチャ記事
# だけがヒットする」前提を崩す独自フィード登録シナリオより前 = summarize.hurl の直後に置く。
# articles_null_published_at.hurl は「limit=1 で先頭記事を拾う」前提のシナリオ群より後に置く。
# read_and_feed_delete.hurl は末尾で null-pubdate フィードを削除するので hurl 群の最後に置く。
# auth.hurl(パスキー認証 API、M2)はフィード・記事に触れないので順序不問(fresh DB 前提のみ。
# secrets/session_hmac_key.txt が必要 — secrets/README.md)
hurl --test --jobs 1 \
  --variable host=http://localhost:8080 \
  --variable fixture_url=http://e2e-fixtures/feed.xml \
  --variable null_pubdate_fixture_url=http://e2e-fixtures/feed-null-pubdate.xml \
  e2e/hurl/core/feeds_and_articles.hurl e2e/hurl/core/summarize.hurl e2e/hurl/core/search.hurl e2e/hurl/core/qa.hurl e2e/hurl/core/articles_null_published_at.hurl e2e/hurl/core/read_and_feed_delete.hurl e2e/hurl/core/auth.hurl

# 4. 条件付きGETが効かない(=毎回200で同じ内容を返す)フィードの再取得でも articles.id の
# 欠番が増えないことの検証。独自フィードを登録するので、上記の厳密なカウントアサーションの後に走らせる
bash e2e/hurl/core/dedupe_no_304_e2e.sh

# 5. enrich.Scheduler(常駐エージェントループの濃縮ステップ)が、手動の POST 無しに
# 新着記事へ summary/tags を自動で付けることの検証(M1)。独自フィードを登録する
bash e2e/hurl/core/enrich_e2e.sh

# 6. 常駐スケジューラ(バックグラウンド自律取得)の検証は独自フィードを登録するので、
# 上記の「ちょうどN件」前提の厳密なカウントアサーションを崩さないよう必ず最後に走らせる
bash e2e/hurl/core/scheduler_e2e.sh

# 7. llm 完全停止時のフェイルソフト検証(M2): e2e-llm-mock を止めて、検索のテキスト単独 200 と
# Q&A の SSE error イベントを確認し、終了時にモックを再開する。停止中は enrich.Scheduler も
# 失敗 attempt を積むため、必ず一番最後に走らせる
bash e2e/hurl/core/rag_failsoft_e2e.sh
```

## Playwright(UI 層)

```bash
# 1. e2e-db をフレッシュにする(上記)

# 2. moka-web 込みで起動(3000 公開 + ORIGIN 上書きは compose.e2e.yaml)
docker compose -f compose.yaml -f compose.e2e.yaml up -d --build --wait moka-core moka-web e2e-fixtures

# 3. Playwright 実行(workers=1 固定、playwright.config.ts)
cd web && pnpm test:e2e
```

## エッジ(Plecto Phase 2: セッション認証 + レート制限 — `e2e/hurl/edge/`)

エッジ側は本番スタックの一員なので compose.e2e.yaml は不要。Phase 2 の配線
(one-shot ジョブ群がビルドした署名済みフィルタ + レンダリング済み manifest)で
plecto が起動済みであること:

```bash
# 初回・フィルタ/manifest 変更時のみ(secrets/README.md の鍵 2 つが必要)
docker compose run --rm plecto-filters-build
docker compose run --rm plecto-manifest-render
docker compose up -d --wait plecto moka-web

# 実走 — セッション cookie(有効/期限切れ/改竄)は moka-core と同じ契約(ADR00021)で
# secrets の共有シークレットから鋳造する。自己署名証明書なので hurl は --insecure。
# 末尾のバーストが per-IP の /auth バケットを空にするため連続実行は 10 秒ほど空ける
bash e2e/hurl/edge/edge_auth_e2e.sh
```

検証内容: 未認証 html GET → 302 `/auth/login` / 非 html → 401 + WWW-Authenticate /
改竄・期限切れ cookie → 401(fail-closed)/ 有効 cookie → 200 / `/auth` は認証除外だが
厳しいバケット(capacity 10)でバースト → 429 + Retry-After / バケットは経路別。

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
  `ORIGIN=http://localhost:3000`、`DATABASE_URL`(e2e-db 向け)、
  `WEBAUTHN_ORIGIN=http://localhost:3000` / `WEBAUTHN_RP_ID=localhost`(Playwright の
  パスキー完走ジャーニー用 — 本番既定は https://localhost / localhost)は compose.e2e.yaml のみ。
  本番デフォルトは SSRF ガード有効・moka-core / moka-web 非公開(トラフィックは Plecto 経由)・`db` 接続。
  セッション署名鍵は本番と同じ配線(`SESSION_HMAC_KEY_FILE` + secret `session_hmac_key`)を
  ベース compose.yaml から継承する — `secrets/session_hmac_key.txt` が無いと auth 系 E2E は 503 になる
- `e2e-db` / `e2e-migrate` / `e2e-llm-mock` は compose.e2e.yaml でしか定義されない(常駐サービス
  5 上限の勘定外、tenets §2-3 — e2e-fixtures と同じ扱い)。本番の `db` / `migrate` / `llm` も
  e2e 実行時に依存関係上一緒に起動しうるが、moka-core は接続しないので触れられない
- `e2e-llm-mock`(`e2e/mock-llm/mock_llm.py`)は本物の llm(iGPU Vulkan passthrough が必要)の
  代わりに決定的な応答を返す OpenAI 互換モック。GitHub-hosted runner に GPU が無いための
  代替で、moka-core → LLM クライアント → DB保存 → API → UI の配線は実コードのまま検証する
  (推論品質そのものは eval/ の管轄。fail-soft 設計により moka-core は本番では `llm` に依存しないが、
  e2e 限定で `LLM_BASE_URL` を e2e-llm-mock へ向けている)。`response_format`(json_schema、
  タグ抽出が使う)を検知したら決定的な `{"tags": [...]}` を返し、それ以外(要約)は固定文言を返す。
  `POST /embeddings`(OpenAI 互換、1024次元)も実装しており、入力文字列の 3-gram feature
  hashing から決定的なベクトルを作る — enrich.Scheduler の埋め込み濃縮とハイブリッド検索の
  ベクトル側が e2e で実際に成功する(埋め込みが効くと、ヒットなしクエリでも cosine 近傍が
  返るため「空配列」の契約は llm 停止時の rag_failsoft.hurl 側で検証する)
- nginx は静的ファイルに ETag / Last-Modified を自動付与するので、再登録シナリオが
  条件付き GET(304)の経路を実際に通る
- `MOKA_SCHEDULER_TICK_SECONDS=3` / `MOKA_ENRICH_TICK_SECONDS=2`(compose.e2e.yaml のみ)。
  常駐スケジューラ・enrich.Scheduler それぞれの due/pending 判定ポーリング間隔を短縮し、
  `scheduler_e2e.sh` / `enrich_e2e.sh` の待ち時間を短くする(本番既定は60秒/15秒)
- Playwright がフォームに入れる fixture URL は moka-core が docker ネットワーク内で解決する
  (ブラウザからは触らない)ので、ホスト側の名前解決は不要
- CI では Hurl に `--report-junit reports/junit.xml` を付ける
