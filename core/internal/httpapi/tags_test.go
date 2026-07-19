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
	"github.com/Kaikei-e/moka-1/core/internal/tags"
)

// fakeArticleTagger は ArticleTagger のテストフェイク。
type fakeArticleTagger struct {
	tag        func(ctx context.Context, articleID int64, articleContent string) (tags.Result, error)
	gotArticle int64
	gotContent string
}

func (f *fakeArticleTagger) Tag(ctx context.Context, articleID int64, articleContent string) (tags.Result, error) {
	f.gotArticle, f.gotContent = articleID, articleContent
	if f.tag == nil {
		return tags.Result{}, nil
	}
	return f.tag(ctx, articleID, articleContent)
}

// fakeTagsReader は TagsReader のテストフェイク。
type fakeTagsReader struct {
	get func(ctx context.Context, articleID int64) ([]string, bool, error)
}

func (f *fakeTagsReader) LatestTags(ctx context.Context, articleID int64) ([]string, bool, error) {
	if f.get == nil {
		return nil, false, nil
	}
	return f.get(ctx, articleID)
}

func TestHandleTagArticle(t *testing.T) {
	t.Parallel()

	t.Run("new tags returns 201 and passes the article content", func(t *testing.T) {
		t.Parallel()

		getter := &fakeGetter{get: func(_ context.Context, id int64) (feed.Article, bool, error) {
			return feed.Article{ID: id, Content: "本文"}, true, nil
		}}
		tagger := &fakeArticleTagger{tag: func(_ context.Context, _ int64, _ string) (tags.Result, error) {
			return tags.Result{Tags: []string{"タグ1", "タグ2"}, Created: true}, nil
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/articles/7/tags", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{article: getter, tagger: tagger}).ServeHTTP(rec, req)

		require.Equal(t, http.StatusCreated, rec.Code)
		assert.Equal(t, int64(7), tagger.gotArticle)
		assert.Equal(t, "本文", tagger.gotContent)

		var got struct {
			Tags []string `json:"tags"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, []string{"タグ1", "タグ2"}, got.Tags)
	})

	t.Run("already stored returns 200", func(t *testing.T) {
		t.Parallel()

		getter := &fakeGetter{get: func(_ context.Context, id int64) (feed.Article, bool, error) {
			return feed.Article{ID: id, Content: "本文"}, true, nil
		}}
		tagger := &fakeArticleTagger{tag: func(_ context.Context, _ int64, _ string) (tags.Result, error) {
			return tags.Result{Tags: []string{"既存タグ"}, Created: false}, nil
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/articles/7/tags", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{article: getter, tagger: tagger}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("unknown article returns 404 without calling the tagger", func(t *testing.T) {
		t.Parallel()

		tagger := &fakeArticleTagger{}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/articles/999999/tags", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{tagger: tagger}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Zero(t, tagger.gotArticle)
	})

	t.Run("non-integer id returns 400", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/articles/not-a-number/tags", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("article lookup failure returns 500", func(t *testing.T) {
		t.Parallel()

		getter := &fakeGetter{get: func(_ context.Context, _ int64) (feed.Article, bool, error) {
			return feed.Article{}, false, assert.AnError
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/articles/7/tags", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{article: getter}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	errTests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"no content maps to 400", fmt.Errorf("empty: %w", tags.ErrNoContent), http.StatusBadRequest},
		{"too long maps to 400", fmt.Errorf("huge: %w", tags.ErrArticleTooLong), http.StatusBadRequest},
		{"empty extraction maps to 422", fmt.Errorf("blank: %w", tags.ErrEmptyExtraction), http.StatusUnprocessableEntity},
		{"invalid tags maps to 422", fmt.Errorf("bad json: %w", tags.ErrInvalidTags), http.StatusUnprocessableEntity},
		{"llm unavailable maps to 502", fmt.Errorf("down: %w", tags.ErrLLMUnavailable), http.StatusBadGateway},
		{"unknown error maps to 500", assert.AnError, http.StatusInternalServerError},
	}
	for _, tt := range errTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			getter := &fakeGetter{get: func(_ context.Context, id int64) (feed.Article, bool, error) {
				return feed.Article{ID: id, Content: "本文"}, true, nil
			}}
			tagger := &fakeArticleTagger{tag: func(_ context.Context, _ int64, _ string) (tags.Result, error) {
				return tags.Result{}, tt.err
			}}
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/articles/7/tags", nil)
			rec := httptest.NewRecorder()
			newTestMux(muxDeps{article: getter, tagger: tagger}).ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}

func TestHandleGetTags(t *testing.T) {
	t.Parallel()

	t.Run("existing tags returns 200 without touching the llm", func(t *testing.T) {
		t.Parallel()

		getter := &fakeGetter{get: func(_ context.Context, id int64) (feed.Article, bool, error) {
			return feed.Article{ID: id}, true, nil
		}}
		reader := &fakeTagsReader{get: func(_ context.Context, _ int64) ([]string, bool, error) {
			return []string{"保存済みタグ"}, true, nil
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/articles/7/tags", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{article: getter, tagsReader: reader}).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		var got struct {
			Tags []string `json:"tags"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, []string{"保存済みタグ"}, got.Tags)
	})

	t.Run("no tags yet returns 404", func(t *testing.T) {
		t.Parallel()

		getter := &fakeGetter{get: func(_ context.Context, id int64) (feed.Article, bool, error) {
			return feed.Article{ID: id}, true, nil
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/articles/7/tags", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{article: getter}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("unknown article returns 404 without checking tags", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/articles/999999/tags", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("non-integer id returns 400", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/articles/not-a-number/tags", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}
