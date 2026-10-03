#!/usr/bin/env python3
"""Own two disposable physical MinIO fixtures; never call platform APIs."""
from __future__ import annotations

import hashlib
import json
import math
import os
from pathlib import Path
import secrets
import stat
import struct
import subprocess
import sys
import tempfile
import time
import urllib.request

SUITE = 'raster-workflow'
CONTAINERS = ('addp-raster-source', 'addp-raster-target', 'addp-raster-fixture-worker')
IMAGE = 'addp-geopython-workflow-engine:dev'
MINIO_IMAGE = 'addp-minio:RELEASE.2025-10-15T17-29-55Z'
TRANSFORM = (110, .01, 0, 20.32, 0, -.01)
SIZE = 256


class FixtureError(RuntimeError):
    pass


def command(args, **kwargs):
    result = subprocess.run(args, capture_output=True, text=True, timeout=300, **kwargs)
    if result.returncode:
        # Docker/GDAL/S3 exceptions can embed passwords or signed URLs.
        raise FixtureError(f'physical fixture command failed: {args[0]} ({result.returncode})')
    return result.stdout.strip()


def private_json(path, payload):
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as stream:
        json.dump(payload, stream)


def environment():
    if any(os.environ.get(key) != value for key, value in {
        'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux', 'ADDP_ONLINE_HOST': '1', 'ADDP_ONLINE_HOSTED': '1'
    }.items()) or os.uname().sysname != 'Linux' or os.uname().machine != 'x86_64':
        raise FixtureError('raster fixture requires GitHub Hosted Linux x86_64')
    root = Path(os.environ['ADDP_ONLINE_SECRET_DIR']).resolve()
    runner = Path(os.environ['RUNNER_TEMP']).resolve()
    if root.parent != runner or not root.name.startswith('addp-online-secret-'):
        raise FixtureError('fixture secrets must use the dedicated runner.temp directory')
    if not root.is_dir() or stat.S_IMODE(root.stat().st_mode) & 0o077:
        raise FixtureError('fixture secret directory must be owner-only')
    return root


def owned(name):
    result = subprocess.run(['docker', 'container', 'inspect', name], capture_output=True, text=True, timeout=30)
    if result.returncode:
        # Distinguish absent containers from an unavailable Docker daemon.
        command(['docker', 'info', '--format', '{{.ServerVersion}}'])
        return False
    payload = json.loads(result.stdout)[0]
    if payload.get('Config', {}).get('Labels', {}).get('com.addp.online-fixture') != SUITE:
        raise FixtureError(f'refusing container owned by another task: {name}')
    return True


def stop():
    errors = []
    for name in reversed(CONTAINERS):
        try:
            if owned(name):
                command(['docker', 'rm', '-fv', name])
            if owned(name):
                raise FixtureError(f'container cleanup has residuals: {name}')
        except (FixtureError, subprocess.SubprocessError) as error:
            errors.append(str(error))
    if errors:
        raise FixtureError('; '.join(errors))


def start(root):
    for name in CONTAINERS:
        if owned(name):
            raise FixtureError('refusing to reuse an existing raster fixture')
    config = {}
    for index, role in enumerate(('source', 'target')):
        config[role] = {
            'endpoint': f'127.0.0.1:{59012 + index}', 'access_key': f'online-raster-{role}',
            'secret_key': secrets.token_hex(32), 'bucket': f'raster-{role}',
        }
    private_json(root / 'raster-fixture.json', config)
    try:
        for index, role in enumerate(('source', 'target')):
            entry = config[role]
            env_path = root / f'raster-{role}.env'
            with os.fdopen(os.open(env_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as stream:
                stream.write(f'MINIO_ROOT_USER={entry["access_key"]}\nMINIO_ROOT_PASSWORD={entry["secret_key"]}\n')
            command(['docker', 'run', '-d', '--name', CONTAINERS[index],
                     '--label', f'com.addp.online-fixture={SUITE}',
                     '--label', f'com.addp.online-run-id={os.environ["ADDP_ONLINE_TEST_RUN_ID"]}',
                     '--env-file', str(env_path), '--mount', 'type=tmpfs,destination=/data',
                     '-p', f'127.0.0.1:{59012 + index}:9000', MINIO_IMAGE, 'server', '/data'])
            deadline = time.monotonic() + 60
            while True:
                try:
                    with urllib.request.urlopen(f'http://{entry["endpoint"]}/minio/health/ready', timeout=3) as response:
                        if response.status == 200:
                            break
                except OSError:
                    pass
                if time.monotonic() >= deadline:
                    raise FixtureError(f'{role} MinIO readiness timed out')
                time.sleep(1)
            private_json(root / f'raster-{role}-engine.json', {
                'name': f'Hosted Raster {role} {os.environ["ADDP_ONLINE_TEST_RUN_ID"]}', 'description': 'Disposable raster T4 physical fixture',
                'engine_type': 'minio', 'engine_origin': 'general',
                'connection_info': {**{key: entry[key] for key in ('endpoint', 'access_key', 'secret_key')}, 'use_ssl': False},
            })
    except BaseException:
        stop()
        raise


def physical_worker(action, root):
    return json.loads(command([
        'docker', 'run', '--rm', '--name', CONTAINERS[2], '--label', f'com.addp.online-fixture={SUITE}',
        '--network', 'host', '--entrypoint', 'python',
        '-v', f'{Path(__file__).resolve()}:/fixture.py:ro', '-v', f'{root}:/secrets:ro',
        IMAGE, '/fixture.py', 'worker-' + action, '/secrets/raster-fixture.json',
    ]))


def worker(action, path):
    # GDAL is confined to the standard Runtime image. No production operator is
    # imported, so this verifies persisted bytes independently of raster_save.
    from minio import Minio
    from osgeo import gdal, osr
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    gdal.UseExceptions()
    config = json.loads(Path(path).read_text())
    clients = {role: Minio(entry['endpoint'], access_key=entry['access_key'], secret_key=entry['secret_key'], secure=False)
               for role, entry in config.items()}
    with tempfile.TemporaryDirectory(prefix='raster-physical-') as directory:
        source_path = Path(directory) / 'source.tif'
        if action == 'seed':
            for role, client in clients.items():
                client.make_bucket(config[role]['bucket'])
            dataset = gdal.GetDriverByName('GTiff').Create(str(source_path), SIZE, SIZE, 2, gdal.GDT_Float64)
            dataset.SetGeoTransform(TRANSFORM)
            crs = osr.SpatialReference(); crs.ImportFromEPSG(4326)
            dataset.SetProjection(crs.ExportToWkt())
            for index in (1, 2):
                values = [-9999.] + [float(index * value) for value in range(2, SIZE * SIZE + 1)]
                band = dataset.GetRasterBand(index)
                band.SetNoDataValue(-9999)
                band.WriteRaster(0, 0, SIZE, SIZE, struct.pack(f'<{len(values)}d', *values), buf_type=gdal.GDT_Float64)
            dataset = None
            clients['source'].fput_object(config['source']['bucket'], 'source.tif', str(source_path), content_type='image/tiff')
            return {'seeded': True, 'source_pixels': SIZE * SIZE, 'target_objects': 0}
        clients['source'].fget_object(config['source']['bucket'], 'source.tif', str(source_path))
        source = gdal.Open(str(source_path))
        for index in (1, 2):
            band = source.GetRasterBand(index)
            if band.GetNoDataValue() != -9999:
                raise FixtureError('workflow modified source NoData')
            values = struct.unpack(f'<{SIZE * SIZE}d', band.ReadRaster(buf_type=gdal.GDT_Float64))
            if values != tuple([-9999.] + [float(index * value) for value in range(2, SIZE * SIZE + 1)]):
                raise FixtureError('workflow modified its source pixels')
        if source.GetGeoTransform() != TRANSFORM or source.RasterCount != 2:
            raise FixtureError('workflow modified source georeferencing/bands')
        original_crs = osr.SpatialReference(); original_crs.ImportFromEPSG(4326)
        if not osr.SpatialReference(source.GetProjection()).IsSame(original_crs):
            raise FixtureError('workflow modified source CRS')
        target_path = Path(directory) / 'result.cog.tif'
        clients['target'].fget_object(config['target']['bucket'], 'result.cog.tif', str(target_path))
        dataset = gdal.Open(str(target_path))
        warnings, errors, _ = validate(dataset, full_check=True)
        if errors or dataset.GetMetadataItem('LAYOUT', 'IMAGE_STRUCTURE') != 'COG':
            raise FixtureError('persisted target is not a valid COG')
        crs = osr.SpatialReference(); crs.ImportFromEPSG(4326)
        actual_crs = osr.SpatialReference(dataset.GetProjection())
        if not actual_crs.IsSame(crs) or dataset.GetGeoTransform() != TRANSFORM:
            raise FixtureError('persisted target changed CRS/transform')
        if (dataset.RasterXSize, dataset.RasterYSize, dataset.RasterCount) != (SIZE, SIZE, 1):
            raise FixtureError('persisted target has invalid dimensions/bands')
        band = dataset.GetRasterBand(1)
        values = struct.unpack(f'<{SIZE * SIZE}d', band.ReadRaster(buf_type=gdal.GDT_Float64))
        factor = 3 if action == 'verify-replace' else 1
        if not math.isnan(band.GetNoDataValue()) or not math.isnan(values[0]):
            raise FixtureError('persisted target did not propagate NoData')
        if values[1:] != tuple(float(factor * value) for value in range(2, SIZE * SIZE + 1)):
            raise FixtureError('persisted target pixels do not match the band expression')
        if [item.object_name for item in clients['target'].list_objects(config['target']['bucket'], recursive=True)] != ['result.cog.tif']:
            raise FixtureError('target contains unexpected or partial artifacts')
        return {'cog_valid': True, 'cog_warnings': len(warnings), 'valid_pixels': SIZE * SIZE - 1,
                'invalid_pixels': 1, 'factor': factor, 'source_unchanged': True,
                'sha256': hashlib.sha256(target_path.read_bytes()).hexdigest()}


def main():
    action = sys.argv[1]
    if action.startswith('worker-'):
        if action not in ('worker-seed', 'worker-verify-create', 'worker-verify-replace'):
            raise FixtureError('unknown worker action')
        result = worker(action.removeprefix('worker-'), sys.argv[2])
    else:
        root = environment()
        if action == 'start':
            start(root); result = {'started': True}
        elif action == 'stop':
            stop(); result = {'containers': 0}
        elif action in ('seed', 'verify-create', 'verify-replace'):
            result = physical_worker(action, root)
        else:
            raise FixtureError('usage: fixture start|seed|verify-create|verify-replace|stop')
    print(json.dumps(result, sort_keys=True))


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        # Never print native SDK/subprocess exception text: it may hold credentials.
        reason = str(error) if isinstance(error, FixtureError) else type(error).__name__
        print(f'Raster physical fixture failed: {reason}', file=sys.stderr)
        raise SystemExit(1)
