# E2E テスト(Playwright)

外側から検証する2層(tdd-workflow Phase 0)。**どちらも Playwright**(Hurl とシェルスクリプトは廃止 — ADR00024):

| 層     | 置き場所          | 対象                                 | 実行                       |
| ------ | ----------------- | ------------------------------------ | -------------------------- |
| API    | `e2e/tests/api/`  | moka-core の HTTP API                | `cd e2e && pnpm test`      |
| エッジ | `e2e/tests/edge/` | Plecto のセッション認証 + レート制限 | `cd e2e && pnpm test:edge` |
| UI     | `web/tests/e2e/`  | moka-web の UI からバックエンドまで  | `cd web && pnpm test:e2e`  |

E2E 関連のコンテナ設定は **compose.e2e.yaml(オーバーレイ)に分離**されており、
本番の compose.yaml には含まれない。通常の `docker compose up -d` では e2e-fixtures は存在せず、
moka-core / moka-web のホスト公開も無い。

## 前提

- `cd e2e && pnpm install`(API / エッジ層)、`cd web && pnpm install`(UI 層)
- **依存を追加・更新するときは pnpm 11 を使うこと**(CI の `pnpm/action-setup` が 11 で固定)。
  pnpm 11 は supply-chain policy(`minimumReleaseAge` — 公開直後のバージョンを拒否する)を
  **解決時に**適用するが、pnpm 10 は適用しない。pnpm 10 で作ったロックファイルは公開直後の
  バージョンを掴んでしまい、CI の `pnpm install --frozen-lockfile` が
  `ERR_PNPM_MINIMUM_RELEASE_AGE_VIOLATION` で落ちる
- **API / エッジ層はブラウザを使わない**(`request` フィクスチャのみ)ので `playwright install` は不要。
  UI 層だけ `cd web && pnpm exec playwright install chromium` が要る
- **フレッシュ DB で走らせる**(201 / 記事件数 / 空状態のアサーションは残留データで壊れる)。
  API 層と UI 層はどちらも登録シナリオを含むため、**それぞれ別のフレッシュ DB で**実行する
- moka-core は `e2e-db`(compose.e2e.yaml 専用サービス・専用ボリューム `e2e-db-data`)に接続する。
  **本番の `db` / `db-data` には一切書き込まない** — `down -v` で本番の購読データを巻き込む事故がない

## フレッシュ状態にする(e2e-db だけをリセット、本番 db-data には触れない)

```bash
docker compose -f compose.yaml -f compose.e2e.yaml rm -f -s e2e-db e2e-migrate 2>/dev/null || true
docker volume rm moka_e2e-db-data 2>/dev/null || true
```

## API 層

```bash
# 1. e2e-db をフレッシュにする(上記)

# 2. e2e オーバーレイ込みで起動(fixture 配信 nginx + e2e-db + プライベート IP 許可 + 8080 公開)
docker compose -f compose.yaml -f compose.e2e.yaml up -d --build --wait moka-core e2e-fixtures

# 3. 実行(1コマンドで全シナリオ。docker compose を触る spec があるのでリポジトリルートから
#    辿れる場所で走らせること)
cd e2e && pnpm test

# 単一ファイルだけ / 名前で絞る
pnpm exec playwright test tests/api/03-search.spec.ts
pnpm exec playwright test -g '既読'
```

`secrets/session_hmac_key.txt` が必要(`secrets/README.md`)— 無いと `07-auth.spec.ts` は 503 で落ちる。

### 実行順序は**ファイル名の連番**が決める

`playwright.config.ts` は `fullyParallel: false` + `workers: 1`。この2つが揃ってはじめて
Playwright はファイルを**パスの辞書順**で直列実行する。連番は 2 桁ゼロ埋めで固定幅にしてある
(辞書順であって数値順ではないため — `10-` は `2-` より前に来てしまう)。

| #   | ファイル                                | 順序の理由                                                                                   |
| --- | --------------------------------------- | -------------------------------------------------------------------------------------------- |
| 01  | `01-feeds-and-articles.spec.ts`         | 25記事フィクスチャを入れる土台。以降の「ちょうど N 件」の基準                                |
| 02  | `02-summarize.spec.ts`                  | `limit=1` で先頭記事を掴む。順序を崩す登録より前                                             |
| 03  | `03-search.spec.ts`                     | 独自フィードが検索ヒットに混ざる前に走らせる                                                 |
| 04  | `04-qa.spec.ts`                         | 同上。`limit=1` の先頭記事前提                                                               |
| 05  | `05-articles-null-published-at.spec.ts` | NULL 記事が fallback で先頭に来るため、02〜04 の前提を崩す。だから後ろ                       |
| 06  | `06-read-and-feed-delete.spec.ts`       | 末尾で null-pubdate フィードを削除する。厳密なカウントを使う最後                             |
| 07  | `07-auth.spec.ts`                       | フィード・記事に触れないので順序不問(フレッシュ DB 前提のみ)                                 |
| 08  | `08-dedupe-no-304.spec.ts`              | 独自フィードを登録する。以降は厳密なカウントに依存しない                                     |
| 09  | `09-enrich.spec.ts`                     | 独自フィードを登録する                                                                       |
| 10  | `10-scheduler.spec.ts`                  | 独自フィードを登録し、DB を直接いじる                                                        |
| 11  | `11-qa-context-relevance.spec.ts`       | 独自フィードを登録する。e2e-llm-mock が生きている必要がある                                  |
| 12  | `12-rag-failsoft.spec.ts`               | **e2e-llm-mock を止める**。止めている間は enrich.Scheduler も失敗 attempt を積むので必ず最後 |

`retries: 0` にしてあるのは意図的。途中まで DB を変えた状態からやり直しても
「フレッシュ DB でちょうど N 件」という主張は再現しない。

## エッジ(Plecto Phase 2: セッション認証 + レート制限)

エッジ側は本番スタックの一員なので compose.e2e.yaml は不要。Phase 2 の配線
(one-shot ジョブ群がビルドした署名済みフィルタ + レンダリング済み manifest)で
plecto が起動済みであること:

```bash
# 初回・フィルタ/manifest 変更時のみ(secrets/README.md の鍵 2 つが必要)
docker compose run --rm plecto-filters-build
docker compose run --rm plecto-manifest-render
docker compose up -d --wait plecto moka-web

# 実走 — セッション cookie(有効/期限切れ/改竄)は moka-core と同じ契約(ADR00021)で
# secrets の共有シークレットから node:crypto の HMAC-SHA256 で鋳造する。
# 自己署名証明書なので ignoreHTTPSErrors(playwright.edge.config.ts)。
# 末尾のバーストが per-IP の /auth バケットを空にするため連続実行は 10 秒ほど空ける
cd e2e && pnpm test:edge
```

検証内容: 未認証 html GET → 302 `/auth/login` / 非 html → 401 + WWW-Authenticate /
改竄・期限切れ cookie → 401(fail-closed)/ 有効 cookie → 200 / `/auth` は認証除外だが
厳しいバケット(capacity 10)でバースト → 429 + Retry-After / バケットは経路別。

**既知の欠落**: Playwright の `APIRequestContext` は HTTP/1.1 のみを喋る(Node の `http`/`https` 実装、
[playwright#31730](https://github.com/microsoft/playwright/issues/31730) は未実装のままクローズ)。
Plecto の HTTP/2 · HTTP/3 経路はこのスイートでは踏めない — ブラウザ経由の UI 層が h2 を通るのと、
Plecto 側のテストが受け持つ(ADR00024)。

## UI 層

```bash
# 1. e2e-db をフレッシュにする(上記)

# 2. moka-web 込みで起動(3000 公開 + ORIGIN 上書きは compose.e2e.yaml)
docker compose -f compose.yaml -f compose.e2e.yaml up -d --build --wait moka-core moka-web e2e-fixtures

# 3. Playwright 実行(workers=1 固定、web/playwright.config.ts)
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

## ローカル CI パリティ(e2e/)

```bash
cd e2e
pnpm lint    # prettier --check + eslint(eslint-plugin-playwright 込み)
pnpm check   # tsc --noEmit(Playwright 自身は型検査をしないので別途必要)
```

## 構成メモ

- `e2e/support/` は spec が共有するヘルパ。**「複数の spec が同じ形で必要とする手順」だけ**を置き、
  何を検証しているかの主張は spec 側に残す
  - `env.ts` — 接続先とフィクスチャ名の定数
  - `types.ts` — API レスポンス封筒の型(`core/internal/httpapi` の writeJSON と 1:1)
  - `moka-api.ts` — 記事の guid 検索・カーソル一覧など、繰り返し使う手順
  - `sse.ts` — `text/event-stream` のパース。**イベント名の配列**で順序ごと表明できる
    (Hurl は `body contains "event: done"` としか書けなかった)
  - `assertions.ts` — Hurl の `isIsoDate` / `isInteger` / `isFloat` / `matches /.+/` 相当
  - `session-cookie.ts` — セッション署名 cookie の鋳造(ADR00021 の契約。openssl 呼び出しの置き換え)
  - `compose.ts` — `docker compose` 操作(`e2e-llm-mock` の停止/再開、`psql` 直接操作)。
    テスト用の特権操作であることを明示するため、**ここ以外から `child_process` を呼ばない**
  - `fixture-files.ts` — 配信フィクスチャの差し替え。mtime を必ず 1 秒以上進めるので、
    ETag が変わることが決定的に保証される(旧 bash 版は「実行に時間がかかるから mtime がずれる」に
    暗黙に依存していた)
- health gate は `tests/setup/` の setup プロジェクトへ集約した(旧 hurl 群は各ファイル冒頭に
  retry 付き `GET /healthz` を重複して書いていた)。UI モードは既定で setup プロジェクトを
  走らせないので、UI モードで動かす時は フィルタで有効にすること
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
  返るため「空配列」の契約は llm 停止時の `12-rag-failsoft.spec.ts` 側で検証する)
- nginx は静的ファイルに ETag / Last-Modified を自動付与するので、再登録シナリオが
  条件付き GET(304)の経路を実際に通る
- `MOKA_SCHEDULER_TICK_SECONDS=3` / `MOKA_ENRICH_TICK_SECONDS=2`(compose.e2e.yaml のみ)。
  常駐スケジューラ・enrich.Scheduler それぞれの due/pending 判定ポーリング間隔を短縮し、
  `10-scheduler.spec.ts` / `09-enrich.spec.ts` の待ち時間を短くする(本番既定は60秒/15秒)
- Playwright がフォームに入れる fixture URL は moka-core が docker ネットワーク内で解決する
  (ブラウザからは触らない)ので、ホスト側の名前解決は不要
- CI では `reports/junit.xml`(junit reporter)と `playwright-report/` を artifact として回収する
