import concurrent.futures
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest

import flow_measure as flow
from flow_report import export_report


SOURCE = 'a' * 40
FINGERPRINT = 'b' * 64


def at(seconds):
    return '2026-01-01T00:00:{:02d}+00:00'.format(seconds)


def journal():
    return dict(schema=flow.SCHEMA, repository='iml885203/orbit', issue=293, started_at=at(0),
                prior_requirements='unobserved', mutation_authority=False, events=[])


def checkpoint(identifier=1, attempt=1, commit=SOURCE, result='failure'):
    return dict(schema='orbit.delivery.v1', repository='iml885203/orbit', issues=[{'number': 293}],
                pr_ci=dict(run_id=identifier, commit=commit, jobs=[dict(id=identifier, attempt=attempt,
                    name='preflight', created_at=at(1), started_at=at(3), completed_at=at(7), conclusion=result)]))


class FlowEvidenceTest(unittest.TestCase):
    def test_parallel_intervals_do_not_inflate_elapsed_and_tracks_are_independent(self):
        value = journal()
        flow.import_span(value, 'dev', at(0), at(10), 'development', 'author', SOURCE, 'Develop', 'passed', 'agent-reported')
        flow.import_span(value, 'review', at(5), at(15), 'review', 'reviewer', SOURCE, 'Review', 'passed', 'agent-reported')
        with tempfile.TemporaryDirectory() as directory:
            summary = export_report(value, flow.validate(value), Path(directory), at(20))
            self.assertEqual(summary['window_seconds'], 20)
            self.assertEqual(summary['completed_interval_union_seconds'], 15)
            self.assertEqual(summary['without_completed_interval_evidence_seconds'], 5)
            self.assertEqual(summary['stage_union_seconds'], {'development': 10, 'review': 10})
            self.assertEqual(summary['uncovered_intervals'], [{'start_seconds': 15, 'end_seconds': 20}])
            events = json.loads((Path(directory)/'trace.json').read_text())['traceEvents']
            spans = [e for e in events if e['ph'] == 'X']
            self.assertEqual(len({e['tid'] for e in spans}), 2)
            self.assertEqual([e['dur'] for e in spans], [10_000_000, 10_000_000])

    def test_incomplete_and_clock_inconsistency_never_look_finished(self):
        value = journal()
        identifier = flow.begin(value, 'review', 'peer', SOURCE, 'Pending review', at=at(5))
        with tempfile.TemporaryDirectory() as directory:
            report = export_report(value, flow.validate(value), Path(directory), at(20))
            self.assertEqual(report['unfinished_spans'], [identifier])
            self.assertEqual(report['completed_interval_union_seconds'], 0)
            with self.assertRaises(ValueError):
                export_report(value, flow.validate(value), Path(directory), at(2))
        with self.assertRaises(ValueError):
            flow.end(value, identifier, 'passed', at=at(4))

    def test_incremental_import_retains_failed_sources_and_attempts(self):
        value = journal()
        first = checkpoint()
        flow.import_delivery(value, first)
        flow.import_delivery(value, first)
        flow.import_delivery(value, checkpoint(2, 2, 'c'*40, 'success'))
        spans = flow.validate(value)
        executions = [s for s in spans.values() if s['stage'] == 'ci']
        self.assertEqual(len(executions), 2)
        self.assertEqual({s['result'] for s in executions}, {'passed', 'failed'})
        self.assertEqual({s['source'] for s in executions}, {SOURCE, 'c'*40})

    def test_pending_observation_can_finish_without_duplicate_execution(self):
        value, pending = journal(), checkpoint()
        pending['pr_ci']['jobs'][0]['completed_at'] = None
        flow.import_delivery(value, pending)
        flow.import_delivery(value, checkpoint())
        self.assertEqual(len([s for s in flow.validate(value).values() if s['stage'] == 'ci']), 1)
        incompatible = checkpoint(result='success')
        with self.assertRaises(ValueError):
            flow.import_delivery(value, incompatible)

    def test_foreign_issue_or_repository_rejected(self):
        for field, wrong in [('repository', 'other/repo'), ('issues', [{'number': 294}])]:
            report = checkpoint()
            report[field] = wrong
            with self.assertRaises(ValueError):
                flow.import_delivery(journal(), report)

    def test_reuse_is_not_a_new_execution_and_legacy_time_is_not_invented(self):
        value = journal()
        report = dict(schema='orbit.dev-check.v1', run='run-one', inputs={'source': FINGERPRINT, 'mode': 'full'},
                      status='passed', reused=False, started_at=at(2), completed_at=at(8))
        flow.import_check(value, report)
        reused = dict(report, reused=True, invoked_at=at(12))
        flow.import_check(value, reused)
        flow.import_check(value, reused)
        flow.import_check(value, dict(report, run='older', started_at=None))
        self.assertEqual(len(flow.validate(value)), 1)
        self.assertEqual(len([e for e in value['events'] if e['type'] == 'note']), 2)

    def test_note_only_trace_and_future_reuse_clock_are_explicit(self):
        value = journal()
        report = dict(schema='orbit.dev-check.v1', run='run-one', inputs={'source': FINGERPRINT},
                      reused=True, invoked_at=at(12))
        flow.import_check(value, report)
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(ValueError):
                export_report(value, flow.validate(value), Path(directory), at(10))
            summary = export_report(value, flow.validate(value), Path(directory), at(20))
            events = json.loads((Path(directory)/'trace.json').read_text())['traceEvents']
            self.assertEqual(len([e for e in events if e['ph'] == 'i']), 1)
            self.assertEqual(summary['completed_interval_union_seconds'], 0)

    def test_bad_source_duplicate_finish_and_malformed_history_rejected(self):
        for source in ['main', '../other', 'a'*39]:
            with self.assertRaises(ValueError):
                flow.begin(journal(), 'review', 'peer', source, 'Review', at=at(1))
        value = journal()
        identifier = flow.begin(value, 'rework', 'author', SOURCE, 'Fix finding', reason='review finding', at=at(2))
        flow.end(value, identifier, 'failed', at=at(3))
        with self.assertRaises(ValueError):
            flow.end(value, identifier, 'passed', at=at(4))
        with self.assertRaises(ValueError):
            flow.validate([])


class FlowCLITest(unittest.TestCase):
    def setUp(self):
        self.storage = tempfile.TemporaryDirectory()
        self.addCleanup(self.storage.cleanup)
        self.workspace = Path(self.storage.name)
        self.script = Path(flow.__file__).resolve()
        self.call('init', '--issue', '293')

    def argv(self, *args):
        return [sys.executable, str(self.script), '--workspace', str(self.workspace), *args]

    def call(self, *args, expected=0):
        result = subprocess.run(self.argv(*args), capture_output=True, text=True, timeout=15)
        self.assertEqual(result.returncode, expected, result.stderr)
        return json.loads(result.stdout) if expected == 0 and result.stdout else result

    def phase(self, command='begin'):
        return [command, '--stage', 'testing', '--actor', 'test', '--source', SOURCE, '--label', 'Fixture validation']

    def test_resume_parallel_writers_and_closed_session(self):
        def record(index):
            result = subprocess.run(self.argv('begin', '--stage', 'review', '--actor', 'peer-'+str(index),
                '--source', SOURCE, '--label', 'Independent review'), capture_output=True, text=True, timeout=15)
            self.assertEqual(result.returncode, 0, result.stderr)
            return json.loads(result.stdout)['span']
        with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
            spans = list(pool.map(record, range(4)))
        self.call('finish', expected=1)
        for identifier in spans:
            self.call('end', '--span', identifier, '--result', 'passed')
        self.call('finish')
        summary = self.call('export')
        self.assertEqual(summary['unfinished_spans'], [])
        self.assertFalse(summary['mutation_authority'])
        self.call(*self.phase(), expected=1)
        self.call('init', '--issue', '293', expected=1)

    def test_command_failure_is_recorded_without_arguments(self):
        self.call(*self.phase('run'), '--', sys.executable, '-c', 'raise SystemExit(7)', expected=7)
        value = json.loads((self.workspace/'flow/journal.json').read_text())
        self.assertEqual(list(flow.validate(value).values())[0]['result'], 'failed')
        self.assertNotIn('SystemExit', json.dumps(value))

    @unittest.skipIf(os.name == 'nt', 'Unix process-group cancellation')
    def test_command_cancel_stops_owned_child_and_records_cancellation(self):
        for sig in (signal.SIGINT, signal.SIGTERM):
            with self.subTest(signal=sig):
                ready = self.workspace/('ready-'+str(sig))
                code = 'from pathlib import Path; import time,os; Path('+repr(str(ready))+').write_text(str(os.getpid())); time.sleep(60)'
                process = subprocess.Popen(self.argv(*self.phase('run'), '--', sys.executable, '-c', code),
                                           stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
                try:
                    deadline = time.monotonic()+8
                    while not ready.exists() and time.monotonic() < deadline:
                        time.sleep(.02)
                    self.assertTrue(ready.exists())
                    child_pid = int(ready.read_text())
                    process.send_signal(sig)
                    process.communicate(timeout=8)
                    self.assertEqual(process.returncode, 128 + sig)
                    with self.assertRaises(ProcessLookupError):
                        os.kill(child_pid, 0)
                    value = json.loads((self.workspace/'flow/journal.json').read_text())
                    self.assertEqual(list(flow.validate(value).values())[-1]['result'], 'cancelled')
                finally:
                    if process.poll() is None:
                        process.kill()
                        process.communicate()

    def test_invalid_operations_do_not_replace_existing_journal(self):
        path = self.workspace/'flow/journal.json'
        original = path.read_bytes()
        self.call('end', '--span', 'missing', '--result', 'passed', expected=1)
        self.assertEqual(path.read_bytes(), original)
        foreign = self.workspace/'foreign.json'
        foreign.write_text(json.dumps(checkpoint()))
        self.call('import-delivery', '--report', str(foreign), expected=1)
        self.assertEqual(path.read_bytes(), original)

    def test_source_and_symlink_outputs_rejected_before_writes(self):
        source = self.script.parent.parent
        result = subprocess.run([sys.executable, str(self.script), '--workspace', str(source), 'init', '--issue', '293'],
                                capture_output=True, timeout=10)
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((source/'flow').exists())
        protected = self.workspace/'protected'
        protected.write_text('preserve')
        (self.workspace/'flow/trace.json.new').symlink_to(protected)
        self.call('export', expected=1)
        self.assertEqual(protected.read_text(), 'preserve')


if __name__ == '__main__':
    unittest.main()
