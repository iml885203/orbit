import os
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


if __name__ == "__main__":
    unittest.main()
