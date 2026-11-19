import contextlib
import io
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

import dev_check
import lint_gate
import verify_types


@unittest.skipIf(os.name == "nt", "executable fixture tools use Unix shebangs")
class IsolatedChecks(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="orbit-check-test-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.source, self.workspace = self.root / "source with spaces", self.root / "work space"
        self.source.mkdir()
        self.workspace.mkdir()
        (self.source / "subject.txt").write_text("pass")
        self.tools = self.root / "tools"
        self.tools.mkdir()
        self.proof = self.root / "proof"
        self.executable("git", '''import pathlib,sys
source=pathlib.Path(sys.argv[2]);args=sys.argv[3:]
if args[0]=='ls-files':sys.stdout.buffer.write(b'\\0'.join(str(p.relative_to(source)).encode() for p in source.rglob('*') if p.is_file())+b'\\0')
elif args[0]=='rev-parse':print('a'*40)
elif args[0]=='tag':print('v1.4.0')
''')
        self.executable("go", "print('go version go1.26.11 fixture')")
        self.executable("make", '''from pathlib import Path
import os,sys
if '--version' in sys.argv:print('make fixture');sys.exit(0)
p=Path(os.environ['ORBIT_CHECK_TEST_PROOF']);p.write_text(p.read_text()+'run\\n' if p.exists() else 'run\\n')
sys.exit(0 if Path('subject.txt').read_text()=='pass' else 2)
''')
        self.environment = mock.patch.dict(os.environ, {"PATH": str(self.tools) + os.pathsep + os.environ["PATH"],
                                                        "ORBIT_CHECK_TEST_PROOF": str(self.proof)}, clear=False)
        self.environment.start()
        self.addCleanup(self.environment.stop)
        for name in ("ORBIT_UI_EXT", "ORBIT_UI_TYPES", "ORBIT_UI_OUTDIR", "GOFLAGS", "GOWORK"):
            os.environ.pop(name, None)

    def executable(self, name, body):
        path = self.tools / name
        path.write_text("#!" + sys.executable + "\n" + body)
        path.chmod(0o755)

    def run_check(self, reuse=False):
        return dev_check.check(self.source, self.workspace, "go", ["./internal/..."], 1, 30, reuse)

    def test_snapshot_reuse_and_changed_untracked_inputs(self):
        first = self.run_check()
        self.assertEqual(first["status"], "passed")
        self.assertFalse(first["commit_authority"])
        self.assertTrue(self.run_check(reuse=True)["reused"])
        self.assertEqual(self.proof.read_text().count("run"), 1)
        (self.source / "new.go").write_text("package fixture")
        self.assertFalse(self.run_check(reuse=True)["reused"])
        self.assertEqual(self.proof.read_text().count("run"), 2)
        self.assertEqual((self.source / "subject.txt").read_text(), "pass")

    def test_external_build_inputs_and_full_reuse_rejected(self):
        for name, value in (("GOWORK", "/external/go.work"), ("GOFLAGS", "-overlay=/external/map.json"),
                            ("GOFLAGS", "-modfile /external/go.mod")):
            with mock.patch.dict(os.environ, {name: value}):
                with self.assertRaises(ValueError):
                    self.run_check()
        with self.assertRaises(ValueError):
            dev_check.check(self.source, self.workspace, "full", [], 1, 30, True)
        self.assertFalse(self.proof.exists())

    def test_enabling_live_tests_invalidates_reuse_without_exposing_configuration(self):
        self.run_check()
        with mock.patch.dict(os.environ, {"ORBIT_D1_LIVE": "1", "ORBIT_E2E_SQL": "private-target-token"}):
            result = self.run_check(reuse=True)
        self.assertFalse(result["reused"])
        self.assertEqual(self.proof.read_text().count("run"), 2)
        self.assertNotIn("private-target-token", __import__('json').dumps(result))

    def test_changed_tool_invalidates_success(self):
        self.run_check()
        self.executable("go", "print('go version go1.26.12 fixture')")
        self.assertFalse(self.run_check(reuse=True)["reused"])
        self.assertEqual(self.proof.read_text().count("run"), 2)

    def test_storage_helper_failure_does_not_run_checks(self):
        self.executable("storage", "import sys;sys.exit(2)")
        result = subprocess.run([sys.executable, str(Path(dev_check.__file__)), "go", "--source",
                                 str(self.source), "--storage-helper", str(self.tools / "storage")],
                                capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(b"no fallback", result.stderr)
        self.assertFalse(self.proof.exists())

    def test_full_forces_complete_go_suite_and_isolated_module_cache(self):
        self.executable("node", "print('v22.0.0')")
        self.executable("pnpm", "print('11.8.0')")
        self.executable("make", """import os,sys
from pathlib import Path
if '--version' in sys.argv:print('make fixture');sys.exit(0)
assert 'GO_TEST_PACKAGES=./...' in sys.argv
assert 'MAKEFILES' not in os.environ
assert 'MAKE' not in os.environ
assert Path(os.environ['GOMODCACHE']).is_relative_to(Path(os.environ['ORBIT_CHECK_TEST_WORKSPACE']).resolve())
""")
        with mock.patch.dict(os.environ, {"GO_TEST_PACKAGES": "./small", "MAKEFILES": "/untrusted", "MAKE": "true",
                                          "ORBIT_CHECK_TEST_WORKSPACE": str(self.workspace)}):
            result = dev_check.check(self.source, self.workspace, "full", [], 1, 30, False)
        self.assertEqual(result["status"], "passed")
        self.assertTrue(result["commit_authority"])

    def test_sigterm_cancels_driver_and_owned_stage(self):
        import signal
        import time
        self.executable("make", """import os,sys,time
from pathlib import Path
if '--version' in sys.argv:print('make fixture');sys.exit(0)
Path(os.environ['ORBIT_CHECK_TEST_PROOF']).write_text(str(os.getpid()))
time.sleep(300)
""")
        driver = subprocess.Popen([sys.executable, str(Path(dev_check.__file__)), "go", "--source", str(self.source),
                                   "--workspace", str(self.workspace), "--json"], stdout=subprocess.PIPE,
                                  stderr=subprocess.PIPE, text=True)
        try:
            deadline = time.monotonic() + 10
            while not self.proof.exists() and time.monotonic() < deadline:
                time.sleep(0.02)
            self.assertTrue(self.proof.exists())
            pid = int(self.proof.read_text())
            driver.send_signal(signal.SIGTERM)
            stdout, stderr = driver.communicate(timeout=10)
            self.assertNotEqual(driver.returncode, 0, stderr)
            self.assertEqual(__import__('json').loads(stdout)["status"], "cancelled")
            with self.assertRaises(ProcessLookupError):
                os.kill(pid, 0)
        finally:
            if driver.poll() is None:
                driver.kill()
                driver.communicate()

    def test_failed_check_cannot_be_reused(self):
        (self.source / "subject.txt").write_text("fail")
        self.assertEqual(self.run_check()["status"], "failed")
        self.assertEqual(self.run_check(reuse=True)["status"], "failed")
        self.assertEqual(self.proof.read_text().count("run"), 2)

    def test_external_symlink_and_source_workspace_rejected(self):
        (self.source / "escape").symlink_to(self.proof)
        self.proof.write_text("private")
        with self.assertRaises(ValueError):
            self.run_check()
        with self.assertRaises(ValueError):
            dev_check.check(self.source, self.source / "cache", "go", [], 1, 30, False)

    def test_lint_cache_does_not_reuse_another_checkout_paths(self):
        self.executable("golangci-lint", """import json,os,sys
from pathlib import Path
if 'version' in sys.argv:print('golangci-lint has version 2.12.2');sys.exit(0)
cache=Path(os.environ.get('GOLANGCI_LINT_CACHE',os.environ['ORBIT_TEST_FAKE_LINT_CACHE']))
cache.mkdir(parents=True,exist_ok=True);saved=cache/'finding.json'
if not saved.exists():saved.write_text(json.dumps({'FromLinter':'errcheck','Text':'unchecked','Pos':{'Filename':str(Path.cwd()/'file.go'),'Line':2}}))
output=next(arg.split('=',1)[1] for arg in sys.argv if arg.startswith('--output.json.path='))
Path(output).write_text(json.dumps({'Issues':[json.loads(saved.read_text())]}))
""")
        second = self.root / "second source"
        second.mkdir()
        with mock.patch.dict(os.environ, {"ORBIT_TEST_FAKE_LINT_CACHE": str(self.root / "shared-lint-cache")}):
            for root in (self.source, second):
                (root / ".golangci-lint-version").write_text('v2.12.2')
                (root / 'file.go').write_text('package fixture\nfunc f() { call() }\n')
                entries = lint_gate.inventory(root, lint_gate.run(root))
                self.assertEqual(entries[0]['path'], 'file.go')

    def test_cli_rejects_source_and_git_workspaces_before_writing(self):
        nested = self.source / "nested"
        nested.mkdir()
        checkout = self.root / "another-checkout"
        (checkout / ".git").mkdir(parents=True)
        for workspace in (self.source, nested, checkout):
            before = sorted(str(path.relative_to(workspace)) for path in workspace.rglob("*"))
            result = subprocess.run([sys.executable, str(Path(dev_check.__file__)), "go", "--source",
                                     str(self.source), "--workspace", str(workspace)], capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(sorted(str(path.relative_to(workspace)) for path in workspace.rglob("*")), before)
        self.assertFalse(self.proof.exists())

    def test_missing_storage_never_falls_back(self):
        command = [sys.executable, str(Path(dev_check.__file__)), "go", "--source", str(self.source),
                   "--workspace", str(self.root / "not-allocated")]
        result = subprocess.run(command, capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.root / "not-allocated").exists())
        self.assertFalse(self.proof.exists())

    def test_workspace_lock_releases_after_owner_crash(self):
        code = "import sys;sys.path.insert(0,sys.argv[1]);from dev_check import workspace_lock;from pathlib import Path;" \
               "\nwith workspace_lock(Path(sys.argv[2])):\n print('ready',flush=True);sys.stdin.read()"
        child = subprocess.Popen([sys.executable, "-c", code, str(Path(dev_check.__file__).parent), str(self.workspace)],
                                 stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True)
        try:
            self.assertEqual(child.stdout.readline().strip(), "ready")
            with self.assertRaises(ValueError):
                with dev_check.workspace_lock(self.workspace):
                    self.fail("two workspace owners")
        finally:
            child.kill()
            child.wait(timeout=5)
            child.stdout.close()
            child.stdin.close()
        with dev_check.workspace_lock(self.workspace):
            pass

    def test_cancellation_kills_descendant_after_leader_exits(self):
        import signal
        import time
        child_code = "import signal,time;signal.signal(signal.SIGTERM,signal.SIG_IGN);print('ready',flush=True);time.sleep(300)"
        leader_code = "import subprocess,sys;from pathlib import Path;p=subprocess.Popen([sys.executable,'-c',sys.argv[1]],stdout=subprocess.PIPE,text=True);p.stdout.readline();Path(sys.argv[2]).write_text(str(p.pid));sys.stdin.read()"
        leader = subprocess.Popen([sys.executable, "-c", leader_code, child_code, str(self.proof)],
                                  stdin=subprocess.PIPE, start_new_session=True)
        try:
            deadline = time.monotonic() + 10
            while not self.proof.exists() and time.monotonic() < deadline:
                time.sleep(0.02)
            self.assertTrue(self.proof.exists())
            pid = int(self.proof.read_text())
            dev_check.stop_process(leader)
            deadline = time.monotonic() + 5
            while time.monotonic() < deadline:
                state = subprocess.run(["ps", "-o", "stat=", "-p", str(pid)], capture_output=True, text=True).stdout.strip()
                if not state or state.startswith("Z"):
                    break
                time.sleep(0.05)
            self.assertTrue(not state or state.startswith("Z"), state)
        finally:
            leader.stdin.close()
            if leader.poll() is None:
                leader.kill()
                leader.wait()
            if self.proof.exists():
                try:
                    os.kill(int(self.proof.read_text()), signal.SIGKILL)
                except ProcessLookupError:
                    pass

    def test_permission_error_distinguishes_zombies_from_running_group(self):
        with mock.patch("dev_check.os.killpg", side_effect=PermissionError):
            with mock.patch("dev_check.subprocess.check_output", return_value="123 Z\n456 S\n"):
                self.assertFalse(dev_check.process_group_running(123))
                self.assertTrue(dev_check.process_group_running(456))
            with mock.patch("dev_check.subprocess.check_output", side_effect=subprocess.CalledProcessError(1, 'ps')):
                with self.assertRaises(subprocess.CalledProcessError):
                    dev_check.process_group_running(123)

    def test_timeout_stops_owned_command(self):
        with self.assertRaises(subprocess.TimeoutExpired):
            dev_check.run_stage([sys.executable, "-c", "import time;time.sleep(300)"], self.source,
                                dict(os.environ), self.workspace / "timeout.log", 0.2)


class LintAndTypes(unittest.TestCase):
    def test_new_finding_is_not_offset_by_removing_old_finding(self):
        old = {"path": "old.go", "rule": "errcheck", "message": "unchecked", "scope": "func old()",
               "source": "f()", "metric": None, "count": 1}
        new = dict(old, path="new.go", scope="func new()")
        self.assertEqual(lint_gate.increases([new], [old]), [new])
        self.assertEqual(lint_gate.increases([dict(old, count=2)], [old]), [dict(old, count=2)])
        budget = dict(old, metric=140)
        self.assertEqual(lint_gate.increases([dict(budget, metric=130)], [budget]), [])
        self.assertTrue(lint_gate.increases([dict(budget, metric=141)], [budget]))

    def test_type_drift_detected_without_git(self):
        with tempfile.TemporaryDirectory(prefix="orbit-type-test-") as temporary:
            root = Path(temporary)
            output = root / "ui/src/lib/types.gen.ts"
            output.parent.mkdir(parents=True)
            output.write_text("old")
            def generate(*args, **kwargs):
                output.write_text("new")
                return subprocess.CompletedProcess([], 0)
            with mock.patch("verify_types.subprocess.run", side_effect=generate):
                with contextlib.redirect_stderr(io.StringIO()):
                    self.assertEqual(verify_types.verify(root), 1)
            self.assertFalse((root / ".git").exists())


if __name__ == "__main__":
    unittest.main()
