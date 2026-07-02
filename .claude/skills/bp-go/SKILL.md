---
name: bp-go
description: Go ベストプラクティス(moka-core 向け、Go 1.26+)。常駐エージェントループ・LLM クライアント・フィード取得を含む moka-core の実装規約。
  TRIGGER when: .go ファイルを編集・作成する時、Go コードを書く時、moka-core(core/ 以下)を実装する時。
  DO NOT TRIGGER when: テストの実行のみ、go.mod の確認のみ、ファイルの読み取りのみ、他言語の作業時。
---

# Go Best Practices (moka-core, Go 1.26+)

## ツールチェーン

- **Go 1.26+**: Green Tea GC がデフォルト。小オブジェクト多量(記事構造体)のワークロードに効くので無効化しない
- **GOMAXPROCS は触らない**: Go 1.25+ はコンテナの CPU 制限を自動認識する。compose の `cpus:` 制限と整合
- **golangci-lint は v2**: `.golangci.yml` 先頭に `version: "2"`。プリセット依存でなく明示的に有効化:
  `govet, errcheck, staticcheck, revive, testifylint, thelper, paralleltest, sloglint, bodyclose, noctx, errorlint, copyloopvar`
- 修正候補は `go fix ./...`(1.26 で go vet と同じ解析基盤に刷新)を先に流す

## プロジェクト構造

```
core/
├── cmd/moka/main.go          # 薄く: config → deps → 配線 → 起動 → signal 待機
└── internal/
    ├── feed/                 # 取得・正規化・スケジューラ
    ├── enrich/               # 要約・タグ・埋め込みのキュー
    ├── rag/                  # ハイブリッド検索・Q&A
    ├── highlight/            # MoA(提案3系統→集約)
    ├── llm/                  # LLM クライアント(唯一の LLM 接点)
    ├── store/                # pgx + マイグレーション
    └── httpapi/              # HTTP ハンドラ
```

- **パッケージ境界 = モジュール境界**(tenets §3.1)。Alt のプロセス境界をパッケージ境界に畳む
- **interface は消費側で定義**: 「accept interfaces, return structs」。`enrich` が LLM を使うなら `enrich` 側に必要最小の interface を書き、`llm` の具象を注入
- HTTP ルーティングは **stdlib の `http.ServeMux`**(1.22+ のメソッド+パスパターン)。Echo/chi 等は入れない(ミニマリズム)

## エラー処理

1. **ラップ必須**: `fmt.Errorf("fetch feed %s: %w", url, err)`。裸の `return nil, err` 禁止
2. **判定は errors.Is / errors.As**: 文字列比較禁止。ドメイン境界の sentinel は `var ErrFeedGone = errors.New(...)` で定義
3. **errorlint が検出する形を書かない**: `err == ErrX` や `err.(*T)` は不可

## Context

4. **I/O する全関数の第一引数に `ctx context.Context`**。構造体フィールドに保持しない
5. **ライフサイクルは `signal.NotifyContext`**: main で `ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)`。エージェントループ・HTTP サーバー・DB プールすべてこの ctx 系譜で graceful shutdown
6. 外部呼び出しには必ずデッドライン: `context.WithTimeout`。フィード取得 30s、LLM は生成長依存なので長め(120s〜)+ キャンセル可能に

## 並行処理(エージェントループの心臓部)

7. **`sync.WaitGroup.Go`(1.25+)を使う**: `wg.Add(1)` + `go func(){ defer wg.Done() }()` の手書きは書かない
   ```go
   var wg sync.WaitGroup
   wg.Go(func() { fetchLoop(ctx) })
   wg.Go(func() { enrichLoop(ctx) })
   wg.Wait()
   ```
8. **本数を絞るなら `errgroup.SetLimit`**: 濃縮ワーカーは LLM の `-np` スロット数に合わせて制限
9. **スケジューラは Ticker + jitter**: 全フィードの同時発火を避ける。`next_fetch_at` は DB が真実、メモリ上のタイマーは再起動で消えてよい設計に
10. **グローバルレートリミッタ**: 外部フィード取得は `golang.org/x/time/rate` の単一 `*rate.Limiter`(最小間隔 5s、tenets §8-5)をプロセス全体で共有。per-host にしない
11. **チャネルの所有者だけが close する**。受信側 close・二重 close はレビューで即差し戻し

## Fail-soft(tenets §2-6)

12. **LLM 停止はコアパスを止めない**: 取得→保存(ステップ 1-2)は llm コンテナと独立に動き続ける。enrichment 失敗は `enrichment_status='failed'` に記録して continue。panic・os.Exit に波及させない
13. **冪等な再処理**: enrichment は status カラムを見て何度でも安全に再実行できる形に。イベントソーシングは使わない(素朴な upsert + status)
14. **LLM ヘルスは踏んでから知る**: 事前 ping ではなく、呼び出し失敗時に backoff(指数 + jitter、上限 5 分)して該当キューだけ寝かせる

## internal/llm(唯一の LLM 接点)

15. **OpenAI 互換 HTTP のみ**: llama.cpp server の `/v1/chat/completions`。SDK・フレームワークは入れない。`http.Client` は使い回す(コネクションプール)
16. **構造化出力はサーバー側で強制**: `response_format: {type: "json_schema", ...}` で llama.cpp の文法制約付き生成を使う。クライアント側は `encoding/json` でパースするだけ
17. **`</think>` 除去はここで一元化**: LFM2.5 系は reasoning-only(tenets §9)。呼び出し側に CoT を漏らさない:
    ```go
    if _, after, found := strings.Cut(raw, "</think>"); found { raw = after }
    ```
18. **モデル・パラメータはリクエストに明示**: LFM2.5 は公式推奨 temperature 0.2 / top_k 80 / repetition_penalty 1.05(tenets §9)。モデルごとの既定値を `llm` パッケージ内の定数表で管理

## slog

19. **`log` パッケージ禁止**。JSON ハンドラ + `slog.With("feed_id", id)` でキー付き。エラーは `slog.Any("err", err)` でなく `"err", err.Error()` か `slog.String`
20. ループ内の高頻度ログは Debug レベルに落とす。Info は「状態が変わった」ときだけ

## DB(PostgreSQL + pgvector)

21. **pgx v5 + pgxpool**。ORM は入れない。クエリが増えてきたら sqlc を検討(手書き SQL が真実)
22. **マイグレーションは Atlas、moka-core は関与しない**(docs/adr/ADR00001.md): スキーマ変更は `db/schema.sql` 編集 → `atlas migrate diff` → `atlas migrate lint`。適用は compose の one-shot `migrate` ジョブ。Go 側に `embed.FS` マイグレーションや起動時スキーマ検査を書かない
23. **埋め込みは `vector` 型 + HNSW インデックス**。ハイブリッド検索は FTS(`tsvector`)と pgvector の RRF 統合を SQL 側で

## テスト

24. **テーブル駆動 + `t.Run` + `t.Parallel()`**。アサーションは `testify/assert`(`require` は前提条件のみ)
25. **`t.Context()`(1.24+)を使う**: テスト内の ctx は `context.Background()` でなく `t.Context()`
26. **時間依存コードは `testing/synctest`(1.25+ 安定)**: スケジューラ・backoff・レートリミッタのテストは `synctest.Test` で仮想時計。実時間 sleep するテストは書かない:
    ```go
    synctest.Test(t, func(t *testing.T) {
        go scheduler.Run(t.Context())
        time.Sleep(5 * time.Second) // 仮想時間で即時に進む
        synctest.Wait()
        // assert...
    })
    ```
27. **LLM はモデルでなく配管をテストする**: `httptest.NewServer` で json_schema 応答・`<think>` 付き応答・500・タイムアウトを返すフェイクを立てる。モデルの品質評価は eval/(bp-python)の仕事で、Go のユニットテストに混ぜない
28. **goroutine リークを CI で捕まえる**: `GOEXPERIMENT=goroutineleakprofile`(1.26 実験)または uber-go/goleak を長期常駐部分のテストに

## 参照

- `docs/tenets/moka-tenets.md` — 本スキルの「tenets §N」参照はこの文書の節番号
