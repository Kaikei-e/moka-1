package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// qa_questions / qa_answers は質問と回答を別イベントで追記する(日時属性1つルール —
// ADR00002)。rag.QAStore を満たす。

// InsertQuestion は質問受信の事実を追記し、その id を返す。
func (s *Store) InsertQuestion(ctx context.Context, articleID int64, question string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO qa_questions (article_id, question) VALUES ($1, $2) RETURNING id`,
		articleID, question,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert question for article %d: %w", articleID, err)
	}
	return id, nil
}

// InsertAnswer は回答完了の事実を追記し、その id を返す。sources JSONB は文脈に使った
// 記事 id の配列(文脈ゼロなら空配列 — null と区別する)。
func (s *Store) InsertAnswer(ctx context.Context, questionID int64, answer string, sourceIDs []int64) (int64, error) {
	if sourceIDs == nil {
		sourceIDs = []int64{}
	}
	sources, err := json.Marshal(sourceIDs)
	if err != nil {
		return 0, fmt.Errorf("marshal answer sources: %w", err)
	}

	var id int64
	err = s.pool.QueryRow(ctx,
		`INSERT INTO qa_answers (question_id, answer, sources) VALUES ($1, $2, $3::jsonb) RETURNING id`,
		questionID, answer, string(sources),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert answer for question %d: %w", questionID, err)
	}
	return id, nil
}
