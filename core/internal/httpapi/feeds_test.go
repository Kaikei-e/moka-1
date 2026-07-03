package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kaikei-e/moka-1/core/internal/feed"
)

// fakeRegistrar は FeedRegistrar のテストフェイク(関数フィールド差し替え)。
type fakeRegistrar struct {
	register func(ctx context.Context, rawURL string) (feed.RegisterResult, error)
}

func (f *fakeRegistrar) Register(ctx context.Context, rawURL string) (feed.RegisterResult, error) {
	return f.register(ctx, rawURL)
}

func TestHandleRegisterFeed(t *testing.T) {
	t.Parallel()

	created := feed.RegisterResult{
		Feed: feed.Feed{
			ID:        1,
			URL:       "https://example.com/feed.xml",
			Title:     "Example",
			CreatedAt: time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC),
		},
		Created:          true,
		InsertedArticles: 3,
	}

	tests := []struct {
		name       string
		body       string
		register   func(ctx context.Context, rawURL string) (feed.RegisterResult, error)
		wantStatus int
		wantError  string
	}{
		{
			name:       "new feed returns 201 with feed and count",
			body:       `{"url": "https://example.com/feed.xml"}`,
			register:   func(_ context.Context, _ string) (feed.RegisterResult, error) { return created, nil },
			wantStatus: http.StatusCreated,
		},
		{
			name: "existing feed returns 200",
			body: `{"url": "https://example.com/feed.xml"}`,
			register: func(_ context.Context, _ string) (feed.RegisterResult, error) {
				r := created
				r.Created = false
				r.InsertedArticles = 0
				return r, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "malformed json returns 400",
			body:       `{`,
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid request body",
		},
		{
			name:       "empty url returns 400",
			body:       `{"url": "  "}`,
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid feed url",
		},
		{
			name: "invalid url maps to 400",
			body: `{"url": "ftp://example.com/feed"}`,
			register: func(_ context.Context, _ string) (feed.RegisterResult, error) {
				return feed.RegisterResult{}, feed.ErrInvalidURL
			},
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid feed url",
		},
		{
			name: "private host maps to 400 in the same bucket",
			body: `{"url": "http://10.0.0.1/feed"}`,
			register: func(_ context.Context, _ string) (feed.RegisterResult, error) {
				return feed.RegisterResult{}, feed.ErrPrivateHost
			},
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid feed url",
		},
		{
			name: "not a feed maps to 422",
			body: `{"url": "https://example.com/page.html"}`,
			register: func(_ context.Context, _ string) (feed.RegisterResult, error) {
				return feed.RegisterResult{}, feed.ErrNotAFeed
			},
			wantStatus: http.StatusUnprocessableEntity,
			wantError:  "content is not a valid feed",
		},
		{
			name: "upstream failure maps to 502",
			body: `{"url": "https://example.com/feed.xml"}`,
			register: func(_ context.Context, _ string) (feed.RegisterResult, error) {
				return feed.RegisterResult{}, feed.ErrUpstreamFetch
			},
			wantStatus: http.StatusBadGateway,
			wantError:  "upstream fetch failed",
		},
		{
			name: "unknown error maps to 500",
			body: `{"url": "https://example.com/feed.xml"}`,
			register: func(_ context.Context, _ string) (feed.RegisterResult, error) {
				return feed.RegisterResult{}, assert.AnError
			},
			wantStatus: http.StatusInternalServerError,
			wantError:  "internal error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mux := NewMux(&fakeRegistrar{register: tt.register}, &fakeFeedLister{}, &fakeLister{}, &fakeGetter{}, &fakeFullTextFetcher{},
				&fakeSummarizer{})
			req := httptest.NewRequestWithContext(t.Context(),
				http.MethodPost, "/api/v1/feeds", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

			if tt.wantError != "" {
				var got map[string]string
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
				assert.Equal(t, tt.wantError, got["error"])
			}
		})
	}

	t.Run("201 body carries feed and inserted_articles, not created flag", func(t *testing.T) {
		t.Parallel()

		reg := &fakeRegistrar{register: func(_ context.Context, rawURL string) (feed.RegisterResult, error) {
			assert.Equal(t, "https://example.com/feed.xml", rawURL)
			return created, nil
		}}
		req := httptest.NewRequestWithContext(t.Context(),
			http.MethodPost, "/api/v1/feeds", strings.NewReader(`{"url": "https://example.com/feed.xml"}`))
		rec := httptest.NewRecorder()
		NewMux(reg, &fakeFeedLister{}, &fakeLister{}, &fakeGetter{}, &fakeFullTextFetcher{}, &fakeSummarizer{}).ServeHTTP(rec, req)

		require.Equal(t, http.StatusCreated, rec.Code)
		var got map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Contains(t, got, "feed")
		assert.Contains(t, got, "inserted_articles")
		assert.NotContains(t, got, "Created")

		var f feed.Feed
		require.NoError(t, json.Unmarshal(got["feed"], &f))
		assert.Equal(t, int64(1), f.ID)
		assert.Equal(t, "Example", f.Title)
	})
}

// fakeFeedLister は FeedLister のテストフェイク。
type fakeFeedLister struct {
	list func(ctx context.Context) ([]feed.Feed, error)
}

func (f *fakeFeedLister) ListFeeds(ctx context.Context) ([]feed.Feed, error) {
	if f.list == nil {
		return nil, nil
	}
	return f.list(ctx)
}

func TestHandleListFeeds(t *testing.T) {
	t.Parallel()

	t.Run("feeds are serialized with snake_case fields", func(t *testing.T) {
		t.Parallel()

		lister := &fakeFeedLister{list: func(_ context.Context) ([]feed.Feed, error) {
			return []feed.Feed{{
				ID:        3,
				URL:       "https://example.com/feed.xml",
				Title:     "Example",
				CreatedAt: time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC),
			}}, nil
		}}
		req := httptest.NewRequestWithContext(t.Context(),
			http.MethodGet, "/api/v1/feeds", nil)
		rec := httptest.NewRecorder()
		NewMux(&fakeRegistrar{}, lister, &fakeLister{}, &fakeGetter{}, &fakeFullTextFetcher{}, &fakeSummarizer{}).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		var got struct {
			Feeds []map[string]any `json:"feeds"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Len(t, got.Feeds, 1)
		assert.Equal(t, "https://example.com/feed.xml", got.Feeds[0]["url"])
		assert.Equal(t, "Example", got.Feeds[0]["title"])
		assert.Contains(t, got.Feeds[0], "created_at")
	})

	t.Run("empty result is a JSON array, not null", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(),
			http.MethodGet, "/api/v1/feeds", nil)
		rec := httptest.NewRecorder()
		NewMux(&fakeRegistrar{}, &fakeFeedLister{}, &fakeLister{}, &fakeGetter{}, &fakeFullTextFetcher{}, &fakeSummarizer{}).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"feeds": []}`, rec.Body.String())
	})

	t.Run("store failure returns 500", func(t *testing.T) {
		t.Parallel()

		lister := &fakeFeedLister{list: func(_ context.Context) ([]feed.Feed, error) {
			return nil, assert.AnError
		}}
		req := httptest.NewRequestWithContext(t.Context(),
			http.MethodGet, "/api/v1/feeds", nil)
		rec := httptest.NewRecorder()
		NewMux(&fakeRegistrar{}, lister, &fakeLister{}, &fakeGetter{}, &fakeFullTextFetcher{}, &fakeSummarizer{}).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}
