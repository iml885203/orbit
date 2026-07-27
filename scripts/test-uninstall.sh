#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_root="$(mktemp -d)"
trap 'rm -rf "$test_root"' EXIT

test_home="$test_root/home"
install_dir="$test_home/.local/bin"
orbit_home="$test_home/.orbit"
mkdir -p "$install_dir" "$orbit_home"

write_fake_binary() {
  printf '#!/usr/bin/env bash\nexit 0\n' >"$install_dir/orbit"
  chmod +x "$install_dir/orbit"
  cp "$install_dir/orbit" "$install_dir/orbit.prev"
}

write_fake_binary
printf 'keep me\n' >"$orbit_home/settings.json"

HOME="$test_home" PATH="$install_dir:$PATH" \
  bash "$repo_root/scripts/uninstall.sh" >/dev/null
test -x "$install_dir/orbit"
test -f "$orbit_home/settings.json"

HOME="$test_home" PATH="$install_dir:$PATH" \
  bash "$repo_root/scripts/uninstall.sh" --yes >/dev/null
test ! -e "$install_dir/orbit"
test ! -e "$install_dir/orbit.prev"
test -f "$orbit_home/settings.json"

write_fake_binary
HOME="$test_home" PATH="$install_dir:$PATH" \
  bash "$repo_root/scripts/uninstall.sh" --yes --purge >/dev/null
test ! -e "$install_dir/orbit"
test ! -e "$orbit_home"

write_fake_binary
if HOME="$test_home" ORBIT_HOME="$test_home" PATH="$install_dir:$PATH" \
  bash "$repo_root/scripts/uninstall.sh" --yes --purge >/dev/null 2>&1; then
  echo "uninstaller accepted an unsafe purge target" >&2
  exit 1
fi
test -x "$install_dir/orbit"
test -d "$test_home"

echo "uninstall preserves user data unless --purge is explicit"
