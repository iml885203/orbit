#!/usr/bin/env python3
"""Run Orbit's existing checks from an isolated source snapshot."""

import argparse
import contextlib
import hashlib
import json
import os
from pathlib import Path
import shutil
import shlex
import signal
import subprocess
import sys
import tempfile
import time
import uuid


SCHEMA = "orbit.dev-check.v1"
MODES = {
    "go": [["make", "test-go"]],
    "ui": [["pnpm", "--dir", "ui", "install", "--frozen-lockfile"],
           ["make", "test-ui-check", "test-ui-lint", "test-ui-unit"]],
    "docs": [["make", "test-docs", "docs-site-check"]],
    "full": [["make", "preflight"]],
}


def git(source, *args):
    return subprocess.check_output(["git", "-C", str(source), *args])


def source_files(source):
    paths = git(source, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
    return sorted(set(os.fsdecode(p) for p in paths.split(b"\0") if p))


def fingerprint(source, paths):
    digest = hashlib.sha256()
    for name in paths:
        relative = Path(name)
        if relative.is_absolute() or ".." in relative.parts:
            raise ValueError("source path escapes the checkout")
        path = source / relative
        digest.update(os.fsencode(name) + b"\0")
        if path.is_symlink():
            target = path.resolve(strict=True)
            target.relative_to(source)
            digest.update(b"link:" + os.fsencode(str(target.relative_to(source))))
        elif path.is_file():
            digest.update(str(path.stat().st_mode & 0o777).encode() + b"\0")
            digest.update(path.read_bytes())
        elif not path.exists():
            digest.update(b"deleted")
        else:
            raise ValueError("unsupported source path: " + name)
    return digest.hexdigest()


def copy_source(source, destination, paths):
    destination.mkdir(parents=True)
    for name in paths:
        original, copied = source / name, destination / name
        if not original.exists() and not original.is_symlink():
            continue
        copied.parent.mkdir(parents=True, exist_ok=True)
        if original.is_symlink():
            relative = original.resolve(strict=True).relative_to(source)
            copied.symlink_to(os.path.relpath(destination / relative, copied.parent))
        else:
            shutil.copy2(original, copied)


def tool_identity(name, args):
    executable = shutil.which(name)
    if not executable:
        raise ValueError("required tool is missing: " + name)
    result = subprocess.run([executable, *args], capture_output=True, text=True, timeout=30)
    if result.returncode:
        raise ValueError("cannot identify tool: " + name)
    stat = Path(executable).resolve().stat()
    return {"version": result.stdout.strip(), "path": str(Path(executable).resolve()),
            "size": stat.st_size, "mtime_ns": stat.st_mtime_ns}


def write_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(path.name + ".new")
    temporary.write_text(json.dumps(value, indent=2) + "\n")
    temporary.replace(path)


def stop_process(process):
    if os.name == "nt":
        subprocess.run(["taskkill", "/PID", str(process.pid), "/T", "/F"], capture_output=True)
    else:
        try:
            os.killpg(process.pid, signal.SIGTERM)
        except ProcessLookupError:
            return
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            process.poll()
            try:
                os.killpg(process.pid, 0)
            except ProcessLookupError:
                return
            time.sleep(0.05)
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
    process.wait(timeout=5)


def run_stage(command, cwd, env, log, timeout):
    started = time.monotonic()
    with log.open("wb") as output:
        process = subprocess.Popen(command, cwd=cwd, env=env, stdout=output,
                                   stderr=subprocess.STDOUT, start_new_session=os.name != "nt")
        try:
            code = process.wait(timeout=timeout)
        except (subprocess.TimeoutExpired, KeyboardInterrupt):
            stop_process(process)
            raise
    return {"command": command, "seconds": round(time.monotonic() - started, 3),
            "exit_code": code, "log": log.name}


@contextlib.contextmanager
def workspace_lock(workspace):
    with (workspace / "validation.lock").open("a+b") as lock:
        if os.name == "nt":
            import msvcrt
            lock.write(b"0")
            lock.flush()
            lock.seek(0)
            try:
                msvcrt.locking(lock.fileno(), msvcrt.LK_NBLCK, 1)
            except OSError as error:
                raise ValueError("validation workspace is busy") from error
        else:
            import fcntl
            try:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError as error:
                raise ValueError("validation workspace is busy") from error
        try:
            yield
        finally:
            if os.name == "nt":
                lock.seek(0)
                msvcrt.locking(lock.fileno(), msvcrt.LK_UNLCK, 1)


@contextlib.contextmanager
def socket_root(workspace):
    target = workspace / "temporary"
    target.mkdir(exist_ok=True)
    if os.name == "nt":
        yield target
        return
    base = Path.home() / ".local" / "state"
    base.mkdir(parents=True, exist_ok=True)
    alias = base / ("ov-" + uuid.uuid4().hex[:8])
    if len(os.fsencode(str(alias))) > 60:
        raise ValueError("home path is too long for a short socket alias")
    alias.symlink_to(target, target_is_directory=True)
    try:
        yield alias
    finally:
        if alias.is_symlink() and alias.resolve() == target.resolve():
            alias.unlink()


def check(source, workspace, mode, packages, jobs, timeout, reuse):
    source, workspace = source.resolve(), workspace.resolve()
    if source == workspace or source in workspace.parents or workspace in source.parents:
        raise ValueError("validation workspace must be outside the source checkout")
    if (workspace / ".git").exists():
        raise ValueError("a Git checkout cannot be a validation workspace")
    if os.name == "nt" and mode != "go":
        raise ValueError("Windows Beta supports Go checks here; use WSL or CI for full/UI/docs checks")
    for key in ("ORBIT_UI_EXT", "ORBIT_UI_TYPES", "ORBIT_UI_OUTDIR"):
        if os.environ.get(key):
            raise ValueError("custom distribution override requires its own build workflow: " + key)
    if jobs < 1 or timeout <= 0:
        raise ValueError("jobs and timeout must be positive")
    for package in packages:
        if not package.startswith("./") or ".." in Path(package).parts:
            raise ValueError("Go packages must stay inside this repository")
    if reuse and mode == "full":
        raise ValueError("full preflight must run fresh before each commit")
    if os.environ.get("GOWORK", "off") not in ("", "off"):
        raise ValueError("external GOWORK is unsupported; use GOWORK=off")
    flags = shlex.split(os.environ.get("GOFLAGS", ""))
    if any(flag.split("=", 1)[0] in ("-overlay", "-modfile") for flag in flags):
        raise ValueError("external Go overlay/modfile inputs are unsupported")
    paths = source_files(source)
    identity = fingerprint(source, paths)
    tags = git(source, "tag", "--list", "v*", "--sort=-v:refname").decode()
    tools = {"make": tool_identity("make", ["--version"])}
    if mode in ("go", "full"):
        tools["go"] = tool_identity("go", ["version"])
    if mode != "go":
        tools.update(node=tool_identity("node", ["--version"]), pnpm=tool_identity("pnpm", ["--version"]))
    relevant = {key: value for key, value in os.environ.items()
                if key.startswith(("GO", "CGO", "NODE", "PNPM", "npm_config_")) or key in ("CI", "TZ", "LANG", "PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH")}
    inputs = {"source": identity, "commit": git(source, "rev-parse", "HEAD").decode().strip(),
              "tags": hashlib.sha256(tags.encode()).hexdigest(), "tools": tools,
              "environment": hashlib.sha256(json.dumps(relevant, sort_keys=True).encode()).hexdigest(),
              "mode": mode, "packages": packages, "jobs": jobs, "timeout": timeout}
    key = hashlib.sha256(json.dumps(inputs, sort_keys=True).encode()).hexdigest()
    report_path = workspace / "reports" / (key + ".json")
    if reuse and report_path.exists():
        report = json.loads(report_path.read_text())
        if report.get("status") == "passed" and report.get("inputs") == inputs:
            report["reused"] = True
            return report
    run = workspace / "runs" / uuid.uuid4().hex
    snapshot = run / "source"
    copy_source(source, snapshot, paths)
    if fingerprint(snapshot, paths) != identity or fingerprint(source, source_files(source)) != identity:
        raise ValueError("source changed while taking the validation snapshot")
    release_tags = run / "release-tags.txt"
    release_tags.write_text(tags)
    report = {"schema": SCHEMA, "inputs": inputs, "status": "running", "stages": [], "run": run.name,
              "reused": False, "commit_authority": False}
    write_json(report_path, report)
    started = time.monotonic()
    try:
        with socket_root(workspace) as temporary:
            env = dict(os.environ, GOWORK="off")
            for name in ("MAKE", "MAKEFLAGS", "MFLAGS", "GNUMAKEFLAGS", "MAKEFILES", "MAKELEVEL", "MAKEOVERRIDES"):
                env.pop(name, None)
            for name in ("TMPDIR", "TMP", "TEMP", "GOTMPDIR", "ORBIT_TEST_SOCKET_ROOT"):
                env[name] = str(temporary)
            for name, relative in {"GOCACHE": "cache/go-build", "GOMODCACHE": "cache/go-mod", "XDG_CACHE_HOME": "cache/xdg",
                                   "npm_config_cache": "cache/npm", "npm_config_store_dir": "cache/pnpm-store",
                                   "PLAYWRIGHT_BROWSERS_PATH": "cache/playwright", "ORBIT_HOME": "runtime/home",
                                   "ORBIT_UPDATE_HOME": "runtime/updates", "ORBIT_INSTANCE_BASE_HOME": "runtime/instances"}.items():
                env[name] = str(workspace / relative)
            env["ORBIT_SOURCE_RELEASE_TAGS_FILE"] = str(release_tags)
            if not env.get("PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH"):
                chrome = Path("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")
                if sys.platform == "darwin" and chrome.is_file():
                    env["PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH"] = str(chrome)
            for index, original in enumerate(MODES[mode]):
                command = list(original)
                if command[0] == "make":
                    command += ["CHECK_JOBS=" + str(jobs), "GO_TEST_PARALLELISM=" + str(jobs)]
                    if mode == "full":
                        command += ["GO_TEST_PACKAGES=./..."]
                    if mode == "go":
                        command += ["GO_TEST_PACKAGES=" + " ".join(packages)]
                stage = run_stage(command, snapshot, env, run / (str(index) + ".log"), timeout)
                stage["log"] = str((run / stage["log"]).relative_to(workspace))
                report["stages"].append(stage)
                write_json(report_path, report)
                if stage["exit_code"]:
                    report["status"] = "failed"
                    break
            else:
                report["status"] = "passed"
            if fingerprint(snapshot, paths) != identity:
                report["status"] = "snapshot_changed"
            if fingerprint(source, source_files(source)) != identity:
                report["status"] = "source_changed"
    except (KeyboardInterrupt, subprocess.TimeoutExpired) as error:
        report["status"] = "cancelled" if isinstance(error, KeyboardInterrupt) else "timed_out"
    except Exception:
        report["status"] = "failed"
        raise
    finally:
        report["commit_authority"] = mode == "full" and report["status"] == "passed"
        report["seconds"] = round(time.monotonic() - started, 3)
        write_json(report_path, report)
        write_json(run / "report.json", report)
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=MODES)
    parser.add_argument("--source", type=Path, default=Path(__file__).resolve().parent.parent)
    storage = parser.add_mutually_exclusive_group(required=True)
    storage.add_argument("--workspace", type=Path,
                        help="existing task workspace allocated under your storage policy")
    storage.add_argument("--storage-helper", type=Path,
                         help="allocate once using HELPER create orbit-validation; caller releases it after the task")
    parser.add_argument("--package", action="append", dest="packages")
    parser.add_argument("--jobs", type=int, default=2)
    parser.add_argument("--timeout", type=float, default=900)
    parser.add_argument("--reuse", action="store_true")
    parser.add_argument("--json", action="store_true")
    args = parser.parse_args()
    if hasattr(signal, "SIGTERM"):
        def cancel(signum, frame):
            raise KeyboardInterrupt
        signal.signal(signal.SIGTERM, cancel)
    try:
        if args.storage_helper:
            allocation = subprocess.run([str(args.storage_helper), "create", "orbit-validation"],
                                        capture_output=True, text=True, timeout=60)
            if allocation.returncode:
                raise ValueError("storage helper failed; no fallback workspace was created")
            args.workspace = Path(allocation.stdout.strip())
        if not args.workspace.is_dir():
            raise ValueError("allocate the task workspace with your storage helper first")
        with workspace_lock(args.workspace):
            report = check(args.source, args.workspace, args.mode, args.packages or ["./internal/..."],
                           args.jobs, args.timeout, args.reuse)
        if args.json:
            report["workspace"] = str(args.workspace.resolve())
            print(json.dumps(report))
        else:
            print("{}: {} ({:.2f}s{})".format(args.mode, report["status"], report["seconds"],
                                             ", reused" if report["reused"] else ""))
            for stage in report["stages"]:
                print("  {:.2f}s  {}".format(stage["seconds"], " ".join(stage["command"])))
            print("Workspace: " + str(args.workspace.resolve()))
        return 0 if report["status"] == "passed" else 1
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        print("validation could not run: " + str(error), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
