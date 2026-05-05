package mcpmw

import (
	"context"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNotificationRecorder_AppendAndSnapshot(t *testing.T) {
	r := newNotificationRecorder(0) // 0 = no cap
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
	r := newNotificationRecorder(2)
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
	r := newNotificationRecorder(0)
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
	r := newNotificationRecorder(0)
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
	r := newNotificationRecorder(0)
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
