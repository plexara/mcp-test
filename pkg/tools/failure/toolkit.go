// Package failure provides test tools that produce controlled failure modes -
// errors, latency, and probabilistic flakiness; so a gateway can be exercised
// against well-defined adversarial inputs.
package failure

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/plexara/mcp-test/pkg/tools"
)

const groupName = "failure"

// Toolkit registers the failure tools.
type Toolkit struct{}

// New returns a Toolkit.
func New() *Toolkit { return &Toolkit{} }

// Name implements tools.Toolkit.
func (Toolkit) Name() string { return groupName }

// Tools implements tools.Toolkit.
func (Toolkit) Tools() []tools.ToolMeta {
	return []tools.ToolMeta{
		{Name: "error", Group: groupName, Description: "Return an error with a caller-specified message and category."},
		{Name: "slow", Group: groupName, Description: "Sleep for the specified milliseconds before returning. Honors context cancellation."},
		{Name: "flaky", Group: groupName, Description: "Fail with probability P (seeded). Same seed + same call number always gives the same outcome."},
	}
}

// RegisterTools wires the tools into s.
func (t *Toolkit) RegisterTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "error",
		Description: "Return an error with a caller-specified message. Optionally categorize as protocol|tool|timeout|auth so audit rows are easy to filter.",
	}, t.handleError)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "slow",
		Description: "Sleep for the specified number of milliseconds, then return. Returns ctx.Err() if cancelled mid-sleep.",
	}, t.handleSlow)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "flaky",
		Description: "Return success or a synthetic failure based on the supplied probability and seed. Useful for retry-policy testing.",
	}, t.handleFlaky)
}

type errorInput struct {
	Message  string `json:"message,omitempty" jsonschema:"error message to return; defaults to 'synthetic error'"`
	Category string `json:"category,omitempty" jsonschema:"optional category label (protocol|tool|timeout|auth) for audit filtering"`
	AsTool   bool   `json:"as_tool,omitempty"  jsonschema:"if true, return CallToolResult.IsError=true instead of a protocol error"`
}

type errorOutput struct {
	Triggered bool `json:"triggered"`
}

func (Toolkit) handleError(_ context.Context, _ *mcp.CallToolRequest, in errorInput) (*mcp.CallToolResult, errorOutput, error) {
	msg := in.Message
	if msg == "" {
		msg = "synthetic error"
	}
	if in.AsTool {
		// Tool-level error: surfaced inside CallToolResult so the client model
		// sees it but the JSON-RPC envelope is success.
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: msg}},
			IsError: true,
		}, errorOutput{Triggered: true}, nil
	}
	// Protocol-level error: bubble up through the SDK as a real error.
	return nil, errorOutput{Triggered: true}, errors.New(msg)
}

type slowInput struct {
	Milliseconds int `json:"milliseconds" jsonschema:"how long to sleep before returning, in ms; capped at 60000"`
}

type slowOutput struct {
	SleptMS int64 `json:"slept_ms"`
}

func (Toolkit) handleSlow(ctx context.Context, _ *mcp.CallToolRequest, in slowInput) (*mcp.CallToolResult, slowOutput, error) {
	d := time.Duration(in.Milliseconds) * time.Millisecond
	if d < 0 {
		d = 0
	}
	if d > 60*time.Second {
		d = 60 * time.Second
	}
	start := time.Now()
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return nil, slowOutput{SleptMS: time.Since(start).Milliseconds()}, ctx.Err()
	}
	return nil, slowOutput{SleptMS: time.Since(start).Milliseconds()}, nil
}

type flakyInput struct {
	FailRate float64 `json:"fail_rate" jsonschema:"probability of failure between 0 and 1"`
	Seed     string  `json:"seed,omitempty"     jsonschema:"seed for reproducible outcomes; same seed + N-th call yields the same outcome"`
	CallID   int     `json:"call_id,omitempty"  jsonschema:"caller-supplied iteration index; combined with seed for repeatability"`
}

type flakyOutput struct {
	Failed   bool    `json:"failed"`
	Roll     float64 `json:"roll"`
	FailRate float64 `json:"fail_rate"`
}

func (Toolkit) handleFlaky(_ context.Context, _ *mcp.CallToolRequest, in flakyInput) (*mcp.CallToolResult, flakyOutput, error) {
	rate := in.FailRate
	if rate < 0 {
		rate = 0
	}
	if rate > 1 {
		rate = 1
	}
	r := flakyRand(in.Seed, in.CallID)
	roll := r.Float64()
	if roll < rate {
		return nil, flakyOutput{Failed: true, Roll: roll, FailRate: rate},
			fmt.Errorf("flaky failure (roll=%.4f < rate=%.4f)", roll, rate)
	}
	return nil, flakyOutput{Failed: false, Roll: roll, FailRate: rate}, nil
}

// flakyRand returns a *rand.Rand seeded by (seed, callID) so failures are
// reproducible across runs. math/rand/v2 is intentional; this is a test
// fixture, not a security primitive.
func flakyRand(seed string, callID int) *rand.Rand {
	if seed == "" {
		return rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())) // #nosec G404 -- non-crypto PRNG; test fixture
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(seed))
	_, _ = fmt.Fprintf(h, "|%d", callID)
	a := h.Sum64()
	h.Reset()
	_, _ = h.Write([]byte("salt|" + seed))
	_, _ = fmt.Fprintf(h, "|%d", callID)
	b := h.Sum64()
	return rand.New(rand.NewPCG(a, b)) // #nosec G404 -- non-crypto PRNG; test fixture
}
