package store

import (
	"context"
	"fmt"
)

// PendingForKind は kind(summary|tags|embedding)についてまだ濃縮されていない記事の id を
// 新しい順に返す(enrich.Scheduler が消費する)。「まだ」の定義:
//   - その kind の succeeded イベントが1件も無い
//   - かつ、恒久的失敗(記事本文が長すぎる・本文が無い — 内容は変わらないので何度リトライ
//     しても直らない)のイベントも無い。これらは summarize/tags 両パッケージの
//     ErrArticleTooLong/ErrNoContent のエラー文字列("... too long to ...", "no content to
//     ..." )に共通する部分文字列で判定する(schema.sql の enrichment_attempts コメント
//     「backoff = 直近の失敗回数から導出」に対応する簡易実装)。
//
// 一時的失敗(LLM 不調等)は除外しない — 毎 tick 素直に再試行される。
//
// kind = 'embedding' だけは導出規則が異なる(成果の不在ではなく最新 fulltext との鮮度
// 比較 — 全文を取り寄せたら埋め込みも作り直す)ため pendingEmbeddings に分岐する。
func (s *Store) PendingForKind(ctx context.Context, kind string, limit int) ([]int64, error) {
	if kind == "embedding" {
		return s.pendingEmbeddings(ctx, limit)
	}
	rows, err := s.pool.Query(ctx,
		`SELECT a.id FROM articles a
		 WHERE NOT EXISTS (
		   SELECT 1 FROM enrichment_attempts ea
		   WHERE ea.article_id = a.id AND ea.kind = $1 AND ea.outcome = 'succeeded'
		 ) AND NOT EXISTS (
		   SELECT 1 FROM enrichment_attempts ea
		   WHERE ea.article_id = a.id AND ea.kind = $1
		     AND (ea.error LIKE '%too long to%' OR ea.error LIKE '%no content to%')
		 )
		 ORDER BY a.created_at DESC LIMIT $2`,
		kind, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select pending articles for kind %s: %w", kind, err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan pending article id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending articles for kind %s: %w", kind, err)
	}
	return ids, nil
}
