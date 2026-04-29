package tests

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/plexara/mcp-test/internal/ui"
)

// TestSPA_EmbeddedFallback verifies that /portal/* falls back to index.html so
// react-router can take over client-side routing. Also asserts the asset path
// returns its actual file (or 404, depending on whether the SPA was built).
func TestSPA_Embedded(t *testing.T) {
	if !ui.Available() {
		t.Skip("SPA not built; run `make ui` to populate internal/ui/dist")
	}
	ts, _ := portalApp(t)

	// Index
	resp, err := http.Get(ts.URL + "/portal/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/portal/: status = %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "<div id=\"root\">") {
		t.Errorf("/portal/ did not return SPA index.html, body=%s", body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %s, want text/html", ct)
	}

	// Client route fallback
	resp, err = http.Get(ts.URL + "/portal/audit")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "<div id=\"root\">") {
		t.Errorf("/portal/audit should fall back to index.html, body=%s", body)
	}
}
