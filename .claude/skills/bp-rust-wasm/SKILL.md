---
name: bp-rust-wasm
description: Rust ベストプラクティス(Plecto WASM フィルタ向け、Edition 2024)。plecto:filter WIT コントラクトを実装する WASM Component Model フィルタの規約とツールチェーン。
  TRIGGER when: .rs / .wit ファイルを編集・作成する時、plecto/filters/ 以下を実装する時、Plecto フィルタを書く時。
  DO NOT TRIGGER when: テストの実行のみ、Cargo.toml の確認のみ、ファイルの読み取りのみ、他言語の作業時。
---

# Rust Best Practices (Plecto WASM Filters)

moka-1 の Rust は**サーバーではなく WASM Component Model フィルタ**(`plecto:filter@0.1.0` WIT コントラクト実装)。
Alt の tokio サーバー規約とは別物。ここでの失敗・不便はドッグフーディングの成果物 — 回避策で黙って進めず Plecto に issue/ADR を起票する(tenets §3.5)。

## ツールチェーン(2026 年時点の正解)

1. **plain cargo + `wasm32-wasip2` ターゲット**: Rust 1.82+ で upstream 化済み。**cargo-component は非推奨化が進行中なので新規では使わない**
   ```bash
   rustup target add wasm32-wasip2
   cargo build --target wasm32-wasip2 --release   # そのままコンポーネントが出る
   ```
2. **バインディングは `wit-bindgen` の `generate!` マクロ**: WIT は Plecto 本体の `wit/` を参照(コピーしない。バージョンずれ検出のため path/OCI 参照で一元化)
   ```rust
   wit_bindgen::generate!({ world: "filter", path: "../../wit" });
   ```
3. **Cargo.toml**: `edition = "2024"`, `crate-type = ["cdylib"]`。リリースプロファイルはサイズ最適化:
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
19. **CI パリティ**: `cargo fmt --check && cargo clippy --target wasm32-wasip2 -- -D warnings && cargo test && cargo build --target wasm32-wasip2 --release`

## 配布(M4 以降)

20. フィルタは OCI アーティファクト + cosign 署名 + SBOM で配布(Plecto M4 機能のドッグフーディング)。ローカル開発では compose のバインドマウント + SIGHUP ホットリロードで回す

## 参照

- `docs/tenets/moka-tenets.md` — 本スキルの「tenets §N」参照はこの文書の節番号
