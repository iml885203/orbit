#!/bin/sh
# No team brands, internal project names, service codenames, or private
# infrastructure identifiers may enter the public source tree.
set -eu
cd "$(dirname "$0")/.."

patterns=${ORBIT_PRIVATE_IDENTIFIERS_PATTERN:-}
if [ -z "$patterns" ]; then
  echo "neutral-source gate skipped (ORBIT_PRIVATE_IDENTIFIERS_PATTERN is not set)"
  exit 0
fi
violations=$(
  rg -n -i --hidden \
    --glob '!.git/**' \
    --glob '!ui/node_modules/**' \
    --glob '!scripts/check-neutral.sh' \
    "$patterns" . \
    | grep -E -i "$patterns" \
    || true
)

if [ -n "$violations" ]; then
  echo "neutral-source gate FAILED — private identifiers remain:"
  echo "$violations"
  exit 1
fi
echo "neutral-source gate OK"
