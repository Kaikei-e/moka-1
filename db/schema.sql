-- moka-1 スキーマの単一ソース(declarative — ADR00001)
-- 変更フロー: このファイルを編集 → `atlas migrate diff` → `atlas migrate lint` → commit
-- migrations/ の SQL を手で編集しない(前進修正のみ)
--
-- 設計方針: イミュータブルデータモデル(kawasima)
--   - リソース(feeds, articles, tags)とイベント(それ以外)を分ける
--   - イベントは日時属性を1つだけ持ち、INSERT-only。UPDATE で事実を上書きしない
--   - 「状態」はカラムでなくイベントの存在から導出する(enrichment_status カラムは持たない)
--   - updated_at カラムは禁止(何が更新されたかわからない無意味なカラムになる)
--   - 訂正・再生成は新しい行の追記(最新 = created_at 降順の先頭)

CREATE EXTENSION IF NOT EXISTS vector;

-- ============ リソース ============

-- 購読フィード。etag / next_fetch_at 等の取得運用状態は feed_fetches(イベント)から導出する
CREATE TABLE feeds (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    url        TEXT NOT NULL UNIQUE,
    title      TEXT,
    fetch_interval_seconds INTEGER NOT NULL DEFAULT 1800,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 記事(取得時点の事実)。要約・タグ・埋め込みは持たない — 濃縮の成果はイベント側
CREATE TABLE articles (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    feed_id      BIGINT NOT NULL REFERENCES feeds (id) ON DELETE CASCADE,
    guid         TEXT NOT NULL,
    url          TEXT NOT NULL,
    title        TEXT NOT NULL,
    content      TEXT,
    published_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (feed_id, guid)
);

-- 記事一覧の keyset ページング用。並びキー (COALESCE(published_at, created_at) DESC, id DESC) と
-- 完全一致させる。published_at が無い記事(フィードに pubDate が無い)は取得できた時刻
-- (created_at)を代替の並びキーとする(取得できた新しい記事が最下部に沈み続けない)
CREATE INDEX articles_sort_key_idx ON articles ((COALESCE(published_at, created_at)) DESC, id DESC);

-- タグ(正規化)。LLM 抽出結果の語彙
CREATE TABLE tags (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============ イベント(INSERT-only) ============

-- フィード取得の事実。条件付き GET 用の etag / last_modified は最新行から引く。
-- 次回取得時刻 = 最新 fetched_at + feeds.fetch_interval_seconds(スケジューラが導出)
CREATE TABLE feed_fetches (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    feed_id       BIGINT NOT NULL REFERENCES feeds (id) ON DELETE CASCADE,
    status_code   INTEGER,
    etag          TEXT,
    last_modified TEXT,
    error         TEXT,
    fetched_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX feed_fetches_latest_idx ON feed_fetches (feed_id, fetched_at DESC);

-- 濃縮の試行(成功も失敗も追記)。pending = 成果イベントが無い記事、backoff = 直近の失敗回数から導出
CREATE TABLE enrichment_attempts (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    article_id   BIGINT NOT NULL REFERENCES articles (id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN ('summary', 'tags', 'embedding')),
    outcome      TEXT NOT NULL CHECK (outcome IN ('succeeded', 'failed')),
    error        TEXT,
    attempted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX enrichment_attempts_latest_idx ON enrichment_attempts (article_id, kind, attempted_at DESC);

-- 要約の成果。再要約 = 追記(最新が有効)
CREATE TABLE article_summaries (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    article_id BIGINT NOT NULL REFERENCES articles (id) ON DELETE CASCADE,
    summary    TEXT NOT NULL,
    model_meta JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX article_summaries_latest_idx ON article_summaries (article_id, created_at DESC);

-- タグ付けの事実(交差エンティティ = イベント)
CREATE TABLE article_tags (
    article_id BIGINT NOT NULL REFERENCES articles (id) ON DELETE CASCADE,
    tag_id     BIGINT NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (article_id, tag_id)
);

CREATE INDEX article_tags_tag_idx ON article_tags (tag_id);

-- 埋め込みの成果。1024次元 = Qwen3-Embedding-0.6B ネイティブ(ADR00008)。
-- 次点 bge-m3 と同次元、4B 格上げ時も MRL 切り詰めで列互換
CREATE TABLE article_embeddings (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    article_id BIGINT NOT NULL REFERENCES articles (id) ON DELETE CASCADE,
    embedding  vector(1024) NOT NULL,
    model      TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX article_embeddings_latest_idx ON article_embeddings (article_id, created_at DESC);

-- 類似検索(cosine — eval/ の retrieval 評価と同一距離)
CREATE INDEX article_embeddings_hnsw_idx ON article_embeddings
    USING hnsw (embedding vector_cosine_ops);

-- 全文取り寄せの成果(INSERT-only)。冪等 — 既にあれば再取得しない(article_id ごとの最新が有効)
CREATE TABLE article_fulltexts (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    article_id BIGINT NOT NULL REFERENCES articles (id) ON DELETE CASCADE,
    text       TEXT NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX article_fulltexts_latest_idx ON article_fulltexts (article_id, fetched_at DESC);

-- 既読の事実。未読 = 行が無い
CREATE TABLE article_reads (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    article_id BIGINT NOT NULL REFERENCES articles (id) ON DELETE CASCADE,
    read_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX article_reads_article_idx ON article_reads (article_id);

-- 今日のハイライト生成の成果(MoA — tenets §3.4)。再生成 = 追記(date ごとの最新が有効)
CREATE TABLE highlights (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    date        DATE NOT NULL,
    article_ids BIGINT[] NOT NULL,
    rationale   TEXT,
    model_meta  JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX highlights_latest_idx ON highlights (date, created_at DESC);

-- 問い返し Q&A。質問と回答は別イベント(日時属性1つルール)
CREATE TABLE qa_questions (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    article_id BIGINT REFERENCES articles (id) ON DELETE SET NULL,
    question   TEXT NOT NULL,
    asked_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE qa_answers (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    question_id BIGINT NOT NULL REFERENCES qa_questions (id) ON DELETE CASCADE,
    answer      TEXT NOT NULL,
    sources     JSONB,
    answered_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX qa_answers_question_idx ON qa_answers (question_id);
