package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kaikei-e/moka-1/core/internal/feed"
)

// fakeLister は ArticleLister のテストフェイク。
type fakeLister struct {
	list func(ctx context.Context, limit int, cursor *feed.ArticleCursor) ([]feed.Article, error)
}

func (f *fakeLister) ListArticles(ctx context.Context, limit int, cursor *feed.ArticleCursor) ([]feed.Article, error) {
	if f.list == nil {
		return nil, nil
	}
	return f.list(ctx, limit, cursor)
}

func articleAt(id int64, ts time.Time) feed.Article {
	return feed.Article{ID: id, FeedID: 1, GUID: "urn:x", URL: "https://example.com", Title: "t", PublishedAt: &ts}
}

// articleAtCreated は published_at が無い記事(fallback ソートキー = created_at)を組み立てる。
func articleAtCreated(id int64, createdAt time.Time) feed.Article {
	return feed.Article{ID: id, FeedID: 1, GUID: "urn:x", URL: "https://example.com", Title: "t", CreatedAt: createdAt}
}

func TestHandleListArticles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		query      string
		list       func(ctx context.Context, limit int, cursor *feed.ArticleCursor) ([]feed.Article, error)
		wantStatus int
		wantLimit  int
	}{
		{
			name:       "defaults to limit 50 and no cursor",
			query:      "",
			wantStatus: http.StatusOK,
			wantLimit:  50,
		},
		{
			name:       "explicit limit is passed through",
			query:      "?limit=10",
			wantStatus: http.StatusOK,
			wantLimit:  10,
		},
		{
			name:       "limit above cap is clamped to 200",
			query:      "?limit=1000",
			wantStatus: http.StatusOK,
			wantLimit:  200,
		},
		{
			name:       "non-integer limit returns 400",
			query:      "?limit=abc",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "zero limit returns 400",
			query:      "?limit=0",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "garbage cursor returns 400",
			query:      "?cursor=not-a-cursor",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:  "store failure returns 500",
			query: "",
			list: func(_ context.Context, _ int, _ *feed.ArticleCursor) ([]feed.Article, error) {
				return nil, assert.AnError
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotLimit int
			var gotCursor *feed.ArticleCursor
			lister := &fakeLister{list: tt.list}
			if lister.list == nil {
				lister.list = func(_ context.Context, limit int, cursor *feed.ArticleCursor) ([]feed.Article, error) {
					gotLimit, gotCursor = limit, cursor
					return nil, nil
				}
			}

			req := httptest.NewRequestWithContext(t.Context(),
				http.MethodGet, "/api/v1/articles"+tt.query, nil)
			rec := httptest.NewRecorder()
			newTestMux(muxDeps{articles: lister}).ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantStatus == http.StatusOK && tt.list == nil {
				assert.Equal(t, tt.wantLimit, gotLimit)
				assert.Nil(t, gotCursor, "カーソル無しリクエストは nil を渡す")
			}
		})
	}

	t.Run("valid cursor is decoded and passed to the store", func(t *testing.T) {
		t.Parallel()

		ts := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
		cur := feed.ArticleCursor{SortKey: ts, ID: 42}

		var gotCursor *feed.ArticleCursor
		lister := &fakeLister{list: func(_ context.Context, _ int, cursor *feed.ArticleCursor) ([]feed.Article, error) {
			gotCursor = cursor
			return nil, nil
		}}
		req := httptest.NewRequestWithContext(t.Context(),
			http.MethodGet, "/api/v1/articles?cursor="+cur.Encode(), nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{articles: lister}).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.NotNil(t, gotCursor)
		assert.Equal(t, int64(42), gotCursor.ID)
		assert.True(t, gotCursor.SortKey.Equal(ts))
	})

	t.Run("full page carries next_cursor pointing at the last article", func(t *testing.T) {
		t.Parallel()

		ts := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
		lister := &fakeLister{list: func(_ context.Context, _ int, _ *feed.ArticleCursor) ([]feed.Article, error) {
			return []feed.Article{articleAt(3, ts.Add(2*time.Hour)), articleAt(2, ts)}, nil
		}}
		req := httptest.NewRequestWithContext(t.Context(),
			http.MethodGet, "/api/v1/articles?limit=2", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{articles: lister}).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		var got struct {
			NextCursor *string `json:"next_cursor"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.NotNil(t, got.NextCursor, "満杯ページは next_cursor を返す")

		decoded, err := feed.DecodeArticleCursor(*got.NextCursor)
		require.NoError(t, err)
		assert.Equal(t, int64(2), decoded.ID, "next_cursor は最後の記事を指す")
	})

	t.Run("next_cursor falls back to created_at when the last article has no published_at", func(t *testing.T) {
		t.Parallel()

		createdAt := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
		lister := &fakeLister{list: func(_ context.Context, _ int, _ *feed.ArticleCursor) ([]feed.Article, error) {
			return []feed.Article{articleAtCreated(5, createdAt), articleAtCreated(4, createdAt)}, nil
		}}
		req := httptest.NewRequestWithContext(t.Context(),
			http.MethodGet, "/api/v1/articles?limit=2", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{articles: lister}).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		var got struct {
			NextCursor *string `json:"next_cursor"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.NotNil(t, got.NextCursor)

		decoded, err := feed.DecodeArticleCursor(*got.NextCursor)
		require.NoError(t, err)
		assert.Equal(t, int64(4), decoded.ID)
		assert.True(t, decoded.SortKey.Equal(createdAt), "published_at が無い記事は created_at を SortKey にする")
	})

	t.Run("short page ends pagination with a null next_cursor", func(t *testing.T) {
		t.Parallel()

		ts := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
		lister := &fakeLister{list: func(_ context.Context, _ int, _ *feed.ArticleCursor) ([]feed.Article, error) {
			return []feed.Article{articleAt(1, ts)}, nil
		}}
		req := httptest.NewRequestWithContext(t.Context(),
			http.MethodGet, "/api/v1/articles?limit=2", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{articles: lister}).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		var got map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Contains(t, got, "next_cursor", "終端でもキー自体は常に返す(契約を安定させる)")
		assert.Equal(t, "null", string(got["next_cursor"]))
	})

	t.Run("empty result is a JSON array, not null", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(),
			http.MethodGet, "/api/v1/articles", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{}).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"articles": [], "next_cursor": null}`, rec.Body.String())
	})

	t.Run("articles are serialized with snake_case fields", func(t *testing.T) {
		t.Parallel()

		feedTitle := "Example Feed"
		lister := &fakeLister{list: func(_ context.Context, _ int, _ *feed.ArticleCursor) ([]feed.Article, error) {
			return []feed.Article{{
				ID: 7, FeedID: 1, FeedTitle: &feedTitle,
				GUID: "urn:x:7", URL: "https://example.com/7", Title: "Seven", Read: true,
			}}, nil
		}}
		req := httptest.NewRequestWithContext(t.Context(),
			http.MethodGet, "/api/v1/articles", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{articles: lister}).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		var got struct {
			Articles []map[string]any `json:"articles"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Len(t, got.Articles, 1)
		assert.InDelta(t, float64(1), got.Articles[0]["feed_id"], 0)
		assert.Equal(t, "urn:x:7", got.Articles[0]["guid"])
		assert.Contains(t, got.Articles[0], "published_at")
		assert.Equal(t, "Example Feed", got.Articles[0]["feed_title"])
		assert.Equal(t, true, got.Articles[0]["read"])
	})

	t.Run("feed_title is null and read false when derived data is absent", func(t *testing.T) {
		t.Parallel()

		lister := &fakeLister{list: func(_ context.Context, _ int, _ *feed.ArticleCursor) ([]feed.Article, error) {
			return []feed.Article{{ID: 7, FeedID: 1, GUID: "urn:x:7", URL: "https://example.com/7", Title: "Seven"}}, nil
		}}
		req := httptest.NewRequestWithContext(t.Context(),
			http.MethodGet, "/api/v1/articles", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{articles: lister}).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		var got struct {
			Articles []map[string]json.RawMessage `json:"articles"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Len(t, got.Articles, 1)
		assert.Equal(t, "null", string(got.Articles[0]["feed_title"]), "title 未設定フィードは null")
		assert.Equal(t, "false", string(got.Articles[0]["read"]), "未読 = false")
	})
}

// fakeGetter は ArticleGetter のテストフェイク。
type fakeGetter struct {
	get func(ctx context.Context, id int64) (feed.Article, bool, error)
}

func (f *fakeGetter) GetArticle(ctx context.Context, id int64) (feed.Article, bool, error) {
	if f.get == nil {
		return feed.Article{}, false, nil
	}
	return f.get(ctx, id)
}

func TestHandleGetArticle(t *testing.T) {
	t.Parallel()

	t.Run("returns the article wrapped in an article key", func(t *testing.T) {
		t.Parallel()

		feedTitle := "Example Feed"
		getter := &fakeGetter{get: func(_ context.Context, id int64) (feed.Article, bool, error) {
			assert.Equal(t, int64(7), id)
			return feed.Article{
				ID: 7, FeedID: 1, FeedTitle: &feedTitle,
				GUID: "urn:x:7", Title: "Seven", Content: "body", Read: true,
			}, true, nil
		}}
		req := httptest.NewRequestWithContext(t.Context(),
			http.MethodGet, "/api/v1/articles/7", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{article: getter}).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		var got struct {
			Article map[string]any `json:"article"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, "urn:x:7", got.Article["guid"])
		assert.Equal(t, "body", got.Article["content"])
		assert.Equal(t, "Example Feed", got.Article["feed_title"])
		assert.Equal(t, true, got.Article["read"])
	})

	t.Run("unknown id returns 404", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(),
			http.MethodGet, "/api/v1/articles/999999", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
		var got map[string]string
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, "article not found", got["error"])
	})

	t.Run("non-integer id returns 400", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(),
			http.MethodGet, "/api/v1/articles/not-a-number", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("store failure returns 500", func(t *testing.T) {
		t.Parallel()

		getter := &fakeGetter{get: func(_ context.Context, _ int64) (feed.Article, bool, error) {
			return feed.Article{}, false, assert.AnError
		}}
		req := httptest.NewRequestWithContext(t.Context(),
			http.MethodGet, "/api/v1/articles/7", nil)
		rec := httptest.NewRecorder()
		newTestMux(muxDeps{article: getter}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}
