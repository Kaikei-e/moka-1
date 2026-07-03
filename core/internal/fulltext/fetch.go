package fulltext

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Kaikei-e/moka-1/core/internal/feed"
)

// maxPageBytes は記事ページ本体の読み取り上限(異常に巨大な応答からの防御)。
const maxPageBytes = 10 << 20 // 10 MiB

// fetchTimeout は 1 回のページ取得のデッドライン。
const fetchTimeout = 30 * time.Second

// HTTPFetcher は記事ページ本体の取得アダプタ(feed.HTTPFetcher と異なり gofeed は使わず、
// 生の HTML バイト列を返す — 抽出は Extractor の責務)。
type HTTPFetcher struct {
	client *http.Client
}

// NewHTTPFetcher は取得クライアントを組む。初回 URL の検証は呼び出し側(Service)の
// 責務で、fetcher はリダイレクト先だけを再検証する(feed.HTTPFetcher と同じ作法)。
func NewHTTPFetcher(v *feed.URLValidator) *HTTPFetcher {
	return &HTTPFetcher{
		client: &http.Client{
			Timeout: fetchTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("too many redirects")
				}
				return v.Validate(req.Context(), req.URL.String())
			},
		},
	}
}

// FetchPage は記事ページを GET し、本体を生バイト列で返す。
func (f *HTTPFetcher) FetchPage(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request %s: %w", rawURL, ErrInvalidURL)
	}
	req.Header.Set("User-Agent", "moka/0.1 (+https://github.com/Kaikei-e/moka-1)")

	resp, err := f.client.Do(req)
	if err != nil {
		// 二重 %w: ErrUpstreamFetch と(リダイレクト再検証の)ErrPrivateHost の両系譜を保つ
		return nil, fmt.Errorf("fetch %s: %w (%w)", rawURL, ErrUpstreamFetch, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("fetch %s: status %d: %w", rawURL, resp.StatusCode, ErrUpstreamFetch)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rawURL, ErrUpstreamFetch)
	}
	return body, nil
}
