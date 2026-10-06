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
MINIO_IMAGE = 'addp-minio:RELEASE.2025-10-15T17-29-55Z'
TRANSFORM = (110, .01, 0, 20.32, 0, -.01)
SIZE = 256
ANGULAR_METRE = math.degrees(1 / 6378137)
SPATIAL_SOURCE_TRANSFORM = (0, ANGULAR_METRE, 0, SIZE * ANGULAR_METRE, 0, -ANGULAR_METRE)
SPATIAL_TRANSFORM = (0, 1, 0, SIZE, 0, -1)
SPATIAL_NODATA = {0, 127 * SIZE + 127}
GRID_CASES = ('resample-size', 'resample-resolution', 'clip-polygon')
MULTIBAND_CASES = ('multiband-alpha', 'multiband-joint', 'multiband-average', 'multiband-average-joint', 'multiband-bilinear', 'multiband-bilinear-joint', 'multiband-average-fractional', 'multiband-average-fractional-joint', 'multiband-average-finite', 'multiband-average-finite-joint')
UTILITY_CASES = ('build-overviews', 'to-cog')
UTILITY_JSON_CASES = ('info-overviews', 'validate-cog-invalid', 'validate-cog-valid')
NON_COG_SOURCE = 'non-cog.tif'
SOURCE_FILES = (('source.tif', False, -9999.), ('spatial.tif', True, -9999.),
                ('multiband.tif', False, float('nan')), ('multiband-average.tif', False, float('nan')),
                ('multiband-average-finite.tif', False, -9999.))
ACTIONS = ('seed', 'verify-create', 'verify-replace', 'verify-mosaic-first', 'verify-mosaic-last', 'verify-analysis') + tuple('verify-' + name for name in GRID_CASES + MULTIBAND_CASES + UTILITY_CASES) + ('verify-utility-queries',)
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
    """Persisted-output oracle: band_nodata=None requires declared NaN, never missing NoData."""
    transform = SPATIAL_TRANSFORM if spatial else TRANSFORM
    return {'width': SIZE, 'height': SIZE, 'band_count': 1, 'source_crs': 'EPSG:3857' if spatial else 'EPSG:4326',
            'extent_srid': 3857 if spatial else 4326, 'transform': list(transform),
            'extent': [transform[0], transform[3] + SIZE * transform[5], transform[0] + SIZE * transform[1], transform[3]],
            'valid_pixels': SIZE * SIZE - (len(SPATIAL_NODATA) if spatial else 1), 'band_nodata': None}


def utility_expectation():
    return {**artifact_expectation(), 'band_count': 2, 'band_nodata': -9999.}


def utility_info_expectation(crs):
    facts = utility_expectation()
    return {key: value for key, value in facts.items() if key not in ('valid_pixels', 'band_nodata')} | {
        'crs': crs, 'bands': [{'band': index, 'dtype': 'Float64', 'nodata': -9999.,
            'nodata_is_nan': False, 'block_size': [128, 128], 'overviews': [[128, 128], [64, 64]]}
            for index in (1, 2)]}


def clip_geometry():
    # Non-integer metre boundaries keep transformed edges away from pixel grid
    # rounding; pixel centres still have an exact integer-domain inclusion oracle.
    return {'type': 'Polygon', 'coordinates': [
        [[31.75,31.75],[224.25,31.75],[224.25,128.25],[128.25,128.25],
         [128.25,224.25],[31.75,224.25],[31.75,31.75]],
        [[63.75,63.75],[63.75,96.25],[96.25,96.25],[96.25,63.75],[63.75,63.75]],
    ]}


def grid_pixels(case_name):
    """Independent nearest-centre/point-in-L oracle, without GDAL or operators."""
    if case_name not in GRID_CASES:
        raise ValueError('unknown raster grid case')
    width, height = (192, 192) if case_name == 'clip-polygon' else (128, 128 if case_name == 'resample-size' else 64)
    for row in range(height):
        for column in range(width):
            if case_name == 'clip-polygon':
                x, y = 32 + column, 223 - row
                position = (SIZE - 1 - y) * SIZE + x
                inside = (x < 128 or y < 128) and not (64 <= x < 96 and 64 <= y < 96)
            else:
                step_y = 2 if case_name == 'resample-size' else 4
                position = (step_y * row + step_y // 2) * SIZE + 2 * column + 1
                inside = True
            yield None if not inside or position in SPATIAL_NODATA else float(position + 1)


def grid_expectation(case_name):
    polygon = case_name == 'clip-polygon'
    width, height = (192, 192) if polygon else (128, 128 if case_name == 'resample-size' else 64)
    step_x, step_y = (1, 1) if polygon else (2, 2 if case_name == 'resample-size' else 4)
    transform = [32 * ANGULAR_METRE if polygon else 0, step_x * ANGULAR_METRE, 0,
                 (224 if polygon else SIZE) * ANGULAR_METRE, 0, -step_y * ANGULAR_METRE]
    return {'width': width, 'height': height, 'band_count': 2 if polygon else 1,
            'source_crs': 'EPSG:4326', 'extent_srid': 4326, 'transform': transform,
            'extent': [transform[0], transform[3] + height * transform[5],
                       transform[0] + width * transform[1], transform[3]],
            'valid_pixels': sum(value is not None for value in grid_pixels(case_name)), 'band_nodata': None}


def multiband_pixels(band, *, source=False, joint=False):
    """Uniform 2x2 source cells; independent nearest-centre and joint-mask oracle."""
    width = SIZE if source else SIZE // 2
    for row in range(width):
        for column in range(width):
            cell = (row // 2 if source else row) * (SIZE // 2) + (column // 2 if source else column)
            if band == 3:
                yield 0. if cell == 2 else 128. if cell in (0, 1, 3) else 255.
            elif joint:
                yield None if cell in (0, 1, 2) else 0. if cell == 4 else float(3 * (cell + 1))
            elif cell == band - 1 or (cell == 2 and not source):
                yield None
            else:
                yield 1e6 if cell == 2 else 0. if cell == 4 else float(band * (cell + 1))


def average_source_pixel(band, row, column):
    cell, sample = (row // 2) * (SIZE // 2) + column // 2, (row % 2) * 2 + column % 2
    if band == 3:
        return 0. if cell == 2 else 128. if cell in (0, 1, 3, 6) else 255.
    if (cell == band - 1 or (cell == 5 and sample == (0 if band == 1 else 3))
        or (cell == 6 and sample == (1 if band == 1 else 2))):
        return None
    return 1e6 if cell == 2 else 0. if cell == 4 else float(band * (cell * 16 + sample * 4))


def area_average(band, row, column, width=SIZE // 2):
    """Exact integer-domain overlap weights; the common area denominator cancels."""
    left, top, right, bottom = column * SIZE, row * SIZE, (column + 1) * SIZE, (row + 1) * SIZE
    contributions = []
    for y in range(top // width, (bottom + width - 1) // width):
        for x in range(left // width, (right + width - 1) // width):
            value = average_source_pixel(band, y, x)
            area = max(0, min((x + 1) * width, right) - max(x * width, left)) * max(0, min((y + 1) * width, bottom) - max(y * width, top))
            if value is not None and average_source_pixel(3, y, x) > 0 and area > 0:
                contributions.append((value, area))
    return (math.fsum(value * area for value, area in contributions) / math.fsum(area for _, area in contributions)
            if contributions else None)


def average_pixels(band, *, source=False, joint=False, width=SIZE // 2):
    width = SIZE if source else width
    for row in range(width):
        for column in range(width):
            if source:
                yield average_source_pixel(band, row, column)
            elif band == 3 or joint:
                first, second = area_average(1, row, column, width), area_average(2, row, column, width)
                if band == 3:
                    # GDAL average generates coverage; source opacity is not copied.
                    yield 255. if first is not None or second is not None else 0.
                else:
                    yield first + second if first is not None and second is not None else None
            else:
                yield area_average(band, row, column, width)


def bilinear_value(band, row, column):
    """Independent four-centre distance weights, excluding this band's invalid neighbours."""
    center_x, center_y = (column + .5) / 2, (row + .5) / 2
    if (average_source_pixel(band, int(center_y), int(center_x)) is None
        or average_source_pixel(3, int(center_y), int(center_x)) <= 0):
        return None
    left, top = math.floor(center_x - .5), math.floor(center_y - .5)
    contributions = []
    for y in range(max(0, top), min(SIZE, top + 2)):
        for x in range(max(0, left), min(SIZE, left + 2)):
            value = average_source_pixel(band, y, x)
            weight = max(0, 1 - abs(x + .5 - center_x)) * max(0, 1 - abs(y + .5 - center_y))
            if value is not None and average_source_pixel(3, y, x) > 0 and weight > 0:
                contributions.append((value, weight))
    return math.fsum(value * weight for value, weight in contributions) / math.fsum(weight for _, weight in contributions)


def bilinear_pixels(band, *, joint=False):
    for row in range(SIZE * 2):
        for column in range(SIZE * 2):
            if band == 3:
                # This Warp kernel generates alpha from the covered containing source cell.
                yield average_source_pixel(3, row // 2, column // 2)
            elif joint:
                first, second = bilinear_value(1, row, column), bilinear_value(2, row, column)
                yield first + second if first is not None and second is not None else None
            else:
                yield bilinear_value(band, row, column)


def multiband_source_name(case_name):
    if case_name not in MULTIBAND_CASES:
        raise ValueError('unknown raster multiband case')
    joint = case_name.endswith('-joint')
    prefix = case_name.removesuffix('-joint') if joint else (
        'multiband-average' if 'average' in case_name or 'bilinear' in case_name else 'multiband')
    if case_name == 'multiband-joint':
        prefix = 'multiband-alpha'
    if 'finite' in case_name and not joint:
        prefix = 'multiband-average-finite'
    return prefix + ('.cog.tif' if joint else '.tif')


def multiband_expectation(case_name):
    if case_name not in MULTIBAND_CASES:
        raise ValueError('unknown raster multiband case')
    result = artifact_expectation()
    bilinear, joint = 'bilinear' in case_name, case_name.endswith('-joint')
    width = 171 if 'fractional' in case_name or 'finite' in case_name else 512 if bilinear else 128
    resolution = SIZE * TRANSFORM[1] / width
    invalid = (64 if joint else 40) if bilinear else (3 if joint else 2)
    result.update(width=width, height=width, band_count=1 if joint else 3,
                  transform=[110, resolution, 0, 20.32, 0, -resolution],
                  valid_pixels=width * width - invalid,
                  band_nodata=-9999. if 'finite' in case_name and not joint else None)
    return result


def computed_expectation(case_name):
    return multiband_expectation(case_name) if case_name in MULTIBAND_CASES else grid_expectation(case_name)


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
    image = os.environ.get('ADDP_ONLINE_RASTER_RUNTIME_IMAGE', '').strip()
    if not image:
        raise FixtureError('product Runtime image is required for raster physical verification')
    result = json.loads(command([
        'docker', 'run', '--rm', '--name', CONTAINERS[2], '--label', f'com.addp.online-fixture={SUITE}',
        '--network', 'host', '--entrypoint', 'python',
        '-v', f'{Path(__file__).resolve()}:/fixture.py:ro', '-v', f'{root}:/secrets:ro',
        image, '/fixture.py', 'worker-' + action, '/secrets/raster-fixture.json',
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
            for name, spatial, source_nodata in SOURCE_FILES:
                path = root / name
                multiband = name.startswith('multiband')
                pixel_formula = average_pixels if name.startswith('multiband-average') else multiband_pixels
                band_count = 3 if multiband else 2
                dataset = gdal.GetDriverByName('GTiff').Create(str(path), SIZE, SIZE, band_count, gdal.GDT_Float64)
                dataset.SetGeoTransform(SPATIAL_SOURCE_TRANSFORM if spatial else TRANSFORM)
                crs = osr.SpatialReference(); crs.ImportFromEPSG(4326)
                dataset.SetProjection(crs.ExportToWkt())
                for index in range(1, band_count + 1):
                    values = tuple(source_nodata if value is None else value for value in pixel_formula(index, source=True)) if multiband else source_values(index, spatial)
                    band = dataset.GetRasterBand(index)
                    band.SetNoDataValue(source_nodata)
                    if multiband and index == 3: band.SetColorInterpretation(gdal.GCI_AlphaBand)
                    band.WriteRaster(0, 0, SIZE, SIZE, struct.pack(f'<{len(values)}d', *values), buf_type=gdal.GDT_Float64)
                    band = None
                dataset = None
                clients['source'].fput_object(config['source']['bucket'], name, str(path), content_type='image/tiff')
                fingerprints[name] = hashlib.sha256(path.read_bytes()).hexdigest()
            # A 256px striped TIFF can satisfy the GDAL layout validator. This
            # larger strip has a width > 1024 and is definitively non-tiled.
            path = root / NON_COG_SOURCE
            dataset = gdal.GetDriverByName('GTiff').Create(str(path), 1025, 1025, 1, gdal.GDT_Byte)
            dataset.SetGeoTransform(TRANSFORM)
            dataset.SetProjection(crs.ExportToWkt())
            dataset.GetRasterBand(1).Fill(7)
            dataset = None
            clients['source'].fput_object(config['source']['bucket'], NON_COG_SOURCE, str(path), content_type='image/tiff')
            fingerprints[NON_COG_SOURCE] = hashlib.sha256(path.read_bytes()).hexdigest()
            return {'seeded': True, 'source_pixels': SIZE * SIZE, 'spatial_source_pixels': SIZE * SIZE, 'target_objects': 0, 'source_sha256': fingerprints}
        if not fingerprint_path.is_file():
            raise FixtureError('source fingerprints are missing')
        fingerprints = json.loads(fingerprint_path.read_text())
        for name, spatial, source_nodata in SOURCE_FILES:
            path = root / name
            clients['source'].fget_object(config['source']['bucket'], name, str(path))
            if hashlib.sha256(path.read_bytes()).hexdigest() != fingerprints.get(name):
                raise FixtureError('workflow modified source bytes')
            source = gdal.Open(str(path))
            multiband = name.startswith('multiband')
            pixel_formula = average_pixels if name.startswith('multiband-average') else multiband_pixels
            for index in range(1, 4 if multiband else 3):
                band = source.GetRasterBand(index)
                values = struct.unpack(f'<{SIZE * SIZE}d', band.ReadRaster(buf_type=gdal.GDT_Float64))
                expected_values = tuple(source_nodata if value is None else value for value in pixel_formula(index, source=True)) if multiband else source_values(index, spatial)
                nodata = band.GetNoDataValue()
                if (nodata is None or (not math.isnan(nodata) if math.isnan(source_nodata) else nodata != source_nodata)
                    or any(not math.isnan(actual) if math.isnan(expected) else actual != expected
                           for actual, expected in zip(values, expected_values))
                    or (multiband and index == 3 and band.GetColorInterpretation() != gdal.GCI_AlphaBand)):
                    raise FixtureError('workflow modified its source pixels/NoData')
            reference = osr.SpatialReference(); reference.ImportFromEPSG(4326)
            if (source.GetGeoTransform() != (SPATIAL_SOURCE_TRANSFORM if spatial else TRANSFORM)
                or source.RasterCount != (3 if multiband else 2) or not osr.SpatialReference(source.GetProjection()).IsSame(reference)):
                raise FixtureError('workflow modified source georeferencing/bands')
        spatial = action.startswith('verify-mosaic-')
        overlap = action.removeprefix('verify-mosaic-') if spatial else None
        target_name = f'mosaic-{overlap}.cog.tif' if spatial else 'result.cog.tif'

        def verify(name, spatial=False, overlap=None, factor=1, grid_case=None, utility_case=None):
            path = root / name
            clients['target'].fget_object(config['target']['bucket'], name, str(path))
            dataset = gdal.Open(str(path))
            warnings, errors, _ = validate(dataset, full_check=True)
            if errors or dataset.GetMetadataItem('LAYOUT', 'IMAGE_STRUCTURE') != 'COG':
                raise FixtureError('persisted target is not a valid COG')
            expectation = utility_expectation() if utility_case else computed_expectation(grid_case) if grid_case else artifact_expectation(spatial)
            reference = osr.SpatialReference(); reference.ImportFromEPSG(expectation['extent_srid'])
            if (not osr.SpatialReference(dataset.GetProjection()).IsSame(reference)
                or any(not math.isclose(actual, expected, rel_tol=0, abs_tol=1e-12) for actual, expected in
                       zip(dataset.GetGeoTransform(), expectation['transform']))):
                raise FixtureError('persisted target changed CRS/transform')
            if (dataset.RasterXSize, dataset.RasterYSize, dataset.RasterCount) != (expectation['width'], expectation['height'], expectation['band_count']):
                raise FixtureError('persisted target has invalid dimensions/bands')
            band = dataset.GetRasterBand(1)
            expected_nodata = expectation['band_nodata']
            if (band.DataType != gdal.GDT_Float64 or band.GetNoDataValue() is None
                or (not math.isnan(band.GetNoDataValue()) if expected_nodata is None else band.GetNoDataValue() != expected_nodata)):
                raise FixtureError('persisted target did not propagate NoData')
            count = expectation['width'] * expectation['height']
            values = struct.unpack(f'<{count}d', band.ReadRaster(buf_type=gdal.GDT_Float64))
            if utility_case:
                levels = (2, 4) if utility_case == 'build-overviews' else (2,)
                for index in (1, 2):
                    current = dataset.GetRasterBand(index)
                    if current.DataType != gdal.GDT_Float64 or current.GetNoDataValue() != -9999.:
                        raise FixtureError('utility target lost band dtype/finite NoData')
                    actual = struct.unpack(f'<{count}d', current.ReadRaster(buf_type=gdal.GDT_Float64))
                    if actual != source_values(index):
                        raise FixtureError('utility target changed source band pixels')
                    mask = current.GetMaskBand().ReadRaster(buf_type=gdal.GDT_Byte)
                    if any(value != (0 if position == 0 else 255) for position, value in enumerate(mask)):
                        raise FixtureError('utility target changed source validity')
                    if current.GetOverviewCount() != len(levels):
                        raise FixtureError('utility target lost explicit overview levels')
                    for level_index, level in enumerate(levels):
                        overview = current.GetOverview(level_index)
                        if (overview.XSize, overview.YSize) != (SIZE // level, SIZE // level):
                            raise FixtureError('utility overview dimensions differ from the explicit grid')
                        expected = tuple(-9999. if row == column == 0 else float(index * (row * SIZE + column + 1))
                            for row in range(0, SIZE, level) for column in range(0, SIZE, level))
                        actual = struct.unpack(f'<{len(expected)}d', overview.ReadRaster(buf_type=gdal.GDT_Float64))
                        if actual != expected:
                            raise FixtureError('utility overview pixels differ from independent nearest sampling')
                        mask = overview.GetMaskBand().ReadRaster(buf_type=gdal.GDT_Byte)
                        if any(value != (0 if position == 0 else 255) for position, value in enumerate(mask)):
                            raise FixtureError('utility overview lost NoData validity')
                return {'cog_valid': True, 'cog_warnings': len(warnings), 'has_overviews': True,
                    'valid_pixels': SIZE * SIZE - 1, 'band_valid_pixels': [SIZE * SIZE - 1] * 2,
                    'source_unchanged': True, 'size_bytes': path.stat().st_size,
                    'overview_sizes': [[SIZE // level] * 2 for level in levels], 'crs': dataset.GetProjection(),
                    'sha256': hashlib.sha256(path.read_bytes()).hexdigest()}
            if grid_case in MULTIBAND_CASES:
                joint = grid_case.endswith('-joint')
                average = 'average' in grid_case
                bilinear = 'bilinear' in grid_case
                pixel_formula = bilinear_pixels if bilinear else average_pixels if average else multiband_pixels
                valid_counts = []
                partial_alpha_pixels = 0
                for index in range(1, dataset.RasterCount + 1):
                    current = dataset.GetRasterBand(index)
                    nodata = current.GetNoDataValue()
                    if (current.DataType != gdal.GDT_Float64 or nodata is None
                        or (not math.isnan(nodata) if expected_nodata is None else nodata != expected_nodata)):
                        raise FixtureError('multiband target lost band dtype/NoData')
                    actual_values = struct.unpack(f'<{count}d', current.ReadRaster(buf_type=gdal.GDT_Float64))
                    expected_values = list(pixel_formula(index, joint=joint, **({'width': expectation['width']} if average else {})))
                    if any((not math.isnan(actual) if expected_nodata is None else actual != expected_nodata) if expected is None else
                           not math.isclose(actual, expected, rel_tol=1e-10, abs_tol=1e-8) if (average or bilinear) and index != 3 else actual != expected
                           for actual, expected in zip(actual_values, expected_values)):
                        raise FixtureError('multiband target pixels/NoData/alpha differ from independent oracle')
                    if index == 3:
                        if current.GetColorInterpretation() != gdal.GCI_AlphaBand:
                            raise FixtureError('multiband target lost alpha interpretation')
                        partial_alpha_pixels = sum(value == 128 for value in actual_values)
                    else:
                        valid_counts.append(sum(value is not None for value in expected_values))
                return {'cog_valid': True, 'cog_warnings': len(warnings), 'has_overviews': band.GetOverviewCount()>0,
                        'valid_pixels': valid_counts[0], 'invalid_pixels': count - valid_counts[0],
                        'band_valid_pixels': valid_counts, 'partial_alpha_pixels': partial_alpha_pixels,
                        'band_nodata': None if math.isnan(band.GetNoDataValue()) else band.GetNoDataValue(),
                        'band_nodata_is_nan': math.isnan(band.GetNoDataValue()),
                        'source_unchanged': True, 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()}
            if grid_case:
                expected_values = list(grid_pixels(grid_case))
                for actual, expected in zip(values, expected_values):
                    if (expected is None and not math.isnan(actual)) or (expected is not None and actual != expected):
                        raise FixtureError('persisted grid target pixels/NoData differ from the independent oracle')
                if grid_case == 'clip-polygon':
                    alpha = dataset.GetRasterBand(2)
                    if alpha.DataType != gdal.GDT_Float64 or alpha.GetColorInterpretation() != gdal.GCI_AlphaBand:
                        raise FixtureError('polygon target lost its alpha band')
                    alpha_values = struct.unpack(f'<{count}d', alpha.ReadRaster(buf_type=gdal.GDT_Float64))
                    if any(actual != (0 if expected is None else 255) for actual, expected in zip(alpha_values, expected_values)):
                        raise FixtureError('polygon alpha differs from boundary/hole/source NoData')
                return {'cog_valid': True, 'cog_warnings': len(warnings), 'has_overviews': band.GetOverviewCount()>0,
                        'valid_pixels': expectation['valid_pixels'],
                        'invalid_pixels': count - expectation['valid_pixels'], 'source_unchanged': True,
                        'sha256': hashlib.sha256(path.read_bytes()).hexdigest()}
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
            return {'cog_valid': True, 'cog_warnings': len(warnings), 'has_overviews': band.GetOverviewCount()>0,
                    'valid_pixels': expectation['valid_pixels'],
                    'invalid_pixels': len(invalid), 'source_unchanged': True,
                    'sha256': hashlib.sha256(path.read_bytes()).hexdigest()}

        grid_case = action.removeprefix('verify-')
        if grid_case in UTILITY_CASES or action == 'verify-utility-queries':
            path = root / NON_COG_SOURCE
            clients['source'].fget_object(config['source']['bucket'], NON_COG_SOURCE, str(path))
            non_cog = gdal.Open(str(path))
            _, errors, _ = validate(non_cog, full_check=True)
            if (hashlib.sha256(path.read_bytes()).hexdigest() != fingerprints.get(NON_COG_SOURCE)
                or not errors or non_cog.GetRasterBand(1).ReadRaster() != bytes([7]) * (1025 * 1025)):
                raise FixtureError('negative COG source changed or no longer has an invalid layout')
            preserved = {'result.cog.tif': verify('result.cog.tif', factor=3)['sha256'],
                'mosaic-first.cog.tif': verify('mosaic-first.cog.tif', True, 'first')['sha256'],
                'mosaic-last.cog.tif': verify('mosaic-last.cog.tif', True, 'last')['sha256']}
            for prior in GRID_CASES + MULTIBAND_CASES:
                preserved[prior + '.cog.tif'] = verify(prior + '.cog.tif', grid_case=prior)['sha256']
            accepted = UTILITY_CASES if action == 'verify-utility-queries' else UTILITY_CASES[:UTILITY_CASES.index(grid_case)]
            for prior in accepted:
                preserved[prior + '.cog.tif'] = verify(prior + '.cog.tif', utility_case=prior)['sha256']
            evidence = verify('build-overviews.cog.tif' if action == 'verify-utility-queries' else grid_case + '.cog.tif',
                utility_case='build-overviews' if action == 'verify-utility-queries' else grid_case)
            evidence.update(case_name=grid_case, preserved_sha256=preserved)
            names = set(preserved) | ({grid_case + '.cog.tif'} if grid_case in UTILITY_CASES else set())
            if set(item.object_name for item in clients['target'].list_objects(config['target']['bucket'], recursive=True)) != names:
                raise FixtureError('utility target contains unexpected or partial artifacts')
            return evidence
        if grid_case in GRID_CASES + MULTIBAND_CASES:
            evidence = verify(grid_case + '.cog.tif', grid_case=grid_case)
            preserved = {'result.cog.tif': verify('result.cog.tif', factor=3)['sha256'],
                         'mosaic-first.cog.tif': verify('mosaic-first.cog.tif', True, 'first')['sha256'],
                         'mosaic-last.cog.tif': verify('mosaic-last.cog.tif', True, 'last')['sha256']}
            cases = GRID_CASES + MULTIBAND_CASES
            for prior in cases[:cases.index(grid_case)]:
                preserved[prior + '.cog.tif'] = verify(prior + '.cog.tif', grid_case=prior)['sha256']
            evidence.update(case_name=grid_case, preserved_sha256=preserved)
            names = list(preserved) + [grid_case + '.cog.tif']
            if sorted(item.object_name for item in clients['target'].list_objects(config['target']['bucket'], recursive=True)) != sorted(names):
                raise FixtureError('target contains unexpected or partial artifacts')
            return evidence

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
