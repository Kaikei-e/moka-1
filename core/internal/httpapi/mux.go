// Package httpapi は moka-core の HTTP ハンドラを持つ。ルーティングは stdlib の
// http.ServeMux(メソッド+パスパターン)のみ(bp-go: フレームワークは入れない)。
package httpapi

import (
	"encoding/json"
	"net/http"
)

// NewMux は moka-core の全ルートを配線した mux を返す。
// M0 スケルトン: /healthz と /api/ プレースホルダのみ。
func NewMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("/api/", handleAPIStub)
	return mux
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

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// エンコード失敗はヘッダ送信後なので握る(スケルトンの範囲では map のみで失敗しない)
	_ = json.NewEncoder(w).Encode(body)
}
