// Package fulltext は記事の「取り寄せ」— 読者の求めに応じて外部サイトから記事全文を
// 連れてくるユースケース — のドメイン型を持つ。DB・HTTP・抽出器の具象は知らない
// (依存は消費側 interface 経由 — clean-architecture)。濃縮(LLM 生成物)とは別概念。
package fulltext

import (
	"context"
	"errors"
	"time"

	"github.com/Kaikei-e/moka-1/core/internal/feed"
)

// URL 検証・取得のエラー分類は feed パッケージのものをそのまま再利用する
// (SSRF ガードは feed.URLValidator の 1 実装のみに保つ — 二重実装しない)。
var (
	ErrInvalidURL    = feed.ErrInvalidURL
	ErrPrivateHost   = feed.ErrPrivateHost
	ErrUpstreamFetch = feed.ErrUpstreamFetch
	// ErrExtractFailed は取得はできたが本文抽出に失敗した(空文字含む)場合。
	ErrExtractFailed = errors.New("full text extraction failed")
)

// FullText は取り寄せた記事全文(article_fulltexts 行)。Text は段落構造を保った
// HTML 文字列(<p> タグ区切り) — フィード由来の articles.content と同じ形で、
// フロントエンドは同じ toParagraphs で安全にパースする。
type FullText struct {
	ArticleID int64     `json:"article_id"`
	Text      string    `json:"text"`
	FetchedAt time.Time `json:"fetched_at"`
}

// Store は取り寄せユースケースの永続化ポート(消費側定義 — 具象は internal/store)。
type Store interface {
	LatestFullText(ctx context.Context, articleID int64) (FullText, bool, error)
	InsertFullText(ctx context.Context, articleID int64, text string) (FullText, error)
}

// PageFetcher は記事ページ本体の HTTP 取得ポート(具象は HTTPFetcher)。
type PageFetcher interface {
	FetchPage(ctx context.Context, url string) ([]byte, error)
}

// Extractor は取得済み HTML から本文(段落構造を保った HTML)を取り出すポート
// (具象は go-trafilatura ラッパ)。
type Extractor interface {
	Extract(html []byte, pageURL string) (string, error)
}

// Validator は URL 検証ポート(具象は *feed.URLValidator — main が共有インスタンスを注入)。
type Validator interface {
	Validate(ctx context.Context, raw string) error
}
