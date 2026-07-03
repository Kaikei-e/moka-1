package feed

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testRSS = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/">
  <channel>
    <title>Test Feed</title>
    <item>
      <title>Full item</title>
      <link>http://example.com/a</link>
      <guid isPermaLink="false">guid-a</guid>
      <pubDate>Wed, 01 Jul 2026 09:00:00 GMT</pubDate>
      <description>desc-a</description>
      <content:encoded>content-a</content:encoded>
    </item>
    <item>
      <title>No guid falls back to link</title>
      <link>http://example.com/b</link>
      <description>desc-b</description>
    </item>
    <item>
      <title>No guid nor link is skipped</title>
      <description>desc-c</description>
    </item>
  </channel>
</rss>`

// permissiveFetcher はループバックの httptest サーバーに接続できる fetcher を返す。
func permissiveFetcher() *HTTPFetcher {
	return NewHTTPFetcher(NewURLValidator(true))
}

func TestHTTPFetcherFetch(t *testing.T) {
	t.Parallel()

	t.Run("parses feed and normalizes items", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("ETag", `"v1"`)
			w.Header().Set("Last-Modified", "Wed, 01 Jul 2026 10:00:00 GMT")
			_, _ = w.Write([]byte(testRSS))
		}))
		t.Cleanup(srv.Close)

		res, err := permissiveFetcher().Fetch(t.Context(), srv.URL, Conditional{})
		require.NoError(t, err)

		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.False(t, res.NotModified)
		assert.Equal(t, "Test Feed", res.Title)
		assert.Equal(t, `"v1"`, res.ETag)
		assert.Equal(t, "Wed, 01 Jul 2026 10:00:00 GMT", res.LastModified)

		require.Len(t, res.Items, 2, "guid も link も無い item はスキップ")

		full := res.Items[0]
		assert.Equal(t, "guid-a", full.GUID)
		assert.Equal(t, "http://example.com/a", full.URL)
		assert.Equal(t, "Full item", full.Title)
		assert.Equal(t, "content-a", full.Content, "content:encoded 優先")
		require.NotNil(t, full.PublishedAt)
		assert.Equal(t, time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC), full.PublishedAt.UTC())

		fallback := res.Items[1]
		assert.Equal(t, "http://example.com/b", fallback.GUID, "guid 無しは link にフォールバック")
		assert.Equal(t, "desc-b", fallback.Content, "content 無しは description にフォールバック")
		assert.Nil(t, fallback.PublishedAt)
	})

	t.Run("sends conditional headers and honors 304", func(t *testing.T) {
		t.Parallel()

		var gotINM, gotIMS string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotINM = r.Header.Get("If-None-Match")
			gotIMS = r.Header.Get("If-Modified-Since")
			w.WriteHeader(http.StatusNotModified)
		}))
		t.Cleanup(srv.Close)

		cond := Conditional{ETag: `"v1"`, LastModified: "Wed, 01 Jul 2026 10:00:00 GMT"}
		res, err := permissiveFetcher().Fetch(t.Context(), srv.URL, cond)
		require.NoError(t, err)

		assert.Equal(t, `"v1"`, gotINM)
		assert.Equal(t, "Wed, 01 Jul 2026 10:00:00 GMT", gotIMS)
		assert.True(t, res.NotModified)
		assert.Equal(t, http.StatusNotModified, res.StatusCode)
		assert.Empty(t, res.Items)
	})

	t.Run("omits conditional headers when state is empty", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Empty(t, r.Header.Values("If-None-Match"))
			assert.Empty(t, r.Header.Values("If-Modified-Since"))
			_, _ = w.Write([]byte(testRSS))
		}))
		t.Cleanup(srv.Close)

		_, err := permissiveFetcher().Fetch(t.Context(), srv.URL, Conditional{})
		require.NoError(t, err)
	})

	t.Run("server error maps to ErrUpstreamFetch with status", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(srv.Close)

		res, err := permissiveFetcher().Fetch(t.Context(), srv.URL, Conditional{})
		require.ErrorIs(t, err, ErrUpstreamFetch)
		assert.Equal(t, http.StatusInternalServerError, res.StatusCode, "イベント記録用にステータスは返す")
	})

	t.Run("connection failure maps to ErrUpstreamFetch", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
		srv.Close() // 即閉じ → connection refused

		_, err := permissiveFetcher().Fetch(t.Context(), srv.URL, Conditional{})
		require.ErrorIs(t, err, ErrUpstreamFetch)
	})

	t.Run("non-feed body maps to ErrNotAFeed", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("<html><body>not a feed</body></html>"))
		}))
		t.Cleanup(srv.Close)

		_, err := permissiveFetcher().Fetch(t.Context(), srv.URL, Conditional{})
		require.ErrorIs(t, err, ErrNotAFeed)
	})

	t.Run("redirect target is re-validated", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://10.0.0.5/feed", http.StatusFound)
		}))
		t.Cleanup(srv.Close)

		// 初回 URL は registrar が検証済みの前提なので fetcher は再検証しない。
		// リダイレクト先だけが CheckRedirect で検査される(strict validator)
		strict := NewHTTPFetcher(NewURLValidator(false))
		_, err := strict.Fetch(t.Context(), srv.URL, Conditional{})
		require.ErrorIs(t, err, ErrPrivateHost)
	})
}
