package store

import (
	"context"
	"fmt"
)

// tags / article_tags は INSERT-only(ADR00002)。再タグ付け = 追記のみ、旧タグは消さない。

// LatestTags は記事に付いている現在のタグ名一覧を返す。1件も無ければ found=false。
func (s *Store) LatestTags(ctx context.Context, articleID int64) ([]string, bool, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT t.name FROM article_tags at
		 JOIN tags t ON t.id = at.tag_id
		 WHERE at.article_id = $1
		 ORDER BY at.created_at, t.name`,
		articleID,
	)
	if err != nil {
		return nil, false, fmt.Errorf("select latest tags %d: %w", articleID, err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, false, fmt.Errorf("scan tag %d: %w", articleID, err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate tags %d: %w", articleID, err)
	}
	if len(names) == 0 {
		return nil, false, nil
	}
	return names, true, nil
}

// UpsertTags は names を tags(正規化語彙)へ ON CONFLICT DO NOTHING で追加し、
// article_tags(交差イベント)へ結びつける。modelMeta は現状 tags/article_tags どちらの
// カラムにも持たせていない(モデル系譜は enrichment_attempts 側で追う) — 引数として
// 受け取るのは summarize.LLMCompleter と対称な呼び出し規約を保つため。
func (s *Store) UpsertTags(ctx context.Context, articleID int64, names []string, _ map[string]any) error {
	if len(names) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin upsert tags %d: %w", articleID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, name := range names {
		var tagID int64
		err := tx.QueryRow(ctx,
			`INSERT INTO tags (name) VALUES ($1)
			 ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
			 RETURNING id`,
			name,
		).Scan(&tagID)
		if err != nil {
			return fmt.Errorf("upsert tag %q: %w", name, err)
		}

		if _, err := tx.Exec(ctx,
			`INSERT INTO article_tags (article_id, tag_id) VALUES ($1, $2)
			 ON CONFLICT (article_id, tag_id) DO NOTHING`,
			articleID, tagID,
		); err != nil {
			return fmt.Errorf("link article %d tag %q: %w", articleID, name, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit upsert tags %d: %w", articleID, err)
	}
	return nil
}
