package summarize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// completeTimeout は 1 回のチャット補完のデッドライン。ADR00007 実測(Qwen3.5-4B no-think
// 6.7s/記事)に対し十分なマージン(同時ロードでの輻輳・コールドキャッシュを考慮)。
// 呼び出し元の ctx がこれより短ければそちらが優先される(context.WithTimeout の性質)。
const completeTimeout = 60 * time.Second

// systemPrompt は要約指示。一言二言の短い要約ではなく、背景・具体的な内容・結論を含めた
// やや詳しい分量を明示的に求める(短すぎる要約は読者にとって記事を読む代わりにならない)。
// 憶測・新情報の追加をしないことも明示する。
const systemPrompt = "あなたは記事要約アシスタントです。以下の記事の要点を漏らさず、" +
	"日本語で400字程度(目安8〜12文)に要約してください。背景・具体的な内容・結論を含め、" +
	"一言二言で終わる短すぎる要約にはしないでください。憶測や記事に無い情報を追加しないでください。"

// サンプリングパラメータ(ADR00007 確定値)。max_tokens は 400 字程度の要約に十分な余裕を持たせる。
const (
	temperature = 0.7
	topP        = 0.8
	topK        = 20
	maxTokens   = 1536
)

// HTTPCompleter は llama.cpp server(OpenAI 互換 /chat/completions)を叩く Completer の
// 具象アダプタ。リトライ・サーキットブレーカーは持たない(単発呼び出し、ミニマリズム原則
// — fulltext.HTTPFetcher と同じ作法。失敗は enrichment_attempts への記録に委ねる)。
type HTTPCompleter struct {
	baseURL string
	client  *http.Client
}

// NewHTTPCompleter は補完クライアントを組む。baseURL は OpenAI 互換ベース(例:
// http://llm:8081/v1、compose.yaml の LLM_BASE_URL)。
func NewHTTPCompleter(baseURL string, client *http.Client) *HTTPCompleter {
	return &HTTPCompleter{baseURL: baseURL, client: client}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Messages           []chatMessage  `json:"messages"`
	Temperature        float64        `json:"temperature"`
	TopP               float64        `json:"top_p"`
	TopK               int            `json:"top_k"`
	MaxTokens          int            `json:"max_tokens"`
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

// Complete は text を要約させ、生の(think 除去前の)応答テキストとモデル系譜を返す。
func (c *HTTPCompleter) Complete(ctx context.Context, text string) (CompletionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, completeTimeout)
	defer cancel()

	body := chatRequest{
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: text},
		},
		Temperature:        temperature,
		TopP:               topP,
		TopK:               topK,
		MaxTokens:          maxTokens,
		ChatTemplateKwargs: map[string]any{"enable_thinking": false},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return CompletionResult{}, fmt.Errorf("marshal request: %w (%w)", ErrLLMUnavailable, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return CompletionResult{}, fmt.Errorf("build request: %w (%w)", ErrLLMUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return CompletionResult{}, fmt.Errorf("call llm: %w (%w)", ErrLLMUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return CompletionResult{}, fmt.Errorf("llm status %d: %w", resp.StatusCode, ErrLLMUnavailable)
	}

	var parsed chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return CompletionResult{}, fmt.Errorf("decode response: %w (%w)", ErrLLMUnavailable, err)
	}
	if len(parsed.Choices) == 0 {
		return CompletionResult{}, fmt.Errorf("no choices in response: %w", ErrLLMUnavailable)
	}

	return CompletionResult{
		Text: parsed.Choices[0].Message.Content,
		Meta: map[string]any{
			"model":           parsed.Model,
			"temperature":     temperature,
			"top_p":           topP,
			"top_k":           topK,
			"enable_thinking": false,
		},
	}, nil
}
