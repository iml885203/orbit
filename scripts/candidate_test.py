import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

import candidate
import native_impact


def artifact_response(metadata, status=200):
    body = metadata if isinstance(metadata, str) else json.dumps(metadata)
    return subprocess.CompletedProcess([], 0 if status == 200 else 1,
                                       stdout='HTTP/2.0 {} Status\ncontent-type: application/json\n\n'.format(status) + body,
                                       stderr='unstructured diagnostic')


class CandidateTrust(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='orbit-candidate-test-')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.directory = self.root / 'candidate'
        (self.directory / 'dist').mkdir(parents=True)
        (self.directory / 'embed').mkdir()
        self.expected = {'schema': 'orbit.candidate.v1', 'repository': 'iml885203/orbit', 'commit': 'a' * 40,
                         'head_commit': 'b' * 40, 'version': 'v1.4.1', 'run_id': '10', 'producer_attempt': '2',
                         'workflow_ref': 'iml885203/orbit/.github/workflows/release.yml@refs/heads/main'}
        for target in candidate.TARGETS:
            (self.directory / 'dist' / candidate.asset(target)).write_bytes(target.encode())
        (self.directory / 'dist/orbit.spdx.json').write_text('{"spdxVersion":"SPDX-2.3"}')
        (self.directory / 'dist/checksums.txt').write_text(''.join(
            candidate.sha(self.directory / 'dist' / candidate.asset(t)) + '  ' + candidate.asset(t) + '\n'
            for t in sorted(candidate.TARGETS)))
        (self.directory / 'embed/index.html').write_text('<title>one dashboard</title>')
        self.value = dict(self.expected, tools={'go': 'go1.26.11'}, assets=candidate.tree(self.directory / 'dist'),
                          embed=candidate.tree(self.directory / 'embed'))
        self.write_manifest()
        self.metadata_override = {}
        self.go = mock.patch('candidate.subprocess.check_output', side_effect=self.build_info)
        self.go.start()
        self.addCleanup(self.go.stop)

    def write_manifest(self):
        candidate.save(self.directory / 'manifest.json', self.value)
        self.digest = candidate.sha(self.directory / 'manifest.json')

    def build_info(self, command, **kwargs):
        target = next(t for t in candidate.TARGETS if candidate.asset(t) == Path(command[-1]).name)
        goos, goarch = target.split('/')
        settings = {'vcs.revision': self.expected['commit'], 'GOOS': goos, 'GOARCH': goarch,
                    'CGO_ENABLED': '0', 'vcs.modified': 'false', '-ldflags': '-s -w -X main.version=' + self.expected['version']}
        settings.update(self.metadata_override)
        return json.dumps({'GoVersion': 'go1.26.11', 'Path': 'github.com/iml885203/orbit/cmd/orbit',
                           'Settings': [{'Key': key, 'Value': value} for key, value in settings.items()]})

    def verify(self):
        return candidate.verify(self.directory, self.expected, self.digest)

    def test_exact_candidate_passes_and_tampering_does_not(self):
        self.assertEqual(self.verify()['assets'], self.value['assets'])
        binary = self.directory / 'dist/orbit-linux-amd64'
        binary.write_bytes(b'tampered')
        with self.assertRaises(ValueError):
            self.verify()

    def test_wrong_source_version_repository_and_producer_are_rejected(self):
        for field, other in [('commit', 'c' * 40), ('version', 'v1.4.10'), ('repository', 'other/repo'),
                             ('run_id', '11'), ('producer_attempt', '3'), ('workflow_ref', 'foreign'),
                             ('head_commit', 'c' * 40)]:
            expected = dict(self.expected, **{field: other})
            with self.subTest(field=field), self.assertRaises(ValueError):
                candidate.verify(self.directory, expected, self.digest)
        (self.directory / 'manifest.json').write_text('{}')
        with self.assertRaises(ValueError):
            self.verify()

    def test_missing_extra_assets_dashboard_changes_and_links_fail(self):
        extra = self.directory / 'dist/orbit-extra'
        extra.write_text('extra')
        with self.assertRaises(ValueError):
            self.verify()
        extra.unlink()
        (self.directory / 'embed/index.html').write_text('different dashboard')
        with self.assertRaises(ValueError):
            self.verify()
        if os.name != 'nt':
            (self.directory / 'embed/linked').symlink_to(self.directory / 'dist/orbit-linux-amd64')
            with self.assertRaises(ValueError):
                candidate.tree(self.directory / 'embed')

    def test_go_revision_architecture_dirty_source_and_version_are_exact(self):
        for changes in [{'vcs.revision': 'c' * 40}, {'GOARCH': '386'}, {'vcs.modified': 'true'},
                        {'CGO_ENABLED': '1'}, {'-ldflags': '-X main.version=v1.4.10'}]:
            with self.subTest(changes=changes), self.assertRaises(ValueError):
                self.metadata_override = changes
                self.verify()

    def receipts(self):
        root = self.root / 'receipts'
        root.mkdir(exist_ok=True)
        for target in candidate.TARGETS:
            value = {'schema': 'orbit.native-receipt.v1', 'result': 'passed', 'suite': 'native', 'target': target,
                     'manifest_sha256': self.digest, 'artifact_id': '20', 'run_id': '10', 'commit': self.expected['commit'],
                     'version': self.expected['version'], 'producer_attempt': '2',
                     'binary_sha256': self.value['assets'][candidate.asset(target)]}
            candidate.save(root / ('native-' + target.replace('/', '-') + '.json'), value)
        return root

    def test_six_matching_receipts_required_partial_skipped_stale_or_tampered_fail(self):
        root = self.receipts()
        self.assertEqual(len(candidate.verify_receipts(root, self.value, self.digest, '20', '10')), 6)
        path = root / 'native-linux-amd64.json'
        original = json.loads(path.read_text())
        for change in [{'result': 'skipped'}, {'artifact_id': '21'}, {'run_id': '11'},
                       {'manifest_sha256': '0' * 64}, {'binary_sha256': '0' * 64},
                       {'producer_attempt': '3'}, {'target': 'linux/arm64'}, {'suite': 'sqlserver'}]:
            candidate.save(path, dict(original, **change))
            with self.subTest(change=change), self.assertRaises(ValueError):
                candidate.verify_receipts(root, self.value, self.digest, '20', '10')
        path.unlink()
        with self.assertRaises(ValueError):
            candidate.verify_receipts(root, self.value, self.digest, '20', '10')

    def produced_receipts(self):
        native, sql = self.root / 'produced-native', self.root / 'produced-sql'
        with mock.patch('candidate.verify_binary'), mock.patch.dict(os.environ, {
                'GITHUB_RUN_ATTEMPT': '7', 'GITHUB_JOB': 'consumer-rerun'}):
            for target in candidate.TARGETS:
                candidate.receipt(self.value, target, self.directory / 'dist' / candidate.asset(target),
                                  native, self.digest, '20', 'native')
            candidate.receipt(self.value, 'linux/amd64', self.directory / 'dist/orbit-linux-amd64',
                              sql, self.digest, '20', 'sqlserver')
        return native, sql

    def promote(self, native, sql):
        publication = self.root / 'publication'
        arguments = ['candidate.py', 'promote', '--directory', str(self.directory),
                     '--version', self.expected['version'], '--producer-attempt', '2',
                     '--workflow-ref', self.expected['workflow_ref'], '--manifest-sha256', self.digest,
                     '--artifact-id', '20', '--archive-digest', 'd' * 64,
                     '--receipts', str(native), '--sql-receipts', str(sql),
                     '--publish-directory', str(publication)]
        with mock.patch.object(sys, 'argv', arguments), \
                mock.patch('candidate.identity', return_value=self.expected), \
                mock.patch('candidate.verify_remote'), mock.patch('sys.stderr'):
            return candidate.main()

    def test_produced_receipts_survive_consumer_reruns_and_promote_exact_bytes(self):
        native, sql = self.produced_receipts()
        records = candidate.verify_selected_receipts(native, self.value, self.digest, '20', '10',
                                                    candidate.TARGETS)
        self.assertTrue(all(row['consumer_attempt'] == '7' for row in records))
        self.assertEqual(self.promote(native, sql), 0)
        self.assertEqual(candidate.tree(self.root / 'publication'), self.value['assets'])

    def test_produced_linux_receipts_keep_selected_ci_coverage(self):
        native, _ = self.produced_receipts()
        selected = ['linux/amd64', 'linux/arm64']
        for target in candidate.TARGETS:
            if target not in selected:
                (native / ('native-' + target.replace('/', '-') + '.json')).unlink()
        self.assertEqual(len(candidate.verify_selected_receipts(
            native, self.value, self.digest, '20', '10', selected)), 2)
        with self.assertRaises(ValueError):
            candidate.verify_receipts(native, self.value, self.digest, '20', '10')

    def test_promotion_rejects_corrupt_or_missing_receipt_bindings_before_copy(self):
        native, sql = self.produced_receipts()
        corruptions = {'schema': 'unknown', 'result': 'skipped', 'suite': 'other',
                       'target': 'linux/arm64', 'manifest_sha256': '0' * 64,
                       'artifact_id': '21', 'run_id': '11', 'commit': 'c' * 40,
                       'version': 'v1.4.2', 'producer_attempt': '3', 'binary_sha256': '0' * 64}
        for path in (native / 'native-linux-amd64.json', sql / 'sqlserver-linux-amd64.json'):
            original = json.loads(path.read_text())
            for field, other in corruptions.items():
                for missing in (False, True):
                    changed = dict(original)
                    if missing:
                        del changed[field]
                    else:
                        changed[field] = other
                    candidate.save(path, changed)
                    with self.subTest(suite=original['suite'], field=field, missing=missing), \
                            mock.patch('candidate.shutil.copytree') as copy:
                        self.assertEqual(self.promote(native, sql), 1)
                        copy.assert_not_called()
                    candidate.save(path, original)

    def test_promotion_retains_separate_receipt_inventory_rules(self):
        native, sql = self.produced_receipts()
        path = native / 'native-linux-amd64.json'
        original = path.read_text()
        path.unlink()
        self.assertEqual(self.promote(native, sql), 1)
        path.write_text(original)
        extra = native / 'extra.json'
        extra.write_text(original)
        self.assertEqual(self.promote(native, sql), 1)
        extra.unlink()
        path = sql / 'sqlserver-linux-amd64.json'
        original = path.read_text()
        path.unlink()
        self.assertEqual(self.promote(native, sql), 1)
        path.write_text(original)
        extra = sql / 'extra.json'
        extra.write_text(original)
        self.assertEqual(self.promote(native, sql), 1)
        extra.unlink()
        path.rename(sql / 'different-name.json')
        (sql / 'diagnostic.txt').write_text('not receipt evidence')
        self.assertEqual(self.promote(native, sql), 0)

    @unittest.skipIf(os.name == 'nt', 'fixture uses Unix symlinks')
    def test_native_and_sql_keep_their_existing_link_policies(self):
        native, sql = self.produced_receipts()
        native_path = native / 'native-linux-amd64.json'
        native_record = self.root / 'native-record'
        native_path.rename(native_record)
        native_path.symlink_to(native_record)
        self.assertEqual(self.promote(native, sql), 1)
        native_path.unlink()
        native_record.rename(native_path)
        sql_path = sql / 'sqlserver-linux-amd64.json'
        sql_record = self.root / 'sql-record'
        sql_path.rename(sql_record)
        sql_path.symlink_to(sql_record)
        self.assertEqual(self.promote(native, sql), 0)

    def test_cli_display_version_accepts_build_time_with_an_exact_tag(self):
        binary = self.directory / 'dist/orbit-linux-amd64'
        with mock.patch.dict(os.environ, {'RUNNER_TEMP': str(self.root)}):
            for reported in (self.expected['version'], self.expected['version'] + ' (2026-11-19 09:30:00 +0000)'):
                envelope = {'schema_version': 'orbit.cli.v1', 'ok': True, 'data': {'version': reported}}
                with mock.patch('candidate.subprocess.check_output', return_value=json.dumps(envelope)):
                    candidate.verify_binary(binary, self.value, 'linux/amd64')
            for reported in (self.expected['version'] + '0 (time)', '', None, 'other'):
                envelope = {'schema_version': 'orbit.cli.v1', 'ok': True, 'data': {'version': reported}}
                with mock.patch('candidate.subprocess.check_output', return_value=json.dumps(envelope)):
                    with self.assertRaises(ValueError):
                        candidate.verify_binary(binary, self.value, 'linux/amd64')

    def test_artifact_api_identity_expiration_and_digest_fail_closed(self):
        metadata = {'id': 20, 'expired': False, 'digest': 'sha256:' + 'd' * 64, 'name': 'orbit-candidate-10-2',
                    'workflow_run': {'id': 10, 'head_sha': self.expected['head_commit']}}
        with mock.patch('candidate.subprocess.run', return_value=artifact_response(metadata)):
            candidate.verify_remote(self.expected, '20', 'd' * 64)
        for change in [{'expired': True}, {'id': 21}, {'digest': None}, {'name': 'orbit-candidate-10-3'},
                       {'workflow_run': {'id': 11, 'head_sha': self.expected['head_commit']}},
                       {'workflow_run': {'id': 10, 'head_sha': 'c' * 40}}]:
            with self.subTest(change=change), mock.patch('candidate.subprocess.run', return_value=artifact_response(dict(metadata, **change))):
                with self.assertRaises(ValueError):
                    candidate.verify_remote(self.expected, '20', 'd' * 64)


class RemoteArtifactRead(unittest.TestCase):
    def setUp(self):
        self.expected = {'repository':'iml885203/orbit','run_id':'10','producer_attempt':'2','head_commit':'b'*40}
        self.metadata = {'id':20,'expired':False,'digest':'sha256:'+'d'*64,'name':'orbit-candidate-10-2',
                         'workflow_run':{'id':10,'head_sha':'b'*40}}

    def verify(self):
        return candidate.verify_remote(self.expected,'20','d'*64)

    def test_transient_statuses_read_only_exact_id_then_verify_identity(self):
        for code in (404,500,502,503,504):
            with self.subTest(code=code), mock.patch('candidate.subprocess.run',side_effect=[artifact_response({},code),artifact_response(self.metadata)]) as request, mock.patch('candidate.time.sleep') as sleep:
                self.assertEqual(self.verify(),self.metadata)
                self.assertEqual(request.call_count,2)
                for call in request.call_args_list:
                    self.assertEqual(call.args[0],['gh','api','--include','repos/iml885203/orbit/actions/artifacts/20'])
                    self.assertEqual(call.kwargs['timeout'],10)
                sleep.assert_called_once_with(0.5)

    def test_exhaustion_has_three_attempts_and_bounded_delays(self):
        with mock.patch('candidate.subprocess.run',return_value=artifact_response({},404)) as request, mock.patch('candidate.time.sleep') as sleep:
            with self.assertRaisesRegex(ValueError,'HTTP 404 after 3 attempt'):
                self.verify()
            self.assertEqual(request.call_count,3)
            self.assertEqual(sleep.call_args_list,[mock.call(0.5),mock.call(1.0)])

    def test_permanent_failures_stop_and_timeout_exhaustion_stays_bounded(self):
        responses = [artifact_response({},code) for code in (401,403,429,422)]
        responses += [subprocess.CompletedProcess([],1,stdout='no headers',stderr='HTTP 404'),
                      subprocess.CompletedProcess([],1,stdout='HTTP/2.0 200 Status\n\n{}',stderr='error')]
        for response in responses:
            with self.subTest(response=response.stdout), mock.patch('candidate.subprocess.run',return_value=response) as request, mock.patch('candidate.time.sleep') as sleep:
                with self.assertRaises(ValueError):self.verify()
                self.assertEqual(request.call_count,1)
                sleep.assert_not_called()
        with mock.patch('candidate.subprocess.run',side_effect=subprocess.TimeoutExpired('gh',10)) as request, mock.patch('candidate.time.sleep') as sleep:
            with self.assertRaisesRegex(ValueError,'10s limit'):self.verify()
            self.assertEqual(request.call_count,3)
            self.assertEqual(sleep.call_args_list,[mock.call(0.5),mock.call(1.0)])

    def test_mixed_temporary_failures_share_three_attempts(self):
        responses = [subprocess.TimeoutExpired('gh',10),artifact_response({},503),
                     subprocess.TimeoutExpired('gh',10),artifact_response(self.metadata)]
        with mock.patch('candidate.subprocess.run',side_effect=responses) as request, mock.patch('candidate.time.sleep') as sleep:
            with self.assertRaisesRegex(ValueError,'after 3 attempts'):self.verify()
            self.assertEqual(request.call_count,3)
            self.assertEqual(sleep.call_args_list,[mock.call(0.5),mock.call(1.0)])

    def test_timeout_then_matching_response_recovers_with_the_same_budget(self):
        with mock.patch('candidate.subprocess.run',side_effect=[subprocess.TimeoutExpired('gh',10),artifact_response(self.metadata)]) as request, mock.patch('candidate.time.sleep') as sleep:
            self.assertEqual(self.verify(),self.metadata)
            self.assertEqual(request.call_count,2)
            self.assertTrue(all(call.kwargs['timeout']==10 for call in request.call_args_list))
            sleep.assert_called_once_with(0.5)

    def test_malformed_or_untrusted_success_never_becomes_retry_success(self):
        values = ['invalid JSON',[],{},dict(self.metadata,expired=True),dict(self.metadata,workflow_run=None),
                  dict(self.metadata,digest='sha256:'+'e'*64),dict(self.metadata,expired='false')]
        for value in values:
            with self.subTest(value=value), mock.patch('candidate.subprocess.run',side_effect=[artifact_response(value),artifact_response(self.metadata)]) as request, mock.patch('candidate.time.sleep') as sleep:
                with self.assertRaises(ValueError):self.verify()
                self.assertEqual(request.call_count,1)
                sleep.assert_not_called()


class NativeImpact(unittest.TestCase):
    def test_only_known_documentation_can_skip(self):
        self.assertFalse(native_impact.classify(['README.md', 'docs/development.md', 'docs/assets/check.svg'])['required'])
        for paths in [[], ['internal/engine/process.go'], ['go.mod'], ['ui/src/App.svelte'], ['VERSION'],
                      ['scripts/install.ps1'], ['.github/workflows/release.yml'], ['unknown-file'],
                      ['internal/deleted.go', 'docs/renamed.md']]:
            with self.subTest(paths=paths):
                self.assertTrue(native_impact.classify(paths)['required'])
        with self.assertRaises(ValueError):
            native_impact.classify(['docs/../scripts/changed.md'])

    def test_unknown_base_cannot_produce_noop_output(self):
        with tempfile.TemporaryDirectory(prefix='orbit-impact-test-') as temporary:
            output = Path(temporary) / 'output'
            result = subprocess.run([sys.executable, str(Path(native_impact.__file__)), '--base', 'not-a-sha',
                                     '--github-output', str(output)], capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(output.exists())


if __name__ == '__main__':
    unittest.main()
