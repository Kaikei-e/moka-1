// ハイブリッド検索のBFF窓口(サイドバーの検索入力 → moka-core GET /api/v1/search)。
// 中継の配線だけを検証する — 検索契約そのもの(記事表現 + score)は core.spec.ts が守る。
import { describe, expect, it } from 'vitest';
import { GET } from './+server';

type GetEvent = Parameters<typeof GET>[0];

function requestEvent(search: string, fetchFn: typeof fetch): GetEvent {
	return {
		fetch: fetchFn,
		url: new URL(`http://localhost/search${search}`)
	} as unknown as GetEvent;
}

function fetchStub(status: number, body: unknown): typeof fetch {
	return async () =>
		new Response(JSON.stringify(body), {
			status,
			headers: { 'Content-Type': 'application/json' }
		});
}

const result = {
	id: 7,
	feed_id: 1,
	guid: 'urn:x:7',
	url: 'https://example.com/7',
	title: 'Seven',
	content: 'body',
	published_at: '2026-07-01T09:00:00Z',
	created_at: '2026-07-01T09:00:00Z',
	feed_title: 'Example',
	read: false,
	score: 0.42
};

describe('GET /search (hybrid search BFF)', () => {
	it('forwards the query to moka-core with a fixed limit and relays the items', async () => {
		let requestedUrl = '';
		const fetchFn: typeof fetch = async (input) => {
			requestedUrl = String(input);
			return new Response(JSON.stringify({ items: [result] }), {
				status: 200,
				headers: { 'Content-Type': 'application/json' }
			});
		};

		const res = await GET(requestEvent('?q=svelte', fetchFn));
		const body = await res.json();

		expect(requestedUrl).toContain('/api/v1/search');
		expect(requestedUrl).toContain('q=svelte');
		expect(requestedUrl).toContain('limit=20');
		expect(body.items).toHaveLength(1);
		expect(body.items[0].title).toBe('Seven');
		expect(body.items[0].score).toBe(0.42);
	});

	it('returns an empty result set for a blank query without calling moka-core (core側では q 空は 400)', async () => {
		let called = false;
		const fetchFn: typeof fetch = async () => {
			called = true;
			return new Response('{}', { status: 200 });
		};

		const res = await GET(requestEvent('?q=%20%20', fetchFn));
		const body = await res.json();

		expect(called).toBe(false);
		expect(body.items).toEqual([]);
	});

	it('maps an upstream failure to a quiet fact-plus-next-step error (§7.2)', async () => {
		const res = await GET(requestEvent('?q=svelte', fetchStub(500, { error: 'internal error' })));

		expect(res.status).toBe(502);
		const body = await res.json();
		expect(body.error).toBe('探せませんでした。再試行してください');
	});
});
