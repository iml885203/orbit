#!/usr/bin/env python3
"""Observe live delivery evidence; never merge, publish or retry remote mutations."""
import argparse
import datetime
import hashlib
import io
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import zipfile

import candidate
import native_checks
from dev_check import validate_workspace, workspace_lock, write_json

REQUIRED_CI = ('go-lint', 'preflight', 'supply-chain', 'installed-user-journeys', 'dotnet-journey', 'native-gate')


class NotFound(ValueError):
    pass


def api(route, binary=False):
    command = ['gh', 'api', '--method', 'GET'] + ([] if binary else ['--include']) + [route]
    process = subprocess.run(command, capture_output=True)
    if binary:
        if process.returncode:
            raise ValueError('GitHub archive query unavailable: ' + route)
        return process.stdout
    header, separator, body = process.stdout.partition(b"\r\n\r\n")
    if not separator:
        header, separator, body = process.stdout.partition(b"\n\n")
    first = header.splitlines()[0] if header else b''
    status = re.fullmatch(rb'HTTP/[0-9.]+ ([0-9]{3})(?: .*)?', first)
    if not separator or not status:
        raise ValueError('GitHub query unavailable: ' + route)
    code = int(status[1])
    if code == 404:
        raise NotFound('GitHub resource not visible: ' + route)
    if process.returncode or not 200 <= code < 300:
        raise ValueError('GitHub query failed with HTTP ' + str(code) + ': ' + route)
    return json.loads(body)


def pages(route, field):
    result, page = [], 1
    while True:
        value = api(route + ('&' if '?' in route else '?') + 'per_page=100&page=' + str(page))
        result.extend(value[field])
        if len(result) >= value['total_count']:
            return result
        if not value[field]:
            raise ValueError('incomplete GitHub pagination')
        page += 1


def elapsed(job):
    start, end = job.get('started_at'), job.get('completed_at')
    if not start or not end:
        return None
    return round((datetime.datetime.fromisoformat(end.replace('Z', '+00:00')) -
                  datetime.datetime.fromisoformat(start.replace('Z', '+00:00'))).total_seconds(), 3)


def latest_jobs(repository, run):
    selected = {}
    for attempt in range(run['run_attempt'], 0, -1):
        rows = pages('repos/' + repository + '/actions/runs/' + str(run['id']) + '/attempts/' + str(attempt) + '/jobs', 'jobs')
        for row in rows:
            if row['name'] not in selected:
                selected[row['name']] = {key: row.get(key) for key in ('id', 'name', 'status', 'conclusion', 'html_url', 'created_at', 'started_at', 'completed_at')}
                selected[row['name']].update(attempt=attempt, seconds=elapsed(row))
    return list(selected.values())


def require_success(run, jobs, names):
    if run['status'] != 'completed' or run['conclusion'] != 'success':
        raise ValueError('latest workflow is not successfully completed')
    by_name = {job['name']: job for job in jobs}
    for name in names:
        if name not in by_name or by_name[name]['status'] != 'completed' or by_name[name]['conclusion'] != 'success':
            raise ValueError('missing or unsuccessful actual CI job: ' + name)


def ci_evidence(repository, commit, event, require=False):
    if not re.fullmatch(r'[0-9a-f]{40}', commit):
        raise ValueError('CI source must be an exact SHA')
    route = 'repos/' + repository + '/actions/workflows/ci.yml/runs?head_sha=' + commit + '&event=' + event
    if event == 'push':
        route += '&branch=main'
    runs = pages(route, 'workflow_runs')
    runs = [run for run in runs if run['head_sha'] == commit and run['event'] == event
            and run['path'].split('@')[0] == '.github/workflows/ci.yml'
            and (event != 'push' or run['head_branch'] == 'main')]
    if not runs:
        if require:
            raise ValueError('no canonical current-source main CI run')
        return {'state': 'missing', 'commit': commit}
    run = max(runs, key=lambda value: value['id'])
    jobs = latest_jobs(repository, run)
    proof = {'run_id': run['id'], 'attempt': run['run_attempt'], 'commit': commit, 'status': run['status'],
             'conclusion': run['conclusion'], 'jobs': jobs, 'url': run['html_url']}
    try:
        proof['native_coverage'] = native_coverage(repository, run, jobs)
        require_success(run, jobs, REQUIRED_CI)
        if proof['native_coverage']['state'] != 'passed':
            raise ValueError('native coverage evidence is incomplete')
        proof['gate_state'] = 'passed'
    except ValueError as error:
        proof['gate_state'] = 'pending' if run['status'] != 'completed' else 'blocked'
        proof['gate_detail'] = str(error)
        if require:
            raise
    return proof


def small_artifact_record(repository, metadata, filename):
    if metadata['expired'] or metadata['size_in_bytes'] > 2 * 1024 * 1024:
        raise ValueError('artifact record is expired or exceeds limit')
    archive = api('repos/' + repository + '/actions/artifacts/' + str(metadata['id']) + '/zip', binary=True)
    if len(archive) > 2 * 1024 * 1024 or metadata.get('digest') != 'sha256:' + hashlib.sha256(archive).hexdigest():
        raise ValueError('artifact record archive digest mismatch')
    with zipfile.ZipFile(io.BytesIO(archive)) as bundle:
        entries = bundle.infolist()
        if len(entries) != 1 or entries[0].filename != filename or entries[0].file_size > 1024 * 1024:
            raise ValueError('unexpected artifact record archive')
        return json.loads(bundle.read(entries[0]))


def native_coverage(repository, run, jobs):
    artifacts = pages('repos/' + repository + '/actions/runs/' + str(run['id']) + '/artifacts', 'artifacts')
    selected = [a for a in artifacts if a['name'] == 'orbit-native-plan-' + str(run['id'])]
    observed = {row['name'] for row in jobs if row['name'] in
                {'native / ' + p['name'] for p in native_checks.PLATFORMS}}
    if selected:
        if len(selected) != 1:
            raise ValueError('ambiguous native coverage plan')
        value = small_artifact_record(repository, selected[0], 'native-plan.json')
        if not isinstance(value, dict):
            raise ValueError('native coverage plan must be an object')
        profile = value.get('profile')
        if (value.get('schema') != 'orbit.native-impact.v1' or value.get('repository') != repository
                or value.get('run_id') != str(run['id']) or value.get('head_commit') != run['head_sha']
                or not isinstance(value.get('commit'), str) or not re.fullmatch(r'[0-9a-f]{40}', value['commit'])
                or (run.get('event') == 'push' and value['commit'] != run['head_sha'])
                or value.get('targets') != native_checks.targets(profile)
                or value.get('required') is not (profile != 'none')
                or not isinstance(value.get('reason'), str) or not value['reason']):
            raise ValueError('native coverage plan does not match observed run')
        proof = {'state': 'planned', 'profile': profile, 'targets': value['targets'], 'commit': value['commit'],
                 'reason': value['reason'], 'source': 'verified current-run native plan artifact'}
    elif run['status'] != 'completed':
        return {'state': 'pending', 'reason': 'coverage plan has not been produced'}
    elif observed == {'native / ' + p['name'] for p in native_checks.PLATFORMS}:
        profile = 'full'
        proof = {'state': 'legacy', 'profile': profile, 'targets': native_checks.targets(profile),
                 'reason': 'legacy full matrix; no profile plan recorded', 'source': 'actual native job inventory'}
    elif not observed and not any(j['name'] == 'candidate / produce' for j in jobs):
        profile = 'none'
        proof = {'state': 'legacy', 'profile': profile, 'targets': [],
                 'reason': 'legacy documentation no-op; native gate remains required', 'source': 'actual job inventory'}
    else:
        raise ValueError('native coverage plan is missing for a partial matrix')
    expected = {'native / ' + p['name'] for p in native_checks.PLATFORMS if p['target'] in proof['targets']}
    proof['observed_jobs'] = sorted(observed)
    if run['status'] == 'completed' and run['conclusion'] == 'success':
        if observed != expected:
            raise ValueError('actual native jobs do not match the planned target set')
        names = ['native-impact'] + sorted(expected)
        if profile != 'none':
            names.append('candidate / produce')
        require_success(run, jobs, names)
        proof['state'] = 'passed'
    return proof


def tiny_identity(repository, run):
    artifacts = pages('repos/' + repository + '/actions/runs/' + str(run['id']) + '/artifacts', 'artifacts')
    matches = [item for item in artifacts if item['name'].startswith('orbit-candidate-identity-' + str(run['id']) + '-')]
    if not matches:
        return {'state': 'not-produced'}
    metadata = max(matches, key=lambda item: item['id'])
    if metadata['expired']:
        return {'state': 'expired', 'identity_artifact_id': metadata['id']}
    if metadata['size_in_bytes'] > 2 * 1024 * 1024:
        raise ValueError('candidate identity artifact exceeds limit')
    value = small_artifact_record(repository, metadata, 'candidate-identity.json')
    if value.get('schema') != 'orbit.candidate-identity.v1':
        raise ValueError('unknown candidate identity schema')
    manifest = value['manifest']
    if (manifest['repository'] != repository or manifest['run_id'] != str(run['id'])
            or manifest['head_commit'] != run['head_sha'] or set(manifest['assets']) != candidate.inventory_names()):
        raise ValueError('candidate identity does not belong to observed run')
    canonical = (json.dumps(manifest, sort_keys=True, indent=2) + '\n').encode()
    if hashlib.sha256(canonical).hexdigest() != value['manifest_sha256']:
        raise ValueError('candidate manifest identity digest mismatch')
    candidate.verify_remote(manifest, value['artifact_id'], value['archive_digest'])
    authority = (run['path'].split('@')[0] == '.github/workflows/release.yml' and run['event'] == 'workflow_dispatch'
                 and run['head_branch'] == 'main' and manifest['commit'] == run['head_sha']
                 and manifest['workflow_ref'] == repository + '/.github/workflows/release.yml@refs/heads/main')
    return {'state': 'observed', 'source': 'GitHub identity artifact and artifact metadata',
            'release_producer': authority, 'identity_artifact_id': metadata['id'],
            'artifact_id': value['artifact_id'], 'archive_digest': value['archive_digest'],
            'manifest_sha256': value['manifest_sha256'], 'manifest': manifest,
            'byte_verification': 'performed by native/promotion jobs; observer reads identity only'}


def tag_commit(repository, tag):
    if not re.fullmatch(r'v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?', tag):
        raise ValueError('invalid release tag')
    value = api('repos/' + repository + '/git/ref/tags/' + tag)['object']
    for _ in range(10):
        if value['type'] == 'commit' and re.fullmatch(r'[0-9a-f]{40}', value['sha']):
            return value['sha']
        if value['type'] != 'tag':
            break
        value = api('repos/' + repository + '/git/tags/' + value['sha'])['object']
    raise ValueError('release tag does not resolve to a commit')


def release_evidence(repository, tag, producer):
    try:
        release = api('repos/' + repository + '/releases/tags/' + tag)
    except NotFound:
        return {'tag': tag, 'state': 'not-published', 'immutable': False, 'candidate_binding': 'unknown'}
    assets = {item['name']: {'digest': item.get('digest'), 'size': item['size']} for item in release['assets']}
    commit = tag_commit(repository, tag)
    evidence = {'state': 'published', 'tag': release['tag_name'], 'immutable': release.get('immutable', False), 'draft': release['draft'],
                'prerelease': release['prerelease'], 'commit': commit, 'url': release['html_url'], 'assets': assets,
                'candidate_binding': 'unknown'}
    if producer.get('state') == 'observed':
        manifest = producer['manifest']
        matches = (producer['release_producer'] and manifest['version'] == tag and manifest['commit'] == commit
                   and set(assets) == set(manifest['assets'])
                   and all(assets[name]['digest'] == 'sha256:' + digest for name, digest in manifest['assets'].items()))
        evidence['candidate_binding'] = 'matched' if matches else 'mismatch'
    return evidence


def reconcile(previous, head, reported):
    old = previous.get('pr', {}).get('head')
    stale = bool(old and old != head)
    carried = [] if stale else previous.get('reported_reviews', [])
    values = [item for item in carried + reported if item['source_sha'] == head]
    unique = {(item['reviewer'], item['source_sha']): item for item in values}
    return stale, list(unique.values())


def observe(args, previous):
    result = {'schema': 'orbit.delivery.v1', 'repository': args.repository,
              'observed_at': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'status': 'observed',
              'mutation_authority': False, 'reported_reviews': []}
    metadata = api('repos/' + args.repository)
    if metadata['full_name'].lower() != args.repository.lower():
        raise ValueError('repository identity changed')
    if args.pr:
        raw = json.loads(subprocess.check_output(['gh', 'pr', 'view', str(args.pr), '--repo', args.repository, '--json',
            'number,url,state,headRefOid,mergeCommit,reviewDecision,closingIssuesReferences'], stderr=subprocess.PIPE))
        result['pr'] = {'number': raw['number'], 'url': raw['url'], 'state': raw['state'], 'head': raw['headRefOid'],
                        'merge_commit': (raw.get('mergeCommit') or {}).get('oid'),
                        'github_review_decision': raw['reviewDecision'], 'review_source': 'GitHub API'}
        result['checkpoint_stale'], result['reported_reviews'] = reconcile(previous, raw['headRefOid'], args.reported_review)
        result['issues'] = []
        for linked in raw['closingIssuesReferences']:
            issue = api('repos/' + args.repository + '/issues/' + str(linked['number']))
            result['issues'].append({key: issue[key] for key in ('number', 'state', 'html_url')})
        result['pr_ci'] = ci_evidence(args.repository, raw['headRefOid'], 'pull_request')
        if raw['state'] == 'MERGED' and result['pr']['merge_commit']:
            result['main_ci'] = ci_evidence(args.repository, result['pr']['merge_commit'], 'push')
        if result['pr_ci'].get('run_id'):
            pr_run = api('repos/' + args.repository + '/actions/runs/' + str(result['pr_ci']['run_id']))
            result['pr_candidate'] = tiny_identity(args.repository, pr_run)
    if args.release_run:
        run = api('repos/' + args.repository + '/actions/runs/' + str(args.release_run))
        if run['path'].split('@')[0] != '.github/workflows/release.yml' or run['event'] != 'workflow_dispatch':
            raise ValueError('selected run is not an explicit release workflow')
        result['release_run'] = {key: run[key] for key in ('id', 'head_sha', 'head_branch', 'event', 'path', 'run_attempt', 'status', 'conclusion', 'html_url')}
        result['release_run']['current_main'] = api('repos/' + args.repository + '/git/ref/heads/main')['object']['sha'] == run['head_sha']
        result['release_jobs'] = latest_jobs(args.repository, run)
        result['candidate'] = tiny_identity(args.repository, run)
        result['main_ci'] = ci_evidence(args.repository, run['head_sha'], 'push')
        result['packages'] = {channel: [job for job in result['release_jobs'] if job['name'].startswith('update-' + channel + ' /')]
                              for channel in ('homebrew', 'scoop')}
    if args.tag:
        result['release'] = release_evidence(args.repository, args.tag, result.get('candidate', {}))
        result['resume'] = ('Publication already exists: verify it, then resume only unfinished verification/package steps; do not publish again.'
                            if result['release']['state'] == 'published' else 'No published release is visible; continue the existing candidate and release approval workflow.')
    else:
        result['resume'] = 'Observe the current failed/pending stage before any separately authorized mutation.'
    return result


def probe_publication(repository, tag, directory, manifest_sha):
    manifest = directory / 'manifest.json'
    if candidate.sha(manifest) != manifest_sha:
        raise ValueError('publication probe manifest digest mismatch')
    value = json.loads(manifest.read_text())
    if (os.environ.get('GITHUB_EVENT_NAME') != 'workflow_dispatch' or os.environ.get('GITHUB_REF') != 'refs/heads/main'
            or value['repository'] != repository or value['version'] != tag
            or value['commit'] != os.environ.get('GITHUB_SHA') or value['run_id'] != os.environ.get('GITHUB_RUN_ID')):
        raise ValueError('publication probe is not the approved main candidate')
    release = release_evidence(repository, tag, {'state': 'observed', 'release_producer': True, 'manifest': value})
    if release['state'] == 'not-published':
        return False
    if not release['immutable'] or release['draft'] or release['candidate_binding'] != 'matched':
        raise ValueError('existing publication does not match immutable candidate evidence')
    return True


def review_argument(value):
    parts = value.split(':')
    if len(parts) != 3 or not re.fullmatch(r'[A-Za-z0-9_./-]{1,100}', parts[0]) or not re.fullmatch(r'[0-9a-f]{40}', parts[1]) or parts[2] not in ('passed', 'blocked'):
        raise argparse.ArgumentTypeError('review must be reviewer:exact-sha:passed|blocked')
    return {'source': 'agent-reported, not GitHub-verified', 'reviewer': parts[0], 'source_sha': parts[1], 'conclusion': parts[2]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--repository', default=os.environ.get('GITHUB_REPOSITORY', 'iml885203/orbit'))
    parser.add_argument('--require-main-ci', action='store_true')
    parser.add_argument('--probe-publication', action='store_true')
    parser.add_argument('--candidate-directory', type=Path)
    parser.add_argument('--manifest-sha256')
    parser.add_argument('--github-output', type=Path)
    parser.add_argument('--commit')
    parser.add_argument('--pr', type=int)
    parser.add_argument('--release-run', type=int)
    parser.add_argument('--tag')
    parser.add_argument('--workspace', type=Path)
    parser.add_argument('--checkpoint', type=Path)
    parser.add_argument('--reported-review', type=review_argument, action='append', default=[])
    parser.add_argument('--json', action='store_true')
    args = parser.parse_args()
    try:
        if not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', args.repository):
            raise ValueError('invalid repository')
        if args.probe_publication:
            if not all((args.tag, args.candidate_directory, args.manifest_sha256, args.github_output)):
                parser.error('publication probe requires tag, candidate directory, manifest SHA and GitHub output')
            accepted = probe_publication(args.repository, args.tag, args.candidate_directory, args.manifest_sha256)
            with args.github_output.open('a') as output:
                output.write('accepted-existing=' + str(accepted).lower() + '\n')
            print(json.dumps({'accepted_existing': accepted, 'mutation_authority': False}))
            return 0
        if args.require_main_ci:
            if not args.commit:
                parser.error('--commit is required for main CI verification')
            print(json.dumps(ci_evidence(args.repository, args.commit, 'push', require=True)))
            return 0
        if not (args.pr or args.release_run or args.tag) or args.workspace is None:
            parser.error('observe a PR/release run/tag and provide --workspace')
        if not args.workspace.is_dir():
            raise ValueError('allocate the task workspace first')
        _, workspace = validate_workspace(Path(__file__).resolve().parent.parent, args.workspace)
        checkpoint = (args.checkpoint or workspace / 'orbit-delivery.json').resolve()
        checkpoint.relative_to(workspace)
        with workspace_lock(workspace):
            previous = json.loads(checkpoint.read_text()) if checkpoint.exists() else {}
            if previous and (previous.get('schema') != 'orbit.delivery.v1' or previous.get('repository') != args.repository):
                raise ValueError('checkpoint belongs to another schema or repository')
            report = observe(args, previous)
            write_json(checkpoint, report)
        if args.json:
            print(json.dumps(report))
        else:
            if 'pr' in report:
                print('PR #{}: {} @ {}'.format(report['pr']['number'], report['pr']['state'], report['pr']['head'][:12]))
            for key in ('pr_ci', 'main_ci', 'release_run'):
                if key in report:
                    print('{}: {}'.format(key, report[key].get('gate_state') or report[key].get('conclusion') or report[key].get('status') or report[key].get('state')))
            for key in ('pr_ci', 'main_ci'):
                for job in report.get(key, {}).get('jobs', []):
                    if job['status'] != 'completed' or job['conclusion'] not in ('success', 'skipped'):
                        print('  {}: {} (attempt {})'.format(job['name'], job['conclusion'] or job['status'], job['attempt']))
            if 'candidate' in report:
                print('Candidate: ' + report['candidate']['state'])
            if 'release' in report:
                print('Release {}: immutable={}, candidate={}'.format(report['release']['tag'], report['release']['immutable'], report['release']['candidate_binding']))
            print(report['resume'])
            print('Checkpoint: ' + str(checkpoint))
        return 0
    except (ValueError, KeyError, TypeError, OSError, subprocess.SubprocessError, zipfile.BadZipFile) as error:
        print('delivery evidence is incomplete; no mutation is authorized: ' + str(error), file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
