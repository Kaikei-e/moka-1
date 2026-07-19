package tags

import (
	"context"

	"github.com/Kaikei-e/moka-1/core/internal/llm"
)

// systemPrompt はタグ抽出指示。eval/prompts/tags_ja.txt(ADR00007 D5 の実測に使われた版、
// Qwen3.5-4B/LFM2.5 とも json_schema パース成功率100%)の移植。記事タイトルは現状
// summarize と同じく渡さない(articleContent のみを根拠にする)。
const systemPrompt = "あなたは記事タグ抽出アシスタントです。以下の記事本文の内容を表すタグを" +
	"1〜5個、指定されたJSONスキーマに従って出力してください。" +
	"タグは日本語で。固有名詞(製品名・企業名等)は記事中の表記のまま用いてください。" +
	"記事の主題を優先し、些末な言及にタグを付けないでください。" +
	"前置きや説明は不要です。JSON以外の文字列を一切出力しないでください。"

// サンプリングパラメータ(ADR00007 D5: 高速パスモデル・json_schema制約下の設定。
// summarize と同じ温度/top_p/top_kだが、max_tokens はタグ用に大幅に短い)。
const (
	temperature = 0.7
	topP        = 0.8
	topK        = 20
	maxTokens   = 256
)

// tagsJSONSchema は response_format(json_schema)で強制するスキーマ。
// eval/src/moka_eval/generate.py の TagResult(tags: list[str], min=1, max=5)と同一。
var tagsJSONSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"tags": map[string]any{
			"type":     "array",
			"items":    map[string]any{"type": "string"},
			"minItems": 1,
			"maxItems": 5,
		},
	},
	"required": []string{"tags"},
}

// LLMCompleter は internal/llm.Client をタグ抽出ユースケースの Completer ポートへ適合させる
// アダプタ(summarize.LLMCompleter と同じ形)。
type LLMCompleter struct {
	client *llm.Client
	model  string
}

// NewLLMCompleter はアダプタを組む。model は router mode(ADR00020)のルーティングキー
// (高速パスの別名 — main が環境変数から注入する)。
func NewLLMCompleter(client *llm.Client, model string) *LLMCompleter {
	return &LLMCompleter{client: client, model: model}
}

func (c *LLMCompleter) request(text string) llm.ChatRequest {
	return llm.ChatRequest{
		Model:              c.model,
		System:             systemPrompt,
		Text:               text,
		Temperature:        temperature,
		TopP:               topP,
		TopK:               topK,
		MaxTokens:          maxTokens,
		ChatTemplateKwargs: map[string]any{"enable_thinking": false},
		Schema:             &llm.Schema{Name: "result", Schema: tagsJSONSchema, Strict: true},
	}
}

// completionMeta は ADR00007 のモデル系譜メタを組む。
func completionMeta(model string) map[string]any {
	return map[string]any{
		"model":           model,
		"temperature":     temperature,
		"top_p":           topP,
		"top_k":           topK,
		"enable_thinking": false,
	}
}

// Extract は tags.Completer を満たす。
func (c *LLMCompleter) Extract(ctx context.Context, text string) (CompletionResult, error) {
	res, err := c.client.Complete(ctx, c.request(text))
	if err != nil {
		return CompletionResult{}, err
	}
	return CompletionResult{Text: res.Text, Meta: completionMeta(res.Model)}, nil
}
