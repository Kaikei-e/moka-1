package feed

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/mmcdole/gofeed"
)

// maxFeedBytes はフィード本文の読み取り上限(異常に巨大な応答からの防御)。
const maxFeedBytes = 10 << 20 // 10 MiB

// HTTPFetcher は外部フィードの取得アダプタ。gofeed の型はこのファイルの外に漏らさない。
type HTTPFetcher struct {
	client *http.Client
}

// NewHTTPFetcher は取得クライアントを組む。初回 URL の検証は呼び出し側(Registrar)の
// 責務で、fetcher はリダイレクト先だけを再検証する(validate→fetch 間 TOCTOU の緩和)。
func NewHTTPFetcher(v *URLValidator) *HTTPFetcher {
	return &HTTPFetcher{
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("too many redirects")
				}
				return v.Validate(req.Context(), req.URL.String())
			},
		},
	}
}

// Fetch は条件付き GET でフィードを取得しパースする。
// 非2xx(304 以外)とトランスポート失敗は ErrUpstreamFetch、パース不能は ErrNotAFeed。
// 失敗時もイベント記録(feed_fetches)用に取得できた範囲の FetchResult を返す。
func (f *HTTPFetcher) Fetch(ctx context.Context, rawURL string, cond Conditional) (FetchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return FetchResult{}, fmt.Errorf("build request %s: %w", rawURL, ErrInvalidURL)
	}
	req.Header.Set("User-Agent", "moka/0.1 (+https://github.com/Kaikei-e/moka-1)")
	if cond.ETag != "" {
		req.Header.Set("If-None-Match", cond.ETag)
	}
	if cond.LastModified != "" {
		req.Header.Set("If-Modified-Since", cond.LastModified)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		// 二重 %w: ErrUpstreamFetch と(リダイレクト再検証の)ErrPrivateHost の両系譜を保つ
		return FetchResult{}, fmt.Errorf("fetch %s: %w (%w)", rawURL, ErrUpstreamFetch, err)
	}
	defer func() { _ = resp.Body.Close() }()

	res := FetchResult{
		StatusCode:   resp.StatusCode,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
	}

	switch {
	case resp.StatusCode == http.StatusNotModified:
		res.NotModified = true
		return res, nil
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return res, fmt.Errorf("fetch %s: status %d: %w", rawURL, resp.StatusCode, ErrUpstreamFetch)
	}

	parsed, err := gofeed.NewParser().Parse(io.LimitReader(resp.Body, maxFeedBytes))
	if err != nil {
		return res, fmt.Errorf("parse %s: %w", rawURL, ErrNotAFeed)
	}

	res.Title = parsed.Title
	res.Items = normalizeItems(parsed.Items)
	return res, nil
}

// normalizeItems はパース結果をドメインの Item に正規化する。
// GUID は guid → link の順でフォールバックし、両方無い item は識別不能なので捨てる。
func normalizeItems(items []*gofeed.Item) []Item {
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if it == nil {
			continue
		}
		guid := it.GUID
		if guid == "" {
			guid = it.Link
		}
		if guid == "" {
			continue
		}
		url := it.Link
		if url == "" {
			url = guid
		}
		content := it.Content
		if content == "" {
			content = it.Description
		}
		out = append(out, Item{
			GUID:        guid,
			URL:         url,
			Title:       it.Title,
			Content:     content,
			PublishedAt: it.PublishedParsed,
		})
	}
	return out
}
