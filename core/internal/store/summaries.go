package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Kaikei-e/moka-1/core/internal/summarize"
)

// article_summaries / enrichment_attempts は INSERT-only(ADR00002)。
// 再要約 = 追記(最新が有効)。model_meta は pgx がそのまま jsonb へエンコード/デコードする。

// LatestSummary は記事の最新の要約を引く。無ければ found=false(エラーではない)。
func (s *Store) LatestSummary(ctx context.Context, articleID int64) (summarize.Summary, bool, error) {
	var sum summarize.Summary
	err := s.pool.QueryRow(ctx,
		`SELECT article_id, summary, model_meta, created_at
		 FROM article_summaries WHERE article_id = $1
		 ORDER BY created_at DESC LIMIT 1`,
		articleID,
	).Scan(&sum.ArticleID, &sum.Text, &sum.ModelMeta, &sum.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return summarize.Summary{}, false, nil
	}
	if err != nil {
		return summarize.Summary{}, false, fmt.Errorf("select latest summary %d: %w", articleID, err)
	}
	return sum, true, nil
}

// InsertSummary は生成した要約を追記する。
func (s *Store) InsertSummary(ctx context.Context, articleID int64, text string, modelMeta map[string]any) (summarize.Summary, error) {
	var sum summarize.Summary
	err := s.pool.QueryRow(ctx,
		`INSERT INTO article_summaries (article_id, summary, model_meta) VALUES ($1, $2, $3)
		 RETURNING article_id, summary, model_meta, created_at`,
		articleID, text, modelMeta,
	).Scan(&sum.ArticleID, &sum.Text, &sum.ModelMeta, &sum.CreatedAt)
	if err != nil {
		return summarize.Summary{}, fmt.Errorf("insert summary %d: %w", articleID, err)
	}
	return sum, nil
}

// InsertEnrichmentAttempt は濃縮の試行(成功・失敗とも)を追記する。
// kind は db/schema.sql の CHECK 制約に合わせる('summary' | 'tags' | 'embedding')。
func (s *Store) InsertEnrichmentAttempt(ctx context.Context, articleID int64, kind, outcome, errMsg string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO enrichment_attempts (article_id, kind, outcome, error) VALUES ($1, $2, $3, NULLIF($4, ''))`,
		articleID, kind, outcome, errMsg,
	)
	if err != nil {
		return fmt.Errorf("insert enrichment attempt %d: %w", articleID, err)
	}
	return nil
}
