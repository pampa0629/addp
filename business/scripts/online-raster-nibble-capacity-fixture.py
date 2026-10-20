#!/usr/bin/env python3
"""Owned NFS classification grids and a blockwise analytical capacity oracle."""
from __future__ import annotations
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

SUITE = 'raster-nibble-capacity'
PREFIX = 'nibble-capacity'
RUNTIME = 'addp-raster-nibble-capacity-runtime'
SIZES = (4096, 10980)
SPACING = {'sparse': 128, 'dense': 2}
TRANSFORM = (600000., 10., 0., 4000020., 0., -10.)
LABELS = (0, -3, 1, 2**53-1, -(2**53-1), 2**53-2)
SOURCES = tuple(f'classes-{size}-{density}.tif' for size in SIZES for density in SPACING)


class FixtureError(RuntimeError):
    pass


def command(args, **kwargs):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=600, **kwargs).stdout.strip()


if sys.argv[1:2] != ['--worker']:
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport, FixtureError, run_runtime_worker


def groups():
    for size in SIZES:
        for density in SPACING:
            for running in (1, 2):
                yield size, density, running


def target_names(size, density, running):
    if size not in SIZES or density not in SPACING or running not in (1, 2):
        raise FixtureError('unknown capacity case')
    return [f'nibble-{size}-{density}-n{running}-{index}.cog.tif' for index in range(2)]


def targets():
    return {name: (size, density) for size, density, running in groups()
            for name in target_names(size, density, running)}


def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size <= 0:
        raise FixtureError('expected positive-size owned ordinary file')
    value = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1024*1024), b''):
            value.update(block)
    return {'sha256': value.hexdigest(), 'size_bytes': path.stat().st_size}


def block_facts(x, y, width, height, spacing):
    import numpy as np
    rows = np.arange(y, y+height, dtype=np.int64)[:, None]
    columns = np.arange(x, x+width, dtype=np.int64)[None, :]
    donors = (rows % spacing == 0) & (columns % spacing == 0)
    nodata = ~donors & ((rows*17 + columns*13) % 263 == 1)
    masked = ~donors & ((rows*31 + columns*23) % 257 == 2)
    labels = np.array(LABELS, dtype=np.int64)
    values = np.where(donors, labels[((rows//spacing)*7 + (columns//spacing)*11) % len(labels)], 7)
    values[nodata] = -9999
    return values, ~masked, ~nodata & ~masked


def expected_block(x, y, width, height, size, spacing):
    """A rectangular donor lattice separates into independently nearest axes.

    At equal distance select the lower coordinate, implementing row then column
    order without invoking product EDT or nearest-site helpers.
    """
    import numpy as np
    def nearest(start, count):
        axis = np.arange(start, start+count, dtype=np.int64)
        lower = axis//spacing*spacing
        upper = np.minimum(lower+spacing, (size-1)//spacing*spacing)
        return np.where(axis-lower <= upper-axis, lower, upper)
    rows, columns = nearest(y, height)[:, None], nearest(x, width)[None, :]
    expected = np.array(LABELS, dtype=np.int64)[((rows//spacing)*7+(columns//spacing)*11) % len(LABELS)].astype(np.float64)
    valid = block_facts(x, y, width, height, spacing)[2]
    expected[~valid] = np.nan
    return expected, valid


def windows(size):
    for y in range(0, size, 512):
        for x in range(0, size, 512):
            yield x, y, min(512, size-x), min(512, size-y)


def artifact_expectation(size):
    return {'width': size, 'height': size, 'band_count': 1, 'source_crs': 'EPSG:32617',
            'extent_srid': 32617, 'transform': list(TRANSFORM), 'band_nodata': None,
            'extent': [600000., 4000020.-10*size, 600000.+10*size, 4000020.]}


def seed(root):
    import numpy as np
    from osgeo import gdal, osr
    if any((root/name).exists() for name in SOURCES) or list((root/'outputs').iterdir()):
        raise FixtureError('refusing to overwrite capacity fixture')
    crs = osr.SpatialReference(); crs.ImportFromEPSG(32617)
    for size in SIZES:
        for density, spacing in SPACING.items():
            path = root/f'classes-{size}-{density}.tif'
            dataset = gdal.GetDriverByName('GTiff').Create(str(path), size, size, 1, gdal.GDT_Int64,
                                                        ['TILED=YES', 'COMPRESS=DEFLATE'])
            dataset.SetGeoTransform(TRANSFORM); dataset.SetProjection(crs.ExportToWkt())
            band = dataset.GetRasterBand(1); band.SetNoDataValue(-9999)
            with gdal.config_option('GDAL_TIFF_INTERNAL_MASK', 'YES'):
                dataset.CreateMaskBand(gdal.GMF_PER_DATASET)
            mask = band.GetMaskBand()
            for x, y, width, height in windows(size):
                values, explicit_mask, _ = block_facts(x, y, width, height, spacing)
                band.WriteArray(values, x, y); mask.WriteArray(explicit_mask.astype(np.uint8)*255, x, y)
            mask = band = dataset = None
    return {'sources': {name: digest(root/name) for name in SOURCES}, 'sizes': list(SIZES),
            'distribution': SPACING, 'sample_kind': 'deterministic-integer-classification-lattice'}


def verify_output(path, size, density):
    import numpy as np
    from osgeo import gdal, osr
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    fact = digest(path); dataset = gdal.Open(str(path)); crs = osr.SpatialReference(); crs.ImportFromEPSG(32617)
    if (dataset is None or dataset.GetDriver().ShortName != 'GTiff' or dataset.RasterCount != 1
        or (dataset.RasterXSize, dataset.RasterYSize, dataset.GetGeoTransform()) != (size, size, TRANSFORM)
        or not dataset.GetProjection() or not osr.SpatialReference(wkt=dataset.GetProjection()).IsSame(crs)):
        raise FixtureError('capacity output grid/CRS/type differs')
    band = dataset.GetRasterBand(1)
    if (band.DataType != gdal.GDT_Float64 or band.GetNoDataValue() is None or not np.isnan(band.GetNoDataValue())
        or band.GetScale() is not None or band.GetOffset() is not None or band.GetUnitType()
        or band.GetColorTable() is not None or band.GetOverviewCount() < 1):
        raise FixtureError('capacity output band/NoData/overview facts differ')
    valid_pixels = checked = 0
    for x, y, width, height in windows(size):
        expected, valid = expected_block(x, y, width, height, size, SPACING[density])
        if not np.array_equal(band.ReadAsArray(x, y, width, height), expected, equal_nan=True):
            raise FixtureError('capacity complete pixel values differ')
        if not np.array_equal(band.GetMaskBand().ReadAsArray(x, y, width, height) != 0, valid):
            raise FixtureError('capacity complete mask differs')
        valid_pixels += int(valid.sum()); checked += width*height
    _, errors, _ = validate(dataset, full_check=True)
    if errors:
        raise FixtureError('capacity COG full layout invalid')
    band = dataset = None
    return {**fact, 'checked_pixels': checked, 'valid_pixels': valid_pixels, 'cog_valid': True,
            'has_overviews': True, 'full_pixel_and_mask_verification': True, 'exact_integer_pixels_verified': True}


def verify(root, names, previous):
    mapping = targets()
    if len(set(names)) != len(names) or any(name not in mapping for name in names):
        raise FixtureError('unknown capacity output inventory')
    if not set(previous) <= set(names) or sorted(p.name for p in (root/'outputs').iterdir()) != sorted(names):
        raise FixtureError('capacity output inventory or sidecars differ')
    output = {}
    for name in names:
        if name in previous:
            if digest(root/'outputs'/name) != {key: previous[name][key] for key in ('sha256', 'size_bytes')}:
                raise FixtureError('previous capacity artifact changed')
            output[name] = previous[name]
        else:
            output[name] = verify_output(root/'outputs'/name, *mapping[name])
    return {'sources': {name: digest(root/name) for name in SOURCES}, 'targets': output,
            'source_unchanged': True, 'runtime_workspaces_removed': True}


def require_disk(root, size, running):
    # 32 B private EDT + 8 B output + 11 B COG including overviews per pixel,
    # conservatively overlapping these phases; retained files already reduce free.
    required = running*size**2*51 + 2*1024**3
    info = os.statvfs(root); available = info.f_bavail*info.f_frsize
    if available < required:
        raise FixtureError('insufficient owned capacity disk space')
    return {'required_free_bytes': required, 'available_bytes': available,
            'reserve_method': 'running*pixels*(32-private+8-output+11-cog)+2GiB'}


def worker(action, root, names):
    scratch = root/'runtime-tmp'
    if action == 'seed':
        return seed(root)
    if action != 'verify' or list(scratch.iterdir()):
        raise FixtureError('capacity action or Runtime temporary cleanup differs')
    # Previously verified facts are retained privately by the host, not published.
    if not names:
        raise FixtureError('missing previous capacity evidence')
    previous = json.loads(names[-1])
    if not isinstance(previous, dict):
        raise FixtureError('invalid previous capacity evidence')
    return verify(root, names[:-1], previous)


def main(action):
    export = NFSExport(PREFIX, command); secret = export.guard(os.environ)
    if action in {'start', 'stop'}:
        result = export.lifecycle(action, secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])
        if action == 'start':
            (export.owned_root(secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])/'runtime-tmp').mkdir(mode=0o777)
        return result
    root = export.owned_root(secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    if (root/'runtime-tmp').is_symlink() or not (root/'runtime-tmp').is_dir():
        raise FixtureError('capacity temporary directory ownership differs')
    if action == 'check-root':
        return {'root': str(root), 'scratch': str(root/'runtime-tmp')}
    if action == 'seed':
        result = run_runtime_worker(command, RUNTIME, SUITE, Path(__file__), 'seed', root, ())
        (secret/'nibble-capacity-source.json').write_text(json.dumps(result)); return result
    if action == 'verify':
        names = sys.argv[2:]
        previous_path = secret/'nibble-capacity-previous.json'
        previous = json.loads(previous_path.read_text()) if previous_path.exists() else {}
        result = run_runtime_worker(command, RUNTIME, SUITE, Path(__file__), 'verify', root,
                                    (*names, json.dumps(previous)))
        if result['sources'] != json.loads((secret/'nibble-capacity-source.json').read_text())['sources']:
            raise FixtureError('capacity source file changed')
        previous_path.write_text(json.dumps(result['targets'])); return result
    raise FixtureError('unknown capacity fixture action')


if __name__ == '__main__':
    try:
        result = worker(sys.argv[2], Path(sys.argv[3]), sys.argv[4:]) if sys.argv[1:2] == ['--worker'] else main(sys.argv[1])
        print(json.dumps(result, sort_keys=True))
    except Exception as error:
        print(f'Nibble capacity fixture failed: {type(error).__name__}: {error}', file=sys.stderr)
        raise SystemExit(1)
