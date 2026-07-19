package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// completeTimeout は 1 回のチャット補完のデッドライン。ADR00007 実測(Qwen3.5-4B no-think
// 6.7s/記事)に対し十分なマージン(同時ロードでの輻輳・コールドキャッシュを考慮)。
// 呼び出し元の ctx がこれより短ければそちらが優先される(context.WithTimeout の性質)。
const completeTimeout = 60 * time.Second

// Client は llama.cpp server(OpenAI 互換 /chat/completions)を叩く薄い HTTP アダプタ。
// リトライ・サーキットブレーカーは持たない(単発呼び出し、ミニマリズム原則 —
// fulltext.HTTPFetcher と同じ作法。失敗は呼び出し元の enrichment_attempts 記録に委ねる)。
type Client struct {
	baseURL string
	client  *http.Client
}

// NewClient はクライアントを組む。baseURL は OpenAI 互換ベース(例: http://llm:8081/v1、
// compose.yaml の LLM_BASE_URL)。
func NewClient(baseURL string, client *http.Client) *Client {
	return &Client{baseURL: baseURL, client: client}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type       string         `json:"type"`
	JSONSchema jsonSchemaBody `json:"json_schema"`
}

type jsonSchemaBody struct {
	Name   string         `json:"name"`
	Schema map[string]any `json:"schema"`
	Strict bool           `json:"strict"`
}

type chatBody struct {
	Messages           []chatMessage   `json:"messages"`
	Temperature        float64         `json:"temperature"`
	TopP               float64         `json:"top_p"`
	TopK               int             `json:"top_k"`
	MaxTokens          int             `json:"max_tokens"`
	ChatTemplateKwargs map[string]any  `json:"chat_template_kwargs"`
	Stream             bool            `json:"stream,omitempty"`
	ResponseFormat     *responseFormat `json:"response_format,omitempty"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

// chatStreamChunk は stream:true 時の1 SSE イベント分(OpenAI互換 chat.completion.chunk)。
type chatStreamChunk struct {
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

// buildBody は Complete/CompleteStream 共通のリクエスト本体を組む。
func buildBody(req ChatRequest, stream bool) chatBody {
	body := chatBody{
		Messages: []chatMessage{
			{Role: "system", Content: req.System},
			{Role: "user", Content: req.Text},
		},
		Temperature:        req.Temperature,
		TopP:               req.TopP,
		TopK:               req.TopK,
		MaxTokens:          req.MaxTokens,
		ChatTemplateKwargs: req.ChatTemplateKwargs,
		Stream:             stream,
	}
	if req.Schema != nil {
		body.ResponseFormat = &responseFormat{
			Type: "json_schema",
			JSONSchema: jsonSchemaBody{
				Name:   req.Schema.Name,
				Schema: req.Schema.Schema,
				Strict: req.Schema.Strict,
			},
		}
	}
	return body
}

// doRequest はリクエストを組んで送信し、2xx を確認した *http.Response を返す。
// 呼び出し元が resp.Body を閉じる責務を持つ。
func (c *Client) doRequest(ctx context.Context, body chatBody) (*http.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w (%w)", ErrUnavailable, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w (%w)", ErrUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call llm: %w (%w)", ErrUnavailable, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("llm status %d: %w", resp.StatusCode, ErrUnavailable)
	}
	return resp, nil
}

// Complete は req を1回補完させ、生の(意味付け前の)応答テキストとモデル名を返す。
func (c *Client) Complete(ctx context.Context, req ChatRequest) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, completeTimeout)
	defer cancel()

	resp, err := c.doRequest(ctx, buildBody(req, false))
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	var parsed chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return Result{}, fmt.Errorf("decode response: %w (%w)", ErrUnavailable, err)
	}
	if len(parsed.Choices) == 0 {
		return Result{}, fmt.Errorf("no choices in response: %w", ErrUnavailable)
	}

	return Result{Text: parsed.Choices[0].Message.Content, Model: parsed.Model}, nil
}

// CompleteStream は Complete のストリーミング版。llama.cpp の OpenAI 互換 SSE
// (`data: {...}` の chat.completion.chunk、終端は `data: [DONE]`)を読み、
// 生チャンク(think 除去前)が届くたびに onRawDelta を呼ぶ。think 除去は呼び出し元の
// 責務 — このメソッドは中継に徹する(clean-architecture: think-strip は消費側に残す)。
// 戻り値は Complete と同じ形(完全な生テキスト+モデル名)。
func (c *Client) CompleteStream(
	ctx context.Context, req ChatRequest, onRawDelta func(delta string),
) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, completeTimeout)
	defer cancel()

	resp, err := c.doRequest(ctx, buildBody(req, true))
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	var full bytes.Buffer
	var model string
	sawChunk := false
	sawDone := false

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}
		if data == "[DONE]" {
			sawDone = true
			break
		}

		var chunk chatStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return Result{}, fmt.Errorf("decode stream chunk: %w (%w)", ErrUnavailable, err)
		}
		sawChunk = true
		if chunk.Model != "" {
			model = chunk.Model
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta.Content
		if delta == "" {
			continue
		}
		full.WriteString(delta)
		onRawDelta(delta)
	}
	if err := scanner.Err(); err != nil {
		return Result{}, fmt.Errorf("read stream: %w (%w)", ErrUnavailable, err)
	}
	if !sawChunk {
		return Result{}, fmt.Errorf("no chunks in stream: %w", ErrUnavailable)
	}
	// [DONE] を見ずに正常終了したストリームは途中終端(llama.cpp のエラー中断等)とみなす。
	// ここで弾かないと、切り詰められた部分テキストが完全な結果として永久保存されうる。
	if !sawDone {
		return Result{}, fmt.Errorf("stream ended without [DONE]: %w", ErrUnavailable)
	}

	return Result{Text: full.String(), Model: model}, nil
}
