// 読書ビューからのクライアント起点の全文取り寄せ(ADR00011 が予告した再取得用ルート)。
// ブラウザは moka-core を直接叩かない — ここがサーバー側の唯一の窓口。
import { json } from '@sveltejs/kit';
import { fetchFullText } from '$lib/server/core';
import type { RequestHandler } from './$types';

export const POST: RequestHandler = async ({ fetch, params }) => {
	const id = Number(params.id);
	if (!Number.isInteger(id) || id < 1) {
		return json({ error: '記事が見つかりません' }, { status: 400 });
	}

	const result = await fetchFullText(fetch, id);
	if (!result.ok) {
		return json({ error: result.message }, { status: result.status });
	}
	return json({ fulltext: result.fullText }, { status: result.created ? 201 : 200 });
};
