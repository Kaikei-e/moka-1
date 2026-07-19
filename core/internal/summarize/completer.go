package summarize

import (
	"context"

	"github.com/Kaikei-e/moka-1/core/internal/llm"
)

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

// LLMCompleter は internal/llm.Client を要約ユースケースの Completer ポートへ適合させる
// アダプタ。HTTP 機構自体は llm.Client(共通基盤)に任せ、ここでは ADR00007 のシステム
// プロンプト・サンプリングパラメータ・モデル系譜メタの組み立てだけを持つ。
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

// Complete は summarize.Completer を満たす。
func (c *LLMCompleter) Complete(ctx context.Context, text string) (CompletionResult, error) {
	res, err := c.client.Complete(ctx, c.request(text))
	if err != nil {
		return CompletionResult{}, err
	}
	return CompletionResult{Text: res.Text, Meta: completionMeta(res.Model)}, nil
}

// CompleteStream は summarize.Completer を満たす。
func (c *LLMCompleter) CompleteStream(
	ctx context.Context, text string, onRawDelta func(delta string),
) (CompletionResult, error) {
	res, err := c.client.CompleteStream(ctx, c.request(text), onRawDelta)
	if err != nil {
		return CompletionResult{}, err
	}
	return CompletionResult{Text: res.Text, Meta: completionMeta(res.Model)}, nil
}
