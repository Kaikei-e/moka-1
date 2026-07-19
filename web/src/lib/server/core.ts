// moka-core への BFF クライアント。moka-core へはサーバー側からのみ届く(bp-svelte §11)。
// ブラウザは moka-web としか話さない — moka-core 非公開の原則を保つ
import {
	authStatusResponseSchema,
	articleResponseSchema,
	articlesResponseSchema,
	feedsResponseSchema,
	fullTextResponseSchema,
	passkeysResponseSchema,
	registerResponseSchema,
	searchResponseSchema,
	summaryResponseSchema,
	tagsResponseSchema,
	type Article,
	type Feed,
	type FullText,
	type Passkey,
	type SearchResult,
	type Summary
} from '$lib/api/schemas';

const baseURL = () => process.env.MOKA_CORE_URL ?? 'http://localhost:8080';

export type RegisterResult =
	| { ok: true; created: boolean; feed: Feed; insertedArticles: number }
	| { ok: false; status: number; message: string };

export type ArticlesPage = { articles: Article[]; nextCursor: string | null };

// カーソルベース(keyset)ページング。cursor 省略時は先頭ページ(サイドバーの無限スクロールと
// SSR 初期表示の両方がこれを使う — 前者は articles/+server.ts 経由、後者は +layout.server.ts 直)
export async function listArticlesPage(
	fetchFn: typeof fetch,
	limit = 20,
	cursor?: string | null
): Promise<ArticlesPage> {
	const params = new URLSearchParams({ limit: String(limit) });
	if (cursor) params.set('cursor', cursor);
	const res = await fetchFn(`${baseURL()}/api/v1/articles?${params}`);
	if (!res.ok) throw new Error(`moka-core list articles: ${res.status}`);
	const body = articlesResponseSchema.parse(await res.json());
	return { articles: body.articles, nextCursor: body.next_cursor };
}

// ハイブリッド検索(GET /api/v1/search)。moka-core 側が pg_trgm + pgvector を RRF で融合し、
// 記事一覧と同じ記事表現 + score を降順で返す(ADR00022)。ページングは無し — limit 一発
export async function searchArticles(
	fetchFn: typeof fetch,
	query: string,
	limit = 20
): Promise<SearchResult[]> {
	const params = new URLSearchParams({ q: query, limit: String(limit) });
	const res = await fetchFn(`${baseURL()}/api/v1/search?${params}`);
	if (!res.ok) throw new Error(`moka-core search: ${res.status}`);
	return searchResponseSchema.parse(await res.json()).items;
}

export async function getArticle(fetchFn: typeof fetch, id: number): Promise<Article | null> {
	const res = await fetchFn(`${baseURL()}/api/v1/articles/${id}`);
	if (res.status === 404) return null;
	if (!res.ok) throw new Error(`moka-core get article ${id}: ${res.status}`);
	return articleResponseSchema.parse(await res.json()).article;
}

export async function listFeeds(fetchFn: typeof fetch): Promise<Feed[]> {
	const res = await fetchFn(`${baseURL()}/api/v1/feeds`);
	if (!res.ok) throw new Error(`moka-core list feeds: ${res.status}`);
	return feedsResponseSchema.parse(await res.json()).feeds;
}

// 既読打刻(冪等、204)。読書ビューを開いた事実を moka-core に残すだけで、失敗しても
// 読書を妨げない — フェイルソフトの判断(黙って握りつぶす)は呼び出し側の BFF ルートが行う
export async function markArticleRead(fetchFn: typeof fetch, id: number): Promise<boolean> {
	const res = await fetchFn(`${baseURL()}/api/v1/articles/${id}/read`, { method: 'POST' });
	return res.status === 204;
}

export type DeleteFeedResult = { ok: true } | { ok: false; status: number; message: string };

// moka-core のエラーステータス → moka の声(事実 + 次の行動、謝罪しない)
const deleteFeedErrorMessages: Record<number, string> = {
	404: 'フィードが見つかりません。再読み込みしてください'
};

// フィードの削除(店との別れ)。204 のみ成功 — CASCADE で記事・要約・既読の事実も消える
export async function deleteFeed(fetchFn: typeof fetch, id: number): Promise<DeleteFeedResult> {
	const res = await fetchFn(`${baseURL()}/api/v1/feeds/${id}`, { method: 'DELETE' });
	if (res.status === 204) return { ok: true };
	return {
		ok: false,
		status: res.status,
		message: deleteFeedErrorMessages[res.status] ?? '削除に失敗しました。再試行してください'
	};
}

export async function listPasskeys(fetchFn: typeof fetch): Promise<Passkey[]> {
	const res = await fetchFn(`${baseURL()}/api/v1/auth/passkeys`);
	if (!res.ok) throw new Error(`moka-core list passkeys: ${res.status}`);
	return passkeysResponseSchema.parse(await res.json()).passkeys;
}

export type DeletePasskeyResult = { ok: true } | { ok: false; status: number; message: string };

// moka-core のエラーステータス → moka の声(事実 + 次の行動、謝罪しない)
const deletePasskeyErrorMessages: Record<number, string> = {
	404: 'パスキーが見つかりません。再読み込みしてください'
};

// パスキーの削除(ハード削除、ADR00019 と同じ流儀)。204 のみ成功
export async function deletePasskey(
	fetchFn: typeof fetch,
	id: number
): Promise<DeletePasskeyResult> {
	const res = await fetchFn(`${baseURL()}/api/v1/auth/passkeys/${id}`, { method: 'DELETE' });
	if (res.status === 204) return { ok: true };
	return {
		ok: false,
		status: res.status,
		message: deletePasskeyErrorMessages[res.status] ?? '削除に失敗しました。再試行してください'
	};
}

// ログアウト(ADR00021)。moka-core はセッションストアを持たないステートレス設計なので
// 失敗しない — 常に cookie を失効させる Set-Cookie 付きで 200 を返す。中身は解釈せず
// relayAuthResponse でそのままブラウザへ中継する(ceremony と同じ作法)
export async function postLogout(fetchFn: typeof fetch): Promise<Response> {
	return fetchFn(`${baseURL()}/api/v1/auth/logout`, { method: 'POST' });
}

// moka-core のエラーステータス → moka の声(事実 + 次の行動、謝罪しない)
const registerErrorMessages: Record<number, string> = {
	400: 'URL が正しくありません',
	422: 'この URL はフィードではないようです',
	502: 'フィードの取得に失敗しました。時間をおいて再試行してください'
};

export async function registerFeed(fetchFn: typeof fetch, url: string): Promise<RegisterResult> {
	const res = await fetchFn(`${baseURL()}/api/v1/feeds`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ url })
	});
	if (res.status === 200 || res.status === 201) {
		const body = registerResponseSchema.parse(await res.json());
		return {
			ok: true,
			created: res.status === 201,
			feed: body.feed,
			insertedArticles: body.inserted_articles
		};
	}
	return {
		ok: false,
		status: res.status,
		message: registerErrorMessages[res.status] ?? '登録に失敗しました。再試行してください'
	};
}

export type FullTextResult =
	| { ok: true; created: boolean; fullText: FullText }
	| { ok: false; status: number; message: string };

// moka-core のエラーステータス → moka の声(事実 + 次の行動、謝罪しない)
const fullTextErrorMessages: Record<number, string> = {
	400: 'URL が正しくありません',
	404: '記事が見つかりません',
	422: '本文を取り出せませんでした',
	502: '取り寄せに失敗しました。時間をおいて再試行してください'
};

// 取り寄せは冪等(moka-core 側で保存済みなら再取得しない) — 新規 201 / 既存 200
export async function fetchFullText(
	fetchFn: typeof fetch,
	articleId: number
): Promise<FullTextResult> {
	const res = await fetchFn(`${baseURL()}/api/v1/articles/${articleId}/fulltext`, {
		method: 'POST'
	});
	if (res.status === 200 || res.status === 201) {
		const body = fullTextResponseSchema.parse(await res.json());
		return { ok: true, created: res.status === 201, fullText: body.fulltext };
	}
	return {
		ok: false,
		status: res.status,
		message: fullTextErrorMessages[res.status] ?? '取り寄せに失敗しました。再試行してください'
	};
}

export type SummaryResult =
	{ ok: true; created: boolean; summary: Summary } | { ok: false; status: number; message: string };

// moka-core のエラーステータス → moka の声(事実 + 次の行動、謝罪しない)
const summarizeErrorMessages: Record<number, string> = {
	400: 'この記事は要約できません',
	404: '記事が見つかりません',
	422: '要約の生成に失敗しました。再試行してください',
	502: '要約に失敗しました。時間をおいて再試行してください'
};

export function summarizeErrorMessage(status: number): string {
	return summarizeErrorMessages[status] ?? '要約に失敗しました。再試行してください';
}

// 要約は冪等(moka-core 側で保存済みなら再生成しない) — 新規 201 / 既存 200。
// force=true なら既存があっても無視して常に新規生成する(読者が明示的に「やり直す」場合)。
export async function summarizeArticle(
	fetchFn: typeof fetch,
	articleId: number,
	force = false
): Promise<SummaryResult> {
	const url = `${baseURL()}/api/v1/articles/${articleId}/summary${force ? '?force=true' : ''}`;
	const res = await fetchFn(url, { method: 'POST' });
	if (res.status === 200 || res.status === 201) {
		const body = summaryResponseSchema.parse(await res.json());
		return { ok: true, created: res.status === 201, summary: body.summary };
	}
	return {
		ok: false,
		status: res.status,
		message: summarizeErrorMessage(res.status)
	};
}

// ストリーミング要約(POST /api/v1/articles/{id}/summary/stream)は SSE をそのまま
// バッファせず呼び出し元(BFFルート)へ返す — パースはしない、中継に徹する。
// force は summarizeArticle と同じ意味(?force=true で常に新規生成)。
export async function summarizeArticleStream(
	fetchFn: typeof fetch,
	articleId: number,
	force = false
): Promise<Response> {
	const url = `${baseURL()}/api/v1/articles/${articleId}/summary/stream${force ? '?force=true' : ''}`;
	return fetchFn(url, { method: 'POST' });
}

// moka-core のエラーステータス → moka の声(事実 + 次の行動、技術用語を出さない)
const qaErrorMessages: Record<number, string> = {
	404: '記事が見つかりません',
	422: '答えられませんでした。訊き直してください',
	500: '答えられませんでした。時間をおいて訊き直してください',
	502: '答えられませんでした。時間をおいて訊き直してください',
	503: '答えられませんでした。時間をおいて訊き直してください'
};

export function qaErrorMessage(status: number): string {
	return qaErrorMessages[status] ?? '答えられませんでした。訊き直してください';
}

// 問い返し(POST /api/v1/articles/{id}/qa)。SSE をバッファせず呼び出し元(BFFルート)へ
// 返す — パースはしない、中継に徹する(summarizeArticleStream と同じ作法)。
export async function askArticleStream(
	fetchFn: typeof fetch,
	articleId: number,
	question: string
): Promise<Response> {
	return fetchFn(`${baseURL()}/api/v1/articles/${articleId}/qa`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ question })
	});
}

// GET /api/v1/articles/{id}/summary は純粋な読み取り — LLM を呼ばない。
// enrich.Scheduler が既に自動生成した要約を、ボタンを押さず確認するための窓口(grill決定)。
// 見つからない(404)は「まだ濃縮されていない」という通常の状態であって例外ではない。
export async function getSummary(
	fetchFn: typeof fetch,
	articleId: number
): Promise<Summary | null> {
	const res = await fetchFn(`${baseURL()}/api/v1/articles/${articleId}/summary`);
	if (res.status === 404) return null;
	if (!res.ok) throw new Error(`moka-core get summary ${articleId}: ${res.status}`);
	return summaryResponseSchema.parse(await res.json()).summary;
}

// GET /api/v1/articles/{id}/tags は純粋な読み取り — LLM を呼ばない(getSummary と対称)。
export async function getTags(fetchFn: typeof fetch, articleId: number): Promise<string[] | null> {
	const res = await fetchFn(`${baseURL()}/api/v1/articles/${articleId}/tags`);
	if (res.status === 404) return null;
	if (!res.ok) throw new Error(`moka-core get tags ${articleId}: ${res.status}`);
	return tagsResponseSchema.parse(await res.json()).tags;
}

// --- パスキー認証(ADR00021)。/auth はエッジ(Plecto フィルタ)の検証除外パスで、
// ここだけが認証前のブラウザに開かれた入口になる ---

// GET /api/v1/auth/status — 鍵(パスキー)が登録済みかの事実だけを返す
export async function getAuthStatus(fetchFn: typeof fetch): Promise<{ registered: boolean }> {
	const res = await fetchFn(`${baseURL()}/api/v1/auth/status`);
	if (!res.ok) throw new Error(`moka-core auth status: ${res.status}`);
	return authStatusResponseSchema.parse(await res.json());
}

export type AuthCeremonyStep =
	'register/begin' | 'register/finish' | 'login/begin' | 'login/finish';

// WebAuthn の儀式(begin/finish)。オプション・資格情報の JSON は解釈せず中継に徹する
// (summary/qa の SSE 中継と同じ作法)。finish では moka-core が署名 cookie を Set-Cookie する
export async function postAuthCeremony(
	fetchFn: typeof fetch,
	step: AuthCeremonyStep,
	body?: string
): Promise<Response> {
	if (body === undefined) {
		return fetchFn(`${baseURL()}/api/v1/auth/${step}`, { method: 'POST' });
	}
	return fetchFn(`${baseURL()}/api/v1/auth/${step}`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body
	});
}

// moka-core の儀式応答をブラウザ向けに写す。成功はボディをそのまま、Set-Cookie を
// 必ず全件中継する — 署名 cookie がブラウザへ届く唯一の経路(ADR00011: ブラウザは
// moka-web としか話さないため、中継が落ちるとログインは成立しない)。
// 失敗は moka の声(事実 + 次の行動、技術用語を出さない)に写し、cookie は運ばない
export async function relayAuthResponse(
	upstream: Response,
	quietMessage: (status: number) => string
): Promise<Response> {
	if (!upstream.ok) {
		return new Response(JSON.stringify({ error: quietMessage(upstream.status) }), {
			status: upstream.status,
			headers: { 'Content-Type': 'application/json' }
		});
	}
	const headers = new Headers();
	const contentType = upstream.headers.get('content-type');
	if (contentType) headers.set('content-type', contentType);
	for (const cookie of upstream.headers.getSetCookie()) headers.append('set-cookie', cookie);
	return new Response(await upstream.text(), { status: upstream.status, headers });
}

export type TagResult =
	{ ok: true; created: boolean; tags: string[] } | { ok: false; status: number; message: string };

// moka-core のエラーステータス → moka の声(事実 + 次の行動、謝罪しない)
const tagErrorMessages: Record<number, string> = {
	400: 'この記事にタグを付けられません',
	404: '記事が見つかりません',
	422: 'タグの抽出に失敗しました。再試行してください',
	502: 'タグの抽出に失敗しました。時間をおいて再試行してください'
};

// タグ抽出は冪等(moka-core 側で保存済みなら再生成しない) — 新規 201 / 既存 200。
// force は無い(summarize と違い、article_tags は追記のみで削除しないため意味が薄い)。
export async function tagArticle(fetchFn: typeof fetch, articleId: number): Promise<TagResult> {
	const res = await fetchFn(`${baseURL()}/api/v1/articles/${articleId}/tags`, { method: 'POST' });
	if (res.status === 200 || res.status === 201) {
		const body = tagsResponseSchema.parse(await res.json());
		return { ok: true, created: res.status === 201, tags: body.tags };
	}
	return {
		ok: false,
		status: res.status,
		message: tagErrorMessages[res.status] ?? 'タグの抽出に失敗しました。再試行してください'
	};
}
