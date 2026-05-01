package httpsrv

import (
	"encoding/json"
	"fmt"
	"log/slog"
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
	mux.Handle("GET /api/v1/portal/audit/events/{id}", mw(http.HandlerFunc(p.auditEventDetail)))
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
//
// Important: deep-copies the APIKeys.File slice before mutating. A naive
// `c := *cfg` only copies the slice header, so mutating entries through
// the local copy would corrupt the live in-memory config that other
// callers (apikey store, auth chain) hold references to.
func sanitizedConfig(cfg *config.Config) map[string]any {
	c := *cfg
	c.Portal.CookieSecret = redactIfSet(c.Portal.CookieSecret)
	c.OIDC.ClientSecret = redactIfSet(c.OIDC.ClientSecret)
	if len(c.APIKeys.File) > 0 {
		clone := make([]config.FileAPIKey, len(c.APIKeys.File))
		copy(clone, c.APIKeys.File)
		for i := range clone {
			clone[i].Key = redactIfSet(clone[i].Key)
		}
		c.APIKeys.File = clone
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

// auditEventDetail returns a single event identified by ID, plus its
// audit_payloads sibling row (when capture is enabled and the row was
// recorded). Loggers that don't persist payloads (memory, noop) return
// the summary alone with payload omitted.
//
// Response shape mirrors the Event JSON marshaling; when payload was
// captured, it appears under the "payload" key.
func (p *PortalAPI) auditEventDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("event id required"))
		return
	}

	// The Logger interface doesn't expose a typed single-event lookup;
	// reuse Query with EventID set and Limit:1. The Postgres store
	// resolves this to a primary-key index lookup; the in-memory logger
	// scans its slice (fine for tests).
	events, err := p.audit.Query(r.Context(), audit.QueryFilter{Limit: 1, EventID: id})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if len(events) == 0 {
		writeError(w, http.StatusNotFound, fmt.Errorf("event %q not found", id))
		return
	}
	ev := events[0]

	// Payload on the in-memory event (if any) is from the original
	// write path and shouldn't be returned to the client; the truth
	// for "what's in the audit_payloads table" is what GetPayload
	// returns. Reset and ask the logger.
	ev.Payload = nil
	if pl, ok := p.audit.(audit.PayloadLogger); ok {
		payload, perr := pl.GetPayload(r.Context(), id)
		if perr != nil {
			// Soft-fail: the summary is real, only the detail is
			// unavailable. Log at WARN with the event ID so operators
			// can chase the cause without the request itself failing.
			slog.Warn("audit: payload fetch failed",
				"event_id", id,
				"err", perr,
			)
		} else {
			ev.Payload = payload
		}
	}

	writeJSON(w, http.StatusOK, ev)
}

// maxTimeseriesWindow caps the requested time-series window at 30 days
// regardless of bucket size, to bound query cost on the audit table.
const maxTimeseriesWindow = 30 * 24 * time.Hour

func (p *PortalAPI) auditTimeseries(w http.ResponseWriter, r *http.Request) {
	f := parseQueryFilter(r)
	bucket := time.Minute
	if v := r.URL.Query().Get("bucket"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			bucket = d
		}
	}
	if bucket < time.Second {
		bucket = time.Second
	}
	if !f.From.IsZero() && !f.To.IsZero() && f.To.Sub(f.From) > maxTimeseriesWindow {
		writeError(w, http.StatusBadRequest, fmt.Errorf("time window exceeds %s", maxTimeseriesWindow))
		return
	}
	pts, err := p.audit.TimeSeries(r.Context(), f.From, f.To, bucket)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
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
