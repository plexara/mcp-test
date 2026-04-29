package tests

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/plexara/mcp-test/pkg/audit"
	"github.com/plexara/mcp-test/pkg/auth"
	"github.com/plexara/mcp-test/pkg/mcpmw"
	"github.com/plexara/mcp-test/pkg/tools"
	"github.com/plexara/mcp-test/pkg/tools/identity"
)

// TestIdentityToolkit_InMemory wires the identity toolkit with the audit
// middleware and exercises whoami/echo over the SDK's in-memory transport.
//
// This intentionally avoids HTTP / Postgres so it runs anywhere; the
// integration_test.go file (build tag "integration") covers the full stack
// end-to-end via testcontainers.
func TestIdentityToolkit_InMemory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mem := audit.NewMemoryLogger()
	chain := auth.NewChain(true /* allowAnonymous */, nil, nil)

	srv := mcp.NewServer(&mcp.Implementation{Name: "mcp-test", Version: "test"}, nil)

	registry := tools.NewRegistry()
	registry.Add(identity.New([]string{"authorization", "cookie"}))
	for _, tk := range registry.Toolkits() {
		tk.RegisterTools(srv)
	}
	srv.AddReceivingMiddleware(mcpmw.Audit(chain, mem, []string{"password"}, registry.Groups()))

	clientT, serverT := mcp.NewInMemoryTransports()

	serverSession, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer clientSession.Close()

	// --- whoami ---
	whoamiRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "whoami",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("whoami: %v", err)
	}
	if whoamiRes.IsError {
		t.Fatalf("whoami returned IsError: %+v", whoamiRes)
	}
	whoamiSC, ok := whoamiRes.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("whoami: structured content not a map: %T", whoamiRes.StructuredContent)
	}
	if whoamiSC["auth_type"] != "anonymous" {
		t.Errorf("whoami auth_type = %v, want anonymous", whoamiSC["auth_type"])
	}
	if whoamiSC["subject"] != "anonymous" {
		t.Errorf("whoami subject = %v, want anonymous", whoamiSC["subject"])
	}

	// --- echo ---
	echoRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "echo",
		Arguments: map[string]any{
			"message": "hello",
			"extras":  map[string]any{"k": "v"},
		},
	})
	if err != nil {
		t.Fatalf("echo: %v", err)
	}
	echoSC, ok := echoRes.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("echo: structured content not a map: %T", echoRes.StructuredContent)
	}
	if echoSC["message"] != "hello" {
		t.Errorf("echo message = %v, want hello", echoSC["message"])
	}

	// In-memory connections deliberately bypass middleware-side audit logging:
	// the SDK transport carries no HTTP headers, so the audit middleware
	// stamps an Anonymous identity for the call but skips the row. Callers
	// running tools over an in-process pipe (the portal Try-It proxy) write
	// their own audit row from the HTTP handler with the verified identity.
	if got := mem.Snapshot(); len(got) != 0 {
		t.Errorf("audit events = %d, want 0 over in-memory transport: %s", len(got), formatEvents(got))
	}
}

func contains(s []string, v string) bool {
	for _, e := range s {
		if e == v {
			return true
		}
	}
	return false
}

func formatEvents(evs []audit.Event) string {
	b, _ := json.MarshalIndent(evs, "", "  ")
	return string(b)
}
