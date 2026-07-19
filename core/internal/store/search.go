package store

import (
	"context"
	"fmt"

	"github.com/Kaikei-e/moka-1/core/internal/feed"
)

// SearchArticlesByText は pg_trgm によるテキスト検索(ADR00022 — rag.TextSearcher)。
// スコアはタイトルの similarity(短文どうしの類似)と本文の word_similarity(クエリが
// 本文のどこかに現れる度合い — 長文に対して similarity はほぼ 0 になるため)の大きい方。
// WHERE は % / <% 演算子(GIN trgm インデックスが効く形)で絞る。閾値は pg_trgm の
// 既定値(similarity 0.3 / word_similarity 0.6)— 短いクエリの取りこぼしはベクトル側の
// 寄与でカバーする前提(ADR00022 の自覚済みトレードオフ)。
func (s *Store) SearchArticlesByText(ctx context.Context, q string, limit int) ([]feed.Article, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+articleCols+`
		 FROM articles a JOIN feeds f ON f.id = a.feed_id
		 WHERE a.title % $1 OR $1 <% COALESCE(a.content, '')
		 ORDER BY GREATEST(
		   similarity(a.title, $1),
		   word_similarity($1, COALESCE(a.content, ''))
		 ) DESC, a.id DESC
		 LIMIT $2`,
		q, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select articles by text: %w", err)
	}
	defer rows.Close()

	var out []feed.Article
	for rows.Next() {
		a, err := scanArticle(rows)
		if err != nil {
			return nil, fmt.Errorf("scan article by text: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate articles by text: %w", err)
	}
	return out, nil
}
