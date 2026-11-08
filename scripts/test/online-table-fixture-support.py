"""Owned physical worker transport for the existing Hosted table fixtures."""
from __future__ import annotations

import json
import os
from pathlib import Path
import re
import subprocess


def physical_worker(action, repository, environment, *, profile, script):
    cases = {'orc': 'native-table-orc', 'avro': 'native-table-avro', 'feather': 'native-table-feather', 'delimited': 'delimited-read-settings'}
    if profile not in cases or action not in {'seed', 'verify', 'delete'}:
        raise ValueError('unknown table fixture profile or action')
    if script != f'business/scripts/online-{profile}-fixture.py':
        raise ValueError('unexpected table fixture worker')
    if any(environment.get(k) != v for k, v in {
        'ADDP_ONLINE_HOSTED': '1', 'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux',
        'ADDP_ONLINE_MANAGER_CASE': cases[profile],
    }.items()):
        raise ValueError(f'{profile.upper()} physical verifier requires its exclusive Hosted case')
    state = Path(environment['ADDP_ONLINE_SECRET_DIR']) / 'nfs-root'
    if state.is_symlink():
        raise ValueError(f'invalid {profile.upper()} fixture state')
    root = Path(state.read_text().strip())
    if (not re.fullmatch(r'/tmp/addp-online-manager-nfs\.[A-Za-z0-9]{6}', str(root))
            or root.is_symlink() or (root / '.addp-online-owner').read_text().strip()
            != environment['ADDP_ONLINE_TEST_RUN_ID']):
        raise ValueError(f'{profile.upper()} fixture ownership mismatch')
    config = {'action': action, 'nfs_root': str(root), 'storage': {
        'endpoint': '127.0.0.1:' + environment['ADDP_ONLINE_MANAGER_MINIO_PORT'],
        'access_key': environment['ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY'],
        'secret_key': environment['ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY'],
        'bucket': environment['ADDP_ONLINE_MANAGER_MINIO_BUCKET'],
    }}
    result = subprocess.run([
        'docker', 'run', '--rm', '-i', '--user', f'{os.getuid()}:{os.getgid()}',
        '--name', 'addp-manager-raster-verifier', '--network', 'host',
        '--label', 'com.addp.online-fixture=manager-internal-artifact-lineage',
        '--mount', f'type=bind,src={Path(repository) / script},dst=/fixture.py,readonly',
        '--mount', f'type=bind,src={root},dst={root}',
        environment['ADDP_ONLINE_RASTER_RUNTIME_IMAGE'], 'python', '/fixture.py', 'worker',
    ], input=json.dumps(config), text=True, capture_output=True, timeout=120)
    if result.returncode:
        code = 'worker_failed'
        try:
            candidate = json.loads(result.stdout).get('error_code')
            if isinstance(candidate, str) and re.fullmatch(r'[A-Za-z_][A-Za-z0-9_]{0,80}', candidate):
                code = candidate
        except (ValueError, AttributeError):
            pass
        raise ValueError(f'{profile.upper()} physical {action} failed ({code}, exit {result.returncode})')
    return json.loads(result.stdout)
