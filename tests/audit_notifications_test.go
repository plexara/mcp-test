package tests

import (
	"context"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/plexara/mcp-test/pkg/audit"
)

// TestHTTP_NotificationsCapturedInAuditPayload locks the SDK contract this
// PR depends on: that mcp.Server.AddSendingMiddleware is invoked when a
// tool calls req.Session.NotifyProgress, and that the recorder seeded by
// the receiving Audit middleware ends up populated by the time the
// audit row is written.
//
// The unit tests in pkg/mcpmw cover the middleware in isolation; this
// test boots the full HTTP stack so a future SDK refactor that decouples
// NotifyProgress from the sending pipeline (or changes the ctx flow)
// surfaces here instead of silently zeroing out audit_payloads.notifications.
func TestHTTP_NotificationsCapturedInAuditPayload(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ts, mem := httpTestApp(t)

	session := connectClient(t, ctx, ts, nil, nil)

	steps := 3
	params := &mcp.CallToolParams{
		Name:      "progress",
		Arguments: map[string]any{"steps": steps, "step_ms": 5},
	}
	params.SetProgressToken("audit-cap-1")
	res, err := session.CallTool(ctx, params)
	if err != nil {
		t.Fatalf("call progress: %v", err)
	}
	if res.IsError {
		t.Fatalf("progress returned IsError")
	}

	ev := waitForEvent(t, mem, "progress", 2*time.Second)

	if ev.Payload == nil {
		t.Fatalf("event has no payload row; capture_payloads should default on")
	}
	if len(ev.Payload.Notifications) < steps {
		t.Fatalf("notifications captured = %d, want >= %d (payload: %+v)",
			len(ev.Payload.Notifications), steps, ev.Payload.Notifications)
	}
	for i, n := range ev.Payload.Notifications {
		if n.Method != "notifications/progress" {
			t.Errorf("n[%d].Method = %q, want notifications/progress", i, n.Method)
		}
		if n.Params == nil {
			t.Errorf("n[%d].Params nil", i)
			continue
		}
		if _, ok := n.Params["progress"]; !ok {
			t.Errorf("n[%d].Params missing progress key: %+v", i, n.Params)
		}
	}
}

func waitForEvent(t *testing.T, mem *audit.MemoryLogger, toolName string, timeout time.Duration) audit.Event {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, e := range mem.Snapshot() {
			if e.ToolName == toolName {
				return e
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("audit event for tool %q never landed within %v", toolName, timeout)
	return audit.Event{}
}
