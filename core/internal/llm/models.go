package llm

// router mode(ADR00020)の役割別モデル別名の既定値。preset ファイル(compose の llm
// サービスが ro マウント)側の宣言と一致させる。上書きは環境変数(LLM_MODEL_FAST /
// LLM_MODEL_EMBEDDING / LLM_MODEL_AGGREGATE — 読むのは composition root の main)。
const (
	// DefaultFastModel は高速パス(要約・タグ抽出 — tenets §3.3)。
	DefaultFastModel = "qwen3.5-4b"
	// DefaultEmbeddingModel は埋め込み(ADR00008)。
	DefaultEmbeddingModel = "qwen3-embedding-0.6b"
	// DefaultAggregateModel は集約層(Q&A 回答生成)。
	DefaultAggregateModel = "gpt-oss-20b"
)

// Models は役割別モデル別名の束(main が環境変数から組んで各アダプタへ配る)。
type Models struct {
	Fast      string
	Embedding string
	Aggregate string
}
