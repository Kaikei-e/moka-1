// 鍵の有無の確認窓口(BFF、ADR00021)。ブラウザは moka-core を直接叩かない —
// /auth 配下はエッジの検証除外パスなので、認証前でもここには届く。
import { json } from '@sveltejs/kit';
import { getAuthStatus } from '$lib/server/core';
import { AUTH_STATUS_UNAVAILABLE } from '$lib/copy';
import type { RequestHandler } from './$types';

export const GET: RequestHandler = async ({ fetch }) => {
	try {
		return json(await getAuthStatus(fetch));
	} catch {
		return json({ error: AUTH_STATUS_UNAVAILABLE }, { status: 502 });
	}
};
