package httpsrv

import (
	"sync"
	"time"
)

// identityRateLimiter is a per-identity token bucket. Used to bound how
// often a single portal user can fire mutating endpoints (initially:
// audit replay). Implementation is intentionally simple: one bucket per
// identity key, refilled at a steady rate, capped at a burst.
//
// It's NOT distributed: multi-instance deployments need a shared
// rate-limit store. That's a known limitation; for the audit replay
// case the failure mode of a hot single user is contained per replica
// and the cost of a missed limit is one extra captured tool call,
// not data corruption.
type identityRateLimiter struct {
	burst    int           // bucket capacity
	refill   time.Duration // one token per refill duration
	mu       sync.Mutex
	buckets  map[string]*tokenBucket
	clock    func() time.Time // injectable for tests
	maxIdle  time.Duration    // GC unused buckets after this idle period
	lastSwep time.Time
}

type tokenBucket struct {
	tokens   float64
	lastSeen time.Time
}

// newIdentityRateLimiter returns a limiter with `burst` capacity and a
// new token every `refill`. clock can be nil to use time.Now.
func newIdentityRateLimiter(burst int, refill time.Duration, clock func() time.Time) *identityRateLimiter {
	if clock == nil {
		clock = time.Now
	}
	return &identityRateLimiter{
		burst:   burst,
		refill:  refill,
		buckets: make(map[string]*tokenBucket),
		clock:   clock,
		maxIdle: 10 * time.Minute,
	}
}

// Allow consumes one token for the given identity key and returns true
// if the call is permitted. False means rate-limited; the caller should
// return 429 with a Retry-After header derived from RetryAfter.
func (l *identityRateLimiter) Allow(key string) bool {
	if key == "" {
		// Fail open for unauthenticated callers: the auth middleware
		// should have rejected them; if they reached here, we'd
		// rather permit than risk a deadlock by keying on "".
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock()
	l.gcLocked(now)

	b := l.buckets[key]
	if b == nil {
		b = &tokenBucket{tokens: float64(l.burst), lastSeen: now}
		l.buckets[key] = b
	}
	// Refill: tokens accrue at 1 per refill duration since the last
	// observation, capped at burst.
	elapsed := now.Sub(b.lastSeen)
	if elapsed > 0 && l.refill > 0 {
		b.tokens += float64(elapsed) / float64(l.refill)
		if b.tokens > float64(l.burst) {
			b.tokens = float64(l.burst)
		}
	}
	b.lastSeen = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// RetryAfter returns the duration until at least one token will be
// available for the given key. Used to populate the Retry-After
// response header when Allow returned false.
func (l *identityRateLimiter) RetryAfter(key string) time.Duration {
	if key == "" {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b == nil || b.tokens >= 1 {
		return 0
	}
	missing := 1 - b.tokens
	return time.Duration(missing * float64(l.refill))
}

// gcLocked drops buckets that haven't been touched in maxIdle. Called
// at the head of each Allow so the map doesn't grow unbounded over a
// long-running process. Caller must hold l.mu.
func (l *identityRateLimiter) gcLocked(now time.Time) {
	if now.Sub(l.lastSwep) < l.maxIdle {
		return
	}
	for k, b := range l.buckets {
		if now.Sub(b.lastSeen) > l.maxIdle {
			delete(l.buckets, k)
		}
	}
	l.lastSwep = now
}
