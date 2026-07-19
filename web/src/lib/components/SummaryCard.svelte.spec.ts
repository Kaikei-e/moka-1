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

// イベントを1つずつ手で流し込める SSE レスポンス(記事切り替え中のストリーム検証用)
function controlledSSEResponse(status: number) {
	let controller!: ReadableStreamDefaultController<Uint8Array>;
	const stream = new ReadableStream<Uint8Array>({
		start(c) {
			controller = c;
		}
	});
	const encoder = new TextEncoder();
	return {
		response: new Response(stream, {
			status,
			headers: { 'Content-Type': 'text/event-stream' }
		}),
		push(e: SSEEvent) {
			controller.enqueue(encoder.encode(sseText([e])));
		},
		close() {
			controller.close();
		}
	};
}

function deferredSSEResponse(status: number, events: SSEEvent[]) {
	let resolve!: () => void;
	const gate = new Promise<void>((r) => (resolve = r));
	const promise = gate.then(() => sseResponse(status, events));
	return { promise, resolve };
}

// マウント時の GET 確認(enrich.Scheduler がまだ何も生成していない = 404)を固定し、
// クリック起点の POST(/summary/stream)だけをテストごとの応答に委ねる。
// GET/POST を method で振り分けるので、既存テストの「クリック後の1回目の応答」という
// 感覚をそのまま保てる(呼び出し回数は mount 分だけ +1 されるので、そちらは書き直す)。
function withNoExistingSummary(onPost: typeof fetch) {
	return vi.fn((input: Parameters<typeof fetch>[0], init?: Parameters<typeof fetch>[1]) => {
		if (!init || init.method === undefined || init.method === 'GET') {
			return Promise.resolve(jsonResponse(404, { error: 'summary not found' }));
		}
		return onPost(input, init);
	});
}

afterEach(() => {
	vi.unstubAllGlobals();
});

describe('SummaryCard.svelte', () => {
	it('checks for an existing summary on mount and shows a summarize button when none exists', async () => {
		const fetchMock = vi.fn(() =>
			Promise.resolve(jsonResponse(404, { error: 'summary not found' }))
		);
		vi.stubGlobal('fetch', fetchMock);

		render(SummaryCard, { articleId: 7 });

		await expect.element(page.getByText('moka による要約')).toBeInTheDocument();
		await expect.element(page.getByRole('button', { name: '要約する' })).toBeVisible();
		expect(fetchMock).toHaveBeenCalledWith('/articles/7/summary');
	});

	it('auto-displays an already-generated summary without any click (enrich.Scheduler ran first)', async () => {
		vi.stubGlobal(
			'fetch',
			vi.fn(() =>
				Promise.resolve(
					jsonResponse(200, {
						summary: {
							article_id: 7,
							text: '自動生成済みの要約',
							model_meta: {},
							created_at: '2026-07-01T09:00:00Z'
						}
					})
				)
			)
		);

		render(SummaryCard, { articleId: 7 });

		await expect.element(page.getByTestId('summary-text')).toHaveTextContent('自動生成済みの要約');
		await expect.element(page.getByRole('button', { name: '要約する' })).not.toBeInTheDocument();
	});

	it('falls back to the summarize button when the mount check itself fails (fail-soft)', async () => {
		vi.stubGlobal(
			'fetch',
			vi.fn(() => Promise.reject(new Error('network down')))
		);

		render(SummaryCard, { articleId: 7 });

		await expect.element(page.getByRole('button', { name: '要約する' })).toBeVisible();
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
			withNoExistingSummary(() => promise)
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
			withNoExistingSummary(() =>
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
		await expect.element(page.getByRole('button', { name: '再試行する' })).toBeVisible();
	});

	it('retries on button click after a failure', async () => {
		const fetchMock = withNoExistingSummary(
			vi
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
				)
		);
		vi.stubGlobal('fetch', fetchMock);

		render(SummaryCard, { articleId: 7 });
		await page.getByRole('button', { name: '要約する' }).click();
		await expect.element(page.getByRole('alert')).toBeVisible();

		await page.getByRole('button', { name: '再試行する' }).click();

		await expect.element(page.getByTestId('summary-text')).toHaveTextContent('再試行後の要約');
		// mount の GET 確認1回 + POST 2回(失敗・再試行)
		expect(fetchMock).toHaveBeenCalledTimes(3);
	});

	it('resets to the summarize button when the article id changes (SvelteKit reuses the component instance)', async () => {
		vi.stubGlobal(
			'fetch',
			withNoExistingSummary(() =>
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

	it('stops applying an in-flight stream after the article id changes (stale deltas must not leak)', async () => {
		const sse = controlledSSEResponse(200);
		vi.stubGlobal(
			'fetch',
			withNoExistingSummary(() => Promise.resolve(sse.response))
		);

		const { rerender } = render(SummaryCard, { articleId: 7 });
		await page.getByRole('button', { name: '要約する' }).click();

		sse.push({ event: 'delta', data: { text: '記事7の要約' } });
		await expect.element(page.getByTestId('summary-text')).toHaveTextContent('記事7の要約');

		await rerender({ articleId: 8 });
		await expect.element(page.getByRole('button', { name: '要約する' })).toBeVisible();

		sse.push({ event: 'delta', data: { text: ' の続き' } });
		sse.push({
			event: 'done',
			data: {
				summary: {
					article_id: 7,
					text: '記事7の要約 の続き',
					model_meta: {},
					created_at: '2026-07-01T09:00:00Z'
				},
				created: true
			}
		});
		sse.close();
		// 取り残されたストリームの処理が済むのを待ってから副作用の不在を確かめる
		await new Promise((r) => setTimeout(r, 20));

		await expect.element(page.getByRole('button', { name: '要約する' })).toBeVisible();
		await expect.element(page.getByTestId('summary-text')).not.toBeInTheDocument();
	});

	it('reserves the card height while streaming so the layout stays stable (§8.1)', async () => {
		const sse = controlledSSEResponse(200);
		vi.stubGlobal(
			'fetch',
			withNoExistingSummary(() => Promise.resolve(sse.response))
		);

		render(SummaryCard, { articleId: 7 });
		const card = page.getByTestId('summary-card');
		await expect.element(card).not.toHaveAttribute('data-streaming');

		await page.getByRole('button', { name: '要約する' }).click();
		await expect.element(card).toHaveAttribute('data-streaming');

		sse.push({ event: 'delta', data: { text: '途中の要約' } });
		await expect.element(page.getByTestId('summary-text')).toHaveTextContent('途中の要約');
		// 逐次表示の間じゅう確保したまま(loading が消えても跳ねない)
		await expect.element(card).toHaveAttribute('data-streaming');

		sse.push({
			event: 'done',
			data: {
				summary: {
					article_id: 7,
					text: '途中の要約',
					model_meta: {},
					created_at: '2026-07-01T09:00:00Z'
				},
				created: true
			}
		});
		sse.close();

		await expect.element(card).not.toHaveAttribute('data-streaming');
	});

	it('shows a quiet regenerate control once a summary is displayed (品質に満足できないときのやり直し)', async () => {
		vi.stubGlobal(
			'fetch',
			withNoExistingSummary(() =>
				Promise.resolve(
					sseResponse(200, [
						{ event: 'delta', data: { text: '最初の要約' } },
						{
							event: 'done',
							data: {
								summary: {
									article_id: 7,
									text: '最初の要約',
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

		render(SummaryCard, { articleId: 7 });
		await page.getByRole('button', { name: '要約する' }).click();

		await expect.element(page.getByTestId('summary-text')).toHaveTextContent('最初の要約');
		await expect.element(page.getByRole('button', { name: '要約をやり直す' })).toBeVisible();
		// 失敗リトライ用のボタン(要約する/再試行する)とは共存しない
		await expect.element(page.getByRole('button', { name: '要約する' })).not.toBeInTheDocument();
		await expect.element(page.getByRole('button', { name: '再試行する' })).not.toBeInTheDocument();
	});

	it('regenerate posts with force=true and replaces the displayed summary on success', async () => {
		const fetchMock = withNoExistingSummary(
			vi
				.fn()
				.mockResolvedValueOnce(
					sseResponse(200, [
						{ event: 'delta', data: { text: '最初の要約' } },
						{
							event: 'done',
							data: {
								summary: {
									article_id: 7,
									text: '最初の要約',
									model_meta: {},
									created_at: '2026-07-01T09:00:00Z'
								},
								created: true
							}
						}
					])
				)
				.mockResolvedValueOnce(
					sseResponse(200, [
						{ event: 'delta', data: { text: 'やり直した要約' } },
						{
							event: 'done',
							data: {
								summary: {
									article_id: 7,
									text: 'やり直した要約',
									model_meta: { regenerated: true },
									created_at: '2026-07-01T09:05:00Z'
								},
								created: true
							}
						}
					])
				)
		);
		vi.stubGlobal('fetch', fetchMock);

		render(SummaryCard, { articleId: 7 });
		await page.getByRole('button', { name: '要約する' }).click();
		await expect.element(page.getByTestId('summary-text')).toHaveTextContent('最初の要約');

		await page.getByRole('button', { name: '要約をやり直す' }).click();

		await expect.element(page.getByTestId('summary-text')).toHaveTextContent('やり直した要約');
		// mount の GET 確認1回 + POST 2回(初回・やり直し)
		expect(fetchMock).toHaveBeenCalledTimes(3);
		const regenerateCallUrl = fetchMock.mock.calls[2][0] as string;
		expect(regenerateCallUrl).toContain('force=true');
	});

	it('a failed regeneration clears the previous summary and offers retry (still force=true)', async () => {
		const fetchMock = withNoExistingSummary(
			vi
				.fn()
				.mockResolvedValueOnce(
					sseResponse(200, [
						{ event: 'delta', data: { text: '最初の要約' } },
						{
							event: 'done',
							data: {
								summary: {
									article_id: 7,
									text: '最初の要約',
									model_meta: {},
									created_at: '2026-07-01T09:00:00Z'
								},
								created: true
							}
						}
					])
				)
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
									model_meta: { regenerated: true },
									created_at: '2026-07-01T09:10:00Z'
								},
								created: true
							}
						}
					])
				)
		);
		vi.stubGlobal('fetch', fetchMock);

		render(SummaryCard, { articleId: 7 });
		await page.getByRole('button', { name: '要約する' }).click();
		await expect.element(page.getByTestId('summary-text')).toHaveTextContent('最初の要約');

		await page.getByRole('button', { name: '要約をやり直す' }).click();

		await expect.element(page.getByRole('alert')).toBeVisible();
		await expect.element(page.getByTestId('summary-text')).not.toBeInTheDocument();
		await expect.element(page.getByRole('button', { name: '再試行する' })).toBeVisible();

		await page.getByRole('button', { name: '再試行する' }).click();

		await expect.element(page.getByTestId('summary-text')).toHaveTextContent('再試行後の要約');
		// mount の GET 確認1回 + POST 3回(初回・やり直し失敗・再試行)
		expect(fetchMock).toHaveBeenCalledTimes(4);
		const thirdPostCallUrl = fetchMock.mock.calls[3][0] as string;
		expect(thirdPostCallUrl).toContain('force=true');
	});
});
