// 問い返し(訊く)のBFF窓口: moka-core の SSE(sources / delta / done / error)を
// パースせずそのまま中継する — AskBar.svelte 側が SSE を読む(summary/stream と同じ作法)。
import { json } from '@sveltejs/kit';
import { z } from 'zod';
import { askArticleStream, qaErrorMessage } from '$lib/server/core';
import { ASK_EMPTY_QUESTION } from '$lib/copy';
import type { RequestHandler } from './$types';

// ブラウザからの入力も境界 — unknown → Zod(bp-svelte §15)
const askBodySchema = z.object({ question: z.string() });

export const POST: RequestHandler = async ({ fetch, params, request }) => {
	const id = Number(params.id);
	if (!Number.isInteger(id) || id < 1) {
		return json({ error: '記事が見つかりません' }, { status: 400 });
	}

	// JSON でない body・question 欠落・空白のみは、まとめて「質問が無い」として 400 に落とす
	const parsed = askBodySchema.safeParse(await request.json().catch(() => null));
	const question = parsed.success ? parsed.data.question.trim() : '';
	if (question === '') {
		return json({ error: ASK_EMPTY_QUESTION }, { status: 400 });
	}

	const upstream = await askArticleStream(fetch, id, question);
	if (upstream.status !== 200) {
		return json({ error: qaErrorMessage(upstream.status) }, { status: upstream.status });
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
