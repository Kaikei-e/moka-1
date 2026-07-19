package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/Kaikei-e/moka-1/core/internal/tags"
)

// ArticleTagger はタグ抽出ユースケースの消費側ポート(具象は *tags.Service、main で注入)。
// M1 では force(やり直し)を持たない(tags.Service 自体が持たない — grill決定)。
type ArticleTagger interface {
	Tag(ctx context.Context, articleID int64, articleContent string) (tags.Result, error)
}

// TagsReader はタグの読み取り専用ポート(具象は *store.Store)。LLM は一切呼ばない —
// GET /tags は「濃縮ずみなら見せる」だけの窓口(summary の GET と対称)。
type TagsReader interface {
	LatestTags(ctx context.Context, articleID int64) ([]string, bool, error)
}

// handleTagArticle は POST /api/v1/articles/{id}/tags。冪等: 新規 201 / 既存 200。
// force は無い(article_tags は追記のみで削除しないため、summarize の force ほど意味を持たない)。
func handleTagArticle(articles ArticleGetter, tagger ArticleTagger) http.HandlerFunc {
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

		res, err := tagger.Tag(r.Context(), id, a.Content)
		if err != nil {
			status, msg := tagsErrorStatus(err)
			writeError(w, status, msg)
			return
		}

		status := http.StatusOK
		if res.Created {
			status = http.StatusCreated
		}
		writeJSON(w, status, map[string]any{"tags": res.Tags})
	}
}

// handleGetTags は GET /api/v1/articles/{id}/tags。純粋な読み取り — LLM を呼ばない。
// タグが無ければ 404(まだ enrich.Scheduler が処理していない、または恒久的に失敗した)。
func handleGetTags(articles ArticleGetter, reader TagsReader) http.HandlerFunc {
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

		names, found, err := reader.LatestTags(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if !found {
			writeError(w, http.StatusNotFound, "tags not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tags": names})
	}
}

// tagsErrorStatus はドメイン sentinel を HTTP ステータスへ写像する(summarizeErrorStatus と同じ形)。
func tagsErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, tags.ErrNoContent), errors.Is(err, tags.ErrArticleTooLong):
		return http.StatusBadRequest, "article cannot be tagged"
	case errors.Is(err, tags.ErrEmptyExtraction), errors.Is(err, tags.ErrInvalidTags):
		return http.StatusUnprocessableEntity, "tag extraction produced no usable tags"
	case errors.Is(err, tags.ErrLLMUnavailable):
		return http.StatusBadGateway, "llm unavailable"
	default:
		return http.StatusInternalServerError, "internal error"
	}
}
