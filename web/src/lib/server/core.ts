// moka-core への BFF クライアント。moka-core へはサーバー側からのみ届く(bp-svelte §11)。
// ブラウザは moka-web としか話さない — moka-core 非公開の原則を保つ
import {
	articleResponseSchema,
	articlesResponseSchema,
	feedsResponseSchema,
	fullTextResponseSchema,
	registerResponseSchema,
	summaryResponseSchema,
	tagsResponseSchema,
	type Article,
	type Feed,
	type FullText,
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
