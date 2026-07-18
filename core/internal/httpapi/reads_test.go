package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Kaikei-e/moka-1/core/internal/feed"
)

// fakeReadMarker は ArticleReadMarker のテストフェイク。
type fakeReadMarker struct {
	mark  func(ctx context.Context, articleID int64) error
	gotID int64
}

func (f *fakeReadMarker) MarkArticleRead(ctx context.Context, articleID int64) error {
	f.gotID = articleID
	if f.mark == nil {
		return nil
	}
	return f.mark(ctx, articleID)
}

func TestHandleMarkArticleRead(t *testing.T) {
	t.Parallel()

	existingGetter := func() *fakeGetter {
		return &fakeGetter{get: func(_ context.Context, id int64) (feed.Article, bool, error) {
			return feed.Article{ID: id}, true, nil
		}}
	}

	t.Run("existing article returns 204 with an empty body", func(t *testing.T) {
		t.Parallel()

		marker := &fakeReadMarker{}
		req := httptest.NewRequestWithContext(t.Context(),
			http.MethodPost, "/api/v1/articles/7/read", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{article: existingGetter(), reads: marker}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNoContent, rec.Code)
		assert.Empty(t, rec.Body.String())
		assert.Equal(t, int64(7), marker.gotID)
	})

	t.Run("already read article still returns 204 (idempotent)", func(t *testing.T) {
		t.Parallel()

		// 冪等 INSERT はストア側の責務(何もしないで nil を返す)— ハンドラは常に 204
		marker := &fakeReadMarker{mark: func(_ context.Context, _ int64) error { return nil }}
		req := httptest.NewRequestWithContext(t.Context(),
			http.MethodPost, "/api/v1/articles/7/read", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{article: existingGetter(), reads: marker}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNoContent, rec.Code)
	})

	t.Run("unknown article returns 404 without calling the marker", func(t *testing.T) {
		t.Parallel()

		marker := &fakeReadMarker{}
		req := httptest.NewRequestWithContext(t.Context(),
			http.MethodPost, "/api/v1/articles/999999/read", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{reads: marker}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Zero(t, marker.gotID)
	})

	t.Run("non-integer id returns 400", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(),
			http.MethodPost, "/api/v1/articles/not-a-number/read", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("article lookup failure returns 500", func(t *testing.T) {
		t.Parallel()

		getter := &fakeGetter{get: func(_ context.Context, _ int64) (feed.Article, bool, error) {
			return feed.Article{}, false, assert.AnError
		}}
		req := httptest.NewRequestWithContext(t.Context(),
			http.MethodPost, "/api/v1/articles/7/read", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{article: getter}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("marker failure returns 500", func(t *testing.T) {
		t.Parallel()

		marker := &fakeReadMarker{mark: func(_ context.Context, _ int64) error { return assert.AnError }}
		req := httptest.NewRequestWithContext(t.Context(),
			http.MethodPost, "/api/v1/articles/7/read", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{article: existingGetter(), reads: marker}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}
