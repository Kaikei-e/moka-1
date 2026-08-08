import { test as setup, expect } from '@playwright/test';

// エッジ(Plecto)→ moka-web まで生きていることの確認。/healthz ルートは
// セッション認証の除外パス(upstream 契約の監視経路)なので cookie 無しで通る。
setup('plecto が healthz に応答する', async ({ request }) => {
	await expect
		.poll(async () => (await request.get('/healthz')).status(), {
			timeout: 60_000,
			intervals: [500, 1000, 2000]
		})
		.toBe(200);
});
