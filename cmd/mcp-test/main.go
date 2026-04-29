// Command mcp-test is an MCP test server with HTTP streamable transport, used
// to exercise MCP gateways (in particular Plexara's). It is also a small,
// best-practices reference for building MCP servers in Go.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

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
	flag.Parse()

	if *showVersion {
		fmt.Println(server.Version())
		return nil
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
