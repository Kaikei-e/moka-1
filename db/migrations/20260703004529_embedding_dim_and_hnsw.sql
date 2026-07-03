-- 次元固定: Qwen3-Embedding-0.6B ネイティブ 1024 次元(ADR00008)。
-- Atlas community 版は vector の次元変更を diff できないため手書き(atlas.hcl のフロー)。
-- テーブルは M0 時点で空のため USING 句なしで安全に型変更できる
ALTER TABLE "public"."article_embeddings" ALTER COLUMN "embedding" TYPE vector(1024);
-- Create index "article_embeddings_hnsw_idx" to table: "article_embeddings"
CREATE INDEX "article_embeddings_hnsw_idx" ON "public"."article_embeddings" USING hnsw ("embedding" vector_cosine_ops);
