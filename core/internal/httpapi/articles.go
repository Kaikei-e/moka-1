package httpapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/Kaikei-e/moka-1/core/internal/feed"
)

// ArticleLister は記事一覧の消費側ポート(具象は *store.Store、main で注入)。
type ArticleLister interface {
	ListArticles(ctx context.Context, limit int, cursor *feed.ArticleCursor) ([]feed.Article, error)
}

// ArticleGetter は記事単体(読書ビュー)の消費側ポート(具象は *store.Store、main で注入)。
type ArticleGetter interface {
	GetArticle(ctx context.Context, id int64) (feed.Article, bool, error)
}

// handleGetArticle は GET /api/v1/articles/{id}。無い記事は 404。
func handleGetArticle(articles ArticleGetter) http.HandlerFunc {
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
		writeJSON(w, http.StatusOK, map[string]feed.Article{"article": a})
	}
}

const (
	defaultListLimit = 50
	maxListLimit     = 200
)

// handleListArticles は GET /api/v1/articles?limit=&cursor=。新しい順(published_at DESC)、
// カーソルベース(keyset)ページング。next_cursor が null になったら終端。
func handleListArticles(articles ArticleLister) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, err := queryInt(r, "limit", defaultListLimit)
		if err != nil || limit < 1 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = min(limit, maxListLimit)

		var cursor *feed.ArticleCursor
		if s := r.URL.Query().Get("cursor"); s != "" {
			c, err := feed.DecodeArticleCursor(s)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid cursor")
				return
			}
			cursor = &c
		}

		list, err := articles.ListArticles(r.Context(), limit, cursor)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if list == nil {
			list = []feed.Article{} // JSON では null でなく [] を返す
		}

		// 満杯ページのみ続きがありうる。next_cursor は常にキーとして返す(契約を安定させる)
		var next *string
		if len(list) == limit {
			last := list[len(list)-1]
			sortKey := last.CreatedAt
			if last.PublishedAt != nil {
				sortKey = *last.PublishedAt
			}
			s := feed.ArticleCursor{SortKey: sortKey, ID: last.ID}.Encode()
			next = &s
		}
		writeJSON(w, http.StatusOK, map[string]any{"articles": list, "next_cursor": next})
	}
}

func queryInt(r *http.Request, key string, def int) (int, error) {
	s := r.URL.Query().Get(key)
	if s == "" {
		return def, nil
	}
	return strconv.Atoi(s)
}
