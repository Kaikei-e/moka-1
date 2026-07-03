package summarize

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

// systemPrompt は要約指示。eval/ での A/B(qwen35-4b、同一モデルでプロンプトのみ差し替え、
// Claude judgeブラインド判定、30ペア中18勝-6敗-6分、sign-test p=0.023)で旧版に勝った版。
// 背景・具体的事実・結論の3点を明示的な出力要件として求めることで、一般論的な埋め草を
// 排し具体性(固有名詞・数値)を上げる。憶測・新情報の追加をしないことも明示する。
const systemPrompt = "あなたは記事要約アシスタントです。以下の記事本文だけを根拠に、" +
	"日本語で400字程度(目安8〜12文)の要約を書いてください。" +
	"要約には必ず次の3つを過不足なく含めてください: " +
	"(1) 背景・前提となる状況、(2) 記事固有の具体的な事実・数値・固有名詞、(3) 記事が示す結論または帰結。" +
	"一般論や紋切り型の言い回しで字数を埋めず、記事に書かれていない情報・推測・意見を一切加えないでください。" +
	"前置きや見出しは不要です。要約本文のみを出力してください。"

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
	Stream             bool           `json:"stream,omitempty"`
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

// buildChatRequest は Complete/CompleteStream 共通のリクエスト本体を組む。
func buildChatRequest(text string, stream bool) chatRequest {
	return chatRequest{
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: text},
		},
		Temperature:        temperature,
		TopP:               topP,
		TopK:               topK,
		MaxTokens:          maxTokens,
		ChatTemplateKwargs: map[string]any{"enable_thinking": false},
		Stream:             stream,
	}
}

// completionMeta は ADR00007 のモデル系譜メタを組む(Complete/CompleteStream 共通)。
func completionMeta(model string) map[string]any {
	return map[string]any{
		"model":           model,
		"temperature":     temperature,
		"top_p":           topP,
		"top_k":           topK,
		"enable_thinking": false,
	}
}

// doRequest はリクエストを組んで送信し、2xx を確認した *http.Response を返す。
// 呼び出し元が resp.Body を閉じる責務を持つ。
func (c *HTTPCompleter) doRequest(ctx context.Context, body chatRequest) (*http.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w (%w)", ErrLLMUnavailable, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w (%w)", ErrLLMUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call llm: %w (%w)", ErrLLMUnavailable, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("llm status %d: %w", resp.StatusCode, ErrLLMUnavailable)
	}
	return resp, nil
}

// Complete は text を要約させ、生の(think 除去前の)応答テキストとモデル系譜を返す。
func (c *HTTPCompleter) Complete(ctx context.Context, text string) (CompletionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, completeTimeout)
	defer cancel()

	resp, err := c.doRequest(ctx, buildChatRequest(text, false))
	if err != nil {
		return CompletionResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	var parsed chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return CompletionResult{}, fmt.Errorf("decode response: %w (%w)", ErrLLMUnavailable, err)
	}
	if len(parsed.Choices) == 0 {
		return CompletionResult{}, fmt.Errorf("no choices in response: %w", ErrLLMUnavailable)
	}

	return CompletionResult{
		Text: parsed.Choices[0].Message.Content,
		Meta: completionMeta(parsed.Model),
	}, nil
}

// CompleteStream は Complete のストリーミング版。llama.cpp の OpenAI 互換 SSE
// (`data: {...}` の chat.completion.chunk、終端は `data: [DONE]`)を読み、
// 生チャンク(think 除去前)が届くたびに onRawDelta を呼ぶ。think 除去は呼び出し元
// (Service)の責務 — このメソッドは中継に徹する(clean-architecture: think-strip は
// ドメインの純粋関数に残す)。戻り値は Complete と同じ形(完全な生テキスト+メタ)。
func (c *HTTPCompleter) CompleteStream(
	ctx context.Context, text string, onRawDelta func(delta string),
) (CompletionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, completeTimeout)
	defer cancel()

	resp, err := c.doRequest(ctx, buildChatRequest(text, true))
	if err != nil {
		return CompletionResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	var full bytes.Buffer
	var model string
	sawChunk := false

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}
		if data == "[DONE]" {
			break
		}

		var chunk chatStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return CompletionResult{}, fmt.Errorf("decode stream chunk: %w (%w)", ErrLLMUnavailable, err)
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
		return CompletionResult{}, fmt.Errorf("read stream: %w (%w)", ErrLLMUnavailable, err)
	}
	if !sawChunk {
		return CompletionResult{}, fmt.Errorf("no chunks in stream: %w", ErrLLMUnavailable)
	}

	return CompletionResult{
		Text: full.String(),
		Meta: completionMeta(model),
	}, nil
}
