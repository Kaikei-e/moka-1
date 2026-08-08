// moka-core の API レスポンス封筒(core/internal/httpapi の writeJSON と 1:1)。
// 検索の SearchHit は feed.Article を埋め込んで score を添える(rag.SearchHit)。

export type Feed = {
	id: number;
	url: string;
	title: string;
	created_at: string;
};

export type Article = {
	id: number;
	feed_id: number;
	feed_title: string | null;
	guid: string;
	url: string;
	title: string;
	content: string;
	published_at: string | null;
	created_at: string;
	read: boolean;
};

export type SearchHit = Article & { score: number };

export type Summary = {
	article_id: number;
	text: string;
	model_meta: Record<string, unknown>;
	created_at: string;
};

export type FullText = {
	article_id: number;
	text: string;
	fetched_at: string;
};

export type PasskeySummary = {
	id: number;
	created_at: string;
	last_used_at: string | null;
};

export type ErrorResponse = { error: string };

export type FeedRegistration = { feed: Feed; inserted_articles: number };
export type FeedListResponse = { feeds: Feed[] };
export type ArticleListResponse = { articles: Article[]; next_cursor: string | null };
export type ArticleResponse = { article: Article };
export type SearchResponse = { items: SearchHit[] };
export type SummaryResponse = { summary: Summary };
export type FullTextResponse = { fulltext: FullText };
export type TagsResponse = { tags: string[] };
export type AuthStatusResponse = { registered: boolean };
export type PasskeyListResponse = { passkeys: PasskeySummary[] };
export type OkResponse = { ok: boolean };

/** POST /api/v1/auth/register/begin が返す WebAuthn CredentialCreation。 */
export type CredentialCreationResponse = {
	publicKey: {
		challenge: string;
		rp: { id: string; name: string };
		user: { id: string; name: string; displayName: string };
	};
};
