# moka-web

moka-1 の Web UI(SvelteKit SSR)。サイドバーの記事リストと読書カラムの2ペインで「静かに読む場所」を提供する。moka-core へはサーバー側(BFF: `+page.server.ts` / `+server.ts`)からのみ到達し、ブラウザは moka-web としか話さない。

- デザインの正典: [DESIGN_LANGUAGE.md](./DESIGN_LANGUAGE.md)(瑠璃と金泥)
- ドメイン用語: リポジトリルートの `CONTEXT.md`

## コマンド

```sh
pnpm install       # 依存の導入
pnpm dev           # 開発サーバー(moka-core は別途 docker compose で)
pnpm lint          # prettier --check + eslint
pnpm check         # svelte-check(型)
pnpm test          # vitest --run(コンポーネントは実ブラウザ実行)
pnpm test:e2e      # Playwright(実スタック前提。手順は ../e2e/README.md)
pnpm format        # prettier --write
pnpm build         # 本番ビルド(adapter-node)
```

環境変数 `MOKA_CORE_URL`(既定 `http://localhost:8080`)で moka-core の宛先を指定する。本番は compose.yaml が配線する。
