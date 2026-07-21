package rag

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kaikei-e/moka-1/core/internal/feed"
)

// fakeQAStore は QAStore ポートのフェイク。
type fakeQAStore struct {
	questionID   int64
	answerID     int64
	questionErr  error
	answerErr    error
	gotArticleID int64
	gotQuestion  string
	gotAnswer    string
	gotSourceIDs []int64
	answered     bool
}

func (f *fakeQAStore) InsertQuestion(_ context.Context, articleID int64, question string) (int64, error) {
	if f.questionErr != nil {
		return 0, f.questionErr
	}
	f.gotArticleID, f.gotQuestion = articleID, question
	return f.questionID, nil
}

func (f *fakeQAStore) InsertAnswer(_ context.Context, questionID int64, answer string, sourceIDs []int64) (int64, error) {
	if f.answerErr != nil {
		return 0, f.answerErr
	}
	f.answered = true
	f.gotAnswer, f.gotSourceIDs = answer, sourceIDs
	_ = questionID
	return f.answerID, nil
}

// fakeContextSearcher は ContextSearcher ポートのフェイク。
type fakeContextSearcher struct {
	hits     []SearchHit
	err      error
	gotQuery string
	gotLimit int
}

func (f *fakeContextSearcher) Search(_ context.Context, q string, limit int) ([]SearchHit, error) {
	f.gotQuery, f.gotLimit = q, limit
	return f.hits, f.err
}

// fakeAnswerCompleter は AnswerCompleter ポートのフェイク。
type fakeAnswerCompleter struct {
	deltas  []string
	raw     string
	err     error
	gotText string
	called  bool
}

func (f *fakeAnswerCompleter) CompleteStream(
	_ context.Context, text string, onRawDelta func(delta string),
) (string, error) {
	f.called, f.gotText = true, text
	if f.err != nil {
		return "", f.err
	}
	for _, d := range f.deltas {
		onRawDelta(d)
	}
	return f.raw, nil
}

func searchHits(ids ...int64) []SearchHit {
	out := make([]SearchHit, 0, len(ids))
	for _, id := range ids {
		out = append(out, SearchHit{Article: feed.Article{
			ID: id, Title: "文脈記事", URL: "https://example.com/ctx", Content: "文脈本文",
		}})
	}
	return out
}

func targetArticle() feed.Article {
	return feed.Article{ID: 7, Title: "対象記事", URL: "https://example.com/7", Content: "対象本文"}
}

func TestAnswererAsk(t *testing.T) {
	t.Parallel()

	t.Run("records the question, emits sources, streams stripped deltas, then persists the answer", func(t *testing.T) {
		t.Parallel()

		store := &fakeQAStore{questionID: 11, answerID: 22}
		search := &fakeContextSearcher{hits: searchHits(3, 4)}
		complete := &fakeAnswerCompleter{deltas: []string{"回答", "本文"}, raw: "回答本文"}
		s := NewAnswerer(store, &fakeFullTextLookup{}, search, complete, discardLogger())

		var gotSources []Source
		var deltas []string
		res, err := s.Ask(t.Context(), targetArticle(), "これは何の話?",
			func(sources []Source) { gotSources = sources },
			func(d string) { deltas = append(deltas, d) })
		require.NoError(t, err)

		assert.Equal(t, int64(7), store.gotArticleID, "質問は受信時に qa_questions へ")
		assert.Equal(t, "これは何の話?", store.gotQuestion)
		assert.Equal(t, "対象記事\nこれは何の話?", search.gotQuery, "検索クエリは対象記事タイトル+質問(変更1)")

		require.Len(t, gotSources, 2)
		assert.Equal(t, Source{ID: 3, Title: "文脈記事", URL: "https://example.com/ctx"}, gotSources[0])

		assert.Equal(t, []string{"回答", "本文"}, deltas)
		assert.Equal(t, "回答本文", store.gotAnswer)
		assert.Equal(t, []int64{3, 4}, store.gotSourceIDs, "sources = 文脈記事ID配列")
		assert.Equal(t, AnswerResult{QuestionID: 11, AnswerID: 22}, res)
	})

	t.Run("excludes the target article from context and caps at top-k", func(t *testing.T) {
		t.Parallel()

		store := &fakeQAStore{questionID: 1, answerID: 2}
		search := &fakeContextSearcher{hits: searchHits(3, 7, 4, 5, 6, 8, 9)} // 7 = 当該記事
		complete := &fakeAnswerCompleter{raw: "回答"}
		s := NewAnswerer(store, &fakeFullTextLookup{}, search, complete, discardLogger())

		var gotSources []Source
		_, err := s.Ask(t.Context(), targetArticle(), "q", func(sources []Source) { gotSources = sources }, func(string) {})
		require.NoError(t, err)

		ids := make([]int64, 0, len(gotSources))
		for _, src := range gotSources {
			ids = append(ids, src.ID)
		}
		assert.Equal(t, []int64{3, 4, 5, 6, 8}, ids, "当該記事(7)を除いた top-5")
	})

	t.Run("prompt contains the question, the target text (fulltext preferred) and context articles", func(t *testing.T) {
		t.Parallel()

		store := &fakeQAStore{questionID: 1, answerID: 2}
		search := &fakeContextSearcher{hits: searchHits(3)}
		complete := &fakeAnswerCompleter{raw: "回答"}
		fullTexts := &fakeFullTextLookup{text: "取り寄せた全文", found: true}
		s := NewAnswerer(store, fullTexts, search, complete, discardLogger())

		_, err := s.Ask(t.Context(), targetArticle(), "これは何の話?", func([]Source) {}, func(string) {})
		require.NoError(t, err)

		assert.Contains(t, complete.gotText, "これは何の話?")
		assert.Contains(t, complete.gotText, "対象記事")
		assert.Contains(t, complete.gotText, "取り寄せた全文", "全文取り寄せ済みならそちらを優先")
		assert.NotContains(t, complete.gotText, "対象本文")
		assert.Contains(t, complete.gotText, "文脈記事")
		assert.Contains(t, complete.gotText, "文脈本文")
	})

	t.Run("think block is stripped from both the stream and the stored answer", func(t *testing.T) {
		t.Parallel()

		store := &fakeQAStore{questionID: 1, answerID: 2}
		complete := &fakeAnswerCompleter{
			deltas: []string{"<think>推論", "過程</think>", "回答本文"},
			raw:    "<think>推論過程</think>回答本文",
		}
		s := NewAnswerer(store, &fakeFullTextLookup{}, &fakeContextSearcher{}, complete, discardLogger())

		var streamed strings.Builder
		_, err := s.Ask(t.Context(), targetArticle(), "q", func([]Source) {}, func(d string) { streamed.WriteString(d) })
		require.NoError(t, err)

		assert.Equal(t, "回答本文", streamed.String(), "CoT をクライアントへ漏らさない")
		assert.Equal(t, "回答本文", store.gotAnswer, "CoT を保存しない(gpt-oss の reasoning 防御)")
	})

	t.Run("search failure is fail-soft: answers from the target article alone", func(t *testing.T) {
		t.Parallel()

		store := &fakeQAStore{questionID: 1, answerID: 2}
		search := &fakeContextSearcher{err: assert.AnError}
		complete := &fakeAnswerCompleter{raw: "回答"}
		s := NewAnswerer(store, &fakeFullTextLookup{}, search, complete, discardLogger())

		var gotSources []Source
		sourcesCalled := false
		res, err := s.Ask(t.Context(), targetArticle(), "q",
			func(sources []Source) { sourcesCalled, gotSources = true, sources }, func(string) {})
		require.NoError(t, err)

		assert.True(t, sourcesCalled, "文脈ゼロでも sources イベントは送る")
		assert.Empty(t, gotSources)
		assert.Equal(t, AnswerResult{QuestionID: 1, AnswerID: 2}, res)
		assert.Empty(t, store.gotSourceIDs)
	})

	t.Run("completer failure wraps ErrLLMUnavailable and stores no answer", func(t *testing.T) {
		t.Parallel()

		store := &fakeQAStore{questionID: 1}
		complete := &fakeAnswerCompleter{err: assert.AnError}
		s := NewAnswerer(store, &fakeFullTextLookup{}, &fakeContextSearcher{}, complete, discardLogger())

		_, err := s.Ask(t.Context(), targetArticle(), "q", func([]Source) {}, func(string) {})
		require.ErrorIs(t, err, ErrLLMUnavailable)
		assert.False(t, store.answered)
	})

	t.Run("unclosed think tag maps to ErrEmptyAnswer", func(t *testing.T) {
		t.Parallel()

		store := &fakeQAStore{questionID: 1}
		complete := &fakeAnswerCompleter{raw: "<think>閉じない推論"}
		s := NewAnswerer(store, &fakeFullTextLookup{}, &fakeContextSearcher{}, complete, discardLogger())

		_, err := s.Ask(t.Context(), targetArticle(), "q", func([]Source) {}, func(string) {})
		require.ErrorIs(t, err, ErrEmptyAnswer)
		assert.False(t, store.answered)
	})

	t.Run("blank answer after strip maps to ErrEmptyAnswer", func(t *testing.T) {
		t.Parallel()

		store := &fakeQAStore{questionID: 1}
		complete := &fakeAnswerCompleter{raw: "<think>推論だけ</think>"}
		s := NewAnswerer(store, &fakeFullTextLookup{}, &fakeContextSearcher{}, complete, discardLogger())

		_, err := s.Ask(t.Context(), targetArticle(), "q", func([]Source) {}, func(string) {})
		require.ErrorIs(t, err, ErrEmptyAnswer)
	})

	t.Run("question insert failure aborts before searching or completing", func(t *testing.T) {
		t.Parallel()

		store := &fakeQAStore{questionErr: assert.AnError}
		complete := &fakeAnswerCompleter{raw: "回答"}
		s := NewAnswerer(store, &fakeFullTextLookup{}, &fakeContextSearcher{}, complete, discardLogger())

		_, err := s.Ask(t.Context(), targetArticle(), "q", func([]Source) {}, func(string) {})
		require.Error(t, err)
		assert.False(t, complete.called)
	})

	t.Run("answer insert failure surfaces as an error", func(t *testing.T) {
		t.Parallel()

		store := &fakeQAStore{questionID: 1, answerErr: assert.AnError}
		complete := &fakeAnswerCompleter{raw: "回答"}
		s := NewAnswerer(store, &fakeFullTextLookup{}, &fakeContextSearcher{}, complete, discardLogger())

		_, err := s.Ask(t.Context(), targetArticle(), "q", func([]Source) {}, func(string) {})
		require.Error(t, err)
	})
}

// TestAnswererSearchContext は文脈検索クエリの組み立て(変更1: タイトル連結)と、
// 弱いヒットの相対閾値フィルタ(変更2)を、searchContext 単体で検証する。
func TestAnswererSearchContext(t *testing.T) {
	t.Parallel()

	t.Run("includes article title in context search query", func(t *testing.T) {
		t.Parallel()

		search := &fakeContextSearcher{hits: searchHits(3, 4)}
		s := NewAnswerer(&fakeQAStore{}, &fakeFullTextLookup{}, search, &fakeAnswerCompleter{}, discardLogger())

		s.searchContext(t.Context(), targetArticle(), "これは何の話?")

		assert.Equal(t, "対象記事\nこれは何の話?", search.gotQuery,
			"クエリは対象記事タイトル+改行+質問(文書側 embedding の title+本文と対称にする)")
	})

	t.Run("drops hits far weaker than the top hit", func(t *testing.T) {
		t.Parallel()

		hits := []SearchHit{
			{Article: feed.Article{ID: 3, Title: "強い文脈", URL: "https://example.com/3"}, Score: 0.02},
			{Article: feed.Article{ID: 4, Title: "そこそこ", URL: "https://example.com/4"}, Score: 0.011}, // 0.02*0.5=0.01 以上 → 残る
			{Article: feed.Article{ID: 5, Title: "弱すぎ", URL: "https://example.com/5"}, Score: 0.005},  // 0.01 未満 → 落ちる
		}
		search := &fakeContextSearcher{hits: hits}
		s := NewAnswerer(&fakeQAStore{}, &fakeFullTextLookup{}, search, &fakeAnswerCompleter{}, discardLogger())

		got := s.searchContext(t.Context(), targetArticle(), "q")

		ids := make([]int64, 0, len(got))
		for _, c := range got {
			ids = append(ids, c.ID)
		}
		assert.Equal(t, []int64{3, 4}, ids, "先頭ヒットの score * 0.5 未満は文脈から落ちる")
	})

	t.Run("keeps only the top hit when every other hit is far weaker", func(t *testing.T) {
		t.Parallel()

		hits := []SearchHit{
			{Article: feed.Article{ID: 3, Title: "唯一の強い文脈", URL: "https://example.com/3"}, Score: 1.0},
			{Article: feed.Article{ID: 4, Title: "弱い", URL: "https://example.com/4"}, Score: 0.1},
			{Article: feed.Article{ID: 5, Title: "もっと弱い", URL: "https://example.com/5"}, Score: 0.01},
		}
		search := &fakeContextSearcher{hits: hits}
		s := NewAnswerer(&fakeQAStore{}, &fakeFullTextLookup{}, search, &fakeAnswerCompleter{}, discardLogger())

		got := s.searchContext(t.Context(), targetArticle(), "q")

		require.Len(t, got, 1)
		assert.Equal(t, int64(3), got[0].ID)
	})

	t.Run("all hits far weaker than the top hit still leaves the top hit itself", func(t *testing.T) {
		t.Parallel()

		// フィルタは先頭ヒット自身には自明に効かない(score >= score*ratio は常に真)。
		// 文脈が完全にゼロ件になるのは検索そのものがヒットゼロ・全滅を返した時のみ
		// (既存のフェイルソフト経路)。
		hits := []SearchHit{
			{Article: feed.Article{ID: 3, Title: "唯一のヒット", URL: "https://example.com/3"}, Score: 0.001},
		}
		search := &fakeContextSearcher{hits: hits}
		s := NewAnswerer(&fakeQAStore{}, &fakeFullTextLookup{}, search, &fakeAnswerCompleter{}, discardLogger())

		got := s.searchContext(t.Context(), targetArticle(), "q")

		require.Len(t, got, 1)
		assert.Equal(t, int64(3), got[0].ID)
	})
}
