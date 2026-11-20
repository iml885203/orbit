import os
import json
import socket
import time
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parent.parent


@unittest.skipIf(os.name == "nt", "journey harness requires Bash")
class JourneyHarnessDiagnostics(unittest.TestCase):
    def test_unexpected_failure_reports_location_before_cleanup_and_preserves_exit(self):
        self.check_unexpected_failure(False)

    def test_cleanup_failure_cannot_replace_unexpected_exit_status(self):
        self.check_unexpected_failure(True)

    def test_successful_body_cannot_hide_cleanup_failure(self):
        self.check_unexpected_failure(True, primary_status=0)

    def test_expected_failure_does_not_replace_later_explicit_exit_location(self):
        self.check_unexpected_failure(False, explicit_exit=True)

    def check_unexpected_failure(self, fail_cleanup, primary_status=42, explicit_exit=False):
        with tempfile.TemporaryDirectory(prefix="orbit-harness-diagnostic-") as temporary:
            root = Path(temporary)
            scripts = root / "scripts"
            scripts.mkdir()
            harness = scripts / "test-journey-port-reliability.sh"
            shutil.copy2(ROOT / "scripts/test-journey-port-reliability.sh", harness)
            if primary_status == 0 or explicit_exit:
                setup = harness.read_text().split('\ncat >"$test_root/transient-conflict.sh"', 1)[0]
                suffix = "\nset +e\nfalse\nset -e\nexit 42\n" if explicit_exit else "\nexit 0\n"
                harness.write_text(setup + suffix)
            retry = scripts / "retry-journey-port-conflict.sh"
            retry.write_text("#!/usr/bin/env bash\necho 'private-log-token=never-print'\nexit 42\n")
            retry.chmod(0o755)
            tools = root / "tools"
            tools.mkdir()
            owned = root / "owned"
            owned.mkdir()
            (tools / "mktemp").write_text("#!" + sys.executable + "\nprint(" + repr(str(owned)) + ")\n")
            (tools / "mktemp").chmod(0o755)
            if fail_cleanup:
                (tools / "rm").write_text("#!/usr/bin/env bash\nexit 61\n")
                (tools / "rm").chmod(0o755)
            proof = root / "cleanup-proof"
            (tools / "docker").write_text("#!" + sys.executable + "\nfrom pathlib import Path\n"
                                          "assert Path(" + repr(str(owned)) + ").exists()\n"
                                          "Path(" + repr(str(proof)) + ").write_text('cleaned')\n")
            (tools / "docker").chmod(0o755)
            environment = dict(os.environ, PATH=str(tools) + os.pathsep + os.environ["PATH"])
            result = subprocess.run(["bash", str(harness)], env=environment, capture_output=True, text=True)
            if primary_status:
                if explicit_exit:
                    command_lines = {next(index for index, text in enumerate(harness.read_text().splitlines(), 1)
                                          if text == "exit 42")}
                else:
                    start = next(index for index, text in enumerate(harness.read_text().splitlines(), 1)
                                 if text.startswith('"$retry_runner"'))
                    # Bash 3 attributes a continued command to its last line; Bash 5 uses its first.
                    command_lines = {start, start + 1}
                self.assertEqual(result.returncode, 42)
                self.assertRegex(result.stderr, r"^Journey harness failed at scripts/test-journey-port-reliability.sh:[0-9]+ \(exit 42\)\n$")
                line = int(result.stderr.split(":", 1)[1].split(" ", 1)[0])
                self.assertIn(line, command_lines)
                self.assertEqual(result.stderr, "Journey harness failed at scripts/test-journey-port-reliability.sh:"
                                 + str(line) + " (exit 42)\n")
            else:
                self.assertEqual(result.returncode, 61)
                self.assertRegex(result.stderr, r"^Journey harness cleanup failed at scripts/test-journey-port-reliability.sh:[0-9]+ \(exit 61\)\n$")
            self.assertNotIn("private-log-token", result.stdout + result.stderr)
            self.assertEqual(owned.exists(), fail_cleanup)
            self.assertEqual(proof.read_text(), "cleaned")


@unittest.skipIf(os.name == "nt", "journey harness requires Bash and Unix signals")
class RelocationFixtureLifetime(unittest.TestCase):
    def exercise(self, delay=0.4, fail_second=False, ignore_term=False, fail_export=False):
        with tempfile.TemporaryDirectory(prefix="orbit-relocation-boundary-") as temporary:
            root = Path(temporary)
            scripts = root / "scripts"
            scripts.mkdir()
            names = ["retry-journey-port-conflict.sh", "hold-test-ports.py",
                     "clean-journey-docker-resources.sh", "export-journey-diagnostics.sh",
                     "export-journey-diagnostics.py"]
            for name in names:
                shutil.copy2(ROOT / "scripts" / name, scripts / name)
            with socket.socket() as listener:
                listener.bind(("127.0.0.1", 0))
                port = listener.getsockname()[1]
            original = (ROOT / "scripts/test-journey-port-reliability.sh").read_text()
            setup = original.split('\ncat >"$test_root/transient-conflict.sh"', 1)[0]
            relocation = original[original.index('if [ -x "$orbit_bin" ]; then'):
                                  original.index('\nif command -v docker')]
            harness = scripts / "test-journey-port-reliability.sh"
            harness.write_text((setup + '\n' + relocation + '\nexit 0\n').replace('21081', str(port)))
            holder = scripts / "hold-test-ports.py"
            text = holder.read_text()
            if ignore_term:
                text = text.replace('stopped = True', 'stopped = False')
            else:
                text = text.replace('    finally:\n        for listener in listeners:\n            listener.close()\n    return 0',
                                    '    finally:\n        time.sleep(' + repr(delay) + ')\n        for listener in listeners:\n            listener.close()\n    return 0')
            holder.write_text(text)
            fake_bin = root / 'orbit'
            fake_bin.write_text('#!/bin/sh\nexit 0\n')
            fake_bin.chmod(0o755)
            runtime = scripts / 'test-runtime-adoption.sh'
            runtime.write_text('#!' + sys.executable + '\n' +
                "import json,os,socket,sys\n" +
                "attempt=int(os.environ['ORBIT_JOURNEY_ATTEMPT'])\n" +
                "with open(os.environ['ATTEMPT_PROOF'],'a') as out:out.write(json.dumps({'attempt':attempt})+'\\n')\n" +
                "if attempt==1:\n print(json.dumps({'error':{'code':'checks_failed','message':'port " + str(port) + " is already in use'}}));sys.exit(1)\n" +
                "with socket.socket() as listener:listener.bind(('127.0.0.1'," + str(port) + "))\n" +
                ("print('password=top-secret');print(json.dumps({'error':{'code':'invalid_environment'}}));sys.exit(37)\n"
                 if fail_second else "print('Python, Node, and Go projects adopt Orbit')\n"))
            runtime.chmod(0o755)
            if fail_export:
                (scripts / 'export-journey-diagnostics.sh').write_text('#!/bin/sh\nexit 55\n')
            tools = root / 'tools'
            tools.mkdir()
            (tools / 'docker').write_text('#!/bin/sh\nexit 0\n')
            (tools / 'docker').chmod(0o755)
            environment = dict(os.environ, PATH=str(tools) + os.pathsep + os.environ['PATH'],
                               ORBIT_BIN=str(fake_bin), ORBIT_JOURNEY_ARTIFACT_DIR=str(root/'artifacts'),
                               ATTEMPT_PROOF=str(root/'attempts'))
            started = time.monotonic()
            process = subprocess.run(['bash', str(harness)], env=environment, text=True,
                                     capture_output=True, timeout=30)
            attempts = [json.loads(line) for line in (root/'attempts').read_text().splitlines()]
            artifacts = root / 'artifacts/journey-harness/attempt-1'
            retained = '\n'.join(path.read_text() for path in artifacts.glob('*')) if artifacts.exists() else ''
            with socket.socket() as listener:
                listener.bind(('127.0.0.1', port))
            return process, attempts, retained, time.monotonic()-started

    def test_retry_waits_for_slow_fixture_socket_release(self):
        process, attempts, _, _ = self.exercise()
        self.assertEqual(process.returncode, 0, process.stderr)
        self.assertEqual([row['attempt'] for row in attempts], [1, 2])

    def test_failed_second_attempt_is_retained_redacted_and_keeps_exit(self):
        process, attempts, retained, _ = self.exercise(fail_second=True)
        self.assertEqual(process.returncode, 37, process.stderr)
        self.assertEqual(len(attempts), 2)
        self.assertIn('invalid_environment', retained)
        self.assertIn('checks_failed', retained)
        self.assertIn('[redacted]', retained)
        self.assertNotIn('top-secret', retained)
        self.assertNotIn('top-secret', process.stdout + process.stderr)

    def test_unreleased_fixture_fails_without_retry_and_cleanup_is_bounded(self):
        process, attempts, retained, elapsed = self.exercise(ignore_term=True)
        self.assertEqual(process.returncode, 70, process.stderr)
        self.assertEqual(len(attempts), 1)
        self.assertIn('fixture_release_failed', retained)
        self.assertLess(elapsed, 25)

    def test_diagnostic_export_failure_cannot_replace_primary_exit(self):
        process, _, _, _ = self.exercise(fail_second=True, fail_export=True)
        self.assertEqual(process.returncode, 37, process.stderr)
        self.assertIn('Failed to export journey harness diagnostics', process.stderr)

    def test_top_level_journey_harness_receives_artifact_directory(self):
        with tempfile.TemporaryDirectory(prefix='orbit-harness-artifact-env-') as temporary:
            root = Path(temporary)
            scripts = root/'scripts'
            scripts.mkdir()
            shutil.copy2(ROOT/'scripts/clean-journey-docker-resources.sh', scripts)
            original = (ROOT/'Makefile').read_text()
            recipe = original[original.index('test-journeys: build'):
                              original.index('\ntest-journey-harness:')]
            proof = root/'proof.json'
            (root/'record.py').write_text("import json,os,sys\nfrom pathlib import Path\nPath("+repr(str(proof))+").write_text(json.dumps({'artifact_dir':os.environ.get('ORBIT_JOURNEY_ARTIFACT_DIR')}))\nsys.exit(42)\n")
            (root/'Makefile').write_text(recipe+'\nbuild:\n\t@true\ntest-journey-harness:\n\t'+sys.executable+' record.py\n')
            result = subprocess.run(['make','test-journeys'],cwd=root,text=True,capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(Path(json.loads(proof.read_text())['artifact_dir']).resolve(),
                             (root/'artifacts/installed-user-journeys').resolve())


if __name__ == "__main__":
    unittest.main()
