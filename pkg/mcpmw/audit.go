// Package mcpmw provides MCP-protocol middleware for the audit pipeline.
package mcpmw

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/plexara/mcp-test/pkg/audit"
	"github.com/plexara/mcp-test/pkg/auth"
)

// Audit returns a Middleware that records every tools/call invocation.
//
// The middleware:
//  1. Pulls Authorization / X-API-Key out of the SDK's RequestExtra.Header and
//     stashes the token + headers on ctx for downstream tool handlers.
//  2. Runs the auth chain to resolve an Identity.
//  3. Times the call, records inputs/outputs, and writes an audit row.
//
// Even if authentication fails the row is written, so failed calls show up in
// the audit log alongside successful ones.
func Audit(chain *auth.Chain, logger audit.Logger, redactKeys []string, toolGroups map[string]string) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}

			// In-memory connections (the portal Try-It proxy) carry no HTTP
			// headers, so we can't authenticate from them here. The portal
			// handler stamps the portal-authenticated identity onto ctx
			// before calling CallTool; honor that if present, otherwise
			// fall back to anonymous so tool handlers reading the identity
			// keep working. Skip writing our own audit row in either case;
			// the portal handler writes one tagged source=portal-tryit.
			if extra := req.GetExtra(); extra == nil || extra.Header == nil {
				if existing := auth.GetIdentity(ctx); existing == nil {
					ctx = auth.WithIdentity(ctx, auth.Anonymous())
				}
				return next(ctx, method, req)
			}

			start := time.Now()
			ctx = enrichContext(ctx, req)

			id, authErr := chain.Authenticate(ctx)
			if id != nil {
				ctx = auth.WithIdentity(ctx, id)
			}

			toolName, params := extractCallParams(req)
			ev := audit.NewEvent(toolName).
				WithRequestID(auth.GetRequestID(ctx)).
				WithSessionID(sessionID(req)).
				WithUser(id).
				WithRemoteAddr(auth.GetRemoteAddr(ctx)).
				WithUserAgent(userAgent(req)).
				WithSource("mcp")

			if g, ok := toolGroups[toolName]; ok {
				ev.WithToolGroup(g)
			}

			if authErr != nil {
				ev.WithResult(false, authErr.Error(), time.Since(start).Milliseconds())
				ev.ErrorCategory = "auth"
				_ = logger.Log(ctx, *ev)
				return nil, authErr
			}

			ev.WithParameters(audit.SanitizeParameters(params, redactKeys))

			res, err := next(ctx, method, req)
			ev.WithResult(err == nil, errString(err), time.Since(start).Milliseconds())
			if cr, ok := res.(*mcp.CallToolResult); ok && cr != nil {
				chars, blocks := measureResult(cr)
				ev.WithResponseSize(chars, blocks)
				if cr.IsError && err == nil {
					ev.Success = false
					ev.ErrorCategory = "tool"
				}
			}
			_ = logger.Log(ctx, *ev)
			return res, err
		}
	}
}

// enrichContext attaches request metadata (headers, token, request ID, remote
// addr) from the SDK's RequestExtra onto ctx so downstream code can read it
// uniformly.
func enrichContext(ctx context.Context, req mcp.Request) context.Context {
	ctx = auth.WithRequestID(ctx, uuid.NewString())
	extra := req.GetExtra()
	if extra == nil {
		return ctx
	}
	if extra.Header != nil {
		// Clone so downstream readers can't observe a future mutation by the
		// SDK or middleware that holds a different reference to the map.
		ctx = auth.WithHeaders(ctx, extra.Header.Clone())
		if tok := tokenFromHeader(extra.Header); tok != "" {
			ctx = auth.WithToken(ctx, tok)
		}
		if ra := extra.Header.Get("X-Forwarded-For"); ra != "" {
			ctx = auth.WithRemoteAddr(ctx, firstAddr(ra))
		}
	}
	return ctx
}

func tokenFromHeader(h http.Header) string {
	if k := h.Get("X-API-Key"); k != "" {
		return k
	}
	a := h.Get("Authorization")
	if a == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(a), "bearer ") {
		return strings.TrimSpace(a[len("bearer "):])
	}
	return ""
}

func firstAddr(xff string) string {
	if i := strings.Index(xff, ","); i >= 0 {
		return strings.TrimSpace(xff[:i])
	}
	return strings.TrimSpace(xff)
}

func sessionID(req mcp.Request) string {
	defer func() {
		// req.GetSession() may return a typed-nil *ServerSession (interface
		// non-nil but holding a nil pointer); calling ID() on that panics.
		// Guard with recover so a fake request used in tests can't crash the
		// audit pipeline.
		_ = recover()
	}()
	s := req.GetSession()
	if s == nil {
		return ""
	}
	return s.ID()
}

func userAgent(req mcp.Request) string {
	if extra := req.GetExtra(); extra != nil && extra.Header != nil {
		return extra.Header.Get("User-Agent")
	}
	return ""
}

func extractCallParams(req mcp.Request) (string, map[string]any) {
	switch p := req.GetParams().(type) {
	case *mcp.CallToolParams:
		args, _ := p.Arguments.(map[string]any)
		return p.Name, args
	case *mcp.CallToolParamsRaw:
		var args map[string]any
		_ = jsonUnmarshal(p.Arguments, &args)
		return p.Name, args
	}
	return "", nil
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// measureResult returns (totalCharsOfTextContent, contentBlockCount).
//
// We size by character count of TextContent only; other content types (image,
// resource) get counted in the block tally but not the char tally. Good
// enough for ranking and dashboard rendering; not authoritative for billing.
func measureResult(cr *mcp.CallToolResult) (int, int) {
	chars := 0
	for _, c := range cr.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			chars += len(tc.Text)
		}
	}
	return chars, len(cr.Content)
}

// jsonUnmarshal is a thin alias so we can swap implementations in tests.
var jsonUnmarshal = func(data []byte, v any) error {
	if len(data) == 0 {
		return errors.New("empty")
	}
	return jsonImpl(data, v)
}
