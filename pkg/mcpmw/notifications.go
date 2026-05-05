package mcpmw

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/plexara/mcp-test/pkg/audit"
)

// notificationRecorder collects server-initiated notifications (progress
// updates, log messages, anything else the SDK sends with a method
// prefixed "notifications/") fired during one tool-call window.
//
// The receiving Audit middleware creates a recorder per call, stashes
// it on ctx, and reads the captured slice back after the call returns.
// The sending middleware (Notifications) reads the recorder off the
// same ctx and appends as notifications fire.
//
// Append is safe for concurrent use; tools that fan out goroutines can
// each call NotifyProgress without external synchronization.
//
// The recorder is read once via Snapshot() right after the receiving
// handler returns. Goroutines that fire notifications AFTER the handler
// returns can still Append safely (the mutex makes that race-free) but
// their entries will not be in the snapshot and are dropped silently.
// This is a deliberate trade-off: post-return notifications are an MCP
// anti-pattern, and the alternative (waiting for stragglers) would
// stall the audit pipeline behind a buggy tool.
type notificationRecorder struct {
	mu         sync.Mutex
	items      []audit.Notification
	max        int
	redactKeys []string
}

func newNotificationRecorder(max int, redactKeys []string) *notificationRecorder {
	return &notificationRecorder{max: max, redactKeys: redactKeys}
}

// Append records one notification. Drops past the cap so a tool that
// emits in a tight loop can't grow the recorder unboundedly. The
// dropped count isn't surfaced today; if it matters later the type can
// expose it.
//
// Notification params are run through audit.SanitizeParameters with the
// same redactKeys configured on the Audit middleware, so a tool that
// emits a sensitive value in a NotifyProgress message or LogMessage
// data blob does not bypass the operator's redact list.
func (r *notificationRecorder) Append(method string, params any) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.max > 0 && len(r.items) >= r.max {
		return
	}
	r.items = append(r.items, audit.Notification{
		Timestamp: time.Now().UTC(),
		Method:    method,
		Params:    audit.SanitizeParameters(paramsToMap(params), r.redactKeys),
	})
}

// Snapshot returns a copy of the recorded notifications. Caller must not
// mutate the returned slice; further Appends won't be reflected.
func (r *notificationRecorder) Snapshot() []audit.Notification {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.items) == 0 {
		return nil
	}
	out := make([]audit.Notification, len(r.items))
	copy(out, r.items)
	return out
}

// paramsToMap renders any SDK-typed Params into a generic map so the
// recorder doesn't have to know about every notification shape. The
// SDK's notification params implement json.Marshaler with the wire
// shape; round-trip through JSON.
func paramsToMap(params any) map[string]any {
	if params == nil {
		return nil
	}
	b, err := json.Marshal(params)
	if err != nil {
		return map[string]any{"_marshal_error": err.Error()}
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]any{"_unmarshal_error": err.Error(), "_raw": string(b)}
	}
	return m
}

// recorderKey is the ctx key for the per-call notification recorder.
type recorderKey struct{}

func withRecorder(ctx context.Context, r *notificationRecorder) context.Context {
	return context.WithValue(ctx, recorderKey{}, r)
}

func getRecorder(ctx context.Context) *notificationRecorder {
	r, _ := ctx.Value(recorderKey{}).(*notificationRecorder)
	return r
}

// Notifications returns a sending-side middleware that records every
// notifications/* method dispatched during the call window of a
// tool/call request. The receiving Audit middleware seeds the
// recorder onto ctx; without it (e.g. a notifications dispatch outside
// any tool call) this middleware is a no-op.
//
// Wire alongside Audit at server boot:
//
//	mcpServer.AddReceivingMiddleware(mcpmw.Audit(...))
//	mcpServer.AddSendingMiddleware(mcpmw.Notifications())
func Notifications() mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if isNotification(method) {
				if r := getRecorder(ctx); r != nil {
					r.Append(method, req.GetParams())
				}
			}
			return next(ctx, method, req)
		}
	}
}

func isNotification(method string) bool {
	return strings.HasPrefix(method, "notifications/")
}
