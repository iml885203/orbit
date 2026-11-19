#!/usr/bin/env python3
"""Build, verify and consume one exact Orbit candidate inventory in Actions."""
import argparse
import datetime
import hashlib
import json
from pathlib import Path
import re
import os
import shutil
import sys
import time
import subprocess
import shlex

TARGETS = ('darwin/amd64', 'darwin/arm64', 'linux/amd64', 'linux/arm64', 'windows/amd64', 'windows/arm64')


def sha(path):
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            digest.update(block)
    return digest.hexdigest()


def tree(root):
    if not root.is_dir() or root.is_symlink():
        raise ValueError('missing candidate directory')
    result = {}
    for path in sorted(root.rglob('*')):
        if path.is_symlink():
            raise ValueError('candidate links are forbidden')
        if path.is_file():
            result[path.relative_to(root).as_posix()] = sha(path)
    return result


def asset(target):
    name = 'orbit-' + target.replace('/', '-')
    return name + ('.exe' if target.startswith('windows/') else '')


def inventory_names():
    return {asset(target) for target in TARGETS} | {'checksums.txt', 'orbit.spdx.json'}


def validate_identity(value, expected):
    if value.get('schema') != 'orbit.candidate.v1':
        raise ValueError('unsupported candidate manifest')
    for name in ('repository', 'commit', 'version', 'run_id', 'producer_attempt', 'workflow_ref', 'head_commit'):
        if value.get(name) != expected[name]:
            raise ValueError('candidate identity mismatch: ' + name)
    if not re.fullmatch(r'[0-9a-f]{40}', value['commit']):
        raise ValueError('candidate source must be an exact SHA')
    if not re.fullmatch(r'v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?', value['version']):
        raise ValueError('candidate version must be semantic')


def verify(root, expected, manifest_sha):
    if {p.name for p in root.iterdir()} != {'dist', 'embed', 'manifest.json'} or any(p.is_symlink() for p in root.iterdir()):
        raise ValueError('unexpected candidate contents')
    manifest = root / 'manifest.json'
    if sha(manifest) != manifest_sha:
        raise ValueError('candidate manifest digest mismatch')
    value = json.loads(manifest.read_text())
    validate_identity(value, expected)
    if set(value['assets']) != inventory_names() or tree(root / 'dist') != value['assets']:
        raise ValueError('candidate asset inventory or digest mismatch')
    if not value['embed'] or tree(root / 'embed') != value['embed']:
        raise ValueError('candidate dashboard inventory or digest mismatch')
    actual_checksums = {}
    for line in (root / 'dist/checksums.txt').read_text().splitlines():
        match = re.fullmatch(r'([0-9a-f]{64})  (orbit-[a-z]+-(?:amd64|arm64)(?:\.exe)?)', line)
        if not match or match[2] in actual_checksums:
            raise ValueError('invalid candidate checksums')
        actual_checksums[match[2]] = match[1]
    if actual_checksums != {asset(t): value['assets'][asset(t)] for t in TARGETS}:
        raise ValueError('candidate checksum inventory mismatch')
    for target in TARGETS:
        info = json.loads(subprocess.check_output(['go', 'version', '-m', '-json', str(root / 'dist' / asset(target))], text=True))
        if info.get('GoVersion') != value['tools']['go'] or info.get('Path') != 'github.com/iml885203/orbit/cmd/orbit':
            raise ValueError('candidate toolchain or main package mismatch')
        settings = {entry['Key']: entry['Value'] for entry in info['Settings']}
        goos, goarch = target.split('/')
        for name, wanted in {'vcs.revision': value['commit'], 'GOOS': goos, 'GOARCH': goarch, 'CGO_ENABLED': '0', 'vcs.modified': 'false'}.items():
            if settings.get(name) != wanted:
                raise ValueError('candidate Go build metadata mismatch: ' + name)
        flags = shlex.split(settings.get('-ldflags', ''))
        if flags.count('main.version=' + value['version']) != 1:
            raise ValueError('candidate executable version metadata mismatch')
    return value


def verify_receipts(root, candidate, manifest_sha, artifact_id, run_id):
    names = {'native-' + target.replace('/', '-') + '.json' for target in TARGETS}
    if {path.name for path in root.iterdir()} != names or any(path.is_symlink() for path in root.iterdir()):
        raise ValueError('unexpected native receipt inventory')
    values = [json.loads(path.read_text()) for path in root.glob('*.json')]
    if len(values) != len(TARGETS) or {v.get('target') for v in values} != set(TARGETS):
        raise ValueError('all six unique native receipts are required')
    for value in values:
        if value.get('schema') != 'orbit.native-receipt.v1' or value.get('result') != 'passed' or value.get('suite') != 'native':
            raise ValueError('native receipt is incomplete')
        for name, wanted in {'manifest_sha256': manifest_sha, 'artifact_id': artifact_id, 'run_id': run_id,
                             'commit': candidate['commit'], 'version': candidate['version'],
                             'producer_attempt': candidate['producer_attempt'],
                             'binary_sha256': candidate['assets'][asset(value['target'])]}.items():
            if value.get(name) != wanted:
                raise ValueError('native receipt identity mismatch: ' + name)
    return values


def identity(version, attempt=None, workflow_ref=None):
    value = {'schema': 'orbit.candidate.v1', 'repository': os.environ['GITHUB_REPOSITORY'],
             'commit': os.environ['GITHUB_SHA'], 'version': version,
             'head_commit': os.environ.get('CANDIDATE_HEAD_SHA', os.environ['GITHUB_SHA']),
             'run_id': os.environ['GITHUB_RUN_ID'],
             'producer_attempt': attempt or os.environ['GITHUB_RUN_ATTEMPT'],
             'workflow_ref': workflow_ref or os.environ['GITHUB_WORKFLOW_REF']}
    validate_identity(value, value)
    if not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', value['repository']):
        raise ValueError('invalid candidate repository')
    if not re.fullmatch(r'[0-9a-f]{40}', value['head_commit']):
        raise ValueError('invalid candidate head SHA')
    if not all(re.fullmatch(r'[1-9][0-9]*', value[key]) for key in ('run_id', 'producer_attempt')):
        raise ValueError('invalid producer run identity')
    if not value['workflow_ref'].startswith(value['repository'] + '/.github/workflows/'):
        raise ValueError('invalid producer workflow ref')
    return value


def save(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, sort_keys=True, indent=2) + '\n')


def timed(command, env=None):
    started = time.monotonic()
    subprocess.run(command, env=env, check=True)
    return round(time.monotonic() - started, 3)


def build(root, version, record):
    expected = identity(version)
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip()
    if revision != expected['commit']:
        raise ValueError('checkout does not match candidate source')
    root.mkdir(parents=True, exist_ok=False)
    (root / 'dist').mkdir()
    timings = {'dashboard_dependencies': timed(['pnpm', '--dir', 'ui', 'install', '--frozen-lockfile']),
               'dashboard': timed(['pnpm', '--dir', 'ui', 'run', 'build']), 'executables': {}}
    shutil.copytree('cmd/orbit/dist', root / 'embed')
    build_time = datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')
    for target in TARGETS:
        goos, goarch = target.split('/')
        env = dict(os.environ, CGO_ENABLED='0', GOOS=goos, GOARCH=goarch)
        timings['executables'][target] = timed(['go', 'build', '-ldflags',
            '-s -w -X main.version=' + version + ' -X main.buildTime=' + build_time,
            '-o', str(root / 'dist' / asset(target)), './cmd/orbit'], env=env)
    tools = {name: subprocess.check_output(command, text=True).strip() for name, command in {
        'go': ['go', 'env', 'GOVERSION'], 'node': ['node', '--version'], 'pnpm': ['pnpm', '--version']}.items()}
    inputs = {name: sha(Path(name)) for name in ('go.mod', 'go.sum', 'ui/pnpm-lock.yaml', 'VERSION',
                                                '.github/workflows/candidate.yml', 'scripts/candidate.py')}
    save(record, dict(expected, tools=tools, inputs=inputs, timings=timings,
                      build_counts={'dashboard': 1, 'product_executables': len(TARGETS)}))


def create(root, expected, record, github_output):
    value = json.loads(record.read_text())
    validate_identity(value, expected)
    if set(tree(root / 'dist')) != inventory_names() - {'checksums.txt'}:
        raise ValueError('candidate build must provide six executables and SBOM exactly')
    checksums = ''.join(sha(root / 'dist' / name) + '  ' + name + '\n' for name in sorted(asset(t) for t in TARGETS))
    (root / 'dist/checksums.txt').write_text(checksums)
    value.update(assets=tree(root / 'dist'), embed=tree(root / 'embed'))
    save(root / 'manifest.json', value)
    digest = sha(root / 'manifest.json')
    verify(root, expected, digest)
    if github_output:
        with github_output.open('a') as output:
            for key, item in {'manifest-sha256': digest, 'producer-attempt': value['producer_attempt'],
                              'workflow-ref': value['workflow_ref']}.items():
                output.write(key + '=' + item + '\n')
    print(json.dumps({'manifest_sha256': digest, 'identity': expected, 'build_counts': value['build_counts'],
                      'timings': value['timings']}))


def verify_remote(expected, artifact_id, archive_digest):
    if not re.fullmatch(r'[1-9][0-9]*', artifact_id) or not re.fullmatch(r'[0-9a-f]{64}', archive_digest):
        raise ValueError('invalid artifact ID or archive digest')
    metadata = json.loads(subprocess.check_output(['gh', 'api',
        'repos/' + expected['repository'] + '/actions/artifacts/' + artifact_id], text=True))
    if (str(metadata['id']) != artifact_id or metadata['expired']
            or metadata.get('digest') != 'sha256:' + archive_digest
            or metadata['name'] != 'orbit-candidate-' + expected['run_id'] + '-' + expected['producer_attempt']
            or str(metadata['workflow_run']['id']) != expected['run_id']
            or metadata['workflow_run']['head_sha'] != expected['head_commit']):
        raise ValueError('artifact is stale, foreign or does not match the trusted producer output')
    return metadata


def install(root, value, target, destination):
    if target not in TARGETS:
        raise ValueError('unknown native target')
    native = subprocess.check_output(['go', 'env', 'GOOS', 'GOARCH'], text=True).split()
    if native != target.split('/'):
        raise ValueError('native source toolchain does not match target')
    destination.mkdir(parents=True, exist_ok=True)
    binary = destination / ('orbit.exe' if target.startswith('windows/') else 'orbit')
    shutil.copy2(root / 'dist' / asset(target), binary)
    binary.chmod(0o755)
    shutil.copytree(root / 'embed', Path('cmd/orbit/dist'), dirs_exist_ok=True)
    verify_binary(binary, value, target)


def verify_binary(binary, value, target):
    if sha(binary) != value['assets'][asset(target)]:
        raise ValueError('executed binary differs from candidate')
    home = Path(os.environ['RUNNER_TEMP']) / 'candidate-version-home'
    env = dict(os.environ, ORBIT_HOME=str(home), ORBIT_UPDATE_HOME=str(home / 'updates'), ORBIT_UPDATE_BACKGROUND='1')
    version = json.loads(subprocess.check_output([str(binary.resolve()), 'version', '--json'], env=env, text=True))
    reported = version.get('data', {}).get('version')
    if (version.get('schema_version') != 'orbit.cli.v1' or not version.get('ok')
            or not isinstance(reported, str) or reported.split(maxsplit=1)[:1] != [value['version']]):
        raise ValueError('executed candidate reports the wrong version')


def receipt(value, target, binary, directory, manifest_sha, artifact_id, suite):
    verify_binary(binary, value, target)
    evidence = {'schema': 'orbit.native-receipt.v1', 'result': 'passed', 'target': target, 'suite': suite,
                'manifest_sha256': manifest_sha, 'artifact_id': artifact_id, 'commit': value['commit'],
                'version': value['version'], 'run_id': value['run_id'],
                'producer_attempt': value['producer_attempt'], 'consumer_attempt': os.environ['GITHUB_RUN_ATTEMPT'],
                'job': os.environ['GITHUB_JOB'], 'binary_sha256': sha(binary)}
    save(directory / (suite + '-' + target.replace('/', '-') + '.json'), evidence)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', choices=('build', 'create', 'consume', 'receipt', 'promote', 'identity'))
    parser.add_argument('--directory', type=Path, required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--record', type=Path)
    parser.add_argument('--producer-attempt')
    parser.add_argument('--workflow-ref')
    parser.add_argument('--manifest-sha256')
    parser.add_argument('--artifact-id')
    parser.add_argument('--archive-digest')
    parser.add_argument('--github-output', type=Path)
    parser.add_argument('--target', choices=TARGETS)
    parser.add_argument('--install-directory', type=Path, default=Path('bin'))
    parser.add_argument('--publish-directory', type=Path, default=Path('dist'))
    parser.add_argument('--receipts', type=Path)
    parser.add_argument('--sql-receipts', type=Path)
    parser.add_argument('--suite', choices=('native', 'sqlserver'), default='native')
    args = parser.parse_args()
    required = {'build': ('record',), 'create': ('record',), 'consume': ('target',),
                'receipt': ('target', 'receipts'), 'promote': ('receipts', 'sql_receipts'), 'identity': ('record',)}
    fields = required[args.command]
    if args.command not in ('build', 'create'):
        fields += ('manifest_sha256', 'artifact_id', 'archive_digest', 'producer_attempt', 'workflow_ref')
    missing = [name for name in fields if getattr(args, name) is None]
    if missing:
        parser.error('missing options: ' + ', '.join('--' + name.replace('_', '-') for name in missing))
    try:
        root = args.directory.resolve()
        expected = identity(args.version, args.producer_attempt, args.workflow_ref)
        if args.command == 'build':
            build(root, args.version, args.record)
        elif args.command == 'create':
            create(root, expected, args.record, args.github_output)
        else:
            value = verify(root, expected, args.manifest_sha256)
            verify_remote(expected, args.artifact_id, args.archive_digest)
            if args.command == 'consume':
                install(root, value, args.target, args.install_directory)
            elif args.command == 'receipt':
                binary = args.install_directory / ('orbit.exe' if args.target.startswith('windows/') else 'orbit')
                receipt(value, args.target, binary, args.receipts, args.manifest_sha256, args.artifact_id, args.suite)
            elif args.command == 'promote':
                verify_receipts(args.receipts, value, args.manifest_sha256, args.artifact_id, expected['run_id'])
                sql = list(args.sql_receipts.glob('*.json'))
                if len(sql) != 1:
                    raise ValueError('one SQL Server receipt is required')
                evidence = json.loads(sql[0].read_text())
                if any(evidence.get(key) != wanted for key, wanted in {
                    'schema': 'orbit.native-receipt.v1', 'suite': 'sqlserver', 'target': 'linux/amd64',
                    'result': 'passed', 'artifact_id': args.artifact_id, 'manifest_sha256': args.manifest_sha256,
                    'commit': value['commit'], 'version': value['version'], 'run_id': expected['run_id'],
                    'binary_sha256': value['assets']['orbit-linux-amd64']}.items()):
                    raise ValueError('SQL Server evidence does not match the candidate')
                shutil.copytree(root / 'dist', args.publish_directory)
                if tree(args.publish_directory) != value['assets']:
                    raise ValueError('promotion changed candidate bytes')
            elif args.command == 'identity':
                save(args.record, {'schema': 'orbit.candidate-identity.v1', 'artifact_id': args.artifact_id,
                                  'archive_digest': args.archive_digest, 'manifest_sha256': args.manifest_sha256,
                                  'manifest': value})
        return 0
    except (ValueError, KeyError, TypeError, OSError, subprocess.SubprocessError) as error:
        print('candidate rejected: ' + str(error), file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
