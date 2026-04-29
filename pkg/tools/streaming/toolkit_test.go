package streaming

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestLongOutput_BlockShape(t *testing.T) {
	tk := New()
	res, _, err := tk.handleLongOutput(context.Background(), nil, longOutputInput{Blocks: 4, Chars: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) != 4 {
		t.Fatalf("blocks = %d, want 4", len(res.Content))
	}
	for i, c := range res.Content {
		tc, ok := c.(*mcp.TextContent)
		if !ok {
			t.Fatalf("block %d not TextContent: %T", i, c)
		}
		if len(tc.Text) != 100 {
			t.Errorf("block %d len = %d, want 100", i, len(tc.Text))
		}
		if !strings.HasPrefix(tc.Text, "[block ") {
			t.Errorf("block %d missing header: %q", i, tc.Text[:20])
		}
	}
}

func TestLongOutput_DefaultsAndCaps(t *testing.T) {
	tk := New()
	res, _, _ := tk.handleLongOutput(context.Background(), nil, longOutputInput{})
	if got := len(res.Content); got != 3 {
		t.Errorf("default blocks = %d, want 3", got)
	}
	res, _, _ = tk.handleLongOutput(context.Background(), nil, longOutputInput{Blocks: 9999, Chars: 9999999})
	if got := len(res.Content); got != 50 {
		t.Errorf("capped blocks = %d, want 50", got)
	}
	tc := res.Content[0].(*mcp.TextContent)
	if len(tc.Text) != 65536 {
		t.Errorf("capped chars = %d, want 65536", len(tc.Text))
	}
}

func TestChatty_HasMultipleBlocks(t *testing.T) {
	tk := New()
	res, _, _ := tk.handleChatty(context.Background(), nil, chattyInput{})
	if len(res.Content) < 2 {
		t.Errorf("chatty should produce >= 2 blocks, got %d", len(res.Content))
	}
}
