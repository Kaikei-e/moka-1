// Package summarize は記事の「要約」— LLM による濃縮の第一弾 — のドメイン型を持つ。
// DB・HTTP・LLM クライアントの具象は知らない(依存は消費側 interface 経由 — clean-architecture)。
// fulltext(取り寄せ)とは別概念: 要約は AI 生成物(CONTEXT.md「濃縮」)。
package summarize

import (
	"context"
	"errors"
	"time"

	"github.com/Kaikei-e/moka-1/core/internal/fulltext"
)

// ドメイン境界の sentinel。httpapi がステータスコードへ写像する。
var (
	// ErrNoContent は記事に要約対象のテキスト(全文・content とも)が無い場合。
	ErrNoContent = errors.New("no content to summarize")
	// ErrArticleTooLong はテキストがモデルの実効コンテキストを超える場合(LLM 呼び出し前のガード)。
	ErrArticleTooLong = errors.New("article too long to summarize")
	// ErrLLMUnavailable は llm サーバーへの呼び出し失敗全般(タイムアウト・接続エラー・非2xx)。
	ErrLLMUnavailable = errors.New("llm unavailable")
	// ErrEmptyCompletion は think 除去後に本文が空、または think タグが閉じずに truncate された場合。
	ErrEmptyCompletion = errors.New("empty completion after think-strip")
)

// maxInputTokens はモデル呼び出し前のクライアント側ガード。実効コンテキスト 16384 トークン
// (llm/models-preset.ini の qwen3.5-4b、2026-07-19 に 8192 から拡張 — 全文取り寄せ後
// 4000〜9000文字級の日本語記事が旧予算を超えて ErrArticleTooLong になる実運用事例のため)
// からシステムプロンプト・max_tokens(1536)分を差し引いた入力予算。
// 正確なトークナイザは導入せず、estimateTokens の保守的(過大)見積りと比較して、
// 超過は ErrArticleTooLong で明示エラーにする(黙ってトランケートしない)。
const maxInputTokens = 14000

// estimateTokens は入力テキストのトークン数を保守的(実際より多め)に見積もる:
// ASCII は 4 文字 ≒ 1 トークン、それ以外(日本語等のマルチバイト文字)は 1 文字 ≒ 2 トークン。
// バイト数(len)で数えると日本語は文字数の3倍に膨れて単位が崩れるため、rune 単位で数える。
func estimateTokens(text string) int {
	ascii, other := 0, 0
	for _, r := range text {
		if r < 128 {
			ascii++
		} else {
			other++
		}
	}
	return ascii/4 + other*2
}

// Summary は要約の成果(article_summaries 行)。ModelMeta はモデル系譜(model/temperature/
// top_p/top_k/enable_thinking/think_stripped)— ADR00007 の A/B 系譜追跡趣旨。
type Summary struct {
	ArticleID int64          `json:"article_id"`
	Text      string         `json:"text"`
	ModelMeta map[string]any `json:"model_meta"`
	CreatedAt time.Time      `json:"created_at"`
}

// Result は要約ユースケースの結果。Created は HTTP 層が 201/200 の判定に使う。
type Result struct {
	Summary Summary
	Created bool
}

// CompletionResult は LLM チャット補完 1 回分の生の結果(think 除去前)。
type CompletionResult struct {
	Text string
	Meta map[string]any
}

// Store は要約ユースケースの永続化ポート(消費側定義 — 具象は internal/store)。
type Store interface {
	LatestSummary(ctx context.Context, articleID int64) (Summary, bool, error)
	InsertSummary(ctx context.Context, articleID int64, text string, modelMeta map[string]any) (Summary, error)
	InsertEnrichmentAttempt(ctx context.Context, articleID int64, kind, outcome, errMsg string) error
}

// FullTextLookup は全文取り寄せ済みテキストの参照ポート(具象は *store.Store)。
// fulltext.FullText をそのまま再利用する(fulltext が feed の型を再利用するのと同じ作法)。
// ここでは参照のみ行い、外部サイトへの新規取り寄せは行わない(要約が副作用で全文取得しない)。
type FullTextLookup interface {
	LatestFullText(ctx context.Context, articleID int64) (fulltext.FullText, bool, error)
}

// Completer は LLM チャット補完の消費側ポート(具象は *HTTPCompleter)。
type Completer interface {
	Complete(ctx context.Context, text string) (CompletionResult, error)
	// CompleteStream は Complete のストリーミング版。onRawDelta には think 除去前の
	// 生チャンクが順に渡る(除去は Service の責務)。戻り値は Complete と同じ完全な結果。
	CompleteStream(ctx context.Context, text string, onRawDelta func(delta string)) (CompletionResult, error)
}

// ストリーミング中の think 除去は llm.ThinkStreamStripper(rag の Q&A と共用)。
// think タグの開閉境界(llm.OpenTag/llm.CloseTag/llm.ThinkLeadingSpace)とともに
// internal/llm に一元化されている(bp-go)。
