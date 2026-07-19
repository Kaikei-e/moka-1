// Package enrich は常駐エージェントループ(tenets §3.2)のうち濃縮ステップ(step3)を担う。
// feed.Scheduler が取得(step1-2)を回すのと対称に、enrich.Scheduler は未濃縮記事を定期的に
// 拾って summarize.Service / tags.Service を呼ぶ。取得ロジック・LLM呼び出しロジック自体は
// 持たない(clean-architecture: 消費側 interface にのみ依存)。
package enrich

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Kaikei-e/moka-1/core/internal/feed"
	"github.com/Kaikei-e/moka-1/core/internal/rag"
	"github.com/Kaikei-e/moka-1/core/internal/summarize"
	"github.com/Kaikei-e/moka-1/core/internal/tags"
)

// defaultTick は pending 判定のポーリング間隔の既定値。
const defaultTick = 15 * time.Second

// defaultBatchSize は 1 tick・1 kind あたりに処理する記事数の上限。
const defaultBatchSize = 20

// summaryKind / tagsKind / embeddingKind は enrichment_attempts.kind の値
// (db/schema.sql の CHECK 制約)。embedding の pending 導出だけは attempts でなく
// 成果(article_embeddings)と最新 fulltext の鮮度比較から導く(store.PendingForKind)。
const (
	summaryKind   = "summary"
	tagsKind      = "tags"
	embeddingKind = "embedding"
)

// ArticleGetter は記事本文の参照ポート(具象は *store.Store)。
type ArticleGetter interface {
	GetArticle(ctx context.Context, id int64) (feed.Article, bool, error)
}

// PendingLister は「まだ濃縮されていない記事」一覧の消費側ポート(具象は *store.Store)。
type PendingLister interface {
	PendingForKind(ctx context.Context, kind string, limit int) ([]int64, error)
}

// Summarizer は要約ユースケースの消費側ポート(具象は *summarize.Service)。
type Summarizer interface {
	Summarize(ctx context.Context, articleID int64, articleContent string, force bool) (summarize.Result, error)
}

// Tagger はタグ抽出ユースケースの消費側ポート(具象は *tags.Service)。
type Tagger interface {
	Tag(ctx context.Context, articleID int64, articleContent string) (tags.Result, error)
}

// Embedder は埋め込みユースケースの消費側ポート(具象は *rag.EmbedService)。
type Embedder interface {
	EmbedArticle(ctx context.Context, articleID int64, title, articleContent string) error
}

// Scheduler は常駐エージェントループの step3(tenets §3.2)を担う単一 goroutine の
// ティッカーループ。feed.Scheduler とは独立したティッカーを持つ(取得とは別のペースで回す)。
type Scheduler struct {
	articles  ArticleGetter
	pending   PendingLister
	summarize Summarizer
	tag       Tagger
	embed     Embedder
	tick      time.Duration
	batchSize int
	log       *slog.Logger
}

// NewScheduler はスケジューラを組む。tick が 0 以下なら既定値(15秒)を使う。
func NewScheduler(
	articles ArticleGetter, pending PendingLister, summarizer Summarizer, tagger Tagger,
	embedder Embedder, tick time.Duration, log *slog.Logger,
) *Scheduler {
	if tick <= 0 {
		tick = defaultTick
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Scheduler{
		articles:  articles,
		pending:   pending,
		summarize: summarizer,
		tag:       tagger,
		embed:     embedder,
		tick:      tick,
		batchSize: defaultBatchSize,
		log:       log,
	}
}

// Run は ctx が cancel されるまでブロックする(main が goroutine で起動する)。
func (s *Scheduler) Run(ctx context.Context) {
	s.tickOnce(ctx) // 起動直後に一度濃縮する(再起動後の取りこぼしを溜めない)

	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tickOnce(ctx)
		}
	}
}

// tickOnce は種類単位(全pending summary → 全pending tags → 全pending embedding)で
// 1周ぶん濃縮する。先行の種類で LLM 不調を検知したら残りの種類は試さずこの tick を打ち切る
// (tenets §3.2 step5 バックプレッシャ。状態はメモリ上のみ、次 tick でリセットされる)。
func (s *Scheduler) tickOnce(ctx context.Context) {
	if !s.runKind(ctx, summaryKind, s.summarizeOne) {
		return
	}
	if !s.runKind(ctx, tagsKind, s.tagOne) {
		return
	}
	s.runKind(ctx, embeddingKind, s.embedOne)
}

// runKind は1種類分の pending 記事を処理する。false を返すのは「LLM 不調によりこの tick の
// 残りを打ち切るべき」の合図(ctx cancel も同様に扱う)。
func (s *Scheduler) runKind(
	ctx context.Context, kind string, process func(ctx context.Context, a feed.Article) error,
) bool {
	ids, err := s.pending.PendingForKind(ctx, kind, s.batchSize)
	if err != nil {
		s.log.Error("list pending articles", "kind", kind, "err", err.Error())
		return true // DB 起因の不調は LLM バックプレッシャとは別問題。次 tick でまた試す
	}

	for _, id := range ids {
		if ctx.Err() != nil {
			return false
		}

		a, found, err := s.articles.GetArticle(ctx, id)
		if err != nil {
			s.log.Warn("fetch article for enrichment", "kind", kind, "article_id", id, "err", err.Error())
			continue
		}
		if !found {
			continue
		}

		if err := process(ctx, a); err != nil {
			if isLLMUnavailable(err) {
				s.log.Warn("llm unavailable, skipping rest of tick", "kind", kind, "article_id", id)
				return false
			}
			s.log.Warn("enrichment failed", "kind", kind, "article_id", id, "err", err.Error())
			continue
		}
	}
	return true
}

func (s *Scheduler) summarizeOne(ctx context.Context, a feed.Article) error {
	_, err := s.summarize.Summarize(ctx, a.ID, a.Content, false)
	return err
}

func (s *Scheduler) tagOne(ctx context.Context, a feed.Article) error {
	_, err := s.tag.Tag(ctx, a.ID, a.Content)
	return err
}

func (s *Scheduler) embedOne(ctx context.Context, a feed.Article) error {
	return s.embed.EmbedArticle(ctx, a.ID, a.Title, a.Content)
}

// isLLMUnavailable は summarize/tags/rag いずれの「LLM 不調」sentinel も検知する。
func isLLMUnavailable(err error) bool {
	return errors.Is(err, summarize.ErrLLMUnavailable) ||
		errors.Is(err, tags.ErrLLMUnavailable) ||
		errors.Is(err, rag.ErrLLMUnavailable)
}
