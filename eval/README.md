# moka-eval — LLMモデル選定ハーネス

moka-1 のモデル選定(品質A/B・速度・VRAM・Embedding retrieval)を行う評価ハーネス。
ホスト直接実行(コンテナ化しない)。llama.cpp server は compose.yaml の `llm` サービス定義を
`docker compose run` で借用してアドホック起動する(イメージpin・iGPU設定の単一の真実を維持)。

- 方法論の根拠: `results/research/*.json`(Phase R 調査、出典URL付き)
- 結論の還流先: `docs/adr/`(moka-adr-writer)

## セットアップ

```bash
cd eval && uv sync --group dev
```

## ランブック(計測は GPU シリアル)

計測前: `docker compose stop llm`(同一iGPUの取り合いを避ける)。終了後 `start llm`。

```bash
# 0. スモーク(機構確認: 起動→1リクエスト→破棄)
uv run moka-eval smoke

# 1. データセット(公開RSS、≥5s/req、本文は gitignore・manifest のみコミット)
uv run --group dataset moka-eval dataset fetch
uv run moka-eval dataset verify
uv run moka-eval dataset recap          # リキャップ用の合成3週分を凍結

# 2. VRAM実プローブ(常駐4+スワップ1の同時ロード、D2判定材料)
uv run moka-eval probe --swap qwen36-35b

# 3. 速度再検証(ADR00006表とのクロスチェック、±10%以内を期待)
uv run moka-eval bench --model lfm25            # 各モデルで繰り返す
uv run moka-eval bench --model qwen35-4b --np 3 # 提案層の並列劣化率

# 4. ロード時間(D2: スワップ運用の実コスト。ホストRAM16GBではキャッシュが効かない想定)
uv run moka-eval loadtime --model qwen36-35b -n 3

# 5. 生成マトリクス
uv run moka-eval generate --model lfm25     --task summarize --run-id d1-lfm25-summ
uv run moka-eval generate --model qwen35-4b --task summarize --run-id d1-qwen4b-summ
uv run moka-eval generate --model lfm25     --task tags      --run-id d5-lfm25-tags
uv run moka-eval generate --model qwen35-4b --task tags      --run-id d5-qwen4b-tags
uv run moka-eval generate --model gptoss20b  --task recap --run-id d2-gptoss-recap
uv run moka-eval generate --model qwen35-35b --task recap --run-id d3-qwen35-recap
uv run moka-eval generate --model qwen36-35b --task recap --run-id d3-qwen36-recap
uv run moka-eval generate --model gemma4-26b --task recap --run-id d3-gemma26-recap

# 6. ペア化(ブラインド+位置無作為化)→ ローカル審判 → 集計
uv run moka-eval pairs --run-a d1-lfm25-summ --run-b d1-qwen4b-summ --task summarize --name d1-summarize
uv run moka-eval judge --name d1-summarize --judge gptoss20b
uv run moka-eval score --name d1-summarize --judges gptoss20b,claude

# 7. Embedding retrieval(コーパス=収集記事、クエリ=data/retrieval/queries.json)。
# "generic" スタイルのクエリ(トピック語なし一般要約質問)は gold 一致で採点できない
# ため recall@k/MRR からは自動的に除外される(embed.load_recall_queries)
uv run moka-eval embed --model qwen3-emb-0.6b --dims 1024,768,512
uv run moka-eval embed --model qwen3-emb-4b   --dims 2560,1024
uv run moka-eval embed --model bge-m3

# 8. Q&A 文脈選定クエリ構成(タイトル連結、RAG精度改善)の評価。"generic" クエリごとに
# 質問文単独 vs タイトル連結クエリでのコサイン近傍 top-k を比較材料化し(DBを持たない
# eval/ 環境ではハイブリッド検索のベクトル側だけの近似 — context_relevance.py 参照)、
# 6と同じ pairwise MoA 審判(ブラインド化+位置スワップ)にそのまま乗せる
uv run moka-eval context-relevance --model qwen3-emb-0.6b --name qa-context-relevance
uv run moka-eval judge --name qa-context-relevance --judge gptoss20b --lenses topical_relevance
uv run moka-eval score --name qa-context-relevance --judges gptoss20b,claude
```

## 審判プロトコル(Phase R 調査準拠)

- 主審 = 候補と別族の最強単一審判(gpt-oss-20b)。弱い異種審判の多数決は組まない
- 追加審判 = Claude(pairs ファイルを読んで同一形式の verdicts.jsonl を书く)。
  ローカル審判との一致率がローカル審判自体のメタ評価になる
- 位置スワップ両順一致のみ勝ち。判定は自由記述CoT→ `[[A]]/[[B]]/[[tie]]` タグ抽出
- gpt-oss 自身の出力を含む比較(recap)は同族バイアスがあるため Claude 判定を主とし、
  gpt-oss 判定は参考値として記録
- 集計: tie 除外の両側符号検定 + 位置一貫性率 + 審判間一致率(`score` が全部出す)

このプロトコルは要約A/B比較専用ではなく、`context-relevance`(検索候補記事一覧A/Bの
比較、lens="topical_relevance")もそのまま乗せている — `judge`/`score` コマンド自体は
比較対象の中身(要約テキストか候補記事一覧テキストか)を区別しない。

## 結果の置き場

- `results/<run_id>/generations.jsonl` — 1行1試行、自己記述的(bp-python §11)
- `results/pairs/` `results/judgments/` `results/summary/` — 判定と集計
- `results/research/` — Phase R 調査エビデンス
- `results/server-logs/` — llama-server ログ

`results/` 配下は全てローカル生成物であり非コミット(gitignore)。実測結果を後から参照したい
場合は ADR 本文に数値を書き残す(例: ADR00023)か、ローカルに保持すること。

## CI パリティ

```bash
uv run ruff format --check . && uv run ruff check . && uv run pyrefly check . && uv run pytest -q
```
