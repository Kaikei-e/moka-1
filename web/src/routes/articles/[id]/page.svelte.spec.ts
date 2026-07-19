import { page } from 'vitest/browser';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import Page from './+page.svelte';

const article = {
	id: 7,
	feed_id: 1,
	guid: 'urn:x:7',
	url: 'https://example.com/articles/7',
	title: 'Seven',
	content: '<p>概要の本文です。</p>',
	published_at: '2026-07-01T09:00:00Z',
	created_at: '2026-07-01T09:00:00Z',
	feed_title: 'Example Feed',
	read: false
};

const otherArticle = {
	id: 8,
	feed_id: 1,
	guid: 'urn:x:8',
	url: 'https://example.com/articles/8',
	title: 'Eight',
	content: '<p>別記事の概要です。</p>',
	published_at: '2026-07-02T09:00:00Z',
	created_at: '2026-07-02T09:00:00Z',
	feed_title: 'Example Feed',
	read: false
};

// layout data(サイドバーの記事一覧)も PageProps.data に合流するのでテストにも含める
const pageData = { articles: [], nextCursor: null, listUnavailable: false, article };
const otherPageData = {
	articles: [],
	nextCursor: null,
	listUnavailable: false,
	article: otherArticle
};

function jsonResponse(status: number, body: unknown) {
	return new Response(JSON.stringify(body), {
		status,
		headers: { 'Content-Type': 'application/json' }
	});
}

function deferredResponse(status: number, body: unknown) {
	let resolve!: () => void;
	const gate = new Promise<void>((r) => (resolve = r));
	const promise = gate.then(() => jsonResponse(status, body));
	return { promise, resolve };
}

type SSEEvent = { event: string; data: unknown };

function sseResponse(status: number, events: SSEEvent[]) {
	const text = events.map((e) => `event: ${e.event}\ndata: ${JSON.stringify(e.data)}\n\n`).join('');
	return new Response(text, { status, headers: { 'Content-Type': 'text/event-stream' } });
}

// 全文取り寄せ・要約/タグ抽出は明示ボタンが引き金。マウント時の GET(/summary・/tags)は
// 濃縮済み確認だけで LLM は呼ばない。既読打刻(/read)は開いた瞬間に fire-and-forget。
// 同じ Page 内で全部を検証できるよう、呼び先を URL + method で振り分ける。
function routeFetch(
	overrides: Partial<{
		fulltext: () => Promise<Response>;
		summary: () => Promise<Response>;
		summaryGet: () => Promise<Response>;
		tags: () => Promise<Response>;
		tagsGet: () => Promise<Response>;
		read: () => Promise<Response>;
	}> = {}
) {
	const fulltext =
		overrides.fulltext ?? (() => Promise.reject(new Error('unmocked fulltext fetch')));
	const summary = overrides.summary ?? (() => Promise.reject(new Error('unmocked summary fetch')));
	const summaryGet =
		overrides.summaryGet ?? (() => jsonResponse(404, { error: 'summary not found' }));
	const tags = overrides.tags ?? (() => Promise.reject(new Error('unmocked tags fetch')));
	const tagsGet = overrides.tagsGet ?? (() => jsonResponse(404, { error: 'tags not found' }));
	const read = overrides.read ?? (() => Promise.resolve(new Response(null, { status: 204 })));
	return vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
		const url = typeof input === 'string' ? input : input.toString();
		const method = (
			init?.method ??
			(typeof input !== 'string' && 'method' in input ? input.method : undefined) ??
			'GET'
		).toUpperCase();
		if (url.includes('/fulltext')) return fulltext();
		if (url.includes('/tags')) return method === 'GET' ? tagsGet() : tags();
		if (url.includes('/summary')) return method === 'GET' ? summaryGet() : summary();
		if (url.includes('/read')) return read();
		return Promise.reject(new Error(`unmocked fetch: ${url}`));
	});
}

afterEach(() => {
	vi.unstubAllGlobals();
});

describe('articles/[id] reading view — 全文取り寄せ', () => {
	it('shows the RSS-derived content and a fetch button before any retrieval', async () => {
		vi.stubGlobal('fetch', routeFetch());
		render(Page, { data: pageData });

		await expect.element(page.getByText('概要の本文です。')).toBeVisible();
		await expect.element(page.getByRole('button', { name: '全文を取り寄せる' })).toBeVisible();
	});

	it('opens the original link in a new tab', async () => {
		vi.stubGlobal('fetch', routeFetch());
		render(Page, { data: pageData });

		const link = page.getByRole('link', { name: '原文を開く' });
		await expect.element(link).toHaveAttribute('target', '_blank');
		await expect.element(link).toHaveAttribute('rel', 'noopener noreferrer');
	});

	it('shows a drip while pending, then replaces the body with the fetched full text', async () => {
		const { promise, resolve } = deferredResponse(201, {
			fulltext: {
				article_id: 7,
				text: '<p>取り寄せた全文の段落。</p>',
				fetched_at: '2026-07-01T10:00:00Z'
			}
		});
		vi.stubGlobal('fetch', routeFetch({ fulltext: () => promise }));

		render(Page, { data: pageData });
		await page.getByRole('button', { name: '全文を取り寄せる' }).click();

		await expect.element(page.getByTestId('fulltext-drip')).toBeVisible();

		resolve();

		await expect.element(page.getByText('取り寄せた全文の段落。')).toBeVisible();
		await expect.element(page.getByText('概要の本文です。')).not.toBeInTheDocument();
		await expect
			.element(page.getByRole('button', { name: '全文を取り寄せる' }))
			.not.toBeInTheDocument();
	});

	it('shows an inline failure block and keeps the retry button on error', async () => {
		vi.stubGlobal(
			'fetch',
			routeFetch({
				fulltext: async () =>
					jsonResponse(502, { error: '取り寄せに失敗しました。時間をおいて再試行してください' })
			})
		);

		render(Page, { data: pageData });
		await page.getByRole('button', { name: '全文を取り寄せる' }).click();

		const errorBlock = page.getByRole('alert');
		await expect.element(errorBlock).toHaveTextContent('失敗:');
		await expect.element(errorBlock).toHaveTextContent('時間をおいて再試行してください');
		await expect.element(page.getByRole('button', { name: '全文を取り寄せる' })).toBeVisible();
	});

	it('switches to the new article body when navigating between articles (SvelteKit reuses the component instance)', async () => {
		vi.stubGlobal('fetch', routeFetch());
		const { rerender } = await render(Page, { data: pageData });
		await expect.element(page.getByText('概要の本文です。')).toBeVisible();

		await rerender({ data: otherPageData });

		await expect.element(page.getByRole('heading', { name: 'Eight' })).toBeVisible();
		await expect.element(page.getByText('別記事の概要です。')).toBeVisible();
		await expect.element(page.getByText('概要の本文です。')).not.toBeInTheDocument();
	});

	it('resets a previously fetched full text when navigating to a different article', async () => {
		const { promise, resolve } = deferredResponse(201, {
			fulltext: {
				article_id: 7,
				text: '<p>取り寄せた全文の段落。</p>',
				fetched_at: '2026-07-01T10:00:00Z'
			}
		});
		vi.stubGlobal('fetch', routeFetch({ fulltext: () => promise }));

		const { rerender } = await render(Page, { data: pageData });
		await page.getByRole('button', { name: '全文を取り寄せる' }).click();
		resolve();
		await expect.element(page.getByText('取り寄せた全文の段落。')).toBeVisible();

		await rerender({ data: otherPageData });

		await expect.element(page.getByRole('heading', { name: 'Eight' })).toBeVisible();
		await expect.element(page.getByText('別記事の概要です。')).toBeVisible();
		await expect.element(page.getByText('取り寄せた全文の段落。')).not.toBeInTheDocument();
		await expect.element(page.getByRole('button', { name: '全文を取り寄せる' })).toBeVisible();
	});

	it('ignores an in-flight fulltext response that resolves after navigating to a different article', async () => {
		const { promise, resolve } = deferredResponse(201, {
			fulltext: {
				article_id: 7,
				text: '<p>記事7の取り寄せ全文。</p>',
				fetched_at: '2026-07-01T10:00:00Z'
			}
		});
		vi.stubGlobal('fetch', routeFetch({ fulltext: () => promise }));

		const { rerender } = await render(Page, { data: pageData });
		await page.getByRole('button', { name: '全文を取り寄せる' }).click();
		await expect.element(page.getByTestId('fulltext-drip')).toBeVisible();

		await rerender({ data: otherPageData });
		resolve();
		await promise;
		// 遅延した応答の処理(json 解析 → 状態反映)が済むのを待ってから副作用の不在を確かめる
		await new Promise((r) => setTimeout(r, 20));

		await expect.element(page.getByText('記事7の取り寄せ全文。')).not.toBeInTheDocument();
		await expect.element(page.getByText('別記事の概要です。')).toBeVisible();
		await expect.element(page.getByRole('button', { name: '全文を取り寄せる' })).toBeVisible();
		await expect.element(page.getByTestId('fulltext-drip')).not.toBeInTheDocument();
	});

	it('does not render the original link when the article URL is not http(s)', async () => {
		vi.stubGlobal('fetch', routeFetch());
		render(Page, {
			data: { ...pageData, article: { ...article, url: 'javascript:alert(1)' } }
		});

		await expect.element(page.getByRole('heading', { name: 'Seven' })).toBeVisible();
		expect(page.getByRole('link', { name: '原文を開く' }).elements()).toHaveLength(0);
	});

	it('renders the fetched full text as real structure — heading, list and code block', async () => {
		const { promise, resolve } = deferredResponse(201, {
			fulltext: {
				article_id: 7,
				text: '<h2>できたものはこちら</h2><p>本文。</p><ul><li>一</li><li>二</li></ul><pre><code>const x = 1;</code></pre>',
				fetched_at: '2026-07-01T10:00:00Z'
			}
		});
		vi.stubGlobal('fetch', routeFetch({ fulltext: () => promise }));

		render(Page, { data: pageData });
		await page.getByRole('button', { name: '全文を取り寄せる' }).click();
		resolve();

		await expect.element(page.getByRole('heading', { name: 'できたものはこちら' })).toBeVisible();
		await expect.element(page.getByRole('list')).toBeVisible();
		await expect.element(page.getByText('一')).toBeVisible();
		await expect.element(page.getByText('const x = 1;')).toBeVisible();
	});

	it('sanitizes dangerous markup out of the fetched full text', async () => {
		const { promise, resolve } = deferredResponse(201, {
			fulltext: {
				article_id: 7,
				text: '<p onclick="alert(1)">安全な本文</p><script>alert(1)</script><a href="javascript:alert(1)">link</a>',
				fetched_at: '2026-07-01T10:00:00Z'
			}
		});
		vi.stubGlobal('fetch', routeFetch({ fulltext: () => promise }));

		const { container } = render(Page, { data: pageData });
		await page.getByRole('button', { name: '全文を取り寄せる' }).click();
		resolve();

		await expect.element(page.getByText('安全な本文')).toBeVisible();
		expect(container.querySelector('script')).toBeNull();
		expect(container.querySelector('[onclick]')).toBeNull();
	});
});

describe('articles/[id] reading view — 要約(moka による濃縮)', () => {
	it('shows a summarize button (no auto-fetch) that fetches independently of the fulltext button', async () => {
		const summaryFetch = vi.fn(async () =>
			sseResponse(200, [
				{ event: 'delta', data: { text: '読書ビューに運ばれてきた要約' } },
				{
					event: 'done',
					data: {
						summary: {
							article_id: 7,
							text: '読書ビューに運ばれてきた要約',
							model_meta: {},
							created_at: '2026-07-01T09:00:00Z'
						},
						created: true
					}
				}
			])
		);
		vi.stubGlobal('fetch', routeFetch({ summary: summaryFetch }));

		render(Page, { data: pageData });

		await expect.element(page.getByText('moka による要約')).toBeVisible();
		await expect.element(page.getByRole('button', { name: '要約する' })).toBeVisible();
		// マウント時の GET 確認は summaryGet 側。ストリーム(POST)はボタン押下まで飛ばない。
		expect(summaryFetch).not.toHaveBeenCalled();

		await page.getByRole('button', { name: '要約する' }).click();

		await expect
			.element(page.getByTestId('summary-text'))
			.toHaveTextContent('読書ビューに運ばれてきた要約');
	});
});

describe('articles/[id] reading view — 既読打刻(fire-and-forget)', () => {
	it('stamps the article as read when the reading view opens', async () => {
		const readFetch = vi.fn(async () => new Response(null, { status: 204 }));
		vi.stubGlobal('fetch', routeFetch({ read: readFetch }));

		render(Page, { data: pageData });

		await expect.element(page.getByRole('heading', { name: 'Seven' })).toBeVisible();
		await vi.waitFor(() => expect(readFetch).toHaveBeenCalledTimes(1));
	});

	it('stamps the new article when navigating between articles', async () => {
		const readUrls: string[] = [];
		const fetchMock = routeFetch();
		vi.stubGlobal(
			'fetch',
			vi.fn((input: RequestInfo | URL) => {
				const url = typeof input === 'string' ? input : input.toString();
				if (url.includes('/read')) readUrls.push(url);
				return fetchMock(input);
			})
		);

		const { rerender } = await render(Page, { data: pageData });
		await vi.waitFor(() => expect(readUrls).toContain('/articles/7/read'));

		await rerender({ data: otherPageData });

		await vi.waitFor(() => expect(readUrls).toContain('/articles/8/read'));
	});

	it('stays silent when the stamp fails — reading is never disturbed (fail-soft)', async () => {
		vi.stubGlobal('fetch', routeFetch({ read: () => Promise.reject(new Error('network down')) }));

		render(Page, { data: pageData });

		await expect.element(page.getByText('概要の本文です。')).toBeVisible();
		// エラー表示は一切出ない(role=alert が存在しない)
		expect(page.getByRole('alert').elements()).toHaveLength(0);
	});
});

describe('articles/[id] reading view — 取り下げ中の UI(対訳・訊く)', () => {
	it('renders neither the 対訳 toggle nor the ask bar until the LLM backend exists', async () => {
		vi.stubGlobal('fetch', routeFetch());
		render(Page, { data: pageData });

		await expect.element(page.getByRole('heading', { name: 'Seven' })).toBeVisible();
		expect(page.getByRole('button', { name: '対訳' }).elements()).toHaveLength(0);
		expect(page.getByPlaceholder('この記事について訊く…').elements()).toHaveLength(0);
	});
});
