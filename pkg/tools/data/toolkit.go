// Package data provides deterministic-output test tools. Same input always
// produces the same output, which lets a gateway test enrichment dedup,
// caching, and size handling against known fixtures.
package data

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/plexara/mcp-test/pkg/tools"
)

const groupName = "data"

// Toolkit registers the data tools.
type Toolkit struct{}

// New returns a Toolkit.
func New() *Toolkit { return &Toolkit{} }

// Name implements tools.Toolkit.
func (Toolkit) Name() string { return groupName }

// Tools implements tools.Toolkit.
func (Toolkit) Tools() []tools.ToolMeta {
	return []tools.ToolMeta{
		{Name: "fixed_response", Group: groupName, Description: "Return a deterministic body for a given key. Same key, same body; every time."},
		{Name: "sized_response", Group: groupName, Description: "Return exactly N bytes of deterministic content. For testing size limits and chunking."},
		{Name: "lorem", Group: groupName, Description: "Return seeded lorem-ipsum text. Same seed produces the same output."},
	}
}

// RegisterTools wires the tools into s.
func (t *Toolkit) RegisterTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "fixed_response",
		Description: "Return a deterministic body derived from a key. Calling with the same key always returns the same body.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
	}, t.handleFixed)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "sized_response",
		Description: "Return exactly the requested number of characters. The body is the lowercase ASCII alphabet repeated; not random.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
	}, t.handleSized)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "lorem",
		Description: "Return N words of lorem-ipsum text. With a seed, output is deterministic; without one, every call differs.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, t.handleLorem)
}

type fixedInput struct {
	Key string `json:"key" jsonschema:"key that selects the response body; same key always yields the same body"`
}

type fixedOutput struct {
	Key  string `json:"key"`
	Hash string `json:"hash"`
	Body string `json:"body"`
}

func (Toolkit) handleFixed(_ context.Context, _ *mcp.CallToolRequest, in fixedInput) (*mcp.CallToolResult, fixedOutput, error) {
	sum := sha256.Sum256([]byte(in.Key))
	hex := hex.EncodeToString(sum[:])
	body := fmt.Sprintf("fixed[%s]: %s", in.Key, hex)
	return nil, fixedOutput{Key: in.Key, Hash: hex, Body: body}, nil
}

type sizedInput struct {
	Size int `json:"size" jsonschema:"number of characters to return; must be >= 0"`
}

type sizedOutput struct {
	Size int    `json:"size"`
	Body string `json:"body"`
}

const (
	sizedAlphabet = "abcdefghijklmnopqrstuvwxyz"
	// sizedMax bounds the result size at 1 MiB. The tool exists to test
	// gateway size-limit handling, not to allocate gigabytes; a caller that
	// passes 2_000_000_000 would otherwise force the server to grow a 2 GB
	// buffer per request.
	sizedMax = 1 << 20
)

func (Toolkit) handleSized(_ context.Context, _ *mcp.CallToolRequest, in sizedInput) (*mcp.CallToolResult, sizedOutput, error) {
	if in.Size < 0 {
		return nil, sizedOutput{}, fmt.Errorf("size must be >= 0, got %d", in.Size)
	}
	if in.Size > sizedMax {
		return nil, sizedOutput{}, fmt.Errorf("size %d exceeds max %d", in.Size, sizedMax)
	}
	if in.Size == 0 {
		return nil, sizedOutput{Size: 0, Body: ""}, nil
	}
	var b strings.Builder
	b.Grow(in.Size)
	for i := 0; i < in.Size; i++ {
		b.WriteByte(sizedAlphabet[i%len(sizedAlphabet)])
	}
	return nil, sizedOutput{Size: in.Size, Body: b.String()}, nil
}

type loremInput struct {
	Words int    `json:"words" jsonschema:"number of words to generate; defaults to 50 when zero"`
	Seed  string `json:"seed,omitempty"  jsonschema:"optional seed; same seed gives the same output"`
}

type loremOutput struct {
	Words int    `json:"words"`
	Body  string `json:"body"`
}

// loremDict is a small word bank for fake-Latin generation.
var loremDict = []string{
	"lorem", "ipsum", "dolor", "sit", "amet", "consectetur", "adipiscing",
	"elit", "sed", "do", "eiusmod", "tempor", "incididunt", "ut", "labore",
	"et", "dolore", "magna", "aliqua", "enim", "ad", "minim", "veniam",
	"quis", "nostrud", "exercitation", "ullamco", "laboris", "nisi",
	"aliquip", "ex", "ea", "commodo", "consequat", "duis", "aute", "irure",
	"in", "reprehenderit", "voluptate", "velit", "esse", "cillum", "fugiat",
	"nulla", "pariatur", "excepteur", "sint", "occaecat", "cupidatat",
	"non", "proident", "sunt", "culpa", "qui", "officia", "deserunt",
	"mollit", "anim", "id", "est", "laborum",
}

func (Toolkit) handleLorem(_ context.Context, _ *mcp.CallToolRequest, in loremInput) (*mcp.CallToolResult, loremOutput, error) {
	n := in.Words
	if n <= 0 {
		n = 50
	}
	if n > 5000 {
		n = 5000
	}
	r := newRand(in.Seed)

	words := make([]string, n)
	for i := 0; i < n; i++ {
		words[i] = loremDict[r.IntN(len(loremDict))]
	}
	if len(words) > 0 {
		// Capitalize first letter so the output reads as prose.
		first := words[0]
		words[0] = strings.ToUpper(first[:1]) + first[1:]
	}
	body := strings.Join(words, " ") + "."
	return nil, loremOutput{Words: n, Body: body}, nil
}

// newRand returns a *rand.Rand seeded deterministically from seed; if seed is
// empty it returns one seeded from a non-deterministic source.
//
// math/rand/v2 is intentional here; these tools generate test fixtures and
// must be reproducible from a seed. crypto/rand would be wrong.
func newRand(seed string) *rand.Rand {
	if seed == "" {
		return rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())) // #nosec G404 -- non-crypto PRNG; test fixture
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(seed))
	a := h.Sum64()
	h.Reset()
	_, _ = h.Write([]byte("salt:" + seed))
	b := h.Sum64()
	return rand.New(rand.NewPCG(a, b)) // #nosec G404 -- non-crypto PRNG; test fixture
}
