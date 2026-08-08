// moka-core の API を叩く薄いヘルパ群。
//
// 方針: シナリオの主張(何を確かめているか)は spec 側に残し、ここには「複数の spec が
// 同じ形で必要とする手順」だけを置く。レスポンス封筒の型付け(types.ts)と、
// Hurl の `[Options] retry:` に相当するポーリング待ちがその中身。

import { expect, type APIRequestContext, type APIResponse } from '@playwright/test';
import type {
	Article,
	ArticleListResponse,
	ErrorResponse,
	FeedListResponse,
	FeedRegistration
} from './types';

/** レスポンスボディを型付き JSON として読む。 */
export async function json<T>(response: APIResponse): Promise<T> {
	return (await response.json()) as T;
}

/**
 * エラー応答の共通形(`{"error": "..."}`)を確かめる。
 * Hurl の `HTTP 4xx` + `jsonpath "$.error" exists` に対応する。
 */
export async function expectErrorResponse(response: APIResponse, status: number): Promise<void> {
	expect(response.status(), await response.text()).toBe(status);
	const body = await json<ErrorResponse>(response);
	expect(body.error, 'error メッセージが載っていること').toBeTruthy();
}

/** POST /api/v1/feeds — 登録(冪等)。 */
export async function registerFeed(api: APIRequestContext, url: string): Promise<APIResponse> {
	return api.post('/api/v1/feeds', { data: { url } });
}

/** 登録して 201/200 のどちらかであることだけを確かめ、封筒を返す(冪等な再登録用)。 */
export async function registerFeedOk(
	api: APIRequestContext,
	url: string
): Promise<FeedRegistration> {
	const response = await registerFeed(api, url);
	expect([200, 201], await response.text()).toContain(response.status());
	return json<FeedRegistration>(response);
}

/** GET /api/v1/articles — 一覧(カーソルページング)。 */
export async function listArticles(
	api: APIRequestContext,
	params: { limit?: number; cursor?: string } = {}
): Promise<ArticleListResponse> {
	const query: Record<string, string> = {};
	if (params.limit !== undefined) query.limit = String(params.limit);
	if (params.cursor !== undefined) query.cursor = params.cursor;
	const response = await api.get('/api/v1/articles', { params: query });
	expect(response.status(), await response.text()).toBe(200);
	return json<ArticleListResponse>(response);
}

/** GET /api/v1/feeds — 登録済みフィード一覧。 */
export async function listFeeds(api: APIRequestContext): Promise<FeedListResponse> {
	const response = await api.get('/api/v1/feeds');
	expect(response.status(), await response.text()).toBe(200);
	return json<FeedListResponse>(response);
}

/**
 * guid で記事を引く。「一覧の先頭」は他シナリオのフィード登録で簡単に崩れるので、
 * 自分のフィクスチャ記事を掴む唯一の決定的な方法がこれ(旧 *_e2e.sh と同じ理由)。
 */
export async function findArticleByGuid(
	api: APIRequestContext,
	guid: string,
	limit = 200
): Promise<Article | undefined> {
	const { articles } = await listArticles(api, { limit });
	return articles.find((a) => a.guid === guid);
}

/**
 * guid の記事が現れるまで待って id を返す。Hurl の `[Options] retry:` 相当。
 * 同期登録の直後なら即座に見つかるが、スケジューラ由来の記事はここで待つ。
 */
export async function waitForArticleIdByGuid(
	api: APIRequestContext,
	guid: string,
	options: { timeout?: number; intervals?: number[] } = {}
): Promise<number> {
	const { timeout = 30_000, intervals = [500, 1000] } = options;
	await expect
		.poll(async () => (await findArticleByGuid(api, guid))?.id ?? null, { timeout, intervals })
		.not.toBeNull();
	const article = await findArticleByGuid(api, guid);
	expect(article, `guid=${guid} の記事が見つかること`).toBeDefined();
	return article!.id;
}

/** 一覧の先頭記事の id(「どれでもよい、存在することだけが前提」の場面用)。 */
export async function firstArticleId(api: APIRequestContext): Promise<number> {
	const { articles } = await listArticles(api, { limit: 1 });
	const first = articles[0];
	expect(first, '記事が1件以上あること').toBeDefined();
	return first!.id;
}

/**
 * SSE エンドポイントへ POST して本文を丸ごと読む。moka-core は最後のイベントを
 * 書いたらストリームを閉じるので、そのままバッファできる(sse.ts の注記参照)。
 */
export async function postSse(
	api: APIRequestContext,
	path: string,
	data: Record<string, unknown> | undefined,
	timeout = 60_000
): Promise<APIResponse> {
	return api.post(path, data === undefined ? { timeout } : { data, timeout });
}
