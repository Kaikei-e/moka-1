-- Drop index "articles_published_at_idx" from table: "articles"
DROP INDEX "public"."articles_published_at_idx";
-- Create index "articles_sort_key_idx" to table: "articles"
CREATE INDEX "articles_sort_key_idx" ON "public"."articles" ((COALESCE(published_at, created_at)) DESC, "id" DESC);
