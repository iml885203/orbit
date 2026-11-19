#!/usr/bin/env python3
"""Record issue phase windows and export evidence to Perfetto; never authorize delivery."""

import argparse
import contextlib
import datetime
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import uuid

from dev_check import stop_process, validate_workspace, write_json
from flow_report import export_report


SCHEMA = 'orbit.flow.v1'
STAGES = ('requirements', 'development', 'testing', 'review', 'rework', 'ci', 'delivery', 'postmerge', 'waiting')
RESULTS = ('passed', 'failed', 'cancelled', 'unknown')


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def timestamp(value):
    if not isinstance(value, str):
        raise ValueError('timestamp must be an ISO string')
    parsed = datetime.datetime.fromisoformat(value.replace('Z', '+00:00'))
    if parsed.tzinfo is None:
        raise ValueError('timestamp must include timezone')
    return parsed.timestamp()


def source_id(value):
    if not isinstance(value, str) or not re.fullmatch(r'[0-9a-f]{40}|[0-9a-f]{64}', value):
        raise ValueError('source identity must be an exact commit or source fingerprint')
    return value


def label(value):
    if not isinstance(value, str) or not value or len(value) > 160 or any(ord(c) < 32 for c in value):
        raise ValueError('labels must be nonempty single-line strings of at most 160 characters')
    return value


def validate(journal):
    if not isinstance(journal, dict) or journal.get('schema') != SCHEMA or not re.fullmatch(r'[\w.-]+/[\w.-]+', journal.get('repository', '')):
        raise ValueError('invalid flow journal identity')
    if journal.get('mutation_authority') is not False or not isinstance(journal.get('events'), list):
        raise ValueError('invalid observational journal')
    if type(journal.get('issue')) is not int or journal['issue'] < 1:
        raise ValueError('invalid issue number')
    started = timestamp(journal['started_at'])
    finished = timestamp(journal['finished_at']) if journal.get('finished_at') else None
    if finished is not None and finished < started:
        raise ValueError('session clock moved backwards')
    spans, seen = {}, set()
    for event in journal['events']:
        if type(event) is not dict or event.get('type') not in ('begin', 'end', 'note'):
            raise ValueError('invalid event')
        at = timestamp(event['at'])
        if at < started or (finished is not None and at > finished):
            raise ValueError('event falls outside the session window')
        identifier = label(event['id'])
        identity = (event['type'], identifier)
        if identity in seen:
            raise ValueError('duplicate event identity')
        seen.add(identity)
        if event['type'] == 'note':
            label(event['label'])
            continue
        if event['type'] == 'begin':
            if identifier in spans or event['stage'] not in STAGES or type(event['round']) is not int or event['round'] < 1:
                raise ValueError('duplicate or invalid span')
            for key in ('label', 'actor', 'provenance'):
                label(event[key])
            source_id(event['source'])
            if event['stage'] in ('waiting', 'rework'):
                label(event.get('reason'))
            spans[identifier] = dict(event)
        else:
            span = spans.get(identifier)
            if span is None or 'end' in span or at < timestamp(span['at']) or event['result'] not in RESULTS:
                raise ValueError('conflicting finish or backwards clock')
            span.update(end=event['at'], result=event['result'])
    if finished is not None and any('end' not in span for span in spans.values()):
        raise ValueError('finished session contains unfinished phases')
    return spans


@contextlib.contextmanager
def journal_lock(directory):
    with (directory / 'validation.lock').open('a+b') as lock:
        if os.name == 'nt':
            import msvcrt
            lock.write(b'0')
            lock.flush()
            lock.seek(0)
            msvcrt.locking(lock.fileno(), msvcrt.LK_LOCK, 1)
        else:
            import fcntl
            fcntl.flock(lock, fcntl.LOCK_EX)
        try:
            yield
        finally:
            if os.name == 'nt':
                lock.seek(0)
                msvcrt.locking(lock.fileno(), msvcrt.LK_UNLCK, 1)
            else:
                fcntl.flock(lock, fcntl.LOCK_UN)


@contextlib.contextmanager
def journal_file(workspace):
    source = Path(__file__).resolve().parent.parent
    _, workspace = validate_workspace(source, workspace)
    if not workspace.is_dir() or any((p / '.git').exists() for p in (workspace, *workspace.parents)):
        raise ValueError('allocate non-Git task storage before recording flow events')
    directory = workspace / 'flow'
    if directory.is_symlink():
        raise ValueError('flow storage cannot be a symlink')
    directory.mkdir(exist_ok=True)
    for name in ('journal.json', 'journal.json.new', 'validation.lock', 'trace.json', 'trace.json.new',
                 'summary.json', 'summary.json.new', 'timeline.svg'):
        if (directory / name).is_symlink():
            raise ValueError('flow files cannot be symlinks')
    with journal_lock(directory):
        path = directory / 'journal.json'
        journal = json.loads(path.read_text()) if path.exists() else None
        if journal is not None:
            validate(journal)
        yield path, journal


def begin(journal, stage, actor, source, name, round_number=1, reason=None, identifier=None, at=None,
          provenance='agent-reported phase window'):
    if journal.get('finished_at'):
        raise ValueError('session is finished')
    event = dict(type='begin', id=identifier or uuid.uuid4().hex, at=at or now(), stage=stage,
                 actor=actor, source=source, label=name, round=round_number, provenance=provenance)
    if reason is not None:
        event['reason'] = reason
    journal['events'].append(event)
    validate(journal)
    return event['id']


def end(journal, identifier, result, at=None):
    journal['events'].append(dict(type='end', id=identifier, at=at or now(), result=result))
    validate(journal)


def import_span(journal, identifier, started, finished, stage, actor, source, name, result, provenance, reason=None):
    existing = validate(journal).get(identifier)
    if existing is None:
        begin(journal, stage, actor, source, name, identifier=identifier, at=started,
              provenance=provenance, reason=reason)
    elif any(existing[key] != value for key, value in [('at', started), ('source', source), ('label', name), ('stage', stage)]):
        raise ValueError('import identity conflicts with an earlier observation')
    if finished:
        if existing and existing.get('end'):
            if existing['end'] != finished or existing['result'] != result:
                raise ValueError('terminal observation conflicts with recorded evidence')
        else:
            end(journal, identifier, result, at=finished)


def note(journal, identifier, name, at=None):
    if not any(event['id'] == identifier for event in journal['events']):
        journal['events'].append(dict(type='note', id=identifier, at=at or now(), label=label(name)))
        validate(journal)


def import_check(journal, report):
    if report.get('schema') != 'orbit.dev-check.v1':
        raise ValueError('not a validation report')
    identity = source_id(report['inputs']['source'])
    identifier = 'check:' + label(report['run'])
    if report.get('reused'):
        invoked = report.get('invoked_at')
        note(journal, identifier + ':reuse:' + (invoked or 'missing-time'),
             'Validation reused; not another test execution' if invoked else 'Reuse invocation time unavailable; not reconstructed', invoked)
        return
    if not report.get('started_at'):
        note(journal, identifier + ':missing-time', 'Validation absolute timing unavailable; not reconstructed')
        return
    result = {'passed': 'passed', 'cancelled': 'cancelled', 'running': 'unknown'}.get(report['status'], 'failed')
    import_span(journal, identifier, report['started_at'], report.get('completed_at'), 'testing', 'validation',
                identity, 'Local ' + report['inputs']['mode'] + ' validation', result,
                'validation tool timestamps; fingerprint-bound execution')


def import_delivery(journal, report):
    if (report.get('schema') != 'orbit.delivery.v1' or report.get('repository') != journal['repository'] or
            not any(issue.get('number') == journal['issue'] for issue in report.get('issues', []))):
        raise ValueError('delivery checkpoint does not belong to this issue/repository')
    for key in ('pr_ci', 'main_ci'):
        run = report.get(key, {})
        if not run.get('run_id'):
            continue
        source = source_id(run['commit'])
        for job in run.get('jobs', []):
            identifier = 'github:{}:{}:{}'.format(run['run_id'], job['attempt'], job['id'])
            if job.get('conclusion') == 'skipped':
                note(journal, identifier + ':skipped', 'GitHub skipped: ' + job['name'])
                continue
            started, finished, queued = (job.get(k) for k in ('started_at', 'completed_at', 'created_at'))
            if not started:
                note(journal, identifier + ':pending-time', 'GitHub start time unavailable: ' + job['name'])
                continue
            if queued and timestamp(queued) < timestamp(started):
                import_span(journal, identifier + ':queue', queued, started, 'waiting', 'GitHub queue', source,
                            job['name'] + ' admission', 'passed', 'GitHub observer job timestamps', reason='job admission queue')
            result = 'passed' if job.get('conclusion') == 'success' else 'cancelled' if job.get('conclusion') == 'cancelled' else 'failed'
            import_span(journal, identifier, started, finished, 'ci', 'GitHub', source, job['name'], result,
                        'GitHub observer checkpoint; not independent delivery authority')
    pr = report.get('pr', {})
    if pr.get('head'):
        note(journal, 'pr:{}:{}'.format(pr['number'], pr['head']), 'PR #{} observed at source {}'.format(pr['number'], pr['head'][:12]))


def input_report(workspace, path):
    path = path.resolve()
    path.relative_to(workspace.resolve())
    return json.loads(path.read_text())


def change(workspace, action):
    with journal_file(workspace) as (path, journal):
        if journal is None:
            raise ValueError('initialize this issue session first')
        result = action(journal)
        validate(journal)
        write_json(path, journal)
        return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--workspace', type=Path, required=True)
    commands = parser.add_subparsers(dest='command', required=True)
    init = commands.add_parser('init', help='Start a new issue session; earlier discussion remains unobserved')
    init.add_argument('--repository', default='iml885203/orbit')
    init.add_argument('--issue', type=int, required=True)
    for command in ('begin', 'run'):
        child = commands.add_parser(command, help='Record an agent phase' if command == 'begin' else 'Measure a local command without recording its arguments')
        child.add_argument('--stage', choices=STAGES, required=True)
        child.add_argument('--actor', required=True)
        child.add_argument('--source', required=True)
        child.add_argument('--label', required=True)
        child.add_argument('--round', type=int, default=1)
        child.add_argument('--reason')
        if command == 'run':
            child.add_argument('argv', nargs=argparse.REMAINDER)
    finish = commands.add_parser('end')
    finish.add_argument('--span', required=True)
    finish.add_argument('--result', choices=RESULTS, required=True)
    for command in ('import-check', 'import-delivery'):
        commands.add_parser(command).add_argument('--report', type=Path, required=True)
    commands.add_parser('finish', help='Finish a session after all recorded phases terminate')
    commands.add_parser('export', help='Write trace.json, timeline.svg and summary.json in task storage')
    args = parser.parse_args()
    try:
        if args.command == 'init':
            with journal_file(args.workspace) as (path, existing):
                if existing is not None:
                    raise ValueError('session already exists; resume without reinitializing')
                journal = dict(schema=SCHEMA, repository=args.repository, issue=args.issue, started_at=now(),
                               events=[], prior_requirements='unobserved', mutation_authority=False)
                validate(journal)
                write_json(path, journal)
            result = {'issue': args.issue, 'started_at': journal['started_at']}
        elif args.command in ('begin', 'run'):
            identifier = change(args.workspace, lambda j: begin(j, args.stage, args.actor, args.source, args.label, args.round, args.reason))
            result = {'span': identifier}
            if args.command == 'run':
                argv = args.argv[1:] if args.argv[:1] == ['--'] else args.argv
                process, status = None, 'failed'
                cancelled_signal = signal.SIGINT
                def cancel_run(signum, _frame):
                    nonlocal cancelled_signal
                    cancelled_signal = signum
                    raise KeyboardInterrupt
                previous = signal.signal(signal.SIGTERM, cancel_run)
                try:
                    if not argv:
                        raise ValueError('run requires a command after --')
                    process = subprocess.Popen(argv, start_new_session=os.name != 'nt')
                    code = process.wait()
                    status = 'passed' if code == 0 else 'failed'
                except KeyboardInterrupt:
                    if process is not None:
                        stop_process(process)
                    status, code = 'cancelled', 128 + cancelled_signal
                finally:
                    signal.signal(signal.SIGTERM, previous)
                    change(args.workspace, lambda j: end(j, identifier, status))
                return code if code >= 0 else 128 - code
        elif args.command == 'end':
            change(args.workspace, lambda j: end(j, args.span, args.result))
            result = {'span': args.span, 'result': args.result}
        elif args.command.startswith('import-'):
            report = input_report(args.workspace, args.report)
            importer = import_check if args.command == 'import-check' else import_delivery
            change(args.workspace, lambda j: importer(j, report))
            result = {'imported': True}
        elif args.command == 'finish':
            def close(journal):
                if any('end' not in span for span in validate(journal).values()):
                    raise ValueError('unfinished phases remain; explicitly finish them before closing')
                if journal.get('finished_at'):
                    raise ValueError('session already finished')
                journal['finished_at'] = now()
            change(args.workspace, close)
            result = {'finished': True}
        else:
            with journal_file(args.workspace) as (path, journal):
                if journal is None:
                    raise ValueError('initialize a session first')
                result = export_report(journal, validate(journal), path.parent, now())
        print(json.dumps(result))
        return 0
    except (ValueError, KeyError, TypeError, OSError, subprocess.SubprocessError) as error:
        print('flow evidence incomplete: ' + str(error), file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
