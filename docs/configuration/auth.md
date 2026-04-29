# Authentication

mcp-test ships three auth methods that can be combined. Every request
runs through an auth chain that resolves to an `Identity` (subject,
email, name, auth type, claims) attached to the request context.

## Auth chain

```
1. Token detection: X-API-Key header → API key path
                    Authorization: Bearer <jwt> → OIDC path
                    Neither → anonymous (if allowed) or 401

2. API key path:
   - Try the file store first (constant-time compare against
     entries from api_keys.file).
   - If miss, try the DB store (bcrypt scan over non-expired rows).

3. OIDC path:
   - Validate the JWT signature against cached JWKS keys.
   - Check iss / aud (required) / azp (if allowed_clients is set) /
     exp / iat with the configured clock_skew_seconds leeway.
   - Build Identity from sub, email, name (or preferred_username)
     and the full claims map.
```

The chain is hot-detected per request based on the token shape
(JWT-looking starts with `ey`). The first method that yields a valid
identity wins.

## File API keys

Plaintext keys configured directly in YAML, matched in constant time.

```yaml
api_keys:
  file:
    - name: ci-runner
      key: "${MCPTEST_CI_KEY}"
      description: "CI integration tests"
    - name: human-tester
      key: "${MCPTEST_HUMAN_KEY}"
      description: "Manual exploration"
```

Use cases:

- Local dev (a single dev key)
- CI fixtures (a runner-specific key)
- Service-to-service when bcrypt's overhead matters

The audit log records the entry's `name`. Empty `key` values are
skipped at load time.

## DB API keys

Bcrypt-hashed entries in the `api_keys` Postgres table, managed via
the portal's API Keys page or directly with SQL.

```yaml
api_keys:
  db:
    enabled: true
```

Use cases:

- Per-user keys you want to rotate without redeploying
- Keys with explicit expiry (`expires_at` column)
- Auditable creation/last-used timestamps

The portal mints keys with a `mt_` prefix and returns the plaintext
exactly once. After that the only reference is the bcrypt hash.

## OIDC delegation

External IdP issues bearer tokens; mcp-test validates them.

```yaml
oidc:
  enabled: true
  issuer: "https://idp.example.com/realms/myorg"
  audience: "mcp-test"
  client_id: "mcp-test-portal"
  jwks_cache_ttl: 1h
```

Required claims:

| Claim | Why |
| --- | --- |
| `iss` | Must equal `oidc.issuer` exactly. |
| `aud` | Must contain `oidc.audience`. (Strings are checked equal; arrays are checked for membership.) |
| `exp` | Must be in the future, with `clock_skew_seconds` leeway. |
| `sub` | Becomes `Identity.Subject`. |

Optional claims that mcp-test reads:

- `email` → `Identity.Email`
- `name` or `preferred_username` → `Identity.Name`
- `azp` / `client_id` / `appid` → checked against `allowed_clients` if
  that list is non-empty

### Wiring Keycloak

The bundled `dev/keycloak/mcp-test-realm.json` is a working starting
point. Two clients:

- `mcp-test-portal`: public PKCE client used by the browser login
  flow. Redirect URI:
  `http://localhost:8080/portal/auth/callback`.
- `mcp-test`: confidential client used for direct grants
  (service-to-service tokens).

Both clients carry an audience mapper that injects `aud=mcp-test` into
both the access token and the id_token. Without this, the validator
rejects tokens because Keycloak doesn't add an audience by default.

### Browser PKCE flow

When `oidc.enabled=true` and `portal.enabled=true`, three additional
endpoints are mounted:

- `GET /portal/auth/login` — generates state + PKCE verifier, sets a
  short-lived signed cookie, redirects to the IdP authorization
  endpoint.
- `GET /portal/auth/callback` — exchanges the auth code for tokens,
  validates the id_token, and issues the long-lived session cookie.
- `POST /portal/auth/logout` — clears the session cookie.

The portal's "Sign in with OIDC" button on the login page goes to
`/portal/auth/login`.

## Anonymous mode

```yaml
auth:
  allow_anonymous: true
```

Bypasses the 401 challenge on `/mcp` (only). Requests without
credentials resolve to:

```json
{ "subject": "anonymous", "auth_type": "anonymous" }
```

This still produces audit rows, so a gateway test in anonymous mode
can verify the gateway is forwarding what it should without auth in
the way. The portal route always requires a credential, regardless.

## Identity on context

Tool handlers and middleware read the resolved identity via:

```go
import "github.com/plexara/mcp-test/pkg/auth"

id := auth.GetIdentity(ctx)
if id == nil { /* unauthenticated */ }
fmt.Println(id.Subject, id.Email, id.AuthType)
```

The `whoami` tool returns this identity verbatim, which is how you
verify what the gateway forwarded.
