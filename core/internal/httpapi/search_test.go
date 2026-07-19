package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kaikei-e/moka-1/core/internal/feed"
	"github.com/Kaikei-e/moka-1/core/internal/rag"
)

// fakeSearcher は ArticleSearcher のテストフェイク。
type fakeSearcher struct {
	search   func(ctx context.Context, q string, limit int) ([]rag.SearchHit, error)
	gotQuery string
	gotLimit int
}

func (f *fakeSearcher) Search(ctx context.Context, q string, limit int) ([]rag.SearchHit, error) {
	f.gotQuery, f.gotLimit = q, limit
	if f.search == nil {
		return nil, nil
	}
	return f.search(ctx, q, limit)
}

func TestHandleSearch(t *testing.T) {
	t.Parallel()

	t.Run("returns hits as items with the article shape plus a score", func(t *testing.T) {
		t.Parallel()

		title := "moka feed"
		searcher := &fakeSearcher{search: func(_ context.Context, _ string, _ int) ([]rag.SearchHit, error) {
			return []rag.SearchHit{
				{Article: feed.Article{ID: 7, FeedID: 1, FeedTitle: &title, Title: "記事", URL: "https://example.com/a", Read: true}, Score: 0.05},
			}, nil
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/search?q=llama", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{searcher: searcher}).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "llama", searcher.gotQuery)

		var got struct {
			Items []map[string]any `json:"items"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Len(t, got.Items, 1)
		item := got.Items[0]
		assert.EqualValues(t, 7, item["id"])
		assert.Equal(t, "記事", item["title"])
		assert.Equal(t, "moka feed", item["feed_title"], "一覧APIと同じ記事表現(feed_title込み)")
		assert.Equal(t, true, item["read"], "一覧APIと同じ記事表現(read込み)")
		assert.InDelta(t, 0.05, item["score"], 0.0001)
	})

	t.Run("no results returns an empty items array, not null", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/search?q=nohit", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{searcher: &fakeSearcher{}}).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"items":[]}`, rec.Body.String())
	})

	t.Run("missing q returns 400 without calling the searcher", func(t *testing.T) {
		t.Parallel()

		searcher := &fakeSearcher{}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/search", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{searcher: searcher}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Empty(t, searcher.gotQuery)
	})

	t.Run("whitespace-only q returns 400", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/search?q=%20%20", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{searcher: &fakeSearcher{}}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("default limit is 20", func(t *testing.T) {
		t.Parallel()

		searcher := &fakeSearcher{}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/search?q=x", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{searcher: searcher}).ServeHTTP(rec, req)

		assert.Equal(t, 20, searcher.gotLimit)
	})

	t.Run("limit is capped at 50", func(t *testing.T) {
		t.Parallel()

		searcher := &fakeSearcher{}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/search?q=x&limit=999", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{searcher: searcher}).ServeHTTP(rec, req)

		assert.Equal(t, 50, searcher.gotLimit)
	})

	t.Run("invalid limit returns 400", func(t *testing.T) {
		t.Parallel()

		for _, limit := range []string{"abc", "0", "-1"} {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/search?q=x&limit="+limit, nil)
			rec := httptest.NewRecorder()
			newTestMux(muxDeps{searcher: &fakeSearcher{}}).ServeHTTP(rec, req)

			assert.Equal(t, http.StatusBadRequest, rec.Code, "limit=%s", limit)
		}
	})

	t.Run("searcher failure returns 500", func(t *testing.T) {
		t.Parallel()

		searcher := &fakeSearcher{search: func(_ context.Context, _ string, _ int) ([]rag.SearchHit, error) {
			return nil, assert.AnError
		}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/search?q=x", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{searcher: searcher}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("non-GET method returns 405", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/search?q=x", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	})
}
