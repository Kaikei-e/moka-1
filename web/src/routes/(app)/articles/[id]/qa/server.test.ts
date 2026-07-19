// 問い返し(訊く)のBFF窓口: POST /articles/[id]/qa → moka-core の SSE をそのまま中継する。
// summary/stream と同じ作法 — パースせず転送し、非200だけ moka の声に写す。
import { describe, expect, it } from 'vitest';
import { POST } from './+server';

type PostEvent = Parameters<typeof POST>[0];

function postEvent(id: string, body: unknown, fetchFn: typeof fetch): PostEvent {
	return {
		fetch: fetchFn,
		params: { id },
		request: new Request(`http://localhost/articles/${id}/qa`, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: typeof body === 'string' ? body : JSON.stringify(body)
		})
	} as unknown as PostEvent;
}

function fetchStub(status: number, body: unknown): typeof fetch {
	return async () =>
		new Response(JSON.stringify(body), {
			status,
			headers: { 'Content-Type': 'application/json' }
		});
}

describe('POST /articles/[id]/qa (問い返し SSE relay BFF)', () => {
	it('forwards the question to moka-core and relays the SSE stream untouched', async () => {
		const sse = 'event: delta\ndata: {"text":"答えの断片"}\n\n';
		let requestedUrl = '';
		let forwardedBody = '';
		const fetchFn: typeof fetch = async (input, init) => {
			requestedUrl = String(input);
			forwardedBody = String(init?.body);
			return new Response(sse, {
				status: 200,
				headers: { 'Content-Type': 'text/event-stream' }
			});
		};

		const res = await POST(postEvent('7', { question: 'この記事の背景は' }, fetchFn));

		expect(requestedUrl).toContain('/api/v1/articles/7/qa');
		expect(JSON.parse(forwardedBody)).toEqual({ question: 'この記事の背景は' });
		expect(res.status).toBe(200);
		expect(res.headers.get('Content-Type')).toBe('text/event-stream');
		expect(await res.text()).toBe(sse);
	});

	it('rejects an invalid article id without calling moka-core', async () => {
		let called = false;
		const fetchFn: typeof fetch = async () => {
			called = true;
			return new Response('{}', { status: 200 });
		};

		const res = await POST(postEvent('abc', { question: 'q' }, fetchFn));

		expect(called).toBe(false);
		expect(res.status).toBe(400);
	});

	it('rejects an empty question without calling moka-core', async () => {
		let called = false;
		const fetchFn: typeof fetch = async () => {
			called = true;
			return new Response('{}', { status: 200 });
		};

		const res = await POST(postEvent('7', { question: '   ' }, fetchFn));

		expect(called).toBe(false);
		expect(res.status).toBe(400);
		const body = await res.json();
		expect(body.error).toBe('質問を入力してください');
	});

	it('rejects a non-JSON body without calling moka-core', async () => {
		let called = false;
		const fetchFn: typeof fetch = async () => {
			called = true;
			return new Response('{}', { status: 200 });
		};

		const res = await POST(postEvent('7', 'not-json', fetchFn));

		expect(called).toBe(false);
		expect(res.status).toBe(400);
	});

	it.each([500, 502])(
		'maps an upstream %i to a quiet servant-voiced error (技術用語を出さない)',
		async (status) => {
			const res = await POST(
				postEvent('7', { question: 'q' }, fetchStub(status, { error: 'llm backend unavailable' }))
			);

			expect(res.status).toBe(status);
			const body = await res.json();
			expect(body.error).toBe('答えられませんでした。時間をおいて訊き直してください');
		}
	);

	it('maps a missing article to the quiet not-found message', async () => {
		const res = await POST(
			postEvent('999', { question: 'q' }, fetchStub(404, { error: 'article not found' }))
		);

		expect(res.status).toBe(404);
		const body = await res.json();
		expect(body.error).toBe('記事が見つかりません');
	});
});
