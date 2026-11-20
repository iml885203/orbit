#!/usr/bin/env python3
"""Select documented native coverage; unknown or unreliable inputs require full validation."""
import argparse
import ast
import json
import os
from pathlib import Path, PurePosixPath
import re
import subprocess
import sys

import native_checks

GENERAL_DOMAINS = ('bigquery', 'cmdmap', 'dbstate', 'devdb', 'history', 'logging',
                   'sqlpublish', 'suggest', 'tracing', 'tunnel')
GENERAL_PREFIXES = ('ui/src/', 'ui/public/', 'website/tests/', 'website/analytics/', '.vitepress/theme/') + tuple('internal/' + name + '/' for name in GENERAL_DOMAINS)
GENERAL_SUFFIXES = {'.go', '.json', '.sql', '.txt', '.yaml', '.yml', '.md', '.svg', '.svelte', '.ts',
                    '.js', '.css', '.html', '.png', '.webp', '.jpg', '.jpeg', '.gif', '.ico', '.woff', '.woff2'}
BUILD_FILES = {'package.json', 'pnpm-lock.yaml', 'pnpm-workspace.yaml', 'package-lock.json', 'yarn.lock',
               'bun.lock', 'bun.lockb', 'go.mod', 'go.sum', 'Dockerfile', 'Makefile', 'tygo.yaml'}
BUILD_CONFIG = re.compile(r'(?:\.config\.(?:js|mjs|cjs|ts|json)$|^tsconfig.*\.json$)')
HOST_REFERENCES = {'runtime', 'syscall', 'os/exec', 'os/signal', 'unsafe', 'C', 'plugin'}
HOST_PREFIXES = ('golang.org/x/sys', 'golang.org/x/term', 'github.com/gofrs/flock',
                 'github.com/spf13/cobra', 'github.com/creack/pty') + tuple(
                     'github.com/iml885203/orbit/internal/' + name for name in
                     ('app', 'cli', 'daemon', 'daemonsrv', 'platform', 'instance', 'process',
                      'container', 'dockerctx', 'preflight', 'releasepolicy', 'shellquote', 'sqlpublish'))
GO_TOKENS = re.compile(r'//[^\n]*|/\*[\s\S]*?\*/|"(?:\\.|[^"\\])*"|`[^`]*`')
PLATFORM_SUFFIX = re.compile(r'_(?:aix|android|darwin|dragonfly|freebsd|hurd|illumos|ios|js|linux|netbsd|openbsd|plan9|solaris|wasip1|windows|zos|unix|386|amd64|amd64p32|arm|arm64|loong64|mips|mips64|mips64le|mipsle|ppc64|ppc64le|riscv64|s390x|sparc64|wasm)(?:_test)?\.go$')
BUILD_CONSTRAINT = re.compile(r'^\s*//(?:go:build\s|\s*\+build\s)', re.MULTILINE)


def native_source(text):
    if BUILD_CONSTRAINT.search(text.lstrip('\ufeff')):
        return True
    for match in GO_TOKENS.finditer(text):
        token = match[0]
        if token.startswith(('//', '/*')):
            continue
        try:
            value = token[1:-1].replace('\r', '') if token.startswith('`') else ast.literal_eval(token)
        except (ValueError, SyntaxError):
            return True
        if value in HOST_REFERENCES or any(value == name or value.startswith(name + '/') for name in HOST_PREFIXES):
            return True
    return False


def documentation(name):
    path = PurePosixPath(name)
    return (name in ('README.md', 'README.zh-TW.md')
            or (name.startswith('docs/') and path.suffix == '.md')
            or (name.startswith('docs/assets/') and path.suffix == '.svg'))


def selection(profile, reason, paths):
    return {'required': profile != 'none', 'profile': profile, 'targets': native_checks.targets(profile),
            'reason': reason, 'paths': paths}


def classify(paths, versions=None):
    if not paths:
        return selection('full', 'no reliable changed-path evidence', [])
    for name in paths:
        path = PurePosixPath(name)
        if path.is_absolute() or '..' in path.parts or not name:
            raise ValueError('invalid changed path')
    general = False
    for name in paths:
        path = PurePosixPath(name)
        if documentation(name):
            continue
        if (not (name.startswith(GENERAL_PREFIXES) or name == 'ui/index.html')
                or path.suffix not in GENERAL_SUFFIXES or path.name in BUILD_FILES
                or BUILD_CONFIG.search(path.name) or PLATFORM_SUFFIX.search(name)):
            return selection('full', 'host, build, policy, platform-specific or unknown input changed', paths)
        if path.suffix == '.go':
            contents = (versions or {}).get(name)
            if not contents or not any(value is not None for value in contents):
                return selection('full', 'Go source constraints could not be established', paths)
            if any(value is not None and native_source(value) for value in contents):
                return selection('full', 'platform constraints or host-sensitive Go references changed', paths)
        general = True
    return selection('linux' if general else 'none',
                     'known general-feature/UI policy' if general else 'documentation-only change', paths)


def source_version(revision, name):
    entry = subprocess.check_output(['git', '--literal-pathspecs', 'ls-tree', '-z', revision, '--', name])
    if not entry:
        return None
    mode, kind, object_id = entry.partition(b'\t')[0].split()
    if mode not in (b'100644', b'100755') or kind != b'blob':
        raise ValueError('changed source is not a regular file')
    return subprocess.check_output(['git', 'cat-file', 'blob', object_id.decode('ascii')]).decode('utf-8')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base', help='exact trusted base SHA; omission schedules full validation')
    parser.add_argument('--github-output')
    parser.add_argument('--record', type=Path)
    args = parser.parse_args()
    try:
        head = subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip()
        versions = {}
        if args.base:
            if not re.fullmatch(r'[0-9a-f]{40}', args.base):
                raise ValueError('impact base must be an exact commit SHA')
            raw = subprocess.check_output(['git', 'diff', '--no-renames', '--name-only', '-z', args.base, head, '--'])
            paths = [name.decode('utf-8') for name in raw.split(b'\0') if name]
            for name in paths:
                if name.endswith('.go') and name.startswith(GENERAL_PREFIXES):
                    versions[name] = [source_version(args.base, name), source_version(head, name)]
        else:
            paths = []
        result = dict(schema='orbit.native-impact.v1', repository=os.environ.get('GITHUB_REPOSITORY'),
                      run_id=os.environ.get('GITHUB_RUN_ID'), commit=head,
                      head_commit=os.environ.get('CANDIDATE_HEAD_SHA', head), base=args.base,
                      **classify(paths, versions))
        if args.record:
            args.record.write_text(json.dumps(result, sort_keys=True, indent=2) + '\n')
        if args.github_output:
            with open(args.github_output, 'a') as output:
                output.write('required=' + str(result['required']).lower() + '\nprofile=' + result['profile'] + '\n')
        print(json.dumps(result))
        return 0
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        print('native impact could not be established: ' + str(error), file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
