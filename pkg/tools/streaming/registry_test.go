package streaming

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/plexara/mcp-test/pkg/build"
)

func TestToolkit_NameAndTools(t *testing.T) {
	tk := New()
	if tk.Name() != "streaming" {
		t.Errorf("Name() = %q", tk.Name())
	}
	tools := tk.Tools()
	if len(tools) != 3 {
		t.Errorf("Tools() = %d, want 3", len(tools))
	}
	want := map[string]bool{"progress": true, "long_output": true, "chatty": true}
	for _, m := range tools {
		if !want[m.Name] {
			t.Errorf("unexpected tool %q", m.Name)
		}
		if m.Group != "streaming" {
			t.Errorf("tool %q group = %q", m.Name, m.Group)
		}
	}
}

func TestToolkit_RegisterTools(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: build.Version}, nil)
	New().RegisterTools(srv) // smoke
}
