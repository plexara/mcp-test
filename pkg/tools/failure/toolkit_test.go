package failure

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestError_ProtocolLevel(t *testing.T) {
	tk := New()
	_, _, err := tk.handleError(context.Background(), nil, errorInput{Message: "boom"})
	if err == nil || err.Error() != "boom" {
		t.Errorf("want protocol error 'boom', got %v", err)
	}
}

func TestError_ToolLevel(t *testing.T) {
	tk := New()
	res, _, err := tk.handleError(context.Background(), nil, errorInput{Message: "soft", AsTool: true})
	if err != nil {
		t.Errorf("AsTool should not bubble protocol error, got %v", err)
	}
	if res == nil || !res.IsError {
		t.Errorf("AsTool result should have IsError=true: %+v", res)
	}
}

func TestSlow_Sleeps(t *testing.T) {
	tk := New()
	start := time.Now()
	_, out, err := tk.handleSlow(context.Background(), nil, slowInput{Milliseconds: 50})
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if elapsed < 45*time.Millisecond {
		t.Errorf("slept %v, want >= 50ms", elapsed)
	}
	if out.SleptMS < 45 {
		t.Errorf("slept_ms = %d, want >= 45", out.SleptMS)
	}
}

func TestSlow_RespectsCancellation(t *testing.T) {
	tk := New()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, _, err := tk.handleSlow(ctx, nil, slowInput{Milliseconds: 5000})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
	if time.Since(start) > 1*time.Second {
		t.Error("did not return promptly on cancel")
	}
}

func TestFlaky_Reproducible(t *testing.T) {
	tk := New()
	in := flakyInput{FailRate: 0.5, Seed: "abc", CallID: 7}
	_, a, errA := tk.handleFlaky(context.Background(), nil, in)
	_, b, errB := tk.handleFlaky(context.Background(), nil, in)
	if (errA == nil) != (errB == nil) {
		t.Errorf("reproducibility broken: errA=%v errB=%v", errA, errB)
	}
	if a.Roll != b.Roll {
		t.Errorf("rolls differ for same input: %v vs %v", a.Roll, b.Roll)
	}
}

func TestFlaky_RateBounds(t *testing.T) {
	tk := New()
	// Rate clamped to [0,1]; rate=0 should never fail.
	for i := 0; i < 100; i++ {
		_, _, err := tk.handleFlaky(context.Background(), nil, flakyInput{FailRate: 0, Seed: "s", CallID: i})
		if err != nil {
			t.Fatalf("rate=0 should never fail; failed at i=%d: %v", i, err)
		}
	}
	// rate=1 (clamped from 999) should always fail.
	for i := 0; i < 50; i++ {
		_, _, err := tk.handleFlaky(context.Background(), nil, flakyInput{FailRate: 999, Seed: "s", CallID: i})
		if err == nil {
			t.Fatalf("rate=1 should always fail; succeeded at i=%d", i)
		}
	}
}
