package httpsrv

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReadiness_DrainsTo503(t *testing.T) {
	r := NewReadiness()
	w := httptest.NewRecorder()
	r.ReadyzHandler()(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusOK {
		t.Errorf("ready status = %d, want 200", w.Code)
	}

	r.SetReady(false)
	w = httptest.NewRecorder()
	r.ReadyzHandler()(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("draining status = %d, want 503", w.Code)
	}
}

func TestHealthzAlwaysOK(t *testing.T) {
	w := httptest.NewRecorder()
	HealthzHandler()(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d", w.Code)
	}
}
