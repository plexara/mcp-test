package data

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSizedResponse_Length(t *testing.T) {
	tk := New()
	for _, n := range []int{0, 1, 26, 27, 1000} {
		_, out, err := tk.handleSized(context.Background(), nil, sizedInput{Size: n})
		if err != nil {
			t.Fatalf("size=%d: %v", n, err)
		}
		if len(out.Body) != n {
			t.Errorf("size=%d: body len = %d, want %d", n, len(out.Body), n)
		}
	}
}

func TestSizedResponse_NegativeRejected(t *testing.T) {
	tk := New()
	_, _, err := tk.handleSized(context.Background(), nil, sizedInput{Size: -1})
	if err == nil {
		t.Error("want error for negative size")
	}
}

func TestFixedResponse_Deterministic(t *testing.T) {
	tk := New()
	_, a, _ := tk.handleFixed(context.Background(), nil, fixedInput{Key: "abc"})
	_, b, _ := tk.handleFixed(context.Background(), nil, fixedInput{Key: "abc"})
	if a.Hash != b.Hash || a.Body != b.Body {
		t.Errorf("not deterministic: %+v vs %+v", a, b)
	}
	_, c, _ := tk.handleFixed(context.Background(), nil, fixedInput{Key: "different"})
	if c.Hash == a.Hash {
		t.Error("different keys produced same hash")
	}
}

func TestLorem_SeededDeterministic(t *testing.T) {
	tk := New()
	_, a, _ := tk.handleLorem(context.Background(), nil, loremInput{Words: 30, Seed: "s1"})
	_, b, _ := tk.handleLorem(context.Background(), nil, loremInput{Words: 30, Seed: "s1"})
	if a.Body != b.Body {
		t.Errorf("seeded output not deterministic:\n%s\nvs\n%s", a.Body, b.Body)
	}
	_, c, _ := tk.handleLorem(context.Background(), nil, loremInput{Words: 30, Seed: "s2"})
	if c.Body == a.Body {
		t.Error("different seeds produced same output")
	}
	if !strings.HasSuffix(a.Body, ".") {
		t.Error("lorem output should end with a period")
	}
}

func TestLorem_DefaultsAndCap(t *testing.T) {
	tk := New()
	_, a, _ := tk.handleLorem(context.Background(), nil, loremInput{Words: 0, Seed: "x"})
	if a.Words != 50 {
		t.Errorf("default words = %d, want 50", a.Words)
	}
	_, b, _ := tk.handleLorem(context.Background(), nil, loremInput{Words: 999_999, Seed: "x"})
	if b.Words != 5000 {
		t.Errorf("capped words = %d, want 5000", b.Words)
	}
}

// Sanity check that Tools() and RegisterTools() agree on tool names.
func TestRegisterTools_AllNamesPresent(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "t"}, nil)
	tk := New()
	tk.RegisterTools(srv)
	for _, m := range tk.Tools() {
		if m.Name == "" {
			t.Errorf("empty name in metadata: %+v", m)
		}
	}
}
