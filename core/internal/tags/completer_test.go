package tags

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kaikei-e/moka-1/core/internal/llm"
)

func TestLLMCompleterExtract(t *testing.T) {
	t.Parallel()

	t.Run("sends response_format json_schema and ADR00007 sampling params", func(t *testing.T) {
		t.Parallel()

		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/chat/completions", r.URL.Path)
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model": "unsloth/Qwen3.5-4B-GGUF:Q4_K_M",
				"choices": []map[string]any{
					{"message": map[string]any{"role": "assistant", "content": `{"tags":["タグ1","タグ2"]}`}},
				},
			})
		}))
		defer srv.Close()

		c := NewLLMCompleter(llm.NewClient(srv.URL, srv.Client()), "qwen3.5-4b")
		res, err := c.Extract(t.Context(), "記事本文")
		require.NoError(t, err)

		assert.Equal(t, "qwen3.5-4b", gotBody["model"], "router mode のルーティングキー(ADR00020)")
		assert.JSONEq(t, `{"tags":["タグ1","タグ2"]}`, res.Text)
		assert.Equal(t, "unsloth/Qwen3.5-4B-GGUF:Q4_K_M", res.Meta["model"])
		assert.InDelta(t, 0.7, res.Meta["temperature"], 0.0001)

		rf, ok := gotBody["response_format"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "json_schema", rf["type"])
		schema, ok := rf["json_schema"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "result", schema["name"])
		assert.Equal(t, true, schema["strict"])

		messages, ok := gotBody["messages"].([]any)
		require.True(t, ok)
		system := messages[0].(map[string]any)
		assert.Equal(t, systemPrompt, system["content"])
	})

	t.Run("llm failure propagates as an error", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		c := NewLLMCompleter(llm.NewClient(srv.URL, srv.Client()), "qwen3.5-4b")
		_, err := c.Extract(t.Context(), "text")
		require.ErrorIs(t, err, llm.ErrUnavailable)
	})
}
