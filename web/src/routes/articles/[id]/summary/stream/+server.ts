// ストリーミング要約(moka-core の SSE エンドポイントを中継するだけの薄い窓口)。
// パースせずそのまま body を転送する — SummaryCard.svelte 側が SSE を読む。
import { json } from '@sveltejs/kit';
import { summarizeArticleStream, summarizeErrorMessage } from '$lib/server/core';
import type { RequestHandler } from './$types';

export const POST: RequestHandler = async ({ fetch, params }) => {
	const id = Number(params.id);
	if (!Number.isInteger(id) || id < 1) {
		return json({ error: '記事が見つかりません' }, { status: 400 });
	}

	const upstream = await summarizeArticleStream(fetch, id);
	if (upstream.status !== 200) {
		return json({ error: summarizeErrorMessage(upstream.status) }, { status: upstream.status });
	}

	return new Response(upstream.body, {
		status: 200,
		headers: {
			'Content-Type': 'text/event-stream',
			'Cache-Control': 'no-cache',
			Connection: 'keep-alive'
		}
	});
};
