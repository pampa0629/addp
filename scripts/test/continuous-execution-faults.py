"""External process faults restricted to this disposable Hosted deployment."""
from __future__ import annotations

import hashlib
import importlib
import os
from pathlib import Path
import select
import signal
import subprocess

SUPPORT = importlib.import_module('scripts.test.orchestrator-execution-faults')


def validate_process(process: Path, root: Path, secret: Path, module: str):
    executable = (process / 'exe').resolve(strict=True)
    environment = dict(x.split('=', 1) for x in (process / 'environ').read_bytes().decode().split('\0') if '=' in x)
    if (module not in {'transfer', 'transfer-continuous-worker'} or
        executable != root / f'.dev-bins/addp-{module}' or process.stat().st_uid != os.getuid() or
        environment.get('ADDP_ONLINE_SECRET_DIR') != str(secret) or
        environment.get('POSTGRES_DB') != 'addp_online' or environment.get('ADDP_ONLINE_HOSTED') != '1'):
        raise ValueError('refusing to signal an unowned Transfer process')


def runtime_environment(environment):
    markers = {'ADDP_ONLINE_HOST', 'ADDP_ONLINE_HOSTED', 'ADDP_ONLINE_TEST',
               'ADDP_ONLINE_SECRET_DIR', 'ADDP_ONLINE_ARTIFACT_DIR', 'ADDP_ONLINE_ENV_FILE'}
    return {key: value for key, value in environment.items()
            if not key.startswith('ADDP_ONLINE_') or key in markers}


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

    def kill_worker_and_replace(self):
        pid, descriptor = self.pin('transfer-continuous-worker')
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
        replacement, descriptor = self.pin('transfer-continuous-worker')
        os.close(descriptor)
        if replacement == pid:
            raise ValueError('replacement must be a distinct proven Worker')
        binary = self.root / '.dev-bins/addp-transfer-continuous-worker'
        return {'old_pid': pid, 'replacement_pid': replacement,
                'executable_sha256': hashlib.sha256(binary.read_bytes()).hexdigest()}

    def stop_owner_during(self, operation):
        _, descriptor = self.pin('transfer')
        try:
            signal.pidfd_send_signal(descriptor, signal.SIGSTOP)
            return operation()
        finally:
            try:
                signal.pidfd_send_signal(descriptor, signal.SIGCONT)
            finally:
                os.close(descriptor)
