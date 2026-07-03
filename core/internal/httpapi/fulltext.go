package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/Kaikei-e/moka-1/core/internal/fulltext"
)

// FullTextFetcher は取り寄せユースケースの消費側ポート(具象は *fulltext.Service、main で注入)。
type FullTextFetcher interface {
	FetchFullText(ctx context.Context, articleID int64, articleURL string) (fulltext.Result, error)
}

// handleFetchFullText は POST /api/v1/articles/{id}/fulltext。冪等: 新規 201 / 既存 200。
// 記事が無ければ 404(取り寄せ先の URL を articles テーブルから引く)。
func handleFetchFullText(articles ArticleGetter, fullTexts FullTextFetcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid article id")
			return
		}

		a, found, err := articles.GetArticle(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if !found {
			writeError(w, http.StatusNotFound, "article not found")
			return
		}

		res, err := fullTexts.FetchFullText(r.Context(), id, a.URL)
		if err != nil {
			status, msg := fullTextErrorStatus(err)
			writeError(w, status, msg)
			return
		}

		status := http.StatusOK
		if res.Created {
			status = http.StatusCreated
		}
		writeJSON(w, status, map[string]fulltext.FullText{"fulltext": res.FullText})
	}
}

// fullTextErrorStatus はドメイン sentinel を HTTP ステータスへ写像する。
// SSRF ブロック(ErrPrivateHost)は ErrInvalidURL と同一バケット — プローブ情報を漏らさない
// (feeds.go の registerErrorStatus と同じ方針)。
func fullTextErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, fulltext.ErrInvalidURL), errors.Is(err, fulltext.ErrPrivateHost):
		return http.StatusBadRequest, "invalid article url"
	case errors.Is(err, fulltext.ErrExtractFailed):
		return http.StatusUnprocessableEntity, "content could not be extracted"
	case errors.Is(err, fulltext.ErrUpstreamFetch):
		return http.StatusBadGateway, "upstream fetch failed"
	default:
		return http.StatusInternalServerError, "internal error"
	}
}
