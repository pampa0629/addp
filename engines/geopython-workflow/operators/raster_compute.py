"""GDAL raster operators. Pixel data and paths stay inside one execution workspace."""
from __future__ import annotations

import ast
from contextlib import contextmanager
from contextvars import ContextVar
from dataclasses import dataclass
import json
import math
from numbers import Integral, Real
import shutil
from pathlib import Path
import tempfile
import uuid
import xml.etree.ElementTree as ET

import numpy as np
import pandas as pd
import geopandas as gpd
from shapely import from_wkb, get_coordinates, has_m, has_z, make_valid
from shapely.geometry import MultiPolygon
from osgeo import gdal, ogr, osr
from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate as validate_layout
from addp_common.workflow_access import (
    require_access_plan, require_source_plan, require_target_plan,
    stage_source_file, publish_target_file,
)
from addp_common.workflow_runtime import WorkflowExecutionError
from addp_common.workflow_access import require_empty_target_directory, target_child_file_plan
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


def _encode_raster(input_raster, profile, compression, blocksize, overview_resampling):
    dataset = _open(input_raster)
    if profile == 'cog':
        output = _cog(dataset, compression, blocksize, overview_resampling)
    elif profile == 'geotiff':
        if compression not in ['DEFLATE', 'LZW', 'ZSTD', 'NONE']:
            raise ValueError('Unsupported TIFF compression')
        output = _translated(dataset, format='GTiff', creationOptions=['TILED=YES', f'COMPRESS={compression}', 'COPY_SRC_OVERVIEWS=YES'])
    else:
        raise ValueError('profile must be geotiff or cog')
    return output


def raster_save(input_raster, access_plan, profile='geotiff', compression='DEFLATE', blocksize=512, overview_resampling='nearest'):
    plan = require_target_plan({'access_plan': access_plan})
    if plan['target']['format'] != 'tiff' or not plan['target']['name'].lower().endswith(('.tif', '.tiff')):
        raise ValueError('Raster target must be a TIFF file')
    output = _encode_raster(input_raster, profile, compression, blocksize, overview_resampling)
    facts = raster_info(output)
    size = output.path.stat().st_size
    published = publish_target_file(output.path, plan)
    # Location and credentials are caller-owned. Return output facts only.
    return {'artifact_type': 'raster', 'format': 'tiff', 'profile': profile,
            'size_bytes': size, 'uploaded_files': published['uploaded_files'], **facts}


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
    if method not in ('sum', 'mean', 'min', 'max', 'stddev'):
        raise ValueError('method must be sum, mean, min, max or stddev')
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
            _, _, arrays, masks = next(_blocks(dataset, [band], window=(left, top, right-left, bottom-top)))
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

            if method in ('min', 'max'):
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


def _squared_distance_line(cost):
    """Lower envelope of (x-q)^2 + cost[q], excluding infinite sites."""
    sites = np.flatnonzero(np.isfinite(cost))
    if not len(sites):
        return np.full(cost.shape, np.inf)
    vertices = np.empty(len(sites), dtype=np.int64)
    boundaries = np.empty(len(sites) + 1, dtype=np.float64)
    vertices[0] = sites[0]
    boundaries[0], boundaries[1] = -np.inf, np.inf
    top = 0
    for site in sites[1:]:
        while True:
            previous = vertices[top]
            # Equivalent to subtracting parabola intercepts, without forming
            # two large squared coordinates and then cancelling them.
            crossing = ((cost[site] - cost[previous]) / (int(site) - int(previous))
                        + int(site) + int(previous)) / 2
            if top == 0 or crossing > boundaries[top]:
                break
            top -= 1
        top += 1
        vertices[top] = site
        boundaries[top], boundaries[top + 1] = crossing, np.inf
    coordinates = np.arange(len(cost), dtype=np.float64)
    owners = vertices[np.searchsorted(boundaries[1:top + 1], coordinates, side='left')]
    return np.square(coordinates - owners) + cost[owners]


def _read_distance_values(stream, count):
    values = np.frombuffer(stream.read(count * 8), dtype=np.float64)
    if values.size != count:
        raise RuntimeError('Incomplete private distance intermediate')
    return values


def _transpose_distance_file(source, target, width, height):
    """Transpose owned raw Float64 storage with one bounded tile, no mmap."""
    with source.open('rb') as reader, target.open('xb+') as writer:
        writer.truncate(width * height * 8)
        for y in range(0, height, 512):
            rows = min(512, height - y)
            for x in range(0, width, 512):
                columns = min(512, width - x)
                tile = np.empty((rows, columns), dtype=np.float64)
                for row in range(rows):
                    reader.seek(((y + row) * width + x) * 8)
                    tile[row] = _read_distance_values(reader, columns)
                for column in range(columns):
                    writer.seek(((x + column) * height + y) * 8)
                    writer.write(tile[:, column].tobytes())


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
    indices = np.arange(width, dtype=np.int64)
    rows_path, columns_path = _path('.distance'), _path('.distance')
    output = destination = None
    try:
        target_count = 0
        with rows_path.open('xb') as rows:
            for row in range(height):
                targets = np.zeros(width, dtype=bool)
                for x, _, arrays, masks in _blocks(dataset, [band], window=(0, row, width, 1)):
                    matched = masks[0][0] & np.isin(arrays[0][0], target_values)
                    targets[x:x + matched.size] = matched
                target_count += int(np.count_nonzero(targets))
                left = np.maximum.accumulate(np.where(targets, indices, -1))
                right = np.minimum.accumulate(np.where(targets, indices, width)[::-1])[::-1]
                distances = np.minimum(np.where(left >= 0, indices - left, np.inf),
                                       np.where(right < width, right - indices, np.inf))
                rows.write(np.square(distances).tobytes())
        if target_count == 0:
            raise ValueError('Proximity requires at least one valid target pixel')
        _transpose_distance_file(rows_path, columns_path, width, height)
        rows_path.unlink()
        with columns_path.open('r+b') as columns:
            for column in range(width):
                columns.seek(column * height * 8)
                distances = _squared_distance_line(_read_distance_values(columns, height))
                columns.seek(column * height * 8)
                columns.write(distances.tobytes())
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
        rows_path.unlink(missing_ok=True)
        columns_path.unlink(missing_ok=True)


def _dem_source(input_raster, elevation_unit, band, minimum_size):
    dataset = _open(input_raster)
    horizontal_metres = _projected_north_up_grid(dataset)
    if min(dataset.RasterXSize, dataset.RasterYSize) < minimum_size:
        raise ValueError(f'DEM must be at least {minimum_size} by {minimum_size} pixels')
    if not isinstance(elevation_unit, str) or elevation_unit not in _ELEVATION_METRES:
        raise ValueError('elevation_unit must explicitly be metre, foot or us_survey_foot')
    source_band = _band(dataset, band)
    if source_band.GetColorInterpretation() == gdal.GCI_AlphaBand:
        raise ValueError('DEM elevation band cannot be alpha')
    unit = source_band.GetUnitType().strip().lower()
    if unit and _ELEVATION_UNIT_NAMES.get(unit) != elevation_unit:
        raise ValueError('DEM band elevation unit is unknown or conflicts with elevation_unit')
    if source_band.GetScale() not in (None, 1) or source_band.GetOffset() not in (None, 0):
        raise ValueError('DEM encoded scale/offset must be applied before terrain analysis')
    normalized = _numeric_output(dataset, 1)
    destination = normalized.GetRasterBand(1)
    destination.SetUnitType('metre')
    valid_count = 0
    for x, y, arrays, masks in _blocks(dataset, [band]):
        with np.errstate(over='ignore', invalid='ignore'):
            values = arrays[0] * _ELEVATION_METRES[elevation_unit]
        if np.any(masks[0] & ~np.isfinite(values)):
            raise ValueError('DEM elevation unit conversion overflow')
        values[~masks[0]] = np.nan
        valid_count += int(np.count_nonzero(masks[0]))
        height, width = values.shape
        destination.WriteRaster(x, y, width, height, values.tobytes(), buf_type=gdal.GDT_Float64)
    destination = source_band = None
    normalized.FlushCache()
    return normalized, horizontal_metres, valid_count


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
_SPECS = [
    (raster_load, OperatorCategory.RASTER_IO, '栅格加载', [_param('access_plan', 'object', '源访问计划'), _param('source_crs', 'str', '缺失时补充源 CRS', '', False)], 'raster', ['read']),
    (raster_save, OperatorCategory.RASTER_IO, '栅格保存', [_INPUT(), _param('access_plan', 'object', '目标访问计划'), _param('profile', 'str', '目标布局', 'geotiff', False, enum=['geotiff', 'cog']), *_COG_OPTIONS], 'object', ['write']),
    (raster_tile, OperatorCategory.RASTER_EXTENT, '栅格切片', [_INPUT(), _param('tile_size', 'list[int]', '每片像素宽高；边缘保留实际尺寸')], 'raster_tiles', ['read']),
    (raster_save_tiles, OperatorCategory.RASTER_IO, '栅格切片批量保存', [_param('input_tiles', 'raster_tiles', '当前执行内的栅格切片集合', role='input'), _param('access_plan', 'object', '目标子目录访问计划'), _param('profile', 'str', '目标布局', 'geotiff', False, enum=['geotiff', 'cog']), *_COG_OPTIONS], 'object', ['read', 'write']),
    (raster_info, OperatorCategory.RASTER_INSPECTION, '栅格信息', [_INPUT()], 'object', ['read']),
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
    (raster_aggregate, OperatorCategory.RASTER_CELL, '栅格聚合', [_INPUT(), _param('factors', 'list[int]', '相邻像元的列、行聚合倍数，例如 [2,2]'), _param('method', 'str', '聚合方法；忽略无效像元并保留边缘组', enum=['sum', 'mean', 'min', 'max'])], 'raster', ['read']),
    (raster_footprint, OperatorCategory.RASTER_EXTENT, '栅格有效像元轮廓', [_INPUT(), _param('bands', 'list[int]', '选择互不重复的一基数据波段，省略时为所有数据波段', None, False), _param('validity', 'str', '有效范围并集或交集', 'any', False, enum=['any', 'all'])], 'GeoDataFrame', ['read']),
    (raster_polygonize, OperatorCategory.RASTER_EXTENT, '分类栅格转面', [_INPUT(), _param('band', 'int', '从 1 开始的整数分类数据波段'), _param('connectivity', 'int', '同分类像元的邻接方式，仅接受整数 4 或 8', 4, False)], 'GeoDataFrame', ['read']),
    (vector_rasterize, OperatorCategory.RASTER_EXTENT, '矢量栅格化', [_param('input_gdf', 'GeoDataFrame', '有效二维面输入，后输入要素覆盖前者', role='input'), _param('reference_raster', 'raster', '仅提供同 CRS 的完整网格，不继承像元值或有效性', role='input'), _param('field', 'str', '数值属性字段，与常量二选一', None, False), _param('burn_value', 'float', '有限数值常量，与属性字段二选一', None, False), _param('all_touched', 'bool', '是否烧写所有触及像元；默认使用 GDAL 像元中心语义', False, False)], 'raster', ['read']),
    (raster_statistics, OperatorCategory.RASTER_STATISTICS, '栅格统计', [_INPUT(), _param('band', 'int', '从 1 开始的波段序号', 1, False)], 'object', ['read']),
    (raster_zonal_statistics, OperatorCategory.RASTER_STATISTICS, '栅格分区统计', [_INPUT(), _param('zones_raster', 'raster', '整数分类分区输入，须与数值栅格具有完全相同的网格', role='input'), _param('band', 'int', '显式选择从 1 开始的数值数据波段'), _param('zones_band', 'int', '显式选择从 1 开始的整数分类数据波段')], 'DataFrame', ['read']),
    (raster_focal_statistics, OperatorCategory.RASTER_STATISTICS, '栅格邻域统计', [_INPUT(), _param('band', 'int', '显式选择从 1 开始的数据波段，不能选择 alpha'), _param('window_size', 'int', '以像元为单位的正方形窗口边长：3 或 5'), _param('method', 'str', 'sum/mean/min/max/stddev（总体标准差）')], 'raster', ['read']),
    (raster_vector_zonal_statistics, OperatorCategory.RASTER_STATISTICS, '矢量分区统计', [_INPUT(), _param('input_gdf', 'GeoDataFrame', '同 CRS 有效二维面，各面独立统计，重叠分别计入', role='input'), _param('band', 'int', '显式选择从 1 开始的数据波段，不能选择 alpha'), _param('all_touched', 'bool', '默认按像元中心覆盖，可选全部触及像元', False, False), _param('field_prefix', 'str', '新增统计列前缀，同名冲突报错，可为空', 'zonal_', False)], 'GeoDataFrame', ['read']),
    (raster_histogram, OperatorCategory.RASTER_STATISTICS, '栅格直方图', [_INPUT(), _param('band', 'int', '从 1 开始的波段序号', 1, False), _param('bins', 'int', '分桶数量', 256, False), _param('value_range', 'list[float]', '统计值域', None, False)], 'object', ['read']),
    (raster_slope, OperatorCategory.RASTER_CELL, 'DEM 坡度', [*_DEM_PARAMS(), _param('slope_unit', 'str', '坡度输出单位', 'degree', False, enum=['degree', 'percent'])], 'raster', ['read']),
    (raster_aspect, OperatorCategory.RASTER_CELL, 'DEM 坡向', _DEM_PARAMS(), 'raster', ['read']),
    (raster_hillshade, OperatorCategory.RASTER_CELL, 'DEM 地形晕渲', [*_DEM_PARAMS(), _param('azimuth', 'float', '光照方位角，北起顺时针 [0,360)', 315., False), _param('altitude', 'float', '光照高度角 (0,90]', 45., False)], 'raster', ['read']),
    (raster_contour, OperatorCategory.RASTER_EXTENT, 'DEM 等值线', [*_DEM_PARAMS(), _param('interval', 'float', '等高距（米），与指定高程二选一', None, False), _param('levels', 'list[float]', '严格递增的指定高程（米）', None, False)], 'GeoDataFrame', ['read']),
]
OPERATORS = {}
for function, category, label, params, output, effects in _SPECS:
    name = function.__name__
    examples = {'input_raster': {'$ref': 'source'}}
    if name in ('raster_load', 'raster_to_cog'):
        examples = {'access_plan': {'schema_version': 'addp.workflow.access-plan/v1', 'source': {'kind':'file','format':'tiff','access':{'method':'mounted_path','path':'/mnt/input.tif'}}}}
    if name in ('raster_save', 'raster_to_cog'):
        examples['access_plan'] = {**examples.get('access_plan', {}), 'schema_version':'addp.workflow.access-plan/v1', 'target':{'kind':'file','format':'tiff','name':'output.tif','write_mode':'create','access':{'method':'mounted_path','path':'/mnt/output.tif'}}}
    examples.update({'raster_reproject':{'target_crs':'EPSG:3857'}, 'raster_resample':{'size':[512,512]}, 'raster_clip':{'boundary_crs':'EPSG:4326','bbox':[110,20,111,21]}, 'raster_band_math':{'expression':'(b2-b1)/(b2+b1)'}}.get(name, {}))
    examples.update({'raster_align': {'reference_raster': {'$ref': 'reference'}},
                     'raster_stack': {'other_raster': {'$ref': 'other'}},
                     'raster_update': {'update_raster': {'$ref': 'update'}},
                     'raster_select_bands': {'bands': [3, 2, 1], 'color_model': 'rgb'}}.get(name, {}))
    if name == 'raster_zonal_statistics':
        examples.update({'zones_raster': {'$ref': 'zones'}, 'band': 1, 'zones_band': 1})
    if name == 'raster_proximity':
        examples.update({'band': 1, 'target_values': [0, 1]})
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
    if name in ('raster_slope', 'raster_aspect', 'raster_hillshade', 'raster_contour'):
        examples.update({'elevation_unit': 'metre', 'band': 1})
        if name == 'raster_contour':
            examples.update({'interval': 10.})
    if name == 'raster_save_tiles':
        examples = {'input_tiles': {'$ref': 'tiles'}, 'access_plan': {
            'schema_version': 'addp.workflow.access-plan/v1', 'target': {
                'kind': 'directory', 'format': 'tiff', 'name': 'scene_tiles', 'write_mode': 'create',
                'access': {'method': 'mounted_path', 'path': '/mnt/scene_tiles'}}}}
    metadata = OperatorMetadata(name=name, type=OperatorType.SPATIAL, category=category,
        description=label, brief_description=label, overview=label + '，复用受控访问计划与当前执行内栅格对象。',
        params=params, output_ports=[OutputPort(name='default', type=output, description=label + '结果')],
        execution_modes=['workflow', 'direct'] if name == 'raster_to_cog' else ['workflow'], effects=effects,
        use_cases=['遥感影像处理', '业务栅格成果生成'], notes=['CRS 不隐式推断', '栅格对象仅在当前工作流内流转'],
        workflow_example={'operator': name, 'params': examples}, attributes={'resource_groups': ['raster']})
    key, descriptor = register_operator(metadata, function)
    OPERATORS[key] = descriptor
