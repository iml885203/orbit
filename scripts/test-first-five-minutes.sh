#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
orbit_bin="${ORBIT_BIN:-$repo_root/bin/orbit}"

for readme in README.md README.zh-TW.md; do
  first_run_section="$(
    awk '
      /^## (First 5 minutes|五分鐘上手)$/ { capture = 1; next }
      capture && /^## / { exit }
      capture { print }
    ' "$repo_root/$readme"
  )"

  for command in "orbit init --yes" "orbit up" "orbit status" "orbit open"; do
    if ! grep -Fx "$command" <<<"$first_run_section" >/dev/null; then
      echo "$readme first-run section is missing: $command" >&2
      exit 1
    fi
  done

  if grep -E 'orbit .*docs/examples/|orbit .*README' <<<"$first_run_section" >/dev/null; then
    echo "$readme first-run section depends on a source checkout." >&2
    exit 1
  fi
done

if [ "${ORBIT_DOCS_ONLY:-}" = "1" ]; then
  echo "README first-run commands are location-independent"
  exit 0
fi

if [ ! -x "$orbit_bin" ]; then
  echo "Orbit binary not found at $orbit_bin; run 'make build' or set ORBIT_BIN." >&2
  exit 1
fi

test_root="$(mktemp -d)"
export ORBIT_HOME="$test_root/orbit-home"
export ORBIT_NAMESPACE="first-run-$$"
export ORBIT_DASHBOARD_PORT="$((23000 + ($$ % 1000)))"

cleanup() {
  "$orbit_bin" down --json >/dev/null 2>&1 || true
  "$orbit_bin" daemon stop --json >/dev/null 2>&1 || true
  rm -rf "$test_root"
}
trap cleanup EXIT

mkdir -p "$test_root/empty-directory"
cd "$test_root/empty-directory"

"$orbit_bin" init --yes
"$orbit_bin" up --json >"$test_root/up.json"
"$orbit_bin" status --json >"$test_root/status.json"
curl --fail --silent --show-error --retry 10 --retry-delay 1 \
  http://127.0.0.1:28080/health >"$test_root/health.json"

python3 -c 'import json, sys; assert json.load(open(sys.argv[1], encoding="utf-8"))["ok"] is True' \
  "$test_root/up.json"
python3 -c 'import json, sys; assert json.load(open(sys.argv[1], encoding="utf-8"))["ok"] is True' \
  "$test_root/status.json"
python3 -c 'import json, sys; assert json.load(open(sys.argv[1], encoding="utf-8"))["ok"] is True' \
  "$test_root/health.json"

echo "README first five minutes succeeds outside a source checkout"
