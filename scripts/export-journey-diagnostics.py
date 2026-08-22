#!/usr/bin/env python3

import json
import os
import pathlib
import re
import sys


MAX_FILES = 24
MAX_FILE_BYTES = 64 * 1024
MAX_TOTAL_BYTES = 512 * 1024
MAX_MANIFEST_BYTES = 32 * 1024
DAEMON_TAIL_LINES = 200
SENSITIVE_KEY = re.compile(
    r"(^|_)(access_key|api_key|authorization|connection_string|cookie|credential|database_url|dsn|env|environment|password|private_key|secret|token)($|_)",
    re.IGNORECASE,
)
SENSITIVE_ASSIGNMENT = re.compile(
    r"(?i)([\"']?[A-Za-z0-9_.-]*(?:access[_-]?key|api[_-]?key|authorization|connection[_-]?string|cookie|credential|database[_-]?url|dsn|password|passwd|private[_-]?key|secret|token)[A-Za-z0-9_.-]*[\"']?[ \t]*[:=][ \t]*)(?:\"[^\"\n]*\"|'[^'\n]*'|[^\s,;}]+(?:[ \t]+[^\s,;}]+)?)"
)
URL_CREDENTIAL = re.compile(r"(https?://)[^/@\s]+@", re.IGNORECASE)
SENSITIVE_QUERY = re.compile(
    r"(?i)([?&](?:access[_-]?key|api[_-]?key|password|secret|token)=)[^&#\s]+"
)
PRIVATE_KEY_BLOCK = re.compile(
    r"-----BEGIN [^-\n]*PRIVATE KEY-----.*?-----END [^-\n]*PRIVATE KEY-----",
    re.DOTALL,
)
TOKEN_SHAPE = re.compile(r"\b(?:gh[oprsu]_[A-Za-z0-9_]{20,}|AKIA[A-Z0-9]{16})\b")
SENSITIVE_MARKER = re.compile(
    r"(?i)access|api.?key|authorization|connection|cookie|credential|database|dsn|password|passwd|private|secret|token|https?://|AKIA|gh[oprsu]_"
)
JOURNEY_FILES = {
    "quickstart": {
        "control-stop.json", "down-again.json", "down.json", "offline-group-typo.json",
        "offline-logs-typo.json", "offline-open-typo.json", "offline-resource-typo.json",
        "offline-restart-typo.json", "offline-up-typo.json", "recover-inventory.json",
        "runtime-failure.json", "runtime-graph.json", "runtime-recovered.json",
        "runtime-threshold.json", "up.stderr", "up.json",
    },
    "local-first": {
        "ambiguous-readiness-inspect.json", "ambiguous-readiness.json", "doctor.json",
        "down.json", "logs.json", "open.json", "shared-down.json", "shared-status.json",
        "shared-up.json", "status.json", "stopped.json", "up-conflict.json", "up.stderr",
        "up.json",
    },
    "project-context-switch": {
        "doctor-after-down.json", "down-outside-project.json", "explicit-envs-api.json",
        "explicit-managed-status.json", "explicit-outside-matched-status.json",
        "explicit-outside-status.json", "explicit-up.json", "same-name-refused.json",
        "status-invalid-project.json", "status-missing-project.json", "status-outside-project.json",
        "up-a.stderr", "up-a.json", "up-b.json",
    },
    "runtime-adoption": {"doctor.stderr", "doctor.json", "status.json", "up.stderr", "up.json"},
    "startup-readiness": {
        "client-logs.json", "delayed-status.json", "delayed-up.stderr", "delayed-up.json",
        "emitted-status.json", "invalid-doctor.json", "invalid-up.json", "silent-logs.json",
        "silent-status.json", "silent-up.json",
    },
    "late-stage-fixture": {"assertion.stderr", "late-status.json", "oversized.stderr"},
}


def fail(message: str) -> None:
    print(message, file=sys.stderr)
    raise SystemExit(2)


def safe_component(value: str, label: str) -> str:
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]*", value):
        fail(f"Invalid {label}: {value}")
    return value


def redact_json(value):
    if isinstance(value, dict):
        return {
            key: "[redacted]" if sensitive_key(key) else redact_json(item)
            for key, item in value.items()
        }
    if isinstance(value, list):
        return [redact_json(item) for item in value]
    if isinstance(value, str):
        return redact_text(value)
    return value


def sensitive_key(key: str) -> bool:
    snake_case = re.sub(r"([a-z0-9])([A-Z])", r"\1_\2", key)
    normalized = re.sub(r"[^A-Za-z0-9]+", "_", snake_case).lower().strip("_")
    return SENSITIVE_KEY.search(normalized) is not None


def redact_text(value: str) -> str:
    if SENSITIVE_MARKER.search(value) is None:
        return value
    value = PRIVATE_KEY_BLOCK.sub("[redacted private key]", value)
    value = URL_CREDENTIAL.sub(r"\1[redacted]@", value)
    value = SENSITIVE_QUERY.sub(r"\1[redacted]", value)
    value = SENSITIVE_ASSIGNMENT.sub(r"\1[redacted]", value)
    return TOKEN_SHAPE.sub("[redacted token]", value)


def bounded(data: bytes, remaining: int) -> bytes:
    limit = min(MAX_FILE_BYTES, remaining)
    if len(data) <= limit:
        return data
    marker = b"\n[truncated]\n"
    return data[: max(0, limit - len(marker))] + marker


def diagnostic_candidates(journey: str, test_root: pathlib.Path):
    candidates = []
    for name in JOURNEY_FILES[journey]:
        path = test_root / name
        if path.is_symlink() or not path.is_file():
            continue
        relative = path.relative_to(test_root)
        candidates.append((path.stat().st_mtime_ns, path, relative))
    for suffix in (".stderr", ".json"):
        matching = (candidate for candidate in candidates if candidate[1].suffix == suffix)
        for _, path, relative in sorted(matching, key=lambda candidate: candidate[0], reverse=True):
            yield path, relative


def sanitized_contents(path: pathlib.Path) -> bytes:
    raw = path.read_bytes()[: MAX_FILE_BYTES * 2]
    text = raw.decode("utf-8", errors="replace")
    if path.suffix == ".json":
        try:
            value = json.loads(text)
        except json.JSONDecodeError:
            return redact_text(text).encode()
        return (json.dumps(redact_json(value), indent=2, sort_keys=True) + "\n").encode()
    return redact_text(text).encode()


def bounded_daemon_tail(path: pathlib.Path) -> bytes:
    read_limit = MAX_FILE_BYTES * 2
    with path.open("rb") as daemon_file:
        daemon_file.seek(0, os.SEEK_END)
        size = daemon_file.tell()
        daemon_file.seek(max(0, size - read_limit))
        raw = daemon_file.read(read_limit)
    if size > read_limit:
        _, _, raw = raw.partition(b"\n")
    lines = raw.decode("utf-8", errors="replace").splitlines()
    return redact_text("\n".join(lines[-DAEMON_TAIL_LINES:]) + "\n").encode()


def main() -> None:
    if len(sys.argv) != 5:
        fail("usage: export-journey-diagnostics.py <journey> <attempt> <test-root> <export-root>")

    journey = safe_component(sys.argv[1], "journey name")
    if journey not in JOURNEY_FILES:
        fail(f"Unknown journey diagnostic allowlist: {journey}")
    attempt = safe_component(sys.argv[2], "attempt")
    test_root = pathlib.Path(sys.argv[3]).resolve(strict=True)
    export_root = pathlib.Path(sys.argv[4]).resolve()
    destination = export_root / journey / f"attempt-{attempt}"
    destination.mkdir(parents=True, exist_ok=True)

    written = 0
    total = 0
    manifest = []
    candidates = []

    daemon_log = pathlib.Path(os.environ.get("ORBIT_HOME", "")) / "daemon.log"
    if daemon_log.is_file() and daemon_log.resolve().is_relative_to(test_root):
        daemon_data = bounded_daemon_tail(daemon_log)
        candidates.append((daemon_log, pathlib.Path("daemon-tail.log"), daemon_data))
    candidates.extend(diagnostic_candidates(journey, test_root))

    for candidate in candidates:
        if written >= MAX_FILES - 1 or total >= MAX_TOTAL_BYTES - MAX_MANIFEST_BYTES:
            break
        if len(candidate) == 2:
            source, relative = candidate
            data = sanitized_contents(source)
        else:
            source, relative, data = candidate
        data = bounded(data, MAX_TOTAL_BYTES - MAX_MANIFEST_BYTES - total)
        if not data:
            continue
        output_name = f"{written + 1:02d}-{str(relative).replace(os.sep, '__')}"
        output = destination / output_name
        output.write_bytes(data)
        manifest.append({"file": output_name, "source": str(relative), "bytes": len(data)})
        written += 1
        total += len(data)

    manifest_data = (
        json.dumps(
            {
                "journey": journey,
                "attempt": attempt,
                "files": manifest,
                "limits": {
                    "file_count_including_manifest": MAX_FILES,
                    "per_file_bytes": MAX_FILE_BYTES,
                    "total_bytes_including_manifest": MAX_TOTAL_BYTES,
                    "daemon_tail_lines": DAEMON_TAIL_LINES,
                },
            },
            indent=2,
            sort_keys=True,
        )
        + "\n"
    ).encode()
    if len(manifest_data) > MAX_MANIFEST_BYTES:
        fail("Journey diagnostic manifest exceeded its reserved size limit.")
    (destination / "manifest.json").write_bytes(manifest_data)


if __name__ == "__main__":
    main()
