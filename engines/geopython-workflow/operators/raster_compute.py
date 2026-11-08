"""GDAL raster operators. Pixel data and paths stay inside one execution workspace."""
from __future__ import annotations

import ast
import heapq
from collections import deque
from contextlib import contextmanager, nullcontext
from contextvars import ContextVar
from dataclasses import dataclass, field
from fractions import Fraction
from functools import lru_cache
from itertools import pairwise
import json
import math
from numbers import Integral, Real
import shutil
from pathlib import Path
import tempfile
import struct
import threading
import uuid
import xml.etree.ElementTree as ET

import numpy as np
import pandas as pd
import geopandas as gpd
from shapely import STRtree, from_wkb, get_coordinates, has_m, has_z, make_valid
from shapely.geometry import LineString, MultiPolygon, Point, box
from osgeo import gdal, ogr, osr
from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate as validate_layout
from addp_common.workflow_access import (
    require_access_plan, require_source_plan, require_target_plan,
    stage_source_file, publish_target_file,
)
from addp_common.workflow_runtime import WorkflowExecutionError
from addp_common.workflow_access import require_empty_target_directory, target_child_file_plan
from .base import OperatorMetadata, OperatorParam, OperatorType, OperatorCategory, OutputPort, register_operator
from .raster_image_export import encode_image, image_export_error
from .surfer import inspect_surfer, validate_surfer6_mask, validate_surfer_ascii_pixels
from .envi import inspect_envi, validate_dataset as validate_envi_dataset, ENVIProfileError

gdal.UseExceptions()
_WORKSPACE = ContextVar('raster_workspace', default=None)
_DISK_PROTECTED = ContextVar('raster_disk_protected', default=False)
_DISK_LANGUAGE = ContextVar('raster_disk_language', default='zh-cn')
_DISK_SAFETY_BYTES = 2 * 1024**3
_DISK_LOCK = threading.RLock()
_DISK_RESERVATIONS = {}
_DISK_MESSAGES = {
    'zh-cn': {
        'unavailable': '无法读取栅格临时磁盘容量（阶段：{phase}）',
        'insufficient': '栅格临时磁盘空间不足（阶段：{phase}）：',
    },
    'en': {
        'unavailable': 'Raster temporary disk capacity unavailable during {phase}',
        'insufficient': 'Insufficient raster temporary disk space during {phase}: ',
    },
}
RESAMPLING = ['nearest', 'bilinear', 'cubic', 'cubicspline', 'lanczos', 'average', 'mode']
_PERCENTILE_UNSET = object()
_ZONAL_PERCENTILE_DTYPE = np.dtype([('zone', np.int64), ('value', np.float64)])


@contextmanager
def raster_workspace():
    with tempfile.TemporaryDirectory(prefix='addp-raster-') as directory:
        token = _WORKSPACE.set(Path(directory))
        disk_token = _DISK_PROTECTED.set(False)
        try:
            yield
        finally:
            _DISK_PROTECTED.reset(disk_token)
            _WORKSPACE.reset(token)


def _path(suffix='.tif'):
    root = _WORKSPACE.get()
    if root is None:
        raise ValueError('Raster operators require an execution workspace')
    return root / (uuid.uuid4().hex + suffix)


@contextmanager
def raster_error_language(language):
    """Bind the submitting request's language inside the execution thread."""
    token = _DISK_LANGUAGE.set(language)
    try:
        yield
    finally:
        _DISK_LANGUAGE.reset(token)


def _disk_message(code, phase):
    return _DISK_MESSAGES[_DISK_LANGUAGE.get()][code].format(phase=phase)


@contextmanager
def _disk_budget(required_bytes, phase, *, private_paths=()):
    """Account for pending writes on the actual temporary filesystem, in-process."""
    root = _WORKSPACE.get()
    if root is None:
        raise ValueError('Raster operators require an execution workspace')
    reservation = object()
    with _DISK_LOCK:
        try:
            device = root.stat().st_dev
            pending = 0
            for filesystem, budget, paths in _DISK_RESERVATIONS.values():
                if filesystem != device:
                    continue
                allocated = 0
                for path in paths:
                    try:
                        allocated += path.stat().st_blocks * 512
                    except FileNotFoundError:
                        pass  # A named owned file may not have been created yet.
                pending += max(0, budget - allocated)
            # Private files only grow while unlocked; deletion takes this lock.
            # Reading allocation before free space therefore errs conservatively.
            available = shutil.disk_usage(root).free
            if type(available) is not int or available < 0:
                raise ValueError('Invalid filesystem capacity observation')
        except (OSError, AttributeError, ValueError) as exc:
            raise RuntimeError(_disk_message('unavailable', phase)) from exc
        if available < required_bytes + pending + _DISK_SAFETY_BYTES:
            raise RuntimeError(
                _disk_message('insufficient', phase) +
                f'required_bytes={required_bytes}, available_bytes={available}, '
                f'pending_bytes={pending}, safety_bytes={_DISK_SAFETY_BYTES}')
        _DISK_RESERVATIONS[reservation] = (device, required_bytes, tuple(private_paths))
    try:
        yield
    finally:
        with _DISK_LOCK:
            del _DISK_RESERVATIONS[reservation]


def _raster_disk_bytes(dataset, *, profile='geotiff', blocksize=256, numeric=False):
    """Conservative tiled storage estimate; not a quota or compressed-size claim."""
    width, height = dataset.RasterXSize, dataset.RasterYSize
    if profile == 'cog':
        if type(blocksize) is not int or not 128 <= blocksize <= 4096 or blocksize % 16:
            raise ValueError('blocksize must be a multiple of 16 between 128 and 4096')
    else:
        blocksize = 256  # The existing GTiff TILED=YES encoder's default.
    bands = [None] if numeric else [dataset.GetRasterBand(i) for i in range(1, dataset.RasterCount + 1)]
    payload = 0
    for band in bands:
        levels = {(width, height)}
        if band is not None:
            levels.update((band.GetOverview(i).XSize, band.GetOverview(i).YSize)
                          for i in range(band.GetOverviewCount()))
        if profile == 'cog':
            w, h = width, height
            while max(w, h) > blocksize:
                w, h = (w + 1) // 2, (h + 1) // 2
                levels.add((w, h))
        # A byte-per-pixel mask overestimates GTiff's packed internal masks.
        pixel_bytes = (8 if numeric else (gdal.GetDataTypeSize(band.DataType) + 7) // 8) + 1
        for w, h in levels:
            tiles = ((w + blocksize - 1) // blocksize) * ((h + blocksize - 1) // blocksize)
            payload += tiles * (blocksize**2 * pixel_bytes + 64)
    # Allow compression expansion, TIFF metadata, and COG's temporary copy.
    return (2 if profile == 'cog' else 1) * (2 * payload + 8 * 1024**2)


@dataclass(frozen=True)
class RasterDataset:
    path: Path
    workspace: Path
    native_driver: str = ''
    related_files: tuple[Path, ...] = ()


@dataclass(frozen=True)
class RasterTiles:
    source: RasterDataset
    tile_width: int
    tile_height: int


def raster_tile(input_raster, tile_size):
    _open(input_raster)
    width, height = _pair(tile_size, 'tile_size', integer=True)
    return RasterTiles(input_raster, width, height)


def raster_tiles_info(input_tiles):
    if not isinstance(input_tiles, RasterTiles):
        raise ValueError('input_tiles must come from a raster_tiles port in this execution')
    dataset = _open(input_tiles.source)
    columns = (dataset.RasterXSize + input_tiles.tile_width - 1) // input_tiles.tile_width
    rows = (dataset.RasterYSize + input_tiles.tile_height - 1) // input_tiles.tile_height
    return {'source_width': dataset.RasterXSize, 'source_height': dataset.RasterYSize,
            'tile_size': [input_tiles.tile_width, input_tiles.tile_height],
            'columns': columns, 'rows': rows, 'total_tiles': columns * rows}


def _raster(path, native_driver='', related_files=()):
    return RasterDataset(Path(path), _WORKSPACE.get(), native_driver, tuple(Path(p) for p in related_files))


def _open(raster):
    if not isinstance(raster, RasterDataset) or raster.workspace != _WORKSPACE.get() or raster.workspace is None:
        raise ValueError('input_raster must come from a raster port in this execution')
    drivers = [raster.native_driver] if raster.native_driver else ['GTiff', 'PNG', 'JPEG']
    options = ['GEOREF_SOURCES=INTERNAL', 'OPEN_REMOTE_GML=NO', 'STRICT=YES', '1BIT_ALPHA_PROMOTION=NO'] if raster.native_driver == 'JP2OpenJPEG' else []
    dataset = gdal.OpenEx(str(raster.path), gdal.OF_RASTER, allowed_drivers=drivers, open_options=options)
    if dataset is None or dataset.RasterCount < 1:
        raise ValueError('Raster dataset has no bands')
    if raster.native_driver == 'ENVI':
        expected = {raster.path.resolve(), *(p.resolve() for p in raster.related_files)}
        if len(raster.related_files) != 1 or {Path(p).resolve() for p in dataset.GetFileList() or []} != expected:
            raise ENVIProfileError('native_composition', _DISK_LANGUAGE.get())
    return dataset


def _crs(value):
    if not isinstance(value, str) or not value.strip():
        raise ValueError('CRS must be explicit and non-empty')
    crs = osr.SpatialReference()
    if crs.SetFromUserInput(value) != 0:
        raise ValueError('Invalid CRS')
    crs.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
    return crs


def _require_georeferencing(dataset):
    _crs(dataset.GetProjection())
    if dataset.GetGeoTransform(can_return_null=True) is None:
        raise ValueError('Raster requires a geotransform')


def _positive(value, name, integer=False):
    if integer and (isinstance(value, bool) or not isinstance(value, int) or value > 2**31 - 1):
        raise ValueError(f'{name} must be a positive integer')
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or value <= 0:
        raise ValueError(f'{name} must be positive and finite')
    return value


def _pair(value, name, integer=False):
    if not isinstance(value, (list, tuple)) or len(value) != 2:
        raise ValueError(f'{name} must contain two values')
    return [_positive(item, name, integer) for item in value]


def _algorithm(value):
    if value not in RESAMPLING:
        raise ValueError('Unsupported resampling algorithm')
    return value


def _translated(dataset, **kwargs):
    path = _path()
    result = gdal.Translate(str(path), dataset, options=gdal.TranslateOptions(**kwargs))
    if result is None:
        raise ValueError('GDAL raster translation failed')
    result.FlushCache()
    result = None
    return _raster(path)


def _warped(datasets, **kwargs):
    for dataset in datasets:
        _require_georeferencing(dataset)
    return _warp(datasets, **kwargs)


def _warp_dataset(datasets, initialize_zero=False, **kwargs):
    result = gdal.Warp(str(_path()),datasets,options=gdal.WarpOptions(
        format='GTiff',creationOptions=['TILED=YES','COMPRESS=DEFLATE'],
        warpOptions=['INIT_DEST=0'] if initialize_zero else [],**kwargs))
    if result is None:
        raise ValueError('GDAL raster warp failed')
    return result


def _warp(datasets, **kwargs):
    has_alpha = kwargs.get('dstAlpha', False) or any(
        dataset.GetRasterBand(index).GetColorInterpretation() == gdal.GCI_AlphaBand
        for dataset in datasets for index in range(1,dataset.RasterCount+1))
    # With a source alpha GDAL does not automatically copy source NoData to the
    # destination. Declare it so each data band's missing values remain missing.
    if has_alpha and 'dstNodata' not in kwargs:
        nodata = datasets[0].GetRasterBand(1).GetNoDataValue()
        if nodata is not None:
            kwargs['dstNodata'] = nodata
    nodata_values = [kwargs['dstNodata']] if 'dstNodata' in kwargs else [
        dataset.GetRasterBand(index).GetNoDataValue()
        for dataset in datasets for index in range(1,dataset.RasterCount+1)]
    restore_nodata = has_alpha and any(value is not None for value in nodata_values)
    result = _warp_dataset(datasets,initialize_zero=restore_nodata,**kwargs)
    path = result.GetDescription()
    if restore_nodata:
        # Alpha blending must start with finite data: NaN * zero density is still NaN.
        # Shared alpha describes overall coverage, not each band's validity.
        alpha = next(result.GetRasterBand(index) for index in range(1,result.RasterCount+1)
                     if result.GetRasterBand(index).GetColorInterpretation() == gdal.GCI_AlphaBand)
        data_bands = [index for index in range(1,result.RasterCount+1)
                      if result.GetRasterBand(index).GetColorInterpretation() != gdal.GCI_AlphaBand]
        for index in data_bands:
            band = result.GetRasterBand(index)
            nodata = band.GetNoDataValue()
            if nodata is None:
                continue
            independent = None
            coverage_band = alpha
            if len(data_bands) > 1:
                # A different band's alpha density must not fill this band's holes
                # or affect its mosaic blending. Warp its data and coverage on the
                # common grid, retaining the requested kernel and source order.
                sources = []
                for dataset in datasets:
                    bands = [index] + [i for i in range(1,dataset.RasterCount+1)
                                       if dataset.GetRasterBand(i).GetColorInterpretation() == gdal.GCI_AlphaBand]
                    sources.append(gdal.Translate(str(_path('.vrt')),dataset,format='VRT',bandList=bands))
                transform = result.GetGeoTransform()
                options = dict(kwargs)
                for key in ('xRes','yRes','targetAlignedPixels','cropToCutline'):
                    options.pop(key,None)
                options.update(width=result.RasterXSize,height=result.RasterYSize,dstAlpha=True,
                               srcAlpha=sources[0].RasterCount>1,
                               outputBounds=[transform[0],transform[3]+transform[5]*result.RasterYSize,
                                             transform[0]+transform[1]*result.RasterXSize,transform[3]])
                independent = _warp_dataset(sources,initialize_zero=True,**options)
                coverage_band = independent.GetRasterBand(2)
            for y in range(0,result.RasterYSize,512):
                for x in range(0,result.RasterXSize,512):
                    width,height = min(512,result.RasterXSize-x),min(512,result.RasterYSize-y)
                    coverage = _read_values(coverage_band,x,y,width,height)
                    invalid = ~np.isfinite(coverage) | (coverage <= 0)
                    if independent is None and not invalid.any():
                        continue
                    source_band = independent.GetRasterBand(1) if independent is not None else band
                    complex_values = gdal.DataTypeIsComplex(band.DataType)
                    buffer_type = gdal.GDT_CFloat64 if complex_values else gdal.GDT_Float64
                    values = np.frombuffer(source_band.ReadRaster(x,y,width,height,buf_type=buffer_type),
                                           dtype=np.complex128 if complex_values else np.float64).reshape(height,width).copy()
                    values[invalid] = nodata
                    band.WriteRaster(x,y,width,height,values.tobytes(),buf_type=buffer_type)
            independent = None
    result.FlushCache()
    result = None
    return _raster(path)


def _resized(dataset, width, height, resampling):
    projection = dataset.GetProjection()
    transform = dataset.GetGeoTransform(can_return_null=True)
    source_width, source_height = dataset.RasterXSize, dataset.RasterYSize
    # Only this trusted execution-local view has synthetic pixel coordinates.
    pixels = gdal.Translate(str(_path('.vrt')), dataset, format='VRT')
    pixels.SetProjection('')
    pixels.SetGeoTransform((0,1,0,source_height,0,-1))
    resized = _warp([pixels], width=width, height=height, resampleAlg=resampling)
    pixels = None
    view_path = _path('.vrt')
    view = gdal.Translate(str(view_path), _open(resized), format='VRT')
    document = ET.fromstring(view.GetMetadata('xml:VRT')[0])
    view = None
    for tag in ('SRS', 'GeoTransform'):
        element = document.find(tag)
        if element is not None:
            document.remove(element)
    if projection:
        ET.SubElement(document,'SRS').text = projection
    if transform is not None:
        xscale, yscale = source_width/width, source_height/height
        restored = (transform[0],transform[1]*xscale,transform[2]*yscale,
                    transform[3],transform[4]*xscale,transform[5]*yscale)
        ET.SubElement(document,'GeoTransform').text = ','.join(map(str,restored))
    ET.ElementTree(document).write(view_path,encoding='utf-8')
    # Materialize an ordinary TIFF; internal VRTs never become raster-port values.
    return _translated(gdal.Open(str(view_path)), format='GTiff')


def _require_self_contained_hfa(path):
    """Bounded reference preflight, before native GDAL can follow dependencies.

    Only entry headers are inspected, not raster metadata or pixel encoding.
    Layout follows GDAL frmts/hfa/hfaopen.cpp and hfaentry.cpp. Native HFA
    remains the sole metadata/pixel decoder. RRD reference lists (including
    internal ones) are outside this initial self-contained profile.
    """
    with path.open('rb') as stream:
        size = path.stat().st_size

        def read_at(offset, count):
            if offset < 0 or offset > size - count:
                raise ValueError('Invalid HFA entry bounds')
            stream.seek(offset)
            data = stream.read(count)
            if len(data) != count:
                raise ValueError('Truncated HFA entry')
            return data

        header = read_at(0, 20)
        if header[:16] != b'EHFA_HEADER_TAG\x00':
            raise ValueError('IMG input must be ERDAS Imagine HFA')
        file_header = read_at(struct.unpack_from('<I', header, 16)[0], 18)
        version, _, root, header_size, _ = struct.unpack('<IIIHI', file_header)
        if version != 1 or header_size < 120 or not root:
            raise ValueError('Unsupported HFA entry header')
        pending, visited = [root], set()
        while pending:
            offset = pending.pop()
            if offset in visited or len(visited) >= 8192:
                raise ValueError('Invalid or over-budget HFA entry tree')
            visited.add(offset)
            entry = read_at(offset, 120)
            next_entry, _, _, child, data_offset, data_size = struct.unpack_from('<IIIIII', entry)
            if data_size and (not data_offset or data_offset > size - data_size):
                raise ValueError('Invalid HFA entry data bounds')
            name = entry[24:88].split(b'\x00', 1)[0].lower()
            kind = entry[88:120].split(b'\x00', 1)[0].lower()
            if name in (b'externalrasterdms', b'dependentfile', b'rrdnameslist') or kind in (
                    b'imgexternalraster', b'eimg_dependentfile', b'eimg_rrdnameslist'):
                raise ValueError('HFA external/dependent files and RRD reference lists are not supported')
            pending.extend(pointer for pointer in (next_entry, child) if pointer)


def _require_single_jpeg2000(path):
    """Inspect framing only; JP2OpenJPEG remains the sole pixel/CRS decoder."""
    size = path.stat().st_size
    with path.open('rb') as stream:
        if stream.read(4) == b'\xff\x4f\xff\x51':
            return 'j2k'
        stream.seek(0)
        if stream.read(12) != b'\x00\x00\x00\x0cjP  \x0d\x0a\x87\x0a':
            raise ValueError('JPEG2000 requires a JP2 container or J2K codestream')
        offset, boxes, codestreams, filetype = 12, 0, 0, False
        while offset < size:
            if boxes >= 4096 or size - offset < 8:
                raise ValueError('Invalid or over-budget JP2 box table')
            boxes += 1
            stream.seek(offset)
            length, kind = struct.unpack('>I4s', stream.read(8))
            header = 8
            if length == 1:
                if size - offset < 16:
                    raise ValueError('Truncated JP2 extended box')
                length, = struct.unpack('>Q', stream.read(8))
                header = 16
            elif length == 0:
                length = size - offset
            if length < header or length > size - offset:
                raise ValueError('Invalid JP2 box bounds')
            if kind == b'ftyp':
                if filetype or length - header < 12 or length - header > 1024 or (length - header) % 4:
                    raise ValueError('Invalid JP2 file type')
                brands = stream.read(length - header)
                if brands[:4] != b'jp2 ' or b'jp2 ' not in [brands[i:i+4] for i in range(8, len(brands), 4)]:
                    raise ValueError('JPX compositions are not supported')
                filetype = True
            if kind in (b'ftbl', b'dtbl', b'jpch', b'jplh', b'comp'):
                raise ValueError('JPEG2000 external fragments and JPX compositions are not supported')
            if kind == b'jp2c':
                codestreams += 1
            offset += length
        if not filetype or codestreams != 1:
            raise ValueError('JP2 requires exactly one self-contained codestream')
    return 'jp2'


def raster_load(access_plan, source_crs=''):
    plan = require_source_plan({'access_plan': access_plan})
    path = stage_source_file(plan, _path().parent / uuid.uuid4().hex)
    source_format = plan['source']['format']
    native_driver = {'img': 'HFA', 'jpeg2000': 'JP2OpenJPEG', 'envi': 'ENVI'}.get(source_format, '')
    related_files = ()
    if source_format == 'envi':
        member = plan['source']['related_files'][0]['access']
        header = path.parent / Path(member['path'] if member['method'] == 'mounted_path' else member['object']).name
        related_files = (header,)
        envi_facts = inspect_envi(path, header, _DISK_LANGUAGE.get())
        expected_header = (plan['source'].get('metadata') or {}).get('expected_header_sha256')
        if expected_header is not None and expected_header != envi_facts['header_sha256']:
            raise ENVIProfileError('header_version', _DISK_LANGUAGE.get())
    if source_format in ('img', 'jpeg2000', 'grd'):
        # Native rasters expose only the selected primary, never sibling sidecars.
        isolated = _path().parent / uuid.uuid4().hex / path.name
        isolated.parent.mkdir()
        shutil.copyfile(path, isolated)
        path = isolated
        if source_format == 'img':
            _require_self_contained_hfa(path)
        elif source_format == 'jpeg2000':
            _require_single_jpeg2000(path)
        else:
            surfer_facts = inspect_surfer(path, _DISK_LANGUAGE.get())
            native_driver = surfer_facts['driver']
    raster = _raster(path, native_driver, related_files)
    try:
        dataset = _open(raster)
    except RuntimeError as exc:
        raise ValueError('Source format must match a supported raster driver') from exc
    expected_driver = native_driver or {'tiff':'GTiff', 'png':'PNG', 'jpeg':'JPEG'}.get(source_format)
    if expected_driver != dataset.GetDriver().ShortName:
        raise ValueError('Source format must match a supported raster driver')
    if source_format == 'envi':
        validate_envi_dataset(dataset, envi_facts, _DISK_LANGUAGE.get())
    if native_driver == 'GSAG':
        validate_surfer_ascii_pixels(dataset, path, _DISK_LANGUAGE.get())
    if native_driver in ('GSBG', 'GSAG'):
        validate_surfer6_mask(dataset, surfer_facts['blank_value'], _DISK_LANGUAGE.get())
    if source_format in ('img', 'jpeg2000', 'grd', 'envi'):
        bands = [dataset.GetRasterBand(index) for index in range(1, dataset.RasterCount + 1)]
        if any(gdal.DataTypeIsComplex(band.DataType) for band in bands):
            raise ValueError(f'Complex {native_driver} bands require an explicit real-valued conversion before loading')
        nodata = [band.GetNoDataValue() for band in bands]
        nodata = ['nan' if value is not None and math.isnan(value) else value for value in nodata]
        if len({band.DataType for band in bands}) != 1 or any(value != nodata[0] for value in nodata):
            raise ValueError(f'{native_driver} bands must share one data type and NoData value for lossless TIFF conversion')
    if source_format != 'envi' and any(Path(file).resolve() != path.resolve() for file in dataset.GetFileList() or []):
        raise ValueError('Raster input must be self-contained; external sidecars are not supported')
    # Snapshot the selected resource so later writes cannot change an upstream input.
    snapshot = path if native_driver else _path(path.suffix)
    if snapshot != path:
        shutil.copyfile(path, snapshot)
    dataset = None
    raster = _raster(snapshot, native_driver, related_files)
    dataset = _open(raster)
    if source_crs:
        supplied = _crs(source_crs)
        if dataset.GetProjection():
            # Raster geotransforms use x/y; compare CRS semantics independently of
            # geographic authority axis order, then preserve the original definition.
            if not _crs(dataset.GetProjection()).IsSame(supplied, [
                'CRITERION=EQUIVALENT_EXCEPT_AXIS_ORDER_GEOGCRS',
                'IGNORE_DATA_AXIS_TO_SRS_AXIS_MAPPING=YES',
            ]):
                raise ValueError('source_crs conflicts with the raster CRS')
        else:
            raster = _translated(dataset, format='GTiff', outputSRS=supplied.ExportToWkt())
    return raster


def raster_inspect(access_plan):
    """Read-only native media facts; no publication and no pixel statistics."""
    plan = require_source_plan({'access_plan': access_plan})
    source_format = plan['source']['format']
    if source_format not in ('img', 'jpeg2000', 'grd', 'envi'):
        raise ValueError('raster_inspect requires IMG, JPEG2000, ENVI or Surfer GRD format')
    with raster_workspace():
        raster = raster_load(plan)
        facts = raster_info(raster)
        driver = raster.native_driver
        profile_facts = (inspect_envi(raster.path, raster.related_files[0], _DISK_LANGUAGE.get()) if source_format == 'envi' else
                        inspect_surfer(raster.path, _DISK_LANGUAGE.get()) if source_format == 'grd' else
                         {'profile':'hfa' if source_format == 'img' else _require_single_jpeg2000(raster.path)})
        profile = profile_facts['profile']
        spatial = {}
        if facts['extent'] and source_format != 'grd':
            spatial['extent'] = facts['extent']
        if facts['extent_srid']:
            spatial['srid'] = facts['extent_srid']
        elif facts['crs']:
            import hashlib
            definition = facts['crs'].strip()
            reference = 'ADDP:CRS:' + hashlib.sha256(definition.encode()).hexdigest()
            spatial.update({'crs_ref': reference, 'crs_definitions': [{
                'id': reference, 'definition_encoding': 'wkt', 'definition': definition,
                'source': 'gdal_' + driver.lower()}]})
        return {'schema_version': 'gdal.raster.inspect/v1', 'format': source_format, 'description': {
            'media': {'kind': 'image', 'encoding': driver, 'width': facts['width'],
                      'mime_type': 'image/jp2' if profile == 'jp2' else 'application/octet-stream',
                      'height': facts['height'], 'size_bytes': raster.path.stat().st_size + sum(p.stat().st_size for p in raster.related_files)},
            'spatial': spatial or None,
            'format_info': {**profile_facts, 'driver': driver, 'scan_complete': True,
                            'band_count': facts['band_count'], 'bands': facts['bands']}}}


def _corners(dataset):
    transform = dataset.GetGeoTransform(can_return_null=True)
    if transform is None:
        return []
    return [(transform[0] + x * transform[1] + y * transform[2],
             transform[3] + x * transform[4] + y * transform[5])
            for x, y in [(0,0), (dataset.RasterXSize,0),
                         (dataset.RasterXSize,dataset.RasterYSize), (0,dataset.RasterYSize)]]


def raster_info(input_raster):
    dataset = _open(input_raster)
    transform = dataset.GetGeoTransform(can_return_null=True)
    corners = _corners(dataset)
    extent = [min(x for x, _ in corners), min(y for _, y in corners),
              max(x for x, _ in corners), max(y for _, y in corners)] if corners else []
    crs = dataset.GetSpatialRef()
    authority = crs.GetAuthorityCode(None) if crs else None
    authority_name = crs.GetAuthorityName(None) if crs else None
    srid = int(authority) if authority_name == 'EPSG' and authority and authority.isdigit() else 0
    bands = []
    for index in range(1, dataset.RasterCount + 1):
        band = dataset.GetRasterBand(index)
        nodata = band.GetNoDataValue()
        bands.append({'band': index, 'dtype': gdal.GetDataTypeName(band.DataType),
                      'color_interpretation': gdal.GetColorInterpretationName(band.GetColorInterpretation()),
                      'nodata': nodata if nodata is None or math.isfinite(nodata) else None,
                      'nodata_is_nan': nodata is not None and math.isnan(nodata),
                      'block_size': band.GetBlockSize(),
                      'overviews': [[band.GetOverview(i).XSize, band.GetOverview(i).YSize]
                                    for i in range(band.GetOverviewCount())]})
    return {'width': dataset.RasterXSize, 'height': dataset.RasterYSize,
            'band_count': dataset.RasterCount, 'bands': bands,
            'crs': dataset.GetProjection(), 'source_crs': f'EPSG:{srid}' if srid else dataset.GetProjection(),
            'extent_srid': srid,
            'transform': list(transform) if transform else [], 'extent': extent}


def validate_cog(input_raster):
    warnings, errors, details = validate_layout(_open(input_raster), full_check=True)
    return {'valid': not errors, 'warnings': list(warnings), 'errors': list(errors)}


def translate_cog(source, target, compression='DEFLATE', blocksize=512, overview_resampling='nearest', *,
                  width=0, height=0, resampling=None, assign_srs=None, callback=None, num_threads='2', use_source_overviews=True, gdal_api=gdal):
    """One GDAL COG encoder shared by workflow persistence and directory mosaics."""
    if compression not in ['DEFLATE', 'LZW', 'ZSTD', 'NONE']:
        raise ValueError('Unsupported COG compression')
    if isinstance(blocksize, bool) or not isinstance(blocksize, int) or blocksize < 128 or blocksize > 4096 or blocksize % 16:
        raise ValueError('blocksize must be a multiple of 16 between 128 and 4096')
    _algorithm(overview_resampling)
    result = gdal_api.Translate(str(target), source, options=gdal_api.TranslateOptions(
        format='COG', width=width, height=height, resampleAlg=resampling, outputSRS=assign_srs,
        callback=callback, overviewLevel=None if use_source_overviews else 'NONE',
        creationOptions=[f'COMPRESS={compression}', f'BLOCKSIZE={blocksize}',
        f'OVERVIEW_RESAMPLING={overview_resampling.upper()}', f'NUM_THREADS={num_threads}',
        'OVERVIEWS=AUTO' if use_source_overviews else 'OVERVIEWS=IGNORE_EXISTING']))
    if result is None:
        raise ValueError('GDAL COG conversion failed')
    result.FlushCache()
    result = None


def _cog(dataset, compression='DEFLATE', blocksize=512, overview_resampling='nearest'):
    path = _path()
    translate_cog(dataset, path, compression, blocksize, overview_resampling)
    result = _raster(path)
    validation = validate_cog(result)
    if not validation['valid']:
        raise ValueError('Generated COG failed structural validation')
    return result


def _encode_raster(input_raster, profile, compression, blocksize, overview_resampling, jpeg_quality=90):
    dataset = _open(input_raster)
    if profile not in ('cog', 'geotiff', 'png', 'jpeg'):
        raise ValueError('profile must be geotiff, cog, png or jpeg')
    budget = (_disk_budget(_raster_disk_bytes(dataset, profile=profile, blocksize=blocksize), 'raster encoding')
              if _DISK_PROTECTED.get() else nullcontext())
    temporary = (gdal.config_option('CPL_TMPDIR', str(_WORKSPACE.get()), thread_local=True)
                 if _DISK_PROTECTED.get() else nullcontext())
    with budget, temporary:
        if profile in ('png', 'jpeg'):
            output = _raster(encode_image(dataset, _path(''), profile, jpeg_quality, _DISK_LANGUAGE.get()))
        elif profile == 'cog':
            output = _cog(dataset, compression, blocksize, overview_resampling)
        else:
            if compression not in ['DEFLATE', 'LZW', 'ZSTD', 'NONE']:
                raise ValueError('Unsupported TIFF compression')
            output = _translated(dataset, format='GTiff', creationOptions=['TILED=YES', f'COMPRESS={compression}', 'COPY_SRC_OVERVIEWS=YES'])
        return output


def raster_save(input_raster, access_plan, profile='geotiff', compression='DEFLATE', blocksize=512, overview_resampling='nearest', jpeg_quality=90):
    plan = require_target_plan({'access_plan': access_plan})
    image = profile in ('png', 'jpeg')
    target_format = profile if image else 'tiff'
    suffixes = {'png': ('.png',), 'jpeg': ('.jpg', '.jpeg')}.get(profile, ('.tif', '.tiff'))
    if image and (plan['target']['format'] != profile or plan['target']['kind'] != 'file'
                  or not plan['target']['name'].lower().endswith(suffixes)):
        raise image_export_error('target', _DISK_LANGUAGE.get())
    if plan['target']['format'] != target_format or not plan['target']['name'].lower().endswith(suffixes):
        raise ValueError('Raster target must be a TIFF file')
    output = _encode_raster(input_raster, profile, compression, blocksize, overview_resampling, jpeg_quality)
    facts = raster_info(output)
    size = output.path.stat().st_size
    published = publish_target_file(output.path, plan)
    # Location and credentials are caller-owned. Return output facts only.
    return {'artifact_type': 'raster', 'format': target_format, 'profile': profile,
            'size_bytes': size, 'uploaded_files': published['uploaded_files'], **facts,
            **({'lossy': profile == 'jpeg', **({'jpeg_quality': jpeg_quality} if profile == 'jpeg' else {})} if image else {})}


def raster_save_tiles(input_tiles, access_plan, profile='geotiff', compression='DEFLATE', blocksize=512, overview_resampling='nearest'):
    facts = raster_tiles_info(input_tiles)
    plan = require_target_plan({'access_plan': access_plan})
    if plan['target']['kind'] != 'directory' or plan['target']['format'] != 'tiff':
        raise ValueError('Raster tiles require a TIFF directory target')
    if profile not in ('geotiff', 'cog') or compression not in ('DEFLATE', 'LZW', 'ZSTD', 'NONE'):
        raise ValueError('Unsupported raster tile profile or compression')
    if profile == 'cog':
        if isinstance(blocksize, bool) or not isinstance(blocksize, int) or not 128 <= blocksize <= 4096 or blocksize % 16:
            raise ValueError('blocksize must be a multiple of 16 between 128 and 4096')
        _algorithm(overview_resampling)
    result = {'artifact_type': 'raster_tiles', **facts, 'completed_tiles': 0, 'tiles': {}}
    key, publication = '', 'not_attempted'
    try:
        if plan['target']['write_mode'] == 'create':
            require_empty_target_directory(plan)
        dataset = _open(input_tiles.source)
        for row in range(0, facts['source_height'], input_tiles.tile_height):
            for column in range(0, facts['source_width'], input_tiles.tile_width):
                key, publication = f'r{row}_c{column}', 'not_attempted'
                window = [column, row, min(input_tiles.tile_width, facts['source_width'] - column),
                          min(input_tiles.tile_height, facts['source_height'] - row)]
                tile = encoded = encoded_dataset = None
                try:
                    # Per-thread GDAL config keeps the file self-contained on Linux as well.
                    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK', 'YES'):
                        tile = _translated(dataset, format='GTiff', srcWin=window, overviewLevel='NONE')
                        encoded = _encode_raster(tile, profile, compression, blocksize, overview_resampling)
                    saved = {'artifact_type': 'raster', 'format': 'tiff', 'profile': profile,
                             'size_bytes': encoded.path.stat().st_size, **raster_info(encoded)}
                    encoded_dataset = _open(encoded)
                    if any(Path(path).resolve() != encoded.path.resolve() for path in encoded_dataset.GetFileList() or []):
                        raise ValueError('Raster tile must be self-contained')
                    encoded_dataset = None
                    publication = 'unknown'
                    published = publish_target_file(encoded.path, target_child_file_plan(plan, key + '.tif'))
                    result['tiles'][key] = {'name': key + '.tif', 'window': window,
                                            'uploaded_files': published['uploaded_files'], **saved}
                    result['completed_tiles'] += 1
                finally:
                    encoded_dataset = None
                    for raster in (tile, encoded):
                        if raster is not None:
                            raster.path.unlink(missing_ok=True)
                            Path(str(raster.path) + '.msk').unlink(missing_ok=True)
                            Path(str(raster.path) + '.aux.xml').unlink(missing_ok=True)
        return result
    except Exception as exc:
        if key in result['tiles']:
            # Cleanup cannot revoke a confirmed write or make it unknown.
            next_index = result['completed_tiles']
            key = f'r{next_index // facts["columns"] * input_tiles.tile_height}_c{next_index % facts["columns"] * input_tiles.tile_width}'
            publication = 'not_attempted'
        if result['completed_tiles'] < facts['total_tiles']:
            result.update({'failed_tile': key, 'publication': publication})
        raise WorkflowExecutionError('Raster tile batch save failed', partial_result=result) from exc


def raster_to_cog(access_plan, options=None):
    plan = require_access_plan({'access_plan': access_plan})
    if options is not None and not isinstance(options, dict):
        raise ValueError('options must be an object')
    options = dict(options or {})
    allowed = {'source_crs', 'compression', 'blocksize', 'overview_resampling'}
    if set(options) - allowed:
        raise ValueError('Unknown COG options')
    with raster_workspace():
        source = raster_load({'schema_version': plan['schema_version'], 'source': plan['source']}, options.pop('source_crs', ''))
        return raster_save(source, {'schema_version': plan['schema_version'], 'target': plan['target']}, profile='cog', **options)


def raster_build_overviews(input_raster, levels=None, resampling='nearest'):
    levels = [2, 4, 8, 16] if levels is None else levels
    if not isinstance(levels, list) or not levels or len(levels) > 16 or any(isinstance(v, bool) or not isinstance(v, int) or v <= 1 for v in levels) or levels != sorted(set(levels)):
        raise ValueError('levels must be increasing unique integers greater than one')
    result = _translated(_open(input_raster), format='GTiff', creationOptions=['TILED=YES'])
    dataset = gdal.Open(str(result.path), gdal.GA_Update)
    if dataset.BuildOverviews(_algorithm(resampling).upper(), levels) != 0:
        raise ValueError('Overview generation failed')
    dataset = None
    return result


def raster_reproject(input_raster, target_crs, resolution=None, resampling='nearest'):
    kwargs = {'dstSRS': _crs(target_crs).ExportToWkt(), 'resampleAlg': _algorithm(resampling)}
    if resolution is not None:
        kwargs['xRes'], kwargs['yRes'] = _pair(resolution, 'resolution')
    return _warped([_open(input_raster)], **kwargs)


def raster_resample(input_raster, size=None, resolution=None, resampling='nearest'):
    if (size is None) == (resolution is None):
        raise ValueError('Specify exactly one of size or resolution')
    dataset = _open(input_raster)
    if size is not None:
        width, height = _pair(size, 'size', integer=True)
        return _resized(dataset, width, height, _algorithm(resampling))
    xres, yres = _pair(resolution, 'resolution')
    return _warped([dataset], xRes=xres, yRes=yres, resampleAlg=_algorithm(resampling))


def _polygon(corners):
    ring = ogr.Geometry(ogr.wkbLinearRing)
    for x, y in corners:
        ring.AddPoint_2D(x,y)
    ring.CloseRings()
    shape = ogr.Geometry(ogr.wkbPolygon)
    shape.AddGeometry(ring)
    return shape


def _require_clip_intersection(dataset, shape):
    footprint = _polygon(_corners(dataset))
    footprint.AssignSpatialReference(_crs(dataset.GetProjection()))
    intersection = shape.Intersection(footprint)
    if intersection is None or intersection.GetDimension() < 2 or intersection.GetArea() <= 0:
        raise ValueError('Clip boundary does not intersect raster with positive area')


def _clip_shape(geometry, crs, dataset):
    if not isinstance(geometry, dict) or geometry.get('type') not in ['Polygon', 'MultiPolygon']:
        raise ValueError('geometry must be a GeoJSON Polygon or MultiPolygon')
    shape = ogr.CreateGeometryFromJson(json.dumps(geometry))
    if shape is None or shape.IsEmpty() or not shape.IsValid():
        raise ValueError('Invalid clip geometry')
    shape.AssignSpatialReference(crs)
    try:
        transform_error = shape.TransformTo(_crs(dataset.GetProjection()))
    except RuntimeError as error:
        raise ValueError('Clip geometry cannot be transformed to raster CRS') from error
    if transform_error != 0:
        raise ValueError('Clip geometry cannot be transformed to raster CRS')
    return shape


def _clip_bounds(bbox, crs, dataset):
    if not isinstance(bbox, list) or len(bbox) != 4 or any(isinstance(v, bool) or not isinstance(v, (int, float)) or not math.isfinite(v) for v in bbox) or bbox[0] >= bbox[2] or bbox[1] >= bbox[3]:
        raise ValueError('bbox must be finite [xmin, ymin, xmax, ymax]')
    try:
        transformed = osr.CoordinateTransformation(crs, _crs(dataset.GetProjection())).TransformBounds(*bbox, 21)
    except RuntimeError as error:
        raise ValueError('Clip bbox cannot be transformed to raster CRS') from error
    if not all(math.isfinite(v) for v in transformed) or transformed[0] >= transformed[2] or transformed[1] >= transformed[3]:
        raise ValueError('Clip bbox cannot be transformed to raster CRS')
    return transformed


def _clip_outside(dataset, shape):
    width, height, transform = _grid(dataset)
    indices = _data_indices(dataset)
    for index in range(1, dataset.RasterCount + 1):
        _band(dataset, index)
    # File-backed coverage retains the exact source grid, including rotation.
    mask = gdal.GetDriverByName('GTiff').Create(
        str(_path()), width, height, 1, gdal.GDT_Byte, ['TILED=YES', 'COMPRESS=DEFLATE'])
    mask.SetGeoTransform(transform)
    mask.SetProjection(dataset.GetProjection())
    mask.GetRasterBand(1).Fill(0)
    boundary = ogr.GetDriverByName('Memory').CreateDataSource('')
    layer = boundary.CreateLayer('boundary', _crs(dataset.GetProjection()), ogr.wkbUnknown)
    feature = ogr.Feature(layer.GetLayerDefn())
    feature.SetGeometry(shape)
    layer.CreateFeature(feature)
    if gdal.RasterizeLayer(mask, [1], layer, burn_values=[1]) != 0:
        raise ValueError('Clip boundary rasterization failed')
    feature = layer = boundary = None
    output = _numeric_output(dataset, len(indices) + 1)
    alpha = output.GetRasterBand(len(indices) + 1)
    alpha.SetColorInterpretation(gdal.GCI_AlphaBand)
    for output_index, source_index in enumerate(indices, 1):
        band = output.GetRasterBand(output_index)
        for x, y, arrays, masks in _blocks(dataset, [source_index]):
            values = arrays[0].copy()
            outside = _read_values(mask.GetRasterBand(1), x, y, values.shape[1], values.shape[0]) == 0
            values[~(masks[0] & outside)] = np.nan
            band.WriteRaster(x, y, values.shape[1], values.shape[0], values.tobytes(), buf_type=gdal.GDT_Float64)
    # Overall alpha must not replace independent per-band NoData validity.
    for y in range(0, height, 512):
        for x in range(0, width, 512):
            span_x, span_y = min(512, width-x), min(512, height-y)
            coverage = np.zeros((span_y, span_x), dtype=bool)
            for index in range(1, len(indices) + 1):
                coverage |= np.isfinite(_read_values(output.GetRasterBand(index), x, y, span_x, span_y))
            alpha.WriteRaster(x, y, span_x, span_y, (coverage*255).astype(np.float64).tobytes(), buf_type=gdal.GDT_Float64)
    path = output.GetDescription()
    alpha = band = output = mask = None
    return _raster(path)


def raster_clip(input_raster, boundary_crs, bbox=None, geometry=None, mode='inside'):
    if mode not in ('inside', 'outside'):
        raise ValueError('mode must be inside or outside')
    if (bbox is None) == (geometry is None):
        raise ValueError('Specify exactly one of bbox or geometry')
    crs = _crs(boundary_crs)
    dataset = _open(input_raster)
    _require_georeferencing(dataset)
    if bbox is not None:
        transformed = _clip_bounds(bbox, crs, dataset)
        if mode == 'outside':
            return _clip_outside(dataset, _polygon([
                (transformed[0], transformed[1]), (transformed[2], transformed[1]),
                (transformed[2], transformed[3]), (transformed[0], transformed[3]),
            ]))
        extent = raster_info(input_raster)['extent']
        bounds = [max(transformed[0], extent[0]), max(transformed[1], extent[1]), min(transformed[2], extent[2]), min(transformed[3], extent[3])]
        if bounds[0] >= bounds[2] or bounds[1] >= bounds[3]:
            raise ValueError('Clip boundary does not intersect raster')
        _require_clip_intersection(dataset, _polygon([
            (bounds[0],bounds[1]), (bounds[2],bounds[1]),
            (bounds[2],bounds[3]), (bounds[0],bounds[3]),
        ]))
        return _warped([dataset], outputBounds=bounds)
    shape = _clip_shape(geometry, crs, dataset)
    if mode == 'outside':
        return _clip_outside(dataset, shape)
    _require_clip_intersection(dataset, shape)
    cutline_path = _path('.geojson')
    driver = ogr.GetDriverByName('GeoJSON')
    cutline = driver.CreateDataSource(str(cutline_path))
    layer = cutline.CreateLayer('boundary', _crs(dataset.GetProjection()), ogr.wkbUnknown)
    feature = ogr.Feature(layer.GetLayerDefn())
    feature.SetGeometry(shape)
    layer.CreateFeature(feature)
    feature = layer = cutline = None
    for index in range(1, dataset.RasterCount + 1):
        _band(dataset, index)
    return _warped([dataset], cutlineDSName=str(cutline_path), cropToCutline=True, dstAlpha=True,
                   outputType=gdal.GDT_Float64, dstNodata=float('nan'))


def raster_mosaic(input_rasters, target_crs, resolution, overlap='last', resampling='nearest'):
    if not isinstance(input_rasters, list) or len(input_rasters) < 2:
        raise ValueError('input_rasters must contain at least two raster ports')
    if overlap not in ['first', 'last']:
        raise ValueError('overlap must be first or last')
    datasets = [_open(raster) for raster in input_rasters]
    if any(dataset.RasterCount != datasets[0].RasterCount for dataset in datasets[1:]):
        raise ValueError('Mosaic inputs must have the same band count')
    for dataset in datasets:
        for index in range(1, dataset.RasterCount + 1):
            _band(dataset, index)
    # GDAL ignores an explicit source mask when source NoData is also present.
    # Encode that coverage per band before warping, using the same bounded
    # validity calculation as other numeric raster composition operators.
    datasets = [_mosaic_masked_source(dataset) for dataset in datasets]
    if overlap == 'first':
        datasets.reverse()
    xres, yres = _pair(resolution, 'resolution')
    return _warped(datasets, dstSRS=_crs(target_crs).ExportToWkt(), xRes=xres, yRes=yres,
                   targetAlignedPixels=True, resampleAlg=_algorithm(resampling), outputType=gdal.GDT_Float64, dstNodata=float('nan'))


def _mosaic_masked_source(dataset):
    indices = list(range(1, dataset.RasterCount + 1))
    if not any(dataset.GetRasterBand(index).GetMaskFlags() & (gdal.GMF_ALL_VALID | gdal.GMF_NODATA | gdal.GMF_ALPHA) == 0
               for index in indices):
        return dataset
    output = _copy_data_bands([(dataset, indices)])
    for index in indices:
        band = output.GetRasterBand(index)
        interpretation = dataset.GetRasterBand(index).GetColorInterpretation()
        band.SetColorInterpretation(interpretation)
        if interpretation == gdal.GCI_AlphaBand:
            for x, y, arrays, _ in _blocks(output, [index]):
                values = arrays[0].copy()
                values[~np.isfinite(values)] = 0
                band.WriteRaster(x,y,values.shape[1],values.shape[0],values.tobytes(),buf_type=gdal.GDT_Float64)
    band = None
    output.FlushCache()
    return output


def _band(dataset, index):
    if isinstance(index, bool) or not isinstance(index, int) or index < 1 or index > dataset.RasterCount:
        raise ValueError('band must be a valid one-based index')
    band = dataset.GetRasterBand(index)
    if gdal.DataTypeIsComplex(band.DataType):
        raise ValueError('Complex-valued bands are not supported by numeric analysis')
    return band


def _read_values(band, x, y, width, height):
    return np.frombuffer(band.ReadRaster(x,y,width,height,buf_type=gdal.GDT_Float64),
                         dtype=np.float64).reshape(height,width)


def _blocks(dataset, indices, *, window=None, native=False):
    bands = [_band(dataset, index) for index in indices]
    alpha = None
    for index in range(1,dataset.RasterCount+1):
        candidate = dataset.GetRasterBand(index)
        if candidate.GetColorInterpretation() == gdal.GCI_AlphaBand:
            alpha = candidate
            break
    start_x, start_y, span_x, span_y = window or (0, 0, dataset.RasterXSize, dataset.RasterYSize)
    end_x, end_y = start_x + span_x, start_y + span_y
    for y in range(start_y, end_y, 512):
        for x in range(start_x, end_x, 512):
            width, height = min(512, end_x - x), min(512, end_y - y)
            coverage = None
            if alpha is not None:
                values = _read_values(alpha,x,y,width,height)
                coverage = np.isfinite(values) & (values > 0)
            arrays, masks = [], []
            for band in bands:
                array = band.ReadAsArray(x, y, width, height) if native else _read_values(band,x,y,width,height)
                mask = (np.frombuffer(band.GetMaskBand().ReadRaster(x, y, width, height, buf_type=gdal.GDT_Byte), dtype=np.uint8).reshape(height, width) != 0) & np.isfinite(array)
                if coverage is not None and band.GetColorInterpretation() != gdal.GCI_AlphaBand:
                    mask &= coverage
                nodata = band.GetNoDataValue()
                if nodata is not None:
                    mask &= array != nodata
                arrays.append(array)
                masks.append(mask)
            yield x, y, arrays, masks


def _footprint_parts(geometry):
    parts = []
    pending = [make_valid(geometry)]
    while pending:
        current = pending.pop()
        if current.is_empty:
            continue
        if current.geom_type == 'Polygon':
            parts.append(current)
        elif current.geom_type in ('MultiPolygon', 'GeometryCollection'):
            pending.extend(current.geoms)
    return parts


def raster_footprint(input_raster, bands=None, validity='any'):
    if validity not in ('any', 'all'):
        raise ValueError('validity must be any or all')
    dataset = _open(input_raster)
    width, height, transform = _grid(dataset)
    data_indices = _data_indices(dataset)
    if bands is None:
        bands = data_indices
    if (not isinstance(bands, list) or not bands or
        any(isinstance(index, bool) or not isinstance(index, int) or index not in data_indices for index in bands) or
        len(set(bands)) != len(bands)):
        raise ValueError('bands must contain distinct one-based data band indices')
    for index in range(1, dataset.RasterCount + 1):
        if dataset.GetRasterBand(index).GetColorInterpretation() == gdal.GCI_AlphaBand:
            _band(dataset, index)
    mask = gdal.GetDriverByName('GTiff').Create(
        str(_path()), width, height, 1, gdal.GDT_Byte, ['TILED=YES', 'COMPRESS=DEFLATE'])
    mask.SetGeoTransform(transform)
    mask.SetProjection(dataset.GetProjection())
    mask_band = mask.GetRasterBand(1)
    valid_count = 0
    for y in range(0, height, 512):
        for x in range(0, width, 512):
            span_x, span_y = min(512, width-x), min(512, height-y)
            coverage = np.full((span_y, span_x), validity == 'all', dtype=bool)
            for index in bands:
                valid = next(_blocks(dataset, [index], window=(x, y, span_x, span_y)))[3][0]
                if validity == 'any':
                    coverage |= valid
                else:
                    coverage &= valid
            valid_count += int(np.count_nonzero(coverage))
            mask_band.WriteRaster(x, y, span_x, span_y, coverage.astype(np.uint8).tobytes(), buf_type=gdal.GDT_Byte)
    mask.FlushCache()
    vector = ogr.GetDriverByName('GPKG').CreateDataSource(str(_path('.gpkg')))
    layer = vector.CreateLayer('footprint', _crs(dataset.GetProjection()), ogr.wkbPolygon)
    if gdal.Polygonize(mask_band, mask_band, layer, -1, []) != 0:
        raise ValueError('Footprint polygonization failed')
    polygons = []
    for feature in layer:
        polygons.extend(_footprint_parts(from_wkb(bytes(feature.GetGeometryRef().ExportToWkb()))))
    feature = layer = vector = mask_band = mask = None
    polygons = _footprint_parts(MultiPolygon(polygons)) if polygons else []
    if valid_count and not polygons:
        raise ValueError('Raster grid cannot represent a polygonal footprint')
    result = gpd.GeoDataFrame(geometry=[MultiPolygon(polygons)] if polygons else [], crs=dataset.GetProjection())
    dataset = None
    return result


def _rasterize_value(value):
    if isinstance(value, (bool, np.bool_)) or not isinstance(value, Real):
        raise ValueError('Rasterization values must be finite real numbers')
    if isinstance(value, Integral) and abs(int(value)) > 2**53 - 1:
        raise ValueError('Rasterization integers must be within the Float64 safe integer range')
    number = float(value)
    if not math.isfinite(number):
        raise ValueError('Rasterization values must be finite real numbers')
    if number.is_integer() and abs(number) > 2**53 - 1:
        raise ValueError('Rasterization integers must be within the Float64 safe integer range')
    return number


_SAMPLE_POINTS_MESSAGES = {
    'gdf': ('点位栅格采样需要具有活动几何列的GeoDataFrame', 'Raster point sampling requires a GeoDataFrame with an active geometry column'),
    'geometry': ('点位栅格采样仅接受非空、有效、有限XY的二维Point', 'Raster point sampling requires non-empty valid finite XY two-dimensional Points'),
    'columns': ('点位栅格采样不允许重复列名或覆盖sample_value／sample_status列', 'Raster point sampling forbids duplicate columns or overwriting sample_value/sample_status'),
    'crs': ('点位与栅格必须具有相同的明确CRS', 'Points and raster must have the same explicit CRS'),
    'grid': ('点位栅格采样需要有限可逆的完整仿射网格', 'Raster point sampling requires a complete finite invertible affine grid'),
    'band': ('点位栅格采样必须显式选择有效的一基数据波段', 'Raster point sampling requires an explicit valid one-based data band'),
    'alpha': ('点位栅格采样不能选择alpha作为数据波段', 'Raster point sampling cannot select alpha as data'),
    'complex': ('点位栅格采样不支持所选复数数据或复数alpha', 'Raster point sampling does not support selected complex data or complex alpha'),
    'numeric': ('有效采样值无法由Float64精确表示', 'A valid sampled value cannot be represented exactly as Float64'),
    'method': ('点位栅格采样方法必须为cell或bilinear', 'Raster point sampling method must be cell or bilinear'),
    'result': ('双线性采样结果超出Float64可表示范围', 'Bilinear sampled result is outside the representable Float64 range'),
}


def _sample_points_error(code):
    return ValueError(_SAMPLE_POINTS_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _sample_point_groups(geometries, width, height, transform, method):
    """Exact affine coordinates and positive support grouped by native read block."""
    origin_x, a, b, origin_y, c, d = map(Fraction, transform)
    determinant = a*d - b*c
    if not determinant:
        raise _sample_points_error('grid')
    groups, required = {}, [-1]*len(geometries)
    for position, point in enumerate(geometries):
        x, y = Fraction(point.x)-origin_x, Fraction(point.y)-origin_y
        col, row = (d*x-b*y)/determinant, (a*y-c*x)/determinant
        if not (0 <= col < width and 0 <= row < height):
            continue
        if method == 'cell':
            support = [(math.floor(col), math.floor(row), Fraction(1))]
        else:
            col, row = col-Fraction(1, 2), row-Fraction(1, 2)
            left, top = math.floor(col), math.floor(row)
            dx, dy = col-left, row-top
            support = [(left+i, top+j, wx*wy) for j, wy in enumerate((1-dy, dy))
                       for i, wx in enumerate((1-dx, dx)) if wx*wy > 0]
        required[position] = 0
        if any(not (0 <= c < width and 0 <= r < height) for c, r, _ in support):
            continue
        required[position] = len(support)
        for col, row, weight in support:
            block = (col//512*512, row//512*512)
            groups.setdefault(block, []).append((position, col-block[0], row-block[1], weight))
    return groups, required


def raster_sample_points(input_raster, input_gdf, band, method='cell'):
    """Keep every Point; sample stored cells or exact centre-based bilinear support."""
    if not isinstance(method, str) or method not in ('cell', 'bilinear'):
        raise _sample_points_error('method')
    if not isinstance(input_gdf, gpd.GeoDataFrame):
        raise _sample_points_error('gdf')
    try:
        geometries = input_gdf.geometry
    except AttributeError as cause:
        raise _sample_points_error('gdf') from cause
    if not input_gdf.columns.is_unique or any(name in input_gdf.columns for name in ('sample_value', 'sample_status')):
        raise _sample_points_error('columns')
    for point in geometries:
        if (point is None or point.is_empty or point.geom_type != 'Point' or has_z(point) or has_m(point)
                or not point.is_valid or not np.isfinite(get_coordinates(point)).all()):
            raise _sample_points_error('geometry')
    dataset = _open(input_raster)
    try:
        try:
            if input_gdf.crs is None or not _crs(input_gdf.crs.to_wkt()).IsSame(_crs(dataset.GetProjection())):
                raise _sample_points_error('crs')
        except (ValueError, RuntimeError) as cause:
            raise _sample_points_error('crs') from cause
        transform = dataset.GetGeoTransform(can_return_null=True)
        if transform is None or len(transform) != 6 or not all(math.isfinite(value) for value in transform):
            raise _sample_points_error('grid')
        if type(band) is not int or not 1 <= band <= dataset.RasterCount:
            raise _sample_points_error('band')
        if dataset.GetRasterBand(band).GetColorInterpretation() == gdal.GCI_AlphaBand:
            raise _sample_points_error('alpha')
        indices = [band]
        for index in range(1, dataset.RasterCount+1):
            candidate = dataset.GetRasterBand(index)
            if index != band and candidate.GetColorInterpretation() != gdal.GCI_AlphaBand:
                continue
            if gdal.DataTypeIsComplex(candidate.DataType):
                raise _sample_points_error('complex')
            if index != band:
                indices.append(index)
        candidate = None
        groups, required = _sample_point_groups(geometries, dataset.RasterXSize, dataset.RasterYSize, transform, method)
        values = [None]*len(geometries)
        statuses = ['outside' if count < 0 else 'nodata' for count in required]
        received, inexact = [0]*len(geometries), [False]*len(geometries)
        totals = [Fraction(0)]*len(geometries)
        for (x, y), points in sorted(groups.items()):
            width, height = min(512, dataset.RasterXSize-x), min(512, dataset.RasterYSize-y)
            for _, _, arrays, masks in _blocks(dataset, indices, window=(x, y, width, height), native=True):
                valid = masks[0].copy()
                for alpha, coverage in zip(arrays[1:], masks[1:]):
                    valid &= coverage & (alpha > 0)
                for position, col, row, weight in points:
                    if not valid[row, col]:
                        continue
                    original = arrays[0][row, col]
                    number = float(original)
                    if not math.isfinite(number) or (np.issubdtype(arrays[0].dtype, np.integer) and int(number) != int(original)):
                        inexact[position] = True
                    received[position] += 1
                    if not math.isfinite(number):
                        continue
                    if method == 'cell':
                        values[position] = number
                    else:
                        totals[position] += Fraction(number)*weight
        for position, count in enumerate(required):
            if count <= 0 or received[position] != count:
                continue
            if inexact[position]:
                raise _sample_points_error('numeric')
            if method == 'bilinear':
                try:
                    number = float(totals[position])
                except OverflowError as cause:
                    raise _sample_points_error('result') from cause
                if not math.isfinite(number) or (number == 0 and totals[position] != 0):
                    raise _sample_points_error('result')
                values[position] = number
            statuses[position] = 'ok'
        result = input_gdf.copy()
        result['sample_value'] = pd.array(values, dtype='Float64')
        result['sample_status'] = pd.array(statuses, dtype='string')
        return result
    finally:
        candidate = dataset = None


_SAMPLE_LINES_MESSAGES = {
    'method': ('沿线栅格采样方法必须为cell或bilinear', 'Raster line sampling method must be cell or bilinear'),
    'spacing': ('沿线采样间距须为可精确表示的有限正实数米值，不能为布尔值', 'Line sampling spacing must be a representable finite positive real number of metres, not a boolean'),
    'gdf': ('沿线栅格采样需要具有唯一活动几何列的GeoDataFrame', 'Raster line sampling requires a GeoDataFrame with one active geometry column'),
    'geometry': ('沿线栅格采样仅接受非空、有效、有限XY的二维非零长度LineString', 'Raster line sampling requires non-empty valid finite XY nonzero-length two-dimensional LineStrings'),
    'columns': ('沿线采样结果字段不能覆盖原字段或活动几何列', 'Line sampling result columns must not overwrite source fields or active geometry'),
    'id': ('沿线采样须显式指定现有非几何、非空且唯一的标量编号字段', 'Line sampling requires an explicit existing non-geometry, non-null unique scalar identifier field'),
    'crs': ('线与栅格须具有相同明确的二维投影CRS及相同已知有限正水平线性单位', 'Lines and raster require the same explicit two-dimensional projected CRS with matching known finite positive horizontal linear units'),
    'numeric': ('沿线采样长度、里程或坐标超出有限Float64范围，非零距离及不同里程须可区分', 'Line sampling length, chainage or coordinates exceed finite Float64 range; nonzero distances and distinct chainages must remain representable'),
}


def _sample_lines_error(code):
    return ValueError(_SAMPLE_LINES_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _line_sample_positions(line, spacing, units):
    # Round each exact squared XY difference times the CRS unit factor to a
    # Float64 metre length once; accumulate those segment facts as exact ratios.
    # Sample positions use exact binary spacing multiples, without an epsilon.
    coordinates = list(line.coords)
    segments, total = [], Fraction(0)
    for start, end in zip(coordinates, coordinates[1:]):
        dx, dy = Fraction(end[0])-Fraction(start[0]), Fraction(end[1])-Fraction(start[1])
        squared = (dx*dx+dy*dy)*units*units
        if not squared:
            continue
        length = Fraction(_sqrt_fraction(squared, error=lambda: _sample_lines_error('numeric')))
        segments.append((start, end, total, length)); total += length
    if not segments:
        raise _sample_lines_error('geometry')
    try:
        endpoint = float(total)
    except OverflowError as cause:
        raise _sample_lines_error('numeric') from cause
    if not math.isfinite(endpoint) or endpoint <= 0:
        raise _sample_lines_error('numeric')
    quotient = total/spacing
    interior = (quotient.numerator-1)//quotient.denominator
    # This is an index/Float64 distinguishability bound, not a workload quota.
    if interior+2 > np.iinfo(np.intp).max or interior > (1 << (np.finfo(np.float64).nmant+1)):
        raise _sample_lines_error('numeric')
    if interior and float(interior*spacing) >= endpoint:
        raise _sample_lines_error('numeric')
    if interior > 1 and float((interior-1)*spacing) >= float(interior*spacing):
        raise _sample_lines_error('numeric')
    samples, cursor, previous = [], 0, -1.
    for index in range(interior+2):
        distance = total if index == interior+1 else index*spacing
        chainage = float(distance)
        if not math.isfinite(chainage) or chainage <= previous:
            raise _sample_lines_error('numeric')
        previous = chainage
        if index == 0:
            xy = coordinates[0]
        elif index == interior+1:
            xy = coordinates[-1]
        else:
            while distance > segments[cursor][2]+segments[cursor][3]:
                cursor += 1
            start, end, origin, length = segments[cursor]
            fraction = (distance-origin)/length
            xy = tuple(float((1-fraction)*Fraction(start[axis])+fraction*Fraction(end[axis])) for axis in (0, 1))
        if not all(math.isfinite(value) for value in xy):
            raise _sample_lines_error('numeric')
        samples.append((chainage, Point(xy)))
    return samples


def raster_sample_lines(input_raster, input_gdf, band, id_field, spacing_metres, method='cell'):
    """Generate metre chainages, then use the sole existing point sampler."""
    if not isinstance(method, str) or method not in ('cell', 'bilinear'):
        raise _sample_lines_error('method')
    try:
        spacing = float(spacing_metres)
        if (isinstance(spacing_metres, (bool, np.bool_)) or not isinstance(spacing_metres, Real)
                or not math.isfinite(spacing) or spacing <= 0 or spacing != spacing_metres):
            raise _sample_lines_error('spacing')
    except (TypeError, ValueError, OverflowError) as cause:
        raise _sample_lines_error('spacing') from cause
    if not isinstance(input_gdf, gpd.GeoDataFrame):
        raise _sample_lines_error('gdf')
    try:
        geometries = input_gdf.geometry
    except AttributeError as cause:
        raise _sample_lines_error('gdf') from cause
    if not input_gdf.columns.is_unique or sum(isinstance(dtype, gpd.array.GeometryDtype) for dtype in input_gdf.dtypes) != 1:
        raise _sample_lines_error('gdf')
    if any(name in input_gdf.columns for name in ('distance_m', 'sample_value', 'sample_status')):
        raise _sample_lines_error('columns')
    _raster_feature_ids(input_gdf, id_field, _sample_lines_error)
    for line in geometries:
        if (line is None or line.is_empty or line.geom_type != 'LineString' or has_z(line) or has_m(line)
                or not line.is_valid or not np.isfinite(get_coordinates(line)).all()):
            raise _sample_lines_error('geometry')
    dataset = _open(input_raster)
    try:
        reference = _crs(dataset.GetProjection())
        crs = input_gdf.crs
        axes = [] if crs is None else crs.axis_info
        if (crs is None or not crs.is_projected or crs.is_compound or len(axes) != 2
                or not reference.IsProjected() or reference.IsCompound() or reference.GetAxesCount() != 2
                or not _crs(crs.to_wkt()).IsSame(reference)
                or any(not math.isfinite(axis.unit_conversion_factor) or axis.unit_conversion_factor <= 0
                       or axis.unit_name.strip().lower() in ('', 'unknown') for axis in axes)
                or axes[0].unit_conversion_factor != axes[1].unit_conversion_factor):
            raise _sample_lines_error('crs')
        units = Fraction(axes[0].unit_conversion_factor)
    except (ValueError, RuntimeError, TypeError, AttributeError) as cause:
        raise _sample_lines_error('crs') from cause
    finally:
        dataset = None
    parents, distances, points = [], [], []
    for position, line in enumerate(geometries):
        for distance, point in _line_sample_positions(line, Fraction(spacing), units):
            parents.append(position); distances.append(distance); points.append(point)
    generated = input_gdf.iloc[parents].copy().reset_index(drop=True)
    generated[geometries.name] = gpd.GeoSeries(points, index=generated.index, crs=input_gdf.crs)
    generated['distance_m'] = pd.array(distances, dtype='Float64')
    return raster_sample_points(input_raster, generated, band, method=method)


def _raster_feature_ids(input_gdf, id_field, error):
    from .geometric_operators import _source_identifiers
    try:
        return _source_identifiers(input_gdf, id_field)
    except (ValueError, TypeError) as cause:
        raise error('id') from cause


def _polygon_geometries(input_gdf):
    if not isinstance(input_gdf, gpd.GeoDataFrame):
        raise ValueError('input_gdf must be a GeoDataFrame')
    try:
        geometries = input_gdf.geometry
    except AttributeError as error:
        raise ValueError('input_gdf must have an active geometry column') from error
    for geometry in geometries:
        if (geometry is None or geometry.is_empty or geometry.geom_type not in ('Polygon', 'MultiPolygon') or
                has_z(geometry) or has_m(geometry) or not geometry.is_valid or
                not np.isfinite(get_coordinates(geometry)).all()):
            raise ValueError('Raster polygon analysis requires valid non-empty finite two-dimensional Polygon or MultiPolygon geometry')
    return geometries


_IDW_MAX_POINTS_UNSET = object()
_IDW_MESSAGES = {
    'power': ('IDW距离幂次必须为非布尔整数1、2或3', 'IDW distance power must be the integer 1, 2 or 3, excluding booleans'),
    'max_points': ('IDW最多支撑点数必须为非布尔正整数，省略时不限', 'IDW maximum support count must be a positive integer, excluding booleans; omit it for unlimited support'),
    'point_counts': ('IDW最多支撑点数不得小于最少支撑点数', 'IDW maximum support count must not be less than minimum support count'),
    'min_points': ('IDW最少支撑点数必须为非布尔正整数', 'IDW minimum support count must be a positive integer, excluding booleans'),
    'radius': ('IDW搜索半径必须为有限正米数且不能为布尔值', 'IDW search radius must be a finite positive number of metres, excluding booleans'),
    'gdf': ('IDW需要具有活动几何列且列名唯一的GeoDataFrame', 'IDW requires a GeoDataFrame with an active geometry column and unique columns'),
    'field': ('IDW必须显式选择一个现有非几何数值字段', 'IDW requires an explicit existing non-geometry numeric field'),
    'geometry': ('IDW仅接受非空、有效、有限XY的二维Point', 'IDW requires non-empty valid finite XY two-dimensional Points'),
    'duplicate': ('IDW不允许重复XY坐标，包括相同数值的重复点', 'IDW forbids duplicate XY coordinates, including duplicate points with equal values'),
    'value': ('IDW样本必须为有限非布尔实数，整数须能由Float64精确表示', 'IDW samples must be finite non-boolean real numbers with integers exactly representable as Float64'),
    'crs': ('IDW点和模板必须具有相同的明确二维投影CRS及相同已知水平线性单位', 'IDW points and template must share an explicit two-dimensional projected CRS with equal known horizontal linear units'),
    'grid': ('IDW模板必须具有有限可逆的完整仿射网格', 'IDW template requires a complete finite invertible affine grid'),
    'write': ('IDW栅格块写入失败', 'IDW raster block write failed'),
    'result': ('IDW插值结果超出Float64可表示范围', 'IDW interpolation result is outside the representable Float64 range'),
}


def _idw_error(code):
    return ValueError(_IDW_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _point_radius(radius_metres, error):
    try:
        if (isinstance(radius_metres, (bool, np.bool_)) or not isinstance(radius_metres, Real)
                or radius_metres <= 0 or not math.isfinite(float(radius_metres))):
            raise error('radius')
        return Fraction(float(radius_metres))
    except (OverflowError, TypeError) as cause:
        raise error('radius') from cause


def _point_geometries(input_gdf, error):
    if not isinstance(input_gdf, gpd.GeoDataFrame) or not input_gdf.columns.is_unique:
        raise error('gdf')
    try:
        return input_gdf.geometry
    except AttributeError as cause:
        raise error('gdf') from cause


def _point_xy(point, error):
    if (point is None or point.is_empty or point.geom_type != 'Point' or has_z(point) or has_m(point)
            or not point.is_valid or not np.isfinite(get_coordinates(point)).all()):
        raise error('geometry')
    return Fraction(point.x), Fraction(point.y)


def _point_grid_spec(dataset, crs, error):
    try:
        reference = _crs(dataset.GetProjection())
        axes = [] if crs is None else crs.axis_info
        if (crs is None or not crs.is_projected or crs.is_compound or len(axes) != 2
                or not reference.IsProjected() or reference.IsCompound() or reference.GetAxesCount() != 2
                or not _crs(crs.to_wkt()).IsSame(reference)
                or any(not math.isfinite(axis.unit_conversion_factor) or axis.unit_conversion_factor <= 0
                       or axis.unit_name.strip().lower() in ('', 'unknown') for axis in axes)
                or axes[0].unit_conversion_factor != axes[1].unit_conversion_factor):
            raise error('crs')
        factor = Fraction(axes[0].unit_conversion_factor)
    except (ValueError, RuntimeError, TypeError, AttributeError) as cause:
        raise error('crs') from cause
    transform = dataset.GetGeoTransform(can_return_null=True)
    if transform is None or len(transform) != 6 or not all(math.isfinite(v) for v in transform):
        raise error('grid')
    transform = tuple(map(Fraction, transform))
    if not transform[1]*transform[5]-transform[2]*transform[4]:
        raise error('grid')
    return transform, factor


def _circle_index_bound(value, upper):
    """Outward rounding only for the index box; the actual circle is exact."""
    limit = np.finfo(np.float64).max
    try:
        result = float(value)
    except OverflowError:
        return limit if value > 0 else -limit
    if not math.isfinite(result):
        return limit if value > 0 else -limit
    rounded_inward = Fraction(result) < value if upper else Fraction(result) > value
    if rounded_inward:
        result = math.nextafter(result, math.inf if upper else -math.inf)
    return max(-limit, min(limit, result))



def _circle_sites(x, y, radius, tree, coordinates):
    candidates = tree.query(box(_circle_index_bound(x-radius, False), _circle_index_bound(y-radius, False),
                                _circle_index_bound(x+radius, True), _circle_index_bound(y+radius, True)))
    limit = radius*radius
    for index in candidates:
        px, py = coordinates[index]
        squared = (px-x)**2 + (py-y)**2
        if squared <= limit:
            yield index, squared


def _point_grid_output(dataset, transform, value_at, error, *, unit=''):
    output = destination = None
    output_path = None; completed = False
    try:
        origin_x, a, b, origin_y, c, d = transform
        width, height = dataset.RasterXSize, dataset.RasterYSize
        output = _numeric_output(dataset, 1); output_path = Path(output.GetDescription())
        destination = output.GetRasterBand(1)
        if unit:
            destination.SetUnitType(unit)
        for top in range(0, height, 512):
            for left in range(0, width, 512):
                span_x, span_y = min(512, width-left), min(512, height-top)
                result = np.full((span_y, span_x), np.nan)
                for row in range(span_y):
                    yy = Fraction(2*(top+row)+1, 2)
                    for column in range(span_x):
                        xx = Fraction(2*(left+column)+1, 2)
                        result[row,column] = value_at(origin_x+a*xx+b*yy, origin_y+c*xx+d*yy)
                if destination.WriteRaster(left, top, span_x, span_y, result.tobytes(), buf_type=gdal.GDT_Float64) != gdal.CE_None:
                    raise error('write')
        destination = None; output.FlushCache(); output = None
        result = _raster(output_path); completed = True
        return result
    finally:
        destination = output = None
        if output_path is not None and not completed:
            output_path.unlink(missing_ok=True)


def _idw_float(exact):
    value = float(exact)
    if not math.isfinite(value) or (exact and value == 0):
        raise _idw_error('result')
    return value


def _idw_radical_add(groups, squared, value, power):
    # q_i/q_j is a rational square exactly when their inverse-root weights
    # belong to the same radical class. No integer factorization is needed.
    for group in groups:
        ratio = group[0] / squared
        n, d = math.isqrt(ratio.numerator), math.isqrt(ratio.denominator)
        if n*n == ratio.numerator and d*d == ratio.denominator:
            scale = Fraction(n, d) ** power
            group[1] += value * scale
            group[2] += scale
            return
    groups.append([squared, value, Fraction(1)])


def _rational_sqrt_bounds(squared, bits):
    # Relative binary bounds on sqrt(q); even tiny or huge q never passes
    # through a floating point root, reciprocal or intermediate weight.
    n, d = squared.numerator, squared.denominator
    shift = bits - (n.bit_length() - d.bit_length()) // 2
    scaled_n, scaled_d = (n << (2*shift), d) if shift >= 0 else (n, d << (-2*shift))
    root = math.isqrt(scaled_n // scaled_d)
    lower = Fraction(root, 1 << shift) if shift >= 0 else Fraction(root << -shift)
    upper = lower if root*root*scaled_d == scaled_n else (Fraction(root+1, 1 << shift) if shift >= 0 else Fraction((root+1) << -shift))
    return lower, upper


def _idw_inverse_root_bounds(squared, power, bits):
    lower, upper = _rational_sqrt_bounds(squared, bits)
    factor = squared ** (power // 2)
    return 1/(upper*factor), 1/(lower*factor)


def _idw_radical_mean(groups, power):
    # Distinct rational-square classes are linearly independent over Q.
    # A rational quotient exists iff all class numerator/denominator ratios
    # agree; this recognizes exact cancellation and binary halfway ties.
    exact = groups[0][1] / groups[0][2]
    if all(numerator == exact*denominator for _, numerator, denominator in groups):
        return _idw_float(exact)
    bits = 64
    while True:
        numerator_lower = numerator_upper = denominator_lower = denominator_upper = Fraction(0)
        for squared, numerator, denominator in groups:
            low, high = _idw_inverse_root_bounds(squared, power, bits)
            numerator_lower += numerator*(low if numerator >= 0 else high)
            numerator_upper += numerator*(high if numerator >= 0 else low)
            denominator_lower += denominator*low
            denominator_upper += denominator*high
        corners = [n/d for n in (numerator_lower, numerator_upper) for d in (denominator_lower, denominator_upper)]
        endpoints = []
        for endpoint in (min(corners), max(corners)):
            try:
                endpoints.append(float(endpoint))
            except OverflowError:
                endpoints.append(-math.inf if endpoint < 0 else math.inf)
        low, high = endpoints
        if low == high:
            if low == 0 or not math.isfinite(low):
                raise _idw_error('result')
            return low
        bits *= 2


def _idw_value(x, y, radius, tree, coordinates, values, min_points, max_points, power=2):
    def nearby():
        for index, squared in _circle_sites(x, y, radius, tree, coordinates):
            px, py = coordinates[index]
            yield squared, px, py, values[index]
    support = nearby()
    if max_points is not _IDW_MAX_POINTS_UNSET:
        support = heapq.nsmallest(max_points, support, key=lambda sample: sample[:3])
    total, weight_sum = Fraction(0), Fraction(0)
    groups = []
    support_count = 0
    for squared, px, py, value in support:
        if not squared:
            return float(value)
        support_count += 1
        if power == 2:
            weight = 1 / squared
            total += value * weight
            weight_sum += weight
        else:
            _idw_radical_add(groups, squared, value, power)
    if support_count < min_points:
        return float('nan')
    return _idw_float(total / weight_sum) if power == 2 else _idw_radical_mean(groups, power)


def raster_interpolate_idw(input_gdf, reference_raster, field, radius_metres, min_points=1, max_points=_IDW_MAX_POINTS_UNSET, power=2):
    """IDW powers 1/2/3; shared exact radius and nearest distance/X/Y selection."""
    if isinstance(power, (bool, np.bool_)) or not isinstance(power, Integral) or power not in (1, 2, 3):
        raise _idw_error('power')
    power = int(power)
    if isinstance(min_points, (bool, np.bool_)) or not isinstance(min_points, Integral) or min_points <= 0:
        raise _idw_error('min_points')
    min_points = int(min_points)
    if max_points is not _IDW_MAX_POINTS_UNSET:
        if isinstance(max_points, (bool, np.bool_)) or not isinstance(max_points, Integral) or max_points <= 0:
            raise _idw_error('max_points')
        max_points = int(max_points)
        if max_points < min_points:
            raise _idw_error('point_counts')
    radius_metres = _point_radius(radius_metres, _idw_error)
    geometries = _point_geometries(input_gdf, _idw_error)
    if not isinstance(field, str) or not field.strip() or field not in input_gdf.columns or field == geometries.name:
        raise _idw_error('field')
    coordinates, values, seen = [], [], set()
    for point, raw in zip(geometries, input_gdf[field]):
        coordinate = _point_xy(point, _idw_error)
        if coordinate in seen:
            raise _idw_error('duplicate')
        seen.add(coordinate)
        try:
            if isinstance(raw, (bool, np.bool_)) or not isinstance(raw, Real):
                raise _idw_error('value')
            number = float(raw)
            if not math.isfinite(number) or (isinstance(raw, Integral) and int(number) != int(raw)):
                raise _idw_error('value')
        except (OverflowError, TypeError) as cause:
            raise _idw_error('value') from cause
        coordinates.append(coordinate); values.append(Fraction(number))
    dataset = None
    try:
        dataset = _open(reference_raster)
        transform, factor = _point_grid_spec(dataset, input_gdf.crs, _idw_error)
        radius = radius_metres / factor
        tree = STRtree(list(geometries))
        return _point_grid_output(dataset, transform,
            lambda x, y: _idw_value(x, y, radius, tree, coordinates, values, min_points, max_points, power), _idw_error)
    finally:
        dataset = None


_DENSITY_WEIGHT_UNSET = object()
_POINT_DENSITY_MESSAGES = {
    'weight_field': ('点密度权重字段必须显式选择现有非几何数值列名', 'Point density weight field must name an existing non-geometry numeric column'),
    'weight': ('点密度权重必须为有限非布尔非负实数，缺失值不可跳过', 'Point density weights must be finite non-boolean nonnegative real numbers; missing values cannot be skipped'),
    'method': ('点密度方法必须为uniform或quartic', 'Point density method must be uniform or quartic'),
    'radius': ('点密度搜索半径必须为有限非布尔正米数', 'Point density search radius must be a finite positive number of metres, excluding booleans'),
    'gdf': ('点密度需要列名唯一且具有活动几何列的GeoDataFrame', 'Point density requires a GeoDataFrame with unique columns and an active geometry column'),
    'geometry': ('点密度仅接受非空、有效、有限XY的二维Point', 'Point density requires non-empty valid finite XY two-dimensional Points'),
    'crs': ('点密度点与模板必须具有相同的明确二维投影CRS及相同已知水平线性单位', 'Point density points and template must share an explicit two-dimensional projected CRS with equal known horizontal linear units'),
    'grid': ('点密度模板必须具有有限可逆的完整仿射网格', 'Point density template requires a complete finite invertible affine grid'),
    'write': ('点密度栅格块写入失败', 'Point density raster block write failed'),
    'result': ('点密度结果超出有限正Float64可表示范围', 'Point density result is outside the finite positive Float64 range'),
}


def _point_density_error(code):
    return ValueError(_POINT_DENSITY_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _point_density_value(mass, area):
    if mass == 0:
        return 0.
    try:
        value = float(Fraction(mass) / area)
    except OverflowError as cause:
        raise _point_density_error('result') from cause
    if not math.isfinite(value) or value <= 0:
        raise _point_density_error('result')
    return value


def _density_weights(input_gdf, geometries, weight_field, error):
    if weight_field is _DENSITY_WEIGHT_UNSET:
        return None
    if (not isinstance(weight_field, str) or not weight_field.strip()
            or weight_field not in input_gdf.columns or weight_field == geometries.name):
        raise error('weight_field')
    weights = []
    for raw in input_gdf[weight_field].array:
        try:
            if isinstance(raw, (bool, np.bool_)) or not isinstance(raw, Real) or raw < 0:
                raise error('weight')
            if isinstance(raw, Integral):
                value = Fraction(int(raw))
            else:
                # The native ratio also proves a floating value is finite, without
                # narrowing tiny rationals or wider floating inputs to Float64.
                numerator, denominator = raw.as_integer_ratio()
                value = Fraction(numerator, denominator)
        except (OverflowError, ValueError, TypeError, AttributeError, ZeroDivisionError) as cause:
            raise error('weight') from cause
        weights.append(value)
    return weights


def _point_density_mass(sites, radius, method, weights=None):
    if method == 'uniform':
        return sum(1 for _ in sites) if weights is None else sum((weights[index] for index, _ in sites), Fraction(0))
    squared_radius = radius*radius
    # The ratio is dimensionless: sites and radius share the original CRS units.
    # Keep boundary cancellation exact, including a legitimate all-boundary zero.
    return 3*sum(((1 if weights is None else weights[index])*(1-squared/squared_radius)**2
                  for index, squared in sites), Fraction(0))


def raster_point_density(input_gdf, reference_raster, radius_metres, method='uniform', weight_field=_DENSITY_WEIGHT_UNSET):
    """Exact metric point density with optional unnormalized nonnegative weights."""
    if not isinstance(method, str) or method not in ('uniform', 'quartic'):
        raise _point_density_error('method')
    radius_metres = _point_radius(radius_metres, _point_density_error)
    geometries = _point_geometries(input_gdf, _point_density_error)
    coordinates = [_point_xy(point, _point_density_error) for point in geometries]
    weights = _density_weights(input_gdf, geometries, weight_field, _point_density_error)
    dataset = None
    try:
        dataset = _open(reference_raster)
        transform, factor = _point_grid_spec(dataset, input_gdf.crs, _point_density_error)
        radius = radius_metres / factor
        area = Fraction(math.pi)*radius_metres*radius_metres
        tree = STRtree(list(geometries))
        return _point_grid_output(dataset, transform,
            lambda x, y: _point_density_value(_point_density_mass(
                _circle_sites(x, y, radius, tree, coordinates), radius, method, weights), area),
            _point_density_error, unit='points/m2' if weights is None else 'weight/m2')
    finally:
        dataset = None


_LINE_DENSITY_MESSAGES = {
    'method': ('线密度方法必须为uniform或quartic', 'Line density method must be uniform or quartic'),
    'weight_field': ('线密度权重字段必须显式选择现有非几何数值列名', 'Line density weight field must name an existing non-geometry numeric column'),
    'weight': ('线密度权重必须为有限非布尔非负实数，缺失值不可跳过', 'Line density weights must be finite non-boolean nonnegative real numbers; missing values cannot be skipped'),
    'radius': ('线密度搜索半径必须为有限非布尔正米数', 'Line density search radius must be a finite positive number of metres, excluding booleans'),
    'gdf': ('线密度需要列名唯一且具有活动几何列的GeoDataFrame', 'Line density requires a GeoDataFrame with unique columns and an active geometry column'),
    'geometry': ('线密度仅接受非空、有效、有限XY且每个分量具有正长度的二维LineString或MultiLineString', 'Line density requires non-empty valid finite XY two-dimensional LineStrings or MultiLineStrings with positive length in every component'),
    'crs': ('线密度线与模板必须具有相同的明确二维投影CRS及相同已知水平线性单位', 'Line density lines and template must share an explicit two-dimensional projected CRS with equal known horizontal linear units'),
    'grid': ('线密度模板必须具有有限可逆的完整仿射网格', 'Line density template requires a complete finite invertible affine grid'),
    'write': ('线密度栅格块写入失败', 'Line density raster block write failed'),
    'result': ('线密度结果超出有限正Float64可表示范围', 'Line density result is outside the finite positive Float64 range'),
}


def _line_density_error(code):
    return ValueError(_LINE_DENSITY_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _line_density_parts(geometries):
    parts, coordinates, parents = [], [], []
    for position, geometry in enumerate(geometries):
        if (geometry is None or geometry.is_empty or geometry.geom_type not in ('LineString', 'MultiLineString')
                or has_z(geometry) or has_m(geometry) or not geometry.is_valid
                or not np.isfinite(get_coordinates(geometry)).all()):
            raise _line_density_error('geometry')
        for part in (geometry.geoms if geometry.geom_type == 'MultiLineString' else [geometry]):
            vertices = [tuple(map(Fraction, xy)) for xy in part.coords]
            if not any(a != b for a, b in zip(vertices, vertices[1:])):
                raise _line_density_error('geometry')
            parts.append(part); coordinates.append(vertices); parents.append(position)
    return parts, coordinates, parents


def _line_density_add(groups, squared, coefficient):
    if not coefficient or not squared:
        return
    n, d = math.isqrt(squared.numerator), math.isqrt(squared.denominator)
    if n*n == squared.numerator and d*d == squared.denominator:
        coefficient *= Fraction(n, d); squared = Fraction(1)
    for group in groups:
        ratio = squared/group[0]
        n, d = math.isqrt(ratio.numerator), math.isqrt(ratio.denominator)
        if n*n == ratio.numerator and d*d == ratio.denominator:
            group[1] += coefficient*Fraction(n, d)
            return
    groups.append([squared, coefficient])


def _line_density_segment(groups, start, end, x, y, radius_squared, weight, method='uniform'):
    # z=b+a*t parameterizes distance along the original segment. Intersect
    # [b,b+a] with [-sqrt(D),sqrt(D)] using exact signs/squares first.
    px, py = start[0]-x, start[1]-y
    vx, vy = end[0]-start[0], end[1]-start[1]
    a = vx*vx+vy*vy
    if not a or not weight:
        return
    b = px*vx+py*vy
    discriminant = a*radius_squared-(px*vy-py*vx)**2
    if discriminant <= 0:
        return
    z0, z1 = b, b+a
    if (z0 >= 0 and z0*z0 >= discriminant) or (z1 <= 0 and z1*z1 >= discriminant):
        return
    # Both methods share exact clipping. Quartic integrates (1-d²/R²)²
    # along original segments; P(±sqrt(D)) = ±8 D² sqrt(D)/15.
    def primitive(z):
        if method == 'uniform':
            return z
        return discriminant*discriminant*z - 2*discriminant*z**3/3 + z**5/5
    constant, roots = Fraction(0), 0
    if z1 < 0 or z1*z1 <= discriminant:
        constant += primitive(z1)
    else:
        roots += 1
    if z0 > 0 or z0*z0 <= discriminant:
        constant -= primitive(z0)
    else:
        roots += 1
    if method == 'uniform':
        rational_scale, radical_scale = 1/a, Fraction(1)
    else:
        denominator = a*a*radius_squared*radius_squared
        rational_scale = 1/(a*denominator)
        radical_scale = 8*discriminant*discriminant/(15*denominator)
    _line_density_add(groups, a, weight*constant*rational_scale)
    _line_density_add(groups, discriminant/a, weight*roots*radical_scale)


def _line_density_float(groups, scale):
    groups = [(q, c*scale) for q, c in groups if c]
    if not groups:
        return 0.
    # Distinct rational-square classes are independent: rational results and
    # exact Float64 halfway ties contain only the class of 1. Others converge.
    bits = 64
    while True:
        lower = upper = Fraction(0)
        for squared, coefficient in groups:
            low, high = _rational_sqrt_bounds(squared, bits)
            lower += coefficient*(low if coefficient >= 0 else high)
            upper += coefficient*(high if coefficient >= 0 else low)
        rounded = []
        for endpoint in (lower, upper):
            try:
                rounded.append(float(endpoint))
            except OverflowError:
                rounded.append(math.inf if endpoint > 0 else -math.inf)
        if rounded[0] == rounded[1]:
            if rounded[0] <= 0 or not math.isfinite(rounded[0]):
                raise _line_density_error('result')
            return rounded[0]
        bits *= 2


def raster_line_density(input_gdf, reference_raster, radius_metres, weight_field=_DENSITY_WEIGHT_UNSET, *, method='uniform'):
    """Analytic uniform or quartic line integral with unnormalized feature weights."""
    if not isinstance(method, str) or method not in ('uniform', 'quartic'):
        raise _line_density_error('method')
    radius_metres = _point_radius(radius_metres, _line_density_error)
    if not radius_metres:
        raise _line_density_error('radius')
    geometries = _point_geometries(input_gdf, _line_density_error)
    parts, coordinates, parents = _line_density_parts(geometries)
    weights = _density_weights(input_gdf, geometries, weight_field, _line_density_error)
    part_weights = [Fraction(1) if weights is None else weights[parent] for parent in parents]
    dataset = None
    try:
        dataset = _open(reference_raster)
        transform, factor = _point_grid_spec(dataset, input_gdf.crs, _line_density_error)
        radius = radius_metres/factor
        scale = (3 if method == 'quartic' else 1)*factor/(Fraction(math.pi)*radius_metres*radius_metres)
        tree = STRtree(parts)
        def value_at(x, y):
            groups = []
            candidates = tree.query(box(_circle_index_bound(x-radius, False), _circle_index_bound(y-radius, False),
                                        _circle_index_bound(x+radius, True), _circle_index_bound(y+radius, True)))
            for index in candidates:
                for start, end in zip(coordinates[index], coordinates[index][1:]):
                    _line_density_segment(groups, start, end, x, y, radius*radius, part_weights[index], method)
            return _line_density_float(groups, scale)
        return _point_grid_output(dataset, transform, value_at, _line_density_error,
                                  unit='metre/m2' if weights is None else 'weight/metre')
    finally:
        dataset = None


def vector_rasterize(input_gdf, reference_raster, field=None, burn_value=None, all_touched=False):
    geometries = _polygon_geometries(input_gdf)
    if type(all_touched) is not bool:
        raise ValueError('all_touched must be a boolean')
    if (field is None) == (burn_value is None):
        raise ValueError('Exactly one of field or burn_value is required')
    if field is not None:
        if (not isinstance(field, str) or not field.strip() or
                input_gdf.columns.to_list().count(field) != 1 or field == geometries.name):
            raise ValueError('field must name one numeric attribute column')
        values = [_rasterize_value(value) for value in input_gdf[field]]
    else:
        values = [_rasterize_value(burn_value)] * len(input_gdf)
    grid = _open(reference_raster)
    _grid(grid)
    if input_gdf.crs is None or not _crs(input_gdf.crs.to_wkt()).IsSame(_crs(grid.GetProjection())):
        raise ValueError('Vector and reference raster must have the same explicit CRS')
    vector = layer = feature = output = None
    try:
        vector = ogr.GetDriverByName('Memory').CreateDataSource('')
        layer = vector.CreateLayer('features', _crs(grid.GetProjection()), ogr.wkbUnknown)
        if layer.CreateField(ogr.FieldDefn('burn_value', ogr.OFTReal)) != 0:
            raise ValueError('Cannot create rasterization value field')
        for geometry, value in zip(geometries, values):
            feature = ogr.Feature(layer.GetLayerDefn())
            feature.SetGeometry(ogr.CreateGeometryFromWkb(geometry.wkb))
            feature.SetField('burn_value', value)
            if layer.CreateFeature(feature) != 0:
                raise ValueError('Cannot create rasterization feature')
            feature = None
        output = _numeric_output(grid, 1)
        output.GetRasterBand(1).Fill(float('nan'))
        options = ['ATTRIBUTE=burn_value', 'ALL_TOUCHED=' + ('TRUE' if all_touched else 'FALSE')]
        if values and gdal.RasterizeLayer(output, [1], layer, options=options) != 0:
            raise ValueError('GDAL vector rasterization failed')
        output.FlushCache()
        return _raster(output.GetDescription())
    finally:
        feature = layer = vector = output = grid = None


def _classification_values(values, valid):
    selected = values[valid]
    if np.issubdtype(values.dtype, np.floating):
        if np.any(np.abs(selected.astype(np.float64, copy=False)) > 2**53 - 1) or np.any(selected != np.trunc(selected)):
            raise ValueError('floating classification values must be exact safe integers')
    elif np.issubdtype(values.dtype, np.unsignedinteger):
        if np.any(selected > np.iinfo(np.int64).max):
            raise ValueError('classification values must fit signed Int64')
    return selected.astype(np.int64)


def _safe_classification_values(values, valid):
    selected = values[valid]
    if np.any(selected < -(2**53 - 1)) or np.any(selected > 2**53 - 1):
        raise ValueError('classification values must be exact safe integers')
    return _classification_values(values, valid)


def raster_polygonize(input_raster, band, connectivity=4):
    if type(connectivity) is not int or connectivity not in (4, 8):
        raise ValueError('connectivity must be integer 4 or 8')
    dataset = _open(input_raster)
    width, height, transform = _grid(dataset)
    if type(band) is not int or band not in _data_indices(dataset):
        raise ValueError('band must be a one-based data band index')
    for index in range(1, dataset.RasterCount + 1):
        if dataset.GetRasterBand(index).GetColorInterpretation() == gdal.GCI_AlphaBand:
            _band(dataset, index)
    classes = mask = vector = layer = feature = None
    category_ids, category_values = {}, []
    try:
        classes = gdal.GetDriverByName('GTiff').Create(
            str(_path()), width, height, 1, gdal.GDT_Int64, ['TILED=YES', 'COMPRESS=DEFLATE'])
        mask = gdal.GetDriverByName('GTiff').Create(
            str(_path()), width, height, 1, gdal.GDT_Byte, ['TILED=YES', 'COMPRESS=DEFLATE'])
        for target in (classes, mask):
            target.SetProjection(dataset.GetProjection())
            target.SetGeoTransform(transform)
        for x, y, arrays, masks in _blocks(dataset, [band], native=True):
            values, valid = arrays[0], masks[0]
            selected = _classification_values(values, valid)
            encoded = np.zeros(values.shape, dtype=np.int64)
            unique, inverse = np.unique(selected, return_inverse=True)
            identifiers = []
            for value in unique:
                code = int(value)
                if code not in category_ids:
                    category_values.append(code)
                    category_ids[code] = len(category_values)
                identifiers.append(category_ids[code])
            # GDAL versions can emit Int64 polygon fields via double. Dense
            # internal IDs keep original large classification codes lossless.
            encoded[valid] = np.asarray(identifiers, dtype=np.int64)[inverse]
            span_y, span_x = values.shape
            classes.GetRasterBand(1).WriteRaster(x, y, span_x, span_y, encoded.tobytes(), buf_type=gdal.GDT_Int64)
            mask.GetRasterBand(1).WriteRaster(x, y, span_x, span_y, valid.astype(np.uint8).tobytes(), buf_type=gdal.GDT_Byte)
        classes.FlushCache(); mask.FlushCache()
        vector = ogr.GetDriverByName('GPKG').CreateDataSource(str(_path('.gpkg')))
        layer = vector.CreateLayer('classes', _crs(dataset.GetProjection()), ogr.wkbPolygon)
        layer.CreateField(ogr.FieldDefn('class_value', ogr.OFTInteger64))
        options = ['8CONNECTED=8'] if connectivity == 8 else []
        if gdal.Polygonize(classes.GetRasterBand(1), mask.GetRasterBand(1), layer, 0, options) != 0:
            raise ValueError('Classification polygonization failed')
        codes, geometries = [], []
        for feature in layer:
            geometry = feature.GetGeometryRef()
            parts = _footprint_parts(from_wkb(bytes(geometry.ExportToWkb()))) if geometry else []
            if not parts:
                raise ValueError('Raster grid cannot represent classification region geometry')
            region = MultiPolygon(parts)
            if not region.is_valid or region.is_empty:
                raise ValueError('Classification region geometry must be valid')
            codes.append(category_values[feature.GetField('class_value') - 1])
            geometries.append(region)
        return gpd.GeoDataFrame({'class_value': np.asarray(codes, dtype=np.int64)},
                                geometry=geometries, crs=dataset.GetProjection())
    finally:
        feature = layer = vector = classes = mask = dataset = None


@dataclass
class _ZoneStatistics:
    total: int = 0
    count: int = 0
    minimum: float = math.inf
    maximum: float = -math.inf
    scale: float = 0.
    anchor: float | None = None
    mean: float = 0.
    m2: float = 0.
    sum: float = 0.
    correction: float = 0.

    def add(self, values, total):
        self.total += total
        n = int(values.size)
        if not n:
            return
        self.minimum = min(self.minimum, float(values.min()))
        self.maximum = max(self.maximum, float(values.max()))
        scale = max(self.scale, abs(self.minimum), abs(self.maximum))
        ratio = self.scale / scale if scale else 0.
        if self.anchor is None:
            self.anchor = float(values[0])
        # Subtract a fixed source value before scaling to retain small variation
        # on a large offset. Half differences also support opposite finite extremes.
        with np.errstate(over='ignore'):
            centered = values - self.anchor
        if np.isfinite(centered).all():
            centered /= scale or 1.
        else:
            centered = (values / 2 - self.anchor / 2) / (scale / 2)
        local_sum = math.fsum(values / (scale or 1.))
        local_mean = math.fsum(centered) / n
        delta = local_mean - self.mean * ratio
        count = self.count + n
        self.m2 = (self.m2 * ratio * ratio + float(np.square(centered - local_mean).sum())
                   + delta * delta * self.count * (n / count))
        self.mean = self.mean * ratio + delta * (n / count)
        previous = self.sum * ratio
        combined = previous + local_sum
        # Neumaier compensation retains small contributions across block cancellation.
        self.correction = (self.correction * ratio +
                           ((previous - combined) + local_sum if abs(previous) >= abs(local_sum)
                            else (local_sum - combined) + previous))
        self.sum, self.scale, self.count = combined, scale, count

    def summary(self):
        stats = [None] * 5
        if self.count:
            total = math.fsum([self.sum, self.correction]) * self.scale
            stats = [total, self.minimum, self.maximum, total / self.count,
                     math.sqrt(max(0., min(1., self.m2 / self.count))) * self.scale]
            if not all(math.isfinite(value) for value in stats):
                raise ValueError('Zonal statistics exceed the finite Float64 output range')
        return [self.total, self.count, self.total - self.count, *stats]


_ZONAL_WEIGHTS_UNSET = object()
_ZONAL_WEIGHT_MESSAGES = {
    'pair': ('weights_raster和weights_band必须同时显式提供且不能为null',
             'weights_raster and weights_band must be explicitly provided together and cannot be null'),
    'percentile': ('栅格分区统计的权重与percentiles参数不能同时提供',
                   'Zonal statistics weights and percentiles cannot be provided together'),
    'grid': ('权重栅格必须与数值和分区栅格具有相同尺寸、完整网格及语义等价CRS',
             'The weight raster must match the value and zone rasters in dimensions, complete grid and equivalent CRS'),
    'band': ('栅格分区权重必须显式选择有效的一基数据波段',
             'Zonal weights require a valid explicit one-based data band'),
    'complex': ('栅格分区权重不支持所选复数数据或复数alpha',
                'Zonal weights do not support selected complex data or complex alpha'),
    'alpha': ('栅格分区权重必须选择数据波段，不能选择alpha',
              'Zonal weights require a data band, not alpha'),
    'coverage': ('分区有效且数值有效的位置必须具有有效且有限的权重，不能补零',
                 'Every valid value in a valid zone must have a valid finite weight; missing weights cannot be filled with zero'),
    'negative': ('分区有效且数值有效的位置的权重必须非负',
                 'Weights at valid values in valid zones must be nonnegative'),
    'stddev_flag': ('include_weighted_stddev必须是布尔值',
                    'include_weighted_stddev must be a boolean'),
    'stddev_weights': ('启用加权总体标准差必须显式提供weights_raster和weights_band',
                       'Weighted population standard deviation requires explicit weights_raster and weights_band'),
    'numeric': ('栅格分区加权统计结果超出有限Float64范围',
                'Weighted zonal statistics exceed the finite Float64 output range'),
    'vector_percentile': ('矢量面分区统计的权重与percentiles参数不能同时提供',
                          'Vector zonal statistics weights and percentiles cannot be provided together'),
    'vector_grid': ('权重栅格必须与数值栅格具有相同尺寸、完整网格及语义等价CRS',
                    'The weight raster must match the value raster in dimensions, complete grid and equivalent CRS'),
    'vector_band': ('矢量面分区权重必须显式选择有效的一基数据波段',
                    'Vector zonal weights require a valid explicit one-based data band'),
    'vector_complex': ('矢量面分区权重不支持所选复数数据或复数alpha',
                       'Vector zonal weights do not support selected complex data or complex alpha'),
    'vector_alpha': ('矢量面分区权重必须选择数据波段，不能选择alpha',
                     'Vector zonal weights require a data band, not alpha'),
    'vector_coverage': ('面覆盖且数值有效的位置必须具有有效且有限的权重，不能补零',
                        'Every valid value covered by a zone must have a valid finite weight; missing weights cannot be filled with zero'),
    'vector_negative': ('面覆盖且数值有效的位置的权重必须非负',
                        'Weights at valid values covered by a zone must be nonnegative'),
    'vector_numeric': ('矢量面分区加权统计结果超出有限Float64范围',
                       'Weighted vector zonal statistics exceed the finite Float64 output range'),
}


def _zonal_weight_error(code):
    return ValueError(_ZONAL_WEIGHT_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _vector_zonal_weight_error(code):
    return _zonal_weight_error(code if code in ('pair', 'stddev_flag', 'stddev_weights') else 'vector_' + code)


def _zonal_weights_requested(weights_raster, weights_band, requested, error):
    weighted = weights_raster is not _ZONAL_WEIGHTS_UNSET or weights_band is not _ZONAL_WEIGHTS_UNSET
    if weighted:
        if (weights_raster is _ZONAL_WEIGHTS_UNSET or weights_band is _ZONAL_WEIGHTS_UNSET
                or weights_raster is None or weights_band is None):
            raise error('pair')
        if requested:
            raise error('percentile')
    return weighted


def _zonal_weights_source(dataset, weights_raster, weights_band, error):
    source = _open(weights_raster)
    try:
        _require_same_grid(dataset, source)
    except ValueError as cause:
        raise error('grid') from cause
    if type(weights_band) is not int or not 1 <= weights_band <= source.RasterCount:
        raise error('band')
    selected = source.GetRasterBand(weights_band)
    if gdal.DataTypeIsComplex(selected.DataType):
        raise error('complex')
    if selected.GetColorInterpretation() == gdal.GCI_AlphaBand:
        raise error('alpha')
    for index in range(1, source.RasterCount + 1):
        alpha = source.GetRasterBand(index)
        if alpha.GetColorInterpretation() == gdal.GCI_AlphaBand and gdal.DataTypeIsComplex(alpha.DataType):
            raise error('complex')
    return source


def _zonal_weight_block(source, band, window, required, error):
    _, _, arrays, valid = next(_blocks(source, [band], window=window, native=True))
    if np.any(required & ~valid[0]):
        raise error('coverage')
    if np.any(arrays[0][required] < 0):
        raise error('negative')
    return arrays[0]


def _dyadic_units(values):
    """Encode one bounded finite Float64 block as exact integers / 2**shift."""
    ratios = [float(value).as_integer_ratio() for value in values.flat]
    shift = max((denominator.bit_length() - 1 for _, denominator in ratios), default=0)
    units = np.fromiter((numerator << (shift - (denominator.bit_length() - 1))
                         for numerator, denominator in ratios), dtype=object, count=values.size).reshape(values.shape)
    return units, shift


@dataclass
class _ZoneWeights:
    mass: Fraction = Fraction(0)
    total: Fraction = Fraction(0)
    all_one: bool = True
    include_stddev: bool = False
    squares: Fraction = Fraction(0)

    def add(self, values, weights):
        if not values.size:
            return
        self.all_one = self.all_one and bool(np.all(weights == 1.))
        values_units, value_shift = _dyadic_units(values)
        weight_units, weight_shift = _dyadic_units(weights)
        self.mass += Fraction(sum(weight_units.flat), 1 << weight_shift)
        self.total += Fraction(sum((values_units * weight_units).flat), 1 << (value_shift + weight_shift))
        if self.include_stddev:
            self.squares += Fraction(sum((values_units ** 2 * weight_units).flat),
                                     1 << (2 * value_shift + weight_shift))

    def summary(self, original, *, error=_zonal_weight_error):
        if not original[1]:
            return [None] * (4 if self.include_stddev else 3)
        if not self.mass:
            return [0., 0., None, *([None] if self.include_stddev else [])]
        try:
            result = ([float(self.mass), original[3], original[6]] if self.all_one else
                      [float(self.mass), float(self.total), float(self.total / self.mass)])
        except OverflowError as cause:
            raise error('numeric') from cause
        if not all(math.isfinite(value) for value in result):
            raise error('numeric')
        if self.include_stddev:
            if self.all_one:
                deviation = original[7]
            else:
                # Exact moments avoid cancellation and squared intermediate overflow.
                variance = self.squares / self.mass - (self.total / self.mass) ** 2
                if variance < 0:
                    raise error('numeric')
                deviation = _sqrt_fraction(variance, error=lambda: error('numeric'), allow_rounded_zero=True)
            result.append(deviation)
        return result


_ZONAL_FREQUENCY_MESSAGES = {
    'method': ('栅格分区统计方法必须为summary或class_frequency', 'Zonal statistics method must be summary or class_frequency'),
    'conflict': ('分类频数模式不能提供percentiles或外部权重，也不能启用加权标准差',
                 'Class frequency cannot accept percentiles or external weights, or enable weighted standard deviation'),
    'grid': ('分类频数的两个栅格须具有相同尺寸、完整网格及明确等价CRS',
             'Class frequency rasters require identical dimensions, complete grids and explicit equivalent CRS'),
    'band': ('分类频数必须显式选择有效的一基分类波段', 'Class frequency requires an explicit valid one-based class band'),
    'zones_band': ('分类频数必须显式选择有效的一基分区波段', 'Class frequency requires an explicit valid one-based zone band'),
    'alpha': ('分类频数不能选择alpha作为数据波段', 'Class frequency cannot select alpha as data'),
    'complex': ('分类频数不支持所选复数数据或复数alpha', 'Class frequency does not support selected complex data or complex alpha'),
    'classes': ('分类频数的有效类别必须为±(2^53−1)内的精确整数',
                'Valid frequency classes must be exact integers within plus or minus (2^53-1)'),
    'zones': ('分类频数的有效分区须为有符号Int64，浮点编码须为精确安全整数',
              'Valid frequency zones must fit signed Int64, with exact safe integers for floating codes'),
    'numeric': ('分类频数计数超出有符号Int64范围', 'Class frequency counts exceed the signed Int64 range'),
}


def _zonal_frequency_error(code):
    return ValueError(_ZONAL_FREQUENCY_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _zonal_frequency_indices(dataset, band, role, error=_zonal_frequency_error):
    if type(band) is not int or not 1 <= band <= dataset.RasterCount:
        raise error(role)
    try:
        selected = _band(dataset, band)
        alphas = [index for index in range(1, dataset.RasterCount+1)
                  if dataset.GetRasterBand(index).GetColorInterpretation() == gdal.GCI_AlphaBand]
        for index in alphas:
            _band(dataset, index)
    except ValueError as cause:
        raise error('complex') from cause
    if selected.GetColorInterpretation() == gdal.GCI_AlphaBand:
        raise error('alpha')
    return [band, *alphas]


def _class_frequency_rows(total, classes, *, allow_empty=False, error=_zonal_frequency_error):
    valid = sum(classes.values())
    if (not (0 if allow_empty else 1) <= total <= np.iinfo(np.int64).max or valid > total
            or any(not 0 < count <= np.iinfo(np.int64).max for count in classes.values())):
        raise error('numeric')
    if classes:
        return [[code, count, float(Fraction(count, valid)), total, valid, total-valid]
                for code, count in sorted(classes.items())]
    return [[None, 0, None, total, 0, total]]


def _zonal_frequency_frame(totals, frequencies):
    columns = ['zone_value', 'class_value', 'count', 'proportion', 'total_count', 'valid_count', 'invalid_count']
    rows = [[zone, *row] for zone in sorted(totals)
            for row in _class_frequency_rows(totals[zone], frequencies.get(zone, {}))]
    result = pd.DataFrame(rows, columns=columns)
    return result.astype({name: 'Int64' if name == 'class_value' else 'Float64' if name == 'proportion' else 'int64'
                          for name in columns})


def _zonal_class_frequency(input_raster, zones_raster, band, zones_band):
    dataset = zones = None
    try:
        dataset, zones = _open(input_raster), _open(zones_raster)
        try:
            _require_same_grid(dataset, zones)
        except (ValueError, RuntimeError) as cause:
            raise _zonal_frequency_error('grid') from cause
        indices = _zonal_frequency_indices(dataset, band, 'band')
        zone_indices = _zonal_frequency_indices(zones, zones_band, 'zones_band')
        totals, frequencies = {}, {}
        for x, y, arrays, masks in _blocks(zones, zone_indices, native=True):
            zone_valid = masks[0].copy()
            for alpha, coverage in zip(arrays[1:], masks[1:]):
                zone_valid &= coverage & (alpha > 0)
            try:
                zone_codes = _classification_values(arrays[0], zone_valid)
            except ValueError as cause:
                raise _zonal_frequency_error('zones') from cause
            height, width = zone_valid.shape
            _, _, values, valid = next(_blocks(dataset, indices, window=(x, y, width, height), native=True))
            required = zone_valid & valid[0]
            for alpha, coverage in zip(values[1:], valid[1:]):
                required &= coverage & (alpha > 0)
            try:
                class_codes = _safe_classification_values(values[0], required)
            except ValueError as cause:
                raise _zonal_frequency_error('classes') from cause
            codes, counts = np.unique(zone_codes, return_counts=True)
            for code, count in zip(codes, counts):
                zone = int(code)
                totals[zone] = totals.get(zone, 0) + int(count)
            # Preserve integer identities in both roles; never combine them through Float64.
            pairs = np.empty(len(class_codes), dtype=[('zone', 'int64'), ('class', 'int64')])
            pairs['zone'], pairs['class'] = zone_codes[required[zone_valid]], class_codes
            members, counts = np.unique(pairs, return_counts=True)
            for member, count in zip(members, counts):
                bucket = frequencies.setdefault(int(member['zone']), {})
                code = int(member['class'])
                bucket[code] = bucket.get(code, 0) + int(count)
        return _zonal_frequency_frame(totals, frequencies)
    finally:
        dataset = zones = None


def raster_zonal_statistics(input_raster, zones_raster, band, zones_band, percentiles=_PERCENTILE_UNSET,
                            *, weights_raster=_ZONAL_WEIGHTS_UNSET, weights_band=_ZONAL_WEIGHTS_UNSET,
                            include_weighted_stddev=False, method='summary'):
    if type(include_weighted_stddev) is not bool:
        raise _zonal_weight_error('stddev_flag')
    if not isinstance(method, str) or method not in ('summary', 'class_frequency'):
        raise _zonal_frequency_error('method')
    if method == 'class_frequency':
        if (percentiles is not _PERCENTILE_UNSET or weights_raster is not _ZONAL_WEIGHTS_UNSET
                or weights_band is not _ZONAL_WEIGHTS_UNSET or include_weighted_stddev):
            raise _zonal_frequency_error('conflict')
        return _zonal_class_frequency(input_raster, zones_raster, band, zones_band)
    requested = percentiles is not _PERCENTILE_UNSET
    weighted = _zonal_weights_requested(weights_raster, weights_band, requested, _zonal_weight_error)
    if include_weighted_stddev and not weighted:
        raise _zonal_weight_error('stddev_weights')
    if requested:
        percentiles = _statistics_percentiles(percentiles)
    dataset, zones = _open(input_raster), _open(zones_raster)
    _require_same_grid(dataset, zones)
    for source, index in ((dataset, band), (zones, zones_band)):
        if _band(source, index).GetColorInterpretation() == gdal.GCI_AlphaBand:
            raise ValueError('Zonal statistics require data bands, not alpha')
        for i in range(1, source.RasterCount + 1):
            if source.GetRasterBand(i).GetColorInterpretation() == gdal.GCI_AlphaBand:
                _band(source, i)
    weights_source = _zonal_weights_source(dataset, weights_raster, weights_band, _zonal_weight_error) if weighted else None
    statistics, weighted_statistics = {}, {}
    with _percentile_runs(dataset, zonal=True) if requested else nullcontext() as runs:
        for x, y, arrays, masks in _blocks(zones, [zones_band], native=True):
            classes = _classification_values(arrays[0], masks[0])
            height, width = arrays[0].shape
            _, _, values, valid = next(_blocks(dataset, [band], window=(x, y, width, height), native=True))
            selected_valid = valid[0][masks[0]]
            selected_values = values[0][masks[0]].astype(np.float64)
            if weighted:
                mass_values = _zonal_weight_block(weights_source, weights_band, (x, y, width, height),
                                                  masks[0] & valid[0], _zonal_weight_error)
                selected_weights = mass_values[masks[0]].astype(np.float64)
            # Sort once per block; never scan the full block once per classification.
            order = np.argsort(classes, kind='stable')
            codes, starts, counts = np.unique(classes[order], return_index=True, return_counts=True)
            for code, start, count in zip(codes, starts, counts):
                group = order[start:start + count]
                statistics.setdefault(int(code), _ZoneStatistics()).add(
                    selected_values[group][selected_valid[group]], int(count))
                if weighted:
                    weighted_statistics.setdefault(int(code), _ZoneWeights(include_stddev=include_weighted_stddev)).add(
                        selected_values[group][selected_valid[group]], selected_weights[group][selected_valid[group]])
            if runs is not None:
                records = np.empty(int(selected_valid.sum()), dtype=_ZONAL_PERCENTILE_DTYPE)
                records['zone'] = classes[selected_valid]
                records['value'] = selected_values[selected_valid]
                runs.add(records)
        columns = ['zone_value', 'total_count', 'valid_count', 'invalid_count', 'sum', 'min', 'max', 'mean', 'stddev']
        rows = []
        for code in sorted(statistics):
            original = statistics[code].summary()
            rows.append([code, *original, *(weighted_statistics[code].summary(original) if weighted else [])])
        if weighted:
            columns += ['weight_sum', 'weighted_sum', 'weighted_mean']
            if include_weighted_stddev:
                columns.append('weighted_stddev')
        result = pd.DataFrame(rows, columns=columns)
        result = result.astype({name: 'int64' if i < 4 else 'Float64' for i, name in enumerate(columns)})
        if requested:
            values = _zonal_percentiles(runs, statistics, percentiles)
            for index, percentile in enumerate(percentiles, 1):
                result[f'percentile_{index}'] = pd.array([percentile] * len(result), dtype='Float64')
                result[f'percentile_value_{index}'] = pd.array(
                    [row[index-1] for row in values], dtype='Float64')
        return result


def _zone_window(dataset, bounds):
    """Conservative rotated-grid pixel bounds, clipped before inversion."""
    corners = _corners(dataset)
    if not all(math.isfinite(value) for point in corners for value in point):
        raise ValueError('Raster extent must be finite')
    left = max(bounds[0], min(x for x, y in corners))
    bottom = max(bounds[1], min(y for x, y in corners))
    right = min(bounds[2], max(x for x, y in corners))
    top = min(bounds[3], max(y for x, y in corners))
    if left > right or bottom > top:
        return None
    inverse = gdal.InvGeoTransform(dataset.GetGeoTransform())
    pixels = [gdal.ApplyGeoTransform(inverse, x, y) for x, y in
              ((left, bottom), (left, top), (right, bottom), (right, top))]
    if not all(math.isfinite(value) for point in pixels for value in point):
        raise ValueError('Zone bounds cannot be represented on the raster grid')
    # One-pixel margin retains GDAL edge coverage despite affine roundoff.
    x = max(0, math.floor(min(p[0] for p in pixels))-1)
    y = max(0, math.floor(min(p[1] for p in pixels))-1)
    end_x = min(dataset.RasterXSize, math.ceil(max(p[0] for p in pixels))+1)
    end_y = min(dataset.RasterYSize, math.ceil(max(p[1] for p in pixels))+1)
    return (x, y, end_x-x, end_y-y) if x < end_x and y < end_y else None



_VECTOR_AREA_COLUMNS = ('covered_area_m2', 'valid_area_m2', 'area_weighted_mean')
_VECTOR_AREA_MESSAGES = {
    'flag': ('include_area_weighted必须是布尔值', 'include_area_weighted must be a boolean'),
    'stddev_flag': ('include_area_weighted_stddev必须是布尔值', 'include_area_weighted_stddev must be a boolean'),
    'stddev_area': ('面积加权总体标准差须显式启用include_area_weighted',
                    'Area-weighted population standard deviation requires include_area_weighted enabled'),
    'conflict': ('矢量面分区面积加权与百分位、外部权重及加权标准差互斥',
                 'Vector zonal area weighting is mutually exclusive with percentiles, external weights and weighted standard deviation'),
    'units': ('矢量面分区面积加权要求已知线性单位的二维非复合投影CRS，请显式重投影',
              'Vector zonal area weighting requires a two-dimensional non-compound projected CRS with known linear units; reproject explicitly'),
    'geometry': ('矢量面分区面积交叠几何无法以有限有效坐标表示',
                 'Vector zonal area intersection geometry cannot be represented by finite valid coordinates'),
    'numeric': ('矢量面分区面积加权结果超出有限Float64范围',
                'Vector zonal area-weighted results exceed the finite Float64 output range'),
}


def _vector_area_error(code):
    return ValueError(_VECTOR_AREA_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _vector_area_scale(dataset, transform):
    reference = _crs(dataset.GetProjection())
    units = reference.GetLinearUnits()
    if (not reference.IsProjected() or reference.IsCompound() or reference.GetAxesCount() != 2
            or not math.isfinite(units) or units <= 0
            or reference.GetLinearUnitsName().lower() in ('unknown', '')):
        raise _vector_area_error('units')
    # Do not overflow or underflow a determinant/unit-square intermediate.
    a, b, c, d = (Fraction(v) for v in (transform[1], transform[2], transform[4], transform[5]))
    return abs(a*d-b*c) * Fraction(units)**2


def _vector_area_geometry(geometry, transform):
    from shapely.affinity import affine_transform
    from shapely.errors import GEOSException
    inverse = gdal.InvGeoTransform(transform)
    try:
        pixel = affine_transform(geometry, [inverse[1], inverse[2], inverse[4], inverse[5], inverse[0], inverse[3]])
        if (pixel.is_empty or not pixel.is_valid or not np.isfinite(get_coordinates(pixel)).all()
                or not math.isfinite(pixel.area) or pixel.area <= 0):
            raise _vector_area_error('geometry')
        return pixel
    except (ValueError, OverflowError, GEOSException) as cause:
        raise _vector_area_error('geometry') from cause


def _vector_area_fractions(geometry, x, y, shape):
    from shapely import box, intersection, area, is_valid
    from shapely.errors import GEOSException
    height, width = shape
    result = np.empty(height*width, dtype=np.float64)
    try:
        for start in range(0, result.size, 4096):
            indices = np.arange(start, min(start+4096, result.size))
            rows, columns = np.divmod(indices, width)
            cells = box(x+columns, y+rows, x+columns+1, y+rows+1)
            overlaps = intersection(cells, geometry)
            values = area(overlaps)
            if (not is_valid(overlaps).all() or not np.isfinite(values).all()
                    or np.any(values < 0) or np.any(values > 1)):
                raise _vector_area_error('geometry')
            result[start:start+len(indices)] = values
    except GEOSException as cause:
        raise _vector_area_error('geometry') from cause
    return result.reshape(shape)


@dataclass
class _VectorAreaStatistics:
    covered: Fraction = Fraction(0)
    weighted: _ZoneWeights = field(default_factory=_ZoneWeights)

    def add(self, values, valid, fractions):
        units, shift = _dyadic_units(fractions)
        self.covered += Fraction(sum(units.flat), 1 << shift)
        selected = valid & (fractions > 0)
        self.weighted.add(values[selected].astype(np.float64), fractions[selected])

    def summary(self, scale):
        try:
            values = [float(self.covered*scale), float(self.weighted.mass*scale),
                      float(self.weighted.total/self.weighted.mass) if self.weighted.mass else None]
        except OverflowError as cause:
            raise _vector_area_error('numeric') from cause
        if any(v is not None and not math.isfinite(v) for v in values):
            raise _vector_area_error('numeric')
        if self.weighted.include_stddev:
            deviation = None
            if self.weighted.mass:
                variance = self.weighted.squares/self.weighted.mass - (self.weighted.total/self.weighted.mass)**2
                if variance < 0:
                    raise _vector_area_error('numeric')
                deviation = _sqrt_fraction(variance, error=lambda: _vector_area_error('numeric'), allow_rounded_zero=True)
            values.append(deviation)
        return values


_VECTOR_ZONAL_ID_UNSET = object()
_VECTOR_FREQUENCY_MESSAGES = {
    'method': ('矢量分区统计方法必须为summary或class_frequency', 'Vector zonal statistics method must be summary or class_frequency'),
    'conflict': ('矢量分类频数不能提供百分位、外部权重或启用加权／面积模式', 'Vector class frequency cannot accept percentiles, external weights or enabled weighted/area modes'),
    'summary_id': ('summary方法不能提供id_field', 'The summary method cannot accept id_field'),
    'id': ('分类频数须显式选择现有非几何、非空唯一标量面编号列', 'Class frequency requires an explicit existing non-geometry unique non-null scalar zone identifier column'),
    'geometry': ('分类频数要求唯一几何列及有效非空有限二维面，允许空批', 'Class frequency requires one geometry column and valid non-empty finite XY polygons; empty batches are allowed'),
    'coverage': ('all_touched必须是布尔值', 'all_touched must be a boolean'),
    'prefix': ('field_prefix必须是字符串', 'field_prefix must be a string'),
    'columns': ('分类频数结果列不能覆盖原属性或活动几何', 'Class frequency result columns must not overwrite source attributes or active geometry'),
    'crs': ('分类频数的面与栅格必须具有相同明确CRS', 'Class frequency polygons and raster require the same explicit CRS'),
    'grid': ('分类频数栅格必须具有完整有限可逆网格', 'Class frequency raster requires a complete finite invertible grid'),
    'band': ('分类频数必须显式选择有效的一基分类波段', 'Class frequency requires an explicit valid one-based class band'),
    'alpha': ('分类频数不能选择alpha作为数据波段', 'Class frequency cannot select alpha as data'),
    'complex': ('分类频数不支持所选复数数据或复数alpha', 'Class frequency does not support selected complex data or complex alpha'),
    'classes': ('分类频数的有效类别必须为±(2^53−1)内的精确整数', 'Valid frequency classes must be exact integers within plus or minus (2^53-1)'),
    'numeric': ('分类频数计数超出有符号Int64范围', 'Class frequency counts exceed the signed Int64 range'),
}


def _vector_frequency_error(code):
    return ValueError(_VECTOR_FREQUENCY_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


@contextmanager
def _vector_zone_blocks(dataset, geometry, indices, all_touched, window):
    """One coverage path shared by numeric and categorical polygon statistics."""
    vector = layer = feature = coverage = iterator = None
    try:
        if window is None:
            yield iter(())
            return
        vector = ogr.GetDriverByName('Memory').CreateDataSource('')
        layer = vector.CreateLayer('zone', _crs(dataset.GetProjection()), ogr.wkbUnknown)
        feature = ogr.Feature(layer.GetLayerDefn())
        feature.SetGeometry(ogr.CreateGeometryFromWkb(geometry.wkb))
        if layer.CreateFeature(feature) != 0:
            raise ValueError('Cannot create zone feature')
        feature = None
        transform = dataset.GetGeoTransform()
        def blocks():
            nonlocal coverage
            for x, y, arrays, masks in _blocks(dataset, indices, window=window, native=True):
                height, width = arrays[0].shape
                coverage = gdal.GetDriverByName('MEM').Create('', width, height, 1, gdal.GDT_Byte)
                origin = gdal.ApplyGeoTransform(transform, x, y)
                coverage.SetGeoTransform((origin[0], transform[1], transform[2], origin[1], transform[4], transform[5]))
                coverage.SetProjection(dataset.GetProjection())
                coverage.GetRasterBand(1).Fill(0)
                layer.ResetReading()
                options = ['ALL_TOUCHED='+('TRUE' if all_touched else 'FALSE')]
                if gdal.RasterizeLayer(coverage, [1], layer, burn_values=[1], options=options) != 0:
                    raise ValueError('Zone coverage rasterization failed')
                covered = coverage.GetRasterBand(1).ReadAsArray() != 0
                yield x, y, arrays, masks, covered
                coverage = None
        iterator = blocks()
        yield iterator
    finally:
        if iterator is not None:
            iterator.close()
        feature = layer = vector = coverage = None


def _vector_zonal_class_frequency(input_raster, input_gdf, band, all_touched, field_prefix, id_field):
    try:
        geometries = _polygon_geometries(input_gdf)
        if (not input_gdf.columns.is_unique
                or sum(isinstance(dtype, gpd.array.GeometryDtype) for dtype in input_gdf.dtypes) != 1):
            raise ValueError('ambiguous geometry schema')
    except (ValueError, AttributeError) as cause:
        raise _vector_frequency_error('geometry') from cause
    _raster_feature_ids(input_gdf, id_field, _vector_frequency_error)
    if type(all_touched) is not bool:
        raise _vector_frequency_error('coverage')
    if not isinstance(field_prefix, str):
        raise _vector_frequency_error('prefix')
    suffixes = ('class_value', 'count', 'proportion', 'total_count', 'valid_count', 'invalid_count')
    names = [field_prefix+name for name in suffixes]
    if any(name in input_gdf.columns for name in names):
        raise _vector_frequency_error('columns')
    dataset = None
    try:
        dataset = _open(input_raster)
        try:
            if input_gdf.crs is None or not _crs(input_gdf.crs.to_wkt()).IsSame(_crs(dataset.GetProjection())):
                raise ValueError('CRS mismatch')
        except (ValueError, RuntimeError) as cause:
            raise _vector_frequency_error('crs') from cause
        try:
            _grid(dataset)
        except (ValueError, RuntimeError) as cause:
            raise _vector_frequency_error('grid') from cause
        indices = _zonal_frequency_indices(dataset, band, 'band', error=_vector_frequency_error)
        positions, rows = [], []
        for position, geometry in enumerate(geometries):
            total, classes = 0, {}
            window = _zone_window(dataset, geometry.bounds)
            with _vector_zone_blocks(dataset, geometry, indices, all_touched, window) as blocks:
                for _, _, arrays, masks, covered in blocks:
                    required = covered & masks[0]
                    for alpha, valid in zip(arrays[1:], masks[1:]):
                        required &= valid & (alpha > 0)
                    try:
                        values = _safe_classification_values(arrays[0], required)
                    except ValueError as cause:
                        raise _vector_frequency_error('classes') from cause
                    codes, counts = np.unique(values, return_counts=True)
                    for code, count in zip(codes, counts):
                        code = int(code)
                        classes[code] = classes.get(code, 0)+int(count)
                    total += int(covered.sum())
            members = _class_frequency_rows(total, classes, allow_empty=True, error=_vector_frequency_error)
            rows.extend(members)
            positions.extend([position]*len(members))
        result = input_gdf.iloc[positions].copy()
        for index, (name, suffix) in enumerate(zip(names, suffixes)):
            result[name] = pd.array([row[index] for row in rows], dtype='Int64' if suffix == 'class_value' else 'Float64' if suffix == 'proportion' else 'int64')
        return result
    finally:
        dataset = None


def raster_vector_zonal_statistics(input_raster, input_gdf, band, all_touched=False, field_prefix='zonal_', percentiles=_PERCENTILE_UNSET,
                                   *, weights_raster=_ZONAL_WEIGHTS_UNSET, weights_band=_ZONAL_WEIGHTS_UNSET,
                                   include_weighted_stddev=False, include_area_weighted=False, include_area_weighted_stddev=False,
                                   method='summary', id_field=_VECTOR_ZONAL_ID_UNSET):
    if type(include_weighted_stddev) is not bool:
        raise _vector_zonal_weight_error('stddev_flag')
    if type(include_area_weighted) is not bool:
        raise _vector_area_error('flag')
    if type(include_area_weighted_stddev) is not bool:
        raise _vector_area_error('stddev_flag')
    if not isinstance(method, str) or method not in ('summary', 'class_frequency'):
        raise _vector_frequency_error('method')
    if method == 'class_frequency':
        if (percentiles is not _PERCENTILE_UNSET or weights_raster is not _ZONAL_WEIGHTS_UNSET
                or weights_band is not _ZONAL_WEIGHTS_UNSET or include_weighted_stddev
                or include_area_weighted or include_area_weighted_stddev):
            raise _vector_frequency_error('conflict')
        return _vector_zonal_class_frequency(input_raster, input_gdf, band, all_touched, field_prefix, id_field)
    if id_field is not _VECTOR_ZONAL_ID_UNSET:
        raise _vector_frequency_error('summary_id')
    if include_area_weighted_stddev and not include_area_weighted:
        raise _vector_area_error('stddev_area')
    if include_area_weighted and (percentiles is not _PERCENTILE_UNSET
            or weights_raster is not _ZONAL_WEIGHTS_UNSET or weights_band is not _ZONAL_WEIGHTS_UNSET
            or include_weighted_stddev):
        raise _vector_area_error('conflict')
    requested = percentiles is not _PERCENTILE_UNSET
    weighted = _zonal_weights_requested(weights_raster, weights_band, requested, _vector_zonal_weight_error)
    if include_weighted_stddev and not weighted:
        raise _vector_zonal_weight_error('stddev_weights')
    if requested:
        percentiles = _statistics_percentiles(percentiles)
    geometries = _polygon_geometries(input_gdf)
    if type(all_touched) is not bool:
        raise ValueError('all_touched must be a boolean')
    if not isinstance(field_prefix, str):
        raise ValueError('field_prefix must be a string')
    names = [field_prefix+name for name in ('total_count', 'valid_count', 'invalid_count',
                                          'sum', 'min', 'max', 'mean', 'stddev')]
    if requested:
        names.extend(field_prefix+name for index in range(1,len(percentiles)+1) for name in (f'percentile_{index}',f'percentile_value_{index}'))
    if weighted:
        names.extend(field_prefix+name for name in ('weight_sum', 'weighted_sum', 'weighted_mean'))
        if include_weighted_stddev:
            names.append(field_prefix+'weighted_stddev')
    if include_area_weighted:
        names.extend(field_prefix+name for name in _VECTOR_AREA_COLUMNS)
        if include_area_weighted_stddev:
            names.append(field_prefix+'area_weighted_stddev')
    if any(name in input_gdf.columns for name in names):
        raise ValueError('Statistics columns must not overwrite input columns')
    dataset = _open(input_raster)
    _, _, transform = _grid(dataset)
    if input_gdf.crs is None or not _crs(input_gdf.crs.to_wkt()).IsSame(_crs(dataset.GetProjection())):
        raise ValueError('Vector zones and raster must have the same explicit CRS')
    if _band(dataset, band).GetColorInterpretation() == gdal.GCI_AlphaBand:
        raise ValueError('Vector zonal statistics require a data band, not alpha')
    for index in range(1, dataset.RasterCount+1):
        if dataset.GetRasterBand(index).GetColorInterpretation() == gdal.GCI_AlphaBand:
            _band(dataset, index)
    weights_source = _zonal_weights_source(dataset, weights_raster, weights_band, _vector_zonal_weight_error) if weighted else None
    area_scale = _vector_area_scale(dataset, transform) if include_area_weighted else None
    rows = []
    vector = layer = feature = coverage = None
    try:
        for geometry in geometries:
            statistics = _ZoneStatistics()
            area_statistics = _VectorAreaStatistics(weighted=_ZoneWeights(include_stddev=include_area_weighted_stddev)) if include_area_weighted else None
            area_geometry = _vector_area_geometry(geometry, transform) if include_area_weighted else None
            weighted_statistics = _ZoneWeights(include_stddev=include_weighted_stddev) if weighted else None
            window = _zone_window(dataset, geometry.bounds)
            with _percentile_runs(dataset, window=window) if requested and window is not None else nullcontext() as runs:
                with _vector_zone_blocks(dataset, geometry, [band], all_touched, window) as blocks:
                    for x, y, arrays, masks, covered in blocks:
                        height, width = arrays[0].shape
                        required = covered & masks[0]
                        values = arrays[0][required].astype(np.float64)
                        statistics.add(values, int(covered.sum()))
                        if area_statistics is not None:
                            fractions = _vector_area_fractions(area_geometry, x, y, arrays[0].shape)
                            area_statistics.add(arrays[0], masks[0], fractions)
                        if weighted:
                            masses = _zonal_weight_block(weights_source, weights_band, (x, y, width, height),
                                                         required, _vector_zonal_weight_error)
                            weighted_statistics.add(values, masses[required].astype(np.float64))
                        if runs is not None:
                            runs.add(values)
                row = statistics.summary()
                if weighted:
                    row.extend(weighted_statistics.summary(row, error=_vector_zonal_weight_error))
                if requested:
                    values = runs.values(statistics.count, percentiles) if runs is not None else [None]*len(percentiles)
                    row.extend(value for pair in zip(percentiles,values) for value in pair)
                if area_statistics is not None:
                    row.extend(area_statistics.summary(area_scale))
                rows.append(row)
    finally:
        feature = layer = vector = coverage = dataset = weights_source = None
    result = input_gdf.copy()
    for index, name in enumerate(names):
        result[name] = pd.array([row[index] for row in rows], dtype='int64' if index < 3 else 'Float64')
    return result


_PERCENTILE_MERGE_VALUES = 32768
_STATISTICS_MESSAGES = {
    'percentiles': ('percentiles必须为非空、严格递增且不重复的列表，各项为0至100之间的有限实数且不能为布尔值',
                    'percentiles must be a nonempty, strictly increasing list without duplicates of finite real numbers from 0 to 100, excluding booleans'),
    'io': ('整幅栅格百分位临时排序读写失败', 'Full-raster percentile temporary sorting I/O failed'),
    'zonal_io': ('栅格分区百分位临时排序读写失败', 'Raster zonal percentile temporary sorting I/O failed'),
    'vector_io': ('矢量面分区百分位临时排序读写失败', 'Vector zonal percentile temporary sorting I/O failed'),
    'range': ('整幅栅格统计结果超出有限Float64范围', 'Full-raster statistics exceed the finite Float64 output range'),
    'alpha': ('栅格统计不支持复数alpha', 'Raster statistics do not support complex alpha'),
}


def _statistics_error(code):
    return ValueError(_STATISTICS_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _merge_percentile_runs(left_path, right_path, output_path, dtype=np.dtype('float64')):
    """Merge sorted files with two fixed buffers; never map the whole raster."""
    with left_path.open('rb') as left, right_path.open('rb') as right, output_path.open('wb') as output:
        buffers = [np.empty(0, dtype=dtype), np.empty(0, dtype=dtype)]
        streams = [left, right]
        finished = [False, False]
        while True:
            for index, stream in enumerate(streams):
                if not buffers[index].size and not finished[index]:
                    buffers[index] = np.fromfile(stream, dtype=dtype, count=_PERCENTILE_MERGE_VALUES)
                    finished[index] = buffers[index].size < _PERCENTILE_MERGE_VALUES
            if not any(buffer.size for buffer in buffers):
                break
            if all(buffer.size for buffer in buffers):
                boundary = min((buffer[-1] for buffer in buffers), key=lambda item: item.tolist())
                cuts = [int(np.searchsorted(buffer, boundary, side='right')) for buffer in buffers]
            else:
                cuts = [buffer.size for buffer in buffers]
            prefix = np.concatenate([buffer[:cut] for buffer, cut in zip(buffers, cuts)])
            prefix.sort()
            prefix.tofile(output)
            buffers = [buffer[cut:] for buffer, cut in zip(buffers, cuts)]
    expected = left_path.stat().st_size + right_path.stat().st_size
    if expected % dtype.itemsize or output_path.stat().st_size != expected:
        raise OSError('Incomplete private percentile merge')


class _PercentileRuns:
    """Binary carry merging retains only logarithmically many private runs."""

    def __init__(self, directory, levels, dtype=np.dtype('float64')):
        self.dtype = dtype
        self.levels = [directory / f'level-{index}.bin' for index in range(levels)]
        self.scratch = [directory / f'merge-{index}.bin' for index in range(2)]

    def add(self, values):
        if not values.size:
            return
        carry, output = self.scratch
        np.sort(values).tofile(carry)
        for level in self.levels:
            if not level.exists():
                with _DISK_LOCK:
                    carry.rename(level)
                return
            _merge_percentile_runs(level, carry, output, dtype=self.dtype)
            with _DISK_LOCK:
                level.unlink()
                carry.unlink()
            carry, output = output, carry
        raise OSError('Private percentile run capacity exceeded')

    def finish(self, count):
        if not count:
            return None
        carry = None
        for level in self.levels:
            if not level.exists():
                continue
            if carry is None:
                carry = level
                continue
            output = next(path for path in self.scratch if path != carry)
            _merge_percentile_runs(carry, level, output, dtype=self.dtype)
            with _DISK_LOCK:
                carry.unlink()
                level.unlink()
            carry = output
        if carry is None or carry.stat().st_size != count * self.dtype.itemsize:
            raise OSError('Incomplete private percentile ranks')
        return carry

    def values(self, count, percentiles):
        carry = self.finish(count)
        if carry is None:
            return [None] * len(percentiles)
        results = []
        with carry.open('rb') as stream:
            for percentile in percentiles:
                lower, upper, weight = _percentile_ranks(count, percentile)
                stream.seek(lower * 8)
                values = np.fromfile(stream, dtype=np.float64, count=1 + (upper != lower))
                if values.size != 1 + (upper != lower):
                    raise OSError('Incomplete private percentile rank read')
                results.append(float(_focal_linear_values(values[:1], values[-1:], weight)[0]))
        return results


def _zonal_percentiles(runs, statistics, percentiles):
    """One final merge and one read-only handle for every zone/request rank."""
    path = runs.finish(sum(group.count for group in statistics.values()))
    if path is None:
        return [[None] * len(percentiles) for _ in statistics]
    values, offset = [], 0
    with path.open('rb') as stream:
        for code in sorted(statistics):
            count = statistics[code].count
            row = []
            for percentile in percentiles:
                if not count:
                    row.append(None)
                    continue
                lower, upper, weight = _percentile_ranks(count, percentile)
                stream.seek((offset + lower) * runs.dtype.itemsize)
                records = np.fromfile(stream, dtype=runs.dtype, count=1 + (upper != lower))
                if records.size != 1 + (upper != lower) or not np.all(records['zone'] == code):
                    raise OSError('Incomplete private zonal percentile ranks')
                row.append(float(_focal_linear_values(records['value'][:1], records['value'][-1:], weight)[0]))
            values.append(row)
            offset += count
    return values


@contextmanager
def _percentile_runs(dataset, *, zonal=False, window=None):
    width, height = window[2:] if window is not None else (dataset.RasterXSize, dataset.RasterYSize)
    block_count = ((width + 511) // 512) * ((height + 511) // 512)
    with tempfile.TemporaryDirectory(prefix='percentile-', dir=_WORKSPACE.get()) as directory:
        dtype = _ZONAL_PERCENTILE_DTYPE if zonal else np.dtype('float64')
        runs = _PercentileRuns(Path(directory), block_count.bit_length(), dtype)
        # At most source runs plus one merged copy, with per-file allocation slack.
        paths = runs.levels + runs.scratch
        required = width * height * 2 * dtype.itemsize + len(paths) * 65536
        phase = 'vector_percentile' if window is not None else 'zonal_percentile' if zonal else 'percentile'
        with _disk_budget(required, phase, private_paths=paths):
            try:
                yield runs
            except OSError as exc:
                raise _statistics_error('vector_io' if window is not None else 'zonal_io' if zonal else 'io') from exc
            finally:
                with _DISK_LOCK:
                    shutil.rmtree(directory)


def _statistics_percentiles(value):
    if not isinstance(value, list) or not value:
        raise _statistics_error('percentiles')
    result = []
    for item in value:
        if (isinstance(item, (bool, np.bool_)) or not isinstance(item, Real)
                or not 0 <= item <= 100 or not math.isfinite(float(item))):
            raise _statistics_error('percentiles')
        item = float(item)
        if result and item <= result[-1]:
            raise _statistics_error('percentiles')
        result.append(item)
    return result


def raster_statistics(input_raster, band=1, percentiles=_PERCENTILE_UNSET):
    requested = percentiles is not _PERCENTILE_UNSET
    if requested:
        percentiles = _statistics_percentiles(percentiles)
    dataset = _open(input_raster)
    _band(dataset, band)
    for index in range(1, dataset.RasterCount + 1):
        candidate = dataset.GetRasterBand(index)
        if candidate.GetColorInterpretation() == gdal.GCI_AlphaBand and gdal.DataTypeIsComplex(candidate.DataType):
            raise _statistics_error('alpha')
    count, mean, m2 = 0, 0.0, 0.0
    minimum, maximum = math.inf, -math.inf
    with _percentile_runs(dataset) if requested else nullcontext() as runs:
        for _, _, arrays, masks in _blocks(dataset, [band], native=True):
            # Apply raw Int64/UInt64 NoData before entering existing Float64 arithmetic.
            values = arrays[0][masks[0]].astype(np.float64)
            n = values.size
            if not n:
                continue
            with np.errstate(over='ignore', invalid='ignore'):
                local_mean = float(values.mean())
                delta = local_mean - mean
                m2 += float(((values - local_mean) ** 2).sum()) + delta * delta * count * n / (count + n)
                mean += delta * n / (count + n)
            count += int(n)
            minimum, maximum = min(minimum, float(values.min())), max(maximum, float(values.max()))
            if runs is not None:
                runs.add(values)
        total = dataset.RasterXSize * dataset.RasterYSize
        result = {'band': band, 'strategy': 'full', 'valid_count': count, 'invalid_count': total - count,
                  'nodata_ratio': (total - count) / total, 'min': minimum if count else None,
                  'max': maximum if count else None, 'mean': mean if count else None,
                  'stddev': math.sqrt(m2 / count) if count else None}
        if count and not all(math.isfinite(result[key]) for key in ('min', 'max', 'mean', 'stddev')):
            raise _statistics_error('range')
        if requested:
            result['percentiles'] = [
                {'percentile': percentile, 'value': value}
                for percentile, value in zip(percentiles, runs.values(count, percentiles))]
        return result


def raster_histogram(input_raster, band=1, bins=256, value_range=None):
    _positive(bins, 'bins', integer=True)
    if bins > 4096:
        raise ValueError('bins must not exceed 4096')
    dataset = _open(input_raster)
    _band(dataset, band)
    if value_range is None:
        stats = raster_statistics(input_raster, band)
        if not stats['valid_count']:
            return {'band': band, 'strategy': 'full', 'edges': [], 'counts': [], 'valid_count': 0,
                    'invalid_count': stats['invalid_count'], 'outside_count': 0}
        value_range = [stats['min'], stats['max']]
        if value_range[0] == value_range[1]:
            value_range = [value_range[0] - 0.5, value_range[1] + 0.5]
    if not isinstance(value_range, list) or len(value_range) != 2 or any(isinstance(v, bool) or not isinstance(v, (int, float)) or not math.isfinite(v) for v in value_range) or value_range[0] >= value_range[1]:
        raise ValueError('value_range must be a finite increasing pair')
    edges = np.linspace(*value_range, bins + 1)
    counts = np.zeros(bins, dtype=np.int64)
    valid = 0
    for _, _, arrays, masks in _blocks(dataset, [band]):
        values = arrays[0][masks[0]]
        valid += values.size
        counts += np.histogram(values, bins=edges)[0]
    return {'band': band, 'strategy': 'full', 'edges': edges.tolist(), 'counts': counts.tolist(),
            'valid_count': int(valid), 'invalid_count': dataset.RasterXSize * dataset.RasterYSize - int(valid),
            'outside_count': int(valid - counts.sum())}


_FUNCTIONS = {'abs': np.abs, 'sqrt': np.sqrt, 'log': np.log, 'minimum': np.minimum, 'maximum': np.maximum}
_BINARY = {ast.Add: np.add, ast.Sub: np.subtract, ast.Mult: np.multiply, ast.Div: np.divide, ast.Pow: np.power}


def _expression(expression, band_count):
    if not isinstance(expression, str) or len(expression) > 2048:
        raise ValueError('expression must be a string of at most 2048 characters')
    tree = ast.parse(expression, mode='eval')
    if len(list(ast.walk(tree))) > 256:
        raise ValueError('expression is too complex')
    indices = set()

    def evaluate(node, bands):
        if isinstance(node, ast.Expression):
            return evaluate(node.body, bands)
        if isinstance(node, ast.Constant) and type(node.value) in (int, float) and abs(node.value) <= 1e100 and math.isfinite(node.value):
            return node.value
        if isinstance(node, ast.Name) and node.id.startswith('b') and node.id[1:].isdigit():
            index = int(node.id[1:])
            if not 1 <= index <= band_count:
                raise ValueError('expression references an unavailable band')
            indices.add(index)
            return bands.get(index, 1.0)
        if isinstance(node, ast.BinOp) and type(node.op) in _BINARY:
            if isinstance(node.op, ast.Pow):
                exponent = node.right
                if isinstance(exponent, ast.UnaryOp) and isinstance(exponent.op, (ast.UAdd, ast.USub)):
                    exponent = exponent.operand
                if not isinstance(exponent, ast.Constant) or type(exponent.value) not in (int, float) or not -16 <= exponent.value <= 16:
                    raise ValueError('Power exponent must be a constant between -16 and 16')
            return _BINARY[type(node.op)](evaluate(node.left, bands), evaluate(node.right, bands))
        if isinstance(node, ast.UnaryOp) and isinstance(node.op, (ast.UAdd, ast.USub)):
            result = evaluate(node.operand, bands)
            return result if isinstance(node.op, ast.UAdd) else -result
        if isinstance(node, ast.Call) and isinstance(node.func, ast.Name) and node.func.id in _FUNCTIONS and not node.keywords:
            arity = 2 if node.func.id in ('minimum', 'maximum') else 1
            if len(node.args) != arity:
                raise ValueError('Invalid expression function arguments')
            return _FUNCTIONS[node.func.id](*(evaluate(arg, bands) for arg in node.args))
        raise ValueError('Unsupported expression syntax')

    with np.errstate(all='ignore'):
        evaluate(tree, {})
    if not indices:
        raise ValueError('expression must reference at least one band')
    return sorted(indices), lambda bands: evaluate(tree, bands)


def raster_band_math(input_raster, expression):
    dataset = _open(input_raster)
    indices, calculate = _expression(expression, dataset.RasterCount)
    path = _path()
    output = gdal.GetDriverByName('GTiff').Create(str(path), dataset.RasterXSize, dataset.RasterYSize, 1, gdal.GDT_Float64, ['TILED=YES', 'COMPRESS=DEFLATE'])
    if dataset.GetGeoTransform(can_return_null=True):
        output.SetGeoTransform(dataset.GetGeoTransform())
    output.SetProjection(dataset.GetProjection())
    band = output.GetRasterBand(1)
    band.SetNoDataValue(float('nan'))
    for x, y, arrays, masks in _blocks(dataset, indices):
        with np.errstate(all='ignore'):
            values = np.broadcast_to(calculate(dict(zip(indices, arrays))), arrays[0].shape).copy()
        valid = np.logical_and.reduce(masks) & np.isfinite(values)
        values[~valid] = np.nan
        band.WriteRaster(x, y, values.shape[1], values.shape[0], values.astype(np.float64).tobytes(), buf_type=gdal.GDT_Float64)
    band = output = None
    return _raster(path)


def _grid(dataset):
    _require_georeferencing(dataset)
    transform = dataset.GetGeoTransform()
    if not all(math.isfinite(value) for value in transform) or gdal.InvGeoTransform(transform) is None:
        raise ValueError('Raster grid must have a finite invertible geotransform')
    return dataset.RasterXSize, dataset.RasterYSize, transform


def _data_indices(dataset):
    indices = [index for index in range(1, dataset.RasterCount + 1)
               if dataset.GetRasterBand(index).GetColorInterpretation() != gdal.GCI_AlphaBand]
    if not indices:
        raise ValueError('Raster must contain at least one data band')
    for index in indices:
        _band(dataset, index)
    return indices


def _numeric_output(grid, count, *, size=None):
    width, height = size or (grid.RasterXSize, grid.RasterYSize)
    output = gdal.GetDriverByName('GTiff').Create(
        str(_path()), width, height, count, gdal.GDT_Float64,
        ['TILED=YES', 'COMPRESS=DEFLATE', 'PHOTOMETRIC=MINISBLACK'])
    transform = grid.GetGeoTransform(can_return_null=True)
    if transform is not None:
        output.SetGeoTransform(transform)
    output.SetProjection(grid.GetProjection())
    for index in range(1, count + 1):
        band = output.GetRasterBand(index)
        band.SetNoDataValue(float('nan'))
        band.SetColorInterpretation(gdal.GCI_Undefined)
    return output


_FOCAL_MESSAGES = {
    'method': ('method必须为sum、mean、min、max、stddev、mode、median、percentile、variety、weighted_mean、weighted_stddev或weighted_sum',
               'method must be sum, mean, min, max, stddev, mode, median, percentile, variety, weighted_mean, weighted_stddev or weighted_sum'),
    'percentiles': ('percentiles必须为非空、严格递增且不重复的0至100有限非布尔数值列表',
                    'percentiles must be a nonempty strictly increasing list of distinct finite non-boolean numbers from 0 to 100'),
    'unused': ('只有method=percentile时才能提供percentiles参数',
               'percentiles is only accepted when method=percentile'),
    'write': ('邻域统计栅格块写入失败', 'Focal statistics raster block write failed'),
    'weights': ('weighted_mean的weights必须为与3、5或7窗口同尺寸的二维列表，元素为有限非负数且至少一个为正，不能为布尔值',
                'weighted_mean weights must be a square list matching the 3, 5 or 7 window, containing finite nonnegative numbers with at least one positive value, excluding booleans'),
    'weights_unused': ('只有method=weighted_mean、weighted_stddev或weighted_sum时才能提供weights参数',
                       'weights is only accepted when method=weighted_mean, weighted_stddev or weighted_sum'),
    'sum_weights': ('weighted_sum的weights必须为与3、5或7窗口同尺寸的二维列表，元素为有限非负数且至少一个为正，不能为布尔值',
                    'weighted_sum weights must be a square list matching the 3, 5 or 7 window, containing finite nonnegative numbers with at least one positive value, excluding booleans'),
    'stddev_weights': ('weighted_stddev的weights必须为与3、5或7窗口同尺寸的二维列表，元素为有限非负数且至少一个为正，不能为布尔值',
                       'weighted_stddev weights must be a square list matching the 3, 5 or 7 window, containing finite nonnegative numbers with at least one positive value, excluding booleans'),
    'weighted_stddev_numeric': ('邻域加权总体标准差无法表示为有限非负Float64结果',
                               'Focal weighted population standard deviation cannot be represented as a finite nonnegative Float64 result'),
    'window_size': ('window_size必须为整数3、5或7',
                    'window_size must be the integer 3, 5 or 7'),
    'window_type': ('window_type必须为square_pixels或circle_metres',
                    'window_type must be square_pixels or circle_metres'),
    'radius': ('圆形邻域的radius_metres必须显式提供有限正数，不能为布尔值',
               'Circular focal radius_metres must be an explicit finite positive number, excluding booleans'),
    'radius_unused': ('像元方窗不得提供radius_metres',
                      'Pixel square windows do not accept radius_metres'),
    'window_unused': ('米制圆窗不得提供window_size',
                      'Metre circle windows do not accept window_size'),
    'circle_weights': ('米制圆窗的weights必须为与派生最小正方形同尺寸的二维列表，元素为有限非负数且不能为布尔值，圆外必须为零，圆内至少一个为正',
                       'Metre circle weights must be a square list matching the derived smallest square, with finite nonnegative non-boolean values, zeros outside the circle and at least one positive value inside'),
    'circle_grid': ('米制圆窗要求已知线性单位的二维投影及保持北向轴手性的有限可逆正交网格，请显式重投影',
                    'Metre circle windows require a two-dimensional projection with known linear units and a finite invertible orthogonal grid with north-up handedness; reproject explicitly'),
    'circle_size': ('米制圆窗的候选像元外接窗口不得超过5×5，不能截断半径',
                    'The candidate pixel bounding window of a metre circle must not exceed 5x5; the radius cannot be truncated'),
    'weighted_numeric': ('邻域加权均值无法表示为有限Float64结果',
                         'Focal weighted mean cannot be represented as a finite Float64 result'),
}
_FOCAL_WEIGHTS_UNSET = object()
_FOCAL_WINDOW_UNSET = object()


def _focal_error(code):
    return ValueError(_FOCAL_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _percentile_ranks(count, percentile):
    position = (count - 1) * Fraction(percentile) / 100
    lower = position.numerator // position.denominator
    weight = position - lower
    return lower, lower + bool(weight), weight


def _focal_linear_values(lower, upper, weight):
    """Interpolate finite Float64 ranks without overflowing a convex result."""
    if weight == 0:
        return lower.copy()
    if weight == Fraction(1, 2):
        # Keep the original median's arithmetic, including subnormal rounding.
        safe = ((np.abs(lower) <= np.finfo(np.float64).max / 2)
                & (np.abs(upper) <= np.finfo(np.float64).max / 2))
        result = np.empty(lower.shape)
        np.add(lower, upper, out=result, where=safe)
        np.divide(result, 2., out=result, where=safe)
        with np.errstate(under='ignore'):
            np.add(lower / 2., upper / 2., out=result, where=~safe)
        return result
    fraction = float(weight)
    complement = float(1 - weight)
    same_sign = (lower >= 0) | (upper <= 0)
    delta = np.zeros(lower.shape)
    np.subtract(upper, lower, out=delta, where=same_sign)
    with np.errstate(under='ignore'):
        if fraction <= .5:
            result = lower + delta * fraction
        else:
            result = upper - delta * complement
        # Opposite-sign endpoints may have an unrepresentable difference.
        opposite = ~same_sign
        result[opposite] = lower[opposite] * complement + upper[opposite] * fraction
    magnitude = np.maximum(np.abs(lower), np.abs(upper))
    limits = np.finfo(np.float64)
    sensitive = ((lower != upper) & ((magnitude > limits.max / 2)
                 | (magnitude < limits.tiny * 4) | opposite))
    if fraction in (0., 1.):
        sensitive |= lower != upper
    # Exact binary endpoint/weight arithmetic is needed only at numerical
    # extremes or cancellation; it also preserves a tiny nonzero rank weight.
    for index in np.flatnonzero(sensitive):
        result[index] = float(Fraction(float(lower[index])) * (1 - weight)
                              + Fraction(float(upper[index])) * weight)
    return result


def _focal_ordered_values(neighbours, shape, window_size):
    """Sort one bounded halo window per core pixel; invalid slots sort last."""
    ordered = np.full((window_size * window_size, *shape), np.inf)
    count = np.zeros(shape, dtype=np.uint8)
    for index, (sample, mask) in enumerate(neighbours()):
        np.copyto(ordered[index], sample, where=mask)
        count += mask
    ordered.sort(axis=0)
    return ordered, count


def _focal_rank_values(ordered, count, percentile):
    result = np.full(count.shape, np.nan)
    for size in np.unique(count):
        if size == 0:
            continue
        lower_rank, upper_rank, weight = _percentile_ranks(int(size), percentile)
        selected = count == size
        result[selected] = _focal_linear_values(ordered[lower_rank][selected],
                                                ordered[upper_rank][selected], weight)
    return result


def _focal_weight_kernel(weights, window_size, *, method='weighted_mean', circle_offsets=None):
    error = ('circle_weights' if circle_offsets is not None else
             {'weighted_mean': 'weights', 'weighted_stddev': 'stddev_weights', 'weighted_sum': 'sum_weights'}[method])
    if (not isinstance(weights, list) or len(weights) != window_size
            or any(not isinstance(row, list) or len(row) != window_size for row in weights)):
        raise _focal_error(error)
    values = []
    for row in weights:
        for value in row:
            try:
                if isinstance(value, (bool, np.bool_)) or not isinstance(value, Real):
                    raise _focal_error(error)
                value = float(value)
            except (TypeError, OverflowError) as cause:
                raise _focal_error(error) from cause
            if not math.isfinite(value) or value < 0:
                raise _focal_error(error)
            values.append(value)
    if not any(values):
        raise _focal_error(error)
    member_weights = tuple(values)
    if circle_offsets is not None:
        radius = window_size // 2
        members = {(dy + radius) * window_size + dx + radius for dy, dx in circle_offsets}
        if any(value and index not in members for index, value in enumerate(values)):
            raise _focal_error(error)
        member_weights = tuple(value for index, value in enumerate(values) if index in members)
    ratios = [value.as_integer_ratio() for value in values]
    shift = max(denominator.bit_length() - 1 for _, denominator in ratios)
    units = [numerator << (shift - (denominator.bit_length() - 1)) for numerator, denominator in ratios]
    divisor = math.gcd(*units)
    return member_weights, tuple(value // divisor for value in units), Fraction(divisor, 1 << shift)


def _focal_weighted_values(values, valid, cx, cy, shape, window_size, units, center_valid, *, method, weight_scale):
    """Bounded exact products/moments; round only the final weighted result."""
    stddev = method == 'weighted_stddev'
    encoded, shift = _dyadic_units(values)
    numerator = np.zeros(shape, dtype=object)
    denominator = np.zeros(shape, dtype=object)
    squares = np.zeros(shape, dtype=object) if stddev else None
    radius = window_size // 2
    height, width = shape
    for index, weight in enumerate(units):
        if not weight:
            continue
        dy, dx = index // window_size - radius, index % window_size - radius
        region = np.s_[cy+dy:cy+dy+height, cx+dx:cx+dx+width]
        numerator += encoded[region] * weight
        if stddev:
            squares += encoded[region] ** 2 * weight
        denominator += valid[region].astype(object) * weight
    result = np.full(shape, np.nan)
    for index in np.flatnonzero(center_valid & (denominator != 0)):
        try:
            if stddev:
                mass = denominator.flat[index]
                variance_numerator = squares.flat[index] * mass - numerator.flat[index] ** 2
                if variance_numerator < 0:
                    raise _focal_error('weighted_stddev_numeric')
                variance = Fraction(variance_numerator, mass * mass << (2 * shift))
                value = _sqrt_fraction(variance, error=lambda: _focal_error('weighted_stddev_numeric'), allow_rounded_zero=True)
            elif method == 'weighted_sum':
                value = float(Fraction(numerator.flat[index], 1 << shift) * weight_scale)
            else:
                value = float(Fraction(numerator.flat[index], denominator.flat[index] << shift))
        except OverflowError as cause:
            if method == 'weighted_sum':
                # Like sum, a genuinely unrepresentable cell is NoData.
                continue
            raise _focal_error('weighted_stddev_numeric' if stddev else 'weighted_numeric') from cause
        if not math.isfinite(value):
            raise _focal_error('weighted_stddev_numeric' if stddev else 'weighted_numeric')
        result.flat[index] = value
    return result


def _focal_circle_window(dataset, radius_metres):
    """Select at most 25 original pixel centres using exact input binary facts."""
    try:
        units = Fraction(_projected_orthogonal_grid_units(dataset, ValueError))
    except ValueError as cause:
        raise _focal_error('circle_grid') from cause
    transform = dataset.GetGeoTransform()
    ux, uy, vx, vy = (Fraction(transform[i]) * units for i in (1, 4, 2, 5))
    radius_squared = Fraction(radius_metres)**2
    rx = math.isqrt(radius_squared // (ux*ux + uy*uy))
    ry = math.isqrt(radius_squared // (vx*vx + vy*vy))
    if rx > 2 or ry > 2:
        raise _focal_error('circle_size')
    offsets = tuple(
        (row, column)
        for row in range(-ry, ry+1)
        for column in range(-rx, rx+1)
        if (column*ux + row*vx)**2 + (column*uy + row*vy)**2 <= radius_squared
    )
    return 2*max(rx, ry)+1, offsets


def raster_focal_statistics(input_raster, band, window_size=_FOCAL_WINDOW_UNSET, method=None, percentiles=_PERCENTILE_UNSET,
                            *, weights=_FOCAL_WEIGHTS_UNSET, window_type='square_pixels', radius_metres=_FOCAL_WINDOW_UNSET):
    """Reduce valid pixel neighbourhoods using bounded source reads with halos."""
    if not isinstance(window_type, str) or window_type not in ('square_pixels', 'circle_metres'):
        raise _focal_error('window_type')
    if window_type == 'square_pixels':
        if radius_metres is not _FOCAL_WINDOW_UNSET:
            raise _focal_error('radius_unused')
        if isinstance(window_size, bool) or not isinstance(window_size, int) or window_size not in (3, 5, 7):
            raise _focal_error('window_size')
    else:
        if window_size is not _FOCAL_WINDOW_UNSET:
            raise _focal_error('window_unused')
        try:
            if isinstance(radius_metres, (bool, np.bool_)) or not isinstance(radius_metres, Real):
                raise _focal_error('radius')
            radius_metres = float(radius_metres)
        except (TypeError, OverflowError) as cause:
            raise _focal_error('radius') from cause
        if not math.isfinite(radius_metres) or radius_metres <= 0:
            raise _focal_error('radius')
    if method not in ('sum', 'mean', 'min', 'max', 'stddev', 'mode', 'median', 'percentile', 'variety', 'weighted_mean', 'weighted_stddev', 'weighted_sum'):
        raise _focal_error('method')
    if method == 'percentile':
        try:
            percentiles = _statistics_percentiles(percentiles)
        except ValueError as cause:
            raise _focal_error('percentiles') from cause
    elif percentiles is not _PERCENTILE_UNSET:
        raise _focal_error('unused')
    weighted = method in ('weighted_mean', 'weighted_stddev', 'weighted_sum')
    if not weighted and weights is not _FOCAL_WEIGHTS_UNSET:
        raise _focal_error('weights_unused')
    if weighted and window_type == 'square_pixels':
        kernel, kernel_units, weight_scale = _focal_weight_kernel(weights, window_size, method=method)
    dataset = _open(input_raster)
    if _band(dataset, band).GetColorInterpretation() == gdal.GCI_AlphaBand:
        raise ValueError('Focal statistics require a data band, not alpha')
    for index in range(1, dataset.RasterCount + 1):
        if dataset.GetRasterBand(index).GetColorInterpretation() == gdal.GCI_AlphaBand:
            _band(dataset, index)
    if window_type == 'circle_metres':
        window_size, offsets = _focal_circle_window(dataset, radius_metres)
        if weighted:
            kernel, kernel_units, weight_scale = _focal_weight_kernel(
                weights, window_size, method=method, circle_offsets=offsets)
    else:
        r = window_size // 2
        offsets = tuple((dy, dx) for dy in range(-r, r+1) for dx in range(-r, r+1))
    reduce_method = method
    if weighted:
        # Uniformity uses window members only. Sum also preserves absolute scale.
        if method == 'weighted_sum':
            if all(value == 1. for value in kernel):
                reduce_method = 'sum'
        elif len(set(kernel)) == 1:
            reduce_method = 'mean' if method == 'weighted_mean' else 'stddev'
    output = _numeric_output(dataset, len(percentiles) if method == 'percentile' else 1)
    destination = None
    output_path = Path(output.GetDescription())
    completed = False
    try:
        if method == 'percentile':
            for index, percentile in enumerate(percentiles, 1):
                destination = output.GetRasterBand(index)
                destination.SetDescription('percentile=' + repr(percentile))
            destination = None
        radius = window_size // 2
        # Keep the 49-plane sort buffer within the old 25-plane block bound.
        core_size = 360 if window_size == 7 else 512 - 2 * radius
        for y in range(0, dataset.RasterYSize, core_size):
            for x in range(0, dataset.RasterXSize, core_size):
                width = min(core_size, dataset.RasterXSize - x)
                height = min(core_size, dataset.RasterYSize - y)
                left, top = max(0, x - radius), max(0, y - radius)
                right = min(dataset.RasterXSize, x + width + radius)
                bottom = min(dataset.RasterYSize, y + height + radius)
                _, _, arrays, masks = next(_blocks(dataset, [band], window=(left, top, right-left, bottom-top),
                                                   native=True))
                if reduce_method in ('mode', 'variety'):
                    _safe_classification_values(arrays[0], masks[0])
                arrays[0] = arrays[0].astype(np.float64)
                # Padding supplies only invalid placeholders, never contributing zeros.
                values = np.pad(np.where(masks[0], arrays[0], 0.), radius)
                valid = np.pad(masks[0], radius, constant_values=False)
                cx, cy = x-left+radius, y-top+radius
                center = values[cy:cy+height, cx:cx+width]
                center_valid = valid[cy:cy+height, cx:cx+width]

                def neighbours():
                    for dy, dx in offsets:
                        yield (values[cy+dy:cy+dy+height, cx+dx:cx+dx+width],
                               valid[cy+dy:cy+dy+height, cx+dx:cx+dx+width])

                def write_result(result, index=1):
                    nonlocal destination
                    result[~center_valid | ~np.isfinite(result)] = np.nan
                    destination = output.GetRasterBand(index)
                    if destination.WriteRaster(x, y, width, height, np.ascontiguousarray(result).tobytes(),
                                               buf_type=gdal.GDT_Float64) != gdal.CE_None:
                        raise _focal_error('write')

                if reduce_method in ('weighted_mean', 'weighted_stddev', 'weighted_sum'):
                    result = _focal_weighted_values(values, valid, cx, cy, center.shape, window_size, kernel_units, center_valid,
                                                    method=reduce_method, weight_scale=weight_scale)
                elif reduce_method == 'mode':
                    result = center.copy()
                    best_count = np.zeros(center.shape, dtype=np.uint8)
                    for candidate, candidate_valid in neighbours():
                        frequency = np.zeros(center.shape, dtype=np.uint8)
                        for sample, mask in neighbours():
                            frequency += mask & (sample == candidate)
                        # Order by count, then membership of the original center
                        # class, then smaller class; invalid placeholders cannot win.
                        tie_better = (result != center) & ((candidate == center) | (candidate < result))
                        take = candidate_valid & ((frequency > best_count)
                            | ((frequency == best_count) & tie_better))
                        np.copyto(result, candidate, where=take)
                        np.copyto(best_count, frequency, where=take)
                elif reduce_method == 'variety':
                    ordered, _ = _focal_ordered_values(neighbours, center.shape, window_size)
                    # Safe integer classes remain exact Float64 values. Count only
                    # finite run starts; repeated classes and invalid slots add zero.
                    result = np.isfinite(ordered[0]).astype(np.float64)
                    for index in range(1, window_size * window_size):
                        result += np.isfinite(ordered[index]) & (ordered[index] != ordered[index-1])
                elif reduce_method in ('median', 'percentile'):
                    ordered, count = _focal_ordered_values(neighbours, center.shape, window_size)
                    for index, percentile in enumerate(percentiles if reduce_method == 'percentile' else [50.], 1):
                        write_result(_focal_rank_values(ordered, count, percentile), index)
                    continue
                elif reduce_method in ('min', 'max'):
                    result = np.full(center.shape, np.inf if reduce_method == 'min' else -np.inf)
                    reduce = np.minimum if reduce_method == 'min' else np.maximum
                    for sample, mask in neighbours():
                        reduce(result, sample, out=result, where=mask)
                else:
                    count = np.zeros(center.shape, dtype=np.int32)
                    scale = np.zeros(center.shape)
                    for sample, mask in neighbours():
                        count += mask
                        np.maximum(scale, np.abs(sample), out=scale, where=mask)
                    scale[scale == 0] = 1.
                    divisor = np.maximum(count, 1)

                    def normalized(sample, mask):
                        if reduce_method == 'stddev':
                            with np.errstate(over='ignore', invalid='ignore'):
                                delta = sample - center
                            # Subtract first to preserve small differences on large offsets;
                            # opposite finite extremes need normalized subtraction instead.
                            term = np.divide(delta, scale, where=np.isfinite(delta), out=np.zeros_like(delta))
                            np.copyto(term, sample/scale - center/scale, where=~np.isfinite(delta))
                        else:
                            term = sample/scale
                        return np.where(mask, term, 0.)

                    total = np.zeros(center.shape)
                    correction = np.zeros(center.shape)
                    for sample, mask in neighbours():
                        term = normalized(sample, mask)
                        combined = total + term
                        correction += np.where(np.abs(total) >= np.abs(term),
                                               (total-combined)+term, (term-combined)+total)
                        total = combined
                    total += correction
                    if reduce_method == 'stddev':
                        mean_delta = total/divisor
                        variance = np.zeros(center.shape)
                        for sample, mask in neighbours():
                            residual = normalized(sample, mask) - mean_delta
                            variance += np.where(mask, residual*residual, 0.)
                        result = np.sqrt(np.clip(variance/divisor, 0., 1.))*scale
                    else:
                        with np.errstate(over='ignore', invalid='ignore'):
                            result = (np.clip(total/divisor, -1., 1.) if reduce_method == 'mean' else total)*scale
                write_result(result)
        destination = None
        output.FlushCache()
        output = None
        result = _raster(output_path)
        completed = True
        return result
    finally:
        destination = output = dataset = None
        if not completed:
            output_path.unlink(missing_ok=True)



_BOUNDARY_MESSAGES = {
    'rounds': {'zh-cn': '边界清理的 rounds 必须为整数 1 或 2',
               'en': 'Boundary cleaning rounds must be the integer 1 or 2'},
    'band': {'zh-cn': '边界清理必须显式选择有效的一基数据波段',
             'en': 'Boundary cleaning requires a valid explicit one-based data band'},
    'complex': {'zh-cn': '边界清理不支持复数数据或复数 alpha 波段',
                'en': 'Boundary cleaning does not support complex data or complex alpha bands'},
    'alpha': {'zh-cn': '边界清理必须选择分类数据波段，不能选择 alpha',
              'en': 'Boundary cleaning requires a classification data band, not alpha'},
    'classes': {'zh-cn': '边界清理的有效分类值必须为 ±(2^53−1) 内的精确整数',
                'en': 'Boundary cleaning classification values must be exact safe integers within ±(2^53−1)'},
    'read': {'zh-cn': '边界清理无法读取已完成的阶段栅格',
             'en': 'Boundary cleaning could not read a completed stage raster'},
    'write': {'zh-cn': '边界清理无法写入阶段栅格',
              'en': 'Boundary cleaning could not write a stage raster'},
}


def _boundary_error(code):
    return ValueError(_BOUNDARY_MESSAGES[code][_DISK_LANGUAGE.get()])


def _neighbourhood_windows(dataset, band, *, native=False):
    """One 510-pixel core plus one-cell halo; retain native dtype when required."""
    for y in range(0, dataset.RasterYSize, 510):
        for x in range(0, dataset.RasterXSize, 510):
            width, height = min(510, dataset.RasterXSize-x), min(510, dataset.RasterYSize-y)
            left, top = max(0, x-1), max(0, y-1)
            right, bottom = min(dataset.RasterXSize, x+width+1), min(dataset.RasterYSize, y+height+1)
            _, _, arrays, masks = next(_blocks(dataset, [band], window=(left, top, right-left, bottom-top), native=native))
            padding = ((top-y+1, y+height+1-bottom), (left-x+1, x+width+1-right))
            yield x, y, arrays[0], masks[0], padding


def _cell_neighbourhood_blocks(dataset, band):
    """A 510-pixel core plus one-cell halo, never reading more than 512 squared."""
    for x, y, array, mask, padding in _neighbourhood_windows(dataset, band):
        values = np.pad(np.where(mask, array, np.nan), padding, constant_values=np.nan)
        yield x, y, values


def _boundary_neighbours(values):
    height, width = values.shape[0]-2, values.shape[1]-2
    for dy in range(3):
        for dx in range(3):
            yield values[dy:dy+height, dx:dx+width]


_SIEVE_PROTECTED_UNSET = object()
_SIEVE_THRESHOLD_UNSET = object()
_SIEVE_MESSAGES = {
    'band': ('小斑块筛除必须显式选择有效的一基数据波段', 'Raster sieve requires an explicit valid one-based data band'),
    'threshold': ('小斑块筛除的min_pixels必须为正整数', 'Raster sieve min_pixels must be a positive integer'),
    'threshold_choice': ('小斑块筛除的min_pixels与min_area_m2必须且只能提供一个',
                         'Raster sieve requires exactly one of min_pixels or min_area_m2'),
    'area_threshold': ('小斑块筛除的min_area_m2必须为有限正数，单位为平方米',
                       'Raster sieve min_area_m2 must be a finite positive number in square metres'),
    'area_grid': ('小斑块筛除面积模式需要已知线性单位的二维投影、北向无旋转网格，请先显式重投影',
                  'Raster sieve area mode requires a projected north-up grid with known linear units; reproject explicitly'),
    'area_numeric': ('小斑块筛除的像元平方米面积必须为有限正Float64，非零面积不能下溢为零',
                     'Raster sieve cell area must be finite positive Float64 square metres; a nonzero area must not underflow to zero'),
    'protected': ('小斑块筛除的protected_values必须为不重复的安全整数列表，可为空',
                  'Raster sieve protected_values must be a list of distinct exact safe integers, possibly empty'),
    'connectivity': ('小斑块筛除必须显式选择整数4或8邻接', 'Raster sieve requires explicit integer connectivity 4 or 8'),
    'alpha': ('小斑块筛除不能选择alpha作为数据波段', 'Raster sieve cannot use an alpha band as data'),
    'complex': ('小斑块筛除不支持复数数据或复数alpha', 'Raster sieve does not support complex data or alpha'),
    'classes': ('小斑块筛除的有效分类必须为安全范围内的精确整数', 'Raster sieve valid classes must be exact integers within the safe range'),
}


def _sieve_error(code):
    return ValueError(_SIEVE_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _classification_rows(dataset, band, *, error):
    """Native validity first; source reads are at most 512 by one pixel."""
    width = dataset.RasterXSize
    for row in range(dataset.RasterYSize):
        values = np.zeros(width, dtype=np.int64)
        valid = np.zeros(width, dtype=bool)
        for x, _, arrays, masks in _blocks(dataset, [band], window=(0, row, width, 1), native=True):
            mask = masks[0][0]
            try:
                checked = _safe_classification_values(arrays[0][0], mask)
            except ValueError as cause:
                raise error('classes') from cause
            span = slice(x, x + len(mask))
            values[span][mask] = checked
            valid[span] = mask
        yield values, valid


def _classification_components(dataset, band, connectivity, labels, *, error):
    """Scanline runs and union by size, with exact counts and first-cell anchors."""
    parents, sizes, classes, anchors = [0], [0], [0], [0]

    def find(identifier):
        root = identifier
        while parents[root] != root:
            root = parents[root]
        while parents[identifier] != identifier:
            following = parents[identifier]
            parents[identifier] = root
            identifier = following
        return root

    def union(left, right):
        left, right = find(left), find(right)
        if left == right:
            return
        if sizes[left] < sizes[right]:
            left, right = right, left
        parents[right] = left
        sizes[left] += sizes[right]
        anchors[left] = min(anchors[left], anchors[right])

    previous = []
    padding = 1 if connectivity == 8 else 0
    width = dataset.RasterXSize
    for row, (values, valid) in enumerate(_classification_rows(dataset, band, error=error)):
        # False sentinels split invalid pixels as well as adjacent differing classes.
        starts = np.flatnonzero(valid & np.r_[True, ~valid[:-1] | (values[1:] != values[:-1])])
        ends = np.flatnonzero(valid & np.r_[~valid[1:] | (values[1:] != values[:-1]), True])
        current = []
        labels[row] = 0
        position = 0
        for start, end in zip(starts.tolist(), ends.tolist()):
            value = int(values[start])
            identifier = len(parents)
            parents.append(identifier); sizes.append(end - start + 1)
            classes.append(value); anchors.append(row * width + start)
            labels[row, start:end + 1] = identifier
            while position < len(previous) and previous[position][1] < start - padding:
                position += 1
            other = position
            while other < len(previous) and previous[other][0] <= end + padding:
                _, _, category, prior = previous[other]
                if category == value:
                    union(identifier, prior)
                other += 1
            current.append((start, end, value, identifier))
        previous = current
    roots = np.asarray([find(i) for i in range(len(parents))], dtype=np.int64)
    return roots, sizes, classes, anchors


@contextmanager
def _classification_labels(dataset, band, connectivity, *, error):
    """One owned disk-label lifecycle for both component consumers."""
    path = _path('.labels')
    labels = None
    try:
        labels = np.memmap(path, dtype=np.int64, mode='w+', shape=(dataset.RasterYSize, dataset.RasterXSize))
        yield (labels, *_classification_components(dataset, band, connectivity, labels, error=error))
    finally:
        try:
            if labels is not None:
                labels._mmap.close()
        finally:
            path.unlink(missing_ok=True)


def _sieve_donors(labels, roots, sizes, classes, anchors, min_pixels, connectivity, protected):
    """Only original roots above the threshold are donors; no chain propagation."""
    chosen = {}

    def consider(left, right):
        different = (left != right) & (left != 0) & (right != 0)
        if not different.any():
            return
        pairs = np.unique(np.column_stack((left[different], right[different])), axis=0)
        for first, second in pairs.tolist():
            for small, donor in ((first, second), (second, first)):
                if sizes[small] >= min_pixels or classes[small] in protected or sizes[donor] < min_pixels:
                    continue
                rank = (-sizes[donor], classes[donor], anchors[donor], donor)
                if small not in chosen or rank < chosen[small]:
                    chosen[small] = rank

    previous = None
    for row in range(labels.shape[0]):
        current = roots[np.asarray(labels[row])]
        consider(current[:-1], current[1:])
        if previous is not None:
            consider(previous, current)
            if connectivity == 8:
                consider(previous[:-1], current[1:])
                consider(previous[1:], current[:-1])
        previous = current
    return {small: candidate[-1] for small, candidate in chosen.items()}


def _sieve_area_minimum(dataset, threshold):
    """Exact count-equivalent ratio; never round an area to a whole-cell cutoff."""
    try:
        horizontal = _projected_north_up_grid(dataset)
    except (ValueError, RuntimeError) as cause:
        raise _sieve_error('area_grid') from cause
    transform = dataset.GetGeoTransform()
    area = Fraction(transform[1]) * Fraction(-transform[5]) * Fraction(horizontal) ** 2
    try:
        representable = float(area)
    except OverflowError as cause:
        raise _sieve_error('area_numeric') from cause
    if not math.isfinite(representable) or representable <= 0:
        raise _sieve_error('area_numeric')
    return threshold / area


def raster_sieve(input_raster, band, min_pixels=_SIEVE_THRESHOLD_UNSET, connectivity=None,
                 protected_values=_SIEVE_PROTECTED_UNSET, *, min_area_m2=_SIEVE_THRESHOLD_UNSET):
    """One frozen connected-component sieve; invalid pixels are never donors."""
    area_mode = min_area_m2 is not _SIEVE_THRESHOLD_UNSET
    if (min_pixels is not _SIEVE_THRESHOLD_UNSET) == area_mode:
        raise _sieve_error('threshold_choice')
    if area_mode:
        try:
            if isinstance(min_area_m2, bool) or not isinstance(min_area_m2, Real) or not math.isfinite(min_area_m2) or min_area_m2 <= 0:
                raise _sieve_error('area_threshold')
            minimum = Fraction(int(min_area_m2)) if isinstance(min_area_m2, Integral) else Fraction(float(min_area_m2))
        except (TypeError, OverflowError) as cause:
            raise _sieve_error('area_threshold') from cause
    else:
        if type(min_pixels) is not int or min_pixels <= 0:
            raise _sieve_error('threshold')
        minimum = min_pixels
    if type(connectivity) is not int or connectivity not in (4, 8):
        raise _sieve_error('connectivity')
    if protected_values is _SIEVE_PROTECTED_UNSET:
        protected_values = []
    if (not isinstance(protected_values, list)
        or any(isinstance(value, bool) or not isinstance(value, Integral)
               or not -(2**53 - 1) <= value <= 2**53 - 1 for value in protected_values)
        or len(set(protected_values)) != len(protected_values)):
        raise _sieve_error('protected')
    protected = frozenset(int(value) for value in protected_values)
    dataset = _open(input_raster)
    if type(band) is not int or not 1 <= band <= dataset.RasterCount:
        raise _sieve_error('band')
    selected = dataset.GetRasterBand(band)
    if selected.GetColorInterpretation() == gdal.GCI_AlphaBand:
        raise _sieve_error('alpha')
    for index in range(1, dataset.RasterCount + 1):
        candidate = dataset.GetRasterBand(index)
        if (index == band or candidate.GetColorInterpretation() == gdal.GCI_AlphaBand) and gdal.DataTypeIsComplex(candidate.DataType):
            raise _sieve_error('complex')
    selected = candidate = None
    if area_mode:
        minimum = _sieve_area_minimum(dataset, minimum)
    output = destination = None
    output_path = None
    completed = False
    try:
        with _classification_labels(dataset, band, connectivity, error=_sieve_error) as (labels, roots, sizes, classes, anchors):
            donors = _sieve_donors(labels, roots, sizes, classes, anchors, minimum, connectivity, protected)
            replacement = np.asarray(classes, dtype=np.float64)
            replacement[0] = np.nan
            for small, donor in donors.items():
                replacement[small] = classes[donor]
            output = _numeric_output(dataset, 1)
            output_path = Path(output.GetDescription())
            destination = output.GetRasterBand(1)
            for row in range(dataset.RasterYSize):
                for x in range(0, dataset.RasterXSize, 512):
                    result = replacement[roots[np.asarray(labels[row, x:x + 512])]]
                    destination.WriteArray(result.reshape(1, -1), x, row)
            destination = None
            output.FlushCache()
            output = None
            result = _raster(output_path)
        completed = True
        return result
    finally:
        destination = output = dataset = None
        if output_path is not None and not completed:
            output_path.unlink(missing_ok=True)


_REGION_GROUP_MESSAGES = {
    'band': ('连通斑块编号必须显式选择有效的一基数据波段', 'Raster region grouping requires an explicit valid one-based data band'),
    'connectivity': ('连通斑块编号必须显式选择整数4或8邻接', 'Raster region grouping requires explicit integer connectivity 4 or 8'),
    'alpha': ('连通斑块编号不能选择alpha作为数据波段', 'Raster region grouping cannot use an alpha band as data'),
    'complex': ('连通斑块编号不支持复数数据或复数alpha', 'Raster region grouping does not support complex data or alpha'),
    'classes': ('连通斑块编号的有效分类必须为安全范围内的精确整数', 'Raster region grouping valid classes must be exact integers within the safe range'),
    'numeric': ('连通斑块数量超出Float64精确正整数编号范围', 'Raster region count exceeds the exact positive Float64 integer label range'),
}
_REGION_GROUP_MAX_LABEL = 2**53 - 1


def _region_group_error(code):
    return ValueError(_REGION_GROUP_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _region_group_numbers(roots, anchors):
    ordered = sorted((i for i in range(1, len(roots)) if roots[i] == i), key=anchors.__getitem__)
    if len(ordered) > _REGION_GROUP_MAX_LABEL:
        raise _region_group_error('numeric')
    numbers = np.full(len(roots), np.nan)
    for number, root in enumerate(ordered, 1):
        numbers[root] = number
    return numbers


def raster_region_group(input_raster, band, connectivity):
    """Label exact same-class components by first original row/column."""
    if type(connectivity) is not int or connectivity not in (4, 8):
        raise _region_group_error('connectivity')
    dataset = _open(input_raster)
    if type(band) is not int or not 1 <= band <= dataset.RasterCount:
        raise _region_group_error('band')
    selected = dataset.GetRasterBand(band)
    if selected.GetColorInterpretation() == gdal.GCI_AlphaBand:
        raise _region_group_error('alpha')
    for index in range(1, dataset.RasterCount+1):
        candidate = dataset.GetRasterBand(index)
        if (index == band or candidate.GetColorInterpretation() == gdal.GCI_AlphaBand) and gdal.DataTypeIsComplex(candidate.DataType):
            raise _region_group_error('complex')
    selected = candidate = None
    destination = output = None
    output_path = None
    completed = False
    try:
        with _classification_labels(dataset, band, connectivity, error=_region_group_error) as (labels, roots, _, _, anchors):
            numbers = _region_group_numbers(roots, anchors)
            output = _numeric_output(dataset, 1)
            output_path = Path(output.GetDescription())
            destination = output.GetRasterBand(1)
            for row in range(dataset.RasterYSize):
                for x in range(0, dataset.RasterXSize, 512):
                    result = numbers[roots[np.asarray(labels[row, x:x+512])]]
                    destination.WriteArray(result.reshape(1, -1), x, row)
            destination = None
            output.FlushCache()
            output = None
            result = _raster(output_path)
        completed = True
        return result
    finally:
        destination = output = dataset = None
        if not completed and output_path is not None:
            output_path.unlink(missing_ok=True)


def raster_boundary_clean(input_raster, band, rounds=1):
    """Synchronous expand/shrink rounds with frozen source class priorities."""
    if type(rounds) is not int or rounds not in (1, 2):
        raise _boundary_error('rounds')
    dataset = _open(input_raster)
    if isinstance(band, bool) or not isinstance(band, int) or not 1 <= band <= dataset.RasterCount:
        raise _boundary_error('band')
    selected = dataset.GetRasterBand(band)
    if selected.GetColorInterpretation() == gdal.GCI_AlphaBand:
        raise _boundary_error('alpha')
    for index in range(1, dataset.RasterCount+1):
        candidate = dataset.GetRasterBand(index)
        if (index == band or candidate.GetColorInterpretation() == gdal.GCI_AlphaBand) and gdal.DataTypeIsComplex(candidate.DataType):
            raise _boundary_error('complex')
    selected = candidate = None
    counts = {}
    for _, _, arrays, masks in _blocks(dataset, [band], native=True):
        try:
            checked = _safe_classification_values(arrays[0], masks[0])
        except ValueError as error:
            raise _boundary_error('classes') from error
        classes, frequencies = np.unique(checked, return_counts=True)
        for value, frequency in zip(classes, frequencies):
            value = int(value)
            counts[value] = counts.get(value, 0) + int(frequency)
    # Ranks are integers; even adjacent safe-integer class codes remain distinct.
    order = sorted(counts, key=lambda value: (counts[value], -value))
    rank = {value: index for index, value in enumerate(order)}
    classes = np.asarray(sorted(counts), dtype=np.float64)
    priorities = np.asarray([rank[int(value)] for value in classes], dtype=np.int64)
    first_path = output_path = None
    try:
        output_path = _boundary_clean_round(dataset, band, classes, priorities)
        if rounds == 2:
            first_path, output_path = output_path, None
            dataset = gdal.Open(str(first_path))
            if dataset is None:
                raise _boundary_error('read')
            output_path = _boundary_clean_round(dataset, 1, classes, priorities)
            dataset = None
            first_path.unlink()
            first_path = None
        result = _raster(output_path)
        output_path = None
        return result
    finally:
        dataset = None
        if first_path is not None:
            first_path.unlink(missing_ok=True)
        if output_path is not None:
            output_path.unlink(missing_ok=True)


def _boundary_clean_round(dataset, band, classes, priorities):
    """Read the complete round baseline and return a closed private result."""
    expanded = output = destination = None
    expanded_path = output_path = None
    completed = False
    try:
        expanded = _numeric_output(dataset, 1)
        expanded_path = Path(expanded.GetDescription())
        destination = expanded.GetRasterBand(1)
        for x, y, values in _cell_neighbourhood_blocks(dataset, band):
            center = values[1:-1, 1:-1]
            best = np.full(center.shape, -1, dtype=np.int64)
            result = np.full(center.shape, np.nan)
            for sample in _boundary_neighbours(values):
                valid = np.isfinite(sample)
                candidate = np.full(center.shape, -1, dtype=np.int64)
                candidate[valid] = priorities[np.searchsorted(classes, sample[valid])]
                take = candidate > best
                np.copyto(result, sample, where=take)
                np.copyto(best, candidate, where=take)
            result[~np.isfinite(center)] = np.nan
            if destination.WriteArray(result, x, y) != gdal.CE_None:
                raise _boundary_error('write')
        destination = None
        if expanded.FlushCache() not in (None, gdal.CE_None):
            raise _boundary_error('write')
        expanded = None
        expanded = gdal.Open(str(expanded_path))
        if expanded is None:
            raise _boundary_error('read')
        output = _numeric_output(dataset, 1)
        output_path = Path(output.GetDescription())
        destination = output.GetRasterBand(1)
        for x, y, values in _cell_neighbourhood_blocks(expanded, 1):
            height, width = values.shape[0]-2, values.shape[1]-2
            _, _, arrays, masks = next(_blocks(dataset, [band], window=(x, y, width, height)))
            baseline = arrays[0]
            restore = np.zeros(baseline.shape, dtype=bool)
            for sample in _boundary_neighbours(values):
                restore |= sample == baseline
            result = np.where(restore, baseline, values[1:-1, 1:-1])
            result[~masks[0]] = np.nan
            if destination.WriteArray(result, x, y) != gdal.CE_None:
                raise _boundary_error('write')
        destination = None
        if output.FlushCache() not in (None, gdal.CE_None):
            raise _boundary_error('write')
        output = None
        completed = True
        return output_path
    finally:
        destination = output = expanded = dataset = None
        if expanded_path is not None:
            expanded_path.unlink(missing_ok=True)
        if output_path is not None and not completed:
            output_path.unlink(missing_ok=True)


_THIN_MESSAGES = {
    'band': {'zh-cn': '栅格细化必须显式选择有效的一基数据波段',
             'en': 'Raster thinning requires a valid explicit one-based data band'},
    'alpha': {'zh-cn': '栅格细化必须选择分类数据波段，不能选择 alpha',
              'en': 'Raster thinning requires a classification data band, not alpha'},
    'complex': {'zh-cn': '栅格细化不支持复数数据或复数 alpha 波段',
                'en': 'Raster thinning does not support complex data or complex alpha bands'},
    'classes': {'zh-cn': '栅格细化的有效分类值必须为 ±(2^53−1) 内的精确整数',
                'en': 'Raster thinning classification values must be exact safe integers within ±(2^53−1)'},
    'foreground': {'zh-cn': '栅格细化必须显式指定一个 ±(2^53−1) 内的精确整数前景分类',
                   'en': 'Raster thinning requires one explicit exact safe integer foreground classification within ±(2^53−1)'},
}


def _thin_error(code):
    return ValueError(_THIN_MESSAGES[code][_DISK_LANGUAGE.get()])


def _thin_subiteration(values, phase):
    """Guo–Hall, ordered neighbours E, NE, N, NW, W, SW, S, SE.

    Phase 0 removes with G3, phase 1 with G3p. The caller supplies an
    immutable global snapshot, so decisions never see same-phase deletions.
    """
    cells = values == 1  # NaN halo outside the image is background.
    nw, n, ne, w, center, e, sw, s, se = _boundary_neighbours(cells)
    neighbours = (e, ne, n, nw, w, sw, s, se)
    transitions = np.zeros(center.shape, dtype=np.uint8)
    pairs_a = np.zeros(center.shape, dtype=np.uint8)
    pairs_b = np.zeros(center.shape, dtype=np.uint8)
    for i in (0, 2, 4, 6):
        transitions += ~neighbours[i] & (neighbours[i+1] | neighbours[(i+2) % 8])
        pairs_a += neighbours[i] | neighbours[i+1]
        pairs_b += neighbours[i+1] | neighbours[(i+2) % 8]
    count = np.minimum(pairs_a, pairs_b)
    protected = ((ne | n | ~se) & e) if phase == 0 else ((sw | s | ~nw) & w)
    remove = center & (transitions == 1) & (count >= 2) & (count <= 3) & ~protected
    return (center & ~remove).astype(np.uint8), bool(remove.any())


def raster_thin(input_raster, band, foreground_value):
    """Thin one exact classification to convergence using two disk snapshots."""
    dataset = _open(input_raster)
    if type(band) is not int or not 1 <= band <= dataset.RasterCount:
        raise _thin_error('band')
    selected = dataset.GetRasterBand(band)
    if selected.GetColorInterpretation() == gdal.GCI_AlphaBand:
        raise _thin_error('alpha')
    for index in range(1, dataset.RasterCount+1):
        candidate = dataset.GetRasterBand(index)
        if (index == band or candidate.GetColorInterpretation() == gdal.GCI_AlphaBand) and gdal.DataTypeIsComplex(candidate.DataType):
            raise _thin_error('complex')
    selected = candidate = None
    if (isinstance(foreground_value, bool) or not isinstance(foreground_value, (int, float))
            or not -(2**53-1) <= foreground_value <= 2**53-1
            or foreground_value != int(foreground_value)):
        raise _thin_error('foreground')
    foreground_value = int(foreground_value)
    # Validate native integer values before Float64 conversion can round them.
    for _, _, arrays, masks in _blocks(dataset, [band], native=True):
        try:
            _safe_classification_values(arrays[0], masks[0])
        except ValueError as error:
            raise _thin_error('classes') from error
    paths = []
    snapshot = destination = output = target = None
    output_path = None
    completed = False
    try:
        paths = [_path(), _path()]
        destination = gdal.GetDriverByName('GTiff').Create(
            str(paths[0]), dataset.RasterXSize, dataset.RasterYSize, 1,
            gdal.GDT_Byte, ['TILED=YES', 'COMPRESS=DEFLATE'])
        target = destination.GetRasterBand(1)
        for x, y, arrays, masks in _blocks(dataset, [band], native=True):
            target.WriteArray((masks[0] & (arrays[0] == foreground_value)).astype(np.uint8), x, y)
        target = None
        destination.FlushCache()
        destination = None
        while True:
            changed = False
            for phase in (0, 1):
                snapshot = gdal.Open(str(paths[0]))
                destination = gdal.GetDriverByName('GTiff').Create(
                    str(paths[1]), dataset.RasterXSize, dataset.RasterYSize, 1,
                    gdal.GDT_Byte, ['TILED=YES', 'COMPRESS=DEFLATE'])
                target = destination.GetRasterBand(1)
                for x, y, values in _cell_neighbourhood_blocks(snapshot, 1):
                    result, removed = _thin_subiteration(values, phase)
                    changed |= removed
                    target.WriteArray(result, x, y)
                target = None
                destination.FlushCache()
                destination = snapshot = None
                paths.reverse()
            if not changed:
                break
        snapshot = gdal.Open(str(paths[0]))
        output = _numeric_output(dataset, 1)
        output_path = Path(output.GetDescription())
        target = output.GetRasterBand(1)
        for x, y, arrays, masks in _blocks(dataset, [band]):
            height, width = arrays[0].shape
            binary = snapshot.GetRasterBand(1).ReadAsArray(x, y, width, height)
            target.WriteArray(np.where(masks[0], binary, np.nan), x, y)
        target = None
        output.FlushCache()
        output = None
        result = _raster(output_path)
        completed = True
        return result
    finally:
        target = output = destination = snapshot = dataset = None
        for path in paths:
            path.unlink(missing_ok=True)
        if output_path is not None and not completed:
            output_path.unlink(missing_ok=True)


def raster_aggregate(input_raster, factors, method):
    """Reduce integer pixel groups with bounded reads and independent band validity."""
    factor_x, factor_y = _pair(factors, 'factors', integer=True)
    if method not in ('sum', 'mean', 'min', 'max'):
        raise ValueError('method must be sum, mean, min or max')
    dataset = _open(input_raster)
    indices = _data_indices(dataset)
    width = (dataset.RasterXSize + factor_x - 1) // factor_x
    height = (dataset.RasterYSize + factor_y - 1) // factor_y
    output = _numeric_output(dataset, len(indices), size=(width, height))
    transform = dataset.GetGeoTransform(can_return_null=True)
    if transform is not None:
        output.SetGeoTransform((transform[0], transform[1] * factor_x, transform[2] * factor_y,
                                transform[3], transform[4] * factor_x, transform[5] * factor_y))
    for destination_index, index in enumerate(indices, 1):
        destination = output.GetRasterBand(destination_index)
        for oy in range(0, height, 512):
            for ox in range(0, width, 512):
                columns, rows = min(512, width - ox), min(512, height - oy)
                sx, sy = ox * factor_x, oy * factor_y
                window = (sx, sy, min(columns * factor_x, dataset.RasterXSize - sx),
                          min(rows * factor_y, dataset.RasterYSize - sy))

                def cells():
                    for x, y, arrays, masks in _blocks(dataset, [index], window=window):
                        values, valid = arrays[0], masks[0]
                        group_x = (np.arange(values.shape[1]) + x - sx) // factor_x
                        group_y = (np.arange(values.shape[0]) + y - sy) // factor_y
                        groups = group_y[:, None] * columns + group_x[None, :]
                        yield groups[valid], values[valid]

                counts = np.zeros(rows * columns, dtype=np.int64)
                if method == 'mean':
                    scales = np.zeros(rows * columns, dtype=np.float64)
                    for groups, values in cells():
                        np.add.at(counts, groups, 1)
                        np.maximum.at(scales, groups, np.abs(values))
                    scales[scales == 0] = 1.0
                initial = math.inf if method == 'min' else -math.inf if method == 'max' else 0.0
                reduced = np.full(rows * columns, initial, dtype=np.float64)
                reduce = np.minimum if method == 'min' else np.maximum if method == 'max' else np.add
                with np.errstate(over='ignore', invalid='ignore'):
                    for groups, values in cells():
                        if method == 'mean':
                            values = (values / scales[groups]) / counts[groups]
                        else:
                            np.add.at(counts, groups, 1)
                        reduce.at(reduced, groups, values)
                    if method == 'mean':
                        # A normalized mean stays in [-1, 1]; contain rounding at the finite limit.
                        np.clip(reduced, -1.0, 1.0, out=reduced)
                        reduced *= scales
                reduced[(counts == 0) | ~np.isfinite(reduced)] = np.nan
                destination.WriteRaster(ox, oy, columns, rows, reduced.tobytes(), buf_type=gdal.GDT_Float64)
    destination = None
    output.FlushCache()
    path = output.GetDescription()
    output = None
    return _raster(path)


def _classification_rules(rules):
    """Compile disjoint exact values and half-open intervals for binary lookup."""
    def number(value):
        if isinstance(value, bool) or not isinstance(value, (int, float)):
            raise ValueError('Classification values must be finite numbers')
        try:
            result = float(value)
        except OverflowError as exc:
            raise ValueError('Classification values must be finite numbers') from exc
        if not math.isfinite(result):
            raise ValueError('Classification values must be finite numbers')
        return result

    if not isinstance(rules, list) or not rules:
        raise ValueError('rules must be a non-empty array')
    compiled = []
    for rule in rules:
        if not isinstance(rule, dict):
            raise ValueError('Each classification rule must be an object')
        if set(rule) == {'value', 'class'}:
            lower = upper = number(rule['value'])
            inclusive = True
        elif set(rule) == {'min', 'max', 'class'}:
            lower = -math.inf if rule['min'] is None else number(rule['min'])
            upper = math.inf if rule['max'] is None else number(rule['max'])
            if lower >= upper:
                raise ValueError('Classification intervals require min < max')
            inclusive = False
        else:
            raise ValueError('Rule must specify value/class or min/max/class only')
        compiled.append((lower, upper, number(rule['class']), inclusive))
    compiled.sort(key=lambda item: (item[0], item[1]))
    for previous, current in zip(compiled, compiled[1:]):
        if previous[1] > current[0] or (previous[1] == current[0] and previous[3]):
            raise ValueError('Classification rules must not overlap or duplicate')
    lower, upper, classes, inclusive = zip(*compiled)
    return (np.asarray(lower), np.asarray(upper), np.asarray(classes), np.asarray(inclusive))


def raster_reclassify(input_raster, rules, band=1, unmatched='nodata'):
    """Classify one data band without changing its grid or reviving invalid pixels."""
    if unmatched not in ('nodata', 'keep'):
        raise ValueError('unmatched must be nodata or keep')
    lower, upper, classes, inclusive = _classification_rules(rules)
    dataset = _open(input_raster)
    selected = _band(dataset, band)
    if selected.GetColorInterpretation() == gdal.GCI_AlphaBand:
        raise ValueError('Alpha is coverage, not a classification data band')
    output = _numeric_output(dataset, 1)
    destination = output.GetRasterBand(1)
    for x, y, arrays, masks in _blocks(dataset, [band]):
        source, valid = arrays[0], masks[0]
        values = source[valid]
        indices = np.searchsorted(lower, values, side='right') - 1
        positions = np.maximum(indices, 0)
        matched = (indices >= 0) & ((values < upper[positions]) |
                                    (inclusive[positions] & (values == upper[positions])))
        classified = values.copy() if unmatched == 'keep' else np.full(values.shape, np.nan)
        classified[matched] = classes[positions[matched]]
        result = np.full(source.shape, np.nan)
        result[valid] = classified
        destination.WriteRaster(x, y, result.shape[1], result.shape[0], result.tobytes(),
                                buf_type=gdal.GDT_Float64)
    destination = None
    output.FlushCache()
    path = output.GetDescription()
    output = None
    return _raster(path)


def _copy_data_bands(sources):
    """Convert coverage to per-band NaN, keeping at most one 512-square band in memory."""
    output = _numeric_output(sources[0][0], sum(len(indices) for _, indices in sources))
    destination_index = 0
    for dataset, indices in sources:
        for index in indices:
            destination_index += 1
            band = output.GetRasterBand(destination_index)
            for x, y, arrays, masks in _blocks(dataset, [index]):
                values = arrays[0].copy()
                values[~masks[0]] = np.nan
                band.WriteRaster(x, y, values.shape[1], values.shape[0], values.tobytes(),
                                 buf_type=gdal.GDT_Float64)
    band = None
    output.FlushCache()
    return output


def raster_align(input_raster, reference_raster, resampling='nearest'):
    dataset, reference = _open(input_raster), _open(reference_raster)
    _grid(dataset)
    _grid(reference)
    algorithm = _algorithm(resampling)
    source = _copy_data_bands([(dataset, _data_indices(dataset))])
    output = _numeric_output(reference, source.RasterCount)
    path = output.GetDescription()
    # Updating a pre-created dataset retains the complete reference affine grid,
    # including rotation. Normalize coverage first so each band warps independently.
    result = gdal.Warp(output, source, options=gdal.WarpOptions(
        resampleAlg=algorithm, srcAlpha=False, srcNodata=float('nan'), dstNodata=float('nan'),
        overviewLevel='NONE', errorThreshold=0,
        warpOptions=['INIT_DEST=NO_DATA', 'UNIFIED_SRC_NODATA=NO']))
    if not result:
        raise ValueError('GDAL raster alignment failed')
    output.FlushCache()
    output = source = None
    return _raster(path)


def _require_same_grid(dataset, other):
    if _grid(dataset) != _grid(other) or not _crs(dataset.GetProjection()).IsSame(
        _crs(other.GetProjection()), ['CRITERION=EQUIVALENT_EXCEPT_AXIS_ORDER_GEOGCRS',
                                    'IGNORE_DATA_AXIS_TO_SRS_AXIS_MAPPING=YES']):
        raise ValueError('Raster grids must match; use raster_align before combining rasters')


def raster_update(input_raster, update_raster):
    dataset, update = _open(input_raster), _open(update_raster)
    _require_same_grid(dataset, update)
    indices, update_indices = _data_indices(dataset), _data_indices(update)
    if len(indices) != len(update_indices):
        raise ValueError('Raster update inputs must have the same data band count')
    output = _numeric_output(dataset, len(indices))
    for destination_index, (base_index, update_index) in enumerate(zip(indices, update_indices), 1):
        destination = output.GetRasterBand(destination_index)
        for x, y, arrays, masks in _blocks(dataset, [base_index]):
            values = arrays[0].copy()
            values[~masks[0]] = np.nan
            height, width = values.shape
            _, _, updates, update_masks = next(_blocks(
                update, [update_index], window=(x, y, width, height)))
            values[update_masks[0]] = updates[0][update_masks[0]]
            destination.WriteRaster(x, y, width, height, values.tobytes(), buf_type=gdal.GDT_Float64)
    destination = None
    output.FlushCache()
    path = output.GetDescription()
    output = None
    return _raster(path)


def raster_stack(input_raster, other_raster):
    dataset, other = _open(input_raster), _open(other_raster)
    _require_same_grid(dataset, other)
    output = _copy_data_bands([(dataset, _data_indices(dataset)), (other, _data_indices(other))])
    path = output.GetDescription()
    output = None
    return _raster(path)


def raster_select_bands(input_raster, bands, color_model='multispectral'):
    dataset = _open(input_raster)
    if not isinstance(bands, list) or not bands:
        raise ValueError('bands must be a non-empty list of one-based data band indices')
    for index in bands:
        if _band(dataset, index).GetColorInterpretation() == gdal.GCI_AlphaBand:
            raise ValueError('Alpha is coverage, not a selectable data band')
    if color_model not in ('multispectral', 'rgb'):
        raise ValueError('Unsupported color_model')
    if color_model == 'rgb' and len(bands) != 3:
        raise ValueError('RGB requires exactly three data bands')
    output = _copy_data_bands([(dataset, bands)])
    if color_model == 'rgb':
        for index, interpretation in enumerate((gdal.GCI_RedBand, gdal.GCI_GreenBand, gdal.GCI_BlueBand), 1):
            output.GetRasterBand(index).SetColorInterpretation(interpretation)
    path = output.GetDescription()
    output = None
    return _raster(path)


_ELEVATION_METRES = {'metre': 1., 'foot': .3048, 'us_survey_foot': 1200 / 3937}
_ELEVATION_UNIT_NAMES = {'m': 'metre', 'meter': 'metre', 'metre': 'metre',
                         'ft': 'foot', 'foot': 'foot', 'foot international': 'foot',
                         'us survey foot': 'us_survey_foot', 'us_survey_foot': 'us_survey_foot'}


def _projected_north_up_grid(dataset):
    _, _, transform = _grid(dataset)
    reference = _crs(dataset.GetProjection())
    horizontal_metres = reference.GetLinearUnits()
    if (not reference.IsProjected() or reference.IsCompound() or reference.GetAxesCount() != 2
        or not math.isfinite(horizontal_metres) or horizontal_metres <= 0
        or reference.GetLinearUnitsName().lower() in ('unknown', '')
        or transform[1] <= 0 or transform[5] >= 0 or transform[2] != 0 or transform[4] != 0):
        raise ValueError('Raster analysis requires a projected north-up grid with known linear units; reproject explicitly')
    return horizontal_metres


def _nearest_line_sites(cost, *, valid=None, x_weight=1, y_weight=1):
    """The sole exact anisotropic integer-cost lower envelope; low site wins ties."""
    if any(type(w) is not int or w <= 0 for w in (x_weight, y_weight)):
        raise ValueError('Distance axis weights must be positive integers')
    sites = np.flatnonzero(np.isfinite(cost) if valid is None else valid)
    if not len(sites):
        return np.full(cost.shape, -1, dtype=np.int64)
    if any(not np.isfinite(cost[site]) or int(cost[site]) != cost[site] for site in sites):
        raise ValueError('Squared raster distance costs must be finite integers')
    vertices = np.empty(len(sites), dtype=np.int64)
    boundaries = [None] * (len(sites) + 1)
    vertices[0] = sites[0]
    top = 0
    for site in sites[1:]:
        while True:
            previous = vertices[top]
            numerator = (x_weight * (int(cost[site]) - int(cost[previous]))
                         + y_weight * (int(site) - int(previous)) * (int(site) + int(previous)))
            crossing = numerator // (2 * y_weight * (int(site) - int(previous))) + 1
            if top == 0 or crossing > boundaries[top]:
                break
            top -= 1
        top += 1
        vertices[top] = site
        boundaries[top] = crossing
    coordinates = np.arange(len(cost), dtype=np.int64)
    # Clipping only for search storage is exact on the queried domain [0,n).
    thresholds = np.asarray([max(-1, min(len(cost), value))
                             for value in boundaries[1:top + 1]], dtype=np.int64)
    owners = vertices[np.searchsorted(thresholds, coordinates, side='right')]
    return owners


def _squared_distance_line(cost, *, valid=None, return_sites=False):
    """Unit-metric projection of the same envelope; preserve square-grid bits."""
    owners = _nearest_line_sites(cost, valid=valid)
    if np.all(owners < 0):
        distances = np.full(cost.shape, np.inf)
    else:
        coordinates = np.arange(len(cost), dtype=np.int64)
        distances = (np.square(coordinates - owners) + cost[owners].astype(np.int64)).astype(np.float64)
    return (distances, owners) if return_sites else distances


def _read_distance_values(stream, count):
    values = np.frombuffer(stream.read(count * 8), dtype=np.float64)
    if values.size != count:
        raise RuntimeError('Incomplete private distance intermediate')
    return values


def _transpose_private_file(source, target, width, height, dtype):
    """Transpose owned fixed-width storage with one bounded tile, no mmap."""
    dtype = np.dtype(dtype)
    size = dtype.itemsize
    with source.open('rb') as reader, target.open('xb+') as writer:
        writer.truncate(width * height * size)
        for y in range(0, height, 512):
            rows = min(512, height - y)
            for x in range(0, width, 512):
                columns = min(512, width - x)
                tile = np.empty((rows, columns), dtype=dtype)
                for row in range(rows):
                    reader.seek(((y + row) * width + x) * size)
                    values = np.frombuffer(reader.read(columns * size), dtype=dtype)
                    if values.size != columns:
                        raise RuntimeError('Incomplete private distance intermediate')
                    tile[row] = values
                for column in range(columns):
                    writer.seek(((x + column) * height + y) * size)
                    writer.write(tile[:, column].tobytes())


def _transpose_distance_file(source, target, width, height):
    _transpose_private_file(source, target, width, height, np.float64)


def _read_site_values(stream, count):
    values = np.frombuffer(stream.read(count * 8), dtype=np.int64)
    if values.size != count:
        raise RuntimeError('Incomplete private nearest-site intermediate')
    return values


@contextmanager
def _nearest_pixel_sites(dataset, band, target_values, *, non_targets=False, axis_weights=None):
    """One separable EDT for distances and original row/column donor identities."""
    width, height = dataset.RasterXSize, dataset.RasterYSize
    # GDAL raster dimensions are signed Int32.  Coordinate fields remain Int64;
    # two maximal squared offsets still fit Int64.  Never encode a flattened
    # donor identity in Float64, which would lose precision on large grids.
    if not (0 < width <= 2**31 - 1 and 0 < height <= 2**31 - 1):
        raise ValueError('Raster dimensions must fit positive signed Int32')
    rows_path, columns_path = _path('.distance'), _path('.distance')
    row_sites, column_sites, nearest_sites = _path('.sites'), _path('.sites'), _path('.sites')
    _DISK_PROTECTED.set(True)
    with _disk_budget(32 * width * height, 'distance transform',
                      private_paths=(rows_path, columns_path, row_sites, column_sites, nearest_sites)):
        indices = np.arange(width, dtype=np.int64)
        try:
            donor_count = 0
            with rows_path.open('xb') as rows, row_sites.open('xb') as sites:
                for row in range(height):
                    donors = np.zeros(width, dtype=bool)
                    for x, _, arrays, masks in _blocks(dataset, [band], window=(0, row, width, 1)):
                        matched = np.isin(arrays[0][0], target_values)
                        donors[x:x + matched.size] = masks[0][0] & (~matched if non_targets else matched)
                    donor_count += int(np.count_nonzero(donors))
                    left = np.maximum.accumulate(np.where(donors, indices, -1))
                    right = np.minimum.accumulate(np.where(donors, indices, width)[::-1])[::-1]
                    choose_left = (left >= 0) & ((right >= width) | (indices - left <= right - indices))
                    owners = np.where(choose_left, left, np.where(right < width, right, -1)).astype(np.int64)
                    present = owners >= 0
                    squared = np.where(present, np.square(indices - owners).astype(np.float64), np.inf)
                    rows.write(squared.tobytes())
                    sites.write(owners.tobytes())
            if donor_count == 0:
                raise ValueError('Raster distance transform requires at least one valid target pixel')
            _transpose_distance_file(rows_path, columns_path, width, height)
            _transpose_private_file(row_sites, column_sites, width, height, np.int64)
            with _DISK_LOCK:
                rows_path.unlink()
                row_sites.unlink()
            with columns_path.open('r+b') as columns, column_sites.open('rb') as sites, nearest_sites.open('xb') as nearest:
                for column in range(width):
                    columns.seek(column * height * 8)
                    stored = _read_distance_values(columns, height)
                    source_columns = _read_site_values(sites, height)
                    present = source_columns >= 0
                    if np.any(present & (source_columns >= width)):
                        raise RuntimeError('Invalid private nearest-site coordinate')
                    costs = np.square(np.int64(column) - source_columns)
                    if np.any(stored[present] != costs[present].astype(np.float64)):
                        raise RuntimeError('Inconsistent private distance intermediate')
                    if axis_weights is None:
                        distances, source_rows = _squared_distance_line(costs, valid=present, return_sites=True)
                        columns.seek(column * height * 8)
                        columns.write(distances.tobytes())
                    else:
                        source_rows = _nearest_line_sites(costs, valid=present,
                                                         x_weight=axis_weights[0], y_weight=axis_weights[1])
                    identities = np.empty((height, 2), dtype=np.int64)
                    identities[:, 0] = source_rows
                    identities[:, 1] = source_columns[source_rows]
                    nearest.write(identities.tobytes())
            yield columns_path if axis_weights is None else None, nearest_sites
        finally:
            with _DISK_LOCK:
                for path in (rows_path, columns_path, row_sites, column_sites, nearest_sites):
                    path.unlink(missing_ok=True)



_PROXIMITY_MESSAGES = {
    'grid': ('邻近距离需要已知线性单位的二维投影及保持北向轴手性的正交网格，请显式重投影', 'Proximity requires a two-dimensional projected orthogonal grid with known linear units and north-up handedness; reproject explicitly'),
    'step': ('邻近距离横纵米制步长必须为有限正数', 'Proximity horizontal and vertical metre steps must be finite and positive'),
    'numeric': ('邻近距离结果超出有限Float64米制范围', 'Proximity exceeds the finite Float64 metre distance range'),
}


def _proximity_error(code):
    return ValueError(_PROXIMITY_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _projected_orthogonal_grid_units(dataset, grid_error):
    """Validate the exact affine axes and return the CRS linear unit factor."""
    _, _, transform = _grid(dataset)
    reference = _crs(dataset.GetProjection())
    units = reference.GetLinearUnits()
    ux, uy, vx, vy = map(Fraction, (transform[1], transform[4], transform[2], transform[5]))
    if (not reference.IsProjected() or reference.IsCompound() or reference.GetAxesCount() != 2
            or not math.isfinite(units) or units <= 0
            or reference.GetLinearUnitsName().lower() in ('unknown', '')
            or ux*vx + uy*vy != 0 or ux*vy - uy*vx >= 0):
        raise grid_error()
    return units


def _proximity_grid_units(dataset):
    return _projected_orthogonal_grid_units(dataset, lambda: _proximity_error('grid'))


def _proximity_rectangular_distances(identities, valid, x, y, transform, units):
    """Project selected original sites to metres, without a rounded squared cost."""
    ux, uy, vx, vy = map(Fraction, (transform[1], transform[4], transform[2], transform[5]))
    factor = Fraction(units)
    result = np.full(valid.shape, np.nan, dtype=np.float64)
    for row, column in zip(*np.nonzero(valid)):
        dr = y + int(row) - int(identities[row, column, 0])
        dc = x + int(column) - int(identities[row, column, 1])
        dx, dy = dc*ux + dr*vx, dc*uy + dr*vy
        result[row, column] = _sqrt_fraction((dx*dx + dy*dy)*factor*factor,
                                            error=lambda: _proximity_error('numeric'))
    return result


def raster_proximity(input_raster, band, target_values):
    dataset = _open(input_raster)
    horizontal_metres = _proximity_grid_units(dataset)
    width, height, transform = _grid(dataset)
    if not math.isfinite(horizontal_metres) or horizontal_metres <= 0:
        raise _proximity_error('step')
    ux, uy, vx, vy = map(Fraction, (transform[1], transform[4], transform[2], transform[5]))
    column_squared, row_squared = ux*ux + uy*uy, vx*vx + vy*vy
    north_up = transform[2] == 0 and transform[4] == 0 and transform[1] > 0 and transform[5] < 0
    if north_up:
        # Preserve original north-up step admission and square output bits.
        step_metres = transform[1] * horizontal_metres
        vertical_metres = -transform[5] * horizontal_metres
    else:
        factor_squared = Fraction(horizontal_metres)**2
        step_metres = _sqrt_fraction(column_squared*factor_squared, error=lambda: _proximity_error('step'))
        vertical_metres = _sqrt_fraction(row_squared*factor_squared, error=lambda: _proximity_error('step'))
    if any(not math.isfinite(v) or v <= 0 for v in (step_metres, vertical_metres)):
        raise _proximity_error('step')
    axis_weights = None
    if not north_up or column_squared != row_squared:
        ratio = column_squared/row_squared
        axis_weights = (ratio.numerator, ratio.denominator)
    selected = _band(dataset, band)
    ranges = {gdal.GDT_Byte: (0, 255), gdal.GDT_Int8: (-128, 127),
              gdal.GDT_UInt16: (0, 65535), gdal.GDT_Int16: (-32768, 32767),
              gdal.GDT_UInt32: (0, 4294967295), gdal.GDT_Int32: (-2147483648, 2147483647)}
    if selected.GetColorInterpretation() == gdal.GCI_AlphaBand or selected.DataType not in ranges:
        raise ValueError('Proximity requires a non-alpha integer data band of at most 32 bits')
    lower, upper = ranges[selected.DataType]
    if (not isinstance(target_values, list) or not target_values
        or any(isinstance(v, bool) or not isinstance(v, Integral) or not lower <= v <= upper for v in target_values)
        or len(set(target_values)) != len(target_values)):
        raise ValueError('target_values must be distinct integers within the selected band type')
    for index in range(1, dataset.RasterCount + 1):
        if dataset.GetRasterBand(index).GetColorInterpretation() == gdal.GCI_AlphaBand:
            _band(dataset, index)
    _DISK_PROTECTED.set(True)
    with _disk_budget(_raster_disk_bytes(dataset, numeric=True), 'distance output'):
        output = destination = None
        output_path, completed = None, False
        try:
            with _nearest_pixel_sites(dataset, band, target_values, axis_weights=axis_weights) as (columns_path, sites_path):
                output = _numeric_output(dataset, 1)
                output_path = Path(output.GetDescription())
                destination = output.GetRasterBand(1)
                destination.SetUnitType('metre')
                with (columns_path if columns_path is not None else sites_path).open('rb') as intermediate:
                    for x, y, arrays, masks in _blocks(dataset, [band]):
                        rows, count = arrays[0].shape
                        if axis_weights is None:
                            distances = np.empty((rows, count), dtype=np.float64)
                            for column in range(count):
                                intermediate.seek(((x + column) * height + y) * 8)
                                distances[:, column] = _read_distance_values(intermediate, rows)
                            with np.errstate(over='ignore', under='ignore', invalid='ignore'):
                                distances = np.sqrt(distances) * step_metres
                            if np.any(masks[0] & ~np.isfinite(distances)):
                                raise ValueError('Proximity exceeds finite Float64 metre distances')
                            distances[~masks[0]] = np.nan
                        else:
                            identities = np.empty((rows, count, 2), dtype=np.int64)
                            for column in range(count):
                                intermediate.seek(((x + column) * height + y) * 16)
                                identities[:, column] = _read_site_values(intermediate, rows * 2).reshape(-1, 2)
                            if (np.any(identities[:, :, 0] < 0) or np.any(identities[:, :, 0] >= height)
                                    or np.any(identities[:, :, 1] < 0) or np.any(identities[:, :, 1] >= width)):
                                raise RuntimeError('Invalid private nearest-site coordinate')
                            distances = _proximity_rectangular_distances(identities, masks[0], x, y,
                                                                      transform, horizontal_metres)
                        destination.WriteArray(distances, x, y)
            path = output.GetDescription()
            destination = None
            output.FlushCache()
            output = None
            result = _raster(path)
            completed = True
            return result
        finally:
            destination = output = None
            if output_path is not None and not completed:
                output_path.unlink(missing_ok=True)


def raster_nibble(input_raster, band, target_values):
    """Replace selected valid classes from the nearest original non-target site."""
    dataset = _open(input_raster)
    _projected_north_up_grid(dataset)
    _, height, transform = _grid(dataset)
    if transform[1] != -transform[5]:
        raise ValueError('Nibble requires square pixels; resample explicitly')
    selected = _band(dataset, band)
    if selected.GetColorInterpretation() == gdal.GCI_AlphaBand:
        raise ValueError('Nibble requires a classification data band, not alpha')
    if (not isinstance(target_values, list) or not target_values
        or any(isinstance(value, bool) or not isinstance(value, Integral)
               or not -(2**53 - 1) <= value <= 2**53 - 1 for value in target_values)
        or len(set(target_values)) != len(target_values)):
        raise ValueError('target_values must be distinct exact safe integers')
    for index in range(1, dataset.RasterCount + 1):
        if dataset.GetRasterBand(index).GetColorInterpretation() == gdal.GCI_AlphaBand:
            _band(dataset, index)
    matches = donors = 0
    for _, _, arrays, masks in _blocks(dataset, [band], native=True):
        _safe_classification_values(arrays[0], masks[0])
        matched = masks[0] & np.isin(arrays[0], target_values)
        matches += int(np.count_nonzero(matched))
        donors += int(np.count_nonzero(masks[0] & ~matched))
    if matches and donors == 0:
        raise ValueError('Nibble requires at least one valid non-target donor pixel')
    _DISK_PROTECTED.set(True)
    with _disk_budget(_raster_disk_bytes(dataset, numeric=True), 'nibble output'):
        output = destination = None
        try:
            if matches == 0:
                output = _copy_data_bands([(dataset, [band])])
                path = output.GetDescription()
                output.FlushCache()
                output = None
                return _raster(path)
            with _nearest_pixel_sites(dataset, band, target_values, non_targets=True) as (_, sites_path):
                output = _numeric_output(dataset, 1)
                destination = output.GetRasterBand(1)
                with sites_path.open('rb') as sites:
                    for x, y, arrays, masks in _blocks(dataset, [band]):
                        values, valid = arrays[0].copy(), masks[0]
                        matched = valid & np.isin(values, target_values)
                        identities = np.empty((*values.shape, 2), dtype=np.int64)
                        for column in range(values.shape[1]):
                            sites.seek(((x + column) * height + y) * 16)
                            identities[:, column] = _read_site_values(sites, values.shape[0] * 2).reshape(-1, 2)
                        # Group original donor reads by source tile.  Memory and
                        # each GDAL read remain bounded independently of scene size.
                        rows, columns = np.nonzero(matched)
                        donor_rows, donor_columns = identities[rows, columns].T
                        if np.any((donor_rows < 0) | (donor_rows >= dataset.RasterYSize)
                                  | (donor_columns < 0) | (donor_columns >= dataset.RasterXSize)):
                            raise RuntimeError('Invalid private nearest-site coordinate')
                        tile_keys = np.stack((donor_rows // 512, donor_columns // 512), axis=1)
                        for tile_row, tile_column in np.unique(tile_keys, axis=0):
                            within = (tile_keys[:, 0] == tile_row) & (tile_keys[:, 1] == tile_column)
                            sx, sy = int(tile_column) * 512, int(tile_row) * 512
                            sw, sh = min(512, dataset.RasterXSize - sx), min(512, dataset.RasterYSize - sy)
                            source = _read_values(selected, sx, sy, sw, sh)
                            values[rows[within], columns[within]] = source[donor_rows[within] - sy, donor_columns[within] - sx]
                        values[~valid] = np.nan
                        destination.WriteArray(values, x, y)
            path = output.GetDescription()
            destination = None
            output.FlushCache()
            output = None
            return _raster(path)
        finally:
            destination = output = None


_FILL_MESSAGES = {
    'grid': ('DEM 填洼需要已知线性单位的二维投影、北向无旋转网格，请先显式重投影', 'DEM depression filling requires a projected north-up grid with known linear units; reproject explicitly'),
    'band': ('DEM 填洼必须显式选择有效的一基高程数据波段', 'DEM depression filling requires an explicit valid one-based elevation band'),
    'complex': ('DEM 填洼不支持复数高程或复数 alpha 波段', 'DEM depression filling does not support complex elevation or alpha bands'),
    'alpha': ('DEM 填洼不能选择 alpha 波段作为高程', 'DEM depression filling cannot use an alpha band as elevation'),
    'unit': ('DEM 填洼必须明确声明 metre、foot 或 us_survey_foot 高程单位', 'DEM depression filling requires elevation_unit metre, foot or us_survey_foot'),
    'unit-conflict': ('DEM 填洼高程波段单位未知或与声明单位冲突', 'DEM depression filling band unit is unknown or conflicts with elevation_unit'),
    'scale': ('DEM 填洼前必须显式应用高程 scale/offset', 'DEM depression filling requires elevation scale/offset to be applied first'),
    'overflow': ('DEM 填洼高程转换为米时溢出', 'DEM depression filling elevation conversion to metres overflowed'),
}


def _fill_error(code):
    return ValueError(_FILL_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _dem_admission(input_raster, elevation_unit, band, minimum_size, *, diagnostic=None,
                   grid_units=None):
    # One DEM admission path; preserve native values for consumers needing exact comparisons.
    def invalid(code, message):
        return diagnostic(code) if diagnostic is not None else ValueError(message)
    dataset = _open(input_raster)
    try:
        horizontal_metres = (grid_units or _projected_north_up_grid)(dataset)
    except ValueError as error:
        if diagnostic is None:
            raise
        raise diagnostic('grid') from error
    if min(dataset.RasterXSize, dataset.RasterYSize) < minimum_size:
        raise ValueError(f'DEM must be at least {minimum_size} by {minimum_size} pixels')
    if not isinstance(elevation_unit, str) or elevation_unit not in _ELEVATION_METRES:
        raise invalid('unit', 'elevation_unit must explicitly be metre, foot or us_survey_foot')
    try:
        source_band = _band(dataset, band)
    except ValueError as error:
        if diagnostic is None:
            raise
        code = 'complex' if type(band) is int and 1 <= band <= dataset.RasterCount else 'band'
        raise diagnostic(code) from error
    if source_band.GetColorInterpretation() == gdal.GCI_AlphaBand:
        raise invalid('alpha', 'DEM elevation band cannot be alpha')
    unit = source_band.GetUnitType().strip().lower()
    if unit and _ELEVATION_UNIT_NAMES.get(unit) != elevation_unit:
        raise invalid('unit-conflict', 'DEM band elevation unit is unknown or conflicts with elevation_unit')
    if source_band.GetScale() not in (None, 1) or source_band.GetOffset() not in (None, 0):
        raise invalid('scale', 'DEM encoded scale/offset must be applied before terrain analysis')
    # _blocks excludes alpha coverage; reject complex alpha before numeric decoding.
    for index in range(1, dataset.RasterCount + 1):
        candidate = dataset.GetRasterBand(index)
        if candidate.GetColorInterpretation() == gdal.GCI_AlphaBand and gdal.DataTypeIsComplex(candidate.DataType):
            raise invalid('complex', 'DEM complex alpha bands are not supported')
    candidate = None
    return dataset, horizontal_metres


_CUT_FILL_MESSAGES = {
    role + '_' + key: (zh.replace('DEM 填洼', '双DEM填挖方' + label),
                       en.replace('DEM depression filling', 'DEM cut/fill ' + role))
    for role, label in (('before', '施工前'), ('after', '施工后'))
    for key, (zh, en) in _FILL_MESSAGES.items() if key != 'overflow'
}
_CUT_FILL_MESSAGES.update({
    'alignment': ('双DEM填挖方的两份DEM必须具有相同CRS、尺寸和完整网格，请先显式对齐',
                  'DEM cut/fill inputs must have the same CRS, dimensions and complete grid; align explicitly first'),
    'empty': ('双DEM填挖方没有共同有效像元，不能生成零土方结果',
              'DEM cut/fill has no jointly valid pixels; a zero-volume result cannot be produced'),
    'range': ('双DEM填挖方的面积或体积超出有限Float64范围，非零量不能下溢为零',
              'DEM cut/fill area or volume exceeds finite Float64 range; a nonzero quantity must not underflow to zero'),
})


def _cut_fill_error(code):
    return ValueError(_CUT_FILL_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _cut_fill_float(value):
    try:
        result = float(value)
    except OverflowError as cause:
        raise _cut_fill_error('range') from cause
    if not math.isfinite(result) or (value != 0 and result == 0):
        raise _cut_fill_error('range')
    return result


def raster_cut_fill(input_raster, after_raster, band, after_band, elevation_unit, after_elevation_unit):
    """Integrate vertical columns on the exact common grid, retaining native differences."""
    before, horizontal = _dem_admission(input_raster, elevation_unit, band, 1,
                                       diagnostic=lambda key: _cut_fill_error('before_' + key))
    after, _ = _dem_admission(after_raster, after_elevation_unit, after_band, 1,
                             diagnostic=lambda key: _cut_fill_error('after_' + key))
    try:
        _require_same_grid(before, after)
    except ValueError as cause:
        raise _cut_fill_error('alignment') from cause
    transform = before.GetGeoTransform()
    area = Fraction(transform[1]) * Fraction(-transform[5]) * Fraction(horizontal) ** 2
    _cut_fill_float(area)
    before_factor = Fraction(_ELEVATION_METRES[elevation_unit])
    after_factor = Fraction(_ELEVATION_METRES[after_elevation_unit])
    cut = fill = Fraction(0)
    cut_count = fill_count = unchanged_count = 0
    for x, y, arrays, masks in _blocks(before, [band], native=True):
        height, width = arrays[0].shape
        _, _, later, later_masks = next(_blocks(after, [after_band], window=(x, y, width, height), native=True))
        jointly_valid = masks[0] & later_masks[0]
        # tolist preserves native integers; no whole-image storage or premature
        # Float64 conversion. Rational arithmetic also handles unlike vertical units.
        for original, updated in zip(arrays[0][jointly_valid].tolist(), later[0][jointly_valid].tolist()):
            delta = Fraction(updated) * after_factor - Fraction(original) * before_factor
            if delta > 0:
                fill += delta
                fill_count += 1
            elif delta < 0:
                cut -= delta
                cut_count += 1
            else:
                unchanged_count += 1
    valid_count = cut_count + fill_count + unchanged_count
    if valid_count == 0:
        raise _cut_fill_error('empty')
    total = before.RasterXSize * before.RasterYSize
    counts = dict(zip(('total_count', 'valid_count', 'invalid_count', 'cut_count', 'fill_count', 'unchanged_count'),
                      (total, valid_count, total-valid_count, cut_count, fill_count, unchanged_count)))
    measures = dict(zip(('valid_area_m2', 'cut_area_m2', 'fill_area_m2', 'unchanged_area_m2',
                         'cut_volume_m3', 'fill_volume_m3', 'net_increase_m3'),
                        (valid_count*area, cut_count*area, fill_count*area, unchanged_count*area,
                         cut*area, fill*area, (fill-cut)*area)))
    return pd.DataFrame({**{name: pd.array([value], dtype='int64') for name, value in counts.items()},
                         **{name: pd.array([_cut_fill_float(value)], dtype='Float64') for name, value in measures.items()}})



_SURFACE_AREA_MESSAGES = {
    key: (zh.replace('DEM 填洼', 'DEM 三维表面积统计'),
          en.replace('DEM depression filling', 'DEM surface area statistics'))
    for key, (zh, en) in _FILL_MESSAGES.items() if key != 'overflow'
}
_SURFACE_AREA_MESSAGES.update({
    'empty': ('DEM 三维表面积统计没有四角完整有效的面片，不能生成零面积结果',
              'DEM surface area statistics has no complete valid quadrilaterals; a zero-area result cannot be produced'),
    'numeric': ('DEM 三维表面积统计的米制步长、面积或比值超出有限Float64范围，非零量不能下溢为零',
                'DEM surface area statistics metre spacing, area or ratio exceeds finite Float64 range; a nonzero quantity must not underflow to zero'),
})


def _surface_area_error(code):
    return ValueError(_SURFACE_AREA_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _surface_area_float(value):
    try:
        number = float(value)
    except (OverflowError, ValueError) as cause:
        raise _surface_area_error('numeric') from cause
    if not math.isfinite(number) or (value != 0 and number == 0):
        raise _surface_area_error('numeric')
    return number


def raster_surface_area(input_raster, band, elevation_unit):
    """Average both triangulations per complete centre-to-centre quadrilateral."""
    from decimal import Decimal, localcontext
    source, horizontal = _dem_admission(input_raster, elevation_unit, band, 1,
                                        diagnostic=_surface_area_error)
    transform = source.GetGeoTransform()
    dx = Fraction(transform[1]) * Fraction(horizontal)
    dy = Fraction(-transform[5]) * Fraction(horizontal)
    _surface_area_float(dx)
    _surface_area_float(dy)
    area = dx * dy
    factor = Fraction(_ELEVATION_METRES[elevation_unit])
    valid_count = 0
    # A Decimal context avoids unnecessary binary64 overflow in squared norms
    # and diagonal sums. Native differences are exact before this conversion.
    with localcontext() as context:
        context.prec = 80
        def decimal(value):
            return Decimal(value.numerator) / Decimal(value.denominator)
        def triangle(x_difference, y_difference):
            squared = area * area + (dy * x_difference) ** 2 + (dx * y_difference) ** 2
            return decimal(squared).sqrt() / 2
        low = high = Decimal(0)
        for y in range(0, source.RasterYSize - 1, 511):
            for x in range(0, source.RasterXSize - 1, 511):
                width = min(512, source.RasterXSize - x)
                height = min(512, source.RasterYSize - y)
                _, _, arrays, masks = next(_blocks(source, [band], window=(x, y, width, height), native=True))
                values, valid = arrays[0], masks[0]
                complete = valid[:-1, :-1] & valid[:-1, 1:] & valid[1:, :-1] & valid[1:, 1:]
                if not complete.any():
                    continue
                # Group only this bounded block; no persistent cache or full DEM.
                quads = np.stack((values[:-1, :-1][complete], values[:-1, 1:][complete],
                                  values[1:, :-1][complete], values[1:, 1:][complete]), axis=1)
                unique, counts = np.unique(quads, axis=0, return_counts=True)
                for samples, count in zip(unique.tolist(), counts.tolist()):
                    a, b, c, d = [Fraction(value) for value in samples]
                    ab, ac = (b - a) * factor, (c - a) * factor
                    cd, bd = (d - c) * factor, (d - b) * factor
                    diagonal_ad = triangle(ab, bd) + triangle(cd, ac)
                    diagonal_bc = triangle(ab, ac) + triangle(cd, bd)
                    low += min(diagonal_ad, diagonal_bc) * count
                    high += max(diagonal_ad, diagonal_bc) * count
                    valid_count += count
        if valid_count == 0:
            raise _surface_area_error('empty')
        projected = decimal(area * valid_count)
        mean = (low + high) / 2
        measures = {'projected_area_m2': projected, 'surface_area_low_m2': low,
                    'surface_area_high_m2': high, 'surface_area_mean_m2': mean,
                    'surface_to_projected_ratio': mean / projected}
        total = max(source.RasterXSize - 1, 0) * max(source.RasterYSize - 1, 0)
        counts = {'total_quad_count': total, 'valid_quad_count': valid_count,
                  'invalid_quad_count': total - valid_count}
        return pd.DataFrame({**{name: pd.array([value], dtype='int64') for name, value in counts.items()},
                             **{name: pd.array([_surface_area_float(value)], dtype='Float64')
                                for name, value in measures.items()}})

_SIGHT_MESSAGES = {
    key: (zh.replace('DEM 填洼', 'DEM 两点通视'), en.replace('DEM depression filling', 'DEM line of sight'))
    for key, (zh, en) in _FILL_MESSAGES.items() if key != 'overflow'
}
_SIGHT_MESSAGES.update({
    'grid': ('DEM 两点通视需要已知线性单位的二维投影及保持北向轴手性的正交网格，请显式重投影',
             'DEM line of sight requires a two-dimensional projected orthogonal grid with known linear units and north-up handedness; reproject explicitly'),
    'xy': ('DEM 两点通视的每个端点必须是两个有限投影坐标 [x,y]',
           'DEM line of sight requires two finite projected coordinates [x,y] for each endpoint'),
    'height': ('DEM 两点通视的两端离地高度必须是有限非负米数',
               'DEM line of sight requires finite nonnegative heights above ground in metres'),
    'coincident': ('DEM 两点通视的观察点和目标点不能重合',
                   'DEM line of sight requires distinct observer and target locations'),
    'outside': ('DEM 两点通视的端点超出栅格范围', 'DEM line of sight endpoint is outside the raster'),
    'invalid': ('DEM 两点通视的端点落入无效高程像元', 'DEM line of sight endpoint has no valid elevation'),
    'numeric': ('DEM 两点通视的米制水平距离超出有限Float64范围或下溢为零',
                'DEM line of sight horizontal distance in metres exceeds finite Float64 range or underflows to zero'),
})


def _sight_error(code):
    return ValueError(_SIGHT_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _sight_number(value, code, error=_sight_error):
    try:
        if (isinstance(value, (bool, np.bool_)) or not isinstance(value, Real)
                or not math.isfinite(float(value))):
            raise error(code)
    except OverflowError as error:
        raise error(code) from error
    return Fraction(int(value) if isinstance(value, Integral) else float(value))


def _sight_crossings(first, last):
    if first < last:
        boundaries = range(math.floor(first) + 1, math.ceil(last))
    elif first > last:
        boundaries = range(math.ceil(first) - 1, math.floor(last), -1)
    else:
        return
    for boundary in boundaries:
        yield (boundary - first) / (last - first)


def _sight_spans(observer, target):
    """Yield the cells intersected over each positive-length line interval."""
    x, y = observer
    tx, ty = target
    dx, dy = tx - x, ty - y
    crossings = heapq.merge(_sight_crossings(x, tx), _sight_crossings(y, ty), (Fraction(0), Fraction(1)))
    for entry, exit in pairwise(crossings):
        if entry == exit:
            continue  # Simultaneous crossings only touch the other cells at a corner.
        midpoint = (entry + exit) / 2
        col, row = math.floor(x + dx * midpoint), math.floor(y + dy * midpoint)
        cols = (col - 1, col) if dx == 0 and x == col and col > 0 else (col,)
        rows = (row - 1, row) if dy == 0 and y == row and row > 0 else (row,)
        for r in rows:
            for c in cols:
                yield entry, exit, c, r


class _SightDEM:
    """One native DEM reader and one exact sightline decision path."""
    def __init__(self, input_raster, band, elevation_unit, error):
        self.error = error
        self.source, horizontal = _dem_admission(
            input_raster, elevation_unit, band, 1, diagnostic=error,
            grid_units=lambda source: _projected_orthogonal_grid_units(source, lambda: error('grid')))
        self.band = band
        self.width, self.height = self.source.RasterXSize, self.source.RasterYSize
        self.x0, self.ux, self.vx, self.y0, self.uy, self.vy = map(Fraction, self.source.GetGeoTransform())
        self.determinant = self.ux*self.vy - self.vx*self.uy
        self.vertical = Fraction(_ELEVATION_METRES[elevation_unit])
        factor_squared = Fraction(horizontal)**2
        self.column_squared = (self.ux**2 + self.uy**2)*factor_squared
        self.row_squared = (self.vx**2 + self.vy**2)*factor_squared
        self.tile = lru_cache(maxsize=4)(self._read_tile)

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        self.tile.cache_clear()
        self.tile = self.source = None

    def _read_tile(self, x, y):
        window = (x, y, min(256, self.width-x), min(256, self.height-y))
        _, _, arrays, masks = next(_blocks(self.source, [self.band], window=window, native=True))
        return arrays[0], masks[0]

    def pixel(self, point):
        if not isinstance(point, (list, tuple)) or len(point) != 2:
            raise self.error('xy')
        x, y = (_sight_number(value, 'xy', self.error) for value in point)
        dx, dy = x-self.x0, y-self.y0
        col = (dx*self.vy - dy*self.vx)/self.determinant
        row = (dy*self.ux - dx*self.uy)/self.determinant
        if not (0 <= col < self.width and 0 <= row < self.height):
            raise self.error('outside')
        return col, row

    def ground(self, col, row):
        x, y = col//256*256, row//256*256
        values, valid = self.tile(x, y)
        return Fraction(values[row-y, col-x].item())*self.vertical if valid[row-y, col-x] else None

    def height_offset(self, value):
        height = _sight_number(value, 'height', self.error)
        if height < 0:
            raise self.error('height')
        return height

    def endpoint_height(self, position, above_ground):
        elevation = self.ground(*map(math.floor, position))
        if elevation is None:
            raise self.error('invalid')
        return elevation + self.height_offset(above_ground)

    def squared_distance(self, observer, target):
        return ((target[0]-observer[0])**2*self.column_squared
                + (target[1]-observer[1])**2*self.row_squared)

    def status(self, observer, target, start, end):
        rise, status = end-start, 'visible'
        for entry, exit, col, row in _sight_spans(observer, target):
            elevation = self.ground(col, row)
            if elevation is None:
                status = 'indeterminate'
                continue
            lowest_t = entry if rise >= 0 else exit
            lowest_height = start + rise*lowest_t
            if elevation > lowest_height or (elevation == lowest_height and (rise == 0 or 0 < lowest_t < 1)):
                return 'blocked'
        return status


def raster_line_of_sight(input_raster, band, elevation_unit, observer_xy, target_xy,
                         observer_height_m, target_height_m):
    """Evaluate a single straight sightline against the native DEM cells it crosses."""
    with _SightDEM(input_raster, band, elevation_unit, _sight_error) as dem:
        observer, target = dem.pixel(observer_xy), dem.pixel(target_xy)
        if observer == target:
            raise _sight_error('coincident')
        start = dem.endpoint_height(observer, observer_height_m)
        end = dem.endpoint_height(target, target_height_m)
        distance = _sqrt_fraction(dem.squared_distance(observer, target), error=lambda: _sight_error('numeric'))
        status = dem.status(observer, target, start, end)
        return pd.DataFrame({'distance_m': [distance], 'status': pd.array([status], dtype='string')})


_VIEWSHED_MESSAGES = {
    key: (zh.replace('DEM 两点通视', 'DEM 单观察点可视域'), en.replace('DEM line of sight', 'DEM viewshed'))
    for key, (zh, en) in _SIGHT_MESSAGES.items() if key not in ('coincident', 'numeric')
}
_VIEWSHED_MESSAGES.update({
    'xy': ('DEM 单观察点可视域的观察点必须是两个有限投影坐标 [x,y]',
           'DEM viewshed requires two finite projected observer coordinates [x,y]'),
    'radius': ('DEM 单观察点可视域必须显式提供有限正米制半径',
               'DEM viewshed requires an explicit finite positive radius in metres'),
    'write': ('DEM 单观察点可视域栅格块写入失败', 'DEM viewshed raster block write failed'),
})


def _viewshed_error(code):
    return ValueError(_VIEWSHED_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def raster_viewshed(input_raster, band, elevation_unit, observer_xy,
                    observer_height_m, target_height_m, radius_metres):
    """Trace target centres within an exact projected circle; keep three valid states."""
    with _SightDEM(input_raster, band, elevation_unit, _viewshed_error) as dem:
        observer = dem.pixel(observer_xy)
        start = dem.endpoint_height(observer, observer_height_m)
        target_offset = dem.height_offset(target_height_m)
        radius = _sight_number(radius_metres, 'radius', _viewshed_error)
        if radius <= 0:
            raise _viewshed_error('radius')
        squared_limit = radius*radius
        output = destination = None
        path = None
        completed = False
        try:
            path = _path()
            output = gdal.GetDriverByName('GTiff').Create(str(path), dem.width, dem.height, 1, gdal.GDT_Byte,
                                                         ['TILED=YES', 'COMPRESS=DEFLATE', 'PHOTOMETRIC=MINISBLACK'])
            output.SetGeoTransform(dem.source.GetGeoTransform())
            output.SetProjection(dem.source.GetProjection())
            destination = output.GetRasterBand(1)
            destination.SetNoDataValue(255)
            destination.SetDescription('viewshed: 0=blocked,1=visible,2=indeterminate')
            for top in range(0, dem.height, 512):
                for left in range(0, dem.width, 512):
                    height, width = min(512, dem.height-top), min(512, dem.width-left)
                    values = np.full((height, width), 255, dtype=np.uint8)
                    for row in range(height):
                        for col in range(width):
                            target = (Fraction(2*(left+col)+1, 2), Fraction(2*(top+row)+1, 2))
                            if dem.squared_distance(observer, target) > squared_limit:
                                continue
                            elevation = dem.ground(left+col, top+row)
                            if elevation is None:
                                continue
                            values[row,col] = (1 if target == observer else
                                {'blocked': 0, 'visible': 1, 'indeterminate': 2}[
                                    dem.status(observer, target, start, elevation+target_offset)])
                    if destination.WriteRaster(left, top, width, height, values.tobytes(), buf_type=gdal.GDT_Byte) != gdal.CE_None:
                        raise _viewshed_error('write')
            destination = None
            output.FlushCache()
            output = None
            result = _raster(path)
            completed = True
            return result
        finally:
            destination = output = None
            if path is not None and not completed:
                path.unlink(missing_ok=True)


def _dem_source(input_raster, elevation_unit, band, minimum_size, *, diagnostic=None):
    dataset, horizontal_metres = _dem_admission(input_raster, elevation_unit, band, minimum_size, diagnostic=diagnostic)
    def invalid(code, message):
        return diagnostic(code) if diagnostic is not None else ValueError(message)
    normalized = _numeric_output(dataset, 1)
    path = Path(normalized.GetDescription())
    destination = None
    completed = False
    try:
        destination = normalized.GetRasterBand(1)
        destination.SetUnitType('metre')
        valid_count = 0
        for x, y, arrays, masks in _blocks(dataset, [band]):
            with np.errstate(over='ignore', invalid='ignore'):
                values = arrays[0] * _ELEVATION_METRES[elevation_unit]
            if np.any(masks[0] & ~np.isfinite(values)):
                raise invalid('overflow', 'DEM elevation unit conversion overflow')
            values[~masks[0]] = np.nan
            valid_count += int(np.count_nonzero(masks[0]))
            height, width = values.shape
            destination.WriteRaster(x, y, width, height, values.tobytes(), buf_type=gdal.GDT_Float64)
        destination = None
        normalized.FlushCache()
        completed = True
        return normalized, horizontal_metres, valid_count
    finally:
        destination = source_band = dataset = None
        if not completed:
            normalized = None
            path.unlink(missing_ok=True)


def _dem_open_outlets(valid):
    """Valid cells on the image edge or next to original invalid coverage."""
    height, width = valid.shape
    padded = np.pad(valid, 1, constant_values=False)
    surrounded = np.ones(valid.shape, dtype=bool)
    for dy in range(3):
        for dx in range(3):
            surrounded &= padded[dy:dy+height, dx:dx+width]
    return valid & ~surrounded


def _priority_flood(values):
    """Eight-connected minimax flood, with every invalid boundary open (epsilon=0)."""
    height, width = values.shape
    valid = np.isfinite(values)
    visited = _dem_open_outlets(valid)
    queue = [(float(values[row, col]), int(row), int(col)) for row, col in np.argwhere(visited)]
    heapq.heapify(queue)
    while queue:
        level, row, col = heapq.heappop(queue)
        for y in range(max(0, row-1), min(height, row+2)):
            for x in range(max(0, col-1), min(width, col+2)):
                if valid[y, x] and not visited[y, x]:
                    visited[y, x] = True
                    elevation = max(level, float(values[y, x]))
                    values[y, x] = elevation
                    heapq.heappush(queue, (elevation, y, x))


def raster_fill_depressions(input_raster, band, elevation_unit):
    """Raise DEM depressions to spill elevation, preserving open and invalid boundaries."""
    normalized = destination = None
    path = None
    completed = False
    try:
        normalized, _, _ = _dem_source(input_raster, elevation_unit, band, 1, diagnostic=_fill_error)
        path = Path(normalized.GetDescription())
        values = np.empty((normalized.RasterYSize, normalized.RasterXSize), dtype=np.float64)
        for x, y, arrays, _ in _blocks(normalized, [1]):
            height, width = arrays[0].shape
            values[y:y+height, x:x+width] = arrays[0]
        _priority_flood(values)
        destination = normalized.GetRasterBand(1)
        for y in range(0, normalized.RasterYSize, 512):
            for x in range(0, normalized.RasterXSize, 512):
                destination.WriteArray(values[y:y+512, x:x+512], x, y)
        destination = None
        normalized.FlushCache()
        normalized = None
        result = _raster(path)
        completed = True
        return result
    finally:
        destination = normalized = None
        if path is not None and not completed:
            path.unlink(missing_ok=True)


_D8_MESSAGES = {
    'grid': ('DEM D8 流向需要已知线性单位的二维投影、北向无旋转网格，请先显式重投影', 'DEM D8 flow direction requires a projected north-up grid with known linear units; reproject explicitly'),
    'band': ('DEM D8 流向必须显式选择有效的一基高程数据波段', 'DEM D8 flow direction requires an explicit valid one-based elevation band'),
    'complex': ('DEM D8 流向不支持复数高程或复数 alpha 波段', 'DEM D8 flow direction does not support complex elevation or alpha bands'),
    'alpha': ('DEM D8 流向不能选择 alpha 波段作为高程', 'DEM D8 flow direction cannot use an alpha band as elevation'),
    'unit': ('DEM D8 流向必须明确声明 metre、foot 或 us_survey_foot 高程单位', 'DEM D8 flow direction requires elevation_unit metre, foot or us_survey_foot'),
    'unit-conflict': ('DEM D8 流向高程波段单位未知或与声明单位冲突', 'DEM D8 flow direction band unit is unknown or conflicts with elevation_unit'),
    'scale': ('DEM D8 流向前必须显式应用高程 scale/offset', 'DEM D8 flow direction requires elevation scale/offset to be applied first'),
    'overflow': ('DEM D8 流向高程转换为米时溢出', 'DEM D8 flow direction elevation conversion to metres overflowed'),
    'distance': ('DEM D8 流向的像元中心距离必须为有限正数', 'DEM D8 flow direction requires finite positive pixel centre distances'),
    'closed': ('DEM D8 流向存在无法排出的封闭洼地，请先显式执行 DEM 填洼', 'DEM D8 flow direction has a closed depression without an outlet; explicitly fill DEM depressions first'),
}
# Row-major neighbours make all equal-gradient and equal-path choices explicit.
_D8_NEIGHBOURS = ((-1, -1, 32), (-1, 0, 64), (-1, 1, 128), (0, -1, 16),
                  (0, 1, 1), (1, -1, 8), (1, 0, 4), (1, 1, 2))


def _d8_error(code):
    return ValueError(_D8_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _d8_directions(values, xsize, ysize):
    """Strict descent followed by exact weighted shortest paths over equal DEM levels."""
    height, width = values.shape
    directions = np.full(values.shape, np.nan)
    valid = np.isfinite(values)
    outlets = _dem_open_outlets(valid)
    distances = [None] * values.size
    offsets = {code: (dy, dx) for dy, dx, code in _D8_NEIGHBOURS}
    codes = {(dy, dx): code for dy, dx, code in _D8_NEIGHBOURS}
    steps = []
    for dy, dx, code in _D8_NEIGHBOURS:
        distance = math.hypot(dx * xsize, dy * ysize)
        if not math.isfinite(distance) or distance <= 0:
            raise _d8_error('distance')
        steps.append((dy, dx, code, codes[-dy, -dx], Fraction(distance)))
    queue = []
    for row, col in np.argwhere(valid):
        row, col = int(row), int(col)
        elevation = Fraction(float(values[row, col]))
        best = 0
        for dy, dx, code, _, distance in steps:
            y, x = row + dy, col + dx
            if 0 <= y < height and 0 <= x < width and valid[y, x] and values[y, x] < values[row, col]:
                slope = (elevation - Fraction(float(values[y, x]))) / distance
                if slope > best:
                    best = slope
                    directions[row, col] = code
        if best == 0 and outlets[row, col]:
            directions[row, col] = 0
        if np.isfinite(directions[row, col]):
            distances[row * width + col] = 0
            queue.append((0, row, col))
    heapq.heapify(queue)
    while queue:
        cost, row, col = heapq.heappop(queue)
        if cost != distances[row * width + col]:
            continue
        for dy, dx, _, reverse, step in steps:
            y, x = row + dy, col + dx
            if not (0 <= y < height and 0 <= x < width and valid[y, x] and values[y, x] == values[row, col]):
                continue
            candidate = cost + step
            prior = distances[y * width + x]
            if prior is None or candidate < prior:
                distances[y * width + x] = candidate
                directions[y, x] = reverse
                heapq.heappush(queue, (candidate, y, x))
            elif candidate == prior:
                py, px = offsets[int(directions[y, x])]
                if (row, col) < (y + py, x + px):
                    directions[y, x] = reverse
    if np.any(valid & ~np.isfinite(directions)):
        raise _d8_error('closed')
    return directions


def raster_flow_direction_d8(input_raster, band, elevation_unit):
    """One downstream per DEM cell, with immutable elevations and acyclic flat routing."""
    normalized = destination = None
    path = None
    completed = False
    try:
        normalized, _, _ = _dem_source(input_raster, elevation_unit, band, 1, diagnostic=_d8_error)
        path = Path(normalized.GetDescription())
        values = np.empty((normalized.RasterYSize, normalized.RasterXSize), dtype=np.float64)
        for x, y, arrays, _ in _blocks(normalized, [1]):
            height, width = arrays[0].shape
            values[y:y+height, x:x+width] = arrays[0]
        transform = normalized.GetGeoTransform()
        directions = _d8_directions(values, transform[1], -transform[5])
        destination = normalized.GetRasterBand(1)
        destination.SetUnitType('')
        for y in range(0, normalized.RasterYSize, 512):
            for x in range(0, normalized.RasterXSize, 512):
                destination.WriteArray(directions[y:y+512, x:x+512], x, y)
        destination = None
        normalized.FlushCache()
        normalized = None
        result = _raster(path)
        completed = True
        return result
    finally:
        destination = normalized = None
        if path is not None and not completed:
            path.unlink(missing_ok=True)


_D8_ACC_MESSAGES = {
    'grid': ('D8 汇流累积需要已知线性单位的二维投影、北向无旋转网格，请先显式重投影', 'D8 flow accumulation requires a projected north-up grid with known linear units; reproject explicitly'),
    'band': ('D8 汇流累积必须显式选择有效的一基方向数据波段', 'D8 flow accumulation requires an explicit valid one-based direction band'),
    'complex': ('D8 汇流累积不支持复数方向或复数 alpha 波段', 'D8 flow accumulation does not support complex direction or alpha bands'),
    'alpha': ('D8 汇流累积不能选择 alpha 波段作为方向', 'D8 flow accumulation cannot use an alpha band as direction'),
    'scale': ('D8 汇流累积前必须显式应用方向 scale/offset', 'D8 flow accumulation requires direction scale/offset to be applied first'),
    'code': ('D8 汇流累积遇到非法方向码，只允许0、1、2、4、8、16、32、64、128', 'D8 flow accumulation found an invalid direction code; only 0, 1, 2, 4, 8, 16, 32, 64, 128 are allowed'),
    'terminal': ('D8 汇流累积的有效终点0必须位于图边或有效／无效覆盖边界', 'D8 flow accumulation terminal 0 must be on the image edge or valid/invalid coverage boundary'),
    'target': ('D8 汇流累积方向必须指向图内有效像元，不能指向图外或无效覆盖', 'D8 flow accumulation direction must target an in-bounds valid pixel, not outside the grid or invalid coverage'),
    'cycle': ('D8 汇流累积检测到方向循环，整个工作流失败', 'D8 flow accumulation detected a direction cycle; the entire workflow fails'),
    'overflow': ('D8 汇流累积计数超过Float64精确整数上限2^53−1', 'D8 flow accumulation count exceeds the Float64 exact integer limit 2^53-1'),
    'measure': ('D8 汇流累积的measure只允许精确小写count或area',
                'D8 flow accumulation measure must be exactly count or area'),
    'area': ('D8 汇流面积的像元投影面积必须能表示为有限严格正数平方米',
             'D8 contributing area cell area must be representable as finite strictly positive square metres'),
    'area_overflow': ('D8 汇流面积累计结果超过Float64有限范围',
                      'D8 contributing area accumulated result exceeds the finite Float64 range'),
}
_D8_MAX_EXACT_COUNT = 2**53 - 1


def _d8_acc_error(code):
    return ValueError(_D8_ACC_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _d8_graph(directions, error):
    """Validate the supplied D8 graph once for all D8 consumers."""
    height, width = directions.shape
    valid = np.isfinite(directions)
    if np.any(valid & ~np.isin(directions, [0, *[step[2] for step in _D8_NEIGHBOURS]])):
        raise error('code')
    if np.any((directions == 0) & ~_dem_open_outlets(valid)):
        raise error('terminal')
    downstream = np.full(directions.size, -1, dtype=np.int64)
    indegree = np.zeros(directions.size, dtype=np.uint8)
    for dy, dx, code in _D8_NEIGHBOURS:
        rows, cols = np.nonzero(directions == code)
        target_rows, target_cols = rows + dy, cols + dx
        if np.any((target_rows < 0) | (target_rows >= height) | (target_cols < 0) | (target_cols >= width)):
            raise error('target')
        if np.any(~valid[target_rows, target_cols]):
            raise error('target')
        targets = target_rows * width + target_cols
        downstream[rows * width + cols] = targets
        # Within one direction targets are unique; a cell has at most eight donors.
        indegree[targets] += 1
    return valid, downstream, indegree


def _d8_topological_order(valid, downstream, indegree, error):
    order = np.empty(np.count_nonzero(valid), dtype=np.int64)
    ready = deque(map(int, np.flatnonzero(valid.ravel() & (indegree == 0))))
    visited = 0
    while ready:
        cell = ready.popleft()
        order[visited] = cell
        visited += 1
        target = int(downstream[cell])
        if target >= 0:
            indegree[target] -= 1
            if indegree[target] == 0:
                ready.append(target)
    if visited != order.size:
        raise error('cycle')
    return order


def _d8_accumulation(directions):
    """Topological upstream counts (excluding self); never repair the supplied graph."""
    valid, downstream, indegree = _d8_graph(directions, _d8_acc_error)
    order = _d8_topological_order(valid, downstream, indegree, _d8_acc_error)
    counts = np.zeros(directions.size, dtype=np.int64)
    for cell in order:
        target = int(downstream[cell])
        if target < 0:
            continue
        # Python integer addition avoids overflow before the exact-output guard.
        count = int(counts[target]) + int(counts[cell]) + 1
        if count > _D8_MAX_EXACT_COUNT:
            raise _d8_acc_error('overflow')
        counts[target] = count
    result = counts.reshape(directions.shape).astype(np.float64)
    result[~valid] = np.nan
    return result


def _d8_direction_source(input_raster, band, error):
    """Read effective directions without rounding NoData classification."""
    source = _open(input_raster)
    try:
        horizontal_metres = _projected_north_up_grid(source)
    except ValueError as cause:
        raise error('grid') from cause
    try:
        selected = _band(source, band)
    except ValueError as cause:
        key = 'complex' if type(band) is int and 1 <= band <= source.RasterCount else 'band'
        raise error(key) from cause
    if selected.GetColorInterpretation() == gdal.GCI_AlphaBand:
        raise error('alpha')
    if selected.GetScale() not in (None, 1) or selected.GetOffset() not in (None, 0):
        raise error('scale')
    for index in range(1, source.RasterCount + 1):
        candidate = source.GetRasterBand(index)
        if candidate.GetColorInterpretation() == gdal.GCI_AlphaBand and gdal.DataTypeIsComplex(candidate.DataType):
            raise error('complex')
    candidate = selected = None
    directions = np.full((source.RasterYSize, source.RasterXSize), np.nan)
    # Classify NoData in the source dtype; Float64 can merge adjacent Int64 codes.
    for x, y, arrays, masks in _blocks(source, [band], native=True):
        values = np.where(masks[0], arrays[0], np.nan)
        height, width = values.shape
        directions[y:y+height, x:x+width] = values
    return source, directions, horizontal_metres


def _d8_output(source, values, unit=''):
    output = destination = None
    path = None
    completed = False
    try:
        output = _numeric_output(source, 1)
        path = Path(output.GetDescription())
        destination = output.GetRasterBand(1)
        destination.SetUnitType(unit)
        for y in range(0, source.RasterYSize, 512):
            for x in range(0, source.RasterXSize, 512):
                destination.WriteArray(values[y:y+512, x:x+512], x, y)
        destination = None
        output.FlushCache()
        output = None
        result = _raster(path)
        completed = True
        return result
    finally:
        destination = output = source = None
        if path is not None and not completed:
            path.unlink(missing_ok=True)


def _d8_cell_area(width, height, horizontal_metres):
    # Exact finite-input product avoids intermediate overflow and underflow.
    try:
        area = float(Fraction(width) * Fraction(height) * Fraction(horizontal_metres) ** 2)
    except OverflowError as cause:
        raise _d8_acc_error('area') from cause
    if not math.isfinite(area) or area <= 0:
        raise _d8_acc_error('area')
    return area


def raster_flow_accumulation_d8(input_raster, band, measure='count'):
    """Count upstream cells excluding self, or measure projected area including self."""
    if type(measure) is not str or measure not in ('count', 'area'):
        raise _d8_acc_error('measure')
    source, directions, factor = _d8_direction_source(input_raster, band, _d8_acc_error)
    area = None
    if measure == 'area':
        transform = source.GetGeoTransform()
        area = _d8_cell_area(transform[1], -transform[5], factor)
    values = _d8_accumulation(directions)
    if measure == 'area':
        with np.errstate(over='ignore', invalid='ignore'):
            values += 1
            values *= area
        if np.any(np.isfinite(directions) & ~np.isfinite(values)):
            raise _d8_acc_error('area_overflow')
    return _d8_output(source, values, 'm2' if measure == 'area' else '')


_D8_WEIGHTED_MESSAGES = {
    key: (zh.replace('D8 汇流累积', 'D8 加权汇流累积'),
          en.replace('D8 flow accumulation', 'D8 weighted flow accumulation'))
    for key, (zh, en) in _D8_ACC_MESSAGES.items() if key != 'overflow'
}
_D8_WEIGHTED_MESSAGES.update({
    'alignment': ('D8 加权汇流累积的方向与权重必须具有相同尺寸、完整仿射网格和等价CRS，请先显式对齐',
                  'D8 weighted flow accumulation requires direction and weights to have the same dimensions, full affine grid and equivalent CRS; align explicitly'),
    'weights_band': ('D8 加权汇流累积必须显式选择有效的一基权重数据波段',
                     'D8 weighted flow accumulation requires an explicit valid one-based weights data band'),
    'weights_complex': ('D8 加权汇流累积不支持复数权重或复数alpha波段',
                        'D8 weighted flow accumulation does not support complex weights or complex alpha bands'),
    'weights_alpha': ('D8 加权汇流累积不能选择alpha波段作为权重',
                      'D8 weighted flow accumulation cannot use an alpha band as weights'),
    'weights_scale': ('D8 加权汇流累积前必须显式应用权重scale/offset',
                      'D8 weighted flow accumulation requires weights scale/offset to be applied first'),
    'coverage': ('D8 加权汇流累积的方向与权重必须具有完全相同的有效覆盖；缺失或非有限权重不能补零或取交集',
                 'D8 weighted flow accumulation requires exactly the same effective coverage for direction and weights; missing or nonfinite weights cannot be filled with zero or intersected'),
    'negative': ('D8 加权汇流累积的有效权重必须为有限非负数，允许合法零',
                 'D8 weighted flow accumulation requires finite nonnegative effective weights; valid zero is allowed'),
    'overflow': ('D8 加权汇流累积的权重和超过Float64有限范围',
                 'D8 weighted flow accumulation sum exceeds the finite Float64 range'),
})


def _d8_weighted_error(code):
    return ValueError(_D8_WEIGHTED_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _d8_weighted_accumulation(directions, weights):
    """Propagate upstream sums plus donor weights, never subtract a cell's own weight."""
    valid, downstream, indegree = _d8_graph(directions, _d8_weighted_error)
    order = _d8_topological_order(valid, downstream, indegree, _d8_weighted_error)
    if not np.array_equal(np.isfinite(weights), valid):
        raise _d8_weighted_error('coverage')
    if np.any(weights[valid] < 0):
        raise _d8_weighted_error('negative')
    sums = np.zeros(directions.size, dtype=np.float64)
    original = weights.ravel()
    for cell in order:
        target = int(downstream[cell])
        if target < 0:
            continue
        value = float(sums[target]) + float(sums[cell]) + float(original[cell])
        if not math.isfinite(value):
            raise _d8_weighted_error('overflow')
        sums[target] = value
    result = sums.reshape(directions.shape)
    result[~valid] = np.nan
    return result


def raster_flow_accumulation_weighted_d8(input_raster, weights_raster, band, weights_band):
    """Sum aligned nonnegative upstream weights, excluding self on the supplied D8 graph."""
    source, directions, _ = _d8_direction_source(input_raster, band, _d8_weighted_error)
    weights_source = _open(weights_raster)
    try:
        _require_same_grid(source, weights_source)
    except ValueError as cause:
        raise _d8_weighted_error('alignment') from cause
    weights_source = None
    # This shared reader classifies native NoData/mask/alpha before Float64
    # conversion; direction-code interpretation belongs to the graph validator.
    weights_source, weights, _ = _d8_direction_source(weights_raster, weights_band,
        lambda code: _d8_weighted_error('alignment' if code == 'grid' else 'weights_' + code))
    unit = weights_source.GetRasterBand(weights_band).GetUnitType()
    weights_source = None
    return _d8_output(source, _d8_weighted_accumulation(directions, weights), unit)


_D8_LENGTH_MESSAGES = {
    key: (zh.replace('D8 汇流累积', 'D8 下游流长'),
          en.replace('D8 flow accumulation', 'D8 downstream flow length'))
    for key, (zh, en) in _D8_ACC_MESSAGES.items() if key != 'overflow'
}
_D8_LENGTH_MESSAGES.update({
    'step': ('D8 下游流长的米制横纵及对角步长必须有限且严格为正',
             'D8 downstream flow length requires finite, strictly positive horizontal, vertical and diagonal steps in metres'),
    'overflow': ('D8 下游流长累计距离超过Float64有限范围',
                 'D8 downstream flow length accumulated distance exceeds the finite Float64 range'),
})


def _d8_length_error(code):
    return ValueError(_D8_LENGTH_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _d8_length_steps(width_metres, height_metres, error):
    diagonal = math.hypot(width_metres, height_metres)
    if any(not math.isfinite(step) or step <= 0 for step in (width_metres, height_metres, diagonal)):
        raise error('step')
    return {code: diagonal if dy and dx else height_metres if dy else width_metres
            for dy, dx, code in _D8_NEIGHBOURS}


def _d8_flow_length(directions, width_metres, height_metres):
    """Sum centre-to-centre edge lengths backwards on the unmodified D8 graph."""
    steps = _d8_length_steps(width_metres, height_metres, _d8_length_error)
    valid, downstream, indegree = _d8_graph(directions, _d8_length_error)
    order = _d8_topological_order(valid, downstream, indegree, _d8_length_error)
    values = np.full(directions.size, np.nan)
    codes = directions.ravel()
    for cell in order[::-1]:
        target = int(downstream[cell])
        if target < 0:
            values[cell] = 0.0
            continue
        length = steps[codes[cell]] + float(values[target])
        if not math.isfinite(length):
            raise _d8_length_error('overflow')
        values[cell] = length
    return values.reshape(directions.shape)


_D8_UPSTREAM_LENGTH_MESSAGES = {
    key: (zh.replace('D8 下游流长', 'D8 上游最长流长'),
          en.replace('D8 downstream flow length', 'D8 longest upstream flow length'))
    for key, (zh, en) in _D8_LENGTH_MESSAGES.items()
}


def _d8_upstream_length_error(code):
    return ValueError(_D8_UPSTREAM_LENGTH_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _d8_upstream_flow_length(directions, width_metres, height_metres):
    """Propagate the maximum upstream distance forward; never sum tributaries."""
    steps = _d8_length_steps(width_metres, height_metres, _d8_upstream_length_error)
    valid, downstream, indegree = _d8_graph(directions, _d8_upstream_length_error)
    order = _d8_topological_order(valid, downstream, indegree, _d8_upstream_length_error)
    values = np.zeros(directions.size, dtype=np.float64)
    codes = directions.ravel()
    for cell in order:
        target = int(downstream[cell])
        if target < 0:
            continue
        length = float(values[cell]) + steps[codes[cell]]
        if not math.isfinite(length):
            raise _d8_upstream_length_error('overflow')
        if length > values[target]:
            values[target] = length
    result = values.reshape(directions.shape)
    result[~valid] = np.nan
    return result


def raster_flow_length_d8(input_raster, band, method):
    """Measure downstream or longest-upstream centre distances in metres."""
    if type(method) is not str or method not in ('downstream', 'upstream_longest'):
        messages = ('D8流长必须显式选择方法downstream或upstream_longest',
                    'D8 flow length requires an explicit method: downstream or upstream_longest')
        raise ValueError(messages[1 if _DISK_LANGUAGE.get() == 'en' else 0])
    error = _d8_length_error if method == 'downstream' else _d8_upstream_length_error
    calculate = _d8_flow_length if method == 'downstream' else _d8_upstream_flow_length
    source, directions, factor = _d8_direction_source(input_raster, band, error)
    transform = source.GetGeoTransform()
    values = calculate(directions, transform[1] * factor, -transform[5] * factor)
    return _d8_output(source, values, 'metre')


_D8_BASIN_MESSAGES = {
    key: (zh.replace('D8 汇流累积', 'D8 汇水分区'),
          en.replace('D8 flow accumulation', 'D8 drainage basins'))
    for key, (zh, en) in _D8_ACC_MESSAGES.items() if key != 'overflow'
}
_D8_BASIN_MESSAGES['overflow'] = (
    'D8 汇水分区的出口数超过Float64精确整数上限2^53−1',
    'D8 drainage basins outlet count exceeds the Float64 exact integer limit 2^53-1',
)


def _d8_basin_error(code):
    return ValueError(_D8_BASIN_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _d8_drainage_basins(directions):
    """Propagate row-major open-outlet IDs on the complete validated D8 graph."""
    valid, downstream, indegree = _d8_graph(directions, _d8_basin_error)
    order = _d8_topological_order(valid, downstream, indegree, _d8_basin_error)
    outlets = np.flatnonzero(directions.ravel() == 0)
    if outlets.size > _D8_MAX_EXACT_COUNT:
        raise _d8_basin_error('overflow')
    labels = np.zeros(directions.size, dtype=np.int64)
    labels[outlets] = np.arange(1, outlets.size + 1, dtype=np.int64)
    for cell in order[::-1]:
        target = int(downstream[cell])
        if target >= 0:
            labels[cell] = labels[target]
    result = labels.reshape(directions.shape).astype(np.float64)
    result[~valid] = np.nan
    return result


def raster_drainage_basins_d8(input_raster, band):
    """Label each effective cell by its final open outlet, without changing flow."""
    source, directions, _ = _d8_direction_source(input_raster, band, _d8_basin_error)
    return _d8_output(source, _d8_drainage_basins(directions))


_SNAP_MESSAGES = {
    'grid': ('汇水点吸附需要已知线性单位的二维投影、北向无旋转网格，请先显式重投影', 'Pour-point snapping requires a projected north-up grid with known linear units; reproject explicitly'),
    'band': ('汇水点吸附必须显式选择有效的一基未加权累积计数波段', 'Pour-point snapping requires an explicit valid one-based unweighted accumulation-count band'),
    'complex': ('汇水点吸附不支持复数计数或复数alpha波段', 'Pour-point snapping does not support complex counts or complex alpha bands'),
    'alpha': ('汇水点吸附不能选择alpha波段作为计数', 'Pour-point snapping cannot use an alpha band as counts'),
    'scale': ('汇水点吸附前必须显式应用计数scale/offset', 'Pour-point snapping requires count scale/offset to be applied first'),
    'points_band': ('汇水点吸附必须显式选择有效的一基汇水点标记数据波段', 'Pour-point snapping requires an explicit valid one-based points data band'),
    'points_complex': ('汇水点吸附不支持复数标记或复数alpha波段', 'Pour-point snapping does not support complex points or complex alpha bands'),
    'points_alpha': ('汇水点吸附不能选择alpha波段作为标记', 'Pour-point snapping cannot use an alpha band as points'),
    'points_scale': ('汇水点吸附前必须显式应用标记scale/offset', 'Pour-point snapping requires points scale/offset to be applied first'),
    'alignment': ('汇水点吸附的计数与标记必须具有相同尺寸、完整仿射网格和等价CRS，请先显式对齐', 'Pour-point snapping requires counts and points to have the same dimensions, full affine grid and equivalent CRS; align explicitly'),
    'coverage': ('汇水点吸附的计数与标记必须具有完全相同的有效覆盖，不能取交集或补零', 'Pour-point snapping requires exactly the same effective coverage for counts and points; do not intersect coverage or fill missing values with zero'),
    'counts_values': ('汇水点吸附的有效未加权累积计数必须为0至2^53−1内的精确整数', 'Pour-point snapping requires exact integer unweighted accumulation counts from 0 to 2^53-1'),
    'points_values': ('汇水点吸附的有效标记必须为0至2^53−1内的精确整数，0为未选择', 'Pour-point snapping requires exact integer point values from 0 to 2^53-1; zero means unselected'),
    'empty': ('汇水点吸附必须至少选择一个有效汇水点', 'Pour-point snapping requires at least one effective selected point'),
    'duplicate': ('汇水点吸附的正编号必须全图唯一，一个编号只能标记一个像元', 'Pour-point snapping requires globally unique positive IDs, one cell per ID'),
    'radius': ('汇水点吸附必须显式指定有限非负的米制半径，不接受布尔值或字符串', 'Pour-point snapping requires an explicit finite nonnegative radius in metres, not a boolean or string'),
    'distance': ('汇水点吸附的米制像元中心步长必须为有限正数', 'Pour-point snapping requires finite positive pixel-centre steps in metres'),
    'collision': ('汇水点吸附碰撞：多个编号选到同一目标像元，整个工作流失败', 'Pour-point snapping collision: multiple IDs selected the same target cell; the entire workflow fails'),
}


def _snap_error(code):
    return ValueError(_SNAP_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _snap_pour_points(counts, points, xstep, ystep, radius_metres):
    """Independent circular searches; original counts and points never change."""
    try:
        radius = float(radius_metres) if type(radius_metres) in (int, float) else math.nan
    except OverflowError:
        radius = math.nan
    if not math.isfinite(radius) or radius < 0:
        raise _snap_error('radius')
    if any(not math.isfinite(step) or step <= 0 for step in (xstep, ystep)):
        raise _snap_error('distance')
    valid = np.isfinite(counts)
    if not np.array_equal(np.isfinite(points), valid):
        raise _snap_error('coverage')
    for values, code in ((counts[valid], 'counts_values'), (points[valid], 'points_values')):
        if np.any((values < 0) | (values > _D8_MAX_EXACT_COUNT) | (values != np.floor(values))):
            raise _snap_error(code)
    selected = np.argwhere(valid & (points > 0))
    if not selected.size:
        raise _snap_error('empty')
    ids = points[valid & (points > 0)]
    if np.unique(ids).size != ids.size:
        raise _snap_error('duplicate')
    height, width = counts.shape
    # Clamp before converting the radius/step ratio to an integer, even for huge radii.
    row_radius = math.ceil(min(height - 1, radius / ystep))
    col_radius = math.ceil(min(width - 1, radius / xstep))
    result = np.where(valid, 0., np.nan)
    for row, col in selected:
        left, right = max(0, col - col_radius), min(width, col + col_radius + 1)
        columns = np.arange(left, right)
        best = None
        for y in range(max(0, row - row_radius), min(height, row + row_radius + 1)):
            with np.errstate(over='ignore'):
                distances = np.hypot((columns - col) * xstep, (y - row) * ystep)
            candidates = valid[y, left:right] & (distances <= radius)
            if not candidates.any():
                continue
            maximum = np.max(counts[y, left:right][candidates])
            candidates &= counts[y, left:right] == maximum
            nearest = np.min(distances[candidates])
            x = int(columns[np.flatnonzero(candidates & (distances == nearest))[0]])
            rank = (-float(maximum), float(nearest), y, x)
            if best is None or rank < best:
                best = rank
        # The original valid point is always in its own inclusive search circle.
        _, _, y, x = best
        if result[y, x] != 0:
            raise _snap_error('collision')
        result[y, x] = points[row, col]
    return result


def raster_snap_pour_points(input_raster, points_raster, band, points_band, radius_metres):
    """Snap unique user IDs to the largest count, allowing cross-divide searches."""
    source, counts, metres = _d8_direction_source(input_raster, band, _snap_error)
    points_source = _open(points_raster)
    try:
        _require_same_grid(source, points_source)
    except ValueError as cause:
        raise _snap_error('alignment') from cause
    points_source = None
    _, points, _ = _d8_direction_source(points_raster, points_band,
        lambda code: _snap_error('alignment' if code == 'grid' else 'points_' + code))
    grid = source.GetGeoTransform()
    values = _snap_pour_points(counts, points, grid[1] * metres, -grid[5] * metres, radius_metres)
    return _d8_output(source, values)


_D8_WATERSHED_MESSAGES = {
    key: (zh.replace('D8 汇流累积', 'D8 指定汇水点汇水区'),
          en.replace('D8 flow accumulation', 'D8 specified-outlet watershed'))
    for key, (zh, en) in _D8_ACC_MESSAGES.items() if key != 'overflow'
}
_D8_WATERSHED_MESSAGES.update({
    'alignment': ('D8 指定汇水点汇水区的流向与标记必须具有相同尺寸、完整仿射网格和等价CRS，请先显式对齐',
                  'D8 specified-outlet watershed requires direction and points to have the same dimensions, full affine grid and equivalent CRS; align explicitly'),
    'points_band': ('D8 指定汇水点汇水区必须显式选择有效的一基汇水点标记数据波段',
                    'D8 specified-outlet watershed requires an explicit valid one-based points data band'),
    'points_complex': ('D8 指定汇水点汇水区不支持复数标记或复数alpha波段',
                       'D8 specified-outlet watershed does not support complex points or complex alpha bands'),
    'points_alpha': ('D8 指定汇水点汇水区不能选择alpha波段作为汇水点标记',
                     'D8 specified-outlet watershed cannot use an alpha band as points'),
    'points_scale': ('D8 指定汇水点汇水区前必须显式应用标记scale/offset',
                     'D8 specified-outlet watershed requires points scale/offset to be applied first'),
    'coverage': ('D8 指定汇水点汇水区的流向与标记必须具有完全相同的有效覆盖，不能取交集或将缺失标记补零',
                 'D8 specified-outlet watershed requires exactly the same effective coverage for direction and points; do not intersect coverage or fill missing points with zero'),
    'points_values': ('D8 指定汇水点汇水区的有效标记必须为0至2^53−1内的精确整数，0为未选择',
                      'D8 specified-outlet watershed requires exact integer point values from 0 to 2^53-1; zero means unselected'),
    'empty': ('D8 指定汇水点汇水区必须至少选择一个有效汇水点',
              'D8 specified-outlet watershed requires at least one effective selected point'),
    'duplicate': ('D8 指定汇水点汇水区的正编号必须全图唯一，一个编号只能标记一个像元',
                  'D8 specified-outlet watershed requires globally unique positive IDs, one cell per ID'),
    'nested': ('D8 指定汇水点汇水区不支持嵌套汇水点：一个指定点不能沿下游到达另一指定点',
               'D8 specified-outlet watershed does not support nested points: a selected point cannot reach another selected point downstream'),
})


def _d8_watershed_error(code):
    return ValueError(_D8_WATERSHED_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


_D8_SUBCATCHMENTS_MESSAGES = {
    key: (zh.replace('D8 指定汇水点汇水区', 'D8 增量子汇水区'),
          en.replace('D8 specified-outlet watershed', 'D8 incremental subcatchments'))
    for key, (zh, en) in _D8_WATERSHED_MESSAGES.items() if key != 'nested'
}


def _d8_subcatchments_error(code):
    return ValueError(_D8_SUBCATCHMENTS_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _d8_point_catchments(directions, points, error, *, reject_nested):
    """One validated graph and reverse pass; a selected point owns its upstream cells."""
    valid, downstream, indegree = _d8_graph(directions, error)
    order = _d8_topological_order(valid, downstream, indegree, error)
    if not np.array_equal(np.isfinite(points), valid):
        raise error('coverage')
    values = points[valid]
    if np.any((values < 0) | (values > _D8_MAX_EXACT_COUNT) | (values != np.floor(values))):
        raise error('points_values')
    selected = values[values > 0]
    if selected.size == 0:
        raise error('empty')
    if np.unique(selected).size != selected.size:
        raise error('duplicate')
    labels = np.zeros(directions.size, dtype=np.int64)
    original = points.ravel()
    for cell in order[::-1]:
        target = int(downstream[cell])
        inherited = labels[target] if target >= 0 else 0
        if original[cell] > 0:
            if reject_nested and inherited != 0:
                raise error('nested')
            labels[cell] = int(original[cell])
        else:
            labels[cell] = inherited
    result = labels.reshape(directions.shape).astype(np.float64)
    result[~valid] = np.nan
    return result


def _d8_watershed(directions, points):
    """Full upstream watersheds of unique, nonnested user IDs."""
    return _d8_point_catchments(directions, points, _d8_watershed_error, reject_nested=True)


def _d8_subcatchments(directions, points):
    """Disjoint incremental regions owned by the first selected point downstream."""
    return _d8_point_catchments(directions, points, _d8_subcatchments_error, reject_nested=False)


def _d8_point_inputs(input_raster, points_raster, band, points_band, error):
    source, directions, _ = _d8_direction_source(input_raster, band, error)
    points_source = _open(points_raster)
    try:
        _require_same_grid(source, points_source)
    except ValueError as cause:
        raise error('alignment') from cause
    points_source = None
    _, points, _ = _d8_direction_source(points_raster, points_band,
        lambda code: error('alignment' if code == 'grid' else 'points_' + code))
    return source, directions, points


def raster_watershed_d8(input_raster, points_raster, band, points_band):
    """Preserve full upstream regions; nested user points are rejected."""
    source, directions, points = _d8_point_inputs(input_raster, points_raster, band, points_band, _d8_watershed_error)
    return _d8_output(source, _d8_watershed(directions, points))


def raster_subcatchments_d8(input_raster, points_raster, band, points_band):
    """Preserve user IDs at the first selected point downstream, allowing nested points."""
    source, directions, points = _d8_point_inputs(input_raster, points_raster, band, points_band, _d8_subcatchments_error)
    return _d8_output(source, _d8_subcatchments(directions, points))


_D8_STREAM_ORDER_MESSAGES = {
    key: (zh.replace('D8 汇流累积', 'D8 Strahler 河流分级'),
          en.replace('D8 flow accumulation', 'D8 Strahler stream order'))
    for key, (zh, en) in _D8_ACC_MESSAGES.items() if key != 'overflow'
}
_D8_STREAM_ORDER_MESSAGES.update({
    'alignment': ('D8 Strahler 河流分级的流向与河网掩膜必须具有相同尺寸、完整仿射网格和等价CRS，请先显式对齐',
                  'D8 Strahler stream order requires direction and stream mask to have the same dimensions, full affine grid and equivalent CRS; align explicitly'),
    'mask_band': ('D8 Strahler 河流分级必须显式选择有效的一基0／1河网掩膜数据波段',
                  'D8 Strahler stream order requires an explicit valid one-based 0/1 stream-mask data band'),
    'mask_complex': ('D8 Strahler 河流分级不支持复数河网掩膜或复数alpha波段',
                     'D8 Strahler stream order does not support complex stream masks or complex alpha bands'),
    'mask_alpha': ('D8 Strahler 河流分级不能选择alpha波段作为河网掩膜',
                   'D8 Strahler stream order cannot use an alpha band as the stream mask'),
    'mask_scale': ('D8 Strahler 河流分级前必须显式应用河网掩膜scale/offset',
                   'D8 Strahler stream order requires stream-mask scale/offset to be applied first'),
    'coverage': ('D8 Strahler 河流分级的流向与河网掩膜必须具有完全相同的有效覆盖，不能取交集或将缺失视为零',
                 'D8 Strahler stream order requires exactly the same effective coverage for direction and stream mask; do not intersect coverage or treat missing cells as zero'),
    'mask_values': ('D8 Strahler 河流分级的有效河网掩膜只允许0和1',
                    'D8 Strahler stream order permits only 0 and 1 in the effective stream mask'),
    'break': ('D8 Strahler 河流分级发现河网断流：非终点河网像元必须继续指向河网，不自动补线',
              'D8 Strahler stream order found a stream break: every nonterminal stream cell must continue to a stream cell; gaps are not filled automatically'),
    'overflow': ('D8 Strahler 河流分级的级别超过Float64精确整数上限2^53−1',
                 'D8 Strahler stream order exceeds the Float64 exact integer limit 2^53-1'),
})


_D8_STREAM_ORDER_MESSAGES['method'] = (
    'D8 河流分级的method必须是strahler或shreve',
    'D8 stream order method must be strahler or shreve',
)
_D8_SHREVE_MESSAGES = {
    key: (zh.replace('D8 Strahler 河流分级', 'D8 Shreve 河流量级').replace('级别', '量级'),
          en.replace('D8 Strahler stream order', 'D8 Shreve stream magnitude'))
    for key, (zh, en) in _D8_STREAM_ORDER_MESSAGES.items()
}


def _d8_stream_order_error(code, method='strahler'):
    messages = _D8_SHREVE_MESSAGES if method == 'shreve' else _D8_STREAM_ORDER_MESSAGES
    return ValueError(messages[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _d8_stream_graph(directions, streams, error):
    """Validate the whole D8 graph and closed downstream membership of a stream mask."""
    valid, downstream, indegree = _d8_graph(directions, error)
    order = _d8_topological_order(valid, downstream, indegree, error)
    river = streams.ravel()
    edges = np.flatnonzero(river & (downstream >= 0))
    if np.any(~river[downstream[edges]]):
        raise error('break')
    return valid, downstream, order


def _d8_stream_order(directions, streams, method='strahler'):
    """One validated stream traversal for Strahler rank or Shreve source magnitude."""
    error = lambda code: _d8_stream_order_error(code, method)
    valid, downstream, order = _d8_stream_graph(directions, streams, error)
    river = streams.ravel()
    levels = np.zeros(directions.size, dtype=np.int64)
    highest_count = np.zeros(directions.size, dtype=np.uint8) if method == 'strahler' else None
    for cell in order:
        if not river[cell]:
            continue
        level = max(1, int(levels[cell]) + int(highest_count[cell] >= 2)) if method == 'strahler' else max(1, int(levels[cell]))
        if level > _D8_MAX_EXACT_COUNT:
            raise error('overflow')
        levels[cell] = level
        target = int(downstream[cell])
        if target >= 0:
            if method == 'shreve':
                total = int(levels[target]) + level
                if total > _D8_MAX_EXACT_COUNT:
                    raise error('overflow')
                levels[target] = total
            elif level > levels[target]:
                levels[target] = level
                highest_count[target] = 1
            elif level == levels[target]:
                highest_count[target] += 1
    result = levels.reshape(directions.shape).astype(np.float64)
    result[~valid] = np.nan
    return result


def _d8_stream_sources(input_raster, stream_raster, band, stream_band, error):
    """One admission path for all explicit D8 stream-mask consumers."""
    source, directions, _ = _d8_direction_source(input_raster, band, error)
    stream_source = _open(stream_raster)
    try:
        _require_same_grid(source, stream_source)
    except ValueError as cause:
        raise error('alignment') from cause
    stream_source = None
    # Reuse the same native-NoData, mask, alpha, scale and band reader. This
    # reader does not interpret direction codes; the full graph is checked below.
    _, values, _ = _d8_direction_source(stream_raster, stream_band,
        lambda code: error('alignment' if code == 'grid' else 'mask_' + code))
    valid = np.isfinite(directions)
    if not np.array_equal(np.isfinite(values), valid):
        raise error('coverage')
    if np.any(valid & ~np.isin(values, (0, 1))):
        raise error('mask_values')
    return source, directions, values == 1


def raster_stream_order_d8(input_raster, stream_raster, band, stream_band, method='strahler'):
    """Compute Strahler rank (default) or Shreve magnitude without filling gaps."""
    if not isinstance(method, str) or method not in ('strahler', 'shreve'):
        raise _d8_stream_order_error('method')
    source, directions, streams = _d8_stream_sources(
        input_raster, stream_raster, band, stream_band, lambda code: _d8_stream_order_error(code, method))
    return _d8_output(source, _d8_stream_order(directions, streams, method))


_D8_STREAM_DISTANCE_MESSAGES = {
    key: (zh.replace('D8 Strahler 河流分级', 'D8 到河网沿流距离'),
          en.replace('D8 Strahler stream order', 'D8 distance to stream'))
    for key, (zh, en) in _D8_STREAM_ORDER_MESSAGES.items()
    if key not in ('overflow', 'method', 'measure', 'area', 'area_overflow')
}
_D8_STREAM_DISTANCE_MESSAGES.update({
    'step': ('D8 到河网沿流距离的米制横纵及对角步长必须有限且严格为正',
             'D8 distance to stream requires finite, strictly positive horizontal, vertical and diagonal steps in metres'),
    'overflow': ('D8 到河网沿流距离累计距离超过Float64有限范围',
                 'D8 distance to stream accumulated distance exceeds the finite Float64 range'),
})


def _d8_stream_distance_error(code):
    return ValueError(_D8_STREAM_DISTANCE_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _d8_distance_to_stream(directions, streams, width_metres, height_metres):
    """Stop at the first stream cell; a valid route with no hit stays NoData."""
    error = _d8_stream_distance_error
    _, downstream, order = _d8_stream_graph(directions, streams, error)
    steps = _d8_length_steps(width_metres, height_metres, error)
    river, codes = streams.ravel(), directions.ravel()
    values = np.full(directions.size, np.nan)
    corrections = np.zeros(directions.size)
    for cell in order[::-1]:
        if river[cell]:
            values[cell] = 0.0
            continue
        target = int(downstream[cell])
        if target < 0 or not math.isfinite(values[target]):
            continue
        # Copy the downstream path's compensation before extending this branch.
        corrected = steps[codes[cell]] - float(corrections[target])
        distance = float(values[target]) + corrected
        if not math.isfinite(distance):
            raise error('overflow')
        corrections[cell] = (distance - float(values[target])) - corrected
        values[cell] = distance
    return values.reshape(directions.shape)


def raster_distance_to_stream_d8(input_raster, stream_raster, band, stream_band):
    """Measure metres along supplied D8 routes to the first explicit stream cell."""
    source, directions, streams = _d8_stream_sources(
        input_raster, stream_raster, band, stream_band, _d8_stream_distance_error)
    factor = _projected_north_up_grid(source)
    transform = source.GetGeoTransform()
    values = _d8_distance_to_stream(directions, streams, transform[1] * factor, -transform[5] * factor)
    return _d8_output(source, values, 'metre')


_D8_HAND_MESSAGES = {
    key: (zh.replace('D8 Strahler 河流分级', 'D8 到河网相对高程（HAND）'),
          en.replace('D8 Strahler stream order', 'D8 height above nearest drainage (HAND)'))
    for key, (zh, en) in _D8_STREAM_ORDER_MESSAGES.items()
    if key not in ('overflow', 'method', 'measure', 'area', 'area_overflow')
}
_D8_HAND_MESSAGES.update({
    'dem_' + key: (zh.replace('DEM 填洼', 'D8 HAND'),
                   en.replace('DEM depression filling', 'D8 HAND'))
    for key, (zh, en) in _FILL_MESSAGES.items()
})
_D8_HAND_MESSAGES.update({
    'dem_alignment': ('D8 HAND的DEM必须与方向具有相同尺寸、完整网格和CRS',
                      'D8 HAND elevation must match direction dimensions, full grid and CRS'),
    'dem_coverage': ('D8 HAND的DEM有效覆盖必须与方向完全相同',
                     'D8 HAND elevation valid coverage must exactly match directions'),
    'uphill': ('D8 HAND的全图下游高程不得升高，允许平坦；逆坡使整个工作流失败',
               'D8 HAND downstream elevation must not increase anywhere; flats are allowed and uphill edges fail the entire workflow'),
    'range': ('D8 HAND的米制高程差必须有限，正高程差不能下溢为零',
              'D8 HAND elevation difference in metres must be finite; a positive difference must not underflow to zero'),
})


def _d8_hand_error(code):
    return ValueError(_D8_HAND_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _d8_hand(directions, streams, elevations, elevation_factor):
    """Propagate the first drain identity; subtract original elevations only once."""
    _, downstream, order = _d8_stream_graph(directions, streams, _d8_hand_error)
    heights = elevations.ravel()
    edges = np.flatnonzero(downstream >= 0)
    # Do not convert integer heights before comparison: adjacent UInt64 values
    # can have the same Float64 representation. Equal stored heights are flats.
    if np.any(heights[downstream[edges]] > heights[edges]):
        raise _d8_hand_error('uphill')
    drains = np.full(directions.size, -1, dtype=np.int64)
    result = np.full(directions.size, np.nan)
    river = streams.ravel()
    integer = np.issubdtype(elevations.dtype, np.integer)
    for cell in order[::-1]:
        target = int(downstream[cell])
        drain = int(cell) if river[cell] else int(drains[target]) if target >= 0 else -1
        drains[cell] = drain
        if drain < 0:
            continue
        current, reference = (int(heights[cell]), int(heights[drain])) if integer else (float(heights[cell]), float(heights[drain]))
        delta = current - reference
        if not integer and not math.isfinite(delta):
            # A finite foot-valued result can have an unrepresentable unscaled
            # subtraction. Compute this rare case without intermediate overflow.
            try:
                value = float((Fraction(current) - Fraction(reference)) * Fraction(elevation_factor))
            except OverflowError as cause:
                raise _d8_hand_error('range') from cause
        else:
            value = delta * elevation_factor
        if not math.isfinite(value) or (delta > 0 and value == 0):
            raise _d8_hand_error('range')
        result[cell] = value
    return result.reshape(directions.shape)


def raster_hand_d8(input_raster, stream_raster, elevation_raster, band, stream_band, elevation_band, elevation_unit):
    """Metres above the first downstream stream, with strict full-graph elevation consistency."""
    source, directions, streams = _d8_stream_sources(
        input_raster, stream_raster, band, stream_band, _d8_hand_error)
    dem, _ = _dem_admission(elevation_raster, elevation_unit, elevation_band, 1,
                            diagnostic=lambda code: _d8_hand_error('dem_' + code))
    try:
        _require_same_grid(source, dem)
    except ValueError as cause:
        raise _d8_hand_error('dem_alignment') from cause
    elevations = None
    valid = np.zeros(directions.shape, dtype=bool)
    factor = _ELEVATION_METRES[elevation_unit]
    for x, y, arrays, masks in _blocks(dem, [elevation_band], native=True):
        values, covered = arrays[0], masks[0]
        if elevations is None:
            elevations = np.empty(directions.shape, dtype=values.dtype)
        with np.errstate(over='ignore', invalid='ignore'):
            metres = values.astype(np.float64) * factor
        if np.any(covered & ~np.isfinite(metres)):
            raise _d8_hand_error('dem_overflow')
        height, width = values.shape
        elevations[y:y+height, x:x+width] = values
        valid[y:y+height, x:x+width] = covered
    dem = None
    if not np.array_equal(valid, np.isfinite(directions)):
        raise _d8_hand_error('dem_coverage')
    return _d8_output(source, _d8_hand(directions, streams, elevations, factor), 'metre')


_D8_STREAM_LINK_MESSAGES = {
    key: (zh.replace('D8 Strahler 河流分级', 'D8 河段编号'),
          en.replace('D8 Strahler stream order', 'D8 stream link'))
    for key, (zh, en) in _D8_STREAM_ORDER_MESSAGES.items() if key not in ('overflow', 'method')
}
_D8_STREAM_LINK_MESSAGES['overflow'] = (
    'D8 河段编号的河段数超过Float64精确整数上限2^53−1',
    'D8 stream link count exceeds the Float64 exact integer limit 2^53-1')


def _d8_stream_link_error(code):
    return ValueError(_D8_STREAM_LINK_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _d8_stream_segments(directions, streams, error):
    """One stream numbering path shared by raster labels and downstream lines."""
    valid, downstream, order = _d8_stream_graph(directions, streams, error)
    river = streams.ravel()
    incoming = np.zeros(directions.size, dtype=np.uint8)
    donors = np.flatnonzero(river & (downstream >= 0))
    np.add.at(incoming, downstream[donors], 1)
    starts = np.flatnonzero(river & (incoming != 1))
    if starts.size > _D8_MAX_EXACT_COUNT:
        raise error('overflow')
    labels = np.zeros(directions.size, dtype=np.int64)
    labels[starts] = np.arange(1, starts.size + 1, dtype=np.int64)
    for cell in order:
        if river[cell]:
            target = int(downstream[cell])
            if target >= 0 and incoming[target] == 1:
                labels[target] = labels[cell]
    return valid, downstream, incoming, starts, labels


def _d8_stream_links(directions, streams):
    valid, _, _, _, labels = _d8_stream_segments(directions, streams, _d8_stream_link_error)
    result = labels.reshape(directions.shape).astype(np.float64)
    result[~valid] = np.nan
    return result


def raster_stream_link_d8(input_raster, stream_raster, band, stream_band):
    """Label an explicit stream mask without filling gaps or producing vector lines."""
    source, directions, streams = _d8_stream_sources(
        input_raster, stream_raster, band, stream_band, _d8_stream_link_error)
    return _d8_output(source, _d8_stream_links(directions, streams))


_D8_STREAM_VECTOR_MESSAGES = {
    key: (zh.replace('D8 河段编号', 'D8 河网转矢量线'),
          en.replace('D8 stream link', 'D8 stream vectorization'))
    for key, (zh, en) in _D8_STREAM_LINK_MESSAGES.items()
}
_D8_STREAM_VECTOR_MESSAGES.update({
    'isolated': ('D8 河网转矢量线不支持孤立河网单像元，不能生成零长度线',
                 'D8 stream vectorization does not support isolated single stream cells; zero-length lines cannot be generated'),
    'step': ('D8 河网转矢量线的米制横纵及对角步长必须有限且严格为正',
             'D8 stream vectorization requires finite, strictly positive horizontal, vertical and diagonal steps in metres'),
    'length': ('D8 河网转矢量线累计长度超过Float64有限范围',
               'D8 stream vectorization accumulated length exceeds the finite Float64 range'),
    'coordinates': ('D8 河网转矢量线要求有限且相邻不同的像元中心坐标，不能生成零长度线',
                    'D8 stream vectorization requires finite, distinct adjacent cell-centre coordinates; zero-length lines cannot be generated'),
})


def _d8_stream_vector_error(code):
    return ValueError(_D8_STREAM_VECTOR_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _d8_stream_lines(directions, streams, transform, horizontal_metres, projection):
    """Emit each stream edge once; upstream lines include the next confluence centre."""
    error = _d8_stream_vector_error
    _, downstream, incoming, starts, labels = _d8_stream_segments(directions, streams, error)
    # Reject isolated selected cells even in a mixed network before building features.
    if np.any((downstream[starts] < 0) & (incoming[starts] == 0)):
        raise error('isolated')
    steps = _d8_length_steps(transform[1] * horizontal_metres, -transform[5] * horizontal_metres, error)
    width = directions.shape[1]
    codes = directions.ravel()
    records, geometries = [], []

    def centre(cell):
        row, col = divmod(cell, width)
        point = (transform[0] + (col + .5) * transform[1],
                 transform[3] + (row + .5) * transform[5])
        if not all(math.isfinite(value) for value in point):
            raise error('coordinates')
        return point

    for start in starts:
        cell = int(start)
        if downstream[cell] < 0:
            # A terminal confluence has no edge of its own; incoming lines end here.
            continue
        link_id = int(labels[cell])
        points = [centre(cell)]
        length = 0.0
        while downstream[cell] >= 0:
            target = int(downstream[cell])
            point = centre(target)
            if ((target % width != cell % width and point[0] == points[-1][0])
                or (target // width != cell // width and point[1] == points[-1][1])):
                raise error('coordinates')
            points.append(point)
            length += steps[codes[cell]]
            if not math.isfinite(length):
                raise error('length')
            cell = target
            if labels[cell] != link_id:
                break
        from_row, from_col = divmod(int(start), width)
        to_row, to_col = divmod(cell, width)
        records.append((link_id, from_row, from_col, to_row, to_col, length))
        geometries.append(LineString(points))
    fields = ('link_id', 'from_row', 'from_col', 'to_row', 'to_col', 'length_m')
    attributes = pd.DataFrame.from_records(records, columns=fields).astype({
        name: 'float64' if name == 'length_m' else 'int64' for name in fields})
    return gpd.GeoDataFrame(attributes, geometry=gpd.GeoSeries(geometries, crs=projection), crs=projection)


def raster_stream_to_vector_d8(input_raster, stream_raster, band, stream_band):
    """Convert explicit D8 stream segments to source-CRS two-dimensional lines."""
    source, directions, streams = _d8_stream_sources(
        input_raster, stream_raster, band, stream_band, _d8_stream_vector_error)
    horizontal_metres = _projected_north_up_grid(source)
    return _d8_stream_lines(directions, streams, source.GetGeoTransform(), horizontal_metres, source.GetProjection())


def _terrain(input_raster, elevation_unit, band, processing, **options):
    source, horizontal_metres, _ = _dem_source(input_raster, elevation_unit, band, 3)
    raw = gdal.DEMProcessing(str(_path()), source, processing,
        format='GTiff', creationOptions=['TILED=YES', 'COMPRESS=DEFLATE'],
        alg='Horn', computeEdges=False, scale=horizontal_metres, **options)
    if raw is None:
        raise ValueError('GDAL terrain analysis failed')
    output = _numeric_output(source, 1)
    destination = output.GetRasterBand(1)
    destination.SetUnitType('degree' if processing == 'aspect' else
                            'percent' if options.get('slopeFormat') == 'percent' else
                            'degree' if processing == 'slope' else '')
    for x, y, arrays, masks in _blocks(raw, [1]):
        values = arrays[0].copy()
        values[~masks[0]] = np.nan
        if processing == 'aspect':
            # GDAL aspect uses pixel-axis gradients rather than map distances.
            # Transform that direction into the projected grid so rectangular
            # pixels have the same physical aspect as square pixels.
            angle = np.radians(values)
            transform = source.GetGeoTransform()
            values = np.degrees(np.arctan2(np.sin(angle) / transform[1],
                                           np.cos(angle) / -transform[5])) % 360
        height, width = values.shape
        destination.WriteRaster(x, y, width, height, values.tobytes(), buf_type=gdal.GDT_Float64)
    destination = raw = source = None
    output.FlushCache()
    path = output.GetDescription()
    output = None
    return _raster(path)



_TPI_MESSAGES = {
    key: (zh.replace('DEM 填洼', 'DEM 地形位置指数'),
          en.replace('DEM depression filling', 'DEM topographic position index'))
    for key, (zh, en) in _FILL_MESSAGES.items() if key != 'overflow'
}
_TPI_MESSAGES['numeric'] = (
    'DEM 地形位置指数结果超出有限Float64范围，非零量不能下溢为零',
    'DEM topographic position index result exceeds finite Float64 range; a nonzero quantity must not underflow to zero')


def _tpi_error(code):
    return ValueError(_TPI_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _tpi_block(values, valid, elevation_factor):
    """Exact stored-value differences; only the final metre result is rounded."""
    neighbourhoods = list(_boundary_neighbours(valid))
    complete = np.logical_and.reduce(neighbourhoods)
    result = np.full(complete.shape, np.nan)
    if not complete.any():
        return result
    # Fraction accepts Python int/float from tolist, preserving UInt64 and
    # subnormal Float64 samples without an intermediate elevation conversion.
    exact = np.empty(values.shape, dtype=object)
    exact[:] = 0
    exact[valid] = [Fraction(value) for value in values[valid].tolist()]
    neighbours = list(_boundary_neighbours(exact))
    delta = 8 * neighbours[4][complete]
    for index, samples in enumerate(neighbours):
        if index != 4:
            delta -= samples[complete]
    factor = Fraction(elevation_factor) / 8
    converted = []
    for value in delta:
        metres = value * factor
        try:
            number = float(metres)
        except OverflowError as cause:
            raise _tpi_error('numeric') from cause
        if not math.isfinite(number) or (metres != 0 and number == 0):
            raise _tpi_error('numeric')
        converted.append(number)
    result[complete] = converted
    return result


_TWI_MESSAGES = {
    'grid': ('地形湿度指数需要已知线性单位的二维投影、北向无旋转方形网格', 'Topographic wetness index requires a projected north-up square grid with known linear units'),
    'alignment': ('地形湿度指数的面积与坡度必须具有相同CRS、尺寸及完整网格，请先显式对齐', 'Topographic wetness index area and slope must have the same CRS, dimensions and complete grid; align explicitly first'),
    'area-band': ('地形湿度指数必须显式选择有效的一基面积数据波段', 'Topographic wetness index requires an explicit valid one-based area data band'),
    'slope-band': ('地形湿度指数必须显式选择有效的一基坡度数据波段', 'Topographic wetness index requires an explicit valid one-based slope data band'),
    'complex': ('地形湿度指数不支持复数数据或复数alpha波段', 'Topographic wetness index does not support complex data or alpha bands'),
    'alpha': ('地形湿度指数不能选择alpha作为数据波段', 'Topographic wetness index cannot use an alpha band as data'),
    'area-unit': ('地形湿度指数面积波段必须声明m2单位，请显式提供计入自身的平方米汇流面积', 'Topographic wetness index area band must declare m2; explicitly supply contributing area including self in square metres'),
    'slope-unit': ('地形湿度指数必须显式声明degree或percent坡度单位', 'Topographic wetness index requires explicit slope_unit degree or percent'),
    'unit-conflict': ('地形湿度指数坡度波段单位未知或与声明冲突', 'Topographic wetness index slope band unit is unknown or conflicts with slope_unit'),
    'scale': ('地形湿度指数前必须显式应用面积及坡度scale/offset', 'Topographic wetness index requires area and slope scale/offset to be applied first'),
    'area': ('地形湿度指数的有效汇流面积必须严格大于零', 'Topographic wetness index valid contributing area must be strictly positive'),
    'slope': ('地形湿度指数的有效坡度不得为负，角度必须小于90度', 'Topographic wetness index valid slope must be nonnegative and degrees must be below 90'),
    'numeric': ('地形湿度指数米制边长或结果超出有限Float64范围，非零量不能下溢为零', 'Topographic wetness index metre cell width or result exceeds finite Float64 range; a nonzero quantity must not underflow to zero'),
}


def _twi_error(code):
    return ValueError(_TWI_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _twi_tangent(degrees):
    """Decimal tangent on [0, 90); caller owns the 80-digit context.

    Reduce to [0, pi/4], then sum until neither series changes at this
    precision. No slope cutoff or binary64 degree-to-radian conversion.
    """
    from decimal import Decimal
    if degrees == 45:
        return Decimal(1)
    reciprocal = degrees > 45
    angle = 90 - degrees if reciprocal else degrees
    pi = Decimal('3.1415926535897932384626433832795028841971693993751058209749445923078164062862089986280348253421170679')
    x = angle * pi / 180
    squared = x * x
    sine = sine_term = x
    cosine = cosine_term = Decimal(1)
    k = 1
    while True:
        sine_term *= -squared / ((2 * k) * (2 * k + 1))
        cosine_term *= -squared / ((2 * k - 1) * (2 * k))
        next_sine, next_cosine = sine + sine_term, cosine + cosine_term
        if next_sine == sine and next_cosine == cosine:
            break
        sine, cosine = next_sine, next_cosine
        k += 1
    return cosine / sine if reciprocal else sine / cosine


def raster_twi(input_raster, slope_raster, band, slope_band, slope_unit):
    """Metric TWI; zero slopes remain NoData."""
    return _area_slope_index(input_raster, slope_raster, band, slope_band, slope_unit,
                             kind='twi', error=_twi_error)


_SPI_MESSAGES = {key: (zh.replace('地形湿度指数', '水流功率指数'),
                       en.replace('Topographic wetness index', 'Stream power index'))
                 for key, (zh, en) in _TWI_MESSAGES.items()}


def _spi_error(code):
    return ValueError(_SPI_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def raster_spi(input_raster, slope_raster, band, slope_band, slope_unit):
    """Raw metric stream power index; zero slopes are valid zero."""
    return _area_slope_index(input_raster, slope_raster, band, slope_band, slope_unit,
                             kind='spi', error=_spi_error)


def _area_slope_index(input_raster, slope_raster, band, slope_band, slope_unit, *, kind, error):
    """Shared metric area/slope admission and bounded native pixel processing."""
    from decimal import Decimal, localcontext
    if not isinstance(slope_unit, str) or slope_unit not in ('degree', 'percent'):
        raise error('slope-unit')
    area, slope = _open(input_raster), _open(slope_raster)
    try:
        horizontal = _projected_north_up_grid(area)
        _projected_north_up_grid(slope)
        transform = area.GetGeoTransform()
        if transform[1] != -transform[5]:
            raise error('grid')
    except ValueError as cause:
        raise error('grid') from cause
    try:
        _require_same_grid(area, slope)
    except ValueError as cause:
        raise error('alignment') from cause
    for role, source, index in (('area', area, band), ('slope', slope, slope_band)):
        if type(index) is not int or not 1 <= index <= source.RasterCount:
            raise error(role + '-band')
        selected = source.GetRasterBand(index)
        if gdal.DataTypeIsComplex(selected.DataType):
            raise error('complex')
        if selected.GetColorInterpretation() == gdal.GCI_AlphaBand:
            raise error('alpha')
        if selected.GetScale() not in (None, 1) or selected.GetOffset() not in (None, 0):
            raise error('scale')
        unit = selected.GetUnitType()
        if role == 'area' and unit != 'm2':
            raise error('area-unit')
        if role == 'slope' and unit and unit != slope_unit:
            raise error('unit-conflict')
        for index in range(1, source.RasterCount + 1):
            candidate = source.GetRasterBand(index)
            if candidate.GetColorInterpretation() == gdal.GCI_AlphaBand and gdal.DataTypeIsComplex(candidate.DataType):
                raise error('complex')
        selected = candidate = None
    width = Fraction(transform[1]) * Fraction(horizontal)
    try:
        metres = float(width)
    except OverflowError as cause:
        raise error('numeric') from cause
    if not math.isfinite(metres) or metres <= 0:
        raise error('numeric')
    output = destination = None
    path = None
    try:
        output = _numeric_output(area, 1)
        path = Path(output.GetDescription())
        destination = output.GetRasterBand(1)
        destination.SetUnitType('1' if kind == 'twi' else 'metre')
        with localcontext() as context:
            context.prec = 80
            decimal_width = Decimal(width.numerator) / Decimal(width.denominator)
            for x, y, arrays, masks in _blocks(area, [band], native=True):
                values, valid = arrays[0], masks[0]
                height, columns = values.shape
                _, _, slopes, slope_masks = next(_blocks(slope, [slope_band], window=(x, y, columns, height), native=True))
                betas, slope_valid = slopes[0], slope_masks[0]
                if np.any(values[valid] <= 0):
                    raise error('area')
                if np.any(betas[slope_valid] < 0) or (slope_unit == 'degree' and np.any(betas[slope_valid] >= 90)):
                    raise error('slope')
                complete = valid & slope_valid
                if kind == 'twi':
                    complete &= betas > 0
                result = np.full(values.shape, np.nan)
                # Only bounded block-local deterministic values are deduplicated.
                tangents, indices = {}, {}
                converted = []
                for a, b in zip(values[complete].tolist(), betas[complete].tolist()):
                    key = (a, b)
                    if key not in indices:
                        if b not in tangents:
                            tangents[b] = Decimal(b) / 100 if slope_unit == 'percent' else _twi_tangent(Decimal(b))
                        if kind == 'twi':
                            ratio = Decimal(a) / decimal_width / tangents[b]
                            exact = ratio.ln()
                        else:
                            exact = Decimal(a) / decimal_width * tangents[b]
                        number = float(exact)
                        if not math.isfinite(number) or (exact != 0 and number == 0):
                            raise error('numeric')
                        indices[key] = number
                    converted.append(indices[key])
                result[complete] = converted
                destination.WriteArray(result, x, y)
        destination = None
        output.FlushCache()
        output = None
        return _raster(path)
    except Exception:
        destination = output = None
        if path is not None:
            path.unlink(missing_ok=True)
        raise


def raster_tpi(input_raster, band, elevation_unit):
    """Centre minus the mean of eight neighbours, in metres, on complete windows."""
    source, _ = _dem_admission(input_raster, elevation_unit, band, 1, diagnostic=_tpi_error)
    output = destination = None
    path = None
    completed = False
    try:
        output = _numeric_output(source, 1)
        path = Path(output.GetDescription())
        destination = output.GetRasterBand(1)
        destination.SetUnitType('metre')
        for x, y, array, mask, padding in _neighbourhood_windows(source, band, native=True):
            result = _tpi_block(np.pad(array, padding, constant_values=0),
                                np.pad(mask, padding, constant_values=False),
                                _ELEVATION_METRES[elevation_unit])
            destination.WriteArray(result, x, y)
        destination = None
        output.FlushCache()
        output = None
        result = _raster(path)
        completed = True
        return result
    finally:
        destination = output = source = None
        if path is not None and not completed:
            path.unlink(missing_ok=True)



_TRI_MESSAGES = {
    key: (zh.replace('DEM 填洼', 'DEM 地形崎岖度'),
          en.replace('DEM depression filling', 'DEM terrain ruggedness index'))
    for key, (zh, en) in _FILL_MESSAGES.items() if key != 'overflow'
}
_TRI_MESSAGES['numeric'] = (
    'DEM 地形崎岖度结果超出有限Float64范围，非零量不能下溢为零',
    'DEM terrain ruggedness index result exceeds finite Float64 range; a nonzero quantity must not underflow to zero')


def _tri_error(code):
    return ValueError(_TRI_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _sqrt_fraction(squared, *, error, allow_rounded_zero=False):
    """Round an exact nonnegative square root to binary64, ties to even.

    Scale before integer square root so no intermediate float can overflow or
    erase a representable subnormal. Compare the exact squared midpoint once.
    """
    if not squared:
        return 0.
    numerator, denominator = squared.numerator, squared.denominator
    exponent = numerator.bit_length() - denominator.bit_length()
    if ((numerator < denominator << exponent) if exponent >= 0
            else (numerator << -exponent) < denominator):
        exponent -= 1
    step = max(exponent // 2 - 52, -1074)
    if step < 0:
        numerator <<= -2 * step
    else:
        denominator <<= 2 * step
    integral = math.isqrt(numerator // denominator)
    midpoint = 4 * numerator - denominator * (2 * integral + 1) ** 2
    if midpoint > 0 or (midpoint == 0 and integral % 2):
        integral += 1
    try:
        result = math.ldexp(float(integral), step)
    except OverflowError as cause:
        raise error() from cause
    if not math.isfinite(result) or (result == 0 and not allow_rounded_zero):
        raise error()
    return result


def _tri_root(squared):
    return _sqrt_fraction(squared, error=lambda: _tri_error('numeric'))


def _tri_block(values, valid, elevation_factor):
    """Keep original integer/floating differences and squared sum exact."""
    complete = np.logical_and.reduce(list(_boundary_neighbours(valid)))
    result = np.full(complete.shape, np.nan)
    if not complete.any():
        return result
    exact = np.empty(values.shape, dtype=object)
    exact[:] = 0
    exact[valid] = [Fraction(value) for value in values[valid].tolist()]
    neighbours = list(_boundary_neighbours(exact))
    centre = neighbours[4][complete]
    squared = np.zeros(centre.shape, dtype=object)
    for index, samples in enumerate(neighbours):
        if index != 4:
            delta = centre - samples[complete]
            squared += delta * delta
    factor_squared = Fraction(elevation_factor) ** 2
    result[complete] = [_tri_root(value * factor_squared) for value in squared]
    return result


def raster_tri(input_raster, band, elevation_unit):
    """Riley root of eight squared centre differences, in metres, on complete windows."""
    source, _ = _dem_admission(input_raster, elevation_unit, band, 1, diagnostic=_tri_error)
    output = destination = None
    path = None
    completed = False
    try:
        output = _numeric_output(source, 1)
        path = Path(output.GetDescription())
        destination = output.GetRasterBand(1)
        destination.SetUnitType('metre')
        for x, y, array, mask, padding in _neighbourhood_windows(source, band, native=True):
            result = _tri_block(np.pad(array, padding, constant_values=0),
                                np.pad(mask, padding, constant_values=False),
                                _ELEVATION_METRES[elevation_unit])
            destination.WriteArray(result, x, y)
        destination = None
        output.FlushCache()
        output = None
        result = _raster(path)
        completed = True
        return result
    finally:
        destination = output = source = None
        if path is not None and not completed:
            path.unlink(missing_ok=True)



_ROUGHNESS_MESSAGES = {
    key: (zh.replace('DEM 填洼', 'DEM 地形粗糙度'),
          en.replace('DEM depression filling', 'DEM terrain roughness'))
    for key, (zh, en) in _FILL_MESSAGES.items() if key != 'overflow'
}
_ROUGHNESS_MESSAGES['numeric'] = (
    'DEM 地形粗糙度结果超出有限Float64范围，非零量不能下溢为零',
    'DEM terrain roughness result exceeds finite Float64 range; a nonzero quantity must not underflow to zero')


def _roughness_error(code):
    return ValueError(_ROUGHNESS_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0])


def _roughness_block(values, valid, elevation_factor):
    """Order native samples first; subtract and scale exactly before rounding."""
    complete = np.logical_and.reduce(list(_boundary_neighbours(valid)))
    result = np.full(complete.shape, np.nan)
    if not complete.any():
        return result
    neighbours = iter(_boundary_neighbours(values))
    minimum = next(neighbours)[complete].copy()
    maximum = minimum.copy()
    for samples in neighbours:
        selected = samples[complete]
        np.minimum(minimum, selected, out=minimum)
        np.maximum(maximum, selected, out=maximum)
    factor = Fraction(elevation_factor)
    converted = []
    for low, high in zip(minimum.tolist(), maximum.tolist()):
        metres = (Fraction(high) - Fraction(low)) * factor
        try:
            number = float(metres)
        except OverflowError as cause:
            raise _roughness_error('numeric') from cause
        if not math.isfinite(number) or (metres != 0 and number == 0):
            raise _roughness_error('numeric')
        converted.append(number)
    result[complete] = converted
    return result


def raster_roughness(input_raster, band, elevation_unit):
    """Maximum minus minimum of nine original elevations, in metres, on complete windows."""
    source, _ = _dem_admission(input_raster, elevation_unit, band, 1, diagnostic=_roughness_error)
    output = destination = None
    path = None
    completed = False
    try:
        output = _numeric_output(source, 1)
        path = Path(output.GetDescription())
        destination = output.GetRasterBand(1)
        destination.SetUnitType('metre')
        for x, y, array, mask, padding in _neighbourhood_windows(source, band, native=True):
            result = _roughness_block(np.pad(array, padding, constant_values=0),
                                np.pad(mask, padding, constant_values=False),
                                _ELEVATION_METRES[elevation_unit])
            destination.WriteArray(result, x, y)
        destination = None
        output.FlushCache()
        output = None
        result = _raster(path)
        completed = True
        return result
    finally:
        destination = output = source = None
        if path is not None and not completed:
            path.unlink(missing_ok=True)


_CURVATURE_MESSAGES = {
    key: (zh.replace('DEM 填洼', 'DEM 平均曲率'),
          en.replace('DEM depression filling', 'DEM mean curvature'))
    for key, (zh, en) in _FILL_MESSAGES.items()
}
_CURVATURE_MESSAGES.update({
    'step': ('DEM 平均曲率的米制横纵像元间距必须能表示为严格正有限值',
             'DEM mean curvature requires representable finite, strictly positive horizontal and vertical spacing in metres'),
    'method': ('DEM 曲率的 method 必须为 mean、profile、tangential 或 plan', 'DEM curvature method must be mean, profile, tangential or plan'),
    'numeric': ('DEM 平均曲率的高程差、拟合导数及结果必须为有限值',
                'DEM mean curvature requires finite elevation differences, fitted derivatives and results'),
})


def _curvature_error(code, method='mean'):
    message = _CURVATURE_MESSAGES[code][1 if _DISK_LANGUAGE.get() == 'en' else 0]
    if method == 'profile':
        message = message.replace('DEM 平均曲率', 'DEM 剖面曲率').replace('DEM mean curvature', 'DEM profile curvature')
    elif method == 'tangential':
        message = message.replace('DEM 平均曲率', 'DEM 切向曲率').replace('DEM mean curvature', 'DEM tangential curvature')
    elif method == 'plan':
        message = message.replace('DEM 平均曲率', 'DEM 平面曲率').replace('DEM mean curvature', 'DEM plan curvature')
    return ValueError(message)


def _mean_curvature(p, q, r, s, t):
    """Convex-positive geometric mean curvature; normalize before squaring slopes."""
    scale = np.maximum(1., np.maximum(np.abs(p), np.abs(q)))
    px, py, one = p/scale, q/scale, 1./scale
    squared = one*one + px*px + py*py
    normal = -(1./scale)/(2.*np.sqrt(squared))
    rr, ss, tt = r*normal, s*normal, t*normal
    # Multiply derivatives before tiny normalized factors; squaring 1/scale
    # first can underflow even when the final curvature is representable.
    return (((rr*one)*one)/squared + ((rr*py)*py)/squared
            - (((ss*px)*py)*2.)/squared
            + ((tt*one)*one)/squared + ((tt*px)*px)/squared)


def _profile_curvature(p, q, r, s, t):
    """Convex-positive vertical profile curvature for nonzero fitted gradients."""
    slope_scale = np.maximum(np.abs(p), np.abs(q))
    u, v = p/slope_scale, q/slope_scale
    length = np.hypot(u, v)
    u, v = u/length, v/length
    scale = np.maximum(1., slope_scale)
    px, py, one = p/scale, q/scale, 1./scale
    squared = one*one + px*px + py*py
    normal = -(1./scale)/np.sqrt(squared)
    # Scale each derivative before squaring the inverse scale: r may be huge
    # while a finite profile curvature remains representable at a steep slope.
    rr, ss, tt = r*normal, s*normal, t*normal
    rr, ss, tt = [((term*one)*one)/squared for term in (rr, ss, tt)]
    return (rr*u)*u + ((ss*u)*v)*2. + (tt*v)*v


def _tangential_curvature(p, q, r, s, t):
    """Convex-positive surface normal curvature along the contour tangent."""
    slope_scale = np.maximum(np.abs(p), np.abs(q))
    u, v = p/slope_scale, q/slope_scale
    length = np.hypot(u, v)
    u, v = u/length, v/length
    scale = np.maximum(1., slope_scale)
    px, py, one = p/scale, q/scale, 1./scale
    normal = -(1./scale)/np.sqrt(one*one + px*px + py*py)
    rr, ss, tt = r*normal, s*normal, t*normal
    # The diagonal weights sum to one. Combine them before the mixed term,
    # rather than subtracting profile curvature from twice the mean.
    return ((rr*v)*v + (tt*u)*u) - ((ss*u)*v)*2.


def _plan_curvature(p, q, r, s, t):
    """Convex-positive horizontal contour curvature, without forming |grad z|**3."""
    # Keep original gradient factors in the numerator: normalizing p/q first
    # can erase a tiny component whose product with a large derivative matters.
    pm, pe = np.frexp(p)
    qm, qe = np.frexp(q)
    rm, re = np.frexp(r)
    sm, se = np.frexp(s)
    tm, te = np.frexp(t)
    scale = np.maximum(np.abs(p), np.abs(q))
    gm, ge = np.frexp(scale)
    gm = gm * np.hypot(p/scale, q/scale)
    denominator = gm*gm*gm
    terms, shifts = np.frexp(np.stack((-rm*qm*qm, (2.*sm*pm)*qm, -tm*pm*pm))/denominator)
    exponents = np.stack((re+2*qe, se+pe+qe, te+2*pe)) - 3*ge + shifts
    # Add the two largest magnitudes first so opposite large contributions can
    # cancel before a small third term. Powers of two retain scaled products
    # outside Float64's range until the final result is assembled.
    zero_exponent = np.iinfo(exponents.dtype).min // 4
    effective = np.where(terms != 0., exponents, zero_exponent)
    order = np.lexsort((np.abs(terms), effective), axis=0)
    terms = np.take_along_axis(terms, order, axis=0)
    exponents = np.take_along_axis(exponents, order, axis=0)
    mantissa, exponent = terms[2], exponents[2]
    for index in (1, 0):
        other, other_exponent = terms[index], exponents[index]
        common = np.maximum(np.where(mantissa != 0., exponent, zero_exponent),
                            np.where(other != 0., other_exponent, zero_exponent))
        common = np.where(common == zero_exponent, 0, common)
        mantissa, shift = np.frexp(np.ldexp(mantissa, exponent-common) +
                                  np.ldexp(other, other_exponent-common))
        exponent = common + shift
    result = np.ldexp(mantissa, exponent)
    if np.any(~np.isfinite(result) | ((mantissa != 0.) & (result == 0.))):
        raise _curvature_error('numeric', 'plan')
    return result


def _curvature_block(values, valid, dx, dy, elevation_factor, method='mean'):
    """Closed-form equal-weight six-coefficient least squares on nine samples."""
    height, width = values.shape[0]-2, values.shape[1]-2
    complete = np.ones((height, width), dtype=bool)
    for row in range(3):
        for col in range(3):
            complete &= valid[row:row+height, col:col+width]
    result = np.full((height, width), np.nan)
    if not np.any(complete):
        return result
    integer = np.issubdtype(values.dtype, np.integer)
    center = values[1:1+height, 1:1+width][complete].astype(object if integer else np.float64)
    flat = np.ones(center.shape, dtype=bool) if method in ('profile', 'tangential', 'plan') else None
    neighbours = []
    with np.errstate(over='ignore', invalid='ignore', divide='ignore', under='ignore'):
        for row in range(3):
            for col in range(3):
                sample = values[row:row+height, col:col+width][complete].astype(object if integer else np.float64)
                delta = sample - center
                if not integer and not np.all(np.isfinite(delta)):
                    raise _curvature_error('numeric', method)
                if flat is not None:
                    flat &= delta == 0
                neighbours.append(delta)
        nw, north, ne, west, centre, east, sw, south, se = neighbours
        # Center subtraction is a translation, not a constraint on fitted f.
        numerators = ((ne-nw)+(east-west)+(se-sw),
                      (nw-sw)+(north-south)+(ne-se),
                      ((nw-north)+(ne-north))+((west-centre)+(east-centre))+((sw-south)+(se-south)),
                      (ne-nw)-(se-sw),
                      ((nw-west)+(sw-west))+((north-centre)+(south-centre))+((ne-east)+(se-east)))
        converted = [np.asarray(n, dtype=np.float64)*elevation_factor for n in numerators]
        p, q = converted[0]/6./dx, converted[1]/6./dy
        r, s, t = converted[2]/3./dx/dx, converted[3]/4./dx/dy, converted[4]/3./dy/dy
        if any(not np.all(np.isfinite(a)) for a in (*converted, p, q, r, s, t)):
            raise _curvature_error('numeric', method)
        if method == 'mean':
            curvature = _mean_curvature(p, q, r, s, t)
            numerical = curvature
        else:
            curvature = np.full(p.shape, np.nan)
            curvature[flat] = 0.
            directional = (p != 0) | (q != 0)
            evaluate = {'profile': _profile_curvature, 'tangential': _tangential_curvature, 'plan': _plan_curvature}[method]
            numerical = evaluate(p[directional], q[directional], r[directional], s[directional], t[directional])
            curvature[directional] = numerical
    if not np.all(np.isfinite(numerical)):
        raise _curvature_error('numeric', method)
    result[complete] = curvature
    return result


def raster_curvature(input_raster, band, elevation_unit, method='mean'):
    """Nine-point quadratic mean, profile, tangential or plan curvature in inverse metres."""
    if not isinstance(method, str) or method not in ('mean', 'profile', 'tangential', 'plan'):
        raise _curvature_error('method')
    source, horizontal_metres = _dem_admission(input_raster, elevation_unit, band, 1,
        diagnostic=lambda code: _curvature_error(code, method))
    transform = source.GetGeoTransform()
    dx, dy = transform[1]*horizontal_metres, -transform[5]*horizontal_metres
    if not all(math.isfinite(value) and value > 0 for value in (dx, dy)):
        raise _curvature_error('step', method)
    output = destination = None
    path = None
    completed = False
    try:
        output = _numeric_output(source, 1)
        path = Path(output.GetDescription())
        destination = output.GetRasterBand(1)
        destination.SetUnitType('1/metre')
        for x, y, array, mask, padding in _neighbourhood_windows(source, band, native=True):
            values = np.pad(array, padding, constant_values=0)
            valid = np.pad(mask, padding, constant_values=False)
            result = _curvature_block(values, valid, dx, dy, _ELEVATION_METRES[elevation_unit], method)
            destination.WriteArray(result, x, y)
        destination = None
        output.FlushCache()
        output = None
        result = _raster(path)
        completed = True
        return result
    finally:
        destination = output = source = None
        if path is not None and not completed:
            path.unlink(missing_ok=True)

def raster_slope(input_raster, elevation_unit, band=1, slope_unit='degree'):
    if slope_unit not in ('degree', 'percent'):
        raise ValueError('slope_unit must be degree or percent')
    return _terrain(input_raster, elevation_unit, band, 'slope', slopeFormat=slope_unit)


def raster_aspect(input_raster, elevation_unit, band=1):
    return _terrain(input_raster, elevation_unit, band, 'aspect', trigonometric=False, zeroForFlat=False)


def raster_hillshade(input_raster, elevation_unit, band=1, azimuth=315., altitude=45.):
    if (isinstance(azimuth, bool) or not isinstance(azimuth, (int, float))
        or not math.isfinite(azimuth) or not 0 <= azimuth < 360
        or isinstance(altitude, bool) or not isinstance(altitude, (int, float))
        or not math.isfinite(altitude) or not 0 < altitude <= 90):
        raise ValueError('hillshade azimuth must be in [0,360) and altitude in (0,90] degrees')
    return _terrain(input_raster, elevation_unit, band, 'hillshade', azimuth=azimuth, altitude=altitude)


def raster_contour(input_raster, elevation_unit, band=1, interval=None, levels=None):
    if (interval is None) == (levels is None):
        raise ValueError('contour requires exactly one of interval or levels in metres')
    options = ['NODATA=nan', 'ELEV_FIELD=0']
    if interval is not None:
        if (isinstance(interval, bool) or not isinstance(interval, (int, float))
            or not math.isfinite(interval) or interval <= 0):
            raise ValueError('contour interval must be positive finite metres')
        options += [f'LEVEL_INTERVAL={interval}', 'LEVEL_BASE=0']
    else:
        if (not isinstance(levels, list) or not levels
            or any(isinstance(v, bool) or not isinstance(v, (int, float)) or not math.isfinite(v) for v in levels)
            or any(left >= right for left, right in zip(levels, levels[1:]))):
            raise ValueError('contour levels must be finite strictly increasing metres')
        options.append('FIXED_LEVELS=' + ','.join(str(value) for value in levels))
    source, _, valid_count = _dem_source(input_raster, elevation_unit, band, 2)
    projection = source.GetProjection()
    if not valid_count:
        source = None
        return gpd.GeoDataFrame({'elevation': np.asarray([], dtype=np.float64)}, geometry=[], crs=projection)
    vector = ogr.GetDriverByName('GPKG').CreateDataSource(str(_path('.gpkg')))
    layer = vector.CreateLayer('contours', _crs(source.GetProjection()), ogr.wkbLineString)
    layer.CreateField(ogr.FieldDefn('elevation', ogr.OFTReal))
    elevations, geometries = [], []
    feature = None
    try:
        if gdal.ContourGenerateEx(source.GetRasterBand(1), layer, options=options) != 0:
            raise ValueError('GDAL contour extraction failed')
        for feature in layer:
            elevations.append(float(feature.GetField('elevation')))
            geometries.append(from_wkb(bytes(feature.GetGeometryRef().ExportToWkb())))
    finally:
        feature = layer = vector = source = None
    return gpd.GeoDataFrame({'elevation': np.asarray(elevations, dtype=np.float64)}, geometry=geometries, crs=projection)


def _param(name, kind, description, default=None, required=True, role='param', enum=None):
    return OperatorParam(name=name, data_type=kind, type=role, description=description,
                         required=required, default=default, enum=enum)


_INPUT = lambda: _param('input_raster', 'raster', '当前工作流的栅格输入端口', role='input')
_ALGORITHM = lambda: _param('resampling', 'str', '重采样算法', 'nearest', False, enum=RESAMPLING)
_DEM_PARAMS = lambda: [_INPUT(), _param('elevation_unit', 'str', '高程数值单位，必须明确声明', enum=list(_ELEVATION_METRES)),
                       _param('band', 'int', '从 1 开始的高程数据波段', 1, False)]
_COG_OPTIONS = [
    _param('compression', 'str', '压缩算法', 'DEFLATE', False, enum=['DEFLATE', 'LZW', 'ZSTD', 'NONE']),
    _param('blocksize', 'int', 'COG 块大小', 512, False),
    _param('overview_resampling', 'str', 'COG 金字塔重采样算法', 'nearest', False, enum=RESAMPLING),
]
_IMAGE_SAVE_OPTIONS = [parameter.model_copy(update={'show_when': {'profile': ['geotiff', 'cog']}})
                       for parameter in _COG_OPTIONS]
_SPECS = [
    (raster_load, OperatorCategory.RASTER_IO, '栅格加载', [_param('access_plan', 'object', '源访问计划'), _param('source_crs', 'str', '缺失时补充源 CRS', '', False)], 'raster', ['read']),
    (raster_save, OperatorCategory.RASTER_IO, '栅格保存', [_INPUT(), _param('access_plan', 'object', '目标访问计划'), _param('profile', 'str', '目标布局', 'geotiff', False, enum=['geotiff', 'cog', 'png', 'jpeg']), *_IMAGE_SAVE_OPTIONS, OperatorParam(name='jpeg_quality', data_type='int', type='param', description='JPEG quality', default=90, required=False, show_when={'profile':'jpeg'})], 'object', ['write']),
    (raster_tile, OperatorCategory.RASTER_EXTENT, '栅格切片', [_INPUT(), _param('tile_size', 'list[int]', '每片像素宽高；边缘保留实际尺寸')], 'raster_tiles', ['read']),
    (raster_save_tiles, OperatorCategory.RASTER_IO, '栅格切片批量保存', [_param('input_tiles', 'raster_tiles', '当前执行内的栅格切片集合', role='input'), _param('access_plan', 'object', '目标子目录访问计划'), _param('profile', 'str', '目标布局', 'geotiff', False, enum=['geotiff', 'cog']), *_COG_OPTIONS], 'object', ['read', 'write']),
    (raster_info, OperatorCategory.RASTER_INSPECTION, '栅格信息', [_INPUT()], 'object', ['read']),
    (raster_inspect, OperatorCategory.RASTER_INSPECTION, '原生栅格媒体检查', [_param('access_plan', 'object', '源访问计划')], 'object', ['read']),
    (validate_cog, OperatorCategory.RASTER_INSPECTION, 'COG 合规校验', [_INPUT()], 'object', ['read']),
    (raster_to_cog, OperatorCategory.RASTER_OUTPUT, '栅格转 COG', [_param('access_plan', 'object', '源和目标访问计划'), _param('options', 'object', 'COG 转换选项', {}, False)], 'object', ['read', 'write']),
    (raster_build_overviews, OperatorCategory.RASTER_OUTPUT, '栅格金字塔', [_INPUT(), _param('levels', 'list[int]', '递增金字塔倍率', [2, 4, 8, 16], False), _ALGORITHM()], 'raster', ['read']),
    (raster_reproject, OperatorCategory.RASTER_GRID, '栅格重投影', [_INPUT(), _param('target_crs', 'str', '目标 CRS'), _param('resolution', 'list[float]', '目标分辨率', None, False), _ALGORITHM()], 'raster', ['read']),
    (raster_resample, OperatorCategory.RASTER_GRID, '栅格重采样', [_INPUT(), _param('size', 'list[int]', '输出宽高', None, False), _param('resolution', 'list[float]', '输出分辨率', None, False), _ALGORITHM()], 'raster', ['read']),
    (raster_clip, OperatorCategory.RASTER_EXTENT, '栅格裁剪', [_INPUT(), _param('boundary_crs', 'str', '边界 CRS'), _param('bbox', 'list[float]', '裁剪边界框', None, False), _param('geometry', 'object', 'GeoJSON 面边界', None, False), _param('mode', 'str', '保留区域内或区域外；区域外保留源完整网格', 'inside', False, enum=['inside', 'outside'])], 'raster', ['read']),
    (raster_mosaic, OperatorCategory.RASTER_EXTENT, '栅格计算镶嵌', [_param('input_rasters', 'list[raster]', '按覆盖优先级排列的栅格输入，至少两个', role='input'), _param('target_crs', 'str', '目标 CRS'), _param('resolution', 'list[float]', '目标分辨率'), _param('overlap', 'str', '重叠策略', 'last', False, enum=['first', 'last']), _ALGORITHM()], 'raster', ['read']),
    (raster_align, OperatorCategory.RASTER_GRID, '栅格网格对齐', [_INPUT(), _param('reference_raster', 'raster', '提供输出 CRS、宽高及仿射网格的参考栅格', role='input'), _ALGORITHM()], 'raster', ['read']),
    (raster_update, OperatorCategory.RASTER_EXTENT, '栅格局部更新', [_INPUT(), _param('update_raster', 'raster', '仅用有效像元覆盖基底的同网格更新源', role='input')], 'raster', ['read']),
    (raster_stack, OperatorCategory.RASTER_BAND, '栅格波段组合', [_INPUT(), _param('other_raster', 'raster', '追加数据波段的同网格栅格', role='input')], 'raster', ['read']),
    (raster_select_bands, OperatorCategory.RASTER_BAND, '栅格波段选择', [_INPUT(), _param('bands', 'list[int]', '按顺序选择原始数据波段，序号从 1 开始，可重复'), _param('color_model', 'str', '输出波段颜色解释', 'multispectral', False, enum=['multispectral', 'rgb'])], 'raster', ['read']),
    (raster_band_math, OperatorCategory.RASTER_CALCULATION, '栅格波段计算', [_INPUT(), _param('expression', 'str', '受限表达式，例如 (b2-b1)/(b2+b1)')], 'raster', ['read']),
    (raster_reclassify, OperatorCategory.RASTER_CLASSIFICATION, '栅格重分类', [_INPUT(), _param('rules', 'list[dict]', '互不重叠的精确值或左闭右开区间分类规则，null 端点表示无界'), _param('band', 'int', '从 1 开始的数据波段序号', 1, False), _param('unmatched', 'str', '未命中规则的有效像元处理方式', 'nodata', False, enum=['nodata', 'keep'])], 'raster', ['read']),
    (raster_proximity, OperatorCategory.RASTER_CALCULATION, '栅格邻近距离', [_INPUT(), _param('band', 'int', '显式选择不超过 32 位的整数数据波段'), _param('target_values', 'list[int]', '有效目标分类集合，允许零、不可重复；北向矩形／正交旋转网格按完整仿射米制距离选点，原北向方形结果保持')], 'raster', ['read']),
    (raster_interpolate_idw, OperatorCategory.RASTER_CALCULATION, '点数据IDW插值', [_param('input_gdf', 'GeoDataFrame', '同CRS二维Point及有限数值字段；重复XY拒绝', role='input'), _param('reference_raster', 'raster', '仅提供网格和CRS，不读取或继承模板值／NoData／mask／alpha', role='input'), _param('field', 'str', '显式数值属性字段；合法零负值保留，整数须精确表示'), _param('radius_metres', 'float', '显式有限正米制圆半径；等号纳入，按power的距离反比加权'), _param('min_points', 'int', '非布尔正整数，默认1；非重合支撑不足为NoData，精确重合仍保留样本值', 1, False), _param('max_points', 'int', '省略不限；显式非布尔正整数且不小于min_points，按精确距离／X／Y取最近N点，精确重合保留原值', None, False), _param('power', 'int', '距离幂次1／2／3；默认2保持原结果，不改变半径／支撑点规则', 2, False)], 'raster', ['read']),
    (raster_point_density, OperatorCategory.RASTER_CALCULATION, '米制圆形点密度', [_param('input_gdf', 'GeoDataFrame', '二维Point逐要素贡献，重复XY保留；可选显式非负权重列', role='input'), _param('reference_raster', 'raster', '仅提供网格／CRS，原像元与mask／alpha不参与', role='input'), _param('radius_metres', 'float', '显式有限正米制半径；无支持为0'), _param('method', 'str', 'uniform等权计数；quartic二维四次核，半径边界贡献0', 'uniform', False, enum=['uniform', 'quartic']), _param('weight_field', 'str', '可选现有非几何数值权重列；有限非负、不归一化，省略保持原结果，显式null非法', None, False)], 'raster', ['read']),
    (raster_line_density, OperatorCategory.RASTER_CALCULATION, '米制圆形线密度', [_param('input_gdf', 'GeoDataFrame', '二维LineString／MultiLineString逐原始线段贡献，重复与重叠独立累计', role='input'), _param('reference_raster', 'raster', '仅提供尺寸／完整网格／CRS，不继承原像元或mask', role='input'), _param('radius_metres', 'float', '显式有限正米制半径；圆内米制线长除完整圆面积，无支持为0'), _param('method', 'str', 'uniform圆内线长；quartic固定二维四次核沿原始段积分，边界0、不离散或归一化', 'uniform', False, enum=['uniform', 'quartic']), _param('weight_field', 'str', '可选现有非几何数值权重列；有限非负、乘线长、不归一化，省略保持原结果，显式null非法', None, False)], 'raster', ['read']),
    (raster_nibble, OperatorCategory.RASTER_CLASSIFICATION, '分类栅格蚕食', [_INPUT(), _param('band', 'int', '显式选择精确安全整数分类数据波段，不能选择 alpha'), _param('target_values', 'list[int]', '待替换的安全整数分类，非空且不重复；取最近原始有效非目标供体，等距按源行列')], 'raster', ['read']),
    (raster_boundary_clean, OperatorCategory.RASTER_CLASSIFICATION, '分类栅格边界清理', [_INPUT(), _param('band', 'int', '显式分类数据波段，保留无效覆盖'), _param('rounds', 'int', '八邻域扩张和收缩轮数，仅接受整数 1 或 2，默认 1；两轮共用原始类别优先级，以完整首轮结果为第二轮基准', 1, False)], 'raster', ['read']),
    (raster_sieve, OperatorCategory.RASTER_CLASSIFICATION, '分类栅格小斑块筛除', [_INPUT(), _param('band', 'int', '显式分类数据波段，精确安全整数；原无效覆盖保留'), _param('min_pixels', 'int', '正整数像元数阈值；与min_area_m2必须且只能提供一个，等于阈值保留', None, False), _param('min_area_m2', 'float', '有限正平方米阈值；与min_pixels互斥，北向无旋转投影网格、按CRS线性单位换算，精确比较且等于阈值保留', None, False), _param('connectivity', 'int', '显式4或8邻接；仅合并到原始达标的直接相邻大斑块，大小／较小分类／最前行列决胜；无供体保留，单轮冻结'), _param('protected_values', 'list[int]', '免于替换的安全整数分类，列表不可重复；空列表保持原结果，保护斑块作为供体仍须原始大小达标', [], False)], 'raster', ['read']),
    (raster_region_group, OperatorCategory.RASTER_CLASSIFICATION, '分类栅格连通斑块编号', [_INPUT(), _param('band', 'int', '显式分类数据波段，安全整数；零和负分类有效，不应用scale/offset'), _param('connectivity', 'int', '显式4或8邻接；同类连通斑块按最前行列从1编号，保留无效覆盖')], 'raster', ['read']),
    (raster_thin, OperatorCategory.RASTER_CLASSIFICATION, '分类栅格细化', [_INPUT(), _param('band', 'int', '显式分类数据波段，不能选择 alpha'), _param('foreground_value', 'float', '单个精确安全整数前景分类；Guo–Hall 八邻域收敛，输出骨架1／背景0／原无效NaN')], 'raster', ['read']),
    (raster_aggregate, OperatorCategory.RASTER_CALCULATION, '栅格聚合', [_INPUT(), _param('factors', 'list[int]', '相邻像元的列、行聚合倍数，例如 [2,2]'), _param('method', 'str', '聚合方法；忽略无效像元并保留边缘组', enum=['sum', 'mean', 'min', 'max'])], 'raster', ['read']),
    (raster_footprint, OperatorCategory.RASTER_EXTENT, '栅格有效像元轮廓', [_INPUT(), _param('bands', 'list[int]', '选择互不重复的一基数据波段，省略时为所有数据波段', None, False), _param('validity', 'str', '有效范围并集或交集', 'any', False, enum=['any', 'all'])], 'GeoDataFrame', ['read']),
    (raster_polygonize, OperatorCategory.RASTER_FORMAT, '分类栅格转面', [_INPUT(), _param('band', 'int', '从 1 开始的整数分类数据波段'), _param('connectivity', 'int', '同分类像元的邻接方式，仅接受整数 4 或 8', 4, False)], 'GeoDataFrame', ['read']),
    (vector_rasterize, OperatorCategory.RASTER_FORMAT, '矢量栅格化', [_param('input_gdf', 'GeoDataFrame', '有效二维面输入，后输入要素覆盖前者', role='input'), _param('reference_raster', 'raster', '仅提供同 CRS 的完整网格，不继承像元值或有效性', role='input'), _param('field', 'str', '数值属性字段，与常量二选一', None, False), _param('burn_value', 'float', '有限数值常量，与属性字段二选一', None, False), _param('all_touched', 'bool', '是否烧写所有触及像元；默认使用 GDAL 像元中心语义', False, False)], 'raster', ['read']),
    (raster_statistics, OperatorCategory.RASTER_STATISTICS, '栅格统计', [_INPUT(), _param('band', 'int', '从 1 开始的波段序号', 1, False), _param('percentiles', 'list[float]', '可选非空严格递增且不重复的0至100有限百分位列表；共享一次全量排序与线性插值，50为中位数；未提供时只返回原统计', None, False)], 'object', ['read']),
    (raster_cut_fill, OperatorCategory.RASTER_STATISTICS, '双DEM填挖方统计', [_INPUT(), _param('after_raster', 'raster', '施工后DEM，须与施工前同CRS、尺寸和完整网格，由用户保证相同高程基准', role='input'), _param('band', 'int', '显式施工前高程数据波段'), _param('after_band', 'int', '显式施工后高程数据波段'), _param('elevation_unit', 'str', '显式施工前高程单位', enum=list(_ELEVATION_METRES)), _param('after_elevation_unit', 'str', '显式施工后高程单位；挖填体积各非负，净增加为填方减挖方，统一立方米', enum=list(_ELEVATION_METRES))], 'DataFrame', ['read']),
    (raster_surface_area, OperatorCategory.RASTER_STATISTICS, 'DEM 三维表面积统计', [_INPUT(), _param('band', 'int', '显式高程数据波段；四角有效像元中心组成面片，外缘不补面积'), _param('elevation_unit', 'str', '显式高程单位；双对角三角剖分逐面片取平均，面积平方米，结果为普通表', enum=list(_ELEVATION_METRES))], 'DataFrame', ['read']),
    (raster_zonal_statistics, OperatorCategory.RASTER_STATISTICS, '栅格分区统计', [_INPUT(), _param('method', 'str', 'summary保留原数值统计；class_frequency按分区／类别计数，以有效分类数为比例分母，全无效分区保留诊断行', 'summary', False, enum=['summary', 'class_frequency']), _param('zones_raster', 'raster', '整数分类分区输入，须与数值栅格具有完全相同的网格', role='input'), _param('band', 'int', '显式选择从 1 开始的数值数据波段'), _param('zones_band', 'int', '显式选择从 1 开始的整数分类数据波段'), _param('weights_raster', 'raster', '可选同网格权重栅格，须与weights_band成对显式提供；有限非负，缺失权重不补零，与percentiles互斥', None, False, role='input'), _param('weights_band', 'int', '提供weights_raster时必填的一基权重数据波段；追加权重总和、加权求和与加权均值，原统计不改写', None, False), _param('include_weighted_stddev', 'bool', '显式启用加权总体标准差，须提供权重；总体分母，全1沿用原stddev，全零为null，原统计保持', False, False), _param('percentiles', 'list[float]', '可选非空严格递增且不重复的0至100有限百分位列表；每区全量固定线性插值、共享一次排序归并，50为中位数；按顺序追加百分位与结果列对，未提供时保持原统计九列', None, False)], 'DataFrame', ['read']),
    (raster_focal_statistics, OperatorCategory.RASTER_STATISTICS, '栅格邻域统计', [_INPUT(), _param('band', 'int', '显式选择从 1 开始的数据波段，不能选择 alpha'), _param('window_type', 'str', 'square_pixels为像元方窗，circle_metres为米制圆窗；两种窗口均支持十二种方法', 'square_pixels', False, enum=['square_pixels', 'circle_metres']), _param('window_size', 'int', 'square_pixels时必须显式提供以像元为单位的正方形窗口边长3、5或7；圆窗不得提供', None, False).model_copy(update={'show_when': {'window_type': ['square_pixels']}}), _param('radius_metres', 'float', 'circle_metres时必须显式提供有限正米制半径；北向矩形或保持北向轴手性的正交旋转网格按完整仿射距离筛选，中心距离等于半径纳入、不设epsilon，候选外接窗口最多5×5；方窗不得提供', None, False).model_copy(update={'show_when': {'window_type': ['circle_metres']}}), _param('method', 'str', 'sum/mean/min/max/stddev（总体标准差）/mode（分类众数；并列优先中心原分类，否则较小分类）/median（中位数；偶数取两中值平均）/percentile（固定线性百分位）/variety（分类种类数；有效整数分类去重计数）/weighted_mean（非负矩阵加权均值；有效位置重新归一化）/weighted_stddev（非负矩阵加权总体标准差）/weighted_sum（非负矩阵加权求和；不归一化）', enum=['sum', 'mean', 'min', 'max', 'stddev', 'mode', 'median', 'percentile', 'variety', 'weighted_mean', 'weighted_stddev', 'weighted_sum']), _param('percentiles', 'list[float]', 'method=percentile时必须显式指定非空严格递增且不重复的0至100有限百分位列表；固定线性插值，50与median一致；按顺序输出多波段，其他方法不得提供', None, False).model_copy(update={'show_when': {'method': ['percentile']}}), _param('weights', 'list[list[float]]', 'weighted_mean/weighted_stddev/weighted_sum必填：有限非负二维权重；方窗为3×3、5×5或7×7，圆窗为派生最小正方形（1×1、3×3或5×5），圆外显式为零，窗口内至少一个正值；行列直接对应，不翻转；其他方法不得提供', None, False).model_copy(update={'show_when': {'method': ['weighted_mean', 'weighted_stddev', 'weighted_sum']}})], 'raster', ['read']),
    (raster_vector_zonal_statistics, OperatorCategory.RASTER_STATISTICS, '矢量分区统计', [_INPUT(), _param('input_gdf', 'GeoDataFrame', '同 CRS 有效二维面，各面独立统计，重叠分别计入', role='input'), _param('band', 'int', '显式选择从 1 开始的数据波段，不能选择 alpha'), _param('method', 'str', 'summary保持原统计；class_frequency按面／类别展开并保留诊断行', 'summary', False, enum=['summary', 'class_frequency']), _param('id_field', 'str', 'class_frequency须显式选择现有非空唯一面编号；保留原类型，不从索引推断', None, False).model_copy(update={'show_when': {'method': ['class_frequency']}}), _param('all_touched', 'bool', '默认按像元中心覆盖，可选全部触及像元', False, False), _param('field_prefix', 'str', '新增统计列前缀，同名冲突报错，可为空', 'zonal_', False), _param('weights_raster', 'raster', '可选同网格权重栅格，须与weights_band成对提供且与percentiles互斥；沿原面覆盖，重叠面独立统计，缺失权重不补零', None, False, role='input'), _param('weights_band', 'int', '提供weights_raster时必填的一基权重数据波段；有限非负，追加三项加权统计，原统计不改写', None, False), _param('include_weighted_stddev', 'bool', '显式启用逐面加权总体标准差，须提供权重；全1沿用原stddev，全零null，原统计和重叠面独立规则保持', False, False), _param('include_area_weighted', 'bool', '显式追加覆盖平方米、有效覆盖平方米及面积加权均值；原八列保持，新面积独立于all_touched；与外部权重、百分位及加权标准差互斥', False, False), _param('include_area_weighted_stddev', 'bool', '仅面积模式显式启用总体标准差；正有效面积常量0、无有效面积null，单位沿数值，保留原十一列及重叠独立规则', False, False), _param('percentiles', 'list[float]', '可选非空严格递增且不重复的0至100有限百分位列表；逐面固定线性插值、共享一次读取排序归并，50为中位数；按顺序追加百分位与结果列对，未提供时保持原统计八列', None, False)], 'GeoDataFrame', ['read']),
    (raster_sample_points, OperatorCategory.RASTER_STATISTICS, '点位栅格采样', [_INPUT(), _param('input_gdf', 'GeoDataFrame', '同CRS的有效二维Point，保留全部点、几何及属性；新增列冲突拒绝', role='input'), _param('band', 'int', '显式一基数据波段；sample_status区分ok／outside／nodata'), _param('method', 'str', 'cell取所在像元原值；bilinear按像元中心插值，正权重支持须全部有效，不补值或重新归一化', 'cell', False, enum=['cell', 'bilinear'])], 'GeoDataFrame', ['read']),
    (raster_sample_lines, OperatorCategory.RASTER_STATISTICS, '沿线栅格采样', [_INPUT(), _param('input_gdf', 'GeoDataFrame', '同投影CRS的有效二维LineString；保留显式编号和全部属性', role='input'), _param('band', 'int', '显式一基数据波段；沿既有原值域和有效性规则'), _param('id_field', 'str', '显式现有非几何非空唯一标量编号字段，不用索引或FID推断'), _param('spacing_metres', 'float', '显式有限正米制间距，保留起终点；结果追加distance_m及采样值／状态'), _param('method', 'str', '复用cell原像元值或bilinear中心插值；无效和范围外采样点保留', 'cell', False, enum=['cell', 'bilinear'])], 'GeoDataFrame', ['read']),
    (raster_histogram, OperatorCategory.RASTER_STATISTICS, '栅格直方图', [_INPUT(), _param('band', 'int', '从 1 开始的波段序号', 1, False), _param('bins', 'int', '分桶数量', 256, False), _param('value_range', 'list[float]', '统计值域', None, False)], 'object', ['read']),
    (raster_fill_depressions, OperatorCategory.RASTER_FLOW, 'DEM 填洼', [_INPUT(), _param('band', 'int', '显式高程数据波段；八邻接，图边及无效区边界开放'), _param('elevation_unit', 'str', '明确原高程单位，输出米制最低溢流高程', enum=list(_ELEVATION_METRES))], 'raster', ['read']),
    (raster_flow_direction_d8, OperatorCategory.RASTER_FLOW, 'DEM D8 流向', [_INPUT(), _param('band', 'int', '显式高程数据波段；平坦区无环，封闭洼地失败'), _param('elevation_unit', 'str', '明确原高程单位，输出方向码与开放终点0', enum=list(_ELEVATION_METRES))], 'raster', ['read']),
    (raster_flow_accumulation_d8, OperatorCategory.RASTER_FLOW, 'D8 汇流累积', [_INPUT(), _param('band', 'int', '显式方向数据波段；非法方向或循环失败'), _param('measure', 'str', 'count：上游有效像元数、不计自身；area：包含自身的投影网格面积、统一平方米', 'count', False, enum=['count', 'area'])], 'raster', ['read']),
    (raster_flow_accumulation_weighted_d8, OperatorCategory.RASTER_FLOW, 'D8 加权汇流累积', [_INPUT(), _param('weights_raster', 'raster', '同网格、同有效覆盖的有限非负权重输入，不补零或自动对齐', role='input'), _param('band', 'int', '显式方向数据波段'), _param('weights_band', 'int', '显式权重数据波段；上游权重和、不计自身，沿用权重单位')], 'raster', ['read']),
    (raster_flow_length_d8, OperatorCategory.RASTER_FLOW, 'D8流长', [_INPUT(), _param('band', 'int', '显式方向数据波段；中心到中心的米制路径，非法方向或循环失败'), _param('method', 'str', 'downstream：到开放终点，终点0；upstream_longest：最长上游入流路径，汇合取最大、无入流0', enum=['downstream', 'upstream_longest'])], 'raster', ['read']),
    (raster_drainage_basins_d8, OperatorCategory.RASTER_CATCHMENT, 'D8 汇水分区', [_INPUT(), _param('band', 'int', '显式方向数据波段；每个开放终点独立分区，按出口行列顺序从1编号，非法方向或循环失败')], 'raster', ['read']),
    (raster_snap_pour_points, OperatorCategory.RASTER_CATCHMENT, '汇水点吸附', [_INPUT(), _param('points_raster', 'raster', '同网格、同有效覆盖的唯一正安全整数标记，零为未选；保留编号，碰撞失败', role='input'), _param('band', 'int', '显式未加权累积计数数据波段'), _param('points_band', 'int', '显式汇水点标记数据波段'), _param('radius_metres', 'float', '显式有限非负米制圆半径；允许跨分水岭，最大计数后按距离／行／列决胜')], 'raster', ['read']),
    (raster_watershed_d8, OperatorCategory.RASTER_CATCHMENT, 'D8 指定汇水点汇水区', [_INPUT(), _param('points_raster', 'raster', '同网格、同有效覆盖的汇水点标记，零为未选、正安全整数编号全图唯一，不自动吸附', role='input'), _param('band', 'int', '显式方向数据波段'), _param('points_band', 'int', '显式标记数据波段；保留编号并包含点自身，嵌套汇水点失败')], 'raster', ['read']),
    (raster_subcatchments_d8, OperatorCategory.RASTER_CATCHMENT, 'D8 增量子汇水区', [_INPUT(), _param('points_raster', 'raster', '同网格、同有效覆盖的汇水点标记，零为未选、正安全整数编号全图唯一，不自动吸附', role='input'), _param('band', 'int', '显式方向数据波段'), _param('points_band', 'int', '显式标记数据波段；保留编号并包含点自身；允许嵌套，按下游第一个指定点划分互斥增量范围')], 'raster', ['read']),
    (raster_stream_order_d8, OperatorCategory.RASTER_STREAM, 'D8 河流分级', [_INPUT(), _param('stream_raster', 'raster', '同网格、同有效覆盖的0／1河网掩膜输入，断流失败、不自动补线', role='input'), _param('band', 'int', '显式方向数据波段'), _param('stream_band', 'int', '显式0／1河网掩膜数据波段'), _param('method', 'str', 'strahler：同最高级汇合升级；shreve：全部实际河网入流量级求和', 'strahler', False, enum=['strahler', 'shreve'])], 'raster', ['read']),
    (raster_distance_to_stream_d8, OperatorCategory.RASTER_STREAM, 'D8 到河网沿流距离', [_INPUT(), _param('stream_raster', 'raster', '同网格、同有效覆盖的0／1河网；河网自身0、未到达NoData、断流失败', role='input'), _param('band', 'int', '显式方向数据波段；完整非法路线或循环失败'), _param('stream_band', 'int', '显式河网数据波段；到首次河网中心的米制沿流距离')], 'raster', ['read']),
    (raster_hand_d8, OperatorCategory.RASTER_STREAM, 'D8 到河网相对高程（HAND）', [_INPUT(), _param('stream_raster', 'raster', '同网格、同有效覆盖的0／1河网；完整非法路线及断流失败', role='input'), _param('elevation_raster', 'raster', '同网格、同有效覆盖的DEM；全图下游不升高，允许平坦、逆坡失败', role='input'), _param('band', 'int', '显式方向数据波段'), _param('stream_band', 'int', '显式二值河网数据波段'), _param('elevation_band', 'int', '显式高程数据波段；相对首次河网高程差，河网0、未到达NoData'), _param('elevation_unit', 'str', '显式高程单位，结果统一米制', enum=['metre', 'foot', 'us_survey_foot'])], 'raster', ['read']),
    (raster_stream_link_d8, OperatorCategory.RASTER_STREAM, 'D8 河段编号', [_INPUT(), _param('stream_raster', 'raster', '同网格、同有效覆盖的0／1河网掩膜，断流失败、不自动补线', role='input'), _param('band', 'int', '显式方向数据波段'), _param('stream_band', 'int', '显式河网数据波段；汇合点归下游段，按河段起点行列顺序从1编号')], 'raster', ['read']),
    (raster_stream_to_vector_d8, OperatorCategory.RASTER_STREAM, 'D8 河网转矢量线', [_INPUT(), _param('stream_raster', 'raster', '同网格、同有效覆盖的0／1河网掩膜；孤立单像元失败，不自动补线', role='input'), _param('band', 'int', '显式方向数据波段'), _param('stream_band', 'int', '显式河网数据波段；连接像元中心，保留河段ID及米制长度')], 'GeoDataFrame', ['read']),
    (raster_twi, OperatorCategory.RASTER_TERRAIN, '地形湿度指数 TWI', [_INPUT(), _param('slope_raster', 'raster', '同完整网格的坡度栅格；零坡度NoData，非零无阈值', role='input'), _param('band', 'int', '显式m2汇流面积波段，计入自身，固定面积除以方形像元米制边长'), _param('slope_band', 'int', '显式坡度数据波段'), _param('slope_unit', 'str', '显式坡度单位；指数允许负数及合法零，不表示实际含水量', enum=['degree', 'percent'])], 'raster', ['read']),
    (raster_spi, OperatorCategory.RASTER_TERRAIN, '水流功率指数 SPI', [_INPUT(), _param('slope_raster', 'raster', '同完整网格的坡度栅格；零坡度0，非零无阈值', role='input'), _param('band', 'int', '显式m2汇流面积波段，计入自身，固定面积除以方形像元米制边长'), _param('slope_band', 'int', '显式坡度数据波段'), _param('slope_unit', 'str', '显式坡度单位；原始非负SPI、单位metre，不表示实际功率或侵蚀量', enum=['degree', 'percent'])], 'raster', ['read']),
    (raster_tpi, OperatorCategory.RASTER_TERRAIN, 'DEM 地形位置指数', [_INPUT(), _param('band', 'int', '显式高程数据波段；固定3×3中心减周围8点等权均值'), _param('elevation_unit', 'str', '显式高程单位；输出米，完整九点有效才计算，合法零保留', enum=list(_ELEVATION_METRES))], 'raster', ['read']),
    (raster_tri, OperatorCategory.RASTER_TERRAIN, 'DEM 地形崎岖度', [_INPUT(), _param('band', 'int', '显式高程数据波段；固定3×3 Riley中心与8邻居高差平方和开方，不除以8'), _param('elevation_unit', 'str', '显式高程单位；输出米，完整九点有效才计算，合法零保留', enum=list(_ELEVATION_METRES))], 'raster', ['read']),
    (raster_roughness, OperatorCategory.RASTER_TERRAIN, 'DEM 地形粗糙度', [_INPUT(), _param('band', 'int', '显式高程数据波段；固定3×3包含中心的九点最大高程减最小高程，不取半高差'), _param('elevation_unit', 'str', '显式高程单位；输出米，完整九点有效才计算，合法零保留', enum=list(_ELEVATION_METRES))], 'raster', ['read']),
    (raster_curvature, OperatorCategory.RASTER_TERRAIN, 'DEM 曲率', [_INPUT(), _param('band', 'int', '显式高程数据波段；完整有效3×3九点等权二次曲面拟合，凸正凹负'), _param('elevation_unit', 'str', '显式高程单位；输出每米（1/metre），边缘及无效邻域NoData', enum=list(_ELEVATION_METRES)), _param('method', 'str', 'mean：平均曲率；profile：剖面曲率；tangential：切向曲率；plan：平面曲率；方向方法九点等高平地0、其他零梯度NoData', 'mean', False, enum=['mean', 'profile', 'tangential', 'plan'])], 'raster', ['read']),
    (raster_slope, OperatorCategory.RASTER_TERRAIN, 'DEM 坡度', [*_DEM_PARAMS(), _param('slope_unit', 'str', '坡度输出单位', 'degree', False, enum=['degree', 'percent'])], 'raster', ['read']),
    (raster_aspect, OperatorCategory.RASTER_TERRAIN, 'DEM 坡向', _DEM_PARAMS(), 'raster', ['read']),
    (raster_hillshade, OperatorCategory.RASTER_TERRAIN, 'DEM 地形晕渲', [*_DEM_PARAMS(), _param('azimuth', 'float', '光照方位角，北起顺时针 [0,360)', 315., False), _param('altitude', 'float', '光照高度角 (0,90]', 45., False)], 'raster', ['read']),
    (raster_contour, OperatorCategory.RASTER_TERRAIN, 'DEM 等值线', [*_DEM_PARAMS(), _param('interval', 'float', '等高距（米），与指定高程二选一', None, False), _param('levels', 'list[float]', '严格递增的指定高程（米）', None, False)], 'GeoDataFrame', ['read']),
    (raster_line_of_sight, OperatorCategory.RASTER_TERRAIN, 'DEM 两点通视', [
        _INPUT(), _param('band', 'int', '显式一基高程数据波段'),
        _param('elevation_unit', 'str', '显式高程单位', enum=list(_ELEVATION_METRES)),
        _param('observer_xy', 'list[float]', '观察点 [x,y]，使用 DEM 投影坐标'),
        _param('target_xy', 'list[float]', '目标点 [x,y]，使用 DEM 投影坐标'),
        _param('observer_height_m', 'float', '观察点离地高度（米），有限且非负'),
        _param('target_height_m', 'float', '目标点离地高度（米），有限且非负'),
    ], 'DataFrame', ['read']),
    (raster_viewshed, OperatorCategory.RASTER_TERRAIN, 'DEM 单观察点可视域', [
        _INPUT(), _param('band', 'int', '显式一基高程数据波段'),
        _param('elevation_unit', 'str', '显式高程单位', enum=list(_ELEVATION_METRES)),
        _param('observer_xy', 'list[float]', '观察点 [x,y]，使用 DEM 投影坐标'),
        _param('observer_height_m', 'float', '观察点离地高度（米），有限且非负'),
        _param('target_height_m', 'float', '各目标中心离地高度（米），有限且非负'),
        _param('radius_metres', 'float', '显式有限正米制半径，等号纳入；遮挡0、可见1、无法判定2、NoData255'),
    ], 'raster', ['read']),
]
OPERATORS = {}
for function, category, label, params, output, effects in _SPECS:
    name = function.__name__
    examples = {'input_raster': {'$ref': 'source'}}
    if name in ('raster_load', 'raster_to_cog', 'raster_inspect'):
        examples = {'access_plan': {'schema_version': 'addp.workflow.access-plan/v1', 'source': {'kind':'file','format':'tiff','access':{'method':'mounted_path','path':'/mnt/input.tif'}}}}
    if name == 'raster_inspect':
        examples['access_plan']['source'].update({'format': 'img', 'access': {'method': 'mounted_path', 'path': '/mnt/input.img'}})
    if name in ('raster_save', 'raster_to_cog'):
        examples['access_plan'] = {**examples.get('access_plan', {}), 'schema_version':'addp.workflow.access-plan/v1', 'target':{'kind':'file','format':'tiff','name':'output.tif','write_mode':'create','access':{'method':'mounted_path','path':'/mnt/output.tif'}}}
    examples.update({'raster_reproject':{'target_crs':'EPSG:3857'}, 'raster_resample':{'size':[512,512]}, 'raster_clip':{'boundary_crs':'EPSG:4326','bbox':[110,20,111,21]}, 'raster_band_math':{'expression':'(b2-b1)/(b2+b1)'}}.get(name, {}))
    examples.update({'raster_align': {'reference_raster': {'$ref': 'reference'}},
                     'raster_stack': {'other_raster': {'$ref': 'other'}},
                     'raster_update': {'update_raster': {'$ref': 'update'}},
                     'raster_select_bands': {'bands': [3, 2, 1], 'color_model': 'rgb'}}.get(name, {}))
    if name in ('raster_twi', 'raster_spi'):
        examples.update({'slope_raster': {'$ref': 'slope'}, 'band': 1, 'slope_band': 1, 'slope_unit': 'degree'})
    if name == 'raster_cut_fill':
        examples.update({'after_raster': {'$ref': 'after'}, 'band': 1, 'after_band': 1, 'elevation_unit': 'metre', 'after_elevation_unit': 'metre'})
    if name == 'raster_zonal_statistics':
        examples.update({'zones_raster': {'$ref': 'zones'}, 'band': 1, 'zones_band': 1})
    if name == 'raster_proximity':
        examples.update({'band': 1, 'target_values': [0, 1]})
    if name == 'raster_thin':
        examples.update({'band': 1, 'foreground_value': 1})
    if name == 'raster_sample_points':
        examples.update({'input_gdf': {'$ref': 'points'}, 'band': 1})
    if name == 'raster_sample_lines':
        examples.update({'input_gdf': {'$ref': 'lines'}, 'band': 1, 'id_field': 'line_id', 'spacing_metres': 10., 'method': 'cell'})
    if name == 'raster_region_group':
        examples.update({'band': 1, 'connectivity': 4})
    if name == 'raster_sieve':
        examples.update({'band': 1, 'min_pixels': 10, 'connectivity': 4, 'protected_values': [7]})
    if name in ('raster_boundary_clean', 'raster_flow_accumulation_d8', 'raster_flow_length_d8', 'raster_drainage_basins_d8'):
        examples.update({'band': 1})
    if name == 'raster_flow_accumulation_d8':
        examples.update({'measure': 'count'})
    if name in ('raster_stream_order_d8', 'raster_stream_link_d8', 'raster_stream_to_vector_d8', 'raster_distance_to_stream_d8'):
        examples.update({'stream_raster': {'$ref': 'streams'}, 'band': 1, 'stream_band': 1})
    if name == 'raster_hand_d8':
        examples.update({'stream_raster': {'$ref': 'streams'}, 'elevation_raster': {'$ref': 'elevation'}, 'band': 1, 'stream_band': 1, 'elevation_band': 1, 'elevation_unit': 'metre'})
    if name == 'raster_flow_length_d8':
        examples.update({'method': 'downstream'})
    if name == 'raster_stream_order_d8':
        examples.update({'method': 'strahler'})
    if name == 'raster_snap_pour_points':
        examples.update({'points_raster': {'$ref': 'points'}, 'band': 1, 'points_band': 1, 'radius_metres': 100.})
    if name in ('raster_watershed_d8', 'raster_subcatchments_d8'):
        examples.update({'points_raster': {'$ref': 'points'}, 'band': 1, 'points_band': 1})
    if name == 'raster_flow_accumulation_weighted_d8':
        examples.update({'weights_raster': {'$ref': 'weights'}, 'band': 1, 'weights_band': 1})
    if name == 'raster_nibble':
        examples.update({'band': 1, 'target_values': [7]})
    if name == 'raster_focal_statistics':
        examples.update({'band': 1, 'window_size': 3, 'method': 'mean'})
    if name == 'raster_vector_zonal_statistics':
        examples.update({'input_gdf': {'$ref': 'zones'}, 'band': 1, 'all_touched': False, 'field_prefix': 'zonal_'})
    if name == 'raster_mosaic':
        examples = {'input_rasters': [{'$ref': 'source'}, {'$ref': 'other'}],
                    'target_crs': 'EPSG:4326', 'resolution': [0.01, 0.01]}
    if name == 'raster_reclassify':
        examples.update({'rules': [{'min': 0, 'max': 10, 'class': 1}, {'value': 10, 'class': 2}],
                         'band': 1, 'unmatched': 'nodata'})
    if name == 'raster_footprint':
        examples.update({'validity': 'any'})
    if name == 'raster_polygonize':
        examples.update({'band': 1, 'connectivity': 4})
    if name == 'raster_interpolate_idw':
        examples = {'input_gdf': {'$ref': 'points'}, 'reference_raster': {'$ref': 'reference'}, 'field': 'value', 'radius_metres': 100., 'min_points': 1, 'power': 2}
    if name == 'raster_line_density':
        examples = {'input_gdf': {'$ref': 'lines'}, 'reference_raster': {'$ref': 'reference'}, 'radius_metres': 100., 'method': 'uniform'}
    if name == 'raster_point_density':
        examples = {'input_gdf': {'$ref': 'points'}, 'reference_raster': {'$ref': 'reference'}, 'radius_metres': 100., 'method': 'uniform'}
    if name == 'vector_rasterize':
        examples = {'input_gdf': {'$ref': 'polygons'}, 'reference_raster': {'$ref': 'reference'},
                    'field': 'class_value', 'all_touched': False}
    if name == 'raster_aggregate':
        examples.update({'factors': [2, 2], 'method': 'mean'})
    if name == 'raster_tile':
        examples.update({'tile_size': [1024, 1024]})
    if name in ('raster_fill_depressions', 'raster_flow_direction_d8', 'raster_slope', 'raster_aspect', 'raster_hillshade', 'raster_contour', 'raster_curvature', 'raster_tpi', 'raster_tri', 'raster_roughness', 'raster_surface_area'):
        examples.update({'elevation_unit': 'metre', 'band': 1})
        if name == 'raster_contour':
            examples.update({'interval': 10.})
        if name == 'raster_curvature':
            examples.update({'method': 'mean'})
    if name == 'raster_viewshed':
        examples.update(band=1, elevation_unit='metre', observer_xy=[500001., 2999999.],
                        observer_height_m=1.7, target_height_m=2., radius_metres=100.)
    if name == 'raster_line_of_sight':
        examples.update(band=1, elevation_unit='metre', observer_xy=[500001., 2999999.],
                        target_xy=[500005., 2999999.], observer_height_m=1.7, target_height_m=2.)
    if name == 'raster_save_tiles':
        examples = {'input_tiles': {'$ref': 'tiles'}, 'access_plan': {
            'schema_version': 'addp.workflow.access-plan/v1', 'target': {
                'kind': 'directory', 'format': 'tiff', 'name': 'scene_tiles', 'write_mode': 'create',
                'access': {'method': 'mounted_path', 'path': '/mnt/scene_tiles'}}}}
    metadata = OperatorMetadata(name=name, type=OperatorType.SPATIAL, category=category,
        description=label, brief_description=label, overview=label + '，复用受控访问计划与当前执行内栅格对象。',
        params=params, output_ports=[OutputPort(name='default', type=output, description=label + '结果')],
        execution_modes=['direct'] if name == 'raster_inspect' else (['workflow', 'direct'] if name == 'raster_to_cog' else ['workflow']), effects=effects,
        use_cases=['遥感影像处理', '业务栅格成果生成'], notes=['CRS 不隐式推断', '栅格对象仅在当前工作流内流转'],
        workflow_example={'operator': name, 'params': examples}, attributes={'resource_groups': ['raster']})
    key, descriptor = register_operator(metadata, function)
    OPERATORS[key] = descriptor
