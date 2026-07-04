package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Kaikei-e/moka-1/core/internal/feed"
)

// InsertArticles は (feed_id, guid) で冪等に記事を挿入し、実際に入った件数を返す。
func (s *Store) InsertArticles(ctx context.Context, feedID int64, items []feed.Item) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}

	batch := &pgx.Batch{}
	for _, it := range items {
		batch.Queue(
			`INSERT INTO articles (feed_id, guid, url, title, content, published_at)
			 VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6)
			 ON CONFLICT (feed_id, guid) DO NOTHING`,
			feedID, it.GUID, it.URL, it.Title, it.Content, it.PublishedAt,
		)
	}

	results := s.pool.SendBatch(ctx, batch)
	defer func() { _ = results.Close() }()

	inserted := 0
	for range items {
		tag, err := results.Exec()
		if err != nil {
			return inserted, fmt.Errorf("insert article: %w", err)
		}
		inserted += int(tag.RowsAffected())
	}
	return inserted, nil
}

// ListArticles は記事を新しい順に keyset ページングで返す(httpapi.ArticleLister)。
// OFFSET は使わない — 深いページで読み飛ばし量が線形に伸びる。並びキーは
// COALESCE(published_at, created_at)(取得元の feed に pubDate が無い記事は
// 取得できた時刻を代替キーにする) DESC, id DESC。カーソルはその「最後に返した行」
// を指し、続きだけをインデックスレンジで引く。
func (s *Store) ListArticles(ctx context.Context, limit int, cursor *feed.ArticleCursor) ([]feed.Article, error) {
	const cols = `id, feed_id, guid, url, title, COALESCE(content, ''), published_at, created_at`
	const sortKey = `COALESCE(published_at, created_at)`
	const order = ` ORDER BY ` + sortKey + ` DESC, id DESC LIMIT `

	var (
		query string
		args  []any
	)
	if cursor == nil {
		query = `SELECT ` + cols + ` FROM articles` + order + `$1`
		args = []any{limit}
	} else {
		// 続き = より古い並びキー、同時刻ならより小さい id
		query = `SELECT ` + cols + ` FROM articles
		 WHERE ` + sortKey + ` < $1 OR (` + sortKey + ` = $1 AND id < $2)` + order + `$3`
		args = []any{cursor.SortKey, cursor.ID, limit}
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select articles: %w", err)
	}
	defer rows.Close()

	var out []feed.Article
	for rows.Next() {
		var a feed.Article
		if err := rows.Scan(&a.ID, &a.FeedID, &a.GUID, &a.URL, &a.Title,
			&a.Content, &a.PublishedAt, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan article: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate articles: %w", err)
	}
	return out, nil
}

// GetArticle は記事を1件引く。無ければ found=false(エラーではない)。
func (s *Store) GetArticle(ctx context.Context, id int64) (feed.Article, bool, error) {
	var a feed.Article
	err := s.pool.QueryRow(ctx,
		`SELECT id, feed_id, guid, url, title, COALESCE(content, ''), published_at, created_at
		 FROM articles WHERE id = $1`,
		id,
	).Scan(&a.ID, &a.FeedID, &a.GUID, &a.URL, &a.Title, &a.Content, &a.PublishedAt, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return feed.Article{}, false, nil
	}
	if err != nil {
		return feed.Article{}, false, fmt.Errorf("select article %d: %w", id, err)
	}
	return a, true, nil
}
