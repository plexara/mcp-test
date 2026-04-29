package httpsrv

import (
	"context"
	"testing"

	"github.com/plexara/mcp-test/pkg/auth"
)

func TestIdentityFromContext(t *testing.T) {
	if id := IdentityFromContext(context.Background()); id != nil {
		t.Error("expected nil identity on bare ctx")
	}
	ctx := auth.WithIdentity(context.Background(), &auth.Identity{Subject: "alice"})
	if id := IdentityFromContext(ctx); id == nil || id.Subject != "alice" {
		t.Errorf("got %+v", id)
	}
}
