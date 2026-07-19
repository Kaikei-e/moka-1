-- pg_trgm 拡張(ADR00022)。atlas community は拡張を diff しないため手書き(init.sql の vector と同じ流儀)
CREATE EXTENSION IF NOT EXISTS pg_trgm;
-- Create index "articles_content_trgm_idx" to table: "articles"
CREATE INDEX "articles_content_trgm_idx" ON "public"."articles" USING gin ("content" gin_trgm_ops);
-- Create index "articles_title_trgm_idx" to table: "articles"
CREATE INDEX "articles_title_trgm_idx" ON "public"."articles" USING gin ("title" gin_trgm_ops);
-- Create "passkey_credentials" table
CREATE TABLE "public"."passkey_credentials" ("id" bigint NOT NULL GENERATED ALWAYS AS IDENTITY, "credential_id" bytea NOT NULL, "public_key" bytea NOT NULL, "meta" jsonb NULL, "created_at" timestamptz NOT NULL DEFAULT now(), PRIMARY KEY ("id"), CONSTRAINT "passkey_credentials_credential_id_key" UNIQUE ("credential_id"));
-- Create "auth_assertions" table
CREATE TABLE "public"."auth_assertions" ("id" bigint NOT NULL GENERATED ALWAYS AS IDENTITY, "credential_id" bigint NOT NULL, "sign_count" bigint NOT NULL, "asserted_at" timestamptz NOT NULL DEFAULT now(), PRIMARY KEY ("id"), CONSTRAINT "auth_assertions_credential_id_fkey" FOREIGN KEY ("credential_id") REFERENCES "public"."passkey_credentials" ("id") ON UPDATE NO ACTION ON DELETE CASCADE);
-- Create index "auth_assertions_latest_idx" to table: "auth_assertions"
CREATE INDEX "auth_assertions_latest_idx" ON "public"."auth_assertions" ("credential_id", "asserted_at" DESC);
