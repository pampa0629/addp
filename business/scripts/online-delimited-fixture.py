"""Fixed-byte GB18030 sources for the existing isolated Manager Hosted suite."""
from __future__ import annotations
import hashlib
import importlib
import io
import json
from pathlib import Path
import sys

DIRECTORY = 'delimited-read-settings'
COUNT = 25
COLUMNS = ['id', '名称𠀀😀', '备注']


def source_bytes(format_name):
    if format_name not in ('csv', 'tsv'):
        raise ValueError('unsupported delimited fixture format')
    separator = b',' if format_name == 'csv' else b'\t'
    raw = b'"id"' + separator + bytes.fromhex('c3fbb3c6953282369439fc36') + separator + bytes.fromhex('b1b8d7a2') + b'\r\n'
    for i in range(COUNT):
        raw += str(i).encode() + separator + bytes.fromhex('b3c7cad0953282369439fc36') + str(i).encode() + separator
        raw += b'"' + bytes.fromhex('b5dad2bbd0d0') + b'\r\n' + bytes.fromhex('b5dab6fed0d02cbaacb6babac5') + b'"\r\n'
    return raw


def public_row(index):
    return {'id': index, COLUMNS[1]: f'城市𠀀😀{index}', COLUMNS[2]: '第一行\n第二行,含逗号'}


def sources():
    return [{'format': f, 'name': f'gb18030-quoted.{f}', 'size_bytes': len(raw),
             'sha256': hashlib.sha256(raw).hexdigest()} for f in ('csv', 'tsv') for raw in [source_bytes(f)]]


def worker(request):
    from minio import Minio
    access = request['storage']
    client = Minio(access['endpoint'], access_key=access['access_key'], secret_key=access['secret_key'], secure=False)
    bucket = access['bucket']
    directory = Path(request['nfs_root']) / DIRECTORY
    marker = directory / '.addp-delimited-fixture'
    action = request['action']
    assert not directory.is_symlink(), 'source_symlink'
    specs = sources()
    if action == 'seed':
        assert not list(client.list_objects(bucket, prefix=DIRECTORY + '/', recursive=True)), 'existing_inputs'
        directory.mkdir(exist_ok=False)
        marker.write_text('addp.delimited-fixture/v1')
        for spec in specs:
            raw = source_bytes(spec['format'])
            path = directory / spec['name']
            path.write_bytes(raw)
            path.chmod(0o644)
            client.put_object(bucket, DIRECTORY + '/' + path.name, io.BytesIO(raw), len(raw), content_type='text/plain')
        return {'sources': specs, 'count': COUNT}
    if action not in ('verify', 'delete'):
        raise ValueError('unknown delimited action')
    if not directory.exists() and action == 'delete':
        assert not list(client.list_objects(bucket, prefix=DIRECTORY + '/', recursive=True)), 'unowned_inputs'
        return {'objects_removed': True, 'files_removed': True}
    assert not marker.is_symlink() and marker.read_text() == 'addp.delimited-fixture/v1', 'fixture_ownership'
    assert all(not (directory / s['name']).is_symlink() for s in specs), 'source_symlink'
    if action == 'verify':
        for spec in specs:
            response = client.get_object(bucket, DIRECTORY + '/' + spec['name'])
            try:
                assert response.read() == (directory / spec['name']).read_bytes() == source_bytes(spec['format']), 'source_changed'
            finally:
                response.close()
                response.release_conn()
        return {'sources_unchanged': True}
    for spec in specs:
        client.remove_object(bucket, DIRECTORY + '/' + spec['name'])
        (directory / spec['name']).unlink(missing_ok=True)
    marker.unlink()
    directory.rmdir()
    assert not list(client.list_objects(bucket, prefix=DIRECTORY + '/', recursive=True)), 'input_cleanup'
    return {'objects_removed': True, 'files_removed': True}


def physical(action, repository, environment):
    return importlib.import_module('scripts.test.online-table-fixture-support').physical_worker(
        action, repository, environment, profile='delimited', script='business/scripts/online-delimited-fixture.py')


if __name__ == '__main__':
    try:
        print(json.dumps(worker(json.load(sys.stdin))))
    except Exception as error:
        codes = {'existing_inputs', 'unowned_inputs', 'fixture_ownership', 'source_symlink', 'source_changed', 'input_cleanup'}
        code = str(error) if isinstance(error, AssertionError) and str(error) in codes else type(error).__name__
        print(json.dumps({'error_code': code}))
        raise SystemExit(1)
