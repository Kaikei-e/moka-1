# plecto/ — moka-1 のエッジ

[Plecto](https://github.com/Kaikei-e/Plecto) のドッグフーディング(tenets §1.2 / §3.5)。
外向きの複雑さ(TLS、HTTP/1·2·3、ルーティング、認証、レート制限)をすべてここに押し出す。

## 現在地: Phase 2(WASM フィルタ第1弾)

TLS 終端 + path ルーティングに WASM フィルタ 2 枚が載った:

- **session-auth**: moka-core が発行する HMAC 署名 cookie の検証のみ(ADR00021)。
  失敗時は text/html な GET → 302 `/auth/login`、それ以外 → 401(Fail-closed)
- **ratelimit**: `x-real-ip`(ホストが偽装不能に再発行)per-IP のトークンバケット。
  同じ wasm を `ratelimit`(全経路)/ `ratelimit-auth`(ログイン経路、厳しめ)の
  2 つの id で登録している(フィルタ 1 枚 = バケット 1 種のため)

ルート: `/auth` → moka-web(ratelimit-auth のみ、未認証者の入口)、`/healthz` → moka-web
(ratelimit のみ、upstream 契約)、`/` → moka-web(ratelimit + session-auth)。
`/api` はエッジ非公開のまま(ADR00011)。

## ファイル

| パス | 役割 |
|---|---|
| `manifest.tmpl.toml` | manifest の**テンプレート**。digest pin と HMAC 鍵は `plecto-manifest-render` ジョブが埋める |
| `filters/` | フィルタの Rust ソース(cargo workspace)。`wit/` は plecto:filter@0.3.0 の vendor |
| `build/filters-build.sh` | one-shot ジョブ `plecto-filters-build` の本体(ビルド → component 化 → 署名 → OCI layout) |
| `build/manifest-render.sh` | one-shot ジョブ `plecto-manifest-render` の本体(テンプレート → 完成 manifest) |

plecto コンテナが読む実体はすべて named volume `plecto-runtime`(コンテナ内 `/run/plecto`、ro)に
one-shot ジョブ群が組み立てる: `manifest.toml` / `filters/<name>/`(署名済み OCI image-layout)/
`keys/filter-signer.pub`([trust] の公開鍵)/ `certs/`(plecto-certs volume が上書き)。

公式イメージ `ghcr.io/kaikei-e/plecto` を compose.yaml から直接参照する(digest pin)。

## 初回セットアップ

`secrets/README.md` の手順で `session_hmac_key.txt` と `plecto_filter_signing_key.pem` を
作ってから `docker compose up -d --wait`(初回はフィルタのビルドで数分かかる。2 回目以降は
`plecto-build-cache` volume で高速)。

**ローカル開発の認証**: フィルタが載ったので https://localhost は認証必須になった。
初回はパスキー登録から:

1. `https://localhost/` を開く → 未認証なので `/auth/login` へ 302 される
2. パスキーを登録する(パスキーが 1 本も無い間だけ登録が開放される — ADR00021 の
   初回ブートストラップ。登録・ログインの儀式は moka-core 側の実装)
3. ログインすると moka-core が署名 cookie(`moka_session`)を発行し、以後のリクエストは
   session-auth フィルタを通過する

エッジを経由しない開発・E2E(`compose.e2e.yaml`: `:3000` / `:8080` 直)は認証の影響を受けない。

## 運用

```bash
curl -sk https://localhost/                # エッジ経由(自己署名なので -k)。未認証は 302/401
curl -s  http://localhost:9099/readyz      # admin: readiness
curl -s  http://localhost:9099/metrics | grep '^plecto_'
docker compose logs plecto -f              # JSON 構造化ログ + アクセスログ

# フィルタのソースを変えた(再ビルド → 再署名 → digest 更新 → リロード):
docker compose run --rm plecto-filters-build
docker compose run --rm plecto-manifest-render
docker compose kill -s HUP plecto

# manifest テンプレートだけ変えた(ルート・バケット仕様・config):
docker compose run --rm plecto-manifest-render
docker compose kill -s HUP plecto
```

- 壊れた manifest / 検証に失敗するフィルタは fail-closed で無視され、旧構成が生き続ける
- **`[trust]`(署名公開鍵)の変更だけは SIGHUP 不可** — `docker compose restart plecto`
- `session_hmac_key` を替えたら moka-core と plecto の両方に行き渡らせる(render 再実行 +
  HUP、moka-core 再起動)。既存セッションは全滅する(ステートレス署名 cookie の性質)

フィルタのユニットテスト・lint はホストで:

```bash
cd plecto/filters && cargo fmt --check && cargo clippy --target wasm32-unknown-unknown --release -- -D warnings \
  && cargo clippy --all-targets -- -D warnings && cargo test \
  && cargo build --target wasm32-unknown-unknown --release
```

(判定ロジックは host API 非依存の純関数に切り出してあり、native `cargo test` で回る。
WIT 越しの実挙動はエッジ経由の E2E で守る — tdd-workflow)

公式イメージは distroless(シェル/openssl 無し)で、`plecto` CLI にも moka-core の
`moka healthz` に相当する自己プローブ用サブコマンドが無いため、compose の `healthcheck:` は
張っていない。上記の `curl` で手動確認するか、`docker compose logs plecto` で起動ログを見る。

## upstream 契約

- 各サービスは **GET /healthz に 200** を返す(通るまでルーティングに入らない = それまで 503)
- upstream へは平文 HTTP/1.1(TLS はエッジで終端)
- `/api` prefix は strip しない — moka-core は `/api/...` のパスで受ける

## Plecto の pin 更新

`compose.yaml` の `plecto.image` のタグと digest を進めて `docker compose pull plecto && docker compose up -d plecto`。
タグは可変(実例: `v0.1.0` が同じ番号のまま指す commit を差し替えたことがある)なので、
`docker buildx imagetools inspect ghcr.io/kaikei-e/plecto:<tag>` で digest を確認してから pin する。
フィルタの WIT(`filters/wit/`)は plecto:filter@0.3.0 の vendor — pin 更新時は
`wkg get plecto:filter@0.3.0`(ghcr.io、prefix `kaikei-e/wit/`)か Plecto リポジトリの
`plecto/wit/world.wit` と差分が無いか確認する。
踏んだ問題は回避せず Plecto 側に issue/ADR を起票する(ドッグフーディングの本義)。
