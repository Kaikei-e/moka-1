import { page } from 'vitest/browser';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import SummaryCard from './SummaryCard.svelte';

function jsonResponse(status: number, body: unknown) {
	return new Response(JSON.stringify(body), {
		status,
		headers: { 'Content-Type': 'application/json' }
	});
}

type SSEEvent = { event: string; data: unknown };

function sseText(events: SSEEvent[]): string {
	return events.map((e) => `event: ${e.event}\ndata: ${JSON.stringify(e.data)}\n\n`).join('');
}

function sseResponse(status: number, events: SSEEvent[]) {
	return new Response(sseText(events), {
		status,
		headers: { 'Content-Type': 'text/event-stream' }
	});
}

function deferredSSEResponse(status: number, events: SSEEvent[]) {
	let resolve!: () => void;
	const gate = new Promise<void>((r) => (resolve = r));
	const promise = gate.then(() => sseResponse(status, events));
	return { promise, resolve };
}

afterEach(() => {
	vi.unstubAllGlobals();
});

describe('SummaryCard.svelte', () => {
	it('is labeled as the voice of moka and shows a summarize button before any request (no auto-fetch)', async () => {
		const fetchMock = vi.fn();
		vi.stubGlobal('fetch', fetchMock);

		render(SummaryCard, { articleId: 7 });

		await expect.element(page.getByText('moka による要約')).toBeInTheDocument();
		await expect.element(page.getByRole('button', { name: '要約する' })).toBeVisible();
		expect(fetchMock).not.toHaveBeenCalled();
	});

	it('shows a drip while pending, then replaces the button with the fetched summary text', async () => {
		const { promise, resolve } = deferredSSEResponse(200, [
			{ event: 'delta', data: { text: '運ばれてきた要約テキスト' } },
			{
				event: 'done',
				data: {
					summary: {
						article_id: 7,
						text: '運ばれてきた要約テキスト',
						model_meta: {},
						created_at: '2026-07-01T09:00:00Z'
					},
					created: true
				}
			}
		]);
		vi.stubGlobal(
			'fetch',
			vi.fn(() => promise)
		);

		render(SummaryCard, { articleId: 7 });
		await page.getByRole('button', { name: '要約する' }).click();

		await expect.element(page.getByTestId('summary-drip')).toBeVisible();

		resolve();

		await expect
			.element(page.getByTestId('summary-text'))
			.toHaveTextContent('運ばれてきた要約テキスト');
		await expect.element(page.getByTestId('summary-drip')).not.toBeInTheDocument();
		await expect.element(page.getByRole('button', { name: '要約する' })).not.toBeInTheDocument();
	});

	it('shows an inline failure block and keeps the button for retry (fail-soft — reading is unaffected)', async () => {
		vi.stubGlobal(
			'fetch',
			vi.fn(() =>
				Promise.resolve(
					jsonResponse(502, { error: '要約に失敗しました。時間をおいて再試行してください' })
				)
			)
		);

		render(SummaryCard, { articleId: 7 });
		await page.getByRole('button', { name: '要約する' }).click();

		const errorBlock = page.getByRole('alert');
		await expect.element(errorBlock).toHaveTextContent('失敗:');
		await expect.element(errorBlock).toHaveTextContent('時間をおいて再試行してください');
		await expect.element(page.getByRole('button', { name: '要約する' })).toBeVisible();
	});

	it('retries on button click after a failure', async () => {
		const fetchMock = vi
			.fn()
			.mockResolvedValueOnce(jsonResponse(502, { error: '要約に失敗しました' }))
			.mockResolvedValueOnce(
				sseResponse(200, [
					{ event: 'delta', data: { text: '再試行後の要約' } },
					{
						event: 'done',
						data: {
							summary: {
								article_id: 7,
								text: '再試行後の要約',
								model_meta: {},
								created_at: '2026-07-01T09:00:00Z'
							},
							created: true
						}
					}
				])
			);
		vi.stubGlobal('fetch', fetchMock);

		render(SummaryCard, { articleId: 7 });
		await page.getByRole('button', { name: '要約する' }).click();
		await expect.element(page.getByRole('alert')).toBeVisible();

		await page.getByRole('button', { name: '要約する' }).click();

		await expect.element(page.getByTestId('summary-text')).toHaveTextContent('再試行後の要約');
		expect(fetchMock).toHaveBeenCalledTimes(2);
	});

	it('resets to the summarize button when the article id changes (SvelteKit reuses the component instance)', async () => {
		vi.stubGlobal(
			'fetch',
			vi.fn(() =>
				Promise.resolve(
					sseResponse(200, [
						{ event: 'delta', data: { text: '記事7の要約' } },
						{
							event: 'done',
							data: {
								summary: {
									article_id: 7,
									text: '記事7の要約',
									model_meta: {},
									created_at: '2026-07-01T09:00:00Z'
								},
								created: true
							}
						}
					])
				)
			)
		);

		const { rerender } = render(SummaryCard, { articleId: 7 });
		await page.getByRole('button', { name: '要約する' }).click();
		await expect.element(page.getByTestId('summary-text')).toHaveTextContent('記事7の要約');

		await rerender({ articleId: 8 });

		await expect.element(page.getByRole('button', { name: '要約する' })).toBeVisible();
		await expect.element(page.getByTestId('summary-text')).not.toBeInTheDocument();
	});
});
