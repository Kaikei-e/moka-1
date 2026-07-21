# moka-1

**moka** = **M**ixture **o**f **K**nowledge **A**gents。
`docker compose up -d` 一発で起動して常駐する、エージェント型 RSS フィードリーダー。
[Plecto](https://github.com/Kaikei-e/Plecto) のドッグフーディングプロジェクトを兼ねる。
設計は [docs/tenets/moka-tenets.md](docs/tenets/moka-tenets.md)、決定記録は [docs/adr/](docs/adr/)。

[English](README.md) · 日本語

## 起動

```bash
git clone https://github.com/Kaikei-e/moka-1 && cd moka-1
openssl rand -base64 24 | tr -d '/+=' > secrets/postgres_password.txt  # 初回のみ(secrets/README.md)
docker compose up -d --wait
```

- UI: https://localhost/ (開発用自己署名証明書なので警告が出る)
- エッジ admin: http://localhost:9099/metrics (ホストローカルのみ)

初回は LLM モデル(~5.2GB)の自動ダウンロードが走るため `--wait` が数分かかる。
LLM が落ちていても RSS リーダーとしては動く(fail-soft)。

## 構成(常駐 5 サービス)

| サービス | 技術 | 役割 |
|---|---|---|
| [plecto](plecto/) | Plecto (Rust + WASM filters) | エッジ: TLS 終端、HTTP/1·2·3、ルーティング |
| moka-core ([core/](core/)) | Go 単一バイナリ | API + 常駐エージェント(取得/濃縮/リキャップ) |
| moka-web ([web/](web/)) | SvelteKit SSR | 読むための UI |
| db ([db/](db/)) | PostgreSQL 18 + pgvector | 記事・フィード・埋め込み・ジョブ状態のすべて |
| llm | llama.cpp server (Vulkan) | ローカル推論 |

マイグレーションは one-shot の `migrate` ジョブ(Atlas)が起動時に自動適用する。
