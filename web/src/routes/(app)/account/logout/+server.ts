// ログアウトの BFF 窓口(ADR00021)。moka-core はセッションストアを持たないステートレス
// 設計なので失敗しない — 常に cookie を失効させる Set-Cookie を必ずブラウザへ中継する
// (register/login の finish と同じ relayAuthResponse 作法)。
import { json } from '@sveltejs/kit';
import { postLogout, relayAuthResponse } from '$lib/server/core';
import { LOGOUT_FAILED } from '$lib/copy';
import type { RequestHandler } from './$types';

export const POST: RequestHandler = async ({ fetch }) => {
	try {
		const upstream = await postLogout(fetch);
		return await relayAuthResponse(upstream, () => LOGOUT_FAILED);
	} catch {
		return json({ error: LOGOUT_FAILED }, { status: 502 });
	}
};
