package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/Kaikei-e/moka-1/core/internal/summarize"
)

// ArticleSummarizer は要約ユースケースの消費側ポート(具象は *summarize.Service、main で注入)。
type ArticleSummarizer interface {
	Summarize(ctx context.Context, articleID int64, articleContent string) (summarize.Result, error)
}

// handleSummarizeArticle は POST /api/v1/articles/{id}/summary。冪等: 新規 201 / 既存 200。
// 記事が無ければ 404(要約対象のフィード由来 content は articles テーブルから引く。
// 全文取り寄せ済みならそちらを優先するかどうかは summarize.Service の責務)。
func handleSummarizeArticle(articles ArticleGetter, summarizer ArticleSummarizer) http.HandlerFunc {
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

		res, err := summarizer.Summarize(r.Context(), id, a.Content)
		if err != nil {
			status, msg := summarizeErrorStatus(err)
			writeError(w, status, msg)
			return
		}

		status := http.StatusOK
		if res.Created {
			status = http.StatusCreated
		}
		writeJSON(w, status, map[string]summarize.Summary{"summary": res.Summary})
	}
}

// summarizeErrorStatus はドメイン sentinel を HTTP ステータスへ写像する。
func summarizeErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, summarize.ErrNoContent), errors.Is(err, summarize.ErrArticleTooLong):
		return http.StatusBadRequest, "article cannot be summarized"
	case errors.Is(err, summarize.ErrEmptyCompletion):
		return http.StatusUnprocessableEntity, "summary generation produced no content"
	case errors.Is(err, summarize.ErrLLMUnavailable):
		return http.StatusBadGateway, "llm unavailable"
	default:
		return http.StatusInternalServerError, "internal error"
	}
}
