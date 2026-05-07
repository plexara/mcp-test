---
title: Portal
description: The embedded React 19 portal: Login, Dashboard, Tools (with Try-It), Audit, API Keys, Config, Discovery, About. Light and dark themes.
---

# Portal

The portal is a React 19 SPA mounted at `/portal/`, served from the
binary via `go:embed all:dist`. There's no separate frontend server.

## Routes

<div class="def-cards" markdown>

<div class="def-card" markdown>
<div class="def-card__head"><code class="def-card__name">/portal/login</code></div>
<div class="def-card__body" markdown>Sign in with OIDC or paste an API key.</div>
</div>

<div class="def-card" markdown>
<div class="def-card__head"><code class="def-card__name">/portal/</code></div>
<div class="def-card__body" markdown>Dashboard: 1-hour stats and recent activity.</div>
</div>

<div class="def-card" markdown>
<div class="def-card__head"><code class="def-card__name">/portal/tools</code></div>
<div class="def-card__body" markdown>Tool catalog grouped by category.</div>
</div>

<div class="def-card" markdown>
<div class="def-card__head"><code class="def-card__name">/portal/tools/&lt;name&gt;</code></div>
<div class="def-card__body" markdown>Per-tool detail with Overview / Try It tabs.</div>
</div>

<div class="def-card" markdown>
<div class="def-card__head"><code class="def-card__name">/portal/audit</code></div>
<div class="def-card__body" markdown>Filterable event browser with pagination, click-to-expand drawer, JSONB filters, SSE live tail.</div>
</div>

<div class="def-card" markdown>
<div class="def-card__head"><code class="def-card__name">/portal/audit/compare</code></div>
<div class="def-card__body" markdown>Side-by-side structural diff of two events; staged from the drawer's Compare button (`?a=<id>&b=<id>`).</div>
</div>

<div class="def-card" markdown>
<div class="def-card__head"><code class="def-card__name">/portal/keys</code></div>
<div class="def-card__body" markdown>DB-backed API key management.</div>
</div>

<div class="def-card" markdown>
<div class="def-card__head"><code class="def-card__name">/portal/config</code></div>
<div class="def-card__body" markdown>Read-only JSON view of the running config (secrets redacted).</div>
</div>

<div class="def-card" markdown>
<div class="def-card__head"><code class="def-card__name">/portal/wellknown</code></div>
<div class="def-card__body" markdown>Pretty-print of the protected-resource and authorization-server metadata that gateways read.</div>
</div>

<div class="def-card" markdown>
<div class="def-card__head"><code class="def-card__name">/portal/about</code></div>
<div class="def-card__body" markdown>Description of the server, the categories, and the live `server.instructions`.</div>
</div>

</div>

## Authentication

Two paths:

1. **OIDC**: click "Sign in with OIDC" on the login screen. Standard
   PKCE flow; on completion the portal sets an HMAC-signed session
   cookie carrying the resolved Identity.
2. **API key**: paste any valid API key (file or DB store). The SPA
   stores it in `sessionStorage` and adds it as `X-API-Key` to every
   API call. No server-side state.

Sign out clears both: the cookie is removed via
`POST /portal/auth/logout` and the API key is deleted from
sessionStorage.

## Theme

Light / dark / system, toggleable from the sidebar footer
(sun / moon / monitor icons). The choice persists in localStorage.
Dark mode follows the OS by default and reacts to live OS theme
changes.

The theme system uses HSL CSS variables for the color tokens; light
and dark schemes share the same variable names with different values.
A small inline script in `index.html` applies the `.dark` class to
`<html>` before stylesheets load to avoid the classic light-flash on
dark systems.

## Audit inspection

The audit page is the operator-facing surface for the audit pipeline. Clicking any row opens a four-tab drawer (Overview / Request / Response / Notifications) deep-linked via `?id=<event-id>`. Inline buttons replay the captured call against the live MCP server (with confirmation; rate-limited per identity) or stash the event for side-by-side comparison at `/portal/audit/compare`. A **Live tail** toggle subscribes to the SSE stream so new events surface above the historical filter view in real time, and a **JSONB filters** toggle opens the editor for the path-aware filters that compile to GIN-indexed containment queries against `audit_payloads`.

The full operator workflow (capture a call, inspect it, replay it, compare to a baseline, filter, export) is in [Inspection workflow](inspection.md).

## Try It

The Tools page's **Try It** tab renders a per-tool form: sliders for
range-bounded numbers, dropdowns for enums, toggles for booleans,
JSON textareas for free-form payloads, and inline help text. Each
form is hand-tuned for the tool, not derived from JSON schema; see
`ui/src/components/ToolForm.tsx` for the declarative spec.

Submitting calls `POST /api/v1/admin/tryit/<name>`, which:

1. Verifies the portal-authenticated identity.
2. Connects to the in-process MCP server via in-memory transport.
3. Calls the tool.
4. Writes a `source=portal-tryit` audit row tagged with the
   user's identity.

Audit rows from Try-It show up in the same audit log alongside
real client calls, distinguishable by the `source` column.

## API Keys

Create, list, and revoke entries in the bcrypt-hashed
`api_keys` Postgres table.

Creating a key returns the plaintext **once**. Copy it immediately;
the server never stores it in cleartext.

Deleting a key immediately revokes it for all callers — the auth
chain re-queries on every request, so there's no cache to invalidate.

## Config viewer

Read-only JSON of the live, post-defaults config. Secrets
(`portal.cookie_secret`, `oidc.client_secret`, file API key values,
the database password segment of the DSN) are replaced with
`[redacted]` before serialization.

Useful for confirming what the running binary is actually using when
you're not sure what env vars the deployment manifest set.

## Discovery

The Discovery tab pretty-prints
`/.well-known/oauth-protected-resource` and
`/.well-known/oauth-authorization-server` so you can see exactly what
an MCP gateway will discover when it points at this server. Useful
when debugging gateway misconfiguration.

## About

The About tab describes what mcp-test is, what each tool category is
for, and the live `server.instructions` text the MCP server returns
to clients at initialize time. Useful for confirming what your model
is being told.

## Disabling

To not ship the portal at all:

```yaml
portal:
  enabled: false
```

The `/portal/*`, `/api/v1/portal/*`, `/api/v1/admin/*`, and
`/portal/auth/*` routes are simply not mounted. The MCP `/` endpoint
keeps working.

## Building

The SPA build pipeline:

```bash
make ui      # cd ui && pnpm install && pnpm build → ui/dist
             # then copy ui/dist → internal/ui/dist
```

`make build` does not build the SPA; run `make ui` first if you need
the portal in the binary. CI and the release pipeline always build
the SPA.

For SPA development with live reload:

```bash
make ui-dev    # vite dev server on :5173, proxies /api to :8080
```

(You also need the binary running on `:8080`, e.g. `make dev-anon`.)
