"""GDAL raster operators. Pixel data and paths stay inside one execution workspace."""
from __future__ import annotations

import ast
import heapq
from collections import deque
from contextlib import contextmanager, nullcontext
from contextvars import ContextVar
from dataclasses import dataclass
from fractions import Fraction
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
from shapely import from_wkb, get_coordinates, has_m, has_z, make_valid
from shapely.geometry import LineString, MultiPolygon
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


def _raster(path, native_driver=''):
    return RasterDataset(Path(path), _WORKSPACE.get(), native_driver)


def _open(raster):
    if not isinstance(raster, RasterDataset) or raster.workspace != _WORKSPACE.get() or raster.workspace is None:
        raise ValueError('input_raster must come from a raster port in this execution')
    drivers = [raster.native_driver] if raster.native_driver else ['GTiff', 'PNG', 'JPEG']
    options = ['GEOREF_SOURCES=INTERNAL', 'OPEN_REMOTE_GML=NO', 'STRICT=YES', '1BIT_ALPHA_PROMOTION=NO'] if raster.native_driver == 'JP2OpenJPEG' else []
    dataset = gdal.OpenEx(str(raster.path), gdal.OF_RASTER, allowed_drivers=drivers, open_options=options)
    if dataset is None or dataset.RasterCount < 1:
        raise ValueError('Raster dataset has no bands')
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
    native_driver = {'img': 'HFA', 'jpeg2000': 'JP2OpenJPEG'}.get(source_format, '')
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
    raster = _raster(path, native_driver)
    try:
        dataset = _open(raster)
    except RuntimeError as exc:
        raise ValueError('Source format must match a supported raster driver') from exc
    expected_driver = native_driver or {'tiff':'GTiff', 'png':'PNG', 'jpeg':'JPEG'}.get(source_format)
    if expected_driver != dataset.GetDriver().ShortName:
        raise ValueError('Source format must match a supported raster driver')
    if native_driver == 'GSAG':
        validate_surfer_ascii_pixels(dataset, path, _DISK_LANGUAGE.get())
    if native_driver in ('GSBG', 'GSAG'):
        validate_surfer6_mask(dataset, surfer_facts['blank_value'], _DISK_LANGUAGE.get())
    if source_format in ('img', 'jpeg2000', 'grd'):
        bands = [dataset.GetRasterBand(index) for index in range(1, dataset.RasterCount + 1)]
        if any(gdal.DataTypeIsComplex(band.DataType) for band in bands):
            raise ValueError(f'Complex {native_driver} bands require an explicit real-valued conversion before loading')
        nodata = [band.GetNoDataValue() for band in bands]
        nodata = ['nan' if value is not None and math.isnan(value) else value for value in nodata]
        if len({band.DataType for band in bands}) != 1 or any(value != nodata[0] for value in nodata):
            raise ValueError(f'{native_driver} bands must share one data type and NoData value for lossless TIFF conversion')
    if any(Path(file).resolve() != path.resolve() for file in dataset.GetFileList() or []):
        raise ValueError('Raster input must be self-contained; external sidecars are not supported')
    # Snapshot the selected resource so later writes cannot change an upstream input.
    snapshot = path if native_driver else _path(path.suffix)
    if snapshot != path:
        shutil.copyfile(path, snapshot)
    dataset = None
    raster = _raster(snapshot, native_driver)
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
    if source_format not in ('img', 'jpeg2000', 'grd'):
        raise ValueError('raster_inspect requires IMG, JPEG2000 or Surfer GRD format')
    with raster_workspace():
        raster = raster_load(plan)
        facts = raster_info(raster)
        driver = raster.native_driver
        profile_facts = (inspect_surfer(raster.path, _DISK_LANGUAGE.get()) if source_format == 'grd' else
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
                      'height': facts['height'], 'size_bytes': raster.path.stat().st_size},
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


def raster_zonal_statistics(input_raster, zones_raster, band, zones_band):
    dataset, zones = _open(input_raster), _open(zones_raster)
    _require_same_grid(dataset, zones)
    for source, index in ((dataset, band), (zones, zones_band)):
        if _band(source, index).GetColorInterpretation() == gdal.GCI_AlphaBand:
            raise ValueError('Zonal statistics require data bands, not alpha')
        for i in range(1, source.RasterCount + 1):
            if source.GetRasterBand(i).GetColorInterpretation() == gdal.GCI_AlphaBand:
                _band(source, i)
    statistics = {}
    for x, y, arrays, masks in _blocks(zones, [zones_band], native=True):
        classes = _classification_values(arrays[0], masks[0])
        height, width = arrays[0].shape
        _, _, values, valid = next(_blocks(dataset, [band], window=(x, y, width, height)))
        selected_values, selected_valid = values[0][masks[0]], valid[0][masks[0]]
        # Sort once per block; never scan the full block once per classification.
        order = np.argsort(classes, kind='stable')
        codes, starts, counts = np.unique(classes[order], return_index=True, return_counts=True)
        for code, start, count in zip(codes, starts, counts):
            group = order[start:start + count]
            statistics.setdefault(int(code), _ZoneStatistics()).add(
                selected_values[group][selected_valid[group]], int(count))
    columns = ['zone_value', 'total_count', 'valid_count', 'invalid_count', 'sum', 'min', 'max', 'mean', 'stddev']
    result = pd.DataFrame([[code, *statistics[code].summary()] for code in sorted(statistics)], columns=columns)
    return result.astype({name: 'int64' if i < 4 else 'Float64' for i, name in enumerate(columns)})


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


def raster_vector_zonal_statistics(input_raster, input_gdf, band, all_touched=False, field_prefix='zonal_'):
    geometries = _polygon_geometries(input_gdf)
    if type(all_touched) is not bool:
        raise ValueError('all_touched must be a boolean')
    if not isinstance(field_prefix, str):
        raise ValueError('field_prefix must be a string')
    names = [field_prefix+name for name in ('total_count', 'valid_count', 'invalid_count',
                                          'sum', 'min', 'max', 'mean', 'stddev')]
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
    rows = []
    vector = layer = feature = coverage = None
    try:
        for geometry in geometries:
            statistics = _ZoneStatistics()
            window = _zone_window(dataset, geometry.bounds)
            if window is not None:
                vector = ogr.GetDriverByName('Memory').CreateDataSource('')
                layer = vector.CreateLayer('zone', _crs(dataset.GetProjection()), ogr.wkbUnknown)
                feature = ogr.Feature(layer.GetLayerDefn())
                feature.SetGeometry(ogr.CreateGeometryFromWkb(geometry.wkb))
                if layer.CreateFeature(feature) != 0:
                    raise ValueError('Cannot create zone feature')
                feature = None
                for x, y, arrays, masks in _blocks(dataset, [band], window=window):
                    height, width = arrays[0].shape
                    coverage = gdal.GetDriverByName('MEM').Create('', width, height, 1, gdal.GDT_Byte)
                    origin = gdal.ApplyGeoTransform(transform, x, y)
                    coverage.SetGeoTransform((origin[0], transform[1], transform[2],
                                              origin[1], transform[4], transform[5]))
                    coverage.SetProjection(dataset.GetProjection())
                    coverage.GetRasterBand(1).Fill(0)
                    layer.ResetReading()
                    options = ['ALL_TOUCHED='+('TRUE' if all_touched else 'FALSE')]
                    if gdal.RasterizeLayer(coverage, [1], layer, burn_values=[1], options=options) != 0:
                        raise ValueError('Zone coverage rasterization failed')
                    covered = coverage.GetRasterBand(1).ReadAsArray() != 0
                    statistics.add(arrays[0][covered & masks[0]], int(covered.sum()))
                    coverage = None
                layer = vector = None
            rows.append(statistics.summary())
    finally:
        feature = layer = vector = coverage = dataset = None
    result = input_gdf.copy()
    for index, name in enumerate(names):
        result[name] = pd.array([row[index] for row in rows], dtype='int64' if index < 3 else 'Float64')
    return result


def raster_statistics(input_raster, band=1):
    dataset = _open(input_raster)
    _band(dataset, band)
    count, mean, m2 = 0, 0.0, 0.0
    minimum, maximum = math.inf, -math.inf
    for _, _, arrays, masks in _blocks(dataset, [band]):
        values = arrays[0][masks[0]]
        n = values.size
        if not n:
            continue
        local_mean = float(values.mean())
        delta = local_mean - mean
        m2 += float(((values - local_mean) ** 2).sum()) + delta * delta * count * n / (count + n)
        mean += delta * n / (count + n)
        count += int(n)
        minimum, maximum = min(minimum, float(values.min())), max(maximum, float(values.max()))
    total = dataset.RasterXSize * dataset.RasterYSize
    return {'band': band, 'strategy': 'full', 'valid_count': count, 'invalid_count': total - count,
            'nodata_ratio': (total - count) / total, 'min': minimum if count else None,
            'max': maximum if count else None, 'mean': mean if count else None,
            'stddev': math.sqrt(m2 / count) if count else None}


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


def raster_focal_statistics(input_raster, band, window_size, method):
    """Reduce valid pixel neighbourhoods using bounded source reads with halos."""
    if isinstance(window_size, bool) or not isinstance(window_size, int) or window_size not in (3, 5):
        raise ValueError('window_size must be the integer 3 or 5')
    if method not in ('sum', 'mean', 'min', 'max', 'stddev', 'mode'):
        raise ValueError('method must be sum, mean, min, max, stddev or mode')
    dataset = _open(input_raster)
    if _band(dataset, band).GetColorInterpretation() == gdal.GCI_AlphaBand:
        raise ValueError('Focal statistics require a data band, not alpha')
    for index in range(1, dataset.RasterCount + 1):
        if dataset.GetRasterBand(index).GetColorInterpretation() == gdal.GCI_AlphaBand:
            _band(dataset, index)
    output = _numeric_output(dataset, 1)
    destination = output.GetRasterBand(1)
    radius = window_size // 2
    core_size = 512 - 2 * radius
    for y in range(0, dataset.RasterYSize, core_size):
        for x in range(0, dataset.RasterXSize, core_size):
            width = min(core_size, dataset.RasterXSize - x)
            height = min(core_size, dataset.RasterYSize - y)
            left, top = max(0, x - radius), max(0, y - radius)
            right = min(dataset.RasterXSize, x + width + radius)
            bottom = min(dataset.RasterYSize, y + height + radius)
            _, _, arrays, masks = next(_blocks(dataset, [band], window=(left, top, right-left, bottom-top),
                                               native=method == 'mode'))
            if method == 'mode':
                _safe_classification_values(arrays[0], masks[0])
                arrays[0] = arrays[0].astype(np.float64)
            # Padding supplies only invalid placeholders, never contributing zeros.
            values = np.pad(np.where(masks[0], arrays[0], 0.), radius)
            valid = np.pad(masks[0], radius, constant_values=False)
            cx, cy = x-left+radius, y-top+radius
            center = values[cy:cy+height, cx:cx+width]
            center_valid = valid[cy:cy+height, cx:cx+width]

            def neighbours():
                for dy in range(-radius, radius+1):
                    for dx in range(-radius, radius+1):
                        yield (values[cy+dy:cy+dy+height, cx+dx:cx+dx+width],
                               valid[cy+dy:cy+dy+height, cx+dx:cx+dx+width])

            if method == 'mode':
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
            elif method in ('min', 'max'):
                result = np.full(center.shape, np.inf if method == 'min' else -np.inf)
                reduce = np.minimum if method == 'min' else np.maximum
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
                    if method == 'stddev':
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
                if method == 'stddev':
                    mean_delta = total/divisor
                    variance = np.zeros(center.shape)
                    for sample, mask in neighbours():
                        residual = normalized(sample, mask) - mean_delta
                        variance += np.where(mask, residual*residual, 0.)
                    result = np.sqrt(np.clip(variance/divisor, 0., 1.))*scale
                else:
                    with np.errstate(over='ignore', invalid='ignore'):
                        result = (np.clip(total/divisor, -1., 1.) if method == 'mean' else total)*scale
            result[~center_valid | ~np.isfinite(result)] = np.nan
            destination.WriteRaster(x, y, width, height, np.ascontiguousarray(result).tobytes(),
                                    buf_type=gdal.GDT_Float64)
    destination = None
    output.FlushCache()
    path = output.GetDescription()
    output = None
    return _raster(path)



_BOUNDARY_MESSAGES = {
    'band': {'zh-cn': '边界清理必须显式选择有效的一基数据波段',
             'en': 'Boundary cleaning requires a valid explicit one-based data band'},
    'complex': {'zh-cn': '边界清理不支持复数数据或复数 alpha 波段',
                'en': 'Boundary cleaning does not support complex data or complex alpha bands'},
    'alpha': {'zh-cn': '边界清理必须选择分类数据波段，不能选择 alpha',
              'en': 'Boundary cleaning requires a classification data band, not alpha'},
    'classes': {'zh-cn': '边界清理的有效分类值必须为 ±(2^53−1) 内的精确整数',
                'en': 'Boundary cleaning classification values must be exact safe integers within ±(2^53−1)'},
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


def raster_boundary_clean(input_raster, band):
    """Frozen global class priorities, followed by one synchronous expand/shrink."""
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
            destination.WriteArray(result, x, y)
        destination = None
        expanded.FlushCache()
        expanded = None
        expanded = gdal.Open(str(expanded_path))
        output = _numeric_output(dataset, 1)
        output_path = Path(output.GetDescription())
        destination = output.GetRasterBand(1)
        for x, y, values in _cell_neighbourhood_blocks(expanded, 1):
            height, width = values.shape[0]-2, values.shape[1]-2
            _, _, arrays, masks = next(_blocks(dataset, [band], window=(x, y, width, height)))
            original = arrays[0]
            restore = np.zeros(original.shape, dtype=bool)
            for sample in _boundary_neighbours(values):
                restore |= sample == original
            result = np.where(restore, original, values[1:-1, 1:-1])
            result[~masks[0]] = np.nan
            destination.WriteArray(result, x, y)
        destination = None
        output.FlushCache()
        output = None
        result = _raster(output_path)
        completed = True
        return result
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


def _squared_distance_line(cost, *, valid=None, return_sites=False):
    """Exact integer-cost envelope; ties belong to the lower source index.

    Raster squared pixel distances are integers.  Intersections are represented
    by the first integer coordinate where the later site strictly wins, using
    Python integer arithmetic rather than rounded Float64 intersections.
    """
    sites = np.flatnonzero(np.isfinite(cost) if valid is None else valid)
    if not len(sites):
        distances = np.full(cost.shape, np.inf)
        return (distances, np.full(cost.shape, -1, dtype=np.int64)) if return_sites else distances
    if any(not np.isfinite(cost[site]) or int(cost[site]) != cost[site] for site in sites):
        raise ValueError('Squared raster distance costs must be finite integers')
    vertices = np.empty(len(sites), dtype=np.int64)
    boundaries = [None] * (len(sites) + 1)
    vertices[0] = sites[0]
    top = 0
    for site in sites[1:]:
        while True:
            previous = vertices[top]
            numerator = (int(cost[site]) - int(cost[previous])
                         + (int(site) - int(previous)) * (int(site) + int(previous)))
            crossing = numerator // (2 * (int(site) - int(previous))) + 1
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
def _nearest_pixel_sites(dataset, band, target_values, *, non_targets=False):
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
                    distances, source_rows = _squared_distance_line(costs, valid=present, return_sites=True)
                    columns.seek(column * height * 8)
                    columns.write(distances.tobytes())
                    identities = np.empty((height, 2), dtype=np.int64)
                    identities[:, 0] = source_rows
                    identities[:, 1] = source_columns[source_rows]
                    nearest.write(identities.tobytes())
            yield columns_path, nearest_sites
        finally:
            with _DISK_LOCK:
                for path in (rows_path, columns_path, row_sites, column_sites, nearest_sites):
                    path.unlink(missing_ok=True)


def raster_proximity(input_raster, band, target_values):
    dataset = _open(input_raster)
    horizontal_metres = _projected_north_up_grid(dataset)
    width, height, transform = _grid(dataset)
    if transform[1] != -transform[5]:
        raise ValueError('Proximity requires square pixels; resample explicitly')
    step_metres = transform[1] * horizontal_metres
    if not math.isfinite(step_metres) or step_metres <= 0:
        raise ValueError('Pixel distance must be a finite positive metre value')
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
        try:
            with _nearest_pixel_sites(dataset, band, target_values) as (columns_path, _):
                output = _numeric_output(dataset, 1)
                destination = output.GetRasterBand(1)
                destination.SetUnitType('metre')
                with columns_path.open('rb') as columns:
                    for x, y, arrays, masks in _blocks(dataset, [band]):
                        rows, count = arrays[0].shape
                        distances = np.empty((rows, count), dtype=np.float64)
                        for column in range(count):
                            columns.seek(((x + column) * height + y) * 8)
                            distances[:, column] = _read_distance_values(columns, rows)
                        with np.errstate(over='ignore', under='ignore', invalid='ignore'):
                            distances = np.sqrt(distances) * step_metres
                        if np.any(masks[0] & ~np.isfinite(distances)):
                            raise ValueError('Proximity exceeds finite Float64 metre distances')
                        distances[~masks[0]] = np.nan
                        destination.WriteArray(distances, x, y)
            path = output.GetDescription()
            destination = None
            output.FlushCache()
            output = None
            return _raster(path)
        finally:
            destination = output = None


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


def _dem_admission(input_raster, elevation_unit, band, minimum_size, *, diagnostic=None):
    # One DEM admission path; preserve native values for consumers needing exact comparisons.
    def invalid(code, message):
        return diagnostic(code) if diagnostic is not None else ValueError(message)
    dataset = _open(input_raster)
    try:
        horizontal_metres = _projected_north_up_grid(dataset)
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


def raster_flow_length_d8(input_raster, band):
    """Measure projected downstream distance in metres to the supplied open outlet."""
    source, directions, factor = _d8_direction_source(input_raster, band, _d8_length_error)
    transform = source.GetGeoTransform()
    values = _d8_flow_length(directions, transform[1] * factor, -transform[5] * factor)
    return _d8_output(source, values, 'metre')


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


def raster_flow_length_upstream_d8(input_raster, band):
    """Measure the longest projected upstream route in metres to each cell."""
    source, directions, factor = _d8_direction_source(input_raster, band, _d8_upstream_length_error)
    transform = source.GetGeoTransform()
    values = _d8_upstream_flow_length(directions, transform[1] * factor, -transform[5] * factor)
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


def _tri_root(squared):
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
        raise _tri_error('numeric') from cause
    if not math.isfinite(result) or result == 0:
        raise _tri_error('numeric')
    return result


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
    (raster_band_math, OperatorCategory.RASTER_CELL, '栅格波段计算', [_INPUT(), _param('expression', 'str', '受限表达式，例如 (b2-b1)/(b2+b1)')], 'raster', ['read']),
    (raster_reclassify, OperatorCategory.RASTER_CELL, '栅格重分类', [_INPUT(), _param('rules', 'list[dict]', '互不重叠的精确值或左闭右开区间分类规则，null 端点表示无界'), _param('band', 'int', '从 1 开始的数据波段序号', 1, False), _param('unmatched', 'str', '未命中规则的有效像元处理方式', 'nodata', False, enum=['nodata', 'keep'])], 'raster', ['read']),
    (raster_proximity, OperatorCategory.RASTER_CELL, '栅格邻近距离', [_INPUT(), _param('band', 'int', '显式选择不超过 32 位的整数数据波段'), _param('target_values', 'list[int]', '有效目标分类值集合，允许合法零，不能重复')], 'raster', ['read']),
    (raster_nibble, OperatorCategory.RASTER_CELL, '分类栅格蚕食', [_INPUT(), _param('band', 'int', '显式选择精确安全整数分类数据波段，不能选择 alpha'), _param('target_values', 'list[int]', '待替换的安全整数分类，非空且不重复；取最近原始有效非目标供体，等距按源行列')], 'raster', ['read']),
    (raster_boundary_clean, OperatorCategory.RASTER_CELL, '分类栅格边界清理', [_INPUT(), _param('band', 'int', '显式分类数据波段；原类别有效总数优先，八邻域单轮扩张和收缩，保留无效覆盖')], 'raster', ['read']),
    (raster_thin, OperatorCategory.RASTER_CELL, '分类栅格细化', [_INPUT(), _param('band', 'int', '显式分类数据波段，不能选择 alpha'), _param('foreground_value', 'float', '单个精确安全整数前景分类；Guo–Hall 八邻域收敛，输出骨架1／背景0／原无效NaN')], 'raster', ['read']),
    (raster_aggregate, OperatorCategory.RASTER_CELL, '栅格聚合', [_INPUT(), _param('factors', 'list[int]', '相邻像元的列、行聚合倍数，例如 [2,2]'), _param('method', 'str', '聚合方法；忽略无效像元并保留边缘组', enum=['sum', 'mean', 'min', 'max'])], 'raster', ['read']),
    (raster_footprint, OperatorCategory.RASTER_EXTENT, '栅格有效像元轮廓', [_INPUT(), _param('bands', 'list[int]', '选择互不重复的一基数据波段，省略时为所有数据波段', None, False), _param('validity', 'str', '有效范围并集或交集', 'any', False, enum=['any', 'all'])], 'GeoDataFrame', ['read']),
    (raster_polygonize, OperatorCategory.RASTER_EXTENT, '分类栅格转面', [_INPUT(), _param('band', 'int', '从 1 开始的整数分类数据波段'), _param('connectivity', 'int', '同分类像元的邻接方式，仅接受整数 4 或 8', 4, False)], 'GeoDataFrame', ['read']),
    (vector_rasterize, OperatorCategory.RASTER_EXTENT, '矢量栅格化', [_param('input_gdf', 'GeoDataFrame', '有效二维面输入，后输入要素覆盖前者', role='input'), _param('reference_raster', 'raster', '仅提供同 CRS 的完整网格，不继承像元值或有效性', role='input'), _param('field', 'str', '数值属性字段，与常量二选一', None, False), _param('burn_value', 'float', '有限数值常量，与属性字段二选一', None, False), _param('all_touched', 'bool', '是否烧写所有触及像元；默认使用 GDAL 像元中心语义', False, False)], 'raster', ['read']),
    (raster_statistics, OperatorCategory.RASTER_STATISTICS, '栅格统计', [_INPUT(), _param('band', 'int', '从 1 开始的波段序号', 1, False)], 'object', ['read']),
    (raster_cut_fill, OperatorCategory.RASTER_STATISTICS, '双DEM填挖方统计', [_INPUT(), _param('after_raster', 'raster', '施工后DEM，须与施工前同CRS、尺寸和完整网格，由用户保证相同高程基准', role='input'), _param('band', 'int', '显式施工前高程数据波段'), _param('after_band', 'int', '显式施工后高程数据波段'), _param('elevation_unit', 'str', '显式施工前高程单位', enum=list(_ELEVATION_METRES)), _param('after_elevation_unit', 'str', '显式施工后高程单位；挖填体积各非负，净增加为填方减挖方，统一立方米', enum=list(_ELEVATION_METRES))], 'DataFrame', ['read']),
    (raster_zonal_statistics, OperatorCategory.RASTER_STATISTICS, '栅格分区统计', [_INPUT(), _param('zones_raster', 'raster', '整数分类分区输入，须与数值栅格具有完全相同的网格', role='input'), _param('band', 'int', '显式选择从 1 开始的数值数据波段'), _param('zones_band', 'int', '显式选择从 1 开始的整数分类数据波段')], 'DataFrame', ['read']),
    (raster_focal_statistics, OperatorCategory.RASTER_STATISTICS, '栅格邻域统计', [_INPUT(), _param('band', 'int', '显式选择从 1 开始的数据波段，不能选择 alpha'), _param('window_size', 'int', '以像元为单位的正方形窗口边长：3 或 5'), _param('method', 'str', 'sum/mean/min/max/stddev（总体标准差）/mode（分类众数；并列优先中心原分类，否则较小分类）')], 'raster', ['read']),
    (raster_vector_zonal_statistics, OperatorCategory.RASTER_STATISTICS, '矢量分区统计', [_INPUT(), _param('input_gdf', 'GeoDataFrame', '同 CRS 有效二维面，各面独立统计，重叠分别计入', role='input'), _param('band', 'int', '显式选择从 1 开始的数据波段，不能选择 alpha'), _param('all_touched', 'bool', '默认按像元中心覆盖，可选全部触及像元', False, False), _param('field_prefix', 'str', '新增统计列前缀，同名冲突报错，可为空', 'zonal_', False)], 'GeoDataFrame', ['read']),
    (raster_histogram, OperatorCategory.RASTER_STATISTICS, '栅格直方图', [_INPUT(), _param('band', 'int', '从 1 开始的波段序号', 1, False), _param('bins', 'int', '分桶数量', 256, False), _param('value_range', 'list[float]', '统计值域', None, False)], 'object', ['read']),
    (raster_fill_depressions, OperatorCategory.RASTER_CELL, 'DEM 填洼', [_INPUT(), _param('band', 'int', '显式高程数据波段；八邻接，图边及无效区边界开放'), _param('elevation_unit', 'str', '明确原高程单位，输出米制最低溢流高程', enum=list(_ELEVATION_METRES))], 'raster', ['read']),
    (raster_flow_direction_d8, OperatorCategory.RASTER_CELL, 'DEM D8 流向', [_INPUT(), _param('band', 'int', '显式高程数据波段；平坦区无环，封闭洼地失败'), _param('elevation_unit', 'str', '明确原高程单位，输出方向码与开放终点0', enum=list(_ELEVATION_METRES))], 'raster', ['read']),
    (raster_flow_accumulation_d8, OperatorCategory.RASTER_CELL, 'D8 汇流累积', [_INPUT(), _param('band', 'int', '显式方向数据波段；非法方向或循环失败'), _param('measure', 'str', 'count：上游有效像元数、不计自身；area：包含自身的投影网格面积、统一平方米', 'count', False, enum=['count', 'area'])], 'raster', ['read']),
    (raster_flow_accumulation_weighted_d8, OperatorCategory.RASTER_CELL, 'D8 加权汇流累积', [_INPUT(), _param('weights_raster', 'raster', '同网格、同有效覆盖的有限非负权重输入，不补零或自动对齐', role='input'), _param('band', 'int', '显式方向数据波段'), _param('weights_band', 'int', '显式权重数据波段；上游权重和、不计自身，沿用权重单位')], 'raster', ['read']),
    (raster_flow_length_d8, OperatorCategory.RASTER_CELL, 'D8 下游流长', [_INPUT(), _param('band', 'int', '显式方向数据波段；中心到中心的米制下游长度，开放终点为零，非法方向或循环失败')], 'raster', ['read']),
    (raster_flow_length_upstream_d8, OperatorCategory.RASTER_CELL, 'D8 上游最长流长', [_INPUT(), _param('band', 'int', '显式方向数据波段；米制最长上游中心间路径，汇合取最大，无入流为零，非法方向或循环失败')], 'raster', ['read']),
    (raster_drainage_basins_d8, OperatorCategory.RASTER_CELL, 'D8 汇水分区', [_INPUT(), _param('band', 'int', '显式方向数据波段；每个开放终点独立分区，按出口行列顺序从1编号，非法方向或循环失败')], 'raster', ['read']),
    (raster_snap_pour_points, OperatorCategory.RASTER_CELL, '汇水点吸附', [_INPUT(), _param('points_raster', 'raster', '同网格、同有效覆盖的唯一正安全整数标记，零为未选；保留编号，碰撞失败', role='input'), _param('band', 'int', '显式未加权累积计数数据波段'), _param('points_band', 'int', '显式汇水点标记数据波段'), _param('radius_metres', 'float', '显式有限非负米制圆半径；允许跨分水岭，最大计数后按距离／行／列决胜')], 'raster', ['read']),
    (raster_watershed_d8, OperatorCategory.RASTER_CELL, 'D8 指定汇水点汇水区', [_INPUT(), _param('points_raster', 'raster', '同网格、同有效覆盖的汇水点标记，零为未选、正安全整数编号全图唯一，不自动吸附', role='input'), _param('band', 'int', '显式方向数据波段'), _param('points_band', 'int', '显式标记数据波段；保留编号并包含点自身，嵌套汇水点失败')], 'raster', ['read']),
    (raster_subcatchments_d8, OperatorCategory.RASTER_CELL, 'D8 增量子汇水区', [_INPUT(), _param('points_raster', 'raster', '同网格、同有效覆盖的汇水点标记，零为未选、正安全整数编号全图唯一，不自动吸附', role='input'), _param('band', 'int', '显式方向数据波段'), _param('points_band', 'int', '显式标记数据波段；保留编号并包含点自身；允许嵌套，按下游第一个指定点划分互斥增量范围')], 'raster', ['read']),
    (raster_stream_order_d8, OperatorCategory.RASTER_CELL, 'D8 河流分级', [_INPUT(), _param('stream_raster', 'raster', '同网格、同有效覆盖的0／1河网掩膜输入，断流失败、不自动补线', role='input'), _param('band', 'int', '显式方向数据波段'), _param('stream_band', 'int', '显式0／1河网掩膜数据波段'), _param('method', 'str', 'strahler：同最高级汇合升级；shreve：全部实际河网入流量级求和', 'strahler', False, enum=['strahler', 'shreve'])], 'raster', ['read']),
    (raster_distance_to_stream_d8, OperatorCategory.RASTER_CELL, 'D8 到河网沿流距离', [_INPUT(), _param('stream_raster', 'raster', '同网格、同有效覆盖的0／1河网；河网自身0、未到达NoData、断流失败', role='input'), _param('band', 'int', '显式方向数据波段；完整非法路线或循环失败'), _param('stream_band', 'int', '显式河网数据波段；到首次河网中心的米制沿流距离')], 'raster', ['read']),
    (raster_hand_d8, OperatorCategory.RASTER_CELL, 'D8 到河网相对高程（HAND）', [_INPUT(), _param('stream_raster', 'raster', '同网格、同有效覆盖的0／1河网；完整非法路线及断流失败', role='input'), _param('elevation_raster', 'raster', '同网格、同有效覆盖的DEM；全图下游不升高，允许平坦、逆坡失败', role='input'), _param('band', 'int', '显式方向数据波段'), _param('stream_band', 'int', '显式二值河网数据波段'), _param('elevation_band', 'int', '显式高程数据波段；相对首次河网高程差，河网0、未到达NoData'), _param('elevation_unit', 'str', '显式高程单位，结果统一米制', enum=['metre', 'foot', 'us_survey_foot'])], 'raster', ['read']),
    (raster_stream_link_d8, OperatorCategory.RASTER_CELL, 'D8 河段编号', [_INPUT(), _param('stream_raster', 'raster', '同网格、同有效覆盖的0／1河网掩膜，断流失败、不自动补线', role='input'), _param('band', 'int', '显式方向数据波段'), _param('stream_band', 'int', '显式河网数据波段；汇合点归下游段，按河段起点行列顺序从1编号')], 'raster', ['read']),
    (raster_stream_to_vector_d8, OperatorCategory.RASTER_EXTENT, 'D8 河网转矢量线', [_INPUT(), _param('stream_raster', 'raster', '同网格、同有效覆盖的0／1河网掩膜；孤立单像元失败，不自动补线', role='input'), _param('band', 'int', '显式方向数据波段'), _param('stream_band', 'int', '显式河网数据波段；连接像元中心，保留河段ID及米制长度')], 'GeoDataFrame', ['read']),
    (raster_tpi, OperatorCategory.RASTER_CELL, 'DEM 地形位置指数', [_INPUT(), _param('band', 'int', '显式高程数据波段；固定3×3中心减周围8点等权均值'), _param('elevation_unit', 'str', '显式高程单位；输出米，完整九点有效才计算，合法零保留', enum=list(_ELEVATION_METRES))], 'raster', ['read']),
    (raster_tri, OperatorCategory.RASTER_CELL, 'DEM 地形崎岖度', [_INPUT(), _param('band', 'int', '显式高程数据波段；固定3×3 Riley中心与8邻居高差平方和开方，不除以8'), _param('elevation_unit', 'str', '显式高程单位；输出米，完整九点有效才计算，合法零保留', enum=list(_ELEVATION_METRES))], 'raster', ['read']),
    (raster_roughness, OperatorCategory.RASTER_CELL, 'DEM 地形粗糙度', [_INPUT(), _param('band', 'int', '显式高程数据波段；固定3×3包含中心的九点最大高程减最小高程，不取半高差'), _param('elevation_unit', 'str', '显式高程单位；输出米，完整九点有效才计算，合法零保留', enum=list(_ELEVATION_METRES))], 'raster', ['read']),
    (raster_curvature, OperatorCategory.RASTER_CELL, 'DEM 曲率', [_INPUT(), _param('band', 'int', '显式高程数据波段；完整有效3×3九点等权二次曲面拟合，凸正凹负'), _param('elevation_unit', 'str', '显式高程单位；输出每米（1/metre），边缘及无效邻域NoData', enum=list(_ELEVATION_METRES)), _param('method', 'str', 'mean：平均曲率；profile：剖面曲率；tangential：切向曲率；plan：平面曲率；方向方法九点等高平地0、其他零梯度NoData', 'mean', False, enum=['mean', 'profile', 'tangential', 'plan'])], 'raster', ['read']),
    (raster_slope, OperatorCategory.RASTER_CELL, 'DEM 坡度', [*_DEM_PARAMS(), _param('slope_unit', 'str', '坡度输出单位', 'degree', False, enum=['degree', 'percent'])], 'raster', ['read']),
    (raster_aspect, OperatorCategory.RASTER_CELL, 'DEM 坡向', _DEM_PARAMS(), 'raster', ['read']),
    (raster_hillshade, OperatorCategory.RASTER_CELL, 'DEM 地形晕渲', [*_DEM_PARAMS(), _param('azimuth', 'float', '光照方位角，北起顺时针 [0,360)', 315., False), _param('altitude', 'float', '光照高度角 (0,90]', 45., False)], 'raster', ['read']),
    (raster_contour, OperatorCategory.RASTER_EXTENT, 'DEM 等值线', [*_DEM_PARAMS(), _param('interval', 'float', '等高距（米），与指定高程二选一', None, False), _param('levels', 'list[float]', '严格递增的指定高程（米）', None, False)], 'GeoDataFrame', ['read']),
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
    if name == 'raster_cut_fill':
        examples.update({'after_raster': {'$ref': 'after'}, 'band': 1, 'after_band': 1, 'elevation_unit': 'metre', 'after_elevation_unit': 'metre'})
    if name == 'raster_zonal_statistics':
        examples.update({'zones_raster': {'$ref': 'zones'}, 'band': 1, 'zones_band': 1})
    if name == 'raster_proximity':
        examples.update({'band': 1, 'target_values': [0, 1]})
    if name == 'raster_thin':
        examples.update({'band': 1, 'foreground_value': 1})
    if name in ('raster_boundary_clean', 'raster_flow_accumulation_d8', 'raster_flow_length_d8', 'raster_flow_length_upstream_d8', 'raster_drainage_basins_d8'):
        examples.update({'band': 1})
    if name == 'raster_flow_accumulation_d8':
        examples.update({'measure': 'count'})
    if name in ('raster_stream_order_d8', 'raster_stream_link_d8', 'raster_stream_to_vector_d8', 'raster_distance_to_stream_d8'):
        examples.update({'stream_raster': {'$ref': 'streams'}, 'band': 1, 'stream_band': 1})
    if name == 'raster_hand_d8':
        examples.update({'stream_raster': {'$ref': 'streams'}, 'elevation_raster': {'$ref': 'elevation'}, 'band': 1, 'stream_band': 1, 'elevation_band': 1, 'elevation_unit': 'metre'})
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
    if name == 'vector_rasterize':
        examples = {'input_gdf': {'$ref': 'polygons'}, 'reference_raster': {'$ref': 'reference'},
                    'field': 'class_value', 'all_touched': False}
    if name == 'raster_aggregate':
        examples.update({'factors': [2, 2], 'method': 'mean'})
    if name == 'raster_tile':
        examples.update({'tile_size': [1024, 1024]})
    if name in ('raster_fill_depressions', 'raster_flow_direction_d8', 'raster_slope', 'raster_aspect', 'raster_hillshade', 'raster_contour', 'raster_curvature', 'raster_tpi', 'raster_tri', 'raster_roughness'):
        examples.update({'elevation_unit': 'metre', 'band': 1})
        if name == 'raster_contour':
            examples.update({'interval': 10.})
        if name == 'raster_curvature':
            examples.update({'method': 'mean'})
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
