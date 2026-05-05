// Package audit defines the audit event shape and the Logger interface.
package audit

import (
	"strings"
	"time"

	"github.com/plexara/mcp-test/pkg/auth"
)

// Event captures the indexable summary of one tool call (or auth failure).
//
// Full request/response payloads live on the sibling Payload struct,
// matched 1:1 by ID. The two-table layout keeps the summary small and
// fast for time-range queries while letting operators drill into the
// full envelope on demand.
type Event struct {
	ID            string         `json:"id"`
	Timestamp     time.Time      `json:"timestamp"`
	DurationMS    int64          `json:"duration_ms"`
	RequestID     string         `json:"request_id,omitempty"`
	SessionID     string         `json:"session_id,omitempty"`
	UserSubject   string         `json:"user_subject,omitempty"`
	UserEmail     string         `json:"user_email,omitempty"`
	AuthType      string         `json:"auth_type,omitempty"`
	APIKeyName    string         `json:"api_key_name,omitempty"`
	ToolName      string         `json:"tool_name"`
	ToolGroup     string         `json:"tool_group,omitempty"`
	Parameters    map[string]any `json:"parameters,omitempty"`
	Success       bool           `json:"success"`
	ErrorMessage  string         `json:"error_message,omitempty"`
	ErrorCategory string         `json:"error_category,omitempty"`
	RequestChars  int            `json:"request_chars,omitempty"`
	ResponseChars int            `json:"response_chars,omitempty"`
	ContentBlocks int            `json:"content_blocks,omitempty"`
	Transport     string         `json:"transport"`
	Source        string         `json:"source"`
	RemoteAddr    string         `json:"remote_addr,omitempty"`
	UserAgent     string         `json:"user_agent,omitempty"`

	// Payload, when non-nil, is the full request/response envelope for
	// this event. It is written to the audit_payloads sibling table in
	// the same transaction as the summary row. Nil means "no detail
	// captured" (capture disabled, or this event predates capture).
	Payload *Payload `json:"payload,omitempty"`
}

// Payload is the full request/response envelope joined 1:1 with an Event
// by ID. Stored in the audit_payloads table. Each side carries a byte
// size and a truncation flag so operators can tell whether they're
// looking at the whole call or a capped prefix.
//
// Fields that the audit middleware can't populate today (the
// transport-layer JSON-RPC ID, the inbound HTTP method and path) are
// intentionally absent from this struct. They will land alongside a
// transport-layer capture hook when replay-via-HTTP needs them.
type Payload struct {
	// JSON-RPC method as the receiving middleware saw it (typically
	// "tools/call"). Captured from the dispatch metadata, not the wire.
	JSONRPCMethod    string         `json:"jsonrpc_method,omitempty"`
	RequestParams    map[string]any `json:"request_params,omitempty"`
	RequestSizeBytes int            `json:"request_size_bytes,omitempty"`
	RequestTruncated bool           `json:"request_truncated,omitempty"`

	// HTTP layer (best-effort; only what the audit middleware can
	// observe through ctx).
	RequestHeaders    map[string][]string `json:"request_headers,omitempty"`
	RequestRemoteAddr string              `json:"request_remote_addr,omitempty"`

	// JSON-RPC response.
	ResponseResult    map[string]any `json:"response_result,omitempty"`
	ResponseError     map[string]any `json:"response_error,omitempty"`
	ResponseSizeBytes int            `json:"response_size_bytes,omitempty"`
	ResponseTruncated bool           `json:"response_truncated,omitempty"`

	// Notifications fired during the call window. NotificationsTruncated
	// is set when the captured slice exceeded MaxPayloadBytes and the
	// tail was dropped to fit; the surviving prefix is what's stored.
	Notifications          []Notification `json:"notifications,omitempty"`
	NotificationsTruncated bool           `json:"notifications_truncated,omitempty"`

	// Replay linkage; if this event was a /replay of another, this
	// points at the original event's ID.
	ReplayedFrom string `json:"replayed_from,omitempty"`
}

// Notification is one server-initiated notification recorded during a
// tool call (typically a progress notification, but the shape is open).
type Notification struct {
	Timestamp time.Time      `json:"ts"`
	Method    string         `json:"method"`
	Params    map[string]any `json:"params,omitempty"`
}

// NewEvent constructs an Event with sensible defaults filled in.
func NewEvent(toolName string) *Event {
	return &Event{
		Timestamp: time.Now().UTC(),
		ToolName:  toolName,
		Transport: "http",
		Source:    "mcp",
	}
}

// WithUser fills user-related fields from the resolved Identity.
func (e *Event) WithUser(id *auth.Identity) *Event {
	if id == nil {
		return e
	}
	e.UserSubject = id.Subject
	e.UserEmail = id.Email
	e.AuthType = id.AuthType
	if id.AuthType == "apikey" {
		e.APIKeyName = id.APIKeyID
	}
	return e
}

// WithRequestID sets the request ID and returns the event for chaining.
func (e *Event) WithRequestID(id string) *Event { e.RequestID = id; return e }

// WithSessionID sets the MCP session ID and returns the event for chaining.
func (e *Event) WithSessionID(id string) *Event { e.SessionID = id; return e }

// WithToolGroup sets the tool's group label (e.g. "identity") for filtering.
func (e *Event) WithToolGroup(g string) *Event { e.ToolGroup = g; return e }

// WithSource sets the source label (e.g. "mcp", "portal-tryit").
func (e *Event) WithSource(s string) *Event { e.Source = s; return e }

// WithTransport sets the transport label (currently always "http").
func (e *Event) WithTransport(t string) *Event { e.Transport = t; return e }

// WithRemoteAddr records the caller's network address.
func (e *Event) WithRemoteAddr(s string) *Event { e.RemoteAddr = s; return e }

// WithUserAgent records the caller's User-Agent header.
func (e *Event) WithUserAgent(s string) *Event { e.UserAgent = s; return e }

// WithParameters sets the (already sanitized) parameters map.
func (e *Event) WithParameters(p map[string]any) *Event { e.Parameters = p; return e }

// WithRequestSize records the byte size of the inbound parameters.
func (e *Event) WithRequestSize(n int) *Event { e.RequestChars = n; return e }

// WithResponseSize records the response payload size and content-block count.
func (e *Event) WithResponseSize(chars, blocks int) *Event {
	e.ResponseChars = chars
	e.ContentBlocks = blocks
	return e
}

// WithResult finalizes the success / error / duration fields.
func (e *Event) WithResult(success bool, errMsg string, durMS int64) *Event {
	e.Success = success
	e.ErrorMessage = errMsg
	e.DurationMS = durMS
	return e
}

// WithPayload attaches a full request/response payload to the event.
// A non-nil pointer is persisted as a row in audit_payloads; pass nil
// to clear (or to leave the event summary-only). The persistence layer
// does not look at the Payload's field contents to decide whether to
// write a row; if you don't want a payload row, pass nil.
func (e *Event) WithPayload(p *Payload) *Event {
	e.Payload = p
	return e
}

// SanitizeParameters walks v and returns a deep copy with any value whose key
// (case-insensitive substring match) appears in redactKeys replaced by
// "[redacted]". Values inside arrays are recursed but array elements are not
// keyed by name, so they are kept as-is.
func SanitizeParameters(v any, redactKeys []string) map[string]any {
	if v == nil {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return map[string]any{"_value": v}
	}
	out := make(map[string]any, len(m))
	for k, val := range m {
		if matchesRedactKey(k, redactKeys) {
			out[k] = "[redacted]"
			continue
		}
		switch sub := val.(type) {
		case map[string]any:
			out[k] = SanitizeParameters(sub, redactKeys)
		case []any:
			out[k] = sanitizeSlice(sub, redactKeys)
		default:
			out[k] = val
		}
	}
	return out
}

func sanitizeSlice(s []any, redactKeys []string) []any {
	out := make([]any, len(s))
	for i, e := range s {
		switch sub := e.(type) {
		case map[string]any:
			out[i] = SanitizeParameters(sub, redactKeys)
		case []any:
			out[i] = sanitizeSlice(sub, redactKeys)
		default:
			out[i] = e
		}
	}
	return out
}

func matchesRedactKey(key string, redactKeys []string) bool {
	lk := strings.ToLower(key)
	for _, rk := range redactKeys {
		if rk == "" {
			continue
		}
		if strings.Contains(lk, strings.ToLower(rk)) {
			return true
		}
	}
	return false
}
