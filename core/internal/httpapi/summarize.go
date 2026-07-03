package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/Kaikei-e/moka-1/core/internal/summarize"
)

// ArticleSummarizer は要約ユースケースの消費側ポート(具象は *summarize.Service、main で注入)。
type ArticleSummarizer interface {
	Summarize(ctx context.Context, articleID int64, articleContent string) (summarize.Result, error)
	// SummarizeStream は Summarize のストリーミング版。onDelta は think 除去済みの
	// 文字列断片を到着順に呼ばれる(専用の /summary/stream ハンドラ用)。
	SummarizeStream(
		ctx context.Context, articleID int64, articleContent string, onDelta func(delta string),
	) (summarize.Result, error)
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

// handleSummarizeArticleStream は POST /api/v1/articles/{id}/summary/stream。
// 一括JSON版(handleSummarizeArticle)とは別の専用エンドポイント — fulltext型の
// 「1機能1エンドポイント」規約を保ちつつ、応答形式(SSE)が全く異なるため分離した。
// 冪等な場合も含め常に text/event-stream で応答する: 既存要約があれば1回の delta
// イベントで全文を送ってから done、新規生成なら think 除去済みの断片を逐次 delta で
// 送る。ヘッダ送出後は HTTP ステータスを変更できないため、途中失敗は SSE の
// error イベントで伝える(HTTP ステータス自体は 200 のまま)。
func handleSummarizeArticleStream(articles ArticleGetter, summarizer ArticleSummarizer) http.HandlerFunc {
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

		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "streaming unsupported")
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no") // リバースプロキシのバッファリングを止める
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		writeEvent := func(event string, payload any) {
			data, _ := json.Marshal(payload)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
			flusher.Flush()
		}

		res, err := summarizer.SummarizeStream(r.Context(), id, a.Content, func(delta string) {
			writeEvent("delta", map[string]string{"text": delta})
		})
		if err != nil {
			status, msg := summarizeErrorStatus(err)
			writeEvent("error", map[string]any{"error": msg, "status": status})
			return
		}

		writeEvent("done", map[string]any{"summary": res.Summary, "created": res.Created})
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
