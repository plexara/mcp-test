package httpsrv

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequireCSRFHeader(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	h := requireCSRFHeader(next)

	tests := []struct {
		name       string
		method     string
		setHeader  bool
		wantStatus int
		wantCalled bool
	}{
		{"GET allowed without header", http.MethodGet, false, http.StatusOK, true},
		{"HEAD allowed without header", http.MethodHead, false, http.StatusOK, true},
		{"POST blocked without header", http.MethodPost, false, http.StatusForbidden, false},
		{"POST allowed with header", http.MethodPost, true, http.StatusOK, true},
		{"DELETE blocked without header", http.MethodDelete, false, http.StatusForbidden, false},
		{"DELETE allowed with header", http.MethodDelete, true, http.StatusOK, true},
		{"PUT blocked without header", http.MethodPut, false, http.StatusForbidden, false},
		{"PATCH blocked without header", http.MethodPatch, false, http.StatusForbidden, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called = false
			req := httptest.NewRequest(tc.method, "/anywhere", strings.NewReader(""))
			if tc.setHeader {
				req.Header.Set("X-Requested-With", "XMLHttpRequest")
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			if called != tc.wantCalled {
				t.Errorf("called = %v, want %v", called, tc.wantCalled)
			}
		})
	}
}
