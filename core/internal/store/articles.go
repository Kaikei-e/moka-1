package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Kaikei-e/moka-1/core/internal/feed"
)

// InsertArticles は (feed_id, guid) で冪等に記事を挿入し、実際に入った件数を返す。
// フィードは巡回のたびに既知の記事も再配信することが多いため、挿入前に既存 guid を
// 除いておく — IDENTITY のシーケンスは ON CONFLICT で捨てた行にも値を消費するので、
// 事前チェック無しだと巡回のたびに大量の欠番が生まれる。ON CONFLICT DO NOTHING 自体は
// 事前チェックとバッチ発行の間で起こりうる競合(同一フィードの同時登録)の安全網として残す。
func (s *Store) InsertArticles(ctx context.Context, feedID int64, items []feed.Item) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}

	newItems, err := s.filterNewGUIDs(ctx, feedID, items)
	if err != nil {
		return 0, err
	}
	if len(newItems) == 0 {
		return 0, nil
	}

	batch := &pgx.Batch{}
	for _, it := range newItems {
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
	for range newItems {
		tag, err := results.Exec()
		if err != nil {
			return inserted, fmt.Errorf("insert article: %w", err)
		}
		inserted += int(tag.RowsAffected())
	}
	return inserted, nil
}

// filterNewGUIDs は items のうち、その feed でまだ保存されていないものだけを返す。
func (s *Store) filterNewGUIDs(ctx context.Context, feedID int64, items []feed.Item) ([]feed.Item, error) {
	guids := make([]string, len(items))
	for i, it := range items {
		guids[i] = it.GUID
	}

	rows, err := s.pool.Query(ctx,
		`SELECT guid FROM articles WHERE feed_id = $1 AND guid = ANY($2)`,
		feedID, guids,
	)
	if err != nil {
		return nil, fmt.Errorf("select existing guids: %w", err)
	}
	defer rows.Close()

	existing := make(map[string]struct{}, len(items))
	for rows.Next() {
		var guid string
		if err := rows.Scan(&guid); err != nil {
			return nil, fmt.Errorf("scan existing guid: %w", err)
		}
		existing[guid] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate existing guids: %w", err)
	}

	newItems := make([]feed.Item, 0, len(items))
	for _, it := range items {
		if _, ok := existing[it.GUID]; !ok {
			newItems = append(newItems, it)
		}
	}
	return newItems, nil
}

// articleCols は記事1行の SELECT 列。articles 行そのものに加え、フィード名
// (feeds.title — 記事は必ずフィードに属すので JOIN は内部結合でよい)と
// 既読フラグ(article_reads の行の有無)を導出して feed.Article を1回で満たす。
const articleCols = `a.id, a.feed_id, f.title, a.guid, a.url, a.title, COALESCE(a.content, ''),
	 a.published_at, a.created_at,
	 EXISTS (SELECT 1 FROM article_reads r WHERE r.article_id = a.id)`

// scanArticle は articleCols と同順で1行を読む。
func scanArticle(row pgx.Row) (feed.Article, error) {
	var a feed.Article
	err := row.Scan(&a.ID, &a.FeedID, &a.FeedTitle, &a.GUID, &a.URL, &a.Title,
		&a.Content, &a.PublishedAt, &a.CreatedAt, &a.Read)
	return a, err
}

// ListArticles は記事を新しい順に keyset ページングで返す(httpapi.ArticleLister)。
// OFFSET は使わない — 深いページで読み飛ばし量が線形に伸びる。並びキーは
// COALESCE(published_at, created_at)(取得元の feed に pubDate が無い記事は
// 取得できた時刻を代替キーにする) DESC, id DESC。カーソルはその「最後に返した行」
// を指し、続きだけをインデックスレンジで引く(feeds との JOIN は並びに関与しない)。
func (s *Store) ListArticles(ctx context.Context, limit int, cursor *feed.ArticleCursor) ([]feed.Article, error) {
	const from = ` FROM articles a JOIN feeds f ON f.id = a.feed_id`
	const sortKey = `COALESCE(a.published_at, a.created_at)`
	const order = ` ORDER BY ` + sortKey + ` DESC, a.id DESC LIMIT `

	var (
		query string
		args  []any
	)
	if cursor == nil {
		query = `SELECT ` + articleCols + from + order + `$1`
		args = []any{limit}
	} else {
		// 続き = より古い並びキー、同時刻ならより小さい id
		query = `SELECT ` + articleCols + from + `
		 WHERE ` + sortKey + ` < $1 OR (` + sortKey + ` = $1 AND a.id < $2)` + order + `$3`
		args = []any{cursor.SortKey, cursor.ID, limit}
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select articles: %w", err)
	}
	defer rows.Close()

	var out []feed.Article
	for rows.Next() {
		a, err := scanArticle(rows)
		if err != nil {
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
	a, err := scanArticle(s.pool.QueryRow(ctx,
		`SELECT `+articleCols+` FROM articles a JOIN feeds f ON f.id = a.feed_id WHERE a.id = $1`,
		id,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return feed.Article{}, false, nil
	}
	if err != nil {
		return feed.Article{}, false, fmt.Errorf("select article %d: %w", id, err)
	}
	return a, true, nil
}
