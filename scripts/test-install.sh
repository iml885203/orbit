#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_root="$(mktemp -d)"
real_home_boundary="$(mktemp -d)"
trap 'rm -rf "$test_root" "$real_home_boundary"' EXIT

build_gomodcache="$(go env GOMODCACHE)"
build_gocache="$(go env GOCACHE)"

mkdir -p "$real_home_boundary/.config/orbit" "$real_home_boundary/.orbit" "$real_home_boundary/.config/gh"
printf 'unchanged\n' >"$real_home_boundary/.config/orbit/canary"
printf 'unchanged\n' >"$real_home_boundary/.orbit/canary"
printf 'unchanged\n' >"$real_home_boundary/.config/gh/canary"
boundary_snapshot() {
  (
    cd "$real_home_boundary"
    find . -print | LC_ALL=C sort
    find . -type f -exec shasum -a 256 {} \; | LC_ALL=C sort
  )
}
boundary_before="$(boundary_snapshot)"
export HOME="$real_home_boundary"
export XDG_CONFIG_HOME="$real_home_boundary/.config"
export XDG_CACHE_HOME="$real_home_boundary/.cache"
export GH_CONFIG_DIR="$real_home_boundary/.config/gh"
export ORBIT_HOME="$real_home_boundary/.orbit"
export ORBIT_UPDATE_HOME="$real_home_boundary/.orbit/update"

mock_bin="$test_root/bin"
fixtures="$test_root/fixtures"
install_dir="$test_root/install"
user_home="$test_root/user"
mkdir -p "$mock_bin" "$fixtures" "$install_dir" "$user_home/config" "$user_home/cache" "$user_home/gh" "$test_root/tmp" "$test_root/build-home"

platform="$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m)"
case "$platform" in
  darwin-x86_64) asset="orbit-darwin-amd64" ;;
  darwin-arm64) asset="orbit-darwin-arm64" ;;
  linux-x86_64) asset="orbit-linux-amd64" ;;
  linux-aarch64) asset="orbit-linux-arm64" ;;
  *) echo "unsupported test platform: $platform" >&2; exit 1 ;;
esac

write_release() {
  local version="$1" checksum
  (cd "$repo_root" && HOME="$test_root/build-home" GOTELEMETRY=off GOMODCACHE="$build_gomodcache" GOCACHE="$build_gocache" go build -ldflags "-s -w -X main.version=v$version -X main.buildTime=2026-07-27T04:44:56Z" -o "$fixtures/$asset" ./cmd/orbit)
  checksum="$(shasum -a 256 "$fixtures/$asset" | awk '{print $1}')"
  printf '%s  %s\n' "$checksum" "$asset" >"$fixtures/checksums.txt"
}

cat >"$mock_bin/curl" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$ORBIT_INSTALL_TEST_CURL_LOG"
exit 22
EOF

cat >"$mock_bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [ "$1 $2" = "auth status" ]; then
  exit 0
fi
if [ "$1 $2" != "release download" ]; then
  exit 1
fi
shift 2
pattern=""
output=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --pattern) pattern="$2"; shift 2 ;;
    --output) output="$2"; shift 2 ;;
    --repo) shift 2 ;;
    --clobber) shift ;;
    *) shift ;;
  esac
done
if [ "${ORBIT_INSTALL_TEST_FAIL_ASSET:-}" = "$pattern" ]; then
  printf 'partial download' >"$output"
  exit 1
fi
cp "$ORBIT_INSTALL_TEST_FIXTURES/$pattern" "$output"
EOF
chmod +x "$mock_bin/curl" "$mock_bin/gh"

install_version() {
  local version="$1"
  env -i \
    PATH="$mock_bin:$PATH" \
    HOME="$user_home" \
    TMPDIR="$test_root/tmp" \
    XDG_CONFIG_HOME="$user_home/config" \
    XDG_CACHE_HOME="$user_home/cache" \
    GH_CONFIG_DIR="$user_home/gh" \
    ORBIT_HOME="$user_home/orbit" \
    ORBIT_UPDATE_HOME="$user_home/update" \
    ORBIT_UPDATE_BACKGROUND=1 \
    ORBIT_ALLOW_DOWNGRADE="${ORBIT_ALLOW_DOWNGRADE:-}" \
    ORBIT_INSTALL_TEST_FAIL_ASSET="${ORBIT_INSTALL_TEST_FAIL_ASSET:-}" \
    ORBIT_INSTALL_DIR="$install_dir" \
    ORBIT_INSTALL_TEST_FIXTURES="$fixtures" \
    ORBIT_INSTALL_TEST_CURL_LOG="$test_root/curl.log" \
    ORBIT_VERSION="v${version}" \
    bash "$repo_root/scripts/install.sh"
}

probe_version() {
  env -i PATH="${PATH:-/usr/bin:/bin}" HOME="$user_home" TMPDIR="$test_root/tmp" \
    XDG_CONFIG_HOME="$user_home/config" XDG_CACHE_HOME="$user_home/cache" \
    GH_CONFIG_DIR="$user_home/gh" ORBIT_HOME="$user_home/orbit" \
    ORBIT_UPDATE_HOME="$user_home/update" ORBIT_UPDATE_BACKGROUND=1 \
    "$1" --version
}

write_release "0.0.1"
rm -f "$test_root/curl.log"
install_output="$(install_version "0.0.1")"
test "$(wc -l <"$test_root/curl.log" | tr -d ' ')" = "1"
test -x "$install_dir/orbit"
test "$(probe_version "$install_dir/orbit")" = "v0.0.1 (2026-07-27 12:44:56 +0800)"
grep -F "Next: export PATH=${install_dir}:\"\$PATH\" && orbit init" <<<"$install_output" >/dev/null
PATH="$install_dir:$mock_bin:/usr/bin:/bin" probe_version orbit >/dev/null

same_version_output="$(install_version "0.0.1")"
grep -F "Already installed: Orbit 0.0.1 at ${install_dir}/orbit" <<<"$same_version_output" >/dev/null
test ! -e "$install_dir/orbit.prev"

write_release "0.0.0"
if install_version "0.0.0" >/dev/null 2>&1; then
  echo "installer unexpectedly downgraded an existing binary" >&2
  exit 1
fi
test "$(probe_version "$install_dir/orbit")" = "v0.0.1 (2026-07-27 12:44:56 +0800)"

ORBIT_ALLOW_DOWNGRADE=1 install_version "0.0.0" >/dev/null
test "$(probe_version "$install_dir/orbit")" = "v0.0.0 (2026-07-27 12:44:56 +0800)"
test "$(probe_version "$install_dir/orbit.prev")" = "v0.0.1 (2026-07-27 12:44:56 +0800)"

write_release "0.0.2"
printf 'bad-checksum  %s\n' "$asset" >"$fixtures/checksums.txt"
if install_version "0.0.2" >/dev/null 2>&1; then
  echo "installer accepted a bad checksum" >&2
  exit 1
fi
test "$(probe_version "$install_dir/orbit")" = "v0.0.0 (2026-07-27 12:44:56 +0800)"
test "$(probe_version "$install_dir/orbit.prev")" = "v0.0.1 (2026-07-27 12:44:56 +0800)"
test -z "$(find "$install_dir" -maxdepth 1 -name '.orbit-install.*' -print -quit)"

write_release "0.0.3"
if ORBIT_INSTALL_TEST_FAIL_ASSET="$asset" install_version "0.0.3" >/dev/null 2>&1; then
  echo "installer accepted an interrupted download" >&2
  exit 1
fi
test "$(probe_version "$install_dir/orbit")" = "v0.0.0 (2026-07-27 12:44:56 +0800)"
test "$(probe_version "$install_dir/orbit.prev")" = "v0.0.1 (2026-07-27 12:44:56 +0800)"
test -z "$(find "$install_dir" -maxdepth 1 -name '.orbit-install.*' -print -quit)"

write_release "0.0.2"
install_version "0.0.2" >/dev/null
test "$(probe_version "$install_dir/orbit")" = "v0.0.2 (2026-07-27 12:44:56 +0800)"
test "$(probe_version "$install_dir/orbit.prev")" = "v0.0.0 (2026-07-27 12:44:56 +0800)"

test "$(boundary_snapshot)" = "$boundary_before"

echo "installer no-op, immediate next step, fallback, downgrade guard, interrupted-download safety, checksum safety, and rollback backup OK"
