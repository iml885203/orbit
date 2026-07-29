#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
orbit_bin="${ORBIT_BIN:-$repo_root/bin/orbit}"

for guide in docs/local-first.md docs/local-first.zh-TW.md; do
  if ! grep -F "orbit up" "$repo_root/$guide" |
    grep -Ei "switch|切換" >/dev/null; then
    echo "$guide must explain that orbit up switches projects." >&2
    exit 1
  fi
done

if [ "${ORBIT_DOCS_ONLY:-}" = "1" ]; then
  echo "Project-switch guides preserve the one-command mental model"
  exit 0
fi

if [ ! -x "$orbit_bin" ]; then
  echo "Orbit binary not found at $orbit_bin; run 'make build' or set ORBIT_BIN." >&2
  exit 1
fi

test_root="$(mktemp -d)"
export ORBIT_HOME="$test_root/home"
export ORBIT_NAMESPACE="project-switch-$$"
export ORBIT_DASHBOARD_PORT="$((25000 + ($$ % 1000)))"
service_port="$((29000 + ($$ % 1000)))"

cleanup() {
  for project in project-b project-a; do
    if [ -d "$test_root/$project" ]; then
      (
        cd "$test_root/$project"
        "$orbit_bin" down --json >/dev/null 2>&1 || true
      )
    fi
  done
  "$orbit_bin" daemon stop --json >/dev/null 2>&1 || true
  rm -rf "$test_root"
}
trap 'status=$?; cleanup; exit "$status"' EXIT

make_project() {
  project="$1"
  resource="$2"
  content="$3"
  mkdir -p "$test_root/$project"
  printf '<h1>%s</h1>\n' "$content" >"$test_root/$project/index.html"
  printf '%s\n' \
    'version: "2"' \
    'services:' \
    "  $resource:" \
    '    type: python' \
    '    path: .' \
    "    command: python3 -m http.server $service_port" \
    "    url: http://localhost:$service_port" \
    '    ports:' \
    "      http: \"$service_port\"" \
    '    health_check:' \
    '      type: http' \
    '      path: /' \
    "      port: $service_port" \
    >"$test_root/$project/orbit.yaml"
}

make_project "project-a" "app-a" "project-a"
make_project "project-b" "app-b" "project-b"

cd "$test_root/project-a"
"$orbit_bin" up --json >"$test_root/up-a.json"
curl --fail --silent --show-error --retry 10 --retry-delay 1 \
  "http://localhost:$service_port" >"$test_root/page-a.html"
grep -F "project-a" "$test_root/page-a.html" >/dev/null

cd "$test_root/project-b"
"$orbit_bin" status --json >"$test_root/status-b-before.json"
"$orbit_bin" doctor --json >"$test_root/doctor-b.json"
"$orbit_bin" inspect --json >"$test_root/inspect-b.json"
if "$orbit_bin" down --json >"$test_root/down-b.json"; then
  echo "orbit down unexpectedly controlled the other project." >&2
  exit 1
fi
curl --fail --silent --show-error \
  "http://localhost:$service_port" >"$test_root/page-a-after-guard.html"
grep -F "project-a" "$test_root/page-a-after-guard.html" >/dev/null

"$orbit_bin" up --json >"$test_root/up-b.json"
curl --fail --silent --show-error --retry 10 --retry-delay 1 \
  "http://localhost:$service_port" >"$test_root/page-b.html"
grep -F "project-b" "$test_root/page-b.html" >/dev/null
"$orbit_bin" status --json >"$test_root/status-b-after.json"

python3 - "$test_root" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])

def read(name):
    return json.loads((root / name).read_text(encoding="utf-8"))

before = read("status-b-before.json")
doctor = read("doctor-b.json")
inspect = read("inspect-b.json")
guard = read("down-b.json")
up = read("up-b.json")
after = read("status-b-after.json")

assert before["ok"] is True
environment = before["data"]["environment"]
assert environment["source"] == "project"
assert environment["selected_name"] == "project-b"
assert environment["context_switch_required"] is True
assert environment["running_name"] == "project-a"
assert before["data"]["daemon"]["context_mismatch"] is True
assert [(item["name"], item["state"]) for item in before["data"]["resources"]] == [
    ("app-b", "stopped")
]
assert [action["command"] for action in before["recommended_actions"]] == [
    "orbit up --json"
]

assert doctor["ok"] is True
assert [action["command"] for action in doctor["recommended_actions"]] == [
    "orbit up --json"
]

assert inspect["ok"] is True
assert inspect["data"]["environment"]["context_switch_required"] is True
assert inspect["data"]["environment"]["running_name"] == "project-a"
assert inspect["data"]["resources"]["stopped"] == ["app-b"]
assert inspect["data"]["risks"][0]["code"] == "project_context_inactive"
assert [action["command"] for action in inspect["recommended_actions"]] == [
    "orbit up --json"
]

assert guard["ok"] is False
assert guard["error"]["code"] == "project_context_inactive"
assert [action["command"] for action in guard["recommended_actions"]] == [
    "orbit up --json"
]

assert up["ok"] is True
switched = up["data"]["context_switch"]
assert switched["from_name"] == "project-a"
assert switched["to_name"] == "project-b"
assert switched["stopped_resources"] == ["app-a"]

assert after["ok"] is True
assert after["data"]["environment"].get("context_switch_required") is not True
assert after["data"]["daemon"].get("context_mismatch") is not True
assert [(item["name"], item["state"]) for item in after["data"]["resources"]] == [
    ("app-b", "healthy")
]
PY

"$orbit_bin" down --json >/dev/null
"$orbit_bin" daemon stop --json >/dev/null

echo "Project switching keeps resource ownership safe and needs only orbit up"
