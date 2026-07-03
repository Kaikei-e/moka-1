package summarize

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPCompleterComplete(t *testing.T) {
	t.Parallel()

	t.Run("sends ADR00007 sampling params and parses the openai-style response", func(t *testing.T) {
		t.Parallel()

		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/chat/completions", r.URL.Path)
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model": "unsloth/Qwen3.5-4B-GGUF:Q4_K_M",
				"choices": []map[string]any{
					{"message": map[string]any{"role": "assistant", "content": "要約結果"}},
				},
			})
		}))
		defer srv.Close()

		c := NewHTTPCompleter(srv.URL, srv.Client())
		res, err := c.Complete(t.Context(), "記事本文")
		require.NoError(t, err)

		assert.Equal(t, "要約結果", res.Text)
		assert.Equal(t, "unsloth/Qwen3.5-4B-GGUF:Q4_K_M", res.Meta["model"])
		assert.InDelta(t, 0.7, res.Meta["temperature"], 0.0001)
		assert.InDelta(t, 0.8, res.Meta["top_p"], 0.0001)
		assert.EqualValues(t, 20, res.Meta["top_k"])
		assert.Equal(t, false, res.Meta["enable_thinking"])

		assert.InDelta(t, 0.7, gotBody["temperature"], 0.0001)
		assert.InDelta(t, 0.8, gotBody["top_p"], 0.0001)
		assert.InDelta(t, 20, gotBody["top_k"], 0.0001)
		kwargs, ok := gotBody["chat_template_kwargs"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, false, kwargs["enable_thinking"])

		messages, ok := gotBody["messages"].([]any)
		require.True(t, ok)
		require.Len(t, messages, 2)
		user := messages[1].(map[string]any)
		assert.Equal(t, "user", user["role"])
		assert.Equal(t, "記事本文", user["content"])
	})

	t.Run("non-2xx response is wrapped as ErrLLMUnavailable", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		c := NewHTTPCompleter(srv.URL, srv.Client())
		_, err := c.Complete(t.Context(), "text")
		require.ErrorIs(t, err, ErrLLMUnavailable)
	})

	t.Run("malformed response body is wrapped as ErrLLMUnavailable", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("not json"))
		}))
		defer srv.Close()

		c := NewHTTPCompleter(srv.URL, srv.Client())
		_, err := c.Complete(t.Context(), "text")
		require.ErrorIs(t, err, ErrLLMUnavailable)
	})

	t.Run("empty choices is wrapped as ErrLLMUnavailable", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"model": "m", "choices": []map[string]any{}})
		}))
		defer srv.Close()

		c := NewHTTPCompleter(srv.URL, srv.Client())
		_, err := c.Complete(t.Context(), "text")
		require.ErrorIs(t, err, ErrLLMUnavailable)
	})

	t.Run("connection failure is wrapped as ErrLLMUnavailable", func(t *testing.T) {
		t.Parallel()

		c := NewHTTPCompleter("http://127.0.0.1:1", http.DefaultClient)
		_, err := c.Complete(t.Context(), "text")
		require.ErrorIs(t, err, ErrLLMUnavailable)
	})
}
