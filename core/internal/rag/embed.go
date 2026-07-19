package rag

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Kaikei-e/moka-1/core/internal/fulltext"
)

// attemptKind は enrichment_attempts.kind の値(db/schema.sql の CHECK 制約に合わせる)。
const attemptKind = "embedding"

// embedBodyMaxRunes は埋め込み入力の本文側の文字数上限(rune 単位)。eval/ の retrieval
// 実測(embed.py CORPUS_MAX_CHARS = 6000)と同じ値・同じ「title + 改行 + 本文」形式で、
// タイトルは切り詰めない。超過は黙って切り詰める(要約と違い、部分でも索引価値がある)。
const embedBodyMaxRunes = 6000

// EmbeddingStore は埋め込みユースケースの永続化ポート(消費側定義 — 具象は internal/store)。
type EmbeddingStore interface {
	InsertEmbedding(ctx context.Context, articleID int64, vector []float32, model string) error
	InsertEnrichmentAttempt(ctx context.Context, articleID int64, kind, outcome, errMsg string) error
}

// FullTextLookup は全文取り寄せ済みテキストの参照ポート(summarize.FullTextLookup と同じ形)。
type FullTextLookup interface {
	LatestFullText(ctx context.Context, articleID int64) (fulltext.FullText, bool, error)
}

// DocumentEmbedder は文書側埋め込みの消費側ポート(具象は *LLMEmbedder)。
// 戻り値の model はサーバーが実際に使ったモデル名(article_embeddings.model へ記録)。
type DocumentEmbedder interface {
	EmbedDocument(ctx context.Context, text string) (vector []float32, model string, err error)
}

// EmbedService は記事埋め込みユースケース: 対象テキスト選定(最新 fulltext 優先)→
// 切り詰め → 埋め込み → 保存。enrich.Scheduler が pending 導出(embedding が無い、または
// 最新 fulltext より古い)に基づいて呼ぶ。冪等短絡は持たない — 鮮度判定は pending 導出
// (store.PendingForKind)が単一の正で、呼ばれたら常に埋め込み直す(ADR00002: 追記)。
type EmbedService struct {
	store     EmbeddingStore
	fullTexts FullTextLookup
	embed     DocumentEmbedder
	log       *slog.Logger
}

// NewEmbedService はポートの具象を受け取って埋め込みユースケースを組む(呼び出しは main のみ)。
func NewEmbedService(store EmbeddingStore, fullTexts FullTextLookup, embed DocumentEmbedder, log *slog.Logger) *EmbedService {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &EmbedService{store: store, fullTexts: fullTexts, embed: embed, log: log}
}

// EmbedArticle は articleID の埋め込みを作って追記する。入力は title + "\n" + 本文
// (最新の取り寄せ済み全文があればそれ、なければフィード由来の articleContent)。
// 本文が空でもタイトルだけで埋め込む — 「本文なし」を恒久失敗にするとバックフィルが
// 毎 tick 空振りし続けるため(summarize の ErrNoContent とは逆の判断)。
func (s *EmbedService) EmbedArticle(ctx context.Context, articleID int64, title, articleContent string) error {
	text := articleContent
	if ft, found, err := s.fullTexts.LatestFullText(ctx, articleID); err != nil {
		return s.fail(ctx, articleID, fmt.Errorf("lookup fulltext %d: %w", articleID, err))
	} else if found && ft.Text != "" {
		text = ft.Text
	}

	input := title + "\n" + truncateRunes(text, embedBodyMaxRunes)

	vector, model, err := s.embed.EmbedDocument(ctx, input)
	if err != nil {
		return s.fail(ctx, articleID, fmt.Errorf("embed: %w (%w)", ErrLLMUnavailable, err))
	}

	persistCtx, cancel := persistContext(ctx)
	defer cancel()
	if err := s.store.InsertEmbedding(persistCtx, articleID, vector, model); err != nil {
		return s.fail(ctx, articleID, fmt.Errorf("insert embedding %d: %w", articleID, err))
	}

	if attemptErr := s.store.InsertEnrichmentAttempt(persistCtx, articleID, attemptKind, "succeeded", ""); attemptErr != nil {
		s.log.Warn("record enrichment attempt", "article_id", articleID, "err", attemptErr.Error())
	}
	return nil
}

// fail は失敗を enrichment_attempts に追記してから、呼び出し元へ返すエラーをそのまま返す
// (summarize / tags の fail-soft 作法)。
func (s *EmbedService) fail(ctx context.Context, articleID int64, cause error) error {
	persistCtx, cancel := persistContext(ctx)
	defer cancel()
	if err := s.store.InsertEnrichmentAttempt(persistCtx, articleID, attemptKind, "failed", cause.Error()); err != nil {
		s.log.Warn("record enrichment attempt", "article_id", articleID, "err", err.Error())
	}
	return cause
}
