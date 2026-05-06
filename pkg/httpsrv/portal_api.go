package httpsrv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/plexara/mcp-test/pkg/audit"
	"github.com/plexara/mcp-test/pkg/auth"
	"github.com/plexara/mcp-test/pkg/build"
	"github.com/plexara/mcp-test/pkg/config"
	"github.com/plexara/mcp-test/pkg/tools"
)

// PortalAPI bundles the portal handlers under /api/v1/portal/*.
//
// Most are read-only (events, dashboard, etc.); replay and the live
// stream are mutating / long-lived. The mcpServer + redactKeys fields
// are needed by replay to invoke a tool through an in-process MCP
// client and sanitize the captured args; both are nil-safe (a nil
// mcpServer makes /replay return 503).
type PortalAPI struct {
	cfg        *config.Config
	registry   *tools.Registry
	audit      audit.Logger
	mcpServer  *mcp.Server
	redactKeys []string

	// replayLimiter rate-limits the per-identity replay calls to
	// keep a misconfigured UI or runaway script from re-firing the
	// same captured tool unboundedly. Created lazily on first use.
	replayLimiterOnce sync.Once
	replayLimiter     *identityRateLimiter
}

// NewPortalAPI returns the API. mcpServer / redactKeys are optional;
// /replay returns 503 when mcpServer is nil (test paths or audit-only
// deployments without a registered MCP server).
func NewPortalAPI(
	cfg *config.Config,
	registry *tools.Registry,
	auditLog audit.Logger,
	mcpServer *mcp.Server,
	redactKeys []string,
) *PortalAPI {
	return &PortalAPI{
		cfg:        cfg,
		registry:   registry,
		audit:      auditLog,
		mcpServer:  mcpServer,
		redactKeys: redactKeys,
	}
}

// Mount adds every endpoint behind the supplied auth middleware. The
// state-changing replay endpoint additionally requires the X-Requested-With
// header (CSRF defense; the SPA sets it on every request, a forged
// <form> POST cannot).
func (p *PortalAPI) Mount(mux *http.ServeMux, mw func(http.Handler) http.Handler) {
	wrap := func(h http.Handler) http.Handler { return mw(requireCSRFHeader(h)) }
	mux.Handle("GET /api/v1/portal/me", mw(http.HandlerFunc(p.me)))
	mux.Handle("GET /api/v1/portal/server", mw(http.HandlerFunc(p.server)))
	mux.Handle("GET /api/v1/portal/instructions", mw(http.HandlerFunc(p.instructions)))
	mux.Handle("GET /api/v1/portal/tools", mw(http.HandlerFunc(p.tools)))
	mux.Handle("GET /api/v1/portal/tools/{name}", mw(http.HandlerFunc(p.toolDetail)))
	mux.Handle("GET /api/v1/portal/audit/events", mw(http.HandlerFunc(p.auditEvents)))
	mux.Handle("GET /api/v1/portal/audit/events/{id}", mw(http.HandlerFunc(p.auditEventDetail)))
	mux.Handle("GET /api/v1/portal/audit/export", mw(http.HandlerFunc(p.auditExport)))
	mux.Handle("POST /api/v1/portal/audit/events/{id}/replay", wrap(http.HandlerFunc(p.auditReplay)))
	mux.Handle("GET /api/v1/portal/audit/stream", mw(http.HandlerFunc(p.auditStream)))
	mux.Handle("GET /api/v1/portal/audit/timeseries", mw(http.HandlerFunc(p.auditTimeseries)))
	mux.Handle("GET /api/v1/portal/audit/breakdown", mw(http.HandlerFunc(p.auditBreakdown)))
	mux.Handle("GET /api/v1/portal/dashboard", mw(http.HandlerFunc(p.dashboard)))
	mux.Handle("GET /api/v1/portal/wellknown", mw(http.HandlerFunc(p.wellknown)))
}

// replayBurst, replayRefill: 5 burst, one token every 12s == 5 per
// minute sustained per identity. Tunable later if operators ask; not
// currently config-exposed because the rate is coupled to the
// in-process MCP client cost, not user-visible behavior.
const (
	replayBurst  = 5
	replayRefill = 12 * time.Second
)

func (p *PortalAPI) limiterForReplay() *identityRateLimiter {
	p.replayLimiterOnce.Do(func() {
		p.replayLimiter = newIdentityRateLimiter(replayBurst, replayRefill, nil)
	})
	return p.replayLimiter
}

// auditReplay re-invokes a captured tool call through the in-process
// MCP client and writes a new audit row tagged source=portal-replay
// with replayed_from pointing at the original event id. The replay
// runs as the portal-authenticated identity (NOT the original
// caller's), so the new audit row reflects who fired the replay.
//
// Refused (4xx, no tool call made):
//   - {id} is not a UUID
//   - the original event is not found (404)
//   - the original event has no captured payload (replay needs the
//     captured request_params; without them we'd be replaying with
//     []any{} which would just exercise tool defaults)
//   - the original event's params contain "[redacted]" values (a
//     replay would call the tool with the literal "[redacted]" string
//     which is unlikely to match the original semantics; refuse and
//     ask the operator to re-stage manually)
//   - the named tool is no longer registered
//   - the per-identity rate limit is exhausted (429 with Retry-After)
//
// The replay does NOT skip a deliberately disabled tool group (config
// gate); the assumption is that if the operator can hit the replay
// endpoint at all, they have authority to invoke any registered tool.
func (p *PortalAPI) auditReplay(w http.ResponseWriter, r *http.Request) {
	if p.mcpServer == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("mcp server not available"))
		return
	}
	rawID := r.PathValue("id")
	parsed, err := uuid.Parse(rawID)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("event id is not a valid uuid"))
		return
	}
	eventID := parsed.String()

	// PortalAuth is required to mount this handler, so a nil or
	// empty identity here means a misconfigured route mount — fail
	// closed rather than fail-open via the rate limiter's empty-key
	// path. Also rejects a non-nil but unpopulated &Identity{} that
	// a buggy authenticator might return.
	id := auth.GetIdentity(r.Context())
	idKey := identityKey(id)
	if id == nil || idKey == "" {
		writeError(w, http.StatusUnauthorized, errors.New("authenticated identity required"))
		return
	}
	if !p.limiterForReplay().Allow(idKey) {
		retry := p.limiterForReplay().RetryAfter(idKey)
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Round(time.Second).Seconds())))
		writeError(w, http.StatusTooManyRequests,
			fmt.Errorf("replay rate limit exceeded; retry in %s", retry.Round(time.Second)))
		return
	}

	// Fetch original event + payload.
	events, err := p.audit.Query(r.Context(), audit.QueryFilter{EventID: eventID, Limit: 1})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if len(events) == 0 {
		writeError(w, http.StatusNotFound, fmt.Errorf("event not found"))
		return
	}
	original := events[0]

	pl, ok := p.audit.(audit.PayloadLogger)
	if !ok {
		writeError(w, http.StatusServiceUnavailable,
			errors.New("replay requires a payload-capable audit backend"))
		return
	}
	payload, err := pl.GetPayload(r.Context(), eventID)
	if err != nil {
		slog.Warn("audit: replay payload fetch failed", "event_id", eventID, "err", err) // #nosec G706 -- eventID is uuid.UUID.String(); cannot carry log-injection bytes.
		writeError(w, http.StatusInternalServerError, errors.New("failed to fetch original payload"))
		return
	}
	if payload == nil || payload.RequestParams == nil {
		writeError(w, http.StatusBadRequest,
			errors.New("original event has no captured request params; cannot replay"))
		return
	}
	if hasRedactedParam(payload.RequestParams) {
		writeError(w, http.StatusBadRequest,
			errors.New("original event has redacted parameter values; replay would not exercise the same call. Re-stage manually via Try-It"))
		return
	}
	if p.registry != nil {
		found := false
		for _, m := range p.registry.All() {
			if m.Name == original.ToolName {
				found = true
				break
			}
		}
		if !found {
			writeError(w, http.StatusBadRequest,
				fmt.Errorf("tool %q is no longer registered", original.ToolName))
			return
		}
	}

	// Deep-copy the captured params before passing to CallTool so
	// the SDK / tool handlers can't mutate the original-event audit
	// row's RequestParams via the shared map pointer (the
	// SanitizeParameters fast path returns the input map AS-IS when
	// redactKeys is empty).
	args := deepCopyMap(payload.RequestParams)

	// Connect an in-process client through in-memory transport, with
	// the portal identity stamped on ctx so the audit middleware
	// sees it (the in-memory transport carries no HTTP headers, so
	// the middleware would otherwise treat the call as anonymous).
	ctx := auth.WithIdentity(r.Context(), id)
	clientT, serverT := mcp.NewInMemoryTransports()
	serverSession, err := p.mcpServer.Connect(ctx, serverT, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer func() { _ = serverSession.Close() }()
	client := mcp.NewClient(&mcp.Implementation{Name: "portal-replay"}, nil)
	clientSession, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer func() { _ = clientSession.Close() }()

	start := time.Now()
	res, callErr := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      original.ToolName,
		Arguments: args,
	})
	elapsed := time.Since(start).Milliseconds()

	newID := p.recordReplayAudit(r, original, args, id, res, callErr, elapsed)

	// Body: the replay response includes both the new audit row's id
	// (so the UI can link to it) and the call result. We surface a
	// top-level success boolean so callers don't have to introspect
	// the SDK-shaped result to detect tool-side IsError. HTTP 502 on
	// transport-level callErr OR tool-side IsError, mirroring
	// /admin/tryit semantics.
	success := callErr == nil && (res == nil || !res.IsError)
	out := map[string]any{
		"replay_event_id": newID,
		"replayed_from":   eventID,
		"result":          res,
		"success":         success,
	}
	if callErr != nil {
		out["error"] = callErr.Error()
	}
	if !success {
		writeJSON(w, http.StatusBadGateway, out)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// deepCopyMap returns a structural deep copy of m. JSON-friendly
// types only (map[string]any, []any, scalars). Used by the replay
// path to prevent the in-process MCP client from mutating the audit
// row's stored RequestParams via shared pointers (the audit logger
// keeps the same map[string]any reference the caller supplied).
func deepCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = deepCopyAny(v)
	}
	return out
}

func deepCopyAny(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return deepCopyMap(t)
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = deepCopyAny(x)
		}
		return out
	default:
		return v // scalars and unknown types pass through (immutable for our purposes)
	}
}

// sseHeartbeat is sent every sseHeartbeatInterval to keep idle
// connections from being closed by intermediate proxies. SSE
// comments (lines starting with `:`) are silently skipped by the
// EventSource browser API; perfect for keepalives.
const (
	sseHeartbeatInterval = 30 * time.Second
	sseSubscriberBuffer  = 64
)

// auditStream is the SSE live-tail endpoint. Subscribes to the audit
// logger's event broadcast (via SubscribingLogger) and emits one SSE
// `audit` event per newly-written audit row, plus a keepalive
// comment every sseHeartbeatInterval.
//
// Per the StreamingLogger / SubscribingLogger split: the export
// endpoint (`/audit/export`) iterates the existing log, while this
// endpoint only sees events written AFTER the subscription opens.
// History + tail are intentionally separate APIs.
//
// Operators behind reverse proxies should ensure SSE-aware
// configuration: response buffering off (X-Accel-Buffering: no for
// nginx), HTTP/1.1 keep-alive long enough for the heartbeat, and
// proxy_read_timeout exceeding the heartbeat interval.
func (p *PortalAPI) auditStream(w http.ResponseWriter, r *http.Request) {
	sub, ok := p.audit.(audit.SubscribingLogger)
	if !ok {
		writeError(w, http.StatusServiceUnavailable,
			errors.New("live tail not supported by configured audit backend"))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError,
			errors.New("response writer does not support streaming"))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	// X-Accel-Buffering off is an nginx-specific hint that disables
	// proxy-side buffering for this response. Harmless on other
	// servers; keeps the stream moving when nginx is the front door.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	events, cancel := sub.Subscribe(sseSubscriberBuffer)
	defer cancel()

	// Initial comment so the client can confirm the connection is
	// live before the first audit event arrives. EventSource
	// dispatches `open` on the first byte, not on connection.
	if _, err := io.WriteString(w, ": connected\n\n"); err != nil {
		return
	}
	flusher.Flush()

	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev, ok := <-events:
			if !ok {
				// Subscriber channel closed: another caller forced
				// cancellation, or the logger is shutting down.
				return
			}
			// Strip the in-memory Payload pointer; live tail is
			// summary-only matching /events. Operators who need the
			// payload follow up with /events/{id}.
			ev.Payload = nil
			// Encode-then-write so a partial failure on the write
			// can't ship a half-formed SSE frame ("event: audit\n"
			// without a data line).
			var buf bytes.Buffer
			buf.WriteString("event: audit\ndata: ")
			frameEnc := json.NewEncoder(&buf)
			frameEnc.SetEscapeHTML(false)
			if err := frameEnc.Encode(&ev); err != nil {
				return
			}
			// Encode writes a trailing newline; SSE needs a blank
			// line after the data: line.
			buf.WriteByte('\n')
			if _, err := w.Write(buf.Bytes()); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// hasRedactedParam returns true when any value at any depth of the
// params tree is the literal string "[redacted]" (the sanitizer's
// substitution). Replaying with redacted values would call the tool
// with a placeholder string; the audit row would mislead about what
// happened, so refuse.
func hasRedactedParam(params map[string]any) bool {
	for _, v := range params {
		if redactedAny(v) {
			return true
		}
	}
	return false
}

func redactedAny(v any) bool {
	switch t := v.(type) {
	case string:
		return t == "[redacted]"
	case map[string]any:
		for _, sub := range t {
			if redactedAny(sub) {
				return true
			}
		}
	case []any:
		for _, sub := range t {
			if redactedAny(sub) {
				return true
			}
		}
	}
	return false
}

// identityKey returns a stable string for rate-limiting. Falls back to
// the zero value (which the limiter fails open on) when id is nil.
func identityKey(id *auth.Identity) string {
	if id == nil {
		return ""
	}
	if id.Subject != "" {
		return id.AuthType + ":" + id.Subject
	}
	return id.AuthType
}

// recordReplayAudit writes the new audit_events row tagged
// source=portal-replay with replayed_from set. Returns the new id so
// the handler can include it in the response body.
//
// The audit Log call uses a derived background context (NOT the
// request ctx) so a client disconnect at the moment we're persisting
// the replay event doesn't drop the row; the response body promised
// `replay_event_id` and that id needs to lead to a real /events row.
func (p *PortalAPI) recordReplayAudit(
	r *http.Request,
	original audit.Event,
	args map[string]any,
	id *auth.Identity,
	res *mcp.CallToolResult,
	callErr error,
	durMS int64,
) string {
	if p.audit == nil {
		return ""
	}
	// Assign the new event id locally so the handler can return it in
	// the response body. audit.Log auto-assigns when ID is unset, but
	// because the Log method takes the event by value the assignment
	// doesn't propagate back here. Setting it explicitly avoids that.
	ev := audit.NewEvent(original.ToolName)
	ev.ID = uuid.NewString()
	ev = ev.
		WithRequestID(uuid.NewString()).
		WithSource("portal-replay").
		WithTransport("http").
		WithRemoteAddr(r.RemoteAddr).
		WithUserAgent(r.UserAgent()).
		WithUser(id).
		WithToolGroup(original.ToolGroup).
		WithParameters(audit.SanitizeParameters(args, p.redactKeys))

	// errCategory mirrors pkg/mcpmw/audit.go's precedence so the
	// replay row's error_category bucket matches what a native tool
	// call would land in. The middleware logic, in plain English:
	//   - cr.IsError && err == nil  -> "tool"
	//   - err != nil                -> "handler" (overwrites tool)
	//   - both succeed              -> "" (success)
	// Mirror it exactly so an operator filtering ?error_category=tool
	// over /events sees both native-tool-errors AND replays-of-them
	// in the same bucket.
	success := callErr == nil && (res == nil || !res.IsError)
	errMsg := ""
	errCategory := ""
	if res != nil && res.IsError && callErr == nil {
		errMsg = "tool returned IsError"
		errCategory = "tool"
	}
	if callErr != nil {
		errMsg = callErr.Error()
		errCategory = "handler"
	}
	ev.ErrorCategory = errCategory
	ev.WithResult(success, errMsg, durMS)
	if res != nil {
		chars, blocks := measureResultBlocks(res)
		ev.WithResponseSize(chars, blocks)
	}

	// Build a payload row that carries the same fields a normal call
	// would (request params + sized response), plus the replay
	// linkage. Operators landing on /events/{replay_event_id} expect
	// to inspect what came back without re-reading the HTTP response.
	pl := &audit.Payload{
		JSONRPCMethod:     "tools/call",
		RequestRemoteAddr: r.RemoteAddr,
		RequestParams:     audit.SanitizeParameters(args, p.redactKeys),
		ReplayedFrom:      original.ID,
	}
	if res != nil {
		pl.ResponseResult = callToolResultToMap(res)
	}
	if callErr != nil {
		pl.ResponseError = map[string]any{
			"message":  callErr.Error(),
			"category": errCategory,
		}
	}
	ev.Payload = pl

	// Use a fresh background ctx with a generous deadline; the request
	// ctx may be cancelled by the time we get here (long replays,
	// client disconnects), and dropping the audit row would mislead
	// the caller about the replay_event_id we already returned.
	logCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.audit.Log(logCtx, *ev); err != nil {
		slog.Warn("audit: replay event log failed", "id", ev.ID, "err", err) // #nosec G706 -- ev.ID is uuid.NewString(); not user input.
	}
	return ev.ID
}

// callToolResultToMap renders the SDK CallToolResult in a JSON-friendly
// shape suitable for storage in audit_payloads.response_result. Mirrors
// the equivalent helper in pkg/mcpmw/audit.go, kept local here to
// avoid a cross-package import for two helpers.
func callToolResultToMap(cr *mcp.CallToolResult) map[string]any {
	out := map[string]any{"isError": cr.IsError}
	blocks := make([]any, 0, len(cr.Content))
	for _, c := range cr.Content {
		switch v := c.(type) {
		case *mcp.TextContent:
			blocks = append(blocks, map[string]any{"type": "text", "text": v.Text})
		case *mcp.ImageContent:
			blocks = append(blocks, map[string]any{
				"type": "image", "mimeType": v.MIMEType, "data": v.Data,
			})
		case *mcp.AudioContent:
			blocks = append(blocks, map[string]any{
				"type": "audio", "mimeType": v.MIMEType, "data": v.Data,
			})
		default:
			// Mirrors pkg/mcpmw/audit.go's contentToGenericMap: the
			// detail keys (marshal_error / unmarshal_error / raw) help
			// operators triage when a future SDK content type marshals
			// without a wire-shape "type" tag.
			b, err := json.Marshal(c)
			if err != nil {
				blocks = append(blocks, map[string]any{
					"type":          "_marshal_error",
					"marshal_error": err.Error(),
				})
				continue
			}
			var m map[string]any
			if err := json.Unmarshal(b, &m); err != nil {
				blocks = append(blocks, map[string]any{
					"type":            "_unmarshal_error",
					"unmarshal_error": err.Error(),
					"raw":             string(b),
				})
				continue
			}
			if _, ok := m["type"]; !ok {
				m["type"] = "_no_type"
			}
			blocks = append(blocks, m)
		}
	}
	out["content"] = blocks
	if cr.StructuredContent != nil {
		out["structuredContent"] = cr.StructuredContent
	}
	return out
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

// validPath reports whether path is non-empty, has no empty segments,
// and contains no ASCII control characters. Empty segments
// ("?param.a..b=v" -> ["a","","b"]) can't match any real payload, and
// control bytes in a path could land in slog lines (a malformed-filter
// path is logged via truncateForLog) and inject newlines / tabs into
// the log stream. Reject at parse time rather than depending on every
// downstream sink to sanitize.
func validPath(path []string) bool {
	if len(path) == 0 {
		return false
	}
	for _, seg := range path {
		if seg == "" {
			return false
		}
		for i := 0; i < len(seg); i++ {
			c := seg[i]
			if c < 0x20 || c == 0x7f {
				return false
			}
		}
	}
	return true
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

	// JSONB path filters and has= shortcuts. Anything malformed
	// (unknown source, empty path segment, unknown has key) is silently
	// dropped rather than failing the whole query, matching how unknown
	// plain query params are handled above.
	for k, vs := range q {
		switch {
		case strings.HasPrefix(k, "param."), strings.HasPrefix(k, "response."), strings.HasPrefix(k, "header."):
			source, rest, ok := strings.Cut(k, ".")
			if !ok || rest == "" || !audit.IsAllowedJSONSource(source) {
				continue
			}
			path := audit.SplitJSONPath(rest)
			if !validPath(path) {
				continue
			}
			// Headers are a flat map[string][]string on the wire and on
			// disk; only one path segment is meaningful (the header name).
			// Reject ?header.X-Api-Key.foo=v at parse time rather than
			// silently truncating .foo, which would mislead the operator
			// about what was actually matched.
			if source == "header" {
				if len(path) != 1 {
					continue
				}
				path[0] = http.CanonicalHeaderKey(path[0])
			}
			for _, v := range vs {
				if v == "" {
					continue
				}
				f.JSONFilters = append(f.JSONFilters, audit.JSONPathFilter{
					Source: source,
					Path:   path,
					Value:  v,
				})
			}
		case k == "has":
			for _, v := range vs {
				if audit.IsAllowedHasKey(v) {
					f.HasKeys = append(f.HasKeys, v)
				}
			}
		}
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
	rawID := r.PathValue("id")
	if rawID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("event id required"))
		return
	}
	// Audit event IDs are UUIDs minted by NewEvent. Reject anything else
	// at the boundary; we then log only the canonicalized uuid.String()
	// rather than the raw path value so gosec's taint analysis can see
	// the user-controlled string is no longer in the slog.Warn path.
	parsed, err := uuid.Parse(rawID)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("event id is not a valid uuid"))
		return
	}
	id := parsed.String()

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
			// id is a canonical uuid.UUID.String() form (validated
			// above); gosec's transitive taint propagation flags this
			// regardless, so #nosec is correct here.
			slog.Warn("audit: payload fetch failed", "event_id", id, "err", perr) // #nosec G706 -- id is a validated, canonicalized UUID; cannot carry log-injection bytes.
		} else {
			ev.Payload = payload
		}
	}

	writeJSON(w, http.StatusOK, ev)
}

// maxExportEvents caps the export endpoint at a fixed event count
// regardless of operator filters, so a misconfigured export can't
// hold the database open or run an httptest pool out of memory.
// Operators wanting more should narrow the filter or page through
// /audit/events directly.
const maxExportEvents = 100_000

// auditExport streams a filtered slice of events as newline-delimited
// JSON (jsonl). Filters mirror /audit/events; the only required
// parameter is format=jsonl (other formats reserved for future).
//
// The summary row (no payload) is what's emitted, matching what
// /events returns. Operators who need the payload should fetch
// individual events via /audit/events/{id} after filtering.
func (p *PortalAPI) auditExport(w http.ResponseWriter, r *http.Request) {
	if got := r.URL.Query().Get("format"); got != "" && got != "jsonl" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("unsupported format %q (only jsonl is supported)", got))
		return
	}
	f := parseQueryFilter(r)
	// Limit/Offset apply differently here: Limit caps the export, but
	// the underlying paginated stream uses its own page size. Map the
	// caller's Limit (if any) onto a hard ceiling, and clear Offset so
	// streams always start from the head of the matching set.
	exportCap := f.Limit
	if exportCap <= 0 || exportCap > maxExportEvents {
		exportCap = maxExportEvents
	}
	f.Limit = 0
	f.Offset = 0

	sl, ok := p.audit.(audit.StreamingLogger)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("export not supported by configured audit backend"))
		return
	}

	// All response headers are deferred until the first row encodes
	// successfully (or the no-rows-success terminal write). If Stream
	// errors before any row (DB down, planner failure on first page),
	// writeError can still send a real 5xx with the right Content-Type;
	// if we'd already sent the ndjson + attachment headers, a 500 JSON
	// body would download as "audit.jsonl" in a browser.
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false) // raw '<' in tool output, not "&lt;"
	written := 0
	headerWritten := false
	flusher, _ := w.(http.Flusher)

	// writeExportHeaders is the single place export-specific response
	// headers are committed. Called once, atomically, before the first
	// body byte. Audit data is operator-sensitive (user IDs, request
	// payloads, error messages), so no-store is mandatory; the
	// attachment hint is for browser-driven downloads.
	writeExportHeaders := func() {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Content-Disposition", `attachment; filename="audit.jsonl"`)
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		headerWritten = true
	}

	err := sl.Stream(r.Context(), f, func(ev audit.Event) error {
		// Per-row ctx check so a client disconnect doesn't waste a
		// full Postgres page (1000 rows) before the page-level ctx
		// check fires inside Stream itself.
		if err := r.Context().Err(); err != nil {
			return err
		}
		if written >= exportCap {
			// Bail by returning a sentinel; the closure can't write a
			// trailer mid-stream once headers are out, so just stop.
			return errExportCapped
		}
		// Payload is summary-only on this endpoint; clear any in-memory
		// pointer set by the underlying logger before encoding.
		ev.Payload = nil
		if !headerWritten {
			writeExportHeaders()
		}
		if err := enc.Encode(&ev); err != nil {
			return err
		}
		written++
		// Flush every 100 lines so a slow consumer sees data promptly
		// and a long-running export shows progress.
		if flusher != nil && written%100 == 0 {
			flusher.Flush()
		}
		return nil
	})
	switch {
	case err == nil, errors.Is(err, errExportCapped):
		// Success path. Filter that matched zero rows still owes the
		// client 200 OK with an empty body.
		if !headerWritten {
			writeExportHeaders()
		}
		if flusher != nil {
			flusher.Flush()
		}
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// Client disconnect or request timeout. If headers haven't
		// gone out we deliberately do NOT commit them (avoids
		// implicit 200 OK when the client gave up); if they have,
		// flush the partial response so the client sees what we got.
		if headerWritten && flusher != nil {
			flusher.Flush()
		}
	case !headerWritten:
		// Real backend error before the first row. Headers haven't
		// committed yet, so we can return a usable status. We log
		// the wrapped error at WARN and return a generic message to
		// the client to avoid leaking pgx / driver internals.
		slog.Warn("audit: export stream failed before first row",
			"err", err,
		)
		writeError(w, http.StatusInternalServerError,
			fmt.Errorf("export stream failed (see server logs)"))
	default:
		// Mid-flight failure: headers already out, can only log and
		// truncate. Client sees a partial response.
		slog.Warn("audit: export stream failed mid-flight",
			"err", err,
			"written", written,
		)
		if flusher != nil {
			flusher.Flush()
		}
	}
}

// errExportCapped is the sentinel a Stream callback returns to signal
// the export hit maxExportEvents (or the operator-supplied cap) and
// should stop without surfacing as a real error.
var errExportCapped = errors.New("export cap reached")

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
