// 既読打刻の BFF 窓口(読書ビューからの fire-and-forget)。ブラウザは moka-core を
// 直接叩かない — ここがサーバー側の唯一の窓口(fulltext/+server.ts と同じ作法)。
// 打刻は増強でありコアパスではない: 失敗しても本文は読めるので、上流の失敗は 502 に
// 畳むだけでボディも持たない(クライアント側はそもそも応答を見ずに握りつぶす)。
import { markArticleRead } from '$lib/server/core';
import type { RequestHandler } from './$types';

export const POST: RequestHandler = async ({ fetch, params }) => {
	const id = Number(params.id);
	if (!Number.isInteger(id) || id < 1) {
		return new Response(null, { status: 400 });
	}

	try {
		const ok = await markArticleRead(fetch, id);
		return new Response(null, { status: ok ? 204 : 502 });
	} catch {
		return new Response(null, { status: 502 });
	}
};
