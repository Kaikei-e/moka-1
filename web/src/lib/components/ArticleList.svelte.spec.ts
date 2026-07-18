import { page } from 'vitest/browser';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import ArticleList from './ArticleList.svelte';
import { EMPTY_ARTICLES, LOADING_MORE, LOAD_MORE_FAILED, RETRY_LOAD_MORE } from '$lib/copy';
import type { Article } from '$lib/api/schemas';

// 実ブラウザ環境(vitest-browser-svelte)では IntersectionObserver は本物だが、
// レイアウト依存(ビューポート内かどうか)にテストを晒すとフレーキーになるため
// フェイクに差し替えて発火タイミングを手で制御する。
class FakeIntersectionObserver {
	static instances: FakeIntersectionObserver[] = [];
	callback: IntersectionObserverCallback;
	observe = vi.fn();
	disconnect = vi.fn();
	unobserve = vi.fn();

	constructor(callback: IntersectionObserverCallback) {
		this.callback = callback;
		FakeIntersectionObserver.instances.push(this);
	}

	trigger(isIntersecting = true) {
		this.callback(
			[{ isIntersecting } as IntersectionObserverEntry],
			this as unknown as IntersectionObserver
		);
	}
}

function latestObserver(): FakeIntersectionObserver {
	const observer = FakeIntersectionObserver.instances.at(-1);
	if (!observer) throw new Error('no IntersectionObserver was constructed');
	return observer;
}

function jsonResponse(status: number, body: unknown) {
	return new Response(JSON.stringify(body), {
		status,
		headers: { 'Content-Type': 'application/json' }
	});
}

function nextArticle(id: number, title: string, overrides: Partial<Article> = {}): Article {
	return {
		id,
		feed_id: 1,
		guid: `urn:x:${id}`,
		url: `https://example.com/${id}`,
		title,
		content: '',
		published_at: '2026-06-30T09:00:00Z',
		created_at: '2026-06-30T09:00:00Z',
		feed_title: 'Example Feed',
		read: false,
		...overrides
	};
}

const articles: Article[] = [
	nextArticle(2, 'Newest', {
		published_at: '2026-07-02T09:00:00Z',
		created_at: '2026-07-02T09:00:00Z'
	}),
	nextArticle(1, 'Older', {
		published_at: '2026-07-01T09:00:00Z',
		created_at: '2026-07-01T09:00:00Z'
	})
];

describe('ArticleList.svelte', () => {
	it('links each article to its reading view', async () => {
		render(ArticleList, { articles, currentId: null });

		const link = page.getByRole('link', { name: /Newest/ });
		await expect.element(link).toHaveAttribute('href', '/articles/2');
	});

	it('marks the article being read with aria-current', async () => {
		render(ArticleList, { articles, currentId: 1 });

		await expect
			.element(page.getByRole('link', { name: /Older/ }))
			.toHaveAttribute('aria-current', 'page');
	});

	it('invites instead of apologizing when there are no articles', async () => {
		render(ArticleList, { articles: [], currentId: null });

		await expect.element(page.getByText(EMPTY_ARTICLES)).toBeInTheDocument();
	});
});

describe('ArticleList.svelte — リスト行の手がかり(フィード名・相対日時・既読の沈み)', () => {
	it('shows the feed name and a relative time as one quiet meta line', async () => {
		const fresh = nextArticle(3, 'Fresh', {
			published_at: new Date(Date.now() - 5 * 60_000).toISOString()
		});
		render(ArticleList, { articles: [fresh], currentId: null });

		await expect.element(page.getByText('Example Feed・5分前')).toBeVisible();
	});

	it('falls back to the article hostname when the feed has no title', async () => {
		const untitled = nextArticle(3, 'Untitled feed article', {
			feed_title: null,
			url: 'https://blog.example.com/entry/3',
			published_at: new Date(Date.now() - 5 * 60_000).toISOString()
		});
		render(ArticleList, { articles: [untitled], currentId: null });

		await expect.element(page.getByText('blog.example.com・5分前')).toBeVisible();
	});

	it('sinks read articles (data-read) and keeps unread ones prominent — no counts, no badges', async () => {
		const mixed = [nextArticle(2, 'Read one', { read: true }), nextArticle(1, 'Unread one')];
		render(ArticleList, { articles: mixed, currentId: null });

		await expect
			.element(page.getByRole('link', { name: /Read one/ }))
			.toHaveAttribute('data-read', 'true');
		await expect
			.element(page.getByRole('link', { name: /Unread one/ }))
			.not.toHaveAttribute('data-read');
	});

	it('optimistically sinks the article being read without waiting for the server stamp', async () => {
		const { rerender } = render(ArticleList, { articles, currentId: null });
		await expect
			.element(page.getByRole('link', { name: /Older/ }))
			.not.toHaveAttribute('data-read');

		await rerender({ articles, currentId: 1 });

		await expect
			.element(page.getByRole('link', { name: /Older/ }))
			.toHaveAttribute('data-read', 'true');
		// 別の記事へ移っても、一度読んだ事実は沈んだまま
		await rerender({ articles, currentId: 2 });
		await expect
			.element(page.getByRole('link', { name: /Older/ }))
			.toHaveAttribute('data-read', 'true');
	});
});

describe('ArticleList.svelte — infinite scroll', () => {
	afterEach(() => {
		vi.unstubAllGlobals();
		FakeIntersectionObserver.instances = [];
	});

	it('renders no sentinel when there is no next page', async () => {
		vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver);
		render(ArticleList, { articles, nextCursor: null, currentId: null });

		await expect.element(page.getByTestId('article-list-sentinel')).not.toBeInTheDocument();
		expect(FakeIntersectionObserver.instances).toHaveLength(0);
	});

	it('requests the next page and appends it once the sentinel becomes visible', async () => {
		const fetchMock = vi.fn(() =>
			Promise.resolve(
				jsonResponse(200, { articles: [nextArticle(3, 'Fetched next')], next_cursor: null })
			)
		);
		vi.stubGlobal('fetch', fetchMock);
		vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver);

		render(ArticleList, { articles, nextCursor: 'cursor-1', currentId: null });
		latestObserver().trigger();

		await expect.element(page.getByRole('link', { name: /Fetched next/ })).toBeVisible();
		expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining('cursor=cursor-1'));
	});

	it('shows the loading drip while the next page is in flight', async () => {
		let resolveFetch!: (res: Response) => void;
		const pending = new Promise<Response>((r) => (resolveFetch = r));
		vi.stubGlobal(
			'fetch',
			vi.fn(() => pending)
		);
		vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver);

		render(ArticleList, { articles, nextCursor: 'cursor-1', currentId: null });
		latestObserver().trigger();

		await expect.element(page.getByText(LOADING_MORE)).toBeVisible();

		resolveFetch(jsonResponse(200, { articles: [], next_cursor: null }));

		await expect.element(page.getByText(LOADING_MORE)).not.toBeInTheDocument();
	});

	it('stops observing once the list reaches its end (next_cursor: null)', async () => {
		vi.stubGlobal(
			'fetch',
			vi.fn(() => Promise.resolve(jsonResponse(200, { articles: [], next_cursor: null })))
		);
		vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver);

		render(ArticleList, { articles, nextCursor: 'cursor-1', currentId: null });
		latestObserver().trigger();

		await expect.element(page.getByTestId('article-list-sentinel')).not.toBeInTheDocument();
	});

	it('shows a fact-plus-next-step failure with a retry button when the fetch fails (§7.2)', async () => {
		vi.stubGlobal(
			'fetch',
			vi.fn(() => Promise.reject(new Error('network down')))
		);
		vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver);

		render(ArticleList, { articles, nextCursor: 'cursor-1', currentId: null });
		latestObserver().trigger();

		await expect.element(page.getByText(LOAD_MORE_FAILED)).toBeVisible();
		await expect.element(page.getByRole('button', { name: RETRY_LOAD_MORE })).toBeVisible();
	});

	it('retries the same page on retry-button click after a failure', async () => {
		const fetchMock = vi
			.fn()
			.mockRejectedValueOnce(new Error('network down'))
			.mockResolvedValueOnce(
				jsonResponse(200, { articles: [nextArticle(3, 'Recovered article')], next_cursor: null })
			);
		vi.stubGlobal('fetch', fetchMock);
		vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver);

		render(ArticleList, { articles, nextCursor: 'cursor-1', currentId: null });
		latestObserver().trigger();
		await expect.element(page.getByText(LOAD_MORE_FAILED)).toBeVisible();

		await page.getByRole('button', { name: RETRY_LOAD_MORE }).click();

		await expect.element(page.getByRole('link', { name: /Recovered article/ })).toBeVisible();
		expect(fetchMock).toHaveBeenCalledTimes(2);
	});

	it('discards accumulated pages and starts over when the props change (e.g. a new feed was registered)', async () => {
		const fetchMock = vi.fn(() =>
			Promise.resolve(
				jsonResponse(200, { articles: [nextArticle(3, 'Loaded from page 2')], next_cursor: null })
			)
		);
		vi.stubGlobal('fetch', fetchMock);
		vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver);

		const { rerender } = render(ArticleList, {
			articles,
			nextCursor: 'cursor-1',
			currentId: null
		});
		latestObserver().trigger();
		await expect.element(page.getByRole('link', { name: /Loaded from page 2/ })).toBeVisible();

		const refreshedArticles = [nextArticle(9, 'Fresh SSR article')];
		await rerender({ articles: refreshedArticles, nextCursor: 'cursor-2', currentId: null });

		await expect
			.element(page.getByRole('link', { name: /Loaded from page 2/ }))
			.not.toBeInTheDocument();
		await expect.element(page.getByRole('link', { name: /Fresh SSR article/ })).toBeVisible();
		await expect.element(page.getByTestId('article-list-sentinel')).toBeInTheDocument();
	});

	it('drops an in-flight page when the props reset while it loads (stale page must not append)', async () => {
		let resolveFetch!: (res: Response) => void;
		const pending = new Promise<Response>((r) => (resolveFetch = r));
		const fetchMock = vi
			.fn<(input: string) => Promise<Response>>()
			.mockImplementationOnce(() => pending)
			.mockImplementation(() =>
				Promise.resolve(jsonResponse(200, { articles: [], next_cursor: null }))
			);
		vi.stubGlobal('fetch', fetchMock);
		vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver);

		const { rerender } = render(ArticleList, {
			articles,
			nextCursor: 'cursor-1',
			currentId: null
		});
		latestObserver().trigger();
		await expect.element(page.getByText(LOADING_MORE)).toBeVisible();

		// 読み込み中に SSR props がリセットされる(例: 新規フィード登録後の再読み込み)
		const refreshedArticles = [nextArticle(9, 'Fresh SSR article')];
		await rerender({ articles: refreshedArticles, nextCursor: 'cursor-2', currentId: null });

		resolveFetch(
			jsonResponse(200, {
				articles: [nextArticle(3, 'Stale page article')],
				next_cursor: 'cursor-stale'
			})
		);
		// 取り残された応答の処理が済むのを待ってから副作用の不在を確かめる
		await new Promise((r) => setTimeout(r, 20));

		expect(page.getByRole('link', { name: /Stale page article/ }).elements()).toHaveLength(0);
		await expect.element(page.getByRole('link', { name: /Fresh SSR article/ })).toBeVisible();

		// cursor も古い応答で上書きされない — 次の読み込みは新しい cursor から始まる
		latestObserver().trigger();
		await expect.element(page.getByText(LOADING_MORE)).not.toBeInTheDocument();
		expect(fetchMock).toHaveBeenCalledTimes(2);
		expect(fetchMock).toHaveBeenLastCalledWith(expect.stringContaining('cursor=cursor-2'));
	});
});
