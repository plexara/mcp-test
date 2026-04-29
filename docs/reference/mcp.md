# MCP protocol

mcp-test implements the MCP 2025-06-18 spec via the official
[modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk).

## Transport

Streamable HTTP only. There is no stdio transport.

The SDK's `mcp.NewStreamableHTTPHandler` is mounted at `/`. Behavior:

- `POST /` with `Content-Type: application/json` and
  `Accept: application/json, text/event-stream`: a JSON-RPC request.
  Returns either a JSON body or an SSE stream depending on the method
  and the client's Accept preference.
- `GET /` with `Accept: text/event-stream`: opens a persistent SSE
  stream for server-initiated messages.
- `DELETE /` with the session header: terminates the session.

The session ID is round-tripped via the `Mcp-Session-Id` header.

## Capabilities

The server advertises:

```json
{
  "logging": {},
  "tools": { "listChanged": true }
}
```

(`prompts`, `resources`, `roots`, `sampling`, `elicitation` are not
implemented; they're not relevant to a gateway test fixture.)

## Methods

The SDK handles every standard MCP method. The relevant ones for
mcp-test:

| Method | Notes |
| --- | --- |
| `initialize` | Returns `serverInfo` (name, version), `capabilities`, `protocolVersion`, and `instructions` (see [Server Instructions](../configuration/instructions.md)). |
| `initialized` (notification) | Marks the session as ready. |
| `tools/list` | Returns every registered tool with its JSON schema (derived from the Go input struct). |
| `tools/call` | Invokes the named tool. Subject to the audit middleware. |
| `notifications/progress` | Server → client. Emitted by the `progress` tool when the caller supplies a `progressToken`. |

## Tool schema

Each tool registers with `mcp.AddTool[In, Out]`. The SDK derives the
JSON schema from the `In` type via reflection, including the
`jsonschema:"..."` struct tag for descriptions.

Example: the `lorem` tool's input is

```go
type loremInput struct {
    Words int    `json:"words" jsonschema:"number of words to generate; defaults to 50 when zero"`
    Seed  string `json:"seed,omitempty"  jsonschema:"optional seed; same seed gives the same output"`
}
```

So `tools/list` returns:

```json
{
  "name": "lorem",
  "description": "Return N words of lorem-ipsum text...",
  "inputSchema": {
    "type": "object",
    "properties": {
      "words": { "type": "integer", "description": "number of words to generate; defaults to 50 when zero" },
      "seed":  { "type": "string",  "description": "optional seed; same seed gives the same output" }
    }
  }
}
```

## Middleware

Two pieces of MCP-side middleware sit between the transport and the
tool handlers:

1. **Audit middleware** (`pkg/mcpmw.Audit`). For `tools/call`:
    - Reads `Authorization` / `X-API-Key` from the SDK's
      `RequestExtra.Header`.
    - Runs the auth chain (file → DB API keys → OIDC).
    - Stamps the resolved Identity onto the request context.
    - Sanitizes parameters via `audit.SanitizeParameters`.
    - Times the call, measures response size, writes an audit row.
2. **In-memory bypass**. When `RequestExtra.Header` is nil (the
   in-memory transport used by the portal Try-It proxy), the audit
   middleware stamps an Anonymous identity, lets the call proceed,
   and writes no audit row. The HTTP handler is responsible for
   writing its own audit row in that case.

## What we don't do

- **Stdio transport**. mcp-test is HTTP-only by design.
- **Server-initiated requests**. The SDK supports them (sampling,
  elicitation, `roots/list`); mcp-test doesn't use them.
- **Resource subscriptions**. No resource implementations.
- **Prompts**. No prompt implementations.
- **MCPB bundles**. The reference project ships installable MCPB
  bundles; we don't, since installing mcp-test in a stdio context
  doesn't really make sense for a gateway test fixture.
