#!/usr/bin/env bash
# coverage-gate.sh — fail if total coverage of testable packages is below MIN.
#
# Why this exists: a few packages (pkg/apikeys, pkg/audit/postgres,
# pkg/database, pkg/database/migrate) require a live Postgres to test
# meaningfully. cmd/mcp-test is a tiny entry point that's covered by the
# integration suite when Docker is available. We exclude these from the local
# gate so `make verify` is runnable on a developer's laptop without Docker.
#
# CI sets RUN_INTEGRATION=1 and runs the testcontainers suite separately,
# at which point those packages are also exercised.
#
# Usage:
#   scripts/coverage-gate.sh [coverage.out [min_percent]]
#
# Exit 0 if total >= MIN, 1 otherwise. Prints per-package and total numbers.

set -euo pipefail

PROFILE="${1:-coverage.out}"
MIN="${2:-80}"

if [[ ! -f "$PROFILE" ]]; then
  echo "coverage profile not found: $PROFILE" >&2
  exit 2
fi

EXCLUDE_PACKAGES=(
  "github.com/plexara/mcp-test/cmd/"
  "github.com/plexara/mcp-test/pkg/apikeys/"
  "github.com/plexara/mcp-test/pkg/audit/postgres/"
  "github.com/plexara/mcp-test/pkg/database/"
  "github.com/plexara/mcp-test/pkg/database/migrate/"
)

# Build a grep pattern that drops profile entries from excluded packages.
EXCLUDE_RE=$(printf "%s|" "${EXCLUDE_PACKAGES[@]}")
EXCLUDE_RE=${EXCLUDE_RE%|}

FILTERED=$(mktemp)
trap 'rm -f "$FILTERED"' EXIT

# Keep the mode line and any line whose path doesn't match an excluded prefix.
{ head -n 1 "$PROFILE"
  tail -n +2 "$PROFILE" | grep -Ev "^($EXCLUDE_RE)" || true
} > "$FILTERED"

# Per-package summary (sourced from filtered profile).
echo "=== coverage by package (excluding postgres-dependent and entry packages) ==="
go tool cover -func="$FILTERED" | awk '
  /^total:/ { next }
  {
    sub(/^github.com\/plexara\/mcp-test\//, "", $1)
    split($1, p, "/")
    pkg = ""
    for (i = 1; i < length(p); i++) pkg = (pkg == "" ? p[i] : pkg "/" p[i])
    if (pkg == "") pkg = p[1]
    gsub(/%/, "", $3)
    s[pkg] += $3; n[pkg]++
  }
  END {
    for (pp in s) printf "  %-32s %5.1f%%  (%d funcs)\n", pp, s[pp]/n[pp], n[pp]
  }
' | sort -k2 -n

# Total over filtered profile.
TOTAL=$(go tool cover -func="$FILTERED" | awk '/^total:/ { gsub(/%/, "", $3); print $3 }')
echo ""
echo "=== filtered total: ${TOTAL}% (gate: >=${MIN}%) ==="

awk -v t="$TOTAL" -v m="$MIN" 'BEGIN { exit !(t+0 >= m+0) }'
RC=$?
if [[ $RC -ne 0 ]]; then
  echo "FAIL: total coverage ${TOTAL}% is below the required ${MIN}%" >&2
  exit 1
fi
echo "OK: coverage gate passed."
