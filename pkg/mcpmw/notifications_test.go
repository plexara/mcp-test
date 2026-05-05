package mcpmw

import (
	"context"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNotificationRecorder_AppendAndSnapshot(t *testing.T) {
	r := newNotificationRecorder(0, nil) // 0 = no cap
	r.Append("notifications/progress", &mcp.ProgressNotificationParams{
		Progress: 1, Total: 3, Message: "step 1/3",
	})
	r.Append("notifications/progress", &mcp.ProgressNotificationParams{
		Progress: 2, Total: 3, Message: "step 2/3",
	})

	snap := r.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(snap))
	}
	if snap[0].Method != "notifications/progress" {
		t.Errorf("Method = %q", snap[0].Method)
	}
	if snap[0].Params["message"] != "step 1/3" {
		t.Errorf("Params[message] = %v", snap[0].Params["message"])
	}
	if snap[0].Timestamp.IsZero() {
		t.Error("Timestamp should be populated")
	}
}

func TestNotificationRecorder_CapDropsExcess(t *testing.T) {
	r := newNotificationRecorder(2, nil)
	r.Append("notifications/progress", nil)
	r.Append("notifications/progress", nil)
	r.Append("notifications/progress", nil) // dropped
	r.Append("notifications/progress", nil) // dropped
	if got := len(r.Snapshot()); got != 2 {
		t.Errorf("snapshot len = %d, want 2 (cap)", got)
	}
}

func TestNotificationRecorder_NilSafe(t *testing.T) {
	var r *notificationRecorder
	r.Append("anything", nil) // must not panic
	if got := r.Snapshot(); got != nil {
		t.Errorf("nil receiver Snapshot = %v, want nil", got)
	}
}

func TestNotificationRecorder_ConcurrentAppend(t *testing.T) {
	r := newNotificationRecorder(0, nil)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Append("notifications/progress", nil)
		}()
	}
	wg.Wait()
	if got := len(r.Snapshot()); got != 100 {
		t.Errorf("after 100 concurrent appends, len = %d, want 100", got)
	}
}

func TestNotifications_NoRecorderOnCtx_NoOp(t *testing.T) {
	// Without a recorder seeded on ctx, the sending middleware just
	// passes through without panicking.
	called := false
	mw := Notifications()
	wrapped := mw(func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		called = true
		return nil, nil
	})
	_, _ = wrapped(context.Background(), "notifications/progress",
		&mcp.ServerRequest[*mcp.ProgressNotificationParams]{
			Params: &mcp.ProgressNotificationParams{Progress: 1},
		})
	if !called {
		t.Error("next should be called even without recorder")
	}
}

func TestNotifications_RecordsWhenRecorderPresent(t *testing.T) {
	r := newNotificationRecorder(0, nil)
	ctx := withRecorder(context.Background(), r)

	mw := Notifications()
	wrapped := mw(func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		return nil, nil
	})
	_, _ = wrapped(ctx, "notifications/progress",
		&mcp.ServerRequest[*mcp.ProgressNotificationParams]{
			Params: &mcp.ProgressNotificationParams{Progress: 1, Total: 5, Message: "halfway"},
		})

	snap := r.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("recorded %d, want 1", len(snap))
	}
	if snap[0].Method != "notifications/progress" {
		t.Errorf("Method = %q", snap[0].Method)
	}
}

func TestNotifications_IgnoresNonNotificationMethods(t *testing.T) {
	// The middleware sees every outbound method; only "notifications/*"
	// should land in the recorder. A "ping" or "logging/*" method
	// shouldn't.
	r := newNotificationRecorder(0, nil)
	ctx := withRecorder(context.Background(), r)

	mw := Notifications()
	wrapped := mw(func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		return nil, nil
	})
	_, _ = wrapped(ctx, "ping",
		&mcp.ServerRequest[*mcp.PingParams]{Params: &mcp.PingParams{}})

	if got := r.Snapshot(); got != nil {
		t.Errorf("non-notification method recorded: %+v", got)
	}
}

func TestIsNotification(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"notifications/progress", true},
		{"notifications/initialized", true},
		{"notifications/logging/message", true},
		{"ping", false},
		{"tools/call", false},
		{"", false},
		{"notifications", false}, // missing trailing /
	}
	for _, c := range cases {
		if got := isNotification(c.in); got != c.want {
			t.Errorf("isNotification(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParamsToMap_NilAndError(t *testing.T) {
	if got := paramsToMap(nil); got != nil {
		t.Errorf("nil params -> %v, want nil", got)
	}
	// SDK params marshal cleanly; non-marshalable inputs (e.g. a chan)
	// should hit the error path without panicking.
	if got := paramsToMap(make(chan int)); got["_marshal_error"] == nil {
		t.Errorf("expected _marshal_error key for unmarshalable input, got %+v", got)
	}
}

func TestNotificationRecorder_AppliesRedactKeys(t *testing.T) {
	// Notification params must be sanitized with the same redactKeys as
	// tool params; otherwise a tool can leak a secret via NotifyProgress
	// or LogMessage and bypass the operator's redact list.
	r := newNotificationRecorder(0, []string{"token", "password"})
	r.Append("notifications/progress", map[string]any{
		"step":     1,
		"token":    "should-be-redacted",
		"password": "also-redacted",
		"nested": map[string]any{
			"safe":     "ok",
			"apiToken": "redacted-by-substring",
		},
	})
	snap := r.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snap len = %d", len(snap))
	}
	p := snap[0].Params
	if p["token"] != "[redacted]" {
		t.Errorf("token = %v, want [redacted]", p["token"])
	}
	if p["password"] != "[redacted]" {
		t.Errorf("password = %v, want [redacted]", p["password"])
	}
	if p["step"] != float64(1) { // JSON round-trip: int becomes float64
		t.Errorf("step = %v (%T), want 1", p["step"], p["step"])
	}
	nested, _ := p["nested"].(map[string]any)
	if nested == nil {
		t.Fatalf("nested missing: %+v", p)
	}
	if nested["safe"] != "ok" {
		t.Errorf("nested.safe = %v, want ok", nested["safe"])
	}
	if nested["apiToken"] != "[redacted]" {
		t.Errorf("nested.apiToken = %v, want [redacted] (substring match)", nested["apiToken"])
	}
}
