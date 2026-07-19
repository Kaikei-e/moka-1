package tags

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
	latest      []string
	latestFound bool
	latestErr   error
	upsertErr   error
	upserted    [][]string
	attemptErr  error
	attempts    []fakeAttempt
}

type fakeAttempt struct {
	articleID int64
	kind      string
	outcome   string
	errMsg    string
}

func (s *fakeStore) LatestTags(_ context.Context, _ int64) ([]string, bool, error) {
	if s.latestErr != nil {
		return nil, false, s.latestErr
	}
	return s.latest, s.latestFound, nil
}

func (s *fakeStore) UpsertTags(ctx context.Context, _ int64, names []string, _ map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.upsertErr != nil {
		return s.upsertErr
	}
	s.upserted = append(s.upserted, names)
	return nil
}

func (s *fakeStore) InsertEnrichmentAttempt(ctx context.Context, articleID int64, kind, outcome, errMsg string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
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

func (c *fakeCompleter) Extract(_ context.Context, text string) (CompletionResult, error) {
	c.numCalls++
	c.gotText = text
	if c.err != nil {
		return CompletionResult{}, c.err
	}
	return c.result, nil
}

func newTestService(store *fakeStore, ft *fakeFullTexts, comp Completer) *Service {
	return NewService(store, ft, comp, nil)
}

func TestServiceTag(t *testing.T) {
	t.Parallel()

	t.Run("existing tags are idempotent and never call fulltext or llm", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{latest: []string{"既存タグ"}, latestFound: true}
		ft := &fakeFullTexts{}
		comp := &fakeCompleter{}
		res, err := newTestService(store, ft, comp).Tag(t.Context(), 7, "some content")
		require.NoError(t, err)

		assert.False(t, res.Created)
		assert.Equal(t, []string{"既存タグ"}, res.Tags)
		assert.False(t, ft.called, "既存なら全文取り寄せの参照すら不要")
		assert.Zero(t, comp.numCalls, "既存なら llm を叩かない")
	})

	t.Run("prefers fulltext over the passed article content", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		ft := &fakeFullTexts{found: true, text: "全文本体"}
		comp := &fakeCompleter{result: CompletionResult{Text: `{"tags":["タグ1","タグ2"]}`, Meta: map[string]any{"model": "m"}}}
		res, err := newTestService(store, ft, comp).Tag(t.Context(), 7, "フィード由来の短い content")
		require.NoError(t, err)

		assert.True(t, res.Created)
		assert.Equal(t, "全文本体", comp.gotText)
		require.Len(t, store.upserted, 1)
		assert.Equal(t, []string{"タグ1", "タグ2"}, store.upserted[0])
	})

	t.Run("falls back to article content when no fulltext is stored", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		ft := &fakeFullTexts{found: false}
		comp := &fakeCompleter{result: CompletionResult{Text: `{"tags":["タグ"]}`, Meta: map[string]any{}}}
		_, err := newTestService(store, ft, comp).Tag(t.Context(), 7, "フィード由来の content")
		require.NoError(t, err)

		assert.Equal(t, "フィード由来の content", comp.gotText)
	})

	t.Run("no content at all returns ErrNoContent and records a failed attempt", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		ft := &fakeFullTexts{found: false}
		comp := &fakeCompleter{}
		_, err := newTestService(store, ft, comp).Tag(t.Context(), 7, "")
		require.ErrorIs(t, err, ErrNoContent)
		assert.Zero(t, comp.numCalls)

		require.Len(t, store.attempts, 1)
		assert.Equal(t, fakeAttempt{7, "tags", "failed", store.attempts[0].errMsg}, store.attempts[0])
		assert.NotEmpty(t, store.attempts[0].errMsg)
	})

	t.Run("article too long is rejected before calling the llm", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		ft := &fakeFullTexts{}
		comp := &fakeCompleter{}
		huge := strings.Repeat("あ", maxInputTokens/2+1)
		_, err := newTestService(store, ft, comp).Tag(t.Context(), 7, huge)
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
		_, err := newTestService(store, ft, comp).Tag(t.Context(), 7, "content")
		require.ErrorIs(t, err, ErrLLMUnavailable)
		require.Len(t, store.attempts, 1)
		assert.Equal(t, "failed", store.attempts[0].outcome)
	})

	t.Run("think tag is stripped before json decoding", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		ft := &fakeFullTexts{}
		comp := &fakeCompleter{result: CompletionResult{
			Text: "<think>推論過程</think>" + `{"tags":["タグ"]}`,
			Meta: map[string]any{"model": "m"},
		}}
		res, err := newTestService(store, ft, comp).Tag(t.Context(), 7, "content")
		require.NoError(t, err)

		assert.Equal(t, []string{"タグ"}, res.Tags)
		require.Len(t, store.attempts, 1)
		assert.Equal(t, "succeeded", store.attempts[0].outcome)
	})

	t.Run("unclosed think tag yields ErrEmptyExtraction and is recorded as failed", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		ft := &fakeFullTexts{}
		comp := &fakeCompleter{result: CompletionResult{Text: "<think>途中で切れた"}}
		_, err := newTestService(store, ft, comp).Tag(t.Context(), 7, "content")
		require.ErrorIs(t, err, ErrEmptyExtraction)
		require.Len(t, store.attempts, 1)
		assert.Equal(t, "failed", store.attempts[0].outcome)
		assert.Empty(t, store.upserted)
	})

	t.Run("malformed json response yields ErrInvalidTags", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		ft := &fakeFullTexts{}
		comp := &fakeCompleter{result: CompletionResult{Text: "not json"}}
		_, err := newTestService(store, ft, comp).Tag(t.Context(), 7, "content")
		require.ErrorIs(t, err, ErrInvalidTags)
		require.Len(t, store.attempts, 1)
		assert.Equal(t, "failed", store.attempts[0].outcome)
		assert.Empty(t, store.upserted)
	})

	t.Run("blank/duplicate tags are sanitized; all-blank yields ErrEmptyExtraction", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		ft := &fakeFullTexts{}
		comp := &fakeCompleter{result: CompletionResult{Text: `{"tags":["  ",""]}`}}
		_, err := newTestService(store, ft, comp).Tag(t.Context(), 7, "content")
		require.ErrorIs(t, err, ErrEmptyExtraction)
		assert.Empty(t, store.upserted)
	})

	t.Run("duplicate tag names are deduped preserving order", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		ft := &fakeFullTexts{}
		comp := &fakeCompleter{result: CompletionResult{Text: `{"tags":["Go","Go"," Rust "]}`}}
		res, err := newTestService(store, ft, comp).Tag(t.Context(), 7, "content")
		require.NoError(t, err)
		assert.Equal(t, []string{"Go", "Rust"}, res.Tags)
	})

	t.Run("success records a succeeded attempt", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		ft := &fakeFullTexts{}
		comp := &fakeCompleter{result: CompletionResult{Text: `{"tags":["タグ1"]}`, Meta: map[string]any{"model": "m"}}}
		res, err := newTestService(store, ft, comp).Tag(t.Context(), 7, "content")
		require.NoError(t, err)

		assert.True(t, res.Created)
		require.Len(t, store.attempts, 1)
		assert.Equal(t, fakeAttempt{7, "tags", "succeeded", ""}, store.attempts[0])
	})
}

// cancelingCompleter は補完中にリクエスト ctx がキャンセルされた状況(クライアント切断・
// タイムアウト)を再現する Completer(summarize.cancelingCompleter と同じ形)。
type cancelingCompleter struct {
	cancel context.CancelFunc
	result CompletionResult
	err    error
}

func (c *cancelingCompleter) Extract(_ context.Context, _ string) (CompletionResult, error) {
	c.cancel()
	return c.result, c.err
}

func TestServiceTagPersistsAfterDisconnect(t *testing.T) {
	t.Parallel()

	t.Run("failed attempt is recorded even when the request ctx is already canceled", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		store := &fakeStore{}
		comp := &cancelingCompleter{cancel: cancel, err: context.Canceled}
		_, err := newTestService(store, &fakeFullTexts{}, comp).Tag(ctx, 7, "content")
		require.ErrorIs(t, err, ErrLLMUnavailable)

		require.Len(t, store.attempts, 1, "切断でも failed イベントを記録する")
		assert.Equal(t, "failed", store.attempts[0].outcome)
	})

	t.Run("completed tags are saved even when the client disconnected at the end", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		store := &fakeStore{}
		comp := &cancelingCompleter{
			cancel: cancel,
			result: CompletionResult{Text: `{"tags":["完走タグ"]}`, Meta: map[string]any{"model": "m"}},
		}
		res, err := newTestService(store, &fakeFullTexts{}, comp).Tag(ctx, 7, "content")
		require.NoError(t, err, "生成が完走したなら切断されていても保存する")

		assert.True(t, res.Created)
		require.Len(t, store.upserted, 1)
		assert.Equal(t, []string{"完走タグ"}, store.upserted[0])
		require.Len(t, store.attempts, 1)
		assert.Equal(t, "succeeded", store.attempts[0].outcome)
	})
}
