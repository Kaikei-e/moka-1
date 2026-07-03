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
	articles ArticleLister,
	article ArticleGetter,
	fullTexts FullTextFetcher,
) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("POST /api/v1/feeds", handleRegisterFeed(feeds))
	mux.HandleFunc("GET /api/v1/feeds", handleListFeeds(feedList))
	mux.HandleFunc("GET /api/v1/articles", handleListArticles(articles))
	mux.HandleFunc("GET /api/v1/articles/{id}", handleGetArticle(article))
	mux.HandleFunc("POST /api/v1/articles/{id}/fulltext", handleFetchFullText(article, fullTexts))
	// メソッド無しパターンはメソッド不一致時の受け皿(無いと /api/ スタブが 501 で拾ってしまう)
	mux.HandleFunc("/api/v1/feeds", handleMethodNotAllowed)
	mux.HandleFunc("/api/v1/articles", handleMethodNotAllowed)
	mux.HandleFunc("/api/v1/articles/{id}", handleMethodNotAllowed)
	mux.HandleFunc("/api/v1/articles/{id}/fulltext", handleMethodNotAllowed)
	mux.HandleFunc("/api/", handleAPIStub)
	return mux
}

func handleMethodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleAPIStub は未実装 API の明示的な 501。エッジ(Plecto)は /api/* を
// strip せずここへ素通しする契約(plecto/manifest.toml)。
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
