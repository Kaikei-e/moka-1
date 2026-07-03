package fulltext

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kaikei-e/moka-1/core/internal/feed"
)

func permissivePageFetcher() *HTTPFetcher {
	return NewHTTPFetcher(feed.NewURLValidator(true))
}

func TestHTTPFetcherFetchPage(t *testing.T) {
	t.Parallel()

	t.Run("returns response body bytes", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("<html><body>hello</body></html>"))
		}))
		t.Cleanup(srv.Close)

		body, err := permissivePageFetcher().FetchPage(t.Context(), srv.URL)
		require.NoError(t, err)
		assert.Equal(t, "<html><body>hello</body></html>", string(body))
	})

	t.Run("sends a moka user agent", func(t *testing.T) {
		t.Parallel()

		var gotUA string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotUA = r.Header.Get("User-Agent")
			_, _ = w.Write([]byte("ok"))
		}))
		t.Cleanup(srv.Close)

		_, err := permissivePageFetcher().FetchPage(t.Context(), srv.URL)
		require.NoError(t, err)
		assert.Contains(t, gotUA, "moka/")
	})

	t.Run("server error maps to ErrUpstreamFetch", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(srv.Close)

		_, err := permissivePageFetcher().FetchPage(t.Context(), srv.URL)
		require.ErrorIs(t, err, ErrUpstreamFetch)
	})

	t.Run("connection failure maps to ErrUpstreamFetch", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
		srv.Close() // 即閉じ → connection refused

		_, err := permissivePageFetcher().FetchPage(t.Context(), srv.URL)
		require.ErrorIs(t, err, ErrUpstreamFetch)
	})

	t.Run("redirect target is re-validated", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://10.0.0.5/article", http.StatusFound)
		}))
		t.Cleanup(srv.Close)

		strict := NewHTTPFetcher(feed.NewURLValidator(false))
		_, err := strict.FetchPage(t.Context(), srv.URL)
		require.ErrorIs(t, err, ErrPrivateHost)
	})

	t.Run("body is capped so a huge page cannot exhaust memory", func(t *testing.T) {
		t.Parallel()

		huge := strings.Repeat("a", maxPageBytes+1024)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(huge))
		}))
		t.Cleanup(srv.Close)

		body, err := permissivePageFetcher().FetchPage(t.Context(), srv.URL)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(body), maxPageBytes)
	})
}
