"""Owned disposable Hosted NFS export lifecycle shared by spatial suites."""
import json
import os
from pathlib import Path
import re
import tempfile


class FixtureError(RuntimeError):
    pass


class NFSExport:
    def __init__(self, prefix, command):
        self.prefix, self.command = prefix, command

    def guard(self, env):
        if any(env.get(k) != v for k, v in {'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux',
                'ADDP_ONLINE_HOST': '1', 'ADDP_ONLINE_HOSTED': '1'}.items()):
            raise FixtureError('requires an exclusive GitHub Hosted Linux Runner')
        if self.command(['uname', '-sm']) != 'Linux x86_64':
            raise FixtureError('requires Linux x86_64')
        if not re.fullmatch(self.prefix + r'-[0-9]+-[0-9]+', env.get('ADDP_ONLINE_TEST_RUN_ID', '')):
            raise FixtureError('invalid NFS run identity')
        secret = Path(env['ADDP_ONLINE_SECRET_DIR'])
        if not secret.is_absolute() or secret.resolve() != secret or not secret.is_dir() or secret.stat().st_uid != os.getuid() or secret.stat().st_mode & 0o077:
            raise FixtureError('requires an owned private secret directory')
        return secret

    def owned_root(self, secret, run_id):
        state = secret / (self.prefix + '-root')
        if state.is_symlink() or not state.is_file():
            raise FixtureError('missing NFS root state')
        root = Path(state.read_text().strip())
        if (root.parent != Path('/tmp') or not re.fullmatch('addp-' + self.prefix + r'-[A-Za-z0-9_]+', root.name)
            or root.resolve() != root or not root.is_dir() or root.stat().st_uid != os.getuid()
            or (root / '.addp-online-owner').is_symlink()
            or (root / '.addp-online-owner').read_text() != run_id):
            raise FixtureError('NFS root ownership mismatch')
        return root

    def lifecycle(self, action, secret, run_id):
        state = secret / (self.prefix + '-root')
        export = Path('/etc/exports.d') / f'addp-{run_id}.exports'
        descriptor = secret / (self.prefix + '-nfs-engine.json')
        if state.is_symlink() or export.is_symlink() or descriptor.is_symlink():
            raise FixtureError('refusing symlink fixture state or export')
        if action == 'start':
            if state.exists() or export.exists() or descriptor.exists() or self.command(['sudo', 'exportfs', '-s']):
                raise FixtureError('refusing existing state or NFS exports')
            root = Path(tempfile.mkdtemp(prefix='addp-' + self.prefix + '-', dir='/tmp'))
            state.write_text(str(root)); state.chmod(0o600)
            (root / '.addp-online-owner').write_text(run_id)
            root.chmod(0o755); (root / 'outputs').mkdir(mode=0o755)
            self.command(['sudo', 'mkdir', '-p', str(export.parent)])
            self.command(['sudo', 'tee', str(export)], input=f'{root} 127.0.0.1(rw,sync,no_subtree_check,insecure,root_squash)\n')
            self.command(['sudo', 'systemctl', 'start', 'rpcbind.service', 'nfs-kernel-server.service'])
            self.command(['sudo', 'exportfs', '-ra'])
            if str(root) not in [line.split()[0] for line in self.command(['showmount', '-e', '127.0.0.1']).splitlines()[1:]]:
                raise FixtureError('NFS export not discoverable')
            with descriptor.open('x') as stream:
                descriptor.chmod(0o600)
                json.dump({'name': f'Hosted {self.prefix} NFS', 'description': 'Disposable spatial persistence T4 fixture',
                    'engine_type': 'nfs', 'engine_origin': 'general',
                    'connection_info': {'server': '127.0.0.1', 'export_path': str(root)}}, stream)
            return {'started': True}
        if action != 'stop':
            raise FixtureError('unknown NFS lifecycle action')
        if not state.exists():
            if export.exists() or descriptor.exists():
                raise FixtureError('NFS export without ownership state')
            return {'cleanup': 'not_started'}
        root = self.owned_root(secret, run_id)
        self.command(['sudo', 'rm', '-f', str(export)]); self.command(['sudo', 'exportfs', '-ra'])
        if self.command(['sudo', 'exportfs', '-s']):
            raise FixtureError('unexpected remaining NFS exports')
        self.command(['sudo', 'systemctl', 'stop', 'nfs-kernel-server.service', 'rpcbind.service', 'rpcbind.socket'])
        self.command(['sudo', 'rm', '-rf', '--', str(root)])
        state.unlink(); descriptor.unlink(missing_ok=True)
        if root.exists() or export.exists():
            raise FixtureError('NFS cleanup has residuals')
        return {'cleanup': 'passed', 'nfs_export_removed': True, 'owned_root_removed': True}


def run_runtime_worker(command, runtime, suite, script, action, root, names):
    info = json.loads(command(['docker', 'inspect', runtime]))[0]
    labels = info['Config']['Labels']
    if (labels.get('com.addp.online-runtime') != suite
        or labels.get('com.addp.online-run-id') != os.environ['ADDP_ONLINE_TEST_RUN_ID']
        or info['Config']['Cmd'] != ['sh', 'container_entrypoint.sh']):
        raise FixtureError('foreign Runtime or non-product entry')
    payload = command(['docker', 'exec', '-i', runtime, 'python', '-', '--worker', action, str(root), *names],
                      input=script.read_text())
    return json.loads(payload)
