package httpsrv

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// fakeFS wraps a fstest.MapFS so we can drive SPAHandler through edge cases.
func fakeFS() fs.FS {
	return fstest.MapFS{
		"index.html":     {Data: []byte(`<!doctype html><div id="root"></div>`)},
		"assets/main.js": {Data: []byte(`console.log("hi")`)},
	}
}

func TestSPA_IndexAtRoot(t *testing.T) {
	h := SPAHandler(fakeFS())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `<div id="root">`) {
		t.Errorf("body did not look like index.html: %s", w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type = %q", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
}

func TestSPA_AssetServed(t *testing.T) {
	h := SPAHandler(fakeFS())
	req := httptest.NewRequest(http.MethodGet, "/assets/main.js", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "console.log") {
		t.Errorf("asset body wrong: %s", w.Body.String())
	}
}

func TestSPA_ClientRouteFallback(t *testing.T) {
	h := SPAHandler(fakeFS())
	req := httptest.NewRequest(http.MethodGet, "/audit", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `<div id="root">`) {
		t.Errorf("client route did not fall through to index: %s", w.Body.String())
	}
}

func TestSPA_MissingAssetReturns404(t *testing.T) {
	h := SPAHandler(fakeFS())
	req := httptest.NewRequest(http.MethodGet, "/assets/missing.css", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("missing asset status = %d, want 404", w.Code)
	}
}
