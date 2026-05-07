#!/usr/bin/env bash
#
# install.sh: apply the mcp-test example to a Kubernetes cluster with
# progress output. Idempotent: re-running safely re-applies every
# manifest and patches a fresh config-hash / secret-hash so a config
# change rolls the Deployment.
#
# What it does:
#   1. Sanity checks (kubectl reachable, current context confirmed).
#   2. Generates secrets in-place if the manifests still hold REPLACE_ME
#      placeholders (postgres password, mcp-test cookie secret, dev API
#      key, and the full DATABASE_URL built from the postgres password).
#   3. Replaces `mcp-test-server.example.com` with $INGRESS_HOST in the
#      Ingress manifest.
#   4. Applies the manifests in numeric order with [N/M] progress.
#   5. Waits for Postgres readiness before applying mcp-test, then waits
#      for the Deployment rollout.
#   6. Prints next steps with the URLs and the dev API key.
#
# Usage:
#   ./install.sh                         # interactive: confirms context, prompts for INGRESS_HOST
#   INGRESS_HOST=mcp.example.com ./install.sh
#   INGRESS_HOST=mcp.example.com KUBE_CONTEXT=staging ./install.sh
#   ./install.sh --dry-run               # render manifests, don't apply
#
# This script is intentionally simple bash; no helm, no kustomize. The
# manifests are valid `kubectl apply -f` input on their own if you'd
# rather wire them into your existing GitOps tool.

set -euo pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
NAMESPACE="mcp-test"
DRY_RUN="false"

# ---------------------------------------------------------------------
# tiny progress helpers
# ---------------------------------------------------------------------

c_reset='\033[0m'; c_bold='\033[1m'; c_dim='\033[2m'
c_ok='\033[32m'; c_warn='\033[33m'; c_err='\033[31m'; c_step='\033[36m'

step()    { printf "${c_step}[%d/%d]${c_reset} %b\n" "$1" "$TOTAL_STEPS" "$2"; }
ok()      { printf "      ${c_ok}✓${c_reset} %b\n" "$1"; }
warn()    { printf "      ${c_warn}!${c_reset} %b\n" "$1"; }
fail()    { printf "      ${c_err}✗${c_reset} %b\n" "$1" >&2; exit 1; }
hdr()     { printf "\n${c_bold}%s${c_reset}\n" "$1"; }
muted()   { printf "${c_dim}%s${c_reset}\n" "$1"; }

for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN="true" ;;
    -h|--help)
      sed -n '/^# install.sh/,/^# This script is/p' "$0" | sed 's/^# //; s/^#//'
      exit 0 ;;
  esac
done

# ---------------------------------------------------------------------
# preflight
# ---------------------------------------------------------------------

hdr "mcp-test Kubernetes installer"

command -v kubectl >/dev/null || fail "kubectl is not on PATH"
command -v openssl >/dev/null || fail "openssl is not on PATH (used for secret generation)"

if [[ -n "${KUBE_CONTEXT:-}" ]]; then
  KCTX_ARGS=(--context "$KUBE_CONTEXT")
else
  KCTX_ARGS=()
fi
KCTX="$(kubectl "${KCTX_ARGS[@]}" config current-context 2>/dev/null || true)"
[[ -n "$KCTX" ]] || fail "could not determine current kubectl context (try setting KUBE_CONTEXT=...)"

printf "  context : %s\n" "$KCTX"
printf "  namespace: %s\n" "$NAMESPACE"

if [[ "$DRY_RUN" != "true" ]]; then
  read -r -p "  apply to this context? [y/N] " confirm
  [[ "$confirm" =~ ^[Yy]$ ]] || fail "aborted"
fi

# ---------------------------------------------------------------------
# placeholder fill
# ---------------------------------------------------------------------

PG_SECRET="$SCRIPT_DIR/12-postgres-secret.yaml"
APP_SECRET="$SCRIPT_DIR/20-mcp-test-secret.yaml"
INGRESS="$SCRIPT_DIR/50-mcp-test-ingress.yaml"

# Postgres password
if grep -q 'REPLACE_ME_POSTGRES_PASSWORD' "$PG_SECRET"; then
  pgpw="$(openssl rand -base64 24 | tr -d '\n=+/' | head -c 28)"
  sed -i.bak "s|REPLACE_ME_POSTGRES_PASSWORD|${pgpw}|" "$PG_SECRET" && rm -f "$PG_SECRET.bak"
  ok "generated POSTGRES_PASSWORD in 12-postgres-secret.yaml"
else
  pgpw="$(awk '/POSTGRES_PASSWORD:/ {gsub(/"/,"",$2); print $2; exit}' "$PG_SECRET")"
  ok "POSTGRES_PASSWORD already set"
fi

# Application secrets
if grep -q 'REPLACE_ME_COOKIE_SECRET' "$APP_SECRET"; then
  cookie="$(openssl rand -base64 32 | tr -d '\n')"
  sed -i.bak "s|REPLACE_ME_COOKIE_SECRET|${cookie}|" "$APP_SECRET" && rm -f "$APP_SECRET.bak"
  ok "generated COOKIE_SECRET in 20-mcp-test-secret.yaml"
fi
if grep -q 'REPLACE_ME_DEV_API_KEY' "$APP_SECRET"; then
  devkey="mcptest_$(openssl rand -base64 24 | tr -d '\n=+/' | head -c 32)"
  sed -i.bak "s|REPLACE_ME_DEV_API_KEY|${devkey}|" "$APP_SECRET" && rm -f "$APP_SECRET.bak"
  ok "generated DEV_KEY in 20-mcp-test-secret.yaml"
else
  devkey="$(awk '/DEV_KEY:/ {gsub(/"/,"",$2); print $2; exit}' "$APP_SECRET")"
fi
if grep -q 'REPLACE_ME_DATABASE_URL' "$APP_SECRET"; then
  dburl="postgres://mcp:${pgpw}@postgres:5432/mcp_test?sslmode=disable"
  # `|` is the sed delim so we don't fight the / in the URL.
  sed -i.bak "s|REPLACE_ME_DATABASE_URL|${dburl}|" "$APP_SECRET" && rm -f "$APP_SECRET.bak"
  ok "wrote DATABASE_URL in 20-mcp-test-secret.yaml"
fi

# Ingress host
if grep -q 'mcp-test-server.example.com' "$INGRESS"; then
  if [[ -z "${INGRESS_HOST:-}" ]]; then
    if [[ "$DRY_RUN" == "true" ]]; then
      INGRESS_HOST="mcp-test-server.example.com"
      warn "INGRESS_HOST not set; --dry-run keeps the placeholder"
    else
      read -r -p "  ingress host (e.g. mcp.example.com): " INGRESS_HOST
      [[ -n "$INGRESS_HOST" ]] || fail "INGRESS_HOST required"
    fi
  fi
  if [[ "$INGRESS_HOST" != "mcp-test-server.example.com" ]]; then
    sed -i.bak "s|mcp-test-server.example.com|${INGRESS_HOST}|g" "$INGRESS" && rm -f "$INGRESS.bak"
    ok "set ingress host to ${INGRESS_HOST}"
  fi
fi

# Compute hashes for the rollout-on-config-change annotations.
config_hash="$(shasum -a 256 "$SCRIPT_DIR/25-mcp-test-configmap.yaml" | awk '{print substr($1,1,12)}')"
secret_hash="$(shasum -a 256 "$APP_SECRET" | awk '{print substr($1,1,12)}')"

# ---------------------------------------------------------------------
# apply
# ---------------------------------------------------------------------

# Order matters: namespace first, then postgres up + ready, then
# mcp-test secrets/configmap, then deployment, then ingress.
MANIFESTS=(
  "00-namespace.yaml"
  "10-postgres-service.yaml"
  "12-postgres-secret.yaml"
  "14-postgres-statefulset.yaml"
  "20-mcp-test-secret.yaml"
  "25-mcp-test-configmap.yaml"
  "30-mcp-test-service.yaml"
  "40-mcp-test-deployment.yaml"
  "50-mcp-test-ingress.yaml"
)
TOTAL_STEPS=$(( ${#MANIFESTS[@]} + 2 ))  # +2 for the postgres-ready and rollout waits

apply() {
  if [[ "$DRY_RUN" == "true" ]]; then
    kubectl "${KCTX_ARGS[@]}" apply --dry-run=client -f "$1" >/dev/null
  else
    kubectl "${KCTX_ARGS[@]}" apply -f "$1" >/dev/null
  fi
}

i=0
for f in "${MANIFESTS[@]}"; do
  i=$(( i + 1 ))
  case "$f" in
    14-postgres-statefulset.yaml)
      step "$i" "applying ${c_bold}$f${c_reset}"
      apply "$SCRIPT_DIR/$f"
      ok "Postgres StatefulSet applied"
      i=$(( i + 1 ))
      step "$i" "waiting for Postgres pod ready..."
      if [[ "$DRY_RUN" != "true" ]]; then
        kubectl "${KCTX_ARGS[@]}" -n "$NAMESPACE" wait --for=condition=Ready pod/postgres-0 \
          --timeout=180s >/dev/null 2>&1 || fail "postgres-0 did not become Ready in 180s"
        ok "postgres-0 Ready"
      else
        muted "      (skipped in dry-run)"
      fi
      ;;
    40-mcp-test-deployment.yaml)
      # Patch the rollout-on-content-change annotations so a config or
      # secret edit takes effect on the next install.sh.
      step "$i" "applying ${c_bold}$f${c_reset} (config-hash=${config_hash}, secret-hash=${secret_hash})"
      tmp="$(mktemp)"
      sed -e "s|config-hash: \"0\"|config-hash: \"${config_hash}\"|" \
          -e "s|secret-hash: \"0\"|secret-hash: \"${secret_hash}\"|" \
          "$SCRIPT_DIR/$f" > "$tmp"
      apply "$tmp"
      rm -f "$tmp"
      ok "mcp-test Deployment applied"
      i=$(( i + 1 ))
      step "$i" "waiting for mcp-test rollout..."
      if [[ "$DRY_RUN" != "true" ]]; then
        kubectl "${KCTX_ARGS[@]}" -n "$NAMESPACE" rollout status deploy/mcp-test \
          --timeout=180s >/dev/null || fail "mcp-test rollout did not complete"
        ok "mcp-test Ready"
      else
        muted "      (skipped in dry-run)"
      fi
      ;;
    *)
      step "$i" "applying ${c_bold}$f${c_reset}"
      apply "$SCRIPT_DIR/$f"
      ok "applied"
      ;;
  esac
done

# ---------------------------------------------------------------------
# done
# ---------------------------------------------------------------------

hdr "Installed."

if [[ "$DRY_RUN" == "true" ]]; then
  muted "(dry-run only; no resources were created)"
  exit 0
fi

host="${INGRESS_HOST:-mcp-test-server.example.com}"

cat <<EOF
  Portal       https://${host}/portal/
  MCP endpoint https://${host}/
  Discovery    https://${host}/.well-known/oauth-protected-resource

  X-API-Key    ${devkey}

Smoke test (replace TOKEN_OR_KEY appropriately):

  curl -i https://${host}/healthz
  curl -i -H "X-API-Key: ${devkey}" \\
      https://${host}/api/v1/portal/me

To roll out a config change:
  edit 25-mcp-test-configmap.yaml or 20-mcp-test-secret.yaml, re-run ./install.sh

To uninstall:
  kubectl delete namespace ${NAMESPACE}
EOF
