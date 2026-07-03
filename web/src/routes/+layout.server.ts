import { listArticles } from '$lib/server/core';
import type { LayoutServerLoad } from './$types';

// サイドバーの記事リストは全ページ共通なので layout で読む。
// フェイルソフト: moka-core が落ちていても骨格は出す(tenets のコアパス/増強の思想を UI にも)
export const load: LayoutServerLoad = async ({ fetch }) => {
	try {
		return { articles: await listArticles(fetch), listUnavailable: false };
	} catch {
		return { articles: [], listUnavailable: true };
	}
};
