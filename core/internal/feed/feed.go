// Package feed はフィード取得・正規化のドメイン型とユースケースを持つ。
// DB・HTTP の具象は知らない(依存は消費側 interface 経由 — clean-architecture)。
package feed

import (
	"errors"
	"time"
)

// ドメイン境界の sentinel。httpapi がステータスコードへ写像する
var (
	ErrInvalidURL    = errors.New("invalid feed url")            // 400
	ErrPrivateHost   = errors.New("feed host is private")        // 400(SSRF ガード)
	ErrUpstreamFetch = errors.New("upstream fetch failed")       // 502
	ErrNotAFeed      = errors.New("content is not a valid feed") // 422
)

// Feed は購読フィード(feeds 行)。
type Feed struct {
	ID        int64     `json:"id"`
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
}

// Item はパース直後・保存前の記事(ID を持たない)。
type Item struct {
	GUID        string
	URL         string
	Title       string
	Content     string
	PublishedAt *time.Time
}

// Article は保存済み記事(articles 行)。store→httpapi を1型で貫く(guardrail #4)。
type Article struct {
	ID          int64      `json:"id"`
	FeedID      int64      `json:"feed_id"`
	GUID        string     `json:"guid"`
	URL         string     `json:"url"`
	Title       string     `json:"title"`
	Content     string     `json:"content"`
	PublishedAt *time.Time `json:"published_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// Conditional は条件付き GET の状態(最新 feed_fetches 行から導出)。
type Conditional struct {
	ETag         string
	LastModified string
}

// FetchResult は1回の取得の結果。
type FetchResult struct {
	StatusCode   int
	ETag         string
	LastModified string
	NotModified  bool // 304
	Title        string
	Items        []Item
}

// FetchRecord は feed_fetches へ追記するイベント。
type FetchRecord struct {
	StatusCode   int
	ETag         string
	LastModified string
	Error        string
}

// RegisterResult は登録ユースケースの結果。Created は HTTP 層が 201/200 の判定に使う。
type RegisterResult struct {
	Feed             Feed `json:"feed"`
	Created          bool `json:"-"`
	InsertedArticles int  `json:"inserted_articles"`
}
