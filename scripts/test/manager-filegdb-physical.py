"""Owned FileGDB source, real read-only NFS mount and two bounded Hosted faults."""
from __future__ import annotations
from contextlib import contextmanager
from copy import deepcopy
from datetime import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import socket
import subprocess
import sys
import tempfile
import time

NAME = 'authorization.gdb'


def require(value, message):
    if not value:
        raise ValueError(message)


def command(args, **kwargs):
    return subprocess.run(args, text=True, capture_output=True, check=True, timeout=120, **kwargs).stdout.strip()


def owned_root(environment=None):
    environment = os.environ if environment is None else environment
    require(all(environment.get(k) == v for k, v in {
        'ADDP_ONLINE_HOSTED': '1', 'ADDP_ONLINE_HOST': '1', 'GITHUB_ACTIONS': 'true',
        'RUNNER_OS': 'Linux', 'ADDP_ONLINE_MANAGER_CASE': 'filegdb-authorization'}.items())
        and sys.platform == 'linux' and os.uname().machine == 'x86_64', 'Hosted FileGDB only')
    secret = Path(environment['ADDP_ONLINE_SECRET_DIR'])
    require(secret.is_absolute() and not secret.is_symlink() and secret.stat().st_mode & 0o077 == 0,
            'private state required')
    root = Path((secret / 'nfs-root').read_text().strip())
    require(re.fullmatch(r'/tmp/addp-online-manager-nfs\.[A-Za-z0-9]{6}', str(root))
        and root.is_dir() and not root.is_symlink()
        and (root / '.addp-online-owner').read_text().strip() == environment['ADDP_ONLINE_TEST_RUN_ID'],
        'foreign NFS root')
    return root, secret


def physical(action, root):
    expected, _ = owned_root()
    require(root == expected and action in {'seed', 'add', 'snapshot', 'delete'}, 'unowned physical action')
    return json.loads(command(['/usr/bin/python3', str(Path(__file__).resolve()), '--worker', action, str(root)]))


def native(action, root):
    # This worker also checks ownership; no command-line path can escape the run.
    require(root == owned_root()[0], 'native worker root mismatch')
    path, marker = root / NAME, root / '.filegdb-acceptance-owner'
    run = os.environ['ADDP_ONLINE_TEST_RUN_ID']
    require(not path.is_symlink() and not marker.is_symlink(), 'source symlink refused')
    if action == 'seed':
        require(not path.exists() and not marker.exists(), 'source already exists')
        marker.write_text(run)
    else:
        require(marker.is_file() and marker.read_text() == run, 'source not owned')
    if action == 'delete':
        if path.exists():
            shutil.rmtree(path)
        marker.unlink()
        return {'source_removed': not path.exists()}
    if action in {'seed', 'add'}:
        from osgeo import ogr, osr
        driver = ogr.GetDriverByName('OpenFileGDB')
        require(driver is not None, 'OpenFileGDB driver unavailable')
        dataset = driver.CreateDataSource(str(path)) if action == 'seed' else driver.Open(str(path), 1)
        require(dataset is not None, 'FileGDB source could not be opened')
        layer_name, count = ('initial_points', 2) if action == 'seed' else ('added_points', 1)
        require(dataset.GetLayerByName(layer_name) is None, 'layer already exists')
        crs = osr.SpatialReference(); require(crs.ImportFromEPSG(4326) == 0, 'CRS unavailable')
        layer = dataset.CreateLayer(layer_name, srs=crs, geom_type=ogr.wkbPoint)
        require(layer is not None, 'layer creation failed')
        require(layer.CreateField(ogr.FieldDefn('label', ogr.OFTString)) == 0, 'label creation failed')
        require(layer.CreateField(ogr.FieldDefn('value', ogr.OFTInteger)) == 0, 'value creation failed')
        for index in range(count):
            feature = ogr.Feature(layer.GetLayerDefn())
            feature.SetField('label', layer_name + '_' + str(index))
            feature.SetField('value', index + 1)
            point = ogr.Geometry(ogr.wkbPoint); point.AddPoint_2D(index, index)
            feature.SetGeometry(point)
            require(layer.CreateFeature(feature) == 0, 'feature creation failed')
            feature = None
        layer = None; dataset = None
        return {'layer': layer_name, 'rows': count}
    require(action == 'snapshot' and path.is_dir(), 'source snapshot missing')
    files = sorted(path.iterdir())
    require(files and all(f.is_file() and not f.is_symlink() for f in files), 'unexpected source member')
    return {f.name: hashlib.sha256(f.read_bytes()).hexdigest() for f in files}


def mount(action):
    root, secret = owned_root()
    state = secret / 'filegdb-nfs-mount'
    if action == 'mount':
        require(not state.exists(), 'read mount already owned')
        path = Path(tempfile.mkdtemp(prefix='addp-filegdb-read-', dir='/tmp'))
        state.write_text(str(path)); state.chmod(0o600)
        command(['sudo', 'mount', '-t', 'nfs', '-o',
                 'ro,vers=3,proto=tcp,soft,timeo=10,retrans=1,noac,lookupcache=none',
                 '127.0.0.1:' + str(root), str(path)])
        return {'mounted': True}
    require(action == 'unmount', 'invalid mount action')
    if not state.exists():
        return {'mount_cleanup': 'not_started'}
    path = Path(state.read_text().strip())
    require(re.fullmatch(r'/tmp/addp-filegdb-read-[A-Za-z0-9_]+', str(path)) and not path.is_symlink(), 'foreign read mount')
    # The existing outer gate stops Runtime before reaching this fixture cleanup.
    if os.path.ismount(path):
        command(['sudo', 'umount', str(path)])
    path.rmdir(); state.unlink()
    return {'mount_cleanup': 'passed'}


@contextmanager
def interruption(repository, environment, module):
    root, secret = owned_root()
    require(module in {'manager', 'nfs'}, 'unsupported fault')
    def lifecycle(recover=False):
        if module == 'manager':
            args = (['bash', 'scripts/dev/start.sh', '--exact-process', '-manager'] if recover else
                    ['bash', 'scripts/dev/stop-exact-process.sh', '--force', '-manager'])
        else:
            args = ['sudo', 'systemctl', 'start' if recover else 'stop', 'nfs-kernel-server.service']
        # Capture no inherited user credentials in process output or public logs.
        launch_environment = {k: v for k, v in environment.items() if not k.startswith((
            'ADDP_ONLINE_TEST_USER_', 'ADDP_ONLINE_FIXTURE_', 'ADDP_ONLINE_ADMIN_USER_'))}
        launch_environment['SKIP_MODTIDY'] = '1'
        with (secret / ('filegdb-' + module + '-lifecycle.log')).open('a') as log:
            result = subprocess.run(args, cwd=repository, env=launch_environment, stdout=log, stderr=subprocess.STDOUT, timeout=120)
        require(result.returncode == 0, 'isolated lifecycle operation failed')
    try:
        observation = {'module': module}
        if module == 'nfs':
            read_root = Path((secret / 'filegdb-nfs-mount').read_text().strip())
            leaf = next((root / NAME).glob('*.gdbtable'))
            probe = ['timeout', '12', 'stat', str(read_root / NAME / leaf.name)]
            require(subprocess.run(probe, capture_output=True, timeout=15).returncode == 0,
                    'NFS data path was not available before injection')
        lifecycle()
        if module == 'manager':
            try:
                with socket.create_connection(('127.0.0.1', 8081), timeout=3):
                    raise ValueError('Manager still accepts connections')
            except ConnectionRefusedError:
                pass
        else:
            # A stopped daemon alone is not proof: test the real uncached mount.
            result = subprocess.run(probe, capture_output=True, timeout=15)
            require(result.returncode == 1, 'NFS data path did not fail with a bounded I/O error')
            observation.update(source_probe_before=0, source_probe_during=result.returncode)
        yield observation
    finally:
        lifecycle(recover=True)


def rule_snapshot(response):
    """Compare every rule field; only the per-query inspection instant is observational."""
    result = deepcopy(response)
    for row in result['data']:
        observed = row['inspection'].pop('observed_at', None)
        require(isinstance(observed, str), 'inspection instant missing')
        instant = datetime.fromisoformat(observed.replace('Z', '+00:00'))
        require(instant.tzinfo is not None, 'inspection instant must include timezone')
    return result


def verify_outages(repository, environment, client, locator, current, snapshot, preview):
    API = __import__('importlib').import_module('scripts.utils.online-api')
    before, source, result = current(), snapshot(), []
    rules = rule_snapshot(before)
    health = API.GatewayClient(environment['MANAGER_URL'], '', 10)
    prior = health.request('GET', '/health/ready', (200,)).payload
    for module in ('manager', 'nfs'):
        with interruption(repository, environment, module) as observation:
            response = preview(client, locator, 'added_points', (500, 502, 503, 504))
            during = current()
            require(rule_snapshot(during) == rules, 'service outage changed the original Grant')
        API.wait_manager_read_boundary(client, int(environment['ADDP_ONLINE_MANAGER_NFS_ENGINE_ID']), time.monotonic() + 120)
        preview(client, locator, 'added_points', 200, 1)
        recovered = current()
        require(rule_snapshot(recovered) == rules and snapshot() == source, 'recovery changed source or original Grant')
        after = health.request('GET', '/health/ready', (200,)).payload
        if module == 'manager':
            require(after.get('instance_id') != prior.get('instance_id')
                    and all(after.get(k) == prior.get(k) for k in ('git_commit', 'build_id', 'source_fingerprint')),
                    'Manager recovery build identity differs')
        fields = ('instance_id', 'git_commit', 'build_id', 'source_fingerprint', 'started_at')
        result.append({**observation, 'failed_read': response, 'same_grant': True, 'source_unchanged': True,
            'current_before': before, 'current_during': during, 'current_after': recovered,
            'before_identity': {k: prior.get(k) for k in fields}, 'after_identity': {k: after.get(k) for k in fields}})
    return result


if __name__ == '__main__':
    if len(sys.argv) == 4 and sys.argv[1] == '--worker':
        print(json.dumps(native(sys.argv[2], Path(sys.argv[3]))))
    else:
        require(len(sys.argv) == 2, 'requires mount/unmount')
        print(json.dumps(mount(sys.argv[1])))
