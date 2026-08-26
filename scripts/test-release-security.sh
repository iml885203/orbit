#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
action_path="$repo_root/.github/actions/verify-orbit-release"
verifier="$action_path/verify.sh"
test_root="$(mktemp -d)"
trap 'rm -rf "$test_root"' EXIT

assets="$test_root/assets"
mkdir -p "$assets"
while IFS= read -r asset; do printf '%s\n' "$asset" > "$assets/$asset"; done < "$action_path/assets.txt"

run_verifier() {
  GITHUB_ACTION_PATH="$action_path" \
    VERIFY_GH_BIN="$test_root/gh" \
    VERIFY_ATTEMPTS="${VERIFY_ATTEMPTS:-1}" \
    VERIFY_RETRY_DELAY_SECONDS=0 \
    FIXTURE_ROOT="$test_root" \
    FIXTURE_IMMUTABLE="${FIXTURE_IMMUTABLE:-true}" \
    FIXTURE_TARGET="${FIXTURE_TARGET:-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}" \
    FIXTURE_FAIL_ASSET="${FIXTURE_FAIL_ASSET:-}" \
    FIXTURE_VERIFY_FAILURES="${FIXTURE_VERIFY_FAILURES:-0}" \
    VERIFY_MODE="$1" \
    VERIFY_TAG="${2:-}" \
    VERIFY_EXPECTED_COMMIT="${3:-}" \
    VERIFY_ASSET_DIRECTORY="${4:-}" \
    "$verifier"
}

cat > "$test_root/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

case "$1 $2" in
  "release verify")
    if [[ "${3:-}" == --help ]]; then exit 0; fi
    count_file="$FIXTURE_ROOT/verify-count"
    count=0
    [[ ! -f "$count_file" ]] || count="$(cat "$count_file")"
    count=$((count + 1))
    printf '%s' "$count" > "$count_file"
    ((count > FIXTURE_VERIFY_FAILURES)) || exit 1
    jq -n \
      --rawfile names "${FIXTURE_ATTESTED_MANIFEST:-$FIXTURE_ROOT/manifest}" \
      '{verificationResult:{statement:{subject:
        ([{uri:"pkg:github/iml885203/orbit@v1.2.3",digest:{sha1:("a"*40)}}] +
         ($names|split("\n")|map(select(length>0))|map({name:.,digest:{sha256:("b"*64)}})))}}}'
    ;;
  "release view")
    jq -n \
      --argjson immutable "$FIXTURE_IMMUTABLE" \
      --arg target "$FIXTURE_TARGET" \
      --arg tag "v1.2.3" \
      --rawfile names "$FIXTURE_ROOT/manifest" \
      '{isImmutable:$immutable,tagName:$tag,targetCommitish:$target,
        assets:($names|split("\n")|map(select(length>0))|map({name:.}))}'
    ;;
  "release verify-asset")
    asset="$(basename "$4")"
    printf '%s\n' "$asset" >> "$FIXTURE_ROOT/verified-assets"
    if [[ "$asset" == "$FIXTURE_FAIL_ASSET" ]]; then exit 1; fi
    printf '%s\n' '{}'
    ;;
  "release download")
    destination=""
    while (($#)); do
      if [[ "$1" == --dir ]]; then destination="$2"; break; fi
      shift
    done
    cp "$FIXTURE_ROOT/assets/"* "$destination/"
    ;;
  "api repos/iml885203/orbit/git/ref/tags/v1.2.3")
    jq -n --arg type "${FIXTURE_TAG_TYPE:-commit}" \
      --arg sha "${FIXTURE_TAG_SHA:-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}" \
      '{object:{type:$type,sha:$sha}}'
    ;;
  "api repos/iml885203/orbit/git/tags/cccccccccccccccccccccccccccccccccccccccc")
    jq -n --arg type "${FIXTURE_PEELED_TYPE:-commit}" \
      --arg sha "${FIXTURE_PEELED_SHA:-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}" \
      '{object:{type:$type,sha:$sha}}'
    ;;
  *) echo "unexpected gh invocation: $*" >&2; exit 2 ;;
esac
EOF
chmod +x "$test_root/gh"
cp "$action_path/assets.txt" "$test_root/manifest"

run_verifier local "" "" "$assets" >/dev/null
printf '%s\n' unexpected > "$assets/unexpected"
if run_verifier local "" "" "$assets" >/dev/null 2>&1; then
  echo "local inventory accepted an extra asset" >&2
  exit 1
fi
rm "$assets/unexpected" "$assets/orbit.spdx.json"
if run_verifier local "" "" "$assets" >/dev/null 2>&1; then
  echo "local inventory accepted a missing asset" >&2
  exit 1
fi
printf '%s\n' orbit.spdx.json > "$assets/orbit.spdx.json"

rm -f "$test_root/verify-count" "$test_root/verified-assets"
VERIFY_ATTEMPTS=3 FIXTURE_VERIFY_FAILURES=2 \
  run_verifier published v1.2.3 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa "$assets" >/dev/null
[[ "$(wc -l < "$test_root/verified-assets" | tr -d ' ')" == 8 ]] || {
  echo "published verification did not verify exactly eight assets" >&2
  exit 1
}

if FIXTURE_IMMUTABLE=false run_verifier published v1.2.3 "" "$assets" >/dev/null 2>&1; then
  echo "published verification accepted a mutable release" >&2
  exit 1
fi
if run_verifier published v1.2.3 bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb "$assets" >/dev/null 2>&1; then
  echo "published verification accepted the wrong approved commit" >&2
  exit 1
fi
if FIXTURE_FAIL_ASSET=orbit-linux-amd64 run_verifier published v1.2.3 "" "$assets" >/dev/null 2>&1; then
  echo "published verification accepted a failed asset attestation" >&2
  exit 1
fi

cp "$test_root/manifest" "$test_root/attested-extra"
printf '%s\n' unexpected >> "$test_root/attested-extra"
if FIXTURE_ATTESTED_MANIFEST="$test_root/attested-extra" \
  run_verifier published v1.2.3 "" "$assets" >/dev/null 2>&1; then
  echo "published verification accepted an extra attested asset" >&2
  exit 1
fi
sed '/orbit.spdx.json/d' "$test_root/manifest" > "$test_root/attested-missing"
if FIXTURE_ATTESTED_MANIFEST="$test_root/attested-missing" \
  run_verifier published v1.2.3 "" "$assets" >/dev/null 2>&1; then
  echo "published verification accepted a missing attested asset" >&2
  exit 1
fi

FIXTURE_TAG_TYPE=tag FIXTURE_TAG_SHA=cccccccccccccccccccccccccccccccccccccccc \
  run_verifier published v1.2.3 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa "$assets" >/dev/null
if FIXTURE_TAG_TYPE=tag FIXTURE_TAG_SHA=cccccccccccccccccccccccccccccccccccccccc \
  FIXTURE_PEELED_TYPE=tree run_verifier published v1.2.3 "" "$assets" >/dev/null 2>&1; then
  echo "published verification accepted an annotated tag that resolves to a non-commit" >&2
  exit 1
fi

echo "release inventory, attestation subjects, retry, tag peeling, commit, and asset failure contracts OK"
