from pathlib import Path
import re
import subprocess
import sys
import tempfile
import unittest

import native_checks

ROOT=Path(__file__).resolve().parent.parent


def job_block(text,name):
    start=text.index('\n  '+name+':\n')
    end=re.search(r'\n  [a-zA-Z][a-zA-Z0-9_-]*:\n',text[start+1:])
    return text[start:] if not end else text[start:start+1+end.start()]


class NativeWorkflowContracts(unittest.TestCase):
    def test_release_has_no_reduced_coverage_path_and_ci_forwards_selection(self):
        release=(ROOT/'.github/workflows/release.yml').read_text()
        native=(ROOT/'.github/workflows/platform-smoke.yml').read_text()
        ci=(ROOT/'.github/workflows/ci.yml').read_text()
        self.assertRegex(job_block(release,'platform-smoke'),r'(?m)^      profile: full$')
        self.assertNotIn('profile:',release[:release.index('\njobs:')])
        self.assertIn('default: full',native[:native.index('\njobs:')])
        self.assertIn("fromJSON(needs.select.outputs.matrix)",job_block(native,'native'))
        self.assertIn("needs.select.result == 'success'",job_block(native,'native'))
        self.assertIn('profile: ${{ needs.native-impact.outputs.profile }}',job_block(ci,'native'))
        self.assertIn('native_checks.py verify',job_block(ci,'native-gate'))
        self.assertIn('--manifest-sha256',job_block(ci,'native-gate'))
        self.assertIn('Publish native coverage plan',job_block(ci,'native-impact'))

    def test_manual_diagnostic_matrix_cli_defaults_full_and_invalid_is_not_a_noop(self):
        import json
        script=str(Path(native_checks.__file__).resolve())
        result=subprocess.run([sys.executable,script,'matrix'],capture_output=True,text=True,check=True)
        self.assertEqual(len(json.loads(result.stdout)['include']),6)
        with tempfile.TemporaryDirectory(prefix='orbit-native-profile-') as temporary:
            output=Path(temporary)/'outputs'
            result=subprocess.run([sys.executable,script,'matrix','--profile','unknown','--github-output',str(output)],capture_output=True,text=True)
            self.assertNotEqual(result.returncode,0);self.assertFalse(output.exists())


if __name__=='__main__':unittest.main()
