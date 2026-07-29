#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
example_root="$repo_root/docs/examples/mini-shop"
app="$example_root/apps/web/app.html"

python3 - "$app" <<'PY'
import sys
from html.parser import HTMLParser


class AppParser(HTMLParser):
    def __init__(self):
        super().__init__()
        self.ids = set()
        self.primary_actions = 0
        self.details = 0

    def handle_starttag(self, tag, attrs):
        values = dict(attrs)
        if values.get("id"):
            self.ids.add(values["id"])
        if tag == "button" and "primary-button" in values.get("class", "").split():
            self.primary_actions += 1
        if tag == "details":
            self.details += 1


parser = AppParser()
with open(sys.argv[1], encoding="utf-8") as source:
    parser.feed(source.read())

required_ids = {"run-success", "run-failure", "run-manual", "services"}
assert required_ids <= parser.ids, required_ids - parser.ids
assert parser.primary_actions == 1, parser.primary_actions
assert parser.details == 2, parser.details
PY

node - "$app" <<'JS'
const fs = require('fs');
const html = fs.readFileSync(process.argv[2], 'utf8');
const scripts = [...html.matchAll(/<script>([\s\S]*?)<\/script>/g)];
if (scripts.length !== 1) {
  throw new Error(`expected one inline script, found ${scripts.length}`);
}
new Function(scripts[0][1]);
JS

python3 -m compileall -q "$example_root/apps"
bash -n "$example_root/scripts/smoke.sh"

if rg -n 'headers\[[0-9]+\]|SELECT name FROM (carts|cart_items)' "$example_root/apps"; then
  echo "mini-shop contains a known response or health-check regression" >&2
  exit 1
fi

if rg -n \
  'dev-advanced|notification-api|observability-api|apps/web/index\.html|scripts/(compact|onboarding|release-check|smoke-compact|smoke-p1|smoke-demo|start-mini)' \
  "$example_root"; then
  echo "mini-shop still references a removed mode or component" >&2
  exit 1
fi

echo "mini-shop example structure is coherent"
