import { fail, redirect } from '@sveltejs/kit';
import { deletePasskey, listPasskeys } from '$lib/server/core';
import type { Actions, PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ fetch }) => {
	// フェイルソフト: 一覧が取れなくてもページ自体は出す(feeds/+page.server.ts と同じ判断)
	try {
		return { passkeys: await listPasskeys(fetch), passkeysUnavailable: false };
	} catch {
		return { passkeys: [], passkeysUnavailable: true };
	}
};

export const actions: Actions = {
	// パスキーの削除(ADR00019 と同じ流儀 — ハード削除)。確認は UI 側(PasskeyList の
	// 警告様式二段確認)が済ませてから、ここに native POST が届く。最後の1本を消しても
	// 拒まない(パスキーが1本も無い状態はブートストラップを再び開く — ADR00021 の意図した回復経路)
	delete: async ({ fetch, request }) => {
		const form = await request.formData();
		const id = Number(form.get('id'));
		if (!Number.isInteger(id) || id < 1) {
			return fail(400, {
				scope: 'delete' as const,
				message: 'パスキーが見つかりません。再読み込みしてください'
			});
		}

		let result;
		try {
			result = await deletePasskey(fetch, id);
		} catch {
			return fail(502, {
				scope: 'delete' as const,
				message: '削除に失敗しました。再試行してください'
			});
		}
		if (!result.ok) {
			return fail(result.status, { scope: 'delete' as const, message: result.message });
		}

		// redirect-after-post: リロードで再削除 POST が飛ばないように(feeds と同じ作法)
		redirect(303, '/account?deleted=1');
	}
};
