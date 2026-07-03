// 読書ビューからのクライアント起点の要約(ADR00011 が予告した再取得用ルート)。
// ブラウザは moka-core を直接叩かない — ここがサーバー側の唯一の窓口。
import { json } from '@sveltejs/kit';
import { summarizeArticle } from '$lib/server/core';
import type { RequestHandler } from './$types';

export const POST: RequestHandler = async ({ fetch, params }) => {
	const id = Number(params.id);
	if (!Number.isInteger(id) || id < 1) {
		return json({ error: '記事が見つかりません' }, { status: 400 });
	}

	const result = await summarizeArticle(fetch, id);
	if (!result.ok) {
		return json({ error: result.message }, { status: result.status });
	}
	return json({ summary: result.summary }, { status: result.created ? 201 : 200 });
};
