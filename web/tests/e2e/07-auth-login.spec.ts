import { test, expect } from '@playwright/test';

// パスキーの入口 /auth/login(ADR00021)。fresh DB = 未登録なので、ブートストラップ導線
// (パスキーを作る)が出る。骨格は bare layout — 鍵を開ける前に記事は見せない。
//
// 儀式の完走: compose.e2e.yaml が WEBAUTHN_ORIGIN=http://localhost:3000 を上書きするので、
// CDP 仮想オーセンティケータで begin → navigator.credentials.create → finish(201 +
// Set-Cookie: moka_session)→ ホーム着地まで実際に通る。続けて同じ仮想キーで
// ログイン(unlock)側の儀式も完走する。
// 前提: フレッシュ DB + compose.e2e.yaml オーバーレイ + secrets/session_hmac_key.txt
test.describe.configure({ mode: 'serial' });

test('未登録の moka は「パスキーを作る」を差し出し、記事の骨格は見せない', async ({ page }) => {
	await page.goto('/auth/login');

	// ブートストラップの声(auth/status → registered: false)
	await expect(page.getByText('はじめまして。この moka はあなたのものです')).toBeVisible();
	await expect(
		page.getByText('パスキーを作ると、この店の鍵はあなただけのものになります')
	).toBeVisible();
	await expect(page.getByRole('button', { name: 'パスキーを作る' })).toBeVisible();

	// bare layout — サイドバー(検索・記事リスト)を持たない
	await expect(page.getByRole('searchbox', { name: '記事を探す' })).not.toBeVisible();

	// 鍵の状態が確かめられない時の文言(503 系)は出ていない = auth 配線が生きている
	await expect(page.getByText('鍵の状態を確かめられませんでした')).not.toBeVisible();
});

test('CDP 仮想オーセンティケータで登録の儀式を完走し、セッション cookie と共にホームへ着地する', async ({
	page
}) => {
	// 仮想オーセンティケータ(CDP WebAuthn ドメイン)で navigator.credentials.create/get を
	// 実際に解決させ、begin → create → finish のブラウザ側配線(webauthn.ts の
	// options パース / credential JSON 化)と moka-core の attestation 検証を丸ごと通す
	const cdp = await page.context().newCDPSession(page);
	await cdp.send('WebAuthn.enable');
	await cdp.send('WebAuthn.addVirtualAuthenticator', {
		options: {
			protocol: 'ctap2',
			transport: 'internal',
			hasResidentKey: true,
			hasUserVerification: true,
			isUserVerified: true,
			automaticPresenceSimulation: true
		}
	});

	await page.goto('/auth/login');
	const beginResponse = page.waitForResponse(
		(res) => res.url().endsWith('/auth/register/begin') && res.request().method() === 'POST'
	);
	const finishResponse = page.waitForResponse(
		(res) => res.url().endsWith('/auth/register/finish') && res.request().method() === 'POST'
	);
	await page.getByRole('button', { name: 'パスキーを作る' }).click();

	const begin = await beginResponse;
	expect(begin.status()).toBe(200);
	expect((await begin.json()).publicKey?.challenge).toBeTruthy();

	// 登録成功 = 201。BFF(relayAuthResponse)が moka-core の Set-Cookie を中継する
	const finish = await finishResponse;
	expect(finish.status()).toBe(201);
	const setCookie = (await finish.headersArray())
		.filter((h) => h.name.toLowerCase() === 'set-cookie')
		.map((h) => h.value)
		.join('\n');
	expect(setCookie).toContain('moka_session=');

	// 儀式完了 → ホームへ着地し、アプリの骨格(サイドバー)が開く
	await page.waitForURL('/');
	await expect(page.getByRole('searchbox', { name: '記事を探す' })).toBeVisible();

	// セッション cookie がブラウザに据わっている(HttpOnly の署名 cookie、ADR00021)
	const session = (await page.context().cookies()).find((c) => c.name === 'moka_session');
	expect(session).toBeTruthy();
	expect(session?.httpOnly).toBe(true);

	// 登録済みになった入口は「おかえりなさい」— 同じ仮想キーでログインの儀式も完走する
	await page.goto('/auth/login');
	await expect(page.getByText('おかえりなさい')).toBeVisible();
	const loginFinishResponse = page.waitForResponse(
		(res) => res.url().endsWith('/auth/login/finish') && res.request().method() === 'POST'
	);
	await page.getByRole('button', { name: 'パスキーで開ける' }).click();
	expect((await loginFinishResponse).status()).toBe(200);
	await page.waitForURL('/');
	await expect(page.getByRole('searchbox', { name: '記事を探す' })).toBeVisible();
});
