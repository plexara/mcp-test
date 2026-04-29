package auth

import (
	"context"
	"net/http"
)

type ctxKey int

const (
	keyToken ctxKey = iota
	keyIdentity
	keyHeaders
	keyRequestID
	keyRemoteAddr
)

// WithToken stashes the bearer or API key token presented by the caller.
func WithToken(ctx context.Context, tok string) context.Context {
	return context.WithValue(ctx, keyToken, tok)
}

// GetToken retrieves the bearer or API key token, or "" if absent.
func GetToken(ctx context.Context) string {
	if v, ok := ctx.Value(keyToken).(string); ok {
		return v
	}
	return ""
}

// WithIdentity stashes the resolved identity for downstream handlers.
func WithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, keyIdentity, id)
}

// GetIdentity retrieves the resolved identity, or nil if not yet authenticated.
func GetIdentity(ctx context.Context) *Identity {
	if v, ok := ctx.Value(keyIdentity).(*Identity); ok {
		return v
	}
	return nil
}

// WithHeaders stashes a redacted clone of the inbound HTTP headers, so MCP tool
// handlers can introspect them via GetHeaders.
func WithHeaders(ctx context.Context, h http.Header) context.Context {
	return context.WithValue(ctx, keyHeaders, h)
}

// GetHeaders retrieves the captured headers, or nil if none were stashed.
func GetHeaders(ctx context.Context) http.Header {
	if v, ok := ctx.Value(keyHeaders).(http.Header); ok {
		return v
	}
	return nil
}

// WithRequestID attaches a request ID to the context.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyRequestID, id)
}

// GetRequestID returns the request ID, or "" if absent.
func GetRequestID(ctx context.Context) string {
	if v, ok := ctx.Value(keyRequestID).(string); ok {
		return v
	}
	return ""
}

// WithRemoteAddr stashes the caller's remote address.
func WithRemoteAddr(ctx context.Context, addr string) context.Context {
	return context.WithValue(ctx, keyRemoteAddr, addr)
}

// GetRemoteAddr returns the caller's remote address.
func GetRemoteAddr(ctx context.Context) string {
	if v, ok := ctx.Value(keyRemoteAddr).(string); ok {
		return v
	}
	return ""
}
