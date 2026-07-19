// Package httpapi は moka-core の HTTP ハンドラを持つ。ルーティングは stdlib の
// http.ServeMux(メソッド+パスパターン)のみ(bp-go: フレームワークは入れない)。
package httpapi

import (
	"encoding/json"
	"net/http"
)

// NewMux は moka-core の全ルートを配線した mux を返す。依存は消費側ポートで受け、
// 具象は cmd/moka/main.go(composition root)が注入する。
// API はバージョニングする(/api/v1/)。より specific なパターンが /api/ スタブに勝つ。
func NewMux(
	feeds FeedRegistrar,
	feedList FeedLister,
	feedDelete FeedDeleter,
	articles ArticleLister,
	article ArticleGetter,
	reads ArticleReadMarker,
	fullTexts FullTextFetcher,
	summarizer ArticleSummarizer,
	summaryReader SummaryReader,
	tagger ArticleTagger,
	tagsReader TagsReader,
	searcher ArticleSearcher,
	answerer ArticleAnswerer,
	authn Authenticator,
) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("POST /api/v1/feeds", handleRegisterFeed(feeds))
	mux.HandleFunc("GET /api/v1/feeds", handleListFeeds(feedList))
	mux.HandleFunc("DELETE /api/v1/feeds/{id}", handleDeleteFeed(feedDelete))
	mux.HandleFunc("GET /api/v1/articles", handleListArticles(articles))
	mux.HandleFunc("GET /api/v1/articles/{id}", handleGetArticle(article))
	mux.HandleFunc("POST /api/v1/articles/{id}/read", handleMarkArticleRead(article, reads))
	mux.HandleFunc("POST /api/v1/articles/{id}/fulltext", handleFetchFullText(article, fullTexts))
	mux.HandleFunc("GET /api/v1/articles/{id}/summary", handleGetSummary(article, summaryReader))
	mux.HandleFunc("POST /api/v1/articles/{id}/summary", handleSummarizeArticle(article, summarizer))
	mux.HandleFunc("POST /api/v1/articles/{id}/summary/stream", handleSummarizeArticleStream(article, summarizer))
	mux.HandleFunc("GET /api/v1/articles/{id}/tags", handleGetTags(article, tagsReader))
	mux.HandleFunc("POST /api/v1/articles/{id}/tags", handleTagArticle(article, tagger))
	mux.HandleFunc("GET /api/v1/search", handleSearch(searcher))
	mux.HandleFunc("POST /api/v1/articles/{id}/qa", handleAskArticle(article, answerer))
	mux.HandleFunc("GET /api/v1/auth/status", handleAuthStatus(authn))
	mux.HandleFunc("POST /api/v1/auth/register/begin", handleRegisterBegin(authn))
	mux.HandleFunc("POST /api/v1/auth/register/finish", handleRegisterFinish(authn))
	mux.HandleFunc("POST /api/v1/auth/login/begin", handleLoginBegin(authn))
	mux.HandleFunc("POST /api/v1/auth/login/finish", handleLoginFinish(authn))
	// メソッド無しパターンはメソッド不一致時の受け皿(無いと /api/ スタブが 501 で拾ってしまう)
	mux.HandleFunc("/api/v1/feeds", handleMethodNotAllowed)
	mux.HandleFunc("/api/v1/feeds/{id}", handleMethodNotAllowed)
	mux.HandleFunc("/api/v1/articles", handleMethodNotAllowed)
	mux.HandleFunc("/api/v1/articles/{id}", handleMethodNotAllowed)
	mux.HandleFunc("/api/v1/articles/{id}/read", handleMethodNotAllowed)
	mux.HandleFunc("/api/v1/articles/{id}/fulltext", handleMethodNotAllowed)
	mux.HandleFunc("/api/v1/articles/{id}/summary", handleMethodNotAllowed)
	mux.HandleFunc("/api/v1/articles/{id}/summary/stream", handleMethodNotAllowed)
	mux.HandleFunc("/api/v1/articles/{id}/tags", handleMethodNotAllowed)
	mux.HandleFunc("/api/v1/search", handleMethodNotAllowed)
	mux.HandleFunc("/api/v1/articles/{id}/qa", handleMethodNotAllowed)
	mux.HandleFunc("/api/v1/auth/status", handleMethodNotAllowed)
	mux.HandleFunc("/api/v1/auth/register/begin", handleMethodNotAllowed)
	mux.HandleFunc("/api/v1/auth/register/finish", handleMethodNotAllowed)
	mux.HandleFunc("/api/v1/auth/login/begin", handleMethodNotAllowed)
	mux.HandleFunc("/api/v1/auth/login/finish", handleMethodNotAllowed)
	mux.HandleFunc("/api/", handleAPIStub)
	return mux
}

func handleMethodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleAPIStub は未実装 API の明示的な 501。呼び出し元は moka-web の BFF のみ
// (ADR00011 — /api はエッジに公開しない。再公開時も /api prefix は strip しない契約)。
func handleAPIStub(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{
		"error": "not implemented yet",
		"path":  r.URL.Path,
	})
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// エンコード失敗はヘッダ送信後なので握る(スケルトンの範囲では map のみで失敗しない)
	_ = json.NewEncoder(w).Encode(body)
}
