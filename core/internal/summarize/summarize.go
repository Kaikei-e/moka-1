// Package summarize は記事の「要約」— LLM による濃縮の第一弾 — のドメイン型を持つ。
// DB・HTTP・LLM クライアントの具象は知らない(依存は消費側 interface 経由 — clean-architecture)。
// fulltext(取り寄せ)とは別概念: 要約は AI 生成物(CONTEXT.md「濃縮」)。
package summarize

import (
	"context"
	"errors"
	"strings"
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

// maxInputChars はモデル呼び出し前のクライアント側ガード。実効コンテキスト 8192 トークンから
// プロンプト・max_tokens 分を差し引いた保守的な文字数上限(日本語 1 文字≒1〜2 トークン想定)。
// 正確なトークナイザは導入せず、超過は ErrArticleTooLong で明示エラーにする(黙ってトランケートしない)。
const maxInputChars = 24000

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

// think タグの開閉境界。stripThink と thinkStreamStripper の両方が参照する。
const (
	openTag  = "<think>"
	closeTag = "</think>"
)

// stripThink は Qwen 系モデルが(flag が効かなかった場合の防御として)応答冒頭に付ける
// <think>...</think> CoT を機械的に剥がす。think タグが無ければそのまま返す。
// 閉じずに truncate された場合は closed=false(呼び出し元は ErrEmptyCompletion を返すべき)。
func stripThink(raw string) (text string, stripped bool, closed bool) {
	_, rest, ok := strings.Cut(raw, openTag)
	if !ok {
		return strings.TrimSpace(raw), false, true
	}

	_, after, ok := strings.Cut(rest, closeTag)
	if !ok {
		return "", true, false
	}

	return strings.TrimSpace(after), true, true
}

// thinkStreamStripState は thinkStreamStripper の内部フェーズ。
type thinkStreamStripState int

const (
	thinkStreamUndecided thinkStreamStripState = iota
	thinkStreamInsideThink
	thinkStreamPassthrough
)

// thinkStreamStripper は stripThink のストリーミング版: チャンク到着のたびに
// 「<think> ブロックの外側」だけを即座に返す。<think> が閉じるかどうか判明するまで
// 何もクライアントへ流さない(ADR00014 §5 の防御的除去をチャンク単位に適用したもの)。
// 最終的な保存判定は常に stripThink(完全な生テキスト) を単一の正とし、これは
// リアルタイム表示専用の補助ロジック。
type thinkStreamStripper struct {
	state   thinkStreamStripState
	pending strings.Builder
}

// feed は生チャンクを1つ処理し、今すぐクライアントへ流してよい文字列を返す。
func (s *thinkStreamStripper) feed(chunk string) string {
	switch s.state {
	case thinkStreamPassthrough:
		return chunk
	case thinkStreamInsideThink:
		s.pending.WriteString(chunk)
		buffered := s.pending.String()
		idx := strings.Index(buffered, closeTag)
		if idx == -1 {
			return ""
		}
		after := buffered[idx+len(closeTag):]
		s.pending.Reset()
		s.state = thinkStreamPassthrough
		return after
	default: // thinkStreamUndecided
		s.pending.WriteString(chunk)
		buffered := s.pending.String()
		if len(buffered) < len(openTag) {
			return ""
		}
		if !strings.HasPrefix(buffered, openTag) {
			s.pending.Reset()
			s.state = thinkStreamPassthrough
			return buffered
		}
		rest := buffered[len(openTag):]
		s.pending.Reset()
		s.state = thinkStreamInsideThink
		idx := strings.Index(rest, closeTag)
		if idx == -1 {
			s.pending.WriteString(rest)
			return ""
		}
		after := rest[idx+len(closeTag):]
		s.state = thinkStreamPassthrough
		return after
	}
}

// finish は完全な応答終端(finish_reason=stop相当)で呼ぶ。未決着のまま残った
// バッファは <think> の開始タグ長にすら満たない = think タグではあり得ないので
// 素直な本文として flush する。closed=false は think タグが閉じずに終わった場合。
func (s *thinkStreamStripper) finish() (flush string, closed bool) {
	if s.state == thinkStreamUndecided && s.pending.Len() > 0 {
		flush = s.pending.String()
		s.pending.Reset()
		s.state = thinkStreamPassthrough
	}
	return flush, s.state != thinkStreamInsideThink
}
