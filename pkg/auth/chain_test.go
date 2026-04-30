package auth

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// fakeOIDC always returns the configured error from ValidateBearer. The
// errFn variant lets a test produce a token-dependent error so we can
// verify the chain's redaction.
type fakeOIDC struct {
	err   error
	errFn func(token string) error
}

func (f *fakeOIDC) ValidateBearer(_ context.Context, tok string) (*Identity, error) {
	if f.errFn != nil {
		return nil, f.errFn(tok)
	}
	if f.err != nil {
		return nil, f.err
	}
	return &Identity{Subject: "alice", AuthType: "oidc"}, nil
}

// fakeAPIKeys returns the configured error from Authenticate.
type fakeAPIKeys struct {
	err   error
	errFn func(token string) error
}

func (f *fakeAPIKeys) Authenticate(_ context.Context, tok string) (*Identity, error) {
	if f.errFn != nil {
		return nil, f.errFn(tok)
	}
	if f.err != nil {
		return nil, f.err
	}
	return &Identity{Subject: "key-bob", AuthType: "apikey"}, nil
}

// captureLogger returns a logger that writes JSON lines into a buffer plus
// the buffer itself for assertions.
func captureLogger(t *testing.T) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	return slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

func TestChain_RejectsWithoutTokenWhenAnonymousOff(t *testing.T) {
	c := NewChain(false, nil, nil)
	_, err := c.Authenticate(context.Background())
	if !errors.Is(err, ErrNotAuthenticated) {
		t.Errorf("err = %v, want ErrNotAuthenticated", err)
	}
}

func TestChain_OIDCFirstHeuristic(t *testing.T) {
	oidc := &fakeOIDC{} // succeeds
	keys := &fakeAPIKeys{err: errors.New("should not be hit")}
	c := NewChain(false, keys, oidc)

	ctx := WithToken(context.Background(), "eyJhbGciOiJSUzI1NiJ9.foo.bar")
	id, err := c.Authenticate(ctx)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if id.AuthType != "oidc" {
		t.Errorf("AuthType = %q, want oidc", id.AuthType)
	}
}

func TestChain_NonJWTTriesAPIKeyFirst(t *testing.T) {
	oidc := &fakeOIDC{err: errors.New("should not be hit")}
	keys := &fakeAPIKeys{} // succeeds
	c := NewChain(false, keys, oidc)

	ctx := WithToken(context.Background(), "mcptest_plain_key")
	id, err := c.Authenticate(ctx)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if id.AuthType != "apikey" {
		t.Errorf("AuthType = %q, want apikey", id.AuthType)
	}
}

func TestChain_LogsOIDCRejectionWithMethodAndCorrelation(t *testing.T) {
	logger, buf := captureLogger(t)
	c := NewChain(false, nil, &fakeOIDC{err: errors.New("audience mismatch: want \"prod\"")}).SetLogger(logger)

	ctx := WithToken(context.Background(), "eyJ.bad.jwt")
	ctx = WithRequestID(ctx, "req-abc-123")
	ctx = WithRemoteAddr(ctx, "10.0.0.7")
	_, err := c.Authenticate(ctx)
	if !errors.Is(err, ErrNotAuthenticated) {
		t.Fatalf("err = %v, want ErrNotAuthenticated", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"level":"WARN"`) {
		t.Errorf("expected WARN (first failure), got: %s", out)
	}
	if !strings.Contains(out, `"method":"oidc"`) {
		t.Errorf("expected method=oidc, got: %s", out)
	}
	if !strings.Contains(out, `"request_id":"req-abc-123"`) {
		t.Errorf("expected request_id field, got: %s", out)
	}
	if !strings.Contains(out, `"remote_addr":"10.0.0.7"`) {
		t.Errorf("expected remote_addr field, got: %s", out)
	}
	if !strings.Contains(out, "audience mismatch") {
		t.Errorf("expected error detail, got: %s", out)
	}
}

func TestChain_RedactsJWTInValidatorError(t *testing.T) {
	// Verify the chain itself strips JWT-shape substrings even when the
	// validator unwisely embeds the rejected token in its error. This
	// tests the redaction code, not the fake's discipline.
	logger, buf := captureLogger(t)
	jwt := "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJhbGljZSJ9.deadbeefcafebabe1234567890"
	leakyOIDC := &fakeOIDC{errFn: func(token string) error {
		return errors.New("rejected token: " + token)
	}}
	c := NewChain(false, nil, leakyOIDC).SetLogger(logger)

	ctx := WithToken(context.Background(), jwt)
	_, _ = c.Authenticate(ctx)
	out := buf.String()
	if strings.Contains(out, jwt) {
		t.Errorf("JWT leaked into log despite redaction:\n%s", out)
	}
	if !strings.Contains(out, "[redacted-jwt]") {
		t.Errorf("expected [redacted-jwt] marker in log, got: %s", out)
	}
	// And the surrounding error message should still be useful.
	if !strings.Contains(out, "rejected token") {
		t.Errorf("expected error context preserved around redaction, got: %s", out)
	}
}

func TestChain_LogsAPIKeyRejection(t *testing.T) {
	logger, buf := captureLogger(t)
	c := NewChain(false, &fakeAPIKeys{err: errors.New("unknown api key")}, nil).SetLogger(logger)

	ctx := WithToken(context.Background(), "mcptest_unknown_key")
	_, _ = c.Authenticate(ctx)
	out := buf.String()
	if !strings.Contains(out, `"method":"apikey"`) {
		t.Errorf("expected method=apikey, got: %s", out)
	}
	if !strings.Contains(out, "unknown api key") {
		t.Errorf("expected error detail, got: %s", out)
	}
}

func TestChain_LogsBothPathsOnFullChainMiss(t *testing.T) {
	logger, buf := captureLogger(t)
	c := NewChain(false,
		&fakeAPIKeys{err: errors.New("no key match")},
		&fakeOIDC{err: errors.New("not a jwt")},
	).SetLogger(logger)

	ctx := WithToken(context.Background(), "plain-token")
	ctx = WithRemoteAddr(ctx, "10.0.0.8")
	_, err := c.Authenticate(ctx)
	if !errors.Is(err, ErrNotAuthenticated) {
		t.Fatalf("err = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"method":"apikey"`) {
		t.Errorf("missing apikey log: %s", out)
	}
	if !strings.Contains(out, `"method":"oidc"`) {
		t.Errorf("missing oidc log: %s", out)
	}
	// First entry per remote_addr in window is WARN; the second
	// (different method, same source within the same call) is DEBUG.
	warnCount := strings.Count(out, `"level":"WARN"`)
	debugCount := strings.Count(out, `"level":"DEBUG"`)
	if warnCount != 1 {
		t.Errorf("expected 1 WARN line (rate-limited), got %d:\n%s", warnCount, out)
	}
	if debugCount != 1 {
		t.Errorf("expected 1 DEBUG line, got %d:\n%s", debugCount, out)
	}
}

func TestChain_NoLogsOnSuccess(t *testing.T) {
	logger, buf := captureLogger(t)
	c := NewChain(false, &fakeAPIKeys{}, nil).SetLogger(logger)

	ctx := WithToken(context.Background(), "good-key")
	_, err := c.Authenticate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no log output on success, got: %s", buf.String())
	}
}

func TestChain_NoLogsOnEmptyToken(t *testing.T) {
	logger, buf := captureLogger(t)
	c := NewChain(false, &fakeAPIKeys{err: errors.New("x")}, &fakeOIDC{err: errors.New("y")}).SetLogger(logger)

	_, _ = c.Authenticate(context.Background())
	if buf.Len() != 0 {
		t.Errorf("expected no log output on empty-token path, got: %s", buf.String())
	}
}

func TestChain_NoStoresConfigured_TokenPresent(t *testing.T) {
	// Both stores nil + token present + anonymous off: nothing to try,
	// chain returns ErrNotAuthenticated and emits nothing.
	logger, buf := captureLogger(t)
	c := NewChain(false, nil, nil).SetLogger(logger)
	ctx := WithToken(context.Background(), "anything")
	_, err := c.Authenticate(ctx)
	if !errors.Is(err, ErrNotAuthenticated) {
		t.Errorf("err = %v, want ErrNotAuthenticated", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no log output when no stores configured, got: %s", buf.String())
	}
}

func TestChain_AnonymousOnEmptyTokenStillReturnsAnonymous(t *testing.T) {
	logger, buf := captureLogger(t)
	c := NewChain(true, nil, nil).SetLogger(logger)
	id, err := c.Authenticate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if id.AuthType != "anonymous" {
		t.Errorf("AuthType = %q, want anonymous", id.AuthType)
	}
	if buf.Len() != 0 {
		t.Errorf("anonymous-allowed empty-token path should not log, got: %s", buf.String())
	}
}

func TestChain_SetLogger_NilFallsBackToDefault(t *testing.T) {
	c := NewChain(false, nil, nil).SetLogger(nil)
	if c.logger == nil {
		t.Error("SetLogger(nil) should fall back to slog.Default()")
	}
}

func TestChain_DefaultLoggerSet(t *testing.T) {
	c := NewChain(false, nil, nil)
	if c.logger == nil {
		t.Error("NewChain should set logger to slog.Default()")
	}
	if c.rateLimit == nil {
		t.Error("NewChain should set rateLimit")
	}
}

func TestChain_WithLogger_DeprecatedAlias(t *testing.T) {
	// WithLogger remains as an alias for SetLogger; verify it still works.
	logger, _ := captureLogger(t)
	c := NewChain(false, nil, nil).WithLogger(logger)
	if c.logger != logger {
		t.Error("WithLogger should set the logger")
	}
}

// --- redactTokenLikes ---

func TestRedactTokenLikes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string // empty means must not appear in output
	}{
		{
			name: "classic three-segment JWT",
			in:   "rejected: eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJhbGljZSJ9.signature_bytes_here",
			want: "rejected: [redacted-jwt]",
		},
		{
			name: "no JWT means no change",
			in:   "audience mismatch: want \"prod\"",
			want: "audience mismatch: want \"prod\"",
		},
		{
			name: "short dotted identifier (not JWT-shaped) passes through",
			in:   "lookup failed for a.b.c",
			want: "lookup failed for a.b.c",
		},
		{
			name: "JWT mid-sentence",
			in:   "the token eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJhbGljZSJ9.deadbeefcafebabe was rejected",
			want: "the token [redacted-jwt] was rejected",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := redactTokenLikes(c.in)
			if got != c.want {
				t.Errorf("redactTokenLikes\n in:   %q\n got:  %q\n want: %q", c.in, got, c.want)
			}
		})
	}
}

// --- failureRateLimiter ---

func TestFailureRateLimiter_FirstWarnsThenDemotes(t *testing.T) {
	r := newFailureRateLimiter(50 * time.Millisecond)
	if !r.shouldWarn("10.0.0.1") {
		t.Error("first call should warn")
	}
	if r.shouldWarn("10.0.0.1") {
		t.Error("second call within window should not warn")
	}
	// Different key starts its own window.
	if !r.shouldWarn("10.0.0.2") {
		t.Error("different key first call should warn")
	}
	// After the window elapses, the original key warns again.
	time.Sleep(60 * time.Millisecond)
	if !r.shouldWarn("10.0.0.1") {
		t.Error("after window elapsed, key should warn again")
	}
}

func TestFailureRateLimiter_EmptyKeyAlwaysWarns(t *testing.T) {
	// No remote_addr → no key to bucket on. Every call returns true so
	// the operator still sees the failure on the first instance per
	// request (and there's only one per request).
	r := newFailureRateLimiter(time.Minute)
	if !r.shouldWarn("") {
		t.Error("empty key should warn")
	}
	if !r.shouldWarn("") {
		t.Error("empty key should always warn (no bucketing)")
	}
}

func TestFailureRateLimiter_NilSafe(t *testing.T) {
	var r *failureRateLimiter
	if !r.shouldWarn("anything") {
		t.Error("nil receiver should default to warn-allowed")
	}
}

func TestFailureRateLimiter_DefaultWindow(t *testing.T) {
	// Window <= 0 should fall back to a minute, not loop forever.
	r := newFailureRateLimiter(0)
	if r.window != time.Minute {
		t.Errorf("default window = %v, want 1m", r.window)
	}
}

func TestFailureRateLimiter_EvictsStaleEntries(t *testing.T) {
	r := newFailureRateLimiter(20 * time.Millisecond)
	for i := 0; i < 10; i++ {
		r.shouldWarn("client-" + string(rune('0'+i)))
	}
	if len(r.lastLog) != 10 {
		t.Fatalf("expected 10 entries, got %d", len(r.lastLog))
	}
	time.Sleep(30 * time.Millisecond)
	// Trigger eviction by issuing one new call.
	r.shouldWarn("trigger")
	if len(r.lastLog) > 1 {
		// Only the trigger key (or a new check) should remain post-eviction.
		t.Errorf("expected stale eviction, got %d entries", len(r.lastLog))
	}
}
