#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_root="$(mktemp -d)"
trap 'rm -rf "$test_root"' EXIT

metadata="$test_root/metadata.json"
make --no-print-directory -C "$repo_root" distribution-metadata >"$metadata"
make --no-print-directory -C "$repo_root" distribution-metadata >"$test_root/metadata-again.json"
cmp "$metadata" "$test_root/metadata-again.json"
test "$("$repo_root/scripts/distribution-metadata-field.sh" release_repository "$metadata")" = "iml885203/orbit"

python3 - "$metadata" "$repo_root/scripts/install.sh" "$repo_root/scripts/install.ps1" <<'PY'
import json
import pathlib
import re
import sys

metadata = json.loads(pathlib.Path(sys.argv[1]).read_text())
unix = pathlib.Path(sys.argv[2]).read_text()
windows = pathlib.Path(sys.argv[3]).read_text()
expected = metadata["release_repository"]
assert re.search(r'REPO="\$\{ORBIT_REPO:-' + re.escape(expected) + r'\}"', unix)
assert f'else {{ "{expected}" }}' in windows
PY

for shape in empty malformed wrong-schema missing-field; do
  fixture="$test_root/$shape.json"
  case "$shape" in
    empty) : >"$fixture" ;;
    malformed) printf '{' >"$fixture" ;;
    wrong-schema) printf '{"schema":"orbit.distribution.v2"}' >"$fixture" ;;
    missing-field) cp "$metadata" "$fixture"; python3 - "$fixture" <<'PY'
import json, pathlib, sys
p = pathlib.Path(sys.argv[1]); value = json.loads(p.read_text()); del value["environment_ref"]; p.write_text(json.dumps(value))
PY
      ;;
  esac
  if "$repo_root/scripts/distribution-metadata-field.sh" environment_ref "$fixture" >"$test_root/out" 2>"$test_root/error"; then
    echo "$shape metadata unexpectedly passed in shared reader" >&2; exit 1
  fi
  grep -F "orbit.distribution.v1" "$test_root/error" >/dev/null
  if "$repo_root/scripts/verify-release-candidate.sh" "v$(tr -d '[:space:]' < "$repo_root/VERSION")" "$fixture" >"$test_root/out" 2>"$test_root/error"; then
    echo "$shape metadata unexpectedly passed release-candidate consumer" >&2; exit 1
  fi
  grep -F "orbit.distribution.v1" "$test_root/error" >/dev/null
  if ORBIT_DISTRIBUTION_METADATA_TEST_FILE="$fixture" ORBIT_DISTRIBUTION_METADATA_VALIDATE_ONLY=1 "$repo_root/scripts/test-quickstart-journey.sh" >"$test_root/out" 2>"$test_root/error"; then
    echo "$shape metadata unexpectedly passed quickstart consumer" >&2; exit 1
  fi
  grep -F "orbit.distribution.v1" "$test_root/error" >/dev/null
  if "$repo_root/scripts/verify-pinned-demo-evidence.sh" "$fixture" >"$test_root/out" 2>"$test_root/error"; then
    echo "$shape metadata unexpectedly passed release-workflow consumer" >&2; exit 1
  fi
  grep -F "orbit.distribution.v1" "$test_root/error" >/dev/null
done

printf '{"schema":"orbit.distribution.v1","environment_ref":"stale"}' >"$test_root/stale.json"
test "$(ORBIT_DISTRIBUTION_METADATA_FILE="$test_root/stale.json" "$repo_root/scripts/distribution-metadata-field.sh" environment_ref)" = "$("$repo_root/scripts/distribution-metadata-field.sh" environment_ref)"

grep -F 'verify-pinned-demo-evidence.sh' "$repo_root/.github/workflows/release.yml" >/dev/null
for consumer in scripts/verify-release-candidate.sh scripts/test-quickstart-journey.sh scripts/verify-pinned-demo-evidence.sh; do
  grep -F 'distribution-metadata-field.sh' "$repo_root/$consumer" >/dev/null
  if grep -E 'sed .*EnvRepoRef|cmd/orbit/extensions.go.*EnvRepoRef' "$repo_root/$consumer" >/dev/null; then
    echo "$consumer still parses Go distribution source" >&2
    exit 1
  fi
done
if grep -E 'sed .*EnvRepoRef|cmd/orbit/extensions.go.*EnvRepoRef' \
  "$repo_root/.github/workflows/release.yml" "$repo_root/scripts/verify-release-candidate.sh" \
  "$repo_root/scripts/test-quickstart-journey.sh" "$repo_root/scripts/verify-pinned-demo-evidence.sh" >/dev/null; then
  echo "a distribution consumer still parses Go source" >&2
  exit 1
fi

echo "distribution metadata exporter, bootstrap defaults, and fail-closed reader OK"
