// Package config loads YAML configuration with ${VAR:-default} env interpolation.
package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration document.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	OIDC     OIDCConfig     `yaml:"oidc"`
	APIKeys  APIKeysConfig  `yaml:"api_keys"`
	Auth     AuthConfig     `yaml:"auth"`
	Database DatabaseConfig `yaml:"database"`
	Audit    AuditConfig    `yaml:"audit"`
	Portal   PortalConfig   `yaml:"portal"`
	Tools    ToolsConfig    `yaml:"tools"`
}

// ServerConfig holds the HTTP listener and lifecycle settings.
type ServerConfig struct {
	Name              string         `yaml:"name"`
	Address           string         `yaml:"address"`
	BaseURL           string         `yaml:"base_url"`
	ReadHeaderTimeout time.Duration  `yaml:"read_header_timeout"`
	Shutdown          ShutdownConfig `yaml:"shutdown"`
	TLS               TLSConfig      `yaml:"tls"`
	Streamable        StreamableHTTP `yaml:"streamable"`
}

// ShutdownConfig tunes graceful-drain behavior.
type ShutdownConfig struct {
	GracePeriod      time.Duration `yaml:"grace_period"`
	PreShutdownDelay time.Duration `yaml:"pre_shutdown_delay"`
}

// TLSConfig configures optional in-process TLS.
type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// StreamableHTTP configures the MCP streamable HTTP transport.
type StreamableHTTP struct {
	SessionTimeout time.Duration `yaml:"session_timeout"`
	Stateless      bool          `yaml:"stateless"`
	JSONResponse   bool          `yaml:"json_response"`
}

// OIDCConfig configures bearer-token validation against an external IdP.
type OIDCConfig struct {
	Enabled                   bool          `yaml:"enabled"`
	Issuer                    string        `yaml:"issuer"`
	Audience                  string        `yaml:"audience"`
	ClientID                  string        `yaml:"client_id"`
	ClientSecret              string        `yaml:"client_secret"`
	AllowedClients            []string      `yaml:"allowed_clients"`
	ClockSkewSeconds          int           `yaml:"clock_skew_seconds"`
	JWKSCacheTTL              time.Duration `yaml:"jwks_cache_ttl"`
	SkipSignatureVerification bool          `yaml:"skip_signature_verification"`
}

// APIKeysConfig groups file and DB API key sources.
type APIKeysConfig struct {
	File []FileAPIKey    `yaml:"file"`
	DB   APIKeysDBConfig `yaml:"db"`
}

// FileAPIKey is a single plaintext key loaded from config.
type FileAPIKey struct {
	Name        string `yaml:"name"`
	Key         string `yaml:"key"`
	Description string `yaml:"description"`
}

// APIKeysDBConfig toggles the bcrypt-hashed Postgres key store.
type APIKeysDBConfig struct {
	Enabled bool `yaml:"enabled"`
}

// AuthConfig controls server-wide auth requirements.
type AuthConfig struct {
	AllowAnonymous   bool `yaml:"allow_anonymous"`
	RequireForMCP    bool `yaml:"require_for_mcp"`
	RequireForPortal bool `yaml:"require_for_portal"`
}

// DatabaseConfig configures the pgx connection pool.
type DatabaseConfig struct {
	URL             string        `yaml:"url"`
	MaxOpenConns    int32         `yaml:"max_open_conns"`
	MaxIdleConns    int32         `yaml:"max_idle_conns"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime"`
}

// AuditConfig controls audit log behavior and parameter redaction.
type AuditConfig struct {
	Enabled       bool     `yaml:"enabled"`
	RetentionDays int      `yaml:"retention_days"`
	RedactKeys    []string `yaml:"redact_keys"`
}

// PortalConfig configures the embedded React portal and its session cookie.
type PortalConfig struct {
	Enabled          bool   `yaml:"enabled"`
	CookieName       string `yaml:"cookie_name"`
	CookieSecret     string `yaml:"cookie_secret"`
	CookieSecure     bool   `yaml:"cookie_secure"`
	OIDCRedirectPath string `yaml:"oidc_redirect_path"`
}

// ToolsConfig toggles each tool group on or off.
type ToolsConfig struct {
	Identity  ToolGroupConfig `yaml:"identity"`
	Data      ToolGroupConfig `yaml:"data"`
	Failure   ToolGroupConfig `yaml:"failure"`
	Streaming ToolGroupConfig `yaml:"streaming"`
}

// ToolGroupConfig is the per-group toggle (currently just enable/disable).
type ToolGroupConfig struct {
	Enabled bool `yaml:"enabled"`
}

// Load reads, env-expands, and validates a YAML config file.
func Load(path string) (*Config, error) {
	// #nosec G304 -- path comes from the operator's --config flag; this is the
	// intended entry point and the binary trusts its CLI args.
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	expanded := expandEnv(string(raw))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// applyDefaults fills empty fields with reasonable defaults.
func (c *Config) applyDefaults() {
	if c.Server.Name == "" {
		c.Server.Name = "mcp-test"
	}
	if c.Server.Address == "" {
		c.Server.Address = ":8080"
	}
	if c.Server.BaseURL == "" {
		c.Server.BaseURL = "http://localhost" + portFromAddr(c.Server.Address)
	}
	if c.Server.ReadHeaderTimeout == 0 {
		c.Server.ReadHeaderTimeout = 10 * time.Second
	}
	if c.Server.Shutdown.GracePeriod == 0 {
		c.Server.Shutdown.GracePeriod = 25 * time.Second
	}
	if c.Server.Shutdown.PreShutdownDelay == 0 {
		c.Server.Shutdown.PreShutdownDelay = 2 * time.Second
	}
	if c.Server.Streamable.SessionTimeout == 0 {
		c.Server.Streamable.SessionTimeout = 30 * time.Minute
	}
	if c.OIDC.ClockSkewSeconds == 0 {
		c.OIDC.ClockSkewSeconds = 30
	}
	if c.OIDC.JWKSCacheTTL == 0 {
		c.OIDC.JWKSCacheTTL = time.Hour
	}
	if c.Database.MaxOpenConns == 0 {
		c.Database.MaxOpenConns = 25
	}
	if c.Database.MaxIdleConns == 0 {
		c.Database.MaxIdleConns = 5
	}
	if c.Database.ConnMaxLifetime == 0 {
		c.Database.ConnMaxLifetime = time.Hour
	}
	if c.Audit.RetentionDays == 0 {
		c.Audit.RetentionDays = 30
	}
	if len(c.Audit.RedactKeys) == 0 {
		c.Audit.RedactKeys = []string{"password", "token", "secret", "authorization", "api_key", "credentials"}
	}
	if c.Portal.CookieName == "" {
		c.Portal.CookieName = "mcp_test_session"
	}
	if c.Portal.OIDCRedirectPath == "" {
		c.Portal.OIDCRedirectPath = "/portal/auth/callback"
	}
}

// Validate fails fast on impossible or insecure configurations.
func (c *Config) Validate() error {
	var errs []string
	if c.Database.URL == "" {
		errs = append(errs, "database.url is required")
	}
	if c.Portal.Enabled && c.Portal.CookieSecret == "" {
		errs = append(errs, "portal.cookie_secret is required when portal.enabled=true")
	}
	if c.OIDC.Enabled && c.OIDC.Issuer == "" {
		errs = append(errs, "oidc.issuer is required when oidc.enabled=true")
	}
	if c.OIDC.SkipSignatureVerification && os.Getenv("MCPTEST_INSECURE") != "1" {
		errs = append(errs, "oidc.skip_signature_verification requires MCPTEST_INSECURE=1")
	}
	if !c.OIDC.Enabled && len(c.APIKeys.File) == 0 && !c.APIKeys.DB.Enabled && !c.Auth.AllowAnonymous {
		errs = append(errs, "no auth method enabled: configure oidc, api_keys, or auth.allow_anonymous")
	}
	if len(errs) > 0 {
		return errors.New("invalid config: " + strings.Join(errs, "; "))
	}
	return nil
}

// expandEnv expands ${VAR} and ${VAR:-default} forms in s using os.LookupEnv.
//
// Plain $VAR is intentionally left untouched; config values often contain
// shell-like syntax (e.g. Postgres connection strings) that we don't want to
// rewrite.
var envPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)

func expandEnv(s string) string {
	return envPattern.ReplaceAllStringFunc(s, func(match string) string {
		groups := envPattern.FindStringSubmatch(match)
		if len(groups) == 0 {
			return match
		}
		name, def := groups[1], groups[2]
		if v, ok := os.LookupEnv(name); ok {
			return v
		}
		return def
	})
}

// portFromAddr returns the :port suffix from an address like ":8080" or "0.0.0.0:8080".
func portFromAddr(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[i:]
	}
	return ":8080"
}
