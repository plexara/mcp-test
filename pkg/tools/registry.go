// Package tools defines the Toolkit interface and shared metadata used by the
// portal to render tool catalogs and Try-It forms.
package tools

import "github.com/modelcontextprotocol/go-sdk/mcp"

// Toolkit is the contract every group of test tools implements.
type Toolkit interface {
	Name() string
	RegisterTools(s *mcp.Server)
	Tools() []ToolMeta
}

// ToolMeta is the portal-friendly description of a single tool.
type ToolMeta struct {
	Name        string `json:"name"`
	Group       string `json:"group"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema,omitempty"`
}

// Registry collects toolkits for portal listing and Try-It dispatch.
type Registry struct {
	toolkits []Toolkit
	byName   map[string]string // tool name -> group
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byName: map[string]string{}}
}

// Add appends a toolkit and indexes its tools by name.
func (r *Registry) Add(tk Toolkit) {
	r.toolkits = append(r.toolkits, tk)
	for _, t := range tk.Tools() {
		r.byName[t.Name] = tk.Name()
	}
}

// Toolkits returns the registered toolkits in registration order.
func (r *Registry) Toolkits() []Toolkit { return r.toolkits }

// Groups returns a map of tool name to its group, suitable for the audit
// middleware's tool_group column.
func (r *Registry) Groups() map[string]string { return r.byName }

// All returns a flat list of every tool's metadata.
func (r *Registry) All() []ToolMeta {
	var out []ToolMeta
	for _, tk := range r.toolkits {
		out = append(out, tk.Tools()...)
	}
	return out
}
