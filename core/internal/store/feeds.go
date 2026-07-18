package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Kaikei-e/moka-1/core/internal/feed"
)

// イミュータブルデータモデル(ADR00001): UPDATE 文は書かない。
// feed_fetches は INSERT-only イベント、条件付き GET の状態は最新行から導出する。

// FeedByURL は URL でフィードを引く。無ければ found=false(エラーではない)。
func (s *Store) FeedByURL(ctx context.Context, url string) (feed.Feed, bool, error) {
	var f feed.Feed
	err := s.pool.QueryRow(ctx,
		`SELECT id, url, COALESCE(title, ''), created_at FROM feeds WHERE url = $1`,
		url,
	).Scan(&f.ID, &f.URL, &f.Title, &f.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return feed.Feed{}, false, nil
	}
	if err != nil {
		return feed.Feed{}, false, fmt.Errorf("select feed by url: %w", err)
	}
	return f, true, nil
}

// InsertFeed はフィードを登録する。title は INSERT 時に一度だけ設定(以後 UPDATE しない)。
// 同時登録の競合(ON CONFLICT DO NOTHING で行が返らない)は既存行の読み直しで解決する。
func (s *Store) InsertFeed(ctx context.Context, url, title string) (feed.Feed, error) {
	var f feed.Feed
	err := s.pool.QueryRow(ctx,
		`INSERT INTO feeds (url, title) VALUES ($1, NULLIF($2, ''))
		 ON CONFLICT (url) DO NOTHING
		 RETURNING id, url, COALESCE(title, ''), created_at`,
		url, title,
	).Scan(&f.ID, &f.URL, &f.Title, &f.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		existing, found, lookErr := s.FeedByURL(ctx, url)
		if lookErr != nil {
			return feed.Feed{}, fmt.Errorf("insert feed conflict lookup: %w", lookErr)
		}
		if !found {
			return feed.Feed{}, fmt.Errorf("insert feed %s: conflict but row not found", url)
		}
		return existing, nil
	}
	if err != nil {
		return feed.Feed{}, fmt.Errorf("insert feed: %w", err)
	}
	return f, nil
}

// LatestFetchConditional は検証子(etag / last_modified)を持つ最新の feed_fetches 行から
// 条件付き GET の状態を導出する。単純な最新1行だと、検証子を返さない 304 や失敗イベントが
// 1つ挟まるだけで有効な検証子を失い、以後フル再取得(または全量200と304の交互)に退化する。
// 検証子付きの履歴が無ければゼロ値(条件無し取得)。
func (s *Store) LatestFetchConditional(ctx context.Context, feedID int64) (feed.Conditional, error) {
	var c feed.Conditional
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(etag, ''), COALESCE(last_modified, '')
		 FROM feed_fetches
		 WHERE feed_id = $1 AND (etag IS NOT NULL OR last_modified IS NOT NULL)
		 ORDER BY fetched_at DESC LIMIT 1`,
		feedID,
	).Scan(&c.ETag, &c.LastModified)
	if errors.Is(err, pgx.ErrNoRows) {
		return feed.Conditional{}, nil
	}
	if err != nil {
		return feed.Conditional{}, fmt.Errorf("select latest fetch: %w", err)
	}
	return c, nil
}

// InsertFeedFetch は取得イベントを追記する(成功・304・失敗すべて)。
func (s *Store) InsertFeedFetch(ctx context.Context, feedID int64, rec feed.FetchRecord) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO feed_fetches (feed_id, status_code, etag, last_modified, error)
		 VALUES ($1, NULLIF($2, 0), NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''))`,
		feedID, rec.StatusCode, rec.ETag, rec.LastModified, rec.Error,
	)
	if err != nil {
		return fmt.Errorf("insert feed fetch: %w", err)
	}
	return nil
}

// DueFeeds は次回取得時刻(最新 feed_fetches.fetched_at + feeds.fetch_interval_seconds)を
// 過ぎた、または一度も取得したことが無いフィードを返す(feed.DueFeedLister)。次回取得時刻は
// 事前計算カラムを持たず、都度このクエリで導出する(schema.sql の feed_fetches コメント通り)。
func (s *Store) DueFeeds(ctx context.Context) ([]feed.Feed, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT f.id, f.url, COALESCE(f.title, ''), f.created_at
		FROM feeds f
		LEFT JOIN LATERAL (
			SELECT fetched_at FROM feed_fetches ff
			WHERE ff.feed_id = f.id
			ORDER BY ff.fetched_at DESC
			LIMIT 1
		) latest ON true
		WHERE latest.fetched_at IS NULL
		   OR latest.fetched_at <= now() - make_interval(secs => f.fetch_interval_seconds)
		ORDER BY f.id`)
	if err != nil {
		return nil, fmt.Errorf("select due feeds: %w", err)
	}
	defer rows.Close()

	var out []feed.Feed
	for rows.Next() {
		var f feed.Feed
		if err := rows.Scan(&f.ID, &f.URL, &f.Title, &f.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan due feed: %w", err)
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate due feeds: %w", err)
	}
	return out, nil
}

// DeleteFeed はフィードを削除する(httpapi.FeedDeleter)。フィード行のハード削除で、
// 配下の記事・取得履歴・濃縮成果・既読は FK の ON DELETE CASCADE がまとめて消す。
// イベントの UPDATE ではなくリソースそのものの削除なので、冒頭のイミュータブル
// データモデルの方針(UPDATE 文は書かない)とは矛盾しない。
// 戻り値は「行を実際に消したか」(false = 元から無い)。
func (s *Store) DeleteFeed(ctx context.Context, id int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM feeds WHERE id = $1`, id)
	if err != nil {
		return false, fmt.Errorf("delete feed %d: %w", id, err)
	}
	return tag.RowsAffected() > 0, nil
}

// ListFeeds は登録済みフィードを新しい順に返す(httpapi.FeedLister)。
func (s *Store) ListFeeds(ctx context.Context) ([]feed.Feed, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, url, COALESCE(title, ''), created_at
		 FROM feeds
		 ORDER BY created_at DESC, id DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("select feeds: %w", err)
	}
	defer rows.Close()

	var out []feed.Feed
	for rows.Next() {
		var f feed.Feed
		if err := rows.Scan(&f.ID, &f.URL, &f.Title, &f.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan feed: %w", err)
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate feeds: %w", err)
	}
	return out, nil
}
