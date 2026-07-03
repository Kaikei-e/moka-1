# Context Map

moka-1 リポジトリのコンテキスト境界。用語の正準ソースは `docs/tenets/moka-tenets.md` と `docs/adr/`。

## Contexts

- [アプリ](./CONTEXT.md) — 常駐エージェント、コアパス/増強、濃縮、取り寄せ、LLM編成、Web UI(moka-core / moka-web / db 共通のドメイン語)
- [エッジ](./plecto/CONTEXT.md) — WASMフィルタ、フィルタチェーン、素通し(Plecto のドメイン語に従属。filter / fast path / decision は Plecto 側の語)
- [評価](./eval/CONTEXT.md) — A/B、審判、スモーク(モデル選定・プロンプト評価の用語)

## Relationships

- **エッジ → アプリ**: Plecto はすべての外部トラフィックを終端し、フィルタチェーンを通してから moka-web / moka-core に渡す。本番では moka-core はホスト非公開(Plecto 経由のみ)
- **評価 → アプリ**: eval/ の A/B・実測結果が LLM 編成(高速パス・提案層・集約層・交代枠)のモデル選定を駆動する(結果は ADR に記録)
- **障害方針の対**: エッジは Fail-closed、アプリ内は フェイルソフト(tenets §2-6)。同じ「失敗時」の語でも所属コンテキストで意味が逆になるため、必ず所属を明示する

## 衝突語の取り扱い

複数コンテキストで意味が異なる語は、アプリ側の文書では限定修飾する:

| 語 | エッジ(Plecto) | アプリ(moka-1) |
|---|---|---|
| fast path / 高速パス | Plecto ネイティブ Rust 側の高速経路 | LLM 編成の軽量モデルの役割名(「高速パス(LLM)」と書く) |
| filter / フィルタ | WASM フィルタ(plecto:filter) | アプリ側では使わない(絞り込みは「検索」「抽出」と言い換える) |
