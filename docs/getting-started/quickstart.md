---
title: Quickstart
description: Bring up Postgres, Keycloak, and the binary in under five minutes with make dev. Point an MCP client at it and call every tool.
---

# Quickstart

The repository ships with a one-command bring-up that gives you a
fully-running stack: Postgres, Keycloak (with a pre-seeded realm), the
binary in foreground, the portal embedded.

```bash
git clone https://github.com/plexara/mcp-test
cd mcp-test
make dev
```

`make dev` does:

1. `docker compose -f docker-compose.dev.yml up -d postgres keycloak`
2. Polls each container until it's healthy and the Keycloak realm
   discovery endpoint responds.
3. Builds the React SPA into `internal/ui/dist` if it isn't already.
4. Runs the binary in the foreground against
   `configs/mcp-test.live.yaml`.

When it's up:

| URL | What |
| --- | --- |
| <http://localhost:8080/portal/> | Portal — sign in with `dev`/`dev` (OIDC) or paste an API key. |
| <http://localhost:8080/> | MCP streamable HTTP endpoint. Browsers redirect to the portal; MCP clients pass through. |
| <http://localhost:8081/> | Keycloak admin console (`admin`/`admin`). |
| <http://localhost:8080/healthz> | Liveness. |

The dev API key is `devkey-please-change`. Override it via
`MCPTEST_DEV_KEY` if you want something else.

## Verify it works

A quick curl smoke test against the running server:

```bash
KEY=devkey-please-change

# Auth challenge — no credentials → 401 with WWW-Authenticate
curl -s -i http://localhost:8080/ -H "Accept: application/json" | head -8

# RFC 9728 protected-resource metadata
curl -s http://localhost:8080/.well-known/oauth-protected-resource | jq

# Portal identity
curl -s -H "X-API-Key: $KEY" http://localhost:8080/api/v1/portal/me | jq
```

To exercise the MCP layer directly, talk JSON-RPC over the streamable
HTTP transport:

```bash
curl -s -X POST http://localhost:8080/ \
  -H "X-API-Key: $KEY" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Protocol-Version: 2025-06-18" \
  -d '{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"1"}},"id":1}'
```

The `result.instructions` field in the response is the server-level
text every MCP client surfaces to the LLM as system context.

## Through the portal

1. Open <http://localhost:8080/portal/>.
2. Click **Sign in with OIDC**, log in with `dev` / `dev`.
3. **Tools** → pick `progress`, click **Try It**, set Steps to 5,
   Step delay to 500ms, click **Run**.
4. Switch to **Audit** to see the row appear.

## Faster iteration: anonymous mode

For UI work or tests where you don't care about auth, skip Keycloak:

```bash
make dev-anon
```

Anonymous mode is *also* compatible with bearer/API-key clients, but
the portal still requires authentication.

## Stop the stack

```bash
# In the foreground binary's terminal: Ctrl-C
make dev-down
```

That stops the containers and removes the Compose network. Volumes
(Postgres data) are kept by default; `docker compose -f
docker-compose.dev.yml down -v` clears them too.

## Next

- [Connect a client](connect-client.md) for Claude Code, the SDK, and
  raw HTTP examples.
- [Configuration → YAML reference](../configuration/reference.md)
  walks every key.
- [Testing a gateway](../operations/gateway-testing.md) is what
  mcp-test exists for.
