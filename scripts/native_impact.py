#!/usr/bin/env python3
"""Schedule native validation unless every changed path is known documentation."""
import argparse
import json
from pathlib import PurePosixPath
import re
import subprocess
import sys


def classify(paths):
    if not paths:
        return {"required": True, "reason": "no reliable changed-path evidence", "paths": []}
    for name in paths:
        path = PurePosixPath(name)
        if path.is_absolute() or ".." in path.parts or not name:
            raise ValueError("invalid changed path")
        documentation = (name in ("README.md", "README.zh-TW.md")
                         or (name.startswith("docs/") and path.suffix == ".md")
                         or (name.startswith("docs/assets/") and path.suffix == ".svg"))
        if not documentation:
            return {"required": True, "reason": "runtime, build, policy or unknown input changed", "paths": paths}
    return {"required": False, "reason": "documentation-only change", "paths": paths}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base", help="exact trusted base SHA; omission schedules native checks")
    parser.add_argument("--github-output")
    args = parser.parse_args()
    try:
        if args.base:
            if not re.fullmatch(r"[0-9a-f]{40}", args.base):
                raise ValueError("impact base must be an exact commit SHA")
            raw = subprocess.check_output(["git", "diff", "--no-renames", "--name-only", "-z", args.base, "HEAD", "--"])
            paths = [name.decode("utf-8") for name in raw.split(b"\0") if name]
        else:
            paths = []
        result = dict(schema="orbit.native-impact.v1", **classify(paths))
        if args.github_output:
            with open(args.github_output, "a") as output:
                output.write("required=" + str(result["required"]).lower() + "\n")
        print(json.dumps(result))
        return 0
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        print("native impact could not be established: " + str(error), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
