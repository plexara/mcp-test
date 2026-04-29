---
title: Identity tools
description: Three identity-passthrough tools: whoami returns the authenticated identity, echo round-trips arguments, headers reveals the HTTP headers that reached the server.
---

# Identity tools

Three tools that surface authentication context, request arguments,
and HTTP headers. The bread-and-butter of verifying what a gateway
forwards.

## whoami

Returns the authenticated identity for the calling MCP session.

**Arguments:** none.

**Returns:**

```json
{
  "subject": "ca01195f-f6c6-488b-9f18-ae1bde84aa38",
  "email": "dev@example.com",
  "name": "Dev User",
  "auth_type": "oidc",
  "claims": { ... full token claims ... }
}
```

For API-key auth, `subject` is `apikey:<name>`, `auth_type` is
`apikey`, and `email`/`name`/`claims` are empty.

**What it tests:** identity forwarding. If the gateway terminates
incoming auth and re-authenticates upstream with its own credentials,
mcp-test will see the gateway's identity, not the original caller's.
If the gateway pass-throughs, mcp-test will see the original.

## echo

Echoes back the arguments exactly as received.

**Arguments:**

| Field | Type | Notes |
| --- | --- | --- |
| `message` | string | Optional. A short string. |
| `extras` | object | Optional. Any free-form payload. |

**Returns:** the same shape, unchanged.

**What it tests:** argument round-trip and any enrichment applied to
args. If the gateway injects extra fields (a tenant_id, a tracing
header transcribed into args, etc.), the diff between what the client
sent and what `echo` returned shows it.

## headers

Returns the HTTP headers received by the MCP server, with sensitive
values redacted.

**Arguments:** none.

**Returns:**

```json
{
  "headers": {
    "Accept": ["application/json, text/event-stream"],
    "Content-Type": ["application/json"],
    "Mcp-Session-Id": ["7SG2G43XYV6JOQZKTMW37GAPM4"],
    "User-Agent": ["claude-code/1.0"],
    "X-Api-Key": ["[redacted]"],
    "Authorization": ["[redacted]"]
  },
  "count": 6
}
```

Headers whose names contain any of the configured `audit.redact_keys`
substrings (case-insensitive) are replaced with `["[redacted]"]`. By
default that includes `authorization`, `cookie`, `api_key`, and a few
others.

**What it tests:** header pass-through. Most gateways add tracing
headers (`X-Request-Id`, `X-Forwarded-For`, etc.) — this tool shows
what arrives. Some gateways also rewrite or strip headers; the
diff against your client's request highlights the rewrites.

If your gateway should be stripping a header (`Authorization` after
re-authentication, say), check this output to confirm.

## Use in a gateway test

Sample assertion in pseudocode:

```
assert call_tool("whoami").subject == "expected-user-from-claim"
assert call_tool("headers").headers["X-Tenant-Id"] == ["acme-corp"]   # gateway-injected
assert "Authorization" not in call_tool("headers").headers              # gateway should strip
```
