---
title: mcp-test
template: home.html
hide:
  - navigation
  - toc
  - footer
---

# mcp-test

A controllable Model Context Protocol server, built specifically as a
fixture for testing MCP gateways end-to-end.

The tools it exposes are intentionally boring; they return predictable
output for predictable input.
The point isn't what they *do*; the point is how they let you verify a
gateway in front of them is doing the right things: forwarding identity,
redacting secrets, applying enrichment, passing progress notifications,
surviving failures, and so on. Every call is captured in a Postgres-backed
audit log, so a tester can compare what the client sent, what reached this
server, and what came back.

It is also an opinionated reference for building production-quality MCP
servers in Go: official SDK, streamable HTTP transport, OIDC delegation,
audit logging, embedded React portal.

[Get started](getting-started/quickstart.md){ .md-button .md-button--primary }
[Source on GitHub](https://github.com/plexara/mcp-test){ .md-button }

## What's inside

<div class="grid cards" markdown>

-   :material-tools:{ .lg } **12 test tools, 4 categories**

    Identity (whoami, echo, headers), data (deterministic fixtures),
    failure modes (errors, slow, flaky), streaming (progress
    notifications, multi-block content).

-   :material-shield-key:{ .lg } **Real auth, three ways**

    File-loaded API keys (constant-time compare), bcrypt-hashed
    Postgres-backed keys, and external OIDC delegation with JWKS
    caching. RFC 9728 protected-resource metadata so MCP clients can
    discover the IdP.

-   :material-database-search:{ .lg } **Audit log of every call**

    Postgres-backed timeline with sanitized parameters, identity,
    latency, response size, and source. Browse, filter, and chart it
    in the embedded React portal.

-   :material-server-network:{ .lg } **Streamable HTTP at the root**

    Mounted at `/`, with browsers redirected to `/portal/` and MCP
    clients passing through. No path conventions to fight.

-   :material-bookshelf:{ .lg } **Built-in instructions**

    The MCP `initialize` response includes server-level instructions
    that clients surface to the LLM as system context, telling models
    these tools are test fixtures, not data sources.

-   :material-alpha-p-circle:{ .lg } **By Plexara**

    Plexara is a commercial MCP server with configurable enrichment
    built in. mcp-test is what we use to verify Plexara's gateway
    behavior end-to-end; we ship it as OSS so anyone building MCP
    integrations can use the same fixture.

</div>

## Why a separate test server?

Validating an MCP gateway means changing one thing on the gateway
(an enrichment rule, an auth policy, a header rewrite) and observing
the diff at the upstream. To do that observably, the upstream has to
be predictable. mcp-test gives you that:

- Tools that return the same output for the same input
  ([`fixed_response`](tools/data.md#fixed_response),
  [`lorem`](tools/data.md#lorem) with a seed).
- Tools that fail on demand and on a schedule
  ([`slow`](tools/failure.md#slow),
  [`flaky`](tools/failure.md#flaky) seeded for reproducibility).
- Tools that emit progress notifications you can count
  ([`progress`](tools/streaming.md#progress)).
- Tools that echo identity and headers so you can confirm what's
  being forwarded ([`whoami`](tools/identity.md#whoami),
  [`headers`](tools/identity.md#headers)).

Pair that with the audit log and you can write end-to-end assertions
about gateway behavior without running fragile real-data fixtures.

## Where to next

- New here? [Quickstart](getting-started/quickstart.md) gets you a
  running stack with `make dev` and a working portal in under five
  minutes.
- Configuring a deployment? [YAML reference](configuration/reference.md)
  documents every key with its default and environment override.
- Wiring an MCP client? [Connect a client](getting-started/connect-client.md)
  has examples for Claude Code, raw HTTP/JSON-RPC, and the SDK.
- Validating a gateway?
  [Testing a gateway](operations/gateway-testing.md) walks through
  what each tool category proves.
