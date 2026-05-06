// Package server composes the MCP server, HTTP mux, and lifecycle.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/plexara/mcp-test/internal/ui"
	"github.com/plexara/mcp-test/pkg/apikeys"
	"github.com/plexara/mcp-test/pkg/audit"
	auditpg "github.com/plexara/mcp-test/pkg/audit/postgres"
	"github.com/plexara/mcp-test/pkg/auth"
	"github.com/plexara/mcp-test/pkg/build"
	"github.com/plexara/mcp-test/pkg/config"
	"github.com/plexara/mcp-test/pkg/database"
	"github.com/plexara/mcp-test/pkg/database/migrate"
	"github.com/plexara/mcp-test/pkg/httpsrv"
	"github.com/plexara/mcp-test/pkg/mcpmw"
	"github.com/plexara/mcp-test/pkg/tools"
	"github.com/plexara/mcp-test/pkg/tools/data"
	"github.com/plexara/mcp-test/pkg/tools/failure"
	"github.com/plexara/mcp-test/pkg/tools/identity"
	"github.com/plexara/mcp-test/pkg/tools/streaming"
)

// Application is the wired-up server, ready to be started with Run.
type Application struct {
	cfg        *config.Config
	logger     *slog.Logger
	pool       *pgxpool.Pool
	mcpServer  *mcp.Server
	registry   *tools.Registry
	auditLog   audit.Logger
	asyncAudit *audit.AsyncLogger // non-nil when audit is enabled; closed during shutdown
	chain      *auth.Chain
	dbKeys     *apikeys.Store
	oidc       *auth.OIDCAuthenticator
	sessions   *httpsrv.SessionStore
	browser    *httpsrv.BrowserAuth
	readiness  *httpsrv.Readiness
	mux        http.Handler
}

// DBKeys returns the DB-backed key store, or nil if api_keys.db.enabled is false.
func (a *Application) DBKeys() *apikeys.Store { return a.dbKeys }

// Build constructs an Application from a config. It opens the database,
// applies migrations, registers tools, and assembles the HTTP mux.
func Build(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*Application, error) {
	if err := migrate.Up(cfg.Database.URL); err != nil {
		return nil, fmt.Errorf("migrations: %w", err)
	}
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}

	// Wrap the underlying Postgres logger with an async buffered drain so a
	// stalled DB can never inflate request latency. When audit.enabled is
	// false we plug in a NoopLogger and skip the drain goroutine entirely.
	var auditLog audit.Logger
	var asyncAudit *audit.AsyncLogger
	if cfg.Audit.Enabled {
		asyncAudit = audit.NewAsyncLogger(auditpg.New(pool), 4096, 5*time.Second, logger)
		auditLog = asyncAudit
	} else {
		auditLog = audit.NoopLogger{}
		logger.Info("audit disabled by config")
	}

	fileStore := auth.NewFileAPIKeyStore(cfg.APIKeys.File)
	var keyStore auth.APIKeyStore = fileStore
	var dbStore *apikeys.Store
	if cfg.APIKeys.DB.Enabled {
		dbStore = apikeys.New(pool)
		keyStore = auth.CombineKeyStores(fileStore, auth.NewDBAPIKeyStore(dbStore.AsAuthStore()))
	}

	var (
		oidcAuth     auth.OIDCValidator
		oidcConcrete *auth.OIDCAuthenticator
	)
	if cfg.OIDC.Enabled {
		v, err := auth.NewOIDC(ctx, cfg.OIDC)
		if err != nil {
			pool.Close()
			return nil, fmt.Errorf("oidc: %w", err)
		}
		oidcAuth = v
		oidcConcrete = v
	}
	chain := auth.NewChain(cfg.Auth.AllowAnonymous, keyStore, oidcAuth).SetLogger(logger)

	app := buildFromDeps(cfg, logger, chain, auditLog)
	app.pool = pool
	app.dbKeys = dbStore
	app.oidc = oidcConcrete
	app.asyncAudit = asyncAudit

	if cfg.Portal.Enabled {
		sessions, err := httpsrv.NewSessionStore(cfg.Portal.CookieName, cfg.Portal.CookieSecret, cfg.Portal.CookieSecure, 12*time.Hour)
		if err != nil {
			pool.Close()
			return nil, fmt.Errorf("session store: %w", err)
		}
		app.sessions = sessions

		if oidcConcrete != nil {
			ba, err := httpsrv.NewBrowserAuth(ctx, cfg, oidcConcrete, sessions, logger)
			if err != nil {
				pool.Close()
				return nil, fmt.Errorf("browser auth: %w", err)
			}
			app.browser = ba
		}
		// Rebuild the mux with portal handlers attached.
		portalAPI := httpsrv.NewPortalAPI(cfg, app.registry, auditLog)
		adminAPI := httpsrv.NewAdminAPI(dbStore, app.mcpServer, auditLog, app.registry, cfg.Audit.RedactKeys)
		portalAuth := httpsrv.NewPortalAuth(sessions, chain)
		app.mux = buildMuxWithPortal(cfg, app.mcpServer, app.readiness, app.browser, portalAPI, adminAPI, portalAuth)
	}
	return app, nil
}

// BuildWithDeps assembles an Application from supplied dependencies, skipping
// database setup. Used by tests to inject in-memory loggers and stub auth.
func BuildWithDeps(cfg *config.Config, logger *slog.Logger, chain *auth.Chain, auditLog audit.Logger) *Application {
	app := buildFromDeps(cfg, logger, chain, auditLog)

	// Mount portal API (read-only) when enabled, even without OIDC. Browser
	// auth (login/callback) is skipped; portal callers can present
	// X-API-Key directly during tests.
	if cfg.Portal.Enabled {
		var sessions *httpsrv.SessionStore
		if cfg.Portal.CookieSecret != "" {
			sessions, _ = httpsrv.NewSessionStore(cfg.Portal.CookieName, cfg.Portal.CookieSecret, false, time.Hour)
		}
		portalAPI := httpsrv.NewPortalAPI(cfg, app.registry, auditLog)
		adminAPI := httpsrv.NewAdminAPI(nil, app.mcpServer, auditLog, app.registry, cfg.Audit.RedactKeys)
		portalAuth := httpsrv.NewPortalAuth(sessions, chain)
		app.sessions = sessions
		app.mux = buildMuxWithPortal(cfg, app.mcpServer, app.readiness, nil, portalAPI, adminAPI, portalAuth)
	}
	return app
}

func buildFromDeps(cfg *config.Config, logger *slog.Logger, chain *auth.Chain, auditLog audit.Logger) *Application {
	mcpServer := mcp.NewServer(
		&mcp.Implementation{Name: cfg.Server.Name, Version: build.Version},
		&mcp.ServerOptions{
			Instructions: cfg.Server.Instructions,
		},
	)
	registry := buildRegistry(cfg)
	for _, tk := range registry.Toolkits() {
		tk.RegisterTools(mcpServer)
	}
	mcpServer.AddReceivingMiddleware(
		mcpmw.Audit(chain, auditLog, cfg.Audit.RedactKeys, registry.Groups(), auditOptions(cfg.Audit)...),
	)
	// Sending side: capture every notifications/* dispatched during a
	// tool-call window so they land in audit_payloads.notifications.
	// No-op when payload capture is off (the recorder isn't seeded).
	mcpServer.AddSendingMiddleware(mcpmw.Notifications())

	readiness := httpsrv.NewReadiness()
	mux := buildMux(cfg, mcpServer, readiness)

	return &Application{
		cfg:       cfg,
		logger:    logger,
		mcpServer: mcpServer,
		registry:  registry,
		auditLog:  auditLog,
		chain:     chain,
		readiness: readiness,
		mux:       mux,
	}
}

// auditOptions translates config.AuditConfig into the mcpmw.AuditOption
// slice the middleware constructor expects. Keeping the mapping in one
// place lets the test path (BuildWithDeps) reuse it.
func auditOptions(cfg config.AuditConfig) []mcpmw.AuditOption {
	var opts []mcpmw.AuditOption
	if cfg.CapturePayloadsEnabled() {
		opts = append(opts, mcpmw.WithPayloadCapture(cfg.MaxPayloadBytes))
		if cfg.CaptureHeadersEnabled() {
			opts = append(opts, mcpmw.WithHeaderCapture())
		}
		if cfg.MaxNotifications > 0 {
			opts = append(opts, mcpmw.WithMaxNotifications(cfg.MaxNotifications))
		}
	}
	return opts
}

func buildRegistry(cfg *config.Config) *tools.Registry {
	r := tools.NewRegistry()
	if cfg.Tools.Identity.Enabled {
		r.Add(identity.New(cfg.Audit.RedactKeys))
	}
	if cfg.Tools.Data.Enabled {
		r.Add(data.New())
	}
	if cfg.Tools.Failure.Enabled {
		r.Add(failure.New())
	}
	if cfg.Tools.Streaming.Enabled {
		r.Add(streaming.New())
	}
	return r
}

// Close releases held resources: drains the async audit queue, then closes
// the database pool.
func (a *Application) Close() {
	if a.asyncAudit != nil {
		a.asyncAudit.Close()
	}
	if a.pool != nil {
		a.pool.Close()
	}
}

// Handler returns the wrapped HTTP handler. Useful for tests via httptest.
func (a *Application) Handler() http.Handler { return a.mux }

// MCPServer exposes the underlying mcp.Server for in-process invocations
// (e.g. the portal Try-It proxy).
func (a *Application) MCPServer() *mcp.Server { return a.mcpServer }

// Registry exposes the tool registry for portal listings.
func (a *Application) Registry() *tools.Registry { return a.registry }

// Run blocks listening on cfg.Server.Address until ctx is cancelled. It
// gracefully drains: flips readiness to false, sleeps PreShutdownDelay so load
// balancers notice, then runs http.Server.Shutdown with GracePeriod.
func (a *Application) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr:              a.cfg.Server.Address,
		Handler:           a.mux,
		ReadHeaderTimeout: a.cfg.Server.ReadHeaderTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		a.logger.Info("listening", "address", a.cfg.Server.Address)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	a.logger.Info("shutdown requested, draining")
	a.readiness.SetReady(false)

	// Interruptible pre-shutdown delay: a second SIGINT (re-cancels ctx) or
	// an unexpected listener error short-circuits the wait so an impatient
	// operator can force shutdown.
	if d := a.cfg.Server.Shutdown.PreShutdownDelay; d > 0 {
		select {
		case <-time.After(d):
		case err := <-errCh:
			a.logger.Warn("listener exited during pre-shutdown delay", "err", err)
		case <-ctx.Done():
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.Server.Shutdown.GracePeriod)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	// Drain any post-Shutdown listener error so the goroutine is fully
	// reaped before we return.
	if err, ok := <-errCh; ok && err != nil {
		a.logger.Warn("listener post-shutdown error", "err", err)
	}
	a.logger.Info("shutdown complete")
	return nil
}

// buildMux mounts /, /.well-known/*, /healthz, /readyz.
//
// MCP lives at the root; everything more specific (health, well-known,
// portal) takes priority. A browser-redirect middleware bounces HTML GETs
// of "/" to /portal/ so operators visiting the bare host get the UI.
func buildMux(cfg *config.Config, mcpServer *mcp.Server, readiness *httpsrv.Readiness) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", httpsrv.HealthzHandler())
	mux.HandleFunc("GET /readyz", readiness.ReadyzHandler())
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", httpsrv.ProtectedResourceMetadata(cfg))
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", httpsrv.AuthorizationServerStub(cfg))

	mux.Handle("/", mcpRootHandler(cfg, mcpServer))

	return httpsrv.CORS(mux)
}

// mcpRootHandler builds the MCP handler stack for "/":
//
//	BrowserRedirect -> MCPAuthGateway -> StreamableHTTPHandler
//
// In that order, so a browser hitting "/" gets redirected before the auth
// gateway tries to 401 it.
func mcpRootHandler(cfg *config.Config, mcpServer *mcp.Server) http.Handler {
	streamable := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return mcpServer },
		&mcp.StreamableHTTPOptions{
			SessionTimeout:             cfg.Server.Streamable.SessionTimeout,
			Stateless:                  cfg.Server.Streamable.Stateless,
			JSONResponse:               cfg.Server.Streamable.JSONResponse,
			DisableLocalhostProtection: true,
		},
	)
	rmURL := httpsrv.ProtectedResourceMetadataURL(cfg)
	gated := httpsrv.MCPAuthGateway(rmURL, cfg.Auth.AllowAnonymous)(streamable)
	return httpsrv.BrowserRedirect("/portal/", gated)
}

// buildMuxWithPortal extends buildMux with the portal's browser-auth
// endpoints and the read-only / admin APIs. Called only when portal.enabled=true.
func buildMuxWithPortal(
	cfg *config.Config,
	mcpServer *mcp.Server,
	readiness *httpsrv.Readiness,
	browser *httpsrv.BrowserAuth,
	portalAPI *httpsrv.PortalAPI,
	adminAPI *httpsrv.AdminAPI,
	portalAuth *httpsrv.PortalAuth,
) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", httpsrv.HealthzHandler())
	mux.HandleFunc("GET /readyz", readiness.ReadyzHandler())
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", httpsrv.ProtectedResourceMetadata(cfg))
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", httpsrv.AuthorizationServerStub(cfg))

	if browser != nil {
		browser.Mount(mux)
	}
	if portalAPI != nil {
		portalAPI.Mount(mux, portalAuth.Middleware)
	}
	if adminAPI != nil {
		adminAPI.Mount(mux, portalAuth.Middleware)
	}

	// SPA: serve embedded dist/ at /portal/. If the SPA wasn't built into
	// internal/ui/dist (only .gitkeep), Available() returns false and we fall
	// back to a friendly placeholder so curl/portal works without the Node
	// build step.
	if ui.Available() {
		spaFS, _ := ui.FS()
		mux.Handle("/portal/", http.StripPrefix("/portal", httpsrv.SPAHandler(spaFS)))
	} else {
		mux.HandleFunc("GET /portal/", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html><body style="font-family:system-ui;padding:2rem">
<h1>mcp-test portal</h1>
<p>The SPA hasn't been built. Run <code>make ui</code> to populate <code>internal/ui/dist/</code> before building the binary, or use the portal API directly:</p>
<ul><li><a href="/api/v1/portal/me">/api/v1/portal/me</a></li><li><a href="/api/v1/portal/tools">/api/v1/portal/tools</a></li><li><a href="/api/v1/portal/dashboard">/api/v1/portal/dashboard</a></li></ul>
</body></html>`))
		})
	}

	mux.Handle("/", mcpRootHandler(cfg, mcpServer))

	return httpsrv.CORS(mux)
}

// Version returns the build metadata as a one-line string.
func Version() string {
	return strings.Join([]string{build.Version, build.Commit, build.Date}, " ")
}
