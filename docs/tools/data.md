# Data tools

Three tools that produce deterministic outputs. Useful for testing
enrichment dedup, response-size handling, and caching boundaries.

## fixed_response

Returns a deterministic body derived from a key.

**Arguments:**

| Field | Type | Notes |
| --- | --- | --- |
| `key` | string | Required. The same key always yields the same body. |

**Returns:**

```json
{
  "key": "hello",
  "hash": "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
  "body": "fixed[hello]: 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
}
```

`hash` is `sha256(key)`. `body` is `"fixed[<key>]: <hash>"`.

**What it tests:**

- **Caching.** Call twice with the same key; if the gateway caches,
  the second response should arrive without an audit row at the
  upstream (verify in mcp-test's audit log).
- **Enrichment dedup.** If the gateway dedups identical responses
  before enrichment, you can detect that by comparing what reaches
  the upstream against what the client sees.
- **Hash stability.** `hash` is reproducible across processes and
  versions, so cross-environment tests can pin to specific hash
  values.

## sized_response

Returns exactly N characters of deterministic content.

**Arguments:**

| Field | Type | Notes |
| --- | --- | --- |
| `size` | int | Required. Must be >= 0. |

**Returns:**

```json
{
  "size": 1024,
  "body": "abcdefghij...abcdefghij" // exactly 1024 chars
}
```

Body is the lowercase ASCII alphabet repeated, truncated/extended to
exactly the requested length.

**What it tests:**

- **Size limits.** Gateways often enforce response-size caps. Bump
  `size` until you hit the cap, then back off.
- **Chunking.** Large responses get chunked over the streamable HTTP
  transport. Use this to verify the gateway forwards multi-chunk
  responses correctly.
- **Compression.** A 64KB body with high redundancy (the alphabet
  repeats) compresses well; comparing on-the-wire bytes shows
  whether the gateway is compressing.

## lorem

Returns N words of seeded lorem-ipsum text.

**Arguments:**

| Field | Type | Notes |
| --- | --- | --- |
| `words` | int | Required. Number of words to generate. Default 50, max 5000. |
| `seed` | string | Optional. Same seed gives the same output. Empty seed produces non-deterministic output. |

**Returns:**

```json
{
  "words": 50,
  "body": "Excepteur ipsum lorem aliqua ... laborum."
}
```

The first word is capitalized; the body ends with a period. Word
selection uses a PCG generator seeded from FNV-1a hashes of the seed
string.

**What it tests:**

- **Reproducibility.** Two runs with the same `(seed, words)` produce
  byte-identical bodies. Useful for assertion-based tests.
- **Content-type handling.** Bodies are realistic prose, which
  exercises gateway enrichment that's looking for natural-language
  content.
- **Word counts.** The audit log records `response_chars` and
  `content_blocks`; with `lorem` you control the input word count
  precisely.

## Determinism guarantee

`fixed_response` and `lorem` (with seed) commit to byte-identical
output across:

- Process restarts.
- Different machines, different OS / arch.
- Different patch versions of the binary (same major version).

If a major-version bump changes the algorithm, the release notes will
call it out. Until then, you can hard-code expected hashes and bodies
in your gateway tests.
