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

printf '#!/usr/bin/env bash\necho "orbit v0.0.1"\n' >"$fixtures/$asset"
chmod +x "$fixtures/$asset"
checksum="$(shasum -a 256 "$fixtures/$asset" | awk '{print $1}')"
printf '%s  %s\n' "$checksum" "$asset" >"$fixtures/checksums.txt"

cat >"$mock_bin/curl" <<'EOF'
#!/usr/bin/env bash
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
cp "$ORBIT_INSTALL_TEST_FIXTURES/$pattern" "$output"
EOF
chmod +x "$mock_bin/curl" "$mock_bin/gh"

PATH="$mock_bin:$PATH" \
  ORBIT_INSTALL_DIR="$install_dir" \
  ORBIT_INSTALL_TEST_FIXTURES="$fixtures" \
  ORBIT_VERSION="v0.0.1" \
  bash "$repo_root/scripts/install.sh"

test -x "$install_dir/orbit"
test "$("$install_dir/orbit" --version)" = "orbit v0.0.1"
echo "private installer fallback OK"
