import { test, expect } from '@playwright/test';

// パスキー管理(削除)とログアウト(/account、ADR00021)。
// このファイルは 07-auth-login.spec.ts が同一フレッシュ DB に登録したパスキーに依存する
// (workers=1・fullyParallel=false でファイルは実行順、07 が最後の登録を残す)。
// 実行順序: 一覧表示 → ログアウト(パスキーはまだ残っている前提)→ 削除(最後の1本を
// 削除してブートストラップが再び開くことを確認、破壊的操作なので末尾に置く)。
test.describe.configure({ mode: 'serial' });

test('/account に登録済みパスキーが一覧できる', async ({ page }) => {
	await page.goto('/account');

	await expect(page.getByRole('heading', { name: 'アカウント' })).toBeVisible();
	await expect(page.getByRole('heading', { name: 'パスキー' })).toBeVisible();
	await expect(page.getByText(/登録: /)).toBeVisible();
	await expect(page.getByRole('button', { name: '削除する' })).toBeVisible();
	await expect(page.getByRole('button', { name: 'ログアウト' })).toBeVisible();
});

test('ログアウトすると cookie が失効し /auth/login へ戻る', async ({ page }) => {
	await page.goto('/account');

	const logoutResponse = page.waitForResponse(
		(res) => res.url().endsWith('/account/logout') && res.request().method() === 'POST'
	);
	await page.getByRole('button', { name: 'ログアウト' }).click();

	const logout = await logoutResponse;
	expect(logout.status()).toBe(200);
	const setCookie = (await logout.headersArray())
		.filter((h) => h.name.toLowerCase() === 'set-cookie')
		.map((h) => h.value)
		.join('\n');
	expect(setCookie).toContain('moka_session=');
	expect(setCookie).toContain('Max-Age=0');

	await page.waitForURL('/auth/login');
	// パスキーはまだ残っているので、次の入口は「おかえりなさい」(ブートストラップは閉じたまま)
	await expect(page.getByText('おかえりなさい')).toBeVisible();

	// ブラウザの cookie も実際に消えている
	const session = (await page.context().cookies()).find((c) => c.name === 'moka_session');
	expect(session).toBeFalsy();
});

test('最後の1本を削除すると一覧から消え、ブートストラップが再び開く', async ({ page }) => {
	await page.goto('/account');
	await expect(page.getByRole('button', { name: '削除する' })).toBeVisible();

	await page.getByRole('button', { name: '削除する' }).click();
	await expect(page.getByRole('region', { name: '削除の確認' })).toBeVisible();
	await expect(page.getByText('これが最後のパスキーです')).toBeVisible();

	await page
		.getByRole('region', { name: '削除の確認' })
		.getByRole('button', { name: '削除する' })
		.click();

	await page.waitForURL('/account?deleted=1');
	await expect(page.getByText('削除しました')).toBeVisible();
	await expect(page.getByRole('button', { name: '削除する' })).not.toBeVisible();

	// パスキーが1本も無い = ブートストラップが再び開く(誰でも再登録できる回復経路、ADR00021)
	await page.goto('/auth/login');
	await expect(page.getByText('はじめまして。この moka はあなたのものです')).toBeVisible();
	await expect(page.getByRole('button', { name: 'パスキーを作る' })).toBeVisible();
});
