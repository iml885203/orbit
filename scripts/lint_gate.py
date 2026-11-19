#!/usr/bin/env python3
"""Reject new Go findings and require explicit reductions of the legacy inventory."""

from collections import Counter
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import subprocess
import sys
import tempfile


BASELINE = ".golangci-baseline.json"


def finding(root, issue):
    path = Path(issue["Pos"]["Filename"])
    if path.is_absolute():
        path = path.relative_to(root)
    if ".." in path.parts:
        raise ValueError("lint finding escapes the source root")
    lines = (root / path).read_text().splitlines()
    line = issue["Pos"]["Line"]
    scope = "package"
    for text in lines[:line]:
        if re.match(r"^(func|type|var|const)\s", text):
            scope = text.strip()
    message, metric = issue["Text"], None
    patterns = {"funlen": r"\((\d+) > \d+\)", "gocognit": r"complexity (\d+) of",
                "nestif": r"\(complexity: (\d+)\)"}
    match = re.search(patterns[issue["FromLinter"]], message) if issue["FromLinter"] in patterns else None
    if match:
        metric = int(match.group(1))
        message = message[:match.start(1)] + "#" + message[match.end(1):]
    return {"path": path.as_posix(), "rule": issue["FromLinter"], "message": message,
            "scope": scope, "source": lines[line - 1].strip(), "metric": metric}


def key(entry):
    return json.dumps({k: v for k, v in entry.items() if k not in ("count", "metric")}, sort_keys=True)


def inventory(root, report):
    root = root.resolve()
    entries = {}
    for issue in report.get("Issues") or []:
        if issue["FromLinter"] == "typecheck":
            raise ValueError("type-checking errors cannot be legacy lint debt")
        item = finding(root, issue)
        identity = key(item)
        if identity in entries:
            entries[identity]["count"] += 1
            if item["metric"] is not None:
                entries[identity]["metric"] = max(entries[identity]["metric"], item["metric"])
        else:
            item["count"] = 1
            entries[identity] = item
    return [entries[k] for k in sorted(entries)]


def increases(current, allowed):
    previous = {key(x): x for x in allowed}
    return [x for x in current if key(x) not in previous or x["count"] > previous[key(x)]["count"]
            or (x["metric"] is not None and (previous[key(x)]["metric"] is None
                                           or x["metric"] > previous[key(x)]["metric"]))]


def validate_base(root, baseline, reference):
    if not re.fullmatch(r"[0-9a-f]{40}", reference):
        raise ValueError("lint base must be an exact commit SHA")
    subprocess.run(["git", "cat-file", "-e", reference + "^{commit}"], cwd=root, check=True,
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    result = subprocess.run(["git", "show", reference + ":" + BASELINE], cwd=root, capture_output=True)
    if result.returncode:
        if baseline["source_commit"] != reference:
            raise ValueError("initial baseline must identify the reviewed base commit")
        return
    old = json.loads(result.stdout)
    if increases(baseline["entries"], old["entries"]):
        raise ValueError("a PR cannot expand the legacy lint inventory")
    if not set(old["enabled_linters"]).issubset(baseline["enabled_linters"]):
        raise ValueError("a PR cannot disable existing lint checks through the baseline")


def run(root, directory=None):
    directory = (directory or root).resolve()
    pinned = (root / ".golangci-lint-version").read_text().strip().lstrip("v")
    version = subprocess.check_output(["golangci-lint", "version"], text=True)
    match = re.search(r"\bversion (\S+)", version)
    if not match or match.group(1) != pinned:
        raise ValueError("install the golangci-lint version in .golangci-lint-version")
    with tempfile.TemporaryDirectory(prefix="orbit-lint-") as temporary:
        output = Path(temporary) / "findings.json"
        env = dict(os.environ, GOOS="linux", GOARCH="amd64", CGO_ENABLED="0",
                   GOLANGCI_LINT_CACHE=str(Path(temporary) / "cache"))
        if directory == root and not (root / "cmd/orbit/dist").exists() and "-overlay" not in env.get("GOFLAGS", ""):
            fixture = Path(temporary) / "index.html"
            fixture.write_text("<title>Go lint embed fixture</title>")
            overlay = Path(temporary) / "overlay.json"
            overlay.write_text(json.dumps({"Replace": {str(root / "cmd/orbit/dist/index.html"): str(fixture)}}))
            env["GOFLAGS"] = env.get("GOFLAGS", "") + " " + shlex.quote("-overlay=" + str(overlay))
        process = subprocess.run(["golangci-lint", "run", "--issues-exit-code=0",
                                  "--max-issues-per-linter=0", "--max-same-issues=0",
                                  "--path-mode=abs", "--config=" + str(root / ".golangci.yml"),
                                  "--output.json.path=" + str(output), "./..."],
                                 cwd=directory, env=env, capture_output=True, text=True)
        if process.returncode:
            raise ValueError("golangci-lint could not complete: " + process.stderr[-4000:])
        return json.loads(output.read_text())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-ref", help="exact trusted PR base / previous main SHA (CI)")
    parser.add_argument("--ratchet", action="store_true", help="remove resolved findings / lower measured budgets only")
    args = parser.parse_args()
    root = Path(__file__).resolve().parent.parent
    try:
        baseline = json.loads((root / BASELINE).read_text())
        if baseline["schema"] != "orbit.lint-baseline.v1" or baseline["target"] != "linux/amd64":
            raise ValueError("unsupported lint baseline")
        if baseline["tool_version"] != (root / ".golangci-lint-version").read_text().strip().lstrip("v"):
            raise ValueError("lint tool version changed; review its baseline metadata")
        if baseline["config_sha256"] != hashlib.sha256((root / ".golangci.yml").read_bytes()).hexdigest():
            raise ValueError("lint configuration changed; review its baseline metadata")
        if args.base_ref:
            validate_base(root, baseline, args.base_ref)
        raw = run(root)
        for module in sorted(root.glob("tools/*/go.mod")):
            extra = run(root, module.parent)
            raw["Issues"] = (raw.get("Issues") or []) + (extra.get("Issues") or [])
        enabled = sorted(x["Name"] for x in raw["Report"]["Linters"] if x.get("Enabled"))
        if not set(baseline["enabled_linters"]).issubset(enabled):
            raise ValueError("legacy lint checks were disabled")
        current = inventory(root, raw)
        added = increases(current, baseline["entries"])
        if added:
            for item in added:
                print("{}: {}: {}".format(item["path"], item["rule"], item["message"]), file=sys.stderr)
            print("new Go lint debt is not allowed", file=sys.stderr)
            return 1
        if current != baseline["entries"]:
            if not args.ratchet:
                raise ValueError("resolved lint debt must be retired with scripts/lint_gate.py --ratchet")
            baseline["entries"] = current
            baseline["enabled_linters"] = enabled
            (root / BASELINE).write_text(json.dumps(baseline, indent=2) + "\n")
        counts = Counter(x["rule"] for x in current for _ in range(x["count"]))
        print("No new Go lint findings; {} legacy findings ({})".format(sum(counts.values()),
              ", ".join("{}:{}".format(k, v) for k, v in sorted(counts.items()))))
        return 0
    except (ValueError, KeyError, OSError, subprocess.SubprocessError) as error:
        print("lint gate failed: " + str(error), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
