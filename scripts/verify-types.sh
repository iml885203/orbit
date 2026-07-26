#!/bin/sh
# Verify that running tygo does not change the generated TypeScript surface.
# Comparing the before/after diff keeps this useful in a dirty development
# worktree while retaining the same behavior on a clean CI checkout.
set -eu
cd "$(dirname "$0")/.."

before=$(mktemp)
after=$(mktemp)
trap 'rm -f "$before" "$after"' EXIT

git diff -- ui/src/lib/types.gen.ts ui/src/lib/types/ >"$before"
go run github.com/gzuidhof/tygo@v0.2.21 generate
git diff -- ui/src/lib/types.gen.ts ui/src/lib/types/ >"$after"

if ! cmp -s "$before" "$after"; then
  echo "generated TypeScript types are out of date; run make gen-types" >&2
  diff -u "$before" "$after" || true
  exit 1
fi

echo "generated TypeScript types are current"
