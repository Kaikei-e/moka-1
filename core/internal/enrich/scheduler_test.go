package enrich

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kaikei-e/moka-1/core/internal/feed"
	"github.com/Kaikei-e/moka-1/core/internal/rag"
	"github.com/Kaikei-e/moka-1/core/internal/summarize"
	"github.com/Kaikei-e/moka-1/core/internal/tags"
)

// fakePendingLister は PendingLister ポートのスクリプト化フェイク。
type fakePendingLister struct {
	byKind map[string][]int64
	errFor map[string]error
}

func (f *fakePendingLister) PendingForKind(_ context.Context, kind string, _ int) ([]int64, error) {
	if err := f.errFor[kind]; err != nil {
		return nil, err
	}
	return f.byKind[kind], nil
}

// fakeArticleGetter は ArticleGetter ポートのインメモリフェイク。
type fakeArticleGetter struct {
	articles map[int64]feed.Article
	errFor   map[int64]error
}

func (f *fakeArticleGetter) GetArticle(_ context.Context, id int64) (feed.Article, bool, error) {
	if err := f.errFor[id]; err != nil {
		return feed.Article{}, false, err
	}
	a, found := f.articles[id]
	return a, found, nil
}

// fakeSummarizer/fakeTagger は Summarizer/Tagger ポートのフェイク。呼び出し順を記録する。
type fakeSummarizer struct {
	calledIDs []int64
	errFor    map[int64]error
}

func (f *fakeSummarizer) Summarize(_ context.Context, articleID int64, _ string, _ bool) (summarize.Result, error) {
	f.calledIDs = append(f.calledIDs, articleID)
	if err := f.errFor[articleID]; err != nil {
		return summarize.Result{}, err
	}
	return summarize.Result{Created: true}, nil
}

type fakeTagger struct {
	calledIDs []int64
	errFor    map[int64]error
}

func (f *fakeTagger) Tag(_ context.Context, articleID int64, _ string) (tags.Result, error) {
	f.calledIDs = append(f.calledIDs, articleID)
	if err := f.errFor[articleID]; err != nil {
		return tags.Result{}, err
	}
	return tags.Result{Created: true}, nil
}

// fakeEmbedder は Embedder ポートのフェイク。呼び出し順と入力を記録する。
type fakeEmbedder struct {
	calledIDs []int64
	gotTitle  map[int64]string
	gotBody   map[int64]string
	errFor    map[int64]error
}

func (f *fakeEmbedder) EmbedArticle(_ context.Context, articleID int64, title, articleContent string) error {
	f.calledIDs = append(f.calledIDs, articleID)
	if f.gotTitle == nil {
		f.gotTitle = map[int64]string{}
		f.gotBody = map[int64]string{}
	}
	f.gotTitle[articleID], f.gotBody[articleID] = title, articleContent
	if err := f.errFor[articleID]; err != nil {
		return err
	}
	return nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func articleSet(ids ...int64) map[int64]feed.Article {
	out := make(map[int64]feed.Article, len(ids))
	for _, id := range ids {
		out[id] = feed.Article{ID: id, Content: "content"}
	}
	return out
}

func TestSchedulerTickOnce(t *testing.T) {
	t.Parallel()

	t.Run("kind-unit ordering: summaries, then tags, then embeddings", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			pending := &fakePendingLister{byKind: map[string][]int64{
				summaryKind:   {1, 2},
				tagsKind:      {3, 4},
				embeddingKind: {5, 6},
			}}
			articles := &fakeArticleGetter{articles: articleSet(1, 2, 3, 4, 5, 6)}
			summarizer := &fakeSummarizer{}
			tagger := &fakeTagger{}
			embedder := &fakeEmbedder{}
			s := NewScheduler(articles, pending, summarizer, tagger, embedder, time.Hour, discardLogger())

			s.tickOnce(t.Context())

			assert.Equal(t, []int64{1, 2}, summarizer.calledIDs)
			assert.Equal(t, []int64{3, 4}, tagger.calledIDs)
			assert.Equal(t, []int64{5, 6}, embedder.calledIDs)
		})
	})

	t.Run("embedder receives the article title and feed content", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			pending := &fakePendingLister{byKind: map[string][]int64{embeddingKind: {1}}}
			articles := &fakeArticleGetter{articles: map[int64]feed.Article{
				1: {ID: 1, Title: "タイトル", Content: "本文"},
			}}
			embedder := &fakeEmbedder{}
			s := NewScheduler(articles, pending, &fakeSummarizer{}, &fakeTagger{}, embedder, time.Hour, discardLogger())

			s.tickOnce(t.Context())

			assert.Equal(t, "タイトル", embedder.gotTitle[1])
			assert.Equal(t, "本文", embedder.gotBody[1])
		})
	})

	t.Run("llm unavailable during embedding pass aborts the rest of that pass", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			pending := &fakePendingLister{byKind: map[string][]int64{embeddingKind: {1, 2}}}
			articles := &fakeArticleGetter{articles: articleSet(1, 2)}
			embedder := &fakeEmbedder{errFor: map[int64]error{1: rag.ErrLLMUnavailable}}
			s := NewScheduler(articles, pending, &fakeSummarizer{}, &fakeTagger{}, embedder, time.Hour, discardLogger())

			s.tickOnce(t.Context())

			assert.Equal(t, []int64{1}, embedder.calledIDs, "rag.ErrLLMUnavailable もバックプレッシャとして検知する")
		})
	})

	t.Run("llm unavailable during summary pass skips the tags pass entirely", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			pending := &fakePendingLister{byKind: map[string][]int64{
				summaryKind: {1, 2},
				tagsKind:    {3},
			}}
			articles := &fakeArticleGetter{articles: articleSet(1, 2, 3)}
			summarizer := &fakeSummarizer{errFor: map[int64]error{1: summarize.ErrLLMUnavailable}}
			tagger := &fakeTagger{}
			embedder := &fakeEmbedder{}
			s := NewScheduler(articles, pending, summarizer, tagger, embedder, time.Hour, discardLogger())

			s.tickOnce(t.Context())

			assert.Equal(t, []int64{1}, summarizer.calledIDs, "llm不調を検知した記事以降は同kind内も打ち切る")
			assert.Empty(t, tagger.calledIDs, "次kind(tags)は今回のtickでは一切試さない")
			assert.Empty(t, embedder.calledIDs, "embedding も今回のtickでは一切試さない")
		})
	})

	t.Run("llm unavailable during tags pass does not undo the completed summary pass", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			pending := &fakePendingLister{byKind: map[string][]int64{
				summaryKind: {1},
				tagsKind:    {2, 3},
			}}
			articles := &fakeArticleGetter{articles: articleSet(1, 2, 3)}
			summarizer := &fakeSummarizer{}
			tagger := &fakeTagger{errFor: map[int64]error{2: tags.ErrLLMUnavailable}}
			embedder := &fakeEmbedder{}
			s := NewScheduler(articles, pending, summarizer, tagger, embedder, time.Hour, discardLogger())

			s.tickOnce(t.Context())

			assert.Equal(t, []int64{1}, summarizer.calledIDs)
			assert.Equal(t, []int64{2}, tagger.calledIDs, "tags側で不調を検知したらそこで打ち切る")
			assert.Empty(t, embedder.calledIDs, "embedding は今回のtickでは一切試さない")
		})
	})

	t.Run("non-llm failure is fail-soft: logs and continues to the next article", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			pending := &fakePendingLister{byKind: map[string][]int64{
				summaryKind: {1, 2, 3},
			}}
			articles := &fakeArticleGetter{articles: articleSet(1, 2, 3)}
			summarizer := &fakeSummarizer{errFor: map[int64]error{2: errors.New("too long")}}
			tagger := &fakeTagger{}
			embedder := &fakeEmbedder{}
			s := NewScheduler(articles, pending, summarizer, tagger, embedder, time.Hour, discardLogger())

			s.tickOnce(t.Context())

			assert.Equal(t, []int64{1, 2, 3}, summarizer.calledIDs, "非LLM由来の失敗は次の記事に進む")
		})
	})

	t.Run("PendingForKind failure for one kind does not block the other kind", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			pending := &fakePendingLister{
				byKind: map[string][]int64{tagsKind: {3}},
				errFor: map[string]error{summaryKind: errors.New("db down")},
			}
			articles := &fakeArticleGetter{articles: articleSet(3)}
			summarizer := &fakeSummarizer{}
			tagger := &fakeTagger{}
			embedder := &fakeEmbedder{}
			s := NewScheduler(articles, pending, summarizer, tagger, embedder, time.Hour, discardLogger())

			s.tickOnce(t.Context())

			assert.Empty(t, summarizer.calledIDs)
			assert.Equal(t, []int64{3}, tagger.calledIDs, "DB起因の不調はLLMバックプレッシャと別扱いで次kindへ進む")
		})
	})

	t.Run("article not found (or fetch error) is skipped without aborting the tick", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			pending := &fakePendingLister{byKind: map[string][]int64{summaryKind: {1, 2, 3}}}
			articles := &fakeArticleGetter{
				articles: articleSet(1, 3),
				errFor:   map[int64]error{2: errors.New("boom")},
			}
			summarizer := &fakeSummarizer{}
			tagger := &fakeTagger{}
			embedder := &fakeEmbedder{}
			s := NewScheduler(articles, pending, summarizer, tagger, embedder, time.Hour, discardLogger())

			s.tickOnce(t.Context())

			assert.Equal(t, []int64{1, 3}, summarizer.calledIDs, "id 2 は見つからない/取得失敗なのでスキップ")
		})
	})

	t.Run("stops iterating once the context is cancelled", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			pending := &fakePendingLister{byKind: map[string][]int64{summaryKind: {1, 2}}}
			articles := &fakeArticleGetter{articles: articleSet(1, 2)}
			summarizer := &fakeSummarizer{}
			tagger := &fakeTagger{}
			embedder := &fakeEmbedder{}
			s := NewScheduler(articles, pending, summarizer, tagger, embedder, time.Hour, discardLogger())

			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			s.tickOnce(ctx)

			assert.Empty(t, summarizer.calledIDs, "cancel 済みの ctx では1件も呼ばない")
		})
	})
}

func TestNewSchedulerDefaultsNonPositiveTick(t *testing.T) {
	t.Parallel()

	s := NewScheduler(&fakeArticleGetter{}, &fakePendingLister{}, &fakeSummarizer{}, &fakeTagger{}, &fakeEmbedder{}, 0, discardLogger())
	assert.Equal(t, defaultTick, s.tick)

	s = NewScheduler(&fakeArticleGetter{}, &fakePendingLister{}, &fakeSummarizer{}, &fakeTagger{}, &fakeEmbedder{}, -time.Second, discardLogger())
	assert.Equal(t, defaultTick, s.tick)
}

func TestSchedulerRun(t *testing.T) {
	t.Parallel()

	t.Run("ticks at the configured interval until the context is cancelled", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			pending := &fakePendingLister{byKind: map[string][]int64{summaryKind: {1}}}
			articles := &fakeArticleGetter{articles: articleSet(1)}
			summarizer := &fakeSummarizer{}
			tagger := &fakeTagger{}
			embedder := &fakeEmbedder{}
			s := NewScheduler(articles, pending, summarizer, tagger, embedder, 10*time.Second, discardLogger())

			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan struct{})
			go func() {
				s.Run(ctx)
				close(done)
			}()

			synctest.Wait()
			require.Len(t, summarizer.calledIDs, 1, "起動直後に1回濃縮する")

			time.Sleep(10 * time.Second)
			synctest.Wait()
			require.Len(t, summarizer.calledIDs, 2, "tick 間隔ごとに再度濃縮する")

			cancel()
			synctest.Wait()
			select {
			case <-done:
			default:
				t.Fatal("Run はループ内で ctx cancel を検知して即座に返るはず")
			}
		})
	})
}
