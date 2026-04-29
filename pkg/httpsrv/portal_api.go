package httpsrv

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/plexara/mcp-test/pkg/audit"
	"github.com/plexara/mcp-test/pkg/auth"
	"github.com/plexara/mcp-test/pkg/build"
	"github.com/plexara/mcp-test/pkg/config"
	"github.com/plexara/mcp-test/pkg/tools"
)

// PortalAPI bundles the read-only handlers under /api/v1/portal/*.
type PortalAPI struct {
	cfg      *config.Config
	registry *tools.Registry
	audit    audit.Logger
}

// NewPortalAPI returns the API.
func NewPortalAPI(cfg *config.Config, registry *tools.Registry, auditLog audit.Logger) *PortalAPI {
	return &PortalAPI{cfg: cfg, registry: registry, audit: auditLog}
}

// Mount adds every endpoint behind the supplied auth middleware.
func (p *PortalAPI) Mount(mux *http.ServeMux, mw func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/portal/me", mw(http.HandlerFunc(p.me)))
	mux.Handle("GET /api/v1/portal/server", mw(http.HandlerFunc(p.server)))
	mux.Handle("GET /api/v1/portal/instructions", mw(http.HandlerFunc(p.instructions)))
	mux.Handle("GET /api/v1/portal/tools", mw(http.HandlerFunc(p.tools)))
	mux.Handle("GET /api/v1/portal/tools/{name}", mw(http.HandlerFunc(p.toolDetail)))
	mux.Handle("GET /api/v1/portal/audit/events", mw(http.HandlerFunc(p.auditEvents)))
	mux.Handle("GET /api/v1/portal/audit/timeseries", mw(http.HandlerFunc(p.auditTimeseries)))
	mux.Handle("GET /api/v1/portal/audit/breakdown", mw(http.HandlerFunc(p.auditBreakdown)))
	mux.Handle("GET /api/v1/portal/dashboard", mw(http.HandlerFunc(p.dashboard)))
	mux.Handle("GET /api/v1/portal/wellknown", mw(http.HandlerFunc(p.wellknown)))
}

// instructions returns the server-level instructions that this server hands
// to MCP clients via ServerOptions.Instructions at initialize time. Most
// clients surface that string to the LLM as system context, so showing it in
// the portal helps operators audit what their model will see.
func (p *PortalAPI) instructions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"instructions": p.cfg.Server.Instructions,
	})
}

func (p *PortalAPI) me(w http.ResponseWriter, r *http.Request) {
	id := auth.GetIdentity(r.Context())
	writeJSON(w, http.StatusOK, id)
}

// sanitizedConfig returns a config with secrets replaced by "[redacted]".
func sanitizedConfig(cfg *config.Config) map[string]any {
	c := *cfg
	c.Portal.CookieSecret = redactIfSet(c.Portal.CookieSecret)
	c.OIDC.ClientSecret = redactIfSet(c.OIDC.ClientSecret)
	for i := range c.APIKeys.File {
		c.APIKeys.File[i].Key = redactIfSet(c.APIKeys.File[i].Key)
	}
	if i := strings.LastIndex(c.Database.URL, "@"); i > 0 {
		c.Database.URL = "[redacted]" + c.Database.URL[i:]
	}
	return map[string]any{
		"version": build.Version,
		"commit":  build.Commit,
		"date":    build.Date,
		"config":  c,
	}
}

func redactIfSet(v string) string {
	if v == "" {
		return ""
	}
	return "[redacted]"
}

func (p *PortalAPI) server(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, sanitizedConfig(p.cfg))
}

func (p *PortalAPI) tools(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"tools": p.registry.All(),
	})
}

func (p *PortalAPI) toolDetail(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	for _, m := range p.registry.All() {
		if m.Name == name {
			writeJSON(w, http.StatusOK, m)
			return
		}
	}
	http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
}

func parseQueryFilter(r *http.Request) audit.QueryFilter {
	q := r.URL.Query()
	f := audit.QueryFilter{}
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.From = t
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.To = t
		}
	}
	f.ToolName = q.Get("tool")
	f.UserID = q.Get("user")
	f.SessionID = q.Get("session")
	f.Search = q.Get("q")
	if v := q.Get("success"); v == "true" {
		yes := true
		f.Success = &yes
	} else if v == "false" {
		no := false
		f.Success = &no
	}
	if v, _ := strconv.Atoi(q.Get("limit")); v > 0 {
		f.Limit = v
	}
	if v, _ := strconv.Atoi(q.Get("offset")); v > 0 {
		f.Offset = v
	}
	return f
}

func (p *PortalAPI) auditEvents(w http.ResponseWriter, r *http.Request) {
	f := parseQueryFilter(r)
	if f.Limit == 0 {
		f.Limit = 50
	}
	events, err := p.audit.Query(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	total, _ := p.audit.Count(r.Context(), f)
	writeJSON(w, http.StatusOK, map[string]any{
		"events": events,
		"total":  total,
		"limit":  f.Limit,
		"offset": f.Offset,
	})
}

func (p *PortalAPI) auditTimeseries(w http.ResponseWriter, r *http.Request) {
	f := parseQueryFilter(r)
	bucket := time.Minute
	if v := r.URL.Query().Get("bucket"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			bucket = d
		}
	}
	pts, err := p.audit.TimeSeries(r.Context(), f.From, f.To, bucket)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"points": pts, "bucket": bucket.String()})
}

func (p *PortalAPI) auditBreakdown(w http.ResponseWriter, r *http.Request) {
	f := parseQueryFilter(r)
	dim := r.URL.Query().Get("by")
	if dim == "" {
		dim = "tool"
	}
	pts, err := p.audit.Breakdown(r.Context(), f.From, f.To, dim)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"breakdown": pts, "by": dim})
}

func (p *PortalAPI) dashboard(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour)
	stats, err := p.audit.Stats(r.Context(), from, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	recent, _ := p.audit.Query(r.Context(), audit.QueryFilter{From: from, To: now, Limit: 20})
	writeJSON(w, http.StatusOK, map[string]any{
		"window_from": from,
		"window_to":   now,
		"stats":       stats,
		"recent":      recent,
	})
}

func (p *PortalAPI) wellknown(w http.ResponseWriter, _ *http.Request) {
	out := map[string]any{
		"protected_resource_url": ProtectedResourceMetadataURL(p.cfg),
		"authorization_server":   p.cfg.OIDC.Issuer,
		"oidc_enabled":           p.cfg.OIDC.Enabled,
		"audience":               p.cfg.OIDC.Audience,
		"mcp_endpoint":           strings.TrimRight(p.cfg.Server.BaseURL, "/") + "/",
	}
	writeJSON(w, http.StatusOK, out)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
}
