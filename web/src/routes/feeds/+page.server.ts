import { fail, redirect } from '@sveltejs/kit';
import { deleteFeed, listFeeds, registerFeed } from '$lib/server/core';
import type { Actions, PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ fetch }) => {
	// フェイルソフト: 一覧が取れなくても登録フォームは出す
	try {
		return { feeds: await listFeeds(fetch), feedsUnavailable: false };
	} catch {
		return { feeds: [], feedsUnavailable: true };
	}
};

// 失敗データは scope で判別する(登録フォームと削除確認は別の場所に表示するため)
export const actions: Actions = {
	// 登録の唯一の action。ホームの空状態フォームもここへ POST する
	register: async ({ fetch, request }) => {
		const form = await request.formData();
		const url = String(form.get('url') ?? '').trim();
		if (!url) {
			return fail(400, { scope: 'register' as const, message: 'URL が正しくありません' });
		}

		let result;
		try {
			result = await registerFeed(fetch, url);
		} catch {
			return fail(502, {
				scope: 'register' as const,
				message: '登録に失敗しました。再試行してください'
			});
		}
		if (!result.ok) {
			return fail(result.status, { scope: 'register' as const, message: result.message });
		}

		// redirect-after-post: リロードで再登録 POST が飛ばないように
		redirect(303, `/feeds?registered=${result.insertedArticles}`);
	},

	// フィードの削除(店との別れ、ADR00019)。CASCADE で記事・要約・既読の事実ごと消える —
	// 確認は UI 側(FeedList の警告様式二段確認)が済ませてから、ここに native POST が届く
	delete: async ({ fetch, request }) => {
		const form = await request.formData();
		const id = Number(form.get('id'));
		if (!Number.isInteger(id) || id < 1) {
			return fail(400, {
				scope: 'delete' as const,
				message: 'フィードが見つかりません。再読み込みしてください'
			});
		}

		let result;
		try {
			result = await deleteFeed(fetch, id);
		} catch {
			return fail(502, {
				scope: 'delete' as const,
				message: '削除に失敗しました。再試行してください'
			});
		}
		if (!result.ok) {
			return fail(result.status, { scope: 'delete' as const, message: result.message });
		}

		// redirect-after-post: リロードで再削除 POST が飛ばないように(登録と同じ作法)
		redirect(303, '/feeds?deleted=1');
	}
};
