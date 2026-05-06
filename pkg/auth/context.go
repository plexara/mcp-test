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

// sensitiveHeaders is the set of inbound HTTP header names whose values are
// stripped before the headers reach ctx (and from there, the audit_payloads
// row). These carry credentials (Authorization, Cookie, X-API-Key) or proxy
// auth state, none of which a tool needs to introspect and all of which are
// dangerous to surface in an audit-log UI. Names are matched
// case-insensitively via http.Header's canonical form.
var sensitiveHeaders = map[string]struct{}{
	"Authorization":       {},
	"Proxy-Authorization": {},
	"Cookie":              {},
	"Set-Cookie":          {},
	"X-Api-Key":           {}, // canonical form of X-API-Key.
}

// RedactHeaders returns a clone of h with sensitive header values replaced by
// a single "[redacted]" entry. Header names are preserved so an operator
// reading the audit log can still see "this request carried an Authorization
// header" without seeing the bearer.
func RedactHeaders(h http.Header) http.Header {
	if h == nil {
		return nil
	}
	out := make(http.Header, len(h))
	for k, vs := range h {
		if _, ok := sensitiveHeaders[http.CanonicalHeaderKey(k)]; ok {
			out[k] = []string{"[redacted]"}
			continue
		}
		out[k] = append([]string(nil), vs...)
	}
	return out
}

// WithHeaders stashes a redacted clone of the inbound HTTP headers, so MCP tool
// handlers can introspect them via GetHeaders. Sensitive headers
// (Authorization, Cookie, X-API-Key, etc.) are replaced with "[redacted]"
// before storage; any future audit-log reader sees the names but not the
// secret values.
func WithHeaders(ctx context.Context, h http.Header) context.Context {
	return context.WithValue(ctx, keyHeaders, RedactHeaders(h))
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
