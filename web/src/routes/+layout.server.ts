import { listArticlesPage } from '$lib/server/core';
import type { LayoutServerLoad } from './$types';

// サイドバーの記事リストは全ページ共通なので layout で読む。無限スクロールの1ページ目
// (limit=20)を読み、続き(2ページ目以降)は articles/+server.ts をクライアントから叩く。
// フェイルソフト: moka-core が落ちていても骨格は出す(tenets のコアパス/増強の思想を UI にも)
export const load: LayoutServerLoad = async ({ fetch }) => {
	try {
		const { articles, nextCursor } = await listArticlesPage(fetch, 20);
		return { articles, nextCursor, listUnavailable: false };
	} catch {
		return { articles: [], nextCursor: null, listUnavailable: true };
	}
};
