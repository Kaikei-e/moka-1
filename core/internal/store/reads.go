package store

import (
	"context"
	"fmt"
)

// MarkArticleRead は既読の事実を記録する(httpapi.ArticleReadMarker)。
// 既読は「行が存在する」ことだけが意味を持つ(schema.sql: 未読 = 行が無い)ので、
// 既に行があれば挿入しない冪等 INSERT — 連打・再送でイベントを積み増して
// article_reads を太らせない。存在チェックと INSERT の間の同時リクエスト競合で
// まれに行が2つ入り得るが、read の導出は EXISTS なので意味は変わらない。
func (s *Store) MarkArticleRead(ctx context.Context, articleID int64) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO article_reads (article_id)
		 SELECT $1
		 WHERE NOT EXISTS (SELECT 1 FROM article_reads WHERE article_id = $1)`,
		articleID,
	)
	if err != nil {
		return fmt.Errorf("mark article %d read: %w", articleID, err)
	}
	return nil
}
