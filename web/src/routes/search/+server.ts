// サイドバーの検索入力のBFF窓口(ハイブリッド検索)。ブラウザは moka-core を直接叩かない —
// articles/+server.ts と同じ作法で、q だけを受け取り limit 固定(20)で中継する。
// 空クエリは moka-core に問い合わせず(core 側では 400)空の結果を返す —
// 通常一覧へ戻るのはクライアントの仕事。封筒は core と同じ items 名で写す。
import { json } from '@sveltejs/kit';
import { searchArticles } from '$lib/server/core';
import { SEARCH_FAILED } from '$lib/copy';
import type { RequestHandler } from './$types';

const SEARCH_LIMIT = 20;

export const GET: RequestHandler = async ({ fetch, url }) => {
	const q = url.searchParams.get('q')?.trim() ?? '';
	if (q === '') return json({ items: [] });
	try {
		const items = await searchArticles(fetch, q, SEARCH_LIMIT);
		return json({ items });
	} catch {
		return json({ error: SEARCH_FAILED }, { status: 502 });
	}
};
