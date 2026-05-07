# Kubernetes example

A self-contained installation of `mcp-test` and a single-replica Postgres on any Kubernetes cluster with an `nginx` ingress controller and a working `cert-manager` `letsencrypt-production` ClusterIssuer. One file per resource, numeric ordering, applied with `install.sh` (or any GitOps tool that consumes plain `kubectl apply -f` input).

## What lands

| File | Resource |
| --- | --- |
| `00-namespace.yaml`         | `Namespace mcp-test` |
| `10-postgres-service.yaml`  | `Service postgres` (headless ClusterIP) |
| `12-postgres-secret.yaml`   | `Secret postgres` (DB / user / password) |
| `14-postgres-statefulset.yaml` | `StatefulSet postgres` (single replica, 5Gi PVC, `postgres:17-alpine`) |
| `20-mcp-test-secret.yaml`   | `Secret mcp-test` (`DATABASE_URL` / `COOKIE_SECRET` / `DEV_KEY`) |
| `25-mcp-test-configmap.yaml`| `ConfigMap mcp-test` (full `mcp-test.yaml`; OIDC off, API-key auth, audit on) |
| `30-mcp-test-service.yaml`  | `Service mcp-test` (ClusterIP, port 8080) |
| `40-mcp-test-deployment.yaml`| `Deployment mcp-test` (`ghcr.io/plexara/mcp-test:v1.2.0`) |
| `50-mcp-test-ingress.yaml`  | `Ingress mcp-test` (TLS via cert-manager, CORS for browser MCP gateways) |

`install.sh` wires the secrets to each other (the postgres password is reused inside the `DATABASE_URL`) and patches a `config-hash` / `secret-hash` annotation on the Deployment so a re-run after a config edit rolls the pod automatically.

## Placeholders

Three things you replace before this is yours:

1. **Ingress host.** `mcp-test-server.example.com` appears once in `50-mcp-test-ingress.yaml` (and in `25-mcp-test-configmap.yaml` as `server.base_url`). `install.sh` will prompt for `INGRESS_HOST` if you haven't set it; it rewrites the ingress in-place. Update `base_url` in the ConfigMap by hand if you change it.
2. **Postgres password.** `REPLACE_ME_POSTGRES_PASSWORD` in `12-postgres-secret.yaml`. `install.sh` writes a random 28-char value on first run.
3. **Application secrets.** `REPLACE_ME_COOKIE_SECRET`, `REPLACE_ME_DEV_API_KEY`, `REPLACE_ME_DATABASE_URL` in `20-mcp-test-secret.yaml`. `install.sh` generates and prints the dev API key on first run.

Everything else is functional defaults.

## Install

```sh
# from this directory
./install.sh
```

The script will:

1. Confirm the current `kubectl` context.
2. Generate the secret values in-place where they're still placeholders.
3. Apply manifests in numeric order with `[N/M]` progress.
4. Wait for `pod/postgres-0` to be Ready before applying the mcp-test side.
5. Wait for the mcp-test Deployment rollout.
6. Print the URLs and the dev API key.

Variables it honors:

| Var | Default | Purpose |
| --- | --- | --- |
| `INGRESS_HOST` | prompts | DNS name your ingress controller routes to mcp-test (e.g. `mcp.example.com`). |
| `KUBE_CONTEXT` | current | kubectl context to install into. |
| `--dry-run` | off | Renders manifests, doesn't apply. |

## Smoke test

Once the install finishes:

```sh
HOST="https://${INGRESS_HOST:-mcp-test-server.example.com}"
KEY="<value printed by install.sh>"

curl -i "${HOST}/healthz"

curl -i -H "X-API-Key: ${KEY}" \
  "${HOST}/api/v1/portal/me"

# Initialize the MCP session, then call a tool.
curl -i -X POST "${HOST}/" \
  -H "X-API-Key: ${KEY}" \
  -H "Accept: application/json, text/event-stream" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","clientInfo":{"name":"smoketest","version":"0.1"},"capabilities":{}}}'
```

Then open the portal at `https://${INGRESS_HOST}/portal/` and paste the same `X-API-Key` value into the login screen.

## Variations

**External Postgres.** Drop `10-postgres-service.yaml`, `12-postgres-secret.yaml`, `14-postgres-statefulset.yaml`, and replace the `DATABASE_URL` field in `20-mcp-test-secret.yaml` with your own connection string. Make sure your CA chain is trusted by the mcp-test pod if you use `sslmode=verify-full`.

**OIDC instead of API keys.** In `25-mcp-test-configmap.yaml`, set `oidc.enabled: true`, fill `oidc.issuer` and `oidc.audience`, and add an `MCPTEST_OIDC_CLIENT_ID` / `MCPTEST_OIDC_CLIENT_SECRET` env mapping in the Deployment from a secret you create. The `mcp-test` repo's OIDC docs at `docs/configuration/auth.md` walk the full flow.

**Different ingress controller.** The annotations in `50-mcp-test-ingress.yaml` are nginx-specific. For traefik / aws-load-balancer-controller / gke ingress, swap them for the equivalent timeout / CORS / buffer-disable annotations. The `ingressClassName` line also needs to match.

**Plain HTTP for local clusters.** Drop the `tls:` block in the ingress and set `portal.cookie_secure: false` in the ConfigMap. Cookies won't survive a session otherwise.

**Higher replicas.** mcp-test is stateless once Postgres is reachable. Bump `replicas` on the Deployment; the StatefulSet stays at 1. The audit log uses Postgres advisory locks for the serial migration step on startup, so concurrent rollouts converge correctly.

## Uninstall

```sh
kubectl delete namespace mcp-test
```

The PVC bound by the Postgres StatefulSet is in the namespace and goes with it. To preserve audit data across reinstalls, change `volumeClaimTemplates.metadata.annotations` to add `helm.sh/resource-policy: keep` or apply a finalizer; out of scope for this example.
