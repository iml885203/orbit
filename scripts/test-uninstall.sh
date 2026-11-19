#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_root="$(mktemp -d)"
trap 'rm -rf "$test_root"' EXIT

test_home="$test_root/home"
install_dir="$test_home/.local/bin"
orbit_home="$test_home/.orbit"
mock_bin="$test_root/mock-bin"
real_rm="$(command -v rm)"
mkdir -p "$install_dir" "$orbit_home" "$mock_bin"

cat > "$mock_bin/rm" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
for target in "$@"; do
  case "$target" in
    -*) ;;
    "$ORBIT_UNINSTALL_TEST_HOME"/*) ;;
    *) echo "refusing non-fixture deletion" >&2; exit 1 ;;
  esac
done
exec "$ORBIT_UNINSTALL_TEST_REAL_RM" "$@"
EOF
chmod +x "$mock_bin/rm"

uninstall_fixture() {
  local target="$1"
  shift
  HOME="$test_home" ORBIT_HOME="$target" \
    ORBIT_UNINSTALL_TEST_HOME="$test_home" ORBIT_UNINSTALL_TEST_REAL_RM="$real_rm" \
    PATH="$mock_bin:$install_dir:$PATH" \
    bash "$repo_root/scripts/uninstall.sh" "$@"
}

printf 'protected\n' > "$test_root/protected-host-binary"
if ORBIT_UNINSTALL_TEST_HOME="$test_home" ORBIT_UNINSTALL_TEST_REAL_RM="$real_rm" \
  "$mock_bin/rm" -f "$test_root/protected-host-binary" >/dev/null 2>&1; then
  echo "fixture deletion guard allowed an external target" >&2
  exit 1
fi
test -f "$test_root/protected-host-binary"

write_fake_binary() {
  printf '#!/usr/bin/env bash\nexit 0\n' >"$install_dir/orbit"
  chmod +x "$install_dir/orbit"
  cp "$install_dir/orbit" "$install_dir/orbit.prev"
}

write_fake_binary
printf 'keep me\n' >"$orbit_home/settings.json"

uninstall_fixture "$orbit_home" >/dev/null
test -x "$install_dir/orbit"
test -f "$orbit_home/settings.json"

uninstall_fixture "$orbit_home" --yes >/dev/null
test ! -e "$install_dir/orbit"
test ! -e "$install_dir/orbit.prev"
test -f "$orbit_home/settings.json"

write_fake_binary
uninstall_fixture "$orbit_home" --yes --purge >/dev/null
test ! -e "$install_dir/orbit"
test ! -e "$orbit_home"

write_fake_binary
if uninstall_fixture "$test_home" --yes --purge >/dev/null 2>&1; then
  echo "uninstaller accepted an unsafe purge target" >&2
  exit 1
fi
test -x "$install_dir/orbit"
test -d "$test_home"

echo "uninstall preserves user data unless --purge is explicit; fixture cannot delete external files"
