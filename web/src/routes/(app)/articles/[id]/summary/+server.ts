// 読書ビューからのクライアント起点の要約(ADR00011 が予告した再取得用ルート)。
// ブラウザは moka-core を直接叩かない — ここがサーバー側の唯一の窓口。
import { json } from '@sveltejs/kit';
import { getSummary, summarizeArticle } from '$lib/server/core';
import type { RequestHandler } from './$types';

// enrich.Scheduler が自動生成した要約を、ボタンを押さず確認するための純粋な読み取り
// (LLM は呼ばない)。無ければ 404 — SummaryCard はこれを「まだ濃縮されていない」と
// 解釈し、明示ボタンにフォールバックする(grill決定)。
export const GET: RequestHandler = async ({ fetch, params }) => {
	const id = Number(params.id);
	if (!Number.isInteger(id) || id < 1) {
		return json({ error: '記事が見つかりません' }, { status: 400 });
	}

	const summary = await getSummary(fetch, id);
	if (!summary) {
		return json({ error: 'summary not found' }, { status: 404 });
	}
	return json({ summary });
};

export const POST: RequestHandler = async ({ fetch, params, url }) => {
	const id = Number(params.id);
	if (!Number.isInteger(id) || id < 1) {
		return json({ error: '記事が見つかりません' }, { status: 400 });
	}

	const force = url.searchParams.get('force') === 'true';
	const result = await summarizeArticle(fetch, id, force);
	if (!result.ok) {
		return json({ error: result.message }, { status: result.status });
	}
	return json({ summary: result.summary }, { status: result.created ? 201 : 200 });
};
