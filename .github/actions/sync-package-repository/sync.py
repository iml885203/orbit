#!/usr/bin/env python3
"""Dispatch or resume one Orbit package updater and observe its exact result."""

import json
import os
import re
import subprocess
import sys
import time


DEADLINE = None


class SyncError(Exception):
    pass


def invoke(arguments, timeout):
    remaining = DEADLINE - time.monotonic()
    if remaining <= 0:
        raise SyncError("Package sync exceeded its 10-minute total budget; inspect the accepted run before recovery")
    try:
        return subprocess.run(
            [os.environ.get("SYNC_GH_BIN", "gh"), *arguments],
            capture_output=True, text=True, timeout=min(timeout, remaining), check=False,
        )
    except subprocess.TimeoutExpired as error:
        raise TimeoutError("GitHub request timed out") from error


def read(endpoint, timeout):
    response = invoke(["api", "--include", endpoint], timeout)
    headers, separator, body = response.stdout.replace("\r\n", "\n").partition("\n\n")
    status = re.match(r"HTTP/\S+ (\d{3})\b", headers)
    if not separator or not status:
        raise SyncError("GitHub returned invalid HTTP data")
    code = int(status[1])
    if code in (404, 500, 502, 503, 504):
        raise TimeoutError(f"GitHub read returned HTTP {code}")
    if code != 200 or response.returncode:
        raise SyncError(f"GitHub read failed: HTTP {code}: {response.stderr.strip()}")
    try:
        return json.loads(body)
    except ValueError as error:
        raise SyncError("GitHub returned invalid JSON") from error


def run_identity(data, repository, run_id, version, title):
    if not isinstance(data, dict):
        raise SyncError("Updater identity is invalid")
    expected_title = title or rf"Update orbit {re.escape(version)} [0-9]+-[0-9]+"
    if (
        data.get("id") != int(run_id)
        or data.get("repository", {}).get("full_name") != repository
        or data.get("path") != ".github/workflows/update.yml"
        or data.get("head_branch") != "main"
        or data.get("event") != "workflow_dispatch"
        or not isinstance(data.get("display_title"), str)
        or (data["display_title"] != title if title else not re.fullmatch(expected_title, data["display_title"]))
    ):
        raise SyncError("Updater does not match repository, workflow, main branch, Orbit project and version")


def main():
    global DEADLINE
    DEADLINE = time.monotonic() + 600
    repository = os.environ["REPOSITORY"]
    version = os.environ["VERSION"]
    attempts = int(os.environ.get("SYNC_POLL_ATTEMPTS", "60"))
    delay = float(os.environ.get("SYNC_POLL_DELAY_SECONDS", "5"))
    timeout = float(os.environ.get("SYNC_REQUEST_TIMEOUT_SECONDS", "10"))
    if attempts < 1 or delay < 0 or timeout <= 0:
        raise SyncError("Invalid observation limits")
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository):
        raise SyncError("Invalid package repository")
    run_id = os.environ.get("EXISTING_RUN_ID", "")
    title = None
    if run_id:
        if not re.fullmatch(r"[1-9][0-9]*", run_id):
            raise SyncError("Existing run ID must be a positive integer")
    else:
        correlation = f'{os.environ["GITHUB_RUN_ID"]}-{os.environ["GITHUB_RUN_ATTEMPT"]}'
        title = f"Update orbit {version} {correlation}"
        try:
            dispatched = invoke([
                "workflow", "run", "update.yml", "--repo", repository, "--ref", "main",
                "-f", "project=orbit", "-f", f"version={version}", "-f", f"correlation={correlation}",
            ], timeout)
        except TimeoutError as error:
            raise SyncError("Dispatch response timed out; inspect accepted work before recovery; do not redispatch") from error
        output = dispatched.stdout + dispatched.stderr
        print(output, end="", flush=True)
        if dispatched.returncode:
            raise SyncError("Package updater dispatch failed; inspect accepted work before retrying")
        urls = re.findall(r"https://github\.com/([^\s/]+/[^\s/]+)/actions/runs/([0-9]+)\b", output)
        if urls:
            if len(urls) != 1 or urls[0][0] != repository:
                raise SyncError("Dispatch returned an invalid updater URL")
            run_id = urls[0][1]
        else:
            for attempt in range(attempts):
                try:
                    runs = read(f"repos/{repository}/actions/workflows/update.yml/runs?event=workflow_dispatch&branch=main&per_page=50", timeout)
                    if not isinstance(runs, dict) or not isinstance(runs.get("workflow_runs"), list):
                        raise SyncError("GitHub returned an invalid run list")
                    matches = [run for run in runs["workflow_runs"] if run.get("display_title") == title]
                    if len(matches) > 1:
                        raise SyncError("More than one updater matches the dispatch")
                    if matches:
                        run_id = str(matches[0].get("id", ""))
                        if not re.fullmatch(r"[1-9][0-9]*", run_id):
                            raise SyncError("GitHub returned an invalid updater ID")
                        break
                except TimeoutError as error:
                    print(f"Discovery read: {error}", file=sys.stderr)
                if attempt + 1 < attempts:
                    time.sleep(min(delay, max(0, DEADLINE - time.monotonic())))
            if not run_id:
                raise SyncError("Updater did not start within the discovery budget; inspect accepted work before recovery")
    url = f"https://github.com/{repository}/actions/runs/{run_id}"
    print(f"Observing accepted updater: {url}", flush=True)
    for attempt in range(attempts):
        try:
            data = read(f"repos/{repository}/actions/runs/{run_id}", timeout)
            run_identity(data, repository, run_id, version, title)
            status, conclusion = data.get("status"), data.get("conclusion")
            if status == "completed":
                if conclusion != "success":
                    raise SyncError(f"Updater finished with {conclusion!r}: {url}")
                print(f"Updater succeeded: {url}")
                return
            if status not in ("queued", "in_progress", "waiting", "pending", "requested") or conclusion is not None:
                raise SyncError(f"Updater returned invalid status/conclusion: {url}")
        except TimeoutError as error:
            print(f"Observation read: {error}: {url}", file=sys.stderr)
        if attempt + 1 < attempts:
            time.sleep(min(delay, max(0, DEADLINE - time.monotonic())))
    raise SyncError(f"Updater observation exhausted; resume this exact run without redispatch: {url}")


if __name__ == "__main__":
    try:
        main()
    except (SyncError, KeyError, ValueError, TypeError, AttributeError) as error:
        print(f"Package sync failed: {error}", file=sys.stderr)
        sys.exit(1)
