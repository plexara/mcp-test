package server

import (
	"testing"

	"github.com/plexara/mcp-test/pkg/config"
)

// auditOptions is the bridge between operator config and middleware
// behavior. A regression that breaks the translation (e.g., header
// capture accidentally always-on) wouldn't be caught by the higher-
// level integration tests since they don't introspect the middleware
// state. Cover the matrix here.
func TestAuditOptions_DefaultsOn(t *testing.T) {
	// Both *bool fields nil → CapturePayloadsEnabled / CaptureHeadersEnabled
	// return true → both options included.
	cfg := config.AuditConfig{}
	opts := auditOptions(cfg)
	if len(opts) < 2 {
		t.Errorf("expected at least 2 opts (payload + header), got %d", len(opts))
	}
}

func TestAuditOptions_DisableCapture_NoOpts(t *testing.T) {
	fa := false
	cfg := config.AuditConfig{CapturePayloads: &fa}
	opts := auditOptions(cfg)
	if len(opts) != 0 {
		t.Errorf("expected zero opts when capture disabled, got %d", len(opts))
	}
}

func TestAuditOptions_DisableHeaders(t *testing.T) {
	tr := true
	fa := false
	cfg := config.AuditConfig{
		CapturePayloads: &tr,
		CaptureHeaders:  &fa,
	}
	opts := auditOptions(cfg)
	// Should have payload capture but NOT header capture; the option
	// list length is the cheap proxy.
	if len(opts) > 1 {
		t.Errorf("with headers off, expected only payload opt(s), got %d", len(opts))
	}
}

func TestAuditOptions_MaxNotificationsThreaded(t *testing.T) {
	cfg := config.AuditConfig{MaxNotifications: 25}
	opts := auditOptions(cfg)
	// payload + header + notifications = 3 opts.
	if len(opts) != 3 {
		t.Errorf("expected 3 opts (payload + header + notifications), got %d", len(opts))
	}
}

func TestAuditOptions_ZeroNotificationsOmitsOpt(t *testing.T) {
	cfg := config.AuditConfig{MaxNotifications: 0}
	opts := auditOptions(cfg)
	// payload + header = 2; no notifications opt because zero means
	// "use the middleware default".
	if len(opts) != 2 {
		t.Errorf("expected 2 opts (no notifications), got %d", len(opts))
	}
}
