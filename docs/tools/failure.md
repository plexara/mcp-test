# Failure-mode tools

Three tools that produce controlled failure modes (errors, latency,
probabilistic flakiness) so a gateway can be exercised against
well-defined adversarial inputs.

## error

Returns an error with a caller-specified message and category.

**Arguments:**

| Field | Type | Notes |
| --- | --- | --- |
| `message` | string | Optional. Defaults to `"synthetic error"`. |
| `category` | string | Optional. One of `protocol`, `tool`, `timeout`, `auth`. Recorded in the audit row's `error_category` column for filtering. |
| `as_tool` | bool | Optional. If `true`, returns a `CallToolResult` with `IsError=true`; otherwise raises a JSON-RPC protocol error. |

**Returns** (when `as_tool=true`):

```json
{ "content": [{ "type": "text", "text": "synthetic error" }], "isError": true }
```

(when `as_tool=false`): a JSON-RPC error response, no body.

**What it tests:**

- **Error propagation.** Tool-level errors (`IsError=true`) and
  protocol-level errors are different beasts. Verify your gateway
  preserves the distinction. Tool-level errors should reach the
  client as a successful tool call with `isError` set; protocol
  errors should surface as JSON-RPC errors.
- **Audit categorization.** The `category` argument lets you tag
  rows so audit-log filters work. Useful for checking that the
  gateway forwarded the error code rather than masking it.

## slow

Sleeps for the specified milliseconds, then returns. Honors
`ctx.Done()`.

**Arguments:**

| Field | Type | Notes |
| --- | --- | --- |
| `milliseconds` | int | Required. Capped at 60000 (60 seconds). |

**Returns:**

```json
{ "slept_ms": 1500 }
```

If the context is cancelled mid-sleep, the tool returns `ctx.Err()`
and the audit row records the partial duration.

**What it tests:**

- **Timeout policy.** Set `milliseconds` past your gateway's deadline.
  The gateway should cancel the call cleanly, mcp-test should see the
  cancellation, and the audit row should reflect a partial duration.
- **Latency budgets.** Run a series of `slow` calls at varying delays
  and verify the gateway's p95/p99 latency reflects them.
- **Concurrency.** Many concurrent `slow` calls expose the gateway's
  ability to multiplex across the streamable HTTP transport without
  serializing.

## flaky

Returns success or a synthetic failure based on the supplied
probability and a seed.

**Arguments:**

| Field | Type | Notes |
| --- | --- | --- |
| `fail_rate` | float | Required. 0 to 1. Clamped if outside. |
| `seed` | string | Optional. Combined with `call_id` for reproducibility. |
| `call_id` | int | Optional. Caller-supplied iteration index. |

**Returns:**

```json
{ "failed": false, "roll": 0.7234, "fail_rate": 0.5 }
```

Same `(seed, call_id)` always produces the same `roll`, and therefore
the same outcome (failed or not). Different `call_id`s with the same
seed give different outcomes — useful for simulating a sequence of
calls where the failure pattern is predictable.

When the call fails, the JSON-RPC envelope is an error with a message
like `flaky failure (roll=0.4231 < rate=0.5000)`.

**What it tests:**

- **Retry policy.** Set `fail_rate=0.5`, vary `call_id` from 0 to N,
  and verify the gateway retries failures up to its configured limit.
  With a fixed seed, you know exactly which `call_id`s will fail.
- **Backoff timing.** Audit rows give you precise inter-call
  intervals when retries are happening.
- **Idempotency.** A retried `flaky` call with the same seed +
  call_id deterministically succeeds or fails on retry. This lets you
  test whether the gateway re-runs the upstream or just replays a
  cached response.

## Determinism guarantee

`flaky` outputs are byte-stable for fixed `(seed, call_id, fail_rate)`
inputs across process restarts, OS, and arch. The PRNG is `math/rand/v2`'s
PCG seeded from FNV-1a hashes of the seed string.
