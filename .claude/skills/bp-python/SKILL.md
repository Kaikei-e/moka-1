---
name: bp-python
description: Python ベストプラクティス(eval/ 専用、Python 3.14 + uv + Pyrefly + Ruff)。モデル A/B・プロンプト評価スクリプトの規約と再現性の作法。
  TRIGGER when: .py ファイルを編集・作成する時、eval/ 以下で評価スクリプトを書く時、モデル比較・プロンプト評価をする時。
  DO NOT TRIGGER when: テストの実行のみ、pyproject.toml の確認のみ、ファイルの読み取りのみ、他言語の作業時。
---

# Python Best Practices (eval/ のみ)

moka-1 の Python は **eval/ の評価スクリプトに限定**(ADR-0001)。本体機能(フィード取得・API・エージェントループ)を Python で書き始めたら設計違反 — その場で指摘して止まること。eval/ の役割は tenets §9 の類のタスク: モデル A/B、プロンプト比較、速度実測、品質採点。

## ツールチェーン

1. **uv で完結**: `uv add` / `uv run` / `uv lock`。pip・手動 venv・conda 禁止。`uv.lock` はコミットする
2. **単発スクリプトは PEP 723 インライン依存**が便利:
   ```python
   # /// script
   # requires-python = ">=3.14"
   # dependencies = ["httpx", "pydantic>=2"]
   # ///
   ```
   `uv run script.py` だけで動く。共有基盤(採点・レポート生成)が育ったら通常の `pyproject.toml` パッケージに昇格
3. **Pyrefly 必須**: `uv run pyrefly check .` 通過必須。mypy / pyright は使わない(v1.0 安定版・Ruff 系より高速、ADR 済みの選定)
4. **Ruff が一次ソース**: `uv run ruff format .` + `uv run ruff check .`。ルールは `E,W,F,B,UP,SIM,N,I,ANN,S,PTH,C4,ASYNC,TRY,RUF` を select。手動スタイル議論禁止

## コード規約

5. **型ヒント必須**: 使い捨てスクリプトでも関数は完全アノテーション。`Any` は境界最小限。Python 3.14 なので `list[str]` / `X | None` / PEP 695 `type` エイリアスを使う(`typing.List` / `Optional` は書かない)
6. **例外は具体的に**: 裸の `except:` / `except Exception:` 禁止。`raise EvalError("scoring failed") from err` で原因チェーン保持
7. **Pydantic v2 で LLM 出力を受ける**: llama.cpp の `json_schema` 出力は `Model.model_validate_json()` でパース。生 dict を引き回さない。**スキーマは Pydantic モデルから `model_json_schema()` で生成してリクエストに渡す**(スキーマと検証の一元化)
8. **HTTP は httpx**: llama.cpp server(localhost)へは `httpx.Client` を使い回す。タイムアウトは生成長に合わせ明示(`timeout=httpx.Timeout(300, connect=5)`)。並列プローブは `asyncio` + `httpx.AsyncClient`、ただし `-np` スロット数を超える同時リクエストを投げない
9. **資源管理は context manager**: `with` / `async with`。裸 `open()` 禁止、パスは `pathlib.Path`

## 評価の作法(このスキルの本体)

10. **`</think>` 除去を忘れない**: LFM2.5 系は reasoning-only(tenets §9)。除去前のテキストで品質採点や長さ測定をすると測定が壊れる。CoT 長・CoT 込みレイテンシは**別の測定項目**として記録する
11. **結果は自己記述的な JSONL**: 1 行 = 1 試行。必ず含める: モデル名・量子化・llama.cpp バージョン・サンプリングパラメータ(temperature/top_k/repetition_penalty)・プロンプトの SHA-256・入力 ID・seed・レイテンシ(prefill/decode 分離)・出力。「後から何の実験だったか分かる」ことが正義
12. **seed 固定 + 複数試行**: 単発比較で結論しない。同一条件 n≥3、モデル間比較は同一入力セットで対にする(paired comparison)
13. **速度測定は分離して報告**: pp(プレフィル)と tg(デコード)を分ける(tenets §9 の形式に合わせる)。ウォームアップ 1 回を捨てる
14. **品質採点に LLM を使う場合**: 採点者モデルは被評価モデルと別系統にする(自己採点バイアス)。採点プロンプトも結果 JSONL にハッシュで記録
15. **結論は draft / ADR に還流**: eval/ の結果で意思決定したら、moka-adr-writer で ADR 化する。スクリプトと結果 JSONL のパスを ADR から参照

## 実行環境

16. **ホスト上で直接実行**: eval/ はコンテナ化しない(コンテナ数上限 5 に含めない)。llama.cpp server へは `http://localhost:<port>`
17. **compose の llm を占有しない配慮**: 長時間ベンチ中は moka-core の濃縮が遅れる(同一 iGPU)。実測系は明示的に「ベンチ中」であることをユーザーに伝えてから回す

## 参照

- `docs/tenets/moka-tenets.md` — 本スキルの「tenets §N」参照はこの文書の節番号
