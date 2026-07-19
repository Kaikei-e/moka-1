package rag

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/Kaikei-e/moka-1/core/internal/feed"
	"github.com/Kaikei-e/moka-1/core/internal/llm"
)

const (
	// contextTopK は文脈に採用するハイブリッド検索結果の上限(当該記事を除く購読履歴)。
	contextTopK = 5
	// answerTargetMaxRunes / answerContextMaxRunes はプロンプト組み立て時の切り詰め
	// (rune 単位)。集約層の実効コンテキストに対する保守的な入力予算 — 対象記事
	// 3000 + 文脈 500×5 で日本語でも 8k トークン級に収まる想定。preset の -c 実測後に
	// 見直す(eval/ の仕事)。
	answerTargetMaxRunes  = 3000
	answerContextMaxRunes = 500
)

// QAStore は問い返しユースケースの永続化ポート(消費側定義 — 具象は internal/store)。
type QAStore interface {
	// InsertQuestion は質問受信の事実を qa_questions へ追記し、その id を返す。
	InsertQuestion(ctx context.Context, articleID int64, question string) (int64, error)
	// InsertAnswer は回答完了の事実を qa_answers へ追記し、その id を返す。
	// sourceIDs は文脈に使った記事 id 配列(sources JSONB)。
	InsertAnswer(ctx context.Context, questionID int64, answer string, sourceIDs []int64) (int64, error)
}

// ContextSearcher は文脈記事選定の消費側ポート(具象は *Searcher)。
type ContextSearcher interface {
	Search(ctx context.Context, q string, limit int) ([]SearchHit, error)
}

// AnswerCompleter は回答生成(ストリーミング補完)の消費側ポート(具象は *LLMAnswerCompleter)。
// onRawDelta には think 除去前の生チャンクが順に渡る(除去は Answerer の責務)。
// 戻り値は完全な生テキスト。
type AnswerCompleter interface {
	CompleteStream(ctx context.Context, text string, onRawDelta func(delta string)) (string, error)
}

// Answerer は問い返し Q&A ユースケース: 質問記録 → 文脈選定(当該記事の全文 +
// ハイブリッド検索 top-k)→ 集約層でストリーミング回答 → think 除去 → 保存。
// interface にのみ依存し、具象は main が注入する(summarize.Service と同じ形)。
type Answerer struct {
	store     QAStore
	fullTexts FullTextLookup
	search    ContextSearcher
	complete  AnswerCompleter
	log       *slog.Logger
}

// NewAnswerer はポートの具象を受け取って Q&A ユースケースを組む(呼び出しは main のみ)。
func NewAnswerer(
	store QAStore, fullTexts FullTextLookup, search ContextSearcher, complete AnswerCompleter, log *slog.Logger,
) *Answerer {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Answerer{store: store, fullTexts: fullTexts, search: search, complete: complete, log: log}
}

// Ask は article への質問に回答する。onSources には文脈に選ばれた記事(当該記事を除く
// 検索 top-k)が回答生成前に1回渡り、onDelta には think 除去済みの回答断片が到着順に渡る。
// 質問は受信時に qa_questions へ、回答は完了時に qa_answers へ追記する(ADR00002)。
// 検索の失敗は当該記事単独の文脈に縮退する(フェイルソフト — 検索は増強であって
// 回答の前提条件ではない)。
func (s *Answerer) Ask(
	ctx context.Context, article feed.Article, question string,
	onSources func([]Source), onDelta func(delta string),
) (AnswerResult, error) {
	questionID, err := s.store.InsertQuestion(ctx, article.ID, question)
	if err != nil {
		return AnswerResult{}, fmt.Errorf("insert question: %w", err)
	}

	contexts := s.searchContext(ctx, article, question)
	sources := make([]Source, 0, len(contexts))
	sourceIDs := make([]int64, 0, len(contexts))
	for _, c := range contexts {
		sources = append(sources, Source{ID: c.ID, Title: c.Title, URL: c.URL})
		sourceIDs = append(sourceIDs, c.ID)
	}
	onSources(sources)

	prompt := s.buildPrompt(ctx, article, question, contexts)

	var stripper llm.ThinkStreamStripper
	raw, err := s.complete.CompleteStream(ctx, prompt, func(rawDelta string) {
		if chunk := stripper.Feed(rawDelta); chunk != "" {
			onDelta(chunk)
		}
	})
	if err != nil {
		return AnswerResult{}, fmt.Errorf("complete answer: %w (%w)", ErrLLMUnavailable, err)
	}
	if flush, _ := stripper.Finish(); flush != "" {
		onDelta(flush)
	}

	// gpt-oss 系は reasoning を出す可能性がある — summarize と同じ防御的除去を単一の正とする
	answer, _, closed := llm.StripThink(raw)
	if !closed {
		return AnswerResult{}, fmt.Errorf("think tag truncated: %w", ErrEmptyAnswer)
	}
	if answer == "" {
		return AnswerResult{}, fmt.Errorf("blank after think-strip: %w", ErrEmptyAnswer)
	}

	persistCtx, cancel := persistContext(ctx)
	defer cancel()
	answerID, err := s.store.InsertAnswer(persistCtx, questionID, answer, sourceIDs)
	if err != nil {
		return AnswerResult{}, fmt.Errorf("insert answer for question %d: %w", questionID, err)
	}

	return AnswerResult{QuestionID: questionID, AnswerID: answerID}, nil
}

// searchContext は質問をクエリにハイブリッド検索し、当該記事を除いた top-k を文脈に選ぶ。
// 検索失敗は warn ログだけ残して文脈ゼロに縮退する。
func (s *Answerer) searchContext(ctx context.Context, article feed.Article, question string) []SearchHit {
	// 当該記事が上位に混ざる分を 1 件多めに引いてから除外する
	hits, err := s.search.Search(ctx, question, contextTopK+1)
	if err != nil {
		s.log.Warn("context search failed, answering from the article alone", "article_id", article.ID, "err", err.Error())
		return nil
	}

	contexts := make([]SearchHit, 0, contextTopK)
	for _, h := range hits {
		if h.ID == article.ID {
			continue
		}
		contexts = append(contexts, h)
		if len(contexts) == contextTopK {
			break
		}
	}
	return contexts
}

// buildPrompt は回答生成のユーザーテキストを組む。対象記事は最新の取り寄せ済み全文を
// 優先し(lookup 失敗・未取得はフィード由来 content に縮退)、文脈記事はフィード由来
// content を使う(N 件の全文引きはしない)。
func (s *Answerer) buildPrompt(
	ctx context.Context, article feed.Article, question string, contexts []SearchHit,
) string {
	text := article.Content
	if ft, found, err := s.fullTexts.LatestFullText(ctx, article.ID); err != nil {
		s.log.Warn("lookup fulltext for qa", "article_id", article.ID, "err", err.Error())
	} else if found && ft.Text != "" {
		text = ft.Text
	}

	var b strings.Builder
	b.WriteString("質問: " + question + "\n\n")
	b.WriteString("対象記事: " + article.Title + "\n" + truncateRunes(text, answerTargetMaxRunes) + "\n")
	for i, c := range contexts {
		fmt.Fprintf(&b, "\n参考記事%d: %s\n%s\n", i+1, c.Title, truncateRunes(c.Content, answerContextMaxRunes))
	}
	return b.String()
}
