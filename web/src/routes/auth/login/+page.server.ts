// ログイン画面の初期状態(ADR00021)。鍵の有無で導線が変わる —
// 未登録なら初回登録(この moka はあなたのもの)、登録済みならログイン(おかえりなさい)。
// 確かめられない時は null を返し、ページが静かな文言でフェイルソフトする。
import { getAuthStatus } from '$lib/server/core';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ fetch }) => {
	try {
		const { registered } = await getAuthStatus(fetch);
		return { registered };
	} catch {
		return { registered: null };
	}
};
