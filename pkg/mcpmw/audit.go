// Package mcpmw provides MCP-protocol middleware for the audit pipeline.
package mcpmw

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/plexara/mcp-test/pkg/audit"
	"github.com/plexara/mcp-test/pkg/auth"
)

// AuditOption configures the Audit middleware. Without options the
// middleware records the indexable summary only (matching pre-payload
// behavior); options opt into full request/response capture, header
// capture, and notification recording.
type AuditOption func(*auditOptions)

type auditOptions struct {
	capturePayloads  bool
	captureHeaders   bool
	maxPayloadBytes  int
	maxNotifications int
}

func defaultAuditOptions() auditOptions {
	return auditOptions{
		capturePayloads:  false,
		captureHeaders:   false,
		maxPayloadBytes:  65536,
		maxNotifications: 100,
	}
}

// WithPayloadCapture turns on the audit_payloads sibling-row capture.
// maxBytes caps each side (request, response) of the captured envelope;
// content beyond is dropped and the matching truncated flag is set on
// the payload row. Pass <=0 to use the default 65536.
func WithPayloadCapture(maxBytes int) AuditOption {
	return func(o *auditOptions) {
		o.capturePayloads = true
		if maxBytes > 0 {
			o.maxPayloadBytes = maxBytes
		}
	}
}

// WithHeaderCapture stores the redacted HTTP request headers in the
// payload row. Has no effect unless WithPayloadCapture is also set.
func WithHeaderCapture() AuditOption {
	return func(o *auditOptions) { o.captureHeaders = true }
}

// WithMaxNotifications caps the number of notifications recorded per
// call. Default 100. Has no effect unless WithPayloadCapture is set.
func WithMaxNotifications(n int) AuditOption {
	return func(o *auditOptions) {
		if n > 0 {
			o.maxNotifications = n
		}
	}
}

// Audit returns a Middleware that records every tools/call invocation.
//
// The middleware:
//  1. Pulls Authorization / X-API-Key out of the SDK's RequestExtra.Header and
//     stashes the token + headers on ctx for downstream tool handlers.
//  2. Runs the auth chain to resolve an Identity.
//  3. Times the call, records inputs/outputs, and writes an audit row.
//
// Even if authentication fails the row is written, so failed calls show up in
// the audit log alongside successful ones.
func Audit(chain *auth.Chain, logger audit.Logger, redactKeys []string, toolGroups map[string]string, opts ...AuditOption) mcp.Middleware {
	o := defaultAuditOptions()
	for _, fn := range opts {
		fn(&o)
	}

	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}

			// In-memory connections (the portal Try-It proxy) carry no HTTP
			// headers, so we can't authenticate from them here. The portal
			// handler stamps the portal-authenticated identity onto ctx
			// before calling CallTool; honor that if present, otherwise
			// fall back to anonymous so tool handlers reading the identity
			// keep working. Skip writing our own audit row in either case;
			// the portal handler writes one tagged source=portal-tryit.
			if extra := req.GetExtra(); extra == nil || extra.Header == nil {
				if existing := auth.GetIdentity(ctx); existing == nil {
					ctx = auth.WithIdentity(ctx, auth.Anonymous())
				}
				return next(ctx, method, req)
			}

			start := time.Now()
			ctx = enrichContext(ctx, req)

			id, authErr := chain.Authenticate(ctx)
			if id != nil {
				ctx = auth.WithIdentity(ctx, id)
			}

			toolName, params := extractCallParams(req)
			ev := audit.NewEvent(toolName).
				WithRequestID(auth.GetRequestID(ctx)).
				WithSessionID(sessionID(req)).
				WithUser(id).
				WithRemoteAddr(auth.GetRemoteAddr(ctx)).
				WithUserAgent(userAgent(req)).
				WithSource("mcp")

			if g, ok := toolGroups[toolName]; ok {
				ev.WithToolGroup(g)
			}

			if authErr != nil {
				ev.WithResult(false, authErr.Error(), time.Since(start).Milliseconds())
				ev.ErrorCategory = "auth"
				if o.capturePayloads {
					ev.WithPayload(buildPayload(ctx, method, params, redactKeys, nil, authErr, "auth", o, nil))
				}
				_ = logger.Log(ctx, *ev)
				return nil, authErr
			}

			ev.WithParameters(audit.SanitizeParameters(params, redactKeys))

			// Seed a notification recorder onto ctx if payload capture is
			// on. The Notifications() sending middleware reads this off
			// ctx and appends as the tool fires NotifyProgress / Log /
			// other notifications/* methods. The recorder applies the
			// same redactKeys as the rest of the audit pipeline, so a
			// tool that emits a sensitive value through NotifyProgress
			// or LogMessage cannot bypass the operator's redact list.
			var recorder *notificationRecorder
			if o.capturePayloads {
				recorder = newNotificationRecorder(o.maxNotifications, redactKeys)
				ctx = withRecorder(ctx, recorder)
			}

			res, err := next(ctx, method, req)
			ev.WithResult(err == nil, errString(err), time.Since(start).Milliseconds())
			var cr *mcp.CallToolResult
			errCategory := ""
			if r, ok := res.(*mcp.CallToolResult); ok && r != nil {
				cr = r
				chars, blocks := measureResult(cr)
				ev.WithResponseSize(chars, blocks)
				if cr.IsError && err == nil {
					ev.Success = false
					errCategory = "tool"
				}
			}
			if errCategory == "" && err != nil {
				errCategory = "handler"
			}
			ev.ErrorCategory = errCategory
			if o.capturePayloads {
				ev.WithPayload(buildPayload(ctx, method, params, redactKeys, cr, err, errCategory, o, recorder.Snapshot()))
			}
			_ = logger.Log(ctx, *ev)
			return res, err
		}
	}
}

// buildPayload assembles the full audit_payloads row for one tools/call.
// errCategory is "auth", "tool", "handler", or "" for success and is
// stored on the response_error blob so the portal can filter without
// string-matching against the message.
//
// The notifications slice is captured by the caller via a session
// recorder (see notification.go); pass nil when no recording was wired.
//
// Each side (request, response) is size-bounded; oversize JSON content
// is dropped wholesale and the matching truncated flag is set. Headers
// are reflected exactly as ctx already carries them (the audit
// middleware clones + redacts them in enrichContext via the caller's
// HeaderCapture middleware).
func buildPayload(
	ctx context.Context,
	method string,
	rawParams map[string]any,
	redactKeys []string,
	cr *mcp.CallToolResult,
	callErr error,
	errCategory string,
	opts auditOptions,
	notifications []audit.Notification,
) *audit.Payload {
	p := &audit.Payload{
		JSONRPCMethod:     method,
		RequestRemoteAddr: auth.GetRemoteAddr(ctx),
	}

	// Request: the sanitized params already live on Event.Parameters,
	// but we duplicate here so the payload row is self-contained.
	sanitized := audit.SanitizeParameters(rawParams, redactKeys)
	if size, ok := jsonSizeWithin(sanitized, opts.maxPayloadBytes); ok {
		p.RequestParams = sanitized
		p.RequestSizeBytes = size
	} else {
		p.RequestTruncated = true
		p.RequestSizeBytes = size
	}

	// Headers: only when the operator opted in. enrichContext already
	// cloned the inbound header set; HeaderCapture (HTTP layer) is
	// responsible for stripping Authorization / Cookie before they
	// reach ctx.
	if opts.captureHeaders {
		if h := auth.GetHeaders(ctx); h != nil {
			p.RequestHeaders = map[string][]string(h)
		}
	}

	// Response: serialize the CallToolResult content blocks. We model
	// the result as a {content:[...], isError:bool, structuredContent:?}
	// shape to match the SDK's wire format so the portal can render
	// each block by type.
	if cr != nil {
		result := callToolResultToMap(cr)
		if size, ok := jsonSizeWithin(result, opts.maxPayloadBytes); ok {
			p.ResponseResult = result
			p.ResponseSizeBytes = size
		} else {
			p.ResponseTruncated = true
			p.ResponseSizeBytes = size
		}
	}

	// Errors land in response_error so the portal can render them
	// distinct from a tool that returned IsError=true. category lets
	// the portal filter ("show me only auth rejects") without string-
	// matching against the message.
	if callErr != nil {
		errPayload := map[string]any{
			"message": callErr.Error(),
		}
		if errCategory != "" {
			errPayload["category"] = errCategory
		}
		p.ResponseError = errPayload
	}

	// Notifications: the recorder already capped count at Append time.
	// Apply a byte cap here too: a LogMessage carries an arbitrary
	// `data any` blob, so a small count of notifications can still
	// exceed maxPayloadBytes. Trim from the tail until the slice fits;
	// flag truncation if anything was dropped.
	if len(notifications) > 0 {
		notifications, p.NotificationsTruncated = fitNotifications(notifications, opts.maxPayloadBytes)
		p.Notifications = notifications
	}

	return p
}

// fitNotifications trims trailing entries until the slice's JSON encoding
// fits in max bytes. Returns the surviving prefix and whether anything
// was dropped. A non-positive max means "no limit". An empty input is a
// pass-through.
//
// Single linear pass: marshal each entry once, accumulate, stop the
// moment the running size would exceed max. Cheaper and simpler than
// the prior binary-search version which re-marshaled growing prefixes.
// Overhead per entry is bounded by the entry's own JSON size, which is
// already capped by the recorder's count cap and the upstream payload
// byte cap.
func fitNotifications(in []audit.Notification, max int) ([]audit.Notification, bool) {
	if len(in) == 0 || max <= 0 {
		return in, false
	}
	const brackets = 2 // "[" + "]"
	running := brackets
	for i, n := range in {
		b, err := json.Marshal(n)
		if err != nil {
			// A single bad entry shouldn't poison the whole slice; drop
			// it and any tail by reporting truncation at this index.
			return in[:i], true
		}
		if i > 0 {
			running++ // ","
		}
		running += len(b)
		if running > max {
			return in[:i], true
		}
	}
	return in, false
}

// callToolResultToMap renders a CallToolResult in the same shape the
// MCP SDK serializes to the wire, so portal consumers can iterate
// content blocks by type without reflection on Go-only types.
//
// Known content types (text/image/audio) are projected into a flat
// {type, ...} map. Anything else falls through to a JSON round-trip via
// the type's own MarshalJSON; the resulting map is preserved verbatim
// (with a defensive type tag injected if the marshal didn't carry one)
// so resource content, embedded resources, and any future content
// kinds are still inspectable in the portal.
func callToolResultToMap(cr *mcp.CallToolResult) map[string]any {
	out := map[string]any{
		"isError": cr.IsError,
	}
	blocks := make([]any, 0, len(cr.Content))
	for _, c := range cr.Content {
		switch v := c.(type) {
		case *mcp.TextContent:
			blocks = append(blocks, map[string]any{
				"type": "text",
				"text": v.Text,
			})
		case *mcp.ImageContent:
			blocks = append(blocks, map[string]any{
				"type":     "image",
				"mimeType": v.MIMEType,
				"data":     v.Data,
			})
		case *mcp.AudioContent:
			blocks = append(blocks, map[string]any{
				"type":     "audio",
				"mimeType": v.MIMEType,
				"data":     v.Data,
			})
		default:
			blocks = append(blocks, contentToGenericMap(c))
		}
	}
	out["content"] = blocks
	if cr.StructuredContent != nil {
		out["structuredContent"] = cr.StructuredContent
	}
	return out
}

// contentToGenericMap round-trips a content block through JSON. The MCP
// SDK's content types implement json.Marshaler with the wire-shape
// "type" tag, so the resulting map is the same thing a peer client
// would receive. If marshal fails (it shouldn't for SDK types), we
// surface the failure rather than dropping the block.
//
// Sentinel "type" values distinguish the three failure modes so the
// portal can render them differently and operators can grep for them:
//   - "_marshal_error": the SDK content's MarshalJSON returned an error
//   - "_unmarshal_error": marshal succeeded but the bytes weren't an object
//   - "_no_type": the SDK marshaled the content without a "type" field
func contentToGenericMap(c mcp.Content) map[string]any {
	b, err := json.Marshal(c)
	if err != nil {
		return map[string]any{
			"type":          "_marshal_error",
			"marshal_error": err.Error(),
		}
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]any{
			"type":            "_unmarshal_error",
			"unmarshal_error": err.Error(),
			"raw":             string(b),
		}
	}
	if _, hasType := m["type"]; !hasType {
		m["type"] = "_no_type"
	}
	return m
}

// jsonSizeWithin reports the JSON byte size of v and whether it falls
// within max. A non-positive max means "no limit" and always returns
// (size, true).
func jsonSizeWithin(v any, max int) (int, bool) {
	if v == nil {
		return 0, true
	}
	b, err := json.Marshal(v)
	if err != nil {
		return 0, false
	}
	size := len(b)
	if max <= 0 {
		return size, true
	}
	return size, size <= max
}

// enrichContext attaches request metadata (headers, token, request ID, remote
// addr) from the SDK's RequestExtra onto ctx so downstream code can read it
// uniformly.
func enrichContext(ctx context.Context, req mcp.Request) context.Context {
	ctx = auth.WithRequestID(ctx, uuid.NewString())
	extra := req.GetExtra()
	if extra == nil {
		return ctx
	}
	if extra.Header != nil {
		// Clone so downstream readers can't observe a future mutation by the
		// SDK or middleware that holds a different reference to the map.
		ctx = auth.WithHeaders(ctx, extra.Header.Clone())
		if tok := tokenFromHeader(extra.Header); tok != "" {
			ctx = auth.WithToken(ctx, tok)
		}
		if ra := extra.Header.Get("X-Forwarded-For"); ra != "" {
			ctx = auth.WithRemoteAddr(ctx, firstAddr(ra))
		}
	}
	return ctx
}

func tokenFromHeader(h http.Header) string {
	if k := h.Get("X-API-Key"); k != "" {
		return k
	}
	a := h.Get("Authorization")
	if a == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(a), "bearer ") {
		return strings.TrimSpace(a[len("bearer "):])
	}
	return ""
}

func firstAddr(xff string) string {
	if i := strings.Index(xff, ","); i >= 0 {
		return strings.TrimSpace(xff[:i])
	}
	return strings.TrimSpace(xff)
}

func sessionID(req mcp.Request) string {
	defer func() {
		// req.GetSession() may return a typed-nil *ServerSession (interface
		// non-nil but holding a nil pointer); calling ID() on that panics.
		// Guard with recover so a fake request used in tests can't crash the
		// audit pipeline.
		_ = recover()
	}()
	s := req.GetSession()
	if s == nil {
		return ""
	}
	return s.ID()
}

func userAgent(req mcp.Request) string {
	if extra := req.GetExtra(); extra != nil && extra.Header != nil {
		return extra.Header.Get("User-Agent")
	}
	return ""
}

func extractCallParams(req mcp.Request) (string, map[string]any) {
	switch p := req.GetParams().(type) {
	case *mcp.CallToolParams:
		args, _ := p.Arguments.(map[string]any)
		return p.Name, args
	case *mcp.CallToolParamsRaw:
		var args map[string]any
		_ = jsonUnmarshal(p.Arguments, &args)
		return p.Name, args
	}
	return "", nil
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// measureResult returns (totalCharsOfTextContent, contentBlockCount).
//
// We size by character count of TextContent only; other content types (image,
// resource) get counted in the block tally but not the char tally. Good
// enough for ranking and dashboard rendering; not authoritative for billing.
func measureResult(cr *mcp.CallToolResult) (int, int) {
	chars := 0
	for _, c := range cr.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			chars += len(tc.Text)
		}
	}
	return chars, len(cr.Content)
}

// jsonUnmarshal is a thin alias so we can swap implementations in tests.
var jsonUnmarshal = func(data []byte, v any) error {
	if len(data) == 0 {
		return errors.New("empty")
	}
	return jsonImpl(data, v)
}
