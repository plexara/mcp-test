package audit

import (
	"context"
	"testing"
	"time"
)

func newTestEvent(tool string, ok bool, dur int64, ts time.Time, sub string) Event {
	return Event{
		Timestamp:   ts,
		ToolName:    tool,
		Success:     ok,
		DurationMS:  dur,
		UserSubject: sub,
		Transport:   "http",
		Source:      "mcp",
	}
}

func TestMemoryLogger_QueryFilters(t *testing.T) {
	m := NewMemoryLogger()
	now := time.Now().UTC().Truncate(time.Second)
	for i, ev := range []Event{
		newTestEvent("a", true, 10, now.Add(-3*time.Minute), "alice"),
		newTestEvent("b", false, 50, now.Add(-2*time.Minute), "bob"),
		newTestEvent("a", true, 20, now.Add(-1*time.Minute), "alice"),
	} {
		ev.ID = string(rune('A' + i))
		_ = m.Log(context.Background(), ev)
	}

	all, _ := m.Query(context.Background(), QueryFilter{})
	if len(all) != 3 {
		t.Fatalf("Query all = %d", len(all))
	}
	// Tool filter
	a, _ := m.Query(context.Background(), QueryFilter{ToolName: "a"})
	if len(a) != 2 {
		t.Errorf("tool=a got %d, want 2", len(a))
	}
	// User filter
	bob, _ := m.Query(context.Background(), QueryFilter{UserID: "bob"})
	if len(bob) != 1 || bob[0].UserSubject != "bob" {
		t.Errorf("user=bob: %+v", bob)
	}
	// Success filter
	yes := true
	ok, _ := m.Query(context.Background(), QueryFilter{Success: &yes})
	if len(ok) != 2 {
		t.Errorf("success=true got %d, want 2", len(ok))
	}
	// Time window
	since := now.Add(-90 * time.Second)
	recent, _ := m.Query(context.Background(), QueryFilter{From: since})
	if len(recent) != 1 {
		t.Errorf("recent got %d, want 1", len(recent))
	}
	// Limit
	lim, _ := m.Query(context.Background(), QueryFilter{Limit: 2})
	if len(lim) != 2 {
		t.Errorf("limit=2 got %d", len(lim))
	}

	// Count mirrors Query.
	n, _ := m.Count(context.Background(), QueryFilter{ToolName: "a"})
	if n != 2 {
		t.Errorf("count tool=a = %d, want 2", n)
	}
}

func TestMemoryLogger_JSONFiltersAndHasKeys(t *testing.T) {
	m := NewMemoryLogger()
	now := time.Now().UTC()
	_ = m.Log(context.Background(), Event{
		ID: "1", ToolName: "echo", Timestamp: now, Success: true,
		Payload: &Payload{
			RequestParams:  map[string]any{"message": "hello", "user": map[string]any{"id": "alice"}},
			ResponseResult: map[string]any{"isError": false},
			RequestHeaders: map[string][]string{"User-Agent": {"curl/8.0"}},
		},
	})
	_ = m.Log(context.Background(), Event{
		ID: "2", ToolName: "echo", Timestamp: now.Add(time.Second), Success: false,
		Payload: &Payload{
			RequestParams:  map[string]any{"message": "world", "user": map[string]any{"id": "bob"}},
			ResponseResult: map[string]any{"isError": true},
			ResponseError:  map[string]any{"category": "tool", "message": "boom"},
		},
	})

	cases := []struct {
		name   string
		filter QueryFilter
		ids    []string // expected event IDs (Query order: ts DESC)
	}{
		{
			"param.user.id=alice",
			QueryFilter{JSONFilters: []JSONPathFilter{{Source: "param", Path: []string{"user", "id"}, Value: "alice"}}},
			[]string{"1"},
		},
		{
			"response.isError=true (bool)",
			QueryFilter{JSONFilters: []JSONPathFilter{{Source: "response", Path: []string{"isError"}, Value: "true"}}},
			[]string{"2"},
		},
		{
			"header.User-Agent=curl/8.0",
			QueryFilter{JSONFilters: []JSONPathFilter{{Source: "header", Path: []string{"User-Agent"}, Value: "curl/8.0"}}},
			[]string{"1"},
		},
		{
			"has=response_error",
			QueryFilter{HasKeys: []string{"response_error"}},
			[]string{"2"},
		},
		{
			"AND: param.user.id=alice AND has=request_headers",
			QueryFilter{
				JSONFilters: []JSONPathFilter{{Source: "param", Path: []string{"user", "id"}, Value: "alice"}},
				HasKeys:     []string{"request_headers"},
			},
			[]string{"1"},
		},
	}
	for _, c := range cases {
		evs, err := m.Query(context.Background(), c.filter)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		var got []string
		for _, ev := range evs {
			got = append(got, ev.ID)
		}
		if !equalStringSlice(got, c.ids) {
			t.Errorf("%s: ids = %v, want %v", c.name, got, c.ids)
		}
	}
}

func TestMemoryLogger_Query_TiedTimestampsAreStable(t *testing.T) {
	// Tied timestamps must produce a deterministic Query order matching
	// the Postgres backend (ts DESC, id ASC). Without the id tiebreaker
	// the two backends could diverge under any ts collision and Stream
	// pagination consumers would see flaky results.
	m := NewMemoryLogger()
	tied := time.Now().UTC()
	for _, id := range []string{"c", "a", "b"} {
		_ = m.Log(context.Background(), Event{
			ID:        id,
			ToolName:  "echo",
			Timestamp: tied,
			Success:   true,
			Transport: "http",
			Source:    "mcp",
		})
	}
	got, _ := m.Query(context.Background(), QueryFilter{})
	if len(got) != 3 {
		t.Fatalf("len = %d", len(got))
	}
	want := []string{"a", "b", "c"}
	for i, ev := range got {
		if ev.ID != want[i] {
			t.Errorf("got[%d] = %q, want %q (full: %v)", i, ev.ID,
				want[i], []string{got[0].ID, got[1].ID, got[2].ID})
		}
	}
}

func TestMemoryLogger_Stream_IgnoresLimit(t *testing.T) {
	// Per the StreamingLogger contract, f.Limit is ignored: Stream
	// iterates the whole filtered set so callers can stop via a sentinel
	// from fn. A regression that forwards Limit to Query would silently
	// truncate exports.
	m := NewMemoryLogger()
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		_ = m.Log(context.Background(), Event{
			ID:        string(rune('a' + i)),
			Timestamp: now.Add(time.Duration(i) * time.Second),
			ToolName:  "echo",
			Success:   true,
			Transport: "http",
			Source:    "mcp",
		})
	}
	count := 0
	if err := m.Stream(context.Background(), QueryFilter{Limit: 2}, func(Event) error {
		count++
		return nil
	}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if count != 5 {
		t.Errorf("count = %d, want 5 (Limit must be ignored)", count)
	}
}

func TestMemoryLogger_Stream(t *testing.T) {
	m := NewMemoryLogger()
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		_ = m.Log(context.Background(), Event{
			ID:        string(rune('a' + i)),
			ToolName:  "echo",
			Timestamp: now.Add(time.Duration(i) * time.Second),
			Success:   true,
			Transport: "http",
			Source:    "mcp",
		})
	}
	var seen []string
	if err := m.Stream(context.Background(), QueryFilter{}, func(ev Event) error {
		seen = append(seen, ev.ID)
		return nil
	}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	// Stream returns Query order (DESC by ts), so newest first.
	if len(seen) != 5 || seen[0] != "e" || seen[4] != "a" {
		t.Errorf("seen = %v, want [e d c b a]", seen)
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestMemoryLogger_TimeSeries(t *testing.T) {
	m := NewMemoryLogger()
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	// 3 events in two distinct minute buckets
	_ = m.Log(context.Background(), newTestEvent("x", true, 10, base, ""))
	_ = m.Log(context.Background(), newTestEvent("x", false, 30, base.Add(10*time.Second), ""))
	_ = m.Log(context.Background(), newTestEvent("x", true, 20, base.Add(70*time.Second), ""))

	pts, err := m.TimeSeries(context.Background(), time.Time{}, time.Time{}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 2 {
		t.Fatalf("buckets = %d, want 2: %+v", len(pts), pts)
	}
	if pts[0].Count != 2 || pts[0].Errors != 1 {
		t.Errorf("bucket 0: count=%d errors=%d, want 2/1", pts[0].Count, pts[0].Errors)
	}
	if pts[1].Count != 1 || pts[1].Errors != 0 {
		t.Errorf("bucket 1: count=%d errors=%d, want 1/0", pts[1].Count, pts[1].Errors)
	}
	if pts[0].AvgDurMS != 20 {
		t.Errorf("bucket 0 avg = %v, want 20", pts[0].AvgDurMS)
	}

	// from/to filter
	pts, _ = m.TimeSeries(context.Background(), base.Add(time.Minute), time.Time{}, time.Minute)
	if len(pts) != 1 {
		t.Errorf("filtered bucket count = %d, want 1", len(pts))
	}

	// bucket <=0 defaults to 1m and doesn't crash
	pts, _ = m.TimeSeries(context.Background(), time.Time{}, time.Time{}, 0)
	if len(pts) == 0 {
		t.Error("default bucket produced no points")
	}
}

func TestMemoryLogger_Breakdown(t *testing.T) {
	m := NewMemoryLogger()
	now := time.Now().UTC()
	_ = m.Log(context.Background(), newTestEvent("a", true, 10, now, "alice"))
	_ = m.Log(context.Background(), newTestEvent("a", false, 10, now, "bob"))
	_ = m.Log(context.Background(), newTestEvent("b", true, 10, now, "alice"))

	cases := map[string]int{
		"tool":      2,
		"user":      2,
		"success":   2,
		"auth_type": 1, // all events have AuthType=""
		"unknown":   0, // returns nil
	}
	for dim, want := range cases {
		got, err := m.Breakdown(context.Background(), time.Time{}, time.Time{}, dim)
		if err != nil {
			t.Errorf("dim=%q: %v", dim, err)
			continue
		}
		if len(got) != want {
			t.Errorf("dim=%q: %d groups, want %d", dim, len(got), want)
		}
	}
}

func TestMemoryLogger_Stats(t *testing.T) {
	m := NewMemoryLogger()
	now := time.Now().UTC()
	for _, dur := range []int64{10, 20, 30, 40, 50} {
		_ = m.Log(context.Background(), newTestEvent("x", dur != 30, dur, now, "alice"))
	}
	_ = m.Log(context.Background(), newTestEvent("y", true, 100, now, "bob"))

	stats, err := m.Stats(context.Background(), time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 6 {
		t.Errorf("total = %d, want 6", stats.Total)
	}
	if stats.Errors != 1 {
		t.Errorf("errors = %d, want 1", stats.Errors)
	}
	if stats.UniqueSubjects != 2 {
		t.Errorf("unique_subjects = %d, want 2", stats.UniqueSubjects)
	}
	if stats.UniqueTools != 2 {
		t.Errorf("unique_tools = %d, want 2", stats.UniqueTools)
	}
	if stats.P50DurationMS == 0 || stats.P95DurationMS == 0 {
		t.Errorf("percentiles unset: %+v", stats)
	}
}

func TestMemoryLogger_Stats_Empty(t *testing.T) {
	m := NewMemoryLogger()
	stats, err := m.Stats(context.Background(), time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 0 || stats.ErrorRate != 0 {
		t.Errorf("empty stats not zero: %+v", stats)
	}
}

func TestEvent_BuilderChain(t *testing.T) {
	id := "subject:alice"
	ev := NewEvent("whoami").
		WithRequestID("req-1").
		WithSessionID("sess-1").
		WithToolGroup("identity").
		WithSource("portal-tryit").
		WithTransport("http").
		WithRemoteAddr("127.0.0.1").
		WithUserAgent("test").
		WithRequestSize(123).
		WithResponseSize(456, 3).
		WithResult(true, "", 42)
	if ev.RequestID != "req-1" || ev.SessionID != "sess-1" || ev.ToolGroup != "identity" {
		t.Errorf("builder fields wrong: %+v", ev)
	}
	if ev.Source != "portal-tryit" || ev.Transport != "http" {
		t.Errorf("source/transport: %+v", ev)
	}
	if ev.RemoteAddr != "127.0.0.1" || ev.UserAgent != "test" {
		t.Errorf("addr/ua: %+v", ev)
	}
	if ev.RequestChars != 123 || ev.ResponseChars != 456 || ev.ContentBlocks != 3 {
		t.Errorf("sizes: %+v", ev)
	}
	if ev.DurationMS != 42 || !ev.Success {
		t.Errorf("result: %+v", ev)
	}
	// WithUser nil-safe.
	ev.WithUser(nil)
	if ev.UserSubject != "" {
		t.Error("WithUser(nil) should not set anything")
	}
	_ = id
}
