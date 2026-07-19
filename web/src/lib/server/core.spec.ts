import { describe, expect, it } from 'vitest';
import {
	deleteFeed,
	fetchFullText,
	getArticle,
	getSummary,
	getTags,
	listArticlesPage,
	listFeeds,
	markArticleRead,
	registerFeed,
	summarizeArticle,
	summarizeArticleStream,
	tagArticle
} from './core';

const article = {
	id: 7,
	feed_id: 1,
	guid: 'urn:x:7',
	url: 'https://example.com/7',
	title: 'Seven',
	content: 'body',
	published_at: '2026-07-01T09:00:00Z',
	created_at: '2026-07-01T09:00:00Z',
	feed_title: 'Example',
	read: false
};

const feed = {
	id: 1,
	url: 'https://example.com/feed.xml',
	title: 'Example',
	created_at: '2026-07-01T09:00:00Z'
};

function fetchStub(status: number, body: unknown): typeof fetch {
	return async () =>
		new Response(JSON.stringify(body), {
			status,
			headers: { 'Content-Type': 'application/json' }
		});
}

describe('listArticlesPage', () => {
	it('parses the articles envelope and exposes the next cursor (pagination contract)', async () => {
		const got = await listArticlesPage(fetchStub(200, { articles: [article], next_cursor: 'abc' }));
		expect(got.articles).toHaveLength(1);
		expect(got.articles[0]?.title).toBe('Seven');
		expect(got.nextCursor).toBe('abc');
	});

	it('reports a null next cursor at the end of the list', async () => {
		const got = await listArticlesPage(fetchStub(200, { articles: [article], next_cursor: null }));
		expect(got.nextCursor).toBeNull();
	});

	it('requests the given limit and, when provided, the cursor', async () => {
		let requestedUrl = '';
		const fetchFn: typeof fetch = async (input) => {
			requestedUrl = String(input);
			return new Response(JSON.stringify({ articles: [], next_cursor: null }), {
				status: 200,
				headers: { 'Content-Type': 'application/json' }
			});
		};

		await listArticlesPage(fetchFn, 20, 'my-cursor');

		expect(requestedUrl).toContain('limit=20');
		expect(requestedUrl).toContain('cursor=my-cursor');
	});

	it('omits the cursor param when none is given (first page)', async () => {
		let requestedUrl = '';
		const fetchFn: typeof fetch = async (input) => {
			requestedUrl = String(input);
			return new Response(JSON.stringify({ articles: [], next_cursor: null }), {
				status: 200,
				headers: { 'Content-Type': 'application/json' }
			});
		};

		await listArticlesPage(fetchFn, 20);

		expect(requestedUrl).not.toContain('cursor=');
	});

	it('rejects a response that does not match the schema', async () => {
		await expect(
			listArticlesPage(fetchStub(200, { articles: [{ id: 'x' }], next_cursor: null }))
		).rejects.toThrow();
	});

	it('rejects a response missing next_cursor (contract drift guard)', async () => {
		await expect(listArticlesPage(fetchStub(200, { articles: [article] }))).rejects.toThrow();
	});

	it('rejects a response missing the read/feed_title fields (contract drift guard)', async () => {
		const legacyArticle: Partial<typeof article> = { ...article };
		delete legacyArticle.feed_title;
		delete legacyArticle.read;
		await expect(
			listArticlesPage(fetchStub(200, { articles: [legacyArticle], next_cursor: null }))
		).rejects.toThrow();
	});

	it('rejects on a non-200 status', async () => {
		await expect(listArticlesPage(fetchStub(500, { error: 'internal error' }))).rejects.toThrow();
	});
});

describe('getArticle', () => {
	it('parses the article envelope', async () => {
		const got = await getArticle(fetchStub(200, { article }), 7);
		expect(got?.guid).toBe('urn:x:7');
	});

	it('returns null when the article does not exist', async () => {
		const got = await getArticle(fetchStub(404, { error: 'article not found' }), 999);
		expect(got).toBeNull();
	});

	it('rejects on other errors', async () => {
		await expect(getArticle(fetchStub(500, { error: 'internal error' }), 7)).rejects.toThrow();
	});
});

describe('listFeeds', () => {
	it('parses the feeds envelope', async () => {
		const got = await listFeeds(fetchStub(200, { feeds: [feed] }));
		expect(got[0]?.title).toBe('Example');
	});
});

describe('registerFeed', () => {
	it('maps 201 to a created result', async () => {
		const got = await registerFeed(
			fetchStub(201, { feed, inserted_articles: 3 }),
			'https://example.com/feed.xml'
		);
		expect(got).toEqual({ ok: true, created: true, feed: expect.anything(), insertedArticles: 3 });
	});

	it('maps 200 to an already-registered result', async () => {
		const got = await registerFeed(
			fetchStub(200, { feed, inserted_articles: 0 }),
			'https://example.com/feed.xml'
		);
		expect(got).toMatchObject({ ok: true, created: false, insertedArticles: 0 });
	});

	it.each([
		[400, 'URL が正しくありません'],
		[422, 'この URL はフィードではないようです'],
		[502, 'フィードの取得に失敗しました。時間をおいて再試行してください'],
		[500, '登録に失敗しました。再試行してください']
	])('maps %i to a quiet fact-plus-next-step message', async (status, message) => {
		const got = await registerFeed(fetchStub(status, { error: 'x' }), 'https://example.com/f');
		expect(got).toEqual({ ok: false, status, message });
	});
});

describe('markArticleRead', () => {
	function statusStub(status: number, capture?: { url?: string; method?: string }): typeof fetch {
		return async (input, init) => {
			if (capture) {
				capture.url = String(input);
				capture.method = init?.method ?? '';
			}
			return new Response(null, { status });
		};
	}

	it('POSTs the read stamp and reports 204 as success (idempotent)', async () => {
		const capture: { url?: string; method?: string } = {};
		const ok = await markArticleRead(statusStub(204, capture), 7);

		expect(ok).toBe(true);
		expect(capture.url).toMatch(/\/api\/v1\/articles\/7\/read$/);
		expect(capture.method).toBe('POST');
	});

	it('reports any other status as failure (the caller stays silent — fail-soft)', async () => {
		expect(await markArticleRead(statusStub(404), 999)).toBe(false);
		expect(await markArticleRead(statusStub(500), 7)).toBe(false);
	});
});

describe('deleteFeed', () => {
	function statusStub(status: number, capture?: { url?: string; method?: string }): typeof fetch {
		return async (input, init) => {
			if (capture) {
				capture.url = String(input);
				capture.method = init?.method ?? '';
			}
			return new Response(null, { status });
		};
	}

	it('DELETEs the feed and maps 204 to ok', async () => {
		const capture: { url?: string; method?: string } = {};
		const got = await deleteFeed(statusStub(204, capture), 1);

		expect(got).toEqual({ ok: true });
		expect(capture.url).toMatch(/\/api\/v1\/feeds\/1$/);
		expect(capture.method).toBe('DELETE');
	});

	it.each([
		[404, 'フィードが見つかりません。再読み込みしてください'],
		[500, '削除に失敗しました。再試行してください'],
		[502, '削除に失敗しました。再試行してください']
	])('maps %i to a quiet fact-plus-next-step message', async (status, message) => {
		const got = await deleteFeed(statusStub(status), 1);
		expect(got).toEqual({ ok: false, status, message });
	});
});

describe('fetchFullText', () => {
	const fulltext = { article_id: 7, text: '全文', fetched_at: '2026-07-01T09:00:00Z' };

	it('maps 201 to a created result', async () => {
		const got = await fetchFullText(fetchStub(201, { fulltext }), 7);
		expect(got).toEqual({ ok: true, created: true, fullText: fulltext });
	});

	it('maps 200 to an already-fetched result', async () => {
		const got = await fetchFullText(fetchStub(200, { fulltext }), 7);
		expect(got).toMatchObject({ ok: true, created: false });
	});

	it.each([
		[400, 'URL が正しくありません'],
		[404, '記事が見つかりません'],
		[422, '本文を取り出せませんでした'],
		[502, '取り寄せに失敗しました。時間をおいて再試行してください'],
		[500, '取り寄せに失敗しました。再試行してください']
	])('maps %i to a quiet fact-plus-next-step message', async (status, message) => {
		const got = await fetchFullText(fetchStub(status, { error: 'x' }), 7);
		expect(got).toEqual({ ok: false, status, message });
	});
});

describe('summarizeArticle', () => {
	const summary = {
		article_id: 7,
		text: '要約テキスト',
		model_meta: { model: 'unsloth/Qwen3.5-4B-GGUF:Q4_K_M' },
		created_at: '2026-07-01T09:00:00Z'
	};

	it('maps 201 to a created result', async () => {
		const got = await summarizeArticle(fetchStub(201, { summary }), 7);
		expect(got).toEqual({ ok: true, created: true, summary });
	});

	it('maps 200 to an already-summarized result', async () => {
		const got = await summarizeArticle(fetchStub(200, { summary }), 7);
		expect(got).toMatchObject({ ok: true, created: false });
	});

	it.each([
		[400, 'この記事は要約できません'],
		[404, '記事が見つかりません'],
		[422, '要約の生成に失敗しました。再試行してください'],
		[502, '要約に失敗しました。時間をおいて再試行してください'],
		[500, '要約に失敗しました。再試行してください']
	])('maps %i to a quiet fact-plus-next-step message', async (status, message) => {
		const got = await summarizeArticle(fetchStub(status, { error: 'x' }), 7);
		expect(got).toEqual({ ok: false, status, message });
	});
});

describe('getSummary', () => {
	const summary = {
		article_id: 7,
		text: '保存済みの要約',
		model_meta: { model: 'unsloth/Qwen3.5-4B-GGUF:Q4_K_M' },
		created_at: '2026-07-01T09:00:00Z'
	};

	it('parses the summary envelope when one exists (LLM is never called for reads)', async () => {
		const got = await getSummary(fetchStub(200, { summary }), 7);
		expect(got).toEqual(summary);
	});

	it('returns null when no summary exists yet (not-yet-enriched is not an error)', async () => {
		const got = await getSummary(fetchStub(404, { error: 'summary not found' }), 7);
		expect(got).toBeNull();
	});

	it('rejects on other errors', async () => {
		await expect(getSummary(fetchStub(500, { error: 'internal error' }), 7)).rejects.toThrow();
	});
});

describe('getTags', () => {
	it('parses the tags envelope when tags exist', async () => {
		const got = await getTags(fetchStub(200, { tags: ['タグ1', 'タグ2'] }), 7);
		expect(got).toEqual(['タグ1', 'タグ2']);
	});

	it('returns null when no tags exist yet (not-yet-enriched is not an error)', async () => {
		const got = await getTags(fetchStub(404, { error: 'tags not found' }), 7);
		expect(got).toBeNull();
	});

	it('rejects on other errors', async () => {
		await expect(getTags(fetchStub(500, { error: 'internal error' }), 7)).rejects.toThrow();
	});
});

describe('tagArticle', () => {
	it('maps 201 to a created result', async () => {
		const got = await tagArticle(fetchStub(201, { tags: ['タグ1'] }), 7);
		expect(got).toEqual({ ok: true, created: true, tags: ['タグ1'] });
	});

	it('maps 200 to an already-tagged result', async () => {
		const got = await tagArticle(fetchStub(200, { tags: ['タグ1'] }), 7);
		expect(got).toMatchObject({ ok: true, created: false });
	});

	it.each([
		[400, 'この記事にタグを付けられません'],
		[404, '記事が見つかりません'],
		[422, 'タグの抽出に失敗しました。再試行してください'],
		[502, 'タグの抽出に失敗しました。時間をおいて再試行してください'],
		[500, 'タグの抽出に失敗しました。再試行してください']
	])('maps %i to a quiet fact-plus-next-step message', async (status, message) => {
		const got = await tagArticle(fetchStub(status, { error: 'x' }), 7);
		expect(got).toEqual({ ok: false, status, message });
	});
});

describe('summarizeArticleStream', () => {
	it('returns the raw upstream response without parsing it (pass-through)', async () => {
		let gotUrl = '';
		let gotMethod = '';
		const fetchFn: typeof fetch = async (input, init) => {
			gotUrl = String(input);
			gotMethod = init?.method ?? '';
			return new Response('event: delta\ndata: {"text":"要約"}\n\n', {
				status: 200,
				headers: { 'Content-Type': 'text/event-stream' }
			});
		};

		const res = await summarizeArticleStream(fetchFn, 7);

		expect(gotMethod).toBe('POST');
		expect(gotUrl).toMatch(/\/api\/v1\/articles\/7\/summary\/stream$/);
		expect(res.status).toBe(200);
		expect(await res.text()).toBe('event: delta\ndata: {"text":"要約"}\n\n');
	});
});
