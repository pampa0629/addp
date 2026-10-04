#!/usr/bin/env python3
"""Own two disposable physical MinIO fixtures; never call platform APIs."""
from __future__ import annotations

import hashlib
from bisect import bisect_right
import json
import math
import os
from pathlib import Path
import secrets
import stat
import statistics
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
ANGULAR_METRE = math.degrees(1 / 6378137)
SPATIAL_SOURCE_TRANSFORM = (0, ANGULAR_METRE, 0, SIZE * ANGULAR_METRE, 0, -ANGULAR_METRE)
SPATIAL_TRANSFORM = (0, 1, 0, SIZE, 0, -1)
SPATIAL_NODATA = {0, 127 * SIZE + 127}
ACTIONS = ('seed', 'verify-create', 'verify-replace', 'verify-mosaic-first', 'verify-mosaic-last', 'verify-analysis')
ANALYSIS_CASES = ('statistics-band-2', 'statistics-all-invalid', 'histogram-auto', 'histogram-range')


def analysis_expectations():
    """Independent numeric oracle from fixture formulas, without GDAL/NumPy/operators."""
    values = list(range(2, SIZE * SIZE + 1))
    total = SIZE * SIZE
    band_values = [2 * value for value in values]
    stats = {'band': 2, 'strategy': 'full', 'valid_count': len(values), 'invalid_count': 1,
             'nodata_ratio': 1 / total, 'min': min(band_values), 'max': max(band_values),
             'mean': statistics.fmean(band_values), 'stddev': statistics.pstdev(band_values)}

    def histogram(pixels, band, low, high):
        edges = [low + (high - low) * index / 4 for index in range(5)]
        counts = [0] * 4
        outside = 0
        for value in pixels:
            if value < low or value > high:
                outside += 1
            else:
                counts[min(bisect_right(edges, value) - 1, 3)] += 1
        return {'band': band, 'strategy': 'full', 'edges': edges, 'counts': counts,
                'valid_count': len(pixels), 'invalid_count': 1, 'outside_count': outside}

    return {
        'statistics-band-2': stats,
        'statistics-all-invalid': {'band': 1, 'strategy': 'full', 'valid_count': 0,
            'invalid_count': total, 'nodata_ratio': 1.0, 'min': None, 'max': None, 'mean': None, 'stddev': None},
        'histogram-auto': histogram(values, 1, min(values), max(values)),
        'histogram-range': histogram(band_values, 2, 32768, 98304),
    }


def source_values(band, spatial=False):
    invalid = SPATIAL_NODATA if spatial else {0}
    return tuple(-9999. if position in invalid else float(band * (position + 1)) for position in range(SIZE * SIZE))


def artifact_expectation(spatial=False):
    transform = SPATIAL_TRANSFORM if spatial else TRANSFORM
    return {'width': SIZE, 'height': SIZE, 'band_count': 1, 'source_crs': 'EPSG:3857' if spatial else 'EPSG:4326',
            'extent_srid': 3857 if spatial else 4326, 'transform': list(transform),
            'extent': [transform[0], transform[3] + SIZE * transform[5], transform[0] + SIZE * transform[1], transform[3]],
            'valid_pixels': SIZE * SIZE - (len(SPATIAL_NODATA) if spatial else 1)}


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
    result = json.loads(command([
        'docker', 'run', '--rm', '--name', CONTAINERS[2], '--label', f'com.addp.online-fixture={SUITE}',
        '--network', 'host', '--entrypoint', 'python',
        '-v', f'{Path(__file__).resolve()}:/fixture.py:ro', '-v', f'{root}:/secrets:ro',
        IMAGE, '/fixture.py', 'worker-' + action, '/secrets/raster-fixture.json',
    ]))
    if action == 'seed':
        private_json(root / 'raster-source-sha256.json', result['source_sha256'])
    return result


def worker(action, path):
    # GDAL stays in the Runtime image; the verifier never imports production operators.
    from minio import Minio
    from osgeo import gdal, osr
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    if action not in ACTIONS:
        raise FixtureError('unknown worker action')
    gdal.UseExceptions()
    config = json.loads(Path(path).read_text())
    fingerprint_path = Path(path).with_name('raster-source-sha256.json')
    clients = {role: Minio(entry['endpoint'], access_key=entry['access_key'], secret_key=entry['secret_key'], secure=False)
               for role, entry in config.items()}
    with tempfile.TemporaryDirectory(prefix='raster-physical-') as directory:
        root = Path(directory)
        if action == 'seed':
            for role, client in clients.items():
                client.make_bucket(config[role]['bucket'])
            fingerprints = {}
            for name, spatial in [('source.tif', False), ('spatial.tif', True)]:
                path = root / name
                dataset = gdal.GetDriverByName('GTiff').Create(str(path), SIZE, SIZE, 2, gdal.GDT_Float64)
                dataset.SetGeoTransform(SPATIAL_SOURCE_TRANSFORM if spatial else TRANSFORM)
                crs = osr.SpatialReference(); crs.ImportFromEPSG(4326)
                dataset.SetProjection(crs.ExportToWkt())
                for index in (1, 2):
                    values = source_values(index, spatial)
                    band = dataset.GetRasterBand(index)
                    band.SetNoDataValue(-9999)
                    band.WriteRaster(0, 0, SIZE, SIZE, struct.pack(f'<{len(values)}d', *values), buf_type=gdal.GDT_Float64)
                    band = None
                dataset = None
                clients['source'].fput_object(config['source']['bucket'], name, str(path), content_type='image/tiff')
                fingerprints[name] = hashlib.sha256(path.read_bytes()).hexdigest()
            return {'seeded': True, 'source_pixels': SIZE * SIZE, 'spatial_source_pixels': SIZE * SIZE, 'target_objects': 0, 'source_sha256': fingerprints}
        if not fingerprint_path.is_file():
            raise FixtureError('source fingerprints are missing')
        fingerprints = json.loads(fingerprint_path.read_text())
        for name, spatial in [('source.tif', False), ('spatial.tif', True)]:
            path = root / name
            clients['source'].fget_object(config['source']['bucket'], name, str(path))
            if hashlib.sha256(path.read_bytes()).hexdigest() != fingerprints.get(name):
                raise FixtureError('workflow modified source bytes')
            source = gdal.Open(str(path))
            for index in (1, 2):
                band = source.GetRasterBand(index)
                values = struct.unpack(f'<{SIZE * SIZE}d', band.ReadRaster(buf_type=gdal.GDT_Float64))
                if band.GetNoDataValue() != -9999 or values != source_values(index, spatial):
                    raise FixtureError('workflow modified its source pixels/NoData')
            reference = osr.SpatialReference(); reference.ImportFromEPSG(4326)
            if (source.GetGeoTransform() != (SPATIAL_SOURCE_TRANSFORM if spatial else TRANSFORM)
                or source.RasterCount != 2 or not osr.SpatialReference(source.GetProjection()).IsSame(reference)):
                raise FixtureError('workflow modified source georeferencing/bands')
        spatial = action.startswith('verify-mosaic-')
        overlap = action.removeprefix('verify-mosaic-') if spatial else None
        target_name = f'mosaic-{overlap}.cog.tif' if spatial else 'result.cog.tif'

        def verify(name, spatial=False, overlap=None, factor=1):
            path = root / name
            clients['target'].fget_object(config['target']['bucket'], name, str(path))
            dataset = gdal.Open(str(path))
            warnings, errors, _ = validate(dataset, full_check=True)
            if errors or dataset.GetMetadataItem('LAYOUT', 'IMAGE_STRUCTURE') != 'COG':
                raise FixtureError('persisted target is not a valid COG')
            expectation = artifact_expectation(spatial)
            reference = osr.SpatialReference(); reference.ImportFromEPSG(expectation['extent_srid'])
            if (not osr.SpatialReference(dataset.GetProjection()).IsSame(reference)
                or dataset.GetGeoTransform() != tuple(expectation['transform'])):
                raise FixtureError('persisted target changed CRS/transform')
            if (dataset.RasterXSize, dataset.RasterYSize, dataset.RasterCount) != (SIZE, SIZE, 1):
                raise FixtureError('persisted target has invalid dimensions/bands')
            band = dataset.GetRasterBand(1)
            if band.DataType != gdal.GDT_Float64 or not math.isnan(band.GetNoDataValue()):
                raise FixtureError('persisted target did not propagate NoData')
            values = struct.unpack(f'<{SIZE * SIZE}d', band.ReadRaster(buf_type=gdal.GDT_Float64))
            invalid = SPATIAL_NODATA if spatial else {0}
            # Near the equator, the projected pixel centres map strictly inside
            # the same source cells. No Warp/production math is used as an oracle.
            seam = 160 if overlap == 'first' else 96
            for position, value in enumerate(values):
                if position in invalid:
                    if not math.isnan(value):
                        raise FixtureError('persisted target did not propagate NoData')
                else:
                    scale = (3 if position % SIZE >= seam else 1) if spatial else factor
                    if value != float(scale * (position + 1)):
                        raise FixtureError('persisted target pixels do not match the expected expression/mosaic seam')
            return {'cog_valid': True, 'cog_warnings': len(warnings), 'valid_pixels': expectation['valid_pixels'],
                    'invalid_pixels': len(invalid), 'source_unchanged': True,
                    'sha256': hashlib.sha256(path.read_bytes()).hexdigest()}

        evidence = verify(target_name, spatial, overlap, 3 if action in ('verify-replace', 'verify-analysis') else 1)
        names = ['result.cog.tif']
        if spatial:
            names.append('mosaic-first.cog.tif')
            evidence['baseline_sha256'] = verify('result.cog.tif', factor=3)['sha256']
            evidence['overlap'] = overlap
            if overlap == 'last':
                names.append('mosaic-last.cog.tif')
                evidence['first_sha256'] = verify('mosaic-first.cog.tif', True, 'first')['sha256']
        else:
            evidence['factor'] = 3 if action in ('verify-replace', 'verify-analysis') else 1
        if action == 'verify-analysis':
            # Analysis must leave all already accepted artifacts byte-for-byte intact.
            names.extend(['mosaic-first.cog.tif', 'mosaic-last.cog.tif'])
            evidence['first_sha256'] = verify('mosaic-first.cog.tif', True, 'first')['sha256']
            evidence['last_sha256'] = verify('mosaic-last.cog.tif', True, 'last')['sha256']
        if sorted(item.object_name for item in clients['target'].list_objects(config['target']['bucket'], recursive=True)) != sorted(names):
            raise FixtureError('target contains unexpected or partial artifacts')
        return evidence


def main():
    action = sys.argv[1]
    if action.startswith('worker-'):
        if action.removeprefix('worker-') not in ACTIONS:
            raise FixtureError('unknown worker action')
        result = worker(action.removeprefix('worker-'), sys.argv[2])
    else:
        root = environment()
        if action == 'start':
            start(root); result = {'started': True}
        elif action == 'stop':
            stop(); result = {'containers': 0}
        elif action in ACTIONS:
            result = physical_worker(action, root)
        else:
            raise FixtureError('unknown physical fixture action')
    print(json.dumps(result, sort_keys=True))


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        # Never print native SDK/subprocess exception text: it may hold credentials.
        reason = str(error) if isinstance(error, FixtureError) else type(error).__name__
        print(f'Raster physical fixture failed: {reason}', file=sys.stderr)
        raise SystemExit(1)
