#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_root="$(mktemp -d)"
trap 'rm -rf "$test_root"' EXIT

mock_bin="$test_root/bin"
fixtures="$test_root/fixtures"
install_dir="$test_root/install"
mkdir -p "$mock_bin" "$fixtures" "$install_dir"

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
  printf '#!/usr/bin/env bash\necho "v%s (2026-07-27 12:44:56 +0800)"\n' "$version" >"$fixtures/$asset"
  chmod +x "$fixtures/$asset"
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
  PATH="$mock_bin:$PATH" \
    ORBIT_INSTALL_DIR="$install_dir" \
    ORBIT_INSTALL_TEST_FIXTURES="$fixtures" \
    ORBIT_INSTALL_TEST_CURL_LOG="$test_root/curl.log" \
    ORBIT_VERSION="v${version}" \
    bash "$repo_root/scripts/install.sh"
}

write_release "0.0.1"
rm -f "$test_root/curl.log"
install_output="$(install_version "0.0.1")"
test "$(wc -l <"$test_root/curl.log" | tr -d ' ')" = "1"
test -x "$install_dir/orbit"
test "$("$install_dir/orbit" --version)" = "v0.0.1 (2026-07-27 12:44:56 +0800)"
grep -F "Next: export PATH=${install_dir}:\"\$PATH\" && orbit init" <<<"$install_output" >/dev/null
PATH="$install_dir:$mock_bin:/usr/bin:/bin" orbit --version >/dev/null

write_release "0.0.0"
if install_version "0.0.0" >/dev/null 2>&1; then
  echo "installer unexpectedly downgraded an existing binary" >&2
  exit 1
fi
test "$("$install_dir/orbit" --version)" = "v0.0.1 (2026-07-27 12:44:56 +0800)"

ORBIT_ALLOW_DOWNGRADE=1 install_version "0.0.0" >/dev/null
test "$("$install_dir/orbit" --version)" = "v0.0.0 (2026-07-27 12:44:56 +0800)"
test "$("$install_dir/orbit.prev" --version)" = "v0.0.1 (2026-07-27 12:44:56 +0800)"

write_release "0.0.2"
printf 'bad-checksum  %s\n' "$asset" >"$fixtures/checksums.txt"
if install_version "0.0.2" >/dev/null 2>&1; then
  echo "installer accepted a bad checksum" >&2
  exit 1
fi
test "$("$install_dir/orbit" --version)" = "v0.0.0 (2026-07-27 12:44:56 +0800)"
test "$("$install_dir/orbit.prev" --version)" = "v0.0.1 (2026-07-27 12:44:56 +0800)"
test -z "$(find "$install_dir" -maxdepth 1 -name '.orbit-install.*' -print -quit)"

write_release "0.0.3"
if ORBIT_INSTALL_TEST_FAIL_ASSET="$asset" install_version "0.0.3" >/dev/null 2>&1; then
  echo "installer accepted an interrupted download" >&2
  exit 1
fi
test "$("$install_dir/orbit" --version)" = "v0.0.0 (2026-07-27 12:44:56 +0800)"
test "$("$install_dir/orbit.prev" --version)" = "v0.0.1 (2026-07-27 12:44:56 +0800)"
test -z "$(find "$install_dir" -maxdepth 1 -name '.orbit-install.*' -print -quit)"

write_release "0.0.2"
install_version "0.0.2" >/dev/null
test "$("$install_dir/orbit" --version)" = "v0.0.2 (2026-07-27 12:44:56 +0800)"
test "$("$install_dir/orbit.prev" --version)" = "v0.0.0 (2026-07-27 12:44:56 +0800)"

echo "installer immediate next step, fallback, downgrade guard, interrupted-download safety, checksum safety, and rollback backup OK"
