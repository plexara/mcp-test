// Command mcp-test is an MCP test server with HTTP streamable transport, used
// to exercise MCP gateways (in particular Plexara's). It is also a small,
// best-practices reference for building MCP servers in Go.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/plexara/mcp-test/internal/server"
	"github.com/plexara/mcp-test/pkg/config"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "configs/mcp-test.yaml", "path to YAML config file")
	address := flag.String("address", "", "override server.address (e.g. :9090)")
	showVersion := flag.Bool("version", false, "print build version and exit")
	healthcheck := flag.Bool("healthcheck", false, "probe http://127.0.0.1:8080/healthz and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(server.Version())
		return nil
	}
	if *healthcheck {
		// Distroless images can't bundle curl/wget, so the binary doubles as
		// its own healthcheck probe. Exits 0 on a 200, non-zero otherwise.
		return runHealthcheck()
	}

	logger := newLogger()
	slog.SetDefault(logger)

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if *address != "" {
		cfg.Server.Address = *address
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	app, err := server.Build(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer app.Close()

	return app.Run(ctx)
}

func runHealthcheck() error {
	url := os.Getenv("MCPTEST_HEALTHCHECK_URL")
	if url == "" {
		url = "http://127.0.0.1:8080/healthz"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	// #nosec G107 G704 -- URL is from a trusted env var the operator sets;
	// this is a self-probe of the binary's own health endpoint.
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: status %d", resp.StatusCode)
	}
	return nil
}

func newLogger() *slog.Logger {
	level := slog.LevelInfo
	switch os.Getenv("LOG_LEVEL") {
	case "debug", "DEBUG":
		level = slog.LevelDebug
	case "warn", "WARN":
		level = slog.LevelWarn
	case "error", "ERROR":
		level = slog.LevelError
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}
