---
title: Connect a client
description: Wire Claude Code, raw HTTP/JSON-RPC, or the official Go SDK to a running mcp-test instance via Streamable HTTP.
---

# Connect a client

mcp-test speaks the [streamable HTTP transport](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports)
defined by the MCP 2025-06-18 spec. Any compliant MCP client can
connect.

## Claude Code

The repository ships a project-scoped `.mcp.json`:

```json
{
  "mcpServers": {
    "mcp-test": {
      "type": "http",
      "transport": "streamable-http",
      "url": "http://localhost:8080/",
      "headers": {
        "X-API-Key": "devkey-please-change"
      }
    }
  }
}
```

After `make dev` is up, restart Claude Code in the project directory.
On startup it will prompt to approve the project MCP server. Accept,
and all 12 tools become callable from within the session.

For OIDC-token auth instead of API key, replace the headers block with
`"Authorization": "Bearer <jwt>"`. You can mint one from Keycloak with:

```bash
TOKEN=$(curl -s -X POST http://localhost:8081/realms/mcp-test/protocol/openid-connect/token \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=password&client_id=mcp-test&client_secret=dev-mcp-test-client-secret&username=dev&password=dev&scope=openid" \
  | jq -r .access_token)
echo $TOKEN
```

## modelcontextprotocol/go-sdk

```go
import "github.com/modelcontextprotocol/go-sdk/mcp"

httpc := &http.Client{
    Transport: &headerInjector{
        rt: http.DefaultTransport,
        headers: http.Header{"X-API-Key": []string{"devkey-please-change"}},
    },
}
transport := &mcp.StreamableClientTransport{
    Endpoint:   "http://localhost:8080/",
    HTTPClient: httpc,
}
client := mcp.NewClient(&mcp.Implementation{Name: "my-client"}, nil)
session, err := client.Connect(ctx, transport, nil)
if err != nil { panic(err) }
defer session.Close()

res, err := session.CallTool(ctx, &mcp.CallToolParams{
    Name: "whoami",
})
```

(`headerInjector` is a small `http.RoundTripper` wrapper; the
`tests/http_test.go` file in the repository has a full example.)

## Raw HTTP / JSON-RPC

The streamable HTTP protocol is JSON-RPC framed. POST to the endpoint
with `Content-Type: application/json` and
`Accept: application/json, text/event-stream`. The server returns
either a JSON body or an SSE stream depending on the request.

### Initialize

```bash
curl -s -X POST http://localhost:8080/ \
  -H "X-API-Key: devkey-please-change" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Protocol-Version: 2025-06-18" \
  -d '{
    "jsonrpc": "2.0",
    "method": "initialize",
    "params": {
      "protocolVersion": "2025-06-18",
      "capabilities": {},
      "clientInfo": {"name": "curl", "version": "1"}
    },
    "id": 1
  }'
```

The response carries an `Mcp-Session-Id` header that subsequent
requests must round-trip.

### List tools

```bash
curl -s -X POST http://localhost:8080/ \
  -H "X-API-Key: devkey-please-change" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: <id from initialize>" \
  -d '{"jsonrpc":"2.0","method":"tools/list","params":{},"id":2}'
```

### Call a tool

```bash
curl -s -X POST http://localhost:8080/ \
  -H "X-API-Key: devkey-please-change" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: <id>" \
  -d '{
    "jsonrpc": "2.0",
    "method": "tools/call",
    "params": {
      "name": "fixed_response",
      "arguments": {"key": "hello"}
    },
    "id": 3
  }'
```

## TLS

For production deployments behind a TLS terminator (an L7 load
balancer, nginx, etc.), set `server.base_url` to the public origin so
the protected-resource metadata advertises the right URL. The binary
itself can also terminate TLS directly via `server.tls.{cert,key}_file`.

## Next

- [Tools overview](../tools/overview.md) lists every tool with its
  input schema.
- [HTTP API reference](../reference/http-api.md) covers the portal
  REST surface.
