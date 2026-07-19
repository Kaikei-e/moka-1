// moka-core レスポンスの境界スキーマ(bp-svelte §15: unknown → Zod、単一ファイルに集約)
import { z } from 'zod';

export const feedSchema = z.object({
	id: z.number(),
	url: z.string(),
	title: z.string(),
	created_at: z.string()
});

export const articleSchema = z.object({
	id: z.number(),
	feed_id: z.number(),
	guid: z.string(),
	url: z.string(),
	title: z.string(),
	content: z.string(),
	published_at: z.string().nullable(),
	created_at: z.string(),
	feed_title: z.string().nullable(), // フィード名(リスト行の手がかり)。欠損時は URL のホスト名で代替
	read: z.boolean() // 既読の事実の有無。リストでは濃淡で静かに沈めるのみ(数を数えない)
});

export const fullTextSchema = z.object({
	article_id: z.number(),
	text: z.string(),
	fetched_at: z.string()
});

export const summarySchema = z.object({
	article_id: z.number(),
	text: z.string(),
	model_meta: z.record(z.string(), z.unknown()),
	created_at: z.string()
});

export const tagsSchema = z.object({ tags: z.array(z.string()) });

// ハイブリッド検索(pg_trgm + pgvector の RRF 融合)の結果 — 記事一覧と同じ記事表現 + score
export const searchResultSchema = articleSchema.extend({ score: z.number() });

// パスキー認証(ADR00021)。status は鍵の有無の事実だけを運ぶ
export const authStatusResponseSchema = z.object({ registered: z.boolean() });

// WebAuthn 儀式オプション(go-webauthn の CredentialCreation / CredentialAssertion)。
// バイナリ列は base64url 文字列で届く — ブラウザ API(ArrayBuffer)への変換は
// lib/webauthn.ts が担い、ここでは境界の形だけを検める(未知のキーは黙って落とす)
const credentialDescriptorSchema = z.object({
	type: z.literal('public-key'),
	id: z.string(),
	transports: z.array(z.string()).optional()
});

export const credentialCreationOptionsSchema = z.object({
	publicKey: z.object({
		rp: z.object({ id: z.string().optional(), name: z.string() }),
		user: z.object({ id: z.string(), name: z.string(), displayName: z.string() }),
		challenge: z.string(),
		pubKeyCredParams: z.array(z.object({ type: z.literal('public-key'), alg: z.number() })),
		timeout: z.number().optional(),
		excludeCredentials: z.array(credentialDescriptorSchema).optional(),
		authenticatorSelection: z
			.object({
				authenticatorAttachment: z.enum(['platform', 'cross-platform']).optional(),
				requireResidentKey: z.boolean().optional(),
				residentKey: z.enum(['discouraged', 'preferred', 'required']).optional(),
				userVerification: z.enum(['discouraged', 'preferred', 'required']).optional()
			})
			.optional(),
		attestation: z.enum(['direct', 'enterprise', 'indirect', 'none']).optional()
	})
});

export const credentialRequestOptionsSchema = z.object({
	publicKey: z.object({
		challenge: z.string(),
		timeout: z.number().optional(),
		rpId: z.string().optional(),
		allowCredentials: z.array(credentialDescriptorSchema).optional(),
		userVerification: z.enum(['discouraged', 'preferred', 'required']).optional()
	})
});

export const articlesResponseSchema = z.object({
	articles: z.array(articleSchema),
	next_cursor: z.string().nullable() // カーソルページング。null = 終端
});
export const articleResponseSchema = z.object({ article: articleSchema });
export const feedsResponseSchema = z.object({ feeds: z.array(feedSchema) });
export const registerResponseSchema = z.object({
	feed: feedSchema,
	inserted_articles: z.number()
});
export const fullTextResponseSchema = z.object({ fulltext: fullTextSchema });
export const summaryResponseSchema = z.object({ summary: summarySchema });
export const tagsResponseSchema = tagsSchema;
export const searchResponseSchema = z.object({ items: z.array(searchResultSchema) });

export type Feed = z.infer<typeof feedSchema>;
export type Article = z.infer<typeof articleSchema>;
export type FullText = z.infer<typeof fullTextSchema>;
export type Summary = z.infer<typeof summarySchema>;
export type Tags = z.infer<typeof tagsSchema>;
export type SearchResult = z.infer<typeof searchResultSchema>;
