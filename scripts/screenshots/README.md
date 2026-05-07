# Portal screenshots

Generates the `docs/images/portal/*-{light,dark}.png` set the documentation site embeds (homepage carousel + inline captures in `docs/operations/inspection.md`). Idempotent: the script truncates `audit_events` / `audit_payloads` / `api_keys` and re-seeds deterministic mock data each run.

## Prerequisites

The dev stack must be running. Open a separate terminal:

```sh
make dev
```

This brings up Postgres, Keycloak, and the mcp-test binary on `http://localhost:8080`. The script connects to the same Postgres to seed mock data and points a headless Chromium at the same binary to capture frames.

## Capture

From the repo root:

```sh
make screenshots
```

This runs `node scripts/screenshots/screenshots.mjs`. On first run it `npm install`s the script's `package.json` (Playwright + `pg`).

Override host / API key for non-default deployments:

```sh
SHOTS_BASE_URL=https://staging.example.com \
SHOTS_API_KEY=$REAL_KEY \
make screenshots
```

## What gets captured

Twelve screens × two themes = 24 PNGs at 1440×900 @ 2x DPR. The homepage carousel embeds 11 of these (login is captured but kept out of the rotation).

| slug | shows |
| --- | --- |
| `login` | Sign-in screen (no auth required for capture). |
| `dashboard` | 1-hour stats + recent activity table. |
| `tools` | Tool catalog grouped by category. |
| `tools-tryit` | The Try-It form for the `progress` tool. |
| `audit` | Filterable event browser, populated with seeded data. |
| `audit-drawer` | Drawer open over the events table; deep-linked to a seeded event id with payload + notifications. |
| `audit-compare` | `/portal/audit/compare` side-by-side diff of two seeded events. |
| `audit-livetail` | Live-tail toggle on, SSE stream connected, buffer rendering above the table. |
| `audit-jsonb` | JSONB filter editor expanded with `param.user.id=alice` applied. |
| `keys` | API key listing. |
| `config` | Read-only config viewer. |
| `wellknown` | Discovery metadata. |

## Preview

After capturing, two preview paths:

```sh
open docs/images/portal/        # raw PNG view in Finder / Preview
make docs-serve                  # http://127.0.0.1:8001 (full site context)
```

`make docs-serve` is the closer-to-production view: the homepage carousel cycles through the screenshots (theme-paired with `data-theme` attributes; the page footer toggle swaps which one is visible), and `inspection.md` embeds use the mkdocs-material `#only-light` / `#only-dark` URL fragments to switch per the reader's selected theme.

## When to re-run

Any portal UI change that would shift pixels: layout tweaks, copy edits, new components, theme adjustments. The seed step is deterministic (a fixed PRNG seed produces the same audit events across runs), so re-running on an unchanged binary gives byte-stable PNGs, meaning git diffs only show real visual changes.

## Troubleshooting

**`Postgres connection failed`**: `make dev` isn't running or the stack hasn't finished starting. `make dev-wait` blocks until both Postgres and Keycloak are reachable.

**`Drawer empty / "No response captured"`**: `audit_payloads` seed didn't run; check the Postgres logs for `relation "audit_payloads" does not exist`. Run migrations: `make migrate`.

**`Timeout waiting for selector role="dialog"`**: the deep-link target id wasn't seeded. The script picks the two most-recent successful payload events for the drawer / compare captures; if the random seed produces fewer than two successful payloads (very unlikely with 100 events), the script aborts before capture with a clear message.
