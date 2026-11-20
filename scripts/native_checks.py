#!/usr/bin/env python3
"""Plan native consumers and verify the selected candidate receipts."""
import argparse
import hashlib
import json
from pathlib import Path
import sys

import candidate

PLATFORMS = (
    dict(name='macOS arm64', runner='macos-15', target='darwin/arm64', windows=False),
    dict(name='macOS amd64', runner='macos-15-intel', target='darwin/amd64', windows=False),
    dict(name='Linux amd64', runner='ubuntu-24.04', target='linux/amd64', windows=False),
    dict(name='Linux arm64', runner='ubuntu-24.04-arm', target='linux/arm64', windows=False),
    dict(name='Windows amd64 Beta', runner='windows-2025', target='windows/amd64', windows=True),
    dict(name='Windows arm64 Beta', runner='windows-11-arm', target='windows/arm64', windows=True),
)


def targets(profile):
    if profile == 'full':
        return list(candidate.TARGETS)
    if profile == 'linux':
        return [target for target in candidate.TARGETS if target.startswith('linux/')]
    if profile == 'none':
        return []
    raise ValueError('unknown native validation profile')


def matrix(profile):
    if profile == 'none':
        raise ValueError('a native workflow cannot execute a no-op profile')
    selected = set(targets(profile))
    return {'include': [dict(row) for row in PLATFORMS if row['target'] in selected]}


def verify(identity, expected, manifest_sha, artifact_id, archive_digest, receipts, profile):
    if profile not in ('linux', 'full'):
        raise ValueError('receipt verification requires a runnable native profile')
    if not isinstance(identity, dict) or not isinstance(identity.get('manifest'), dict):
        raise ValueError('native gate identity must contain a manifest object')
    value = identity['manifest']
    candidate.validate_identity(value, expected)
    canonical = (json.dumps(value, sort_keys=True, indent=2) + '\n').encode()
    if (identity.get('schema') != 'orbit.candidate-identity.v1'
            or identity.get('artifact_id') != artifact_id
            or identity.get('archive_digest') != archive_digest
            or identity.get('manifest_sha256') != manifest_sha
            or hashlib.sha256(canonical).hexdigest() != manifest_sha
            or set(value['assets']) != candidate.inventory_names()):
        raise ValueError('native gate candidate identity mismatch')
    candidate.verify_remote(expected, artifact_id, archive_digest)
    values = candidate.verify_selected_receipts(receipts, value, manifest_sha, artifact_id,
                                               expected['run_id'], targets(profile))
    return {'schema': 'orbit.native-validation.v1', 'profile': profile, 'targets': targets(profile),
            'artifact_id': artifact_id, 'manifest_sha256': manifest_sha, 'receipts': len(values)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    plan = commands.add_parser('matrix')
    plan.add_argument('--profile', choices=('linux', 'full'), default='full')
    plan.add_argument('--github-output')
    check = commands.add_parser('verify')
    check.add_argument('--profile', choices=('linux', 'full'), required=True)
    check.add_argument('--identity', type=Path, required=True)
    check.add_argument('--receipts', type=Path, required=True)
    for option in ('version', 'producer-attempt', 'workflow-ref', 'manifest-sha256', 'artifact-id', 'archive-digest'):
        check.add_argument('--' + option, required=True)
    args = parser.parse_args()
    try:
        if args.command == 'matrix':
            result = matrix(args.profile)
            if args.github_output:
                with open(args.github_output, 'a') as output:
                    output.write('matrix=' + json.dumps(result, separators=(',', ':')) + '\n')
        else:
            expected = candidate.identity(args.version, args.producer_attempt, args.workflow_ref)
            result = verify(json.loads(args.identity.read_text()), expected, args.manifest_sha256,
                            args.artifact_id, args.archive_digest, args.receipts, args.profile)
        print(json.dumps(result))
        return 0
    except (ValueError, KeyError, TypeError, OSError) as error:
        print('native validation rejected: ' + str(error), file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
