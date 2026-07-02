---
name: docker-compose
description: moka-1 スタックの Docker Compose 操作と compose.yaml の書き方。ワンキック起動・開発ループ・ヘルスチェック・開発機(iGPU/Vulkan)固有設定・トラブルシュート。
  TRIGGER when: compose.yaml を編集する時、スタックの起動/停止/再起動/ログ確認をする時、コンテナの不調を調査する時、llm サービスの設定を触る時。
  DO NOT TRIGGER when: コンテナと無関係なコード編集時。
---

# Docker Compose Operations (moka-1)

## 大原則: ワンキック

`docker compose up -d` **一発で全部が上がる**のが moka-1 の存在理由(tenets §2-1)。
マイグレーション・モデル DL・初期設定を「手動手順」として増やす変更は設計違反。起動シーケンス内に自動化して入れる。
ワンキックの検証は `docker compose up -d --wait`(全 healthcheck が通るまで待って exit code で返る)で行う。

## 基本操作

```bash
docker compose up -d --wait          # ワンキック起動 + healthy まで待機
docker compose ps                    # 状態確認
docker compose logs <service> -f     # ログ追尾
docker compose down                  # 停止(named volume は保持)
docker compose down -v               # データも消す(要確認: 記事 DB が飛ぶ)
docker compose up -d --build moka-core   # コード変更後の再ビルド
docker compose watch                 # 開発ループ(develop.watch 設定時)
```

## compose.yaml の書き方(2026 idiom)

- **`version:` キーは書かない**(廃止済み)。トップレベルに `name: moka`
- **依存は healthcheck 連動で**:
  ```yaml
  depends_on:
    db:
      condition: service_healthy
      restart: true        # db が再作成されたら自分も再起動
  ```
  ただし **moka-core → llm は depends_on に入れない**(fail-soft: llm 無しでも起動する。tenets §2-6)
- **healthcheck は start_period + start_interval**: 起動直後だけ短間隔で叩く(Docker 25+):
  ```yaml
  healthcheck:
    test: ["CMD", "curl", "-sf", "http://localhost:8080/healthz"]
    interval: 30s
    start_period: 60s
    start_interval: 2s
  ```
  llm はモデルロードが遅いので `start_period` を長めに(数分)
- **restart: unless-stopped** を全常駐サービスに(one-shot の `migrate` は除く — `restart: "no"`)
- **開発ループは develop.watch**: moka-core / moka-web に `develop.watch`(`sync` + `rebuild`)を書き、`docker compose watch` で回す
- 秘密値はファイルベース **Docker secrets**(`secrets/` — gitignore 済み、初回手順は secrets/README.md)。compose.yaml・.env への直書きをしない。コンテナ側は `*_FILE` 系(例 `POSTGRES_PASSWORD_FILE=/run/secrets/postgres_password`)で受ける

## サービス構成(常駐上限 5 — 増やす前に tenets §2-3 を読む)

| サービス | 役割 | 依存 |
|---|---|---|
| `plecto` | エッジ: TLS 終端、ルーティング、WASM フィルタ | moka-core, moka-web (upstream) |
| `moka-core` | API + 常駐エージェントループ (Go) | migrate(completed)、db(healthy)、llm(**soft** — depends_on 禁止) |
| `moka-web` | UI (SvelteKit SSR) | moka-core |
| `db` | PostgreSQL 17 + pgvector | — |
| `llm` | llama.cpp server (Vulkan / RADV) | — |
| `migrate` | **one-shot**: Atlas マイグレーション適用(ADR00001) | db(healthy)。実行後 exit 0 — 常駐上限 5 には数えない |

## migrate ジョブ(Atlas、ADR00001)

```yaml
migrate:
  image: arigaio/atlas:<PINNED>          # latest 禁止
  command: migrate apply --url "$DATABASE_URL" --dir file:///migrations
  volumes:
    - ./db/migrations:/migrations:ro
  depends_on:
    db:
      condition: service_healthy
  restart: "no"                          # one-shot。unless-stopped にしない
```

- moka-core 側: `depends_on: migrate: condition: service_completed_successfully`
- スキーマ変更の開発フロー: `db/schema.sql` 編集 → `atlas migrate diff` → `atlas migrate lint` → commit(`atlas.sum` 更新は `atlas migrate hash`)
- **migrations/ の SQL を手で直接編集しない**(適用済み履歴の改変)。誤りは新しいマイグレーションで前進修正

## llm サービス(開発機ハードウェア固有)

```yaml
llm:
  image: ghcr.io/ggml-org/llama.cpp:server-vulkan-<PINNED>   # b6709 以上に pin。latest 禁止
  devices:
    - /dev/dri:/dev/dri          # iGPU (gfx1150) パススルー
  group_add:
    - video
    - render
  volumes:
    - ./models:/models
```

- **Vulkan 固定**: gfx1150 は ROCm 非対応 + Vulkan の方が速い(tenets 参考)。CUDA/ROCm イメージに切り替えない
- **バージョン pin は性能問題**: llama.cpp のバージョン差で同一モデル・同一 GPU でも 4 倍以上の速度差の事例(tenets §9)。更新は eval/ の速度実測とセットで意図的に行い、ADR に残す
- **モデル自動 DL**: `llama-server -hf <org>/<repo>:<quant>` で初回起動時に HF から取得可能。entrypoint スクリプトで賄えない要件が出るまでこれを使う
- **`-np` 並列スロット**: MoA 提案層の 3 ペルソナ同時デコード用。KV キャッシュ消費が乗るので VRAM 収支(tenets §3.3、~26GB/32GB)を崩さない範囲で
- 複数モデル常駐は llama.cpp の複数プロセス化ではなく、まず 1 サーバー + モデル切替(`--model` 別ポート追加は VRAM 収支を再計算してから)

## ヘルスチェック(手動)

```bash
docker compose ps --format 'table {{.Name}}\t{{.Status}}'
curl -sf http://localhost:8080/healthz         # moka-core(直)
curl -sf http://localhost:8081/health          # llama.cpp server
curl -sfk https://localhost/                   # Plecto 経由(エッジ確認)
docker stats --no-stream                       # メモリ / CPU 収支
```

## トラブルシュート

| 症状 | 確認 | 対処 |
|---|---|---|
| llm が unhealthy | `logs llm` で Vulkan デバイス検出(`ggml_vulkan: Found 1 Vulkan devices`)を確認 | `/dev/dri` マウント・group_add・BIOS の iGPU 32GB 割り当てを確認。**moka-core は fail-soft で動き続けるのが正常** — 濃縮だけ止まる |
| llm のロードで `missing tensor` | イメージのビルド番号 | b6709 未満は `lfm2moe` 非対応(tenets §9)。pin を上げる |
| 生成が異常に遅い | `logs llm` で GPU オフロード層数、`docker stats` で RAM スワップ | `-ngl` 指定漏れ / VRAM 溢れで CPU フォールバックしていないか。モデル常駐合計を tenets §3.3 の収支表と突き合わせ |
| moka-core が起動しない | `logs migrate` → `logs moka-core` の順 | migrate が exit 0 か(`docker compose ps -a`)、db の `service_healthy` 条件、`.env` の DSN を確認 |
| migrate が失敗する | `logs migrate` のエラー、`atlas.sum` 不整合 | checksum エラーなら `atlas migrate hash` の掛け忘れ。SQL エラーなら**前進修正**(新マイグレーション追加)。適用済みファイルを書き換えない |
| フィード取得が遅い | 仕様かも | グローバルレートリミッタ(外部リクエスト ≥5s)。フィード数 × 5s が周回時間の下限 |
| Plecto がリクエストを落とす | `logs plecto` で short-circuit 理由 | フィルタの fail-closed 動作なら設計通り。Plecto 側の問題なら**回避せず issue 起票**(ドッグフーディング) |
| ポート競合 | `ss -tlnp` | 旧 Ollama(停止済みのはず)等の残骸を確認 |
| ディスク肥大 | `docker system df` | `models/` の古い GGUF、`docker image prune`。named volume(db)は消さない |

## 参照

- `docs/tenets/moka-tenets.md` — 本スキルの「tenets §N」参照はこの文書の節番号
