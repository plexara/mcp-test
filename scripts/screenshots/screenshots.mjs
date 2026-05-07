#!/usr/bin/env node
/**
 * Capture portal screenshots in light + dark mode.
 *
 * Prerequisites: the dev stack is running (`make dev` or equivalent) so:
 *   - Postgres is reachable at MCPTEST_DATABASE_URL
 *   - The mcp-test binary is reachable at MCPTEST_BASE_URL
 *
 * The script:
 *   1. Seeds the audit_events and api_keys tables with deterministic mock data.
 *   2. Drives a headless Chromium via Playwright through every portal page.
 *   3. Saves PNG screenshots into docs/images/portal/<page>-<theme>.png at 2x DPR.
 *
 * Re-run on every portal UI change. The seed step is idempotent (truncate + insert).
 */

import { chromium } from "playwright";
import pg from "pg";
import { mkdir, rm } from "node:fs/promises";
import { existsSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = resolve(__dirname, "..", "..");

const BASE_URL = process.env.MCPTEST_BASE_URL || "http://localhost:8080";
const DATABASE_URL =
  process.env.MCPTEST_DATABASE_URL ||
  "postgres://mcp:mcp@localhost:5432/mcp_test?sslmode=disable";
const API_KEY = process.env.MCPTEST_DEV_KEY || "devkey-please-change";
const OUT_DIR = resolve(REPO_ROOT, "docs/images/portal");

const VIEWPORT = { width: 1440, height: 900 };
const DEVICE_SCALE = 2; // retina-sharp screenshots

// PAGES: each entry produces one screenshot per theme. `path` may be a
// string or a 0-arg function (so deep-link targets can reference state
// stashed by seed(), e.g. DRAWER_TARGET_ID). `prep` runs after navigation
// to drive the UI into the right state for the capture (open a drawer,
// toggle live tail, expand a panel).
const PAGES = [
  { slug: "login",     path: "/portal/login",          requiresAuth: false, prep: null },
  { slug: "dashboard", path: "/portal/",               requiresAuth: true,  prep: null },
  { slug: "tools",     path: "/portal/tools",          requiresAuth: true,  prep: null },
  { slug: "tools-tryit", path: "/portal/tools/progress", requiresAuth: true,
    prep: async (page) => {
      // Click into the Try It tab so the form is visible.
      const tryIt = page.locator('button:has-text("Try It")').first();
      if (await tryIt.count()) await tryIt.click();
      await page.waitForTimeout(200);
    } },
  { slug: "audit",     path: "/portal/audit",          requiresAuth: true,  prep: null },

  // v1.2 inspection-utility captures.
  //
  // audit-drawer: deep-link to a seeded event with payload so the four
  // tabs (Overview / Request / Response / Notifications) all have
  // meaningful content. Targets a successful "progress" or similar tool
  // call so notifications render.
  { slug: "audit-drawer",
    path: () => `/portal/audit?id=${DRAWER_TARGET_ID}`,
    requiresAuth: true,
    prep: async (page) => {
      // Wait for the drawer panel (role=dialog) to mount and for the
      // detail query to resolve so the header shows the tool name
      // rather than the loading spinner.
      await page.waitForSelector('[role="dialog"][aria-label="Audit event detail"]', { timeout: 5000 });
      await page.waitForTimeout(500);
    } },

  // audit-compare: side-by-side diff. Two seeded event ids piped via
  // the URL params; the page renders Summary + per-payload diff trees.
  { slug: "audit-compare",
    path: () => `/portal/audit/compare?a=${COMPARE_A_ID}&b=${COMPARE_B_ID}`,
    requiresAuth: true,
    prep: async (page) => {
      // Wait for both event queries to resolve before snapping; the
      // headers go from "Loading..." to the tool name on success.
      await page.waitForSelector('text="Compare events"', { timeout: 5000 });
      await page.waitForLoadState("networkidle");
      await page.waitForTimeout(400);
    } },

  // audit-livetail: live tail toggled on with the buffer visible.
  // Doesn't actually need new SSE traffic to capture; the toggle is
  // visually distinctive (animate-pulse Radio icon, success-colored
  // border, "Waiting for events..." or buffer rendered above table).
  { slug: "audit-livetail",
    path: "/portal/audit",
    requiresAuth: true,
    prep: async (page) => {
      const tail = page.locator('button:has-text("Live tail")').first();
      await tail.click();
      // Give the SSE handshake a beat to land before snapping. The
      // server emits a `: connected` comment immediately, but the
      // empty-buffer state ("Waiting for events...") is also a valid
      // shot.
      await page.waitForTimeout(800);
    } },

  // audit-jsonb: filter editor expanded with one sample filter applied.
  { slug: "audit-jsonb",
    path: "/portal/audit",
    requiresAuth: true,
    prep: async (page) => {
      const toggle = page.locator('button:has-text("JSONB filters")').first();
      await toggle.click();
      await page.waitForTimeout(200);
      // Add a representative `param.user.id=alice` filter so the
      // editor and the active-filter chip both render.
      const pathInput = page.locator('input[placeholder="dotted.path"]').first();
      const valueInput = page.locator('input[placeholder="value"]').first();
      await pathInput.fill("user.id");
      await valueInput.fill("alice");
      await page.locator('button:has-text("add")').first().click();
      await page.waitForTimeout(400);
    } },

  { slug: "keys",      path: "/portal/keys",           requiresAuth: true,  prep: null },
  { slug: "config",    path: "/portal/config",         requiresAuth: true,  prep: null },
  { slug: "wellknown", path: "/portal/wellknown",      requiresAuth: true,  prep: null },
];

const THEMES = [
  { slug: "light", classes: [] },
  { slug: "dark",  classes: ["dark"] },
];

// ---------------------------------------------------------------------------
// Mock data
// ---------------------------------------------------------------------------

const TOOLS = [
  { name: "whoami",         group: "identity", duration: [3, 18] },
  { name: "echo",           group: "identity", duration: [4, 22] },
  { name: "headers",        group: "identity", duration: [5, 30] },
  { name: "fixed_response", group: "data",     duration: [2, 12] },
  { name: "sized_response", group: "data",     duration: [4, 80] },
  { name: "lorem",          group: "data",     duration: [6, 28] },
  { name: "error",          group: "failure",  duration: [1, 8],  errorRate: 1.0 },
  { name: "slow",           group: "failure",  duration: [200, 2400] },
  { name: "flaky",          group: "failure",  duration: [3, 24], errorRate: 0.4 },
  { name: "progress",       group: "streaming", duration: [800, 3500] },
  { name: "long_output",    group: "streaming", duration: [12, 90] },
  { name: "chatty",         group: "streaming", duration: [4, 18] },
];

const USERS = [
  { subject: "ca01195f-f6c6-488b-9f18-ae1bde84aa38", email: "alice@example.com", name: "Alice Anderson", auth: "oidc" },
  { subject: "9f8b2e1c-aa94-4c12-93e1-7d0f2c5a9b88", email: "bob@example.com",   name: "Bob Becker",     auth: "oidc" },
  { subject: "apikey:ci-runner",     email: null, name: null, auth: "apikey", apiKey: "ci-runner" },
  { subject: "apikey:dev-local",     email: null, name: null, auth: "apikey", apiKey: "dev-local" },
];

// Deterministic PRNG so the same seed produces the same screenshots across runs.
function mulberry32(seed) {
  return function () {
    let t = (seed += 0x6d2b79f5);
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}
const rand = mulberry32(20260429); // YYYYMMDD

function randInt(lo, hi) { return Math.floor(rand() * (hi - lo + 1)) + lo; }
function pick(arr) { return arr[Math.floor(rand() * arr.length)]; }

function makeAuditEvent(now, ageSeconds) {
  const ts = new Date(now.getTime() - ageSeconds * 1000);
  const tool = pick(TOOLS);
  const user = pick(USERS);
  const errorRate = tool.errorRate ?? 0.05;
  const success = rand() > errorRate;
  const durationMs = randInt(tool.duration[0], tool.duration[1]);
  const responseChars = success ? randInt(48, 2400) : 0;
  const contentBlocks = success ? randInt(1, 4) : 1;
  const errorCategory = success ? "" : pick(["tool", "protocol", "timeout"]);
  const errorMessage = success ? "" : pick([
    "synthetic error",
    "flaky failure (roll=0.42 < rate=0.50)",
    "context deadline exceeded",
  ]);

  let parameters = null;
  if (tool.name === "echo")           parameters = { message: "hello", extras: { traceId: "abc-123" } };
  if (tool.name === "fixed_response") parameters = { key: pick(["alpha", "beta", "gamma", "delta"]) };
  if (tool.name === "sized_response") parameters = { size: pick([512, 1024, 4096, 16384]) };
  if (tool.name === "lorem")          parameters = { words: pick([20, 50, 100]), seed: pick(["a", "b", null]) };
  if (tool.name === "slow")           parameters = { milliseconds: durationMs };
  if (tool.name === "flaky")          parameters = { fail_rate: 0.5, seed: "demo", call_id: randInt(1, 50) };
  if (tool.name === "progress")       parameters = { steps: randInt(3, 12), step_ms: randInt(100, 500) };

  return {
    id: crypto.randomUUID(),
    ts,
    duration_ms: durationMs,
    request_id: crypto.randomUUID(),
    session_id: pick(["7SG2G43XYV6JOQZKTMW37GAPM4", "BXQ9K2P5R7T8YV3JM4NW6ZA8H1", "K3L9MNB2C5XV7YQ8RT4P6WZ1JD"]),
    user_subject: user.subject,
    user_email: user.email,
    auth_type: user.auth,
    api_key_name: user.auth === "apikey" ? user.apiKey : null,
    tool_name: tool.name,
    tool_group: tool.group,
    parameters: parameters ? JSON.stringify(parameters) : null,
    success,
    error_message: errorMessage,
    error_category: errorCategory,
    request_chars: parameters ? JSON.stringify(parameters).length : 0,
    response_chars: responseChars,
    content_blocks: contentBlocks,
    transport: "http",
    source: rand() < 0.15 ? "portal-tryit" : "mcp",
    remote_addr: pick(["10.0.1.42", "10.0.1.55", "192.168.1.10"]),
    user_agent: pick(["claude-code/1.0", "mcp-go/1.5.0", "curl/8.4.0"]),
  };
}

// ---------------------------------------------------------------------------
// Seed
// ---------------------------------------------------------------------------

async function seed() {
  console.log("→ connecting to Postgres");
  const client = new pg.Client({ connectionString: DATABASE_URL });
  await client.connect();

  try {
    console.log("→ truncating audit_events + audit_payloads + api_keys");
    // audit_payloads cascades on audit_events delete, but TRUNCATE is
    // explicit per table so the order doesn't matter here.
    await client.query("TRUNCATE audit_payloads");
    await client.query("TRUNCATE audit_events CASCADE");
    await client.query("TRUNCATE api_keys");

    console.log("→ inserting api_keys");
    // bcrypt hash of the literal string "demo-key-not-real" (cost 10) so the
    // table has data without any of these keys actually authenticating.
    const dummyHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy";
    const apiKeys = [
      { name: "ci-runner",      desc: "CI integration tests",        created: "now() - interval '21 days'", lastUsed: "now() - interval '12 minutes'" },
      { name: "alice-personal", desc: "Alice personal exploration",  created: "now() - interval '3 days'",  lastUsed: "now() - interval '2 hours'" },
      { name: "demo-only",      desc: "Read-only demo key",          created: "now() - interval '60 days'", lastUsed: "NULL" },
    ];
    for (const k of apiKeys) {
      await client.query(
        `INSERT INTO api_keys (id, name, hash, description, created_by, created_at, last_used_at)
         VALUES ($1, $2, $3, $4, 'alice@example.com', ${k.created}, ${k.lastUsed})`,
        [crypto.randomUUID(), k.name, dummyHash, k.desc],
      );
    }

    console.log("→ inserting audit_events");
    const now = new Date();
    const events = [];
    // 100 events over the past 75 minutes, weighted toward the recent end.
    for (let i = 0; i < 100; i++) {
      const skew = Math.pow(rand(), 2.2); // bias toward 0 (= recent)
      const ageSeconds = Math.floor(skew * 75 * 60);
      events.push(makeAuditEvent(now, ageSeconds));
    }

    const placeholders = events.map((_, i) => {
      const o = i * 22;
      return `($${o+1},$${o+2},$${o+3},$${o+4},$${o+5},$${o+6},$${o+7},$${o+8},$${o+9},$${o+10},$${o+11},$${o+12},$${o+13},$${o+14},$${o+15},$${o+16},$${o+17},$${o+18},$${o+19},$${o+20},$${o+21},$${o+22})`;
    }).join(",\n");

    const values = events.flatMap((e) => [
      e.id, e.ts, e.duration_ms, e.request_id, e.session_id,
      e.user_subject, e.user_email, e.auth_type, e.api_key_name,
      e.tool_name, e.tool_group, e.parameters,
      e.success, e.error_message, e.error_category,
      e.request_chars, e.response_chars, e.content_blocks,
      e.transport, e.source, e.remote_addr, e.user_agent,
    ]);

    await client.query(
      `INSERT INTO audit_events (
        id, ts, duration_ms, request_id, session_id,
        user_subject, user_email, auth_type, api_key_name,
        tool_name, tool_group, parameters,
        success, error_message, error_category,
        request_chars, response_chars, content_blocks,
        transport, source, remote_addr, user_agent
      ) VALUES ${placeholders}`,
      values,
    );

    // Seed audit_payloads for the 20 most-recent events so the v1.2
    // drawer / compare / inspection screenshots have meaningful Request
    // / Response / Notifications tabs to render. The other 80 events
    // stay summary-only; that's a realistic mix for a deployment with
    // capture_payloads on but post-retention payload pruning.
    console.log("→ inserting audit_payloads (20 most-recent events)");
    const recent = [...events]
      .sort((a, b) => b.ts.getTime() - a.ts.getTime())
      .slice(0, 20);
    for (const e of recent) {
      // Build payload request_params from the seeded summary params (when
      // present), and inject a realistic gateway-context block: `user.id`
      // mirrors the audit row's identity so demo JSONB filters like
      // `param.user.id=alice` actually return matches; tenant/region are
      // there to make the JSON view look like a real production payload.
      const baseParams = e.parameters ? JSON.parse(e.parameters) : {};
      const userId = e.user_email
        ? e.user_email.split("@")[0]
        : (e.api_key_name || "anonymous");
      const params = {
        ...baseParams,
        user: {
          id: userId,
          tenant: pick(["acme", "globex", "initech"]),
        },
        request_id: e.request_id,
      };
      let result = null;
      let errBlob = null;
      let notifications = null;
      if (e.success) {
        // Build a plausible-looking CallToolResult JSON for the Response tab.
        result = {
          content: [
            { type: "text", text: makeResponseText(e.tool_name, params) },
          ],
          isError: false,
        };
        if (e.tool_name === "progress" || e.tool_name === "chatty") {
          notifications = makeProgressNotifications(e.ts, params);
        }
      } else {
        errBlob = { message: e.error_message, category: e.error_category };
      }
      await client.query(
        `INSERT INTO audit_payloads (
          event_id, jsonrpc_method,
          request_params, request_size_bytes,
          request_headers, request_remote_addr,
          response_result, response_error, response_size_bytes,
          notifications, notifications_truncated,
          captured_at
        ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
        [
          e.id,
          "tools/call",
          JSON.stringify(params),
          e.request_chars,
          JSON.stringify({
            "User-Agent": [e.user_agent],
            "X-Forwarded-For": [e.remote_addr],
            "Content-Type": ["application/json"],
            // Sensitive headers are stored redacted; mirror the pkg/auth
            // RedactHeaders contract so the drawer Request tab shows the
            // [redacted] values an operator will see in production.
            "Authorization": ["[redacted]"],
            "Cookie": ["[redacted]"],
          }),
          e.remote_addr,
          result ? JSON.stringify(result) : null,
          errBlob ? JSON.stringify(errBlob) : null,
          e.response_chars,
          notifications ? JSON.stringify(notifications) : null,
          false,
          e.ts,
        ],
      );
    }

    console.log(`✓ seeded ${events.length} audit events (${recent.length} with payloads) + ${apiKeys.length} api keys`);

    // Stash the two most-recent successful event ids so the capture
    // step can navigate to /audit?id=<id> and /audit/compare?a=<>&b=<>
    // without guessing.
    const successful = recent.filter((e) => e.success);
    if (successful.length < 2) {
      throw new Error("seed produced fewer than 2 successful payload events; reduce errorRate or increase event count");
    }
    DRAWER_TARGET_ID = successful[0].id;
    COMPARE_A_ID    = successful[0].id;
    COMPARE_B_ID    = successful[1].id;
  } finally {
    await client.end();
  }
}

// makeResponseText shapes a plausible response-block string per tool so
// the Response tab renders something that looks like the real call.
function makeResponseText(tool, params) {
  switch (tool) {
    case "echo":           return JSON.stringify({ ...params, echoed_at: new Date().toISOString() });
    case "fixed_response": return JSON.stringify({ key: params.key, value: "static-fixture" });
    case "sized_response": return "x".repeat(Math.min(params.size, 240)) + "...";
    case "lorem":          return "Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod...";
    case "whoami":         return JSON.stringify({ subject: "alice@example.com", auth_type: "oidc" });
    case "headers":        return JSON.stringify({ "User-Agent": "claude-code/1.0", "X-Forwarded-For": "10.0.1.42" });
    case "slow":           return JSON.stringify({ slept_ms: params.milliseconds });
    case "progress":       return JSON.stringify({ steps_completed: params.steps });
    default:               return `result for ${tool}`;
  }
}

function makeProgressNotifications(eventTs, params) {
  const steps = params.steps ?? 5;
  const stepMs = params.step_ms ?? 200;
  const out = [];
  for (let i = 1; i <= steps; i++) {
    out.push({
      ts: new Date(eventTs.getTime() - (steps - i) * stepMs).toISOString(),
      method: "notifications/progress",
      params: {
        progressToken: "demo-token",
        progress: i,
        total: steps,
        message: `step ${i}/${steps}`,
      },
    });
  }
  return out;
}

// Filled by seed() and consumed by the capture step's prep functions.
let DRAWER_TARGET_ID = null;
let COMPARE_A_ID = null;
let COMPARE_B_ID = null;

// ---------------------------------------------------------------------------
// Capture
// ---------------------------------------------------------------------------

async function capture() {
  console.log(`→ launching Chromium against ${BASE_URL}`);
  if (existsSync(OUT_DIR)) await rm(OUT_DIR, { recursive: true });
  await mkdir(OUT_DIR, { recursive: true });

  const browser = await chromium.launch();
  const context = await browser.newContext({
    viewport: VIEWPORT,
    deviceScaleFactor: DEVICE_SCALE,
  });

  // Reduce motion so background animations don't shimmer between captures.
  await context.addInitScript(() => {
    const css = `*,*::before,*::after{animation-duration:0s !important;animation-delay:0s !important;transition-duration:0s !important}`;
    const s = document.createElement("style");
    s.textContent = css;
    (document.head || document.documentElement).appendChild(s);
  });

  // Establish the portal origin so localStorage / sessionStorage are usable.
  const page = await context.newPage();
  await page.goto(`${BASE_URL}/portal/login`, { waitUntil: "domcontentloaded" });
  await page.evaluate((key) => sessionStorage.setItem("mcp-test-api-key", key), API_KEY);

  for (const target of PAGES) {
    for (const theme of THEMES) {
      // Set theme + api key in storage before navigation. The portal's
      // index.html script reads localStorage at parse time and applies the
      // .dark class before stylesheets load, avoiding any flash.
      await page.evaluate(
        ({ themeSlug, apiKey, requiresAuth }) => {
          localStorage.setItem("mcp-test-theme", themeSlug);
          if (requiresAuth) sessionStorage.setItem("mcp-test-api-key", apiKey);
          else              sessionStorage.removeItem("mcp-test-api-key");
        },
        { themeSlug: theme.slug, apiKey: API_KEY, requiresAuth: target.requiresAuth },
      );

      const targetPath = typeof target.path === "function" ? target.path() : target.path;
      await page.goto(`${BASE_URL}${targetPath}`, { waitUntil: "networkidle" });
      // Some portal pages trigger queries; wait a short beat for them to settle.
      await page.waitForTimeout(500);

      // Re-apply the .dark class in case React replaced it.
      await page.evaluate((dark) => {
        const html = document.documentElement;
        if (dark) html.classList.add("dark");
        else      html.classList.remove("dark");
      }, theme.slug === "dark");

      if (target.prep) await target.prep(page);
      await page.waitForTimeout(200);

      const out = resolve(OUT_DIR, `${target.slug}-${theme.slug}.png`);
      await page.screenshot({ path: out, fullPage: false });
      console.log(`  ✓ ${target.slug} (${theme.slug}) → ${out.replace(REPO_ROOT + "/", "")}`);
    }
  }

  await browser.close();
}

// ---------------------------------------------------------------------------
// Run
// ---------------------------------------------------------------------------

(async () => {
  try {
    await seed();
    await capture();
    console.log("\nDone. Screenshots in docs/images/portal/.");
  } catch (err) {
    console.error("FAIL:", err.message);
    process.exit(1);
  }
})();
