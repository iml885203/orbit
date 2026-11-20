#!/usr/bin/env python3
"""Exercise the shared action using a fake GitHub transport."""

import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
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
    if fixture.get("checkpoint_write_failure"):
        checkpoint = pathlib.Path(os.environ["SYNC_RESULT_PATH"])
        checkpoint.unlink()
        checkpoint.mkdir()
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
                     "EXISTING_RUN_ID": existing, "SYNC_RESULT_PATH": "", "SYNC_POLL_ATTEMPTS": "3",
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


class PackageObservationContract(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        # Transport records only the proposed checkpoint, never remote response bodies.
        fake = FAKE_GH.replace('fixture = json.loads',
            'checkpoint = pathlib.Path(os.environ["SYNC_RESULT_PATH"])\n'
            'with (root / "snapshots").open("a") as out:\n'
            '    out.write(json.dumps(json.loads(checkpoint.read_text()) if checkpoint.exists() else None) + "\\n")\n'
            'fixture = json.loads')
        gh = self.root / 'gh'
        gh.write_text(fake)
        gh.chmod(0o755)
        self.environment = {**os.environ, 'FIXTURE_ROOT': str(self.root), 'SYNC_GH_BIN': str(gh),
            'SYNC_RESULT_PATH': str(self.root / 'result.json'), 'REPOSITORY': 'owner/packages',
            'VERSION': 'v1.5.0', 'GITHUB_RUN_ID': '123', 'GITHUB_RUN_ATTEMPT': '1',
            'EXISTING_RUN_ID': '', 'SYNC_POLL_ATTEMPTS': '3', 'SYNC_POLL_DELAY_SECONDS': '0',
            'SYNC_REQUEST_TIMEOUT_SECONDS': '2'}

    def run_observer(self, replies=None, **fixture):
        (self.root / 'fixture').write_text(json.dumps({'run': replies or [updater()], **fixture}))
        return subprocess.run([str(SYNC)], env=self.environment, text=True, capture_output=True)

    def result(self):
        raw = (self.root / 'result.json').read_bytes()
        self.assertLessEqual(len(raw), 16384)
        value = json.loads(raw)
        self.assertEqual(value['schema'], 'orbit.package-observation.v1')
        return value

    def calls(self):
        return [json.loads(line) for line in (self.root / 'calls').read_text().splitlines()]

    def test_selected_checkpoint_precedes_detail_get_but_is_not_verified(self):
        process = self.run_observer()
        self.assertEqual(process.returncode, 0, process.stderr)
        snapshots = [json.loads(line) for line in (self.root / 'snapshots').read_text().splitlines()]
        self.assertEqual(snapshots[0]['state'], 'dispatch')
        self.assertEqual(snapshots[1]['state'], 'selected')
        self.assertFalse(snapshots[1]['identity_verified'])
        self.assertEqual(snapshots[1]['selected_run']['id'], '111')
        value = self.result()
        self.assertEqual(value['state'], 'success')
        self.assertEqual(value['reason_code'], 'updater_success')
        self.assertTrue(value['identity_verified'])
        # success describes updater only; no fabricated formula/release verification.
        self.assertNotIn('manifest_verified', value)

    def test_resume_reobserves_selected_run_without_dispatch(self):
        self.assertEqual(self.run_observer().returncode, 0)
        before = len(self.calls())
        second = self.run_observer([updater(head_branch='foreign')])
        self.assertNotEqual(second.returncode, 0)
        self.assertEqual(self.calls()[before:], [['api', '--include', 'repos/owner/packages/actions/runs/111']])
        value = self.result()
        self.assertEqual(value['state'], 'identity_mismatch')
        self.assertEqual(value['reason_code'], 'identity_mismatch')
        self.assertEqual([row['field'] for row in value['mismatches']], ['head_branch'])
        self.assertFalse(value['identity_verified'])

    def test_malformed_identity_is_bounded_and_raw_body_not_retained(self):
        sentinel = 'DO_NOT_RETAIN_PRIVATE_BODY'
        changed = updater(display_title={'private': sentinel}, head_branch='x' * 100000)
        process = self.run_observer([changed])
        self.assertNotEqual(process.returncode, 0)
        self.assertEqual(self.result()['state'], 'identity_mismatch')
        raw = (self.root / 'result.json').read_text()
        self.assertNotIn(sentinel, raw)
        self.assertNotIn('x' * 257, raw)

    def test_short_secret_shaped_values_never_enter_mismatch_artifact(self):
        sentinel = 'ghp_PRIVATE_SHORT'
        for change in ({'id': sentinel}, {'repository': {'full_name': sentinel}},
                       {'path': sentinel}, {'head_branch': sentinel},
                       {'event': sentinel}, {'display_title': sentinel}):
            with self.subTest(field=next(iter(change))):
                (self.root / 'result.json').unlink(missing_ok=True)
                process = self.run_observer([updater(**change)])
                self.assertNotEqual(process.returncode, 0)
                self.assertEqual(self.result()['state'], 'identity_mismatch')
                self.assertNotIn(sentinel, (self.root / 'result.json').read_text())

    def test_oversize_restored_checkpoint_performs_no_external_actions(self):
        # The sentinel is intentionally invalid JSON; size rejection must precede decoding.
        (self.root / 'result.json').write_bytes(b'x' * 16385)
        process = self.run_observer()
        self.assertNotEqual(process.returncode, 0)
        self.assertFalse((self.root / 'calls').exists())

    def test_conflicting_repository_checkpoint_has_no_requests(self):
        self.assertEqual(self.run_observer().returncode, 0)
        before = len(self.calls())
        self.environment['REPOSITORY'] = 'other/packages'
        self.assertNotEqual(self.run_observer().returncode, 0)
        self.assertEqual(len(self.calls()), before)

    def test_unknown_dispatch_cannot_redispatch_on_restart(self):
        first = self.run_observer(dispatch_timeout=True)
        self.assertNotEqual(first.returncode, 0)
        self.assertEqual(self.result()['reason_code'], 'dispatch_unknown')
        before = len(self.calls())
        second = self.run_observer()
        self.assertNotEqual(second.returncode, 0)
        self.assertEqual(len(self.calls()), before)
        self.assertEqual(self.result()['recovery']['action'], 'inspect_dispatch')



    def test_explicit_existing_id_needs_no_dispatch_producer(self):
        self.environment['EXISTING_RUN_ID'] = '111'
        self.environment.pop('GITHUB_RUN_ID')
        self.environment.pop('GITHUB_RUN_ATTEMPT')
        process = self.run_observer([updater(display_title='Update orbit v1.5.0 999-2')])
        self.assertEqual(process.returncode, 0, process.stderr)
        self.assertEqual(self.calls(), [['api', '--include', 'repos/owner/packages/actions/runs/111']])
        self.assertIsNone(self.result()['producer'])
        self.assertFalse(self.result()['dispatch_acknowledged'])

    def test_restoration_preserves_original_dispatch_title(self):
        self.assertEqual(self.run_observer().returncode, 0)
        self.environment.update(GITHUB_RUN_ID='999', GITHUB_RUN_ATTEMPT='2')
        process = self.run_observer()
        self.assertEqual(process.returncode, 0, process.stderr)
        self.assertEqual(self.result()['producer'], {'run_id': '123', 'attempt': '1'})
        process = self.run_observer([updater(display_title='Update orbit v1.5.0 999-2')])
        self.assertNotEqual(process.returncode, 0)
        self.assertEqual(self.result()['reason_code'], 'identity_mismatch')

    def test_conflicting_or_invalid_checkpoint_cannot_make_requests(self):
        self.assertEqual(self.run_observer().returncode, 0)
        valid = self.result()
        invalid = []
        for field, value in [('schema', 'other'), ('version', 'v2.0.0'), ('state', 'unknown'),
                             ('identity_verified', 'true'), ('producer', {'run_id': 'bad', 'attempt': '1'}),
                             ('selected_run', {'id': '0', 'url': 'https://evil.example'}),
                             ('mismatches', [{'field': 'private', 'expected': 'x', 'observed': 'x'}])]:
            invalid.append({**valid, field: value})
        for value in invalid:
            with self.subTest(value=value):
                (self.root / 'result.json').write_text(json.dumps(value))
                before = len(self.calls())
                self.assertNotEqual(self.run_observer().returncode, 0)
                self.assertEqual(len(self.calls()), before)
        (self.root / 'result.json').write_text(json.dumps(valid))
        self.environment['EXISTING_RUN_ID'] = '222'
        before = len(self.calls())
        self.assertNotEqual(self.run_observer().returncode, 0)
        self.assertEqual(len(self.calls()), before)

    def test_unknown_top_level_fields_are_ignored_and_not_reemitted(self):
        self.assertEqual(self.run_observer().returncode, 0)
        value = self.result()
        value['future_field'] = 'DO_NOT_RETAIN_PRIVATE_BODY'
        (self.root / 'result.json').write_text(json.dumps(value))
        before = len(self.calls())
        process = self.run_observer()
        self.assertEqual(process.returncode, 0, process.stderr)
        self.assertEqual(self.calls()[before:], [['api', '--include', 'repos/owner/packages/actions/runs/111']])
        self.assertNotIn('future_field', self.result())
        self.assertNotIn('DO_NOT_RETAIN_PRIVATE_BODY', (self.root / 'result.json').read_text())

    def test_result_write_failure_prevents_dispatch(self):
        # Existing parent and lock fit NAME_MAX; atomic temporary filename cannot fit.
        self.environment['SYNC_RESULT_PATH'] = str(self.root / ('r' * 250))
        process = self.run_observer()
        self.assertNotEqual(process.returncode, 0)
        self.assertFalse((self.root / 'calls').exists())

    def test_result_write_failure_after_dispatch_retains_exact_url_in_logs(self):
        process = self.run_observer(checkpoint_write_failure=True)
        self.assertNotEqual(process.returncode, 0)
        self.assertIn('https://github.com/owner/packages/actions/runs/111', process.stdout)
        self.assertEqual(len(self.calls()), 1)

    def test_discovery_persists_selected_record_before_detail_read(self):
        process = self.run_observer(dispatch='', list=[{'body': {'workflow_runs': [updater()['body']]}}])
        self.assertEqual(process.returncode, 0, process.stderr)
        snapshots = [json.loads(line) for line in (self.root / 'snapshots').read_text().splitlines()]
        self.assertEqual(snapshots[1]['state'], 'dispatch')
        self.assertTrue(snapshots[1]['dispatch_acknowledged'])
        self.assertIsNone(snapshots[1]['selected_run'])
        self.assertEqual(snapshots[2]['state'], 'selected')
        self.assertFalse(snapshots[2]['identity_verified'])

    def test_unknown_dispatch_can_be_reconciled_with_explicit_id(self):
        self.assertNotEqual(self.run_observer(dispatch_timeout=True).returncode, 0)
        before = len(self.calls())
        self.environment['EXISTING_RUN_ID'] = '111'
        process = self.run_observer()
        self.assertEqual(process.returncode, 0, process.stderr)
        self.assertEqual(self.calls()[before:], [['api', '--include', 'repos/owner/packages/actions/runs/111']])
        self.assertEqual(self.result()['producer'], {'run_id': '123', 'attempt': '1'})

    def test_failure_verdict_retains_only_allowlisted_observation(self):
        for reply, reason in [(updater(conclusion='failure'), 'updater_failed'),
                              ({'code': 403}, 'request_denied'),
                              ({'raw': 'DO_NOT_RETAIN_PRIVATE_BODY'}, 'invalid_response'),
                              (updater(status='in_progress', conclusion=None), 'observation_exhausted')]:
            with self.subTest(reason=reason):
                (self.root / 'result.json').unlink(missing_ok=True)
                process = self.run_observer([reply])
                self.assertNotEqual(process.returncode, 0)
                value = self.result()
                self.assertEqual(value['state'], 'failure')
                self.assertEqual(value['reason_code'], reason)
                self.assertFalse(value['identity_verified'])
                self.assertNotIn('DO_NOT_RETAIN_PRIVATE_BODY', (self.root / 'result.json').read_text())

    def test_nested_repository_malformed_types_fail_with_safe_marker(self):
        for repository in (None, [], 'ghp_PRIVATE_SHORT', {'full_name': ['ghp_PRIVATE_SHORT']}):
            with self.subTest(repository=repository):
                (self.root / 'result.json').unlink(missing_ok=True)
                process = self.run_observer([updater(repository=repository)])
                self.assertNotEqual(process.returncode, 0)
                value = self.result()
                self.assertEqual(value['reason_code'], 'identity_mismatch')
                self.assertEqual([row['field'] for row in value['mismatches']], ['repository'])
                self.assertNotIn('ghp_PRIVATE_SHORT', (self.root / 'result.json').read_text())


    def test_same_checkpoint_cannot_dispatch_concurrently(self):
        (self.root / 'fixture').write_text(json.dumps({'run': [updater()], 'dispatch_timeout': True}))
        first = subprocess.Popen([str(SYNC)], env=self.environment, text=True,
                                 stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            deadline = time.monotonic() + 2
            while not (self.root / 'calls').exists() and time.monotonic() < deadline:
                time.sleep(0.01)
            self.assertTrue((self.root / 'calls').exists())
            second = subprocess.run([str(SYNC)], env=self.environment, text=True, capture_output=True)
            self.assertNotEqual(second.returncode, 0)
            self.assertEqual(len(self.calls()), 1)
            first.communicate(timeout=5)
            self.assertNotEqual(first.returncode, 0)
            self.assertEqual(self.result()['reason_code'], 'dispatch_unknown')
        finally:
            if first.poll() is None:
                first.kill()
                first.communicate(timeout=5)

    def test_nonregular_checkpoint_is_rejected_without_requests(self):
        (self.root / 'result.json').mkdir()
        self.assertNotEqual(self.run_observer().returncode, 0)
        self.assertFalse((self.root / 'calls').exists())



if __name__ == "__main__":
    unittest.main()
