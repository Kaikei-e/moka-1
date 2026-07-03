import { fail, redirect } from '@sveltejs/kit';
import { listFeeds, registerFeed } from '$lib/server/core';
import type { Actions, PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ fetch }) => {
	// フェイルソフト: 一覧が取れなくても登録フォームは出す
	try {
		return { feeds: await listFeeds(fetch), feedsUnavailable: false };
	} catch {
		return { feeds: [], feedsUnavailable: true };
	}
};

export const actions: Actions = {
	// 登録の唯一の action。ホームの空状態フォームもここへ POST する
	default: async ({ fetch, request }) => {
		const form = await request.formData();
		const url = String(form.get('url') ?? '').trim();
		if (!url) {
			return fail(400, { message: 'URL が正しくありません' });
		}

		let result;
		try {
			result = await registerFeed(fetch, url);
		} catch {
			return fail(502, { message: '登録に失敗しました。再試行してください' });
		}
		if (!result.ok) {
			return fail(result.status, { message: result.message });
		}

		// redirect-after-post: リロードで再登録 POST が飛ばないように
		redirect(303, `/feeds?registered=${result.insertedArticles}`);
	}
};
