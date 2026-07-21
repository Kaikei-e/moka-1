---
name: bp-rust-wasm
description: Rust ベストプラクティス(Plecto WASM フィルタ向け、Edition 2024)。plecto:filter WIT コントラクトを実装する WASM Component Model フィルタの規約とツールチェーン。
  TRIGGER when: .rs / .wit ファイルを編集・作成する時、plecto/filters/ 以下を実装する時、Plecto フィルタを書く時。
  DO NOT TRIGGER when: テストの実行のみ、Cargo.toml の確認のみ、ファイルの読み取りのみ、他言語の作業時。
---

# Rust Best Practices (Plecto WASM Filters)

moka-1 の Rust は**サーバーではなく WASM Component Model フィルタ**(`plecto:filter@0.3.0` WIT コントラクト実装 — 契約バージョンは Plecto 本体と独立採番)。
Alt の tokio サーバー規約とは別物。ここでの失敗・不便はドッグフーディングの成果物 — 回避策で黙って進めず Plecto に issue/ADR を起票する(tenets §3.5)。

## ツールチェーン(Plecto 0.5.3 / MSRV 1.97.1 — WIT は Phase 2 実装 2026-07-19 の plecto:filter@0.3.0 のまま)

1. **`wasm32-unknown-unknown` + `wasm-tools component new` の2段構え**(Plecto docs/writing-a-filter.md の公式手順。**wasip2 ではない** — Tier A フィルタは zero-WASI で、`wasi:*` import が1つでもあるとロード拒否される)。cargo-component も使わない:
   ```bash
   rustup target add wasm32-unknown-unknown
   cargo build --target wasm32-unknown-unknown --release
   wasm-tools component new target/wasm32-unknown-unknown/release/<name>.wasm -o <name>.component.wasm
   wasm-tools component wit <name>.component.wasm   # import が plecto:filter/* のみか確認
   ```
   moka-1 では `plecto/build/filters-build.sh`(one-shot ジョブ)がこの手順の単一ソース。
2. **バインディングは `wit-bindgen`(0.59系)の `generate!` マクロ**: WIT は `wkg get plecto:filter@0.3.0`(ghcr.io / prefix `kaikei-e/wit/`)で取得し `plecto/filters/wit/` に vendor(リリースノートの digest と照合してから使う):
   ```rust
   wit_bindgen::generate!({ world: "filter", path: "wit" });
   ```
3. **Cargo.toml**: `edition = "2024"`, `crate-type = ["cdylib"]`, 自前 `[workspace]` 宣言(親 workspace に吸収させない)。リリースプロファイルはサイズ最適化:
   ```toml
   [profile.release]
   opt-level = "s"
   lto = true
   strip = true
   ```
4. **WASI 0.3 (P3) はまだ追わない**: 2026 年時点で RC 段階。Plecto は 0.2 系 Component Model。**再検討トリガー: Plecto 本体が P3 / async component に移行したら**

## フィルタ設計

5. **決定は必ず 3 値を明示的に返す**: `continue` / `modified(edit)` / `short-circuit(synthetic-response)`。暗黙のフォールスルーを書かない
6. **fail-closed**: 認証・防御フィルタで「エラーだから素通し」は絶対に書かない。パース失敗・KV 不達・想定外入力はすべて short-circuit(401/403/500)に倒す:
   ```rust
   let Some(key) = headers.get("x-api-key") else {
       return Decision::ShortCircuit(unauthorized());
   };
   ```
7. **capability は deny-by-default**: manifest で lend された host API(`host-log` / `host-clock` / `host-kv` / `host-counter` / `host-ratelimit`)以外を前提にしない。新 capability が必要になったら、まずそれが「per-request ポリシー」か「グローバル横断関心事」かを問う — 後者なら Plecto ネイティブ側の仕事(ADR 29 の role-driven placement)
8. **フィルタはステートレス**: `static mut`・`OnceLock` でのリクエスト跨ぎ状態は禁止。trust-branched 実行(pooled か fresh-per-request か)は運用側の決定であり、フィルタはどちらでも正しく動く必要がある。共有状態は host-kv / host-counter へ
9. **1 フィルタ = 1 関心事**: 認証フィルタにレート制限を混ぜない。チェーンの合成は plecto/manifest.toml の仕事
10. **予算感覚**: pooled 実行のオーバーヘッドは ~2µs/req。ホットパス(on-request)でのアロケーション・正規表現コンパイルを避ける。regex が要るなら `init()` で構築して resource に保持

## 一般 Rust 原則

11. **エラー型は thiserror**: `#[derive(Debug, Error)]` でフィルタ内エラーを定義し、最終的に WIT の decision に写像する。`anyhow` は使わない(境界が WIT なので型を保つ)
12. **借用優先**: `.clone()` を安易に使わない。`&str` > `String`、`&[u8]` > `Vec<u8>`。ヘッダ処理はゼロコピーを意識
13. **match 網羅性**: WIT variant・自前 enum とも `_` ワイルドカードより明示的なバリアント列挙。WIT 側の追加をコンパイルエラーで検出する
14. **`pub(crate)` デフォルト**: 公開は wit-bindgen が要求するエクスポートのみ
15. **println! / eprintln! / tracing 禁止**: ログは `host-log` capability のみ。時刻は `host-clock`(`SystemTime::now()` はサンドボックスで lend されない前提で書く)
16. **panic はトラップ**: フィルタの panic は Plecto 側で fail-closed 扱いになる(通信は落ちる)。回復可能な失敗は Result で運び、panic は「バグ」の意味にのみ予約。`unwrap()` は初期化時と不変条件の明示(コメント付き)のみ

## テスト

17. **ロジックは native でユニットテスト**: 判定ロジックを host API 非依存の純関数(`&HeaderMap -> Verdict` 的シグネチャ)に切り出し、`cargo test`(ホスト native)で回す。host API はトレイトで抽象化してフェイクを注入
18. **境界は E2E で**: WIT 越しの実挙動は Plecto ホストに実際にロードして Hurl で検証(tdd-workflow の `e2e/hurl/edge/`)。wasmtime を使った自前ハーネスは作らない(Plecto のカンフォーマンステストに寄せる)
19. **CI パリティ**: `cargo fmt --check && cargo clippy --all-targets -- -D warnings && cargo clippy --target wasm32-unknown-unknown --release -- -D warnings && cargo test && cargo build --target wasm32-unknown-unknown --release`

## 配布(Phase 2 で前倒し確定)

20. Plecto は**ローカルでも bare `.wasm` をロードしない** — 署名済み OCI image-layout + digest pin + `[trust]` 公開鍵が必須(fail-closed)。moka-1 では one-shot ジョブ2本(`plecto-filters-build` → `plecto-manifest-render`)がビルド〜署名〜manifest レンダリングを担い、反映は `docker compose run --rm` で両ジョブ再実行 → `docker compose kill -s HUP plecto`。GHCR への OCI 配布は Phase 4(tenets §3.5)

## 参照

- `docs/tenets/moka-tenets.md` — 本スキルの「tenets §N」参照はこの文書の節番号
