package httpsrv

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/plexara/mcp-test/pkg/audit"
	"github.com/plexara/mcp-test/pkg/auth"
	"github.com/plexara/mcp-test/pkg/tools"
	"github.com/plexara/mcp-test/pkg/tools/identity"
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

// TestAdminAPI_TryitCapturesPayload verifies that a Try-It invocation
// writes an audit row whose payload carries the captured request_params
// and response_result. Without payload capture the audit drawer's
// Response tab renders "No response captured." for portal Try-It rows
// even though everything is meant to be on by default.
func TestAdminAPI_TryitCapturesPayload(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Add(identity.New(nil))

	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	for _, tk := range reg.Toolkits() {
		tk.RegisterTools(mcpServer)
	}

	mem := audit.NewMemoryLogger()
	api := NewAdminAPI(nil, mcpServer, mem, reg, nil)

	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := auth.WithIdentity(r.Context(), &auth.Identity{Subject: "alice", AuthType: "oidc"})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	mux := http.NewServeMux()
	api.Mount(mux, mw)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tryit/echo",
		strings.NewReader(`{"arguments":{"message":"hi"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("tryit status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	rows := mem.Snapshot()
	if len(rows) != 1 {
		t.Fatalf("expected 1 audit row, got %d", len(rows))
	}
	got := rows[0]
	if got.Source != "portal-tryit" {
		t.Errorf("source = %q, want portal-tryit", got.Source)
	}
	if got.Payload == nil {
		t.Fatal("audit row has no Payload — drawer Response tab will render 'No response captured.'")
	}
	if got.Payload.RequestParams["message"] != "hi" {
		t.Errorf("request_params.message = %v, want hi", got.Payload.RequestParams["message"])
	}
	if got.Payload.ResponseResult == nil {
		t.Error("response_result is nil — drawer Response tab won't render the tool's output")
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
