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

func (s *fakeStore) InsertSummary(ctx context.Context, articleID int64, text string, modelMeta map[string]any) (Summary, error) {
	// 実DB(pgx)と同じく、キャンセル済み ctx では書けない
	if err := ctx.Err(); err != nil {
		return Summary{}, err
	}
	if s.insertErr != nil {
		return Summary{}, s.insertErr
	}
	sum := Summary{ArticleID: articleID, Text: text, ModelMeta: modelMeta}
	s.inserted = append(s.inserted, sum)
	return sum, nil
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

	// streamChunks はストリーム版が呼ばれた際に onRawDelta へ順に流す生チャンク。
	// 空なら result.Text をまるごと1チャンクとして流す。
	streamChunks []string
	streamErr    error
	streamCalls  int
}

func (c *fakeCompleter) Complete(_ context.Context, text string) (CompletionResult, error) {
	c.numCalls++
	c.gotText = text
	if c.err != nil {
		return CompletionResult{}, c.err
	}
	return c.result, nil
}

func (c *fakeCompleter) CompleteStream(
	_ context.Context, text string, onRawDelta func(string),
) (CompletionResult, error) {
	c.streamCalls++
	c.gotText = text
	if c.streamErr != nil {
		return CompletionResult{}, c.streamErr
	}
	chunks := c.streamChunks
	if len(chunks) == 0 && c.result.Text != "" {
		chunks = []string{c.result.Text}
	}
	for _, chunk := range chunks {
		onRawDelta(chunk)
	}
	return c.result, nil
}

func newTestService(store *fakeStore, ft *fakeFullTexts, comp Completer) *Service {
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
		// 非ASCII 1 文字 ≒ 2 トークンの保守的見積りなので、予算の半分+1 文字で超過する
		huge := strings.Repeat("あ", maxInputTokens/2+1)
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

// cancelingCompleter は補完中にリクエスト ctx がキャンセルされた状況(クライアント切断・
// タイムアウト)を再現する Completer。
type cancelingCompleter struct {
	cancel context.CancelFunc
	result CompletionResult
	err    error
}

func (c *cancelingCompleter) Complete(_ context.Context, _ string) (CompletionResult, error) {
	c.cancel()
	return c.result, c.err
}

func (c *cancelingCompleter) CompleteStream(
	_ context.Context, _ string, onRawDelta func(string),
) (CompletionResult, error) {
	c.cancel()
	if c.err == nil && c.result.Text != "" {
		onRawDelta(c.result.Text)
	}
	return c.result, c.err
}

func TestServiceSummarizePersistsAfterDisconnect(t *testing.T) {
	t.Parallel()

	t.Run("failed attempt is recorded even when the request ctx is already canceled", func(t *testing.T) {
		t.Parallel()

		// 切断・タイムアウトはまさに failed を記録すべき事象 — キャンセル済み ctx を
		// そのまま使うと insert 自体が失敗して何も残らない(ADR00014 §7 違反)
		ctx, cancel := context.WithCancel(t.Context())
		store := &fakeStore{}
		comp := &cancelingCompleter{cancel: cancel, err: context.Canceled}
		_, err := newTestService(store, &fakeFullTexts{}, comp).Summarize(ctx, 7, "content")
		require.ErrorIs(t, err, ErrLLMUnavailable)

		require.Len(t, store.attempts, 1, "切断でも failed イベントを記録する")
		assert.Equal(t, "failed", store.attempts[0].outcome)
	})

	t.Run("completed summary is saved even when the client disconnected at the end", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		store := &fakeStore{}
		comp := &cancelingCompleter{
			cancel: cancel,
			result: CompletionResult{Text: "完成した要約", Meta: map[string]any{"model": "m"}},
		}
		res, err := newTestService(store, &fakeFullTexts{}, comp).Summarize(ctx, 7, "content")
		require.NoError(t, err, "生成が完走したなら切断されていても保存する")

		assert.True(t, res.Created)
		require.Len(t, store.inserted, 1)
		assert.Equal(t, "完成した要約", store.inserted[0].Text)
		require.Len(t, store.attempts, 1)
		assert.Equal(t, "succeeded", store.attempts[0].outcome)
	})

	t.Run("stream: failed attempt is recorded after mid-stream disconnect", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		store := &fakeStore{}
		comp := &cancelingCompleter{cancel: cancel, err: context.Canceled}
		_, err := newTestService(store, &fakeFullTexts{}, comp).SummarizeStream(ctx, 7, "content", func(string) {})
		require.ErrorIs(t, err, ErrLLMUnavailable)

		require.Len(t, store.attempts, 1)
		assert.Equal(t, "failed", store.attempts[0].outcome)
		assert.Empty(t, store.inserted, "部分テキストは保存しない")
	})
}

func TestServiceSummarizeStream(t *testing.T) {
	t.Parallel()

	t.Run("existing summary is idempotent: emits the whole text once and never calls llm", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{latest: Summary{ArticleID: 7, Text: "既存の要約"}, latestFound: true}
		ft := &fakeFullTexts{}
		comp := &fakeCompleter{}
		var deltas []string
		res, err := newTestService(store, ft, comp).SummarizeStream(t.Context(), 7, "content", func(d string) {
			deltas = append(deltas, d)
		})
		require.NoError(t, err)

		assert.False(t, res.Created)
		assert.Equal(t, []string{"既存の要約"}, deltas)
		assert.Zero(t, comp.streamCalls)
	})

	t.Run("new summary streams deltas as they arrive and saves the final stripped text", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		ft := &fakeFullTexts{}
		comp := &fakeCompleter{
			streamChunks: []string{"要約", "結果です"},
			result:       CompletionResult{Text: "要約結果です", Meta: map[string]any{"model": "m"}},
		}
		var deltas []string
		res, err := newTestService(store, ft, comp).SummarizeStream(t.Context(), 7, "content", func(d string) {
			deltas = append(deltas, d)
		})
		require.NoError(t, err)

		assert.True(t, res.Created)
		// 先頭バッファ方式(<think>判定用に最低 len("<think>") バイト分は内部で溜める)
		// のため、最初の数チャンクは1回のコールバックに統合されうる。連結結果を検証する。
		assert.Equal(t, "要約結果です", strings.Join(deltas, ""))
		require.Len(t, store.inserted, 1)
		assert.Equal(t, "要約結果です", store.inserted[0].Text)
		require.Len(t, store.attempts, 1)
		assert.Equal(t, "succeeded", store.attempts[0].outcome)
	})

	t.Run("think tag is buffered and never leaked to the stream callback", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		ft := &fakeFullTexts{}
		comp := &fakeCompleter{
			streamChunks: []string{"<thi", "nk>推論</thi", "nk>本文の要約"},
			result:       CompletionResult{Text: "<think>推論</think>本文の要約", Meta: map[string]any{"model": "m"}},
		}
		var deltas []string
		res, err := newTestService(store, ft, comp).SummarizeStream(t.Context(), 7, "content", func(d string) {
			deltas = append(deltas, d)
		})
		require.NoError(t, err)

		assert.True(t, res.Created)
		assert.Equal(t, "本文の要約", strings.Join(deltas, ""))
		assert.Equal(t, "本文の要約", res.Summary.Text)
		assert.Equal(t, true, res.Summary.ModelMeta["think_stripped"])
	})

	t.Run("unclosed think tag: nothing streamed, discarded (not saved), recorded as failed", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		ft := &fakeFullTexts{}
		comp := &fakeCompleter{
			streamChunks: []string{"<think>", "途中で切れた推論"},
			result:       CompletionResult{Text: "<think>途中で切れた推論"},
		}
		var deltas []string
		_, err := newTestService(store, ft, comp).SummarizeStream(t.Context(), 7, "content", func(d string) {
			deltas = append(deltas, d)
		})
		require.ErrorIs(t, err, ErrEmptyCompletion)
		assert.Empty(t, deltas)
		assert.Empty(t, store.inserted)
		require.Len(t, store.attempts, 1)
		assert.Equal(t, "failed", store.attempts[0].outcome)
	})

	t.Run("llm stream failure is wrapped as ErrLLMUnavailable, nothing saved", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		ft := &fakeFullTexts{}
		comp := &fakeCompleter{streamErr: assert.AnError}
		_, err := newTestService(store, ft, comp).SummarizeStream(t.Context(), 7, "content", func(string) {})
		require.ErrorIs(t, err, ErrLLMUnavailable)
		assert.Empty(t, store.inserted)
		require.Len(t, store.attempts, 1)
		assert.Equal(t, "failed", store.attempts[0].outcome)
	})

	t.Run("no content at all returns ErrNoContent without touching the llm", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		ft := &fakeFullTexts{found: false}
		comp := &fakeCompleter{}
		_, err := newTestService(store, ft, comp).SummarizeStream(t.Context(), 7, "", func(string) {})
		require.ErrorIs(t, err, ErrNoContent)
		assert.Zero(t, comp.streamCalls)
	})
}
