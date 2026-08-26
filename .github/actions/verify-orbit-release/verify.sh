#!/usr/bin/env bash
set -euo pipefail

repository="${VERIFY_REPOSITORY:-iml885203/orbit}"
mode="${VERIFY_MODE:-}"
tag="${VERIFY_TAG:-}"
expected_commit="${VERIFY_EXPECTED_COMMIT:-}"
asset_directory="${VERIFY_ASSET_DIRECTORY:-}"
attempts="${VERIFY_ATTEMPTS:-10}"
delay="${VERIFY_RETRY_DELAY_SECONDS:-6}"
gh_bin="${VERIFY_GH_BIN:-gh}"
manifest="$GITHUB_ACTION_PATH/assets.txt"

fail() {
  echo "release verification failed: $*" >&2
  exit 1
}

expected_assets() {
  LC_ALL=C sort "$manifest"
}

verify_inventory() {
  local directory="$1"
  local actual
  actual="$(find "$directory" -maxdepth 1 -type f -exec basename {} \; | LC_ALL=C sort)"
  if [[ "$actual" != "$(expected_assets)" ]]; then
    echo "expected release assets:" >&2
    expected_assets >&2
    echo "actual release assets:" >&2
    printf '%s\n' "$actual" >&2
    fail "asset inventory does not match"
  fi
}

case "$mode" in
  local)
    [[ -n "$asset_directory" ]] || fail "local mode requires asset-directory"
    verify_inventory "$asset_directory"
    exit 0
    ;;
  published) ;;
  *) fail "mode must be local or published" ;;
esac

[[ "$tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$ ]] ||
  fail "invalid release tag: $tag"
"$gh_bin" release verify --help >/dev/null 2>&1 ||
  fail "GitHub CLI does not support immutable release verification"

release_json=""
attestation_json=""
verified=false
for ((attempt = 1; attempt <= attempts; attempt++)); do
  release_json="$("$gh_bin" release view "$tag" --repo "$repository" \
    --json isImmutable,tagName,targetCommitish,assets 2>/dev/null || true)"
  if [[ "$(jq -r '.isImmutable // false' <<<"$release_json" 2>/dev/null)" == true ]] &&
    attestation_json="$("$gh_bin" release verify "$tag" --repo "$repository" --format json 2>/dev/null)"; then
    verified=true
    break
  fi
  if ((attempt < attempts)); then sleep "$delay"; fi
done
[[ "$verified" == true ]] || fail "$repository@$tag is not an immutable attested release after $attempts attempts"

[[ "$(jq -r .tagName <<<"$release_json")" == "$tag" ]] || fail "release tag does not match $tag"
published_assets="$(jq -r '.assets[].name' <<<"$release_json" | LC_ALL=C sort)"
[[ "$published_assets" == "$(expected_assets)" ]] || fail "published asset inventory does not match"
attested_assets="$(
  jq -r '.verificationResult.statement.subject[] | select(.name != null) | .name' \
    <<<"$attestation_json" 2>/dev/null | LC_ALL=C sort
)"
[[ "$attested_assets" == "$(expected_assets)" ]] || fail "attested asset inventory does not match"

ref_json="$("$gh_bin" api "repos/$repository/git/ref/tags/$tag")"
tag_commit="$(jq -r '.object.sha' <<<"$ref_json")"
if [[ "$(jq -r '.object.type' <<<"$ref_json")" == tag ]]; then
  tag_json="$("$gh_bin" api "repos/$repository/git/tags/$tag_commit")"
  [[ "$(jq -r '.object.type' <<<"$tag_json")" == commit ]] || fail "annotated release tag does not resolve to a commit"
  tag_commit="$(jq -r '.object.sha' <<<"$tag_json")"
elif [[ "$(jq -r '.object.type' <<<"$ref_json")" != commit ]]; then
  fail "release tag does not resolve to a commit"
fi
target="$(jq -r .targetCommitish <<<"$release_json")"
if [[ ! "$target" =~ ^[0-9a-f]{40}$ ]]; then
  target="$("$gh_bin" api "repos/$repository/commits/$target" --jq .sha)"
fi
[[ "$tag_commit" == "$target" ]] || fail "release target $target does not match tag commit $tag_commit"
if [[ -n "$expected_commit" && "$tag_commit" != "$expected_commit" ]]; then
  fail "release commit $tag_commit does not match approved commit $expected_commit"
fi

temporary_directory=""
if [[ -z "$asset_directory" ]]; then
  temporary_directory="$(mktemp -d)"
  trap 'rm -rf "$temporary_directory"' EXIT
  asset_directory="$temporary_directory"
  "$gh_bin" release download "$tag" --repo "$repository" --dir "$asset_directory"
fi
verify_inventory "$asset_directory"
while IFS= read -r asset; do
  "$gh_bin" release verify-asset "$tag" "$asset_directory/$asset" --repo "$repository" --format json >/dev/null
done < "$manifest"

echo "Verified immutable Orbit release $tag at $tag_commit with all expected assets"
