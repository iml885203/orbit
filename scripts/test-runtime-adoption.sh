#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
orbit_bin="${ORBIT_BIN:-$repo_root/bin/orbit}"

if [ ! -x "$orbit_bin" ]; then
  echo "Orbit binary not found at $orbit_bin; run 'make build' or set ORBIT_BIN." >&2
  exit 1
fi

test_root="$(mktemp -d)"
export ORBIT_HOME="$test_root/home"
export ORBIT_NAMESPACE="runtime-adoption-$$"
export ORBIT_DASHBOARD_PORT="$((32000 + ($$ % 500)))"
python_port="$((32500 + ($$ % 500)))"
node_port="$((33000 + ($$ % 500)))"
go_port="$((33500 + ($$ % 500)))"

cleanup() {
  "$orbit_bin" down --json >/dev/null 2>&1 || true
  "$orbit_bin" daemon stop --json >/dev/null 2>&1 || true
  rm -rf "$test_root"
}
trap 'status=$?; cleanup; exit "$status"' EXIT

cat >"$test_root/app.py" <<'PY'
import http.server
import os

http.server.ThreadingHTTPServer(
    ("127.0.0.1", int(os.environ["PORT"])),
    http.server.SimpleHTTPRequestHandler,
).serve_forever()
PY

cat >"$test_root/server.mjs" <<'JS'
import http from "node:http";

http.createServer((_request, response) => {
  response.end("node ready");
}).listen(Number(process.env.PORT), "127.0.0.1");
JS

cat >"$test_root/package.json" <<'JSON'
{
  "private": true,
  "type": "module"
}
JSON

cat >"$test_root/go.mod" <<'MOD'
module runtime-adoption

go 1.24
MOD

cat >"$test_root/main.go" <<'GO'
package main

import (
	"fmt"
	"net/http"
	"os"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("go ready"))
	})
	if err := http.ListenAndServe("127.0.0.1:"+os.Getenv("PORT"), nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
GO

cat >"$test_root/orbit.yaml" <<YAML
version: "3"
services:
  python:
    command: python3 app.py
    ports:
      http:
        preferred: $python_port
  node:
    command: node server.mjs
    ports:
      http:
        preferred: $node_port
  go:
    command: go run main.go
    ports:
      http:
        preferred: $go_port
YAML

cd "$test_root"
"$orbit_bin" doctor --json >"$test_root/doctor.json"
if ! "$orbit_bin" up --json >"$test_root/up.json" 2>"$test_root/up.stderr"; then
  echo "orbit up failed during runtime adoption." >&2
  if [ -s "$test_root/up.json" ]; then
    echo "orbit up JSON:" >&2
    sed 's/^/  /' "$test_root/up.json" >&2
  fi
  if [ -s "$test_root/up.stderr" ]; then
    echo "orbit up stderr:" >&2
    sed 's/^/  /' "$test_root/up.stderr" >&2
  fi
  echo "orbit status after the failure:" >&2
  "$orbit_bin" status --json >&2 || true
  if [ -s "$ORBIT_HOME/daemon.log" ]; then
    echo "daemon log tail:" >&2
    tail -n 80 "$ORBIT_HOME/daemon.log" | sed 's/^/  /' >&2
  fi
  exit 1
fi
"$orbit_bin" status --json >"$test_root/status.json"

python3 - "$test_root" <<'PY'
import json
import pathlib
import sys
import urllib.request

root = pathlib.Path(sys.argv[1])
doctor = json.loads((root / "doctor.json").read_text(encoding="utf-8"))
up = json.loads((root / "up.json").read_text(encoding="utf-8"))
status = json.loads((root / "status.json").read_text(encoding="utf-8"))

assert doctor["ok"] is True
assert up["ok"] is True
resources = {resource["name"]: resource for resource in status["data"]["resources"]}
assert set(resources) == {"python", "node", "go"}
assert all(resource["state"] == "healthy" for resource in resources.values())

for name, expected in {
    "python": None,
    "node": b"node ready",
    "go": b"go ready",
}.items():
    with urllib.request.urlopen(resources[name]["url"], timeout=2) as response:
        body = response.read()
        assert response.status == 200
        if expected is not None:
            assert body == expected
PY

echo "Python, Node, and Go projects adopt Orbit with command plus port"
