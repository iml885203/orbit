#!/usr/bin/env python3
"""Exercise the shared action using a fake GitHub transport."""

import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parent.parent
SYNC = ROOT / ".github/actions/sync-package-repository/sync.sh"
FAKE_GH = '''#!/usr/bin/env python3
import json, os, pathlib, sys, time
root = pathlib.Path(os.environ["FIXTURE_ROOT"])
with (root / "calls").open("a") as out:
    out.write(json.dumps(sys.argv[1:]) + "\\n")
fixture = json.loads((root / "fixture").read_text())
if sys.argv[1:3] == ["workflow", "run"]:
    if fixture.get("dispatch_timeout"):
        time.sleep(3)
    print(fixture.get("dispatch", "https://github.com/owner/packages/actions/runs/111"))
    sys.exit(fixture.get("dispatch_exit", 0))
if sys.argv[1:3] != ["api", "--include"]:
    sys.exit(7)
kind = "list" if "?" in sys.argv[3] else "run"
count_path = root / (kind + "_count")
count = int(count_path.read_text()) if count_path.exists() else 0
count_path.write_text(str(count + 1))
replies = fixture[kind]
reply = replies[min(count, len(replies) - 1)]
if reply.get("timeout"):
    time.sleep(3)
code = reply.get("code", 200)
print("HTTP/2.0 " + str(code) + " Test\\nContent-Type: application/json\\n")
print(reply.get("raw", json.dumps(reply.get("body"))))
sys.exit(0 if code == 200 else 1)
'''


def updater(**changes):
    data = {
        "id": 111, "repository": {"full_name": "owner/packages"},
        "path": ".github/workflows/update.yml", "head_branch": "main",
        "event": "workflow_dispatch", "display_title": "Update orbit v1.5.0 123-1",
        "status": "completed", "conclusion": "success",
    }
    data.update(changes)
    return {"body": data}


class PackageSync(unittest.TestCase):
    def run_sync(self, replies=None, existing="", **fixture):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            gh = root / "gh"
            gh.write_text(FAKE_GH)
            gh.chmod(0o755)
            (root / "fixture").write_text(json.dumps({"run": replies or [updater()], **fixture}))
            result = subprocess.run(
                [str(SYNC)], text=True, capture_output=True, check=False,
                env={**os.environ, "FIXTURE_ROOT": directory, "SYNC_GH_BIN": str(gh),
                     "REPOSITORY": "owner/packages", "VERSION": "v1.5.0",
                     "GITHUB_RUN_ID": "123", "GITHUB_RUN_ATTEMPT": "1",
                     "EXISTING_RUN_ID": existing, "SYNC_POLL_ATTEMPTS": "3",
                     "SYNC_POLL_DELAY_SECONDS": "0", "SYNC_REQUEST_TIMEOUT_SECONDS": "2"},
            )
            calls = [json.loads(line) for line in (root / "calls").read_text().splitlines()] if (root / "calls").exists() else []
            return result, calls

    def assert_failed(self, result, message):
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertIn(message, result.stderr)

    def test_url_first_observes_exact_run_after_transient_reads(self):
        for reply in ({"code": 404}, {"code": 500}, {"code": 502}, {"code": 503}, {"code": 504}, {"timeout": True}):
            with self.subTest(reply=reply):
                result, calls = self.run_sync([reply, updater()])
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(sum(call[:2] == ["workflow", "run"] for call in calls), 1)
                self.assertTrue(all(call[2].endswith("/111") for call in calls if call[0] == "api"))
                self.assertIn("https://github.com/owner/packages/actions/runs/111", result.stdout)

    def test_list_fallback_waits_for_own_title(self):
        result, calls = self.run_sync(dispatch="", list=[{"body": {"workflow_runs": []}}, {"body": {"workflow_runs": [updater()["body"]]}}])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(sum("?" in call[2] for call in calls if call[0] == "api"), 2)

    def test_queued_run_reaches_success(self):
        result, calls = self.run_sync([
            updater(status="queued", conclusion=None),
            updater(status="in_progress", conclusion=None), updater(),
        ])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(calls), 4)

    def test_dispatch_timeout_does_not_retry(self):
        result, calls = self.run_sync(dispatch_timeout=True)
        self.assert_failed(result, "Dispatch response timed out")
        self.assertEqual(len(calls), 1)

    def test_discovery_exhaustion(self):
        result, calls = self.run_sync(dispatch="", list=[{"body": {"workflow_runs": []}}])
        self.assert_failed(result, "discovery budget")
        self.assertEqual(len(calls), 4)

    def test_observation_exhaustion_never_redispatches(self):
        for reply in ({"code": 404}, updater(status="in_progress", conclusion=None)):
            result, calls = self.run_sync([reply])
            self.assert_failed(result, "observation exhausted")
            self.assertEqual(len(calls), 4)
            self.assertIn("https://github.com/owner/packages/actions/runs/111", result.stderr)

    def test_terminal_non_success(self):
        for conclusion in ("failure", "cancelled", "timed_out", "skipped", "neutral", None):
            result, calls = self.run_sync([updater(conclusion=conclusion)])
            self.assert_failed(result, "Updater finished")
            self.assertEqual(len(calls), 2)

    def test_unauthorized_rate_limited_and_invalid_reads_are_not_retried(self):
        for reply in ({"code": 401}, {"code": 403}, {"code": 429}, {"raw": "bad json"}, {"body": []}, updater(status="unknown")):
            result, calls = self.run_sync([reply])
            self.assert_failed(result, "Package sync failed")
            self.assertEqual(len(calls), 2)

    def test_recovery_verifies_without_dispatch_or_discovery(self):
        result, calls = self.run_sync([{"code": 404}, updater(display_title="Update orbit v1.5.0 999-2")], existing="111")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(calls), 2)
        self.assertTrue(all(call == ["api", "--include", "repos/owner/packages/actions/runs/111"] for call in calls))

    def test_recovery_rejects_foreign_identity(self):
        for change in ({"id": 222}, {"repository": {"full_name": "other/packages"}}, {"path": ".github/workflows/other.yml"}, {"head_branch": "feature"}, {"event": "push"}, {"display_title": "Update other v1.5.0 123-1"}, {"display_title": "Update orbit v1.4.0 123-1"}, {"display_title": "Update orbit v1.5.0 123-1 extra"}):
            with self.subTest(change=change):
                result, calls = self.run_sync([updater(**change)], existing="111")
                self.assert_failed(result, "Updater does not match")
                self.assertEqual(len(calls), 1)

    def test_invalid_recovery_id_fails_without_requests(self):
        for run_id in ("0", "-1", "111?", "1/2"):
            result, calls = self.run_sync(existing=run_id)
            self.assert_failed(result, "positive integer")
            self.assertEqual(calls, [])

    def test_dispatch_failure_is_not_retried(self):
        result, calls = self.run_sync(dispatch="HTTP 403: denied", dispatch_exit=1)
        self.assert_failed(result, "dispatch failed")
        self.assertEqual(len(calls), 1)

    def test_dispatch_rejects_foreign_url(self):
        result, calls = self.run_sync(dispatch="https://github.com/other/packages/actions/runs/111")
        self.assert_failed(result, "invalid updater URL")
        self.assertEqual(len(calls), 1)

    def test_dispatched_title_must_match_correlation(self):
        result, calls = self.run_sync([updater(display_title="Update orbit v1.5.0 999-1")])
        self.assert_failed(result, "Updater does not match")
        self.assertEqual(len(calls), 2)


if __name__ == "__main__":
    unittest.main()
