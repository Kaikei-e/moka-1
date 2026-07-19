// パスキー登録の儀式・完了(BFF、ADR00021)。資格情報(WebAuthn JSON)はそのまま転送し、
// moka-core が発行する署名 cookie の Set-Cookie を必ずブラウザへ中継する(relayAuthResponse)。
import { json } from '@sveltejs/kit';
import { postAuthCeremony, relayAuthResponse } from '$lib/server/core';
import { AUTH_ALREADY_REGISTERED, AUTH_REGISTER_FAILED } from '$lib/copy';
import type { RequestHandler } from './$types';

const quietMessage = (status: number) =>
	status === 409 ? AUTH_ALREADY_REGISTERED : AUTH_REGISTER_FAILED;

export const POST: RequestHandler = async ({ fetch, request }) => {
	try {
		const upstream = await postAuthCeremony(fetch, 'register/finish', await request.text());
		return await relayAuthResponse(upstream, quietMessage);
	} catch {
		return json({ error: AUTH_REGISTER_FAILED }, { status: 502 });
	}
};
