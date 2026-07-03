package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMux(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantBody   map[string]string
	}{
		{
			name:       "healthz returns ok",
			method:     http.MethodGet,
			path:       "/healthz",
			wantStatus: http.StatusOK,
			wantBody:   map[string]string{"status": "ok"},
		},
		{
			name:       "healthz rejects POST",
			method:     http.MethodPost,
			path:       "/healthz",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "api stub returns 501 for unimplemented paths",
			method:     http.MethodGet,
			path:       "/api/nope",
			wantStatus: http.StatusNotImplemented,
			wantBody:   map[string]string{"error": "not implemented yet", "path": "/api/nope"},
		},
		{
			name:       "feeds rejects DELETE",
			method:     http.MethodDelete,
			path:       "/api/v1/feeds",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "article by id rejects POST",
			method:     http.MethodPost,
			path:       "/api/v1/articles/7",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "articles rejects POST",
			method:     http.MethodPost,
			path:       "/api/v1/articles",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "fulltext rejects GET",
			method:     http.MethodGet,
			path:       "/api/v1/articles/7/fulltext",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "unknown path returns 404",
			method:     http.MethodGet,
			path:       "/nope",
			wantStatus: http.StatusNotFound,
		},
	}

	mux := NewMux(&fakeRegistrar{}, &fakeFeedLister{}, &fakeLister{}, &fakeGetter{}, &fakeFullTextFetcher{})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantBody != nil {
				var got map[string]string
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
				assert.Equal(t, tt.wantBody, got)
			}
		})
	}
}
