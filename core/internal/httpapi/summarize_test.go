package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kaikei-e/moka-1/core/internal/feed"
	"github.com/Kaikei-e/moka-1/core/internal/summarize"
)

// fakeSummarizer は ArticleSummarizer のテストフェイク。
type fakeSummarizer struct {
	summarize  func(ctx context.Context, articleID int64, articleContent string) (summarize.Result, error)
	gotArticle int64
	gotContent string
}

func (f *fakeSummarizer) Summarize(ctx context.Context, articleID int64, articleContent string) (summarize.Result, error) {
	f.gotArticle, f.gotContent = articleID, articleContent
	if f.summarize == nil {
		return summarize.Result{}, nil
	}
	return f.summarize(ctx, articleID, articleContent)
}

func TestHandleSummarizeArticle(t *testing.T) {
	t.Parallel()

	t.Run("new summary returns 201 and passes the article content", func(t *testing.T) {
		t.Parallel()

		getter := &fakeGetter{get: func(_ context.Context, id int64) (feed.Article, bool, error) {
			return feed.Article{ID: id, Content: "本文"}, true, nil
		}}
		summarizer := &fakeSummarizer{summarize: func(_ context.Context, id int64, _ string) (summarize.Result, error) {
			return summarize.Result{Summary: summarize.Summary{ArticleID: id, Text: "要約"}, Created: true}, nil
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/articles/7/summary", nil)
		rec := httptest.NewRecorder()
		NewMux(&fakeRegistrar{}, &fakeFeedLister{}, &fakeLister{}, getter, &fakeFullTextFetcher{},
			summarizer).ServeHTTP(rec, req)

		require.Equal(t, http.StatusCreated, rec.Code)
		assert.Equal(t, int64(7), summarizer.gotArticle)
		assert.Equal(t, "本文", summarizer.gotContent)

		var got struct {
			Summary map[string]any `json:"summary"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, "要約", got.Summary["text"])
	})

	t.Run("already stored returns 200", func(t *testing.T) {
		t.Parallel()

		getter := &fakeGetter{get: func(_ context.Context, id int64) (feed.Article, bool, error) {
			return feed.Article{ID: id, Content: "本文"}, true, nil
		}}
		summarizer := &fakeSummarizer{summarize: func(_ context.Context, id int64, _ string) (summarize.Result, error) {
			return summarize.Result{Summary: summarize.Summary{ArticleID: id, Text: "既存"}, Created: false}, nil
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/articles/7/summary", nil)
		rec := httptest.NewRecorder()
		NewMux(&fakeRegistrar{}, &fakeFeedLister{}, &fakeLister{}, getter, &fakeFullTextFetcher{},
			summarizer).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("unknown article returns 404 without calling the summarizer", func(t *testing.T) {
		t.Parallel()

		summarizer := &fakeSummarizer{}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/articles/999999/summary", nil)
		rec := httptest.NewRecorder()
		NewMux(&fakeRegistrar{}, &fakeFeedLister{}, &fakeLister{}, &fakeGetter{}, &fakeFullTextFetcher{},
			summarizer).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Zero(t, summarizer.gotArticle)
	})

	t.Run("non-integer id returns 400", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/articles/not-a-number/summary", nil)
		rec := httptest.NewRecorder()
		NewMux(&fakeRegistrar{}, &fakeFeedLister{}, &fakeLister{}, &fakeGetter{}, &fakeFullTextFetcher{},
			&fakeSummarizer{}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("article lookup failure returns 500", func(t *testing.T) {
		t.Parallel()

		getter := &fakeGetter{get: func(_ context.Context, _ int64) (feed.Article, bool, error) {
			return feed.Article{}, false, assert.AnError
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/articles/7/summary", nil)
		rec := httptest.NewRecorder()
		NewMux(&fakeRegistrar{}, &fakeFeedLister{}, &fakeLister{}, getter, &fakeFullTextFetcher{},
			&fakeSummarizer{}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"no content maps to 400", fmt.Errorf("empty: %w", summarize.ErrNoContent), http.StatusBadRequest},
		{"too long maps to 400", fmt.Errorf("huge: %w", summarize.ErrArticleTooLong), http.StatusBadRequest},
		{"empty completion maps to 422", fmt.Errorf("blank: %w", summarize.ErrEmptyCompletion), http.StatusUnprocessableEntity},
		{"llm unavailable maps to 502", fmt.Errorf("down: %w", summarize.ErrLLMUnavailable), http.StatusBadGateway},
		{"unknown error maps to 500", assert.AnError, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			getter := &fakeGetter{get: func(_ context.Context, id int64) (feed.Article, bool, error) {
				return feed.Article{ID: id, Content: "本文"}, true, nil
			}}
			summarizer := &fakeSummarizer{summarize: func(_ context.Context, _ int64, _ string) (summarize.Result, error) {
				return summarize.Result{}, tt.err
			}}
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/articles/7/summary", nil)
			rec := httptest.NewRecorder()
			NewMux(&fakeRegistrar{}, &fakeFeedLister{}, &fakeLister{}, getter, &fakeFullTextFetcher{},
				summarizer).ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}
