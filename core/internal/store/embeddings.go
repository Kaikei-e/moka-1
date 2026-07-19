package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Kaikei-e/moka-1/core/internal/feed"
)

// article_embeddings は INSERT-only(ADR00002)。再埋め込み = 追記(記事ごとの最新が有効)。

// vectorLiteral は []float32 を pgvector の入力リテラル("[x,y,...]")にする。
// pgvector 用のコーデック依存を増やさず、テキスト表現 + ::vector キャストで渡す
// (ミニマリズム — 埋め込みの読み書きはこのファイルに閉じる)。
func vectorLiteral(vec []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, v := range vec {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(v), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

// InsertEmbedding は生成した埋め込みを追記する(rag.EmbeddingStore)。
func (s *Store) InsertEmbedding(ctx context.Context, articleID int64, vector []float32, model string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO article_embeddings (article_id, embedding, model) VALUES ($1, $2::vector, $3)`,
		articleID, vectorLiteral(vector), model,
	)
	if err != nil {
		return fmt.Errorf("insert embedding %d: %w", articleID, err)
	}
	return nil
}

// pendingEmbeddings は「埋め込みがまだ無い、または最新 fulltext(fetched_at)より新しい
// 埋め込みが無い」記事の id を新しい順に返す(embedding の pending 導出 — 成果イベントの
// 不在・鮮度から導く。既存記事のバックフィルもこの導出で自然に走る)。
// summary/tags と違い恒久的失敗が無い(タイトルは常にある)ため attempts は見ない。
func (s *Store) pendingEmbeddings(ctx context.Context, limit int) ([]int64, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT a.id FROM articles a
		 WHERE NOT EXISTS (
		   SELECT 1 FROM article_embeddings e
		   WHERE e.article_id = a.id
		     AND e.created_at > COALESCE(
		       (SELECT max(ft.fetched_at) FROM article_fulltexts ft WHERE ft.article_id = a.id),
		       '-infinity'::timestamptz)
		 )
		 ORDER BY a.created_at DESC LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select pending embeddings: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan pending embedding article id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending embeddings: %w", err)
	}
	return ids, nil
}

// SearchArticlesByVector は記事ごと最新の埋め込みに対する cosine 近傍の上位 limit 件を
// 近い順に返す(rag.VectorSearcher)。順位だけが RRF 融合に使われる。
func (s *Store) SearchArticlesByVector(ctx context.Context, vector []float32, limit int) ([]feed.Article, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+articleCols+`
		 FROM (
		   SELECT DISTINCT ON (article_id) article_id, embedding
		   FROM article_embeddings
		   ORDER BY article_id, created_at DESC
		 ) e
		 JOIN articles a ON a.id = e.article_id
		 JOIN feeds f ON f.id = a.feed_id
		 ORDER BY e.embedding <=> $1::vector
		 LIMIT $2`,
		vectorLiteral(vector), limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select articles by vector: %w", err)
	}
	defer rows.Close()

	var out []feed.Article
	for rows.Next() {
		a, err := scanArticle(rows)
		if err != nil {
			return nil, fmt.Errorf("scan article by vector: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate articles by vector: %w", err)
	}
	return out, nil
}
