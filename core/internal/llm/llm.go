// Package llm は llama.cpp server(OpenAI 互換 /chat/completions)との HTTP 機構を持つ。
// システムプロンプト・サンプリングパラメータ・生成結果の意味付け(model_meta 等)は
// 消費側(summarize・tags)の責務であり、ここには置かない(clean-architecture)。
package llm

import "errors"

// ErrUnavailable は llm サーバーへの呼び出し失敗全般(タイムアウト・接続エラー・非2xx・
// 不正な応答)。消費側は自分のドメイン sentinel でさらにラップしてよい。
var ErrUnavailable = errors.New("llm unavailable")

// Schema は response_format による json_schema 制約付き補完を指定する(llama.cpp の
// OpenAI 互換拡張。name/strict は仕様上必須のフィールド)。nil のままなら自由形式のテキスト補完。
type Schema struct {
	Name   string
	Schema map[string]any
	Strict bool
}

// ChatRequest は 1 回のチャット補完に必要な全パラメータ(呼び出し元が組み立てる)。
type ChatRequest struct {
	// Model は router mode(ADR00020)のルーティングキー(モデル別名)。空なら送らない。
	Model              string
	System             string
	Text               string
	Temperature        float64
	TopP               float64
	TopK               int
	MaxTokens          int
	ChatTemplateKwargs map[string]any
	Schema             *Schema
}

// Result は 1 回のチャット補完の生の結果(think 除去前・呼び出し元向けの意味付け前)。
type Result struct {
	Text  string
	Model string
}
