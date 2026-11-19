#!/usr/bin/env python3
"""Check generated content without requiring Git metadata in a verification copy."""

import hashlib
from pathlib import Path
import subprocess
import sys


def generated_content(root):
    paths = [root / "ui/src/lib/types.gen.ts"]
    paths += sorted((root / "ui/src/lib/types").rglob("*"))
    return {str(path.relative_to(root)): hashlib.sha256(path.read_bytes()).hexdigest()
            for path in paths if path.is_file()}


def verify(root):
    before = generated_content(root)
    code = subprocess.run(["go", "run", "github.com/gzuidhof/tygo@v0.2.21", "generate"], cwd=root).returncode
    if code:
        return code
    after = generated_content(root)
    changed = sorted(name for name in before.keys() | after.keys() if before.get(name) != after.get(name))
    if changed:
        print("generated TypeScript types are out of date; run make gen-types", file=sys.stderr)
        for name in changed:
            print("  " + name, file=sys.stderr)
        return 1
    print("generated TypeScript types are current")
    return 0


if __name__ == "__main__":
    sys.exit(verify(Path(__file__).resolve().parent.parent))
