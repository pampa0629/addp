#!/usr/bin/env python3
"""Owned ext4 pressure and an independent small-raster oracle for Hosted T4."""
from __future__ import annotations
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile

SUITE = 'raster-disk-pressure'
PREFIX = 'disk-pressure'
RUNTIME = 'addp-raster-disk-pressure-runtime'
SOURCE = 'classes.tif'
WIDTH = HEIGHT = 1024
TRANSFORM = (100., 2.5, 0., 200., 0., -2.5)
GIB = 1024**3
SAFETY = 2 * GIB
IMAGE_BYTES = 4 * GIB
PRESSURE_FREE = {'distance': SAFETY + 40*1024**2, 'encoding': SAFETY + 128*1024**2}
TARGETS = tuple(f'{op}-{stage}.cog.tif' for op in ('nibble', 'proximity') for stage in ('baseline', 'recovered'))


class FixtureError(RuntimeError):
    pass


def command(args, **kwargs):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=120, **kwargs).stdout.strip()


if sys.argv[1:2] != ['--worker']:
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport, run_runtime_worker


def guard(env=os.environ):
    return NFSExport(PREFIX, command).guard(env)


def state_path(secret):
    return secret / 'disk-pressure-storage.json'


def storage(secret, *, mounted=True):
    path = state_path(secret)
    if path.is_symlink() or not path.is_file() or path.stat().st_mode & 0o077:
        raise FixtureError('invalid private storage state')
    state = json.loads(path.read_text())
    root = Path(state['root'])
    runner = Path(os.environ['RUNNER_TEMP'])
    if (root.parent != runner or root.resolve() != root or not root.is_dir()
        or root.stat().st_uid != os.getuid() or not re.fullmatch(r'addp-disk-pressure-storage-[A-Za-z0-9_]+', root.name)
        or (root/'.addp-online-owner').is_symlink()
        or (root/'.addp-online-owner').read_text() != os.environ['ADDP_ONLINE_TEST_RUN_ID']):
        raise FixtureError('foreign storage root')
    image, mount = root/'disk.img', root/'mount'
    if (image.is_symlink() or mount.is_symlink()
        or (image.exists() and (not image.is_file() or image.stat().st_uid != os.getuid()))
        or (state.get('device') and not image.is_file())):
        raise FixtureError('foreign storage image or mount')
    if mounted:
        if image.stat().st_size != IMAGE_BYTES or image.stat().st_blocks*512 < IMAGE_BYTES:
            raise FixtureError('pressure image is not the bounded allocated file')
        device = state.get('device', '')
        if not re.fullmatch(r'/dev/loop[0-9]+', device):
            raise FixtureError('invalid owned loop device')
        loops = json.loads(command(['sudo', 'losetup', '--json', '--list', '--output', 'NAME,BACK-FILE']))['loopdevices']
        owned = [v for v in loops if v['name'] == device]
        if len(owned) != 1 or owned[0]['back-file'] != str(image):
            raise FixtureError('loop backing file ownership differs')
        mounts = json.loads(command(['findmnt', '--json', '--mountpoint', str(mount), '--output', 'SOURCE,FSTYPE,TARGET']))['filesystems']
        if mounts != [{'source': device, 'fstype': 'ext4', 'target': str(mount)}]:
            raise FixtureError('not the owned ext4 mount')
        if mount.stat().st_dev == root.stat().st_dev:
            raise FixtureError('pressure mount is not a separate filesystem')
        if not 3*GIB < shutil.disk_usage(mount).total <= IMAGE_BYTES:
            raise FixtureError('pressure filesystem capacity differs from bounded image')
        if (mount/'.addp-online-owner').read_text() != os.environ['ADDP_ONLINE_TEST_RUN_ID']:
            raise FixtureError('mounted filesystem ownership differs')
    return state, root, mount


def start_storage(secret):
    path = state_path(secret)
    if path.exists() or path.is_symlink():
        raise FixtureError('refusing existing storage state')
    runner = Path(os.environ['RUNNER_TEMP'])
    if runner.resolve() != runner or not runner.is_dir() or shutil.disk_usage(runner).free < IMAGE_BYTES + 4*GIB:
        raise FixtureError('insufficient host headroom for bounded image')
    root = Path(tempfile.mkdtemp(prefix='addp-disk-pressure-storage-', dir=runner))
    (root/'.addp-online-owner').write_text(os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    state = {'root': str(root), 'device': None}
    path.write_text(json.dumps(state)); path.chmod(0o600)
    image = root/'disk.img'
    with image.open('xb') as f:
        os.posix_fallocate(f.fileno(), 0, IMAGE_BYTES)
    image.chmod(0o600)
    (root/'mount').mkdir()
    device = command(['sudo', 'losetup', '--find', '--show', str(image)])
    state['device'] = device; path.write_text(json.dumps(state))
    if not re.fullmatch(r'/dev/loop[0-9]+', device):
        raise FixtureError('unexpected allocated loop name')
    # mkfs's default discard could punch holes in the already allocated image.
    command(['sudo', 'mkfs.ext4', '-F', '-m', '0', '-E', 'nodiscard', '-L', 'addp-pressure', device])
    command(['sudo', 'mount', '-t', 'ext4', '-o', 'nodev,nosuid,noexec', device, str(root/'mount')])
    command(['sudo', 'chown', f'{os.getuid()}:{os.getgid()}', str(root/'mount')])
    (root/'mount'/'.addp-online-owner').write_text(os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    (root/'mount'/'execution').mkdir()
    storage(secret)
    (secret/'disk-pressure-tmp-root').write_text(str(root/'mount'/'execution'))
    return {'filesystem': 'ext4', 'image_bytes': IMAGE_BYTES, 'separate_filesystem': True}


def stop_storage(secret):
    path = state_path(secret)
    if not path.exists():
        return {'storage_cleanup': 'not_started'}
    state, root, mount = storage(secret, mounted=False)
    device = state.get('device')
    if device:
        loops = json.loads(command(['sudo', 'losetup', '--json', '--list', '--output', 'NAME,BACK-FILE']))['loopdevices']
        owned = [v for v in loops if v['name'] == device]
        if len(owned) != 1 or owned[0]['back-file'] != str(root/'disk.img'):
            raise FixtureError('refusing foreign loop cleanup')
        if os.path.ismount(mount):
            # Verify exact source before unmounting, including partial startup.
            mounts = json.loads(command(['findmnt', '--json', '--mountpoint', str(mount), '--output', 'SOURCE,FSTYPE,TARGET']))['filesystems']
            if mounts != [{'source': device, 'fstype': 'ext4', 'target': str(mount)}]:
                raise FixtureError('refusing foreign mount cleanup')
            command(['sudo', 'umount', str(mount)])
        command(['sudo', 'losetup', '--detach', device])
        loops = json.loads(command(['sudo', 'losetup', '--json', '--list', '--output', 'NAME,BACK-FILE']))['loopdevices']
        if any(v['name'] == device or v['back-file'] == str(root/'disk.img') for v in loops):
            raise FixtureError('loop device remains')
    if os.path.ismount(mount):
        raise FixtureError('mounted filesystem remains')
    shutil.rmtree(root)
    path.unlink(); (secret/'disk-pressure-tmp-root').unlink(missing_ok=True)
    if root.exists():
        raise FixtureError('storage image/root remains')
    return {'storage_cleanup': 'passed', 'mount_removed': True, 'loop_detached': True, 'image_removed': True}


def pressure(secret, phase):
    _, _, mount = storage(secret)
    work = mount/'execution'
    if list(work.iterdir()):
        raise FixtureError('refusing pressure adjustment while workspace remains')
    filler = mount/'pressure.bin'
    if filler.is_symlink():
        raise FixtureError('refusing symlink filler')
    filler.unlink(missing_ok=True)
    if phase not in ('release', *PRESSURE_FREE):
        raise FixtureError('unknown pressure phase')
    if phase != 'release':
        desired = PRESSURE_FREE[phase]
        count = shutil.disk_usage(work).free - desired
        if count <= 0:
            raise FixtureError('filesystem too small for chosen pressure window')
        with filler.open('xb') as f:
            os.posix_fallocate(f.fileno(), 0, count)
            os.fsync(f.fileno())
        if not desired-4*1024**2 <= shutil.disk_usage(work).free <= desired+4*1024**2:
            raise FixtureError('actual filesystem free space outside pressure window')
    return {'phase': phase, 'available_bytes': shutil.disk_usage(work).free,
            'filesystem_bytes': shutil.disk_usage(work).total,
            'filler_allocated_bytes': filler.stat().st_blocks*512 if filler.exists() else 0}


def runtime_identity():
    info = json.loads(command(['docker', 'inspect', RUNTIME]))[0]
    _, _, mount = storage(guard())
    if (info['Config']['Labels'].get('com.addp.online-runtime') != SUITE
        or info['Config']['Labels'].get('com.addp.online-run-id') != os.environ['ADDP_ONLINE_TEST_RUN_ID']
        or info['Config']['Cmd'] != ['sh', 'container_entrypoint.sh']
        or info['HostConfig']['Memory'] != 2*GIB or info['HostConfig']['MemorySwap'] != 2*GIB
        or info['HostConfig']['NanoCpus'] != 4_000_000_000
        or info['HostConfig'].get('Privileged') or info['HostConfig'].get('CapAdd')
        or 'TMPDIR=/raster-pressure-tmp' not in info['Config']['Env']
        or not any(v['Type'] == 'bind' and v['Source'] == str(mount/'execution')
                   and v['Destination'] == '/raster-pressure-tmp' and v['RW'] for v in info['Mounts'])
        or info['RestartCount'] != 0 or info['State']['OOMKilled'] or not info['State']['Running']):
        raise FixtureError('Runtime ownership/default entry/resource limits or health differ')
    payload = '''import json,os
from pathlib import Path
pending=[1]; identities=[]
while pending:
 p=pending.pop(); stat=Path(f'/proc/{p}/stat').read_text().rsplit(')',1)[1].split()
 if stat[0]=='Z': continue
 identities.append([p,stat[19]])
 pending.extend(int(x) for x in Path(f'/proc/{p}/task/{p}/children').read_text().split() if int(x)!=os.getpid())
print(json.dumps(sorted(identities)))
'''
    servers = json.loads(command(['docker', 'exec', '-i', RUNTIME, 'python', '-'], input=payload))
    if len(servers) < 2:
        raise FixtureError('missing product master/worker processes')
    return {'container_id': info['Id'], 'image_id': info['Image'], 'server_processes': servers,
            'restart_count': 0, 'oom_killed': False}


def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size <= 0:
        raise FixtureError('expected positive-size owned ordinary file')
    return {'size_bytes': path.stat().st_size, 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()}


def arrays():
    import numpy as np
    a = np.full((HEIGHT, WIDTH), 2, dtype=np.int16)
    a[:, 0] = np.arange(HEIGHT) % 2
    a[101:104, 255:259] = -9999
    valid = a != -9999
    valid[510:514, 510:514] = False
    return a, valid


def seed(root):
    import numpy as np
    from osgeo import gdal, osr
    a, valid = arrays(); crs = osr.SpatialReference(); crs.ImportFromEPSG(3857)
    ds = gdal.GetDriverByName('GTiff').Create(str(root/SOURCE), WIDTH, HEIGHT, 2, gdal.GDT_Int16,
        options=['TILED=YES', 'COMPRESS=DEFLATE'])
    ds.SetGeoTransform(TRANSFORM); ds.SetProjection(crs.ExportToWkt())
    ds.GetRasterBand(1).Fill(9); ds.GetRasterBand(2).WriteArray(a)
    ds.GetRasterBand(2).SetNoDataValue(-9999)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK', 'YES'):
        ds.CreateMaskBand(gdal.GMF_PER_DATASET)
    ds.GetRasterBand(2).GetMaskBand().WriteArray(np.where(valid, 255, 0).astype('uint8'))
    ds = None
    return {'sources': {SOURCE: digest(root/SOURCE)}}


def expectation():
    return {'width': WIDTH, 'height': HEIGHT, 'band_count': 1, 'source_crs': 'EPSG:3857', 'extent_srid': 3857,
            'transform': list(TRANSFORM), 'band_nodata': None,
            'extent': [100., 200.-2.5*HEIGHT, 100.+2.5*WIDTH, 200.]}


def verify(root, names):
    import numpy as np
    from osgeo import gdal, osr
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    if len(names) != len(set(names)) or any(n not in TARGETS for n in names):
        raise FixtureError('invalid exact target inventory')
    if sorted(p.name for p in (root/'outputs').iterdir()) != sorted(names):
        raise FixtureError('unexpected output, sidecar or partial publication')
    _, valid = arrays(); crs = osr.SpatialReference(); crs.ImportFromEPSG(3857)
    result = {'sources': {SOURCE: digest(root/SOURCE)}, 'targets': {}}
    for name in names:
        expected = np.broadcast_to((np.arange(HEIGHT) % 2)[:, None], (HEIGHT, WIDTH)).astype('float64').copy() if name.startswith('nibble-') else np.broadcast_to(2.5*np.arange(WIDTH), (HEIGHT, WIDTH)).copy()
        expected[~valid] = np.nan
        fact = digest(root/'outputs'/name)
        ds = gdal.Open(str(root/'outputs'/name))
        if ds is None: raise FixtureError('missing raster dataset')
        band = ds.GetRasterBand(1)
        actual_crs = osr.SpatialReference(); actual_crs.ImportFromWkt(ds.GetProjection())
        if (ds.RasterCount != 1 or (ds.RasterXSize, ds.RasterYSize) != (WIDTH, HEIGHT)
            or ds.GetGeoTransform() != TRANSFORM or not actual_crs.IsSame(crs)
            or band.DataType != gdal.GDT_Float64 or band.GetNoDataValue() is None or not np.isnan(band.GetNoDataValue())
            or not np.array_equal(band.ReadAsArray(), expected, equal_nan=True)
            or not np.array_equal(band.GetMaskBand().ReadAsArray(), np.where(valid, 255, 0))):
            raise FixtureError('complete pixels/mask, grid, CRS or Float64 NaN differ')
        if band.GetUnitType() != ('metre' if name.startswith('proximity-') else ''):
            raise FixtureError('output units differ')
        if band.GetScale() is not None or band.GetOffset() is not None or band.GetColorTable() is not None:
            raise FixtureError('source display attributes leaked')
        _, errors, _ = validate(ds, full_check=True)
        if errors or band.GetBlockSize() != [4096, 4096] or band.GetOverviewCount() != 0:
            raise FixtureError('COG layout or explicit blocksize differs')
        result['targets'][name] = {**fact, 'checked_pixels': WIDTH*HEIGHT,
            'valid_pixels': int(valid.sum()), 'cog_valid': True, 'full_pixel_and_mask_verification': True,
            'blocksize': 4096, 'has_overviews': False}
        band = ds = None
    return result


def worker(action, root, names):
    if action == 'seed':
        return seed(root)
    if action != 'verify':
        raise FixtureError('unknown Runtime worker action')
    work = Path(tempfile.gettempdir())
    if work != Path('/raster-pressure-tmp') or list(work.iterdir()):
        raise FixtureError('Runtime temporary directory escaped or has residuals')
    return {**verify(root, names), 'runtime_workspaces_removed': True,
            'temporary_available_bytes': shutil.disk_usage(work).free}


def main(action):
    secret = guard(); export = NFSExport(PREFIX, command)
    run_id = os.environ['ADDP_ONLINE_TEST_RUN_ID']
    if action == 'start':
        result = export.lifecycle('start', secret, run_id)
        return {**result, **start_storage(secret)}
    if action == 'stop':
        errors = []; results = {}
        for fn in (lambda: stop_storage(secret), lambda: export.lifecycle('stop', secret, run_id)):
            try: results.update(fn())
            except Exception as e: errors.append(f'{type(e).__name__}: {e}')
        if errors: raise FixtureError('fixture cleanup failed: '+'; '.join(errors))
        return results
    if action in ('distance', 'encoding', 'release'):
        return pressure(secret, action)
    if action == 'runtime-identity':
        return runtime_identity()
    root = export.owned_root(secret, run_id)
    if action not in ('seed', 'verify'):
        raise FixtureError('unknown fixture action')
    result = run_runtime_worker(command, RUNTIME, SUITE, Path(__file__), action, root, sys.argv[2:])
    initial = secret/'disk-pressure-source.json'
    if action == 'seed': initial.write_text(json.dumps(result))
    elif result['sources'] != json.loads(initial.read_text())['sources']:
        raise FixtureError('source file changed')
    return result


if __name__ == '__main__':
    try:
        result = worker(sys.argv[2], Path(sys.argv[3]), sys.argv[4:]) if sys.argv[1:2] == ['--worker'] else main(sys.argv[1])
        print(json.dumps(result, sort_keys=True))
    except Exception as error:
        print(f'Disk pressure fixture failed: {type(error).__name__}: {error}', file=sys.stderr)
        raise SystemExit(1)
