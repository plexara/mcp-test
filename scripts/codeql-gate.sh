#!/usr/bin/env bash
#
# codeql-gate.sh — fail if a CodeQL SARIF result has any findings that
# aren't excluded by the project config.
#
# Args:
#   $1  path to SARIF file
#   $2  path to codeql-config.yml (used to read query-filters.exclude.id)
#
# Exit 0 = clean. Exit 1 = at least one finding survives the filters.

set -euo pipefail

SARIF="${1:-}"
CONFIG="${2:-}"

if [[ -z "$SARIF" || ! -f "$SARIF" ]]; then
  echo "codeql-gate: missing SARIF input" >&2
  exit 1
fi

# Build the exclude list from codeql-config.yml.
EXCLUDES=()
if [[ -n "$CONFIG" && -f "$CONFIG" ]]; then
  while IFS= read -r line; do
    [[ -n "$line" ]] && EXCLUDES+=("$line")
  done < <(awk '
    /^query-filters:/ { in_qf=1; next }
    in_qf && /^[^ ]/  { in_qf=0 }
    in_qf && /^[[:space:]]*-[[:space:]]*exclude:/ { in_excl=1; next }
    in_qf && in_excl && /^[[:space:]]*id:/ {
      sub(/^[[:space:]]*id:[[:space:]]*/, "")
      sub(/[[:space:]]+#.*$/, "")
      gsub(/[\047"]/, "")
      print
      in_excl=0
    }
  ' "$CONFIG")
fi

# Pull every result's ruleId from SARIF. Use a while-read loop instead
# of `mapfile`/`readarray` so we work on macOS bash 3.2 too.
RULES=()
while IFS= read -r line; do
  [[ -n "$line" ]] && RULES+=("$line")
done < <(jq -r '.runs[]?.results[]?.ruleId // empty' "$SARIF")

KEPT=()
for r in "${RULES[@]+"${RULES[@]}"}"; do
  excluded=0
  for e in "${EXCLUDES[@]+"${EXCLUDES[@]}"}"; do
    if [[ "$r" == "$e" ]]; then
      excluded=1
      break
    fi
  done
  [[ $excluded -eq 0 ]] && KEPT+=("$r")
done

if [[ ${#KEPT[@]} -eq 0 ]]; then
  exit 0
fi

echo "codeql-gate: ${#KEPT[@]} findings after exclusions:" >&2
for r in "${KEPT[@]+"${KEPT[@]}"}"; do
  echo "  - $r" >&2
done
echo "" >&2
echo "Inspect details with:" >&2
echo "  jq '.runs[].results[] | select(.ruleId == \"<id>\") | {ruleId, locations}' $SARIF" >&2
exit 1
