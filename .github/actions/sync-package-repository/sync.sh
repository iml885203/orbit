#!/usr/bin/env bash
# Dispatches a package repository's updater and waits for that exact run.
set -euo pipefail

gh_bin="${SYNC_GH_BIN:-gh}"
attempts="${SYNC_POLL_ATTEMPTS:-60}"
delay="${SYNC_POLL_DELAY_SECONDS:-5}"
correlation="${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}"
display_title="Update orbit ${VERSION} ${correlation}"

dispatched="$("$gh_bin" workflow run update.yml --repo "$REPOSITORY" --ref main \
  -f project=orbit -f version="$VERSION" -f correlation="$correlation" 2>&1)" ||
  { printf '%s\n' "$dispatched" >&2; exit 1; }
printf '%s\n' "$dispatched"

# gh prints the dispatched run's URL; the run list can lag it by minutes, so
# the title lookup is only the fallback for a gh that prints none.
run_id="$(grep -oE '/actions/runs/[0-9]+' <<<"$dispatched" | head -n 1 | sed 's#.*/##' || true)"
for ((i = 0; i < attempts && ${#run_id} == 0; i++)); do
  run_id="$("$gh_bin" run list --repo "$REPOSITORY" --workflow update.yml \
    --event workflow_dispatch --limit 50 --json databaseId,displayTitle \
    --jq ".[] | select(.displayTitle == \"$display_title\") | .databaseId" | head -n 1 || true)"
  [ -n "$run_id" ] || sleep "$delay"
done
[ -n "$run_id" ] || { echo "$REPOSITORY updater did not start" >&2; exit 1; }
"$gh_bin" run watch "$run_id" --repo "$REPOSITORY" --exit-status
