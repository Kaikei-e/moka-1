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
	"github.com/Kaikei-e/moka-1/core/internal/fulltext"
)

// fakeFullTextFetcher は FullTextFetcher のテストフェイク。
type fakeFullTextFetcher struct {
	fetch      func(ctx context.Context, articleID int64, articleURL string) (fulltext.Result, error)
	gotURL     string
	gotArticle int64
}

func (f *fakeFullTextFetcher) FetchFullText(ctx context.Context, articleID int64, articleURL string) (fulltext.Result, error) {
	f.gotArticle, f.gotURL = articleID, articleURL
	if f.fetch == nil {
		return fulltext.Result{}, nil
	}
	return f.fetch(ctx, articleID, articleURL)
}

func TestHandleFetchFullText(t *testing.T) {
	t.Parallel()

	t.Run("new fulltext returns 201 and looks up the article url", func(t *testing.T) {
		t.Parallel()

		getter := &fakeGetter{get: func(_ context.Context, id int64) (feed.Article, bool, error) {
			return feed.Article{ID: id, URL: "https://example.com/a"}, true, nil
		}}
		fetcher := &fakeFullTextFetcher{fetch: func(_ context.Context, id int64, _ string) (fulltext.Result, error) {
			return fulltext.Result{FullText: fulltext.FullText{ArticleID: id, Text: "全文"}, Created: true}, nil
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/articles/7/fulltext", nil)
		rec := httptest.NewRecorder()
		NewMux(&fakeRegistrar{}, &fakeFeedLister{}, &fakeLister{}, getter, fetcher).ServeHTTP(rec, req)

		require.Equal(t, http.StatusCreated, rec.Code)
		assert.Equal(t, int64(7), fetcher.gotArticle)
		assert.Equal(t, "https://example.com/a", fetcher.gotURL)

		var got struct {
			FullText map[string]any `json:"fulltext"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, "全文", got.FullText["text"])
	})

	t.Run("already stored returns 200", func(t *testing.T) {
		t.Parallel()

		getter := &fakeGetter{get: func(_ context.Context, id int64) (feed.Article, bool, error) {
			return feed.Article{ID: id, URL: "https://example.com/a"}, true, nil
		}}
		fetcher := &fakeFullTextFetcher{fetch: func(_ context.Context, id int64, _ string) (fulltext.Result, error) {
			return fulltext.Result{FullText: fulltext.FullText{ArticleID: id, Text: "既存"}, Created: false}, nil
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/articles/7/fulltext", nil)
		rec := httptest.NewRecorder()
		NewMux(&fakeRegistrar{}, &fakeFeedLister{}, &fakeLister{}, getter, fetcher).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("unknown article returns 404 without calling the fetcher", func(t *testing.T) {
		t.Parallel()

		fetcher := &fakeFullTextFetcher{}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/articles/999999/fulltext", nil)
		rec := httptest.NewRecorder()
		NewMux(&fakeRegistrar{}, &fakeFeedLister{}, &fakeLister{}, &fakeGetter{}, fetcher).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Zero(t, fetcher.gotArticle)
	})

	t.Run("non-integer id returns 400", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/articles/not-a-number/fulltext", nil)
		rec := httptest.NewRecorder()
		NewMux(&fakeRegistrar{}, &fakeFeedLister{}, &fakeLister{}, &fakeGetter{}, &fakeFullTextFetcher{}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("article lookup failure returns 500", func(t *testing.T) {
		t.Parallel()

		getter := &fakeGetter{get: func(_ context.Context, _ int64) (feed.Article, bool, error) {
			return feed.Article{}, false, assert.AnError
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/articles/7/fulltext", nil)
		rec := httptest.NewRecorder()
		NewMux(&fakeRegistrar{}, &fakeFeedLister{}, &fakeLister{}, getter, &fakeFullTextFetcher{}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"invalid url maps to 400", fmt.Errorf("bad: %w", fulltext.ErrInvalidURL), http.StatusBadRequest},
		{"private host maps to 400", fmt.Errorf("private: %w", fulltext.ErrPrivateHost), http.StatusBadRequest},
		{"extract failure maps to 422", fmt.Errorf("empty: %w", fulltext.ErrExtractFailed), http.StatusUnprocessableEntity},
		{"upstream fetch failure maps to 502", fmt.Errorf("boom: %w", fulltext.ErrUpstreamFetch), http.StatusBadGateway},
		{"unknown error maps to 500", assert.AnError, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			getter := &fakeGetter{get: func(_ context.Context, id int64) (feed.Article, bool, error) {
				return feed.Article{ID: id, URL: "https://example.com/a"}, true, nil
			}}
			fetcher := &fakeFullTextFetcher{fetch: func(_ context.Context, _ int64, _ string) (fulltext.Result, error) {
				return fulltext.Result{}, tt.err
			}}
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/articles/7/fulltext", nil)
			rec := httptest.NewRecorder()
			NewMux(&fakeRegistrar{}, &fakeFeedLister{}, &fakeLister{}, getter, fetcher).ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}
