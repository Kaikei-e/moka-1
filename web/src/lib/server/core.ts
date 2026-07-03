// moka-core への BFF クライアント。moka-core へはサーバー側からのみ届く(bp-svelte §11)。
// ブラウザは moka-web としか話さない — moka-core 非公開の原則を保つ
import {
	articleResponseSchema,
	articlesResponseSchema,
	feedsResponseSchema,
	fullTextResponseSchema,
	registerResponseSchema,
	type Article,
	type Feed,
	type FullText
} from '$lib/api/schemas';

const baseURL = () => process.env.MOKA_CORE_URL ?? 'http://localhost:8080';

export type RegisterResult =
	| { ok: true; created: boolean; feed: Feed; insertedArticles: number }
	| { ok: false; status: number; message: string };

export async function listArticles(fetchFn: typeof fetch, limit = 50): Promise<Article[]> {
	const res = await fetchFn(`${baseURL()}/api/v1/articles?limit=${limit}`);
	if (!res.ok) throw new Error(`moka-core list articles: ${res.status}`);
	return articlesResponseSchema.parse(await res.json()).articles;
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
