// 既読打刻 BFF の中継配線だけを検証する — moka-core との契約(POST /read → 204)は
// core.spec.ts が守る。フェイルソフト: どんな失敗もボディ無しのステータスに畳む。
import { describe, expect, it } from 'vitest';
import { POST } from './+server';

type PostEvent = Parameters<typeof POST>[0];

function postEvent(id: string, fetchFn: typeof fetch): PostEvent {
	return { fetch: fetchFn, params: { id } } as unknown as PostEvent;
}

describe('POST /articles/[id]/read (既読打刻 BFF)', () => {
	it('relays the stamp to moka-core and answers 204 with no body', async () => {
		let requestedUrl = '';
		let requestedMethod = '';
		const fetchFn: typeof fetch = async (input, init) => {
			requestedUrl = String(input);
			requestedMethod = init?.method ?? '';
			return new Response(null, { status: 204 });
		};

		const res = await POST(postEvent('7', fetchFn));

		expect(requestedUrl).toMatch(/\/api\/v1\/articles\/7\/read$/);
		expect(requestedMethod).toBe('POST');
		expect(res.status).toBe(204);
		expect(await res.text()).toBe('');
	});

	it('rejects a non-numeric id with 400 without calling moka-core', async () => {
		let called = false;
		const fetchFn: typeof fetch = async () => {
			called = true;
			return new Response(null, { status: 204 });
		};

		const res = await POST(postEvent('abc', fetchFn));

		expect(res.status).toBe(400);
		expect(called).toBe(false);
	});

	it('maps an upstream error status to a bodyless 502 (fail-soft)', async () => {
		const res = await POST(postEvent('7', async () => new Response(null, { status: 500 })));

		expect(res.status).toBe(502);
		expect(await res.text()).toBe('');
	});

	it('maps a network failure to a bodyless 502 (fail-soft)', async () => {
		const failing: typeof fetch = async () => {
			throw new Error('network down');
		};

		const res = await POST(postEvent('7', failing));

		expect(res.status).toBe(502);
	});
});
