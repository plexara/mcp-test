package httpsrv

import (
	"testing"
	"time"
)

func TestIdentityRateLimiter_BurstThenRefill(t *testing.T) {
	var now time.Time
	clock := func() time.Time { return now }
	now = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	l := newIdentityRateLimiter(3, time.Second, clock)

	// Three calls in quick succession: all allowed (burst).
	for i := 0; i < 3; i++ {
		if !l.Allow("alice") {
			t.Fatalf("call %d: should be allowed", i+1)
		}
	}
	// Fourth call: bucket empty, rate-limited.
	if l.Allow("alice") {
		t.Fatal("4th call should be rate-limited")
	}
	if r := l.RetryAfter("alice"); r <= 0 || r > time.Second {
		t.Errorf("RetryAfter = %v, want (0, 1s]", r)
	}

	// Advance 1 second: one token refilled.
	now = now.Add(time.Second)
	if !l.Allow("alice") {
		t.Error("after 1s refill, 5th call should be allowed")
	}
	if l.Allow("alice") {
		t.Error("6th call back-to-back should be rate-limited")
	}
}

func TestIdentityRateLimiter_PerKeyIndependent(t *testing.T) {
	var now time.Time
	clock := func() time.Time { return now }
	now = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	l := newIdentityRateLimiter(2, time.Second, clock)

	// alice burns her bucket.
	if !l.Allow("alice") || !l.Allow("alice") {
		t.Fatal("alice's first two calls should be allowed")
	}
	if l.Allow("alice") {
		t.Fatal("alice's third call should be blocked")
	}
	// bob is unaffected.
	if !l.Allow("bob") {
		t.Error("bob's first call should be allowed")
	}
}

func TestIdentityRateLimiter_EmptyKeyAllows(t *testing.T) {
	l := newIdentityRateLimiter(1, time.Second, nil)
	// Empty key fail-opens to avoid keying everyone-as-anonymous on
	// the same bucket (DoS vector).
	for i := 0; i < 100; i++ {
		if !l.Allow("") {
			t.Fatalf("empty key allow %d should always pass", i)
		}
	}
}

func TestIdentityRateLimiter_GCDropsIdleBuckets(t *testing.T) {
	var now time.Time
	clock := func() time.Time { return now }
	now = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	l := newIdentityRateLimiter(1, time.Second, clock)
	l.maxIdle = 5 * time.Minute

	l.Allow("alice")
	if len(l.buckets) != 1 {
		t.Fatalf("expected 1 bucket after Allow")
	}
	// Advance past maxIdle and trigger another Allow on a different
	// key; alice's bucket should be GC'd.
	now = now.Add(10 * time.Minute)
	l.Allow("bob")
	l.mu.Lock()
	_, aliceStill := l.buckets["alice"]
	l.mu.Unlock()
	if aliceStill {
		t.Error("alice's bucket should have been GC'd after 10 minutes idle")
	}
}
