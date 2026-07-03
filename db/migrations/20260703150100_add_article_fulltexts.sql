-- Create "article_fulltexts" table
CREATE TABLE "public"."article_fulltexts" ("id" bigint NOT NULL GENERATED ALWAYS AS IDENTITY, "article_id" bigint NOT NULL, "text" text NOT NULL, "fetched_at" timestamptz NOT NULL DEFAULT now(), PRIMARY KEY ("id"), CONSTRAINT "article_fulltexts_article_id_fkey" FOREIGN KEY ("article_id") REFERENCES "public"."articles" ("id") ON UPDATE NO ACTION ON DELETE CASCADE);
-- Create index "article_fulltexts_latest_idx" to table: "article_fulltexts"
CREATE INDEX "article_fulltexts_latest_idx" ON "public"."article_fulltexts" ("article_id", "fetched_at" DESC);
