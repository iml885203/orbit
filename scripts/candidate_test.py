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
        with mock.patch('candidate.subprocess.check_output', return_value=json.dumps(metadata)):
            candidate.verify_remote(self.expected, '20', 'd' * 64)
        for change in [{'expired': True}, {'id': 21}, {'digest': None}, {'name': 'orbit-candidate-10-3'},
                       {'workflow_run': {'id': 11, 'head_sha': self.expected['head_commit']}},
                       {'workflow_run': {'id': 10, 'head_sha': 'c' * 40}}]:
            with self.subTest(change=change), mock.patch('candidate.subprocess.check_output', return_value=json.dumps(dict(metadata, **change))):
                with self.assertRaises(ValueError):
                    candidate.verify_remote(self.expected, '20', 'd' * 64)


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
