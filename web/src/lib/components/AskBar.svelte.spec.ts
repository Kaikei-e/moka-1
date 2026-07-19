import { page } from 'vitest/browser';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import AskBar from './AskBar.svelte';
import { ANSWERING } from '$lib/copy';

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

const answeredSSE: SSEEvent[] = [
	{
		event: 'sources',
		data: { articles: [{ id: 3, title: '関連する記事', url: 'https://example.com/3' }] }
	},
	{ event: 'delta', data: { text: '背景は' } },
	{ event: 'delta', data: { text: 'こうです' } },
	{ event: 'done', data: { question_id: 1, answer_id: 1 } }
];

afterEach(() => {
	vi.unstubAllGlobals();
});

describe('AskBar.svelte', () => {
	it('streams the answer for a question and lists the sources beneath it', async () => {
		const requests: { url: string; body: string }[] = [];
		const fetchMock = vi.fn((...[input, init]: Parameters<typeof fetch>) => {
			requests.push({ url: String(input), body: String(init?.body) });
			return Promise.resolve(sseResponse(200, answeredSSE));
		});
		vi.stubGlobal('fetch', fetchMock);

		render(AskBar, { articleId: 7 });

		const input = page.getByPlaceholder('この記事について訊く…');
		await input.fill('この記事の背景は');
		await page.getByRole('button', { name: '訊く' }).click();

		await expect.element(page.getByText('この記事の背景は')).toBeInTheDocument();
		await expect.element(page.getByTestId('qa-answer')).toHaveTextContent('背景はこうです');
		const source = page.getByRole('link', { name: '関連する記事' });
		await expect.element(source).toBeVisible();
		await expect.element(source).toHaveAttribute('href', '/articles/3');
		// 送信後は下書きが空に戻る
		await expect.element(input).toHaveValue('');

		expect(requests).toHaveLength(1);
		expect(requests[0]?.url).toBe('/articles/7/qa');
		expect(JSON.parse(requests[0]?.body ?? '')).toEqual({ question: 'この記事の背景は' });
	});

	it('shows a drip with the craft copy while waiting for the first token', async () => {
		const { promise, resolve } = deferredSSEResponse(200, answeredSSE);
		vi.stubGlobal(
			'fetch',
			vi.fn(() => promise)
		);

		render(AskBar, { articleId: 7 });
		await page.getByPlaceholder('この記事について訊く…').fill('要点は');
		await page.getByRole('button', { name: '訊く' }).click();

		await expect.element(page.getByTestId('qa-drip')).toBeVisible();
		await expect.element(page.getByText(ANSWERING)).toBeInTheDocument();

		resolve();

		await expect.element(page.getByTestId('qa-answer')).toHaveTextContent('背景はこうです');
		await expect.element(page.getByTestId('qa-drip')).not.toBeInTheDocument();
	});

	it('ignores empty questions', async () => {
		const fetchMock = vi.fn(() => Promise.resolve(sseResponse(200, answeredSSE)));
		vi.stubGlobal('fetch', fetchMock);

		render(AskBar, { articleId: 7 });

		await page.getByRole('button', { name: '訊く' }).click();

		await expect.element(page.getByPlaceholder('この記事について訊く…')).toBeInTheDocument();
		expect(fetchMock).not.toHaveBeenCalled();
	});

	it('replaces a technical error event with the quiet servant copy (技術用語を読者に見せない)', async () => {
		// moka-core の SSE error は技術文言("llm unavailable" 等)を運ぶ — そのまま出さない
		vi.stubGlobal(
			'fetch',
			vi.fn(() =>
				Promise.resolve(
					sseResponse(200, [{ event: 'error', data: { message: 'llm unavailable' } }])
				)
			)
		);

		render(AskBar, { articleId: 7 });
		await page.getByPlaceholder('この記事について訊く…').fill('なぜ');
		await page.getByRole('button', { name: '訊く' }).click();

		const alert = page.getByRole('alert');
		await expect.element(alert).toHaveTextContent('失敗:');
		await expect.element(alert).toHaveTextContent('答えられませんでした');
		expect(page.getByText(/llm unavailable/).elements()).toHaveLength(0);
	});

	it('shows a quiet failure block when the BFF answers with an error status', async () => {
		vi.stubGlobal(
			'fetch',
			vi.fn(() =>
				Promise.resolve(
					jsonResponse(502, { error: '答えられませんでした。時間をおいて訊き直してください' })
				)
			)
		);

		render(AskBar, { articleId: 7 });
		await page.getByPlaceholder('この記事について訊く…').fill('なぜ');
		await page.getByRole('button', { name: '訊く' }).click();

		const alert = page.getByRole('alert');
		await expect.element(alert).toHaveTextContent('失敗:');
		await expect.element(alert).toHaveTextContent('時間をおいて訊き直してください');
	});

	it('clears the question stack and the draft when the article id changes (SvelteKit reuses the component instance)', async () => {
		vi.stubGlobal(
			'fetch',
			vi.fn(() => Promise.resolve(sseResponse(200, answeredSSE)))
		);

		const { rerender } = render(AskBar, { articleId: 7 });

		const input = page.getByPlaceholder('この記事について訊く…');
		await input.fill('この記事の要点は');
		await page.getByRole('button', { name: '訊く' }).click();
		await expect.element(page.getByTestId('qa-answer')).toHaveTextContent('背景はこうです');
		await input.fill('書きかけの質問');

		await rerender({ articleId: 8 });

		expect(page.getByText('この記事の要点は').elements()).toHaveLength(0);
		expect(page.getByTestId('qa-answer').elements()).toHaveLength(0);
		await expect.element(input).toHaveValue('');
	});
});
