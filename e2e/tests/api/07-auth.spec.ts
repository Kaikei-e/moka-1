// パスキー認証 API(M2 / Plecto Phase 2: /api/v1/auth/* — ADR00021)
// 移行元: hurl/core/auth.hurl
//
// 前提: SESSION_HMAC_KEY_FILE が配線済み + secrets/session_hmac_key.txt が存在すること
// (未配線なら全エンドポイントが 503 — その分岐は unit テストの領分)。WebAuthn の儀式完走
// (attestation 生成)は仮想認証器を持つブラウザが要るため、page を使わないこの API 層のテストでは
// 出来ない。ここで見るのは fresh DB のブートストラップ分岐: 未登録 status / login 404 /
// register/begin 200 / 不正 attestation 400 / challenge の一回性。register/begin の 409
// (登録済みでの再登録拒否)は儀式完走が前提なので unit テスト(httpapi/auth_test.go)が守る。
//
// health gate は tests/setup/core-health.setup.ts(setup プロジェクト)に集約済みなのでここでは
// 打たない。フィードにも記事にも触れないので他シナリオとの順序依存は無い(フレッシュ DB 前提のみ、
// e2e/README.md の実行順序表を参照)。
//
// 変数: hurl 側の `host` は playwright.config.ts の baseURL(support/env.ts の coreBaseURL)に
// 対応し、request フィクスチャへ自動注入される。このファイルはテストをまたいで受け渡す
// モジュールスコープ変数を持たない(hurl 側にも [Captures] は無い)。
import { test, expect } from '@playwright/test';
import { json } from '../../support/moka-api';
import type {
	AuthStatusResponse,
	CredentialCreationResponse,
	ErrorResponse,
	OkResponse,
	PasskeyListResponse
} from '../../support/types';

test.describe.configure({ mode: 'serial' });

// 2. fresh DB は未登録 — web はこれでブートストラップ画面(パスキーを作る)を出す
test('2. fresh DB では registered が false であること', async ({ request }) => {
	const response = await request.get('/api/v1/auth/status');
	expect(response.status(), await response.text()).toBe(200);
	const body = await json<AuthStatusResponse>(response);
	expect(body.registered, '$.registered が false であること').toBe(false);
});

// 3 / 3b. 未登録での login/begin・login/finish はどちらも 404(ブートストラップ前にログインは
// 無い。儀式の有無より先に登録有無で断る)
test('未登録での login/begin・login/finish はどちらも 404 "no passkey registered" であること', async ({
	request
}) => {
	// 3. 未登録での login/begin は 404(ブートストラップ前にログインは無い)
	await test.step('3. login/begin は 404', async () => {
		const response = await request.post('/api/v1/auth/login/begin');
		expect(response.status(), await response.text()).toBe(404);
		const body = await json<ErrorResponse>(response);
		expect(body.error, '$.error').toBe('no passkey registered');
	});

	// 3b. login/finish も未登録 404(儀式の有無より先に登録有無で断る)
	await test.step('3b. login/finish も未登録 404', async () => {
		const response = await request.post('/api/v1/auth/login/finish', { data: {} });
		expect(response.status(), await response.text()).toBe(404);
		const body = await json<ErrorResponse>(response);
		expect(body.error, '$.error').toBe('no passkey registered');
	});
});

// 4/5/5b/6. register/begin で作った challenge が不正な attestation で消費され、
// 再利用できず、失敗した儀式では何も登録されないことを一続きの流れとして確認する
test('register の失敗した儀式は状態を変えず、challenge は一度しか使えないこと', async ({
	request
}) => {
	// 4. register/begin — fresh DB では 200 で CredentialCreation(単一ユーザー固定の RP/user)。
	// 409 になるのはパスキー登録済みでブートストラップが閉じた後のみ(fresh DB では起きない)
	await test.step('4. register/begin は 200 で CredentialCreation を返す', async () => {
		const response = await request.post('/api/v1/auth/register/begin');
		expect(response.status(), await response.text()).toBe(200);
		const body = await json<CredentialCreationResponse>(response);
		expect(body.publicKey.challenge, '$.publicKey.challenge exists').toBeDefined();
		expect(body.publicKey.rp.id, '$.publicKey.rp.id').toBe('localhost');
		expect(body.publicKey.rp.name, '$.publicKey.rp.name').toBe('moka');
		expect(body.publicKey.user.name, '$.publicKey.user.name').toBe('owner');
	});

	// 5. 不正な attestation での register/finish は 400(challenge はこの失敗で消費される)
	await test.step('5. 不正な attestation での register/finish は 400', async () => {
		const response = await request.post('/api/v1/auth/register/finish', {
			data: { nonsense: true }
		});
		expect(response.status(), await response.text()).toBe(400);
		const body = await json<ErrorResponse>(response);
		expect(body.error, '$.error').toBe('invalid webauthn response');
	});

	// 5b. challenge は一回で消費済み — begin 無しの再 finish は 400(no pending ceremony)
	await test.step('5b. begin 無しの再 finish は 400 "no pending ceremony"', async () => {
		const response = await request.post('/api/v1/auth/register/finish', {
			data: { nonsense: true }
		});
		expect(response.status(), await response.text()).toBe(400);
		const body = await json<ErrorResponse>(response);
		expect(body.error, '$.error').toBe('no pending ceremony');
	});

	// 6. 失敗した儀式では何も登録されない(fail-closed)
	await test.step('6. 失敗した儀式では何も登録されていない(fail-closed)', async () => {
		const response = await request.get('/api/v1/auth/status');
		expect(response.status(), await response.text()).toBe(200);
		const body = await json<AuthStatusResponse>(response);
		expect(body.registered, '$.registered が false のままであること').toBe(false);
	});
});

// ---- パスキー管理・ログアウト(fresh DB で確認できる境界のみ。実資格情報を伴う一覧・削除の
// 実地検証は儀式完走が要るため UI 層 Playwright(web/tests/e2e/08-account-passkeys.spec.ts)の
// 領分)----

// 7. 未登録 = 一覧は空配列
test('7. 未登録では passkeys 一覧が空配列であること', async ({ request }) => {
	const response = await request.get('/api/v1/auth/passkeys');
	expect(response.status(), await response.text()).toBe(200);
	const body = await json<PasskeyListResponse>(response);
	expect(body.passkeys, '$.passkeys count == 0').toHaveLength(0);
});

// 8. 数値でない id での削除は 400
test('8. 数値でない passkey id の削除は 400 "invalid passkey id"', async ({ request }) => {
	const response = await request.delete('/api/v1/auth/passkeys/not-a-number');
	expect(response.status(), await response.text()).toBe(400);
	const body = await json<ErrorResponse>(response);
	expect(body.error, '$.error').toBe('invalid passkey id');
});

// 9. 存在しない id での削除は 404(未登録状態でも同じ写像)
test('9. 存在しない passkey id の削除は 404 "passkey not found"', async ({ request }) => {
	const response = await request.delete('/api/v1/auth/passkeys/999999');
	expect(response.status(), await response.text()).toBe(404);
	const body = await json<ErrorResponse>(response);
	expect(body.error, '$.error').toBe('passkey not found');
});

// 10. ログアウトはセッションの有無に関わらず 200(ステートレス — ADR00021)。
// 常に cookie を失効させる Set-Cookie を返す(ブラウザは Max-Age=0 で即座に破棄する)。
// headers() は複数の Set-Cookie を "\n" 結合してしまうので headersArray() で個別に取る
// (調査ダイジェスト §8.2 / 共通ルール #3)。
test('10. logout はセッション無しでも 200 で、cookie を失効させる Set-Cookie を返すこと', async ({
	request
}) => {
	const response = await request.post('/api/v1/auth/logout');
	expect(response.status(), await response.text()).toBe(200);
	const body = await json<OkResponse>(response);
	expect(body.ok, '$.ok が true であること').toBe(true);

	const setCookieValues = response
		.headersArray()
		.filter((header) => header.name.toLowerCase() === 'set-cookie')
		.map((header) => header.value);
	expect(setCookieValues.length, 'Set-Cookie ヘッダが存在すること').toBeGreaterThanOrEqual(1);
	// hurl 側の 2 つの `header "Set-Cookie" contains ...` は、logout が返す Set-Cookie が
	// 1本だけなので「同一の cookie 文字列が両方を含む」ことを主張していた。2つの述語を
	// 別々の cookie で満たしてしまわないよう、moka_session の1本を掴んでから両方を見る
	const sessionCookie = setCookieValues.find((value) => value.includes('moka_session='));
	expect(sessionCookie, 'header "Set-Cookie" contains "moka_session="').toBeDefined();
	expect(sessionCookie, 'header "Set-Cookie" contains "Max-Age=0"').toContain('Max-Age=0');
});
