package config

import "testing"

func TestAuditConfig_CapturePayloadsEnabled(t *testing.T) {
	// Unset → default true.
	c := AuditConfig{}
	if !c.CapturePayloadsEnabled() {
		t.Error("unset CapturePayloads should default true")
	}
	// Explicit true.
	tr := true
	c = AuditConfig{CapturePayloads: &tr}
	if !c.CapturePayloadsEnabled() {
		t.Error("explicit true not honored")
	}
	// Explicit false.
	fa := false
	c = AuditConfig{CapturePayloads: &fa}
	if c.CapturePayloadsEnabled() {
		t.Error("explicit false not honored")
	}
}

func TestAuditConfig_CaptureHeadersEnabled(t *testing.T) {
	c := AuditConfig{}
	if !c.CaptureHeadersEnabled() {
		t.Error("unset CaptureHeaders should default true")
	}
	tr := true
	c = AuditConfig{CaptureHeaders: &tr}
	if !c.CaptureHeadersEnabled() {
		t.Error("explicit true not honored")
	}
	fa := false
	c = AuditConfig{CaptureHeaders: &fa}
	if c.CaptureHeadersEnabled() {
		t.Error("explicit false not honored")
	}
}
