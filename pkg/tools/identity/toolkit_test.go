package identity

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/plexara/mcp-test/pkg/auth"
	"github.com/plexara/mcp-test/pkg/build"
)

func TestNew_LowercasesRedactHeaders(t *testing.T) {
	tk := New([]string{"Authorization", "X-API-Key", "Cookie"})
	for _, h := range []string{"authorization", "x-api-key", "cookie"} {
		if !tk.shouldRedact(h) {
			t.Errorf("shouldRedact(%q) = false, want true", h)
		}
	}
	if tk.shouldRedact("user-agent") {
		t.Error("user-agent should not be redacted")
	}
}

func TestToolkit_Name(t *testing.T) {
	if New(nil).Name() != "identity" {
		t.Error("Name() should be identity")
	}
}

func TestRegisterTools(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: build.Version}, nil)
	New(nil).RegisterTools(srv) // smoke; should not panic
}

func TestStringError(t *testing.T) {
	var err error = stringError("boom")
	if err.Error() != "boom" {
		t.Errorf("stringError = %q", err.Error())
	}
	if !errors.Is(stringError("x"), stringError("x")) {
		// Two distinct stringErrors are NOT equal under errors.Is by default;
		// at minimum the basic Error() interface must work.
		t.Log("stringError equality not implemented; that's fine, just exercising the type")
	}
}

func TestToolkit_Tools(t *testing.T) {
	tools := New(nil).Tools()
	if len(tools) != 3 {
		t.Errorf("Tools() = %d, want 3", len(tools))
	}
	names := map[string]bool{}
	for _, m := range tools {
		names[m.Name] = true
		if m.Group != "identity" {
			t.Errorf("tool %q group = %q", m.Name, m.Group)
		}
	}
	for _, want := range []string{"whoami", "echo", "headers"} {
		if !names[want] {
			t.Errorf("missing tool: %s", want)
		}
	}
}

func TestHandleWhoami_NoIdentity(t *testing.T) {
	tk := New(nil)
	_, _, err := tk.handleWhoami(context.Background(), nil, whoamiInput{})
	if err == nil {
		t.Error("expected error when ctx has no identity")
	}
}

func TestHandleWhoami_WithIdentity(t *testing.T) {
	tk := New(nil)
	ctx := auth.WithIdentity(context.Background(), &auth.Identity{
		Subject: "alice", Email: "alice@example.com", Name: "Alice", AuthType: "oidc",
		Claims: map[string]any{"role": "admin"},
	})
	_, out, err := tk.handleWhoami(ctx, nil, whoamiInput{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Subject != "alice" || out.Email != "alice@example.com" || out.AuthType != "oidc" {
		t.Errorf("whoami output = %+v", out)
	}
}

func TestHandleEcho(t *testing.T) {
	tk := New(nil)
	in := echoIO{Message: "hi", Extras: map[string]any{"x": 1}}
	_, out, err := tk.handleEcho(context.Background(), nil, in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Message != "hi" || out.Extras["x"] != 1 {
		t.Errorf("echo round-trip lost data: %+v", out)
	}
}

func TestHandleHeaders_NoExtra(t *testing.T) {
	tk := New([]string{"authorization"})
	req := &mcp.CallToolRequest{}
	_, out, err := tk.handleHeaders(context.Background(), req, headersInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Headers) != 0 || out.Count != 0 {
		t.Errorf("expected empty output, got %+v", out)
	}
}

func TestHandleHeaders_RedactsSensitive(t *testing.T) {
	tk := New([]string{"authorization", "x-api-key"})
	req := &mcp.CallToolRequest{
		Extra: &mcp.RequestExtra{Header: http.Header{
			"User-Agent":    []string{"agent"},
			"Authorization": []string{"Bearer secret"},
			"X-Api-Key":     []string{"abc"},
		}},
	}
	_, out, err := tk.handleHeaders(context.Background(), req, headersInput{})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Headers["Authorization"]; len(got) != 1 || got[0] != "[redacted]" {
		t.Errorf("Authorization = %v", got)
	}
	if got := out.Headers["X-Api-Key"]; len(got) != 1 || got[0] != "[redacted]" {
		t.Errorf("X-Api-Key = %v", got)
	}
	if got := out.Headers["User-Agent"]; len(got) != 1 || got[0] != "agent" {
		t.Errorf("User-Agent should not be redacted; got %v", got)
	}
	if out.Count != 3 {
		t.Errorf("Count = %d", out.Count)
	}
}
