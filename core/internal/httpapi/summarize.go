package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/Kaikei-e/moka-1/core/internal/summarize"
)

// ArticleSummarizer は要約ユースケースの消費側ポート(具象は *summarize.Service、main で注入)。
type ArticleSummarizer interface {
	// force が true なら既存の要約があっても無視し、常に新規生成する(読者が品質に
	// 満足できず明示的に「やり直す」場合。クエリパラメータ ?force=true で起動する)。
	Summarize(ctx context.Context, articleID int64, articleContent string, force bool) (summarize.Result, error)
	// SummarizeStream は Summarize のストリーミング版。onDelta は think 除去済みの
	// 文字列断片を到着順に呼ばれる(専用の /summary/stream ハンドラ用)。
	SummarizeStream(
		ctx context.Context, articleID int64, articleContent string, force bool, onDelta func(delta string),
	) (summarize.Result, error)
}

// forceRegenerate は ?force=true クエリパラメータを読む(要約の明示的なやり直し用)。
func forceRegenerate(r *http.Request) bool {
	return r.URL.Query().Get("force") == "true"
}

// SummaryReader は要約の読み取り専用ポート(具象は *store.Store)。LLM は一切呼ばない —
// GET /summary は「濃縮ずみなら見せる」だけの窓口(enrich.Scheduler が自動生成した要約を
// UI がボタンを押さず確認できるようにする、grill決定)。
type SummaryReader interface {
	LatestSummary(ctx context.Context, articleID int64) (summarize.Summary, bool, error)
}

// handleGetSummary は GET /api/v1/articles/{id}/summary。純粋な読み取り — LLM を呼ばない。
// 要約が無ければ 404(まだ enrich.Scheduler が処理していない、または恒久的に失敗した)。
func handleGetSummary(articles ArticleGetter, reader SummaryReader) http.HandlerFunc {
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

		sum, found, err := reader.LatestSummary(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if !found {
			writeError(w, http.StatusNotFound, "summary not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]summarize.Summary{"summary": sum})
	}
}

// handleSummarizeArticle は POST /api/v1/articles/{id}/summary。冪等: 新規 201 / 既存 200。
// ?force=true を付けると既存要約があっても無視して常に新規生成する(この場合も 201)。
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

		clearWriteDeadline(w)

		res, err := summarizer.Summarize(r.Context(), id, a.Content, forceRegenerate(r))
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
// error イベントで伝える(HTTP ステータス自体は 200 のまま)。?force=true で既存要約
// があっても無視し常に新規生成する(handleSummarizeArticle と同じ規約)。
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

		clearWriteDeadline(w)

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

		res, err := summarizer.SummarizeStream(r.Context(), id, a.Content, forceRegenerate(r), func(delta string) {
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

// clearWriteDeadline は要約系ハンドラの書き込みデッドラインを解除する。サーバー全体の
// WriteTimeout(60秒)は LLM の completeTimeout(60秒)と同値のため、生成が上限に近づくと
// done/error イベントを送る前に接続が強制切断される — LLM を待つルートだけ上限を外し、
// 応答の長さは completeTimeout 側で抑える。失敗してもストリーミング自体は試みる(fail-soft)。
func clearWriteDeadline(w http.ResponseWriter) {
	if err := http.NewResponseController(w).SetWriteDeadline(time.Time{}); err != nil {
		slog.Warn("clear write deadline", "err", err.Error())
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
