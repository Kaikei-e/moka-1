import { test as setup, expect } from '@playwright/test';

// moka-core の起動待ち。旧 Hurl 群は各ファイル冒頭に retry 付きの GET /healthz を
// 置いていたが、Playwright では setup プロジェクトの依存として1回だけ通す
// (playwright.config.ts の projects: setup → api)。
setup('moka-core が healthz に応答する', async ({ request }) => {
	await expect
		.poll(async () => (await request.get('/healthz')).status(), {
			timeout: 60_000,
			intervals: [500, 1000, 2000]
		})
		.toBe(200);
});
