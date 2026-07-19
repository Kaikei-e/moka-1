package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// embedTimeout は 1 回の埋め込みのデッドライン。0.6B の埋め込みは補完よりずっと速いが、
// router mode の LRU 退避後の再ロード(ADR00020)を考慮したマージンを持たせる。
const embedTimeout = 30 * time.Second

// embedQueryInstruction はクエリ側にのみ付ける instruction prefix(ADR00008 — 英語推奨、
// eval/ の retrieval 実測と同一)。文書側は素のまま埋め込む。プレフィックスの付与は
// この llm パッケージに集約し、呼び出し側には漏らさない。
const embedQueryInstruction = "Instruct: Given a web search query, retrieve relevant passages that answer the query\nQuery: "

// EmbedRequest は 1 回の埋め込みに必要なパラメータ。Query が true ならクエリ側の
// instruction prefix を付けて埋め込む(検索クエリ用)。false は文書側(素のまま)。
type EmbedRequest struct {
	Model string
	Input string
	Query bool
}

// EmbedResult は 1 回の埋め込みの結果。Model はサーバーが実際に使ったモデル名
// (article_embeddings.model への記録用)。
type EmbedResult struct {
	Vector []float32
	Model  string
}

// embedBody は POST /embeddings(OpenAI 互換)のリクエスト本体。
type embedBody struct {
	Model string `json:"model,omitempty"`
	Input string `json:"input"`
}

// embedResponse は OpenAI 互換 /embeddings の応答。
type embedResponse struct {
	Model string `json:"model"`
	Data  []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed は req を 1 回埋め込み、ベクトルとモデル名を返す。
func (c *Client) Embed(ctx context.Context, req EmbedRequest) (EmbedResult, error) {
	ctx, cancel := context.WithTimeout(ctx, embedTimeout)
	defer cancel()

	input := req.Input
	if req.Query {
		input = embedQueryInstruction + input
	}

	resp, err := c.doRequest(ctx, "/embeddings", embedBody{Model: req.Model, Input: input})
	if err != nil {
		return EmbedResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	var parsed embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return EmbedResult{}, fmt.Errorf("decode embeddings response: %w (%w)", ErrUnavailable, err)
	}
	if len(parsed.Data) == 0 || len(parsed.Data[0].Embedding) == 0 {
		return EmbedResult{}, fmt.Errorf("no embedding in response: %w", ErrUnavailable)
	}

	return EmbedResult{Vector: parsed.Data[0].Embedding, Model: parsed.Model}, nil
}
