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

# The demo is versioned on its own calendar scheme (vYEAR.MONTH.N) and is NOT
# re-tagged for every Orbit release — it moves only when the demo itself
# changes. So the pin is checked for existence, never for matching this tag.
env_repo_ref="$(sed -n 's/.*EnvRepoRef: "\([^"]*\)".*/\1/p' cmd/orbit/extensions.go)"
if [[ -z "$env_repo_ref" ]]; then
  echo "cmd/orbit/extensions.go has no EnvRepoRef; \`orbit init\` needs a pinned demo ref" >&2
  exit 1
fi

# The pinned demo tag must exist, because `orbit init` clones exactly this ref.
# Run locally so a bad pin surfaces before a release is ever dispatched, and
# skipped without network so an offline build still verifies version strings.
demo_repo="https://github.com/iml885203/orbit-demo.git"
if demo_tags="$(git ls-remote --tags --exit-code "$demo_repo" "refs/tags/$env_repo_ref" 2>/dev/null)"; then
  : "${demo_tags:?}"
elif [[ -z "${demo_tags+set}" ]] && ! git ls-remote --exit-code "$demo_repo" HEAD >/dev/null 2>&1; then
  echo "warning: cannot reach $demo_repo; version strings verified, paired demo tag unchecked" >&2
else
  echo "orbit-demo has no tag $env_repo_ref, but cmd/orbit/extensions.go pins it; \`orbit init\` would clone a ref that does not exist. Tag orbit-demo first." >&2
  exit 1
fi

echo "release candidate ${tag} version strings and paired demo tag are coherent"
