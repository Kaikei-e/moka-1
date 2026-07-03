-- 記事一覧 API のカーソルベース(keyset)ページング化に伴い、
-- インデックスを並びキー (published_at DESC NULLS LAST, id DESC) と完全一致させる。
-- 旧 (published_at DESC) は DESC NULLS FIRST であり NULLS LAST の並びに使えない。
DROP INDEX "articles_published_at_idx";
CREATE INDEX "articles_published_at_idx" ON "articles" ("published_at" DESC NULLS LAST, "id" DESC);
