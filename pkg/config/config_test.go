package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandEnv(t *testing.T) {
	t.Setenv("FOO", "bar")
	cases := map[string]string{
		"${FOO}":               "bar",
		"${MISSING:-fallback}": "fallback",
		"${FOO:-fallback}":     "bar",
		"plain $FOO no expand": "plain $FOO no expand",
		"prefix-${FOO}-suffix": "prefix-bar-suffix",
		"${MISSING}":           "",
	}
	for input, want := range cases {
		if got := expandEnv(input); got != want {
			t.Errorf("expandEnv(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestLoadAndValidate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DBURL", "postgres://x:y@localhost/z")
	t.Setenv("COOKIE_SECRET", "0123456789abcdef0123456789abcdef")

	yaml := `
server:
  address: ":9999"
oidc:
  enabled: false
api_keys:
  file:
    - { name: dev, key: testkey, description: "" }
auth:
  allow_anonymous: false
database:
  url: "${DBURL}"
portal:
  enabled: true
  cookie_secret: "${COOKIE_SECRET}"
`
	path := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database.URL != "postgres://x:y@localhost/z" {
		t.Errorf("DB URL not expanded: %q", cfg.Database.URL)
	}
	if cfg.Server.Name != "mcp-test" {
		t.Errorf("default name not applied: %q", cfg.Server.Name)
	}
	if cfg.Server.Shutdown.GracePeriod == 0 {
		t.Error("default grace period not applied")
	}
}

func TestValidateNoAuthFails(t *testing.T) {
	cfg := &Config{Database: DatabaseConfig{URL: "postgres://x"}}
	cfg.applyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation to fail when no auth method configured")
	}
}
