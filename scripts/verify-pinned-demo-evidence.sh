#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
metadata_fixture="${1:-}"
read_metadata() {
  if [[ -n "$metadata_fixture" ]]; then
    "$repo_root/scripts/distribution-metadata-field.sh" "$1" "$metadata_fixture"
  else
    "$repo_root/scripts/distribution-metadata-field.sh" "$1"
  fi
}

demo_repository="$(read_metadata environment_repository)"
demo_repository="${demo_repository#https://github.com/}"
demo_repository="${demo_repository%.git}"
demo_ref="$(read_metadata environment_ref)"

ref_object="$(gh api "repos/$demo_repository/git/ref/tags/$demo_ref")"
demo_commit="$(jq -r '.object.sha' <<<"$ref_object")"
if [[ "$(jq -r '.object.type' <<<"$ref_object")" == "tag" ]]; then
  demo_commit="$(gh api "repos/$demo_repository/git/tags/$demo_commit" --jq '.object.sha')"
fi
successful_demo_gate="$(
  gh api "repos/$demo_repository/commits/$demo_commit/check-runs" \
    --jq '[.check_runs[] | select(.name == "validate" and .conclusion == "success")] | length'
)"
if [[ "$successful_demo_gate" == "0" ]]; then
  echo "$demo_repository@$demo_ref must pass its validate journey before Orbit pins it" >&2
  exit 1
fi
