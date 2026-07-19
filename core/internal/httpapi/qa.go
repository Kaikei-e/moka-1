package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Kaikei-e/moka-1/core/internal/feed"
	"github.com/Kaikei-e/moka-1/core/internal/rag"
)

// ArticleAnswerer は問い返し Q&A ユースケースの消費側ポート(具象は *rag.Answerer、main で注入)。
// onSources には検索で文脈に選ばれた記事が(回答生成の前に)1回渡り、onDelta には think
// 除去済みの回答断片が到着順に渡る。
type ArticleAnswerer interface {
	Ask(ctx context.Context, article feed.Article, question string,
		onSources func([]rag.Source), onDelta func(delta string)) (rag.AnswerResult, error)
}

// askBody は POST /api/v1/articles/{id}/qa のリクエスト本体。
type askBody struct {
	Question string `json:"question"`
}

// handleAskArticle は POST /api/v1/articles/{id}/qa。summarize の stream ハンドラと同じ
// SSE(event/data 形式)で応答する。イベント順: sources(文脈記事)→ delta(回答断片)
// → done(question_id / answer_id)。ヘッダ送出後は HTTP ステータスを変更できないため、
// 途中失敗は SSE の error イベントで伝える(HTTP ステータス自体は 200 のまま)。
func handleAskArticle(articles ArticleGetter, answerer ArticleAnswerer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid article id")
			return
		}

		var body askBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		question := strings.TrimSpace(body.Question)
		if question == "" {
			writeError(w, http.StatusBadRequest, "missing question")
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

		res, err := answerer.Ask(r.Context(), a, question,
			func(sources []rag.Source) {
				if sources == nil {
					sources = []rag.Source{} // JSON では null でなく [] を返す
				}
				writeEvent("sources", map[string]any{"articles": sources})
			},
			func(delta string) {
				writeEvent("delta", map[string]string{"text": delta})
			})
		if err != nil {
			writeEvent("error", map[string]string{"message": qaErrorMessage(err)})
			return
		}

		writeEvent("done", map[string]any{"question_id": res.QuestionID, "answer_id": res.AnswerID})
	}
}

// qaErrorMessage はドメイン sentinel を SSE error イベントのメッセージへ写像する。
func qaErrorMessage(err error) string {
	switch {
	case errors.Is(err, rag.ErrLLMUnavailable):
		return "llm unavailable"
	case errors.Is(err, rag.ErrEmptyAnswer):
		return "answer generation produced no content"
	default:
		return "internal error"
	}
}
