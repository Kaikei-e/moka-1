package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Kaikei-e/moka-1/core/internal/feed"
)

// FeedRegistrar は登録ユースケースの消費側ポート(具象は feed.Registrar、main で注入)。
type FeedRegistrar interface {
	Register(ctx context.Context, rawURL string) (feed.RegisterResult, error)
}

// FeedLister は登録済みフィード一覧の消費側ポート(具象は *store.Store、main で注入)。
type FeedLister interface {
	ListFeeds(ctx context.Context) ([]feed.Feed, error)
}

// handleListFeeds は GET /api/v1/feeds。新しい順(created_at DESC)。
func handleListFeeds(feeds FeedLister) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := feeds.ListFeeds(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if list == nil {
			list = []feed.Feed{} // JSON では null でなく [] を返す
		}
		writeJSON(w, http.StatusOK, map[string][]feed.Feed{"feeds": list})
	}
}

// handleRegisterFeed は POST /api/v1/feeds。登録は冪等: 新規 201 / 既存 200。
// 要約は自動キックしない — 読者の明示操作(POST /api/v1/articles/{id}/summary)のみが
// 引き金(fulltext と同じ作法。ユーザー指示によりM1の自動フックは見送り)。
func handleRegisterFeed(feeds FeedRegistrar) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		rawURL := strings.TrimSpace(req.URL)
		if rawURL == "" {
			writeError(w, http.StatusBadRequest, "invalid feed url")
			return
		}

		res, err := feeds.Register(r.Context(), rawURL)
		if err != nil {
			status, msg := registerErrorStatus(err)
			writeError(w, status, msg)
			return
		}

		status := http.StatusOK
		if res.Created {
			status = http.StatusCreated
		}
		writeJSON(w, status, res)
	}
}

// registerErrorStatus はドメイン sentinel を HTTP ステータスへ写像する。
// SSRF ブロック(ErrPrivateHost)は ErrInvalidURL と同一バケット — プローブ情報を漏らさない。
func registerErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, feed.ErrInvalidURL), errors.Is(err, feed.ErrPrivateHost):
		return http.StatusBadRequest, "invalid feed url"
	case errors.Is(err, feed.ErrNotAFeed):
		return http.StatusUnprocessableEntity, "content is not a valid feed"
	case errors.Is(err, feed.ErrUpstreamFetch):
		return http.StatusBadGateway, "upstream fetch failed"
	default:
		return http.StatusInternalServerError, "internal error"
	}
}
