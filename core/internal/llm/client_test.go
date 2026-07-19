package llm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testReq(text string) ChatRequest {
	return ChatRequest{
		System:             "system prompt",
		Text:               text,
		Temperature:        0.7,
		TopP:               0.8,
		TopK:               20,
		MaxTokens:          1536,
		ChatTemplateKwargs: map[string]any{"enable_thinking": false},
	}
}

func TestClientComplete(t *testing.T) {
	t.Parallel()

	t.Run("sends sampling params and parses the openai-style response", func(t *testing.T) {
		t.Parallel()

		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/chat/completions", r.URL.Path)
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model": "unsloth/Qwen3.5-4B-GGUF:Q4_K_M",
				"choices": []map[string]any{
					{"message": map[string]any{"role": "assistant", "content": "結果"}},
				},
			})
		}))
		defer srv.Close()

		c := NewClient(srv.URL, srv.Client())
		res, err := c.Complete(t.Context(), testReq("記事本文"))
		require.NoError(t, err)

		assert.Equal(t, "結果", res.Text)
		assert.Equal(t, "unsloth/Qwen3.5-4B-GGUF:Q4_K_M", res.Model)

		assert.InDelta(t, 0.7, gotBody["temperature"], 0.0001)
		assert.InDelta(t, 0.8, gotBody["top_p"], 0.0001)
		assert.InDelta(t, 20, gotBody["top_k"], 0.0001)
		kwargs, ok := gotBody["chat_template_kwargs"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, false, kwargs["enable_thinking"])
		_, hasResponseFormat := gotBody["response_format"]
		assert.False(t, hasResponseFormat, "no response_format when Schema is nil")

		messages, ok := gotBody["messages"].([]any)
		require.True(t, ok)
		require.Len(t, messages, 2)
		user := messages[1].(map[string]any)
		assert.Equal(t, "user", user["role"])
		assert.Equal(t, "記事本文", user["content"])
	})

	t.Run("Model set is sent as the model field (router mode routing — ADR00020)", func(t *testing.T) {
		t.Parallel()

		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model":   "qwen3.5-4b",
				"choices": []map[string]any{{"message": map[string]any{"content": "結果"}}},
			})
		}))
		defer srv.Close()

		req := testReq("text")
		req.Model = "qwen3.5-4b"

		c := NewClient(srv.URL, srv.Client())
		_, err := c.Complete(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, "qwen3.5-4b", gotBody["model"])
	})

	t.Run("empty Model omits the model field entirely", func(t *testing.T) {
		t.Parallel()

		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model":   "m",
				"choices": []map[string]any{{"message": map[string]any{"content": "結果"}}},
			})
		}))
		defer srv.Close()

		c := NewClient(srv.URL, srv.Client())
		_, err := c.Complete(t.Context(), testReq("text"))
		require.NoError(t, err)
		_, hasModel := gotBody["model"]
		assert.False(t, hasModel)
	})

	t.Run("nil ChatTemplateKwargs omits the field (gpt-oss 系テンプレートに余計なkwargを渡さない)", func(t *testing.T) {
		t.Parallel()

		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model":   "m",
				"choices": []map[string]any{{"message": map[string]any{"content": "結果"}}},
			})
		}))
		defer srv.Close()

		req := testReq("text")
		req.ChatTemplateKwargs = nil

		c := NewClient(srv.URL, srv.Client())
		_, err := c.Complete(t.Context(), req)
		require.NoError(t, err)
		_, hasKwargs := gotBody["chat_template_kwargs"]
		assert.False(t, hasKwargs)
	})

	t.Run("Schema set sends response_format json_schema", func(t *testing.T) {
		t.Parallel()

		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model":   "m",
				"choices": []map[string]any{{"message": map[string]any{"content": `{"tags":["a"]}`}}},
			})
		}))
		defer srv.Close()

		req := testReq("記事本文")
		req.Schema = &Schema{
			Name:   "result",
			Schema: map[string]any{"type": "object"},
			Strict: true,
		}

		c := NewClient(srv.URL, srv.Client())
		res, err := c.Complete(t.Context(), req)
		require.NoError(t, err)
		assert.JSONEq(t, `{"tags":["a"]}`, res.Text)

		rf, ok := gotBody["response_format"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "json_schema", rf["type"])
		schema, ok := rf["json_schema"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "result", schema["name"])
		assert.Equal(t, true, schema["strict"])
	})

	t.Run("non-2xx response is wrapped as ErrUnavailable", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		c := NewClient(srv.URL, srv.Client())
		_, err := c.Complete(t.Context(), testReq("text"))
		require.ErrorIs(t, err, ErrUnavailable)
	})

	t.Run("malformed response body is wrapped as ErrUnavailable", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("not json"))
		}))
		defer srv.Close()

		c := NewClient(srv.URL, srv.Client())
		_, err := c.Complete(t.Context(), testReq("text"))
		require.ErrorIs(t, err, ErrUnavailable)
	})

	t.Run("empty choices is wrapped as ErrUnavailable", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"model": "m", "choices": []map[string]any{}})
		}))
		defer srv.Close()

		c := NewClient(srv.URL, srv.Client())
		_, err := c.Complete(t.Context(), testReq("text"))
		require.ErrorIs(t, err, ErrUnavailable)
	})

	t.Run("connection failure is wrapped as ErrUnavailable", func(t *testing.T) {
		t.Parallel()

		c := NewClient("http://127.0.0.1:1", http.DefaultClient)
		_, err := c.Complete(t.Context(), testReq("text"))
		require.ErrorIs(t, err, ErrUnavailable)
	})
}

func TestClientCompleteStream(t *testing.T) {
	t.Parallel()

	t.Run("sends stream:true and delivers deltas in order via callback", func(t *testing.T) {
		t.Parallel()

		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/chat/completions", r.URL.Path)
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)
			for _, chunk := range []string{
				`{"model":"unsloth/Qwen3.5-4B-GGUF:Q4_K_M","choices":[{"delta":{"content":"要約"}}]}`,
				`{"model":"unsloth/Qwen3.5-4B-GGUF:Q4_K_M","choices":[{"delta":{"content":"結果"}}]}`,
				`{"model":"unsloth/Qwen3.5-4B-GGUF:Q4_K_M","choices":[{"delta":{},"finish_reason":"stop"}]}`,
			} {
				_, _ = w.Write([]byte("data: " + chunk + "\n\n"))
				flusher.Flush()
			}
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			flusher.Flush()
		}))
		defer srv.Close()

		c := NewClient(srv.URL, srv.Client())
		var deltas []string
		res, err := c.CompleteStream(t.Context(), testReq("記事本文"), func(delta string) {
			deltas = append(deltas, delta)
		})
		require.NoError(t, err)

		assert.Equal(t, []string{"要約", "結果"}, deltas)
		assert.Equal(t, "要約結果", res.Text)
		assert.Equal(t, "unsloth/Qwen3.5-4B-GGUF:Q4_K_M", res.Model)

		assert.Equal(t, true, gotBody["stream"])
		messages, ok := gotBody["messages"].([]any)
		require.True(t, ok)
		require.Len(t, messages, 2)
		user := messages[1].(map[string]any)
		assert.Equal(t, "記事本文", user["content"])
	})

	t.Run("non-2xx response is wrapped as ErrUnavailable", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		defer srv.Close()

		c := NewClient(srv.URL, srv.Client())
		_, err := c.CompleteStream(t.Context(), testReq("text"), func(string) {})
		require.ErrorIs(t, err, ErrUnavailable)
	})

	t.Run("malformed chunk is wrapped as ErrUnavailable", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)
			_, _ = w.Write([]byte("data: not json\n\n"))
			flusher.Flush()
		}))
		defer srv.Close()

		c := NewClient(srv.URL, srv.Client())
		_, err := c.CompleteStream(t.Context(), testReq("text"), func(string) {})
		require.ErrorIs(t, err, ErrUnavailable)
	})

	t.Run("stream ending without [DONE] is wrapped as ErrUnavailable (truncation guard)", func(t *testing.T) {
		t.Parallel()

		// llama.cpp がエラー中断してレスポンスを正常クローズした場合の再現:
		// チャンクは届いたが終端マーカー [DONE] が無い。部分テキストを完全な結果として
		// 返してはいけない(冪等保存され再生成されないため)。
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)
			_, _ = w.Write([]byte(`data: {"model":"m","choices":[{"delta":{"content":"途中まで"}}]}` + "\n\n"))
			_, _ = w.Write([]byte(`data: {"error":{"message":"slot released"}}` + "\n\n"))
			flusher.Flush()
		}))
		defer srv.Close()

		c := NewClient(srv.URL, srv.Client())
		_, err := c.CompleteStream(t.Context(), testReq("text"), func(string) {})
		require.ErrorIs(t, err, ErrUnavailable)
	})

	t.Run("stream with no chunks at all is wrapped as ErrUnavailable", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
		}))
		defer srv.Close()

		c := NewClient(srv.URL, srv.Client())
		_, err := c.CompleteStream(t.Context(), testReq("text"), func(string) {})
		require.ErrorIs(t, err, ErrUnavailable)
	})

	t.Run("connection failure is wrapped as ErrUnavailable", func(t *testing.T) {
		t.Parallel()

		c := NewClient("http://127.0.0.1:1", http.DefaultClient)
		_, err := c.CompleteStream(t.Context(), testReq("text"), func(string) {})
		require.ErrorIs(t, err, ErrUnavailable)
	})
}
