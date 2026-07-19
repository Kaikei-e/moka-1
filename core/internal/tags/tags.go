// Package tags は記事の「タグ抽出」— LLM による濃縮の一つ(要約と並ぶ増強)— の
// ドメイン型を持つ。DB・HTTP・LLM クライアントの具象は知らない(依存は消費側 interface
// 経由 — clean-architecture)。internal/summarize と相似形(ADR00007 D5: タグ抽出は
// 高速パスモデルの構造化出力で、品質はLFM2.5と同格・json_schemaパース成功率100%)。
package tags

import (
	"context"
	"errors"
	"strings"

	"github.com/Kaikei-e/moka-1/core/internal/fulltext"
)

// ドメイン境界の sentinel。httpapi がステータスコードへ写像する。
var (
	// ErrNoContent は記事にタグ抽出対象のテキスト(全文・content とも)が無い場合。
	ErrNoContent = errors.New("no content to tag")
	// ErrArticleTooLong はテキストがモデルの実効コンテキストを超える場合(LLM 呼び出し前のガード)。
	ErrArticleTooLong = errors.New("article too long to tag")
	// ErrLLMUnavailable は llm サーバーへの呼び出し失敗全般(タイムアウト・接続エラー・非2xx)。
	ErrLLMUnavailable = errors.New("llm unavailable")
	// ErrEmptyExtraction は think 除去後に本文が空、think タグが閉じずに truncate された場合、
	// または JSON デコード後にタグが 1 件も残らなかった場合。
	ErrEmptyExtraction = errors.New("empty tag extraction")
	// ErrInvalidTags は response_format(json_schema) 制約下にもかかわらず応答が
	// スキーマ({tags: string[]})としてデコードできなかった場合。
	ErrInvalidTags = errors.New("invalid tags response")
)

// maxInputTokens はモデル呼び出し前のクライアント側ガード。summarize と同じ実効コンテキスト
// 16384 トークン(2026-07-19 に 8192 から拡張、summarize.go 参照)から、システムプロンプトと
// タグ抽出の max_tokens(256、要約よりずっと短い)分を差し引いた入力予算。
// 正確なトークナイザは導入せず、estimateTokens の保守的(過大)見積りと
// 比較して、超過は ErrArticleTooLong で明示エラーにする(黙ってトランケートしない)。
const maxInputTokens = 15000

// estimateTokens は入力テキストのトークン数を保守的(実際より多め)に見積もる。
// summarize.estimateTokens と同じ考え方(ASCII 4文字≒1トークン、他は1文字≒2トークン)。
func estimateTokens(text string) int {
	ascii, other := 0, 0
	for _, r := range text {
		if r < 128 {
			ascii++
		} else {
			other++
		}
	}
	return ascii/4 + other*2
}

// Result はタグ抽出ユースケースの結果。Created は HTTP 層が 201/200 の判定に使う。
type Result struct {
	Tags    []string
	Created bool
}

// CompletionResult は LLM チャット補完 1 回分の生の結果(think 除去前・JSON パース前)。
type CompletionResult struct {
	Text string
	Meta map[string]any
}

// Store はタグ抽出ユースケースの永続化ポート(消費側定義 — 具象は internal/store)。
type Store interface {
	// LatestTags は記事に付いている現在のタグ名一覧を返す。1件も無ければ found=false。
	LatestTags(ctx context.Context, articleID int64) (tags []string, found bool, err error)
	// UpsertTags は names を tags(正規化語彙)/article_tags へ追記する(ADR00002: 削除はしない)。
	UpsertTags(ctx context.Context, articleID int64, names []string, modelMeta map[string]any) error
	InsertEnrichmentAttempt(ctx context.Context, articleID int64, kind, outcome, errMsg string) error
}

// FullTextLookup は全文取り寄せ済みテキストの参照ポート(summarize.FullTextLookup と同じ形)。
type FullTextLookup interface {
	LatestFullText(ctx context.Context, articleID int64) (fulltext.FullText, bool, error)
}

// Completer は LLM チャット補完(json_schema 制約付き)の消費側ポート(具象は *LLMCompleter)。
type Completer interface {
	Extract(ctx context.Context, text string) (CompletionResult, error)
}

// tagsPayload は response_format(json_schema) で強制される応答の形。
type tagsPayload struct {
	Tags []string `json:"tags"`
}

// sanitizeTags は LLM 応答由来のタグ名を整形する: 前後空白を落とし、空文字列を除き、
// 出現順を保ったまま重複を除く。json_schema 制約下でも防御的に行う。
func sanitizeTags(raw []string) []string {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}
