package llm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientEmbed(t *testing.T) {
	t.Parallel()

	t.Run("posts model and raw input to /embeddings and parses the vector", func(t *testing.T) {
		t.Parallel()

		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/embeddings", r.URL.Path)
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model": "qwen3-embedding-0.6b",
				"data":  []map[string]any{{"embedding": []float64{0.1, -0.2, 0.3}, "index": 0}},
			})
		}))
		defer srv.Close()

		c := NewClient(srv.URL, srv.Client())
		res, err := c.Embed(t.Context(), EmbedRequest{Model: "qwen3-embedding-0.6b", Input: "タイトル\n本文"})
		require.NoError(t, err)

		assert.Equal(t, []float32{0.1, -0.2, 0.3}, res.Vector)
		assert.Equal(t, "qwen3-embedding-0.6b", res.Model)
		assert.Equal(t, "qwen3-embedding-0.6b", gotBody["model"])
		assert.Equal(t, "タイトル\n本文", gotBody["input"], "文書側は素のまま(prefixなし — ADR00008)")
	})

	t.Run("Query true prepends the instruction prefix to the input", func(t *testing.T) {
		t.Parallel()

		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model": "m",
				"data":  []map[string]any{{"embedding": []float64{1}}},
			})
		}))
		defer srv.Close()

		c := NewClient(srv.URL, srv.Client())
		_, err := c.Embed(t.Context(), EmbedRequest{Model: "m", Input: "検索語", Query: true})
		require.NoError(t, err)

		assert.Equal(t,
			"Instruct: Given a web search query, retrieve relevant passages that answer the query\nQuery: 検索語",
			gotBody["input"])
	})

	t.Run("non-2xx response is wrapped as ErrUnavailable", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		c := NewClient(srv.URL, srv.Client())
		_, err := c.Embed(t.Context(), EmbedRequest{Model: "m", Input: "text"})
		require.ErrorIs(t, err, ErrUnavailable)
	})

	t.Run("empty data is wrapped as ErrUnavailable", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"model": "m", "data": []map[string]any{}})
		}))
		defer srv.Close()

		c := NewClient(srv.URL, srv.Client())
		_, err := c.Embed(t.Context(), EmbedRequest{Model: "m", Input: "text"})
		require.ErrorIs(t, err, ErrUnavailable)
	})

	t.Run("empty embedding vector is wrapped as ErrUnavailable", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model": "m",
				"data":  []map[string]any{{"embedding": []float64{}}},
			})
		}))
		defer srv.Close()

		c := NewClient(srv.URL, srv.Client())
		_, err := c.Embed(t.Context(), EmbedRequest{Model: "m", Input: "text"})
		require.ErrorIs(t, err, ErrUnavailable)
	})

	t.Run("connection failure is wrapped as ErrUnavailable", func(t *testing.T) {
		t.Parallel()

		c := NewClient("http://127.0.0.1:1", http.DefaultClient)
		_, err := c.Embed(t.Context(), EmbedRequest{Model: "m", Input: "text"})
		require.ErrorIs(t, err, ErrUnavailable)
	})
}
