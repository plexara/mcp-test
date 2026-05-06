package audit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeLogger captures Log calls and lets tests inject errors / blocks.
type fakeLogger struct {
	mu       sync.Mutex
	events   []Event
	err      error
	hangCh   chan struct{} // when non-nil, Log blocks on it
	logCount int64
}

func (f *fakeLogger) Log(ctx context.Context, ev Event) error {
	atomic.AddInt64(&f.logCount, 1)
	if f.hangCh != nil {
		select {
		case <-f.hangCh:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.events = append(f.events, ev)
	return nil
}
func (f *fakeLogger) Query(context.Context, QueryFilter) ([]Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Event, len(f.events))
	copy(out, f.events)
	return out, nil
}
func (f *fakeLogger) Count(context.Context, QueryFilter) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return int64(len(f.events)), nil
}
func (f *fakeLogger) TimeSeries(context.Context, time.Time, time.Time, time.Duration) ([]TimePoint, error) {
	return []TimePoint{{Count: 1}}, nil
}
func (f *fakeLogger) Breakdown(context.Context, time.Time, time.Time, string) ([]BreakdownPoint, error) {
	return []BreakdownPoint{{Key: "k", Count: 2}}, nil
}
func (f *fakeLogger) Stats(context.Context, time.Time, time.Time) (Stats, error) {
	return Stats{Total: 99}, nil
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestAsyncLogger_LogDrainsToInner(t *testing.T) {
	inner := &fakeLogger{}
	a := NewAsyncLogger(inner, 16, time.Second, quietLogger())
	defer a.Close()

	for i := 0; i < 5; i++ {
		_ = a.Log(context.Background(), Event{ToolName: "t"})
	}
	// Wait for the worker to drain.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&inner.logCount) == 5 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := atomic.LoadInt64(&inner.logCount); got != 5 {
		t.Errorf("inner.logCount = %d, want 5", got)
	}
	if a.Dropped() != 0 {
		t.Errorf("Dropped = %d, want 0", a.Dropped())
	}
}

func TestAsyncLogger_DropsWhenFull(t *testing.T) {
	inner := &fakeLogger{hangCh: make(chan struct{})}
	a := NewAsyncLogger(inner, 2, time.Second, quietLogger())
	t.Cleanup(func() {
		close(inner.hangCh)
		a.Close()
	})

	// Fill the buffer + force drops.
	for i := 0; i < 100; i++ {
		_ = a.Log(context.Background(), Event{})
	}
	if a.Dropped() == 0 {
		t.Error("expected drops, got 0")
	}
}

func TestAsyncLogger_StreamFallback_NonStreamingInner(t *testing.T) {
	// fakeLogger above does NOT implement StreamingLogger (no Stream
	// method). AsyncLogger.Stream must fall back to a bounded Query()
	// rather than panic or no-op. Documented as best-effort, capped
	// at MaxQueryLimit; assert events flow through and the cap holds.
	inner := &fakeLogger{}
	for i := 0; i < 5; i++ {
		inner.events = append(inner.events, Event{ToolName: "t", ID: string(rune('a' + i))})
	}
	a := NewAsyncLogger(inner, 16, time.Second, quietLogger())
	defer a.Close()

	count := 0
	if err := a.Stream(context.Background(), QueryFilter{}, func(Event) error {
		count++
		return nil
	}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if count != 5 {
		t.Errorf("count = %d, want 5", count)
	}
}

func TestAsyncLogger_StreamFallback_ClampsToMaxQueryLimit(t *testing.T) {
	// fakeLogger does NOT implement StreamingLogger AND does not honor
	// QueryFilter.Limit (Query returns all events). The Stream fallback
	// must enforce the MaxQueryLimit cap itself rather than trusting
	// the inner backend to do it. Push more than the cap; assert fn
	// is invoked exactly MaxQueryLimit times.
	inner := &fakeLogger{}
	const stored = MaxQueryLimit + 500
	for i := 0; i < stored; i++ {
		inner.events = append(inner.events, Event{ToolName: "t"})
	}
	a := NewAsyncLogger(inner, 16, time.Second, quietLogger())
	defer a.Close()

	count := 0
	if err := a.Stream(context.Background(), QueryFilter{}, func(Event) error {
		count++
		return nil
	}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if count != MaxQueryLimit {
		t.Errorf("count = %d, want MaxQueryLimit (%d): wrapper failed to enforce cap when inner ignored Limit",
			count, MaxQueryLimit)
	}
}

func TestAsyncLogger_DelegatesReadMethods(t *testing.T) {
	inner := &fakeLogger{events: []Event{{ToolName: "x"}, {ToolName: "y"}}}
	a := NewAsyncLogger(inner, 16, time.Second, quietLogger())
	defer a.Close()

	if got, _ := a.Count(context.Background(), QueryFilter{}); got != 2 {
		t.Errorf("Count = %d, want 2", got)
	}
	evs, _ := a.Query(context.Background(), QueryFilter{})
	if len(evs) != 2 {
		t.Errorf("Query len = %d, want 2", len(evs))
	}
	pts, _ := a.TimeSeries(context.Background(), time.Time{}, time.Time{}, time.Minute)
	if len(pts) != 1 {
		t.Errorf("TimeSeries len = %d", len(pts))
	}
	bd, _ := a.Breakdown(context.Background(), time.Time{}, time.Time{}, "tool")
	if len(bd) != 1 {
		t.Errorf("Breakdown len = %d", len(bd))
	}
	s, _ := a.Stats(context.Background(), time.Time{}, time.Time{})
	if s.Total != 99 {
		t.Errorf("Stats.Total = %d", s.Total)
	}
}

func TestAsyncLogger_CloseDrainsQueue(t *testing.T) {
	inner := &fakeLogger{}
	a := NewAsyncLogger(inner, 64, time.Second, quietLogger())
	for i := 0; i < 32; i++ {
		_ = a.Log(context.Background(), Event{ToolName: "t"})
	}
	a.Close()
	if got := atomic.LoadInt64(&inner.logCount); got != 32 {
		t.Errorf("after Close logCount = %d, want 32", got)
	}
	// Calling Close again is safe.
	a.Close()
}

func TestAsyncLogger_InnerErrorIsLogged(t *testing.T) {
	inner := &fakeLogger{err: errors.New("nope")}
	a := NewAsyncLogger(inner, 4, time.Second, quietLogger())
	_ = a.Log(context.Background(), Event{ToolName: "t"})
	a.Close() // drains
	if atomic.LoadInt64(&inner.logCount) != 1 {
		t.Error("inner Log was not called")
	}
}

func TestAsyncLogger_DefaultsApplied(t *testing.T) {
	// Zero buffer / timeout should fall back to defaults; nil logger
	// should fall back to slog.Default().
	a := NewAsyncLogger(&fakeLogger{}, 0, 0, nil)
	defer a.Close()
	if cap(a.ch) != 1024 {
		t.Errorf("default buffer = %d, want 1024", cap(a.ch))
	}
	if a.timeout != 5*time.Second {
		t.Errorf("default timeout = %v, want 5s", a.timeout)
	}
}

func TestNoopLogger_AllNoops(t *testing.T) {
	var n NoopLogger
	if err := n.Log(context.Background(), Event{}); err != nil {
		t.Errorf("Log err = %v", err)
	}
	if evs, _ := n.Query(context.Background(), QueryFilter{}); evs != nil {
		t.Errorf("Query = %v, want nil", evs)
	}
	if c, _ := n.Count(context.Background(), QueryFilter{}); c != 0 {
		t.Errorf("Count = %d, want 0", c)
	}
	if pts, _ := n.TimeSeries(context.Background(), time.Time{}, time.Time{}, time.Minute); pts != nil {
		t.Errorf("TimeSeries = %v, want nil", pts)
	}
	if bd, _ := n.Breakdown(context.Background(), time.Time{}, time.Time{}, "tool"); bd != nil {
		t.Errorf("Breakdown = %v, want nil", bd)
	}
	if s, _ := n.Stats(context.Background(), time.Time{}, time.Time{}); s != (Stats{}) {
		t.Errorf("Stats = %+v, want zero", s)
	}
}
