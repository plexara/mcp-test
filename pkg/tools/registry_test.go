package tools_test

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/plexara/mcp-test/pkg/tools"
)

// fakeToolkit is a minimal Toolkit implementation for registry tests.
type fakeToolkit struct {
	name string
	meta []tools.ToolMeta
}

func (f *fakeToolkit) Name() string                { return f.name }
func (f *fakeToolkit) Tools() []tools.ToolMeta     { return f.meta }
func (f *fakeToolkit) RegisterTools(_ *mcp.Server) {}

func TestRegistry_AddAndQuery(t *testing.T) {
	r := tools.NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if len(r.Toolkits()) != 0 {
		t.Errorf("fresh registry has %d toolkits", len(r.Toolkits()))
	}
	if len(r.All()) != 0 {
		t.Errorf("fresh registry All() = %d", len(r.All()))
	}
	if len(r.Groups()) != 0 {
		t.Errorf("fresh registry Groups() = %d", len(r.Groups()))
	}

	tk := &fakeToolkit{
		name: "fake",
		meta: []tools.ToolMeta{
			{Name: "alpha", Group: "fake", Description: "first"},
			{Name: "beta", Group: "fake", Description: "second"},
		},
	}
	r.Add(tk)

	if got := r.Toolkits(); len(got) != 1 {
		t.Errorf("Toolkits() = %d, want 1", len(got))
	}
	all := r.All()
	if len(all) != 2 {
		t.Errorf("All() = %d, want 2", len(all))
	}
	groups := r.Groups()
	if groups["alpha"] != "fake" || groups["beta"] != "fake" {
		t.Errorf("Groups() = %+v", groups)
	}
}
