package httpapi

import (
	"context"
	"net/http"
	"strconv"
)

// ArticleReadMarker は既読記録の消費側ポート(具象は *store.Store、main で注入)。
// 冪等 — 既に既読でもエラーにしない(重複行も作らない)。
type ArticleReadMarker interface {
	MarkArticleRead(ctx context.Context, articleID int64) error
}

// handleMarkArticleRead は POST /api/v1/articles/{id}/read。既読の事実を記録する。
// 冪等: 既読済みでも 204(fulltext と同じ作法で、記事の存在確認を先に行い無ければ 404)。
func handleMarkArticleRead(articles ArticleGetter, reads ArticleReadMarker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid article id")
			return
		}

		_, found, err := articles.GetArticle(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if !found {
			writeError(w, http.StatusNotFound, "article not found")
			return
		}

		if err := reads.MarkArticleRead(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
