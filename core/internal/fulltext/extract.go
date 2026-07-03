package fulltext

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"

	"github.com/go-shiori/dom"
	"github.com/markusmobius/go-trafilatura"
)

// TrafilaturaExtractor は go-trafilatura で本文を取り出す(独立ベンチ F1=0.960、
// tenets §7 の選定根拠)。go-trafilatura の型はこのファイルの外に漏らさない。
type TrafilaturaExtractor struct{}

// NewTrafilaturaExtractor は抽出器を組む(状態を持たないので値は使い回してよい)。
func NewTrafilaturaExtractor() *TrafilaturaExtractor {
	return &TrafilaturaExtractor{}
}

// Extract は HTML から本文を取り出し、段落構造を保った HTML 文字列(<p> タグ区切り)を返す。
// フロントエンドの toParagraphs がフィード由来の content と同じ経路で安全にパースできるよう、
// プレーンテキストではなくマークアップのまま渡す(go-trafilatura の ContentText は改行を
// 保持せず全段落を1行に平坦化してしまうため使わない)。フォールバック抽出(go-readability /
// go-domdistiller)を有効にし、難しいページでも取りこぼしを減らす。
func (TrafilaturaExtractor) Extract(rawHTML []byte, pageURL string) (string, error) {
	u, err := url.Parse(pageURL)
	if err != nil {
		return "", fmt.Errorf("parse page url %s: %w", pageURL, ErrExtractFailed)
	}

	result, err := trafilatura.Extract(bytes.NewReader(rawHTML), trafilatura.Options{
		OriginalURL:    u,
		EnableFallback: true,
	})
	if err != nil {
		return "", fmt.Errorf("extract %s: %w (%w)", pageURL, ErrExtractFailed, err)
	}
	if result.ContentNode == nil {
		return "", fmt.Errorf("extract %s: empty result: %w", pageURL, ErrExtractFailed)
	}

	// 見出しは読書ビューがタイトルとして別枠で表示する。段落として重複させない
	for _, h := range dom.QuerySelectorAll(result.ContentNode, "h1, h2, h3, h4, h5, h6") {
		if h.Parent != nil {
			h.Parent.RemoveChild(h)
		}
	}

	text := strings.TrimSpace(dom.OuterHTML(result.ContentNode))
	text = strings.TrimPrefix(text, "<body>")
	text = strings.TrimSuffix(text, "</body>")
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("extract %s: empty result: %w", pageURL, ErrExtractFailed)
	}
	return text, nil
}
