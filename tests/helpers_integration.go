//go:build integration

package tests

import (
	"io"
	"log/slog"
	"net/http"
	"testing"
)

func slogDiscard(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// withHeader builds a RoundTripper that adds a single static header to every
// request. Wraps the http_test.go headerInjector (which already supports
// multi-header maps) for callsite ergonomics.
func withHeader(rt http.RoundTripper, key, value string) http.RoundTripper {
	return &headerInjector{rt: rt, headers: http.Header{key: []string{value}}}
}
