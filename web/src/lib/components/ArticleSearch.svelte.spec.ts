import { page } from 'vitest/browser';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import ArticleSearch from './ArticleSearch.svelte';
import { SEARCHING, SEARCH_EMPTY, SEARCH_FAILED } from '$lib/copy';

function jsonResponse(status: number, body: unknown) {
	return new Response(JSON.stringify(body), {
		status,
		headers: { 'Content-Type': 'application/json' }
	});
}

const result = {
	id: 7,
	feed_id: 1,
	guid: 'urn:x:7',
	url: 'https://example.com/7',
	title: '瑠璃と金泥の記事',
	content: 'body',
	published_at: '2026-07-01T09:00:00Z',
	created_at: '2026-07-01T09:00:00Z',
	feed_title: 'Example',
	read: false,
	score: 0.42
};

function deferredResponse(status: number, body: unknown) {
	let resolve!: () => void;
	const gate = new Promise<void>((r) => (resolve = r));
	const promise = gate.then(() => jsonResponse(status, body));
	return { promise, resolve };
}

afterEach(() => {
	vi.unstubAllGlobals();
});

describe('ArticleSearch.svelte', () => {
	it('debounces the query, calls the search BFF and lists the results as article rows', async () => {
		const requestedUrls: string[] = [];
		const fetchMock = vi.fn((input: Parameters<typeof fetch>[0]) => {
			requestedUrls.push(String(input));
			return Promise.resolve(jsonResponse(200, { items: [result] }));
		});
		vi.stubGlobal('fetch', fetchMock);

		render(ArticleSearch, { currentId: null });

		await page.getByLabelText('記事を探す').fill('瑠璃');

		const row = page.getByRole('link', { name: /瑠璃と金泥の記事/ });
		await expect.element(row).toBeVisible();
		await expect.element(row).toHaveAttribute('href', '/articles/7');
		// リスト行と同じ手がかり(フィード名)がメタに載る
		await expect.element(page.getByText(/Example/)).toBeInTheDocument();

		expect(requestedUrls).toHaveLength(1);
		expect(requestedUrls[0]).toContain('/search?q=');
		expect(requestedUrls[0]).toContain(encodeURIComponent('瑠璃'));
	});

	it('shows the drip with the craft copy while the search is in flight', async () => {
		const { promise, resolve } = deferredResponse(200, { items: [result] });
		vi.stubGlobal(
			'fetch',
			vi.fn(() => promise)
		);

		render(ArticleSearch, { currentId: null });
		await page.getByLabelText('記事を探す').fill('瑠璃');

		await expect.element(page.getByTestId('search-drip')).toBeVisible();
		await expect.element(page.getByText(SEARCHING)).toBeInTheDocument();

		resolve();

		await expect.element(page.getByRole('link', { name: /瑠璃と金泥の記事/ })).toBeVisible();
		await expect.element(page.getByTestId('search-drip')).not.toBeInTheDocument();
	});

	it('shows a quiet empty message when nothing matches', async () => {
		vi.stubGlobal(
			'fetch',
			vi.fn(() => Promise.resolve(jsonResponse(200, { items: [] })))
		);

		render(ArticleSearch, { currentId: null });
		await page.getByLabelText('記事を探す').fill('存在しない言葉');

		await expect.element(page.getByText(SEARCH_EMPTY)).toBeInTheDocument();
	});

	it('returns to the normal list when the query is cleared (results region disappears)', async () => {
		vi.stubGlobal(
			'fetch',
			vi.fn(() => Promise.resolve(jsonResponse(200, { items: [result] })))
		);

		render(ArticleSearch, { currentId: null });
		const input = page.getByLabelText('記事を探す');
		await input.fill('瑠璃');
		await expect.element(page.getByRole('link', { name: /瑠璃と金泥の記事/ })).toBeVisible();

		await input.fill('');

		await expect
			.element(page.getByRole('link', { name: /瑠璃と金泥の記事/ }))
			.not.toBeInTheDocument();
		expect(page.getByText(SEARCH_EMPTY).elements()).toHaveLength(0);
	});

	it('shows a quiet failure message when the search BFF fails', async () => {
		vi.stubGlobal(
			'fetch',
			vi.fn(() => Promise.resolve(jsonResponse(502, { error: SEARCH_FAILED })))
		);

		render(ArticleSearch, { currentId: null });
		await page.getByLabelText('記事を探す').fill('瑠璃');

		await expect.element(page.getByText(SEARCH_FAILED)).toBeInTheDocument();
	});

	it('marks the open article row with aria-current', async () => {
		vi.stubGlobal(
			'fetch',
			vi.fn(() => Promise.resolve(jsonResponse(200, { items: [result] })))
		);

		render(ArticleSearch, { currentId: 7 });
		await page.getByLabelText('記事を探す').fill('瑠璃');

		await expect
			.element(page.getByRole('link', { name: /瑠璃と金泥の記事/ }))
			.toHaveAttribute('aria-current', 'page');
	});

	it('discards a stale in-flight response after the query changes (no flicker of old results)', async () => {
		const first = deferredResponse(200, { items: [result] });
		const second = {
			...result,
			id: 8,
			title: '二杯目の記事'
		};
		const fetchMock = vi
			.fn()
			.mockReturnValueOnce(first.promise)
			.mockResolvedValueOnce(jsonResponse(200, { items: [second] }));
		vi.stubGlobal('fetch', fetchMock);

		render(ArticleSearch, { currentId: null });
		const input = page.getByLabelText('記事を探す');
		await input.fill('瑠璃');
		// 1杯目が飛行中のまま打ち直す
		await new Promise((r) => setTimeout(r, 350));
		await input.fill('二杯目');

		await expect.element(page.getByRole('link', { name: /二杯目の記事/ })).toBeVisible();

		first.resolve();
		await new Promise((r) => setTimeout(r, 20));

		await expect.element(page.getByRole('link', { name: /二杯目の記事/ })).toBeVisible();
		expect(page.getByRole('link', { name: /瑠璃と金泥の記事/ }).elements()).toHaveLength(0);
	});
});
