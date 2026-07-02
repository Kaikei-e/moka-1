---
name: bp-svelte
description: Svelte 5 / SvelteKit / TypeScript ベストプラクティス(moka-web 向け)。Runes ベースのコンポーネント設計、SvelteKit データフロー、型安全性の規約。
  TRIGGER when: .svelte / .ts ファイルを編集・作成する時、moka-web(web/ 以下)を実装する時。
  DO NOT TRIGGER when: テストの実行のみ、設定ファイルの確認のみ、ファイルの読み取りのみ、他言語の作業時。
---

# Svelte 5 & SvelteKit & TypeScript Best Practices (moka-web)

## Svelte 5 Runes

1. **Runes のみ**: `$state` / `$derived` / `$effect`。レガシー `$:` リアクティブ宣言・`export let` は禁止
2. **Props は `$props()` + 型**:
   ```svelte
   <script lang="ts">
     import type { Snippet } from 'svelte';
     let { article, onRead, children }: {
       article: Article; onRead?: (id: string) => void; children?: Snippet;
     } = $props();
   </script>
   ```
3. **導出は `$derived` / `$derived.by`**、副作用だけ `$effect`。`$effect` 内で状態を組み立て始めたら `$derived.by` に書き直す
4. **`$effect` は cleanup を返す**: EventSource / IntersectionObserver / タイマーは return で解放。DOM 要素に紐づくものは `{@attach ...}`(attachments、`use:` アクションの後継)を優先
5. **大きく置換されるデータは `$state.raw`**: 記事リスト・検索結果は要素単位の deep reactivity が不要。proxy コストを避け、更新は再代入で。シリアライズは `$state.snapshot()`
6. **合成は snippet**: `{#snippet row(item)}` + `{@render row(x)}`。slot は非推奨
7. **`$app/state` を使う**(`$app/stores` は非推奨): `page.url` 等は `$app/state` から

## SvelteKit データフロー

8. **初期データは load 関数**: `+page.server.ts` の `load` で moka-core から取得。コンポーネント内 fetch で初期データを組むと waterfall になる
9. **更新系は form actions + `use:enhance`**(既読化・フィード購読・Q&A 送信)。JS 無効でも動く progressive enhancement を既定に
10. **remote functions(2.27+)は当面使わない**: 実験フラグ(`kit.experimental.remoteFunctions` + `compilerOptions.experimental.async`)が要る段階。**再検討トリガー: 実験フラグが外れて安定化したら**、クライアント起点の再取得(ハイライト再生成ボタン等)を query/command に置き換える価値を評価
11. **moka-core へのアクセスはサーバー側に寄せる**: `+page.server.ts` / `+server.ts` 経由。ブラウザ→moka-core 直行のパスを作る場合は plecto/manifest.toml のルーティングと必ず整合させる
12. **エージェントの進行通知は SSE**: 濃縮・ハイライト生成の進捗は `+server.ts` から `text/event-stream`。WebSocket は入れない(ミニマリズム)
13. **ストリーミング load**: 重い付加データ(関連記事等)は load から Promise のまま返して `{#await}` で受ける。ページ骨格を先に出す

## TypeScript

14. **strict: true + noUncheckedIndexedAccess + verbatimModuleSyntax**: 必須。弱めない。型のみインポートは `import type`
15. **境界では unknown → Zod**: moka-core レスポンスは Zod スキーマ(単一の `lib/api/schemas.ts` に集約)で parse してから使う。`as` キャスト・`!` 非 null アサーション禁止
16. **判別共用体 + 網羅性**: `enrichment_status` のような状態は tagged union にし、`switch` の default で `satisfies never`
17. **`satisfies` でリテラル推論保持**: ルート表・設定オブジェクトは `satisfies Record<...>` で検査しつつ推論を殺さない
18. **型ガード > アサーション**: `value is T` の predicate を書く。Zod があるので手書きガードは境界の外側だけ

## テスト

19. **コンポーネントは vitest-browser-svelte**(実ブラウザ実行)。jsdom でのコンポーネントテストは書かない
20. **純ロジック(日付整形・RRF スコア表示等)は node 環境の vitest** に分離。コンポーネントに埋めずに `lib/` へ抽出してからテスト
21. E2E(Playwright)の規約は tdd-workflow スキル参照

## moka-1 固有

22. **ミニマル UI 原則**: 「読む」体験が主。3D 可視化・アニメーションライブラリ・重い UI キットは tenets §3.4 で不採用決定済み。新規依存はまず疑い、追加するなら bundle への影響を報告
23. **LLM 由来テキストの表示規約**: 要約・ハイライト理由には「AI 生成」の視覚的区別を付ける。`</think>` が漏れて表示されたらそれは moka-core 側のバグ(internal/llm の一元除去に違反)— UI 側で握りつぶさず報告する

## 参照

- `docs/tenets/moka-tenets.md` — 本スキルの「tenets §N」参照はこの文書の節番号
