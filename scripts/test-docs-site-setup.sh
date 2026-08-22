#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_root="$(mktemp -d)"
cleanup() {
  rm -rf "$test_root"
}
trap cleanup EXIT

mkdir -p "$test_root/bin"
cat >"$test_root/bin/pnpm" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$PNPM_CALLS"
SH
chmod +x "$test_root/bin/pnpm"

run_make() {
  : >"$test_root/calls"
  PATH="$test_root/bin:$PATH" PNPM_CALLS="$test_root/calls" \
    make --no-print-directory -s -C "$repo_root" "$@"
}

assert_count() {
  expected="$1"
  pattern="$2"
  actual="$(grep -Fxc -- "$pattern" "$test_root/calls" || true)"
  if [ "$actual" -ne "$expected" ]; then
    echo "Expected $expected docs setup call(s) for '$pattern', found $actual:" >&2
    sed 's/^/  /' "$test_root/calls" >&2
    exit 1
  fi
}

run_make docs-site-check
assert_count 1 "--dir website install --frozen-lockfile"
assert_count 1 "--dir website exec playwright install chromium"
assert_count 0 "--dir website exec playwright install-deps chromium"
assert_count 1 "--dir website run check"

run_make docs-site-linux-deps docs-site-check
assert_count 1 "--dir website install --frozen-lockfile"
assert_count 1 "--dir website exec playwright install chromium"
assert_count 1 "--dir website exec playwright install-deps chromium"
assert_count 1 "--dir website run check"

run_make PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH=/provided/chromium docs-site-check
assert_count 1 "--dir website install --frozen-lockfile"
assert_count 0 "--dir website exec playwright install chromium"
assert_count 0 "--dir website exec playwright install-deps chromium"
assert_count 1 "--dir website run check"

echo "Documentation checks install dependencies and the browser through one owned setup path"
