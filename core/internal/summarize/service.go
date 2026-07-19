package summarize

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Kaikei-e/moka-1/core/internal/llm"
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
// 返す(冪等 — fulltext.Service.FetchFullText と同じ思想)。force が true の場合はこの冪等
// 短絡を無視して常に LLM を呼び直す(読者が品質に満足できず明示的に「やり直す」場合。
// article_summaries は INSERT-only なので新しい行が追記され、既存行は消えない — ADR00002)。
// articleContent はフィード由来の本文(呼び出し元が articles テーブルから引いて渡す) —
// 全文取り寄せ済みならそちらを優先する。
func (s *Service) Summarize(ctx context.Context, articleID int64, articleContent string, force bool) (Result, error) {
	if !force {
		existing, found, err := s.store.LatestSummary(ctx, articleID)
		if err != nil {
			return Result{}, fmt.Errorf("lookup summary %d: %w", articleID, err)
		}
		if found {
			return Result{Summary: existing, Created: false}, nil
		}
	}

	text, err := s.resolveText(ctx, articleID, articleContent)
	if err != nil {
		return Result{}, s.fail(ctx, articleID, err)
	}

	if est := estimateTokens(text); est > maxInputTokens {
		return Result{}, s.fail(ctx, articleID, fmt.Errorf("~%d tokens: %w", est, ErrArticleTooLong))
	}

	completion, err := s.complete.Complete(ctx, text)
	if err != nil {
		return Result{}, s.fail(ctx, articleID, fmt.Errorf("complete: %w (%w)", ErrLLMUnavailable, err))
	}

	summaryText, stripped, closed := llm.StripThink(completion.Text)
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
	if force {
		meta["regenerated"] = true
	}

	persistCtx, cancel := persistContext(ctx)
	defer cancel()
	sum, err := s.store.InsertSummary(persistCtx, articleID, summaryText, meta)
	if err != nil {
		return Result{}, s.fail(ctx, articleID, fmt.Errorf("insert summary %d: %w", articleID, err))
	}

	if attemptErr := s.store.InsertEnrichmentAttempt(persistCtx, articleID, attemptKind, "succeeded", ""); attemptErr != nil {
		s.log.Warn("record enrichment attempt", "article_id", articleID, "err", attemptErr.Error())
	}
	return Result{Summary: sum, Created: true}, nil
}

// SummarizeStream は Summarize のストリーミング版。既に保存済みなら(冪等)llm を
// 呼ばず既存テキストを1回の onDelta で返す。force が true なら Summarize と同様この
// 短絡を無視して常に新規生成する。新規生成時は生チャンクを thinkStreamStripper に通し、
// think ブロックの外側だけを onDelta で逐次流す(専用の /summary/stream エンドポイント用)。
// 最終的な保存判定は Summarize と同じ stripThink(完全な生テキスト) — ストリーミング中の
// 見た目はあくまで補助であり、接続断・LLM失敗時は部分テキストを一切保存せず failed のみ
// 記録する(ADR00014 §7 踏襲)。
func (s *Service) SummarizeStream(
	ctx context.Context, articleID int64, articleContent string, force bool, onDelta func(delta string),
) (Result, error) {
	if !force {
		existing, found, err := s.store.LatestSummary(ctx, articleID)
		if err != nil {
			return Result{}, fmt.Errorf("lookup summary %d: %w", articleID, err)
		}
		if found {
			onDelta(existing.Text)
			return Result{Summary: existing, Created: false}, nil
		}
	}

	text, err := s.resolveText(ctx, articleID, articleContent)
	if err != nil {
		return Result{}, s.fail(ctx, articleID, err)
	}

	if est := estimateTokens(text); est > maxInputTokens {
		return Result{}, s.fail(ctx, articleID, fmt.Errorf("~%d tokens: %w", est, ErrArticleTooLong))
	}

	var stripper llm.ThinkStreamStripper
	completion, err := s.complete.CompleteStream(ctx, text, func(rawDelta string) {
		if chunk := stripper.Feed(rawDelta); chunk != "" {
			onDelta(chunk)
		}
	})
	if err != nil {
		return Result{}, s.fail(ctx, articleID, fmt.Errorf("complete: %w (%w)", ErrLLMUnavailable, err))
	}
	if flush, _ := stripper.Finish(); flush != "" {
		onDelta(flush)
	}

	summaryText, stripped, closed := llm.StripThink(completion.Text)
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
	if force {
		meta["regenerated"] = true
	}

	persistCtx, cancel := persistContext(ctx)
	defer cancel()
	sum, err := s.store.InsertSummary(persistCtx, articleID, summaryText, meta)
	if err != nil {
		return Result{}, s.fail(ctx, articleID, fmt.Errorf("insert summary %d: %w", articleID, err))
	}

	if attemptErr := s.store.InsertEnrichmentAttempt(persistCtx, articleID, attemptKind, "succeeded", ""); attemptErr != nil {
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

// persistTimeout は事後永続化(成果・試行イベントの書き込み)のデッドライン。
const persistTimeout = 10 * time.Second

// persistContext は生成完了後・失敗確定後の永続化に使う ctx を返す。リクエスト ctx の
// キャンセルを引き継がない — 切断・タイムアウトはまさに failed を記録すべき事象であり、
// キャンセル済み ctx のままでは insert 自体が即失敗して何も残らない(ADR00014 §7)。
// 完成した要約が切断と同時に破棄されるのも防ぐ。無期限にしないよう独自の短いデッドラインを敷く。
func persistContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
}

// fail は失敗を enrichment_attempts に追記してから、呼び出し元へ返すエラーをそのまま返す。
// 記録自体の失敗はログに落として本来のエラーを握りつぶさない(fulltext の fail-soft 作法)。
func (s *Service) fail(ctx context.Context, articleID int64, cause error) error {
	persistCtx, cancel := persistContext(ctx)
	defer cancel()
	if err := s.store.InsertEnrichmentAttempt(persistCtx, articleID, attemptKind, "failed", cause.Error()); err != nil {
		s.log.Warn("record enrichment attempt", "article_id", articleID, "err", err.Error())
	}
	return cause
}
