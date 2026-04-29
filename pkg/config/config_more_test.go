package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestApplyDefaults_FillsZeros(t *testing.T) {
	c := &Config{}
	c.applyDefaults()

	if c.Server.Name != "mcp-test" {
		t.Errorf("Server.Name = %q", c.Server.Name)
	}
	if c.Server.Address != ":8080" {
		t.Errorf("Server.Address = %q", c.Server.Address)
	}
	if c.Server.BaseURL == "" {
		t.Error("Server.BaseURL not defaulted")
	}
	if c.Server.ReadHeaderTimeout != 10*time.Second {
		t.Errorf("ReadHeaderTimeout = %v", c.Server.ReadHeaderTimeout)
	}
	if c.Server.Shutdown.GracePeriod != 25*time.Second {
		t.Errorf("GracePeriod = %v", c.Server.Shutdown.GracePeriod)
	}
	if c.OIDC.ClockSkewSeconds != 30 {
		t.Errorf("ClockSkewSeconds = %d", c.OIDC.ClockSkewSeconds)
	}
	if c.OIDC.JWKSCacheTTL != time.Hour {
		t.Errorf("JWKSCacheTTL = %v", c.OIDC.JWKSCacheTTL)
	}
	if c.Database.MaxOpenConns != 25 {
		t.Errorf("MaxOpenConns = %d", c.Database.MaxOpenConns)
	}
	if c.Audit.RetentionDays != 30 {
		t.Errorf("RetentionDays = %d", c.Audit.RetentionDays)
	}
	if len(c.Audit.RedactKeys) == 0 {
		t.Error("RedactKeys not defaulted")
	}
	if c.Portal.CookieName != "mcp_test_session" {
		t.Errorf("CookieName = %q", c.Portal.CookieName)
	}
	if c.Portal.OIDCRedirectPath != "/portal/auth/callback" {
		t.Errorf("OIDCRedirectPath = %q", c.Portal.OIDCRedirectPath)
	}
	if c.Server.Instructions == "" {
		t.Error("Server.Instructions not defaulted")
	}
}

func TestApplyDefaults_DoesNotOverrideSet(t *testing.T) {
	c := &Config{}
	c.Server.Name = "custom"
	c.Server.Address = ":9999"
	c.Audit.RedactKeys = []string{"only-this"}
	c.applyDefaults()
	if c.Server.Name != "custom" {
		t.Errorf("Server.Name overridden to %q", c.Server.Name)
	}
	if c.Server.Address != ":9999" {
		t.Errorf("Server.Address overridden to %q", c.Server.Address)
	}
	if len(c.Audit.RedactKeys) != 1 || c.Audit.RedactKeys[0] != "only-this" {
		t.Errorf("RedactKeys overridden to %v", c.Audit.RedactKeys)
	}
}

func TestValidate_PortalRequiresCookieSecret(t *testing.T) {
	c := &Config{}
	c.Database.URL = "postgres://x"
	c.Portal.Enabled = true
	c.Auth.AllowAnonymous = true // satisfy auth-required check
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "cookie_secret") {
		t.Errorf("expected cookie_secret error, got: %v", err)
	}
	c.Portal.CookieSecret = "16-bytes-or-more-secret"
	if err := c.Validate(); err != nil {
		t.Errorf("unexpected error after secret set: %v", err)
	}
}

func TestValidate_OIDCRequiresIssuer(t *testing.T) {
	c := &Config{}
	c.Database.URL = "postgres://x"
	c.OIDC.Enabled = true
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "oidc.issuer") {
		t.Errorf("expected oidc.issuer error, got: %v", err)
	}
}

func TestValidate_SkipSignatureGated(t *testing.T) {
	c := &Config{}
	c.Database.URL = "postgres://x"
	c.OIDC.Enabled = true
	c.OIDC.Issuer = "https://idp"
	c.OIDC.SkipSignatureVerification = true
	t.Setenv("MCPTEST_INSECURE", "")
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "MCPTEST_INSECURE") {
		t.Errorf("expected MCPTEST_INSECURE gate error, got: %v", err)
	}
	t.Setenv("MCPTEST_INSECURE", "1")
	if err := c.Validate(); err != nil {
		t.Errorf("unexpected error with MCPTEST_INSECURE=1: %v", err)
	}
}

func TestExpandEnv_DefaultPattern(t *testing.T) {
	t.Setenv("PRESENT_VAR", "hello")
	os.Unsetenv("ABSENT_VAR")

	cases := []struct {
		in, want string
	}{
		{"${PRESENT_VAR}", "hello"},
		{"${ABSENT_VAR:-fallback}", "fallback"},
		{"${PRESENT_VAR:-ignored}", "hello"},
		{"plain text", "plain text"},
		{"$NOT_DOLLAR_BRACE", "$NOT_DOLLAR_BRACE"},
		{"${ABSENT_VAR}", ""}, // no default → empty
	}
	for _, c := range cases {
		if got := expandEnv(c.in); got != c.want {
			t.Errorf("expandEnv(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPortFromAddr(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{":8080", ":8080"},
		{"0.0.0.0:9999", ":9999"},
		{"localhost:80", ":80"},
		{"", ":8080"},
	}
	for _, c := range cases {
		if got := portFromAddr(c.in); got != c.want {
			t.Errorf("portFromAddr(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/no/such/file.yaml")
	if err == nil || !strings.Contains(err.Error(), "read config") {
		t.Errorf("expected read error, got: %v", err)
	}
}

func TestLoad_BadYAML(t *testing.T) {
	tmp, err := os.CreateTemp("", "bad-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())
	_, _ = tmp.WriteString("server: { name: [unterminated\n")
	tmp.Close()
	_, err = Load(tmp.Name())
	if err == nil || !strings.Contains(err.Error(), "parse config") {
		t.Errorf("expected parse error, got: %v", err)
	}
}
