# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## プロジェクト概要

**moka-1** = エージェント型RSSフィードリーダー。`docker compose up -d` 一発で起動し(One-kick)、常駐エージェントが「取得→濃縮→提示」を無人で回す。[Alt](https://github.com/Kaikei-e/Alt) のミニマリズムAI強化版であり、[Plecto](https://github.com/Kaikei-e/Plecto)(WASM拡張L7ゲートウェイ)のドッグフーディングを兼ねる。

正準ドキュメント:

- `docs/tenets/moka-tenets.md` — 設計原理・アーキテクチャ・決定事項のまとめ(「§N」参照はこの文書)
- `docs/adr/` — ADR00001.md からの連番、日本語、`template.md` 準拠(`moka-adr-writer` スキル)
- `CONTEXT.md`(存在すれば)— ドメイン用語集

## アーキテクチャ

**常駐サービス上限 = 5**(compose.yaml)。one-shot ジョブ(`migrate`、`plecto-certs-init`)は数えない。これを超える機能は新サービスではなく既存コンテナ内のモジュールとして実装する。

| サービス | 実体 | 役割 |
|---|---|---|
| `plecto` | Plecto (Rust + WASM filters) | エッジ: TLS終端、HTTP/1·2·3、ルーティング。全外部トラフィックはここを通る |
| `moka-core` | Go 単一バイナリ (`core/`) | API + 常駐エージェント。取得/濃縮/RAG/ハイライトを **プロセス境界でなくパッケージ境界**(`internal/{feed,enrich,rag,highlight,llm,store,httpapi}`)で分ける |
| `moka-web` | SvelteKit SSR (`web/`) | 読むためのUI |
| `db` | PostgreSQL + pgvector | 記事・フィード・埋め込み・ジョブ状態、すべてここ。検索エンジン/キャッシュは導入しない |
| `llm` | llama.cpp server (Vulkan) | ローカル推論のみ。クラウドAPI不使用 |

**コアパス vs 増強(フェイルソフト)**: フィード取得→正規化→DB書き込みまでがコアパス。要約・タグ・埋め込み(濃縮)以降は増強であり、`llm` が死んでも素のRSSリーダーとして動き続けること。イベントソーシング(CQRS)は無し — スキーマはイミュータブルデータモデル(ADR00002)で、濃縮の成果は INSERT-only のイベント表(`enrichment_attempts` / `article_summaries` 等)への追記、pending は成果イベントの不在から導出する(`enrichment_status` カラムは持たない)。

- 本番では moka-core はホスト非公開(Plecto 経由のみ)。SSRF ガード有効。E2E 用の穴あけは `compose.e2e.yaml` オーバーレイに隔離されている
- スキーマは `db/schema.sql` が単一ソース。Atlas が versioned migrations を生成し、compose の one-shot `migrate` ジョブが起動時に自動適用。moka-core は自前マイグレーション機構を持たない
- `eval/`(Python)はモデル選定・プロンプト評価ハーネス。非コンテナ・ホスト直接実行で、サービス数上限に含めない

## コマンド

### スタック起動

```bash
docker compose up -d --wait   # 初回はモデルDL(~2.5GB)で数分かかる
```

### core/ (Go 1.26) — ローカルCIパリティ

```bash
cd core && gofmt -l . && go vet ./... && go fix ./... && golangci-lint run && go test ./...
go test ./internal/feed -run TestValidateURL   # 単一テスト
```

golangci-lint は **v2 系**(`.golangci.yml` に `version: "2"`、プリセットでなく明示的 enable)。

### web/ (SvelteKit + pnpm)

```bash
cd web
pnpm lint          # prettier --check + eslint
pnpm check         # svelte-check(型)
pnpm test          # vitest --run(ブラウザテストは Playwright)
pnpm format        # prettier --write
```

### eval/ (Python 3.14 / uv + Pyrefly)

```bash
cd eval && uv sync --group dev
uv run ruff format --check . && uv run ruff check . && uv run pyrefly check .
uv run pytest
uv run moka-eval smoke   # 実行系は eval/README.md のランブック参照(計測はGPUシリアル)
```

### db/ (Atlas)

```bash
cd db
atlas migrate diff <name> --env local     # schema.sql との差分から migration 生成
atlas migrate lint --env local --latest 1 # 破壊的変更の検査
# --env local は DATABASE_URL を参照(dev-database は pgvector イメージ、atlas.hcl 参照)
```

### E2E (Hurl + Playwright)

フレッシュDB前提・compose.e2e.yaml オーバーレイ込み。手順は `e2e/README.md`。API 層は Hurl(DB依存シナリオのため `--jobs 1`)、UI 層は Playwright(`web/tests/e2e/`、`cd web && pnpm test:e2e`、workers=1)。両者は別々のフレッシュ DB で走らせる。

## 規約

- **API は `/api/v1/` でバージョニング**(`core/internal/httpapi/mux.go`)
- **スキーマに `updated_at` カラムを作らない** — 目的別タイムスタンプ(`fetched_at`、`summarized_at` 等)を使う
- **秘密値はファイルベース Docker secrets**(`secrets/`、gitignore 済み)。環境変数直書き・コミット禁止。ホスト名等のマシン固有情報もリポジトリ文書に書かない(ハードウェアスペックの記載は可)
- **イメージ・モデルのバージョンは pin**(compose.yaml の llama.cpp 等)。更新は eval/ の実測とセットでのみ行う(llama.cpp はバージョン差で速度が数倍変わる事例あり)
- **外部フィードへのリクエスト間隔 ≥ 5s** を厳守(条件付きGET併用)
- ドキュメント(ADR・tenets・コミュニケーション)は日本語
- 開発フローは `tdd-workflow` スキル(outside-in: E2E → Unit、最後に全変更モジュールでローカルCIパリティ)。言語別の流儀は `bp-go` / `bp-svelte` / `bp-python` / `bp-rust-wasm`、層の切り方は `clean-architecture` スキル参照
