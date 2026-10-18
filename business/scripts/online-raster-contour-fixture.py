#!/usr/bin/env python3
"""Owned NFS export and independent GDAL oracle for contour persistence T4."""
from __future__ import annotations
import hashlib
import json
import math
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

SUITE = 'raster-contour-persistence'
RUNTIME = 'addp-raster-contour-runtime'
TARGETS = {'levels.gpkg': [200., 350., 500.], 'interval.gpkg': [200., 300., 400., 500., 600., 700.]}


class FixtureError(RuntimeError):
    pass


def command(args, **kwargs):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=120, **kwargs).stdout.strip()


def guard(env=os.environ):
    if any(env.get(k) != v for k, v in {'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux',
            'ADDP_ONLINE_HOST': '1', 'ADDP_ONLINE_HOSTED': '1'}.items()):
        raise FixtureError('requires an exclusive GitHub Hosted Linux Runner')
    if command(['uname', '-sm']) != 'Linux x86_64':
        raise FixtureError('requires Linux x86_64')
    if not re.fullmatch(r'contour-[0-9]+-[0-9]+', env.get('ADDP_ONLINE_TEST_RUN_ID', '')):
        raise FixtureError('invalid contour run identity')
    secret = Path(env['ADDP_ONLINE_SECRET_DIR'])
    if not secret.is_absolute() or secret.resolve() != secret or not secret.is_dir() or secret.stat().st_uid != os.getuid() or secret.stat().st_mode & 0o077:
        raise FixtureError('requires an owned private secret directory')
    return secret


def owned_root(secret):
    state = secret / 'contour-root'
    if state.is_symlink() or not state.is_file():
        raise FixtureError('missing contour root state')
    root = Path(state.read_text().strip())
    if (root.parent != Path('/tmp') or not re.fullmatch(r'addp-contour-[A-Za-z0-9_]+', root.name)
        or root.resolve() != root or not root.is_dir() or root.stat().st_uid != os.getuid()
        or (root / '.addp-online-owner').is_symlink()
        or (root / '.addp-online-owner').read_text() != os.environ['ADDP_ONLINE_TEST_RUN_ID']):
        raise FixtureError('contour root ownership mismatch')
    return root


def digest(path):
    if path.is_symlink() or not path.is_file():
        raise FixtureError('expected an ordinary owned file')
    size = path.stat().st_size
    if size <= 0:
        raise FixtureError('file is empty')
    return {'sha256': hashlib.sha256(path.read_bytes()).hexdigest(), 'size_bytes': size}


def seed(root):
    from osgeo import gdal, osr
    import numpy as np
    path = root / 'dem.tif'
    if path.exists() or list((root / 'outputs').iterdir()):
        raise FixtureError('refusing to overwrite existing fixture data')
    rows, cols = np.indices((48, 64))
    values = 100 + 4 * (cols + .5) + 9 * (rows + .5)
    raster = gdal.GetDriverByName('GTiff').Create(str(path), 64, 48, 1, gdal.GDT_Float64)
    crs = osr.SpatialReference(); crs.ImportFromEPSG(3857)
    raster.SetProjection(crs.ExportToWkt()); raster.SetGeoTransform((1000, 2, 0, 2000, 0, -3))
    raster.GetRasterBand(1).WriteArray(values); raster = None
    return {'source': digest(path), 'crs': 'EPSG:3857', 'width': 64, 'height': 48}


def verify(root, names):
    from osgeo import gdal, ogr, osr
    if not names or len(set(names)) != len(names) or any(name not in TARGETS for name in names):
        raise FixtureError('invalid exact contour target inventory')
    if sorted(p.name for p in (root / 'outputs').iterdir()) != sorted(names):
        raise FixtureError('unexpected output, sidecar or temporary file')
    raster = gdal.Open(str(root / 'dem.tif'))
    if (raster.RasterXSize, raster.RasterYSize, raster.RasterCount) != (64, 48, 1):
        raise FixtureError('source grid changed')
    result = {'source': digest(root / 'dem.tif'), 'targets': {}}
    expected_crs = osr.SpatialReference(); expected_crs.ImportFromEPSG(3857)
    for name in names:
        path = root / 'outputs' / name
        file = digest(path)
        data = ogr.Open(str(path), 0)
        if data is None or data.GetDriver().GetName() != 'GPKG' or data.GetLayerCount() != 1:
            raise FixtureError('expected exactly one GeoPackage vector layer')
        layer = data.GetLayer(0)
        if not layer.GetSpatialRef() or not layer.GetSpatialRef().IsSame(expected_crs):
            raise FixtureError('contour source projection was not preserved')
        schema = layer.GetLayerDefn()
        if schema.GetFieldCount() != 1 or schema.GetFieldDefn(0).GetName() != 'elevation' or schema.GetFieldDefn(0).GetType() != ogr.OFTReal:
            raise FixtureError('expected only Float64 elevation in metres')
        levels, points, count = set(), 0, 0
        for feature in layer:
            elevation = feature.GetField('elevation')
            geometry = feature.GetGeometryRef()
            if elevation not in TARGETS[name] or geometry is None or geometry.GetGeometryName() != 'LINESTRING' or geometry.GetPointCount() < 2:
                raise FixtureError('invalid contour geometry or level')
            for i in range(geometry.GetPointCount()):
                x, y, _ = geometry.GetPoint(i)
                # GDAL extends contours to half-cell outer edges; evaluate the
                # independent plane on the source sample-centre domain.
                z = 100 + 2 * (min(1127., max(1001., x)) - 1000) + 3 * (2000 - min(1998.5, max(1857.5, y)))
                if not (math.isfinite(x) and math.isfinite(y) and 1000 <= x <= 1128 and 1856 <= y <= 2000 and math.isclose(z, elevation, rel_tol=0, abs_tol=2e-5)):
                    raise FixtureError('contour vertex differs from the independent elevation plane')
                points += 1
            levels.add(elevation); count += 1
        if levels != set(TARGETS[name]) or count != len(TARGETS[name]):
            raise FixtureError('missing, duplicated or unexpected contour lines')
        result['targets'][name] = {**file, 'layer_name': layer.GetName(), 'feature_count': count,
                                   'vertex_count': points, 'levels_metres': sorted(levels), 'crs': 'EPSG:3857'}
        data = None
    return result


def worker(action, root, names):
    if action not in {'seed', 'verify'}:
        raise FixtureError('unknown worker action')
    if action == 'verify' and list(Path(tempfile.gettempdir()).glob('addp-raster-*')):
        raise FixtureError('Runtime raster execution workspace has residuals')
    if action == 'seed':
        return seed(root)
    return {**verify(root, names), 'runtime_workspaces_removed': True}


def runtime_worker(action, root, names=()):
    info = json.loads(command(['docker', 'inspect', RUNTIME]))[0]
    labels = info['Config']['Labels']
    if labels.get('com.addp.online-runtime') != SUITE or labels.get('com.addp.online-run-id') != os.environ['ADDP_ONLINE_TEST_RUN_ID'] or info['Config']['Cmd'] != ['sh', 'container_entrypoint.sh']:
        raise FixtureError('foreign Runtime or non-product entry')
    payload = command(['docker', 'exec', '-i', RUNTIME, 'python', '-', '--worker', action, str(root), *names], input=Path(__file__).read_text())
    return json.loads(payload)


def main(action):
    secret = guard()
    state = secret / 'contour-root'
    export = Path('/etc/exports.d') / f'addp-{os.environ["ADDP_ONLINE_TEST_RUN_ID"]}.exports'
    descriptor = secret / 'contour-nfs-engine.json'
    if action == 'start':
        if state.exists() or state.is_symlink() or export.exists() or export.is_symlink() or command(['sudo', 'exportfs', '-s']):
            raise FixtureError('refusing existing state or NFS exports')
        root = Path(tempfile.mkdtemp(prefix='addp-contour-', dir='/tmp'))
        state.write_text(str(root)); state.chmod(0o600)
        (root / '.addp-online-owner').write_text(os.environ['ADDP_ONLINE_TEST_RUN_ID'])
        root.chmod(0o755); (root / 'outputs').mkdir(mode=0o755)
        command(['sudo', 'mkdir', '-p', str(export.parent)])
        command(['sudo', 'tee', str(export)], input=f'{root} 127.0.0.1(rw,sync,no_subtree_check,insecure,root_squash)\n')
        command(['sudo', 'systemctl', 'start', 'rpcbind.service', 'nfs-kernel-server.service'])
        command(['sudo', 'exportfs', '-ra'])
        if str(root) not in [line.split()[0] for line in command(['showmount', '-e', '127.0.0.1']).splitlines()[1:]]:
            raise FixtureError('NFS export not discoverable')
        with descriptor.open('x') as stream:
            descriptor.chmod(0o600)
            json.dump({'name': 'Hosted Contour NFS', 'engine_type': 'nfs', 'engine_origin': 'general',
                       'connection_info': {'server': '127.0.0.1', 'export_path': str(root)}}, stream)
        return {'started': True}
    if action == 'stop':
        if state.is_symlink() or export.is_symlink():
            raise FixtureError('refusing symlink fixture state or export')
        if not state.exists():
            if export.exists(): raise FixtureError('NFS export without ownership state')
            return {'cleanup': 'not_started'}
        root = owned_root(secret)
        command(['sudo', 'rm', '-f', str(export)]); command(['sudo', 'exportfs', '-ra'])
        if command(['sudo', 'exportfs', '-s']): raise FixtureError('unexpected remaining NFS exports')
        command(['sudo', 'systemctl', 'stop', 'nfs-kernel-server.service', 'rpcbind.service', 'rpcbind.socket'])
        # Product container writes as root. Delete only this positively identified
        # fixture root after removing its NFS export, including partial results.
        command(['sudo', 'rm', '-rf', '--', str(root)])
        state.unlink(); descriptor.unlink(missing_ok=True)
        if root.exists() or export.exists(): raise FixtureError('NFS cleanup has residuals')
        return {'cleanup': 'passed', 'nfs_export_removed': True, 'owned_root_removed': True}
    root = owned_root(secret)
    if action == 'seed':
        result = runtime_worker('seed', root)
        (secret / 'contour-source.json').write_text(json.dumps(result))
        return result
    if action == 'verify':
        result = runtime_worker('verify', root, sys.argv[2:])
        if result['source'] != json.loads((secret / 'contour-source.json').read_text())['source']:
            raise FixtureError('source file changed')
        return result
    raise FixtureError('unknown fixture action')


if __name__ == '__main__':
    try:
        if sys.argv[1:2] == ['--worker']:
            result = worker(sys.argv[2], Path(sys.argv[3]), sys.argv[4:])
        else:
            result = main(sys.argv[1])
        print(json.dumps(result, sort_keys=True))
    except Exception as error:
        print(f'Contour fixture failed: {type(error).__name__}: {error}', file=sys.stderr)
        raise SystemExit(1)
