---
name: moka-adr-writer
description: moka-1 プロジェクトの Architecture Decision Record を docs/adr/ に日本語で書く。実装完了後やユーザーが「ADR書いて」「ADRにまとめて」「決定を記録して」と言った時、または設計上の決定(モデル選定・ライブラリ採否・構成変更)を下した直後に使用する。テンプレート(docs/adr/template.md)参照方式・ADR00001.md からの連番。デプロイ工程は含まない。
allowed-tools: Bash, Read, Glob, Grep, Edit, Write
---

# moka-1 ADR Writer

2 フェーズ: **実装確認**(§1)→ **ADR 執筆**(§2)。Alt の ADR 運用に忠実だが、デプロイ工程(Pact/c2quay)は moka-1 に存在しないため無い。
設計判断のみの ADR(コード変更なし)は §1 をスキップする。

## §1. 実装確認

ADR は「動いた状態」を固定する行為なので、コード変更を伴う場合は最低限のテストを先に回す:

| 変更の種類 | コマンド |
|---|---|
| moka-core (Go) | `cd core && go test ./...` |
| Plecto フィルタ (Rust) | `cd plecto/filters && cargo test` |
| moka-web (Svelte/TS) | `cd web && bun run check && bun test` |
| eval/ (Python) | `cd eval && uv run pyrefly check . && uv run pytest` |
| ドキュメントのみ | skip |

テストが落ちていたら ADR は書かず、原因を報告して止まる。ADR は「動いた実装の決定記録」であり、憶測を書く場所ではない。

## §2. ADR 執筆

### 2.1 番号とテンプレート

```bash
ls docs/adr/ | grep -E '^ADR[0-9]{5}\.md$' | sort | tail -1   # 最新番号を確認
```

- ファイル名は **`ADR` + 5 桁ゼロ埋め**: `ADR00001.md`, `ADR00002.md`, ...(最新 +1。1 本も無ければ `ADR00001.md` から)
- **`docs/adr/template.md` を Read で開き、そのセクション見出しをそのまま使う**(勝手に増減しない)
- draft_XXXX.md 内のミニ ADR は番号体系の**外**(ドラフトはドラフト)。正式化する価値がある決定は、この方式で新番号の ADR として転記する

### 2.2 Frontmatter

| フィールド | 値の決め方 |
|---|---|
| `title` | 動詞始まりの行動指向の一文。ADR 番号は含めない |
| `date` | `YYYY-MM-DD`(当日) |
| `status` | 原則 `accepted`。過去 ADR を無効化する場合のみ新 ADR 側は `accepted` のまま、旧 ADR を `superseded by [[ADRNNNNN]]` に更新 |
| `tags` | §2.4 の許可タグから最大 5 個 |
| `affected_modules` | モジュール名と変更概要を 1 行/件(`core/internal/llm`, `plecto/filters/auth`, `web`, `eval`, `compose` など) |
| `aliases` | `ADR-N` と `ADR-0000N` の 2 形式を必ず両方(Obsidian リンク解決用) |

### 2.3 本文ルール

- **日本語で書く**。サービス名 / コマンド / ライブラリ名 / ファイルパスは英語のまま
- **セクション順は template.md を尊重**: Context / Decision / Consequences (Pros, Cons/Tradeoffs) / Revisit Triggers / Related ADRs
- **Context**: なぜこの決定が必要だったかを定量 / 定性の根拠とともに。実測(eval/ の JSONL、ベンチ数値)があれば数値を残し、スクリプトと結果ファイルのパスを参照する
- **Decision**: 採用した選択肢に加え、**検討した代替案と却下理由**を必ず書く
- **Consequences**: Pros と Cons/Tradeoffs を分けて列挙。未解決の負債は Cons に
- **Revisit Triggers**(moka-1 独自の必須セクション): この決定を覆す・再評価する具体的条件。「今は不採用」判断が多いミニマリズム運用の要
- コードブロックは判断の根拠に必要な最小限。ロジックの羅列は diff で読めるので省く
- **Related ADRs は wikilink `[[ADRNNNNN]] タイトル` 形式**(Obsidian のグラフビュー / バックリンク用)。draft への参照は相対リンクで可

### 2.4 許可タグ

```
architecture, minimalism, agent, rss, feed, llm, model-selection, prompt,
rag, embedding, highlight, recap, database, migration, frontend, backend,
api, docker, compose, plecto, wasm, filter, edge, security, authentication,
rate-limiting, performance, testing, ci, refactoring, bugfix, logging,
eval, dogfooding, aix-hardware
```

この外のタグを増やしたくなったら、ADR を書く前にこのスキル(§2.4)を先に更新する。

### 2.5 情報衛生(公開リポジトリ)

含めない:
- 本番 IP / 公開ドメイン / 秘匿ポート
- 資格情報・API キー・シークレット類
- 個人名(公開コントリビューターとして記録されているものを除く)
- **開発機のマシン名(機微情報)**。「開発機」「ホスト」と書く

OK: `localhost:XXXX`、compose サービス名、ハードウェアスペック(HX370 / 64GB / iGPU 32GB — 性能判断の根拠として必要)。

### 2.6 書き込みと読み返し

Write ツールで `docs/adr/ADRNNNNN.md` を作る。heredoc や `cat > ...` は使わない。
書き込み後に Read で自分の出力を読み返し、見出し / frontmatter / wikilink 形式 / aliases の 2 形式を確認する。
旧 ADR を supersede した場合は旧ファイルの `status` 更新も忘れない。

## §3. 完了報告

- 書いた ADR のパス(`docs/adr/ADRNNNNN.md`)とタイトル
- green だったテスト(§1 を実行した場合)
- supersede した旧 ADR(あれば)
- commit はユーザーの指示があった場合のみ

## 参照

- `docs/adr/template.md` — セクションと frontmatter のソース(真実はこちら)
- `docs/tenets/moka-tenets.md` §7 — 決定事項の公開まとめ(ローカルドラフト内のミニ ADR は番号体系の外)
