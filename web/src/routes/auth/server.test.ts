// パスキー認証の BFF 窓口(ADR00021)。配線と Set-Cookie 中継だけを検証する —
// WebAuthn の儀式 JSON はパースせず中継に徹する(summary/qa の SSE 中継と同じ作法)。
// moka-core が発行する署名 cookie はここを素通りしてブラウザに届かなければならない。
import { describe, expect, it } from 'vitest';
import { GET as status } from './status/+server';
import { POST as registerBegin } from './register/begin/+server';
import { POST as registerFinish } from './register/finish/+server';
import { POST as loginBegin } from './login/begin/+server';
import { POST as loginFinish } from './login/finish/+server';

function postRequest(body?: string): Request {
	return new Request('http://localhost/auth/ceremony', {
		method: 'POST',
		...(body === undefined ? {} : { headers: { 'Content-Type': 'application/json' }, body })
	});
}

const call = {
	status: (fetchFn: typeof fetch) =>
		status({ fetch: fetchFn } as unknown as Parameters<typeof status>[0]),
	registerBegin: (fetchFn: typeof fetch) =>
		registerBegin({
			fetch: fetchFn,
			request: postRequest()
		} as unknown as Parameters<typeof registerBegin>[0]),
	registerFinish: (fetchFn: typeof fetch, body: string) =>
		registerFinish({
			fetch: fetchFn,
			request: postRequest(body)
		} as unknown as Parameters<typeof registerFinish>[0]),
	loginBegin: (fetchFn: typeof fetch) =>
		loginBegin({
			fetch: fetchFn,
			request: postRequest()
		} as unknown as Parameters<typeof loginBegin>[0]),
	loginFinish: (fetchFn: typeof fetch, body: string) =>
		loginFinish({
			fetch: fetchFn,
			request: postRequest(body)
		} as unknown as Parameters<typeof loginFinish>[0])
};

function jsonResponse(statusCode: number, body: unknown, cookies: string[] = []): Response {
	const headers = new Headers({ 'Content-Type': 'application/json' });
	for (const cookie of cookies) headers.append('Set-Cookie', cookie);
	return new Response(JSON.stringify(body), { status: statusCode, headers });
}

describe('GET /auth/status (BFF)', () => {
	it('relays the registration state from moka-core', async () => {
		let requestedUrl = '';
		const fetchFn: typeof fetch = async (input) => {
			requestedUrl = String(input);
			return jsonResponse(200, { registered: true });
		};

		const res = await call.status(fetchFn);

		expect(requestedUrl).toContain('/api/v1/auth/status');
		expect(res.status).toBe(200);
		expect(await res.json()).toEqual({ registered: true });
	});

	it('maps an upstream failure to a quiet fact-plus-next-step error (§7.2)', async () => {
		const fetchFn: typeof fetch = async () => {
			throw new Error('connection refused');
		};

		const res = await call.status(fetchFn);

		expect(res.status).toBe(502);
		const body = await res.json();
		expect(body.error).toBe('鍵の状態を確かめられませんでした。再読み込みしてください');
	});
});

describe('POST /auth/register/begin (BFF)', () => {
	it('forwards to moka-core and relays the creation options untouched', async () => {
		const creation = { publicKey: { challenge: 'AQID' } };
		let requestedUrl = '';
		let method = '';
		const fetchFn: typeof fetch = async (input, init) => {
			requestedUrl = String(input);
			method = init?.method ?? '';
			return jsonResponse(200, creation);
		};

		const res = await call.registerBegin(fetchFn);

		expect(requestedUrl).toContain('/api/v1/auth/register/begin');
		expect(method).toBe('POST');
		expect(res.status).toBe(200);
		expect(await res.json()).toEqual(creation);
	});

	it('maps the already-registered conflict (409) to a quiet message', async () => {
		const res = await call.registerBegin(async () =>
			jsonResponse(409, { error: 'already registered' })
		);

		expect(res.status).toBe(409);
		const body = await res.json();
		expect(body.error).toBe('パスキーは既にあります。再読み込みしてください');
	});

	it('maps an unreachable moka-core to a quiet 502', async () => {
		const fetchFn: typeof fetch = async () => {
			throw new Error('connection refused');
		};

		const res = await call.registerBegin(fetchFn);

		expect(res.status).toBe(502);
		const body = await res.json();
		expect(body.error).toBe('パスキーを作れませんでした。もう一度試してください');
	});
});

describe('POST /auth/register/finish (BFF)', () => {
	it('forwards the credential body untouched and relays Set-Cookie to the browser', async () => {
		const credential = JSON.stringify({ id: 'cred', rawId: 'AQID', response: {} });
		const cookie = 'moka_session=signed.value; Path=/; HttpOnly; SameSite=Lax';
		let forwardedBody = '';
		let forwardedContentType = '';
		const fetchFn: typeof fetch = async (_input, init) => {
			forwardedBody = String(init?.body);
			forwardedContentType = new Headers(init?.headers).get('Content-Type') ?? '';
			return jsonResponse(201, { ok: true }, [cookie]);
		};

		const res = await call.registerFinish(fetchFn, credential);

		expect(forwardedBody).toBe(credential);
		expect(forwardedContentType).toBe('application/json');
		expect(res.status).toBe(201);
		expect(res.headers.getSetCookie()).toEqual([cookie]);
		expect(await res.json()).toEqual({ ok: true });
	});

	it('relays every Set-Cookie header, not just the first', async () => {
		const cookies = ['moka_session=a; Path=/; HttpOnly', 'moka_hint=b; Path=/'];
		const fetchFn: typeof fetch = async () => jsonResponse(201, { ok: true }, cookies);

		const res = await call.registerFinish(fetchFn, '{}');

		expect(res.headers.getSetCookie()).toEqual(cookies);
	});

	it('maps an upstream rejection to a quiet message without leaking cookies', async () => {
		const res = await call.registerFinish(
			async () => jsonResponse(400, { error: 'verification failed' }),
			'{}'
		);

		expect(res.status).toBe(400);
		const body = await res.json();
		expect(body.error).toBe('パスキーを作れませんでした。もう一度試してください');
		expect(res.headers.getSetCookie()).toEqual([]);
	});
});

describe('POST /auth/login/begin (BFF)', () => {
	it('forwards to moka-core and relays the assertion options untouched', async () => {
		const assertion = { publicKey: { challenge: 'BAUG' } };
		let requestedUrl = '';
		const fetchFn: typeof fetch = async (input) => {
			requestedUrl = String(input);
			return jsonResponse(200, assertion);
		};

		const res = await call.loginBegin(fetchFn);

		expect(requestedUrl).toContain('/api/v1/auth/login/begin');
		expect(res.status).toBe(200);
		expect(await res.json()).toEqual(assertion);
	});

	it('maps the not-registered state (404) to a quiet message', async () => {
		const res = await call.loginBegin(async () => jsonResponse(404, { error: 'no credentials' }));

		expect(res.status).toBe(404);
		const body = await res.json();
		expect(body.error).toBe('パスキーがまだありません。再読み込みしてください');
	});
});

describe('POST /auth/login/finish (BFF)', () => {
	it('forwards the assertion body untouched and relays Set-Cookie to the browser', async () => {
		const assertion = JSON.stringify({ id: 'cred', rawId: 'AQID', response: {} });
		const cookie = 'moka_session=signed.value; Path=/; HttpOnly; SameSite=Lax';
		let forwardedBody = '';
		let requestedUrl = '';
		const fetchFn: typeof fetch = async (input, init) => {
			requestedUrl = String(input);
			forwardedBody = String(init?.body);
			return jsonResponse(200, { ok: true }, [cookie]);
		};

		const res = await call.loginFinish(fetchFn, assertion);

		expect(requestedUrl).toContain('/api/v1/auth/login/finish');
		expect(forwardedBody).toBe(assertion);
		expect(res.status).toBe(200);
		expect(res.headers.getSetCookie()).toEqual([cookie]);
		expect(await res.json()).toEqual({ ok: true });
	});

	it('maps a failed assertion to a quiet message (技術用語を出さない)', async () => {
		const res = await call.loginFinish(
			async () => jsonResponse(400, { error: 'assertion verification failed' }),
			'{}'
		);

		expect(res.status).toBe(400);
		const body = await res.json();
		expect(body.error).toBe('鍵を開けられませんでした。もう一度試してください');
	});
});
