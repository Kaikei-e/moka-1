// ログアウトの BFF 窓口(ADR00021)。moka-core は失敗しない(ステートレス)ので、
// ここで確かめるのは Set-Cookie の中継が落ちていないことだけ(register/login と同じ作法)。
import { describe, expect, it } from 'vitest';
import { POST as logout } from './+server';

function postRequest(): Request {
	return new Request('http://localhost/account/logout', { method: 'POST' });
}

function jsonResponse(statusCode: number, body: unknown, cookies: string[] = []): Response {
	const headers = new Headers({ 'Content-Type': 'application/json' });
	for (const cookie of cookies) headers.append('Set-Cookie', cookie);
	return new Response(JSON.stringify(body), { status: statusCode, headers });
}

describe('POST /account/logout (BFF)', () => {
	it('relays the cookie-clearing Set-Cookie from moka-core', async () => {
		let requestedUrl = '';
		let requestedMethod = '';
		const fetchFn: typeof fetch = async (input, init) => {
			requestedUrl = String(input);
			requestedMethod = init?.method ?? '';
			return jsonResponse(200, { ok: true }, [
				'moka_session=; Path=/; Max-Age=0; HttpOnly; Secure; SameSite=Lax'
			]);
		};

		const res = await logout({
			fetch: fetchFn,
			request: postRequest()
		} as unknown as Parameters<typeof logout>[0]);

		expect(requestedUrl).toContain('/api/v1/auth/logout');
		expect(requestedMethod).toBe('POST');
		expect(res.status).toBe(200);
		expect(await res.json()).toEqual({ ok: true });
		const setCookie = res.headers.getSetCookie?.() ?? [res.headers.get('set-cookie') ?? ''];
		expect(setCookie.join('\n')).toContain('moka_session=');
		expect(setCookie.join('\n')).toContain('Max-Age=0');
	});

	it('upstream network failure returns a quiet 502', async () => {
		const fetchFn: typeof fetch = async () => {
			throw new Error('network down');
		};

		const res = await logout({
			fetch: fetchFn,
			request: postRequest()
		} as unknown as Parameters<typeof logout>[0]);

		expect(res.status).toBe(502);
		const body = await res.json();
		expect(body.error).toBeTruthy();
	});
});
