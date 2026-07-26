#!/usr/bin/env bash
# Install or update the Orbit CLI.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.sh | bash
#
# Env overrides:
#   ORBIT_INSTALL_DIR   target dir (default: ~/.local/bin or /usr/local/bin)
#   ORBIT_VERSION       release tag (default: latest)
#   ORBIT_REPO          GitHub owner/repo (default: iml885203/orbit)
#   ORBIT_BASE_URL      release asset base URL (overrides repo and version)

set -euo pipefail

REPO="${ORBIT_REPO:-iml885203/orbit}"
VERSION="${ORBIT_VERSION:-latest}"

detect_platform() {
  local os arch
  case "$(uname -s)" in
    Darwin) os="darwin" ;;
    Linux)  os="linux" ;;
    MINGW*|MSYS*|CYGWIN*) os="windows" ;;
    *) echo "unsupported OS: $(uname -s)" >&2; exit 1 ;;
  esac
  case "$(uname -m)" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *) echo "unsupported arch: $(uname -m)" >&2; exit 1 ;;
  esac
  echo "${os}-${arch}"
}

pick_install_dir() {
  if [ -n "${ORBIT_INSTALL_DIR:-}" ]; then
    echo "$ORBIT_INSTALL_DIR"; return
  fi
  if [ -w "/usr/local/bin" ] 2>/dev/null; then
    echo "/usr/local/bin"; return
  fi
  echo "$HOME/.local/bin"
}

release_base_url() {
  if [ -n "${ORBIT_BASE_URL:-}" ]; then
    echo "$ORBIT_BASE_URL"
  elif [ "$VERSION" = "latest" ]; then
    echo "https://github.com/${REPO}/releases/latest/download"
  else
    echo "https://github.com/${REPO}/releases/download/${VERSION}"
  fi
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    echo "SHA-256 tool not found (need sha256sum or shasum)" >&2
    return 1
  fi
}

main() {
  local platform binary base_url url checksum_url dir tmp_dir tmp checksum_file
  local expected actual target
  platform="$(detect_platform)"
  binary="orbit-${platform}"
  [[ "$platform" == windows-* ]] && binary="${binary}.exe"
  base_url="$(release_base_url)"
  url="${base_url}/${binary}"
  checksum_url="${base_url}/checksums.txt"

  dir="$(pick_install_dir)"
  mkdir -p "$dir"
  target="${dir}/orbit"
  [[ "$platform" == windows-* ]] && target="${target}.exe"

  echo "Downloading ${url}"
  tmp_dir="$(mktemp -d)"
  trap 'rm -rf "$tmp_dir"' EXIT
  tmp="${tmp_dir}/${binary}"
  checksum_file="${tmp_dir}/checksums.txt"
  if ! curl -fsSL "$url" -o "$tmp"; then
    echo "download failed: ${url}" >&2
    exit 1
  fi
  if ! curl -fsSL "$checksum_url" -o "$checksum_file"; then
    echo "checksum download failed: ${checksum_url}" >&2
    exit 1
  fi

  expected="$(awk -v name="$binary" '$2 == name || $2 == "*" name { print $1; exit }' "$checksum_file")"
  if [ -z "$expected" ]; then
    echo "checksum missing for ${binary}" >&2
    exit 1
  fi
  actual="$(sha256_file "$tmp")"
  if [ "$actual" != "$expected" ]; then
    echo "checksum mismatch for ${binary}" >&2
    exit 1
  fi
  echo "Verified SHA-256: ${actual}"

  chmod +x "$tmp"
  if [ -f "$target" ]; then
    rm -f "${target}.prev"
    if ln "$target" "${target}.prev" 2>/dev/null || cp -p "$target" "${target}.prev"; then
      echo "Previous binary backed up to ${target}.prev"
    fi
  fi
  mv "$tmp" "$target"
  trap - EXIT
  rm -rf "$tmp_dir"

  echo "Installed: ${target}"
  if ! command -v orbit >/dev/null 2>&1 || [ "$(command -v orbit)" != "$target" ]; then
    case ":$PATH:" in
      *":${dir}:"*) ;;
      *) echo "Add ${dir} to your PATH to run 'orbit' directly." ;;
    esac
  fi
  "$target" --version || true
}

main "$@"
