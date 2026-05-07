---
title: Overview
description: What mcp-test is, who should use it, and why a separate test fixture beats poking real upstream MCP servers.
---

# Overview

mcp-test is a Go binary that exposes an MCP server over HTTP, backed by
Postgres (for the audit log and DB-resident API keys) and optionally
fronted by an OIDC provider (for bearer-token auth and portal login).

## What you get

<div class="def-cards" markdown>

<div class="def-card" markdown>
<div class="def-card__head"><code class="def-card__name">/</code></div>
<div class="def-card__body" markdown>
The MCP streamable HTTP endpoint. Browsers hitting this URL are redirected to `/portal/`; MCP clients pass through.
</div>
</div>

<div class="def-card" markdown>
<div class="def-card__head"><code class="def-card__name">/portal/</code></div>
<div class="def-card__body" markdown>
An embedded React 19 portal: dashboard, tools (with a per-tool Try-It form), audit log browser, API-key management, server config viewer, gateway-discovery surface.
</div>
</div>

<div class="def-card" markdown>
<div class="def-card__head"><code class="def-card__name">/api/v1/portal/*</code></div>
<div class="def-card__body" markdown>
Read-only REST endpoints behind cookie or API-key auth. Useful for scripting against a running server.
</div>
</div>

<div class="def-card" markdown>
<div class="def-card__head"><code class="def-card__name">/api/v1/admin/*</code></div>
<div class="def-card__body" markdown>
Mutating REST endpoints (key CRUD, Try-It proxy).
</div>
</div>

<div class="def-card" markdown>
<div class="def-card__head"><code class="def-card__name">/.well-known/oauth-protected-resource</code></div>
<div class="def-card__body" markdown>
RFC 9728 metadata advertising the issuer. The MCP auth gateway points 401 challenges at it.
</div>
</div>

<div class="def-card" markdown>
<div class="def-card__head"><code class="def-card__name">/.well-known/oauth-authorization-server</code></div>
<div class="def-card__body" markdown>
Stub pointing at the upstream issuer's metadata.
</div>
</div>

<div class="def-card" markdown>
<div class="def-card__head"><code class="def-card__name">/healthz and /readyz</code></div>
<div class="def-card__body" markdown>
Liveness and readiness probes.
</div>
</div>

</div>

## Components in front of the binary

- **Postgres** stores audit events and bcrypt-hashed API keys.
  Schema-managed with golang-migrate; the binary applies
  pending migrations at boot.
- **OIDC provider** (optional but recommended) issues bearer tokens
  for API clients and ID tokens for portal login. mcp-test validates
  via JWKS, with TTL caching. The bundled `docker-compose.dev.yml`
  ships a Keycloak with a pre-seeded realm so you can exercise the
  full path locally.

## Auth model

Every request enters one of two paths:

1. **MCP path** (`/`). The HTTP gateway middleware checks for the
   presence of `Authorization: Bearer <jwt>` or `X-API-Key: <key>`
   and either passes through or returns a 401 with a
   `WWW-Authenticate` challenge that points at the resource metadata
   document. The audit middleware on the MCP side then runs the auth
   chain (file API keys → DB API keys → OIDC validator) and stamps
   the resolved Identity onto the request context.
2. **Portal path** (`/portal/...`, `/api/v1/...`). The portal auth
   middleware accepts either the signed session cookie (set by the
   browser PKCE flow) or `X-API-Key` / `Authorization: Bearer`. On
   miss, it serves a 401 — anonymous mode is intentionally not
   honored on portal routes because the portal exposes audit data
   and admin actions.

There is no persona / RBAC system. Any authenticated caller is an
admin from the portal's perspective.

## What this is *not*

- Not a real data source. The tools are test fixtures.
- Not a multi-tenant production server. There's no per-tenant
  isolation; one mcp-test instance is one logical server.
- Not a substitute for running your real upstream MCP server in
  staging. It's a deterministic counterpart that helps you trust
  the gateway behavior between the two.
