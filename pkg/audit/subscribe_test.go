package audit

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func TestAsyncLogger_Subscribe_DeliversAfterSuccessfulInnerLog(t *testing.T) {
	inner := &fakeLogger{}
	a := NewAsyncLogger(inner, 16, time.Second,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer a.Close()

	ch, cancel := a.Subscribe(8)
	defer cancel()

	_ = a.Log(context.Background(), Event{ToolName: "echo"})

	select {
	case ev := <-ch:
		if ev.ToolName != "echo" {
			t.Errorf("ToolName = %q, want echo", ev.ToolName)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber did not receive event within 2s")
	}
}

func TestAsyncLogger_Subscribe_FanOutToMultiple(t *testing.T) {
	inner := &fakeLogger{}
	a := NewAsyncLogger(inner, 16, time.Second,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer a.Close()

	const n = 3
	chs := make([]<-chan Event, n)
	cancels := make([]func(), n)
	for i := 0; i < n; i++ {
		chs[i], cancels[i] = a.Subscribe(8)
	}
	defer func() {
		for _, c := range cancels {
			c()
		}
	}()

	_ = a.Log(context.Background(), Event{ToolName: "fan"})

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			select {
			case ev := <-chs[i]:
				if ev.ToolName != "fan" {
					t.Errorf("subscriber %d: ToolName = %q", i, ev.ToolName)
				}
			case <-time.After(2 * time.Second):
				t.Errorf("subscriber %d: timed out", i)
			}
		}()
	}
	wg.Wait()
}

func TestAsyncLogger_Subscribe_CancelStopsDelivery(t *testing.T) {
	inner := &fakeLogger{}
	a := NewAsyncLogger(inner, 16, time.Second,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer a.Close()

	ch, cancel := a.Subscribe(8)

	// Receive the first event.
	_ = a.Log(context.Background(), Event{ToolName: "first"})
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("first event not received")
	}

	// Cancel and verify the channel is closed.
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("channel should be closed after cancel")
		}
	case <-time.After(time.Second):
		t.Error("channel was not closed after cancel")
	}

	// Further Log calls should not panic / not deliver to the cancelled subscriber.
	_ = a.Log(context.Background(), Event{ToolName: "after-cancel"})
	time.Sleep(50 * time.Millisecond) // let the drain run
	// Cancel again is a no-op (sync.Once).
	cancel()
}

func TestAsyncLogger_Subscribe_SlowConsumerDropsEvents(t *testing.T) {
	// A buffer of 2 with 100 events posted: producer must not block,
	// subscriber sees at most 2 events before the rest are dropped.
	inner := &fakeLogger{}
	a := NewAsyncLogger(inner, 1024, time.Second,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer a.Close()

	ch, cancel := a.Subscribe(2)
	defer cancel()

	for i := 0; i < 100; i++ {
		_ = a.Log(context.Background(), Event{ToolName: "spam"})
	}
	// Give the drain a moment to flush.
	time.Sleep(100 * time.Millisecond)

	received := 0
drain:
	for {
		select {
		case <-ch:
			received++
		default:
			break drain
		}
	}
	if received < 1 || received > 2 {
		t.Errorf("received %d events, want 1 or 2 (buffer=2)", received)
	}
}

func TestAsyncLogger_Subscribe_FailedInnerLogDoesNotBroadcast(t *testing.T) {
	// Subscribers see only events that succeeded at the underlying
	// backend: a failed write must not appear on the live tail.
	inner := &fakeLogger{err: errLogFailed}
	a := NewAsyncLogger(inner, 16, time.Second,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer a.Close()

	ch, cancel := a.Subscribe(8)
	defer cancel()

	_ = a.Log(context.Background(), Event{ToolName: "should-not-appear"})

	select {
	case ev := <-ch:
		t.Errorf("subscriber received event from failed write: %+v", ev)
	case <-time.After(200 * time.Millisecond):
		// expected: no delivery
	}
}

// errLogFailed is a sentinel for the failed-inner test above. Assigned
// here to avoid needing a stdlib import in the test loop.
var errLogFailed = &mockErr{"inner log failed"}

type mockErr struct{ msg string }

func (e *mockErr) Error() string { return e.msg }
