// パスキー登録の儀式・開始(BFF、ADR00021)。CredentialCreation JSON は解釈せず中継に徹する。
// 既登録(409)は初回ブートストラップが閉じた印 — 静かな文言に写すだけ。
import { json } from '@sveltejs/kit';
import { postAuthCeremony, relayAuthResponse } from '$lib/server/core';
import { AUTH_ALREADY_REGISTERED, AUTH_REGISTER_FAILED } from '$lib/copy';
import type { RequestHandler } from './$types';

const quietMessage = (status: number) =>
	status === 409 ? AUTH_ALREADY_REGISTERED : AUTH_REGISTER_FAILED;

export const POST: RequestHandler = async ({ fetch }) => {
	try {
		return await relayAuthResponse(await postAuthCeremony(fetch, 'register/begin'), quietMessage);
	} catch {
		return json({ error: AUTH_REGISTER_FAILED }, { status: 502 });
	}
};
