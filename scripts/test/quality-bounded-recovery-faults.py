"""Private fault controls for the disposable Quality bounded-recovery case."""

from __future__ import annotations

from contextlib import contextmanager
from importlib import import_module
import hashlib
import json
import os
from pathlib import Path
import re
import select
import signal
import subprocess
import sys
import time
import uuid

HOSTED = import_module('scripts.test.orchestrator-execution-faults')
CHILD = import_module('scripts.test.continuous-execution-faults')
SOURCE = import_module('business.scripts.online-quality-postgres-fixture')
ROOT = Path(__file__).resolve().parents[2]
UNITS = ('quality-worker-1', 'quality-worker-2')
RECOVERY = 'quality-paused-workers.json'


def boundary():
    directory = HOSTED.deployment_secret()
    if (os.environ.get('ADDP_ONLINE_QUALITY_CASE') != 'bounded-recovery'
            or os.environ.get('ADDP_DEV_QUALITY_WORKER_REPLICAS') != '2'
            or os.environ.get('QUALITY_WORKER_CONCURRENCY') != '1'
            or os.environ.get('QUALITY_WORKER_LEASE_DURATION') != '30s'
            or not hasattr(os, 'pidfd_open') or not hasattr(signal, 'pidfd_send_signal')):
        raise ValueError('Quality faults require the admitted two-Worker Hosted case')
    return directory


def process_record(unit):
    directory = boundary()
    if unit not in UNITS:
        raise ValueError('invalid Quality Worker unit')
    path = ROOT / '.dev-pids' / (unit + '.pid')
    if path.is_symlink() or not re.fullmatch(r'[1-9][0-9]*', path.read_text().strip()):
        raise ValueError('invalid Quality Worker PID record')
    pid = int(path.read_text().strip())
    return validate_process(Path('/proc') / str(pid), ROOT, directory, unit, pid)


def validate_process(proc, root, directory, unit, pid):
    if unit not in UNITS:
        raise ValueError('invalid Quality Worker unit')
    binary = root / '.dev-bins/addp-quality-worker'
    env = dict(x.split('=', 1) for x in proc.joinpath('environ').read_bytes().decode().split('\0') if '=' in x)
    stat = proc.joinpath('stat').read_text().rsplit(')', 1)[1].split()
    if (pid <= 1 or proc.stat().st_uid != os.getuid() or stat[0] in {'Z', 'X'}
            or proc.joinpath('cwd').resolve(strict=True) != root
            or proc.joinpath('exe').resolve(strict=True) != binary.resolve(strict=True)
            or env.get('ADDP_ONLINE_SECRET_DIR') != str(directory)
            or env.get('ADDP_ONLINE_HOSTED') != '1' or env.get('POSTGRES_DB') != 'addp_online'):
        raise ValueError('Quality Worker is not owned by this deployment')
    instance = env.get('ADDP_PROCESS_INSTANCE_ID', '')
    if str(uuid.UUID(instance)) != instance:
        raise ValueError('Quality Worker process instance is invalid')
    return {'unit': unit, 'pid': pid, 'start_ticks': stat[19], 'instance_id': instance,
            'binary_sha256': hashlib.sha256(binary.read_bytes()).hexdigest()}



def resume_paused():
    directory = boundary()
    path = directory / RECOVERY
    if not path.exists() and not path.is_symlink():
        return
    if path.is_symlink() or path.stat().st_mode & 0o777 != 0o600 or path.stat().st_size > 4096:
        raise ValueError('invalid private Quality recovery record')
    value = json.loads(path.read_text())
    if not isinstance(value, dict) or set(value) != {'root', 'workers'} or value['root'] != str(ROOT) or not isinstance(value['workers'], list) or len(value['workers']) > 2:
        raise ValueError('invalid Quality recovery scope')
    seen, failed = set(), False
    for row in value['workers']:
        try:
            if (not isinstance(row, dict) or set(row) != {'unit', 'pid', 'start_ticks', 'binary_sha256', 'instance_id'}
                    or row['unit'] not in UNITS or row['unit'] in seen or type(row['pid']) is not int or row['pid'] <= 1):
                raise ValueError('invalid paused Quality identity')
            seen.add(row['unit'])
            if not Path('/proc', str(row['pid'])).exists():
                continue
            fd = os.pidfd_open(row['pid'])
            try:
                if process_record(row['unit']) != row:
                    raise ValueError('Quality recovery identity changed')
                signal.pidfd_send_signal(fd, signal.SIGCONT)
            finally:
                os.close(fd)
        except (OSError, ValueError, KeyError, IndexError):
            failed = True
    if failed:
        raise ValueError('Quality paused Worker recovery incomplete')
    path.unlink()


class HostedFaults:
    def __init__(self, tenant_id, plan_id):
        self.directory = boundary()
        if type(tenant_id) is not int or tenant_id <= 1 or type(plan_id) is not int or plan_id <= 0:
            raise ValueError('invalid Quality probe scope')
        self.tenant_id, self.plan_id = tenant_id, plan_id
        self.executions, self.paused, self.pins = set(), {}, {}
        if (self.directory / RECOVERY).exists() or (self.directory / RECOVERY).is_symlink():
            raise ValueError('previous Quality faults require recovery')

    def close(self):
        try:
            resume_paused()
        finally:
            for _, fd in self.pins.values():
                os.close(fd)
            self.pins.clear()

    def pin(self, unit):
        row = process_record(unit)
        fd = os.pidfd_open(row['pid'])
        try:
            if process_record(unit) != row:
                raise ValueError('Quality Worker changed while pinning')
        except BaseException:
            os.close(fd)
            raise
        if unit in self.pins:
            os.close(self.pins[unit][1])
        self.pins[unit] = row, fd
        return row

    def pause(self, unit, paused):
        if type(paused) is not bool:
            raise ValueError('invalid Quality pause state')
        if unit not in self.pins:
            self.pin(unit)
        row, fd = self.pins[unit]
        if process_record(unit) != row:
            raise ValueError('Quality Worker identity changed before signal')
        if paused:
            if unit in self.paused:
                raise ValueError('Quality Worker already paused')
            self.paused[unit] = row
        elif unit not in self.paused:
            raise ValueError('Quality Worker was not paused')
        path = self.directory / RECOVERY
        temporary = path.with_suffix('.tmp')
        with open(temporary, 'x', opener=lambda name, flags: os.open(name, flags, 0o600)) as out:
            json.dump({'root': str(ROOT), 'workers': list(self.paused.values())}, out)
            out.flush()
            os.fsync(out.fileno())
        temporary.replace(path)
        signal.pidfd_send_signal(fd, signal.SIGSTOP if paused else signal.SIGCONT)
        CHILD.wait_owner_state(row['pid'], paused)
        if not paused:
            del self.paused[unit]
            # Retaining the resumed identity on disk makes outer cleanup idempotent.
        return row

    def crash_and_replace(self, unit):
        if unit != UNITS[0] or unit in self.paused:
            raise ValueError('crash is restricted to the active first Quality Worker')
        if unit not in self.pins:
            self.pin(unit)
        row, fd = self.pins[unit]
        if process_record(unit) != row:
            raise ValueError('Quality crash identity changed')
        signal.pidfd_send_signal(fd, signal.SIGKILL)
        if not select.select([fd], [], [], 10)[0]:
            raise ValueError('crashed Quality Worker did not exit')
        peer = process_record(UNITS[1])
        peer_units = ('system', 'gateway', 'quality', 'monitor', 'meta', 'model', 'orchestrator', 'develop', 'standard')
        peers = {name: (ROOT / '.dev-pids' / (name + '.pid')).read_text()
                 for name in peer_units if (ROOT / '.dev-pids' / (name + '.pid')).exists()}
        log = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']) / 'quality-worker-replacement.log'
        with log.open('a') as output:
            subprocess.run(['bash', 'scripts/dev/start.sh', '-quality'], cwd=ROOT,
                           env={**CHILD.runtime_environment(os.environ), 'SKIP_MODTIDY': '1'},
                           stdout=output, stderr=subprocess.STDOUT, timeout=180, check=True)
        new = self.pin(unit)
        if (new['pid'] == row['pid'] or new['binary_sha256'] != row['binary_sha256']
                or process_record(UNITS[1]) != peer
                or any((ROOT / '.dev-pids' / (name + '.pid')).read_text() != value for name, value in peers.items())):
            raise ValueError('Quality replacement changed build or peer identity')
        return {'old_pid': row['pid'], 'replacement_pid': new['pid'],
                'binary_sha256': new['binary_sha256'], 'peer_preserved': True}

    def postgres(self, source=False):
        boundary()
        if source:
            SOURCE.require_owner()
            return ['docker', 'exec', '-i', SOURCE.CONTAINER, 'psql', '-X', '-qAt',
                    '-v', 'ON_ERROR_STOP=1', '-U', 'postgres', '-d', 'quality_fixture']
        result = subprocess.run(['docker', 'inspect', 'addp-postgres'], text=True, capture_output=True, check=True, timeout=10)
        item = json.loads(result.stdout)[0]
        labels = item['Config']['Labels']
        env = dict(value.split('=', 1) for value in item['Config']['Env'] if '=' in value)
        if (not item['State']['Running'] or labels.get('com.docker.compose.service') != 'postgres'
                or labels.get('com.docker.compose.project.working_dir') != str(ROOT)
                or env.get('POSTGRES_DB') != 'addp_online' or not env.get('POSTGRES_USER') or not env.get('POSTGRES_PASSWORD')
                or any(env.get(key) != os.environ.get(key) for key in ('POSTGRES_USER', 'POSTGRES_PASSWORD'))):
            raise ValueError('Quality probe requires the owned Hosted Infra')
        return ['docker', 'exec', '-i', item['Id'], 'psql', '-X', '-qAt', '-v', 'ON_ERROR_STOP=1',
                '-U', env['POSTGRES_USER'], '-d', 'addp_online']

    def track(self, execution_id):
        if str(uuid.UUID(execution_id)) != execution_id:
            raise ValueError('invalid Quality execution UUID')
        self.executions.add(execution_id)

    def snapshot(self, execution_id):
        if execution_id not in self.executions:
            raise ValueError('Quality private probe is outside this run')
        statement = f"""BEGIN READ ONLY;
SELECT row_to_json(e) FROM (
 SELECT execution_id, created_at, started_at, completed_at, execution_time_ms,
        status, attempt, lease_token, lease_owner, lease_expires_at,
        (lease_expires_at < clock_timestamp()) AS expired
 FROM common.task_executions
 WHERE tenant_id={self.tenant_id} AND module='quality' AND task_type='quality_plan'
   AND source_task_id='{self.plan_id}' AND execution_id='{execution_id}'
) e;
COMMIT;
"""
        result = subprocess.run(self.postgres(), input=statement, text=True, capture_output=True, timeout=10)
        if result.returncode:
            raise ValueError('Quality private read-only snapshot failed')
        item = json.loads(result.stdout)
        if not isinstance(item, dict) or item.get('execution_id') != execution_id:
            raise ValueError('Quality private snapshot identity mismatch')
        return item

    def source_waiters(self):
        # Only the fixture role waiting on the Model-owned table is relevant.
        sql = """BEGIN READ ONLY;
SELECT count(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid
WHERE a.datname='quality_fixture' AND a.usename='quality_owner'
  AND l.relation='public.quality_target_a'::regclass AND NOT l.granted;
COMMIT;
"""
        result = subprocess.run(self.postgres(True), input=sql, text=True, capture_output=True, timeout=10)
        if result.returncode or not result.stdout.strip().isdigit():
            raise ValueError('Quality source wait probe failed')
        return int(result.stdout.strip())

    def old_worker_rejected(self, execution_id):
        if execution_id not in self.executions:
            raise ValueError('Quality log probe is outside this run')
        row, _ = self.pins[UNITS[0]]
        if process_record(UNITS[0]) != row:
            raise ValueError('Quality log process identity changed')
        directory = ROOT / 'logs/runtime/quality' / row['instance_id']
        paths = list(directory.glob('*.jsonl'))
        if len(paths) > 16:
            raise ValueError('Quality log probe exceeds its budget')
        for path in paths:
            if path.is_symlink() or path.stat().st_size > 16 * 1024 * 1024:
                raise ValueError('invalid Quality runtime log source')
            if f'quality plan {execution_id} lease renewal failed:' in path.read_text():
                return True
        return False

    @contextmanager
    def locked(self, kind):
        if kind not in {'source', 'events'}:
            raise ValueError('invalid Quality lock scope')
        table = 'public.quality_target_a' if kind == 'source' else 'common.execution_events'
        # The session stays idle in a transaction. Closing stdin rolls it back.
        process = subprocess.Popen(self.postgres(kind == 'source'), text=True, stdin=subprocess.PIPE,
                                   stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, bufsize=1)
        try:
            process.stdin.write(f"BEGIN; SET LOCAL lock_timeout='10s'; LOCK TABLE {table} IN ACCESS EXCLUSIVE MODE; SELECT 'quality-lock-ready';\n")
            process.stdin.flush()
            if not select.select([process.stdout], [], [], 15)[0] or process.stdout.readline().strip() != 'quality-lock-ready':
                raise ValueError('Quality fixture lock did not become ready')
            yield
        finally:
            if process.stdin and not process.stdin.closed:
                process.stdin.close()
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)
                raise ValueError('Quality fixture lock cleanup failed')
            if process.returncode:
                raise ValueError('Quality fixture lock session failed')


if __name__ == '__main__':
    try:
        if sys.argv[1:] != ['resume']:
            raise ValueError('only private recovery is supported')
        resume_paused()
    except (OSError, ValueError, KeyError):
        print('Quality paused Worker recovery failed', file=sys.stderr)
        raise SystemExit(1)
