package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Kaikei-e/moka-1/core/internal/fulltext"
)

// article_fulltexts は INSERT-only(ADR00002)。取り寄せは冪等 — 既に行があれば
// 再取得せず最新行をそのまま返す(fulltext.Service の責務)。

// LatestFullText は記事の最新の取り寄せ済み全文を引く。無ければ found=false(エラーではない)。
func (s *Store) LatestFullText(ctx context.Context, articleID int64) (fulltext.FullText, bool, error) {
	var ft fulltext.FullText
	err := s.pool.QueryRow(ctx,
		`SELECT article_id, text, fetched_at
		 FROM article_fulltexts WHERE article_id = $1
		 ORDER BY fetched_at DESC LIMIT 1`,
		articleID,
	).Scan(&ft.ArticleID, &ft.Text, &ft.FetchedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return fulltext.FullText{}, false, nil
	}
	if err != nil {
		return fulltext.FullText{}, false, fmt.Errorf("select latest fulltext %d: %w", articleID, err)
	}
	return ft, true, nil
}

// InsertFullText は取り寄せた全文を追記する。
func (s *Store) InsertFullText(ctx context.Context, articleID int64, text string) (fulltext.FullText, error) {
	var ft fulltext.FullText
	err := s.pool.QueryRow(ctx,
		`INSERT INTO article_fulltexts (article_id, text) VALUES ($1, $2)
		 RETURNING article_id, text, fetched_at`,
		articleID, text,
	).Scan(&ft.ArticleID, &ft.Text, &ft.FetchedAt)
	if err != nil {
		return fulltext.FullText{}, fmt.Errorf("insert fulltext %d: %w", articleID, err)
	}
	return ft, nil
}
