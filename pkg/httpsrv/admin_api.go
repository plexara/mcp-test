package httpsrv

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/plexara/mcp-test/pkg/apikeys"
	"github.com/plexara/mcp-test/pkg/audit"
	"github.com/plexara/mcp-test/pkg/auth"
	"github.com/plexara/mcp-test/pkg/tools"
)

// AdminAPI bundles mutating handlers under /api/v1/admin/*.
//
// Per the project decision, any authenticated caller is admin. The middleware
// supplied to Mount is responsible for enforcing authentication.
type AdminAPI struct {
	keys       *apikeys.Store
	mcpServer  *mcp.Server
	audit      audit.Logger
	registry   *tools.Registry
	redactKeys []string
}

// NewAdminAPI returns the API. keys may be nil if api_keys.db.enabled is false
// (in which case key endpoints return 503). audit/registry/redactKeys may be
// nil; tryit then falls back to a non-logging path.
func NewAdminAPI(
	keys *apikeys.Store,
	mcpServer *mcp.Server,
	auditLog audit.Logger,
	registry *tools.Registry,
	redactKeys []string,
) *AdminAPI {
	return &AdminAPI{
		keys:       keys,
		mcpServer:  mcpServer,
		audit:      auditLog,
		registry:   registry,
		redactKeys: redactKeys,
	}
}

// Mount adds the admin endpoints behind mw. State-changing endpoints get an
// additional X-Requested-With check as CSRF protection (the SPA sets the
// header on every request; a forged <form> submission cannot).
func (a *AdminAPI) Mount(mux *http.ServeMux, mw func(http.Handler) http.Handler) {
	wrap := func(h http.Handler) http.Handler { return mw(requireCSRFHeader(h)) }
	mux.Handle("POST /api/v1/admin/keys", wrap(http.HandlerFunc(a.createKey)))
	mux.Handle("GET  /api/v1/admin/keys", mw(http.HandlerFunc(a.listKeys)))
	mux.Handle("DELETE /api/v1/admin/keys/{name}", wrap(http.HandlerFunc(a.deleteKey)))
	mux.Handle("POST /api/v1/admin/tryit/{name}", wrap(http.HandlerFunc(a.tryit)))
}

type createKeyReq struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

func (a *AdminAPI) createKey(w http.ResponseWriter, r *http.Request) {
	if a.keys == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("api_keys.db disabled"))
		return
	}
	var req createKeyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	id := auth.GetIdentity(r.Context())
	createdBy := ""
	if id != nil {
		createdBy = id.Subject
	}
	k, err := a.keys.Create(r.Context(), req.Name, req.Description, createdBy, req.ExpiresAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, k)
}

func (a *AdminAPI) listKeys(w http.ResponseWriter, r *http.Request) {
	if a.keys == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("api_keys.db disabled"))
		return
	}
	list, err := a.keys.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": list})
}

func (a *AdminAPI) deleteKey(w http.ResponseWriter, r *http.Request) {
	if a.keys == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("api_keys.db disabled"))
		return
	}
	name := r.PathValue("name")
	err := a.keys.Delete(r.Context(), name)
	if errors.Is(err, apikeys.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type tryitReq struct {
	Arguments map[string]any `json:"arguments,omitempty"`
}

// tryit invokes a registered tool through an in-process MCP client connected
// to the running server via in-memory transport. Because the MCP audit
// middleware bypasses logging for in-memory connections (it can't auth from a
// pipe), tryit writes its own audit row tagged source=portal-tryit using the
// caller's portal-authenticated identity.
func (a *AdminAPI) tryit(w http.ResponseWriter, r *http.Request) {
	if a.mcpServer == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("mcp server not available"))
		return
	}
	name := r.PathValue("name")
	var req tryitReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	// Stamp the portal-authenticated identity onto the in-memory ctx so the
	// audit middleware (which bypasses its own auth chain on the in-memory
	// pipe) and any tool that reads auth.GetIdentity see the real caller
	// instead of "anonymous." Without this, `whoami` invoked from Try-It
	// would return anonymous regardless of who's logged in to the portal.
	id := auth.GetIdentity(r.Context())
	ctx := r.Context()
	if id != nil {
		ctx = auth.WithIdentity(ctx, id)
	}

	clientT, serverT := mcp.NewInMemoryTransports()
	serverSession, err := a.mcpServer.Connect(ctx, serverT, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer func() { _ = serverSession.Close() }()

	client := mcp.NewClient(&mcp.Implementation{Name: "portal-tryit"}, nil)
	clientSession, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer func() { _ = clientSession.Close() }()

	start := time.Now()
	res, callErr := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      name,
		Arguments: req.Arguments,
	})
	elapsed := time.Since(start).Milliseconds()

	a.recordTryitAudit(r, name, req.Arguments, id, res, callErr, elapsed)

	if callErr != nil {
		writeError(w, http.StatusBadGateway, callErr)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// recordTryitAudit writes a single audit_events row tagged source=portal-tryit
// with the portal-authenticated identity, sanitized parameters, and a
// best-effort response-size measurement. Errors are swallowed (the call
// itself succeeded; a bad audit row shouldn't fail the user's request).
func (a *AdminAPI) recordTryitAudit(
	r *http.Request,
	toolName string,
	args map[string]any,
	id *auth.Identity,
	res *mcp.CallToolResult,
	callErr error,
	durMS int64,
) {
	if a.audit == nil {
		return
	}
	ev := audit.NewEvent(toolName).
		WithRequestID(uuid.NewString()).
		WithSource("portal-tryit").
		WithTransport("http").
		WithRemoteAddr(r.RemoteAddr).
		WithUserAgent(r.UserAgent()).
		WithUser(id).
		WithParameters(audit.SanitizeParameters(args, a.redactKeys))
	if a.registry != nil {
		if g, ok := a.registry.Groups()[toolName]; ok {
			ev.WithToolGroup(g)
		}
	}

	success := callErr == nil
	errMsg := ""
	if callErr != nil {
		errMsg = callErr.Error()
	}
	if res != nil && res.IsError {
		success = false
		if errMsg == "" {
			errMsg = "tool returned IsError"
		}
		ev.ErrorCategory = "tool"
	}
	ev.WithResult(success, errMsg, durMS)
	if res != nil {
		chars, blocks := measureResultBlocks(res)
		ev.WithResponseSize(chars, blocks)
	}
	_ = a.audit.Log(r.Context(), *ev)
}

func measureResultBlocks(res *mcp.CallToolResult) (int, int) {
	chars := 0
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			chars += len(tc.Text)
		}
	}
	return chars, len(res.Content)
}
