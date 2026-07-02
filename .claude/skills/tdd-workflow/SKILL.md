---
name: tdd-workflow
description: moka-1 の Test-Driven Development ワークフロー。新機能実装・バグ修正・リファクタリング時、またはユーザーが「TDDで」と言った時に使用する。outside-in(E2E → Unit)の順で失敗するテストから書き、RED-GREEN-REFACTOR を回し、最後にローカル CI パリティ(format/lint/type/test)を全変更モジュールで通す。
allowed-tools: Bash, Read, Glob, Grep, Edit, Write
argument-hint: <feature-description>
---

# TDD Workflow (moka-1)

**順序は outside-in(E2E → Unit)、量はテストピラミッド(E2E 少・Unit 多)**
([Fowler — Practical Test Pyramid](https://martinfowler.com/articles/practical-test-pyramid.html))。
順序と量は別の軸で、同時に適用する。Plan mode でも実装 mode でも、コードやテストが変わりうる作業では従うこと。

Alt との違い — **CDC(Pact)フェーズはない**:
- バックエンドは moka-core 単体。契約を交わすサービス境界が存在しない
- Plecto フィルタとの境界は WIT(`plecto:filter`)が型レベルで契約を強制する。契約テストの代わりに Plecto のカンフォーマンステストと E2E で守る
- moka-web ⇄ moka-core は同一リポジトリ・同時デプロイなので、E2E + Zod スキーマ検証で足りる

## Phase 0: E2E FIRST

**目的**: 変更が届けるべき外部から観測可能な振る舞いを、最外殻の失敗するテストとして先に固定する(executable specification)。

### 判定木

| 変更対象 | ツール | 置き場所 |
|---|---|---|
| moka-web の UI / ユーザーフロー | Playwright | `web/tests/e2e/*.spec.ts` |
| moka-core の HTTP API | Hurl | `e2e/hurl/core/*.hurl` |
| Plecto フィルタ(認証・レート制限等) | Hurl(Plecto 経由で叩く) | `e2e/hurl/edge/*.hurl` |
| フルスタック(UI が新 API を呼ぶ) | 両方: Playwright 1 本 + Hurl 1 本 | 上記両方 |
| 内部リファクタ(外部振る舞い不変) | **Phase 0 スキップ** → Phase 1 へ | — |
| エージェントループの無人動作(ハイライト生成等) | Hurl(トリガー API + 結果ポーリング) | `e2e/hurl/agent/*.hurl` |

### Playwright の書き方

- **ロケータ**: `getByRole` / `getByLabel` / `getByText` / `getByTestId`。CSS / XPath 禁止
- **web-first assertion**: `await expect(locator).toBeVisible()`。`expect(await locator.isVisible()).toBe(true)` は禁止(auto-wait が効かない)
- `waitForTimeout`・手動リトライループ禁止。auto-waiting を信頼する
- 1 `test()` = 1 ユーザージャーニー。fresh browser context で分離。共有 setup は `beforeEach`
- 認証が要るフローは `storageState` でセッションを再利用(Plecto 認証フィルタ導入後)
- 決定的に: フィード・記事は fixture で seed する。「DB に入っている何か」に依存しない
- LLM 依存の表示(要約・ハイライト)は **存在と形をアサートし、内容の文言をアサートしない**

### Hurl の書き方

- ホスト・トークンは `--variable host=...` で渡す。`http://localhost:...` のハードコード禁止
- ビジネスエンドポイントの前に health-gate: `--retry 10 --retry-interval 2000` で `/healthz` を待つ
- **DB 依存シナリオは `--jobs 1`**(FK / sequence の順序が並列で壊れる)
- アサーション: 暗黙(status / headers)→ 明示 `jsonpath` の順。`contains` / `matches /regex/` / `isIsoDate` / `isUuid` / `count == N` を使い分ける
- リクエスト連鎖は `[Captures]` + `{{var}}`。setup をファイル間で複製しない
- CI フラグ: `--test --report-junit reports/junit.xml`

### 手順

1. 判定木でスコープ決定
2. 失敗する E2E を書く(隣のファイルをテンプレートに)
3. 実行して **RED の理由を確認する**: 「振る舞いが未実装」で落ちるのが正解。404 / connection refused / compose 未起動 / typo で落ちているなら、それは RED ではなく環境不備。先に直す
4. 失敗する E2E を単独 commit: `test(e2e): add failing <feature> scenario`
5. Phase 1 へ

## Phase 1: RED(失敗する Unit テスト)

E2E が示す振る舞いを、実装単位の失敗するテストに分解する。

| モジュール | パターン |
|---|---|
| core/ (Go) | テーブル駆動 + `t.Run` + `t.Parallel()`。時間依存(スケジューラ・backoff)は `testing/synctest` で仮想時計。LLM は `httptest.NewServer` のフェイク(bp-go §27) |
| plecto/filters (Rust) | 判定ロジックを host API 非依存の純関数に切り出し、native `cargo test`。WIT 境界そのものは E2E に任せる |
| web/ (Svelte/TS) | vitest。コンポーネントは vitest-browser-svelte(実ブラウザ)、純ロジックは node 環境 |
| eval/ (Python) | pytest(評価基盤のロジックにテストが要る場合のみ) |

原則:
- **1 テスト = 1 振る舞い**。テスト名は仕様文(`"skips enrichment when llm is down"`)
- モック層は薄く: 消費側 interface(Port)の境界だけ。深いモックの入れ子はテスト対象の設計が悪いサイン — 先に設計を直す
- 実行して RED を確認してから次へ。**書いたテストが最初から green なら、それはテストが間違っている**

## Phase 2: GREEN(最小実装)

- テストを通す**最小限**のコードだけ書く。汎用化・最適化・きれいにする作業はしない(Phase 3 の仕事)
- 「仮実装(ベタ書き)→ 三角測量(2 例目で一般化)」を恐れない
- **禁止**: テストの期待値を実装に合わせて書き換えて green にすること。RED の期待値が間違っていたと分かった場合は、その理由をユーザーに報告してから直す
- 全テスト(新規 + 既存)green を確認

## Phase 3: REFACTOR

- green を保ったまま: 重複除去 → 命名 → 構造。1 リファクタごとにテスト再実行
- テストコード自体もリファクタ対象(fixture 抽出、テーブル整理)
- 「ついでの機能追加」はしない。見つけた別の問題は報告して別タスクに切る

## Phase 4: ローカル CI パリティ(完了宣言の前に必須)

「テストが通る」≠「CI が通る」。**触ったモジュールすべて**で以下を回す:

```bash
# core/ (Go) — golangci-lint は v2 系(.golangci.yml に version: "2")
cd core && gofmt -l . && go vet ./... && go fix ./... && golangci-lint run && go test ./...

# db/ (Atlas — スキーマ / マイグレーションを触った場合のみ。ADR00001)
cd db && atlas migrate validate && atlas migrate lint --latest 1

# plecto/filters (Rust)
cd plecto/filters && cargo fmt --check && cargo clippy --target wasm32-wasip2 -- -D warnings \
  && cargo test && cargo build --target wasm32-wasip2 --release

# web/ (Svelte/TS)
cd web && bun run check && bun run lint && bun test

# eval/ (Python)
cd eval && uv run ruff format --check . && uv run ruff check . && uv run pyrefly check .
```

- 全部 green になってから完了を報告する
- 落ちたものは**落ちた出力とともに**報告する。勝手に「完了」と言わない。lint の自動修正(`ruff format`、`gofmt -w`)は掛けてよいが、掛けたことを報告する

## バグ修正の場合

1. **再現する失敗テストを先に書く**(バグの層に応じて E2E か Unit)。これが regression guard になる
2. 修正 → green → Phase 3, 4 は同じ
3. 「テストなしの 1 行修正」はしない。テストが書けない構造なら、それ自体を報告する

## 完了報告に含めるもの

- 書いたテスト(パスと本数)と、それが守る振る舞いの 1 行説明
- 各 Phase の green 証跡(実行したコマンドと結果)
- スキップした Phase とその理由(判定木のどれに該当したか)
