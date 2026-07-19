package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kaikei-e/moka-1/core/internal/feed"
	"github.com/Kaikei-e/moka-1/core/internal/rag"
)

// fakeAnswerer は ArticleAnswerer のテストフェイク。
type fakeAnswerer struct {
	ask         func(ctx context.Context, article feed.Article, question string, onSources func([]rag.Source), onDelta func(string)) (rag.AnswerResult, error)
	gotArticle  int64
	gotQuestion string
}

func (f *fakeAnswerer) Ask(
	ctx context.Context, article feed.Article, question string,
	onSources func([]rag.Source), onDelta func(string),
) (rag.AnswerResult, error) {
	f.gotArticle, f.gotQuestion = article.ID, question
	if f.ask == nil {
		onSources(nil)
		return rag.AnswerResult{}, nil
	}
	return f.ask(ctx, article, question, onSources, onDelta)
}

func askRequest(t *testing.T, path, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestHandleAskArticle(t *testing.T) {
	t.Parallel()

	t.Run("streams sources, deltas, then done in SSE order", func(t *testing.T) {
		t.Parallel()

		getter := &fakeGetter{get: func(_ context.Context, id int64) (feed.Article, bool, error) {
			return feed.Article{ID: id, Title: "対象記事", Content: "本文"}, true, nil
		}}
		answerer := &fakeAnswerer{ask: func(
			_ context.Context, _ feed.Article, _ string, onSources func([]rag.Source), onDelta func(string),
		) (rag.AnswerResult, error) {
			onSources([]rag.Source{{ID: 3, Title: "文脈記事", URL: "https://example.com/ctx"}})
			onDelta("回答")
			onDelta("本文")
			return rag.AnswerResult{QuestionID: 11, AnswerID: 22}, nil
		}}
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{article: getter, answerer: answerer}).ServeHTTP(rec,
			askRequest(t, "/api/v1/articles/7/qa", `{"question":"これは何の話?"}`))

		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
		assert.Equal(t, int64(7), answerer.gotArticle)
		assert.Equal(t, "これは何の話?", answerer.gotQuestion)

		body := rec.Body.String()
		assert.Contains(t, body,
			"event: sources\ndata: {\"articles\":[{\"id\":3,\"title\":\"文脈記事\",\"url\":\"https://example.com/ctx\"}]}\n\n")
		assert.Contains(t, body, "event: delta\ndata: {\"text\":\"回答\"}\n\n")
		assert.Contains(t, body, "event: delta\ndata: {\"text\":\"本文\"}\n\n")
		assert.Contains(t, body, "event: done\ndata: {\"answer_id\":22,\"question_id\":11}\n\n")

		sourcesAt := strings.Index(body, "event: sources")
		deltaAt := strings.Index(body, "event: delta")
		doneAt := strings.Index(body, "event: done")
		assert.True(t, sourcesAt < deltaAt && deltaAt < doneAt, "sources → delta → done の順")
	})

	t.Run("no context articles still emits a sources event with an empty array", func(t *testing.T) {
		t.Parallel()

		getter := &fakeGetter{get: func(_ context.Context, id int64) (feed.Article, bool, error) {
			return feed.Article{ID: id, Content: "本文"}, true, nil
		}}
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{article: getter, answerer: &fakeAnswerer{}}).ServeHTTP(rec,
			askRequest(t, "/api/v1/articles/7/qa", `{"question":"q"}`))

		assert.Contains(t, rec.Body.String(), "event: sources\ndata: {\"articles\":[]}\n\n")
	})

	t.Run("failure emits an error event instead of a done event", func(t *testing.T) {
		t.Parallel()

		getter := &fakeGetter{get: func(_ context.Context, id int64) (feed.Article, bool, error) {
			return feed.Article{ID: id, Content: "本文"}, true, nil
		}}
		answerer := &fakeAnswerer{ask: func(
			_ context.Context, _ feed.Article, _ string, onSources func([]rag.Source), onDelta func(string),
		) (rag.AnswerResult, error) {
			onSources(nil)
			onDelta("途中まで")
			return rag.AnswerResult{}, fmt.Errorf("down: %w", rag.ErrLLMUnavailable)
		}}
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{article: getter, answerer: answerer}).ServeHTTP(rec,
			askRequest(t, "/api/v1/articles/7/qa", `{"question":"q"}`))

		assert.Equal(t, http.StatusOK, rec.Code) // ヘッダ送出済みなのでHTTPステータスはOKのまま
		body := rec.Body.String()
		assert.Contains(t, body, "event: error\ndata: {\"message\":\"llm unavailable\"}\n\n")
		assert.NotContains(t, body, "event: done\n")
	})

	t.Run("empty answer maps to a dedicated error message", func(t *testing.T) {
		t.Parallel()

		getter := &fakeGetter{get: func(_ context.Context, id int64) (feed.Article, bool, error) {
			return feed.Article{ID: id, Content: "本文"}, true, nil
		}}
		answerer := &fakeAnswerer{ask: func(
			_ context.Context, _ feed.Article, _ string, _ func([]rag.Source), _ func(string),
		) (rag.AnswerResult, error) {
			return rag.AnswerResult{}, fmt.Errorf("blank: %w", rag.ErrEmptyAnswer)
		}}
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{article: getter, answerer: answerer}).ServeHTTP(rec,
			askRequest(t, "/api/v1/articles/7/qa", `{"question":"q"}`))

		assert.Contains(t, rec.Body.String(),
			"event: error\ndata: {\"message\":\"answer generation produced no content\"}\n\n")
	})

	t.Run("unknown error maps to internal error message", func(t *testing.T) {
		t.Parallel()

		getter := &fakeGetter{get: func(_ context.Context, id int64) (feed.Article, bool, error) {
			return feed.Article{ID: id, Content: "本文"}, true, nil
		}}
		answerer := &fakeAnswerer{ask: func(
			_ context.Context, _ feed.Article, _ string, _ func([]rag.Source), _ func(string),
		) (rag.AnswerResult, error) {
			return rag.AnswerResult{}, assert.AnError
		}}
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{article: getter, answerer: answerer}).ServeHTTP(rec,
			askRequest(t, "/api/v1/articles/7/qa", `{"question":"q"}`))

		assert.Contains(t, rec.Body.String(), "event: error\ndata: {\"message\":\"internal error\"}\n\n")
	})

	t.Run("empty question returns a plain 400 without calling the answerer", func(t *testing.T) {
		t.Parallel()

		getter := &fakeGetter{get: func(_ context.Context, id int64) (feed.Article, bool, error) {
			return feed.Article{ID: id, Content: "本文"}, true, nil
		}}
		for _, body := range []string{`{"question":""}`, `{"question":"   "}`, `{}`} {
			answerer := &fakeAnswerer{}
			rec := httptest.NewRecorder()
			newTestMux(muxDeps{article: getter, answerer: answerer}).ServeHTTP(rec,
				askRequest(t, "/api/v1/articles/7/qa", body))

			assert.Equal(t, http.StatusBadRequest, rec.Code, "body=%s", body)
			assert.Zero(t, answerer.gotArticle)
		}
	})

	t.Run("malformed json body returns 400", func(t *testing.T) {
		t.Parallel()

		getter := &fakeGetter{get: func(_ context.Context, id int64) (feed.Article, bool, error) {
			return feed.Article{ID: id, Content: "本文"}, true, nil
		}}
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{article: getter}).ServeHTTP(rec,
			askRequest(t, "/api/v1/articles/7/qa", `not json`))

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("unknown article returns a plain 404 before entering SSE mode", func(t *testing.T) {
		t.Parallel()

		answerer := &fakeAnswerer{}
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{answerer: answerer}).ServeHTTP(rec,
			askRequest(t, "/api/v1/articles/999999/qa", `{"question":"q"}`))

		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.NotEqual(t, "text/event-stream", rec.Header().Get("Content-Type"))
		assert.Zero(t, answerer.gotArticle)
	})

	t.Run("non-integer id returns 400", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		newTestMux(muxDeps{}).ServeHTTP(rec,
			askRequest(t, "/api/v1/articles/not-a-number/qa", `{"question":"q"}`))

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("non-POST method returns 405", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/articles/7/qa", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	})
}
