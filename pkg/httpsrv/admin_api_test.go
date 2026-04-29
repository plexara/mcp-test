package httpsrv

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// AdminAPI with no DB store should return 503 for key endpoints, exercising
// the "DB disabled" branches.
func TestAdminAPI_KeysWithoutDB(t *testing.T) {
	api := NewAdminAPI(nil, nil, nil, nil, nil)
	mux := http.NewServeMux()
	api.Mount(mux, func(h http.Handler) http.Handler { return h })

	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/admin/keys"},
		{http.MethodGet, "/api/v1/admin/keys"},
		{http.MethodDelete, "/api/v1/admin/keys/foo"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: status = %d, want 503", tc.method, tc.path, w.Code)
		}
	}
}

func TestAdminAPI_TryitWithoutMCPServer(t *testing.T) {
	api := NewAdminAPI(nil, nil, nil, nil, nil)
	mux := http.NewServeMux()
	api.Mount(mux, func(h http.Handler) http.Handler { return h })

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tryit/whoami", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}
