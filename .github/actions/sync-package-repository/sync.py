#!/usr/bin/env python3
"""Dispatch or resume one Orbit package updater and observe its exact result."""

import json
import os
import re
import subprocess
import sys
import time

from observation import CheckpointError, Observation, mismatch, positive, STATUS, CONCLUSION


DEADLINE = None


class SyncError(Exception):
    def __init__(self, message, reason='invalid_response', mismatches=None):
        super().__init__(message)
        self.reason = reason
        self.mismatches = mismatches or []


def invoke(arguments, timeout):
    remaining = DEADLINE - time.monotonic()
    if remaining <= 0:
        raise SyncError("Package sync exceeded its 10-minute total budget; inspect the accepted run before recovery", "budget_exhausted")
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
        raise SyncError(f"GitHub read failed: HTTP {code}: {response.stderr.strip()}", "request_denied")
    try:
        return json.loads(body)
    except ValueError as error:
        raise SyncError("GitHub returned invalid JSON") from error


def run_identity(data, repository, run_id, version, title):
    if not isinstance(data, dict):
        raise SyncError("Updater identity is invalid")
    repo = data.get('repository')
    values = {'id': data.get('id'), 'repository': repo.get('full_name') if isinstance(repo, dict) else repo,
              'path': data.get('path'), 'head_branch': data.get('head_branch'),
              'event': data.get('event'), 'display_title': data.get('display_title')}
    expected = {'id': int(run_id), 'repository': repository, 'path': '.github/workflows/update.yml',
                'head_branch': 'main', 'event': 'workflow_dispatch'}
    rows = [mismatch(field, value) for field, value in values.items() if field in expected and
            (value != expected[field] or (field == 'repository' and not isinstance(repo, dict)))]
    observed_title = values['display_title']
    expected_title = rf"Update orbit {re.escape(version)} [0-9]+-[0-9]+"
    if (not isinstance(observed_title, str) or
            (observed_title != title if title else not re.fullmatch(expected_title, observed_title))):
        rows.append(mismatch('display_title', observed_title))
    if rows:
        fields = ', '.join(row['field'] for row in rows)
        raise SyncError("Updater does not match repository, workflow, main branch, Orbit project and version: " + fields,
                        'identity_mismatch', rows)


def execute(result):
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
    producer = None
    title = None
    if run_id:
        if not re.fullmatch(r"[1-9][0-9]*", run_id):
            raise SyncError("Existing run ID must be a positive integer")
    if not run_id and result.value is None:
        producer = {'run_id': os.environ['GITHUB_RUN_ID'], 'attempt': os.environ['GITHUB_RUN_ATTEMPT']}
        if not all(positive(value) for value in producer.values()):
            raise SyncError('Invalid dispatch producer')
    run_id, producer = result.prepare(run_id, producer)
    if producer:
        title = f"Update orbit {version} {producer['run_id']}-{producer['attempt']}"
    if not run_id:
        correlation = f"{producer['run_id']}-{producer['attempt']}"
        title = f"Update orbit {version} {correlation}"
        try:
            dispatched = invoke([
                "workflow", "run", "update.yml", "--repo", repository, "--ref", "main",
                "-f", "project=orbit", "-f", f"version={version}", "-f", f"correlation={correlation}",
            ], timeout)
        except TimeoutError as error:
            raise SyncError("Dispatch response timed out; inspect accepted work before recovery; do not redispatch", "dispatch_unknown") from error
        output = dispatched.stdout + dispatched.stderr
        print(output, end="", flush=True)
        if dispatched.returncode:
            raise SyncError("Package updater dispatch failed; inspect accepted work before retrying", "dispatch_unknown")
        result.transition('dispatch', 'dispatch_unknown', acknowledged=True)
        urls = re.findall(r"https://github\.com/([^\s/]+/[^\s/]+)/actions/runs/([0-9]+)\b", output)
        if urls:
            if len(urls) != 1 or urls[0][0] != repository:
                raise SyncError("Dispatch returned an invalid updater URL", "dispatch_unknown")
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
                raise SyncError("Updater did not start within the discovery budget; inspect accepted work before recovery", "discovery_exhausted")
    url = f"https://github.com/{repository}/actions/runs/{run_id}"
    print(f"Observing accepted updater: {url}", flush=True)
    result.transition('selected', 'selected_unverified', run_id=run_id)
    for attempt in range(attempts):
        try:
            data = read(f"repos/{repository}/actions/runs/{run_id}", timeout)
            run_identity(data, repository, run_id, version, title)
            status, conclusion = data.get("status"), data.get("conclusion")
            safe_observation = {'status': status, 'conclusion': conclusion} if (
                isinstance(status, str) and status in STATUS and
                (conclusion is None or isinstance(conclusion, str) and conclusion in CONCLUSION)) else None
            result.transition('accepted', 'accepted', observation=safe_observation)
            if status == "completed":
                if conclusion != "success":
                    raise SyncError(f"Updater finished with {conclusion!r}: {url}", "updater_failed")
                result.transition('success', 'updater_success', observation={'status': status, 'conclusion': conclusion})
                print(f"Updater succeeded: {url}")
                return
            if status not in ("queued", "in_progress", "waiting", "pending", "requested") or conclusion is not None:
                raise SyncError(f"Updater returned invalid status/conclusion: {url}")
            result.transition('observing', 'observing', observation={'status': status, 'conclusion': conclusion})
        except TimeoutError as error:
            print(f"Observation read: {error}: {url}", file=sys.stderr)
        if attempt + 1 < attempts:
            time.sleep(min(delay, max(0, DEADLINE - time.monotonic())))
    raise SyncError(f"Updater observation exhausted; resume this exact run without redispatch: {url}", "observation_exhausted")


def main():
    result = Observation(os.environ.get('SYNC_RESULT_PATH'), os.environ['REPOSITORY'], os.environ['VERSION'])
    try:
        try:
            execute(result)
        except SyncError as error:
            if result.value is not None:
                state = 'identity_mismatch' if error.reason == 'identity_mismatch' else 'failure'
                result.transition(state, error.reason, observation=result.value['observation'], mismatches=error.mismatches)
            raise
        except (KeyError, ValueError, TypeError, AttributeError) as error:
            if result.value is not None:
                result.transition('failure', 'invalid_response')
            raise SyncError('Updater returned invalid response data') from error
    finally:
        result.close()


if __name__ == "__main__":
    try:
        main()
    except (SyncError, CheckpointError, OSError, KeyError, ValueError, TypeError, AttributeError) as error:
        print(f"Package sync failed: {error}", file=sys.stderr)
        sys.exit(1)
