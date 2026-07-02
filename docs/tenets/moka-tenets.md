# moka-1 Tenets — 設計原理と決定事項

**日付**: 2026-07-02
**位置づけ**: moka-1 の設計原理(tenets)・アーキテクチャ・決定事項の公開版まとめ。ローカルの検討ドラフト(`draft_*.md`、リポジトリ管理外)を集約したもの。各所の「§N」参照はこの文書の節番号。

---

## 0. 一行定義

**moka-1**は、`docker compose up -d` 一発で起動して常駐する**エージェント型RSSフィードリーダー**。
[Alt](https://github.com/Kaikei-e/Alt)のミニマリズムAI強化版であり、[Plecto](https://github.com/Kaikei-e/Plecto)の試験運用・ドッグフーディングプロジェクトを兼ねる。

---

## 1. なぜ作るのか

### 1.1 Altからの反省 = ミニマリズムの根拠

Altは6層マイクロサービス・7つのPostgreSQLインスタンス・Go/Python/Rust/TypeScriptのポリグロット構成に育った。機能は揃ったが、以下のコストが顕在化した:

- **運用重量**: `altctl` による多スタック管理、コンパイル系サービスの明示的リビルド、サービス間のProtobuf契約維持
- **認知負荷**: イベントソーシング+CQRS+Redis Streamsは、個人利用のRSSリーダーには過剰な整合性機構
- **起動コスト**: 「読みたい」と思ってから読めるまでの距離が遠い

moka-1はこの逆を張る。**「1コマンド、少数コンテナ、単一DB、単一言語(+フロント)」**を制約として先に固定し、その中でAltのAI機能のエッセンス(要約・タグ・Q&A・リキャップ)を再構成する。

### 1.2 Plectoドッグフーディングの根拠

Plecto(Rust製・WASM Component Model拡張のL7ゲートウェイ)は実トラフィックを流す小規模で本気のワークロードを必要としている。moka-1は:

- 外向きエッジ(TLS終端、ルーティング、レート制限)をPlectoに全面委任する
- 認証・防御ロジックを`plecto:filter` WASMフィルタとして実装し、**フィルタ開発の実地検証台**になる
- 個人利用規模なので、Plectoの未成熟部分が露見しても被害が小さい

「AltのミニマリズムAI強化版」と「Plectoの実戦投入」は独立した目的ではない。**エッジの複雑さをPlectoに押し出すからこそ、アプリ側がミニマルでいられる**という設計上の相互依存がある。

---

## 2. 設計原則(Tenets)

1. **One-kick**: `git clone && docker compose up -d` で完結。マイグレーション・モデル取得・初期設定はすべて起動シーケンス内で自動化
2. **常駐エージェント**: cronの寄せ集めではなく、単一の常駐プロセスが「取得→濃縮→提示」のループを自律的に回す。ユーザーが見ていない間に仕事を終えている
3. **サービス数上限 = 5**: Plecto / app / DB / llama.cpp / frontend。これを超える機能は既存コンテナ内のモジュールとして実装する。上限が数えるのは**常駐サービス**であり、走って終了する one-shot ジョブ(マイグレーション等)は含めない([ADR00001](../adr/ADR00001.md))
4. **単一DB**: PostgreSQL 1インスタンス + pgvector。検索エンジン・キャッシュ・分析基盤は導入しない。全文検索はPostgreSQLのFTS + pgvectorハイブリッドで賄う
5. **ローカルLLMのみ**: クラウドAPIキー不要。開発機のiGPU(32GB割り当て)内で完結
6. **フェイルソフト**: LLMが死んでもRSSリーダーとしては動き続ける。AI機能は常に「あれば嬉しい増強」であり、コアパスの依存にしない(PlectoのFail-closedはエッジの話、アプリ内はFail-soft)

---

## 3. アーキテクチャ

```
                        ┌─────────────────────────────────────┐
 Internet ──TLS──▶ Plecto (エッジ)                            │
                  │  ・TLS終端 / HTTP/1·2·3                    │
                  │  ・WASMフィルタ: 認証, レート制限, 防御     │
                  └──────┬──────────────────┬─────────────────┘
                         │                  │
                  ┌──────▼──────┐    ┌──────▼──────┐
                  │  moka-web   │    │  moka-core  │ ◀── 常駐エージェント
                  │ (SvelteKit) │───▶│    (Go)     │      ループ本体
                  └─────────────┘    └──┬───────┬──┘
                                        │       │
                              ┌─────────▼──┐ ┌──▼───────────────┐
                              │ PostgreSQL │ │ llama.cpp server  │
                              │ + pgvector │ │ (Vulkan / RADV)   │
                              └────────────┘ └───────────────────┘
```

### 3.1 コンテナ構成(5個)

| コンテナ | 技術 | 役割 |
|---|---|---|
| `plecto` | Plecto (Rust + WASM filters) | エッジ: TLS、ルーティング、認証フィルタ、レート制限 |
| `moka-core` | Go 単一バイナリ | API + 常駐エージェント(取得/濃縮/インデックス/リキャップを全部内包) |
| `moka-web` | SvelteKit | 読むためのUI。ミニマル、SSR |
| `db` | PostgreSQL 17 + pgvector | 記事、フィード、埋め込み、ジョブ状態、すべてここ |
| `llm` | llama.cpp server (Vulkan) | ローカル推論。`-np` バッチで多ペルソナ並列 |

AltのAI系サービス群(pre-processor / search-indexer / tag-generator / news-creator / rag-orchestrator / recap-worker)は、**すべて `moka-core` 内のGoモジュール(interfaceで分離)に畳み込む**。プロセス境界ではなくパッケージ境界で分ける。

### 3.2 moka-core: エージェントループ

```
loop:
  1. due なフィードを取得(条件付きGET、外部リクエスト間隔 ≥ 5s を厳守)
  2. 新着記事を正規化 → DB書き込み(この時点で「読める」)
  3. 濃縮キューへ投入: 要約 / タグ抽出(LLM構造化出力) / 埋め込み生成
  4. アイドル時間に重い仕事: リキャップ生成、「今日のハイライト」選定(MoA)
  5. バックプレッシャ: llm が落ちていれば濃縮をスキップして 1-2 だけ回す
```

**ステップ2までがコアパス、3以降が増強**。イベントソーシングは持たず、`articles` への素朴なupsert + `enrichment_status` カラムで冪等に再処理する。

### 3.3 LLM編成

開発機(Ryzen AI 9 HX370 / 64GB RAM / iGPU 32GB割り当て)の gfx1150 は ROCm 非対応かつ Vulkan が優位のため、**バックエンドは Vulkan 固定**。

| 役割 | モデル(候補) | 常駐 | 用途 |
|---|---|---|---|
| 高速パス | LFM2.5-8B-A1B (Q4_K_M, ~5.2GB) | ○ | 要約・タグ抽出・翻訳。期待速度 60–75 t/s |
| 提案層 | Qwen3.5-4B (~2.6GB) / Gemma4 E4B (~3.0GB) / LFM2.5(兼務) | ○ | 「今日のハイライト」候補を3系統(真の系統差)で提案 |
| 集約層 | gpt-oss-20B (~11.3GB) | ○ | 提案の審査・統合、RAG Q&Aの最終回答 |
| (交代枠) | Qwen3.5-35B-A3B (~22.4GB) | スワップ | 品質最優先のバッチのみロード |

VRAM収支: 常駐合計 ~22GB + KVキャッシュ 3–4GB ≒ 26GB で 32GB 内。モデル選定の根拠と運用注意は §9。

### 3.4 AI機能の取捨(Alt→moka-1)

| Alt機能 | moka-1 | 判断 |
|---|---|---|
| 要約 | ✅ llama.cpp直 | コア価値。Ollamaは使わず llama.cpp server に一本化 |
| タグ生成(PyTorch NLP) | ✅ LLM構造化出力に置換 | サービス1個分を削減 |
| RAG Q&A(pgvector) | ✅ 同一DB内 | コア価値 |
| 週次リキャップ | ✅ moka-core内のバッチに縮退 | LLMに素直に任せる |
| Acolyte(LangGraph研究レポート) | ❌ 見送り | ミニマリズムに反する。v2以降で検討 |
| TTS / 3Dタグ可視化 | ❌ 見送り | |
| イベントソーシング/CQRS | ❌ 不採用 | 素朴なCRUD + statusカラム |
| Meilisearch | ❌ 不採用 | PostgreSQL FTS + pgvector |

**moka-1が足すもの**:
- **今日のハイライト**: 提案層3系統がそれぞれ「読むべき3本」を推薦 → 集約層が理由付きで統合(MoA構成のミニ適用)。エージェントが毎朝、勝手に用意しておく
- **問い返し**: 記事を開いた状態で「これの背景は?」と聞くと、購読履歴RAG + 当該記事コンテキストで回答

### 3.5 Plectoドッグフーディング計画

1. **Phase 1**: 素通しリバースプロキシ(TLS終端 + ルーティングのみ)。データパスの安定性検証
2. **Phase 2**: WASMフィルタ第1弾 — セッション認証(host-kv)、レート制限(host-ratelimit)。Altのauth-hub + Kratosスタック相当を**フィルタ2枚**に置換できるかの実験
3. **Phase 3**: `on-request-body` を使うフィルタ(API書き込みのペイロード検証)
4. **Phase 4**: フィルタのOCI配布 + cosign署名 + SIGHUPホットリロードの運用試験

各Phaseで踏んだ問題はPlecto側にissue/ADR起票する。**moka-1のリリースノートがそのままPlectoのフィールドレポートになる**のが理想。

---

## 4. データモデル(最小)

```sql
feeds(id, url, title, etag, last_modified, fetch_interval, next_fetch_at, ...)
articles(id, feed_id, guid, url, title, content, published_at,
         enrichment_status,      -- pending | summarized | embedded | failed
         summary, tags jsonb, embedding vector(...))
highlights(id, date, article_ids, rationale, model_meta jsonb)
qa_sessions(id, article_id nullable, question, answer, sources jsonb, created_at)
```

4テーブル+α。スキーマ管理は **Atlas**([ADR00001](../adr/ADR00001.md)): `db/schema.sql` を単一ソースに `atlas migrate diff` で versioned migrations を生成し、compose 内の one-shot `migrate` ジョブが起動時に自動適用する(One-kick 維持)。moka-core は自前マイグレーション機構を持たない。

---

## 5. リポジトリ構成

```
moka/
├── compose.yaml            # これ1枚でワンキック
├── plecto/
│   ├── manifest.toml       # ルート・upstream・フィルタチェーン定義
│   └── filters/            # WASMフィルタ(Rust) — ドッグフーディングの主戦場
├── core/                   # Go: API + エージェントループ
│   ├── cmd/moka/
│   └── internal/{feed,enrich,rag,highlight,llm,store,httpapi}/
├── db/                     # Atlas: atlas.hcl / schema.sql / migrations/(ADR00001)
├── web/                    # SvelteKit
├── eval/                   # Python (uv + Pyrefly): モデルA/B・プロンプト評価(非コンテナ、§7)
├── models/                 # 起動時に自動DLされるGGUF置き場(.gitignore)
└── docs/
    ├── tenets/             # 本書
    └── adr/                # ADR00001.md からの連番 + template.md
```

---

## 6. マイルストーン

| M | 内容 | Done条件 |
|---|---|---|
| **M0** | ワンキック骨格 | compose up → フィード登録 → 記事が読める(AI無し)。Plectoは素通し |
| **M1** | 高速パスAI | 要約+タグが新着に自動付与。llama.cpp Vulkanスモークテスト完了 |
| **M2** | RAG | 埋め込み+ハイブリッド検索+問い返しQ&A |
| **M3** | エージェント化 | 今日のハイライト(MoA)+週次リキャップが無人で出る |
| **M4** | Plectoフィルタ | 認証+レート制限をWASMフィルタで。Phase 2完了 |
| **M5** | 運用硬化 | モデル自動DL、ヘルスチェック、フェイルソフト検証、フィルタ署名配布 |

---

## 7. 決定事項: 実装言語はGo(本体) + Python(評価のみ)

**日付**: 2026-07-02 / **ステータス**: Accepted(正式ADR化は docs/adr/ で行う)

**文脈**: 「GoはLLM資産に弱いのでPythonの方が良いのでは」という問い。uv + Pyrefly(2026年5月に1.0安定版、Instagram ~20M LOCで本番運用)の導入を前提にフェアに比較調査した。

**確認した事実**:
- uv + Pyreflyにより、Pythonの古典的弱点(依存管理・型検査の速度と精度)は実質解消している
- 一方でGoに対する「LLM資産」優位のうち、moka-1に効くものはほぼ残らない:
  - 構造化出力 → llama.cpp serverの`json_schema`(文法制約付き生成)でサーバー側解決
  - LangGraph等のフレームワーク → 設計で意図的に不採用(§3.4)
  - 本文抽出 → ScrapingHubベンチで go-trafilatura F1=0.960 vs Python版 0.958 のほぼ同点、Go版の方が高速
- Goが依然優位: 常駐デーモンの足腰(goroutine)、静的バイナリ ~20MB、実行時強制の型
- Pythonが依然優位: プロンプト・評価実験の回転速度、新手法への初日アクセス

**決定**:
1. **moka-core本体はGo**。moka-1のLLM層の実体は「HTTPクライアント + プロンプトテンプレート + `</think>`除去 + JSONパース」の数百行であり、Python固有資産を使わない
2. **モデルA/B・プロンプト評価は`eval/`のPython(uv + Pyrefly)**。非コンテナ(ホスト直接実行)、コンテナ数上限5に影響しない
3. **再検討トリガー**: v2でマルチステップ研究エージェントを復活させる場合はLangGraphサイドカー(Python)を「追加」で検討(置換ではない)

---

## 8. 未決事項

1. **モデル最終選定**: LFM2.5のVulkan/gfx1150実測が未実施(§9の要検証項目)。A/B結果次第で高速パスをQwen3.5-4Bに差し替え
2. **集約層の択一**: gpt-oss-20B常駐 vs Qwen3.5-35B-A3Bオンデマンドスワップの実レイテンシ比較
3. **認証方式**: 単一ユーザー前提でどこまで簡素化するか(パスキー1本 + Plectoフィルタでセッション検証、が現時点の仮説)
4. **モデル管理UX**: llama.cpp直行方針の下で、pull相当の体験を`compose`内でどう再現するか
5. **フィード取得マナー**: 「外部リクエスト間隔≥5s」のグローバルレートリミッタの具体設計

---

## 9. モデル編成の根拠(LFM2.5-8B-A1B 調査要点)

高速パス候補 LFM2.5-8B-A1B(Liquid AI、2026年5月公開のエッジ向けMoE)の調査結論:

**なぜ「手足」に向くか**:
- MoEの特性上「メモリは総8.3B・速度はアクティブ1.5B」。RTX 5060 Ti実測でDense 12B(Q8)の約10倍の生成速度
- ベンチの強みが指示追従(IFEval 91.84)・非ハルシネーション(63.47、前世代7.46)・エージェント系に偏る。「自律的に難問を解く頭脳」ではなく「定型作業を高速・正確にこなす手足」— 最終集約役には向かない(そこは20B級の領分)
- 18/24層が畳み込みでKVキャッシュが小さく、プレフィルが速い。「記事全文を食わせる」ワークロード形状と好相性

**運用上の注意**:
1. llama.cpp は **b6709 以上**必須(`lfm2moe` 対応)。未満は `missing tensor` でロード失敗
2. **reasoning-only 設計**: 毎回 `<think>` CoT を前置する。`</think>` 以降を取り出す後処理が必須(moka-core の internal/llm で一元化)。CoT は英語で出ることが多い
3. llama.cpp のバージョン差で同一モデル・同一GPUでも4倍以上の速度差の事例あり。**バージョン pin + 意図的更新**(eval/ の実測とセット)
4. 公式推奨サンプリング: temperature 0.2 / top_k 80 / repetition_penalty 1.05
5. Q4_K_M で約5.2GB

**要検証(eval/ の最初の仕事)**:
1. Vulkan(RADV gfx1150)スモークテスト
2. 速度実測: tg(60〜75 t/s 仮説)/ pp / `-np 3` バッチ時の劣化率
3. 日本語記事要約での平均CoT長と実効レイテンシの Qwen3.5-4B 比較
4. 同一記事セットでの要約・提案品質 A/B

**注記**: 公開ベンチ数値の多くはベンダー自社測定。採用判断は必ず自前データ(日本語記事の要約・QA)でのA/Bを経る。

---

## 参考

- [Alt](https://github.com/Kaikei-e/Alt) — 本家
- [Plecto](https://github.com/Kaikei-e/Plecto) — WASM Component Model拡張のL7ゲートウェイ
- [llama.cpp Vulkan性能スレッド](https://github.com/ggml-org/llama.cpp/discussions/10879) / [HX370 UMA実測issue](https://github.com/ggml-org/llama.cpp/issues/19396)
- [gfx1150でROCmがVulkanより遅い件](https://github.com/lemonade-sdk/llamacpp-rocm/issues/57)
- 類例: [Precis](https://github.com/leozqin/precis) / [RLLM](https://github.com/DanielZhangyc/RLLM) — 「常駐エージェント+MoA+エッジゲートウェイ」構成は持たない(moka-1の差別化点)
