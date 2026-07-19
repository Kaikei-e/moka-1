package rag

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kaikei-e/moka-1/core/internal/fulltext"
)

// fakeEmbeddingStore は EmbeddingStore ポートのフェイク。
type fakeEmbeddingStore struct {
	insertErr    error
	gotVector    []float32
	gotModel     string
	gotArticleID int64
	attempts     []string // "kind/outcome" の列
	attemptErrs  []string
}

func (f *fakeEmbeddingStore) InsertEmbedding(_ context.Context, articleID int64, vector []float32, model string) error {
	if f.insertErr != nil {
		return f.insertErr
	}
	f.gotArticleID, f.gotVector, f.gotModel = articleID, vector, model
	return nil
}

func (f *fakeEmbeddingStore) InsertEnrichmentAttempt(_ context.Context, _ int64, kind, outcome, errMsg string) error {
	f.attempts = append(f.attempts, kind+"/"+outcome)
	f.attemptErrs = append(f.attemptErrs, errMsg)
	return nil
}

// fakeFullTextLookup は FullTextLookup ポートのフェイク。
type fakeFullTextLookup struct {
	text  string
	found bool
	err   error
}

func (f *fakeFullTextLookup) LatestFullText(_ context.Context, articleID int64) (fulltext.FullText, bool, error) {
	if f.err != nil {
		return fulltext.FullText{}, false, f.err
	}
	return fulltext.FullText{ArticleID: articleID, Text: f.text}, f.found, nil
}

// fakeDocEmbedder は DocumentEmbedder ポートのフェイク。
type fakeDocEmbedder struct {
	vec      []float32
	model    string
	err      error
	gotInput string
}

func (f *fakeDocEmbedder) EmbedDocument(_ context.Context, text string) ([]float32, string, error) {
	f.gotInput = text
	if f.err != nil {
		return nil, "", f.err
	}
	return f.vec, f.model, nil
}

func TestEmbedServiceEmbedArticle(t *testing.T) {
	t.Parallel()

	t.Run("embeds title plus feed content and records a succeeded attempt", func(t *testing.T) {
		t.Parallel()

		store := &fakeEmbeddingStore{}
		embedder := &fakeDocEmbedder{vec: []float32{0.1, 0.2}, model: "qwen3-embedding-0.6b"}
		s := NewEmbedService(store, &fakeFullTextLookup{}, embedder, discardLogger())

		err := s.EmbedArticle(t.Context(), 7, "タイトル", "本文")
		require.NoError(t, err)

		assert.Equal(t, "タイトル\n本文", embedder.gotInput, "eval と同じ title+\\n+text 形式・文書側は素のまま")
		assert.Equal(t, int64(7), store.gotArticleID)
		assert.Equal(t, []float32{0.1, 0.2}, store.gotVector)
		assert.Equal(t, "qwen3-embedding-0.6b", store.gotModel, "モデル名を article_embeddings.model に記録")
		assert.Equal(t, []string{"embedding/succeeded"}, store.attempts)
	})

	t.Run("prefers the latest fulltext over feed content", func(t *testing.T) {
		t.Parallel()

		embedder := &fakeDocEmbedder{vec: []float32{1}, model: "m"}
		s := NewEmbedService(&fakeEmbeddingStore{}, &fakeFullTextLookup{text: "取り寄せた全文", found: true}, embedder, discardLogger())

		require.NoError(t, s.EmbedArticle(t.Context(), 7, "タイトル", "フィード本文"))
		assert.Equal(t, "タイトル\n取り寄せた全文", embedder.gotInput)
	})

	t.Run("truncates the body to the rune budget, keeping the title intact", func(t *testing.T) {
		t.Parallel()

		embedder := &fakeDocEmbedder{vec: []float32{1}, model: "m"}
		s := NewEmbedService(&fakeEmbeddingStore{}, &fakeFullTextLookup{}, embedder, discardLogger())

		long := strings.Repeat("あ", embedBodyMaxRunes+100)
		require.NoError(t, s.EmbedArticle(t.Context(), 7, "タイトル", long))

		want := utf8.RuneCountInString("タイトル\n") + embedBodyMaxRunes
		assert.Equal(t, want, utf8.RuneCountInString(embedder.gotInput))
	})

	t.Run("empty content still embeds the title alone (no permanent-failure loop)", func(t *testing.T) {
		t.Parallel()

		store := &fakeEmbeddingStore{}
		embedder := &fakeDocEmbedder{vec: []float32{1}, model: "m"}
		s := NewEmbedService(store, &fakeFullTextLookup{}, embedder, discardLogger())

		require.NoError(t, s.EmbedArticle(t.Context(), 7, "タイトルだけ", ""))
		assert.Equal(t, "タイトルだけ\n", embedder.gotInput)
		assert.Equal(t, []string{"embedding/succeeded"}, store.attempts)
	})

	t.Run("embedder failure records a failed attempt and wraps ErrLLMUnavailable", func(t *testing.T) {
		t.Parallel()

		store := &fakeEmbeddingStore{}
		embedder := &fakeDocEmbedder{err: assert.AnError}
		s := NewEmbedService(store, &fakeFullTextLookup{}, embedder, discardLogger())

		err := s.EmbedArticle(t.Context(), 7, "タイトル", "本文")
		require.ErrorIs(t, err, ErrLLMUnavailable)
		assert.Equal(t, []string{"embedding/failed"}, store.attempts)
		assert.Empty(t, store.gotVector, "失敗時は成果を保存しない")
	})

	t.Run("insert failure records a failed attempt without an llm sentinel", func(t *testing.T) {
		t.Parallel()

		store := &fakeEmbeddingStore{insertErr: assert.AnError}
		s := NewEmbedService(store, &fakeFullTextLookup{}, &fakeDocEmbedder{vec: []float32{1}, model: "m"}, discardLogger())

		err := s.EmbedArticle(t.Context(), 7, "タイトル", "本文")
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrLLMUnavailable)
		assert.Equal(t, []string{"embedding/failed"}, store.attempts)
	})

	t.Run("fulltext lookup failure records a failed attempt", func(t *testing.T) {
		t.Parallel()

		store := &fakeEmbeddingStore{}
		s := NewEmbedService(store, &fakeFullTextLookup{err: assert.AnError}, &fakeDocEmbedder{vec: []float32{1}}, discardLogger())

		err := s.EmbedArticle(t.Context(), 7, "タイトル", "本文")
		require.Error(t, err)
		assert.Equal(t, []string{"embedding/failed"}, store.attempts)
	})
}
