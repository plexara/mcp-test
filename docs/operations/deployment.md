# Deployment

mcp-test is a single Go binary. Run it next to a Postgres instance
and an OIDC provider. There's no separate web frontend, no sidecar.

## Container

The official image is `ghcr.io/plexara/mcp-test`.

```bash
docker pull ghcr.io/plexara/mcp-test:latest
```

Tags: `latest` (every push to `main`), `vX.Y.Z` (every release tag),
`vX` (the latest in a major series). Multi-arch (linux/amd64,
linux/arm64).

The image is `scratch`-based with the binary at
`/usr/local/bin/mcp-test` running as UID 1000. CA certificates are
included for OIDC discovery. There's no shell, no package manager,
nothing to update inside the image.

## Minimum runtime

```yaml
# values.yaml or equivalent
config:
  database:
    url: "${MCPTEST_DATABASE_URL}"
  oidc:
    enabled: true
    issuer: "https://idp.example.com/realms/myorg"
    audience: "mcp-test"
    client_id: "mcp-test-portal"
  portal:
    enabled: true
    cookie_secret: "${MCPTEST_COOKIE_SECRET}"

env:
  MCPTEST_DATABASE_URL: "postgres://mcp:secret@db:5432/mcp_test?sslmode=require"
  MCPTEST_COOKIE_SECRET: "<32+ bytes from your secret manager>"
```

Mount the config at `/app/configs/mcp-test.yaml` and run with
`--config /app/configs/mcp-test.yaml`.

## Kubernetes

A minimal Deployment / Service / Ingress:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-test
spec:
  replicas: 2
  selector:
    matchLabels: { app: mcp-test }
  template:
    metadata:
      labels: { app: mcp-test }
    spec:
      containers:
        - name: mcp-test
          image: ghcr.io/plexara/mcp-test:v1.0.0
          args: ["--config", "/app/configs/mcp-test.yaml"]
          env:
            - name: MCPTEST_DATABASE_URL
              valueFrom:
                secretKeyRef: { name: mcp-test, key: database_url }
            - name: MCPTEST_COOKIE_SECRET
              valueFrom:
                secretKeyRef: { name: mcp-test, key: cookie_secret }
          ports:
            - { containerPort: 8080, name: http }
          readinessProbe:
            httpGet: { path: /readyz, port: http }
          livenessProbe:
            httpGet: { path: /healthz, port: http }
          volumeMounts:
            - name: config
              mountPath: /app/configs
              readOnly: true
      volumes:
        - name: config
          configMap: { name: mcp-test-config }
---
apiVersion: v1
kind: Service
metadata: { name: mcp-test }
spec:
  selector: { app: mcp-test }
  ports: [{ name: http, port: 80, targetPort: 8080 }]
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: mcp-test
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt
spec:
  tls:
    - hosts: [mcp-test.example.com]
      secretName: mcp-test-tls
  rules:
    - host: mcp-test.example.com
      http:
        paths:
          - { path: /, pathType: Prefix, backend: { service: { name: mcp-test, port: { number: 80 } } } }
```

## Multi-replica

mcp-test uses the SDK's default in-memory session store. With
`server.streamable.stateless: false` (default), each replica owns
its own MCP sessions. To run multiple replicas behind a single host
without sticky sessions, you'd need an external session store; the
SDK supports it but mcp-test doesn't ship one.

For a typical deployment of mcp-test (a test fixture, not a
production data plane), one replica is plenty. If you need HA, run
two with sticky sessions at the load balancer.

## Postgres

Point at any PostgreSQL 14+ instance. mcp-test applies migrations on
startup. Common patterns:

- **Cloud-managed** (RDS, Cloud SQL, Aurora): set the DSN, done.
- **Sidecar Postgres** in the same pod: fine for ephemeral test
  fixtures; the database goes away with the pod.
- **Shared instance** with other apps: create a dedicated database
  and user. mcp-test only owns its own schema; coexists fine.

DSN format: standard `postgres://user:pass@host:port/db?sslmode=...`.
Set `sslmode=require` or `verify-full` for any networked deployment.

## OIDC

Any OAuth 2.1 / OIDC compliant IdP works (Keycloak, Auth0, Okta,
Azure AD, Google). Configure two clients:

1. **Public PKCE client** for the portal browser flow. Redirect URI
   `https://<your-host>/portal/auth/callback`.
2. **Confidential client** for service-to-service tokens that hit
   `/`. Direct grant or client_credentials flow. The audience claim
   on issued tokens must equal `oidc.audience`.

If your IdP doesn't add an audience claim by default (Keycloak's
default behavior, for example), configure an audience mapper. The
bundled `dev/keycloak/mcp-test-realm.json` shows how.

## Logging

mcp-test logs JSON to stderr at the level set by `LOG_LEVEL` (default
`info`). Tool calls themselves don't log via slog; they go to the
audit table. Use the audit log for behavioral data and slog for
operational events (startup, shutdown, listener errors).

Forward stderr to your log pipeline of choice.

## Metrics

mcp-test does not currently emit Prometheus metrics. You can derive
operational metrics from the audit table and from standard Go
runtime metrics if you wrap the binary in a metrics-aware proxy. We'd
take a PR adding native Prometheus support; for now it isn't there.

## Hardening

- **TLS**: terminate at your ingress, not at the binary. Set
  `server.base_url` to your public origin so the protected-resource
  metadata advertises the correct URL.
- **Cookie secret**: use 32+ bytes from a secret manager. Rotating
  invalidates all active portal sessions.
- **Audit retention**: enforce via cron, not via mcp-test config
  (mcp-test doesn't prune).
- **API keys**: prefer DB-backed keys (auditable, expirable) over
  file keys for production. File keys are appropriate for static
  service-to-service credentials managed via your secret manager.
- **`oidc.skip_signature_verification`**: never enable in
  production. The binary refuses to honor it without
  `MCPTEST_INSECURE=1`.
