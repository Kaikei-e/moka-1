// Package rag は RAG — ハイブリッド検索(pg_trgm + pgvector の RRF 融合、ADR00022)と
// 問い返し Q&A — のドメイン型とユースケースを持つ。DB・HTTP・LLM クライアントの具象は
// 知らない(依存は消費側 interface 経由 — clean-architecture)。検索・Q&A は増強であり、
// llm が死んでいても検索はテキスト側単独に縮退して動き続ける(tenets §2-6 フェイルソフト)。
package rag

import (
	"context"
	"errors"
	"time"

	"github.com/Kaikei-e/moka-1/core/internal/feed"
)

// ドメイン境界の sentinel。httpapi がステータスコード / SSE error メッセージへ写像する。
var (
	// ErrLLMUnavailable は llm サーバーへの呼び出し失敗全般(タイムアウト・接続エラー・非2xx)。
	ErrLLMUnavailable = errors.New("llm unavailable")
	// ErrEmptyAnswer は think 除去後に回答が空、または think タグが閉じずに truncate された場合。
	ErrEmptyAnswer = errors.New("empty answer after think-strip")
)

// SearchHit はハイブリッド検索の1件。記事表現は一覧 API と同じ feed.Article をそのまま
// 埋め込み(guardrail #4: 層のためのマッピング型を作らない)、RRF 融合スコアを添える。
type SearchHit struct {
	feed.Article
	Score float64 `json:"score"`
}

// Source は Q&A の文脈に選ばれた記事への参照(SSE sources イベントの1件)。
type Source struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// AnswerResult は Q&A ユースケースの結果(SSE done イベントの中身)。
type AnswerResult struct {
	QuestionID int64
	AnswerID   int64
}

// persistTimeout は事後永続化(成果・試行イベントの書き込み)のデッドライン
// (summarize.persistTimeout と同値)。
const persistTimeout = 10 * time.Second

// persistContext は生成完了後・失敗確定後の永続化に使う ctx を返す。リクエスト ctx の
// キャンセルを引き継がない(summarize.persistContext と同じ理由 — ADR00014 §7)。
func persistContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
}

// truncateRunes は text を最大 maxRunes 文字(rune 単位)に切り詰める。バイト単位で切ると
// マルチバイト文字(日本語)の途中で壊れるため rune 境界で切る。
func truncateRunes(text string, maxRunes int) string {
	count := 0
	for i := range text {
		if count == maxRunes {
			return text[:i]
		}
		count++
	}
	return text
}
