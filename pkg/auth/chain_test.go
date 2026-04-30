package auth

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// fakeOIDC always returns the configured error from ValidateBearer.
type fakeOIDC struct{ err error }

func (f *fakeOIDC) ValidateBearer(_ context.Context, _ string) (*Identity, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &Identity{Subject: "alice", AuthType: "oidc"}, nil
}

// fakeAPIKeys returns the configured error from Authenticate.
type fakeAPIKeys struct{ err error }

func (f *fakeAPIKeys) Authenticate(_ context.Context, _ string) (*Identity, error) {
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

	// JWT-shaped token (starts with "ey") tries OIDC first, gets identity,
	// never falls through to API keys.
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

func TestChain_LogsOIDCRejectionWithMethodTag(t *testing.T) {
	logger, buf := captureLogger(t)
	c := NewChain(false, nil, &fakeOIDC{err: errors.New("audience mismatch: want \"prod\"")}).WithLogger(logger)

	ctx := WithToken(context.Background(), "eyJ.bad.jwt")
	_, err := c.Authenticate(ctx)
	if !errors.Is(err, ErrNotAuthenticated) {
		t.Fatalf("err = %v, want ErrNotAuthenticated", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"level":"WARN"`) {
		t.Errorf("expected WARN level, got: %s", out)
	}
	if !strings.Contains(out, `"method":"oidc"`) {
		t.Errorf("expected method=oidc tag, got: %s", out)
	}
	if !strings.Contains(out, "audience mismatch") {
		t.Errorf("expected error detail in log, got: %s", out)
	}
	// The token must NEVER be logged.
	if strings.Contains(out, "eyJ.bad.jwt") {
		t.Errorf("token leaked into log: %s", out)
	}
}

func TestChain_LogsAPIKeyRejection(t *testing.T) {
	logger, buf := captureLogger(t)
	c := NewChain(false, &fakeAPIKeys{err: errors.New("unknown api key")}, nil).WithLogger(logger)

	ctx := WithToken(context.Background(), "mcptest_unknown_key_value")
	_, _ = c.Authenticate(ctx)
	out := buf.String()
	if !strings.Contains(out, `"method":"apikey"`) {
		t.Errorf("expected method=apikey tag, got: %s", out)
	}
	if !strings.Contains(out, "unknown api key") {
		t.Errorf("expected error detail, got: %s", out)
	}
	if strings.Contains(out, "mcptest_unknown_key_value") {
		t.Errorf("token leaked into log: %s", out)
	}
}

func TestChain_LogsBothPathsOnFullChainMiss(t *testing.T) {
	// Non-JWT token: tries apikey first, falls through to oidc. Both should
	// log independently when both reject.
	logger, buf := captureLogger(t)
	c := NewChain(false,
		&fakeAPIKeys{err: errors.New("no key match")},
		&fakeOIDC{err: errors.New("not a jwt")},
	).WithLogger(logger)

	ctx := WithToken(context.Background(), "plain-token")
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
	if !strings.Contains(out, "no key match") || !strings.Contains(out, "not a jwt") {
		t.Errorf("missing per-method error detail: %s", out)
	}
}

func TestChain_NoLogsOnSuccess(t *testing.T) {
	logger, buf := captureLogger(t)
	c := NewChain(false, &fakeAPIKeys{}, nil).WithLogger(logger)

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
	// Missing-token path returns ErrNotAuthenticated directly without
	// invoking any authenticator; no log should be written.
	logger, buf := captureLogger(t)
	c := NewChain(false, &fakeAPIKeys{err: errors.New("x")}, &fakeOIDC{err: errors.New("y")}).WithLogger(logger)

	_, _ = c.Authenticate(context.Background())
	if buf.Len() != 0 {
		t.Errorf("expected no log output on empty-token path, got: %s", buf.String())
	}
}

func TestChain_WithLogger_NilFallsBackToDefault(t *testing.T) {
	c := NewChain(false, nil, nil).WithLogger(nil)
	if c.logger == nil {
		t.Error("WithLogger(nil) should fall back to slog.Default(), not leave nil")
	}
}

func TestChain_DefaultLoggerSet(t *testing.T) {
	c := NewChain(false, nil, nil)
	if c.logger == nil {
		t.Error("NewChain should set logger to slog.Default()")
	}
}
