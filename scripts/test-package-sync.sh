#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
sync="$repo_root/.github/actions/sync-package-repository/sync.sh"
test_root="$(mktemp -d)"
trap 'rm -rf "$test_root"' EXIT

# Fake gh: FIXTURE_DISPATCH_URL is what `workflow run` prints; the run list
# shows the run only from list call FIXTURE_LIST_VISIBLE_FROM on (0 = never).
cat >"$test_root/gh" <<'GH'
#!/usr/bin/env bash
set -euo pipefail
echo "$*" >>"$FIXTURE_ROOT/calls"
case "$1 $2" in
  "workflow run")
    if [ -n "${FIXTURE_DISPATCH_ERROR:-}" ]; then echo "$FIXTURE_DISPATCH_ERROR" >&2; exit 1; fi
    [ -z "$FIXTURE_DISPATCH_URL" ] || echo "$FIXTURE_DISPATCH_URL" ;;
  "run list")
    count=$(( $(cat "$FIXTURE_ROOT/lists" 2>/dev/null || echo 0) + 1 ))
    echo "$count" >"$FIXTURE_ROOT/lists"
    if [ "$FIXTURE_LIST_VISIBLE_FROM" -gt 0 ] && [ "$count" -ge "$FIXTURE_LIST_VISIBLE_FROM" ]; then echo 222; fi ;;
  "run watch") [ "$3" = "$FIXTURE_EXPECT_WATCH" ] || { echo "watched $3, want $FIXTURE_EXPECT_WATCH" >&2; exit 9; } ;;
esac
GH
chmod +x "$test_root/gh"

run_sync() {
  rm -f "$test_root/calls" "$test_root/lists"
  SYNC_GH_BIN="$test_root/gh" SYNC_POLL_ATTEMPTS=3 SYNC_POLL_DELAY_SECONDS=0 \
    FIXTURE_ROOT="$test_root" GITHUB_RUN_ID=40314140983 GITHUB_RUN_ATTEMPT=1 \
    REPOSITORY=owner/homebrew-tap VERSION=v0.16.7 "$sync" >/dev/null 2>"$test_root/stderr"
}

# The dispatch output names the run: watch it even though the list lags.
FIXTURE_DISPATCH_URL="https://github.com/owner/homebrew-tap/actions/runs/111" \
  FIXTURE_LIST_VISIBLE_FROM=0 FIXTURE_EXPECT_WATCH=111 run_sync
if grep -q "^run list" "$test_root/calls"; then
  echo "sync polled the run list although gh named the dispatched run" >&2
  exit 1
fi

# A gh that prints no URL falls back to the lagging title lookup.
FIXTURE_DISPATCH_URL="" FIXTURE_LIST_VISIBLE_FROM=2 FIXTURE_EXPECT_WATCH=222 run_sync

# A run that never appears fails loudly instead of passing silently.
if FIXTURE_DISPATCH_URL="" FIXTURE_LIST_VISIBLE_FROM=0 FIXTURE_EXPECT_WATCH=none run_sync; then
  echo "sync succeeded without ever finding the updater run" >&2
  exit 1
fi
grep -q "updater did not start" "$test_root/stderr"

# A refused dispatch keeps gh's reason in the log.
if FIXTURE_DISPATCH_ERROR="HTTP 403: Resource not accessible by integration" FIXTURE_DISPATCH_URL="" \
  FIXTURE_LIST_VISIBLE_FROM=0 FIXTURE_EXPECT_WATCH=none run_sync; then
  echo "sync succeeded although the dispatch was refused" >&2
  exit 1
fi
grep -q "HTTP 403" "$test_root/stderr"

echo "package sync watches the dispatched updater run, even while the run list lags"
