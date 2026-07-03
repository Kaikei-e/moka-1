package summarize

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kaikei-e/moka-1/core/internal/fulltext"
)

// fakeStore は Store ポートのインメモリフェイク。呼び出しを記録する。
type fakeStore struct {
	latest      Summary
	latestFound bool
	latestErr   error
	insertErr   error
	inserted    []Summary
	attemptErr  error
	attempts    []fakeAttempt
}

type fakeAttempt struct {
	articleID int64
	kind      string
	outcome   string
	errMsg    string
}

func (s *fakeStore) LatestSummary(_ context.Context, _ int64) (Summary, bool, error) {
	if s.latestErr != nil {
		return Summary{}, false, s.latestErr
	}
	return s.latest, s.latestFound, nil
}

func (s *fakeStore) InsertSummary(_ context.Context, articleID int64, text string, modelMeta map[string]any) (Summary, error) {
	if s.insertErr != nil {
		return Summary{}, s.insertErr
	}
	sum := Summary{ArticleID: articleID, Text: text, ModelMeta: modelMeta}
	s.inserted = append(s.inserted, sum)
	return sum, nil
}

func (s *fakeStore) InsertEnrichmentAttempt(_ context.Context, articleID int64, kind, outcome, errMsg string) error {
	s.attempts = append(s.attempts, fakeAttempt{articleID, kind, outcome, errMsg})
	return s.attemptErr
}

// fakeFullTexts は FullTextLookup ポートのフェイク。
type fakeFullTexts struct {
	text   string
	found  bool
	err    error
	called bool
}

func (f *fakeFullTexts) LatestFullText(_ context.Context, articleID int64) (fulltext.FullText, bool, error) {
	f.called = true
	if f.err != nil {
		return fulltext.FullText{}, false, f.err
	}
	if !f.found {
		return fulltext.FullText{}, false, nil
	}
	return fulltext.FullText{ArticleID: articleID, Text: f.text}, true, nil
}

// fakeCompleter は Completer ポートのフェイク。
type fakeCompleter struct {
	result   CompletionResult
	err      error
	gotText  string
	numCalls int
}

func (c *fakeCompleter) Complete(_ context.Context, text string) (CompletionResult, error) {
	c.numCalls++
	c.gotText = text
	if c.err != nil {
		return CompletionResult{}, c.err
	}
	return c.result, nil
}

func newTestService(store *fakeStore, ft *fakeFullTexts, comp *fakeCompleter) *Service {
	return NewService(store, ft, comp, nil)
}

func TestServiceSummarize(t *testing.T) {
	t.Parallel()

	t.Run("existing summary is idempotent and never calls fulltext or llm", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{latest: Summary{ArticleID: 7, Text: "既存の要約"}, latestFound: true}
		ft := &fakeFullTexts{}
		comp := &fakeCompleter{}
		res, err := newTestService(store, ft, comp).Summarize(t.Context(), 7, "some content")
		require.NoError(t, err)

		assert.False(t, res.Created)
		assert.Equal(t, "既存の要約", res.Summary.Text)
		assert.False(t, ft.called, "既存なら全文取り寄せの参照すら不要")
		assert.Zero(t, comp.numCalls, "既存なら llm を叩かない")
	})

	t.Run("prefers fulltext over the passed article content", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		ft := &fakeFullTexts{found: true, text: "全文本体"}
		comp := &fakeCompleter{result: CompletionResult{Text: "要約結果", Meta: map[string]any{"model": "m"}}}
		res, err := newTestService(store, ft, comp).Summarize(t.Context(), 7, "フィード由来の短い content")
		require.NoError(t, err)

		assert.True(t, res.Created)
		assert.Equal(t, "全文本体", comp.gotText)
		require.Len(t, store.inserted, 1)
		assert.Equal(t, "要約結果", store.inserted[0].Text)
	})

	t.Run("falls back to article content when no fulltext is stored", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		ft := &fakeFullTexts{found: false}
		comp := &fakeCompleter{result: CompletionResult{Text: "要約結果", Meta: map[string]any{}}}
		_, err := newTestService(store, ft, comp).Summarize(t.Context(), 7, "フィード由来の content")
		require.NoError(t, err)

		assert.Equal(t, "フィード由来の content", comp.gotText)
	})

	t.Run("no content at all returns ErrNoContent and records a failed attempt", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		ft := &fakeFullTexts{found: false}
		comp := &fakeCompleter{}
		_, err := newTestService(store, ft, comp).Summarize(t.Context(), 7, "")
		require.ErrorIs(t, err, ErrNoContent)
		assert.Zero(t, comp.numCalls)

		require.Len(t, store.attempts, 1)
		assert.Equal(t, fakeAttempt{7, "summary", "failed", store.attempts[0].errMsg}, store.attempts[0])
		assert.NotEmpty(t, store.attempts[0].errMsg)
	})

	t.Run("article too long is rejected before calling the llm", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		ft := &fakeFullTexts{}
		comp := &fakeCompleter{}
		huge := strings.Repeat("あ", maxInputChars+1)
		_, err := newTestService(store, ft, comp).Summarize(t.Context(), 7, huge)
		require.ErrorIs(t, err, ErrArticleTooLong)
		assert.Zero(t, comp.numCalls, "上限超過なら llm を叩かない")
		require.Len(t, store.attempts, 1)
		assert.Equal(t, "failed", store.attempts[0].outcome)
	})

	t.Run("llm failure is wrapped as ErrLLMUnavailable and recorded", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		ft := &fakeFullTexts{}
		comp := &fakeCompleter{err: assert.AnError}
		_, err := newTestService(store, ft, comp).Summarize(t.Context(), 7, "content")
		require.ErrorIs(t, err, ErrLLMUnavailable)
		require.Len(t, store.attempts, 1)
		assert.Equal(t, "failed", store.attempts[0].outcome)
	})

	t.Run("think tag is stripped and recorded in model_meta", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		ft := &fakeFullTexts{}
		comp := &fakeCompleter{result: CompletionResult{
			Text: "<think>推論過程</think>剥がされた後の要約",
			Meta: map[string]any{"model": "m"},
		}}
		res, err := newTestService(store, ft, comp).Summarize(t.Context(), 7, "content")
		require.NoError(t, err)

		assert.Equal(t, "剥がされた後の要約", res.Summary.Text)
		assert.Equal(t, true, res.Summary.ModelMeta["think_stripped"])
		require.Len(t, store.attempts, 1)
		assert.Equal(t, "succeeded", store.attempts[0].outcome)
	})

	t.Run("unclosed think tag yields ErrEmptyCompletion and is recorded as failed", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		ft := &fakeFullTexts{}
		comp := &fakeCompleter{result: CompletionResult{Text: "<think>途中で切れた"}}
		_, err := newTestService(store, ft, comp).Summarize(t.Context(), 7, "content")
		require.ErrorIs(t, err, ErrEmptyCompletion)
		require.Len(t, store.attempts, 1)
		assert.Equal(t, "failed", store.attempts[0].outcome)
		assert.Empty(t, store.inserted)
	})

	t.Run("success records a succeeded attempt and think_stripped false when no think tag", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		ft := &fakeFullTexts{}
		comp := &fakeCompleter{result: CompletionResult{Text: "素直な要約", Meta: map[string]any{"model": "m"}}}
		res, err := newTestService(store, ft, comp).Summarize(t.Context(), 7, "content")
		require.NoError(t, err)

		assert.True(t, res.Created)
		require.Len(t, store.attempts, 1)
		assert.Equal(t, fakeAttempt{7, "summary", "succeeded", ""}, store.attempts[0])
		assert.Equal(t, false, res.Summary.ModelMeta["think_stripped"])
	})
}
