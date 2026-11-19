import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock
import zipfile

import candidate
import delivery_status as delivery


def job(name, conclusion='success', number=1):
    return {'id': number, 'name': name, 'status': 'completed', 'conclusion': conclusion,
            'started_at': '2026-11-19T00:00:00Z', 'completed_at': '2026-11-19T00:00:10Z',
            'html_url': 'https://github.com/iml885203/orbit/actions/runs/10/job/' + str(number)}


class ActualJobEvidence(unittest.TestCase):
    def test_failed_job_rerun_retains_same_run_successes_but_new_failure_wins(self):
        run = {'id': 10, 'run_attempt': 2, 'status': 'completed', 'conclusion': 'success'}
        old = [job(name, number=index) for index, name in enumerate(delivery.REQUIRED_CI, 1)]
        for outcome in ('success', 'failure', 'skipped'):
            new = [job('dotnet-journey', outcome, 99)]
            def rows(route, field):
                return new if '/attempts/2/' in route else old
            with mock.patch('delivery_status.pages', side_effect=rows):
                combined = delivery.latest_jobs('iml885203/orbit', run)
            selected = {item['name']: item for item in combined}
            self.assertEqual(selected['preflight']['attempt'], 1)
            self.assertEqual(selected['dotnet-journey']['attempt'], 2)
            if outcome == 'success':
                delivery.require_success(run, combined, delivery.REQUIRED_CI)
            else:
                with self.assertRaises(ValueError):
                    delivery.require_success(run, combined, delivery.REQUIRED_CI)

    def test_partial_pending_and_missing_jobs_never_satisfy_release_gate(self):
        rows = [job(name) for name in delivery.REQUIRED_CI]
        for state in [{'status': 'in_progress', 'conclusion': None}, {'status': 'completed', 'conclusion': 'failure'}]:
            with self.assertRaises(ValueError):
                delivery.require_success(state, rows, delivery.REQUIRED_CI)
        with self.assertRaises(ValueError):
            delivery.require_success({'status': 'completed', 'conclusion': 'success'}, rows[:-1], delivery.REQUIRED_CI)

    def test_new_pending_canonical_run_cannot_use_older_green_run(self):
        commit = 'a' * 40
        older = {'id': 10, 'head_sha': commit, 'event': 'push', 'path': '.github/workflows/ci.yml',
                 'head_branch': 'main', 'status': 'completed', 'conclusion': 'success', 'run_attempt': 1, 'html_url': 'old'}
        newer = dict(older, id=11, status='in_progress', conclusion=None)
        with mock.patch('delivery_status.pages', return_value=[older, newer]), mock.patch('delivery_status.latest_jobs', return_value=[job(n) for n in delivery.REQUIRED_CI]):
            with self.assertRaises(ValueError):
                delivery.ci_evidence('iml885203/orbit', commit, 'push', require=True)

    def test_http_status_is_structured_and_transport_failure_is_not_absence(self):
        missing = subprocess.CompletedProcess([], 1, b'HTTP/2.0 404 Not Found\nHeader: value\r\n\r\n{}', b'')
        with mock.patch('delivery_status.subprocess.run', return_value=missing):
            with self.assertRaises(delivery.NotFound):
                delivery.api('repos/example/repo')
        failed = subprocess.CompletedProcess([], 1, b'', b'private diagnostic')
        with mock.patch('delivery_status.subprocess.run', return_value=failed):
            with self.assertRaises(ValueError) as caught:
                delivery.api('repos/example/repo')
            self.assertNotIsInstance(caught.exception, delivery.NotFound)
            self.assertNotIn('private diagnostic', str(caught.exception))


class CheckpointAndPublication(unittest.TestCase):
    def test_new_head_invalidates_reported_review_checkpoint(self):
        old = {'pr': {'head': 'a' * 40}, 'reported_reviews': [delivery.review_argument('reviewer:' + 'a' * 40 + ':passed')]}
        stale, reviews = delivery.reconcile(old, 'b' * 40, [])
        self.assertTrue(stale)
        self.assertEqual(reviews, [])
        stale, reviews = delivery.reconcile(old, 'a' * 40, [])
        self.assertFalse(stale)
        self.assertIn('not GitHub-verified', reviews[0]['source'])

    def test_api_failure_cannot_replace_checkpoint_with_fresh_success(self):
        with tempfile.TemporaryDirectory(prefix='orbit-delivery-test-') as temporary:
            workspace = Path(temporary).resolve()
            checkpoint = workspace / 'orbit-delivery.json'
            previous = {'schema': 'orbit.delivery.v1', 'repository': 'iml885203/orbit', 'observed_at': 'old'}
            checkpoint.write_text(json.dumps(previous))
            argv = ['delivery_status.py', '--pr', '1', '--repository', 'iml885203/orbit', '--workspace', str(workspace)]
            with mock.patch.object(sys, 'argv', argv), mock.patch('delivery_status.observe', side_effect=OSError('offline')):
                with mock.patch('sys.stderr', new_callable=io.StringIO):
                    self.assertEqual(delivery.main(), 1)
            self.assertEqual(json.loads(checkpoint.read_text()), previous)

    def test_existing_publication_is_reconciled_and_tampering_never_republished(self):
        with tempfile.TemporaryDirectory(prefix='orbit-publication-test-') as temporary:
            directory = Path(temporary)
            value = {'repository': 'iml885203/orbit', 'version': 'v1.4.1', 'commit': 'a' * 40, 'run_id': '10'}
            candidate.save(directory / 'manifest.json', value)
            digest = candidate.sha(directory / 'manifest.json')
            context = {'GITHUB_EVENT_NAME': 'workflow_dispatch', 'GITHUB_REF': 'refs/heads/main',
                       'GITHUB_SHA': 'a' * 40, 'GITHUB_RUN_ID': '10'}
            with mock.patch.dict(os.environ, context):
                with mock.patch('delivery_status.release_evidence', return_value={'state': 'not-published'}):
                    self.assertFalse(delivery.probe_publication('iml885203/orbit', 'v1.4.1', directory, digest))
                accepted = {'state': 'published', 'immutable': True, 'draft': False, 'candidate_binding': 'matched'}
                with mock.patch('delivery_status.release_evidence', return_value=accepted):
                    self.assertTrue(delivery.probe_publication('iml885203/orbit', 'v1.4.1', directory, digest))
                for change in ({'immutable': False}, {'draft': True}, {'candidate_binding': 'mismatch'}):
                    with mock.patch('delivery_status.release_evidence', return_value=dict(accepted, **change)):
                        with self.assertRaises(ValueError):
                            delivery.probe_publication('iml885203/orbit', 'v1.4.1', directory, digest)
            with mock.patch.dict(os.environ, dict(context, GITHUB_REF='refs/heads/feature')):
                with self.assertRaises(ValueError):
                    delivery.probe_publication('iml885203/orbit', 'v1.4.1', directory, digest)


class CandidateIdentityArchive(unittest.TestCase):
    def identity(self, name='candidate-identity.json'):
        manifest = {'schema': 'orbit.candidate.v1', 'repository': 'iml885203/orbit', 'version': 'v1.4.1',
                    'commit': 'a' * 40, 'head_commit': 'a' * 40, 'run_id': '10', 'producer_attempt': '1',
                    'workflow_ref': 'iml885203/orbit/.github/workflows/release.yml@refs/heads/main',
                    'assets': {asset: 'c' * 64 for asset in candidate.inventory_names()}}
        digest = hashlib.sha256((json.dumps(manifest, sort_keys=True, indent=2) + '\n').encode()).hexdigest()
        value = {'schema': 'orbit.candidate-identity.v1', 'artifact_id': '20', 'archive_digest': 'd' * 64,
                 'manifest_sha256': digest, 'manifest': manifest}
        stream = io.BytesIO()
        with zipfile.ZipFile(stream, 'w') as archive:
            archive.writestr(name, json.dumps(value))
        raw = stream.getvalue()
        metadata = {'id': 21, 'name': 'orbit-candidate-identity-10-1', 'expired': False,
                    'size_in_bytes': len(raw), 'digest': 'sha256:' + hashlib.sha256(raw).hexdigest()}
        return raw, metadata

    def test_small_identity_is_bound_to_run_and_never_extracts_paths(self):
        run = {'id': 10, 'head_sha': 'a' * 40, 'path': '.github/workflows/release.yml',
               'event': 'workflow_dispatch', 'head_branch': 'main'}
        raw, metadata = self.identity()
        with mock.patch('delivery_status.pages', return_value=[metadata]), mock.patch('delivery_status.api', return_value=raw), mock.patch('candidate.verify_remote'):
            value = delivery.tiny_identity('iml885203/orbit', run)
            self.assertTrue(value['release_producer'])
            self.assertEqual(value['artifact_id'], '20')
            with self.assertRaises(ValueError):
                delivery.tiny_identity('iml885203/orbit', dict(run, head_sha='b' * 40))
        raw, metadata = self.identity('../candidate-identity.json')
        with mock.patch('delivery_status.pages', return_value=[metadata]), mock.patch('delivery_status.api', return_value=raw):
            with self.assertRaises(ValueError):
                delivery.tiny_identity('iml885203/orbit', run)


if __name__ == '__main__':
    unittest.main()
