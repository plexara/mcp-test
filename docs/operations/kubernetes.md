---
title: Kubernetes example
description: Self-contained Kubernetes manifests + install script for mcp-test and a single-replica Postgres. One file per resource, applied with progress.
---

# Kubernetes example

A working installation of mcp-test on any Kubernetes cluster with an `nginx` ingress controller. One file per Kubernetes resource, applied with a small bash installer that prints `[N/M]` progress and waits on readiness between steps.

The manifests live in [`examples/kubernetes/`](https://github.com/plexara/mcp-test/tree/main/examples/kubernetes) on the source repo. Clone the repo, edit the placeholders, run `./install.sh`.

## What gets installed

A single namespace with two workloads:

```mermaid
flowchart LR
    client[client]
    ingress["Ingress<br/>nginx + cert-manager"]
    server["Deployment<br/>mcp-test"]
    db[("StatefulSet<br/>postgres")]

    client -->|"https://mcp-test-server.example.com/"| ingress
    ingress --> server
    server -->|"DATABASE_URL"| db
```

## Manifests

The numeric file prefixes are the apply order; the installer (below) gates on Postgres readiness halfway through. Each file is small enough to read end-to-end. Replace the `REPLACE_ME_*` placeholders manually or let `install.sh` generate values for you on first run.

### Namespace

The namespace everything else lands in. Standard `app.kubernetes.io/*` labels for tooling discovery (Lens, k9s, ArgoCD).

```yaml title="examples/kubernetes/00-namespace.yaml"
apiVersion: v1
kind: Namespace
metadata:
  name: mcp-test
  labels:
    app.kubernetes.io/name: mcp-test
    app.kubernetes.io/part-of: mcp-test
    app.kubernetes.io/managed-by: manifest
```

### Postgres Service

Headless (`clusterIP: None`) so the StatefulSet gets a stable per-pod DNS record (`postgres-0.postgres.mcp-test.svc.cluster.local`). The mcp-test connection string uses the bare `postgres` host name; in-namespace DNS resolves it.

```yaml title="examples/kubernetes/10-postgres-service.yaml"
# Headless ClusterIP for the Postgres StatefulSet. mcp-test reaches the
# DB via DNS name `postgres.mcp-test.svc.cluster.local:5432`. Headless
# (clusterIP: None) gives the StatefulSet a stable per-pod DNS record
# (`postgres-0.postgres...`); the connection string in 20-mcp-test-secret
# uses the unqualified `postgres` host because both pods share a
# namespace and DNS resolves it.
apiVersion: v1
kind: Service
metadata:
  name: postgres
  namespace: mcp-test
  labels:
    app: postgres
    app.kubernetes.io/name: postgres
    app.kubernetes.io/component: db
    app.kubernetes.io/part-of: mcp-test
spec:
  clusterIP: None
  selector:
    app: postgres
  ports:
    - name: postgres
      port: 5432
      targetPort: postgres
      protocol: TCP
```

### Postgres Secret

DB / user / password. The `POSTGRES_PASSWORD` placeholder is filled in-place by `install.sh` with `openssl rand -base64 24`. Pre-fill it manually if you want stable creds across reinstalls (a private fork pattern).

```yaml title="examples/kubernetes/12-postgres-secret.yaml"
# Postgres credentials. Used by both the postgres container (POSTGRES_*
# env) and built into the mcp-test connection string in
# 20-mcp-test-secret. The install.sh script will fill the empty fields
# below with a random password on first run; pre-filling them here lets
# you commit a fixed password to a private fork if you want stable creds
# across reinstalls.
apiVersion: v1
kind: Secret
metadata:
  name: postgres
  namespace: mcp-test
  labels:
    app: postgres
    app.kubernetes.io/name: postgres
    app.kubernetes.io/component: db
    app.kubernetes.io/part-of: mcp-test
type: Opaque
stringData:
  POSTGRES_DB: mcp_test
  POSTGRES_USER: mcp
  # REPLACE: 24+ char random string (e.g. `openssl rand -base64 24`).
  # install.sh writes one for you on a fresh install.
  POSTGRES_PASSWORD: "REPLACE_ME_POSTGRES_PASSWORD"
```

### Postgres StatefulSet

Single replica with a 5Gi PVC. `pg_isready` probes for readiness and liveness. The `securityContext` runs as the `postgres` UID baked into the alpine image. Bump the storage request before scale-out demos.

```yaml title="examples/kubernetes/14-postgres-statefulset.yaml"
# Single-replica Postgres for the mcp-test audit log + DB-backed API
# keys. 5Gi PVC sized for ~50M audit_events rows at the default
# 30-day retention with payloads on. Bump the storage request before
# scale-out demos.
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: postgres
  namespace: mcp-test
  labels:
    app: postgres
    app.kubernetes.io/name: postgres
    app.kubernetes.io/component: db
    app.kubernetes.io/part-of: mcp-test
spec:
  serviceName: postgres
  replicas: 1
  selector:
    matchLabels:
      app: postgres
  template:
    metadata:
      labels:
        app: postgres
        app.kubernetes.io/name: postgres
        app.kubernetes.io/component: db
        app.kubernetes.io/part-of: mcp-test
    spec:
      terminationGracePeriodSeconds: 30
      securityContext:
        runAsUser: 70 # postgres uid in the alpine image
        runAsGroup: 70
        fsGroup: 70
      containers:
        - name: postgres
          image: postgres:17-alpine
          imagePullPolicy: IfNotPresent
          ports:
            - name: postgres
              containerPort: 5432
              protocol: TCP
          env:
            - name: POSTGRES_DB
              valueFrom:
                secretKeyRef:
                  name: postgres
                  key: POSTGRES_DB
            - name: POSTGRES_USER
              valueFrom:
                secretKeyRef:
                  name: postgres
                  key: POSTGRES_USER
            - name: POSTGRES_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: postgres
                  key: POSTGRES_PASSWORD
            - name: PGDATA
              value: /var/lib/postgresql/data/pgdata
          volumeMounts:
            - name: data
              mountPath: /var/lib/postgresql/data
          readinessProbe:
            exec:
              command: ["pg_isready", "-U", "mcp", "-d", "mcp_test"]
            initialDelaySeconds: 5
            periodSeconds: 5
            timeoutSeconds: 3
            failureThreshold: 6
          livenessProbe:
            exec:
              command: ["pg_isready", "-U", "mcp", "-d", "mcp_test"]
            initialDelaySeconds: 30
            periodSeconds: 30
            timeoutSeconds: 5
            failureThreshold: 3
          resources:
            requests:
              cpu: 100m
              memory: 256Mi
            limits:
              cpu: 1000m
              memory: 1Gi
  volumeClaimTemplates:
    - metadata:
        name: data
      spec:
        accessModes: ["ReadWriteOnce"]
        resources:
          requests:
            storage: 5Gi
        # storageClassName: REPLACE_ME (omit to use cluster default)
```

### mcp-test Secret

Three application secrets: the full Postgres `DATABASE_URL` (built from the postgres password by the installer to keep them in sync), the portal cookie HMAC key, and the bootstrap `X-API-Key` value. All three are `REPLACE_ME` placeholders that `install.sh` fills with `openssl rand` output on first run.

```yaml title="examples/kubernetes/20-mcp-test-secret.yaml"
# Application secrets for mcp-test:
#   - DATABASE_URL: full Postgres connection string. Built from the
#     postgres credentials so this manifest is self-contained for
#     installs that bundle Postgres in-cluster (the default). For an
#     external Postgres, replace this with your own URL.
#   - COOKIE_SECRET: HMAC key for the portal's session cookie. ANY
#     32+ random bytes; rotating invalidates all live sessions.
#   - DEV_KEY: bootstrap API key. Sent as `X-API-Key: <value>` to MCP
#     and admin endpoints; matches the `api_keys.file` entry in the
#     ConfigMap.
#
# install.sh fills the empty fields below with `openssl rand` output
# on a fresh install. The DATABASE_URL also references the postgres
# password; the script regenerates the URL whenever it touches the
# postgres secret to keep them in sync.
apiVersion: v1
kind: Secret
metadata:
  name: mcp-test
  namespace: mcp-test
  labels:
    app: mcp-test
    app.kubernetes.io/name: mcp-test
    app.kubernetes.io/component: server
    app.kubernetes.io/part-of: mcp-test
type: Opaque
stringData:
  # postgres://USER:PASSWORD@HOST:5432/DB?sslmode=disable
  DATABASE_URL: "REPLACE_ME_DATABASE_URL"
  # 32 bytes base64 (e.g. `openssl rand -base64 32`)
  COOKIE_SECRET: "REPLACE_ME_COOKIE_SECRET"
  # API key sent as `X-API-Key`. Use a long random value or the
  # mcptest_-prefixed format the dev tooling produces.
  DEV_KEY: "REPLACE_ME_DEV_API_KEY"
```

### mcp-test ConfigMap

The full `mcp-test.yaml` runtime config. Defaults: API-key auth (OIDC commented out), audit on with payloads + headers + 30-day retention, all four toolkits enabled. The container reads `${MCPTEST_DATABASE_URL}`, `${MCPTEST_COOKIE_SECRET}`, and `${MCPTEST_DEV_KEY}` via env interpolation; those env vars come from the Secret above.

REPLACE: `server.base_url` if your ingress host isn't `mcp-test-server.example.com`.

```yaml title="examples/kubernetes/25-mcp-test-configmap.yaml"
# mcp-test runtime config. The container reads this from
# /etc/mcp-test/mcp-test.yaml; see 40-mcp-test-deployment.yaml.
#
# Defaults below ship a working installation with:
#   - API-key auth only (OIDC disabled). Operators authenticate by
#     pasting the dev key on the portal login screen, and MCP clients
#     send `X-API-Key: <DEV_KEY>`. To turn on OIDC instead, set
#     `oidc.enabled: true` and supply issuer + client credentials via
#     additional secret keys + env mappings in the Deployment.
#   - Audit log fully on, payloads + headers captured, 30-day retention.
#   - Portal at /portal/, MCP at /, behind the same Ingress.
apiVersion: v1
kind: ConfigMap
metadata:
  name: mcp-test
  namespace: mcp-test
  labels:
    app: mcp-test
    app.kubernetes.io/name: mcp-test
    app.kubernetes.io/component: server
    app.kubernetes.io/part-of: mcp-test
data:
  mcp-test.yaml: |
    server:
      name: mcp-test
      address: ":8080"
      # REPLACE: must match the public URL clients reach this server at.
      # Used in OIDC redirects (when enabled) and RFC 9728 metadata.
      base_url: "https://mcp-test-server.example.com"
      read_header_timeout: 10s
      shutdown:
        grace_period: 25s
        pre_shutdown_delay: 2s
      tls:
        enabled: false
      streamable:
        session_timeout: 30m
        stateless: false
        json_response: false

    oidc:
      enabled: false
      # When you flip enabled to true, supply these via env from a
      # secret you add yourself. The defaults below match the dev
      # Keycloak; replace with your real issuer.
      # issuer: "https://your-idp.example.com/realms/mcp-test"
      # audience: "mcp-test"
      # client_id: "${MCPTEST_OIDC_CLIENT_ID}"
      # client_secret: "${MCPTEST_OIDC_CLIENT_SECRET}"

    api_keys:
      file:
        - name: bootstrap
          key: "${MCPTEST_DEV_KEY}"
          description: "Bootstrap API key for portal admin and MCP smoke tests"
      db:
        enabled: true

    auth:
      allow_anonymous: false
      require_for_mcp: true
      require_for_portal: true

    database:
      url: "${MCPTEST_DATABASE_URL}"
      max_open_conns: 25
      max_idle_conns: 5
      conn_max_lifetime: 1h

    audit:
      enabled: true
      capture_payloads: true
      capture_headers: true
      max_payload_bytes: 65536
      retention_days: 30
      redact_keys: [password, token, secret, authorization, api_key, credentials, cookie]

    portal:
      enabled: true
      cookie_name: mcp_test_session
      cookie_secret: "${MCPTEST_COOKIE_SECRET}"
      # cookie_secure must be false when serving over plain HTTP locally;
      # leave true for the Ingress + cert-manager flow this example uses.
      cookie_secure: true
      oidc_redirect_path: /portal/auth/callback

    tools:
      identity:  { enabled: true }
      data:      { enabled: true }
      failure:   { enabled: true }
      streaming: { enabled: true }
```

### mcp-test Service

ClusterIP, port 8080. The Ingress points at this Service.

```yaml title="examples/kubernetes/30-mcp-test-service.yaml"
apiVersion: v1
kind: Service
metadata:
  name: mcp-test
  namespace: mcp-test
  labels:
    app: mcp-test
    app.kubernetes.io/name: mcp-test
    app.kubernetes.io/component: server
    app.kubernetes.io/part-of: mcp-test
spec:
  type: ClusterIP
  selector:
    app: mcp-test
  ports:
    - name: http
      protocol: TCP
      port: 8080
      targetPort: http
```

### mcp-test Deployment

Single replica (mcp-test is stateless once Postgres is reachable; bump for HA). Reads its config from `/etc/mcp-test/mcp-test.yaml` via the mounted ConfigMap; reads three env vars from the Secret. Tight `securityContext` (non-root, read-only root FS, all capabilities dropped). The `config-hash` / `secret-hash` annotations are placeholders that `install.sh` patches to a SHA256 of the on-disk file contents so a config change triggers a rollout automatically.

```yaml title="examples/kubernetes/40-mcp-test-deployment.yaml"
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-test
  namespace: mcp-test
  labels:
    app: mcp-test
    app.kubernetes.io/name: mcp-test
    app.kubernetes.io/component: server
    app.kubernetes.io/part-of: mcp-test
spec:
  replicas: 1
  revisionHistoryLimit: 3
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 0
      maxSurge: 1
  selector:
    matchLabels:
      app: mcp-test
  template:
    metadata:
      labels:
        app: mcp-test
        app.kubernetes.io/name: mcp-test
        app.kubernetes.io/component: server
        app.kubernetes.io/part-of: mcp-test
      annotations:
        # Force a rollout when ConfigMap or Secret content changes.
        # install.sh patches these to a SHA256 of the on-disk file
        # contents; replace manually with `kubectl rollout restart` if
        # you edit the manifests outside the script.
        config-hash: "0"
        secret-hash: "0"
    spec:
      terminationGracePeriodSeconds: 30
      containers:
        - name: mcp-test
          image: ghcr.io/plexara/mcp-test:v1.2.0
          imagePullPolicy: IfNotPresent
          args:
            - --config
            - /etc/mcp-test/mcp-test.yaml
          ports:
            - name: http
              containerPort: 8080
              protocol: TCP
          env:
            - name: MCPTEST_DATABASE_URL
              valueFrom:
                secretKeyRef:
                  name: mcp-test
                  key: DATABASE_URL
            - name: MCPTEST_COOKIE_SECRET
              valueFrom:
                secretKeyRef:
                  name: mcp-test
                  key: COOKIE_SECRET
            - name: MCPTEST_DEV_KEY
              valueFrom:
                secretKeyRef:
                  name: mcp-test
                  key: DEV_KEY
          volumeMounts:
            - name: config
              mountPath: /etc/mcp-test
              readOnly: true
          livenessProbe:
            httpGet:
              path: /healthz
              port: http
            initialDelaySeconds: 15
            periodSeconds: 30
            timeoutSeconds: 3
            failureThreshold: 3
          readinessProbe:
            httpGet:
              path: /readyz
              port: http
            initialDelaySeconds: 5
            periodSeconds: 10
            timeoutSeconds: 3
            failureThreshold: 3
          resources:
            requests:
              cpu: 50m
              memory: 128Mi
            limits:
              cpu: 500m
              memory: 512Mi
          securityContext:
            runAsNonRoot: true
            runAsUser: 65532
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop: ["ALL"]
      volumes:
        - name: config
          configMap:
            name: mcp-test
```

### mcp-test Ingress

cert-manager automation for TLS, CORS for browser-side MCP gateways, and the long-timeout / no-buffer nginx annotations the streamable HTTP transport needs (live tail and SSE responses can't tolerate proxy buffering).

REPLACE: `mcp-test-server.example.com` (3 occurrences) with your DNS name. `install.sh` rewrites them in-place when you set `INGRESS_HOST`.

```yaml title="examples/kubernetes/50-mcp-test-ingress.yaml"
# Ingress for mcp-test. Assumes:
#   - ingress-nginx as the controller (annotations are nginx-specific).
#     Adjust for traefik / aws-load-balancer-controller / etc.
#   - cert-manager with a `letsencrypt-production` ClusterIssuer for
#     automated TLS. Remove the `cert-manager.io/cluster-issuer`
#     annotation and the `tls:` block if you handle TLS at a different
#     layer (terminating LB, service mesh, etc.).
#
# REPLACE: `mcp-test-server.example.com` (3 occurrences below) with the
# DNS name your Ingress controller routes to this service.
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: mcp-test
  namespace: mcp-test
  labels:
    app: mcp-test
    app.kubernetes.io/name: mcp-test
    app.kubernetes.io/component: server
    app.kubernetes.io/part-of: mcp-test
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-production
    # CORS: lets a browser-side MCP gateway call this server. Tighten
    # the allow-origin list to the gateway hosts you actually use.
    nginx.ingress.kubernetes.io/enable-cors: "true"
    nginx.ingress.kubernetes.io/cors-allow-methods: "GET, POST, DELETE, OPTIONS"
    nginx.ingress.kubernetes.io/cors-allow-origin: "*"
    nginx.ingress.kubernetes.io/cors-allow-credentials: "true"
    # Streamable HTTP / SSE: long-lived connections for live tail and
    # MCP server-initiated messages. Disable buffering so the client
    # sees frames as they're written.
    nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
    nginx.ingress.kubernetes.io/proxy-buffering: "off"
    nginx.ingress.kubernetes.io/proxy-buffer-size: "8k"
    nginx.ingress.kubernetes.io/service-upstream: "true"
spec:
  ingressClassName: nginx
  rules:
    - host: mcp-test-server.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: mcp-test
                port:
                  number: 8080
  tls:
    - hosts:
        - mcp-test-server.example.com
      secretName: mcp-test-tls
```

## Install

```sh
git clone https://github.com/plexara/mcp-test.git
cd mcp-test/examples/kubernetes

INGRESS_HOST=mcp.example.com ./install.sh
```

The full source of [`install.sh`](https://github.com/plexara/mcp-test/blob/main/examples/kubernetes/install.sh) is in the repo. The two interesting blocks are the placeholder-fill (generates secrets in-place) and the apply loop (gates on Postgres readiness):

```bash title="examples/kubernetes/install.sh (excerpt: placeholder fill)"
# Postgres password
if grep -q 'REPLACE_ME_POSTGRES_PASSWORD' "$PG_SECRET"; then
  pgpw="$(openssl rand -base64 24 | tr -d '\n=+/' | head -c 28)"
  sed -i.bak "s|REPLACE_ME_POSTGRES_PASSWORD|${pgpw}|" "$PG_SECRET" && rm -f "$PG_SECRET.bak"
fi

# Application secrets
if grep -q 'REPLACE_ME_COOKIE_SECRET' "$APP_SECRET"; then
  cookie="$(openssl rand -base64 32 | tr -d '\n')"
  sed -i.bak "s|REPLACE_ME_COOKIE_SECRET|${cookie}|" "$APP_SECRET" && rm -f "$APP_SECRET.bak"
fi
if grep -q 'REPLACE_ME_DEV_API_KEY' "$APP_SECRET"; then
  devkey="mcptest_$(openssl rand -base64 24 | tr -d '\n=+/' | head -c 32)"
  sed -i.bak "s|REPLACE_ME_DEV_API_KEY|${devkey}|" "$APP_SECRET" && rm -f "$APP_SECRET.bak"
fi
if grep -q 'REPLACE_ME_DATABASE_URL' "$APP_SECRET"; then
  dburl="postgres://mcp:${pgpw}@postgres:5432/mcp_test?sslmode=disable"
  sed -i.bak "s|REPLACE_ME_DATABASE_URL|${dburl}|" "$APP_SECRET" && rm -f "$APP_SECRET.bak"
fi
```

```bash title="examples/kubernetes/install.sh (excerpt: apply loop with readiness gates)"
for f in "${MANIFESTS[@]}"; do
  case "$f" in
    14-postgres-statefulset.yaml)
      step "$i" "applying $f"
      apply "$SCRIPT_DIR/$f"
      step "$i" "waiting for Postgres pod ready..."
      kubectl -n "$NAMESPACE" wait --for=condition=Ready pod/postgres-0 \
          --timeout=180s
      ;;
    40-mcp-test-deployment.yaml)
      step "$i" "applying $f (config-hash=${config_hash}, secret-hash=${secret_hash})"
      sed -e "s|config-hash: \"0\"|config-hash: \"${config_hash}\"|" \
          -e "s|secret-hash: \"0\"|secret-hash: \"${secret_hash}\"|" \
          "$SCRIPT_DIR/$f" | kubectl apply -f -
      step "$i" "waiting for mcp-test rollout..."
      kubectl -n "$NAMESPACE" rollout status deploy/mcp-test --timeout=180s
      ;;
    *)
      step "$i" "applying $f"
      apply "$SCRIPT_DIR/$f"
      ;;
  esac
done
```

The installer:

1. Confirms the current `kubectl` context interactively before applying.
2. Generates the postgres password, portal cookie secret, and dev API key in-place where the manifests still hold `REPLACE_ME` placeholders.
3. Rewrites `mcp-test-server.example.com` to `$INGRESS_HOST` in `50-mcp-test-ingress.yaml`.
4. Applies manifests in numeric order with `[N/M]` progress.
5. Waits for `pod/postgres-0` to be `Ready` (180s) before applying the mcp-test side.
6. Waits for the mcp-test Deployment rollout (180s).
7. Patches a `config-hash` / `secret-hash` annotation on the Deployment so a re-run after a config edit triggers a new rollout without `kubectl rollout restart`.
8. Prints the URLs and the dev API key.

Variables it honors:

<div class="config-keys" markdown>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">INGRESS_HOST</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">default</span><span class="config-key__chip-value">prompts</span></span>
<span class="config-key__chip"><span class="config-key__chip-label">example</span><code class="config-key__chip-value">mcp.example.com</code></span>
</div>
</div>
<div class="config-key__body" markdown>
DNS name your ingress controller routes to mcp-test. Rewrites `50-mcp-test-ingress.yaml` in place. Also update `server.base_url` in `25-mcp-test-configmap.yaml` to match.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">KUBE_CONTEXT</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">default</span><span class="config-key__chip-value">current</span></span>
<span class="config-key__chip"><span class="config-key__chip-label">example</span><code class="config-key__chip-value">staging</code></span>
</div>
</div>
<div class="config-key__body" markdown>
kubectl context to install into. The installer confirms it interactively before applying.
</div>
</div>

<div class="config-key" markdown>
<div class="config-key__head">
<code class="config-key__name">--dry-run</code>
<div class="config-key__chips">
<span class="config-key__chip"><span class="config-key__chip-label">flag</span><span class="config-key__chip-value">off</span></span>
</div>
</div>
<div class="config-key__body" markdown>
Renders manifests, doesn't apply. Useful for previewing the placeholders the installer would fill.
</div>
</div>

</div>

## Smoke test

Once the install finishes:

```sh
HOST="https://${INGRESS_HOST}"
KEY="<value printed by install.sh>"

# Liveness
curl -i "${HOST}/healthz"

# Identity through the portal API
curl -i -H "X-API-Key: ${KEY}" "${HOST}/api/v1/portal/me"

# Initialize an MCP session, then call a tool.
curl -i -X POST "${HOST}/" \
  -H "X-API-Key: ${KEY}" \
  -H "Accept: application/json, text/event-stream" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","clientInfo":{"name":"smoketest","version":"0.1"},"capabilities":{}}}'
```

Then open `https://${INGRESS_HOST}/portal/` and paste the same `X-API-Key` value into the login screen.

## Variations

### External Postgres

Drop `10-postgres-service.yaml`, `12-postgres-secret.yaml`, and `14-postgres-statefulset.yaml`. Replace the `DATABASE_URL` field in `20-mcp-test-secret.yaml` with your own connection string. If you use `sslmode=verify-full`, make sure the mcp-test pod trusts your CA chain (volume-mount it into `/etc/ssl/certs/` and set `SSL_CERT_DIR`).

### OIDC instead of API keys

In `25-mcp-test-configmap.yaml`, set `oidc.enabled: true` and fill `oidc.issuer` / `oidc.audience`. Add an `MCPTEST_OIDC_CLIENT_ID` / `MCPTEST_OIDC_CLIENT_SECRET` env mapping in `40-mcp-test-deployment.yaml` from a secret you create. The full identity model is documented in [Authentication](../configuration/auth.md).

### Different ingress controller

The annotations in `50-mcp-test-ingress.yaml` are nginx-specific. For traefik, aws-load-balancer-controller, or GKE ingress, swap them for the equivalent timeout / CORS / buffer-disable annotations and update `ingressClassName` to match.

### Plain HTTP for local clusters

Drop the `tls:` block in `50-mcp-test-ingress.yaml` and set `portal.cookie_secure: false` in the ConfigMap. Cookies won't survive a session over plain HTTP otherwise.

### Higher replicas

mcp-test is stateless once Postgres is reachable. Bump `replicas` on the Deployment; the StatefulSet stays at 1. The audit log uses Postgres advisory locks for the serial migration step on startup, so concurrent rollouts converge correctly.

## Uninstall

```sh
kubectl delete namespace mcp-test
```

The PVC bound by the Postgres StatefulSet is in the namespace and goes with it. Preserve audit data across reinstalls by adding a finalizer to the PVC before deleting the namespace, or by switching to an external Postgres.
