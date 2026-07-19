// パスキーログインの儀式・開始(BFF、ADR00021)。CredentialAssertion JSON は解釈せず中継に
// 徹する。未登録(404)は先に作成が要るという事実 — 静かな文言に写すだけ。
import { json } from '@sveltejs/kit';
import { postAuthCeremony, relayAuthResponse } from '$lib/server/core';
import { AUTH_LOGIN_FAILED, AUTH_NOT_REGISTERED } from '$lib/copy';
import type { RequestHandler } from './$types';

const quietMessage = (status: number) => (status === 404 ? AUTH_NOT_REGISTERED : AUTH_LOGIN_FAILED);

export const POST: RequestHandler = async ({ fetch }) => {
	try {
		return await relayAuthResponse(await postAuthCeremony(fetch, 'login/begin'), quietMessage);
	} catch {
		return json({ error: AUTH_LOGIN_FAILED }, { status: 502 });
	}
};
