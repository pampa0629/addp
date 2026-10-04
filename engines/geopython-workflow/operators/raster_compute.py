"""GDAL raster operators. Pixel data and paths stay inside one execution workspace."""
from __future__ import annotations

import ast
from contextlib import contextmanager
from contextvars import ContextVar
from dataclasses import dataclass
import json
import math
import shutil
from pathlib import Path
import tempfile
import uuid

import numpy as np
from osgeo import gdal, ogr, osr
from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate as validate_layout
from addp_common.workflow_access import (
    require_access_plan, require_source_plan, require_target_plan,
    stage_source_file, publish_target_file,
)
from .base import OperatorMetadata, OperatorParam, OperatorType, OperatorCategory, OutputPort, register_operator

gdal.UseExceptions()
_WORKSPACE = ContextVar('raster_workspace', default=None)
RESAMPLING = ['nearest', 'bilinear', 'cubic', 'cubicspline', 'lanczos', 'average', 'mode']


@contextmanager
def raster_workspace():
    with tempfile.TemporaryDirectory(prefix='addp-raster-') as directory:
        token = _WORKSPACE.set(Path(directory))
        try:
            yield
        finally:
            _WORKSPACE.reset(token)


def _path(suffix='.tif'):
    root = _WORKSPACE.get()
    if root is None:
        raise ValueError('Raster operators require an execution workspace')
    return root / (uuid.uuid4().hex + suffix)


@dataclass(frozen=True)
class RasterDataset:
    path: Path
    workspace: Path


def _raster(path):
    return RasterDataset(Path(path), _WORKSPACE.get())


def _open(raster):
    if not isinstance(raster, RasterDataset) or raster.workspace != _WORKSPACE.get() or raster.workspace is None:
        raise ValueError('input_raster must come from a raster port in this execution')
    dataset = gdal.OpenEx(str(raster.path), gdal.OF_RASTER, allowed_drivers=['GTiff', 'PNG', 'JPEG'])
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
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or value <= 0:
        raise ValueError(f'{name} must be positive and finite')
    if integer and (not isinstance(value, int) or value > 2**31 - 1):
        raise ValueError(f'{name} must be a positive integer')
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
    path = _path()
    result = gdal.Warp(str(path), datasets, options=gdal.WarpOptions(
        format='GTiff', creationOptions=['TILED=YES', 'COMPRESS=DEFLATE'], **kwargs))
    if result is None:
        raise ValueError('GDAL raster warp failed')
    result.FlushCache()
    result = None
    return _raster(path)


def raster_load(access_plan, source_crs=''):
    plan = require_source_plan({'access_plan': access_plan})
    path = stage_source_file(plan, _path().parent / uuid.uuid4().hex)
    raster = _raster(path)
    dataset = _open(raster)
    drivers = {'tiff':'GTiff', 'png':'PNG', 'jpeg':'JPEG'}
    if drivers.get(plan['source']['format']) != dataset.GetDriver().ShortName:
        raise ValueError('Source format must match a supported raster driver')
    if any(Path(file).resolve() != path.resolve() for file in dataset.GetFileList() or []):
        raise ValueError('Raster input must be self-contained; external sidecars are not supported')
    # Snapshot the selected resource so later writes cannot change an upstream input.
    snapshot = _path(path.suffix)
    shutil.copyfile(path, snapshot)
    dataset = None
    raster = _raster(snapshot)
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
                  width=0, height=0, resampling=None, assign_srs=None, callback=None, num_threads='2', gdal_api=gdal):
    """One GDAL COG encoder shared by workflow persistence and directory mosaics."""
    if compression not in ['DEFLATE', 'LZW', 'ZSTD', 'NONE']:
        raise ValueError('Unsupported COG compression')
    if isinstance(blocksize, bool) or not isinstance(blocksize, int) or blocksize < 128 or blocksize > 4096 or blocksize % 16:
        raise ValueError('blocksize must be a multiple of 16 between 128 and 4096')
    _algorithm(overview_resampling)
    result = gdal_api.Translate(str(target), source, options=gdal_api.TranslateOptions(
        format='COG', width=width, height=height, resampleAlg=resampling, outputSRS=assign_srs,
        callback=callback, creationOptions=[f'COMPRESS={compression}', f'BLOCKSIZE={blocksize}',
        f'OVERVIEW_RESAMPLING={overview_resampling.upper()}', f'NUM_THREADS={num_threads}']))
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


def raster_save(input_raster, access_plan, profile='geotiff', compression='DEFLATE', blocksize=512, overview_resampling='nearest'):
    plan = require_target_plan({'access_plan': access_plan})
    if plan['target']['format'] != 'tiff' or not plan['target']['name'].lower().endswith(('.tif', '.tiff')):
        raise ValueError('Raster target must be a TIFF file')
    dataset = _open(input_raster)
    if profile == 'cog':
        output = _cog(dataset, compression, blocksize, overview_resampling)
    elif profile == 'geotiff':
        if compression not in ['DEFLATE', 'LZW', 'ZSTD', 'NONE']:
            raise ValueError('Unsupported TIFF compression')
        output = _translated(dataset, format='GTiff', creationOptions=['TILED=YES', f'COMPRESS={compression}', 'COPY_SRC_OVERVIEWS=YES'])
    else:
        raise ValueError('profile must be geotiff or cog')
    facts = raster_info(output)
    size = output.path.stat().st_size
    published = publish_target_file(output.path, plan)
    # Location and credentials are caller-owned. Return output facts only.
    return {'artifact_type': 'raster', 'format': 'tiff', 'profile': profile,
            'size_bytes': size, 'uploaded_files': published['uploaded_files'], **facts}


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
        return _translated(dataset, format='GTiff', width=width, height=height, resampleAlg=_algorithm(resampling))
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


def raster_clip(input_raster, boundary_crs, bbox=None, geometry=None):
    if (bbox is None) == (geometry is None):
        raise ValueError('Specify exactly one of bbox or geometry')
    crs = _crs(boundary_crs)
    dataset = _open(input_raster)
    _require_georeferencing(dataset)
    if bbox is not None:
        if not isinstance(bbox, list) or len(bbox) != 4 or any(isinstance(v, bool) or not isinstance(v, (int, float)) or not math.isfinite(v) for v in bbox) or bbox[0] >= bbox[2] or bbox[1] >= bbox[3]:
            raise ValueError('bbox must be finite [xmin, ymin, xmax, ymax]')
        transformed = osr.CoordinateTransformation(crs, _crs(dataset.GetProjection())).TransformBounds(*bbox, 21)
        extent = raster_info(input_raster)['extent']
        bounds = [max(transformed[0], extent[0]), max(transformed[1], extent[1]), min(transformed[2], extent[2]), min(transformed[3], extent[3])]
        if bounds[0] >= bounds[2] or bounds[1] >= bounds[3]:
            raise ValueError('Clip boundary does not intersect raster')
        _require_clip_intersection(dataset, _polygon([
            (bounds[0],bounds[1]), (bounds[2],bounds[1]),
            (bounds[2],bounds[3]), (bounds[0],bounds[3]),
        ]))
        return _warped([dataset], outputBounds=bounds)
    if not isinstance(geometry, dict) or geometry.get('type') not in ['Polygon', 'MultiPolygon']:
        raise ValueError('geometry must be a GeoJSON Polygon or MultiPolygon')
    shape = ogr.CreateGeometryFromJson(json.dumps(geometry))
    if shape is None or shape.IsEmpty() or not shape.IsValid():
        raise ValueError('Invalid clip geometry')
    shape.AssignSpatialReference(crs)
    if shape.TransformTo(_crs(dataset.GetProjection())) != 0:
        raise ValueError('Clip geometry cannot be transformed to raster CRS')
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


def raster_mosaic(input_raster, other_raster, target_crs, resolution, overlap='last', resampling='nearest'):
    if overlap not in ['first', 'last']:
        raise ValueError('overlap must be first or last')
    datasets = [_open(input_raster), _open(other_raster)]
    if datasets[0].RasterCount != datasets[1].RasterCount:
        raise ValueError('Mosaic inputs must have the same band count')
    for dataset in datasets:
        for index in range(1, dataset.RasterCount + 1):
            _band(dataset, index)
    if overlap == 'first':
        datasets.reverse()
    xres, yres = _pair(resolution, 'resolution')
    return _warped(datasets, dstSRS=_crs(target_crs).ExportToWkt(), xRes=xres, yRes=yres,
                   targetAlignedPixels=True, resampleAlg=_algorithm(resampling), outputType=gdal.GDT_Float64, dstNodata=float('nan'))


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


def _blocks(dataset, indices):
    bands = [_band(dataset, index) for index in indices]
    alpha = None
    for index in range(1,dataset.RasterCount+1):
        candidate = dataset.GetRasterBand(index)
        if candidate.GetColorInterpretation() == gdal.GCI_AlphaBand:
            alpha = candidate
            break
    for y in range(0, dataset.RasterYSize, 512):
        for x in range(0, dataset.RasterXSize, 512):
            width, height = min(512, dataset.RasterXSize - x), min(512, dataset.RasterYSize - y)
            coverage = None
            if alpha is not None:
                values = _read_values(alpha,x,y,width,height)
                coverage = np.isfinite(values) & (values > 0)
            arrays, masks = [], []
            for band in bands:
                array = _read_values(band,x,y,width,height)
                mask = (np.frombuffer(band.GetMaskBand().ReadRaster(x, y, width, height, buf_type=gdal.GDT_Byte), dtype=np.uint8).reshape(height, width) != 0) & np.isfinite(array)
                if coverage is not None and band.GetColorInterpretation() != gdal.GCI_AlphaBand:
                    mask &= coverage
                nodata = band.GetNoDataValue()
                if nodata is not None:
                    mask &= array != nodata
                arrays.append(array)
                masks.append(mask)
            yield x, y, arrays, masks


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


def _param(name, kind, description, default=None, required=True, role='param', enum=None):
    return OperatorParam(name=name, data_type=kind, type=role, description=description,
                         required=required, default=default, enum=enum)


_INPUT = lambda: _param('input_raster', 'raster', '当前工作流的栅格输入端口', role='input')
_ALGORITHM = lambda: _param('resampling', 'str', '重采样算法', 'nearest', False, enum=RESAMPLING)
_COG_OPTIONS = [
    _param('compression', 'str', '压缩算法', 'DEFLATE', False, enum=['DEFLATE', 'LZW', 'ZSTD', 'NONE']),
    _param('blocksize', 'int', 'COG 块大小', 512, False),
    _param('overview_resampling', 'str', 'COG 金字塔重采样算法', 'nearest', False, enum=RESAMPLING),
]
_SPECS = [
    (raster_load, '栅格加载', [_param('access_plan', 'object', '源访问计划'), _param('source_crs', 'str', '缺失时补充源 CRS', '', False)], 'raster', ['read']),
    (raster_save, '栅格保存', [_INPUT(), _param('access_plan', 'object', '目标访问计划'), _param('profile', 'str', '目标布局', 'geotiff', False, enum=['geotiff', 'cog']), *_COG_OPTIONS], 'object', ['write']),
    (raster_info, '栅格信息', [_INPUT()], 'object', ['read']),
    (validate_cog, 'COG 合规校验', [_INPUT()], 'object', ['read']),
    (raster_to_cog, '栅格转 COG', [_param('access_plan', 'object', '源和目标访问计划'), _param('options', 'object', 'COG 转换选项', {}, False)], 'object', ['read', 'write']),
    (raster_build_overviews, '栅格金字塔', [_INPUT(), _param('levels', 'list[int]', '递增金字塔倍率', [2, 4, 8, 16], False), _ALGORITHM()], 'raster', ['read']),
    (raster_reproject, '栅格重投影', [_INPUT(), _param('target_crs', 'str', '目标 CRS'), _param('resolution', 'list[float]', '目标分辨率', None, False), _ALGORITHM()], 'raster', ['read']),
    (raster_resample, '栅格重采样', [_INPUT(), _param('size', 'list[int]', '输出宽高', None, False), _param('resolution', 'list[float]', '输出分辨率', None, False), _ALGORITHM()], 'raster', ['read']),
    (raster_clip, '栅格裁剪', [_INPUT(), _param('boundary_crs', 'str', '边界 CRS'), _param('bbox', 'list[float]', '裁剪边界框', None, False), _param('geometry', 'object', 'GeoJSON 面边界', None, False)], 'raster', ['read']),
    (raster_mosaic, '栅格计算镶嵌', [_INPUT(), _param('other_raster', 'raster', '第二个栅格输入端口', role='input'), _param('target_crs', 'str', '目标 CRS'), _param('resolution', 'list[float]', '目标分辨率'), _param('overlap', 'str', '重叠策略', 'last', False, enum=['first', 'last']), _ALGORITHM()], 'raster', ['read']),
    (raster_band_math, '栅格波段计算', [_INPUT(), _param('expression', 'str', '受限表达式，例如 (b2-b1)/(b2+b1)')], 'raster', ['read']),
    (raster_statistics, '栅格统计', [_INPUT(), _param('band', 'int', '从 1 开始的波段序号', 1, False)], 'object', ['read']),
    (raster_histogram, '栅格直方图', [_INPUT(), _param('band', 'int', '从 1 开始的波段序号', 1, False), _param('bins', 'int', '分桶数量', 256, False), _param('value_range', 'list[float]', '统计值域', None, False)], 'object', ['read']),
]
OPERATORS = {}
for function, label, params, output, effects in _SPECS:
    name = function.__name__
    examples = {'input_raster': {'$ref': 'source'}}
    if name in ('raster_load', 'raster_to_cog'):
        examples = {'access_plan': {'schema_version': 'addp.workflow.access-plan/v1', 'source': {'kind':'file','format':'tiff','access':{'method':'mounted_path','path':'/mnt/input.tif'}}}}
    if name in ('raster_save', 'raster_to_cog'):
        examples['access_plan'] = {**examples.get('access_plan', {}), 'schema_version':'addp.workflow.access-plan/v1', 'target':{'kind':'file','format':'tiff','name':'output.tif','write_mode':'create','access':{'method':'mounted_path','path':'/mnt/output.tif'}}}
    examples.update({'raster_reproject':{'target_crs':'EPSG:3857'}, 'raster_resample':{'size':[512,512]}, 'raster_clip':{'boundary_crs':'EPSG:4326','bbox':[110,20,111,21]}, 'raster_band_math':{'expression':'(b2-b1)/(b2+b1)'}, 'raster_mosaic':{'other_raster':{'$ref':'other'},'target_crs':'EPSG:4326','resolution':[0.01,0.01]}}.get(name, {}))
    metadata = OperatorMetadata(name=name, type=OperatorType.SPATIAL, category=OperatorCategory.RASTER,
        description=label, brief_description=label, overview=label + '，复用受控访问计划与当前执行内栅格对象。',
        params=params, output_ports=[OutputPort(name='default', type=output, description=label + '结果')],
        execution_modes=['workflow', 'direct'] if name == 'raster_to_cog' else ['workflow'], effects=effects,
        use_cases=['遥感影像处理', '业务栅格成果生成'], notes=['CRS 不隐式推断', '栅格对象仅在当前工作流内流转'],
        workflow_example={'operator': name, 'params': examples})
    key, descriptor = register_operator(metadata, function)
    OPERATORS[key] = descriptor
