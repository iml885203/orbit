#!/usr/bin/env bash
set -euo pipefail

tag="${1:-}"
if [[ ! "$tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$ ]]; then
  echo "usage: $0 vMAJOR.MINOR.PATCH[-PRERELEASE]" >&2
  exit 1
fi

version="${tag#v}"

for manifest in \
  plugins/orbit-agent/.codex-plugin/plugin.json \
  plugins/orbit-agent/.claude-plugin/plugin.json
do
  actual="$(node -e 'const fs=require("fs"); process.stdout.write(JSON.parse(fs.readFileSync(process.argv[1])).version)' "$manifest")"
  if [[ "$actual" != "$version" ]]; then
    echo "$manifest has version $actual; expected $version" >&2
    exit 1
  fi
done

echo "release candidate ${tag} is coherent"
