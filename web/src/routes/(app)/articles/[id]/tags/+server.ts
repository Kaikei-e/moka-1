// 読書ビューからのタグ取得・タグ抽出(summary/+server.ts と対称の窓口)。
// ブラウザは moka-core を直接叩かない — ここがサーバー側の唯一の窓口。
import { json } from '@sveltejs/kit';
import { getTags, tagArticle } from '$lib/server/core';
import type { RequestHandler } from './$types';

// enrich.Scheduler が自動生成したタグを、ボタンを押さず確認するための純粋な読み取り
// (LLM は呼ばない)。無ければ 404 — TagList はこれを「まだ濃縮されていない」と
// 解釈し、明示ボタンにフォールバックする(grill決定、summary の GET と同じ作法)。
export const GET: RequestHandler = async ({ fetch, params }) => {
	const id = Number(params.id);
	if (!Number.isInteger(id) || id < 1) {
		return json({ error: '記事が見つかりません' }, { status: 400 });
	}

	const tags = await getTags(fetch, id);
	if (!tags) {
		return json({ error: 'tags not found' }, { status: 404 });
	}
	return json({ tags });
};

export const POST: RequestHandler = async ({ fetch, params }) => {
	const id = Number(params.id);
	if (!Number.isInteger(id) || id < 1) {
		return json({ error: '記事が見つかりません' }, { status: 400 });
	}

	const result = await tagArticle(fetch, id);
	if (!result.ok) {
		return json({ error: result.message }, { status: result.status });
	}
	return json({ tags: result.tags }, { status: result.created ? 201 : 200 });
};
