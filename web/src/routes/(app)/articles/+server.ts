// サイドバー(記事リスト)の無限スクロール、クライアント起点のページ取得。
// ブラウザは moka-core を直接叩かない — ここがサーバー側の唯一の窓口
// (articles/[id]/fulltext/+server.ts と同じ作法)。ページサイズは固定(20)で、
// cursor だけをクエリから受け取って moka-core へそのまま中継する。
import { json } from '@sveltejs/kit';
import { listArticlesPage } from '$lib/server/core';
import { LOAD_MORE_FAILED } from '$lib/copy';
import type { RequestHandler } from './$types';

const PAGE_SIZE = 20;

export const GET: RequestHandler = async ({ fetch, url }) => {
	const cursor = url.searchParams.get('cursor');
	try {
		const { articles, nextCursor } = await listArticlesPage(fetch, PAGE_SIZE, cursor);
		return json({ articles, next_cursor: nextCursor });
	} catch {
		return json({ error: LOAD_MORE_FAILED }, { status: 502 });
	}
};
