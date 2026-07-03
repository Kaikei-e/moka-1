package summarize

import (
	"context"
	"fmt"
	"log/slog"
)

// attemptKind は enrichment_attempts.kind の値(db/schema.sql の CHECK 制約に合わせる)。
const attemptKind = "summary"

// Service は要約ユースケース: 冪等確認 → 対象テキスト選定 → 長さガード → LLM 補完 →
// think 除去 → 保存。interface にのみ依存し、具象は main が注入する(fulltext.Service と同じ形)。
type Service struct {
	store     Store
	fullTexts FullTextLookup
	complete  Completer
	log       *slog.Logger
}

// NewService はポートの具象を受け取って要約ユースケースを組む(呼び出しは main のみ)。
func NewService(store Store, fullTexts FullTextLookup, complete Completer, log *slog.Logger) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{store: store, fullTexts: fullTexts, complete: complete, log: log}
}

// Summarize は articleID の要約を作る。既に保存済みなら外部へは何も呼ばず、その行をそのまま
// 返す(冪等 — fulltext.Service.FetchFullText と同じ思想)。articleContent はフィード由来の
// 本文(呼び出し元が articles テーブルから引いて渡す) — 全文取り寄せ済みならそちらを優先する。
func (s *Service) Summarize(ctx context.Context, articleID int64, articleContent string) (Result, error) {
	existing, found, err := s.store.LatestSummary(ctx, articleID)
	if err != nil {
		return Result{}, fmt.Errorf("lookup summary %d: %w", articleID, err)
	}
	if found {
		return Result{Summary: existing, Created: false}, nil
	}

	text, err := s.resolveText(ctx, articleID, articleContent)
	if err != nil {
		return Result{}, s.fail(ctx, articleID, err)
	}

	if len(text) > maxInputChars {
		return Result{}, s.fail(ctx, articleID, fmt.Errorf("%d chars: %w", len(text), ErrArticleTooLong))
	}

	completion, err := s.complete.Complete(ctx, text)
	if err != nil {
		return Result{}, s.fail(ctx, articleID, fmt.Errorf("complete: %w (%w)", ErrLLMUnavailable, err))
	}

	summaryText, stripped, closed := stripThink(completion.Text)
	if !closed {
		return Result{}, s.fail(ctx, articleID, fmt.Errorf("think tag truncated: %w", ErrEmptyCompletion))
	}
	if summaryText == "" {
		return Result{}, s.fail(ctx, articleID, fmt.Errorf("blank after think-strip: %w", ErrEmptyCompletion))
	}

	meta := completion.Meta
	if meta == nil {
		meta = map[string]any{}
	}
	meta["think_stripped"] = stripped

	sum, err := s.store.InsertSummary(ctx, articleID, summaryText, meta)
	if err != nil {
		return Result{}, s.fail(ctx, articleID, fmt.Errorf("insert summary %d: %w", articleID, err))
	}

	if attemptErr := s.store.InsertEnrichmentAttempt(ctx, articleID, attemptKind, "succeeded", ""); attemptErr != nil {
		s.log.Warn("record enrichment attempt", "article_id", articleID, "err", attemptErr.Error())
	}
	return Result{Summary: sum, Created: true}, nil
}

// resolveText は要約対象のテキストを決める: 全文取り寄せ済みならそれを優先し、
// 無ければ呼び出し元が渡した articleContent(フィード由来)にフォールバックする。
func (s *Service) resolveText(ctx context.Context, articleID int64, articleContent string) (string, error) {
	if ft, found, err := s.fullTexts.LatestFullText(ctx, articleID); err != nil {
		return "", fmt.Errorf("lookup fulltext %d: %w", articleID, err)
	} else if found && ft.Text != "" {
		return ft.Text, nil
	}
	if articleContent == "" {
		return "", ErrNoContent
	}
	return articleContent, nil
}

// fail は失敗を enrichment_attempts に追記してから、呼び出し元へ返すエラーをそのまま返す。
// 記録自体の失敗はログに落として本来のエラーを握りつぶさない(fulltext の fail-soft 作法)。
func (s *Service) fail(ctx context.Context, articleID int64, cause error) error {
	if err := s.store.InsertEnrichmentAttempt(ctx, articleID, attemptKind, "failed", cause.Error()); err != nil {
		s.log.Warn("record enrichment attempt", "article_id", articleID, "err", err.Error())
	}
	return cause
}
