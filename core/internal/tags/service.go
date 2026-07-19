package tags

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/Kaikei-e/moka-1/core/internal/llm"
)

// attemptKind は enrichment_attempts.kind の値(db/schema.sql の CHECK 制約に合わせる)。
const attemptKind = "tags"

// Service はタグ抽出ユースケース: 冪等確認 → 対象テキスト選定 → 長さガード → LLM補完
// (json_schema制約)→ JSONデコード → 保存。interface にのみ依存し、具象は main が注入する
// (summarize.Service と同じ形)。M1 では force(やり直し)は持たない — article_tags は
// PRIMARY KEY(article_id, tag_id) で同名タグの再抽出は新行を増やさず、旧タグも削除しない
// (ADR00002)ため、summarize の force ほど意味を持たない。
type Service struct {
	store     Store
	fullTexts FullTextLookup
	extract   Completer
	log       *slog.Logger
}

// NewService はポートの具象を受け取ってタグ抽出ユースケースを組む(呼び出しは main のみ)。
func NewService(store Store, fullTexts FullTextLookup, extract Completer, log *slog.Logger) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{store: store, fullTexts: fullTexts, extract: extract, log: log}
}

// Tag は articleID のタグを抽出する。既に付いていれば外部へは何も呼ばず、その一覧を
// そのまま返す(冪等 — summarize.Service.Summarize と同じ思想)。articleContent は
// フィード由来の本文(呼び出し元が articles テーブルから引いて渡す) — 全文取り寄せ済み
// ならそちらを優先する。
func (s *Service) Tag(ctx context.Context, articleID int64, articleContent string) (Result, error) {
	existing, found, err := s.store.LatestTags(ctx, articleID)
	if err != nil {
		return Result{}, fmt.Errorf("lookup tags %d: %w", articleID, err)
	}
	if found {
		return Result{Tags: existing, Created: false}, nil
	}

	text, err := s.resolveText(ctx, articleID, articleContent)
	if err != nil {
		return Result{}, s.fail(ctx, articleID, err)
	}

	if est := estimateTokens(text); est > maxInputTokens {
		return Result{}, s.fail(ctx, articleID, fmt.Errorf("~%d tokens: %w", est, ErrArticleTooLong))
	}

	completion, err := s.extract.Extract(ctx, text)
	if err != nil {
		return Result{}, s.fail(ctx, articleID, fmt.Errorf("extract: %w (%w)", ErrLLMUnavailable, err))
	}

	raw, _, closed := llm.StripThink(completion.Text)
	if !closed {
		return Result{}, s.fail(ctx, articleID, fmt.Errorf("think tag truncated: %w", ErrEmptyExtraction))
	}
	if raw == "" {
		return Result{}, s.fail(ctx, articleID, fmt.Errorf("blank after think-strip: %w", ErrEmptyExtraction))
	}

	var payload tagsPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return Result{}, s.fail(ctx, articleID, fmt.Errorf("decode tags json: %w (%w)", ErrInvalidTags, err))
	}

	names := sanitizeTags(payload.Tags)
	if len(names) == 0 {
		return Result{}, s.fail(ctx, articleID, fmt.Errorf("no tags after sanitize: %w", ErrEmptyExtraction))
	}

	persistCtx, cancel := persistContext(ctx)
	defer cancel()
	if err := s.store.UpsertTags(persistCtx, articleID, names, completion.Meta); err != nil {
		return Result{}, s.fail(ctx, articleID, fmt.Errorf("upsert tags %d: %w", articleID, err))
	}

	if attemptErr := s.store.InsertEnrichmentAttempt(persistCtx, articleID, attemptKind, "succeeded", ""); attemptErr != nil {
		s.log.Warn("record enrichment attempt", "article_id", articleID, "err", attemptErr.Error())
	}
	return Result{Tags: names, Created: true}, nil
}

// resolveText は summarize.Service.resolveText と同じ思想: 全文取り寄せ済みならそれを
// 優先し、無ければ呼び出し元が渡した articleContent(フィード由来)にフォールバックする。
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

// persistTimeout は事後永続化(成果・試行イベントの書き込み)のデッドライン
// (summarize.persistTimeout と同値)。
const persistTimeout = 10 * time.Second

// persistContext は生成完了後・失敗確定後の永続化に使う ctx を返す。リクエスト ctx の
// キャンセルを引き継がない(summarize.persistContext と同じ理由 — ADR00014 §7)。
func persistContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
}

// fail は失敗を enrichment_attempts に追記してから、呼び出し元へ返すエラーをそのまま返す。
func (s *Service) fail(ctx context.Context, articleID int64, cause error) error {
	persistCtx, cancel := persistContext(ctx)
	defer cancel()
	if err := s.store.InsertEnrichmentAttempt(persistCtx, articleID, attemptKind, "failed", cause.Error()); err != nil {
		s.log.Warn("record enrichment attempt", "article_id", articleID, "err", err.Error())
	}
	return cause
}
