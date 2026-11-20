import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

import candidate
import native_checks


class SelectedNativeEvidence(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='orbit-selected-native-')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.expected = dict(schema='orbit.candidate.v1', repository='iml885203/orbit', commit='a'*40,
                             head_commit='b'*40, version='v1.4.1', run_id='10', producer_attempt='1',
                             workflow_ref='iml885203/orbit/.github/workflows/ci.yml@refs/heads/main')
        self.manifest = dict(self.expected, assets={name: hashlib.sha256(name.encode()).hexdigest()
                                                   for name in candidate.inventory_names()})
        self.digest = hashlib.sha256((json.dumps(self.manifest,sort_keys=True,indent=2)+'\n').encode()).hexdigest()
        self.identity = dict(schema='orbit.candidate-identity.v1', artifact_id='20', archive_digest='c'*64,
                             manifest_sha256=self.digest, manifest=self.manifest)

    def receipts(self, profile):
        for p in self.root.iterdir():
            p.unlink()
        for target in native_checks.targets(profile):
            candidate.save(self.root/('native-'+target.replace('/','-')+'.json'),
                           dict(schema='orbit.native-receipt.v1', result='passed', suite='native', target=target,
                                manifest_sha256=self.digest, artifact_id='20', run_id='10', commit='a'*40,
                                version='v1.4.1', producer_attempt='1',
                                binary_sha256=self.manifest['assets'][candidate.asset(target)]))

    def verify(self, profile):
        with mock.patch('candidate.verify_remote') as remote:
            value = native_checks.verify(self.identity,self.expected,self.digest,'20','c'*64,self.root,profile)
            remote.assert_called_once_with(self.expected,'20','c'*64)
        return value

    def test_exact_selected_inventory_passes_and_partial_cannot_publish(self):
        for profile,count in [('linux',2),('full',6)]:
            self.receipts(profile)
            self.assertEqual(self.verify(profile)['receipts'],count)
        self.receipts('linux')
        with self.assertRaises(ValueError):
            candidate.verify_receipts(self.root,self.manifest,self.digest,'20','10')
        with self.assertRaises(ValueError):
            self.verify('full')
        self.receipts('full')
        with self.assertRaises(ValueError):
            self.verify('linux')

    def test_missing_foreign_stale_tampered_or_skipped_receipt_fails(self):
        for field,value in [('target','darwin/arm64'),('result','skipped'),('suite','sqlserver'),
                            ('artifact_id','21'),('run_id','11'),('commit','d'*40),('producer_attempt','2'),
                            ('manifest_sha256','e'*64),('binary_sha256','f'*64),('version','v1.4.0')]:
            with self.subTest(field=field):
                self.receipts('linux');p=self.root/'native-linux-arm64.json'
                receipt=json.loads(p.read_text());receipt[field]=value;candidate.save(p,receipt)
                with self.assertRaises(ValueError):self.verify('linux')
        self.receipts('linux');(self.root/'native-linux-arm64.json').unlink()
        with self.assertRaises(ValueError):self.verify('linux')
        for targets in [[],['linux/arm64','linux/arm64'],['other/arm64']]:
            with self.assertRaises(ValueError):candidate.verify_selected_receipts(self.root,self.manifest,self.digest,'20','10',targets)

    def test_identity_cannot_be_substituted_or_narrowed(self):
        self.receipts('linux')
        for field,value in [('schema','unknown'),('artifact_id','21'),('archive_digest','d'*64),('manifest_sha256','e'*64)]:
            old=self.identity[field];self.identity[field]=value
            with self.assertRaises(ValueError):self.verify('linux')
            self.identity[field]=old
        self.identity['manifest']=dict(self.manifest,head_commit='d'*40)
        with self.assertRaises(ValueError):self.verify('linux')

    def test_matrix_defaults_and_unknown_profiles_cannot_reduce_coverage(self):
        self.assertEqual({p['target'] for p in native_checks.matrix('full')['include']},set(candidate.TARGETS))
        self.assertEqual({p['target'] for p in native_checks.matrix('linux')['include']},{'linux/amd64','linux/arm64'})
        for profile in ['none','', 'unknown']:
            with self.assertRaises(ValueError):native_checks.matrix(profile)


if __name__=='__main__':unittest.main()
