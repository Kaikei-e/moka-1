package rag

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kaikei-e/moka-1/core/internal/feed"
)

// fakeTextSearcher / fakeVectorSearcher / fakeQueryEmbedder は Searcher の各ポートのフェイク。
type fakeTextSearcher struct {
	hits     []feed.Article
	err      error
	gotQuery string
	gotLimit int
}

func (f *fakeTextSearcher) SearchArticlesByText(_ context.Context, q string, limit int) ([]feed.Article, error) {
	f.gotQuery, f.gotLimit = q, limit
	return f.hits, f.err
}

type fakeVectorSearcher struct {
	hits      []feed.Article
	err       error
	called    bool
	gotVector []float32
	gotLimit  int
}

func (f *fakeVectorSearcher) SearchArticlesByVector(_ context.Context, vec []float32, limit int) ([]feed.Article, error) {
	f.called, f.gotVector, f.gotLimit = true, vec, limit
	return f.hits, f.err
}

type fakeQueryEmbedder struct {
	vec    []float32
	err    error
	called bool
	gotQ   string
}

func (f *fakeQueryEmbedder) EmbedQuery(_ context.Context, q string) ([]float32, error) {
	f.called, f.gotQ = true, q
	return f.vec, f.err
}

func articles(ids ...int64) []feed.Article {
	out := make([]feed.Article, 0, len(ids))
	for _, id := range ids {
		out = append(out, feed.Article{ID: id, Title: "記事"})
	}
	return out
}

func hitIDs(hits []SearchHit) []int64 {
	out := make([]int64, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.ID)
	}
	return out
}

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestSearcherSearch(t *testing.T) {
	t.Parallel()

	t.Run("fuses text and vector ranks with RRF k=60", func(t *testing.T) {
		t.Parallel()

		text := &fakeTextSearcher{hits: articles(1, 2, 3)}
		vec := &fakeVectorSearcher{hits: articles(2, 3, 4)}
		embed := &fakeQueryEmbedder{vec: []float32{0.1}}
		s := NewSearcher(text, vec, embed, discardLogger())

		hits, err := s.Search(t.Context(), "クエリ", 10)
		require.NoError(t, err)

		// RRF: score(2)=1/62+1/61, score(3)=1/63+1/62, score(1)=1/61, score(4)=1/62
		assert.Equal(t, []int64{2, 3, 1, 4}, hitIDs(hits))
		require.Len(t, hits, 4)
		assert.InDelta(t, 1.0/62+1.0/61, hits[0].Score, 1e-9)
		assert.InDelta(t, 1.0/61, hits[2].Score, 1e-9)

		assert.Equal(t, "クエリ", text.gotQuery)
		assert.Equal(t, "クエリ", embed.gotQ)
		assert.Equal(t, []float32{0.1}, vec.gotVector)
		assert.Equal(t, 10, text.gotLimit)
		assert.Equal(t, 10, vec.gotLimit)
	})

	t.Run("caps the fused result at limit", func(t *testing.T) {
		t.Parallel()

		text := &fakeTextSearcher{hits: articles(1, 2)}
		vec := &fakeVectorSearcher{hits: articles(3, 4)}
		s := NewSearcher(text, vec, &fakeQueryEmbedder{vec: []float32{1}}, discardLogger())

		hits, err := s.Search(t.Context(), "q", 3)
		require.NoError(t, err)
		assert.Len(t, hits, 3)
	})

	t.Run("embed failure degrades to text-only without an error (fail-soft)", func(t *testing.T) {
		t.Parallel()

		text := &fakeTextSearcher{hits: articles(1, 2)}
		vec := &fakeVectorSearcher{hits: articles(9)}
		embed := &fakeQueryEmbedder{err: assert.AnError}
		s := NewSearcher(text, vec, embed, discardLogger())

		hits, err := s.Search(t.Context(), "q", 10)
		require.NoError(t, err, "llm 停止時も検索は 200 を返し続ける")
		assert.Equal(t, []int64{1, 2}, hitIDs(hits))
		assert.False(t, vec.called, "クエリ埋め込みが無ければベクトル側は引かない")
	})

	t.Run("vector search failure degrades to text-only without an error", func(t *testing.T) {
		t.Parallel()

		text := &fakeTextSearcher{hits: articles(1)}
		vec := &fakeVectorSearcher{err: assert.AnError}
		s := NewSearcher(text, vec, &fakeQueryEmbedder{vec: []float32{1}}, discardLogger())

		hits, err := s.Search(t.Context(), "q", 10)
		require.NoError(t, err)
		assert.Equal(t, []int64{1}, hitIDs(hits))
	})

	t.Run("text search failure is an error (DB down is not fail-soft)", func(t *testing.T) {
		t.Parallel()

		text := &fakeTextSearcher{err: assert.AnError}
		s := NewSearcher(text, &fakeVectorSearcher{}, &fakeQueryEmbedder{vec: []float32{1}}, discardLogger())

		_, err := s.Search(t.Context(), "q", 10)
		require.Error(t, err)
	})

	t.Run("no hits from either side returns an empty result", func(t *testing.T) {
		t.Parallel()

		s := NewSearcher(&fakeTextSearcher{}, &fakeVectorSearcher{}, &fakeQueryEmbedder{vec: []float32{1}}, discardLogger())

		hits, err := s.Search(t.Context(), "q", 10)
		require.NoError(t, err)
		assert.Empty(t, hits)
	})
}
