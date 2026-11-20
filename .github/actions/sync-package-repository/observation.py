"""Bounded package-updater observation and explicit local recovery checkpoint."""

import datetime
import json
import os
from pathlib import Path
import re
import tempfile

SCHEMA = 'orbit.package-observation.v1'
LIMIT = 16384
STATES = {'dispatch', 'selected', 'accepted', 'observing', 'success', 'failure', 'identity_mismatch'}
REASONS = {'dispatch_pending', 'dispatch_unknown', 'selected_unverified', 'accepted', 'observing',
           'updater_success', 'updater_failed', 'identity_mismatch', 'observation_exhausted',
           'request_denied', 'invalid_response', 'discovery_exhausted', 'budget_exhausted'}
STATUS = {'queued', 'in_progress', 'waiting', 'pending', 'requested', 'completed'}
CONCLUSION = {'success', 'failure', 'cancelled', 'timed_out', 'skipped', 'neutral', 'action_required', 'stale', 'startup_failure'}
RULES = {'id': 'selected_run_id', 'repository': 'requested_repository', 'path': 'update_workflow',
         'head_branch': 'main_branch', 'event': 'dispatch_event', 'display_title': 'dispatch_title'}
MARKERS = {'different', 'missing', 'invalid_type', 'oversize'}
FIELDS = {'schema', 'repository', 'version', 'producer', 'state', 'reason_code', 'dispatch_acknowledged',
          'identity_verified', 'selected_run', 'observation', 'mismatches', 'started_at', 'observed_at', 'recovery'}


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def positive(value):
    return isinstance(value, str) and re.fullmatch(r'[1-9][0-9]{0,19}', value) is not None


def run_url(repository, run_id):
    return f'https://github.com/{repository}/actions/runs/{run_id}'


def mismatch(field, value):
    marker = 'different'
    if value is None:
        marker = 'missing'
    elif (field == 'id' and type(value) is not int) or (field != 'id' and not isinstance(value, str)):
        marker = 'invalid_type'
    elif isinstance(value, str) and len(value) > 256:
        marker = 'oversize'
    return {'field': field, 'expected': RULES[field], 'observed': marker}


class CheckpointError(Exception):
    pass


class Observation:
    def __init__(self, path, repository, version):
        self.path = Path(path) if path else None
        self.value = None
        self.lock = None
        if self.path is None:
            return
        if not re.fullmatch(r'[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}', repository):
            raise CheckpointError('Invalid checkpoint repository')
        if not re.fullmatch(r'v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?', version) or len(version) > 128:
            raise CheckpointError('Invalid checkpoint release version')
        if self.path.is_symlink() or self.path.exists() and not self.path.is_file():
            raise CheckpointError('Checkpoint must be a regular file, not a symlink')
        if self.path.with_name(self.path.name + '.lock').is_symlink():
            raise CheckpointError('Checkpoint lock cannot be a symlink')
        self.lock = self.path.with_name(self.path.name + '.lock').open('a+b')
        try:
            if os.name == 'nt':
                import msvcrt
                self.lock.write(b'0')
                self.lock.flush()
                self.lock.seek(0)
                msvcrt.locking(self.lock.fileno(), msvcrt.LK_NBLCK, 1)
            else:
                import fcntl
                fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            if self.path.exists():
                with self.path.open('rb') as source:
                    raw = source.read(LIMIT + 1)
                if len(raw) > LIMIT:
                    raise CheckpointError('Checkpoint exceeds 16 KiB')
                self.value = json.loads(raw)
                self.validate(repository, version)
                self.value = {field: self.value[field] for field in FIELDS}
        except Exception:
            self.close()
            raise

    def close(self):
        if self.lock is not None:
            self.lock.close()
            self.lock = None

    def validate(self, repository, version):
        value = self.value
        if type(value) is not dict or not FIELDS <= value.keys() or value.get('schema') != SCHEMA:
            raise CheckpointError('Invalid checkpoint schema')
        if value['repository'] != repository or value['version'] != version:
            raise CheckpointError('Checkpoint repository/version conflict')
        if value['state'] not in STATES or value['reason_code'] not in REASONS:
            raise CheckpointError('Invalid checkpoint state')
        if type(value['dispatch_acknowledged']) is not bool or type(value['identity_verified']) is not bool:
            raise CheckpointError('Invalid checkpoint verification flags')
        producer = value['producer']
        if producer is not None and (type(producer) is not dict or set(producer) != {'run_id', 'attempt'} or
                                     not all(positive(producer[k]) for k in ('run_id', 'attempt'))):
            raise CheckpointError('Invalid checkpoint producer')
        selected = value['selected_run']
        if selected is not None and (type(selected) is not dict or set(selected) != {'id', 'url'} or
                                     not positive(selected['id']) or selected['url'] != run_url(repository, selected['id'])):
            raise CheckpointError('Invalid checkpoint selected run')
        if value['state'] in {'selected', 'accepted', 'observing', 'success', 'identity_mismatch'} and selected is None:
            raise CheckpointError('Checkpoint state requires a selected run')
        if value['identity_verified'] != (value['state'] in {'accepted', 'observing', 'success'}):
            raise CheckpointError('Inconsistent checkpoint verification state')
        observation = value['observation']
        if observation is not None and (type(observation) is not dict or set(observation) != {'status', 'conclusion'} or
                                        observation['status'] not in STATUS or
                                        observation['conclusion'] not in CONCLUSION | {None}):
            raise CheckpointError('Invalid checkpoint observation')
        rows = value['mismatches']
        if type(rows) is not list or len(rows) > len(RULES):
            raise CheckpointError('Invalid checkpoint mismatches')
        seen = set()
        for row in rows:
            if (type(row) is not dict or set(row) != {'field', 'expected', 'observed'} or
                    not isinstance(row['field'], str) or row['field'] not in RULES or row['field'] in seen or
                    row['expected'] != RULES[row['field']] or row['observed'] not in MARKERS):
                raise CheckpointError('Invalid checkpoint mismatch')
            seen.add(row['field'])
        recovery = value['recovery']
        if (type(recovery) is not dict or set(recovery) != {'action', 'run_id'} or
                recovery['action'] not in {'observe_existing_run', 'inspect_dispatch', 'inspect_failure', 'none'} or
                recovery['run_id'] != (selected['id'] if selected else None)):
            raise CheckpointError('Invalid checkpoint recovery')
        for field in ('started_at', 'observed_at'):
            if not isinstance(value[field], str) or len(value[field]) > 40:
                raise CheckpointError('Invalid checkpoint timestamp')
            try:
                parsed = datetime.datetime.fromisoformat(value[field])
                if parsed.tzinfo is None:
                    raise ValueError()
            except ValueError as error:
                raise CheckpointError('Invalid checkpoint timestamp') from error

    def prepare(self, existing, producer):
        if self.path is None:
            return existing, producer
        if self.value is not None:
            selected = self.value['selected_run']
            if selected:
                if existing and existing != selected['id']:
                    raise CheckpointError('Existing run ID conflicts with checkpoint')
                return selected['id'], self.value['producer']
            if not existing:
                raise CheckpointError('Dispatch state is uncertain; inspect accepted work and supply EXISTING_RUN_ID; do not redispatch')
            return existing, self.value['producer']
        self.value = {'schema': SCHEMA, 'repository': os.environ['REPOSITORY'], 'version': os.environ['VERSION'],
                      'producer': producer, 'state': 'dispatch', 'reason_code': 'dispatch_pending',
                      'dispatch_acknowledged': False, 'identity_verified': False, 'selected_run': None,
                      'observation': None, 'mismatches': [], 'started_at': now(), 'observed_at': now(),
                      'recovery': {'action': 'inspect_dispatch', 'run_id': None}}
        self.save()
        return existing, producer

    def transition(self, state, reason, run_id=None, acknowledged=None, observation=None, mismatches=None):
        if self.path is None:
            return
        value = self.value
        value.update(state=state, reason_code=reason, identity_verified=state in {'accepted', 'observing', 'success'},
                     observed_at=now(), observation=observation, mismatches=mismatches or [])
        if run_id is not None:
            value['selected_run'] = {'id': run_id, 'url': run_url(value['repository'], run_id)}
        if acknowledged is not None:
            value['dispatch_acknowledged'] = acknowledged
        selected = value['selected_run']
        if state == 'success':
            action = 'none'
        elif state == 'identity_mismatch' or reason == 'updater_failed':
            action = 'inspect_failure'
        else:
            action = 'observe_existing_run' if selected else 'inspect_dispatch'
        value['recovery'] = {'action': action, 'run_id': selected['id'] if selected else None}
        self.save()

    def save(self):
        self.validate(self.value['repository'], self.value['version'])
        raw = (json.dumps(self.value, sort_keys=True, ensure_ascii=True) + '\n').encode()
        if len(raw) > LIMIT:
            raise CheckpointError('Serialized checkpoint exceeds 16 KiB')
        temporary = None
        try:
            with tempfile.NamedTemporaryFile(dir=self.path.parent, prefix=self.path.name + '.', delete=False) as output:
                temporary = Path(output.name)
                output.write(raw)
                output.flush()
                os.fsync(output.fileno())
            os.replace(temporary, self.path)
            if os.name != 'nt':
                directory = os.open(self.path.parent, os.O_RDONLY)
                try:
                    os.fsync(directory)
                finally:
                    os.close(directory)
        finally:
            if temporary is not None:
                temporary.unlink(missing_ok=True)
