// Package streaming provides test tools that exercise progress notifications,
// multi-block content, and chunked output; the long-running side of the MCP
// surface that gateways must pass through faithfully.
package streaming

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/plexara/mcp-test/pkg/tools"
)

const groupName = "streaming"

// Toolkit registers the streaming tools.
type Toolkit struct{}

// New returns a Toolkit.
func New() *Toolkit { return &Toolkit{} }

// Name implements tools.Toolkit.
func (Toolkit) Name() string { return groupName }

// Tools implements tools.Toolkit.
func (Toolkit) Tools() []tools.ToolMeta {
	return []tools.ToolMeta{
		{Name: "progress", Group: groupName, Description: "Emit N progress notifications, then return."},
		{Name: "long_output", Group: groupName, Description: "Return one CallToolResult containing M text blocks of K characters each."},
		{Name: "chatty", Group: groupName, Description: "Return a CallToolResult with multiple, varied content blocks (mix of texts)."},
	}
}

// RegisterTools wires the tools into s.
func (t *Toolkit) RegisterTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "progress",
		Description: "Emit N progress notifications spaced step_ms apart, then return. The client must include a progressToken in _meta to receive notifications.",
	}, t.handleProgress)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "long_output",
		Description: "Return one CallToolResult containing `blocks` text content items, each of `chars` characters.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, t.handleLongOutput)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "chatty",
		Description: "Return a CallToolResult with several diverse text content blocks. Useful for verifying the gateway preserves block ordering and types.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, t.handleChatty)
}

type progressInput struct {
	Steps  int `json:"steps"          jsonschema:"number of progress notifications to emit; default 5; max 100"`
	StepMS int `json:"step_ms"        jsonschema:"sleep between steps in ms; default 200; max 5000"`
}

type progressOutput struct {
	Steps    int  `json:"steps"`
	Notified bool `json:"notified"` // true iff caller supplied a progressToken
	Done     bool `json:"done"`
}

func (Toolkit) handleProgress(ctx context.Context, req *mcp.CallToolRequest, in progressInput) (*mcp.CallToolResult, progressOutput, error) {
	steps := in.Steps
	if steps <= 0 {
		steps = 5
	}
	if steps > 100 {
		steps = 100
	}
	stepMS := in.StepMS
	if stepMS <= 0 {
		stepMS = 200
	}
	if stepMS > 5000 {
		stepMS = 5000
	}

	token := req.Params.GetProgressToken()
	notified := token != nil

	for i := 0; i < steps; i++ {
		if notified {
			// If the client is gone (transport closed mid-stream) NotifyProgress
			// returns an error; bail out rather than burning the rest of the
			// step budget on writes nobody will read.
			if err := req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
				ProgressToken: token,
				Progress:      float64(i + 1),
				Total:         float64(steps),
				Message:       fmt.Sprintf("step %d/%d", i+1, steps),
			}); err != nil {
				return nil, progressOutput{Steps: i, Notified: notified, Done: false}, err
			}
		}
		select {
		case <-ctx.Done():
			return nil, progressOutput{Steps: i, Notified: notified, Done: false}, ctx.Err()
		case <-time.After(time.Duration(stepMS) * time.Millisecond):
		}
	}
	return nil, progressOutput{Steps: steps, Notified: notified, Done: true}, nil
}

type longOutputInput struct {
	Blocks int `json:"blocks" jsonschema:"number of text content blocks to return; default 3; max 50"`
	Chars  int `json:"chars"  jsonschema:"characters per block; default 256; max 65536"`
}

func (Toolkit) handleLongOutput(_ context.Context, _ *mcp.CallToolRequest, in longOutputInput) (*mcp.CallToolResult, any, error) {
	blocks := in.Blocks
	if blocks <= 0 {
		blocks = 3
	}
	if blocks > 50 {
		blocks = 50
	}
	chars := in.Chars
	if chars <= 0 {
		chars = 256
	}
	if chars > 65536 {
		chars = 65536
	}

	contents := make([]mcp.Content, 0, blocks)
	for i := 0; i < blocks; i++ {
		contents = append(contents, &mcp.TextContent{Text: filler(i, chars)})
	}
	return &mcp.CallToolResult{Content: contents}, nil, nil
}

func filler(seq, n int) string {
	header := fmt.Sprintf("[block %d] ", seq)
	if n <= len(header) {
		return header[:n]
	}
	body := strings.Repeat("abcdefghij", (n-len(header)+9)/10)
	return header + body[:n-len(header)]
}

type chattyInput struct{}

func (Toolkit) handleChatty(_ context.Context, _ *mcp.CallToolRequest, _ chattyInput) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: "first block: short"},
			&mcp.TextContent{Text: "second block: a slightly longer string with multiple words"},
			&mcp.TextContent{Text: "third block: numbers 1 2 3 4 5"},
			&mcp.TextContent{Text: "fourth block: unicode; café résumé naïve"},
		},
	}, nil, nil
}
