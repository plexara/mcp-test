// Package identity contains test tools that surface identity, args, and HTTP
// headers; the bread-and-butter of verifying an MCP gateway's pass-through
// behavior.
package identity

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/plexara/mcp-test/pkg/auth"
	"github.com/plexara/mcp-test/pkg/tools"
)

const groupName = "identity"

// Toolkit registers the identity test tools.
type Toolkit struct {
	redactHeaders []string
}

// New returns a toolkit. redactHeaders names headers whose values should be
// replaced with "[redacted]" by the headers tool.
func New(redactHeaders []string) *Toolkit {
	rh := make([]string, 0, len(redactHeaders))
	for _, h := range redactHeaders {
		rh = append(rh, strings.ToLower(h))
	}
	return &Toolkit{redactHeaders: rh}
}

// Name implements tools.Toolkit.
func (Toolkit) Name() string { return groupName }

// Tools implements tools.Toolkit.
func (Toolkit) Tools() []tools.ToolMeta {
	return []tools.ToolMeta{
		{Name: "whoami", Group: groupName, Description: "Return the authenticated identity for the calling MCP session."},
		{Name: "echo", Group: groupName, Description: "Echo the provided arguments back to the caller."},
		{Name: "headers", Group: groupName, Description: "Return the HTTP headers received by this server, with sensitive values redacted."},
	}
}

// RegisterTools wires the tools into the given MCP server.
func (t *Toolkit) RegisterTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "whoami",
		Description: "Return the authenticated identity for the calling MCP session.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, t.handleWhoami)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "echo",
		Description: "Echo back the arguments object exactly as received.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, t.handleEcho)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "headers",
		Description: "Return the HTTP headers received by this MCP server, with sensitive values redacted.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, t.handleHeaders)
}

type whoamiInput struct{}

type whoamiOutput struct {
	Subject  string         `json:"subject"`
	Email    string         `json:"email,omitempty"`
	Name     string         `json:"name,omitempty"`
	AuthType string         `json:"auth_type"`
	Claims   map[string]any `json:"claims,omitempty"`
}

func (Toolkit) handleWhoami(ctx context.Context, _ *mcp.CallToolRequest, _ whoamiInput) (*mcp.CallToolResult, whoamiOutput, error) {
	id := auth.GetIdentity(ctx)
	if id == nil {
		return nil, whoamiOutput{}, errIdentityMissing
	}
	out := whoamiOutput{
		Subject:  id.Subject,
		Email:    id.Email,
		Name:     id.Name,
		AuthType: id.AuthType,
		Claims:   id.Claims,
	}
	return nil, out, nil
}

type echoIO struct {
	Message string         `json:"message,omitempty" jsonschema:"a short message to include verbatim in the response"`
	Extras  map[string]any `json:"extras,omitempty"  jsonschema:"optional free-form payload echoed unchanged"`
}

func (Toolkit) handleEcho(_ context.Context, _ *mcp.CallToolRequest, in echoIO) (*mcp.CallToolResult, echoIO, error) {
	return nil, in, nil
}

type headersInput struct{}

type headersOutput struct {
	Headers map[string][]string `json:"headers"`
	Count   int                 `json:"count"`
}

func (t *Toolkit) handleHeaders(_ context.Context, req *mcp.CallToolRequest, _ headersInput) (*mcp.CallToolResult, headersOutput, error) {
	extra := req.GetExtra()
	if extra == nil || extra.Header == nil {
		return nil, headersOutput{Headers: map[string][]string{}}, nil
	}
	out := make(map[string][]string, len(extra.Header))
	for k, vs := range extra.Header {
		lk := strings.ToLower(k)
		if t.shouldRedact(lk) {
			out[k] = []string{"[redacted]"}
			continue
		}
		out[k] = append([]string{}, vs...)
	}
	// JSON encoders sort map keys lexically on serialization, so the
	// returned object is already deterministic; no need to re-store into
	// a fresh map.
	return nil, headersOutput{Headers: out, Count: len(out)}, nil
}

func (t *Toolkit) shouldRedact(headerLower string) bool {
	for _, r := range t.redactHeaders {
		if strings.Contains(headerLower, r) {
			return true
		}
	}
	return false
}

// errIdentityMissing is returned when the audit middleware failed to attach
// an identity to the context; this should never happen in practice but we
// guard against it.
var errIdentityMissing = stringError("no identity on context")

type stringError string

func (e stringError) Error() string { return string(e) }
