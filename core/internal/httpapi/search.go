package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/Kaikei-e/moka-1/core/internal/rag"
)

// ArticleSearcher はハイブリッド検索の消費側ポート(具象は *rag.Searcher、main で注入)。
type ArticleSearcher interface {
	Search(ctx context.Context, q string, limit int) ([]rag.SearchHit, error)
}

const (
	defaultSearchLimit = 20
	maxSearchLimit     = 50
)

// handleSearch は GET /api/v1/search?q=&limit=。q 空は 400。記事表現は一覧 API と同じ
// (rag.SearchHit が feed.Article を埋め込む)で、RRF 融合スコアを score として添える。
// llm 停止時のテキスト側単独への縮退は rag.Searcher の責務(ここは 200 を返すだけ)。
func handleSearch(searcher ArticleSearcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			writeError(w, http.StatusBadRequest, "missing query")
			return
		}

		limit, err := queryInt(r, "limit", defaultSearchLimit)
		if err != nil || limit < 1 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = min(limit, maxSearchLimit)

		items, err := searcher.Search(r.Context(), q, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if items == nil {
			items = []rag.SearchHit{} // JSON では null でなく [] を返す(一覧APIと同じ契約)
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}
