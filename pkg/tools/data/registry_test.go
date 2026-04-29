package data

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/plexara/mcp-test/pkg/build"
)

func TestToolkit_Name(t *testing.T) {
	if New().Name() != "data" {
		t.Errorf("Name() = %q", New().Name())
	}
}

func TestToolkit_RegisterTools(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: build.Version}, nil)
	New().RegisterTools(srv) // smoke
}
