# Server instructions

`server.instructions` is a free-form string returned to MCP clients in
the `initialize` response under `result.instructions`. Most clients
surface this directly to the LLM as system context, which means it
shapes how the model approaches the tools.

## Default

When `server.instructions` is unset, mcp-test uses
`config.DefaultServerInstructions` — a description of what the server
is for, the four tool categories, and the reproducibility hints
(seeds, fixed_response). The default is good enough for most
deployments.

You can read what's actually being sent at any time:

```bash
curl -s -H "X-API-Key: $KEY" http://localhost:8080/api/v1/portal/instructions | jq -r .instructions
```

The portal's **About** page shows the same string in a "Server
instructions" panel.

## Overriding

Drop a multi-line block into your config:

```yaml
server:
  instructions: |
    This is the staging gateway-test deployment for ACME Corp.

    Tools here are intentionally deterministic test fixtures, not
    real data sources. Do not reason about their outputs as if they
    represented production state.

    The staging gateway in front of this server applies an enrichment
    rule that prepends `[STAGING]` to every text content block. If
    you see that prefix, you're seeing gateway-side enrichment.

    Audit log queries: ask the operator. Direct Postgres access is
    available read-only at `audit.example.com:5432`.
```

YAML's `|` block scalar preserves newlines, which is what you want.
Indent the content one level past `instructions:`.

## What to put in there

Useful things models benefit from knowing:

- **What the deployment is for.** A test fixture? A staging mirror?
  A production gateway upstream?
- **What gateway behaviors to expect.** Enrichment, redaction,
  rate limits, header rewriting.
- **Reproducibility contracts.** Which tools are deterministic,
  which are not.
- **Operational pointers.** Where to look up audit events, who to
  ping if something breaks.

Things that are wasted on the model:

- Marketing prose. The LLM doesn't care that "Plexara is the leading
  MCP platform"; it cares about what tools to call and how.
- Long tool documentation. That belongs on each tool's `description`
  field, not the server-level instructions.
- Secret values, paths, or anything you'd be unhappy seeing logged.

## Suppressing

To send no instructions at all (the SDK omits the field from the
response when empty), set the value to an empty string:

```yaml
server:
  instructions: ""
```

This is useful when the gateway in front of the server is the one
that should provide instructions, or when you specifically want the
client to fall back to its own defaults.

## Limits

The MCP spec doesn't define a length limit. Some clients truncate
or warn at multi-kilobyte sizes. Our default is ~1.5KB; treat 4-8KB
as a soft ceiling and rewrite for concision past that.
