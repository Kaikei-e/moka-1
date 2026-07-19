package rag

import (
	"context"

	"github.com/Kaikei-e/moka-1/core/internal/llm"
)

// LLMEmbedder は internal/llm.Client を rag の QueryEmbedder / DocumentEmbedder ポートへ
// 適合させるアダプタ。クエリ側の instruction prefix の付与は llm.Client 側に集約されている
// (ADR00008)ので、ここでは Query フラグを立てるだけ。
type LLMEmbedder struct {
	client *llm.Client
	model  string
}

// NewLLMEmbedder はアダプタを組む。model は router mode(ADR00020)のルーティングキー
// (埋め込みモデルの別名 — main が環境変数から注入する)。
func NewLLMEmbedder(client *llm.Client, model string) *LLMEmbedder {
	return &LLMEmbedder{client: client, model: model}
}

// EmbedQuery は rag.QueryEmbedder を満たす(クエリ側 — instruction prefix 付き)。
func (e *LLMEmbedder) EmbedQuery(ctx context.Context, q string) ([]float32, error) {
	res, err := e.client.Embed(ctx, llm.EmbedRequest{Model: e.model, Input: q, Query: true})
	if err != nil {
		return nil, err
	}
	return res.Vector, nil
}

// EmbedDocument は rag.DocumentEmbedder を満たす(文書側 — 素のまま)。
func (e *LLMEmbedder) EmbedDocument(ctx context.Context, text string) ([]float32, string, error) {
	res, err := e.client.Embed(ctx, llm.EmbedRequest{Model: e.model, Input: text})
	if err != nil {
		return nil, "", err
	}
	return res.Vector, res.Model, nil
}

// answerSystemPrompt は回答指示。summarize のプロンプト流儀(役割 → 根拠の限定 →
// 出力要件 → 禁止事項 → 形式)に合わせた日本語回答指示。
const answerSystemPrompt = "あなたは購読フィードの記事に基づいて質問に答えるアシスタントです。" +
	"「質問:」に対して、「対象記事」と「参考記事」だけを根拠に、日本語で簡潔に(目安2〜6文)回答してください。" +
	"記事固有の具体的な事実・数値・固有名詞を優先し、記事に書かれていない情報・推測・意見を一切加えないでください。" +
	"根拠が記事中に見つからない場合は、その旨を正直に述べてください。" +
	"前置きや見出しは不要です。回答本文のみを出力してください。"

// 集約層(gpt-oss-20b)のサンプリングパラメータ。公式推奨は temperature 1.0 / top_p 1.0
// (top_k は無効 = 0)。eval/ での A/B 実測が済むまでは公式推奨に従う。
const (
	answerTemperature = 1.0
	answerTopP        = 1.0
	answerTopK        = 0
	answerMaxTokens   = 1536
)

// LLMAnswerCompleter は internal/llm.Client を rag の AnswerCompleter ポートへ適合させる
// アダプタ(summarize.LLMCompleter と同じ形)。ChatTemplateKwargs は送らない —
// enable_thinking は Qwen 系テンプレート固有の kwarg で、gpt-oss 系には存在しない。
type LLMAnswerCompleter struct {
	client *llm.Client
	model  string
}

// NewLLMAnswerCompleter はアダプタを組む。model は router mode(ADR00020)のルーティング
// キー(集約層の別名 — main が環境変数から注入する)。
func NewLLMAnswerCompleter(client *llm.Client, model string) *LLMAnswerCompleter {
	return &LLMAnswerCompleter{client: client, model: model}
}

// CompleteStream は rag.AnswerCompleter を満たす。
func (c *LLMAnswerCompleter) CompleteStream(
	ctx context.Context, text string, onRawDelta func(delta string),
) (string, error) {
	res, err := c.client.CompleteStream(ctx, llm.ChatRequest{
		Model:       c.model,
		System:      answerSystemPrompt,
		Text:        text,
		Temperature: answerTemperature,
		TopP:        answerTopP,
		TopK:        answerTopK,
		MaxTokens:   answerMaxTokens,
	}, onRawDelta)
	if err != nil {
		return "", err
	}
	return res.Text, nil
}
