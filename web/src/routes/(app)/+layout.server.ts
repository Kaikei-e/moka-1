import { listArticlesPage } from '$lib/server/core';
import type { LayoutServerLoad } from './$types';

// サイドバーの記事リストは (app) グループの全ページ共通なので layout で読む。無限スクロールの
// 1ページ目(limit=20)を読み、続き(2ページ目以降)は articles/+server.ts をクライアントから叩く。
// フェイルソフト: moka-core が落ちていても骨格は出す(tenets のコアパス/増強の思想を UI にも)
//
// この load は url に依存しない(依存すると全ナビゲーションで再実行され、ArticleList が
// 積み上げた読み込み済みページを articles prop の更新が破棄する — 03-mobile-navigation の
// regression guard)。/auth は route group の外(ルート直下の bare layout)にあり、
// 「鍵を開ける前に記事を運ばない」(ADR00021)は構造で維持する
export const load: LayoutServerLoad = async ({ fetch }) => {
	try {
		const { articles, nextCursor } = await listArticlesPage(fetch, 20);
		return { articles, nextCursor, listUnavailable: false };
	} catch {
		return { articles: [], nextCursor: null, listUnavailable: true };
	}
};
