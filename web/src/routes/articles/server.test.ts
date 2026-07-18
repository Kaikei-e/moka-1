// サイドバー無限スクロールのクライアント起点ページ取得(BFF窓口)。中継の配線だけを検証する
// — moka-core とのプロトコル詳細(cursorページングの契約自体)は core.spec.ts が守る。
import { describe, expect, it } from 'vitest';
import { GET } from './+server';

type GetEvent = Parameters<typeof GET>[0];

function requestEvent(search: string, fetchFn: typeof fetch): GetEvent {
	return {
		fetch: fetchFn,
		url: new URL(`http://localhost/articles${search}`)
	} as unknown as GetEvent;
}

function fetchStub(status: number, body: unknown): typeof fetch {
	return async () =>
		new Response(JSON.stringify(body), {
			status,
			headers: { 'Content-Type': 'application/json' }
		});
}

describe('GET /articles (infinite scroll BFF)', () => {
	it('forwards the cursor to moka-core with a fixed page size and relays the page', async () => {
		let requestedUrl = '';
		const fetchFn: typeof fetch = async (input) => {
			requestedUrl = String(input);
			return new Response(
				JSON.stringify({
					articles: [
						{
							id: 1,
							feed_id: 1,
							guid: 'g',
							url: 'u',
							title: 't',
							content: '',
							published_at: null,
							created_at: '2026-07-01T00:00:00Z',
							feed_title: 'Example',
							read: false
						}
					],
					next_cursor: 'xyz'
				}),
				{ status: 200, headers: { 'Content-Type': 'application/json' } }
			);
		};

		const res = await GET(requestEvent('?cursor=abc', fetchFn));
		const body = await res.json();

		expect(requestedUrl).toContain('limit=20');
		expect(requestedUrl).toContain('cursor=abc');
		expect(body.articles).toHaveLength(1);
		expect(body.next_cursor).toBe('xyz');
	});

	it('requests the first page when no cursor is given', async () => {
		let requestedUrl = '';
		const fetchFn: typeof fetch = async (input) => {
			requestedUrl = String(input);
			return new Response(JSON.stringify({ articles: [], next_cursor: null }), {
				status: 200,
				headers: { 'Content-Type': 'application/json' }
			});
		};

		await GET(requestEvent('', fetchFn));

		expect(requestedUrl).not.toContain('cursor=');
	});

	it('maps an upstream failure to a quiet fact-plus-next-step error (§7.2)', async () => {
		const res = await GET(requestEvent('?cursor=abc', fetchStub(500, { error: 'internal error' })));

		expect(res.status).toBe(502);
		const body = await res.json();
		expect(body.error).toBe('続きを読み込めませんでした');
	});
});
