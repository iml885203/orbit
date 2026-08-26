#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
field="${1:?usage: $0 FIELD}"
metadata_file="$(mktemp)"
trap 'rm -f "$metadata_file"' EXIT

if [[ -n "${2:-}" ]]; then
  cp "$2" "$metadata_file"
else
  make --no-print-directory -C "$repo_root" distribution-metadata >"$metadata_file"
fi

python3 - "$metadata_file" "$field" <<'PY'
import json
import pathlib
import sys

contract = "orbit.distribution.v1"
required = {
    "schema", "environment_repository", "environment_ref", "install_url",
    "release_api_url", "release_repository", "default_environment",
}
try:
    raw = pathlib.Path(sys.argv[1]).read_text(encoding="utf-8")
    value = json.loads(raw)
except (OSError, UnicodeError, json.JSONDecodeError) as error:
    raise SystemExit(f"{contract}: invalid metadata JSON: {error}")
if not isinstance(value, dict) or value.get("schema") != contract:
    raise SystemExit(f"{contract}: missing or unsupported schema")
missing = sorted(key for key in required if not isinstance(value.get(key), str) or not value[key])
if missing:
    raise SystemExit(f"{contract}: missing required fields: {', '.join(missing)}")
field = sys.argv[2]
if field not in required:
    raise SystemExit(f"{contract}: unknown field: {field}")
print(value[field])
PY
