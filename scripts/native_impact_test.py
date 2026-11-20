import json
from pathlib import Path
import os
import subprocess
import sys
import tempfile
import unittest

import native_impact


class NativeSchedulingPolicy(unittest.TestCase):
    def test_profiles_and_strongest_mixed_requirement(self):
        self.assertEqual(native_impact.classify(['README.md','docs/check.md','docs/assets/check.svg'])['profile'],'none')
        for paths,versions in [(['ui/src/App.svelte'],None),(['website/tests/site.spec.ts'],None),
                               (['internal/bigquery/jobs.go'],{'internal/bigquery/jobs.go':['package bigquery\n',None]})]:
            self.assertEqual(native_impact.classify(paths,versions)['profile'],'linux')
        for name in ['internal/app/env_apply.go','internal/platform/process_unix.go','internal/daemon/wire.go',
                     'internal/process/manager.go','scripts/install.sh','scripts/native_impact.py','go.mod',
                     'go.sum','Makefile','.github/workflows/ci.yml','unknown-file','ui/package.json','ui/pnpm-lock.yaml',
                     'ui/vite.config.ts','website/package.json','website/pnpm-lock.yaml','website/scripts/site-contract.mjs',
                     'internal/tunnel/new-install.ps1','internal/bigquery/unknown.bin','ui/src/package.json']:
            with self.subTest(name=name):
                self.assertEqual(native_impact.classify(['ui/src/App.svelte',name])['profile'],'full')
        self.assertEqual(native_impact.classify([])['profile'],'full')
        self.assertEqual(native_impact.classify(['internal/bigquery/jobs.go'])['profile'],'full')
        for suffix in ['windows.go','darwin_test.go','arm64.go','freebsd.go','wasm_test.go']:
            self.assertEqual(native_impact.classify(['internal/bigquery/job_'+suffix])['profile'],'full')
        for text in ['//go:build windows\npackage bigquery','// +build darwin\npackage bigquery','\ufeff//go:build windows\npackage bigquery','\ufeff// +build darwin\npackage bigquery']:
            for versions in [[text,'package bigquery'],['package bigquery',text]]:
                self.assertEqual(native_impact.classify(['internal/bigquery/jobs.go'],{'internal/bigquery/jobs.go':versions})['profile'],'full')


    def test_current_host_and_cli_sources_override_general_domain_membership(self):
        root=Path(__file__).resolve().parent.parent
        for name in ['internal/sqlpublish/tools.go','internal/sqlpublish/publish.go','internal/devdb/db_query.go',
                     'internal/bigquery/commands.go','internal/bigquery/receipt.go','internal/tunnel/claim_lifecycle.go',
                     'internal/devdb/db_publish_run.go']:
            with self.subTest(name=name):
                self.assertEqual(native_impact.classify([name],{name:[None,(root/name).read_text()]})['profile'],'full')
        for body in ['package feature\nimport `runtime`','package feature\nimport "runti\\x6de"',
                     'package feature\nimport "os/exec"','package feature\nimport `runti\rme`']:
            for versions in [[body,None],[None,body]]:
                self.assertEqual(native_impact.classify(['internal/bigquery/generic.go'],{'internal/bigquery/generic.go':versions})['profile'],'full')

    def test_actual_git_diff_covers_rename_delete_constraints_and_invalid_evidence(self):
        with tempfile.TemporaryDirectory(prefix='orbit-native-diff-') as directory:
            root=Path(directory)
            def git(*args):return subprocess.check_output(['git',*args],cwd=root,text=True).strip()
            git('init','-q');git('config','user.email','fixture@example.invalid');git('config','user.name','Fixture')
            p=root/'internal/bigquery/client.go';p.parent.mkdir(parents=True);p.write_text('//go:build windows\npackage bigquery\n')
            git('add','.');git('commit','-qm','base');base=git('rev-parse','HEAD')
            p.unlink();q=p.with_name('generic.go');q.write_text('package bigquery\n')
            git('add','-A');git('commit','-qm','rename and remove constraint')
            script=str(Path(native_impact.__file__).resolve())
            r=subprocess.run([sys.executable,script,'--base',base],cwd=root,capture_output=True,text=True,check=True)
            result=json.loads(r.stdout);self.assertEqual(result['profile'],'full');self.assertIn('internal/bigquery/client.go',result['paths'])
            base=git('rev-parse','HEAD');q.write_text('package bigquery\nvar changed = true\n');git('add','.');git('commit','-qm','general change')
            r=subprocess.run([sys.executable,script,'--base',base],cwd=root,capture_output=True,text=True,check=True)
            self.assertEqual(json.loads(r.stdout)['profile'],'linux')
            for args in [[],['--base','invalid'],['--base','f'*40]]:
                output=root/'result';r=subprocess.run([sys.executable,script,*args,'--github-output',str(output)],cwd=root,capture_output=True,text=True)
                if not args:self.assertEqual(json.loads(r.stdout)['profile'],'full')
                else:self.assertNotEqual(r.returncode,0)
                if output.exists():output.unlink()


if __name__=='__main__':unittest.main()
