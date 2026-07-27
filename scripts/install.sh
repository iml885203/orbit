#!/usr/bin/env bash
# Install or update the Orbit CLI.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.sh | bash
# Private repository rehearsal:
#   gh api -H "Accept: application/vnd.github.raw+json" \
#     repos/iml885203/orbit/contents/scripts/install.sh | bash
#
# Env overrides:
#   ORBIT_INSTALL_DIR   target dir (default: ~/.local/bin or /usr/local/bin)
#   ORBIT_VERSION       release tag (default: latest)
#   ORBIT_REPO          GitHub owner/repo (default: iml885203/orbit)
#   ORBIT_BASE_URL      release asset base URL (overrides repo and version)
#   ORBIT_ALLOW_DOWNGRADE=1  permit replacing a newer installed version

set -euo pipefail

REPO="${ORBIT_REPO:-iml885203/orbit}"
VERSION="${ORBIT_VERSION:-latest}"
INSTALL_TMP_DIR=""

cleanup_install_temp() {
  case "$INSTALL_TMP_DIR" in
    */.orbit-install.*) rm -rf -- "$INSTALL_TMP_DIR" ;;
  esac
  INSTALL_TMP_DIR=""
}

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

binary_version() {
  local output
  output="$("$1" --version 2>/dev/null)" || return 1
  if [[ "$output" =~ ^orbit[[:space:]]+v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)([-+][0-9A-Za-z.-]+)?$ ]]; then
    echo "${BASH_REMATCH[1]}.${BASH_REMATCH[2]}.${BASH_REMATCH[3]}${BASH_REMATCH[4]}"
    return
  fi
  return 1
}

version_is_newer() {
  local left="${1%%+*}" right="${2%%+*}"
  local left_core="${left%%-*}" right_core="${right%%-*}"
  local left_pre="" right_pre=""
  [[ "$left" == *-* ]] && left_pre="${left#*-}"
  [[ "$right" == *-* ]] && right_pre="${right#*-}"

  local left_parts right_parts
  IFS=. read -r -a left_parts <<<"$left_core"
  IFS=. read -r -a right_parts <<<"$right_core"
  local i
  for i in 0 1 2; do
    if ((10#${left_parts[$i]} > 10#${right_parts[$i]})); then return 0; fi
    if ((10#${left_parts[$i]} < 10#${right_parts[$i]})); then return 1; fi
  done

  if [ -z "$left_pre" ]; then [ -n "$right_pre" ]; return; fi
  if [ -z "$right_pre" ]; then return 1; fi

  IFS=. read -r -a left_parts <<<"$left_pre"
  IFS=. read -r -a right_parts <<<"$right_pre"
  local count="${#left_parts[@]}"
  ((${#right_parts[@]} > count)) && count="${#right_parts[@]}"
  for ((i = 0; i < count; i++)); do
    [ "$i" -lt "${#left_parts[@]}" ] || return 1
    [ "$i" -lt "${#right_parts[@]}" ] || return 0
    local left_id="${left_parts[$i]}" right_id="${right_parts[$i]}"
    [ "$left_id" = "$right_id" ] && continue
    if [[ "$left_id" =~ ^[0-9]+$ ]] && [[ "$right_id" =~ ^[0-9]+$ ]]; then
      ((10#$left_id > 10#$right_id)) && return 0
      return 1
    fi
    [[ "$left_id" =~ ^[0-9]+$ ]] && return 1
    [[ "$right_id" =~ ^[0-9]+$ ]] && return 0
    [[ "$left_id" > "$right_id" ]]
    return
  done
  return 1
}

download_asset() {
  local asset="$1" destination="$2" url="$3"
  if curl -fsSL "$url" -o "$destination"; then
    return
  fi
  if [ -n "${ORBIT_BASE_URL:-}" ]; then
    echo "download failed: ${url}" >&2
    return 1
  fi
  if ! command -v gh >/dev/null 2>&1 || ! gh auth status >/dev/null 2>&1; then
    echo "download failed: ${url}" >&2
    echo "for a private repository, install and authenticate GitHub CLI (gh)" >&2
    return 1
  fi

  echo "Anonymous download unavailable; retrying with authenticated GitHub CLI"
  if [ "$VERSION" = "latest" ]; then
    gh release download --repo "$REPO" --pattern "$asset" --output "$destination" --clobber
  else
    gh release download "$VERSION" --repo "$REPO" --pattern "$asset" --output "$destination" --clobber
  fi
}

main() {
  local platform binary base_url url checksum_url dir tmp_dir tmp checksum_file
  local expected actual target candidate_version current_version backup
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
  tmp_dir="$(mktemp -d "${dir}/.orbit-install.XXXXXX")"
  INSTALL_TMP_DIR="$tmp_dir"
  trap cleanup_install_temp EXIT
  tmp="${tmp_dir}/${binary}"
  checksum_file="${tmp_dir}/checksums.txt"
  download_asset "$binary" "$tmp" "$url"
  download_asset "checksums.txt" "$checksum_file" "$checksum_url"

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
  if ! candidate_version="$(binary_version "$tmp")"; then
    echo "downloaded ${binary} does not report a valid Orbit semantic version" >&2
    exit 1
  fi
  if [ "$VERSION" != "latest" ] && [ "${VERSION#v}" != "$candidate_version" ]; then
    echo "downloaded version ${candidate_version} does not match requested ${VERSION}" >&2
    exit 1
  fi
  if [ -f "$target" ]; then
    current_version="$(binary_version "$target" || true)"
    if [ -n "$current_version" ] &&
       version_is_newer "$current_version" "$candidate_version" &&
       [ "${ORBIT_ALLOW_DOWNGRADE:-0}" != "1" ]; then
      echo "refusing to replace newer Orbit ${current_version} with ${candidate_version}" >&2
      echo "use 'orbit update --rollback', or set ORBIT_ALLOW_DOWNGRADE=1 for an intentional downgrade" >&2
      exit 1
    fi
    backup="${tmp_dir}/previous"
    if ! ln "$target" "$backup" 2>/dev/null && ! cp -p "$target" "$backup"; then
      echo "cannot back up current binary; leaving ${target} unchanged" >&2
      exit 1
    fi
    mv -f "$backup" "${target}.prev"
    echo "Previous binary backed up to ${target}.prev"
  fi
  mv "$tmp" "$target"
  cleanup_install_temp
  trap - EXIT

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
