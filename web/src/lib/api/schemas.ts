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
	created_at: z.string()
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

export type Feed = z.infer<typeof feedSchema>;
export type Article = z.infer<typeof articleSchema>;
export type FullText = z.infer<typeof fullTextSchema>;
export type Summary = z.infer<typeof summarySchema>;
