// パスキーログインの儀式・完了(BFF、ADR00021)。assertion(WebAuthn JSON)はそのまま転送し、
// moka-core が発行する署名 cookie の Set-Cookie を必ずブラウザへ中継する(relayAuthResponse)。
import { json } from '@sveltejs/kit';
import { postAuthCeremony, relayAuthResponse } from '$lib/server/core';
import { AUTH_LOGIN_FAILED, AUTH_NOT_REGISTERED } from '$lib/copy';
import type { RequestHandler } from './$types';

const quietMessage = (status: number) => (status === 404 ? AUTH_NOT_REGISTERED : AUTH_LOGIN_FAILED);

export const POST: RequestHandler = async ({ fetch, request }) => {
	try {
		const upstream = await postAuthCeremony(fetch, 'login/finish', await request.text());
		return await relayAuthResponse(upstream, quietMessage);
	} catch {
		return json({ error: AUTH_LOGIN_FAILED }, { status: 502 });
	}
};
