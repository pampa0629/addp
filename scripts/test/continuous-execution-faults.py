"""External process faults restricted to this disposable Hosted deployment."""
from __future__ import annotations

import hashlib
import importlib
import json
import os
from pathlib import Path
import select
import signal
import subprocess
import time

SUPPORT = importlib.import_module('scripts.test.orchestrator-execution-faults')


def validate_process(process: Path, root: Path, secret: Path, module: str):
    executable = (process / 'exe').resolve(strict=True)
    environment = dict(x.split('=', 1) for x in (process / 'environ').read_bytes().decode().split('\0') if '=' in x)
    binary = 'transfer-continuous-worker' if module == 'transfer-continuous-worker-1' else module
    if (module not in {'transfer', 'transfer-continuous-worker-1'} or
        executable != root / f'.dev-bins/addp-{binary}' or process.stat().st_uid != os.getuid() or
        environment.get('ADDP_ONLINE_SECRET_DIR') != str(secret) or
        environment.get('POSTGRES_DB') != 'addp_online' or environment.get('ADDP_ONLINE_HOSTED') != '1'):
        raise ValueError('refusing to signal an unowned Transfer process')


def runtime_environment(environment):
    markers = {'ADDP_ONLINE_HOST', 'ADDP_ONLINE_HOSTED', 'ADDP_ONLINE_TEST',
               'ADDP_ONLINE_SECRET_DIR', 'ADDP_ONLINE_ARTIFACT_DIR', 'ADDP_ONLINE_ENV_FILE'}
    return {key: value for key, value in environment.items()
            if not key.startswith('ADDP_ONLINE_') or key in markers}


def wait_owner_state(pid, stopped):
    deadline = time.monotonic() + 5
    while True:
        state = next((line.split()[1] for line in (Path('/proc') / str(pid) / 'status').read_text().splitlines()
                      if line.startswith('State:')), '')
        if (stopped and state == 'T') or (not stopped and state in {'R', 'S', 'D'}):
            return
        if time.monotonic() >= deadline:
            raise ValueError('owned Owner process did not reach its expected state')
        time.sleep(.05)


class HostedFaults:
    def __init__(self):
        self.secret = SUPPORT.deployment_secret()
        self.root = Path(__file__).resolve().parents[2]

    def pin(self, module):
        pid = int((self.root / f'.dev-pids/{module}.pid').read_text().strip())
        if pid <= 1:
            raise ValueError('invalid owned PID')
        descriptor = os.pidfd_open(pid)
        try:
            validate_process(Path('/proc') / str(pid), self.root, self.secret, module)
        except BaseException:
            os.close(descriptor)
            raise
        return pid, descriptor

    def protection_ready(self, tenant_id):
        # Read the Owner's canonical durable readiness fact, never set it in a fixture.
        # This probe is admitted only for this exclusive Hosted Infra and its actual credentials.
        if type(tenant_id) is not int or tenant_id <= 1 or SUPPORT.deployment_secret() != self.secret:
            raise ValueError('invalid protection readiness boundary')
        inspected = subprocess.run(['docker', 'inspect', 'addp-postgres'], check=True,
                                   capture_output=True, text=True, timeout=10)
        container = json.loads(inspected.stdout)[0]
        labels = container['Config']['Labels']
        environment = dict(value.split('=', 1) for value in container['Config']['Env'] if '=' in value)
        if (not container['State']['Running'] or labels.get('com.docker.compose.service') != 'postgres'
                or labels.get('com.docker.compose.project.working_dir') != str(self.root)
                or environment.get('POSTGRES_DB') != 'addp_online' or not environment.get('POSTGRES_PASSWORD')
                or any(environment.get(key) != os.environ.get(key) for key in ('POSTGRES_USER', 'POSTGRES_PASSWORD'))):
            raise ValueError('protection readiness PostgreSQL is not owned by this deployment')
        sql = ('BEGIN READ ONLY; SELECT initialized::text FROM transfer.protection_projection_checkpoints '
               f'WHERE tenant_id = {tenant_id}; COMMIT;')
        result = subprocess.run(['docker', 'exec', '-i', container['Id'], 'psql', '-X', '-qAt',
                                 '-v', 'ON_ERROR_STOP=1', '-U', environment['POSTGRES_USER'], '-d', 'addp_online'],
                                input=sql, check=True, capture_output=True, text=True, timeout=10)
        state = result.stdout.strip()
        if state not in {'', 'false', 'true'}:
            raise ValueError('invalid Owner protection readiness fact')
        return state == 'true'

    def wait_protection_ready(self, tenant_id):
        deadline = time.monotonic() + 90
        while not self.protection_ready(tenant_id):
            if time.monotonic() >= deadline:
                raise ValueError('Owner initial protection synchronization did not become ready')
            time.sleep(.5)

    def kill_worker_and_replace(self):
        pid, descriptor = self.pin('transfer-continuous-worker-1')
        try:
            signal.pidfd_send_signal(descriptor, signal.SIGKILL)
            if not select.select([descriptor], [], [], 10)[0]:
                raise ValueError('owned Worker did not exit after SIGKILL')
        finally:
            os.close(descriptor)
        # Keep human tokens/passwords in the acceptance process, never the product runtime.
        env = {**runtime_environment(os.environ), 'SKIP_MODTIDY': '1'}
        path = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']) / 'continuous-worker-replacement.log'
        with path.open('a') as output:
            subprocess.run(['bash', 'scripts/dev/start.sh', '-transfer'], cwd=self.root,
                           env=env, stdout=output, stderr=subprocess.STDOUT, check=True, timeout=180)
        replacement, descriptor = self.pin('transfer-continuous-worker-1')
        os.close(descriptor)
        if replacement == pid:
            raise ValueError('replacement must be a distinct proven Worker')
        binary = self.root / '.dev-bins/addp-transfer-continuous-worker'
        return {'old_pid': pid, 'replacement_pid': replacement,
                'executable_sha256': hashlib.sha256(binary.read_bytes()).hexdigest()}

    def stop_owner_during(self, operation):
        pid, descriptor = self.pin('transfer')
        try:
            signal.pidfd_send_signal(descriptor, signal.SIGSTOP)
            wait_owner_state(pid, True)
            operation()
        finally:
            try:
                signal.pidfd_send_signal(descriptor, signal.SIGCONT)
                wait_owner_state(pid, False)
            finally:
                os.close(descriptor)
        return {'owner_pid': pid, 'stopped_verified': True, 'resumed_verified': True}
