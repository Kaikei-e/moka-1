import { defineConfig } from '@playwright/test';
import { edgeBaseURL } from './support/env';

// エッジ(Plecto Phase 2: セッション認証 + レート制限)の E2E。
// API 層(playwright.config.ts)とは別の config にしてあるのは、こちらだけ前提が違うため —
// compose.e2e.yaml は不要で、代わりに本番スタックのエッジ側(署名済みフィルタ +
// レンダリング済み manifest で起動した plecto)が要る。手順は e2e/README.md 参照。
//
// 自己署名証明書なので ignoreHTTPSErrors。末尾のバーストシナリオが per-IP の /auth
// バケットを空にするため、連続実行する場合は 10 秒ほど空ける。
export default defineConfig({
	testDir: 'tests',
	fullyParallel: false,
	workers: 1,
	retries: 0,
	forbidOnly: !!process.env.CI,
	timeout: 60_000,
	expect: { timeout: 15_000 },

	use: {
		baseURL: edgeBaseURL,
		ignoreHTTPSErrors: true,
		trace: 'retain-on-failure'
	},

	projects: [
		{
			name: 'setup',
			testDir: 'tests/setup',
			testMatch: /edge-health\.setup\.ts/
		},
		{
			name: 'edge',
			testDir: 'tests/edge',
			dependencies: ['setup']
		}
	],

	reporter: process.env.CI
		? [['list'], ['html', { open: 'never' }], ['junit', { outputFile: 'reports/junit-edge.xml' }]]
		: [['list']]
});
