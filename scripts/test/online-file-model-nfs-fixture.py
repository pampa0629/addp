"""Disposable NFS source for the existing FTP/NFS exact-model Hosted gate."""
from importlib import import_module
import json
import os
from pathlib import Path
import subprocess
import sys

NFS = import_module('scripts.utils.online_nfs_fixture')
SEED = import_module('business.ftp.seed')

def command(args, **kwargs):
    return subprocess.run(args, check=True, text=True, capture_output=True, timeout=60, **kwargs).stdout.strip()

def owner():
    fixture = NFS.NFSExport('ftp', command, source_subdirectory='data', access_mode='ro')
    secret = fixture.guard(os.environ)
    return fixture, secret, os.environ['ADDP_ONLINE_TEST_RUN_ID']

def owned_root():
    fixture, secret, run_id = owner()
    root = fixture.owned_root(secret, run_id)
    descriptor = json.loads((secret / 'ftp-nfs-engine.json').read_text())
    source = root / 'data'
    if source.is_symlink() or not source.is_dir() or descriptor['connection_info'] != {'server':'127.0.0.1', 'export_path':str(source)}:
        raise NFS.FixtureError('NFS model source identity changed')
    return root

def main(action):
    fixture, secret, run_id = owner()
    result = fixture.lifecycle(action, secret, run_id)
    if action == 'start':
        root = fixture.owned_root(secret, run_id)
        source = root / 'data'
        SEED.write_samples(source)
        models = SEED.write_model_samples(source)
        (secret / 'nfs-model-samples.json').write_text(json.dumps(models))
        path = secret / 'ftp-nfs-engine.json'
        descriptor = json.loads(path.read_text())
        descriptor['name'] = 'Hosted NFS'
        descriptor['description'] = 'Disposable exact-composition NFS model source'
        path.write_text(json.dumps(descriptor))
        unsupported = source / 'unsupported'
        unsupported.mkdir(mode=0o755)
        (root / 'outside.txt').write_text('outside export sentinel')
        (unsupported / 'inside-link').symlink_to('../models/ifc/model.ifc')
        (unsupported / 'outside-link').symlink_to(root / 'outside.txt')
        (unsupported / 'directory-link').symlink_to('../models/ifc', target_is_directory=True)
        os.mkfifo(unsupported / 'fifo', mode=0o644)
    else:
        (secret / 'nfs-model-samples.json').unlink(missing_ok=True)
    print(json.dumps(result))

if __name__ == '__main__':
    if len(sys.argv) != 2 or sys.argv[1] not in {'start','stop'}:
        raise SystemExit('requires start or stop')
    main(sys.argv[1])
