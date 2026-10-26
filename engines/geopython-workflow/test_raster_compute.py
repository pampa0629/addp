import json
import math
import statistics
import hashlib
import multiprocessing
import os
from pathlib import Path
import platform
import tempfile
import time
import threading
from concurrent.futures import ThreadPoolExecutor
import numpy as np
import pytest
from osgeo import gdal, ogr, osr
from operators import OPERATORS, list_operators
from operators.raster_compute import (
    raster_workspace, raster_load, raster_info, raster_save, raster_to_cog,
    raster_resample, raster_reproject, raster_clip, raster_mosaic,
    raster_band_math, raster_statistics, raster_histogram, raster_build_overviews, validate_cog,
    raster_align, raster_stack, raster_select_bands, raster_update,
    _classification_rules, raster_reclassify, raster_aggregate, raster_footprint,
    raster_tile, raster_save_tiles, raster_tiles_info,
    raster_slope, raster_aspect, raster_hillshade, raster_contour, vector_rasterize,
    raster_focal_statistics, raster_vector_zonal_statistics, raster_proximity, raster_error_language,
)
from workflow_engine import execute_workflow
from operators.raster_operators import build_raster_mosaic
from operators.raster_compute import raster_inspect, _require_self_contained_hfa



@pytest.mark.parametrize('codec,epsg,geoboxes', [
    ('JP2',4326,['GeoJP2=YES','GMLJP2=NO']),
    ('JP2',32650,['GeoJP2=NO','GMLJP2=YES']),
    ('JP2',None,['GeoJP2=NO','GMLJP2=NO']),
    ('J2K',None,[]),
])
@pytest.mark.parametrize('lossless', [True, False])
def test_jpeg2000_native_inspection_and_cog_preserve_decoded_pixels(tmp_path, codec, epsg, geoboxes, lossless):
    path = tmp_path / ('scene.jp2' if codec == 'JP2' else 'scene.j2k')
    source = gdal.GetDriverByName('MEM').Create('', 64, 32, 3, gdal.GDT_Byte)
    grid = (113.,.001,0.,28.,0.,-.001) if epsg == 4326 else (500000.,10.,0.,3100000.,0.,-10.)
    if epsg:
        crs = osr.SpatialReference(); crs.ImportFromEPSG(epsg)
        source.SetSpatialRef(crs); source.SetGeoTransform(grid)
    expected = np.arange(64*32,dtype=np.uint8).reshape(32,64)
    for i in range(1,4): source.GetRasterBand(i).WriteArray(expected + i)
    encoded = gdal.GetDriverByName('JP2OpenJPEG').CreateCopy(str(path),source,options=[
        'CODEC='+codec, 'REVERSIBLE='+('YES' if lossless else 'NO'),
        'QUALITY='+('100' if lossless else '35'), 'RESOLUTIONS=2', *geoboxes])
    encoded = None; source = None
    before = hashlib.sha256(path.read_bytes()).hexdigest()
    decoded = gdal.OpenEx(str(path),gdal.OF_RASTER,allowed_drivers=['JP2OpenJPEG'],open_options=['GEOREF_SOURCES=INTERNAL'])
    native_pixels = decoded.ReadAsArray(); decoded = None
    # Even explicitly conflicting external positioning must not override the internal source.
    Path(str(path)+'.aux.xml').write_text('<PAMDataset><SRS>EPSG:3857</SRS><GeoTransform>1,1,0,2,0,-1</GeoTransform></PAMDataset>')
    path.with_suffix('.j2w').write_text('1\n0\n0\n-1\n100\n100\n')
    facts = raster_inspect(source_plan(path,'jpeg2000'))
    assert facts['format'] == 'jpeg2000'
    description = facts['description']
    assert description['media']['encoding'] == 'JP2OpenJPEG'
    assert description['media']['size_bytes'] == path.stat().st_size
    assert description['format_info']['profile'] == codec.lower()
    assert description['format_info']['scan_complete'] is True
    assert description['format_info']['band_count'] == 3
    if epsg:
        assert description['spatial']['srid'] == epsg
    else:
        assert description['spatial'] is None
    target = tmp_path/'result.cog.tif'
    plan = {**source_plan(path,'jpeg2000'),'target':target_plan(target)['target']}
    raster_to_cog(plan)
    output = gdal.Open(str(target))
    np.testing.assert_array_equal(output.ReadAsArray(),native_pixels)
    if lossless:
        for i in range(3): np.testing.assert_array_equal(native_pixels[i],expected+i+1)
    if epsg:
        assert output.GetSpatialRef().GetAuthorityCode(None)==str(epsg)
        assert output.GetGeoTransform() == pytest.approx(grid)
    else: assert not output.GetProjection() and output.GetGeoTransform(can_return_null=True) is None
    output = None
    assert hashlib.sha256(path.read_bytes()).hexdigest()==before


@pytest.mark.parametrize('mutation', ['wrong-magic','bad-length','multi-codestream','external-fragments','jpx'])
def test_jpeg2000_framing_rejected_before_native_open(tmp_path, monkeypatch, mutation):
    import struct
    signature = b'\x00\x00\x00\x0cjP  \x0d\x0a\x87\x0a'
    def box(kind,payload): return struct.pack('>I4s',len(payload)+8,kind)+payload
    ftyp = box(b'ftyp',b'jp2 '+bytes(4)+b'jp2 ')
    stream = box(b'jp2c',b'\xff\x4f\xff\x51')
    raw = signature+ftyp+stream
    if mutation=='wrong-magic': raw=b'not a JPEG2000 file'
    elif mutation=='bad-length': raw=signature+struct.pack('>I4s',0xfffffffe,b'ftyp')
    elif mutation=='multi-codestream': raw+=stream
    elif mutation=='external-fragments': raw+=box(b'ftbl',b'')
    elif mutation=='jpx': raw=signature+box(b'ftyp',b'jpx '+bytes(4)+b'jpx ')+stream
    path=tmp_path/'invalid.jp2';path.write_bytes(raw)
    monkeypatch.setattr(gdal,'OpenEx',lambda *a,**kw:pytest.fail('native decoder opened invalid composition'))
    with pytest.raises(ValueError): raster_inspect(source_plan(path,'jpeg2000'))


def test_jpeg2000_disguised_tiff_and_truncated_codestream_rejected(tmp_path):
    fake=create_raster(tmp_path/'fake.jp2',np.ones((2,2)))
    with pytest.raises(ValueError,match='JPEG2000'): raster_inspect(source_plan(fake,'jpeg2000'))
    path=tmp_path/'truncated.j2k';path.write_bytes(b'\xff\x4f\xff\x51'+bytes(64))
    with pytest.raises((ValueError,RuntimeError)): raster_inspect(source_plan(path,'jpeg2000'))


@pytest.mark.parametrize('compressed', [False, True])
@pytest.mark.parametrize('epsg', [4326, 32650, None])
def test_img_real_hfa_inspect_and_cog_pixels(tmp_path, compressed, epsg):
    path = tmp_path / 'scene.img'
    dataset = gdal.GetDriverByName('HFA').Create(str(path), 32, 16, 3, gdal.GDT_UInt16,
                                               options=['COMPRESSED=YES' if compressed else 'COMPRESSED=NO'])
    transform = (110, .01, 0, 30, 0, -.01) if epsg == 4326 else (500000, 10, 0, 3000000, 0, -10)
    if epsg:
        crs = osr.SpatialReference()
        crs.ImportFromEPSG(epsg)
        dataset.SetProjection(crs.ExportToWkt())
        dataset.SetGeoTransform(transform)
    expected = np.arange(32*16, dtype=np.uint16).reshape(16, 32)
    for band in range(1, 4):
        dataset.GetRasterBand(band).WriteArray(expected + band)
        dataset.GetRasterBand(band).SetNoDataValue(65535)
    dataset = None
    before = hashlib.sha256(path.read_bytes()).hexdigest()
    result = raster_inspect(source_plan(path, 'img'))
    description = result['description']
    assert result['format'] == 'img'
    assert description['media']['width'] == 32
    assert description['media']['height'] == 16
    assert description['media']['encoding'] == 'HFA'
    assert description['format_info']['band_count'] == 3
    assert description['format_info']['scan_complete'] is True
    assert description['format_info']['bands'][0]['nodata'] == 65535
    if epsg:
        assert description['spatial']['srid'] == epsg
        assert description['spatial']['extent'] == pytest.approx([
            transform[0], transform[3] + 16*transform[5], transform[0] + 32*transform[1], transform[3]])
    else:
        assert not description['spatial'] or not description['spatial'].get('srid')
    target = tmp_path / 'scene.cog.tif'
    plan = {**source_plan(path, 'img'), 'target': target_plan(target)['target']}
    if not epsg:
        raster_to_cog(plan)
        output = gdal.Open(str(target))
        assert not output.GetProjection()
        output = None
    else:
        artifact = raster_to_cog(plan)
        assert artifact['size_bytes'] == target.stat().st_size
        output = gdal.Open(str(target))
        assert output.GetGeoTransform() == pytest.approx(transform)
        assert output.GetSpatialRef().GetAuthorityCode(None) == str(epsg)
        for band in range(1, 4):
            np.testing.assert_array_equal(output.GetRasterBand(band).ReadAsArray(), expected + band)
            assert output.GetRasterBand(band).GetNoDataValue() == 65535
        output = None
        with raster_workspace():
            assert validate_cog(raster_load(source_plan(target)))['valid']
    assert hashlib.sha256(path.read_bytes()).hexdigest() == before


@pytest.mark.parametrize('option', ['USE_SPILL=YES', 'DEPENDENT_FILE=outside.img'])
def test_img_dependency_rejected_before_native_open(tmp_path, monkeypatch, option):
    path = tmp_path / 'dependent.img'
    dataset = gdal.GetDriverByName('HFA').Create(str(path), 8, 8, 1, gdal.GDT_Byte, options=[option])
    dataset.GetRasterBand(1).Fill(42)
    dataset = None
    monkeypatch.setattr(gdal, 'OpenEx', lambda *a, **kw: pytest.fail('native decoder reached before dependency rejection'))
    with pytest.raises(ValueError, match='external/dependent'):
        raster_inspect(source_plan(path, 'img'))


@pytest.mark.parametrize('payload', [b'not HFA at all but long enough', b'EHFA_HEADER_TAG\0' + bytes(32)])
def test_img_invalid_header_is_rejected(tmp_path, payload):
    path = tmp_path / 'fake.img'
    path.write_bytes(payload)
    with pytest.raises(ValueError):
        raster_inspect(source_plan(path, 'img'))


def test_img_tiff_renamed_and_wrong_declared_format_are_rejected(tmp_path):
    tiff = create_raster(tmp_path / 'fake.img', np.ones((2, 2)))
    with pytest.raises(ValueError, match='HFA'):
        raster_inspect(source_plan(tiff, 'img'))
    real = tmp_path / 'real.img'
    dataset = gdal.GetDriverByName('HFA').Create(str(real), 8, 8, 1, gdal.GDT_Byte)
    dataset = None
    with raster_workspace(), pytest.raises(ValueError, match='format must match'):
        raster_load(source_plan(real, 'tiff'))


def test_img_entry_cycle_rejected_with_bounded_preflight(tmp_path):
    import struct
    path = tmp_path / 'cycle.img'
    dataset = gdal.GetDriverByName('HFA').Create(str(path), 8, 8, 1, gdal.GDT_Byte)
    dataset = None
    data = bytearray(path.read_bytes())
    header = struct.unpack_from('<I', data, 16)[0]
    root = struct.unpack_from('<I', data, header + 8)[0]
    struct.pack_into('<I', data, root, root)
    path.write_bytes(data)
    with pytest.raises(ValueError, match='entry tree'):
        _require_self_contained_hfa(path)


def test_img_complex_pixels_do_not_silently_discard_imaginary_component(tmp_path):
    path = tmp_path / 'complex.img'
    dataset = gdal.GetDriverByName('HFA').Create(str(path), 8, 8, 1, gdal.GDT_CFloat32)
    dataset.GetRasterBand(1).Fill(3, 4)
    dataset = None
    with pytest.raises(ValueError, match='Complex HFA'):
        raster_inspect(source_plan(path, 'img'))


def test_img_mixed_nodata_is_not_silently_replaced_by_one_tiff_tag(tmp_path):
    path = tmp_path / 'mixed-nodata.img'
    dataset = gdal.GetDriverByName('HFA').Create(str(path), 8, 8, 2, gdal.GDT_UInt16)
    dataset.GetRasterBand(1).Fill(7)
    dataset.GetRasterBand(1).SetNoDataValue(0)
    dataset.GetRasterBand(2).Fill(9)
    dataset.GetRasterBand(2).SetNoDataValue(1)
    dataset = None
    target = tmp_path / 'must-not-publish.tif'
    plan = {**source_plan(path, 'img'), 'target': target_plan(target)['target']}
    with pytest.raises(ValueError, match='share one data type and NoData'):
        raster_to_cog(plan)
    assert not target.exists()



def source_plan(path, format='tiff'):
    return {'schema_version': 'addp.workflow.access-plan/v1', 'source': {
        'kind': 'file', 'format': format, 'access': {'method': 'mounted_path', 'path': str(path)}}}


@pytest.mark.parametrize('overlap', ['first', 'last'])
@pytest.mark.parametrize('order', [[0, 1, 2], [2, 0, 1]])
def test_ordered_multi_raster_mosaic(tmp_path, overlap, order):
    arrays = [np.full((2, 4), value, dtype=float) for value in [10, 20, 30]]
    arrays[0][0, 0] = 0
    arrays[1][0, 1] = -9999
    arrays[2][0, 2] = -9999
    for array in arrays:
        array[1, 3] = -9999
    paths = [create_raster(tmp_path/f'source-{i}.tif', data, transform=(0,1,0,2,0,-1)) for i,data in enumerate(arrays)]
    hashes = [hashlib.sha256(path.read_bytes()).hexdigest() for path in paths]
    expected = np.full((2,4), np.nan)
    priority = order[::-1] if overlap == 'first' else order
    for index in priority:
        valid = arrays[index] != -9999
        expected[valid] = arrays[index][valid]
    with raster_workspace():
        inputs = [raster_load(source_plan(paths[index])) for index in order]
        result = raster_mosaic(inputs, 'EPSG:4326', [1,1], overlap)
        output = gdal.Open(str(result.path))
        np.testing.assert_allclose(read_band_values(output.GetRasterBand(1)), expected, equal_nan=True)
        assert output.GetGeoTransform() == (0,1,0,2,0,-1)
        output = None
        target = tmp_path/'multi.cog.tif'
        artifact = raster_save(result, target_plan(target), profile='cog')
        assert artifact['size_bytes'] == target.stat().st_size > 0
        assert validate_cog(raster_load(source_plan(target)))['valid']
    assert [hashlib.sha256(path.read_bytes()).hexdigest() for path in paths] == hashes


@pytest.mark.parametrize('value', [None, {}, [], [None], [None, {}], 'file.tif'])
def test_multi_raster_mosaic_rejects_invalid_collection(value):
    with raster_workspace(), pytest.raises((TypeError, ValueError)):
        raster_mosaic(value, 'EPSG:4326', [1,1])


def test_multi_raster_mosaic_metadata_and_legacy_rejection(tmp_path):
    meta = next(item for item in list_operators() if item['id'] == 'raster_mosaic')
    inputs = [param for param in meta['parameters'] if param['param_type'] == 'input']
    assert len(inputs) == 1
    assert (inputs[0]['name'], inputs[0]['type'], inputs[0]['item_type']) == ('input_rasters', 'array', 'raster')
    path = create_raster(tmp_path/'source.tif', np.ones((2,2)))
    other = create_raster(tmp_path/'other.tif', np.ones((2,2,2)))
    with raster_workspace():
        raster = raster_load(source_plan(path))
        with pytest.raises(ValueError, match='at least two'):
            raster_mosaic([raster], 'EPSG:4326', [1,1])
        with pytest.raises(TypeError, match='input_raster'):
            raster_mosaic(input_raster=raster, other_raster=raster, target_crs='EPSG:4326', resolution=[1,1])
        with pytest.raises(ValueError, match='band count'):
            raster_mosaic([raster, raster, raster_load(source_plan(other))], 'EPSG:4326', [1,1])


@pytest.mark.parametrize('overlap', ['first', 'last'])
@pytest.mark.parametrize('order', [[0,1,2], [2,0,1]])
def test_multi_raster_mosaic_independent_bands_masks_alpha_and_union(tmp_path, overlap, order):
    sources, values, validity = [], [], []
    for index in range(3):
        data = np.arange(12, dtype=float).reshape(2,2,3) + index*20
        data[0,0,2] = -9999
        data[1,1,1] = -9999
        alpha = np.full((2,3),255.)
        alpha[0,1] = 0
        mask = np.full((2,3),255, dtype=np.uint8)
        if index == 1:
            mask[1,0] = 0
        path = create_raster(tmp_path/f'bands-{index}.tif', np.concatenate([data,alpha[None,...]]),
                             transform=(index*2,1,0,2,0,-1))
        dataset = gdal.Open(str(path),gdal.GA_Update)
        dataset.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):
            dataset.CreateMaskBand(gdal.GMF_PER_DATASET)
        dataset.GetRasterBand(1).GetMaskBand().WriteRaster(0,0,3,2,mask.tobytes())
        dataset = None
        sources.append(path)
        values.append(data)
        validity.append((data!=-9999)&(alpha[None,...]>0)&(mask[None,...]>0))
    originals = [path.read_bytes() for path in sources]
    expected = np.full((2,2,7),np.nan)
    for index in (order[::-1] if overlap=='first' else order):
        region = expected[:,:,index*2:index*2+3]
        region[validity[index]] = values[index][validity[index]]
    with raster_workspace():
        inputs = [raster_load(source_plan(sources[index])) for index in order]
        result = raster_mosaic(inputs,'EPSG:4326',[1,1],overlap)
        workspace = result.workspace
        target = tmp_path/'result.cog.tif'
        artifact = raster_save(result,target_plan(target),profile='cog')
        assert artifact['size_bytes'] == target.stat().st_size > 0
        assert validate_cog(raster_load(source_plan(target)))['valid']
        output = gdal.Open(str(target))
        assert (output.RasterXSize,output.RasterYSize,output.RasterCount)==(7,2,3)
        for band in (1,2):
            np.testing.assert_allclose(read_band_values(output.GetRasterBand(band)),expected[band-1],equal_nan=True)
        output = None
    assert not workspace.exists()
    assert [path.read_bytes() for path in sources] == originals


def test_multi_raster_mosaic_workflow_ref_order(tmp_path):
    paths = [create_raster(tmp_path/f'source-{index}.tif',np.full((2,2),index*10.)) for index in (1,2,3)]
    tasks = [{'id': f'load{index}', 'operator': 'raster_load', 'depends_on': [],
              'params': {'access_plan': source_plan(path)}} for index,path in enumerate(paths,1)]
    target = tmp_path/'workflow.cog.tif'
    tasks.extend([
        {'id': 'mosaic', 'operator': 'raster_mosaic', 'depends_on': ['load1','load2','load3'], 'params': {
            'input_rasters': [{'$ref': f'load{index}', 'port': 'default'} for index in (3,1,2)],
            'target_crs': 'EPSG:4326', 'resolution': [1,1], 'overlap': 'last'}},
        {'id': 'save', 'operator': 'raster_save', 'depends_on': ['mosaic'], 'params': {
            'input_raster': {'$ref': 'mosaic'}, 'access_plan': target_plan(target), 'profile': 'cog'}}])
    result = execute_workflow({'tasks': tasks})
    assert result['status']=='success', result
    assert json.loads(result['final_result'])['size_bytes']==target.stat().st_size>0
    output = gdal.Open(str(target))
    np.testing.assert_allclose(read_band_values(output.GetRasterBand(1)),np.full((2,2),20.))
    output = None


def target_plan(path, mode='create'):
    return {'schema_version': 'addp.workflow.access-plan/v1', 'target': {
        'kind': 'file', 'format': 'tiff', 'name': path.name, 'write_mode': mode,
        'access': {'method': 'mounted_path', 'path': str(path)}}}


def read_band_values(band):
    return np.frombuffer(band.ReadRaster(buf_type=gdal.GDT_Float64),
                         dtype=np.float64).reshape(band.YSize,band.XSize)


def create_raster(path, array, transform=(0, 1, 0, 4, 0, -1), crs='EPSG:4326', nodata=-9999):
    if array.ndim == 2:
        array = array[None, ...]
    ds = gdal.GetDriverByName('GTiff').Create(str(path), array.shape[2], array.shape[1], array.shape[0], gdal.GDT_Float64)
    if transform is not None:
        ds.SetGeoTransform(transform)
    if crs:
        definition = osr.SpatialReference(); definition.SetFromUserInput(crs)
        ds.SetProjection(definition.ExportToWkt())
    for index, values in enumerate(array, 1):
        band = ds.GetRasterBand(index); band.SetNoDataValue(nodata); band.WriteRaster(0, 0, values.shape[1], values.shape[0], values.astype(np.float64).tobytes(), buf_type=gdal.GDT_Float64)
    ds = None
    return path


@pytest.mark.parametrize('east,north', [(1., 0.), (0., 1.), (-1., 0.), (0., -1.), (2., 3.), (0., 0.)])
def test_dem_terrain_plane_independent_values_grid_edges_and_cog(tmp_path, east, north):
    rows, columns = np.indices((9, 11))
    elevations = 100 + east * (columns + .5) * 2 - north * (rows + .5) * 3
    path = create_raster(tmp_path/'dem.tif', elevations, (600000,2,0,4000000,0,-3), 'EPSG:32617')
    before = hashlib.sha256(path.read_bytes()).hexdigest()
    magnitude = math.hypot(east, north)
    expected_slope = math.degrees(math.atan(magnitude))
    expected_aspect = math.degrees(math.atan2(-east, -north)) % 360 if magnitude else math.nan
    azimuth, altitude = 315., 45.
    light = (math.sin(math.radians(azimuth)) * math.cos(math.radians(altitude)),
             math.cos(math.radians(azimuth)) * math.cos(math.radians(altitude)), math.sin(math.radians(altitude)))
    illumination = max(0., (-east*light[0] - north*light[1] + light[2]) / math.sqrt(1+magnitude*magnitude))
    expected_hillshade = math.floor(1 + 254*illumination + .5)
    with raster_workspace():
        source = raster_load(source_plan(path))
        for name, result, expected, tolerance in (
            ('slope', raster_slope(source, 'metre'), expected_slope, 1e-5),
            ('percent', raster_slope(source, 'metre', slope_unit='percent'), magnitude*100, 1e-4),
            ('aspect', raster_aspect(source, 'metre'), expected_aspect, 1e-5),
            ('shade', raster_hillshade(source, 'metre'), expected_hillshade, 0)):
            dataset = gdal.Open(str(result.path))
            actual = read_band_values(dataset.GetRasterBand(1))
            assert dataset.GetGeoTransform() == (600000,2,0,4000000,0,-3)
            assert dataset.GetSpatialRef().GetAuthorityCode(None) == '32617'
            assert dataset.GetRasterBand(1).DataType == gdal.GDT_Float64
            assert np.isnan(actual[[0,-1],:]).all() and np.isnan(actual[:,[0,-1]]).all()
            np.testing.assert_allclose(actual[1:-1,1:-1], expected, atol=tolerance, rtol=0, equal_nan=True)
            dataset = None
            target = tmp_path/(name+'.cog.tif')
            saved = raster_save(result, target_plan(target), profile='cog', blocksize=128)
            assert type(saved['size_bytes']) is int and saved['size_bytes'] == target.stat().st_size > 0
            assert validate_cog(raster_load(source_plan(target)))['valid']
    assert hashlib.sha256(path.read_bytes()).hexdigest() == before


@pytest.mark.parametrize('unit,factor', [('metre',1), ('foot',.3048), ('us_survey_foot',1200/3937)])
@pytest.mark.parametrize('crs', ['EPSG:32617', 'EPSG:2277'])
def test_dem_terrain_units_are_explicit_and_horizontal_units_come_from_crs(tmp_path, unit, factor, crs):
    reference = osr.SpatialReference()
    assert reference.SetFromUserInput(crs) == 0
    assert reference.IsProjected() and reference.GetAuthorityCode(None) == crs.split(':')[1]
    horizontal = reference.GetLinearUnits()
    rows, columns = np.indices((5,7))
    values = (100 + columns*horizontal*2 + rows*horizontal*3) / factor
    path = create_raster(tmp_path/'unit.tif', values, (10,2,0,30,0,-3), crs)
    with raster_workspace():
        raster = raster_load(source_plan(path))
        result = raster_slope(raster, unit)
        dataset = gdal.Open(str(result.path))
        # GDAL's Horn implementation reads elevations as Float32 internally.
        # The absolute tolerance covers that quantization after unit conversion.
        np.testing.assert_allclose(read_band_values(dataset.GetRasterBand(1))[1:-1,1:-1], math.degrees(math.atan(math.sqrt(2))), atol=3e-4, rtol=0)
        dataset = None


@pytest.mark.parametrize('operation', [raster_slope, raster_aspect, raster_hillshade])
@pytest.mark.parametrize('fault', ['nodata', 'mask', 'alpha', 'nan', 'inf', 'all-invalid'])
def test_dem_terrain_validity_requires_all_nine_samples_and_keeps_partial_alpha(tmp_path, operation, fault):
    rows, columns = np.indices((7,7)); values = columns.astype(float) + rows*2
    alpha = np.full((7,7),128.)
    if fault == 'nodata': values[3,3] = -9999
    if fault == 'nan': values[3,3] = math.nan
    if fault == 'inf': values[3,3] = math.inf
    if fault == 'alpha': alpha[3,3] = 0
    if fault == 'all-invalid': values[:] = -9999
    path = create_raster(tmp_path/'holes.tif', np.stack([values,alpha]), (0,1,0,7,0,-1), 'EPSG:3857')
    ds = gdal.Open(str(path), gdal.GA_Update); ds.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand)
    if fault == 'mask':
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'): ds.CreateMaskBand(gdal.GMF_PER_DATASET)
        mask = np.full((7,7),255,np.uint8);mask[3,3]=0
        ds.GetRasterBand(1).GetMaskBand().WriteRaster(0,0,7,7,mask.tobytes())
    ds = None
    with raster_workspace():
        result = operation(raster_load(source_plan(path)), 'metre')
        dataset = gdal.Open(str(result.path)); actual = read_band_values(dataset.GetRasterBand(1))
        valid = np.zeros((7,7), bool);valid[1:-1,1:-1] = True;valid[2:5,2:5] = False
        if fault == 'all-invalid': valid[:] = False
        np.testing.assert_array_equal(np.isfinite(actual), valid)
        dataset = None


@pytest.mark.parametrize('fault', ['geographic','rotated','south-up','missing-crs','missing-grid','unit','unit-conflict','unit-unknown','scale','offset','alpha','band','small'])
def test_dem_terrain_rejects_ambiguous_input_before_gdal_computation(tmp_path, fault, monkeypatch):
    transform = (0,1,0,5,0,-1)
    crs = 'EPSG:4326' if fault=='geographic' else '' if fault=='missing-crs' else 'EPSG:3857'
    if fault=='rotated': transform=(0,1,.2,5,0,-1)
    if fault=='south-up': transform=(0,1,0,5,0,1)
    if fault=='missing-grid': transform=None
    path=create_raster(tmp_path/'bad.tif',np.ones((2,2) if fault=='small' else (5,5)),transform,crs)
    ds=gdal.Open(str(path),gdal.GA_Update);b=ds.GetRasterBand(1)
    if fault=='unit-conflict':b.SetUnitType('foot')
    if fault=='unit-unknown':b.SetUnitType('fathom')
    if fault=='scale':b.SetScale(2)
    if fault=='offset':b.SetOffset(10)
    if fault=='alpha':b.SetColorInterpretation(gdal.GCI_AlphaBand)
    b=ds=None
    monkeypatch.setattr(gdal,'DEMProcessing',lambda *a,**k:pytest.fail('invalid DEM reached GDAL terrain computation'))
    with raster_workspace(),pytest.raises(ValueError):
        raster_slope(raster_load(source_plan(path)), 'unknown' if fault=='unit' else 'metre', band=True if fault=='band' else 1)


@pytest.mark.parametrize('kwargs', [{'azimuth':True},{'azimuth':360},{'azimuth':-1},{'altitude':0},{'altitude':91},{'altitude':math.nan}])
def test_dem_hillshade_rejects_invalid_lighting(kwargs):
    with pytest.raises(ValueError,match='hillshade'): raster_hillshade(None,'metre',**kwargs)


@pytest.mark.parametrize('mode', ['interval','levels'])
def test_dem_contour_plane_levels_metres_geometry_and_empty_result(tmp_path, mode):
    rows,columns=np.indices((7,7))
    path=create_raster(tmp_path/'contour.tif',(columns+.5)*2/.3048,(100,2,0,50,0,-3),'EPSG:3857')
    before=hashlib.sha256(path.read_bytes()).hexdigest()
    with raster_workspace():
        source=raster_load(source_plan(path))
        result=raster_contour(source,'foot',**({'interval':4} if mode=='interval' else {'levels':[4,8,12]}))
        assert set(result.elevation)=={4,8,12}
        assert str(result.elevation.dtype)=='float64' and result.crs.to_epsg()==3857
        for row in result.itertuples():
            assert row.geometry.geom_type=='LineString' and row.geometry.is_valid and not row.geometry.is_empty
            assert all(abs(x-(100+row.elevation))<1e-5 for x,y in row.geometry.coords)
        empty=raster_contour(source,'foot',levels=[1000.])
        assert empty.empty and list(empty.columns)==['elevation','geometry'] and str(empty.elevation.dtype)=='float64'
    assert hashlib.sha256(path.read_bytes()).hexdigest()==before


@pytest.mark.parametrize('kwargs', [{},{'interval':1,'levels':[1]},{'interval':0},{'interval':True},{'interval':math.inf},{'levels':[]},{'levels':[2,1]},{'levels':[1,1]},{'levels':[True]},{'levels':[math.nan]},{'levels':'1,2'}])
def test_dem_contour_rejects_ambiguous_or_invalid_levels(kwargs):
    with pytest.raises(ValueError,match='contour'): raster_contour(None,'metre',**kwargs)


def test_dem_terrain_metadata_contract_is_discovered_without_private_paths():
    catalog={item['id']:item for item in list_operators()}
    for name in ('raster_slope','raster_aspect','raster_hillshade','raster_contour'):
        spec=catalog[name]
        assert spec['execution_modes']==['workflow'] and spec['effects']==['read']
        assert spec['attributes']['resource_groups']==['raster']
        params={param['name']:param for param in spec['parameters']}
        assert params['input_raster']['param_type']=='input'
        assert params['elevation_unit']['required'] is True
        assert spec['output_ports'][0]['type']==('GeoDataFrame' if name=='raster_contour' else 'raster')
        assert not {'path','access_plan','engine_id'} & params.keys()


@pytest.mark.parametrize('operator', ['raster_slope','raster_aspect','raster_hillshade','raster_contour'])
@pytest.mark.parametrize('invalid', [False,True])
def test_dem_terrain_async_dag_saves_via_existing_ports_and_cleans_workspace(tmp_path,monkeypatch,operator,invalid):
    import api_server
    import workflow_engine
    source=create_raster(tmp_path/'dem.tif',np.tile(np.arange(9,dtype=float),(9,1)),
                         transform=(100,1,0,200,0,-1),crs='EPSG:3857')
    before=source.read_bytes()
    contour=operator=='raster_contour'
    target=tmp_path/('contours.geojson' if contour else 'terrain.cog.tif')
    params={'input_raster':{'$ref':'load'},'elevation_unit':'unknown' if invalid else 'metre'}
    if contour:params['levels']=[2.,4.,6.]
    save_params=({'input_df':{'$ref':'terrain'},'path':target.name,'connection_info':{'mount_path':str(tmp_path)}} if contour else
                 {'input_raster':{'$ref':'terrain'},'access_plan':target_plan(target),'profile':'cog','blocksize':128})
    tasks=[{'id':'load','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(source)}},
           {'id':'terrain','operator':operator,'depends_on':['load'],'params':params},
           {'id':'save','operator':'save' if contour else 'raster_save','depends_on':['terrain'],'params':save_params}]
    paths=[];original=workflow_engine._execute_operator
    def execute(name,params):
        if name==operator:paths.append(params['input_raster'].workspace)
        return original(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',execute)
    client=api_server.app.test_client()
    response=client.post('/api/workflow',json={'workflow_def':{'tasks':tasks},'input_data':{},
        'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert response.status_code==202,response.json
    deadline=time.monotonic()+10
    while time.monotonic()<deadline:
        status=client.get('/api/executions/'+response.json['execution_id']).json
        if status['status'] in ('success','failed'):break
        time.sleep(.01)
    assert status['status']==('failed' if invalid else 'success'),status
    assert paths and all(not p.exists() for p in paths) and source.read_bytes()==before
    if invalid:assert not target.exists()
    elif contour:
        import geopandas as gpd
        saved=gpd.read_file(target)
        assert set(saved.elevation)=={2,4,6} and saved.crs.to_epsg()==3857
        assert set(saved.geometry.geom_type)=={'LineString'}
    else:
        result=json.loads(status['result'])
        assert result['size_bytes']==target.stat().st_size>0
        with raster_workspace():assert validate_cog(raster_load(source_plan(target)))['valid']
    assert client.post('/api/operators/'+operator+'/invoke',json={'params':{}}).status_code==403


@pytest.mark.parametrize('fault', ['mask','alpha','nan','all-invalid'])
def test_dem_contour_selected_band_validity_and_no_fill(tmp_path,fault):
    rows,columns=np.indices((7,7));values=columns.astype(float)+.5
    alpha=np.full((7,7),128.)
    if fault=='nan':values[:,3]=np.nan
    if fault=='all-invalid':values[:]=np.nan
    if fault=='alpha':alpha[:,3]=0
    path=create_raster(tmp_path/'contour-holes.tif',np.stack([np.full((7,7),1000.),values,alpha]),
        (100,1,0,200,0,-1),'EPSG:3857',nodata=math.nan)
    dataset=gdal.Open(str(path),gdal.GA_Update);dataset.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
    if fault=='mask':
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):dataset.CreateMaskBand(gdal.GMF_PER_DATASET)
        mask=np.full((7,7),255,np.uint8);mask[:,3]=0
        dataset.GetRasterBand(2).GetMaskBand().WriteRaster(0,0,7,7,mask.tobytes())
    dataset=None
    with raster_workspace():
        result=raster_contour(raster_load(source_plan(path)),'metre',band=2,levels=[2.,3.5,5.])
        assert set(result.elevation)==(set() if fault=='all-invalid' else {2.,5.})
        for row in result.itertuples():
            assert all(abs(x-(100+row.elevation))<1e-5 for x,y in row.geometry.coords)


def test_dem_terrain_bounded_normalization_keeps_cross_block_neighbors(tmp_path,monkeypatch):
    import operators.raster_compute as compute
    rows,columns=np.indices((515,517));values=(columns+rows*2).astype(float)
    values[511,511]=np.nan
    path=create_raster(tmp_path/'block.tif',values,(0,1,0,515,0,-1),'EPSG:3857',nodata=math.nan)
    original=compute._read_values;reads=[]
    def bounded(band,x,y,width,height):
        assert width<=512 and height<=512
        reads.append((x,y,width,height))
        return original(band,x,y,width,height)
    monkeypatch.setattr(compute,'_read_values',bounded)
    with raster_workspace():
        result=raster_slope(raster_load(source_plan(path)),'metre')
        dataset=gdal.Open(str(result.path));actual=read_band_values(dataset.GetRasterBand(1))
        valid=np.zeros(values.shape,bool);valid[1:-1,1:-1]=True;valid[510:513,510:513]=False
        np.testing.assert_array_equal(np.isfinite(actual),valid)
        np.testing.assert_allclose(actual[valid],math.degrees(math.atan(math.sqrt(5))),atol=1e-5)
        dataset=None
    assert any(x==512 or y==512 for x,y,w,h in reads)


def test_dem_contour_gdal_failure_closes_vector_before_workspace_cleanup(tmp_path,monkeypatch):
    path=create_raster(tmp_path/'failure.tif',np.arange(25,dtype=float).reshape(5,5),crs='EPSG:3857')
    before=path.read_bytes()
    def fail(*args,**kwargs):raise RuntimeError('injected contour failure')
    monkeypatch.setattr(gdal,'ContourGenerateEx',fail)
    with raster_workspace():
        source=raster_load(source_plan(path));workspace=source.workspace
        with pytest.raises(RuntimeError,match='injected contour failure'):raster_contour(source,'metre',interval=5)
    assert not workspace.exists() and path.read_bytes()==before


def aggregate_oracle(data, valid, factors, method):
    fx, fy = factors
    expected = np.full(((data.shape[0] + fy - 1)//fy, (data.shape[1] + fx - 1)//fx), np.nan)
    for row in range(expected.shape[0]):
        for column in range(expected.shape[1]):
            region = np.s_[row*fy:(row+1)*fy, column*fx:(column+1)*fx]
            values = data[region][valid[region]].tolist()
            if values:
                expected[row, column] = (math.fsum(v/len(values) for v in values) if method == 'mean'
                    else math.fsum(values) if method == 'sum' else min(values) if method == 'min' else max(values))
    return expected


@pytest.mark.parametrize('method', ['sum', 'mean', 'min', 'max'])
@pytest.mark.parametrize('transform,crs', [(None, ''), ((3, 2, .5, 7, -.25, -2), 'EPSG:3857')])
@pytest.mark.parametrize('factors', [[3, 2], [1, 1], [2147483647, 2147483647]])
def test_aggregate_partial_groups_grid_missing_facts_and_cog(tmp_path, method, transform, crs, factors):
    data = np.arange(35, dtype=float).reshape(5, 7) - 5
    data[0:2, 0:3] = -9999
    data[3, 3], data[4, 6] = np.inf, np.nan
    path = create_raster(tmp_path/'aggregate.tif', data, transform, crs)
    before = path.read_bytes()
    expected = aggregate_oracle(data, np.isfinite(data) & (data != -9999), factors, method)
    with raster_workspace():
        result = raster_aggregate(raster_load(source_plan(path)), factors, method)
        facts = raster_info(result)
        fx, fy = factors
        expected_transform = [transform[0], transform[1]*fx, transform[2]*fy,
                              transform[3], transform[4]*fx, transform[5]*fy] if transform else []
        assert facts['width'] == expected.shape[1] and facts['height'] == expected.shape[0]
        assert facts['transform'] == expected_transform and bool(facts['crs']) == bool(crs)
        target = tmp_path/'aggregate.cog.tif'
        saved = raster_save(result, target_plan(target), profile='cog', blocksize=128)
        assert saved['size_bytes'] == target.stat().st_size > 0
        loaded = raster_load(source_plan(target))
        assert validate_cog(loaded)['valid']
        output = gdal.Open(str(loaded.path))
        band = output.GetRasterBand(1)
        np.testing.assert_allclose(read_band_values(band), expected, equal_nan=True)
        np.testing.assert_array_equal(np.frombuffer(band.GetMaskBand().ReadRaster(), dtype=np.uint8).reshape(expected.shape)>0, np.isfinite(expected))
        assert band.DataType == gdal.GDT_Float64 and np.isnan(band.GetNoDataValue())
        band = output = None
    assert path.read_bytes() == before


@pytest.mark.parametrize('method', ['sum', 'mean', 'min', 'max'])
def test_aggregate_independent_bands_mask_alpha_and_zero(tmp_path, method):
    data = np.arange(24, dtype=float).reshape(4,6)
    other = -data - 1
    data[0,2] = -9999
    other[1,2] = np.nan
    data[2,0] = 0
    alpha = np.full((4,6),255.)
    alpha[:2,:2], alpha[2,0], alpha[2,3] = 0, 128, np.inf
    mask = np.full((4,6),255, dtype=np.uint8)
    mask[3,5] = 0
    path = create_raster(tmp_path/'bands.tif', np.stack([data,other,alpha]))
    ds = gdal.Open(str(path),gdal.GA_Update)
    ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):
        ds.CreateMaskBand(gdal.GMF_PER_DATASET)
    ds.GetRasterBand(1).GetMaskBand().WriteRaster(0,0,6,4,mask.tobytes())
    ds = None
    with raster_workspace():
        result = raster_aggregate(raster_load(source_plan(path)),[2,2],method)
        ds = gdal.Open(str(result.path))
        assert ds.RasterCount == 2
        for index, values in enumerate([data,other],1):
            valid = (mask>0) & np.isfinite(alpha) & (alpha>0) & np.isfinite(values) & (values!=-9999)
            expected = aggregate_oracle(values,valid,[2,2],method)
            np.testing.assert_allclose(read_band_values(ds.GetRasterBand(index)),expected,equal_nan=True)
            assert ds.GetRasterBand(index).GetColorInterpretation() != gdal.GCI_AlphaBand
        ds = None


@pytest.mark.parametrize('method', ['sum', 'mean', 'min', 'max'])
@pytest.mark.parametrize('shape,factors', [((513,1025),[1024,512]), ((7,1027),[1,3]), ((513,517),[3,5])])
def test_aggregate_streams_large_groups_and_cross_block_boundaries(tmp_path, monkeypatch, method, shape, factors):
    import operators.raster_compute as compute
    data = np.arange(np.prod(shape),dtype=float).reshape(shape)%97-48
    data[::31,::17] = -9999
    expected = aggregate_oracle(data,data!=-9999,factors,method)
    path = create_raster(tmp_path/'large.tif',data)
    read = compute._read_values
    windows = []
    def bounded(band,x,y,width,height):
        windows.append((width,height))
        assert width<=512 and height<=512
        return read(band,x,y,width,height)
    monkeypatch.setattr(compute,'_read_values',bounded)
    with raster_workspace():
        result = raster_aggregate(raster_load(source_plan(path)),factors,method)
        ds = gdal.Open(str(result.path))
        np.testing.assert_allclose(read_band_values(ds.GetRasterBand(1)),expected,equal_nan=True,atol=1e-10)
        ds = None
    assert len(windows)>1


@pytest.mark.parametrize('factors', [None, [], [2], [1,2,3], '2', [True,2], [2.,2], [0,2], [-1,2], [2,2**31], [10**400,1], [np.inf,2]])
def test_aggregate_rejects_invalid_factors(raster_file,factors):
    with raster_workspace(), pytest.raises(ValueError):
        raster_aggregate(raster_load(source_plan(raster_file)),factors,'mean')


def test_aggregate_rejects_complex_alpha_only_and_invalid_method(tmp_path):
    for kind in ['complex','alpha']:
        path = tmp_path/(kind+'.tif')
        ds = gdal.GetDriverByName('GTiff').Create(str(path),2,2,1,gdal.GDT_CFloat64 if kind=='complex' else gdal.GDT_Float64)
        if kind=='alpha':
            ds.GetRasterBand(1).SetColorInterpretation(gdal.GCI_AlphaBand)
        ds = None
        with raster_workspace(), pytest.raises(ValueError):
            raster_aggregate(raster_load(source_plan(path)),[2,2],'sum')
    path = create_raster(tmp_path/'plain.tif',np.ones((2,2)))
    with raster_workspace(), pytest.raises(ValueError,match='method'):
        raster_aggregate(raster_load(source_plan(path)),[2,2],'median')


@pytest.mark.parametrize('value',[1e308, np.finfo(np.float64).max, np.nextafter(0.,1.), 0.])
def test_aggregate_mean_avoids_sum_overflow_and_subnormal_underflow(tmp_path,value):
    path = create_raster(tmp_path/'extreme-values.tif',np.full((3,3),value))
    with raster_workspace():
        source = raster_load(source_plan(path))
        result = raster_aggregate(source,[3,3],'mean')
        ds = gdal.Open(str(result.path))
        assert read_band_values(ds.GetRasterBand(1))[0,0] == pytest.approx(value,rel=1e-15,abs=0)
        ds = None
        if value==1e308:
            assert raster_statistics(raster_aggregate(source,[3,3],'sum'))['valid_count']==0


@pytest.mark.parametrize('rules', [
    None, {}, [], '[]', [1], [None],
    [{'value': 1}], [{'value': 1, 'class': 2, 'extra': 3}],
    [{'min': 1, 'class': 2}], [{'min': 2, 'max': 2, 'class': 1}],
    [{'min': 3, 'max': 2, 'class': 1}],
    [{'value': True, 'class': 2}], [{'value': '1', 'class': 2}],
    [{'value': None, 'class': 2}], [{'value': float('nan'), 'class': 2}],
    [{'value': float('inf'), 'class': 2}], [{'value': 10**400, 'class': 2}],
    [{'value': 1, 'class': False}], [{'value': 1, 'class': None}],
    [{'value': 1, 'class': '2'}], [{'value': 1, 'class': float('nan')}],
    [{'min': False, 'max': 2, 'class': 1}],
    [{'min': 1, 'max': float('inf'), 'class': 1}],
    [{'value': 1, 'class': 2}, {'value': 1, 'class': 3}],
    [{'min': 0, 'max': 2, 'class': 1}, {'min': 1, 'max': 3, 'class': 2}],
    [{'min': 0, 'max': 2, 'class': 1}, {'value': 0, 'class': 2}],
    [{'min': 0, 'max': 2, 'class': 1}, {'value': 1, 'class': 2}],
    [{'min': None, 'max': None, 'class': 1}, {'value': 1, 'class': 2}],
])
def test_reclassify_rejects_invalid_rules(rules):
    with pytest.raises(ValueError):
        _classification_rules(rules)


@pytest.mark.parametrize('unmatched', ['nodata', 'keep'])
def test_reclassify_boundaries_unordered_rules_and_source_hash(tmp_path, unmatched):
    values = np.array([[-2, -1, 0, 1, 2, 3, 4, 5, -9999, np.nan, np.inf]])
    path = create_raster(tmp_path / 'classes.tif', values)
    before = hashlib.sha256(path.read_bytes()).hexdigest()
    rules = [{'value': 2, 'class': 0}, {'min': 4, 'max': None, 'class': 9},
             {'min': 0, 'max': 2, 'class': 10}, {'min': None, 'max': -1, 'class': -5}]
    expected = np.array([[-5, -1 if unmatched == 'keep' else np.nan, 10, 10, 0,
                          3 if unmatched == 'keep' else np.nan, 9, 9, np.nan, np.nan, np.nan]])
    with raster_workspace():
        source = raster_load(source_plan(path))
        for ordered in [rules, list(reversed(rules))]:
            classified = raster_reclassify(source, ordered, unmatched=unmatched)
            dataset = gdal.Open(str(classified.path))
            assert dataset.RasterCount == 1
            assert dataset.GetGeoTransform() == (0, 1, 0, 4, 0, -1)
            np.testing.assert_allclose(read_band_values(dataset.GetRasterBand(1)), expected, equal_nan=True)
            assert np.isnan(dataset.GetRasterBand(1).GetNoDataValue())
            dataset = None
    assert hashlib.sha256(path.read_bytes()).hexdigest() == before


@pytest.mark.parametrize('transform,crs', [(None, ''), ((3, 2, .5, 7, -.25, -2), 'EPSG:3857')])
def test_reclassify_selected_band_validity_and_cog(tmp_path, transform, crs):
    data = np.array([[0, 1, 2, 3], [4, 5, -9999, 7]], dtype=float)
    alpha = np.array([[128, 0, 255, np.nan], [255, 255, 255, 255]], dtype=float)
    path = create_raster(tmp_path / 'validity.tif', np.stack([data * 2, data, alpha]), transform, crs)
    dataset = gdal.Open(str(path), gdal.GA_Update)
    dataset.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK', 'YES'):
        dataset.CreateMaskBand(gdal.GMF_PER_DATASET)
    dataset.GetRasterBand(2).GetMaskBand().WriteRaster(0, 0, 4, 2, bytes([255]*4 + [0,255,255,255]))
    dataset = None
    target = tmp_path / 'result.cog.tif'
    with raster_workspace():
        source = raster_load(source_plan(path))
        result = raster_reclassify(source, [{'min': None, 'max': None, 'class': 0}], unmatched='keep', band=2)
        facts = raster_info(result)
        assert facts['transform'] == (list(transform) if transform else [])
        assert bool(facts['crs']) == bool(crs)
        saved = raster_save(result, target_plan(target), profile='cog', blocksize=128)
        assert saved['size_bytes'] == target.stat().st_size > 0
        reloaded = raster_load(source_plan(target))
        assert validate_cog(reloaded)['valid']
        dataset = gdal.Open(str(reloaded.path))
        expected = np.array([[0, np.nan, 0, np.nan], [np.nan, 0, np.nan, 0]])
        np.testing.assert_allclose(read_band_values(dataset.GetRasterBand(1)), expected, equal_nan=True)
        assert dataset.GetRasterBand(1).DataType == gdal.GDT_Float64
        dataset = None
        with pytest.raises(ValueError, match='Alpha'):
            raster_reclassify(source, [{'value': 1, 'class': 1}], unmatched='nodata', band=3)


def test_reclassify_binary_lookup_matches_independent_oracle_across_blocks(tmp_path, monkeypatch):
    import operators.raster_compute as compute
    values = np.random.default_rng(19).uniform(-100, 100, (513, 1025))
    values[0, 0] = -9999
    rules = [{'min': float(lo), 'max': float(lo + .25), 'class': index}
             for index, lo in enumerate(np.arange(-100, 100, 1))]
    rules += [{'value': 0.5, 'class': -1}]
    values[512, 1024] = .5
    expected = np.full(values.shape, np.nan)
    for rule in rules:
        matched = values == rule['value'] if 'value' in rule else (values >= rule['min']) & (values < rule['max'])
        expected[matched & (values != -9999)] = rule['class']
    path = create_raster(tmp_path / 'blocks.tif', values)
    windows = []
    read = compute._read_values
    def tracked_read(band, x, y, width, height):
        windows.append((width, height))
        return read(band, x, y, width, height)
    monkeypatch.setattr(compute, '_read_values', tracked_read)
    with raster_workspace():
        result = raster_reclassify(raster_load(source_plan(path)), list(reversed(rules)), unmatched='nodata')
        dataset = gdal.Open(str(result.path))
        np.testing.assert_allclose(read_band_values(dataset.GetRasterBand(1)), expected, equal_nan=True)
        dataset = None
    assert len(windows) == 6 and all(width <= 512 and height <= 512 for width, height in windows)


def test_reclassify_default_policy_and_invalid_options(raster_file):
    with raster_workspace():
        source = raster_load(source_plan(raster_file))
        result = raster_reclassify(source, [{'value': 2, 'class': 10}])
        stats = raster_statistics(result)
        assert stats['valid_count'] == 1 and stats['invalid_count'] == 15
        assert stats['min'] == 10
        with pytest.raises(ValueError, match='unmatched'):
            raster_reclassify(source, [{'value': 1, 'class': 1}], unmatched='invalid')
        for band in [0, True, 1.5, 3]:
            with pytest.raises(ValueError, match='band'):
                raster_reclassify(source, [{'value': 1, 'class': 1}], unmatched='nodata', band=band)


def test_reclassify_adjacent_intervals_and_float_boundaries(tmp_path):
    values = np.array([[np.nextafter(0., -np.inf), 0, np.nextafter(1., -np.inf),
                        1, np.nextafter(1., np.inf), np.nextafter(2., -np.inf), 2]])
    path = create_raster(tmp_path / 'edges.tif', values)
    rules = [{'min': 0, 'max': 1, 'class': 10}, {'min': 1, 'max': 2, 'class': 20},
             {'value': 2, 'class': 30}]
    with raster_workspace():
        result = raster_reclassify(raster_load(source_plan(path)), rules, unmatched='nodata')
        dataset = gdal.Open(str(result.path))
        np.testing.assert_allclose(read_band_values(dataset.GetRasterBand(1)),
                                   [[np.nan, 10, 10, 20, 20, 20, 30]], equal_nan=True)
        dataset = None


@pytest.mark.parametrize('unmatched', ['nodata', 'keep'])
def test_reclassify_all_invalid_pixels_remain_invalid(tmp_path, unmatched):
    path = create_raster(tmp_path / 'invalid.tif', np.array([[-9999, np.nan, np.inf, -np.inf]]))
    with raster_workspace():
        result = raster_reclassify(raster_load(source_plan(path)),
                            [{'min': None, 'max': None, 'class': 1}], unmatched=unmatched)
        stats = raster_statistics(result)
        assert stats['valid_count'] == 0 and stats['invalid_count'] == 4


def test_reclassify_rejects_complex_band(tmp_path):
    path = tmp_path / 'complex.tif'
    dataset = gdal.GetDriverByName('GTiff').Create(str(path), 1, 1, 1, gdal.GDT_CFloat64)
    dataset.GetRasterBand(1).WriteRaster(0, 0, 1, 1, np.array([1 + 2j], dtype=np.complex128).tobytes(),
                                       buf_type=gdal.GDT_CFloat64)
    dataset = None
    with raster_workspace():
        with pytest.raises(ValueError, match='Complex'):
            raster_reclassify(raster_load(source_plan(path)), [{'value': 1, 'class': 2}], unmatched='nodata')


def test_reclassify_public_metadata_and_executable_example(raster_file):
    specs = {item['id']: item for item in list_operators()}
    spec = specs['raster_reclassify']
    params = {param['name']: param for param in spec['parameters']}
    assert params['rules']['type'] == 'array' and params['rules']['item_type'] == 'object'
    assert params['rules']['required'] and 'default' not in params['rules']
    assert params['input_raster']['type'] == 'raster' and params['input_raster']['param_type'] == 'input'
    assert params['band']['type'] == 'integer' and params['band']['default'] == 1
    assert params['unmatched']['enum'] == ['nodata', 'keep']
    assert params['unmatched']['default'] == 'nodata' and not params['unmatched']['required']
    assert spec['execution_modes'] == ['workflow'] and spec['effects'] == ['read']
    assert spec['attributes']['resource_groups'] == ['raster']
    assert spec['output_ports'] == [{'name': 'default', 'type': 'raster',
                                    'description': '栅格重分类结果', 'is_default': True}]
    example = spec['detailed_description']['workflow_example']['params']
    with raster_workspace():
        result = raster_reclassify(**{**example, 'input_raster': raster_load(source_plan(raster_file))})
        assert raster_statistics(result)['valid_count'] == 9


def test_aggregate_public_metadata_and_example(raster_file):
    spec = next(item for item in list_operators() if item['id']=='raster_aggregate')
    params = {param['name']:param for param in spec['parameters']}
    assert params['factors']['type']=='array' and params['factors']['item_type']=='integer'
    assert params['method']['enum']==['sum','mean','min','max']
    assert all(params[name]['required'] and 'default' not in params[name] for name in ['factors','method'])
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read']
    assert spec['attributes']['resource_groups']==['raster']
    assert params['input_raster']['type']=='raster' and spec['output_ports'][0]['type']=='raster'
    example = spec['detailed_description']['workflow_example']['params']
    with raster_workspace():
        result = raster_aggregate(**{**example,'input_raster':raster_load(source_plan(raster_file))})
        ds = gdal.Open(str(result.path))
        np.testing.assert_allclose(read_band_values(ds.GetRasterBand(1)),[[13/3,5.5],[11.5,13.5]])
        ds = None


@pytest.mark.parametrize('method',['sum','mean','min','max'])
def test_aggregate_async_public_dag_cog_and_cleanup(raster_file,tmp_path,monkeypatch,method):
    import api_server
    import operators.raster_compute as compute
    paths = []
    original = OPERATORS['raster_load']['function']
    def tracked(*args,**kwargs):
        paths.append(compute._WORKSPACE.get())
        return original(*args,**kwargs)
    monkeypatch.setitem(OPERATORS['raster_load'],'function',tracked)
    target = tmp_path/'aggregate.cog.tif'
    before = raster_file.read_bytes()
    workflow = {'tasks':[
        {'id':'load','operator':'raster_load','params':{'access_plan':source_plan(raster_file)},'depends_on':[]},
        {'id':'aggregate','operator':'raster_aggregate','params':{'input_raster':{'$ref':'load'},'factors':[3,2],'method':method},'depends_on':['load']},
        {'id':'save','operator':'raster_save','params':{'input_raster':{'$ref':'aggregate'},'access_plan':target_plan(target),'profile':'cog','blocksize':128},'depends_on':['aggregate']},
    ]}
    client = api_server.app.test_client()
    response = client.post('/api/workflow',json={'workflow_def':workflow,'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert response.status_code==202,response.json
    deadline = time.monotonic()+5
    while time.monotonic()<deadline:
        status = client.get('/api/executions/'+response.json['execution_id']).json
        if status['status'] in ['success','failed']:
            break
        time.sleep(.01)
    assert status['status']=='success',status
    facts = json.loads(status['result'])
    assert facts['size_bytes']==target.stat().st_size>0
    assert paths and all(not path.exists() and str(path) not in json.dumps(status) for path in paths)
    assert str(tmp_path) not in json.dumps(status) and raster_file.read_bytes()==before
    values = np.arange(1,17,dtype=float).reshape(4,4)
    expected = aggregate_oracle(values,values!=1,[3,2],method)
    ds = gdal.Open(str(target))
    np.testing.assert_allclose(read_band_values(ds.GetRasterBand(1)),expected,equal_nan=True)
    ds = None
    assert client.post('/api/operators/raster_aggregate/invoke',json={'params':{}}).status_code==403


@pytest.mark.parametrize('unmatched', [None, 'keep'], ids=['default-nodata', 'keep'])
@pytest.mark.parametrize('api', [False, True], ids=['runner', 'async-http'])
def test_reclassify_public_dag_cog_and_cleanup(raster_file, tmp_path, monkeypatch, unmatched, api):
    import operators.raster_compute as compute
    paths = []
    original = OPERATORS['raster_load']['function']
    def tracked(*args, **kwargs):
        paths.append(compute._WORKSPACE.get())
        return original(*args, **kwargs)
    monkeypatch.setitem(OPERATORS['raster_load'], 'function', tracked)
    target = tmp_path / 'classified.cog.tif'
    before = raster_file.read_bytes()
    params = {'input_raster': {'$ref': 'load'},
              'rules': [{'min': 2, 'max': 5, 'class': 10}, {'value': 5, 'class': 0}]}
    if unmatched is not None:
        params['unmatched'] = unmatched
    workflow = {'tasks': [
        {'id': 'load', 'operator': 'raster_load', 'params': {'access_plan': source_plan(raster_file)}, 'depends_on': []},
        {'id': 'classify', 'operator': 'raster_reclassify', 'params': params, 'depends_on': ['load']},
        {'id': 'save', 'operator': 'raster_save', 'params': {'input_raster': {'$ref': 'classify'},
         'access_plan': target_plan(target), 'profile': 'cog', 'blocksize': 128}, 'depends_on': ['classify']},
    ]}
    if api:
        import api_server
        client = api_server.app.test_client()
        response = client.post('/api/workflow', json={'workflow_def': workflow, 'runtime': {
            'tenant_id': 7, 'execution_authorization': {'id': 1, 'effects': ['read', 'write']}}})
        assert response.status_code == 202, response.json
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            status = client.get('/api/executions/' + response.json['execution_id']).json
            if status['status'] in ['success', 'failed']:
                break
            time.sleep(.01)
        assert status['status'] == 'success', status
        facts = json.loads(status['result'])
        serialized = json.dumps(status)
        assert client.post('/api/operators/raster_reclassify/invoke', json={'params': {}}).status_code == 403
    else:
        result = execute_workflow(workflow)
        assert result['status'] == 'success', result
        facts = json.loads(result['final_result'])
        serialized = json.dumps(result)
    assert facts['size_bytes'] == target.stat().st_size > 0
    assert paths and all(not path.exists() and str(path) not in serialized for path in paths)
    assert str(tmp_path) not in serialized and raster_file.read_bytes() == before
    expected = np.arange(1, 17, dtype=float).reshape(4, 4)
    expected[0, 0] = np.nan
    expected[(expected >= 2) & (expected < 5)] = 10
    expected[1, 0] = 0
    if unmatched is None:
        expected[1, 1:] = np.nan
        expected[2:] = np.nan
    dataset = gdal.Open(str(target))
    np.testing.assert_allclose(read_band_values(dataset.GetRasterBand(1)), expected, equal_nan=True)
    dataset = None
    with raster_workspace():
        assert validate_cog(raster_load(source_plan(target)))['valid']


@pytest.fixture
def raster_file(tmp_path):
    values = np.arange(1, 17, dtype=float).reshape(4, 4)
    values[0, 0] = -9999
    return create_raster(tmp_path / 'source.tif', np.stack([values, values * 2]))


def test_block_statistics_histogram_and_nodata(raster_file):
    with raster_workspace():
        raster = raster_load(source_plan(raster_file))
        stats = raster_statistics(raster)
        np.testing.assert_allclose([stats['min'], stats['max'], stats['mean'], stats['stddev']], [2, 16, 9, np.std(np.arange(2, 17))])
        assert (stats['valid_count'], stats['invalid_count']) == (15, 1)
        histogram = raster_histogram(raster, bins=4, value_range=[4, 12])
        expected, edges = np.histogram(np.arange(2, 17), bins=4, range=(4, 12))
        assert histogram['counts'] == expected.tolist()
        assert histogram['edges'] == edges.tolist()
        assert histogram['outside_count'] == 6


def test_statistics_combines_blocks_and_all_nodata(tmp_path):
    data = np.arange(1025 * 513, dtype=float).reshape(513, 1025) + 1e9
    path = create_raster(tmp_path / 'blocks.tif', data)
    with raster_workspace():
        stats = raster_statistics(raster_load(source_plan(path)))
        assert stats['valid_count'] == data.size
        assert stats['mean'] == pytest.approx(data.mean())
        assert stats['stddev'] == pytest.approx(data.std())
    path = create_raster(tmp_path / 'empty.tif', np.full((3, 3), -9999.0))
    with raster_workspace():
        raster = raster_load(source_plan(path))
        assert raster_statistics(raster)['mean'] is None
        assert raster_histogram(raster)['counts'] == []


def _profile_process_usage():
    import resource
    import sys
    usage = resource.getrusage(resource.RUSAGE_SELF)
    return {'process_cpu_seconds': usage.ru_utime + usage.ru_stime,
            'process_peak_rss_bytes': int(usage.ru_maxrss * (1 if sys.platform == 'darwin' else 1024))}


def _raster_profile_task(source, target, profile_case, epoch, barrier=None):
    """Each thread owns its workspace and GDAL datasets; cache and RSS are process-wide."""
    if profile_case == 'directory_mosaic':
        return _directory_mosaic_profile_task(source, target, epoch, barrier)
    stages = []
    workspace = None

    def measure(name, operation):
        nonlocal workspace
        started = time.perf_counter()
        result = operation()
        finished = time.perf_counter()
        if workspace is None:
            workspace = result.workspace
        observed_bytes = sum(p.stat().st_size for p in workspace.rglob('*') if p.is_file())
        if target.is_file():
            observed_bytes += target.stat().st_size
        stages.append({'operator': name, 'elapsed_seconds': finished - started,
                       'started_seconds': started - epoch, 'finished_seconds': finished - epoch,
                       'process_peak_rss_bytes': _profile_process_usage()['process_peak_rss_bytes'],
                       'observed_file_bytes': observed_bytes})
        return result

    if barrier is not None:
        barrier.wait(timeout=30)
    started = time.perf_counter()
    with raster_workspace():
        raster = measure('raster_load', lambda: raster_load(source_plan(source)))
        facts = raster_info(raster)
        expression = '(' + '+'.join(f'b{i}' for i in range(1, facts['band_count'] + 1)) + ')*2+1'
        stats = measure('raster_statistics', lambda: raster_statistics(raster))
        histogram = measure('raster_histogram', lambda: raster_histogram(raster))
        resampling = None
        if profile_case == 'multiband_reproject':
            expression = None
            transform = facts['transform']
            resolution = [6378137 * np.deg2rad(transform[1]) * 1.5,
                          6378137 * np.deg2rad(-transform[5]) / np.cos(np.deg2rad(transform[3])) * 1.5]
            computed = measure('raster_reproject_nearest', lambda: raster_reproject(
                raster, 'EPSG:3857', resolution=resolution, resampling='nearest'))
            resampling = {'algorithm': 'nearest', 'target_crs': 'EPSG:3857', 'resolution': resolution}
        else:
            computed = measure('raster_band_math', lambda: raster_band_math(raster, expression))
        if profile_case == 'average_resample':
            size = [(facts['width'] + 1) // 2, (facts['height'] + 1) // 2]
            computed = measure('raster_resample_average',
                               lambda: raster_resample(computed, size=size, resampling='average'))
            resampling = {'algorithm': 'average', 'size': size}
        saved = measure('raster_save_cog', lambda: raster_save(computed, target_plan(target), profile='cog'))
        reloaded = measure('raster_load_saved', lambda: raster_load(source_plan(target)))
        output_stats = measure('raster_statistics_saved', lambda: raster_statistics(reloaded))
        validation = measure('validate_cog_saved', lambda: validate_cog(reloaded))
    finished = time.perf_counter()
    return {'source': facts, 'expression': expression, 'profile_case': profile_case,
            'resampling': resampling, 'statistics': stats, 'histogram': histogram,
            'saved': saved, 'output_statistics': output_stats, 'validation': validation,
            'workspace': str(workspace), 'workspace_removed': not workspace.exists(), 'stages': stages,
            'thread_id': threading.get_ident(), 'started_seconds': started - epoch,
            'finished_seconds': finished - epoch, 'elapsed_seconds': finished - started}


def _profile_environment():
    """Collect report facts in-process; platform.platform() can fork after PROJ use."""
    system, release, machine = platform.system(), platform.release(), platform.machine()
    return {'platform': '-'.join((system, release, machine)), 'machine': machine,
            'cpu_count': os.cpu_count(), 'python': platform.python_version(),
            'gdal': gdal.VersionInfo('RELEASE_NAME'), 'gdal_cache_max_bytes': gdal.GetCacheMax()}


def _raster_profile_worker(source, targets, scratch, connection, profile_case, execution_mode):
    """Measure one process independently of the parent's pixel oracle."""
    import faulthandler
    import traceback
    # Native aborts bypass Python exceptions. Keep all thread stacks outside the pipe.
    # The OS owns this child-only descriptor until process exit, including teardown.
    fault_fd = os.open(scratch / 'native-fault.log', os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    faulthandler.enable(file=fault_fd, all_threads=True)
    # Set the process-wide temporary root once, before starting any threads.
    tempfile.tempdir = str(scratch)
    try:
        started = time.perf_counter()
        if execution_mode == 'parallel':
            barrier = threading.Barrier(len(targets))
            with ThreadPoolExecutor(max_workers=len(targets)) as executor:
                futures = [executor.submit(_raster_profile_task, source, target, profile_case, started, barrier)
                           for target in targets]
                reports = [future.result() for future in futures]
        else:
            reports = [_raster_profile_task(source, target, profile_case, started) for target in targets]
        connection.send({'tasks': reports, 'execution_mode': execution_mode,
                         'elapsed_seconds': time.perf_counter() - started, **_profile_process_usage(),
                         'mosaic_vrts_removed': not any(name.startswith('addp-raster-mosaic-')
                                                       for name in gdal.ReadDir('/vsimem') or []),
                         'environment': _profile_environment(),
                         'native_fault_handler_enabled': faulthandler.is_enabled()})
    except BaseException:
        connection.send({'error': traceback.format_exc()})
    finally:
        connection.close()


def _run_raster_profile(source, targets, tmp_path, profile_case, execution_mode):
    context = multiprocessing.get_context('spawn')
    receiver, sender = context.Pipe(duplex=False)
    # Parent-owned scratch is also reclaimed after child failure or termination.
    with tempfile.TemporaryDirectory(prefix='profile-worker-', dir=tmp_path) as scratch:
        process = context.Process(target=_raster_profile_worker,
                                  args=(source, targets, Path(scratch), sender, profile_case, execution_mode))
        process.start()
        sender.close()

        def failure(reason):
            fault_log = Path(scratch) / 'native-fault.log'
            diagnostic = fault_log.read_text(errors='replace') if fault_log.exists() else ''
            return (f'{reason}: case={profile_case} mode={execution_mode} '
                    f'pid={process.pid} exitcode={process.exitcode}\n{diagnostic}')

        try:
            assert receiver.poll(300), failure('Raster profile worker exceeded 300 seconds')
            try:
                report = receiver.recv()
            except EOFError as exc:
                process.join(10)
                raise AssertionError(failure('Raster profile worker closed its pipe without a report')) from exc
            process.join(10)
            assert process.exitcode == 0, failure('Raster profile worker did not exit successfully')
        finally:
            if process.is_alive():
                process.terminate()
                process.join(10)
            receiver.close()
    assert 'error' not in report, report.get('error')
    return report


def _raster_profile_abnormal_exit(source, targets, scratch, connection, profile_case, execution_mode):
    # Exit without Python unwinding, as a native failure does, without crashing the test host.
    (scratch / 'native-fault.log').write_text('native fault diagnostic sentinel\n')
    if execution_mode == 'after_report':
        connection.send({'tasks': []})
    os._exit(29)


@pytest.mark.parametrize('execution_mode', ['before_report', 'after_report'])
def test_raster_profile_rejects_abnormal_child_exit_with_diagnostics(tmp_path, monkeypatch, execution_mode):
    monkeypatch.setattr(__import__(__name__), '_raster_profile_worker', _raster_profile_abnormal_exit)
    with pytest.raises(AssertionError) as error:
        _run_raster_profile(tmp_path, [], tmp_path, 'directory_mosaic', execution_mode)
    message = str(error.value)
    assert 'exitcode=29' in message
    assert 'case=directory_mosaic' in message and 'mode=' + execution_mode in message
    assert 'native fault diagnostic sentinel' in message
    assert not list(tmp_path.glob('profile-worker-*'))


def _raster_profile_teardown_probe(source, targets, scratch, connection, profile_case, execution_mode):
    # spawn imports the unpatched worker in the child; probe the period after its return.
    import faulthandler
    enable = faulthandler.enable
    destinations = []

    def record_destination(*, file, all_threads):
        destinations.append(file)
        enable(file=file, all_threads=all_threads)

    faulthandler.enable = record_destination
    try:
        _raster_profile_worker(source, targets, scratch, connection, profile_case, 'serial')
    finally:
        faulthandler.enable = enable
    if not faulthandler.is_enabled():
        os._exit(30)
    # Exercise the very same still-open descriptor without raising a fatal signal.
    faulthandler.dump_traceback(file=destinations[0], all_threads=True)
    os._exit(29)


def test_raster_profile_keeps_native_diagnostics_through_child_teardown(tmp_path, monkeypatch):
    monkeypatch.setattr(__import__(__name__), '_raster_profile_worker', _raster_profile_teardown_probe)
    with pytest.raises(AssertionError) as error:
        _run_raster_profile(tmp_path, [], tmp_path, 'directory_mosaic', 'serial')
    message = str(error.value)
    assert 'exitcode=29' in message
    assert '_raster_profile_teardown_probe' in message
    assert not list(tmp_path.glob('profile-worker-*'))


def test_raster_profile_preserves_python_failure_and_cleans_scratch(tmp_path):
    with pytest.raises(AssertionError, match='no source TIFF files were discovered'):
        _run_raster_profile(tmp_path, [tmp_path / 'output'], tmp_path,
                            'directory_mosaic', 'serial')
    assert not list(tmp_path.glob('profile-worker-*'))
    assert not (tmp_path / 'output').exists()


def _profile_values(original, y, height):
    width = original.RasterXSize
    combined = np.zeros((height, width), dtype=np.float64)
    joint_valid = np.ones(combined.shape, dtype=bool)
    counts = []
    for index in range(1, original.RasterCount + 1):
        band = original.GetRasterBand(index)
        values = np.frombuffer(band.ReadRaster(0, y, width, height, buf_type=gdal.GDT_Float64),
                               dtype=np.float64).reshape(height, width)
        valid = np.frombuffer(band.GetMaskBand().ReadRaster(0, y, width, height, buf_type=gdal.GDT_Byte),
                              dtype=np.uint8).reshape(height, width) != 0
        valid &= np.isfinite(values)
        nodata = band.GetNoDataValue()
        if nodata is not None:
            valid &= values != nodata
        counts.append(int(valid.sum()))
        joint_valid &= valid
        combined += values
        if index == 1:
            selected_source = values[valid]
    expected = combined * 2 + 1
    joint_valid &= np.isfinite(expected)
    return expected, joint_valid, counts, selected_source


def _profile_moments(accumulator, selected):
    if not selected.size:
        return
    if accumulator['origin'] is None:
        accumulator['origin'] = float(selected[0])
    centered = selected - accumulator['origin']
    accumulator['count'] += selected.size
    accumulator['sum'] += float(centered.sum())
    accumulator['squares'] += float(np.square(centered).sum())
    accumulator['min'] = min(accumulator['min'], float(selected.min()))
    accumulator['max'] = max(accumulator['max'], float(selected.max()))


def _profile_average_blocks(original, width, height):
    """Independent area overlap oracle, bounded by output strips and source bands."""
    source_width, source_height = original.RasterXSize, original.RasterYSize
    # Integer edge units avoid accumulating floating-point coordinate errors.
    columns = np.arange(width, dtype=np.int64)
    x_start = columns * source_width // width
    x_contributors = (source_width + width - 1) // width + 1
    y_contributors = (source_height + height - 1) // height + 1
    for row in range(0, height, 127):
        rows = np.arange(row, min(row + 127, height), dtype=np.int64)
        y_start = rows * source_height // height
        source_row = int(y_start[0])
        source_end = min(source_height, int(y_start[-1]) + y_contributors)
        values, valid, _, _ = _profile_values(original, source_row, source_end - source_row)
        numerator = np.zeros((rows.size, width), dtype=np.float64)
        denominator = np.zeros_like(numerator)
        for dy in range(y_contributors):
            source_y = y_start + dy
            y_weight = np.maximum(0, np.minimum((rows + 1) * source_height, (source_y + 1) * height)
                                  - np.maximum(rows * source_height, source_y * height))
            y_index = np.minimum(source_y, source_height - 1) - source_row
            for dx in range(x_contributors):
                source_x = x_start + dx
                x_weight = np.maximum(0, np.minimum((columns + 1) * source_width, (source_x + 1) * width)
                                      - np.maximum(columns * source_width, source_x * width))
                x_index = np.minimum(source_x, source_width - 1)
                covered = valid[y_index[:, None], x_index[None, :]]
                weights = y_weight[:, None] * x_weight[None, :] * covered
                numerator += np.where(covered, values[y_index[:, None], x_index[None, :]], 0) * weights
                denominator += weights
        expected = np.full_like(numerator, np.nan)
        np.divide(numerator, denominator, out=expected, where=denominator > 0)
        yield row, expected


def test_profile_area_oracle_fractional_edges_and_joint_nodata(tmp_path):
    source = create_raster(tmp_path / 'oracle.tif', np.array([[[0, 10], [20, 40]],
                                                            [[2, -9999], [4, 8]]], dtype=float))
    original = gdal.Open(str(source))
    blocks = list(_profile_average_blocks(original, 3, 3))
    assert len(blocks) == 1 and blocks[0][0] == 0
    np.testing.assert_allclose(blocks[0][1], [[5, 5, np.nan], [27, 151 / 3, 97], [49, 73, 97]],
                               rtol=0, atol=1e-12, equal_nan=True)
    np.testing.assert_allclose(next(_profile_average_blocks(original, 1, 1))[1], [[151 / 3]])
    original = None
    values = np.array([[0, 10, 20], [30, 40, 50], [60, 70, 80]], dtype=float)
    other = np.zeros_like(values)
    other[1, 1] = -9999
    source = create_raster(tmp_path / 'unequal-areas.tif', np.stack([values, other]))
    original = gdal.Open(str(source))
    # A 1.5 x 1.5 footprint weights full, half and quarter cells; the missing
    # center contributes neither its value nor its quarter-cell area.
    np.testing.assert_allclose(next(_profile_average_blocks(original, 2, 2))[1], [[21, 51], [111, 141]],
                               rtol=0, atol=1e-12)
    original = None


def _profile_source(tmp_path, profile_case='band_math'):
    external = os.environ.get('ADDP_RASTER_PROFILE_SOURCE')
    source = Path(external).resolve() if external is not None else tmp_path / 'profile-source.tif'
    if external is None:
        values = (np.arange(1025 * 513).reshape(513, 1025) % 2001 - 1000).astype(float)
        bands = []
        for index in range(3):
            band = values + index * 7
            band.flat[index::97] = -32768
            bands.append(band)
        geographic = profile_case == 'multiband_reproject'
        create_raster(source, np.stack(bands), crs='EPSG:4326' if geographic else 'EPSG:32650',
                      transform=(110, .0002, 0, 30, 0, -.0002) if geographic else (0, 1, 0, 4, 0, -1),
                      nodata=-32768)
    assert source.is_file(), f'Raster profile source does not exist: {source}'

    return source, external is None


def _profile_digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def _mercator_inverse(x, y):
    # EPSG:3857 is spherical Mercator, independently inverted without GDAL Warp.
    return np.rad2deg(x / 6378137), np.rad2deg(np.arctan(np.sinh(y / 6378137)))


def _assert_profile_moments(stats, accumulator, total):
    count = accumulator['count']
    assert count > 0
    centered_mean = accumulator['sum'] / count
    mean = accumulator['origin'] + centered_mean
    stddev = np.sqrt(max(0, accumulator['squares'] / count - centered_mean ** 2))
    assert (stats['valid_count'], stats['invalid_count']) == (count, total - count)
    np.testing.assert_allclose([stats['min'], stats['max'], stats['mean'], stats['stddev']],
                               [accumulator['min'], accumulator['max'], mean, stddev], rtol=1e-8, atol=1e-8)


def _validate_reprojection_profile(source, target, report):
    original, output = gdal.Open(str(source)), gdal.Open(str(target))
    assert original.GetSpatialRef().GetAuthorityCode(None) == '4326'
    assert output.GetSpatialRef().GetAuthorityCode(None) == '3857'
    assert output.RasterCount == original.RasterCount > 1
    source_grid, grid = original.GetGeoTransform(), output.GetGeoTransform()
    assert source_grid[2] == source_grid[4] == grid[2] == grid[4] == 0
    assert source_grid[1] > 0 and source_grid[5] < 0
    left, top = source_grid[0], source_grid[3]
    right = left + source_grid[1] * original.RasterXSize
    bottom = top + source_grid[5] * original.RasterYSize
    bounds = [6378137 * np.deg2rad(left), 6378137 * np.log(np.tan(np.pi / 4 + np.deg2rad(bottom) / 2)),
              6378137 * np.deg2rad(right), 6378137 * np.log(np.tan(np.pi / 4 + np.deg2rad(top) / 2))]
    np.testing.assert_allclose([grid[0], grid[3]], [bounds[0], bounds[3]], rtol=0, atol=1e-7)
    np.testing.assert_allclose([grid[1], -grid[5]], report['resampling']['resolution'], rtol=1e-12)
    # Automatic bounds first round GDAL's diagonal-derived suggested grid,
    # then round again to the requested resolution; a half-target-cell bound
    # alone incorrectly rejects the legitimate extra edge cells.
    suggested_resolution = np.hypot(bounds[2] - bounds[0], bounds[3] - bounds[1]) / np.hypot(
        original.RasterXSize, original.RasterYSize)
    assert abs(output.RasterXSize * grid[1] - (bounds[2] - bounds[0])) <= (grid[1] + suggested_resolution) / 2 + 1e-7
    assert abs(output.RasterYSize * -grid[5] - (bounds[3] - bounds[1])) <= (-grid[5] + suggested_resolution) / 2 + 1e-7
    source_moments = {'count': 0, 'origin': None, 'sum': 0., 'squares': 0., 'min': np.inf, 'max': -np.inf}
    bins = np.zeros(256, dtype=np.int64)
    first_band = original.GetRasterBand(1)
    for row in range(0, original.RasterYSize, 127):
        height = min(127, original.RasterYSize - row)
        values = np.frombuffer(first_band.ReadRaster(0, row, original.RasterXSize, height,
                               buf_type=gdal.GDT_Float64), dtype=np.float64)
        mask = np.frombuffer(first_band.GetMaskBand().ReadRaster(0, row, original.RasterXSize,
                            height, buf_type=gdal.GDT_Byte), dtype=np.uint8) != 0
        selected = values[mask & np.isfinite(values) & (values != first_band.GetNoDataValue())]
        _profile_moments(source_moments, selected)
        bins += np.histogram(selected, bins=report['histogram']['edges'])[0]
    _assert_profile_moments(report['statistics'], source_moments, original.RasterXSize * original.RasterYSize)
    assert report['histogram']['counts'] == bins.tolist()
    minimum, maximum = source_moments['min'], source_moments['max']
    value_bounds = [minimum, maximum] if minimum < maximum else [minimum - .5, maximum + .5]
    np.testing.assert_array_equal(report['histogram']['edges'], np.linspace(*value_bounds, 257))
    assert (report['histogram']['valid_count'], report['histogram']['invalid_count'], report['histogram']['outside_count']) == (
        source_moments['count'], original.RasterXSize * original.RasterYSize - source_moments['count'], 0)
    counts = []
    for band_index in range(1, original.RasterCount + 1):
        source_band, output_band = original.GetRasterBand(band_index), output.GetRasterBand(band_index)
        assert source_band.DataType == output_band.DataType
        nodata = source_band.GetNoDataValue()
        assert nodata is not None and np.isfinite(nodata)
        assert output_band.GetNoDataValue() == nodata
        count = 0
        output_moments = {'count': 0, 'origin': None, 'sum': 0., 'squares': 0., 'min': np.inf, 'max': -np.inf}
        for row in range(0, output.RasterYSize, 127):
            height = min(127, output.RasterYSize - row)
            longitude, latitude = _mercator_inverse(
                grid[0] + (np.arange(output.RasterXSize) + .5) * grid[1],
                grid[3] + (np.arange(row, row + height) + .5) * grid[5])
            columns = np.floor((longitude - source_grid[0]) / source_grid[1]).astype(int)
            rows = np.floor((latitude - source_grid[3]) / source_grid[5]).astype(int)
            covered = ((rows[:, None] >= 0) & (rows[:, None] < original.RasterYSize)
                       & (columns[None, :] >= 0) & (columns[None, :] < original.RasterXSize))
            rows = np.clip(rows, 0, original.RasterYSize - 1)
            columns = np.clip(columns, 0, original.RasterXSize - 1)
            first, end = int(rows.min()), int(rows.max()) + 1
            values = np.frombuffer(source_band.ReadRaster(0, first, original.RasterXSize, end - first,
                                   buf_type=gdal.GDT_Float64), dtype=np.float64).reshape(end - first, original.RasterXSize)
            masks = np.frombuffer(source_band.GetMaskBand().ReadRaster(0, first, original.RasterXSize,
                                  end - first, buf_type=gdal.GDT_Byte), dtype=np.uint8).reshape(values.shape)
            expected = values[rows[:, None] - first, columns[None, :]]
            valid = covered & (masks[rows[:, None] - first, columns[None, :]] != 0) & np.isfinite(expected) & (expected != nodata)
            actual = np.frombuffer(output_band.ReadRaster(0, row, output.RasterXSize, height,
                                  buf_type=gdal.GDT_Float64), dtype=np.float64).reshape(height, output.RasterXSize)
            actual_mask = np.frombuffer(output_band.GetMaskBand().ReadRaster(0, row, output.RasterXSize,
                                       height, buf_type=gdal.GDT_Byte), dtype=np.uint8).reshape(actual.shape) != 0
            np.testing.assert_array_equal(actual_mask, valid)
            np.testing.assert_array_equal(actual[valid], expected[valid])
            assert (actual[~valid] == nodata).all()
            count += int(valid.sum())
            _profile_moments(output_moments, actual[valid])
        counts.append(count)
        if band_index == 1:
            _assert_profile_moments(report['output_statistics'], output_moments, output.RasterXSize * output.RasterYSize)
    report['output_band_valid_counts'] = counts
    original = output = None


def _directory_profile_source(source, root):
    root.mkdir()
    dataset = gdal.Open(str(source))
    width, height = dataset.RasterXSize, dataset.RasterYSize
    x_edges, y_edges = [0, width // 2, width], [0, height // 2, height]
    for y in range(2):
        for x in range(2):
            tile = gdal.Translate(str(root / f'{y}{x}.tif'), dataset, format='GTiff',
                                  srcWin=[x_edges[x], y_edges[y], x_edges[x + 1] - x_edges[x], y_edges[y + 1] - y_edges[y]],
                                  creationOptions=['TILED=NO', 'COMPRESS=DEFLATE'])
            assert tile is not None
            tile = None
    dataset = None
    return root


def _directory_mosaic_profile_task(source, target, epoch, barrier):
    if barrier is not None:
        barrier.wait(timeout=30)
    started = time.perf_counter()
    total_pixels = 0
    for tile in sorted(source.glob('*.tif')):
        dataset = gdal.Open(str(tile))
        total_pixels += dataset.RasterXSize * dataset.RasterYSize
        dataset = None
    result = build_raster_mosaic(access_plan={
        'source': {'root_uri': str(source), 'recursive': True, 'include_patterns': ['*.tif']},
        'target': {'dataset_root_uri': str(target), 'dataset_name': target.name}},
        placement={'mode': 'detached'},
        cog={'compression': 'DEFLATE', 'blocksize': 512, 'overview_resampling': 'NEAREST',
             'leaf_concurrency': 4, 'num_threads': 2},
        overview={'enabled': True, 'max_pixels': total_pixels, 'resampling': 'NEAREST'},
        tiles={'enabled': False})
    finished = time.perf_counter()
    return {'profile_case': 'directory_mosaic', 'mosaic': result, 'thread_id': threading.get_ident(),
            'started_seconds': started - epoch, 'finished_seconds': finished - epoch,
            'elapsed_seconds': finished - started,
            'observed_file_bytes': sum(p.stat().st_size for p in target.rglob('*') if p.is_file())}


def _assert_nearest_profile_overviews(dataset):
    # Compare every pyramid sample with the independently established base grid.
    for band_index in range(1, dataset.RasterCount + 1):
        band = dataset.GetRasterBand(band_index)
        rows, columns = np.arange(band.YSize), np.arange(band.XSize)
        for level in range(band.GetOverviewCount()):
            overview = band.GetOverview(level)
            # NEAREST pyramid levels round top-left positions in the preceding grid.
            rows = rows[np.floor(np.arange(overview.YSize) * rows.size / overview.YSize + .5).astype(int)]
            columns = columns[np.floor(np.arange(overview.XSize) * columns.size / overview.XSize + .5).astype(int)]
            chunk = max(1, 127 * overview.YSize // band.YSize)
            for start in range(0, overview.YSize, chunk):
                selected = rows[start:start + chunk]
                first, end = int(selected.min()), int(selected.max()) + 1
                expected = np.frombuffer(band.ReadRaster(0, first, band.XSize, end - first,
                                        buf_type=gdal.GDT_Float64), dtype=np.float64).reshape(end - first, band.XSize)
                expected = expected[selected[:, None] - first, columns]
                actual = np.frombuffer(overview.ReadRaster(0, start, overview.XSize, selected.size,
                                      buf_type=gdal.GDT_Float64), dtype=np.float64).reshape(expected.shape)
                np.testing.assert_array_equal(actual, expected)


def _assert_profile_raster_copy(source, target):
    original, output = gdal.Open(str(source)), gdal.Open(str(target))
    assert (original.RasterXSize, original.RasterYSize, original.RasterCount) == (output.RasterXSize, output.RasterYSize, output.RasterCount)
    np.testing.assert_array_equal(original.GetGeoTransform(), output.GetGeoTransform())
    assert original.GetSpatialRef().IsSame(output.GetSpatialRef())
    for index in range(1, original.RasterCount + 1):
        source_band, output_band = original.GetRasterBand(index), output.GetRasterBand(index)
        assert source_band.DataType == output_band.DataType
        assert source_band.GetNoDataValue() == output_band.GetNoDataValue()
        for row in range(0, original.RasterYSize, 127):
            height = min(127, original.RasterYSize - row)
            for first_band, second_band, dtype in ((source_band, output_band, gdal.GDT_Float64),
                                                   (source_band.GetMaskBand(), output_band.GetMaskBand(), gdal.GDT_Byte)):
                assert first_band.ReadRaster(0, row, original.RasterXSize, height, buf_type=dtype) == second_band.ReadRaster(
                    0, row, output.RasterXSize, height, buf_type=dtype)
    _assert_nearest_profile_overviews(output)
    original = output = None
    with raster_workspace():
        assert validate_cog(raster_load(source_plan(target))) == {'valid': True, 'warnings': [], 'errors': []}


def _validate_directory_profile(source, directory, target, task):
    result = task['mosaic']
    assert result['status'] == 'success' and result['format'] == 'raster_mosaic'
    assert result['source_count'] == result['leaf_count'] == 4 and result['failed_count'] == 0
    assert result['stage_timings']['leaf_cog']['generated_count'] == 4
    assert result['stage_timings']['leaf_cog']['concurrency'] == 4
    manifest = json.loads((target / 'mosaic.addp.json').read_text())
    index = json.loads((target / 'index/source-index.json').read_text())
    assert manifest['format'] == 'raster_mosaic' and manifest['layout'] == 'whole'
    assert manifest['refs']['overview'] == 'overviews/overview.cog.tif'
    leaves = index['leaves']
    assert len(leaves) == 4
    expected_paths = {'mosaic.addp.json', 'index/source-index.json', 'overviews/overview.cog.tif'}
    for tile in sorted(directory.glob('*.tif')):
        relative = 'leaf/' + tile.stem + '.cog.tif'
        assert sum(leaf['leaf_ref'] == relative and leaf['cog_validation']['status'] == 'valid' for leaf in leaves) == 1
        _assert_profile_raster_copy(tile, target / relative)
        expected_paths.add(relative)
    _assert_profile_raster_copy(source, target / 'overviews/overview.cog.tif')
    actual = {p.relative_to(target).as_posix(): p for p in target.rglob('*') if p.is_file()}
    assert set(actual) == expected_paths
    task['artifact_sizes_bytes'] = {name: path.stat().st_size for name, path in actual.items()}
    assert all(size > 0 for size in task['artifact_sizes_bytes'].values())
    task['cog_sha256'] = {name: _profile_digest(path) for name, path in actual.items() if path.suffix == '.tif'}


def _validate_profile_result(source, target, report, profile_case, original_hash, default_source):
    assert report['workspace_removed']
    assert report['profile_case'] == profile_case
    assert report['validation'] == {'valid': True, 'warnings': [], 'errors': []}
    assert report['saved']['size_bytes'] == target.stat().st_size > 0
    if profile_case == 'multiband_reproject':
        _validate_reprojection_profile(source, target, report)
        assert _profile_digest(source) == original_hash
        return

    original = gdal.Open(str(source), gdal.GA_ReadOnly)
    output = gdal.Open(str(target), gdal.GA_ReadOnly)
    assert original.RasterCount == report['source']['band_count']
    assert output.RasterCount == 1
    assert all(not gdal.DataTypeIsComplex(original.GetRasterBand(i).DataType)
               for i in range(1, original.RasterCount + 1))
    assert all(original.GetRasterBand(i).GetColorInterpretation() != gdal.GCI_AlphaBand
               for i in range(1, original.RasterCount + 1))
    width, height = original.RasterXSize, original.RasterYSize
    output_width, output_height = ((width + 1) // 2, (height + 1) // 2) if profile_case == 'average_resample' else (width, height)
    assert (output.RasterXSize, output.RasterYSize) == (output_width, output_height)
    assert report['resampling'] == ({'algorithm': 'average', 'size': [output_width, output_height]}
                                    if profile_case == 'average_resample' else None)
    transform = original.GetGeoTransform()
    if profile_case == 'band_math':
        assert transform == output.GetGeoTransform()
    np.testing.assert_allclose(output.GetGeoTransform(),
                               [transform[0], transform[1] * width / output_width,
                                transform[2] * height / output_height, transform[3],
                                transform[4] * width / output_width, transform[5] * height / output_height],
                               rtol=1e-12, atol=1e-12)
    assert original.GetSpatialRef().IsSame(output.GetSpatialRef())
    output_band = output.GetRasterBand(1)
    assert np.isnan(output_band.GetNoDataValue())
    assert output_band.DataType == gdal.GDT_Float64
    assert output_band.GetOverviewCount() > 0
    total = original.RasterXSize * original.RasterYSize
    # Centered moments use a fixed sample origin rather than the operator's block-merge algorithm.
    moments = [{'count': 0, 'origin': None, 'sum': 0.0, 'squares': 0.0,
                'min': np.inf, 'max': -np.inf} for _ in range(2)]
    band_counts = [0] * original.RasterCount
    bins = np.zeros(256, dtype=np.int64)
    # Different block shape and independent GDAL/NumPy reads avoid reusing the operator's oracle.
    for y in range(0, original.RasterYSize, 127):
        height = min(127, original.RasterYSize - y)
        expected, joint_valid, counts, selected_source = _profile_values(original, y, height)
        band_counts = [count + block_count for count, block_count in zip(band_counts, counts)]
        bins += np.histogram(selected_source, bins=report['histogram']['edges'])[0]
        _profile_moments(moments[0], selected_source)
        if profile_case == 'band_math':
            actual = np.frombuffer(output_band.ReadRaster(0, y, original.RasterXSize, height,
                                  buf_type=gdal.GDT_Float64), dtype=np.float64).reshape(expected.shape)
            np.testing.assert_array_equal(actual[joint_valid], expected[joint_valid])
            assert np.isnan(actual[~joint_valid]).all()
            output_valid = np.frombuffer(output_band.GetMaskBand().ReadRaster(0, y, original.RasterXSize, height,
                                        buf_type=gdal.GDT_Byte), dtype=np.uint8).reshape(expected.shape) != 0
            np.testing.assert_array_equal(output_valid, joint_valid)
            _profile_moments(moments[1], expected[joint_valid])
    if profile_case == 'average_resample':
        for y, expected in _profile_average_blocks(original, output_width, output_height):
            block_height = expected.shape[0]
            actual = np.frombuffer(output_band.ReadRaster(0, y, output_width, block_height,
                                  buf_type=gdal.GDT_Float64), dtype=np.float64).reshape(expected.shape)
            np.testing.assert_allclose(actual, expected, rtol=1e-8, atol=1e-7, equal_nan=True)
            output_valid = np.frombuffer(output_band.GetMaskBand().ReadRaster(0, y, output_width, block_height,
                                        buf_type=gdal.GDT_Byte), dtype=np.uint8).reshape(expected.shape) != 0
            np.testing.assert_array_equal(output_valid, np.isfinite(expected))
            _profile_moments(moments[1], expected[np.isfinite(expected)])
    for stats, accumulator, pixel_count in zip((report['statistics'], report['output_statistics']), moments,
                                               (total, output_width * output_height)):
        count = accumulator['count']
        assert count > 0
        centered_mean = accumulator['sum'] / count
        mean = accumulator['origin'] + centered_mean
        stddev = np.sqrt(max(0, accumulator['squares'] / count - centered_mean ** 2))
        assert (stats['valid_count'], stats['invalid_count']) == (count, pixel_count - count)
        np.testing.assert_allclose([stats['min'], stats['max'], stats['mean'], stats['stddev']],
                                   [accumulator['min'], accumulator['max'], mean, stddev],
                                   rtol=1e-8, atol=1e-8)
    count = moments[0]['count']
    minimum, maximum = moments[0]['min'], moments[0]['max']
    report['source_band_valid_counts'] = band_counts
    if default_source and profile_case == 'band_math':
        assert moments[1]['count'] < min(band_counts)
    histogram = report['histogram']
    assert histogram['counts'] == bins.tolist()
    assert (histogram['valid_count'], histogram['invalid_count'], histogram['outside_count']) == (count, total - count, 0)
    bounds = [minimum, maximum] if minimum < maximum else [minimum - 0.5, maximum + 0.5]
    np.testing.assert_array_equal(histogram['edges'], np.linspace(*bounds, 257))
    output_band = original = output = None
    assert _profile_digest(source) == original_hash


@pytest.mark.parametrize('profile_case', ['band_math', 'average_resample'])
def test_multiblock_cog_roundtrip_profile(tmp_path, profile_case):
    """T1 correctness gate; an external TIFF uses the same path for local profiling."""
    source, default_source = _profile_source(tmp_path)
    original_hash = _profile_digest(source)
    target = tmp_path / 'profile-result.tif'
    batch = _run_raster_profile(source, [target], tmp_path, profile_case, 'serial')
    report = {**batch['tasks'][0], **{key: batch[key] for key in
              ('environment', 'process_cpu_seconds', 'process_peak_rss_bytes')}}
    _validate_profile_result(source, target, report, profile_case, original_hash, default_source)
    print('RASTER_PROFILE ' + json.dumps({'source_sha256': original_hash,
          'source_size_bytes': source.stat().st_size, **report}, sort_keys=True))


@pytest.mark.parametrize('execution_mode', ['serial', 'parallel'])
def test_same_process_raster_pair_profile(tmp_path, execution_mode):
    """Two complete chains share the process cache, with independent files and full oracles."""
    source, default_source = _profile_source(tmp_path)
    original_hash = _profile_digest(source)
    targets = [tmp_path / f'profile-result-{index}.tif' for index in range(2)]
    report = _run_raster_profile(source, targets, tmp_path, 'average_resample', execution_mode)
    first, second = report['tasks']
    assert first['workspace'] != second['workspace']
    _profile_pair_metrics(report)
    for target, task in zip(targets, report['tasks']):
        _validate_profile_result(source, target, task, 'average_resample', original_hash, default_source)
        assert not Path(task['workspace']).exists()
        task['output_sha256'] = _profile_digest(target)
    assert first['output_sha256'] == second['output_sha256']
    assert _profile_digest(source) == original_hash
    print('RASTER_PAIR_PROFILE ' + json.dumps({'source_sha256': original_hash,
          'source_size_bytes': source.stat().st_size, **report}, sort_keys=True))


def _profile_pair_metrics(report):
    assert report['native_fault_handler_enabled']
    first, second = report['tasks']
    overlap = min(task['finished_seconds'] for task in report['tasks']) - max(
        task['started_seconds'] for task in report['tasks'])
    if report['execution_mode'] == 'parallel':
        assert first['thread_id'] != second['thread_id']
        assert overlap > 0
    else:
        assert first['finished_seconds'] <= second['started_seconds']
    report['task_overlap_seconds'] = max(0, overlap)
    report['throughput_tasks_per_second'] = len(report['tasks']) / report['elapsed_seconds']


def test_profile_mercator_inverse_hand_calculated_anchors():
    np.testing.assert_allclose(_mercator_inverse(np.array([0, np.pi * 6378137]),
                               np.array([0, 6378137 * np.log(1 + np.sqrt(2))])), [[0, 180], [0, 45]],
                               rtol=0, atol=1e-12)


@pytest.mark.parametrize('execution_mode', ['serial', 'parallel'])
def test_multiband_reprojection_pair_profile(tmp_path, execution_mode):
    source, default_source = _profile_source(tmp_path, 'multiband_reproject')
    original_hash = _profile_digest(source)
    targets = [tmp_path / f'profile-result-{index}.tif' for index in range(2)]
    report = _run_raster_profile(source, targets, tmp_path, 'multiband_reproject', execution_mode)
    _profile_pair_metrics(report)
    first, second = report['tasks']
    assert first['workspace'] != second['workspace']
    for target, task in zip(targets, report['tasks']):
        _validate_profile_result(source, target, task, 'multiband_reproject', original_hash, default_source)
        assert not Path(task['workspace']).exists()
        task['output_sha256'] = _profile_digest(target)
    assert first['output_sha256'] == second['output_sha256']
    assert _profile_digest(source) == original_hash
    print('RASTER_PAIR_PROFILE ' + json.dumps({'source_sha256': original_hash,
          'source_size_bytes': source.stat().st_size, **report}, sort_keys=True))


@pytest.mark.parametrize('fault', ['value', 'mask'])
def test_reprojection_profile_oracle_rejects_nonfirst_band_corruption(tmp_path, fault):
    source, _ = _profile_source(tmp_path, 'multiband_reproject')
    target = tmp_path / 'original.tif'
    report = _run_raster_profile(source, [target], tmp_path, 'multiband_reproject', 'serial')['tasks'][0]
    _validate_reprojection_profile(source, target, report)
    tampered = tmp_path / 'tampered.tif'
    copied = gdal.Translate(str(tampered), str(target), format='GTiff')
    band = copied.GetRasterBand(copied.RasterCount)
    mask = np.frombuffer(band.GetMaskBand().ReadRaster(buf_type=gdal.GDT_Byte), dtype=np.uint8).reshape(
        copied.RasterYSize, copied.RasterXSize)
    y, x = np.argwhere(mask != 0)[0]
    value = np.frombuffer(band.ReadRaster(int(x), int(y), 1, 1, buf_type=gdal.GDT_Float64), dtype=np.float64)[0]
    band.WriteRaster(int(x), int(y), 1, 1, np.array([value + 123 if fault == 'value' else band.GetNoDataValue()],
                     dtype=np.float64).tobytes(), buf_type=gdal.GDT_Float64)
    band = copied = None
    with pytest.raises(AssertionError):
        _validate_reprojection_profile(source, tampered, report)


@pytest.mark.parametrize('fault', ['value', 'nodata'])
def test_nearest_overview_oracle_rejects_tampered_level(tmp_path, fault):
    values = np.arange(512 * 512).reshape(512, 512)
    original = create_raster(tmp_path / 'source.tif', np.stack([values, values + 7]))
    path = tmp_path / 'pyramid.tif'
    dataset = gdal.Translate(str(path), str(original), format='GTiff', creationOptions=['TILED=YES'])
    assert dataset.BuildOverviews('NEAREST', [2, 4]) == 0
    _assert_nearest_profile_overviews(dataset)
    band = dataset.GetRasterBand(2).GetOverview(0)
    band.WriteRaster(0, 0, 1, 1, np.array([-9999 if fault == 'nodata' else 123456],
                     dtype=np.float64).tobytes(), buf_type=gdal.GDT_Float64)
    band.FlushCache()
    with pytest.raises(AssertionError):
        _assert_nearest_profile_overviews(dataset)
    dataset = None


@pytest.mark.parametrize('downsample', [False, True])
def test_directory_mosaic_overviews_use_global_base_grid(tmp_path, downsample):
    # Legitimate AVERAGE source pyramids must not determine a NEAREST global grid.
    rows, columns = np.indices((514, 514))
    values = (rows * 37 + columns * 13) % 30001
    original = create_raster(tmp_path / 'original.tif', np.stack([values, values + 101]))
    source = tmp_path / 'source'
    source.mkdir()
    for y in range(2):
        for x in range(2):
            tile = gdal.Translate(str(source / f'{y}{x}.tif'), str(original), format='COG',
                                  srcWin=[x * 257, y * 257, 257, 257],
                                  creationOptions=['BLOCKSIZE=128', 'OVERVIEW_RESAMPLING=AVERAGE'])
            assert tile is not None and tile.GetRasterBand(1).GetOverviewCount() > 0
            tile = None
    target = tmp_path / 'mosaic'
    result = build_raster_mosaic(access_plan={
        'source': {'root_uri': str(source), 'recursive': True, 'include_patterns': ['*.tif']},
        'target': {'dataset_root_uri': str(target), 'dataset_name': target.name}},
        placement={'mode': 'detached'}, cog={'blocksize': 128, 'num_threads': 2},
        overview={'enabled': True, 'max_pixels': 257 * 257 if downsample else 514 * 514,
                  'resampling': 'NEAREST'}, tiles={'enabled': False})
    assert result['status'] == 'success'
    output = gdal.Open(str(target / 'overviews/overview.cog.tif'))
    assert (output.RasterXSize, output.RasterYSize) == ((257, 257) if downsample else (514, 514))
    for index in (1, 2):
        expected = values + (index - 1) * 101
        if downsample:
            expected = expected[1::2, 1::2]
        band = output.GetRasterBand(index)
        np.testing.assert_array_equal(read_band_values(band), expected)
    _assert_nearest_profile_overviews(output)
    output = None


@pytest.mark.parametrize('execution_mode', ['serial', 'parallel'])
def test_directory_mosaic_pair_profile(tmp_path, execution_mode):
    source, _ = _profile_source(tmp_path)
    original_hash = _profile_digest(source)
    directory = _directory_profile_source(source, tmp_path / 'tiles')
    source_hashes = {p.name: _profile_digest(p) for p in directory.glob('*.tif')}
    targets = [tmp_path / f'profile-mosaic-{index}' for index in range(2)]
    report = _run_raster_profile(directory, targets, tmp_path, 'directory_mosaic', execution_mode)
    _profile_pair_metrics(report)
    assert report['mosaic_vrts_removed']
    for target, task in zip(targets, report['tasks']):
        _validate_directory_profile(source, directory, target, task)
    assert report['tasks'][0]['cog_sha256'] == report['tasks'][1]['cog_sha256']
    assert _profile_digest(source) == original_hash
    assert source_hashes == {p.name: _profile_digest(p) for p in directory.glob('*.tif')}
    report['source_tile_sha256'] = source_hashes
    print('RASTER_PAIR_PROFILE ' + json.dumps({'source_sha256': original_hash,
          'source_size_bytes': source.stat().st_size, **report}, sort_keys=True))


@pytest.mark.parametrize('dtype,alpha,positions,counts,alpha_valid', [
    (gdal.GDT_Byte, [[0,255,128,0],[255,0,255,255]],
     [(0,1),(0,2),(1,0),(1,2),(1,3)], [2,1,2], 8),
    (gdal.GDT_UInt16, [[0,65535,1,0],[65535,0,65535,65535]],
     [(0,1),(0,2),(1,0),(1,2),(1,3)], [2,1,2], 8),
    (gdal.GDT_Float64, [[0,255,128,0],[255,0,255,255]],
     [(0,1),(0,2),(1,0),(1,2),(1,3)], [2,1,2], 8),
    (gdal.GDT_Float64, [[np.nan,1,0.5,0],[np.inf,-1,0,1]],
     [(0,1),(0,2),(1,3)], [2,0,1], 6),
], ids=['byte-alpha', 'uint16-alpha', 'float64-alpha', 'fractional-and-invalid-alpha'])
def test_numeric_analysis_excludes_transparent_source_data(tmp_path, dtype, alpha, positions, counts, alpha_valid):
    source = tmp_path / 'alpha.tif'
    data = np.array([[0,20,30,40],[50,60,70,80]], dtype=np.float64)
    alpha = np.array(alpha, dtype=np.float64)
    expected_values = np.array([data[y,x] for y,x in positions])
    dataset = gdal.GetDriverByName('GTiff').Create(str(source), 4,2,2,dtype)
    dataset.SetGeoTransform((0,1,0,2,0,-1))
    crs = osr.SpatialReference(); crs.ImportFromEPSG(4326)
    dataset.SetSpatialRef(crs)
    for index, array in enumerate([data,alpha],1):
        dataset.GetRasterBand(index).WriteRaster(0,0,4,2,array.tobytes(),buf_type=gdal.GDT_Float64)
    dataset.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand)
    dataset = None
    original = source.read_bytes()
    with raster_workspace():
        raster = raster_load(source_plan(source))
        stats = raster_statistics(raster)
        assert (stats['valid_count'], stats['invalid_count'], stats['min'], stats['max']) == (len(positions),8-len(positions),20,80)
        assert stats['mean'] == pytest.approx(expected_values.mean())
        histogram = raster_histogram(raster, bins=3, value_range=[20,80])
        assert histogram['counts'] == counts
        assert (histogram['valid_count'], histogram['invalid_count'], histogram['outside_count']) == (len(positions),8-len(positions),0)
        calculated = raster_band_math(raster, 'b1*2')
        assert raster_statistics(calculated)['valid_count'] == len(positions)
        target = tmp_path / 'math.tif'
        raster_save(calculated, target_plan(target), profile='cog')
        persisted = raster_load(source_plan(target))
        assert raster_statistics(persisted)['mean'] == pytest.approx(expected_values.mean()*2)
        # Alpha remains an explicit selectable band; its zero values are coverage facts.
        assert raster_statistics(raster, band=2)['valid_count'] == alpha_valid
    output = gdal.Open(str(target))
    expected = np.full(data.shape, np.nan)
    for y,x in positions:
        expected[y,x] = data[y,x]*2
    np.testing.assert_equal(read_band_values(output.GetRasterBand(1)), expected)
    assert source.read_bytes() == original


def test_math_is_safe_and_propagates_nodata(raster_file):
    with raster_workspace():
        raster = raster_load(source_plan(raster_file))
        result = raster_band_math(raster, '(b2-b1)/(b2+b1)')
        stats = raster_statistics(result)
        assert stats['valid_count'] == 15
        assert stats['mean'] == pytest.approx(1/3)
        reciprocal = raster_band_math(raster, 'b1**-1')
        assert raster_statistics(reciprocal)['mean'] == pytest.approx(np.mean(1 / np.arange(2, 17)))
        invalid = raster_band_math(raster, 'b1/(b1-b1)')
        assert raster_statistics(invalid)['valid_count'] == 0
        for expression in ["__import__('os').system('id')", 'b1.__class__', 'b1[0]', '[b1]', 'b3', '1', 'abs(b1,b2)', 'True', 'b1**(10**1000)']:
            with pytest.raises((ValueError, OverflowError, SyntaxError)):
                raster_band_math(raster, expression)


def test_resampling_and_reprojection(raster_file):
    with raster_workspace():
        raster = raster_load(source_plan(raster_file))
        half = raster_resample(raster, size=[2, 2])
        facts = raster_info(half)
        assert facts['transform'] == [0, 2, 0, 4, 0, -2]
        assert (facts['width'], facts['height']) == (2, 2)
        assert raster_statistics(half)['mean'] == pytest.approx(11)
        warped = raster_reproject(raster, 'EPSG:3857')
        assert raster_info(warped)['extent_srid'] == 3857
        assert raster_info(warped)['extent'][2] > 400000
        for kwargs in [{}, {'size': [2,2], 'resolution': [1,1]}, {'size': [True,2]}, {'resolution':[0,1]}]:
            with pytest.raises(ValueError): raster_resample(raster, **kwargs)


@pytest.mark.parametrize('transform,crs', [
    (None,''), (None,'EPSG:4326'), ((10,2,.5,20,.25,-3),''),
    ((10,2,.5,20,.25,-3),'EPSG:4326'),
], ids=['no-georeferencing','crs-only','transform-only','rotated-georeferencing'])
@pytest.mark.parametrize('algorithm', ['nearest','bilinear','average'])
@pytest.mark.parametrize('profile', ['geotiff','cog'])
@pytest.mark.parametrize('size', [[6,4],[6,2]], ids=['uniform-scale','unequal-scale'])
def test_size_resampling_preserves_missing_facts_and_rotated_footprint(tmp_path,transform,crs,algorithm,profile,size):
    values = np.array([[0,30,60],[90,120,150]],dtype=float)
    width,height = size
    xscale,yscale = 3/width,2/height
    source = create_raster(tmp_path/'source.tif',values,transform=transform,crs=crs)
    original = source.read_bytes()
    expected = np.repeat(np.repeat(values,height//2,axis=0),width//3,axis=1)
    if algorithm=='bilinear':
        sy,sx = np.indices(values.shape)
        for row in range(height):
            for column in range(width):
                weights = np.maximum(0,1-np.abs(sx+.5-(column+.5)*xscale))*np.maximum(0,1-np.abs(sy+.5-(row+.5)*yscale))
                expected[row,column] = np.sum(weights*values)/weights.sum()
    target = tmp_path/'result.tif'
    with raster_workspace():
        raster = raster_load(source_plan(source))
        workspace = raster.workspace
        result = raster_resample(raster,size=size,resampling=algorithm)
        facts = raster_info(result)
        assert (facts['width'],facts['height'])==(width,height)
        assert facts['crs']==raster_info(raster)['crs']
        if transform is None:
            assert facts['transform']==[] and facts['extent']==[]
        else:
            np.testing.assert_allclose(facts['transform'],[transform[0],transform[1]*xscale,transform[2]*yscale,
                                                         transform[3],transform[4]*xscale,transform[5]*yscale])
            np.testing.assert_allclose(facts['extent'],raster_info(raster)['extent'])
            output = gdal.Open(str(result.path))
            for x,y in [(0,0),(3,0),(3,2),(0,2)]:
                np.testing.assert_allclose(gdal.ApplyGeoTransform(output.GetGeoTransform(),x/xscale,y/yscale),
                                           gdal.ApplyGeoTransform(transform,x,y))
            output = None
        raster_save(result,target_plan(target),profile=profile,blocksize=128)
        persisted = raster_load(source_plan(target))
        saved_facts = raster_info(persisted)
        assert saved_facts['crs']==facts['crs'] and saved_facts['transform']==facts['transform']
        assert saved_facts['extent']==facts['extent']
        output = gdal.Open(str(persisted.path))
        np.testing.assert_allclose(read_band_values(output.GetRasterBand(1)),expected)
        output = None
        assert raster_statistics(persisted)['valid_count']==width*height
        if not crs or transform is None:
            with pytest.raises(ValueError,match='CRS' if not crs else 'geotransform'):
                raster_resample(persisted,resolution=[1,1])
            with pytest.raises(ValueError,match='CRS' if not crs else 'geotransform'):
                raster_reproject(persisted,'EPSG:3857')
        if profile=='cog':
            assert validate_cog(persisted)['valid']
    assert not workspace.exists() and source.read_bytes()==original
    assert {path.name for path in tmp_path.iterdir()}=={'source.tif','result.tif'}


@pytest.mark.parametrize('format,driver,suffix', [('png','PNG','.png'),('jpeg','JPEG','.jpg')])
@pytest.mark.parametrize('source_crs', ['', 'EPSG:4326'])
@pytest.mark.parametrize('profile', ['geotiff','cog'])
def test_size_resampling_accepts_unlocated_images_without_fabricating_location(tmp_path,format,driver,suffix,source_crs,profile):
    pixels = np.array([[0,20,40,60],[80,100,120,140]],dtype=np.uint8)
    alpha = np.array([[0,255,128,255],[255,255,255,255]],dtype=np.uint8)
    memory = gdal.GetDriverByName('MEM').Create('',4,2,2 if format=='png' else 1,gdal.GDT_Byte)
    memory.GetRasterBand(1).WriteRaster(0,0,4,2,pixels.tobytes())
    if format=='png':
        memory.GetRasterBand(2).WriteRaster(0,0,4,2,alpha.tobytes())
        memory.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand)
    source = tmp_path/('source'+suffix)
    encoded = gdal.GetDriverByName(driver).CreateCopy(str(source),memory)
    encoded = memory = None
    original = source.read_bytes()
    decoded = gdal.Open(str(source))
    expected = np.repeat(np.repeat(read_band_values(decoded.GetRasterBand(1)),2,axis=0),2,axis=1)
    valid = np.repeat(np.repeat(alpha>0,2,axis=0),2,axis=1) if format=='png' else np.ones(expected.shape,dtype=bool)
    decoded = None
    target = tmp_path/'result.tif'
    with raster_workspace():
        raster = raster_load(source_plan(source,format),source_crs=source_crs)
        workspace = raster.workspace
        resized = raster_resample(raster,size=[8,4])
        raster_save(resized,target_plan(target),profile=profile,blocksize=128)
        persisted = raster_load(source_plan(target))
        facts = raster_info(persisted)
        assert (facts['width'],facts['height'])==(8,4)
        assert facts['transform']==[] and facts['extent']==[]
        assert facts['source_crs']==source_crs
        output = gdal.Open(str(persisted.path))
        np.testing.assert_equal(read_band_values(output.GetRasterBand(1))[valid],expected[valid])
        assert output.GetRasterBand(1).DataType==gdal.GDT_Byte
        if format=='png':
            np.testing.assert_equal(read_band_values(output.GetRasterBand(2)),np.repeat(np.repeat(alpha,2,axis=0),2,axis=1))
        output = None
        statistics = raster_statistics(persisted)
        assert (statistics['valid_count'],statistics['invalid_count'])==(np.count_nonzero(valid),np.count_nonzero(~valid))
        assert statistics['mean']==pytest.approx(expected[valid].mean())
        if profile=='cog':
            assert validate_cog(persisted)['valid']
    assert not workspace.exists() and source.read_bytes()==original
    assert {path.name for path in tmp_path.iterdir()}=={source.name,'result.tif'}


@pytest.mark.parametrize('dtype,opaque,nodata,fill', [
    (gdal.GDT_Byte,255,250,200), (gdal.GDT_UInt16,65535,65000,60000),
    (gdal.GDT_Float64,255,np.nan,1e6),
])
@pytest.mark.parametrize('algorithm', ['nearest','average'])
@pytest.mark.parametrize('profile', ['geotiff','cog'])
def test_size_resampling_preserves_data_type_alpha_validity_and_legal_zero(tmp_path,dtype,opaque,nodata,fill,algorithm,profile):
    values = np.repeat(np.repeat([[0,20],[30,fill]],2,axis=0),2,axis=1).astype(float)
    alpha = np.repeat(np.repeat([[opaque,opaque//2],[opaque,0]],2,axis=0),2,axis=1).astype(float)
    source = tmp_path/'source.tif'
    dataset = gdal.GetDriverByName('GTiff').Create(str(source),4,4,2,dtype)
    for index,array in enumerate([values,alpha],1):
        band = dataset.GetRasterBand(index)
        band.WriteRaster(0,0,4,4,array.tobytes(),buf_type=gdal.GDT_Float64)
    dataset.GetRasterBand(1).SetNoDataValue(nodata)
    dataset.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand)
    dataset = band = None
    original = source.read_bytes()
    target = tmp_path/'result.tif'
    with raster_workspace():
        raster = raster_load(source_plan(source))
        workspace = raster.workspace
        result = raster_resample(raster,size=[2,2],resampling=algorithm)
        raster_save(result,target_plan(target),profile=profile,blocksize=128)
        persisted = raster_load(source_plan(target))
        output = gdal.Open(str(persisted.path))
        np.testing.assert_equal(read_band_values(output.GetRasterBand(1))[[0,0,1],[0,1,0]],[0,20,30])
        # Average emits opaque coverage when contributors exist; nearest retains density.
        expected_alpha = [[opaque,opaque//2 if algorithm=='nearest' else opaque],[opaque,0]]
        np.testing.assert_equal(read_band_values(output.GetRasterBand(2)),expected_alpha)
        assert output.GetRasterBand(1).DataType==dtype
        output = None
        statistics = raster_statistics(persisted)
        assert (statistics['valid_count'],statistics['invalid_count'],statistics['min'],statistics['max'])==(3,1,0,30)
        assert statistics['mean']==pytest.approx(50/3)
        histogram = raster_histogram(persisted,bins=3,value_range=[0,30])
        assert histogram['counts']==[1,0,2] and histogram['outside_count']==0
        calculated = raster_band_math(persisted,'b1+1')
        output = gdal.Open(str(calculated.path))
        np.testing.assert_equal(read_band_values(output.GetRasterBand(1)),[[1,21],[31,np.nan]])
        output = None
        if profile=='cog':
            assert validate_cog(persisted)['valid']
    assert not workspace.exists() and source.read_bytes()==original
    assert {path.name for path in tmp_path.iterdir()}=={'source.tif','result.tif'}


@pytest.mark.parametrize('operator', ['resize','resample','reproject'])
@pytest.mark.parametrize('nodata', [-9999,float('nan')], ids=['finite-nodata','nan-nodata'])
@pytest.mark.parametrize('opaque', [255,128], ids=['opaque','partial-alpha'])
@pytest.mark.parametrize('profile', ['geotiff','cog'])
@pytest.mark.parametrize('algorithm', ['nearest','average'])
def test_multiband_warp_keeps_independent_nodata_with_shared_alpha(tmp_path,operator,nodata,opaque,profile,algorithm):
    first = np.array([[0,nodata,10,1e6],[20,30,nodata,nodata]],dtype=np.float64)
    second = np.array([[5,15,nodata,1e6],[nodata,40,nodata,nodata]],dtype=np.float64)
    alpha = np.full(first.shape,opaque,dtype=np.float64)
    alpha[0,3] = 0
    # Uniform 2x2 cells give the same independent nearest and area-average values.
    source_values = np.stack([np.repeat(np.repeat(array,2,axis=0),2,axis=1)
                              for array in [first,second,alpha]])
    source = create_raster(tmp_path/'source.tif',source_values,transform=(0,1,0,4,0,-1),nodata=nodata)
    dataset = gdal.Open(str(source),gdal.GA_Update)
    dataset.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
    dataset = None
    original = source.read_bytes()
    expected = np.stack([first,second])
    expected[:,alpha==0] = nodata
    valid = np.isfinite(expected)&(expected!=nodata)
    target = tmp_path/'result.tif'
    with raster_workspace():
        raster = raster_load(source_plan(source))
        workspace = raster.workspace
        if operator=='resize':
            result = raster_resample(raster,size=[4,2],resampling=algorithm)
        elif operator=='resample':
            result = raster_resample(raster,resolution=[2,2],resampling=algorithm)
        else:
            result = raster_reproject(raster,'EPSG:4326',[2,2],resampling=algorithm)
        output = gdal.Open(str(result.path))
        assert (output.RasterXSize,output.RasterYSize,output.RasterCount)==(4,2,3)
        np.testing.assert_allclose(output.GetGeoTransform(),[0,2,0,4,0,-2])
        for index in (1,2):
            band = output.GetRasterBand(index)
            declared_nodata = band.GetNoDataValue()
            assert declared_nodata is not None
            assert np.isnan(declared_nodata) if np.isnan(nodata) else declared_nodata==nodata
            actual = read_band_values(band)
            np.testing.assert_equal(actual,expected[index-1])
        np.testing.assert_equal(read_band_values(output.GetRasterBand(3))>0,valid.any(axis=0))
        output = band = None
        raster_save(result,target_plan(target),profile=profile,blocksize=128)
        persisted = raster_load(source_plan(target))
        for index in (1,2):
            stats = raster_statistics(persisted,band=index)
            values = expected[index-1][valid[index-1]]
            assert (stats['valid_count'],stats['invalid_count'])==(values.size,8-values.size)
            np.testing.assert_allclose([stats['min'],stats['max'],stats['mean'],stats['stddev']],
                                       [values.min(),values.max(),values.mean(),values.std()])
        calculated = raster_band_math(persisted,'b1+b2')
        output = gdal.Open(str(calculated.path))
        calculated_expected = np.full(first.shape,np.nan)
        joint = valid.all(axis=0)
        calculated_expected[joint] = expected[0,joint]+expected[1,joint]
        np.testing.assert_equal(read_band_values(output.GetRasterBand(1)),calculated_expected)
        output = None
        assert raster_statistics(calculated)['valid_count']==2
        assert raster_histogram(calculated,bins=2,value_range=[0,80])['counts']==[1,1]
        if profile=='cog':
            assert validate_cog(persisted)['valid']
    assert not workspace.exists() and source.read_bytes()==original
    assert {path.name for path in tmp_path.iterdir()}=={'source.tif','result.tif'}


@pytest.mark.parametrize('nodata', [-9999,float('nan')], ids=['finite-nodata','nan-nodata'])
@pytest.mark.parametrize('operator', ['resize','resample','reproject'])
@pytest.mark.parametrize('opaque', [255,128], ids=['opaque','partial-alpha'])
@pytest.mark.parametrize('profile', ['geotiff','cog'])
def test_multiband_bilinear_uses_each_bands_valid_contributors(tmp_path,nodata,operator,opaque,profile):
    values = np.array([[[0,10,30,1e6],[20,nodata,50,1e6],[70,80,100,1e6],[1e6]*4],
                       [[nodata,15,35,1e6],[25,45,55,1e6],[75,85,nodata,1e6],[1e6]*4]],dtype=float)
    alpha = np.zeros((4,4))
    alpha[:3,:3] = opaque
    source = create_raster(tmp_path/'source.tif',np.concatenate([values,alpha[None,...]]),nodata=nodata)
    dataset = gdal.Open(str(source),gdal.GA_Update)
    dataset.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
    dataset = None
    original = source.read_bytes()
    valid = np.isfinite(values)&(values!=nodata)&(alpha>0)
    expected = np.full((2,8,8),np.nan)
    source_y,source_x = np.indices((4,4))
    for row in range(8):
        for column in range(8):
            center_x,center_y = (column+.5)/2,(row+.5)/2
            weights = np.maximum(0,1-np.abs(source_x+.5-center_x))*np.maximum(0,1-np.abs(source_y+.5-center_y))
            for index in range(2):
                if not valid[index,int(center_y),int(center_x)]:
                    continue
                contributors = valid[index]&(weights>0)
                expected[index,row,column] = np.sum(values[index,contributors]*weights[contributors])/weights[contributors].sum()
    with raster_workspace():
        raster = raster_load(source_plan(source))
        workspace = raster.workspace
        if operator=='resize':
            result = raster_resample(raster,size=[8,8],resampling='bilinear')
        elif operator=='resample':
            result = raster_resample(raster,resolution=[.5,.5],resampling='bilinear')
        else:
            result = raster_reproject(raster,'EPSG:4326',[.5,.5],resampling='bilinear')
        raster_save(result,target_plan(tmp_path/'result.tif'),profile=profile,blocksize=128)
        persisted = raster_load(source_plan(tmp_path/'result.tif'))
        output = gdal.Open(str(persisted.path))
        np.testing.assert_allclose(output.GetGeoTransform(),[0,.5,0,4,0,-.5])
        for index in (1,2):
            actual = read_band_values(output.GetRasterBand(index))
            if not np.isnan(nodata):
                actual[actual==nodata] = np.nan
            np.testing.assert_allclose(actual,expected[index-1],rtol=1e-7,atol=1e-8,equal_nan=True)
            assert raster_statistics(persisted,index)['valid_count']==np.count_nonzero(np.isfinite(expected[index-1]))
        output = None
        calculated = raster_band_math(persisted,'b1+b2')
        output = gdal.Open(str(calculated.path))
        np.testing.assert_allclose(read_band_values(output.GetRasterBand(1)),expected.sum(axis=0),rtol=1e-7,atol=1e-8,equal_nan=True)
        output = None
        if profile=='cog':
            assert validate_cog(persisted)['valid']
    assert not workspace.exists() and source.read_bytes()==original
    assert {path.name for path in tmp_path.iterdir()}=={'source.tif','result.tif'}


@pytest.mark.parametrize('nodata', [-9999,float('nan')], ids=['finite-nodata','nan-nodata'])
@pytest.mark.parametrize('overlap', ['first','last'])
@pytest.mark.parametrize('profile', ['geotiff','cog'])
def test_multiband_clip_mosaic_preserves_each_bands_missing_pixels(tmp_path,nodata,overlap,profile):
    left = np.array([[[0,nodata,20,1e6],[40,50,nodata,nodata]],
                     [[5,15,nodata,1e6],[45,nodata,65,nodata]]],dtype=np.float64)
    right = np.array([[[100,110,nodata,1e6],[nodata,150,160,nodata]],
                      [[105,nodata,125,1e6],[145,155,nodata,175]]],dtype=np.float64)
    # A valid bottom/right anchor keeps the requested union grid independent of
    # GDAL's removal of wholly blank target-aligned edge rows and columns.
    alpha = np.full((2,4),255.)
    alpha[0,3] = 0
    sources = []
    for name,values in [('left',left),('right',right)]:
        path = create_raster(tmp_path/(name+'.tif'),np.concatenate([values,alpha[None,...]]),
                             transform=(0,1,0,2,0,-1),nodata=nodata)
        dataset = gdal.Open(str(path),gdal.GA_Update)
        dataset.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
        dataset = None
        sources.append(path)
    originals = [path.read_bytes() for path in sources]
    for values in (left,right):
        values[(~np.isfinite(values))|(values==nodata)] = np.nan
        values[:,alpha==0] = np.nan
    preferred,fallback = (left,right) if overlap=='first' else (right,left)
    expected = np.where(np.isfinite(preferred),preferred,fallback)
    polygon = {'type':'Polygon','coordinates':[[[0,0],[4,0],[4,2],[0,2],[0,0]]]}
    with raster_workspace():
        rasters = [raster_load(source_plan(path)) for path in sources]
        workspace = rasters[0].workspace
        clipped = raster_clip(rasters[0],'EPSG:4326',geometry=polygon)
        output = gdal.Open(str(clipped.path))
        for index in (1,2):
            np.testing.assert_equal(read_band_values(output.GetRasterBand(index)),left[index-1])
        output = None
        mosaic = raster_mosaic([clipped, rasters[1]],'EPSG:4326',[1,1],overlap)
        raster_save(mosaic,target_plan(tmp_path/'result.tif'),profile=profile,blocksize=128)
        persisted = raster_load(source_plan(tmp_path/'result.tif'))
        output = gdal.Open(str(persisted.path))
        assert (output.RasterXSize,output.RasterYSize,output.RasterCount)==(4,2,3)
        for index in (1,2):
            np.testing.assert_equal(read_band_values(output.GetRasterBand(index)),expected[index-1])
            assert raster_statistics(persisted,index)['valid_count']==np.count_nonzero(np.isfinite(expected[index-1]))
        output = None
        computed = raster_band_math(persisted,'b1+b2')
        output = gdal.Open(str(computed.path))
        np.testing.assert_equal(read_band_values(output.GetRasterBand(1)),expected.sum(axis=0))
        output = None
        if profile=='cog':
            assert validate_cog(persisted)['valid']
    assert not workspace.exists()
    assert [path.read_bytes() for path in sources]==originals
    assert {path.name for path in tmp_path.iterdir()}=={'left.tif','right.tif','result.tif'}


@pytest.mark.parametrize('algorithm,resolution,width,height', [
    ('bilinear',.5,12,8), ('average',1.5,4,3),
])
@pytest.mark.parametrize('operator', ['resample','resize','reproject'])
@pytest.mark.parametrize('nodata', [-9999,float('nan')], ids=['finite-nodata','nan-nodata'])
@pytest.mark.parametrize('opaque', [255,128], ids=['opaque','partial-alpha'])
@pytest.mark.parametrize('profile', ['geotiff','cog'])
def test_interpolation_excludes_invalid_edge_pixels_and_preserves_saved_analysis(
        tmp_path, algorithm, resolution, width, height, operator, nodata, opaque, profile):
    values = np.array([[0,10,30,1e6,1e6,1e6],
                       [20,nodata,50,1e6,1e6,1e6],
                       [70,80,100,1e6,1e6,1e6],
                       [1e6,1e6,1e6,1e6,1e6,1e6]],dtype=np.float64)
    alpha = np.zeros(values.shape)
    alpha[:3,:3] = opaque
    source = create_raster(tmp_path/'source.tif',np.stack([values,alpha]),nodata=nodata)
    dataset = gdal.Open(str(source),gdal.GA_Update)
    dataset.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand)
    dataset = None
    original = source.read_bytes()
    valid_source = np.isfinite(values)&(values!=nodata)&(alpha>0)
    xres,yres = (values.shape[1]/width,values.shape[0]/height) if operator=='resize' else (resolution,resolution)
    expected = np.full((height,width),np.nan)
    source_y,source_x = np.indices(values.shape)
    for row in range(height):
        for column in range(width):
            if algorithm=='bilinear':
                center_x,center_y = (column+.5)*xres,(row+.5)*yres
                # Warp's bilinear kernel first requires a valid containing source cell.
                if not valid_source[int(center_y),int(center_x)]:
                    continue
                weights = np.maximum(0,1-np.abs(source_x+.5-center_x))*np.maximum(0,1-np.abs(source_y+.5-center_y))
            else:
                # Area overlap supplies the average weights, including fractional cells.
                weights = np.maximum(0,np.minimum(source_x+1,(column+1)*xres)-np.maximum(source_x,column*xres))*np.maximum(
                    0,np.minimum(source_y+1,(row+1)*yres)-np.maximum(source_y,row*yres))
            contributors = valid_source&(weights>0)
            if contributors.any():
                expected[row,column] = np.sum(values[contributors]*weights[contributors])/np.sum(weights[contributors])
    if algorithm=='average':
        anchor = [[70/11,310/11,np.nan,np.nan],[52,76,np.nan,np.nan],[220/3,280/3,np.nan,np.nan]] if operator=='resize' else [
            [7.5,30,np.nan,np.nan],[60,82.5,np.nan,np.nan],[np.nan]*4]
        np.testing.assert_allclose(expected,anchor,equal_nan=True)
    target = tmp_path/'result.tif'
    with raster_workspace():
        raster = raster_load(source_plan(source))
        workspace = raster.workspace
        if operator=='resize':
            result = raster_resample(raster,size=[width,height],resampling=algorithm)
        elif operator=='resample':
            result = raster_resample(raster,resolution=[resolution,resolution],resampling=algorithm)
        else:
            result = raster_reproject(raster,'EPSG:4326',[resolution,resolution],resampling=algorithm)
        output = gdal.Open(str(result.path))
        assert (output.RasterXSize,output.RasterYSize,output.RasterCount)==(width,height,2)
        np.testing.assert_allclose(output.GetGeoTransform(),[0,xres,0,4,0,-yres])
        assert output.GetSpatialRef().GetAuthorityCode(None)=='4326'
        band = output.GetRasterBand(1)
        actual = read_band_values(band)
        coverage = read_band_values(output.GetRasterBand(2))
        np.testing.assert_equal(coverage>0,np.isfinite(expected))
        np.testing.assert_allclose(actual[np.isfinite(expected)],expected[np.isfinite(expected)],rtol=1e-7,atol=1e-8)
        if band.GetNoDataValue() is not None:
            np.testing.assert_equal(actual[~np.isfinite(expected)],np.full(np.count_nonzero(~np.isfinite(expected)),band.GetNoDataValue()))
        output = band = None
        raster_save(result,target_plan(target),profile=profile,blocksize=128)
        persisted = raster_load(source_plan(target))
        statistics = raster_statistics(persisted)
        valid = expected[np.isfinite(expected)]
        assert (statistics['valid_count'],statistics['invalid_count'])==(valid.size,expected.size-valid.size)
        np.testing.assert_allclose([statistics['min'],statistics['max'],statistics['mean'],statistics['stddev']],
                                   [valid.min(),valid.max(),valid.mean(),valid.std()],rtol=1e-7,atol=1e-8)
        histogram = raster_histogram(persisted,bins=5,value_range=[0,100])
        counts,edges = np.histogram(valid,bins=5,range=(0,100))
        assert histogram['counts']==counts.tolist() and histogram['edges']==edges.tolist()
        assert histogram['outside_count']==0
        assert raster_statistics(persisted,band=2)['valid_count']==expected.size
        calculated = raster_band_math(persisted,'b1+1')
        output = gdal.Open(str(calculated.path))
        np.testing.assert_allclose(read_band_values(output.GetRasterBand(1)),expected+1,rtol=1e-7,atol=1e-8,equal_nan=True)
        output = None
        if profile=='cog':
            assert validate_cog(persisted)['valid']
    assert not workspace.exists()
    output = gdal.Open(str(target))
    np.testing.assert_equal(read_band_values(output.GetRasterBand(2))>0,np.isfinite(expected))
    np.testing.assert_allclose(read_band_values(output.GetRasterBand(1))[np.isfinite(expected)],valid,rtol=1e-7,atol=1e-8)
    output = None
    assert source.read_bytes()==original
    assert {path.name for path in tmp_path.iterdir()}=={'source.tif','result.tif'}


@pytest.mark.parametrize('dtype,opaque,nodata', [
    (gdal.GDT_Byte,255,250), (gdal.GDT_UInt16,65535,65000), (gdal.GDT_Float64,255,-9999),
], ids=['byte-alpha','uint16-alpha','float64-alpha'])
@pytest.mark.parametrize('overlap', ['first','last'])
@pytest.mark.parametrize('profile', ['geotiff','cog'])
def test_alpha_clip_reproject_mosaic_save_preserves_valid_pixels(
        tmp_path, dtype, opaque, nodata, overlap, profile):
    def write_source(path, data, alpha, transform, crs):
        dataset = gdal.GetDriverByName('GTiff').Create(str(path),6,4,2,dtype)
        dataset.SetGeoTransform(transform)
        reference = osr.SpatialReference(); reference.SetFromUserInput(crs)
        dataset.SetSpatialRef(reference)
        for index,array in enumerate([data,alpha],1):
            band = dataset.GetRasterBand(index)
            band.SetNoDataValue(nodata)
            band.WriteRaster(0,0,6,4,array.astype(np.float64).tobytes(),buf_type=gdal.GDT_Float64)
        dataset.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand)
        dataset = band = None
        return path

    values = np.arange(24,dtype=np.float64).reshape(4,6)
    values[1,4] = nodata
    alpha = np.full((4,6),opaque,dtype=np.float64)
    alpha[0,1] = alpha[3,5] = 0
    alpha[0,0] = opaque//2
    source = write_source(tmp_path/'source.tif',values,alpha,(0,1,0,4,0,-1),'EPSG:4326')
    clipped_expected = values.copy()
    clipped_expected[(alpha==0)|(values==nodata)] = np.nan
    clipped_expected[1:3,2:4] = np.nan
    polygon = {'type':'Polygon','coordinates':[
        [[0,0],[6,0],[6,4],[0,4],[0,0]],
        [[2,1],[2,3],[4,3],[4,1],[2,1]],
    ]}
    # Independent spherical Mercator formulas determine the target grid and source cells.
    radius = 6378137.0
    resolution = radius*np.pi/180
    north = radius*np.log(np.tan(np.pi/4+np.deg2rad(4)/2))
    target_transform = (0,resolution,0,north,0,-resolution)
    other_values = np.arange(24,dtype=np.float64).reshape(4,6)+100
    other_values[1,4] = nodata
    other_values[3,5] = 0
    other_alpha = np.full((4,6),opaque,dtype=np.float64)
    other_alpha[0,2] = other_alpha[2,0] = 0
    other_alpha[3,5] = opaque//2
    other = write_source(tmp_path/'other.tif',other_values,other_alpha,
                         (2*resolution,resolution,0,5*resolution,0,-resolution),'EPSG:3857')
    originals = {path:path.read_bytes() for path in [source,other]}
    target = tmp_path/'result.tif'
    with raster_workspace():
        raster = raster_load(source_plan(source))
        workspace = raster.workspace
        clipped = raster_clip(raster,'EPSG:4326',geometry=polygon)
        clipped_dataset = gdal.Open(str(clipped.path))
        assert clipped_dataset.RasterCount==2
        np.testing.assert_equal(read_band_values(clipped_dataset.GetRasterBand(1)),clipped_expected)
        clipped_alpha = clipped_dataset.GetRasterBand(2)
        assert clipped_alpha.GetColorInterpretation()==gdal.GCI_AlphaBand
        coverage = read_band_values(clipped_alpha)
        np.testing.assert_equal(coverage>0,np.isfinite(clipped_expected))
        assert 0<coverage[0,0]<coverage.max()
        clipped_dataset = clipped_alpha = None
        warped = raster_reproject(clipped,'EPSG:3857',[resolution,resolution])
        warped_dataset = gdal.Open(str(warped.path))
        assert (warped_dataset.RasterXSize,warped_dataset.RasterYSize,warped_dataset.RasterCount)==(6,4,2)
        np.testing.assert_allclose(warped_dataset.GetGeoTransform(),target_transform,rtol=0,atol=1e-7)
        assert warped_dataset.GetSpatialRef().GetAuthorityCode(None)=='3857'
        longitude = np.rad2deg((np.arange(6)+.5)*resolution/radius)
        latitude = np.rad2deg(np.arctan(np.sinh((north-(np.arange(4)+.5)*resolution)/radius)))
        source_x = np.floor(longitude).astype(int)
        source_y = np.floor(4-latitude).astype(int)
        warped_expected = clipped_expected[np.ix_(source_y,source_x)]
        np.testing.assert_equal(read_band_values(warped_dataset.GetRasterBand(1)),warped_expected)
        np.testing.assert_equal(read_band_values(warped_dataset.GetRasterBand(2))>0,np.isfinite(warped_expected))
        warped_dataset = None
        other_raster = raster_load(source_plan(other))
        mosaic = raster_mosaic([warped, other_raster],'EPSG:3857',[resolution,resolution],overlap)
        mosaic_dataset = gdal.Open(str(mosaic.path))
        width,height = mosaic_dataset.RasterXSize,mosaic_dataset.RasterYSize
        assert width==8 and height>=5
        np.testing.assert_allclose(mosaic_dataset.GetGeoTransform(),
                                   (0,resolution,0,5*resolution,0,-resolution),rtol=0,atol=1e-7)
        # Sample both independently known source grids at every returned target center.
        # GDAL may retain blank edge rows in its automatically suggested union extent.
        rows,columns = np.indices((height,width))
        center_x = (columns+.5)*resolution
        center_y = (5-rows-.5)*resolution
        left_x = np.floor(center_x/resolution).astype(int)
        left_y = np.floor((north-center_y)/resolution).astype(int)
        left_inside = (left_x>=0)&(left_x<6)&(left_y>=0)&(left_y<4)
        left = np.full((height,width),np.nan)
        left[left_inside] = warped_expected[left_y[left_inside],left_x[left_inside]]
        right_x = np.floor((center_x-2*resolution)/resolution).astype(int)
        right_y = np.floor((5*resolution-center_y)/resolution).astype(int)
        right_inside = (right_x>=0)&(right_x<6)&(right_y>=0)&(right_y<4)
        right = np.full((height,width),np.nan)
        other_valid = other_values.copy()
        other_valid[(other_alpha==0)|(other_values==nodata)] = np.nan
        right[right_inside] = other_valid[right_y[right_inside],right_x[right_inside]]
        preferred,fallback = (left,right) if overlap=='first' else (right,left)
        expected = np.where(np.isfinite(preferred),preferred,fallback)
        np.testing.assert_equal(read_band_values(mosaic_dataset.GetRasterBand(1)),expected)
        np.testing.assert_equal(read_band_values(mosaic_dataset.GetRasterBand(2))>0,np.isfinite(expected))
        mosaic_dataset = None
        raster_save(mosaic,target_plan(target),profile=profile,blocksize=128)
        persisted = raster_load(source_plan(target))
        facts = raster_info(persisted)
        assert (facts['width'],facts['height'],facts['band_count'],facts['extent_srid'])==(width,height,2,3857)
        stats = raster_statistics(persisted)
        valid = expected[np.isfinite(expected)]
        assert (stats['valid_count'],stats['invalid_count'])==(valid.size,expected.size-valid.size)
        np.testing.assert_allclose([stats['min'],stats['max'],stats['mean'],stats['stddev']],
                                   [valid.min(),valid.max(),valid.mean(),valid.std()])
        histogram = raster_histogram(persisted,bins=5,value_range=[0,125])
        counts,edges = np.histogram(valid,bins=5,range=(0,125))
        assert histogram['counts']==counts.tolist() and histogram['edges']==edges.tolist()
        assert histogram['outside_count']==0
        calculated = raster_band_math(persisted,'b1+1')
        calculated_dataset = gdal.Open(str(calculated.path))
        np.testing.assert_equal(read_band_values(calculated_dataset.GetRasterBand(1)),expected+1)
        calculated_dataset = None
        if profile=='cog':
            assert validate_cog(persisted)['valid']
    assert not workspace.exists()
    output = gdal.Open(str(target))
    np.testing.assert_equal(read_band_values(output.GetRasterBand(1)),expected)
    assert output.GetRasterBand(1).DataType==gdal.GDT_Float64
    assert np.isnan(output.GetRasterBand(1).GetNoDataValue())
    output = None
    assert all(path.read_bytes()==original for path,original in originals.items())
    assert {path.name for path in tmp_path.iterdir()}=={'source.tif','other.tif','result.tif'}


@pytest.mark.parametrize('mode', [None, 'inside', 'outside'])
def test_clip_mode_metadata_and_bbox(raster_file, mode):
    spec = next(item for item in list_operators() if item['id'] == 'raster_clip')
    param = next(p for p in spec['parameters'] if p['name'] == 'mode')
    assert param['enum'] == ['inside', 'outside'] and param['default'] == 'inside'
    assert spec['execution_modes'] == ['workflow'] and spec['attributes']['resource_groups'] == ['raster']
    with raster_workspace():
        source = raster_load(source_plan(raster_file))
        result = raster_clip(source, 'EPSG:4326', bbox=[1,1,3,3], **({} if mode is None else {'mode': mode}))
        ds = gdal.Open(str(result.path))
        if mode == 'outside':
            expected = np.arange(1,17,dtype=float).reshape(4,4)
            expected[0,0] = np.nan
            expected[1:3,1:3] = np.nan
            assert raster_info(result)['transform'] == raster_info(source)['transform']
            assert (ds.RasterXSize,ds.RasterYSize) == (4,4)
        else:
            expected = np.array([[6,7],[10,11]],dtype=float)
        np.testing.assert_equal(read_band_values(ds.GetRasterBand(1)),expected)
        ds = None


@pytest.mark.parametrize('rotation', [0, .25])
@pytest.mark.parametrize('kind', ['hole', 'islands', 'disjoint', 'cover'])
def test_outside_clip_polygon_complement_and_rotated_grid(tmp_path, rotation, kind):
    transform = (0,1,rotation,4,rotation,-1)
    def ring(x0,y0,x1,y1):
        return [[transform[0]+x*transform[1]+y*transform[2], transform[3]+x*transform[4]+y*transform[5]]
                for x,y in [(x0,y0),(x1,y0),(x1,y1),(x0,y1),(x0,y0)]]
    if kind == 'hole':
        geometry = {'type':'Polygon','coordinates':[ring(0,0,4,4),list(reversed(ring(1,1,3,3)))]}
        retained = np.zeros((4,4),dtype=bool); retained[1:3,1:3] = True
    elif kind == 'islands':
        geometry = {'type':'MultiPolygon','coordinates':[[ring(0,0,1,4)],[ring(3,0,4,4)]]}
        retained = np.ones((4,4),dtype=bool); retained[:,[0,3]] = False
    else:
        geometry = {'type':'Polygon','coordinates':[ring(10,10,12,12) if kind == 'disjoint' else ring(-1,-1,5,5)]}
        retained = np.full((4,4),kind == 'disjoint')
    values = np.arange(16,dtype=float).reshape(4,4)
    values[0,3] = -9999
    source = create_raster(tmp_path/'source.tif', values, transform=transform)
    original = source.read_bytes()
    valid = retained & (values != -9999)
    expected = np.where(valid,values,np.nan)
    with raster_workspace():
        result = raster_clip(raster_load(source_plan(source)),'EPSG:4326',geometry=geometry,mode='outside')
        ds = gdal.Open(str(result.path))
        assert ds.GetGeoTransform() == transform and ds.RasterCount == 2
        np.testing.assert_equal(read_band_values(ds.GetRasterBand(1)),expected)
        np.testing.assert_equal(read_band_values(ds.GetRasterBand(2)),valid*255)
        ds = None
        stats = raster_statistics(result)
        assert stats['valid_count'] == int(valid.sum()) and stats['invalid_count'] == 16-int(valid.sum())
        target = tmp_path/'outside.cog.tif'
        saved = raster_save(result,target_plan(target),profile='cog',blocksize=128)
        assert saved['size_bytes'] == target.stat().st_size > 0
        loaded = raster_load(source_plan(target))
        assert validate_cog(loaded)['valid'] and raster_statistics(loaded) == stats
        persisted = gdal.Open(str(loaded.path))
        np.testing.assert_equal(read_band_values(persisted.GetRasterBand(1)),expected)
        assert persisted.GetGeoTransform() == transform
        persisted = None
    assert not result.workspace.exists() and source.read_bytes() == original


@pytest.mark.parametrize('boundary_kind', ['bbox','geometry'])
def test_outside_clip_different_boundary_crs(tmp_path,boundary_kind):
    # Independently map longitude/latitude to spherical Web Mercator.
    radius = 6378137.
    points = [[radius*math.radians(x),radius*math.log(math.tan(math.pi/4+math.radians(y)/2))]
              for x,y in [(1,1),(3,1),(3,3),(1,3),(1,1)]]
    boundary = {'bbox':[points[0][0],points[0][1],points[2][0],points[2][1]]} if boundary_kind == 'bbox' else {
        'geometry':{'type':'Polygon','coordinates':[points]}}
    values = np.arange(16,dtype=float).reshape(4,4)
    source = create_raster(tmp_path/'source.tif',values)
    with raster_workspace():
        result = raster_clip(raster_load(source_plan(source)),'EPSG:3857',mode='outside',**boundary)
        expected = values.copy(); expected[1:3,1:3] = np.nan
        ds = gdal.Open(str(result.path))
        np.testing.assert_equal(read_band_values(ds.GetRasterBand(1)),expected)
        assert ds.GetGeoTransform() == (0,1,0,4,0,-1)
        ds = None


@pytest.mark.parametrize('nodata', [-9999, float('nan')],ids=['finite','nan'])
def test_outside_clip_independent_nodata_mask_alpha_and_zero(tmp_path,nodata):
    first = np.arange(16,dtype=float).reshape(4,4)
    second = first+100
    first[0,1],second[0,2] = nodata,nodata
    first[3,0],second[3,1] = np.inf,np.nan
    alpha = np.full((4,4),255.); alpha[1,0],alpha[0,0] = 0,128
    source = create_raster(tmp_path/'source.tif',np.stack([first,second,alpha]),nodata=nodata)
    ds = gdal.Open(str(source),gdal.GA_Update)
    ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK', 'YES'):
        ds.CreateMaskBand(gdal.GMF_PER_DATASET)
    mask = np.full((4,4),255,dtype=np.uint8); mask[3,3] = 0
    ds.GetRasterBand(1).GetMaskBand().WriteRaster(0,0,4,4,mask.tobytes())
    ds = None
    ds = gdal.Open(str(source))
    assert ds.GetFileList() == [str(source)]
    ds = None
    originals = {p:p.read_bytes() for p in tmp_path.iterdir()}
    retained = mask.astype(bool) & (alpha>0); retained[1:3,1:3] = False
    expected = [np.where(retained & np.isfinite(v) & (v != nodata),v,np.nan) for v in [first,second]]
    with raster_workspace():
        result = raster_clip(raster_load(source_plan(source)),'EPSG:4326',bbox=[1,1,3,3],mode='outside')
        target = tmp_path/'independent.cog.tif'
        saved = raster_save(result,target_plan(target),profile='cog',blocksize=128)
        assert saved['size_bytes'] == target.stat().st_size > 0
        ds = gdal.Open(str(target))
        assert ds.RasterCount == 3
        for index,values in enumerate(expected,1):
            np.testing.assert_equal(read_band_values(ds.GetRasterBand(index)),values)
            assert np.isnan(ds.GetRasterBand(index).GetNoDataValue())
        np.testing.assert_equal(read_band_values(ds.GetRasterBand(3)),np.logical_or(*[np.isfinite(v) for v in expected])*255)
        assert read_band_values(ds.GetRasterBand(1))[0,0] == 0
        ds = None
    assert all(p.read_bytes() == content for p,content in originals.items())


def test_outside_clip_center_rule_and_bounded_reads(tmp_path,monkeypatch):
    import operators.raster_compute as compute
    source = create_raster(tmp_path/'large.tif',np.ones((517,513)),transform=(0,1,0,517,0,-1))
    original = compute._read_values
    reads = []
    def read(band,x,y,width,height):
        reads.append((width,height))
        assert width<=512 and height<=512
        return original(band,x,y,width,height)
    monkeypatch.setattr(compute,'_read_values',read)
    with raster_workspace():
        result = raster_clip(raster_load(source_plan(source)),'EPSG:4326',bbox=[.1,516.1,.4,516.4],mode='outside')
        assert raster_statistics(result)['valid_count'] == 517*513  # Intersects a pixel but misses its center.
        ds = gdal.Open(str(result.path))
        assert (ds.RasterXSize,ds.RasterYSize) == (513,517)
        ds = None
    assert reads and (1,5) in reads


@pytest.mark.parametrize('params', [
    {'mode':'invalid','bbox':[1,1,3,3]}, {'mode':True,'bbox':[1,1,3,3]},
    {'mode':'outside'}, {'mode':'outside','bbox':[1,1,3,3],'geometry':{}},
    {'mode':'outside','bbox':[3,1,1,3]}, {'mode':'outside','geometry':{'type':'Point','coordinates':[1,1]}},
    {'mode':'outside','geometry':{'type':'Polygon','coordinates':[[[0,0],[3,3],[3,0],[0,3],[0,0]]]}}
])
def test_outside_clip_rejects_invalid_configuration(raster_file,params):
    with raster_workspace(),pytest.raises(ValueError):
        raster_clip(raster_load(source_plan(raster_file)),'EPSG:4326',**params)


@pytest.mark.parametrize('transform,crs',[(None,'EPSG:4326'),((0,1,0,4,0,-1),''),((0,1,2,4,2,4),'EPSG:4326')])
def test_outside_clip_rejects_missing_or_singular_grid(tmp_path,transform,crs):
    source = create_raster(tmp_path/'source.tif',np.ones((4,4)),transform=transform,crs=crs)
    with raster_workspace(),pytest.raises(ValueError):
        raster_clip(raster_load(source_plan(source)),'EPSG:4326',bbox=[1,1,3,3],mode='outside')


def test_outside_clip_async_public_dag_cog_and_cleanup(raster_file,tmp_path,monkeypatch):
    import api_server
    import operators.raster_compute as compute
    paths = []
    original = OPERATORS['raster_load']['function']
    def tracked(*args,**kwargs):
        paths.append(compute._WORKSPACE.get())
        return original(*args,**kwargs)
    monkeypatch.setitem(OPERATORS['raster_load'],'function',tracked)
    target = tmp_path/'outside.cog.tif'
    before = raster_file.read_bytes()
    workflow = {'tasks':[
        {'id':'load','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(raster_file)}},
        {'id':'clip','operator':'raster_clip','depends_on':['load'],'params':{'input_raster':{'$ref':'load'},'boundary_crs':'EPSG:4326','bbox':[1,1,3,3],'mode':'outside'}},
        {'id':'save','operator':'raster_save','depends_on':['clip'],'params':{'input_raster':{'$ref':'clip'},'access_plan':target_plan(target),'profile':'cog','blocksize':128}},
    ]}
    client = api_server.app.test_client()
    response = client.post('/api/workflow',json={'workflow_def':workflow,'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert response.status_code == 202,response.json
    deadline = time.monotonic()+5
    while time.monotonic()<deadline:
        status = client.get('/api/executions/'+response.json['execution_id']).json
        if status['status'] in ['success','failed']: break
        time.sleep(.01)
    assert status['status'] == 'success',status
    facts = json.loads(status['result'])
    assert facts['size_bytes'] == target.stat().st_size > 0
    ds = gdal.Open(str(target))
    expected = np.arange(1,17,dtype=float).reshape(4,4); expected[0,0] = np.nan; expected[1:3,1:3] = np.nan
    np.testing.assert_equal(read_band_values(ds.GetRasterBand(1)),expected)
    ds = None
    assert paths and all(not p.exists() and str(p) not in json.dumps(status) for p in paths)
    assert raster_file.read_bytes() == before
    assert client.post('/api/operators/raster_clip/invoke',json={'params':{}}).status_code == 403


@pytest.mark.parametrize('alpha',[False,True],ids=['complex-data','complex-alpha'])
def test_outside_clip_rejects_complex_bands(tmp_path,alpha):
    path = tmp_path/'complex.tif'
    ds = gdal.GetDriverByName('GTiff').Create(str(path),4,4,2,gdal.GDT_CFloat64)
    ds.SetGeoTransform((0,1,0,4,0,-1))
    crs = osr.SpatialReference(); crs.ImportFromEPSG(4326); ds.SetProjection(crs.ExportToWkt())
    if alpha: ds.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand)
    ds = None
    with raster_workspace(),pytest.raises(ValueError,match='Complex'):
        raster_clip(raster_load(source_plan(path)),'EPSG:4326',bbox=[1,1,3,3],mode='outside')


@pytest.mark.parametrize('native_exceptions',[False,True])
@pytest.mark.parametrize('kind',['bbox','geometry'])
def test_outside_clip_rejects_failed_boundary_transform(tmp_path,native_exceptions,kind):
    source = create_raster(tmp_path/'mercator.tif',np.ones((4,4)),crs='EPSG:3857')
    boundary = {'bbox':[0,94,1,95]} if kind == 'bbox' else {
        'geometry':{'type':'Polygon','coordinates':[[[0,94],[1,94],[1,95],[0,95],[0,94]]]}}
    previous = osr.GetUseExceptions()
    (osr.UseExceptions if native_exceptions else osr.DontUseExceptions)()
    try:
        with raster_workspace(),pytest.raises(ValueError,match='transform'):
            raster_clip(raster_load(source_plan(source)),'EPSG:4326',mode='outside',**boundary)
    finally:
        (osr.UseExceptions if previous else osr.DontUseExceptions)()


def test_clip_and_mosaic(raster_file, tmp_path):
    right = create_raster(tmp_path / 'right.tif', np.full((2, 4, 4), 100.0), transform=(2,1,0,4,0,-1))
    with raster_workspace():
        raster = raster_load(source_plan(raster_file))
        clipped = raster_clip(raster, 'EPSG:4326', bbox=[1, 1, 3, 3])
        assert raster_info(clipped)['extent'] == [1, 1, 3, 3]
        assert raster_statistics(clipped)['mean'] == pytest.approx(8.5)
        geometry = {'type':'Polygon','coordinates':[[[1,1],[3,1],[3,3],[1,3],[1,1]]]}
        masked = raster_clip(raster, 'EPSG:4326', geometry=geometry)
        assert raster_info(masked)['width'] == 2
        with pytest.raises(ValueError, match='intersect'): raster_clip(raster, 'EPSG:4326', bbox=[10,10,12,12])
        other = raster_load(source_plan(right))
        last = raster_mosaic([raster, other], 'EPSG:4326', [1,1], 'last')
        first = raster_mosaic([raster, other], 'EPSG:4326', [1,1], 'first')
        assert raster_info(last)['extent'] == [0,0,6,4]
        assert raster_statistics(last)['mean'] > raster_statistics(first)['mean']


def test_polygon_clip_integer_data_keeps_valid_zero_and_excludes_transparent_pixels(tmp_path):
    values = np.arange(16, dtype=float).reshape(4, 4)
    values[0, 0] = -9999
    values[0, 1] = 0
    source = create_raster(tmp_path / 'float.tif', values)
    integer = tmp_path / 'integer.tif'
    dataset = gdal.Translate(str(integer), str(source), outputType=gdal.GDT_Int16)
    dataset = None
    geometry = {'type': 'Polygon', 'coordinates': [[[0,0],[4,0],[4,2],[2,2],[2,4],[0,4],[0,0]]]}
    with raster_workspace():
        clipped = raster_clip(raster_load(source_plan(integer)), 'EPSG:4326', geometry=geometry)
        facts = raster_info(clipped)
        assert facts['band_count'] == 2
        assert facts['bands'][0]['dtype'] == 'Float64'
        assert facts['bands'][0]['nodata_is_nan']
        expected = [values[y, x] for y in range(4) for x in range(4)
                    if (x < 2 or y >= 2) and (x, y) != (0, 0)]
        statistics = raster_statistics(clipped)
        assert (statistics['valid_count'], statistics['invalid_count'], statistics['min']) == (11, 5, 0)
        assert statistics['mean'] == pytest.approx(np.mean(expected))
        assert raster_statistics(raster_band_math(clipped, 'b1+1'))['valid_count'] == 11


def test_polygon_clip_rejects_complex_bands_before_float64_conversion(tmp_path):
    source = tmp_path / 'complex.tif'
    dataset = gdal.GetDriverByName('GTiff').Create(str(source), 4, 4, 1, gdal.GDT_CFloat64)
    dataset.SetGeoTransform((0,1,0,4,0,-1))
    crs = osr.SpatialReference(); crs.ImportFromEPSG(4326)
    dataset.SetProjection(crs.ExportToWkt())
    pixels = np.full((4,4), 1+2j, dtype=np.complex128)
    dataset.GetRasterBand(1).WriteRaster(0,0,4,4,pixels.tobytes(),buf_type=gdal.GDT_CFloat64)
    dataset = None
    with raster_workspace():
        raster = raster_load(source_plan(source))
        with pytest.raises(ValueError, match='Complex'):
            raster_clip(raster, 'EPSG:4326', geometry={'type': 'Polygon', 'coordinates': [
                [[0,0],[4,0],[4,4],[0,4],[0,0]]]})


@pytest.mark.parametrize('transform,boundary', [
    ((0,1,0,4,0,-1), {'geometry': {'type': 'Polygon', 'coordinates': [
        [[-1,-1],[5,-1],[5,5],[-1,5],[-1,-1]],
        [[-0.5,-0.5],[-0.5,4.5],[4.5,4.5],[4.5,-0.5],[-0.5,-0.5]],
    ]}}),
    ((0,1,0,4,0,-1), {'geometry': {'type': 'MultiPolygon', 'coordinates': [
        [[[-2,1],[-1,1],[-1,3],[-2,3],[-2,1]]],
        [[[5,1],[6,1],[6,3],[5,3],[5,1]]],
    ]}}),
    ((0,1,0,4,0,-1), {'geometry': {'type': 'Polygon', 'coordinates': [
        [[-1,3],[1,5],[-1,5],[-1,3]],
    ]}}),
    ((0,1,1,4,1,-1), {'bbox': [0,0,0.5,0.5]}),
    ((0,1,1,4,1,-1), {'geometry': {'type': 'Polygon', 'coordinates': [
        [[0,0],[0.5,0],[0.5,0.5],[0,0.5],[0,0]],
    ]}}),
], ids=['inside-hole', 'outside-multipolygon', 'touch-only', 'rotated-bbox', 'rotated-polygon'])
def test_clip_rejects_disjoint_footprints_even_when_envelopes_overlap(tmp_path, transform, boundary):
    source = create_raster(tmp_path / 'source.tif', np.ones((4,4)), transform=transform)
    original = source.read_bytes()
    with raster_workspace():
        raster = raster_load(source_plan(source))
        with pytest.raises(ValueError, match='intersect'):
            raster_clip(raster, 'EPSG:4326', **boundary)
    assert source.read_bytes() == original


def test_multipolygon_clip_preserves_separate_valid_islands(tmp_path):
    values = np.arange(1,17,dtype=float).reshape(4,4)
    source = create_raster(tmp_path / 'source.tif', values)
    geometry = {'type': 'MultiPolygon', 'coordinates': [
        [[[0,0],[1,0],[1,4],[0,4],[0,0]]],
        [[[3,0],[4,0],[4,4],[3,4],[3,0]]],
    ]}
    with raster_workspace():
        clipped = raster_clip(raster_load(source_plan(source)), 'EPSG:4326', geometry=geometry)
        statistics = raster_statistics(clipped)
        assert (statistics['valid_count'], statistics['invalid_count']) == (8,8)
        assert statistics['mean'] == pytest.approx(8.5)
        target = tmp_path / 'islands.tif'
        raster_save(clipped, target_plan(target), profile='cog')
        persisted = raster_load(source_plan(target))
        assert validate_cog(persisted)['valid']
        assert raster_statistics(persisted) == statistics
    dataset = gdal.Open(str(target))
    expected = values.copy(); expected[:,1:3] = np.nan
    np.testing.assert_equal(read_band_values(dataset.GetRasterBand(1)), expected)
    np.testing.assert_equal(read_band_values(dataset.GetRasterBand(2)), [[255,0,0,255]]*4)


@pytest.mark.parametrize('native_exceptions', [False,True], ids=['return-code','native-exception'])
def test_clip_rejects_failed_boundary_crs_transformation(tmp_path, native_exceptions):
    source = create_raster(tmp_path / 'mercator.tif', np.ones((4,4)), crs='EPSG:3857')
    original = ogr.GetUseExceptions()
    (ogr.UseExceptions if native_exceptions else ogr.DontUseExceptions)()
    try:
        with raster_workspace():
            raster = raster_load(source_plan(source))
            with pytest.raises(ValueError, match='transform'):
                raster_clip(raster, 'EPSG:4326', geometry={'type': 'Polygon', 'coordinates': [
                    [[0,94],[1,94],[1,95],[0,95],[0,94]],
                ]})
    finally:
        (ogr.UseExceptions if original else ogr.DontUseExceptions)()


def test_cog_save_validate_overviews_and_failed_replace(raster_file, tmp_path):
    target = tmp_path / 'result.tif'
    with raster_workspace():
        raster = raster_load(source_plan(raster_file))
        strip_file = create_raster(tmp_path / 'strips.tif', np.ones((1025, 1025)))
        assert not validate_cog(raster_load(source_plan(strip_file)))['valid']
        overview = raster_build_overviews(raster, levels=[2,4])
        assert raster_info(overview)['bands'][0]['overviews'] == [[2,2],[1,1]]
        assert raster_info(raster)['bands'][0]['overviews'] == []
        artifact = raster_save(raster, target_plan(target), profile='cog')
        assert artifact['format'] == 'tiff' and artifact['profile'] == 'cog'
        assert validate_cog(raster_load(source_plan(target)))['valid']
        original = target.read_bytes()
        with pytest.raises(ValueError): raster_save(raster, target_plan(target), profile='cog')
        with pytest.raises(ValueError): raster_save(raster, target_plan(target,'replace'), profile='cog', blocksize=1)
        assert target.read_bytes() == original
        assert set(artifact).isdisjoint({'path','access_plan','connection_info','storage_ref'})


@pytest.mark.parametrize('format,driver,suffix', [('png','PNG','.png'), ('jpeg','JPEG','.jpg')])
@pytest.mark.parametrize('source_crs', ['', 'EPSG:4326'], ids=['missing-crs','explicit-crs'])
@pytest.mark.parametrize('profile', ['geotiff','cog'])
def test_async_image_pyramid_save_keeps_pixels_overviews_and_missing_georeferencing(
        tmp_path, format, driver, suffix, source_crs, profile):
    import api_server
    height, width = 128,256
    y,x = np.indices((height,width))
    # Constant 8x8 cells let both pyramid levels select the same independent value.
    pixels = (((x//8 + y//8*7)%100)*2+25).astype(np.uint8)
    alpha = np.full((height,width),255,dtype=np.uint8)
    alpha[:16,:16] = 0
    alpha[64:80,128:144] = 128
    memory = gdal.GetDriverByName('MEM').Create('',width,height,2 if format=='png' else 1,gdal.GDT_Byte)
    memory.GetRasterBand(1).WriteRaster(0,0,width,height,pixels.tobytes(),buf_type=gdal.GDT_Byte)
    if format=='png':
        memory.GetRasterBand(2).WriteRaster(0,0,width,height,alpha.tobytes(),buf_type=gdal.GDT_Byte)
        memory.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand)
    source = tmp_path / ('source'+suffix)
    encoded = gdal.GetDriverByName(driver).CreateCopy(str(source),memory)
    encoded = memory = None
    original = source.read_bytes()
    decoded = gdal.Open(str(source))
    assert decoded.GetDriver().ShortName == driver
    assert decoded.GetGeoTransform(can_return_null=True) is None and not decoded.GetProjection()
    decoded_pixels = read_band_values(decoded.GetRasterBand(1))
    if format=='png':
        np.testing.assert_equal(decoded_pixels,pixels)
    expected = decoded_pixels*2
    if format=='png':
        expected[:16,:16] = np.nan
    # JPEG's oracle is its actual decoded pixels, without claiming lossless encoding.
    for block_y in range(0,height,8):
        for block_x in range(0,width,8):
            cell = expected[block_y:block_y+8,block_x:block_x+8]
            np.testing.assert_equal(cell,np.full((8,8),cell[0,0]))
    decoded = None
    target = tmp_path / ('workflow.'+profile+'.tif')
    workflow = {'tasks': [
        {'id':'load','operator':'raster_load','depends_on':[],
         'params':{'access_plan':source_plan(source,format),'source_crs':source_crs}},
        {'id':'math','operator':'raster_band_math','depends_on':['load'],
         'params':{'input_raster':{'$ref':'load'},'expression':'b1*2'}},
        {'id':'pyramid','operator':'raster_build_overviews','depends_on':['math'],
         'params':{'input_raster':{'$ref':'math'},'levels':[2,4],'resampling':'nearest'}},
        {'id':'save','operator':'raster_save','depends_on':['pyramid'],
         'params':{'input_raster':{'$ref':'pyramid'},'access_plan':target_plan(target),
                   'profile':profile,'blocksize':128}},
        {'id':'reload','operator':'raster_load','depends_on':['save'],
         'params':{'access_plan':source_plan(target)}},
        {'id':'statistics','operator':'raster_statistics','depends_on':['reload'],
         'params':{'input_raster':{'$ref':'reload'}}},
    ]}
    client = api_server.app.test_client()
    response = client.post('/api/workflow',json={'workflow_def':workflow,'runtime':{
        'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert response.status_code == 202, response.json
    deadline = time.monotonic()+5
    while time.monotonic()<deadline:
        status = client.get('/api/executions/'+response.json['execution_id']).json
        if status['status'] in ('success','failed'): break
        time.sleep(.01)
    assert status['status']=='success', status
    stats = json.loads(status['result'])
    valid = expected[np.isfinite(expected)]
    assert (stats['valid_count'],stats['invalid_count']) == (valid.size,width*height-valid.size)
    np.testing.assert_allclose([stats['min'],stats['max'],stats['mean'],stats['stddev']],
                               [valid.min(),valid.max(),valid.mean(),valid.std()])
    assert str(tmp_path) not in json.dumps(status) and 'addp-raster-' not in json.dumps(status)
    output = gdal.Open(str(target))
    assert output.GetGeoTransform(can_return_null=True) is None
    assert bool(output.GetProjection()) == bool(source_crs)
    if source_crs:
        assert output.GetSpatialRef().GetAuthorityCode(None)=='4326'
    assert (output.RasterXSize,output.RasterYSize,output.RasterCount)==(width,height,1)
    band = output.GetRasterBand(1)
    assert band.DataType==gdal.GDT_Float64 and np.isnan(band.GetNoDataValue())
    np.testing.assert_equal(read_band_values(band),expected)
    assert band.GetOverviewCount()==2
    for index,level in enumerate([2,4]):
        overview = band.GetOverview(index)
        assert (overview.XSize,overview.YSize)==(width//level,height//level)
        np.testing.assert_equal(read_band_values(overview),expected[::level,::level])
    output = band = None
    with raster_workspace():
        persisted = raster_load(source_plan(target))
        facts = raster_info(persisted)
        assert facts['transform']==[] and facts['extent']==[]
        assert facts['bands'][0]['overviews']==[[128,64],[64,32]]
        if profile=='cog':
            assert validate_cog(persisted)['valid']
        with pytest.raises(ValueError,match='geotransform' if source_crs else 'CRS'):
            raster_reproject(persisted,'EPSG:3857')
        with pytest.raises(ValueError,match='Source format'):
            raster_load(source_plan(source))
    if profile=='cog':
        direct_target = tmp_path / 'direct.tif'
        plan = {**source_plan(source,format),'target':target_plan(direct_target)['target']}
        direct = client.post('/api/operators/raster_to_cog/invoke',json={'tenant_id':7,'params':{
            'access_plan':plan,'options':{'source_crs':source_crs,'blocksize':128}}})
        assert direct.status_code==200, direct.json
        assert direct.json['result']['band_count']==(2 if format=='png' else 1)
        direct_dataset = gdal.Open(str(direct_target))
        np.testing.assert_equal(read_band_values(direct_dataset.GetRasterBand(1)),decoded_pixels)
        if format=='png':
            np.testing.assert_equal(read_band_values(direct_dataset.GetRasterBand(2)),alpha)
        direct_dataset = None
        with raster_workspace():
            assert validate_cog(raster_load(source_plan(direct_target)))['valid']
        original_target = direct_target.read_bytes()
        plan['source']['format']='tiff'
        plan['target']['write_mode']='replace'
        rejected = client.post('/api/operators/raster_to_cog/invoke',json={'tenant_id':7,'params':{'access_plan':plan}})
        assert rejected.status_code==500
        assert rejected.json['error_code']=='EXECUTION_FAILED'
        assert direct_target.read_bytes()==original_target
    assert source.read_bytes()==original
    assert set(path.name for path in tmp_path.iterdir())=={
        source.name,target.name,*(['direct.tif'] if profile=='cog' else []),
    }


def test_source_crs_is_explicit_and_conflicts_fail(tmp_path):
    path = create_raster(tmp_path / 'unknown.tif', np.ones((2,2)), crs='')
    with raster_workspace():
        raster = raster_load(source_plan(path))
        assert raster_info(raster)['crs'] == ''
        with pytest.raises(ValueError): raster_reproject(raster, 'EPSG:3857')
        assigned = raster_load(source_plan(path), 'EPSG:4326')
        assert raster_info(assigned)['extent_srid'] == 4326
        assert raster_info(raster)['crs'] == ''
        known = create_raster(tmp_path / 'known.tif', np.ones((2,2)))
        with pytest.raises(ValueError, match='conflicts'): raster_load(source_plan(known), 'EPSG:3857')


def test_rotated_extent_and_unforgeable_runtime_input(tmp_path):
    path = create_raster(tmp_path / 'rotated.tif', np.ones((2,2)), transform=(10,2,1,20,1,-2))
    with raster_workspace():
        raster = raster_load(source_plan(path))
        assert raster_info(raster)['extent'] == [10,16,16,22]
        with pytest.raises(ValueError): raster_info({'path':str(path)})
    with raster_workspace():
        with pytest.raises(ValueError): raster_info(raster)


def test_dag_persists_artifact_and_hides_temporary_paths(raster_file, tmp_path):
    target = tmp_path / 'dag.tif'
    workflow = {'tasks':[
        {'id':'load','operator':'raster_load','params':{'access_plan':source_plan(raster_file)},'depends_on':[]},
        {'id':'math','operator':'raster_band_math','params':{'input_raster':{'$ref':'load','port':'default'},'expression':'b1*2'},'depends_on':['load']},
        {'id':'save','operator':'raster_save','params':{'input_raster':{'$ref':'math'},'access_plan':target_plan(target),'profile':'cog'},'depends_on':['math']},
    ]}
    result = execute_workflow(workflow)
    assert result['status'] == 'success', result
    assert target.exists()
    assert json.loads(result['final_result'])['profile'] == 'cog'
    encoded = json.dumps(result)
    assert str(raster_file) not in encoded and 'addp-raster-' not in encoded
    workflow['tasks'][1]['params']['expression'] = 'b99'
    failed = execute_workflow(workflow)
    assert failed['status'] == 'failed'


def test_conversion_source_target_credentials_are_separate(monkeypatch, raster_file, tmp_path):
    import addp_common.workflow_access as access
    calls = []
    class Client:
        def __init__(self, endpoint): self.endpoint = endpoint
        def fget_object(self, bucket, key, path):
            calls.append(('read', self.endpoint, bucket, key)); Path(path).write_bytes(raster_file.read_bytes())
        def bucket_exists(self, bucket): return True
        def stat_object(self, bucket, key):
            class NotFound(Exception): code = 'NoSuchKey'
            raise NotFound()
        def fput_object(self, bucket, key, path, content_type):
            calls.append(('write', self.endpoint, bucket, key)); assert Path(path).exists()
    monkeypatch.setattr(access, '_object_store_client', lambda config: Client(config['endpoint']))
    plan = {**source_plan(raster_file), 'target':target_plan(tmp_path/'remote.tif')['target']}
    for label, endpoint in [('source','source:9000'),('target','target:9000')]:
        plan[label]['access'] = {'method':'object_store','endpoint':endpoint,'access_key':label+'-key','secret_key':label+'-secret','bucket':label,'object':'sample.tif'}
    result = raster_to_cog(plan)
    assert calls == [('read','source:9000','source','sample.tif'),('write','target:9000','target','sample.tif')]
    assert 'secret' not in json.dumps(result)


@pytest.mark.parametrize('input_data', ['omitted', {}, None], ids=['omitted', 'empty-object', 'reject-null'])
def test_async_api_runs_authorized_raster_dag(raster_file, input_data):
    import api_server
    workflow = {'tasks':[
        {'id':'load','operator':'raster_load','params':{'access_plan':source_plan(raster_file)},'depends_on':[]},
        {'id':'stats','operator':'raster_statistics','params':{'input_raster':{'$ref':'load'}},'depends_on':['load']},
    ]}
    client = api_server.app.test_client()
    assert client.post('/api/workflow',json={'workflow_def':workflow}).status_code == 400
    payload = {'workflow_def':workflow,'runtime':{
        'tenant_id':7,'execution_authorization':{'id':1,'effects':['read']}}}
    if input_data != 'omitted':
        payload['input_data'] = input_data
    response = client.post('/api/workflow',json=payload)
    if input_data is None:
        assert response.status_code == 400
        assert response.json['error_code'] == 'INVALID_PARAMS'
        return
    assert response.status_code == 202
    execution_id = response.json['execution_id']
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        status = client.get('/api/executions/'+execution_id).json
        if status['status'] in ['success','failed']: break
        time.sleep(0.01)
    assert status['status'] == 'success', status
    assert json.loads(status['result'])['valid_count'] == 15


def test_metadata_has_typed_raster_ports_and_no_old_conversion():
    specs = {item['id']:item for item in list_operators()}
    assert 'tiff_to_cog' not in specs
    assert specs['raster_load']['output_ports'][0]['type'] == 'raster'
    assert specs['raster_band_math']['parameters'][0]['type'] == 'raster'
    assert specs['raster_to_cog']['execution_modes'] == ['workflow','direct']
    assert specs['raster_save']['effects'] == ['write']
    for name, inputs in [('raster_align', ['input_raster', 'reference_raster']),
                         ('raster_stack', ['input_raster', 'other_raster']),
                         ('raster_select_bands', ['input_raster'])]:
        spec = specs[name]
        assert [p['name'] for p in spec['parameters'] if p['type'] == 'raster'] == inputs
        assert spec['output_ports'][0]['type'] == 'raster'
        assert spec['execution_modes'] == ['workflow']
        assert spec['attributes']['resource_groups'] == ['raster']


@pytest.mark.parametrize('transform', [(0, 1, 0, 4, 0, -1), (0, 1, 0.25, 4, 0.25, -1)])
def test_align_exact_reference_grid_and_outside_nodata(tmp_path, transform):
    values = np.arange(16, dtype=float).reshape(4, 4)
    source = create_raster(tmp_path / 'source.tif', values, transform=transform)
    # Shift the reference exactly one source column. Its values/mask do not gate coverage.
    target_transform = (transform[0] + transform[1], *transform[1:3],
                        transform[3] + transform[4], *transform[4:])
    reference = create_raster(tmp_path / 'reference.tif', np.full((4, 4), -9999.), transform=target_transform)
    expected = np.column_stack([values[:, 1:], np.full(4, np.nan)])
    before = {path: hashlib.sha256(path.read_bytes()).hexdigest() for path in [source, reference]}
    with raster_workspace():
        aligned = raster_align(raster_load(source_plan(source)), raster_load(source_plan(reference)))
        dataset = gdal.Open(str(aligned.path))
        assert dataset.GetGeoTransform() == target_transform
        assert (dataset.RasterXSize, dataset.RasterYSize) == (4, 4)
        np.testing.assert_equal(read_band_values(dataset.GetRasterBand(1)), expected)
        dataset = None
    assert not aligned.workspace.exists()
    assert before == {path: hashlib.sha256(path.read_bytes()).hexdigest() for path in before}


@pytest.mark.parametrize('algorithm', ['nearest', 'average'])
def test_align_cross_crs_independent_nodata_alpha_and_zero(tmp_path, algorithm):
    first = np.array([[0, -9999, 2, 3], [4, 5, 6, 7]], dtype=float)
    second = np.array([[10, 11, -9999, 13], [14, 15, 16, 17]], dtype=float)
    alpha = np.array([[128, 255, 255, 0], [255, 255, 255, 255]], dtype=float)
    source = create_raster(tmp_path / 'source.tif', np.stack([first, second, alpha]),
                           transform=(0, 1, 0, 2, 0, -1))
    dataset = gdal.Open(str(source), gdal.GA_Update)
    dataset.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
    dataset = None
    # Nearest centres in Web Mercator correspond to the lon/lat cells. Average
    # uses exact source row boundaries, avoiding tiny cross-CRS overlap with row 2.
    scale = 6378137 * np.pi / 180
    width = 4 if algorithm == 'nearest' else 2
    reference_scale = scale if algorithm == 'nearest' else 1
    reference = create_raster(tmp_path / 'reference.tif', np.ones((2, width)),
                             transform=(0, reference_scale * 4 / width, 0, reference_scale * 2, 0, -reference_scale),
                             crs='EPSG:3857' if algorithm == 'nearest' else 'EPSG:4326')
    with raster_workspace():
        result = raster_align(raster_load(source_plan(source)), raster_load(source_plan(reference)), algorithm)
        output = gdal.Open(str(result.path))
        assert output.RasterCount == 2
        assert output.GetSpatialRef().IsSame(gdal.Open(str(reference)).GetSpatialRef())
        if algorithm == 'nearest':
            expected = np.stack([first, second])
            expected[(expected == -9999) | np.broadcast_to(alpha == 0, expected.shape)] = np.nan
        else:
            expected = np.array([[[0, 2], [4.5, 6.5]], [[10.5, np.nan], [14.5, 16.5]]])
        for index in range(2):
            np.testing.assert_allclose(read_band_values(output.GetRasterBand(index + 1)), expected[index],
                                       rtol=0, atol=0.001, equal_nan=True)
            assert np.isnan(output.GetRasterBand(index + 1).GetNoDataValue())
        output = None


def test_stack_select_rgb_cog_and_cross_file_weighted_expression(tmp_path):
    first = np.arange(513 * 515, dtype=float).reshape(513, 515)
    first[0, 1] = -9999
    second = np.full(first.shape, 100.)
    second[1, 0] = -9999
    sources = [create_raster(tmp_path / 'first.tif', first), create_raster(tmp_path / 'second.tif', second)]
    hashes = [hashlib.sha256(path.read_bytes()).hexdigest() for path in sources]
    with raster_workspace():
        rasters = [raster_load(source_plan(path)) for path in sources]
        stacked = raster_stack(*rasters)
        weighted = raster_band_math(stacked, '0.3*b1+0.7*b2')
        expected = 0.3 * first + 0.7 * second
        expected[(first == -9999) | (second == -9999)] = np.nan
        output = gdal.Open(str(weighted.path))
        np.testing.assert_allclose(read_band_values(output.GetRasterBand(1)), expected, equal_nan=True)
        output = None
        # A non-referenced band's NoData must not invalidate the expression.
        assert raster_statistics(raster_band_math(stacked, 'b1'))['invalid_count'] == 1
        rgb = raster_select_bands(stacked, [2, 1, 2], 'rgb')
        assert [b['color_interpretation'] for b in raster_info(rgb)['bands']] == ['Red', 'Green', 'Blue']
        target = tmp_path / 'rgb.cog.tif'
        saved = raster_save(rgb, target_plan(target), profile='cog')
        assert saved['size_bytes'] == target.stat().st_size > 0
        reloaded = raster_load(source_plan(target))
        assert validate_cog(reloaded)['valid']
        output = gdal.Open(str(target))
        for index, values in enumerate([second, first, second], 1):
            expected_band = np.where(values == -9999, np.nan, values)
            band = output.GetRasterBand(index)
            np.testing.assert_equal(read_band_values(band), expected_band)
            assert band.GetColorInterpretation() == [gdal.GCI_RedBand, gdal.GCI_GreenBand, gdal.GCI_BlueBand][index - 1]
            assert band.GetOverviewCount() > 0
        output = None
    assert not rgb.workspace.exists()
    assert hashes == [hashlib.sha256(path.read_bytes()).hexdigest() for path in sources]


def test_select_bands_respects_per_band_mask_alpha_and_missing_georeferencing(tmp_path):
    values = np.array([[[0, 1, 2, 3]], [[10, 11, 12, 13]], [[128, 255, 0, 255]]], dtype=float)
    source = create_raster(tmp_path / 'masked.tif', values, transform=None, crs='')
    dataset = gdal.Open(str(source), gdal.GA_Update)
    dataset.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
    # Internal dataset mask excludes a pixel regardless of its finite data value.
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK', 'YES'):
        dataset.CreateMaskBand(gdal.GMF_PER_DATASET)
        dataset.GetRasterBand(1).GetMaskBand().WriteRaster(0, 0, 4, 1, bytes([255, 0, 255, 255]))
    dataset = None
    with raster_workspace():
        raster = raster_load(source_plan(source))
        selected = raster_select_bands(raster, [2, 1])
        facts = raster_info(selected)
        assert facts['transform'] == facts['extent'] == [] and facts['crs'] == ''
        assert facts['band_count'] == 2
        dataset = gdal.Open(str(selected.path))
        np.testing.assert_equal(read_band_values(dataset.GetRasterBand(1)), [[10, np.nan, np.nan, 13]])
        np.testing.assert_equal(read_band_values(dataset.GetRasterBand(2)), [[0, np.nan, np.nan, 3]])
        dataset = None
        with pytest.raises(ValueError, match='Alpha'):
            raster_select_bands(raster, [3])


@pytest.mark.parametrize('bands,color_model', [([], 'multispectral'), ([True], 'multispectral'),
    ([1.0], 'multispectral'), ([0], 'multispectral'), ([99], 'multispectral'),
    ('1', 'multispectral'), ([1], 'rgb'), ([1, 2], 'rgb'), ([1], 'automatic')])
def test_select_bands_rejects_invalid_parameters(raster_file, bands, color_model):
    with raster_workspace(), pytest.raises(ValueError):
        raster_select_bands(raster_load(source_plan(raster_file)), bands, color_model)


@pytest.mark.parametrize('fault', ['size', 'origin', 'rotation', 'crs', 'missing-crs', 'missing-transform', 'singular'])
def test_stack_rejects_unaligned_grids(tmp_path, fault):
    first = create_raster(tmp_path / 'first.tif', np.ones((4, 4)))
    kwargs = {'size': {'array': np.ones((4, 3))}, 'origin': {'transform': (0.5, 1, 0, 4, 0, -1)},
              'rotation': {'transform': (0, 1, 0.25, 4, 0.25, -1)}, 'crs': {'crs': 'EPSG:3857'},
              'missing-crs': {'crs': ''}, 'missing-transform': {'transform': None},
              'singular': {'transform': (0, 1, 1, 4, 1, 1)}}[fault]
    second = create_raster(tmp_path / 'second.tif', **{'array': np.ones((4, 4)), **kwargs})
    with raster_workspace(), pytest.raises(ValueError):
        raster_stack(raster_load(source_plan(first)), raster_load(source_plan(second)))


def test_multiraster_dag_summary_and_cleanup(tmp_path, monkeypatch):
    import operators.raster_compute as compute
    first = create_raster(tmp_path / 'first.tif', np.ones((4, 4)))
    second = create_raster(tmp_path / 'second.tif', np.full((4, 4), 3.), transform=(1, 1, 0, 4, 0, -1))
    target = tmp_path / 'weighted.cog.tif'
    paths = []
    original = OPERATORS['raster_load']['function']
    def tracked(*args, **kwargs):
        paths.append(compute._WORKSPACE.get())
        return original(*args, **kwargs)
    monkeypatch.setitem(OPERATORS['raster_load'], 'function', tracked)
    tasks = [{'id': name, 'operator': 'raster_load', 'depends_on': [], 'params': {'access_plan': source_plan(path)}}
             for name, path in [('first', first), ('second', second)]]
    tasks += [
        {'id': 'align', 'operator': 'raster_align', 'depends_on': ['first', 'second'], 'params': {
            'input_raster': {'$ref': 'second'}, 'reference_raster': {'$ref': 'first'}}},
        {'id': 'stack', 'operator': 'raster_stack', 'depends_on': ['first', 'align'], 'params': {
            'input_raster': {'$ref': 'first'}, 'other_raster': {'$ref': 'align'}}},
        {'id': 'math', 'operator': 'raster_band_math', 'depends_on': ['stack'], 'params': {
            'input_raster': {'$ref': 'stack'}, 'expression': '0.5*b1+0.5*b2'}},
        {'id': 'save', 'operator': 'raster_save', 'depends_on': ['math'], 'params': {
            'input_raster': {'$ref': 'math'}, 'access_plan': target_plan(target), 'profile': 'cog'}}]
    result = execute_workflow({'tasks': tasks})
    assert result['status'] == 'success', result
    serialized = json.dumps(result)
    assert all(not path.exists() and str(path) not in serialized for path in paths)
    output = gdal.Open(str(target))
    np.testing.assert_equal(read_band_values(output.GetRasterBand(1)), np.tile([np.nan, 2, 2, 2], (4, 1)))
    output = None
    target.unlink()
    tasks[2]['params']['resampling'] = 'unsafe'
    result = execute_workflow({'tasks': tasks})
    assert result['status'] == 'failed' and not target.exists()
    assert all(not path.exists() for path in paths)


def test_concurrent_workspaces_are_isolated(raster_file):
    def run(_):
        with raster_workspace():
            raster = raster_load(source_plan(raster_file))
            return raster.workspace, raster_statistics(raster)['mean']
    with ThreadPoolExecutor(max_workers=2) as pool:
        results = list(pool.map(run, range(2)))
    assert results[0][0] != results[1][0]
    assert all(not path.exists() and mean == 9 for path, mean in results)


def test_source_snapshot_and_driver_restriction(raster_file, tmp_path):
    with raster_workspace():
        raster = raster_load(source_plan(raster_file))
        before = raster_statistics(raster)
        create_raster(raster_file, np.full((4,4), 777.0))
        assert raster_statistics(raster) == before
        # Renaming a VRT cannot turn arbitrary filesystem references into a TIFF.
        disguised = tmp_path / 'disguised.tif'
        disguised.write_text('<VRTDataset rasterXSize="1" rasterYSize="1"><VRTRasterBand dataType="Byte" band="1"/></VRTDataset>')
        with pytest.raises((ValueError, RuntimeError)): raster_load(source_plan(disguised))


@pytest.mark.parametrize('operator,params', [
    ('raster_band_math', {'expression': 'b99'}),
    ('raster_aggregate', {'factors': [0,2], 'method': 'mean'}),
    ('raster_clip', {'boundary_crs': 'EPSG:4326', 'bbox': [1,1,3,3], 'mode': 'invalid'}),
    ('raster_clip', {'boundary_crs': 'EPSG:4326', 'bbox': [1,1,3,3], 'mode': 'outside', 'geometry': {}}),
    ('raster_reclassify', {'rules': [{'min': 0, 'max': 5, 'class': 1}, {'value': 3, 'class': 2}]}),
    ('raster_reclassify', {'rules': '[{"value":2,"class":1}]'}),
    ('raster_clip', {'boundary_crs': 'EPSG:4326', 'geometry': {'type': 'Polygon', 'coordinates': [
        [[-1,-1],[5,-1],[5,5],[-1,5],[-1,-1]],
        [[-0.5,-0.5],[-0.5,4.5],[4.5,4.5],[4.5,-0.5],[-0.5,-0.5]],
    ]}}),
], ids=['invalid-band', 'invalid-aggregate', 'invalid-clip-mode', 'ambiguous-outside-boundary', 'overlapping-classes', 'stringified-rules', 'disjoint-clip'])
def test_failed_dag_cleans_workspace(monkeypatch, raster_file, tmp_path, operator, params):
    from operators.raster_compute import _WORKSPACE
    original = OPERATORS['raster_load']['function']
    paths = []

    def track(*args, **kwargs):
        paths.append(_WORKSPACE.get())
        return original(*args, **kwargs)

    monkeypatch.setitem(OPERATORS['raster_load'], 'function', track)
    target = tmp_path / 'failed.tif'
    original_source = raster_file.read_bytes()
    result = execute_workflow({'tasks': [
        {'id': 'load', 'operator': 'raster_load', 'depends_on': [], 'params': {'access_plan': source_plan(raster_file)}},
        {'id': 'bad', 'operator': operator, 'depends_on': ['load'], 'params': {'input_raster': {'$ref': 'load'}, **params}},
        {'id': 'save', 'operator': 'raster_save', 'depends_on': ['bad'], 'params': {
            'input_raster': {'$ref': 'bad'}, 'access_plan': target_plan(target), 'profile': 'cog',
        }},
    ]})
    assert result['status'] == 'failed'
    assert paths and all(not path.exists() for path in paths)
    assert not target.exists()
    assert raster_file.read_bytes() == original_source


def test_mosaic_preserves_empty_pixels_and_overviews_survive_save(tmp_path):
    left = create_raster(tmp_path / 'left.tif', np.ones((4, 4)))
    right = create_raster(tmp_path / 'right.tif', np.full((4, 4), 3.0), transform=(6, 1, 0, 4, 0, -1))
    with raster_workspace():
        result = raster_mosaic([raster_load(source_plan(left)), raster_load(source_plan(right))], 'EPSG:4326', [1, 1])
        stats = raster_statistics(result)
        assert stats['valid_count'] == 32 and stats['invalid_count'] == 8
        assert stats['mean'] == 2
        assert raster_info(result)['bands'][0]['dtype'] == 'Float64'
        overview = raster_build_overviews(result, levels=[2, 4])
        target = tmp_path / 'overviews.tif'
        raster_save(overview, target_plan(target))
        assert raster_info(raster_load(source_plan(target)))['bands'][0]['overviews'] == [[5, 2], [3, 1]]


def test_direct_cog_api_uses_the_same_access_plan_contract(raster_file, tmp_path):
    import api_server
    target = tmp_path / 'direct.tif'
    plan = {**source_plan(raster_file), 'target': target_plan(target)['target']}
    client = api_server.app.test_client()
    response = client.post('/api/operators/raster_to_cog/invoke', json={'tenant_id':7,'params': {
        'access_plan': plan, 'options': {'source_crs': '+proj=longlat +datum=WGS84 +no_defs'},
    }})
    assert response.status_code == 200, response.json
    assert response.json['result']['profile'] == 'cog'
    assert str(tmp_path) not in json.dumps(response.json)
    with raster_workspace():
        output = raster_load(source_plan(target))
        assert validate_cog(output)['valid']
        assert raster_info(output)['extent_srid'] == 4326
    assert client.post('/api/operators/tiff_to_cog/invoke', json={'params': {}}).status_code == 404
    assert client.post('/api/operators/raster_info/invoke', json={'params': {}}).status_code == 403
    with pytest.raises(ValueError, match='object'):
        raster_to_cog(plan, options=[['compression', 'LZW']])


def test_non_epsg_authority_is_not_reported_as_epsg(monkeypatch):
    import operators.raster_compute as compute
    dataset = gdal.GetDriverByName('MEM').Create('', 2, 2, 1)
    crs = osr.SpatialReference()
    crs.SetFromUserInput('EPSG:3857')
    crs.SetAuthority(None, 'ESRI', 102100)
    dataset.SetSpatialRef(crs)
    assert dataset.GetSpatialRef().GetAuthorityName(None) == 'ESRI'
    monkeypatch.setattr(compute, '_open', lambda _: dataset)
    facts = raster_info(None)
    assert facts['extent_srid'] == 0
    assert facts['source_crs'] == dataset.GetProjection()
    assert not facts['source_crs'].startswith('EPSG:')


def test_raster_http_bounded_admission_direct_sharing_and_policy_failure(monkeypatch):
    import threading
    from types import SimpleNamespace
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    from raster_resources import RasterPolicyUnavailable
    release = threading.Event()
    started, lock = [], threading.Lock()
    class Runner:
        def __init__(self, *, language):
            assert language == 'zh-cn'

        def execute(self, workflow, input_data, **kwargs):
            with lock:
                started.append(workflow)
            assert release.wait(5)
            return SimpleNamespace(final_result='ok', all_results={}, task_order=[])
    monkeypatch.setattr(api_server, 'GeoPythonWorkflowRunner', Runner)
    registry = ExecutionRegistry()
    monkeypatch.setattr(api_server, 'executions', registry)
    client = api_server.app.test_client()
    payload = {'workflow_def': {'tasks': [{'id':'r', 'operator':'raster_load', 'params':{}, 'depends_on':[]}]},
               'runtime': {'tenant_id':7, 'execution_authorization': {'id':1, 'effects':['read']}}}
    try:
        accepted = [client.post('/api/workflow', json=payload) for _ in range(4)]
        assert all(response.status_code == 202 for response in accepted)
        deadline = time.monotonic() + 2
        while len(started) < 2 and time.monotonic() < deadline:
            time.sleep(.01)
        assert len(started) == 2
        statuses = [client.get('/api/executions/' + response.json['execution_id']).json['status'] for response in accepted]
        assert statuses == ['running','running','pending','pending']
        rejected = client.post('/api/workflow', json=payload)
        assert rejected.status_code == 503 and rejected.json['error_code'] == 'RUNTIME_BUSY'
        assert len(registry._executions) == 4
        direct = client.post('/api/operators/raster_to_cog/invoke', json={'tenant_id':7, 'params':{}})
        assert direct.status_code == 503 and direct.json['error_code'] == 'RUNTIME_BUSY'
        assert client.post('/api/operators/raster_to_cog/invoke', json={'params':{}}).status_code == 400
        def unavailable():
            raise RasterPolicyUnavailable('unavailable')
        monkeypatch.setattr(api_server.raster_resources, 'refresh', unavailable)
        failed = client.post('/api/workflow', json=payload)
        assert failed.status_code == 503 and failed.json['error_code'] == 'RUNTIME_POLICY_UNAVAILABLE'
        # Vector execution does not consume raster capacity or fetch raster policy.
        vector = {'workflow_def': {'tasks': [{'id':'v', 'operator':'buffer', 'params':{}, 'depends_on':[]}]},
                  'runtime': {'tenant_id':7, 'execution_authorization': {'id':1, 'effects':['read','write']}}}
        assert client.post('/api/workflow', json=vector).status_code == 202
    finally:
        release.set()
        deadline = time.monotonic() + 2
        while api_server.raster_resources.admission.snapshot()['running'] and time.monotonic() < deadline:
            time.sleep(.01)
        assert api_server.raster_resources.admission.snapshot()['running'] == 0


@pytest.mark.parametrize('pattern', ['hole', 'islands', 'diagonal', 'pinch', 'checkerboard', 'full', 'empty'])
@pytest.mark.parametrize('transform', [(0, 1, 0, 4, 0, -1), (10, 2, .5, 20, .25, -3)])
def test_footprint_exact_pixel_union_holes_and_four_connectivity(tmp_path, pattern, transform):
    from shapely.geometry import Polygon
    from shapely.ops import unary_union
    valid = np.ones((4, 4), dtype=bool)
    if pattern == 'hole': valid[1:3, 1:3] = False
    elif pattern == 'islands': valid[:] = False; valid[0, 0] = valid[3, 3] = True
    elif pattern == 'diagonal': valid = np.eye(4, dtype=bool)
    elif pattern == 'pinch': valid[1, 1] = valid[2, 2] = False
    elif pattern == 'checkerboard': valid = np.indices((4, 4)).sum(axis=0) % 2 == 0
    elif pattern == 'empty': valid[:] = False
    source = create_raster(tmp_path / 'source.tif', np.where(valid, 0., -9999.), transform=transform)
    before = source.read_bytes()
    def point(x, y):
        return (transform[0] + x * transform[1] + y * transform[2], transform[3] + x * transform[4] + y * transform[5])
    expected = unary_union([Polygon([point(x, y), point(x+1, y), point(x+1, y+1), point(x, y+1)])
        for y in range(4) for x in range(4) if valid[y, x]])
    with raster_workspace():
        loaded = raster_load(source_plan(source))
        workspace = loaded.workspace
        result = raster_footprint(loaded)
        assert result.crs.to_epsg() == 4326 and list(result.columns) == ['geometry']
        assert len(result) == int(valid.any())
        if valid.any():
            geometry = result.geometry.iloc[0]
            assert geometry.geom_type == 'MultiPolygon' and geometry.is_valid
            assert geometry.symmetric_difference(expected).area == pytest.approx(0, abs=1e-12)
            assert geometry.area == pytest.approx(valid.sum() * abs(transform[1]*transform[5]-transform[2]*transform[4]))
            if pattern == 'hole': assert sum(len(part.interiors) for part in geometry.geoms) == 1
            if pattern == 'diagonal': assert len(geometry.geoms) == 4
        assert any(path.suffix == '.gpkg' for path in workspace.iterdir())
    assert not workspace.exists() and source.read_bytes() == before


@pytest.mark.parametrize('nodata', [-9999., float('nan')], ids=['finite', 'nan'])
@pytest.mark.parametrize('validity,bands', [('any', None), ('all', None), ('any', [1]), ('all', [2])])
def test_footprint_independent_band_nodata_mask_alpha_zero(tmp_path, nodata, validity, bands):
    from shapely.geometry import box
    from shapely.ops import unary_union
    first = np.zeros((4, 4)); second = np.full((4, 4), 2.)
    first[0, 1], second[0, 2] = nodata, nodata
    first[3, 0], second[3, 1] = np.inf, np.nan
    alpha = np.full((4, 4), 255.); alpha[1, 0], alpha[0, 0], alpha[3, 2] = 0, 128, np.inf
    source = create_raster(tmp_path / 'source.tif', np.stack([first, second, alpha]), nodata=nodata)
    ds = gdal.Open(str(source), gdal.GA_Update)
    ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK', 'YES'):
        ds.CreateMaskBand(gdal.GMF_PER_DATASET)
    mask = np.full((4, 4), 255, dtype=np.uint8); mask[3, 3] = 0
    ds.GetRasterBand(1).GetMaskBand().WriteRaster(0, 0, 4, 4, mask.tobytes()); ds = None
    before = source.read_bytes()
    base = mask.astype(bool) & np.isfinite(alpha) & (alpha > 0)
    selected = [first, second] if bands is None else [first if bands == [1] else second]
    masks = [base & np.isfinite(values) & (values != nodata) for values in selected]
    valid = np.logical_or.reduce(masks) if validity == 'any' else np.logical_and.reduce(masks)
    expected = unary_union([box(x, 3-y, x+1, 4-y) for y in range(4) for x in range(4) if valid[y, x]])
    with raster_workspace():
        result = raster_footprint(raster_load(source_plan(source)), bands=bands, validity=validity)
        assert result.geometry.iloc[0].equals(expected)
    assert source.read_bytes() == before


@pytest.mark.parametrize('params', [{'validity': 'unknown'}, {'validity': True}, {'bands': []}, {'bands': [1, 1]},
    {'bands': [0]}, {'bands': [True]}, {'bands': [1.0]}, {'bands': [4]}, {'bands': '1'}, {'bands': [3]}])
def test_footprint_rejects_invalid_selection(tmp_path, params):
    source = create_raster(tmp_path / 'source.tif', np.ones((3, 4, 4)))
    ds = gdal.Open(str(source), gdal.GA_Update); ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand); ds = None
    with raster_workspace():
        loaded = raster_load(source_plan(source))
        workspace = loaded.workspace
        with pytest.raises(ValueError): raster_footprint(loaded, **params)
    assert not workspace.exists()


@pytest.mark.parametrize('defect', ['crs', 'transform', 'singular', 'nonfinite', 'complex-data', 'complex-alpha', 'alpha-only'])
def test_footprint_rejects_untrustworthy_grid_and_complex_input(tmp_path, defect):
    from operators.raster_compute import _raster, _WORKSPACE
    with raster_workspace():
        workspace = _WORKSPACE.get()
        source = workspace / 'source.tif'
        dtype = gdal.GDT_CFloat64 if defect.startswith('complex') else gdal.GDT_Float64
        ds = gdal.GetDriverByName('GTiff').Create(str(source), 4, 4, 2 if defect == 'complex-alpha' else 1, dtype)
        if defect != 'crs': ds.SetProjection(_crs_wkt_for_footprint(4326))
        if defect != 'transform': ds.SetGeoTransform((0, 1, 2, 4, .5, 1) if defect == 'singular' else (0, float('nan'), 0, 4, 0, -1) if defect == 'nonfinite' else (0, 1, 0, 4, 0, -1))
        if defect in ('complex-alpha', 'alpha-only'): ds.GetRasterBand(ds.RasterCount).SetColorInterpretation(gdal.GCI_AlphaBand)
        ds = None
        with pytest.raises(ValueError): raster_footprint(_raster(source))


def test_footprint_bounded_reads_preserve_cross_block_and_corner_cells(tmp_path, monkeypatch):
    import operators.raster_compute as compute
    from shapely.geometry import box
    from shapely.ops import unary_union
    values = np.full((517, 513), -9999.)
    values[510:515, 509:513] = 0.; values[0, 0] = 1
    source = create_raster(tmp_path / 'source.tif', values, transform=(0, 1, 0, 517, 0, -1))
    expected = unary_union([box(509, 2, 513, 7), box(0, 516, 1, 517)])
    original = compute._read_values; reads = []
    def read(band, x, y, width, height):
        reads.append((width, height)); assert width <= 512 and height <= 512
        return original(band, x, y, width, height)
    monkeypatch.setattr(compute, '_read_values', read)
    with raster_workspace():
        result = raster_footprint(raster_load(source_plan(source)))
        assert result.geometry.iloc[0].equals(expected)
    assert (1, 5) in reads


@pytest.mark.parametrize('invalid', [False, True], ids=['success', 'failed'])
def test_footprint_metadata_vector_port_and_async_vector_composition(tmp_path, monkeypatch, invalid):
    import api_server
    spec = next(item for item in list_operators() if item['id'] == 'raster_footprint')
    assert spec['output_ports'][0]['type'] == 'GeoDataFrame'
    assert spec['execution_modes'] == ['workflow'] and spec['effects'] == ['read']
    assert spec['attributes']['resource_groups'] == ['raster']
    params = {item['name']: item for item in spec['parameters']}
    assert params['validity']['default'] == 'any' and params['validity']['enum'] == ['any', 'all']
    source = create_raster(tmp_path / 'source.tif', np.ones((4, 4)), crs='EPSG:3857')
    before = source.read_bytes()
    definition = {'tasks': [
        {'id': 'load', 'operator': 'raster_load', 'depends_on': [], 'params': {'access_plan': source_plan(source)}},
        {'id': 'footprint', 'operator': 'raster_footprint', 'depends_on': ['load'], 'params': {'input_raster': {'$ref': 'load'}}},
        {'id': 'area', 'operator': 'get_area', 'depends_on': ['footprint'], 'params': {'input_gdf': {'$ref': 'footprint'}}},
    ]}
    if invalid: definition['tasks'][1]['params']['validity'] = 'unknown'
    paths = []
    import workflow_engine
    original = workflow_engine._execute_operator
    def execute(name, params):
        if name == 'raster_footprint': paths.append(params['input_raster'].workspace)
        return original(name, params)
    monkeypatch.setattr(workflow_engine, '_execute_operator', execute)
    client = api_server.app.test_client()
    response = client.post('/api/workflow', json={'workflow_def': definition, 'input_data': {}, 'runtime': {
        'tenant_id': 7, 'execution_authorization': {'id': 1, 'effects': ['read']}}})
    assert response.status_code == 202
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        status = client.get('/api/executions/' + response.json['execution_id']).json
        if status['status'] in ('success', 'failed'): break
        time.sleep(.01)
    assert status['status'] == ('failed' if invalid else 'success'), status
    assert paths and all(not path.exists() for path in paths) and source.read_bytes() == before
    if not invalid:
        result = json.loads(status['result'])
        assert result['type'] == 'FeatureCollection' and result['features'][0]['geometry']['type'] == 'MultiPolygon'
        from pyproj import CRS
        assert CRS.from_user_input(result['crs']['properties']['name']).to_epsg() == 3857
        assert result['features'][0]['properties']['area'] == 16
    assert client.post('/api/operators/raster_footprint/invoke', json={'params': {}}).status_code == 403


def _crs_wkt_for_footprint(code):
    reference = osr.SpatialReference()
    reference.ImportFromEPSG(code)
    return reference.ExportToWkt()


@pytest.mark.parametrize('nodata', [-9999., float('nan')], ids=['finite', 'nan'])
@pytest.mark.parametrize('transform', [(0,1,0,2,0,-1), (3,2,.5,8,-.25,-2)])
def test_update_independent_validity_zero_alpha_masks_and_cog(tmp_path, nodata, transform):
    base = create_raster(tmp_path/'base.tif', np.array([
        [[10,nodata,12,13],[14,15,16,17]],
        [[20,21,nodata,23],[24,25,26,27]],
        [[255,255,255,255],[0,128,255,255]],
    ]), transform=transform, nodata=nodata)
    patch = create_raster(tmp_path/'patch.tif', np.array([
        [[nodata,0,102,np.nan],[104,nodata,np.inf,107]],
        [[200,nodata,202,203],[204,205,206,nodata]],
        [[255,128,0,255],[128,255,255,0]],
    ]), transform=transform, nodata=nodata)
    for path in (base,patch):
        ds=gdal.Open(str(path),gdal.GA_Update)
        ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
        ds=None
    originals={p:p.read_bytes() for p in (base,patch)}
    expected=np.array([[[10,0,12,13],[104,15,16,17]],
                       [[200,21,np.nan,203],[204,25,206,27]]])
    with raster_workspace():
        loaded, update = raster_load(source_plan(base)), raster_load(source_plan(patch))
        ds=gdal.Open(str(update.path),gdal.GA_Update)
        assert ds.CreateMaskBand(gdal.GMF_PER_DATASET)==0
        ds.GetRasterBand(1).GetMaskBand().WriteRaster(0,0,4,2,bytes([255]*5+[0]+[255]*2))
        ds=None
        private={p:p.read_bytes() for p in update.path.parent.glob(update.path.name+'*')}
        result=raster_update(loaded,update)
        ds=gdal.Open(str(result.path))
        assert ds.RasterCount==2 and ds.GetGeoTransform()==transform
        for index in (1,2):
            band=ds.GetRasterBand(index)
            np.testing.assert_allclose(read_band_values(band),expected[index-1],equal_nan=True)
            assert band.DataType==gdal.GDT_Float64 and math.isnan(band.GetNoDataValue())
            assert band.GetColorInterpretation()!=gdal.GCI_AlphaBand
        band=ds=None
        target=tmp_path/'updated.cog.tif'
        saved=raster_save(result,target_plan(target),profile='cog',blocksize=128)
        assert saved['size_bytes']==target.stat().st_size>0
        reread=raster_load(source_plan(target))
        assert validate_cog(reread)['valid']
        ds=gdal.Open(str(reread.path))
        for index in (1,2):
            band=ds.GetRasterBand(index)
            np.testing.assert_allclose(read_band_values(band),expected[index-1],equal_nan=True)
            assert band.GetMaskBand().ReadRaster()==bytes(255 if math.isfinite(v) else 0 for v in expected[index-1].flat)
        band=ds=None
        assert all(p.read_bytes()==b for p,b in private.items())
    assert all(p.read_bytes()==b for p,b in originals.items())


@pytest.mark.parametrize('fault', ['width','transform','rotation','crs','missing-crs','missing-grid','singular','nonfinite','bands','complex','alpha-only'])
def test_update_rejects_grid_and_band_mismatch(tmp_path,fault):
    base=create_raster(tmp_path/'base.tif',np.ones((2,3)))
    values=np.ones((2,4) if fault=='width' else (2,2,3) if fault=='bands' else (2,3))
    transform=(0,1,0,4,0,-1)
    transform={'transform':(1,1,0,4,0,-1),'rotation':(0,1,.1,4,0,-1),
               'missing-grid':None,'singular':(0,1,1,4,1,1),
               'nonfinite':(0,float('nan'),0,4,0,-1)}.get(fault,transform)
    patch=create_raster(tmp_path/'patch.tif',values,transform=transform,
                        crs='' if fault=='missing-crs' else 'EPSG:3857' if fault=='crs' else 'EPSG:4326')
    if fault=='complex':
        converted=gdal.Translate(str(tmp_path/'complex.tif'),str(patch),outputType=gdal.GDT_CFloat64)
        converted=None; patch=tmp_path/'complex.tif'
    if fault=='alpha-only':
        ds=gdal.Open(str(patch),gdal.GA_Update); ds.GetRasterBand(1).SetColorInterpretation(gdal.GCI_AlphaBand); ds=None
    with raster_workspace():
        loaded,update=raster_load(source_plan(base)),raster_load(source_plan(patch))
        with pytest.raises(ValueError): raster_update(loaded,update)


def test_update_chained_priority_and_bounded_reads(tmp_path,monkeypatch):
    import operators.raster_compute as compute
    original=compute._read_values
    reads=[]
    def bounded(band,x,y,width,height):
        assert width<=512 and height<=512
        reads.append((width,height))
        return original(band,x,y,width,height)
    monkeypatch.setattr(compute,'_read_values',bounded)
    base=create_raster(tmp_path/'base.tif',np.full((513,1025),7.))
    first=np.full((513,1025),-9999.); first[511:513,511:513]=11
    second=np.full((513,1025),-9999.); second[512,512]=0; second[-1,-1]=13
    paths=[base,create_raster(tmp_path/'first.tif',first),create_raster(tmp_path/'second.tif',second)]
    originals={p:p.read_bytes() for p in paths}
    with raster_workspace():
        a,b,c=[raster_load(source_plan(p)) for p in paths]
        result=raster_update(raster_update(a,b),c)
        ds=gdal.Open(str(result.path)); band=ds.GetRasterBand(1)
        expected=np.full((513,1025),7.); expected[511:513,511:513]=11; expected[512,512]=0; expected[-1,-1]=13
        np.testing.assert_array_equal(read_band_values(band),expected)
        band=ds=None
    assert len(reads)>12 and all(p.read_bytes()==b for p,b in originals.items())


@pytest.mark.parametrize('invalid',[False,True])
def test_update_workflow_metadata_async_save_and_cleanup(tmp_path,monkeypatch,invalid):
    import api_server
    import operators.raster_compute as compute
    base=create_raster(tmp_path/'base.tif',np.full((2,3),10.))
    patch=create_raster(tmp_path/'patch.tif',np.ones((2,4)) if invalid else np.array([[-9999,0,22],[23,-9999,25.]]))
    target=tmp_path/'result.tif'
    spec=next(item for item in list_operators() if item['id']=='raster_update')
    assert [p['name'] for p in spec['parameters'] if p['type']=='raster']==['input_raster','update_raster']
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read']
    assert spec['output_ports'][0]['type']=='raster' and spec['attributes']['resource_groups']==['raster']
    tasks=[{'id':'base','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(base)}},
           {'id':'patch','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(patch)}},
           {'id':'update','operator':'raster_update','depends_on':['base','patch'],
            'params':{'input_raster':{'$ref':'base'},'update_raster':{'$ref':'patch'}}},
           {'id':'save','operator':'raster_save','depends_on':['update'],
            'params':{'input_raster':{'$ref':'update'},'access_plan':target_plan(target),'profile':'cog'}}]
    # Use the same owner test HTTP entry as other real asynchronous DAG cases.
    client=api_server.app.test_client()
    import workflow_engine
    paths=[]
    original=workflow_engine._execute_operator
    def execute(name,params):
        if name=='raster_update': paths.append(params['input_raster'].workspace)
        return original(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',execute)
    response=client.post('/api/workflow',json={'workflow_def':{'tasks':tasks},'input_data':{},
        'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert response.status_code==202
    deadline=time.monotonic()+10
    while time.monotonic()<deadline:
        status=client.get('/api/executions/'+response.json['execution_id']).json
        if status['status'] in ('success','failed'): break
        time.sleep(.01)
    assert status['status']==('failed' if invalid else 'success'),status
    if not invalid:
        result=json.loads(status['result']); assert result['size_bytes']==target.stat().st_size>0
        ds=gdal.Open(str(target)); band=ds.GetRasterBand(1)
        np.testing.assert_array_equal(read_band_values(band),[[10,0,22],[23,10,25]])
        band=ds=None
    assert paths and all(not path.exists() for path in paths)
    assert client.post('/api/operators/raster_update/invoke',json={'params':{}}).status_code==403


@pytest.mark.parametrize('base_valid',[False,True])
def test_update_all_invalid_patch_keeps_base_validity(tmp_path,base_valid):
    base=create_raster(tmp_path/'base.tif',np.full((2,3),7. if base_valid else -9999.))
    patch=create_raster(tmp_path/'patch.tif',np.full((2,3),float('nan')),nodata=float('nan'))
    with raster_workspace():
        result=raster_update(raster_load(source_plan(base)),raster_load(source_plan(patch)))
        ds=gdal.Open(str(result.path)); band=ds.GetRasterBand(1)
        np.testing.assert_allclose(read_band_values(band),np.full((2,3),7. if base_valid else np.nan),equal_nan=True)
        band=ds=None


def tile_target_plan(path, mode='create'):
    plan = target_plan(path, mode)
    plan['target']['kind'] = 'directory'
    return plan


@pytest.mark.parametrize('profile', ['geotiff', 'cog'])
@pytest.mark.parametrize('kind', ['uint64', 'rgba', 'palette', 'mask', 'nan', 'complex'])
def test_raster_tiles_exact_windows_dtype_masks_colors_and_source_immutable(tmp_path, profile, kind):
    source = tmp_path/'source.tif'
    dtype = {'uint64': gdal.GDT_UInt64, 'nan': gdal.GDT_Float64, 'complex': gdal.GDT_CFloat64}.get(kind, gdal.GDT_Byte)
    count = 4 if kind == 'rgba' else 1
    ds = gdal.GetDriverByName('GTiff').Create(str(source), 7, 5, count, dtype)
    transform = (110., 2., .5, 20., -.25, -3.) if kind != 'uint64' else None
    if transform:
        ds.SetGeoTransform(transform)
        crs = osr.SpatialReference(); crs.ImportFromEPSG(4326); ds.SetProjection(crs.ExportToWkt())
    if kind == 'palette':
        palette = gdal.ColorTable()
        for value in range(256): palette.SetColorEntry(value, (value, 255-value, 0, 255))
        ds.GetRasterBand(1).SetRasterColorTable(palette)
    for index in range(1, count+1):
        values = np.arange(35, dtype=np.uint64 if kind == 'uint64' else np.float64).reshape(5,7) + index
        if kind == 'uint64': values += 2**53
        if kind == 'nan':
            values[:3,:4] = np.nan
            ds.GetRasterBand(index).SetNoDataValue(float('nan'))
        if kind == 'complex': values = values + 1j*(values+2)
        if kind == 'rgba':
            ds.GetRasterBand(index).SetColorInterpretation([gdal.GCI_RedBand,gdal.GCI_GreenBand,gdal.GCI_BlueBand,gdal.GCI_AlphaBand][index-1])
            if index == 4: values.fill(255); values[0,0] = 0; values[4,6] = 128
        ds.GetRasterBand(index).WriteArray(values)
    if kind == 'mask':
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK', 'YES'):
            ds.CreateMaskBand(gdal.GMF_PER_DATASET)
        mask = np.full((5,7),255,dtype=np.uint8); mask[:3,:4] = 0
        ds.GetRasterBand(1).GetMaskBand().WriteArray(mask)
    ds = None
    original = source.read_bytes()
    with raster_workspace():
        loaded = raster_load(source_plan(source))
        tiles = raster_tile(loaded, [4,3])
        assert raster_tiles_info(tiles)['total_tiles'] == 4
        target = tmp_path/'tiles'
        result = raster_save_tiles(tiles, tile_target_plan(target), profile=profile, blocksize=128)
        assert result['total_tiles'] == result['completed_tiles'] == 4
        assert set(path.name for path in target.iterdir()) == {'r0_c0.tif','r0_c4.tif','r3_c0.tif','r3_c4.tif'}
        coverage = np.zeros((5,7),dtype=np.uint8)
        original_ds = gdal.Open(str(source))
        for key, fact in result['tiles'].items():
            x,y,w,h = fact['window']; output = gdal.Open(str(target/fact['name']))
            assert fact['size_bytes'] == (target/fact['name']).stat().st_size > 0
            expected = (transform[0]+x*transform[1]+y*transform[2],transform[1],transform[2],transform[3]+x*transform[4]+y*transform[5],transform[4],transform[5]) if transform else None
            assert output.GetGeoTransform(can_return_null=True) == expected
            assert output.GetProjection() == original_ds.GetProjection()
            assert len(output.GetFileList()) == 1
            for index in range(1,count+1):
                actual, wanted = output.GetRasterBand(index), original_ds.GetRasterBand(index)
                assert actual.DataType == wanted.DataType
                assert actual.ReadRaster() == wanted.ReadRaster(x,y,w,h)
                assert actual.GetMaskBand().ReadRaster() == wanted.GetMaskBand().ReadRaster(x,y,w,h)
                assert actual.GetColorInterpretation() == wanted.GetColorInterpretation()
                if kind == 'palette': assert actual.GetRasterColorTable().GetColorEntry(19) == (19,236,0,255)
                actual = wanted = None
            coverage[y:y+h,x:x+w] += 1
            output = None
            reloaded = raster_load(source_plan(target/fact['name']))
            if profile == 'cog': assert validate_cog(reloaded)['valid']
        original_ds = None
        assert np.all(coverage == 1)
        # Materialized intermediates are reclaimed per tile; only source snapshots remain.
        assert len(list(loaded.workspace.glob('*.tif'))) == 5  # source plus four verification reloads
    assert source.read_bytes() == original
    with raster_workspace():
        with pytest.raises(ValueError): raster_tiles_info(tiles)


@pytest.mark.parametrize('size', [[0,2],[True,2],[1.5,2],[2**31,2],[2],[],None])
def test_raster_tiles_reject_invalid_grid_and_public_forgery(tmp_path, size):
    source = create_raster(tmp_path/'source.tif', np.ones((2,3)))
    with raster_workspace():
        with pytest.raises(ValueError): raster_tile(raster_load(source_plan(source)), size)
        with pytest.raises(ValueError): raster_save_tiles({'runtime_type':'raster_tiles'},tile_target_plan(tmp_path/'tiles'))


@pytest.mark.parametrize('failure', ['before_publish', 'unknown_publish', 'later_node', 'cleanup_mid', 'cleanup_final'])
def test_raster_tiles_async_partial_failure_preserves_files_ledger_and_cleans_workspace(tmp_path, monkeypatch, failure):
    import api_server
    import operators.raster_compute as compute
    import workflow_engine
    source = create_raster(tmp_path/'source.tif', np.arange(35,dtype=float).reshape(5,7))
    target = tmp_path/'tiles'
    original = compute.publish_target_file
    calls, workspaces, published_paths, cleanup_failures = [], [], [], []
    def publish(path, plan):
        calls.append(plan['target']['name'])
        if len(calls) == 3 and failure == 'before_publish': raise OSError('private endpoint must not leak')
        result = original(path, plan)
        published_paths.append(path)
        if len(calls) == 3 and failure == 'unknown_publish': raise OSError('private endpoint must not leak')
        return result
    monkeypatch.setattr(compute, 'publish_target_file', publish)
    unlink = Path.unlink
    def fail_cleanup(path, *args, **kwargs):
        limit = {'cleanup_mid': 2, 'cleanup_final': 4}.get(failure)
        if limit and len(published_paths) == limit and path in published_paths and not cleanup_failures:
            cleanup_failures.append(path)
            raise OSError('private cleanup path must not leak')
        return unlink(path, *args, **kwargs)
    monkeypatch.setattr(Path, 'unlink', fail_cleanup)
    execute = workflow_engine._execute_operator
    def tracked(name, params):
        if name == 'raster_tile': workspaces.append(params['input_raster'].workspace)
        return execute(name, params)
    monkeypatch.setattr(workflow_engine, '_execute_operator', tracked)
    tasks = [
        {'id':'load','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(source)}},
        {'id':'tile','operator':'raster_tile','depends_on':['load'],'params':{'input_raster':{'$ref':'load'},'tile_size':[4,3]}},
        {'id':'save','operator':'raster_save_tiles','depends_on':['tile'],'params':{'input_tiles':{'$ref':'tile'},'access_plan':tile_target_plan(target),'profile':'cog','blocksize':128}},
    ]
    if failure == 'later_node': tasks.append({'id':'fail','operator':'raster_statistics','depends_on':['load','save'],'params':{'input_raster':{'$ref':'load'},'band':99}})
    client = api_server.app.test_client()
    response = client.post('/api/workflow',json={'workflow_def':{'tasks':tasks},'input_data':{},'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert response.status_code == 202
    deadline = time.monotonic()+10
    while time.monotonic() < deadline:
        status = client.get('/api/executions/'+response.json['execution_id']).json
        if status['status'] in ('success','failed'): break
        time.sleep(.01)
    assert status['status'] == 'failed',status
    saved = json.loads(status['all_results']['save'])
    assert saved['completed_tiles'] == (4 if failure in ('later_node', 'cleanup_final') else 2)
    assert len(saved['tiles']) == saved['completed_tiles']
    for fact in saved['tiles'].values():
        assert (target / fact['name']).stat().st_size == fact['size_bytes'] > 0
    assert len(list(target.glob('*.tif'))) == {'before_publish':2,'unknown_publish':3,'later_node':4,'cleanup_mid':2,'cleanup_final':4}[failure]
    if failure in ('before_publish', 'unknown_publish'):
        assert saved['failed_tile'] == 'r3_c0' and saved['publication'] == 'unknown'
    elif failure == 'cleanup_mid':
        assert saved['failed_tile'] == 'r3_c0' and saved['publication'] == 'not_attempted'
    else:
        assert 'failed_tile' not in saved and 'publication' not in saved
    if failure.startswith('cleanup_'):
        assert len(cleanup_failures) == 1
    assert 'private endpoint' not in json.dumps(status) and 'private cleanup' not in json.dumps(status)
    assert all(not path.exists() for path in workspaces)
    assert client.post('/api/operators/raster_tile/invoke',json={'params':{}}).status_code == 403
    assert client.post('/api/operators/raster_save_tiles/invoke',json={'params':{}}).status_code == 403


@pytest.mark.parametrize('size,windows', [([20,20], [[0,0,7,5]]), ([1,1], [[x,y,1,1] for y in range(5) for x in range(7)])])
def test_raster_tiles_single_and_pixel_tiles_are_reusable(tmp_path, size, windows):
    source = create_raster(tmp_path/'source.tif', np.arange(35,dtype=float).reshape(5,7))
    with raster_workspace():
        collection = raster_tile(raster_load(source_plan(source)), size)
        first = raster_save_tiles(collection, tile_target_plan(tmp_path/'first'))
        second = raster_save_tiles(collection, tile_target_plan(tmp_path/'second'))
        assert first == second
        assert [fact['window'] for fact in first['tiles'].values()] == windows
        assert first['completed_tiles'] == len(windows)


def test_raster_tiles_create_refuses_nonempty_scope_replace_preserves_other_files(tmp_path):
    from addp_common.workflow_runtime import WorkflowExecutionError
    source = create_raster(tmp_path/'source.tif', np.arange(35,dtype=float).reshape(5,7))
    target = tmp_path/'tiles'; target.mkdir()
    unrelated = target/'keep.txt'; unrelated.write_text('retain')
    with raster_workspace():
        collection = raster_tile(raster_load(source_plan(source)), [4,3])
        with pytest.raises(WorkflowExecutionError) as failure:
            raster_save_tiles(collection, tile_target_plan(target))
        manifest = failure.value.partial_result
        assert manifest['completed_tiles'] == 0 and manifest['tiles'] == {}
        assert manifest['failed_tile'] == '' and manifest['publication'] == 'not_attempted'
        assert list(target.iterdir()) == [unrelated]
        result = raster_save_tiles(collection, tile_target_plan(target, 'replace'))
        assert result['completed_tiles'] == 4 and unrelated.read_text() == 'retain'
        for fact in result['tiles'].values():
            assert (target/fact['name']).stat().st_size == fact['size_bytes'] > 0


def test_raster_tiles_encoding_failure_retains_confirmed_prefix_without_attempting_next_publish(tmp_path, monkeypatch):
    from addp_common.workflow_runtime import WorkflowExecutionError
    import operators.raster_compute as compute
    source = create_raster(tmp_path/'source.tif', np.arange(35,dtype=float).reshape(5,7))
    target = tmp_path/'tiles'
    encode, calls = compute._encode_raster, []
    def fail_third(*args):
        calls.append(args[0])
        if len(calls) == 3: raise OSError('private encoding failure')
        return encode(*args)
    monkeypatch.setattr(compute, '_encode_raster', fail_third)
    with raster_workspace():
        collection = raster_tile(raster_load(source_plan(source)), [4,3])
        with pytest.raises(WorkflowExecutionError) as failure:
            raster_save_tiles(collection, tile_target_plan(target))
        manifest = failure.value.partial_result
        assert manifest['completed_tiles'] == 2 and manifest['failed_tile'] == 'r3_c0'
        assert manifest['publication'] == 'not_attempted'
        assert {p.name for p in target.iterdir()} == {'r0_c0.tif','r0_c4.tif'}
        assert all(not raster.path.exists() for raster in calls)


def _classification_region_oracle(values, valid, transform, connectivity):
    from shapely.geometry import Polygon
    from shapely.ops import unary_union
    unseen = set(zip(*np.where(valid)))
    regions = []
    offsets = [(y, x) for y in (-1, 0, 1) for x in (-1, 0, 1)
               if (y or x) and (connectivity == 8 or abs(y) + abs(x) == 1)]
    def point(x, y):
        return (transform[0] + x * transform[1] + y * transform[2],
                transform[3] + x * transform[4] + y * transform[5])
    while unseen:
        start = min(unseen); unseen.remove(start)
        code = int(values[start]); pending = [start]; cells = []
        while pending:
            y, x = pending.pop()
            cells.append(Polygon([point(x,y), point(x+1,y), point(x+1,y+1), point(x,y+1)]))
            for dy, dx in offsets:
                other = (y+dy, x+dx)
                if other in unseen and int(values[other]) == code:
                    unseen.remove(other); pending.append(other)
        regions.append((code, unary_union(cells)))
    return regions


@pytest.mark.parametrize('connectivity', [4, 8])
@pytest.mark.parametrize('transform', [(0,1,0,5,0,-1), (100,2,.5,200,.25,-3)])
def test_raster_polygonize_independent_components_holes_codes_and_source(tmp_path, connectivity, transform):
    from operators.raster_compute import raster_polygonize
    values = np.array([[0,0,0,0,0], [0,2,2,2,0], [0,2,-9999,2,0],
                       [0,2,2,2,0], [-3,0,0,0,-3]], dtype=np.float64)
    valid = values != -9999
    expected = _classification_region_oracle(values, valid, transform, connectivity)
    path = create_raster(tmp_path/'classes.tif', values, transform=transform)
    before = path.read_bytes()
    with raster_workspace():
        loaded = raster_load(source_plan(path)); workspace = loaded.workspace
        result = raster_polygonize(loaded, 1, connectivity)
        assert list(result.columns) == ['class_value', 'geometry']
        assert str(result.class_value.dtype) == 'int64' and result.crs.to_epsg() == 4326
        actual = list(zip(result.class_value, result.geometry))
        assert len(actual) == len(expected)
        for code, geometry in expected:
            matches = [(c,g) for c,g in actual if c == code and g.equals(geometry)]
            assert len(matches) == 1
        assert result.geometry.is_valid.all() and not result.geometry.has_z.any()
        assert all(g.geom_type == 'MultiPolygon' for g in result.geometry)
        assert any(len(part.interiors) for g in result.geometry for part in g.geoms)
    assert not workspace.exists() and before == path.read_bytes()


@pytest.mark.parametrize('connectivity,regions', [(4, 2), (8, 1)])
def test_raster_polygonize_diagonal_connectivity_keeps_valid_multipart(tmp_path, connectivity, regions):
    from operators.raster_compute import raster_polygonize
    path = create_raster(tmp_path/'diagonal.tif', np.array([[7,-9999],[-9999,7]]))
    with raster_workspace():
        result = raster_polygonize(raster_load(source_plan(path)), 1, connectivity)
        assert len(result) == regions and result.class_value.tolist() == [7]*regions
        assert result.geometry.is_valid.all()
        assert sum(g.area for g in result.geometry) == 2
        assert sum(len(g.geoms) for g in result.geometry) == 2


def test_raster_polygonize_selected_band_mask_alpha_and_nonfinite_values(tmp_path):
    from operators.raster_compute import raster_polygonize
    values = np.array([[0,1,2,3], [4,np.nan,np.inf,7], [8,9,-9999,11]], dtype=np.float64)
    alpha = np.full(values.shape, 255.); alpha[0,1] = 0; alpha[0,2] = 64; alpha[2,0] = np.nan
    path = create_raster(tmp_path/'validity.tif', np.stack([np.full(values.shape,.5),values,alpha]))
    ds = gdal.Open(str(path), gdal.GA_Update)
    ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK', 'YES'): ds.CreateMaskBand(gdal.GMF_PER_DATASET)
    mask = np.full(values.shape,255,dtype=np.uint8); mask[2,1] = 0
    ds.GetRasterBand(1).GetMaskBand().WriteArray(mask); ds = None
    valid = (mask != 0) & np.isfinite(values) & (values != -9999) & np.isfinite(alpha) & (alpha > 0)
    expected = _classification_region_oracle(values, valid, (0,1,0,4,0,-1), 4)
    with raster_workspace():
        result = raster_polygonize(raster_load(source_plan(path)), 2)
        assert sorted(result.class_value) == sorted(code for code,_ in expected)
        assert 0 in result.class_value.to_list() and 2 in result.class_value.to_list()
        for code, geometry in expected:
            assert result[result.class_value == code].geometry.iloc[0].equals(geometry)


@pytest.mark.parametrize('params', [{'band':True}, {'band':1.}, {'band':0}, {'band':3},
    {'band':1,'connectivity':True}, {'band':1,'connectivity':4.}, {'band':1,'connectivity':6}])
def test_raster_polygonize_rejects_invalid_band_and_connectivity(tmp_path, params):
    from operators.raster_compute import raster_polygonize
    path = create_raster(tmp_path/'classes.tif', np.ones((2,3,3)))
    ds = gdal.Open(str(path), gdal.GA_Update); ds.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand); ds = None
    with raster_workspace(), pytest.raises(ValueError): raster_polygonize(raster_load(source_plan(path)), **params)


@pytest.mark.parametrize('dtype', [gdal.GDT_Float32, gdal.GDT_Float64])
@pytest.mark.parametrize('value', [.5, -(2**53), 2**53])
def test_raster_polygonize_rejects_lossy_float_classes(tmp_path, value, dtype):
    from operators.raster_compute import raster_polygonize
    path = tmp_path/'classes.tif'
    ds = gdal.GetDriverByName('GTiff').Create(str(path), 2, 1, 1, dtype)
    ds.SetGeoTransform((0,1,0,1,0,-1)); ds.SetProjection(_crs_wkt_for_footprint(3857))
    ds.GetRasterBand(1).WriteArray(np.array([[value,1.]], dtype=np.float64)); ds=None
    with raster_workspace(), pytest.raises(ValueError, match='exact safe integers'):
        raster_polygonize(raster_load(source_plan(path)), 1)


def test_raster_polygonize_float32_safe_integral_codes_remain_valid(tmp_path):
    from operators.raster_compute import raster_polygonize
    path = tmp_path/'float32.tif'
    ds = gdal.GetDriverByName('GTiff').Create(str(path), 3, 1, 1, gdal.GDT_Float32)
    ds.SetGeoTransform((0,1,0,1,0,-1)); ds.SetProjection(_crs_wkt_for_footprint(3857))
    ds.GetRasterBand(1).WriteArray(np.array([[0,-3,2**52]], dtype=np.float32)); ds=None
    with raster_workspace():
        result = raster_polygonize(raster_load(source_plan(path)), 1)
        assert sorted(result.class_value.to_list()) == [-3,0,2**52]
        assert str(result.class_value.dtype) == 'int64' and result.geometry.area.sum() == 3


@pytest.mark.parametrize('values,dtype', [
    ([-2**63,2**53+1,2**53+2,2**63-1], gdal.GDT_Int64),
    ([0,2**63-1], gdal.GDT_UInt64),
])
def test_raster_polygonize_preserves_native_integer_precision(tmp_path, values, dtype):
    from operators.raster_compute import raster_polygonize
    path = tmp_path/'native.tif'
    ds = gdal.GetDriverByName('GTiff').Create(str(path), len(values), 1, 1, dtype)
    ds.SetGeoTransform((0,1,0,1,0,-1)); ds.SetProjection(_crs_wkt_for_footprint(3857))
    ds.GetRasterBand(1).WriteArray(np.array([values], dtype=np.int64 if dtype==gdal.GDT_Int64 else np.uint64)); ds=None
    with raster_workspace():
        result = raster_polygonize(raster_load(source_plan(path)), 1)
        assert sorted(result.class_value.to_list()) == sorted(values)


def test_raster_polygonize_uint64_overflow_and_native_nodata_precision(tmp_path):
    from operators.raster_compute import raster_polygonize
    path = tmp_path/'native.tif'
    ds = gdal.GetDriverByName('GTiff').Create(str(path), 2, 1, 1, gdal.GDT_UInt64)
    ds.SetGeoTransform((0,1,0,1,0,-1)); ds.SetProjection(_crs_wkt_for_footprint(3857))
    ds.GetRasterBand(1).WriteArray(np.array([[2**63,2**63+1]],dtype=np.uint64)); ds=None
    with raster_workspace(), pytest.raises(ValueError, match='signed Int64'):
        raster_polygonize(raster_load(source_plan(path)), 1)
    ds=gdal.Open(str(path),gdal.GA_Update)
    ds.GetRasterBand(1).WriteArray(np.array([[2**53,2**53+1]],dtype=np.uint64))
    ds.GetRasterBand(1).SetNoDataValue(2**53); ds=None
    with raster_workspace():
        result=raster_polygonize(raster_load(source_plan(path)),1)
        assert result.class_value.to_list()==[2**53+1]


@pytest.mark.parametrize('defect', ['crs','transform','singular','complex','complex_alpha'])
def test_raster_polygonize_rejects_untrustworthy_grid_and_complex(tmp_path,defect):
    from operators.raster_compute import raster_polygonize, _raster
    path=create_raster(tmp_path/'invalid.tif',np.ones((2,2)),crs='' if defect=='crs' else 'EPSG:3857',
        transform=None if defect=='transform' else (0,1,0,0,0,0) if defect=='singular' else (0,1,0,2,0,-1))
    if defect in ('complex','complex_alpha'):
        ds=gdal.Translate(str(tmp_path/'converted.tif'),str(path),outputType=gdal.GDT_CFloat64)
        if defect=='complex_alpha':
            ds.GetRasterBand(1).SetColorInterpretation(gdal.GCI_AlphaBand)
        ds=None;path=tmp_path/'converted.tif'
    with raster_workspace(),pytest.raises(ValueError):raster_polygonize(_raster(path),1)


def test_raster_polygonize_empty_schema_and_bounded_cross_block_reads(tmp_path,monkeypatch):
    from operators.raster_compute import raster_polygonize
    empty=create_raster(tmp_path/'empty.tif',np.full((3,3),-9999.))
    with raster_workspace():
        result=raster_polygonize(raster_load(source_plan(empty)),1)
        assert result.empty and str(result.class_value.dtype)=='int64' and result.crs.to_epsg()==4326
    path=create_raster(tmp_path/'large.tif',np.zeros((515,517)))
    original=gdal.Band.ReadAsArray;windows=[]
    def read(band,x,y,width,height,*args,**kwargs):
        windows.append((width,height));return original(band,x,y,width,height,*args,**kwargs)
    monkeypatch.setattr(gdal.Band,'ReadAsArray',read)
    with raster_workspace():
        result=raster_polygonize(raster_load(source_plan(path)),1)
        assert len(result)==1 and result.geometry.iloc[0].area==515*517
    assert len(windows)>=4 and all(w<=512 and h<=512 for w,h in windows)


@pytest.mark.parametrize('invalid', [False, True], ids=['saved','failure-cleanup'])
def test_raster_polygonize_async_dag_reclassification_vector_save_and_cleanup(tmp_path, monkeypatch, invalid):
    import api_server
    import workflow_engine
    import geopandas as gpd
    source=create_raster(tmp_path/'source.tif',np.array([[.25,.25,1.25],[.25,1.25,1.25]]),crs='EPSG:3857')
    before=source.read_bytes();target=tmp_path/'regions.gpkg'
    tasks=[
        {'id':'load','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(source)}},
        {'id':'classify','operator':'raster_reclassify','depends_on':['load'],'params':{'input_raster':{'$ref':'load'},'rules':[{'min':0,'max':1,'class':0},{'min':1,'max':2,'class':2}]}},
        {'id':'regions','operator':'raster_polygonize','depends_on':['classify'],'params':{'input_raster':{'$ref':'classify'},'band':1,'connectivity':6 if invalid else 4}},
        {'id':'save','operator':'save','depends_on':['regions'],'params':{'input_df':{'$ref':'regions'},'path':target.name,'connection_info':{'mount_path':str(tmp_path)}}},
    ]
    paths=[];original=workflow_engine._execute_operator
    def execute(name,params):
        if name=='raster_polygonize':paths.append(params['input_raster'].workspace)
        return original(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',execute)
    client=api_server.app.test_client()
    response=client.post('/api/workflow',json={'workflow_def':{'tasks':tasks},'input_data':{},'runtime':{
        'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert response.status_code==202,response.json
    deadline=time.monotonic()+10
    while time.monotonic()<deadline:
        status=client.get('/api/executions/'+response.json['execution_id']).json
        if status['status'] in ('success','failed'):break
        time.sleep(.01)
    assert status['status']==('failed' if invalid else 'success'),status
    assert paths and all(not path.exists() for path in paths) and source.read_bytes()==before
    if invalid:assert not target.exists()
    else:
        saved=gpd.read_file(target)
        assert list(saved.columns)==['class_value','geometry'] and sorted(saved.class_value)==[0,2]
        assert saved.crs.to_epsg()==3857 and saved.geometry.is_valid.all()
        assert saved.geometry.area.sum()==6 and target.stat().st_size>0
    assert client.post('/api/operators/raster_polygonize/invoke',json={'params':{}}).status_code==403


def test_raster_polygonize_metadata_and_executable_example(tmp_path):
    spec=next(item for item in list_operators() if item['id']=='raster_polygonize')
    params={item['name']:item for item in spec['parameters']}
    assert params['band']['required'] is True and params['connectivity']['default']==4
    assert spec['output_ports'][0]['type']=='GeoDataFrame'
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read']
    assert spec['attributes']['resource_groups']==['raster']
    assert not {'locator','path','engine_id','access_plan'} & params.keys()
    from operators.raster_compute import raster_polygonize
    path=create_raster(tmp_path/'example.tif',np.ones((3,3)))
    with raster_workspace():
        args=dict(spec['detailed_description']['workflow_example']['params']);args['input_raster']=raster_load(source_plan(path))
        assert raster_polygonize(**args).class_value.tolist()==[1]


def test_raster_polygonize_code_mapping_remains_consistent_across_blocks(tmp_path):
    from operators.raster_compute import raster_polygonize
    values=np.full((3,1027),2**53+1,dtype=np.int64);values[1,510:514]=2**53+2
    path=tmp_path/'cross-block.tif'
    ds=gdal.GetDriverByName('GTiff').Create(str(path),1027,3,1,gdal.GDT_Int64)
    ds.SetGeoTransform((0,1,0,3,0,-1));ds.SetProjection(_crs_wkt_for_footprint(3857))
    ds.GetRasterBand(1).WriteArray(values);ds=None
    with raster_workspace():
        result=raster_polygonize(raster_load(source_plan(path)),1)
        assert len(result)==2
        assert dict(zip(result.class_value.to_list(), result.geometry.area))=={2**53+1:3077,2**53+2:4}


def test_raster_polygonize_actual_gpkg_save_preserves_large_codes(tmp_path):
    from operators.raster_compute import raster_polygonize
    from operators.io_operators import save
    import geopandas as gpd
    path=tmp_path/'large.tif';target=tmp_path/'large.gpkg'
    codes=[-2**63,2**53+1,2**63-1]
    ds=gdal.GetDriverByName('GTiff').Create(str(path),3,1,1,gdal.GDT_Int64)
    ds.SetGeoTransform((0,1,0,1,0,-1));ds.SetProjection(_crs_wkt_for_footprint(3857))
    ds.GetRasterBand(1).WriteArray(np.array([codes],dtype=np.int64));ds=None
    with raster_workspace():
        result=raster_polygonize(raster_load(source_plan(path)),1)
        save(result,connection_info={'mount_path':str(tmp_path)},path=target.name)
    actual=gpd.read_file(target)
    assert sorted(actual.class_value.to_list())==sorted(codes)
    assert str(actual.class_value.dtype)=='int64' and actual.crs.to_epsg()==3857


def test_raster_polygonize_intermediate_failure_closes_and_cleans_files(tmp_path,monkeypatch):
    from operators.raster_compute import raster_polygonize
    path=create_raster(tmp_path/'source.tif',np.ones((3,3)));before=path.read_bytes()
    workspace=None
    def fail(*args,**kwargs):raise RuntimeError('injected polygonization failure')
    monkeypatch.setattr(gdal,'Polygonize',fail)
    with pytest.raises(RuntimeError,match='injected'),raster_workspace():
        source=raster_load(source_plan(path));workspace=source.workspace
        raster_polygonize(source,1)
    assert workspace is not None and not workspace.exists() and path.read_bytes()==before


def _rasterization_faces(transform):
    import geopandas as gpd
    from shapely.affinity import affine_transform
    from shapely.geometry import Polygon, MultiPolygon, box
    faces = [Polygon([(.1,.1),(4.9,.1),(4.9,4.9),(.1,4.9)],
                     holes=[[(1.1,1.1),(3.9,1.1),(3.9,3.9),(1.1,3.9)]]),
             box(4.1,.1,4.3,4.9),
             MultiPolygon([box(5.1,.1,5.9,1.9),box(5.1,4.1,5.9,4.9)]),
             box(-.9,2.1,.2,3.9)]
    affine = [transform[1],transform[2],transform[4],transform[5],transform[0],transform[3]]
    return gpd.GeoDataFrame({'code':[0,-3.25,8,21]},
        geometry=[affine_transform(g,affine) for g in faces],crs='EPSG:3857').rename_geometry('shape')


def _rasterization_oracle(faces, values, transform, width, height, all_touched):
    from shapely.geometry import Polygon, Point
    def point(x,y):
        return (transform[0]+x*transform[1]+y*transform[2],
                transform[3]+x*transform[4]+y*transform[5])
    expected=np.full((height,width),np.nan)
    for y in range(height):
        for x in range(width):
            cell=Polygon([point(x,y),point(x+1,y),point(x+1,y+1),point(x,y+1)])
            for geometry,value in zip(faces.geometry,values):
                # Test faces deliberately avoid ambiguous grid/centre coincidences.
                covered=(geometry.intersection(cell).area>0 if all_touched else geometry.contains(Point(point(x+.5,y+.5))))
                if covered:expected[y,x]=value
    return expected


@pytest.mark.parametrize('transform', [(0,1,0,6,0,-1),(100,2,.5,200,.25,-3)])
@pytest.mark.parametrize('all_touched', [False,True])
@pytest.mark.parametrize('mode', ['field','constant'])
def test_vector_rasterize_independent_coverage_holes_rotation_cog_and_immutable_inputs(tmp_path,transform,all_touched,mode):
    faces=_rasterization_faces(transform)
    original=faces.copy(deep=True)
    faces.index=[42,1,1,-7]  # Priority follows position, including duplicate labels.
    original.index=faces.index
    values=faces.code.to_list() if mode=='field' else [-1.5]*len(faces)
    expected=_rasterization_oracle(faces,values,transform,7,6,all_touched)
    path=create_raster(tmp_path/'reference.tif',np.full((2,6,7),-9999.),transform,'EPSG:3857')
    ds=gdal.Open(str(path),gdal.GA_Update);ds.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
    ds.GetRasterBand(1).GetMaskBand().Fill(0);ds=None
    before=path.read_bytes()
    with raster_workspace():
        reference=raster_load(source_plan(path));workspace=reference.workspace
        result=vector_rasterize(faces,reference,all_touched=all_touched,
            **({'field':'code'} if mode=='field' else {'burn_value':-1.5}))
        assert result.path!=reference.path
        output=gdal.Open(str(result.path));band=output.GetRasterBand(1)
        assert output.RasterCount==1 and band.DataType==gdal.GDT_Float64
        assert math.isnan(band.GetNoDataValue()) and output.GetGeoTransform()==transform
        assert _crs_wkt_for_footprint(3857)==output.GetProjection()
        np.testing.assert_allclose(read_band_values(band),expected,equal_nan=True)
        np.testing.assert_array_equal(np.frombuffer(band.GetMaskBand().ReadRaster(),dtype=np.uint8).reshape(6,7),np.where(np.isfinite(expected),255,0))
        band=output=None
        target=tmp_path/'output.cog.tif'
        artifact=raster_save(result,target_plan(target),profile='cog',blocksize=128)
        assert artifact['size_bytes']==target.stat().st_size>0
        loaded=raster_load(source_plan(target));assert validate_cog(loaded)['valid']
        saved=gdal.Open(str(loaded.path));np.testing.assert_allclose(read_band_values(saved.GetRasterBand(1)),expected,equal_nan=True);saved=None
    assert not workspace.exists() and path.read_bytes()==before
    import geopandas.testing
    geopandas.testing.assert_geodataframe_equal(faces,original)


def test_vector_rasterize_overlap_follows_current_position_not_index(tmp_path):
    import geopandas as gpd
    from shapely.geometry import box
    faces=gpd.GeoDataFrame({'value':[0,-7.25,13]},geometry=[box(.1,.1,2.9,2.9)]*3,crs=3857,index=[9,1,9])
    path=create_raster(tmp_path/'reference.tif',np.ones((3,3)),(0,1,0,3,0,-1),'EPSG:3857')
    with raster_workspace():
        reference=raster_load(source_plan(path))
        for batch,value in [(faces,13),(faces.iloc[::-1],0)]:
            result=vector_rasterize(batch,reference,field='value')
            ds=gdal.Open(str(result.path));np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),np.full((3,3),value));ds=None


@pytest.mark.parametrize('value', [0,-1.25,2**53-1,-(2**53-1),np.int64(2**53-1),np.float32(3.25)])
def test_vector_rasterize_accepts_finite_values_and_safe_integer_endpoints(tmp_path,value):
    import geopandas as gpd
    from shapely.geometry import box
    faces=gpd.GeoDataFrame({'code':[value]},geometry=[box(-1,-1,4,4)],crs=3857)
    path=create_raster(tmp_path/'reference.tif',np.zeros((2,2)),(0,1,0,2,0,-1),'EPSG:3857')
    with raster_workspace():
        reference=raster_load(source_plan(path))
        for params in [{'field':'code'},{'burn_value':value}]:
            result=vector_rasterize(faces,reference,**params)
            ds=gdal.Open(str(result.path));np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),np.full((2,2),value,dtype=np.float64));ds=None


@pytest.mark.parametrize('value', [True,np.bool_(False),'1',None,complex(1,0),np.nan,np.inf,-np.inf,2**53,-2**53,np.uint64(2**64-1),float(2**53)])
def test_vector_rasterize_rejects_bad_values_including_off_grid_attributes(tmp_path,value):
    import geopandas as gpd
    from shapely.geometry import box
    faces=gpd.GeoDataFrame({'code':[value]},geometry=[box(100,100,101,101)],crs=3857)
    path=create_raster(tmp_path/'reference.tif',np.ones((2,2)),crs='EPSG:3857')
    with raster_workspace():
        reference=raster_load(source_plan(path))
        with pytest.raises(ValueError):vector_rasterize(faces,reference,field='code')
        with pytest.raises(ValueError):vector_rasterize(faces,reference,burn_value=value)


@pytest.mark.parametrize('params', [{},{'field':'code','burn_value':1},{'field':''},{'field':'missing'},
    {'field':'geometry'},{'field':0},{'burn_value':1,'all_touched':'false'},
    {'burn_value':1,'all_touched':0},{'burn_value':1,'all_touched':None}])
def test_vector_rasterize_rejects_ambiguous_modes_fields_and_nonboolean_coverage(tmp_path,params):
    import geopandas as gpd
    from shapely.geometry import box
    faces=gpd.GeoDataFrame({'code':[1]},geometry=[box(0,0,1,1)],crs=3857)
    path=create_raster(tmp_path/'reference.tif',np.ones((2,2)),crs='EPSG:3857')
    with raster_workspace(),pytest.raises(ValueError):
        vector_rasterize(faces,raster_load(source_plan(path)),**params)


@pytest.mark.parametrize('wkt', [None,'POLYGON EMPTY','POINT (0 0)','LINESTRING (0 0,1 1)',
    'POLYGON ((0 0,2 2,0 2,2 0,0 0))','POLYGON Z ((0 0 1,1 0 1,1 1 1,0 0 1))',
    'POLYGON M ((0 0 1,1 0 1,1 1 1,0 0 1))',
    'POLYGON ((0 0,Infinity 0,1 1,0 0))'])
def test_vector_rasterize_rejects_geometry_without_dropping_or_repairing_rows(tmp_path,wkt):
    import geopandas as gpd
    from shapely import from_wkt
    faces=gpd.GeoDataFrame(geometry=[from_wkt(wkt) if wkt else None],crs=3857)
    path=create_raster(tmp_path/'reference.tif',np.ones((2,2)),crs='EPSG:3857')
    with raster_workspace(),pytest.raises(ValueError,match='two-dimensional'):
        vector_rasterize(faces,raster_load(source_plan(path)),burn_value=1)


@pytest.mark.parametrize('problem', ['missing_vector_crs','different_crs','missing_grid_crs','missing_transform','singular_transform','not_gdf','no_active_geometry','duplicate_field'])
def test_vector_rasterize_rejects_invalid_inputs_and_grid_contract(tmp_path,problem):
    import geopandas as gpd
    from shapely.geometry import box
    faces=gpd.GeoDataFrame({'code':[1]},geometry=[box(0,0,1,1)],crs=None if problem=='missing_vector_crs' else 3857)
    if problem=='different_crs':faces=faces.to_crs(4326)
    if problem=='not_gdf':faces={}
    if problem=='no_active_geometry':faces=gpd.GeoDataFrame({'code':[1]})
    if problem=='duplicate_field':faces.insert(0,'code',2,allow_duplicates=True)
    transform=None if problem=='missing_transform' else (0,1,1,2,1,1) if problem=='singular_transform' else (0,1,0,2,0,-1)
    path=create_raster(tmp_path/'reference.tif',np.ones((2,2)),transform,'' if problem=='missing_grid_crs' else 'EPSG:3857')
    with raster_workspace(),pytest.raises((ValueError,AttributeError)):
        vector_rasterize(faces,raster_load(source_plan(path)),field='code')


@pytest.mark.parametrize('outside_only', [False,True])
def test_vector_rasterize_empty_or_outside_batch_is_all_nodata_without_reference_pixel_reads(tmp_path,monkeypatch,outside_only):
    import geopandas as gpd
    from shapely.geometry import box
    faces=gpd.GeoDataFrame({'code':[1] if outside_only else []},geometry=[box(100,100,101,101)] if outside_only else [],crs=3857)
    path=create_raster(tmp_path/'reference.tif',np.ones((3,1030)),(0,1,0,3,0,-1),'EPSG:3857')
    with raster_workspace():
        reference=raster_load(source_plan(path))
        def forbidden(*args,**kwargs):raise AssertionError('reference pixels must not be read')
        with monkeypatch.context() as patch:
            patch.setattr(gdal.Band,'ReadAsArray',forbidden);patch.setattr(gdal.Band,'ReadRaster',forbidden)
            for params in [{'field':'code'},{'burn_value':0}]:
                result=vector_rasterize(faces,reference,**params)
        ds=gdal.Open(str(result.path));assert np.isnan(read_band_values(ds.GetRasterBand(1))).all();ds=None


def test_vector_rasterize_polygonize_roundtrip_complete_validity_and_rotated_grid(tmp_path):
    from operators.raster_compute import raster_polygonize
    transform=(100,2,.5,200,.25,-3)
    values=np.array([[0,0,0,0,0], [0,2,2,2,0], [0,2,-9999,2,0], [0,2,2,2,0], [-3,0,0,0,-3]],dtype=float)
    path=create_raster(tmp_path/'classes.tif',values,transform,'EPSG:3857')
    with raster_workspace():
        reference=raster_load(source_plan(path));regions=raster_polygonize(reference,1,8)
        result=vector_rasterize(regions,reference,field='class_value')
        ds=gdal.Open(str(result.path));expected=np.where(values==-9999,np.nan,values)
        np.testing.assert_allclose(read_band_values(ds.GetRasterBand(1)),expected,equal_nan=True);ds=None


@pytest.mark.parametrize('invalid', [False,True],ids=['saved-cog','failure-cleanup'])
def test_vector_rasterize_async_dag_polygonize_save_and_cleanup(tmp_path,monkeypatch,invalid):
    import api_server
    import workflow_engine
    values=np.array([[0,2,2],[0,-9999,2]],dtype=float)
    source=create_raster(tmp_path/'source.tif',values,crs='EPSG:3857')
    before=source.read_bytes();target=tmp_path/'output.cog.tif'
    tasks=[
        {'id':'load','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(source)}},
        {'id':'faces','operator':'raster_polygonize','depends_on':['load'],'params':{'input_raster':{'$ref':'load'},'band':1}},
        {'id':'burn','operator':'vector_rasterize','depends_on':['faces','load'],'params':{'input_gdf':{'$ref':'faces'},'reference_raster':{'$ref':'load'},'field':'missing' if invalid else 'class_value'}},
        {'id':'save','operator':'raster_save','depends_on':['burn'],'params':{'input_raster':{'$ref':'burn'},'access_plan':target_plan(target),'profile':'cog','blocksize':128}},
    ]
    paths=[];original=workflow_engine._execute_operator
    def execute(name,params):
        if name=='vector_rasterize':paths.append(params['reference_raster'].workspace)
        return original(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',execute)
    client=api_server.app.test_client()
    response=client.post('/api/workflow',json={'workflow_def':{'tasks':tasks},'input_data':{},'runtime':{
        'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert response.status_code==202,response.json
    deadline=time.monotonic()+10
    while time.monotonic()<deadline:
        status=client.get('/api/executions/'+response.json['execution_id']).json
        if status['status'] in ('success','failed'):break
        time.sleep(.01)
    assert status['status']==('failed' if invalid else 'success'),status
    assert paths and all(not path.exists() for path in paths) and source.read_bytes()==before
    if invalid:assert not target.exists()
    else:
        ds=gdal.Open(str(target));np.testing.assert_allclose(read_band_values(ds.GetRasterBand(1)),np.where(values==-9999,np.nan,values),equal_nan=True);ds=None
        artifact=json.loads(status['all_results']['save'])
        assert artifact['size_bytes']==target.stat().st_size>0
    assert client.post('/api/operators/vector_rasterize/invoke',json={'params':{}}).status_code==403


def test_vector_rasterize_metadata_example_is_executable_and_declares_both_typed_inputs(tmp_path):
    spec=next(item for item in list_operators() if item['id']=='vector_rasterize')
    params={item['name']:item for item in spec['parameters']}
    assert params['input_gdf']['param_type']==params['reference_raster']['param_type']=='input'
    assert params['input_gdf']['type']=='object' and params['reference_raster']['type']=='raster'
    assert not params['field']['required'] and not params['burn_value']['required']
    assert params['all_touched']['default'] is False
    assert spec['output_ports'][0]['type']=='raster' and spec['execution_modes']==['workflow']
    assert spec['effects']==['read'] and spec['attributes']['resource_groups']==['raster']
    assert not {'path','locator','engine_id','access_plan'} & params.keys()
    path=create_raster(tmp_path/'reference.tif',np.ones((6,7)),crs='EPSG:3857')
    with raster_workspace():
        args=dict(spec['detailed_description']['workflow_example']['params'])
        args['input_gdf']=_rasterization_faces((0,1,0,4,0,-1)).rename(columns={'code':'class_value'})
        args['reference_raster']=raster_load(source_plan(path))
        result=vector_rasterize(**args);assert result.path.exists()


def test_vector_rasterize_cross_block_coverage_and_injected_native_failure_cleanup(tmp_path,monkeypatch):
    import geopandas as gpd
    from shapely.geometry import box
    path=create_raster(tmp_path/'reference.tif',np.ones((3,1030)),(0,1,0,3,0,-1),'EPSG:3857')
    faces=gpd.GeoDataFrame({'value':[0,-2.5]},geometry=[box(.1,.1,1029.9,2.9),box(510.1,.1,514.9,2.9)],crs=3857)
    with raster_workspace():
        reference=raster_load(source_plan(path));workspace=reference.workspace
        result=vector_rasterize(faces,reference,field='value')
        ds=gdal.Open(str(result.path));actual=read_band_values(ds.GetRasterBand(1));ds=None
        expected=np.zeros((3,1030));expected[:,510:515]=-2.5
        np.testing.assert_array_equal(actual,expected)
        def fail_after_partial_write(output,*args,**kwargs):
            output.GetRasterBand(1).WriteArray(np.array([[1.]]))
            raise RuntimeError('injected native rasterization failure')
        monkeypatch.setattr(gdal,'RasterizeLayer',fail_after_partial_write)
        with pytest.raises(RuntimeError,match='injected native'):
            vector_rasterize(faces,reference,field='value')
        assert workspace.exists()
    assert not workspace.exists()


def test_vector_rasterize_explicit_vector_reproject_chain_and_expired_reference(tmp_path):
    import geopandas as gpd
    from shapely.geometry import box
    from operators.spatial_transform_operators import vector_reproject
    faces=gpd.GeoDataFrame(geometry=[box(-1,-1,1,1)],crs=4326)
    original=faces.copy(deep=True)
    path=create_raster(tmp_path/'reference.tif',np.ones((2,2)),(-1000,1000,0,1000,0,-1000),'EPSG:3857')
    with raster_workspace():
        reference=raster_load(source_plan(path))
        with pytest.raises(ValueError,match='same explicit CRS'):
            vector_rasterize(faces,reference,burn_value=0)
        result=vector_rasterize(vector_reproject(faces,target_crs='EPSG:3857'),reference,burn_value=0)
        ds=gdal.Open(str(result.path));np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),np.zeros((2,2)));ds=None
    import geopandas.testing
    geopandas.testing.assert_geodataframe_equal(faces,original)
    with raster_workspace(),pytest.raises(ValueError,match='this execution'):
        vector_rasterize(faces,reference,burn_value=0)


def test_zonal_statistics_rotated_grid_validity_groups_and_empty_zones(tmp_path):
    import pandas as pd
    from operators.raster_compute import raster_zonal_statistics
    transform = (110, 2, .5, 30, .25, -3)
    classes = np.array([[0, 0, 2, 7, 2], [0, -3, 2, 7, -9999], [2, 2, -3, 7, 2]], dtype=float)
    values = np.array([[0, 4, 8, -9999, 100], [6, -2, np.nan, np.inf, 999], [12, -9999, -4, -9999, 16]], dtype=float)
    z = create_raster(tmp_path/'zones.tif', np.stack([classes, np.full(classes.shape, 255.)]), transform)
    v = create_raster(tmp_path/'values.tif', np.stack([values, np.full(values.shape, 255.)]), transform)
    # Independent masks and alpha exclusions; partial opacity remains valid.
    for path, hidden, partial in [(z, (0,4), (0,1)), (v, (2,4), (1,0))]:
        ds = gdal.Open(str(path), gdal.GA_Update)
        ds.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand)
        alpha = np.full(classes.shape, 255.)
        alpha[hidden] = 0; alpha[partial] = 128
        ds.GetRasterBand(2).WriteArray(alpha)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK', 'YES'):
            ds.GetRasterBand(1).CreateMaskBand(gdal.GMF_PER_DATASET)
        mask = np.full(classes.shape, 255, dtype=np.uint8)
        mask[1,2] = 0
        ds.GetRasterBand(1).GetMaskBand().WriteArray(mask)
        assert ds.GetFileList() == [str(path)]
        ds = None
    snapshots = [p.read_bytes() for p in (v,z)]
    with raster_workspace():
        table = raster_zonal_statistics(raster_load(source_plan(v)), raster_load(source_plan(z)), 1, 1)
    assert isinstance(table, pd.DataFrame) and not hasattr(table, 'geometry')
    assert table.zone_value.tolist() == [-3, 0, 2, 7]
    for name in ('zone_value','total_count','valid_count','invalid_count'):
        assert str(table[name].dtype) == 'int64'
    records = table.set_index('zone_value')
    expected = {-3: ([-2.,-4.],2), 0: ([0.,4.,6.],3), 2: ([8.,12.],4), 7: ([],3)}
    for code,(numbers,total) in expected.items():
        row = records.loc[code]
        assert (row.total_count,row.valid_count,row.invalid_count) == (total,len(numbers),total-len(numbers))
        if numbers:
            assert row[['sum','min','max','mean','stddev']].tolist() == pytest.approx(
                [sum(numbers), min(numbers), max(numbers), np.mean(numbers), np.std(numbers)])
        else:
            assert row[['sum','min','max','mean','stddev']].isna().all()
    assert [p.read_bytes() for p in (v,z)] == snapshots


@pytest.mark.parametrize('kind', ['float_safe','int64','uint64','empty'])
def test_zonal_statistics_classification_precision_and_empty_schema(tmp_path, kind):
    from operators.raster_compute import raster_zonal_statistics
    path = tmp_path/'zones.tif'
    dtype = {'float_safe':gdal.GDT_Float64,'int64':gdal.GDT_Int64,'uint64':gdal.GDT_UInt64,'empty':gdal.GDT_Int64}[kind]
    data = {'float_safe':[-(2**53-1),0,2**53-1], 'int64':[-2**63,2**53+1,2**63-1],
            'uint64':[0,2**53+1,2**63-1], 'empty':[-99,-99,-99]}[kind]
    ds = gdal.GetDriverByName('GTiff').Create(str(path),3,1,1,dtype)
    crs=osr.SpatialReference();crs.ImportFromEPSG(4326);ds.SetProjection(crs.ExportToWkt());ds.SetGeoTransform((0,1,0,1,0,-1))
    array=np.array([data],dtype={'float_safe':'float64','int64':'int64','uint64':'uint64','empty':'int64'}[kind])
    ds.GetRasterBand(1).WriteArray(array)
    if kind=='empty':ds.GetRasterBand(1).SetNoDataValue(-99)
    ds=None
    value=create_raster(tmp_path/'value.tif',np.array([[1.,2.,3.]]),transform=(0,1,0,1,0,-1))
    with raster_workspace():
        result=raster_zonal_statistics(raster_load(source_plan(value)),raster_load(source_plan(path)),1,1)
    assert result.zone_value.tolist()==([] if kind=='empty' else data)
    assert result.columns.tolist()==['zone_value','total_count','valid_count','invalid_count','sum','min','max','mean','stddev']
    assert result.dtypes.astype(str).tolist()==['int64']*4+['Float64']*5
    if kind!='empty':assert result['sum'].tolist()==[1.,2.,3.]


@pytest.mark.parametrize('problem',['fraction','unsafe_float','uint_overflow','alpha','complex','complex_alpha',
                                    'missing_crs','missing_grid','singular','different_crs','different_size','shift','rotation',
                                    'band_zero','band_bool','band_float','band_missing'])
def test_zonal_statistics_rejects_invalid_categories_bands_and_grids(tmp_path,problem):
    from operators.raster_compute import raster_zonal_statistics
    z=create_raster(tmp_path/'zones.tif',np.ones((2,3)))
    v=create_raster(tmp_path/'value.tif',np.ones((2,3)))
    if problem in ('complex','complex_alpha','uint_overflow'):
        dtype=gdal.GDT_UInt64 if problem=='uint_overflow' else gdal.GDT_CFloat64
        ds=gdal.GetDriverByName('GTiff').Create(str(z),3,2,2 if problem=='complex_alpha' else 1,dtype)
        src=gdal.Open(str(v));ds.SetProjection(src.GetProjection());ds.SetGeoTransform(src.GetGeoTransform());src=None
        ds.GetRasterBand(1).WriteArray(np.full((2,3),2**63 if problem=='uint_overflow' else 1))
        if problem=='complex_alpha':
            ds.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand);ds.GetRasterBand(2).Fill(255)
        ds=None
    else:
        ds=gdal.Open(str(z),gdal.GA_Update)
        if problem in ('fraction','unsafe_float'):ds.GetRasterBand(1).Fill(.5 if problem=='fraction' else 2**53)
        if problem=='alpha':ds.GetRasterBand(1).SetColorInterpretation(gdal.GCI_AlphaBand)
        if problem=='missing_crs':ds.SetProjection('')
        if problem=='different_crs':crs=osr.SpatialReference();crs.ImportFromEPSG(3857);ds.SetProjection(crs.ExportToWkt())
        if problem in ('singular','shift','rotation'):
            ds.SetGeoTransform({'singular':(0,1,1,4,1,1),'shift':(1e-10,1,0,4,0,-1),'rotation':(0,1,.1,4,0,-1)}[problem])
        ds=None
        if problem=='missing_grid':z=create_raster(z,np.ones((2,3)),transform=None)
        if problem=='different_size':z=create_raster(z,np.ones((2,4)))
    band={'band_zero':0,'band_bool':True,'band_float':1.,'band_missing':3}.get(problem,1)
    with raster_workspace(),pytest.raises(ValueError):
        raster_zonal_statistics(raster_load(source_plan(v)),raster_load(source_plan(z)),band,1)


@pytest.mark.parametrize('case',['cancellation','opposite_extremes','tiny','large_offset','overflow'])
def test_zonal_statistics_block_streaming_and_stable_float_statistics(tmp_path,monkeypatch,case):
    from operators.raster_compute import raster_zonal_statistics
    width=1030
    data=np.zeros((1,width))
    if case=='cancellation':data[0,[0,512,1024]]=[1e16,1.,-1e16]
    if case=='opposite_extremes':data[0,:]=np.resize([1e308,-1e308],width)
    if case=='tiny':data[0,:]=np.resize([1e-300,-1e-300],width)
    if case=='large_offset':data[0,:]=np.resize([1e16-2,1e16+2],width)
    if case=='overflow':data[0,:]=1e308
    value=create_raster(tmp_path/'value.tif',data)
    zones=create_raster(tmp_path/'zones.tif',np.zeros(data.shape))
    with raster_workspace():
        loaded,z=raster_load(source_plan(value)),raster_load(source_plan(zones))
        readraster,readarray=gdal.Band.ReadRaster,gdal.Band.ReadAsArray
        reads=[]
        def bounded_read(original):
            def read(self,x=0,y=0,width=None,height=None,*args,**kwargs):
                assert width is not None and height is not None and width<=512 and height<=512
                reads.append((x,y,width,height));return original(self,x,y,width,height,*args,**kwargs)
            return read
        monkeypatch.setattr(gdal.Band,'ReadRaster',bounded_read(readraster))
        monkeypatch.setattr(gdal.Band,'ReadAsArray',bounded_read(readarray))
        if case=='overflow':
            with pytest.raises(ValueError,match='finite Float64'):raster_zonal_statistics(loaded,z,1,1)
        else:
            row=raster_zonal_statistics(loaded,z,1,1).iloc[0]
            assert row.valid_count==width and row.invalid_count==0
            if case=='cancellation':
                assert row['sum']==pytest.approx(1.)
                assert row['mean']==pytest.approx(1./width,rel=1e-14)
            elif case=='large_offset':
                assert row['mean']==1e16
                assert row.stddev==pytest.approx(2.,rel=1e-14)
            else:
                assert row['sum']==0.
                assert row['mean']==pytest.approx(0.,abs=1e-315)
                expected=1e308 if case=='opposite_extremes' else 1e-300
                assert row.stddev==pytest.approx(expected,rel=1e-14,abs=0)
        assert reads and {r[0] for r in reads}=={0,512,1024}


@pytest.mark.parametrize('invalid',[False,True],ids=['full-table-saved','failure-cleanup'])
def test_zonal_statistics_async_full_table_save_and_bounded_preview(tmp_path,monkeypatch,invalid):
    import pandas as pd
    import api_server
    import workflow_engine
    zones=create_raster(tmp_path/'zones.tif',np.arange(72,dtype=float).reshape(6,12))
    values=create_raster(tmp_path/'value.tif',np.arange(72,dtype=float).reshape(6,12)*2)
    target=tmp_path/'statistics.csv';snapshots=[p.read_bytes() for p in (zones,values)]
    tasks=[{'id':name,'operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(path)}} for name,path in [('values',values),('zones',zones)]]
    tasks += [{'id':'stats','operator':'raster_zonal_statistics','depends_on':['values','zones'],
               'params':{'input_raster':{'$ref':'values'},'zones_raster':{'$ref':'zones'},'band':1,'zones_band':2 if invalid else 1}},
              {'id':'save','operator':'save','depends_on':['stats'],
               'params':{'input_df':{'$ref':'stats'},'connection_info':{'mount_path':str(tmp_path)},'path':target.name,'mode':'fail'}}]
    paths=[];original=workflow_engine._execute_operator
    def execute(name,params):
        if name=='raster_zonal_statistics':paths.extend([params[key].workspace for key in ('input_raster','zones_raster')])
        return original(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',execute)
    client=api_server.app.test_client()
    response=client.post('/api/workflow',json={'workflow_def':{'tasks':tasks},'input_data':{},'runtime':{
        'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert response.status_code==202,response.json
    deadline=time.monotonic()+10
    while time.monotonic()<deadline:
        status=client.get('/api/executions/'+response.json['execution_id']).json
        if status['status'] in ('success','failed'):break
        time.sleep(.01)
    assert status['status']==('failed' if invalid else 'success'),status
    assert paths and all(not p.exists() for p in paths)
    assert [p.read_bytes() for p in (zones,values)]==snapshots
    if invalid:assert not target.exists()
    else:
        table=pd.read_csv(target)
        assert table.zone_value.tolist()==list(range(72))
        assert table['sum'].tolist()==list(range(0,144,2))
        assert (table.valid_count==1).all() and (table.stddev==0).all()
        preview=json.loads(status['all_results']['stats'])
        assert preview['type']=='pandas_dataframe' and preview['row_count']==72 and preview['truncated'] is True
        assert preview['preview_limit']==len(preview['preview_rows'])==50
        assert preview['schema'][0]=={'name':'zone_value','type':'int64'}
        assert json.loads(status['result'])==preview
    assert client.post('/api/operators/raster_zonal_statistics/invoke',json={'params':{}}).status_code==403


def test_zonal_statistics_metadata_typed_inputs_required_bands_and_executable_example(tmp_path):
    spec=next(item for item in list_operators() if item['id']=='raster_zonal_statistics')
    params={item['name']:item for item in spec['parameters']}
    for name in ('input_raster','zones_raster'):
        assert params[name]['param_type']=='input' and params[name]['type']=='raster'
    assert params['band']['required'] and params['zones_band']['required']
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read']
    assert spec['attributes']['resource_groups']==['raster'] and spec['output_ports'][0]['type']=='DataFrame'
    path=create_raster(tmp_path/'grid.tif',np.zeros((2,3)))
    tasks=[{'id':name,'operator':'raster_load','params':{'access_plan':source_plan(path)},'depends_on':[]} for name in ('source','zones')]
    tasks += [{'id':'stats',**spec['detailed_description']['workflow_example'],'depends_on':['source','zones']}]
    result=execute_workflow({'tasks':tasks})
    assert result['status']=='success',result
    preview=json.loads(result['final_result'])
    assert preview['row_count']==1 and preview['truncated'] is False
    assert preview['preview_rows'][0]['zone_value']==0 and preview['preview_rows'][0]['sum']==0.


def test_zonal_statistics_multiband_cross_block_independent_oracle(tmp_path):
    from operators.raster_compute import raster_zonal_statistics
    rng=np.random.default_rng(1023)
    shape=(513,517)
    classes=rng.choice([-3.,0.,2.,11.],size=shape)
    values=rng.normal(100000,17,size=shape)
    classes[::19,::17]=-9999;values[::13,::11]=np.nan
    transform=(100,2,.5,30,.25,-3)
    z=create_raster(tmp_path/'zones.tif',np.stack([np.full(shape,.125),classes]),transform)
    v=create_raster(tmp_path/'values.tif',np.stack([np.ones(shape),values]),transform)
    with raster_workspace():
        table=raster_zonal_statistics(raster_load(source_plan(v)),raster_load(source_plan(z)),2,2).set_index('zone_value')
    assert table.index.tolist()==[-3,0,2,11]
    for code in table.index:
        group=classes==code;sample=values[group & np.isfinite(values)];row=table.loc[code]
        assert row.total_count==np.count_nonzero(group)
        assert row.valid_count==sample.size and row.invalid_count==np.count_nonzero(group)-sample.size
        np.testing.assert_allclose(row[['sum','min','max','mean','stddev']].to_numpy(dtype=float),
            [math.fsum(sample),sample.min(),sample.max(),sample.mean(),sample.std()],rtol=1e-11,atol=1e-11)


def _focal_oracle(values, valid, window_size, method):
    """Scalar reference: enumerate actual neighbours, independent of tile reduction."""
    result = np.full(values.shape, np.nan)
    radius = window_size // 2
    for row, column in zip(*np.nonzero(valid)):
        sample = [float(values[y,x])
                  for y in range(max(0,row-radius), min(values.shape[0],row+radius+1))
                  for x in range(max(0,column-radius), min(values.shape[1],column+radius+1)) if valid[y,x]]
        if method == 'mode':
            from collections import Counter
            counts = Counter(sample)
            modes = [value for value,count in counts.items() if count==max(counts.values())]
            result[row,column] = values[row,column] if values[row,column] in modes else min(modes)
        else:
            operation = {'sum':math.fsum, 'mean':statistics.fmean, 'min':min, 'max':max,
                         'stddev':statistics.pstdev}[method]
            result[row,column] = operation(sample)
    return result


@pytest.mark.parametrize('window_size', [3,5])
@pytest.mark.parametrize('method', ['sum','mean','min','max','stddev','mode'])
def test_focal_statistics_selected_band_rotated_validity_edges_and_scalar_oracle(tmp_path, window_size, method):
    values = np.arange(56,dtype=float).reshape(7,8)-21
    values[1,1] = -9999; values[2,2] = np.nan
    values[2,3] = np.inf; values[2,4] = -np.inf
    alpha = np.full(values.shape,128.)
    alpha[3,1] = 0; alpha[3,2] = np.nan; alpha[3,3] = -1; alpha[3,4] = np.inf
    mask = np.full(values.shape,255,dtype=np.uint8); mask[4,3] = 0
    transform = (110.,2.,.5,30.,.25,-3.)
    path = create_raster(tmp_path/'selected.tif',np.stack([np.full(values.shape,np.nan), values, alpha]),transform)
    ds = gdal.Open(str(path),gdal.GA_Update)
    ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
    ds.GetRasterBand(2).SetScale(17.); ds.GetRasterBand(2).SetOffset(500.)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):
        ds.CreateMaskBand(gdal.GMF_PER_DATASET)
    ds.GetRasterBand(2).GetMaskBand().WriteArray(mask); ds = None
    snapshot = path.read_bytes()
    valid = (mask!=0) & np.isfinite(values) & (values!=-9999) & np.isfinite(alpha) & (alpha>0)
    expected = _focal_oracle(values,valid,window_size,method)
    with raster_workspace():
        result = raster_focal_statistics(raster_load(source_plan(path)),2,window_size,method)
        ds = gdal.Open(str(result.path))
        assert ds.RasterCount==1 and ds.GetGeoTransform()==transform
        assert ds.GetSpatialRef().GetAuthorityCode(None)=='4326'
        band = ds.GetRasterBand(1)
        assert band.DataType==gdal.GDT_Float64 and math.isnan(band.GetNoDataValue())
        assert band.GetScale() is None and band.GetOffset() is None
        assert band.GetColorInterpretation()!=gdal.GCI_AlphaBand and band.GetColorTable() is None
        np.testing.assert_allclose(read_band_values(band),expected,rtol=1e-14,atol=1e-14,equal_nan=True)
        assert np.array_equal(read_band_values(band.GetMaskBand())!=0, valid)
        band=None; ds=None
    assert path.read_bytes()==snapshot


@pytest.mark.parametrize('window_size', [3,5])
@pytest.mark.parametrize('method', ['sum','mean','min','max','stddev'])
def test_focal_statistics_full_image_analytic_plane_across_both_block_axes(tmp_path,monkeypatch,window_size,method):
    height,width = 515,519
    yy,xx = np.indices((height,width)); values = xx+2.*yy
    path = create_raster(tmp_path/'plane.tif',values)
    radius = window_size//2
    # Independent separable closed form for a plane, including truncated borders.
    x_samples = [list(range(max(0,x-radius),min(width,x+radius+1))) for x in range(width)]
    y_samples = [list(range(max(0,y-radius),min(height,y+radius+1))) for y in range(height)]
    mx=np.array([statistics.fmean(s) for s in x_samples]); my=np.array([statistics.fmean(s) for s in y_samples])
    expected=my[:,None]*2+mx
    if method=='sum':expected=expected*np.array([len(s) for s in y_samples])[:,None]*np.array([len(s) for s in x_samples])
    if method=='min':expected=np.array([s[0] for s in y_samples])[:,None]*2+np.array([s[0] for s in x_samples])
    if method=='max':expected=np.array([s[-1] for s in y_samples])[:,None]*2+np.array([s[-1] for s in x_samples])
    if method=='stddev':expected=np.sqrt(np.array([statistics.pvariance(s) for s in y_samples])[:,None]*4+np.array([statistics.pvariance(s) for s in x_samples]))
    with raster_workspace():
        loaded=raster_load(source_plan(path))
        original=gdal.Band.ReadRaster; reads=[]
        def bounded(self,x=0,y=0,xsize=None,ysize=None,*args,**kwargs):
            assert xsize is not None and ysize is not None and xsize<=512 and ysize<=512
            reads.append((x,y,xsize,ysize))
            return original(self,x,y,xsize,ysize,*args,**kwargs)
        with monkeypatch.context() as scoped:
            scoped.setattr(gdal.Band,'ReadRaster',bounded)
            result=raster_focal_statistics(loaded,1,window_size,method)
        ds=gdal.Open(str(result.path))
        np.testing.assert_allclose(read_band_values(ds.GetRasterBand(1)),expected,rtol=1e-13,atol=1e-12)
        ds=None
    assert any(x>0 for x,y,w,h in reads) and any(y>0 for x,y,w,h in reads)


@pytest.mark.parametrize('window_size',[3,5])
@pytest.mark.parametrize('shape',[(1,1),(1,7),(9,1)])
@pytest.mark.parametrize('transform,crs',[(None,None),(None,'EPSG:4326'),((1,2,.5,3,.25,-4),None)])
def test_focal_statistics_missing_location_thin_images_zero_and_all_invalid(tmp_path,window_size,shape,transform,crs):
    path=create_raster(tmp_path/'unlocated.tif',np.zeros(shape),transform=transform,crs=crs)
    with raster_workspace():
        source=raster_load(source_plan(path))
        for method in ('sum','mean','min','max','stddev'):
            result=raster_focal_statistics(source,1,window_size,method)
            ds=gdal.Open(str(result.path))
            assert ds.GetGeoTransform(can_return_null=True)==transform
            assert bool(ds.GetProjection())==bool(crs)
            np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),np.zeros(shape))
            ds=None
        invalid=create_raster(tmp_path/'invalid.tif',np.full(shape,-9999.))
        result=raster_focal_statistics(raster_load(source_plan(invalid)),1,window_size,'stddev')
        ds=gdal.Open(str(result.path))
        assert np.isnan(read_band_values(ds.GetRasterBand(1))).all(); ds=None


@pytest.mark.parametrize('case', ['large_offset','opposite_extremes','tiny','subnormal','cancellation','overflow'])
def test_focal_statistics_numerical_stability(tmp_path,case):
    samples = {'large_offset':[1e16-2,1e16+2], 'opposite_extremes':[1e308,-1e308],
               'tiny':[1e-300,-1e-300], 'subnormal':[5e-324,-5e-324],
               'cancellation':[1e16,1.,-1e16], 'overflow':[np.finfo(float).max]*2}[case]
    values=np.array([samples])
    path=create_raster(tmp_path/'numeric.tif',values)
    with raster_workspace():
        loaded=raster_load(source_plan(path))
        for method in ('sum','mean','stddev'):
            result=raster_focal_statistics(loaded,1,5,method)
            ds=gdal.Open(str(result.path)); actual=read_band_values(ds.GetRasterBand(1)); ds=None
            if case=='overflow' and method=='sum':assert np.isnan(actual).all()
            else:
                expected={'sum': lambda:math.fsum(samples), 'mean':lambda:statistics.fmean(samples) if case!='overflow' else np.finfo(float).max,
                          'stddev':lambda:statistics.pstdev(samples)}[method]()
                np.testing.assert_allclose(actual,expected,rtol=1e-14,atol=0)


@pytest.mark.parametrize('parameter,value', [('window_size',True),('window_size',3.),('window_size',1),('window_size',7),
    ('window_size',None),('band',True),('band',0),('band',1.),('band',4),('method','median'),('method',None)])
def test_focal_statistics_rejects_invalid_parameters(tmp_path,parameter,value):
    path=create_raster(tmp_path/'source.tif',np.ones((2,3)))
    params={'band':1,'window_size':3,'method':'mean',parameter:value}
    with raster_workspace(),pytest.raises(ValueError):
        raster_focal_statistics(raster_load(source_plan(path)),**params)


@pytest.mark.parametrize('kind',['selected-alpha','complex-data','complex-alpha'])
@pytest.mark.parametrize('method',['mean','mode'])
def test_focal_statistics_data_band_and_complex_rules(tmp_path,kind,method):
    path=tmp_path/'bands.tif'
    datatype=gdal.GDT_Float64 if kind=='selected-alpha' else gdal.GDT_CFloat64
    ds=gdal.GetDriverByName('GTiff').Create(str(path),2,2,2,datatype)
    ds.GetRasterBand(1).Fill(2); ds.GetRasterBand(2).Fill(1)
    if kind in ('selected-alpha','complex-alpha'):ds.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand)
    ds=None
    with raster_workspace(),pytest.raises(ValueError):
        raster_focal_statistics(raster_load(source_plan(path)),2 if kind=='selected-alpha' else 1,3,method)


@pytest.mark.parametrize('invalid',[False,True],ids=['cog-save','failure-cleanup'])
def test_focal_statistics_async_cog_save_and_cleanup(tmp_path,monkeypatch,invalid):
    import api_server
    import workflow_engine
    values=np.arange(42,dtype=float).reshape(6,7); values[2,3]=-9999
    path=create_raster(tmp_path/'source.tif',values,(110,2,.5,30,.25,-3))
    target=tmp_path/'focal.cog.tif'; snapshot=path.read_bytes()
    tasks=[{'id':'source','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(path)}},
           {'id':'focal','operator':'raster_focal_statistics','depends_on':['source'],
            'params':{'input_raster':{'$ref':'source'},'band':1,'window_size':4 if invalid else 5,'method':'mean'}},
           {'id':'save','operator':'raster_save','depends_on':['focal'],
            'params':{'input_raster':{'$ref':'focal'},'access_plan':target_plan(target),'profile':'cog'}}]
    paths=[]; original=workflow_engine._execute_operator
    def execute(name,params):
        if name=='raster_focal_statistics':paths.append(params['input_raster'].workspace)
        return original(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',execute)
    client=api_server.app.test_client()
    response=client.post('/api/workflow',json={'workflow_def':{'tasks':tasks},'input_data':{},'runtime':{
        'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert response.status_code==202,response.json
    deadline=time.monotonic()+10
    while time.monotonic()<deadline:
        status=client.get('/api/executions/'+response.json['execution_id']).json
        if status['status'] in ('success','failed'):break
        time.sleep(.01)
    assert status['status']==('failed' if invalid else 'success'),status
    assert paths and all(not p.exists() for p in paths) and path.read_bytes()==snapshot
    if invalid:assert not target.exists()
    else:
        artifact=json.loads(status['result'])
        assert type(artifact['size_bytes']) is int and artifact['size_bytes']==target.stat().st_size>0
        with raster_workspace():
            assert validate_cog(raster_load(source_plan(target)))['valid']
        ds=gdal.Open(str(target))
        assert ds.GetGeoTransform()==(110,2,.5,30,.25,-3)
        np.testing.assert_allclose(read_band_values(ds.GetRasterBand(1)),_focal_oracle(values,values!=-9999,5,'mean'),equal_nan=True)
        ds=None
    assert client.post('/api/operators/raster_focal_statistics/invoke',json={'params':{}}).status_code==403


def test_focal_statistics_metadata_and_executable_example(tmp_path):
    spec=next(item for item in list_operators() if item['id']=='raster_focal_statistics')
    params={p['name']:p for p in spec['parameters']}
    assert params['input_raster']['param_type']=='input' and params['input_raster']['type']=='raster'
    assert all(params[key]['required'] for key in ('band','window_size','method'))
    assert 'mode' in params['method']['description'] and '并列' in params['method']['description']
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read']
    assert spec['attributes']['resource_groups']==['raster'] and spec['output_ports'][0]['type']=='raster'
    path=create_raster(tmp_path/'source.tif',np.full((2,3),7.))
    result=execute_workflow({'tasks':[
        {'id':'source','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(path)}},
        {'id':'focal',**spec['detailed_description']['workflow_example'],'depends_on':['source']}]})
    assert result['status']=='success',result


@pytest.mark.parametrize('transform,crs',[((0,1,0,6,0,-1),3857),((100,2,.5,200,.25,-3),3857),((110,.01,.001,30,.0005,-.02),4326)])
@pytest.mark.parametrize('all_touched',[False,True])
def test_vector_zonal_statistics_independent_geometry_values_overlap_holes_and_order(tmp_path,transform,crs,all_touched):
    import geopandas as gpd
    import geopandas.testing
    import pandas as pd
    from shapely.affinity import affine_transform
    from shapely.geometry import box
    faces=_rasterization_faces(transform)
    affine=[transform[1],transform[2],transform[4],transform[5],transform[0],transform[3]]
    geometries=list(faces.geometry)+[faces.geometry.iloc[0],affine_transform(box(100,100,101,101),affine),affine_transform(box(1.1,1.1,1.9,1.9),affine)]
    faces=gpd.GeoDataFrame({'code':[7]*7,'name':list('abcdefg'),'shape':geometries},geometry='shape',crs=crs,index=[7,2,7,1,9,0,3])
    original=faces.copy()
    values=np.arange(42,dtype=float).reshape(6,7)-13; values[0,0]=0; values[1,1]=-9999
    values[2,2]=np.nan;values[4,1]=np.inf;values[5,3]=-np.inf
    alpha=np.full(values.shape,128.);alpha[4,2]=0;alpha[3,3]=np.nan;alpha[4,4]=-1;alpha[3,5]=np.inf
    mask=np.full(values.shape,255,dtype=np.uint8);mask[3,2]=0
    path=create_raster(tmp_path/'values.tif',np.stack([np.full(values.shape,np.nan),values,alpha]),transform,'EPSG:'+str(crs))
    ds=gdal.Open(str(path),gdal.GA_Update);ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
    ds.GetRasterBand(2).SetScale(17.);ds.GetRasterBand(2).SetOffset(500.)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
    ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);ds=None
    before=path.read_bytes()
    valid=np.isfinite(values)&(values!=-9999)&np.isfinite(alpha)&(alpha>0)&(mask!=0)
    with raster_workspace():
        result=raster_vector_zonal_statistics(raster_load(source_plan(path)),faces,2,all_touched,field_prefix='stats_')
    assert result.geometry.name=='shape' and result.index.tolist()==faces.index.tolist()
    assert result.columns.tolist()==['code','name','shape']+['stats_'+n for n in ('total_count','valid_count','invalid_count','sum','min','max','mean','stddev')]
    geopandas.testing.assert_geodataframe_equal(result[faces.columns],original)
    for position in range(len(faces)):
        covered=np.isfinite(_rasterization_oracle(faces.iloc[[position]],[1],transform,7,6,all_touched))
        sample=values[covered&valid];row=result.iloc[position]
        assert [row['stats_'+n] for n in ('total_count','valid_count','invalid_count')]==[int(covered.sum()),sample.size,int((covered&~valid).sum())]
        if not sample.size:assert all(pd.isna(row['stats_'+n]) for n in ('sum','min','max','mean','stddev'))
        else:
            actual=[row['stats_'+n] for n in ('sum','min','max','mean','stddev')]
            np.testing.assert_allclose(actual,[math.fsum(sample),min(sample),max(sample),statistics.fmean(sample),statistics.pstdev(sample)],rtol=1e-13,atol=1e-13)
    assert result.iloc[0]['stats_total_count']==result.iloc[4]['stats_total_count']>0
    assert result.iloc[5]['stats_total_count']==0
    assert result.iloc[6]['stats_valid_count']==0 and result.iloc[6]['stats_invalid_count']>0
    assert all(str(result['stats_'+n].dtype)=='int64' for n in ('total_count','valid_count','invalid_count'))
    assert all(str(result['stats_'+n].dtype)=='Float64' for n in ('sum','min','max','mean','stddev'))
    assert path.read_bytes()==before
    geopandas.testing.assert_geodataframe_equal(faces,original)


@pytest.mark.parametrize('all_touched',[False,True])
def test_vector_zonal_statistics_rotated_cross_block_bounded_reads_and_exact_cell_sets(tmp_path,monkeypatch,all_touched):
    import geopandas as gpd
    from shapely.affinity import affine_transform
    from shapely.geometry import box
    transform=(100,2,.5,200,.25,-3)
    affine=[2,.5,.25,-3,100,200]
    yy,xx=np.indices((515,519));values=xx+2.*yy
    faces=gpd.GeoDataFrame(geometry=[affine_transform(box(.1,.1,518.9,514.9),affine),affine_transform(box(507.1,507.1,512.9,512.9),affine)],crs=3857)
    path=create_raster(tmp_path/'large.tif',values,transform,'EPSG:3857')
    with raster_workspace():
        loaded=raster_load(source_plan(path));original=gdal.Band.ReadRaster;reads=[]
        def bounded(self,x=0,y=0,width=None,height=None,*args,**kwargs):
            assert width is not None and height is not None and width<=512 and height<=512
            reads.append((x,y,width,height));return original(self,x,y,width,height,*args,**kwargs)
        with monkeypatch.context() as scoped:
            scoped.setattr(gdal.Band,'ReadRaster',bounded)
            result=raster_vector_zonal_statistics(loaded,faces,1,all_touched)
    for position,sample in enumerate((values.ravel(),values[507:513,507:513].ravel())):
        row=result.iloc[position]
        assert row.zonal_total_count==row.zonal_valid_count==sample.size and row.zonal_invalid_count==0
        np.testing.assert_allclose([row['zonal_'+n] for n in ('sum','min','max','mean','stddev')],
            [math.fsum(sample),min(sample),max(sample),statistics.fmean(sample),statistics.pstdev(sample)],rtol=1e-12)
    assert any(x>0 for x,y,w,h in reads) and any(y>0 for x,y,w,h in reads)


@pytest.mark.parametrize('case',['cancellation','large_offset','opposite_extremes','tiny','overflow'])
def test_vector_zonal_statistics_reuses_stable_streaming_statistics(tmp_path,case):
    import geopandas as gpd
    from shapely.geometry import box
    data=np.zeros((1,1030))
    if case=='cancellation':data[0,[0,512,1024]]=[1e16,1.,-1e16]
    elif case=='large_offset':data[0,:]=np.resize([1e16-2,1e16+2],1030)
    elif case=='opposite_extremes':data[0,:]=np.resize([1e308,-1e308],1030)
    elif case=='tiny':data[0,:]=np.resize([1e-300,-1e-300],1030)
    else:data[:]=1e308
    path=create_raster(tmp_path/'stable.tif',data,(0,1,0,1,0,-1),'EPSG:3857')
    faces=gpd.GeoDataFrame(geometry=[box(-.1,-.1,1030.1,1.1)],crs=3857)
    with raster_workspace():
        loaded=raster_load(source_plan(path))
        if case=='overflow':
            with pytest.raises(ValueError,match='finite Float64'):raster_vector_zonal_statistics(loaded,faces,1)
        else:
            row=raster_vector_zonal_statistics(loaded,faces,1).iloc[0]
            assert row.zonal_valid_count==1030
            if case=='cancellation':assert row.zonal_sum==pytest.approx(1.) and row.zonal_mean==pytest.approx(1/1030,rel=1e-14)
            elif case=='large_offset':assert row.zonal_mean==1e16 and row.zonal_stddev==pytest.approx(2.,rel=1e-14)
            else:assert row.zonal_mean==0 and row.zonal_stddev==pytest.approx(1e308 if case=='opposite_extremes' else 1e-300,rel=1e-14,abs=0)


@pytest.mark.parametrize('outside',[False,True])
def test_vector_zonal_statistics_empty_outside_no_reads_and_custom_empty_prefix(tmp_path,monkeypatch,outside):
    import geopandas as gpd
    from shapely.geometry import box
    faces=gpd.GeoDataFrame({'name':['far'] if outside else []},geometry=[box(1e100,1e100,2e100,2e100)] if outside else [],crs=3857)
    path=create_raster(tmp_path/'source.tif',np.ones((2,3)),crs='EPSG:3857')
    with raster_workspace():
        loaded=raster_load(source_plan(path))
        def forbidden(*args,**kwargs):raise AssertionError('empty/outside zones must not read pixels')
        with monkeypatch.context() as scoped:
            scoped.setattr(gdal.Band,'ReadRaster',forbidden);scoped.setattr(gdal.Band,'ReadAsArray',forbidden)
            result=raster_vector_zonal_statistics(loaded,faces,1,field_prefix='')
        assert len(result)==len(faces) and str(result['sum'].dtype)=='Float64'
        if outside:assert result.iloc[0].total_count==0 and result.iloc[0]['sum'] is __import__('pandas').NA
    with raster_workspace(),pytest.raises(ValueError,match='this execution'):raster_vector_zonal_statistics(loaded,faces,1)


@pytest.mark.parametrize('fault',['missing_vector_crs','different_crs','missing_raster_crs','missing_transform','singular','not_gdf','no_geometry',
    'column_collision','geometry_collision','prefix_none','prefix_bool','coverage_number','coverage_none','band_alpha','band_bool','band_float','band_missing'])
def test_vector_zonal_statistics_rejects_invalid_contracts(tmp_path,fault):
    import geopandas as gpd
    from shapely.geometry import box
    faces=gpd.GeoDataFrame({'code':[1]},geometry=[box(.1,.1,2.9,2.9)],crs=None if fault=='missing_vector_crs' else 3857)
    if fault=='different_crs':faces=faces.to_crs(4326)
    if fault=='not_gdf':faces={}
    if fault=='no_geometry':faces=gpd.GeoDataFrame({'code':[1]})
    if fault=='column_collision':faces['zonal_mean']=999
    if fault=='geometry_collision':faces=faces.rename_geometry('zonal_sum')
    transform=None if fault=='missing_transform' else (0,1,1,2,1,1) if fault=='singular' else (0,1,0,3,0,-1)
    path=create_raster(tmp_path/'source.tif',np.ones((2,3,3)),transform,None if fault=='missing_raster_crs' else 'EPSG:3857')
    if fault=='band_alpha':
        ds=gdal.Open(str(path),gdal.GA_Update);ds.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand);ds=None
    params={'band':2 if fault=='band_alpha' else True if fault=='band_bool' else 1. if fault=='band_float' else 99 if fault=='band_missing' else 1}
    if fault in ('prefix_none','prefix_bool'):params['field_prefix']=None if fault=='prefix_none' else True
    if fault in ('coverage_number','coverage_none'):params['all_touched']=1 if fault=='coverage_number' else None
    with raster_workspace(),pytest.raises(ValueError):raster_vector_zonal_statistics(raster_load(source_plan(path)),faces,**params)


@pytest.mark.parametrize('wkt',[None,'POLYGON EMPTY','POINT (0 0)','LINESTRING (0 0,1 1)',
    'POLYGON ((0 0,2 2,0 2,2 0,0 0))','POLYGON Z ((0 0 1,1 0 1,1 1 1,0 0 1))',
    'POLYGON M ((0 0 1,1 0 1,1 1 1,0 0 1))','POLYGON ((0 0,Infinity 0,1 1,0 0))'])
def test_vector_zonal_statistics_rejects_invalid_geometry_without_repair(tmp_path,wkt):
    import geopandas as gpd
    from shapely import from_wkt
    faces=gpd.GeoDataFrame(geometry=[from_wkt(wkt) if wkt else None],crs=3857)
    path=create_raster(tmp_path/'source.tif',np.ones((2,3)),crs='EPSG:3857')
    with raster_workspace(),pytest.raises(ValueError,match='two-dimensional'):raster_vector_zonal_statistics(raster_load(source_plan(path)),faces,1)


@pytest.mark.parametrize('invalid',[False,True],ids=['gpkg-save','failure-cleanup'])
@pytest.mark.parametrize('zones_mode',['polygonize','null-and-outside'])
def test_vector_zonal_statistics_async_polygonize_chain_gpkg_save_and_cleanup(tmp_path,monkeypatch,invalid,zones_mode):
    import api_server
    import workflow_engine
    import geopandas as gpd
    values=np.array([[0,0,2],[0,2,2]],dtype=float)
    if zones_mode=='null-and-outside':values[0,0]=-9999
    source=create_raster(tmp_path/'classes.tif',values,crs='EPSG:3857')
    target=tmp_path/'statistics.gpkg';before=source.read_bytes()
    tasks=[{'id':'source','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(source)}},
        {'id':'zones','operator':'raster_polygonize','depends_on':['source'],'params':{'input_raster':{'$ref':'source'},'band':1,'connectivity':4}},
        {'id':'stats','operator':'raster_vector_zonal_statistics','depends_on':['source','zones'],
         'params':{'input_raster':{'$ref':'source'},'input_gdf':{'$ref':'zones'},'band':99 if invalid else 1}},
        {'id':'save','operator':'save','depends_on':['stats'],'params':{'input_df':{'$ref':'stats'},'path':target.name,'connection_info':{'mount_path':str(tmp_path)}}}]
    if zones_mode=='null-and-outside':
        tasks[1]={'id':'zones','operator':'load_from_wkt','depends_on':[],'params':{
            'wkt_list':['POLYGON ((0.1 2.1,2.9 2.1,2.9 3.9,0.1 3.9,0.1 2.1))','POLYGON ((0.1 3.1,0.9 3.1,0.9 3.9,0.1 3.9,0.1 3.1))','POLYGON ((20 20,21 20,21 21,20 21,20 20))'],
            'properties':[{'class_value':c} for c in (11,12,13)],'crs':'EPSG:3857'}}
    paths=[];original=workflow_engine._execute_operator
    def execute(name,params):
        if name=='raster_vector_zonal_statistics':paths.append(params['input_raster'].workspace)
        return original(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',execute)
    client=api_server.app.test_client()
    response=client.post('/api/workflow',json={'workflow_def':{'tasks':tasks},'input_data':{},'runtime':{
        'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert response.status_code==202,response.json
    deadline=time.monotonic()+10
    while time.monotonic()<deadline:
        status=client.get('/api/executions/'+response.json['execution_id']).json
        if status['status'] in ('success','failed'):break
        time.sleep(.01)
    assert status['status']==('failed' if invalid else 'success'),status
    assert paths and all(not p.exists() for p in paths) and source.read_bytes()==before
    if invalid:assert not target.exists()
    else:
        saved=gpd.read_file(target).set_index('class_value')
        assert target.stat().st_size>0 and saved.crs.to_epsg()==3857
        if zones_mode=='polygonize':
            assert sorted(saved.index)==[0,2]
            assert saved.zonal_total_count.tolist()==[3,3] and saved.zonal_invalid_count.tolist()==[0,0]
            assert saved.zonal_sum.tolist()==[0,6] and saved.zonal_mean.tolist()==[0,2] and saved.zonal_stddev.tolist()==[0,0]
        else:
            assert sorted(saved.index)==[11,12,13]
            assert saved.zonal_total_count.tolist()==[6,1,0] and saved.zonal_valid_count.tolist()==[5,0,0]
            assert saved.loc[12:13,['zonal_sum','zonal_min','zonal_max','zonal_mean','zonal_stddev']].isna().all().all()
    assert client.post('/api/operators/raster_vector_zonal_statistics/invoke',json={'params':{}}).status_code==403


def test_vector_zonal_statistics_metadata_example_and_native_failure_cleanup(tmp_path,monkeypatch):
    import geopandas as gpd
    from shapely.geometry import box
    spec=next(item for item in list_operators() if item['id']=='raster_vector_zonal_statistics')
    params={p['name']:p for p in spec['parameters']}
    assert params['input_raster']['type']=='raster' and params['input_gdf']['type']=='object' and params['band']['required']
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read'] and spec['attributes']['resource_groups']==['raster']
    assert spec['output_ports'][0]['type']=='GeoDataFrame'
    path=create_raster(tmp_path/'source.tif',np.ones((3,3)),(0,1,0,3,0,-1),'EPSG:3857');before=path.read_bytes()
    with raster_workspace():
        args=dict(spec['detailed_description']['workflow_example']['params'])
        args.update(input_raster=raster_load(source_plan(path)),input_gdf=gpd.GeoDataFrame(geometry=[box(.1,.1,2.9,2.9)],crs=3857))
        assert raster_vector_zonal_statistics(**args).iloc[0].zonal_valid_count==9
        workspace=args['input_raster'].workspace
        def fail(*args,**kwargs):raise RuntimeError('injected zone burn failure')
        monkeypatch.setattr(gdal,'RasterizeLayer',fail)
        with pytest.raises(RuntimeError,match='injected'):raster_vector_zonal_statistics(**args)
    assert not workspace.exists() and path.read_bytes()==before


def proximity_source(path, values, *, dtype=gdal.GDT_Int16, crs='EPSG:3857', transform=(100,2,0,200,0,-2), nodata=-9999):
    data=values[None,...] if values.ndim==2 else values
    dataset=gdal.GetDriverByName('GTiff').Create(str(path),data.shape[2],data.shape[1],data.shape[0],dtype)
    if transform is not None:dataset.SetGeoTransform(transform)
    if crs:
        reference=osr.SpatialReference();reference.SetFromUserInput(crs);dataset.SetProjection(reference.ExportToWkt())
    for index,array in enumerate(data,1):
        band=dataset.GetRasterBand(index);band.WriteArray(array)
        if nodata is not None:band.SetNoDataValue(nodata)
    band=dataset=None
    return path


def proximity_oracle(values, codes, valid, step):
    y,x=np.indices(values.shape)
    targets=np.argwhere(valid & np.isin(values,codes))
    squared=np.full(values.shape,np.inf)
    for row,column in targets:
        squared=np.minimum(squared,(x-int(column))**2+(y-int(row))**2)
    return np.where(valid,np.sqrt(squared)*step,np.nan)


@pytest.mark.parametrize('shape',[(1,1),(1,9),(9,1),(37,41)])
@pytest.mark.parametrize('crs,factor',[('EPSG:3857',1.),('EPSG:2277',1200/3937)])
def test_raster_proximity_exact_centres_targets_zero_raw_values_units_and_cog(tmp_path,shape,crs,factor):
    values=np.full(shape,8,dtype=np.int16)
    values.flat[0]=0;values.flat[-1]=-3
    if values.size>2:values.flat[1]=-9999
    path=proximity_source(tmp_path/'source.tif',values,crs=crs)
    dataset=gdal.Open(str(path),gdal.GA_Update);band=dataset.GetRasterBand(1)
    band.SetScale(17);band.SetOffset(500);band=None;dataset=None
    before=path.read_bytes();valid=values!=-9999
    with raster_workspace():
        result=raster_proximity(raster_load(source_plan(path)),1,[0,-3]);workspace=result.workspace
        assert not list(workspace.glob('*.distance'))
        target=tmp_path/'distance.cog.tif';artifact=raster_save(result,target_plan(target),profile='cog',blocksize=128)
        assert artifact['size_bytes']==target.stat().st_size>0
        assert validate_cog(raster_load(source_plan(target)))['valid']
        dataset=gdal.Open(str(target));band=dataset.GetRasterBand(1)
        assert dataset.GetGeoTransform()==(100,2,0,200,0,-2) and dataset.RasterCount==1
        reference=osr.SpatialReference();reference.ImportFromWkt(dataset.GetProjection());assert reference.GetAuthorityCode(None)==crs.split(':')[-1]
        assert band.DataType==gdal.GDT_Float64 and np.isnan(band.GetNoDataValue()) and band.GetUnitType()=='metre'
        assert band.GetScale() is None and band.GetOffset() is None
        np.testing.assert_allclose(read_band_values(band),proximity_oracle(values,[0,-3],valid,2*factor),rtol=1e-14,atol=1e-14,equal_nan=True)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray(),np.where(valid,255,0))
        band=dataset=None
    assert not workspace.exists() and path.read_bytes()==before


def test_raster_proximity_independent_random_nearest_oracle_catches_gdal_approximation(tmp_path):
    # Seed 37 includes a configuration where GDAL's neighbour propagation
    # misses the true nearest centre by about .088 pixels, beyond Float32 error.
    shape=(37,41)
    for seed in (0,1,13,37,73):
        values=np.full(shape,9,dtype=np.int16)
        values.flat[np.random.default_rng(seed).choice(values.size,size=20,replace=False)]=1
        source=proximity_source(tmp_path/f'source-{seed}.tif',values)
        with raster_workspace():
            result=raster_proximity(raster_load(source_plan(source)),1,[1]);dataset=gdal.Open(str(result.path))
            np.testing.assert_allclose(read_band_values(dataset.GetRasterBand(1)),proximity_oracle(values,[1],np.ones(shape,bool),2),rtol=1e-14,atol=1e-14)
            dataset=None


def test_raster_proximity_cross_block_selected_band_validity_holes_are_not_obstacles(tmp_path,monkeypatch):
    import operators.raster_compute as compute
    values=np.full((515,519),9,dtype=np.int16);values[0,0]=0;values[514,518]=-3
    values[200:207,200:207]=-9999
    alpha=np.full(values.shape,255,dtype=np.int16);alpha[0,0]=128;alpha[100:103,:]=0;alpha[511,511]=-1
    mask=np.full(values.shape,255,dtype=np.uint8);mask[508,510]=0
    source=proximity_source(tmp_path/'source.tif',np.stack([np.zeros_like(values),values,alpha]))
    dataset=gdal.Open(str(source),gdal.GA_Update);dataset.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):dataset.CreateMaskBand(gdal.GMF_PER_DATASET)
    dataset.GetRasterBand(1).GetMaskBand().WriteArray(mask);dataset=None
    before=source.read_bytes();reads=[];original=compute._read_values
    def bounded(band,x,y,width,height):
        assert width<=512 and height<=512
        reads.append((width,height));return original(band,x,y,width,height)
    monkeypatch.setattr(compute,'_read_values',bounded)
    with raster_workspace():
        result=raster_proximity(raster_load(source_plan(source)),2,[0,-3]);dataset=gdal.Open(str(result.path))
        valid=(values!=-9999)&(alpha>0)&(mask>0)
        np.testing.assert_allclose(read_band_values(dataset.GetRasterBand(1)),proximity_oracle(values,[0,-3],valid,2),rtol=1e-14,atol=1e-14,equal_nan=True)
        assert read_band_values(dataset.GetRasterBand(1))[104,0]==208
        dataset=None
    assert reads and max(w for w,h in reads)==512 and source.read_bytes()==before


@pytest.mark.parametrize('dtype,code',[(gdal.GDT_Byte,255),(gdal.GDT_Int8,-128),(gdal.GDT_UInt16,65535),(gdal.GDT_Int16,-32768),(gdal.GDT_UInt32,4294967295),(gdal.GDT_Int32,-2147483648)])
def test_raster_proximity_preserves_all_approved_integer_class_limits(tmp_path,dtype,code):
    source=proximity_source(tmp_path/'source.tif',np.array([[0,code],[0,0]],dtype=np.int64),dtype=dtype,nodata=None)
    with raster_workspace():
        result=raster_proximity(raster_load(source_plan(source)),1,[code]);dataset=gdal.Open(str(result.path))
        np.testing.assert_allclose(read_band_values(dataset.GetRasterBand(1)),[[2,0],[math.sqrt(8),2]],rtol=1e-14)
        dataset=None


@pytest.mark.parametrize('fault',['geographic','missing-crs','missing-grid','rotated','south-up','west-up','nonsquare','unknown-unit',
    'float32','float64','int64','uint64','complex','alpha','band-zero','band-bool','band-float','band-missing',
    'empty-targets','tuple-targets','float-target','bool-target','outside-type','duplicate-target','none-targets','no-targets','all-invalid'])
def test_raster_proximity_rejects_unapproved_inputs_without_intermediates(tmp_path,fault):
    transform={'missing-grid':None,'rotated':(0,2,.1,0,0,-2),'south-up':(0,2,0,0,0,2),
        'west-up':(0,-2,0,0,0,-2),'nonsquare':(0,2,0,0,0,-3)}.get(fault,(0,2,0,0,0,-2))
    crs='EPSG:4326' if fault=='geographic' else '' if fault=='missing-crs' else 'EPSG:3857'
    dtype={'float32':gdal.GDT_Float32,'float64':gdal.GDT_Float64,'int64':gdal.GDT_Int64,
        'uint64':gdal.GDT_UInt64,'complex':gdal.GDT_CFloat32}.get(fault,gdal.GDT_Int16)
    source=proximity_source(tmp_path/'bad.tif',np.ones((3,4)),dtype=dtype,crs=crs,transform=transform,nodata=None if fault=='uint64' else -9999)
    if fault in ('alpha','unknown-unit','all-invalid'):
        data=gdal.Open(str(source),gdal.GA_Update)
        if fault=='alpha':data.GetRasterBand(1).SetColorInterpretation(gdal.GCI_AlphaBand)
        elif fault=='all-invalid':data.GetRasterBand(1).SetNoDataValue(1)
        else:
            srs=osr.SpatialReference();srs.SetProjCS('Unspecified linear unit grid');srs.SetWellKnownGeogCS('WGS84')
            srs.SetMercator(0,13,1,0,0);srs.SetLinearUnits('unknown',1.23456789);data.SetProjection(srs.ExportToWkt())
        data=None
    band={'band-zero':0,'band-bool':True,'band-float':1.,'band-missing':2}.get(fault,1)
    codes={'empty-targets':[],'tuple-targets':(1,),'float-target':[1.],'bool-target':[True],'outside-type':[32768],
        'duplicate-target':[1,1],'none-targets':None,'no-targets':[0]}.get(fault,[1])
    before=source.read_bytes()
    with raster_workspace(),pytest.raises(ValueError):
        loaded=raster_load(source_plan(source));workspace=loaded.workspace;raster_proximity(loaded,band,codes)
    assert not workspace.exists() and source.read_bytes()==before


@pytest.mark.parametrize('failure',['transpose','line','write'])
def test_raster_proximity_failure_closes_files_and_cleans_private_intermediates(tmp_path,monkeypatch,failure):
    import operators.raster_compute as compute
    source=proximity_source(tmp_path/'source.tif',np.array([[1,0],[0,0]],dtype=np.int16))
    def fail(*args,**kwargs):raise RuntimeError('injected distance failure')
    if failure=='transpose':monkeypatch.setattr(compute,'_transpose_distance_file',fail)
    elif failure=='line':monkeypatch.setattr(compute,'_squared_distance_line',fail)
    else:
        class FailingBand:
            def SetUnitType(self,*a):pass
            WriteArray=fail
        class FailingOutput:
            def GetRasterBand(self,*a):return FailingBand()
        monkeypatch.setattr(compute,'_numeric_output',lambda *a:FailingOutput())
    with raster_workspace():
        loaded=raster_load(source_plan(source));workspace=loaded.workspace
        with pytest.raises(RuntimeError,match='injected'):raster_proximity(loaded,1,[1])
        assert not list(workspace.glob('*.distance'))
    assert not workspace.exists()


@pytest.mark.parametrize('invalid',[False,True])
def test_raster_proximity_async_cog_dag_metadata_example_and_workspace_cleanup(tmp_path,monkeypatch,invalid):
    import api_server
    import workflow_engine
    source=proximity_source(tmp_path/'source.tif',np.array([[0,9,9],[9,9,1]],dtype=np.int16))
    target=tmp_path/'distance.cog.tif';before=source.read_bytes()
    spec=next(s for s in list_operators() if s['name']=='raster_proximity')
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read'] and spec['attributes']['resource_groups']==['raster']
    params=spec['detailed_description']['workflow_example']['params'].copy();params['target_values']=[7] if invalid else [0,1]
    tasks=[{'id':'source','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(source)}},
        {'id':'distance','operator':'raster_proximity','depends_on':['source'],'params':params},
        {'id':'save','operator':'raster_save','depends_on':['distance'],'params':{
            'input_raster':{'$ref':'distance'},'access_plan':target_plan(target),'profile':'cog','blocksize':128}}]
    paths=[];original=workflow_engine._execute_operator
    def execute(name,params):
        if name=='raster_proximity':paths.append(params['input_raster'].workspace)
        return original(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',execute)
    client=api_server.app.test_client();response=client.post('/api/workflow',json={'workflow_def':{'tasks':tasks},'input_data':{},'runtime':{
        'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert response.status_code==202,response.json
    deadline=time.monotonic()+10
    while time.monotonic()<deadline:
        status=client.get('/api/executions/'+response.json['execution_id']).json
        if status['status'] in ('success','failed'):break
        time.sleep(.01)
    assert status['status']==('failed' if invalid else 'success'),status
    assert paths and all(not p.exists() for p in paths) and source.read_bytes()==before
    if invalid:assert not target.exists()
    else:
        artifact=json.loads(status['result']);assert artifact['size_bytes']==target.stat().st_size>0
        with raster_workspace():assert validate_cog(raster_load(source_plan(target)))['valid']
    assert client.post('/api/operators/raster_proximity/invoke',json={'params':{}}).status_code==403


def test_raster_proximity_lower_envelope_matches_definition_for_missing_sites_and_ties():
    from operators.raster_compute import _squared_distance_line
    random=np.random.default_rng(84019)
    for length in (1,2,3,17,100):
        for _ in range(20):
            cost=random.integers(0,1000,size=length).astype(float)
            cost[random.random(length)<.4]=np.inf
            coordinates=np.arange(length)
            expected=np.minimum.reduce([(coordinates-i)**2+value for i,value in enumerate(cost)])
            np.testing.assert_array_equal(_squared_distance_line(cost),expected)


@pytest.mark.parametrize('fault',['alpha','mask','nodata','all-invalid-targets'])
def test_raster_proximity_excludes_invalid_targets_without_other_band_interference(tmp_path,fault):
    values=np.array([[1,9,1],[1,1,9]],dtype=np.int16)
    alpha=np.array([[128,255,0],[-1,255,255]],dtype=np.int16)
    mask=np.array([[255,255,255],[255,0,255]],dtype=np.uint8)
    if fault=='alpha':mask[:]=255
    if fault=='mask':alpha[:]=255
    if fault=='nodata':values[0,2]=-9999
    if fault=='all-invalid-targets':alpha[0,0]=0
    path=proximity_source(tmp_path/'targets.tif',np.stack([np.full_like(values,-9999),values,alpha]))
    dataset=gdal.Open(str(path),gdal.GA_Update);dataset.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):dataset.CreateMaskBand(gdal.GMF_PER_DATASET)
    dataset.GetRasterBand(1).GetMaskBand().WriteArray(mask);dataset=None
    with raster_workspace():
        loaded=raster_load(source_plan(path))
        if fault=='all-invalid-targets':
            with pytest.raises(ValueError,match='valid target'):raster_proximity(loaded,2,[1])
        else:
            result=raster_proximity(loaded,2,[1]);dataset=gdal.Open(str(result.path))
            valid=(alpha>0)&(mask>0)&(values!=-9999)
            np.testing.assert_allclose(read_band_values(dataset.GetRasterBand(1)),proximity_oracle(values,[1],valid,2),rtol=1e-14,equal_nan=True)
            dataset=None


def test_raster_proximity_truncated_intermediate_fails_and_is_removed(tmp_path,monkeypatch):
    import operators.raster_compute as compute
    source=proximity_source(tmp_path/'source.tif',np.array([[1,0],[0,0]],dtype=np.int16))
    transpose=compute._transpose_distance_file
    def truncated(source,target,width,height):
        transpose(source,target,width,height)
        with target.open('r+b') as file:file.truncate(0)
    monkeypatch.setattr(compute,'_transpose_distance_file',truncated)
    with raster_workspace():
        loaded=raster_load(source_plan(source))
        with pytest.raises(RuntimeError,match='Incomplete private distance'):raster_proximity(loaded,1,[1])
        assert not list(loaded.workspace.glob('*.distance'))


def test_raster_proximity_overflow_fails_only_for_valid_distance_values(tmp_path,monkeypatch):
    import operators.raster_compute as compute
    monkeypatch.setattr(compute,'_projected_north_up_grid',lambda dataset:1e308)
    for invalid_far in (False,True):
        values=np.array([[1,9,-9999 if invalid_far else 9]],dtype=np.int16)
        source=proximity_source(tmp_path/f'source-{invalid_far}.tif',values,transform=(0,1,0,0,0,-1))
        with raster_workspace():
            loaded=raster_load(source_plan(source))
            if not invalid_far:
                with pytest.raises(ValueError,match='finite Float64'):raster_proximity(loaded,1,[1])
            else:
                result=raster_proximity(loaded,1,[1]);dataset=gdal.Open(str(result.path))
                np.testing.assert_array_equal(read_band_values(dataset.GetRasterBand(1)),[[0.,1e308,np.nan]])
                dataset=None
            assert not list(loaded.workspace.glob('*.distance'))


@pytest.mark.parametrize('window_size',[3,5])
@pytest.mark.parametrize('values',[
    [[0,0,7],[0,7,7],[-3,-3,9]],
    [[0,0,7],[0,9,7],[7,-3,-3]],
    [[2**53-2,2**53-2,2**53-1],[2**53-2,2**53-1,2**53-1],[0,7,-3]],
    [[-(2**53-1),-(2**53-1),-(2**53-2)],[-(2**53-1),-(2**53-2),-(2**53-2)],[0,7,-3]],
    [[-3,0,-3],[0,0,7],[7,7,7]],
    [[-3,0,-3],[0,7,0],[-3,7,-3]],
    [[0,1,2],[3,4,5],[6,7,8]],
    [[2**53-1,2**53-2,2**53-1],[2**53-2,0,2**53-2],[2**53-1,7,2**53-1]],
    [[-(2**53-1),-(2**53-2),-(2**53-1)],[-(2**53-2),0,-(2**53-2)],[-(2**53-1),7,-(2**53-1)]],
])
def test_focal_mode_exact_classes_ties_center_preference_then_smaller(tmp_path,window_size,values):
    values=np.array(values,dtype=float);source=create_raster(tmp_path/'classes.tif',values)
    expected=_focal_oracle(values,np.ones(values.shape,bool),window_size,'mode')
    with raster_workspace():
        result=raster_focal_statistics(raster_load(source_plan(source)),1,window_size,'mode')
        ds=gdal.Open(str(result.path));np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),expected);ds=None
    counts=__import__('collections').Counter(values.flat)
    modes=[v for v,c in counts.items() if c==max(counts.values())]
    assert expected[1,1]==(values[1,1] if values[1,1] in modes else min(modes))


@pytest.mark.parametrize('window_size',[3,5])
@pytest.mark.parametrize('shape',[(1,1),(1,7),(7,1),(3,4)])
@pytest.mark.parametrize('all_invalid',[False,True])
def test_focal_mode_thin_edges_all_invalid_and_missing_location(tmp_path,window_size,shape,all_invalid):
    values=(np.arange(math.prod(shape)).reshape(shape)%3-1).astype(float)
    if all_invalid:values[:]=np.nan
    source=create_raster(tmp_path/'unlocated.tif',values,transform=None,crs='')
    before=source.read_bytes()
    with raster_workspace():
        result=raster_focal_statistics(raster_load(source_plan(source)),1,window_size,'mode');workspace=result.workspace
        ds=gdal.Open(str(result.path));band=ds.GetRasterBand(1)
        assert ds.GetGeoTransform(can_return_null=True) is None and ds.GetProjection()==''
        assert band.DataType==gdal.GDT_Float64 and math.isnan(band.GetNoDataValue())
        np.testing.assert_array_equal(read_band_values(band),_focal_oracle(values,np.isfinite(values),window_size,'mode'))
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray(),np.where(np.isfinite(values),255,0));band=ds=None
    assert not workspace.exists() and source.read_bytes()==before


@pytest.mark.parametrize('window_size',[3,5])
def test_focal_mode_full_classification_across_blocks_independent_counter_and_cog(tmp_path,monkeypatch,window_size):
    import operators.raster_compute as compute
    y,x=np.indices((515,519));classes=np.array([-3.,0.,7.])[(x//11+y//13)%3]
    classes[200:207,200:207]=-9999;classes[509,509]=np.nan;classes[511,512]=np.inf
    alpha=np.full(classes.shape,128.);alpha[0,1]=0;alpha[511,511]=-1;alpha[508,510]=np.nan
    mask=np.full(classes.shape,255,dtype=np.uint8);mask[510,509]=0
    transform=(100,2,.5,200,.25,-3)
    path=create_raster(tmp_path/'source.tif',np.stack([np.full(classes.shape,.5),classes,alpha]),transform,'EPSG:3857')
    ds=gdal.Open(str(path),gdal.GA_Update);ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
    ds.GetRasterBand(2).SetScale(17);ds.GetRasterBand(2).SetOffset(500)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
    ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);ds=None
    before=path.read_bytes();valid=np.isfinite(classes)&(classes!=-9999)&np.isfinite(alpha)&(alpha>0)&(mask!=0)
    expected=_focal_oracle(classes,valid,window_size,'mode');original=compute._read_values;reads=[]
    def bounded(band,x,y,width,height):
        assert width<=512 and height<=512
        reads.append((x,y,width,height));return original(band,x,y,width,height)
    monkeypatch.setattr(compute,'_read_values',bounded)
    with raster_workspace():
        result=raster_focal_statistics(raster_load(source_plan(path)),2,window_size,'mode');workspace=result.workspace
        target=tmp_path/'mode.cog.tif';artifact=raster_save(result,target_plan(target),profile='cog',blocksize=128)
        assert artifact['size_bytes']==target.stat().st_size>0 and validate_cog(raster_load(source_plan(target)))['valid']
        ds=gdal.Open(str(target));band=ds.GetRasterBand(1)
        assert ds.GetGeoTransform()==transform and ds.GetSpatialRef().GetAuthorityCode(None)=='3857'
        assert band.GetScale() is None and band.GetOffset() is None
        np.testing.assert_array_equal(read_band_values(band),expected)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray(),np.where(valid,255,0));band=ds=None
    assert reads and any(x>0 for x,y,w,h in reads) and any(y>0 for x,y,w,h in reads)
    assert path.read_bytes()==before and not workspace.exists()


@pytest.mark.parametrize('datatype,value',[(gdal.GDT_Float64,.5),(gdal.GDT_Float64,2**53),
    (gdal.GDT_Float64,-2**53),(gdal.GDT_Int64,2**53+1),(gdal.GDT_Int64,-2**53-1),(gdal.GDT_UInt64,2**64-1)])
def test_focal_mode_rejects_fractional_or_unsafe_original_classes_without_rounding(tmp_path,datatype,value):
    path=tmp_path/'invalid.tif';ds=gdal.GetDriverByName('GTiff').Create(str(path),3,2,1,datatype)
    ds.GetRasterBand(1).Fill(0)
    dtype=np.uint64 if datatype==gdal.GDT_UInt64 else np.int64 if datatype==gdal.GDT_Int64 else float
    ds.GetRasterBand(1).WriteArray(np.array([[value]],dtype=dtype));ds=None;before=path.read_bytes()
    with raster_workspace(),pytest.raises(ValueError,match='exact safe integers'):
        source=raster_load(source_plan(path));workspace=source.workspace;raster_focal_statistics(source,1,3,'mode')
    assert not workspace.exists() and path.read_bytes()==before


def test_focal_mode_only_valid_selected_classes_are_checked_and_hidden_decimals_ignored(tmp_path):
    values=np.array([[.5,2**53,-2**53],[0,-3,7]],dtype=float)
    alpha=np.array([[255,0,255],[128,255,255]],dtype=float);mask=np.array([[255,255,0],[255,255,255]],dtype=np.uint8)
    path=create_raster(tmp_path/'source.tif',np.stack([np.full(values.shape,.25),values,alpha]))
    ds=gdal.Open(str(path),gdal.GA_Update);ds.GetRasterBand(2).SetNoDataValue(.5)
    ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
    ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);ds=None
    valid=(values!=.5)&(alpha>0)&(mask!=0)
    with raster_workspace():
        result=raster_focal_statistics(raster_load(source_plan(path)),2,3,'mode');ds=gdal.Open(str(result.path))
        np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),_focal_oracle(values,valid,3,'mode'));ds=None


@pytest.mark.parametrize('case',['direct','reclassified','invalid'])
def test_focal_mode_async_reclassification_cog_and_failure_cleanup(tmp_path,monkeypatch,case):
    import api_server,workflow_engine
    values=np.array([[0,0,8,8],[0,-9999,4,8],[4,4,4,8]],dtype=float)
    if case=='invalid':values[0,0]=.5
    path=create_raster(tmp_path/'source.tif',values,(100,2,.5,200,.25,-3),'EPSG:3857');target=tmp_path/'mode.cog.tif';before=path.read_bytes()
    tasks=[{'id':'source','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(path)}}];source='source'
    classified=values.copy()
    if case=='reclassified':
        tasks.append({'id':'classes','operator':'raster_reclassify','depends_on':['source'],'params':{
            'input_raster':{'$ref':'source'},'band':1,'rules':[{'value':0,'class':-3},{'value':4,'class':0},{'value':8,'class':7}]}})
        source='classes';classified=np.select([values==0,values==4,values==8],[-3.,0.,7.],default=np.nan)
    tasks += [{'id':'mode','operator':'raster_focal_statistics','depends_on':[source],
        'params':{'input_raster':{'$ref':source},'band':1,'window_size':3,'method':'mode'}},
        {'id':'save','operator':'raster_save','depends_on':['mode'],'params':{
            'input_raster':{'$ref':'mode'},'access_plan':target_plan(target),'profile':'cog','blocksize':128}}]
    paths=[];original=workflow_engine._execute_operator
    def execute(name,params):
        if name=='raster_focal_statistics':paths.append(params['input_raster'].workspace)
        return original(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',execute)
    client=api_server.app.test_client();response=client.post('/api/workflow',json={'workflow_def':{'tasks':tasks},'input_data':{},'runtime':{
        'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert response.status_code==202,response.json
    deadline=time.monotonic()+10
    while time.monotonic()<deadline:
        status=client.get('/api/executions/'+response.json['execution_id']).json
        if status['status'] in ('success','failed'):break
        time.sleep(.01)
    assert status['status']==('failed' if case=='invalid' else 'success'),status
    assert paths and all(not p.exists() for p in paths) and path.read_bytes()==before
    if case=='invalid':assert not target.exists()
    else:
        expected=_focal_oracle(classified,np.isfinite(classified)&(classified!=-9999),3,'mode')
        with raster_workspace():assert validate_cog(raster_load(source_plan(target)))['valid']
        ds=gdal.Open(str(target));np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),expected);ds=None
        artifact=json.loads(status['result']);assert artifact['size_bytes']==target.stat().st_size>0
    assert client.post('/api/operators/raster_focal_statistics/invoke',json={'params':{}}).status_code==403


@pytest.mark.parametrize('datatype,code',[(gdal.GDT_Int64,2**53-1),(gdal.GDT_Int64,-(2**53-1)),(gdal.GDT_UInt64,2**53-1)])
def test_focal_mode_safe_native_int64_classes_preserve_exact_values(tmp_path,datatype,code):
    adjacent=code+1 if code<0 else code-1
    path=tmp_path/'safe.tif';ds=gdal.GetDriverByName('GTiff').Create(str(path),2,2,1,datatype)
    ds.GetRasterBand(1).WriteArray(np.array([[code,adjacent],[0,code]],dtype=np.uint64 if datatype==gdal.GDT_UInt64 else np.int64));ds=None
    before=path.read_bytes()
    with raster_workspace():
        result=raster_focal_statistics(raster_load(source_plan(path)),1,3,'mode');ds=gdal.Open(str(result.path))
        np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),np.full((2,2),code,dtype=float));ds=None
    assert path.read_bytes()==before


def test_focal_mode_result_connects_existing_classification_polygonize_without_filling_holes(tmp_path):
    from operators.raster_compute import raster_polygonize
    classes=np.array([[0,0,7,7],[0,-9999,7,7],[-3,-3,0,7]],dtype=float)
    transform=(100,2,.5,200,.25,-3);source=create_raster(tmp_path/'classes.tif',classes,transform,'EPSG:3857')
    before=source.read_bytes();valid=classes!=-9999;expected=_focal_oracle(classes,valid,3,'mode')
    determinant=abs(transform[1]*transform[5]-transform[2]*transform[4])
    with raster_workspace():
        filtered=raster_focal_statistics(raster_load(source_plan(source)),1,3,'mode')
        regions=raster_polygonize(filtered,1,4)
        assert regions.crs.to_epsg()==3857
        assert set(int(c) for c in regions.class_value)==set(int(c) for c in np.unique(expected[valid]))
        for code in np.unique(expected[valid]):
            area=sum(g.area for g in regions.loc[regions.class_value==int(code)].geometry)
            assert area==np.count_nonzero(expected==code)*determinant
        assert regions.area.sum()==np.count_nonzero(valid)*determinant
    assert source.read_bytes()==before


# Boundary clean uses an independent scalar oracle: immutable full stages, not halos.
def _boundary_clean_oracle(values, valid):
    from collections import Counter
    counts = Counter(int(value) for value in values[valid])
    height, width = values.shape
    def neighbours(y, x):
        return ((ny, nx) for ny in range(max(0, y-1), min(height, y+2))
                for nx in range(max(0, x-1), min(width, x+2)) if valid[ny, nx])
    expanded = np.full(values.shape, np.nan)
    result = expanded.copy()
    for y, x in np.argwhere(valid):
        choices = [int(values[ny, nx]) for ny, nx in neighbours(y, x)]
        expanded[y, x] = max(choices, key=lambda value: (counts[value], -value))
    for y, x in np.argwhere(valid):
        original = values[y, x]
        result[y, x] = (original if any(expanded[ny, nx] == original for ny, nx in neighbours(y, x))
                        else expanded[y, x])
    return result


@pytest.mark.parametrize('shape', [(1,1), (1,17), (19,1), (8,11), (513,517)])
def test_boundary_clean_complete_oracle_edges_halos_and_stage_snapshot(tmp_path, shape, monkeypatch):
    import operators.raster_compute as compute
    rng = np.random.default_rng(811)
    values = rng.choice([0., -7., 42.], size=shape, p=[.55,.35,.1])
    valid = rng.random(shape) > .07
    values[~valid] = -9999
    transform = (100,2,.5,200,.25,-3)
    path = create_raster(tmp_path/'source.tif',values,transform,'EPSG:3857')
    before = path.read_bytes()
    windows = []
    blocks = compute._blocks
    def bounded(dataset, indices, **kwargs):
        for x,y,arrays,masks in blocks(dataset,indices,**kwargs):
            assert all(array.shape[0] <= 512 and array.shape[1] <= 512 for array in arrays)
            windows.append((x,y,arrays[0].shape))
            yield x,y,arrays,masks
    monkeypatch.setattr(compute,'_blocks',bounded)
    expected = _boundary_clean_oracle(values,valid)
    with raster_workspace():
        source = raster_load(source_plan(path)); prior = set(source.workspace.iterdir())
        result = compute.raster_boundary_clean(source,1)
        assert set(source.workspace.iterdir()) == prior | {result.path}
        ds = gdal.Open(str(result.path)); band = ds.GetRasterBand(1)
        np.testing.assert_array_equal(read_band_values(band),expected)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,valid)
        assert ds.GetGeoTransform() == transform and ds.RasterCount == 1
        assert band.DataType == gdal.GDT_Float64 and math.isnan(band.GetNoDataValue())
        assert band.GetScale() is None and band.GetOffset() is None and band.GetColorTable() is None
        band = ds = None
        if shape == (513,517):
            target = tmp_path/'clean.cog.tif'
            artifact = raster_save(result,target_plan(target),profile='cog',blocksize=128)
            assert artifact['size_bytes'] == target.stat().st_size > 0
            assert validate_cog(raster_load(source_plan(target)))['valid']
            ds=gdal.Open(str(target));saved=ds.GetRasterBand(1)
            np.testing.assert_array_equal(read_band_values(saved),expected)
            np.testing.assert_array_equal(saved.GetMaskBand().ReadAsArray()!=0,valid)
            assert ds.GetGeoTransform()==transform and ds.GetSpatialRef().GetAuthorityCode(None)=='3857'
            assert saved.GetOverviewCount()>0
            saved=ds=None
    assert windows and path.read_bytes() == before


def test_boundary_clean_global_disconnected_counts_override_code_and_local_majority(tmp_path):
    import operators.raster_compute as compute
    # Class 1 wins globally, despite its remote disconnected regions. Local mode
    # would pick 90; numerical priority would also incorrectly pick 90.
    values = np.full((11,19),1.)
    values[3:8,5:14] = 90
    values[5,9] = 1
    values[5,8] = 0
    valid = np.ones(values.shape,dtype=bool)
    expected = _boundary_clean_oracle(values,valid)
    assert expected[5,8] == 1 and expected[5,9] == 1
    path = create_raster(tmp_path/'source.tif',values)
    with raster_workspace():
        result = compute.raster_boundary_clean(raster_load(source_plan(path)),1)
        ds = gdal.Open(str(result.path));np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),expected);ds=None


@pytest.mark.parametrize('codes', [(-7,0), (2**53-2,2**53-1), (-(2**53-1),-(2**53-2))])
def test_boundary_clean_equal_global_counts_smaller_exact_class_wins(tmp_path, codes):
    import operators.raster_compute as compute
    values = np.tile(np.array(codes,dtype=np.int64),(7,4))
    path=tmp_path/'classes.tif';ds=gdal.GetDriverByName('GTiff').Create(str(path),8,7,1,gdal.GDT_Int64)
    ds.GetRasterBand(1).WriteArray(values);ds=None
    with raster_workspace():
        result=compute.raster_boundary_clean(raster_load(source_plan(path)),1)
        ds=gdal.Open(str(result.path));actual=read_band_values(ds.GetRasterBand(1));ds=None
        np.testing.assert_array_equal(actual,np.full(values.shape,float(min(codes))))


@pytest.mark.parametrize('position', ['none','crs-only','transform-only'])
def test_boundary_clean_preserves_missing_location_and_all_invalid(tmp_path, position):
    import operators.raster_compute as compute
    path=create_raster(tmp_path/'empty.tif',np.full((4,6),-9999.),
                       None if position!='transform-only' else (0,2,.5,0,.2,-3),
                       'EPSG:4326' if position=='crs-only' else '')
    with raster_workspace():
        source=raster_load(source_plan(path));facts=raster_info(source)
        result=compute.raster_boundary_clean(source,1);info=raster_info(result)
        for key in ('width','height','crs','transform','extent'):assert info[key]==facts[key]
        ds=gdal.Open(str(result.path));assert np.isnan(read_band_values(ds.GetRasterBand(1))).all();ds=None


def test_boundary_clean_selected_band_mask_alpha_nonfinite_and_raw_classes(tmp_path):
    import operators.raster_compute as compute
    values=np.array([[0,2,.5,2**53,1],[2,0,-3,1,np.inf],[0,1,-3,2,np.nan]],dtype=float)
    alpha=np.full(values.shape,255.);alpha[0,3]=0;alpha[1,4]=np.nan;alpha[2,4]=-1;alpha[1,1]=128
    mask=np.full(values.shape,255,dtype=np.uint8);mask[0,2]=0
    path=create_raster(tmp_path/'source.tif',np.stack([np.full(values.shape,.25),values,alpha]))
    ds=gdal.Open(str(path),gdal.GA_Update)
    ds.GetRasterBand(2).SetScale(100);ds.GetRasterBand(2).SetOffset(12)
    ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
    ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);ds=None
    valid=np.isfinite(values)&np.isfinite(alpha)&(alpha>0)&(mask>0)
    with raster_workspace():
        result=compute.raster_boundary_clean(raster_load(source_plan(path)),2)
        ds=gdal.Open(str(result.path));np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),_boundary_clean_oracle(values,valid));ds=None


@pytest.mark.parametrize('value', [.5,2**53,-2**53])
def test_boundary_clean_rejects_noninteger_or_unsafe_valid_classes_before_output(tmp_path,value):
    import operators.raster_compute as compute
    values=np.ones((5,5));values[2,2]=value
    path=create_raster(tmp_path/'bad.tif',values)
    with raster_error_language('en'),raster_workspace():
        source=raster_load(source_plan(path));before=set(source.workspace.iterdir())
        with pytest.raises(ValueError,match='classification values'):compute.raster_boundary_clean(source,1)
        assert set(source.workspace.iterdir())==before


@pytest.mark.parametrize('band', [True,0,-1,1.5,3])
def test_boundary_clean_rejects_illegal_bands(tmp_path,band):
    import operators.raster_compute as compute
    path=create_raster(tmp_path/'source.tif',np.ones((3,3)))
    with raster_error_language('en'),raster_workspace(),pytest.raises(ValueError,match='band'):compute.raster_boundary_clean(raster_load(source_plan(path)),band)


@pytest.mark.parametrize('case', ['data-complex','alpha-complex','selected-alpha'])
def test_boundary_clean_rejects_complex_and_alpha_data(tmp_path,case):
    import operators.raster_compute as compute
    path=tmp_path/'source.tif'
    ds=gdal.GetDriverByName('GTiff').Create(str(path),3,3,2,gdal.GDT_CFloat64 if 'complex' in case else gdal.GDT_Float64)
    for i in (1,2):ds.GetRasterBand(i).WriteArray(np.ones((3,3)))
    if case!='data-complex':ds.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand)
    ds=None
    with raster_error_language('en'),raster_workspace(),pytest.raises(ValueError,match='[Cc]omplex|alpha'):
        compute.raster_boundary_clean(raster_load(source_plan(path)),2 if case=='selected-alpha' else 1)


@pytest.mark.parametrize('stage', ['expand','shrink'])
def test_boundary_clean_intermediate_and_failed_output_removed(tmp_path,monkeypatch,stage):
    import operators.raster_compute as compute
    path=create_raster(tmp_path/'source.tif',np.ones((6,7)))
    windows=compute._cell_neighbourhood_blocks
    def fail(dataset,band):
        # A read-only reopened dataset is the complete expansion snapshot.
        expanding=dataset.GetRasterBand(1).GetNoDataValue()==-9999
        for block in windows(dataset,band):
            yield block
            if expanding == (stage=='expand'):raise RuntimeError('injected stage failure')
    monkeypatch.setattr(compute,'_cell_neighbourhood_blocks',fail)
    with raster_workspace():
        source=raster_load(source_plan(path));before=set(source.workspace.iterdir())
        with pytest.raises(RuntimeError,match='injected'):compute.raster_boundary_clean(source,1)
        assert set(source.workspace.iterdir())==before


@pytest.mark.parametrize('case',['direct','reclassified','invalid'])
def test_boundary_clean_async_cog_reclassification_and_failure_cleanup(tmp_path,monkeypatch,case):
    import api_server,workflow_engine
    values=np.array([[0,0,8,8],[0,-9999,4,8],[4,4,4,8]],dtype=float)
    if case=='invalid':values[0,0]=.5
    path=create_raster(tmp_path/'source.tif',values,(100,2,.5,200,.25,-3),'EPSG:3857')
    target=tmp_path/'clean.cog.tif';before=path.read_bytes()
    tasks=[{'id':'source','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(path)}}];source='source';classified=values.copy()
    if case=='reclassified':
        tasks.append({'id':'classes','operator':'raster_reclassify','depends_on':['source'],'params':{
            'input_raster':{'$ref':'source'},'band':1,'rules':[{'value':0,'class':-3},{'value':4,'class':0},{'value':8,'class':7}]}})
        source='classes';classified=np.select([values==0,values==4,values==8],[-3.,0.,7.],default=np.nan)
    tasks += [{'id':'clean','operator':'raster_boundary_clean','depends_on':[source],'params':{'input_raster':{'$ref':source},'band':1}},
              {'id':'save','operator':'raster_save','depends_on':['clean'],'params':{'input_raster':{'$ref':'clean'},'access_plan':target_plan(target),'profile':'cog','blocksize':128}}]
    paths=[];original=workflow_engine._execute_operator
    def execute(name,params):
        if name=='raster_boundary_clean':paths.append(params['input_raster'].workspace)
        return original(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',execute)
    client=api_server.app.test_client();response=client.post('/api/workflow',json={'workflow_def':{'tasks':tasks},'input_data':{},'runtime':{
        'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert response.status_code==202,response.json
    deadline=time.monotonic()+10
    while time.monotonic()<deadline:
        status=client.get('/api/executions/'+response.json['execution_id']).json
        if status['status'] in ('success','failed'):break
        time.sleep(.01)
    assert status['status']==('failed' if case=='invalid' else 'success'),status
    assert paths and all(not p.exists() for p in paths) and path.read_bytes()==before
    if case=='invalid':assert not target.exists()
    else:
        with raster_workspace():assert validate_cog(raster_load(source_plan(target)))['valid']
        ds=gdal.Open(str(target));np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),_boundary_clean_oracle(classified,np.isfinite(classified)&(classified!=-9999)));ds=None
        artifact=json.loads(status['result']);assert artifact['size_bytes']==target.stat().st_size>0
    assert client.post('/api/operators/raster_boundary_clean/invoke',json={'params':{}}).status_code==403


def test_boundary_clean_discovery_single_required_band_and_workflow_resource_contract():
    spec=next(item for item in list_operators() if item['id']=='raster_boundary_clean')
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read']
    assert spec['attributes']['resource_groups']==['raster']
    assert spec['category_path']==['栅格处理','像元计算']
    assert [p['name'] for p in spec['parameters']]==['input_raster','band']
    assert all(p['required'] for p in spec['parameters'])
    assert spec['detailed_description']['workflow_example']['params']['band']==1


@pytest.mark.parametrize('dtype,value,nodata',[(gdal.GDT_Int64,2**53+1,2**53),
    (gdal.GDT_Int64,-2**53-1,-2**53),(gdal.GDT_UInt64,2**64-1,2**64-2)])
def test_boundary_clean_native_invalid_class_cannot_hide_behind_rounded_nodata(tmp_path,dtype,value,nodata):
    import operators.raster_compute as compute
    path=tmp_path/'native.tif';ds=gdal.GetDriverByName('GTiff').Create(str(path),2,2,1,dtype)
    ds.GetRasterBand(1).SetNoDataValue(nodata)
    ds.GetRasterBand(1).WriteArray(np.array([[nodata,value],[0,0]],dtype=np.uint64 if dtype==gdal.GDT_UInt64 else np.int64));ds=None
    with raster_error_language('en'),raster_workspace(),pytest.raises(ValueError,match='exact safe integers'):
        compute.raster_boundary_clean(raster_load(source_plan(path)),1)


@pytest.mark.parametrize('language,prefix',[('zh-cn','边界清理'),('en','Boundary cleaning')])
@pytest.mark.parametrize('reason',['band','alpha','complex','classes'])
def test_boundary_clean_diagnostics_follow_submission_language(tmp_path,language,prefix,reason):
    import operators.raster_compute as compute
    path=tmp_path/'source.tif';ds=gdal.GetDriverByName('GTiff').Create(str(path),2,2,1,
                       gdal.GDT_CFloat64 if reason=='complex' else gdal.GDT_Float64)
    ds.GetRasterBand(1).Fill(.5 if reason=='classes' else 0)
    if reason=='alpha':ds.GetRasterBand(1).SetColorInterpretation(gdal.GCI_AlphaBand)
    ds=None
    with raster_error_language(language),raster_workspace(),pytest.raises(ValueError,match='^'+prefix):
        compute.raster_boundary_clean(raster_load(source_plan(path)),0 if reason=='band' else 1)


@pytest.mark.parametrize('dtype', [gdal.GDT_Int64,gdal.GDT_UInt64])
def test_boundary_clean_safe_classes_with_unsafe_nodata_and_explicit_mask(tmp_path,dtype):
    import operators.raster_compute as compute
    path=tmp_path/'native.tif';ds=gdal.GetDriverByName('GTiff').Create(str(path),5,3,1,dtype)
    array=np.array([[0,2**53-1,2**53-1,0,2**53],[2**53-1,0,0,2**53-1,0],[0,0,2**53-1,2**53-1,0]],
                   dtype=np.uint64 if dtype==gdal.GDT_UInt64 else np.int64)
    ds.GetRasterBand(1).SetNoDataValue(2**53);ds.GetRasterBand(1).WriteArray(array)
    mask=np.full(array.shape,255,dtype=np.uint8);mask[1,1]=0
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
    ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);ds=None
    valid=(array!=2**53)&(mask!=0)
    with raster_workspace():
        result=compute.raster_boundary_clean(raster_load(source_plan(path)),1)
        ds=gdal.Open(str(result.path));np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),_boundary_clean_oracle(array,valid));ds=None


# Independent scalar Guo–Hall decisions, evaluated on a whole immutable image.
# Neighbour bit order E, NE, N, NW, W, SW, S, SE; no product helpers or halos.
def _thin_scalar_delete(bits, phase):
    transitions = sum(not bits[i] and (bits[(i+1)%8] or bits[(i+2)%8]) for i in (0,2,4,6))
    n1 = sum(bits[i] or bits[i+1] for i in (0,2,4,6))
    n2 = sum(bits[i] or bits[(i+1)%8] for i in (1,3,5,7))
    direction = ((bits[1] or bits[2] or not bits[7]) and bits[0]) if phase == 0 else ((bits[5] or bits[6] or not bits[3]) and bits[4])
    return transitions == 1 and min(n1,n2) in (2,3) and not direction


def _thin_oracle(values, valid, foreground):
    image = valid & (values == foreground)
    offsets = ((0,1),(-1,1),(-1,0),(-1,-1),(0,-1),(1,-1),(1,0),(1,1))
    while True:
        count = int(image.sum())
        for phase in (0,1):
            padded = np.pad(image,1)
            output = image.copy()
            for y,x in np.argwhere(image):
                bits = [bool(padded[y+1+dy,x+1+dx]) for dy,dx in offsets]
                if _thin_scalar_delete(bits,phase):output[y,x] = False
            image = output
        if count == int(image.sum()):return np.where(valid,image.astype(float),np.nan)


def test_raster_thin_all_neighbourhoods_and_phase_order():
    import operators.raster_compute as compute
    offsets = ((1,2),(0,2),(0,1),(0,0),(1,0),(2,0),(2,1),(2,2))
    for phase in (0,1):
        for number in range(256):
            bits = [bool(number & (1 << i)) for i in range(8)]
            values = np.zeros((3,3));values[1,1] = 1
            for point,value in zip(offsets,bits):values[point] = value
            actual,removed = compute._thin_subiteration(values,phase)
            expected = _thin_scalar_delete(bits,phase)
            assert removed == expected and actual[0,0] == (not expected),(phase,number)
            values[1,1] = 0
            assert not compute._thin_subiteration(values,phase)[1]


def test_raster_thin_published_guo_hall_golden_and_idempotence(tmp_path):
    import operators.raster_compute as compute
    # The published scikit-image Guo–Hall example fixes orientation and phase order.
    values = np.zeros((7,7));values[1:-1,2:-2] = 1;values[0,1] = 1
    expected = np.zeros((7,7));expected[0,1] = expected[1,2] = 1;expected[2:5,3] = 1
    path=create_raster(tmp_path/'golden.tif',values)
    with raster_workspace():
        result=compute.raster_thin(raster_load(source_plan(path)),1,1)
        second=compute.raster_thin(result,1,1)
        for item in (result,second):
            ds=gdal.Open(str(item.path));np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),expected);ds=None


def _thin_components(image, connectivity):
    remaining=set(map(tuple,np.argwhere(image)));count=0
    offsets=[(y,x) for y in (-1,0,1) for x in (-1,0,1) if (y or x) and (connectivity==8 or abs(y)+abs(x)==1)]
    while remaining:
        pending=[remaining.pop()];count+=1
        while pending:
            y,x=pending.pop()
            for dy,dx in offsets:
                point=(y+dy,x+dx)
                if point in remaining:remaining.remove(point);pending.append(point)
    return count


@pytest.mark.parametrize('shape',['line','diagonal','t','y','cross','ring','point','border','square'])
def test_raster_thin_topology_holes_endpoints_and_full_oracle(tmp_path,shape):
    import operators.raster_compute as compute
    values=np.zeros((21,25))
    if shape=='line':values[8:13,3:22]=1
    elif shape=='diagonal':
        for i in range(3,18):values[i-1:i+2,i-1:i+2]=1
    elif shape in ('t','cross'):
        values[3:18,10:15]=1;values[3:8 if shape=='t' else 13,3:22]=1
    elif shape=='y':
        values[10:19,10:13]=1
        for i in range(2,11):values[i,i:i+3]=1;values[i,20-i:23-i]=1
    elif shape=='ring':values[3:18,3:22]=1;values[7:14,7:18]=0
    elif shape=='point':values[4,4]=1;values[15,20]=1
    elif shape=='border':values[:5,:18]=1;values[:18,:5]=1
    else:values[8:10,9:11]=1
    expected=_thin_oracle(values,np.ones(values.shape,bool),1)
    path=create_raster(tmp_path/'shape.tif',values)
    with raster_workspace():
        result=compute.raster_thin(raster_load(source_plan(path)),1,1)
        ds=gdal.Open(str(result.path));actual=read_band_values(ds.GetRasterBand(1));ds=None
        np.testing.assert_array_equal(actual,expected)
        assert not np.any((actual==1)&(values!=1))
        assert _thin_components(actual==1,8)==_thin_components(values==1,8)
        assert _thin_components(np.pad(actual==0,1,constant_values=True),4)==_thin_components(np.pad(values==0,1,constant_values=True),4)


@pytest.mark.parametrize('shape',[(1,1),(1,517),(515,1),(515,519)])
def test_raster_thin_cross_block_global_snapshots_and_cog(tmp_path,shape,monkeypatch):
    import operators.raster_compute as compute
    values=np.zeros(shape);height,width=shape
    # Thick crossing over the 510-core and 512-source boundaries, plus a ring.
    values[max(0,height//2-3):height//2+4,:]=7
    values[:,max(0,width//2-3):width//2+4]=7
    if min(shape)>30:values[498:515,498:519]=7;values[502:509,502:512]=0
    valid=np.ones(shape,bool);valid[height//2,width//3]=False;values[~valid]=-9999
    expected=_thin_oracle(values,valid,7)
    transform=(100,2,.5,200,.25,-3)
    path=create_raster(tmp_path/'source.tif',values,transform,'EPSG:3857');before=path.read_bytes()
    blocks=compute._blocks;reads=[]
    def bounded(dataset,indices,**kwargs):
        for x,y,arrays,masks in blocks(dataset,indices,**kwargs):
            assert all(max(a.shape)<=512 for a in arrays)
            reads.append(arrays[0].shape);yield x,y,arrays,masks
    monkeypatch.setattr(compute,'_blocks',bounded)
    with raster_workspace():
        source=raster_load(source_plan(path));prior=set(source.workspace.iterdir())
        result=compute.raster_thin(source,1,7)
        assert set(source.workspace.iterdir())==prior|{result.path}
        ds=gdal.Open(str(result.path));band=ds.GetRasterBand(1)
        np.testing.assert_array_equal(read_band_values(band),expected)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,valid)
        assert band.DataType==gdal.GDT_Float64 and math.isnan(band.GetNoDataValue())
        assert band.GetScale() is None and band.GetOffset() is None and band.GetColorTable() is None
        assert ds.RasterCount==1 and ds.GetGeoTransform()==transform
        assert ds.GetSpatialRef().GetAuthorityCode(None)=='3857';band=ds=None
        if min(shape)>30:
            target=tmp_path/'thin.cog.tif';artifact=raster_save(result,target_plan(target),profile='cog',blocksize=128)
            assert artifact['size_bytes']==target.stat().st_size>0
            assert validate_cog(raster_load(source_plan(target)))['valid']
            ds=gdal.Open(str(target));band=ds.GetRasterBand(1)
            np.testing.assert_array_equal(read_band_values(band),expected)
            np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,valid)
            assert ds.GetGeoTransform()==transform and band.GetOverviewCount()>0;band=ds=None
    assert reads and path.read_bytes()==before


@pytest.mark.parametrize('foreground',[0,-7,2**53-1,-(2**53-1)])
def test_raster_thin_explicit_class_native_precision_and_other_classes(tmp_path,foreground):
    import operators.raster_compute as compute
    values=np.full((13,17),42,dtype=np.int64);values[2:11,3:14]=foreground;values[5:8,6:11]=42
    path=tmp_path/'classes.tif';ds=gdal.GetDriverByName('GTiff').Create(str(path),17,13,1,gdal.GDT_Int64)
    ds.GetRasterBand(1).WriteArray(values);ds=None
    with raster_workspace():
        result=compute.raster_thin(raster_load(source_plan(path)),1,foreground)
        ds=gdal.Open(str(result.path));np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),_thin_oracle(values,np.ones(values.shape,bool),foreground));ds=None


@pytest.mark.parametrize('position',['none','crs-only','transform-only'])
@pytest.mark.parametrize('empty',['no-hit','all-invalid'])
def test_raster_thin_empty_and_missing_location(tmp_path,position,empty):
    import operators.raster_compute as compute
    values=np.full((4,6),-9999. if empty=='all-invalid' else 9.)
    path=create_raster(tmp_path/'empty.tif',values,None if position!='transform-only' else (0,2,.5,0,.2,-3),'EPSG:4326' if position=='crs-only' else '')
    with raster_workspace():
        source=raster_load(source_plan(path));result=compute.raster_thin(source,1,0)
        for key in ('width','height','crs','transform','extent'):assert raster_info(result)[key]==raster_info(source)[key]
        ds=gdal.Open(str(result.path));actual=read_band_values(ds.GetRasterBand(1));ds=None
        assert np.isnan(actual).all() if empty=='all-invalid' else (actual==0).all()


def test_raster_thin_alpha_mask_nonfinite_selected_band_and_raw_storage(tmp_path):
    import operators.raster_compute as compute
    values=np.ones((13,17));values[0,0]=.5;values[0,1]=2**53;values[0,2]=np.inf;values[0,3]=np.nan;values[4:7,7:10]=-9999
    alpha=np.full(values.shape,255.);alpha[0,1]=0;alpha[0,2]=-1;alpha[0,3]=np.nan;alpha[2,2]=128
    mask=np.full(values.shape,255,np.uint8);mask[0,0]=0;mask[8,8]=0
    path=create_raster(tmp_path/'source.tif',np.stack([np.full(values.shape,.25),values,alpha]))
    ds=gdal.Open(str(path),gdal.GA_Update);ds.GetRasterBand(2).SetScale(100);ds.GetRasterBand(2).SetOffset(12)
    ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
    ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);ds=None
    valid=np.isfinite(values)&(values!=-9999)&np.isfinite(alpha)&(alpha>0)&(mask>0)
    with raster_workspace():
        result=compute.raster_thin(raster_load(source_plan(path)),2,1)
        ds=gdal.Open(str(result.path));np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),_thin_oracle(values,valid,1));ds=None


@pytest.mark.parametrize('language,prefix',[('zh-cn','栅格细化'),('en','Raster thinning')])
@pytest.mark.parametrize('reason',['band','alpha','complex','classes','foreground'])
def test_raster_thin_bilingual_validation_and_no_output(tmp_path,language,prefix,reason):
    import operators.raster_compute as compute
    path=tmp_path/'bad.tif';ds=gdal.GetDriverByName('GTiff').Create(str(path),3,3,1,gdal.GDT_CFloat64 if reason=='complex' else gdal.GDT_Float64)
    ds.GetRasterBand(1).Fill(.5 if reason=='classes' else 1)
    if reason=='alpha':ds.GetRasterBand(1).SetColorInterpretation(gdal.GCI_AlphaBand)
    ds=None
    with raster_error_language(language),raster_workspace():
        source=raster_load(source_plan(path));before=set(source.workspace.iterdir())
        with pytest.raises(ValueError,match='^'+prefix):compute.raster_thin(source,0 if reason=='band' else 1,True if reason=='foreground' else 1)
        assert set(source.workspace.iterdir())==before


@pytest.mark.parametrize('value',[True,None,[],{},'1',.5,float('inf'),float('-inf'),float('nan'),2**53,-2**53])
def test_raster_thin_rejects_foreground_parameters(tmp_path,value):
    import operators.raster_compute as compute
    path=create_raster(tmp_path/'source.tif',np.ones((2,2)))
    with raster_error_language('en'),raster_workspace(),pytest.raises(ValueError,match='foreground classification'):
        compute.raster_thin(raster_load(source_plan(path)),1,value)


@pytest.mark.parametrize('dtype,value,nodata',[(gdal.GDT_Int64,2**53+1,2**53),(gdal.GDT_Int64,-2**53-1,-2**53),(gdal.GDT_UInt64,2**64-1,2**64-2)])
def test_raster_thin_native_class_cannot_hide_behind_rounded_nodata(tmp_path,dtype,value,nodata):
    import operators.raster_compute as compute
    path=tmp_path/'native.tif';ds=gdal.GetDriverByName('GTiff').Create(str(path),2,2,1,dtype)
    ds.GetRasterBand(1).SetNoDataValue(nodata)
    ds.GetRasterBand(1).WriteArray(np.array([[nodata,value],[0,0]],dtype=np.uint64 if dtype==gdal.GDT_UInt64 else np.int64));ds=None
    with raster_error_language('en'),raster_workspace(),pytest.raises(ValueError,match='exact safe integers'):
        compute.raster_thin(raster_load(source_plan(path)),1,0)


@pytest.mark.parametrize('stage',['seed','phase0','phase1','output'])
def test_raster_thin_failure_removes_all_private_snapshots(tmp_path,monkeypatch,stage):
    import operators.raster_compute as compute
    path=create_raster(tmp_path/'source.tif',np.ones((7,9)))
    blocks=compute._blocks;subiteration=compute._thin_subiteration;native_calls=0
    def fail_blocks(dataset,indices,**kwargs):
        nonlocal native_calls
        if kwargs.get('native'):native_calls+=1
        for block in blocks(dataset,indices,**kwargs):
            yield block
            if stage=='seed' and kwargs.get('native') and native_calls==2:raise RuntimeError('injected thinning failure')
            if stage=='output' and dataset.GetDescription()==str(source.path) and not kwargs.get('native'):raise RuntimeError('injected thinning failure')
    def fail_phase(values,phase):
        if stage=='phase'+str(phase):raise RuntimeError('injected thinning failure')
        return subiteration(values,phase)
    monkeypatch.setattr(compute,'_blocks',fail_blocks);monkeypatch.setattr(compute,'_thin_subiteration',fail_phase)
    with raster_workspace():
        source=raster_load(source_plan(path));before=set(source.workspace.iterdir())
        with pytest.raises(RuntimeError,match='injected thinning failure'):compute.raster_thin(source,1,1)
        assert set(source.workspace.iterdir())==before


@pytest.mark.parametrize('case',['direct','reclassified','invalid'])
def test_raster_thin_async_save_reclassification_and_cleanup(tmp_path,monkeypatch,case):
    import api_server,workflow_engine
    values=np.zeros((9,11));values[1:8,2:9]=8;values[3:6,4:7]=0;values[0,0]=-9999
    if case=='invalid':values[0,1]=.5
    path=create_raster(tmp_path/'source.tif',values,(0,2,.5,10,.25,-3),'EPSG:4326');before=path.read_bytes();target=tmp_path/'thin.cog.tif'
    tasks=[{'id':'source','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(path)}}];source='source';foreground=8;classified=values.copy()
    if case=='reclassified':
        tasks.append({'id':'classes','operator':'raster_reclassify','depends_on':['source'],'params':{'input_raster':{'$ref':'source'},'band':1,'rules':[{'value':0,'class':-7},{'value':8,'class':0}]}})
        source='classes';foreground=0;classified=np.select([values==0,values==8],[-7.,0.],default=np.nan)
    tasks += [{'id':'thin','operator':'raster_thin','depends_on':[source],'params':{'input_raster':{'$ref':source},'band':1,'foreground_value':foreground}},
              {'id':'save','operator':'raster_save','depends_on':['thin'],'params':{'input_raster':{'$ref':'thin'},'access_plan':target_plan(target),'profile':'cog','blocksize':128}}]
    paths=[];execute=workflow_engine._execute_operator
    def capture(name,params):
        if name=='raster_thin':paths.append(params['input_raster'].workspace)
        return execute(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',capture)
    client=api_server.app.test_client();response=client.post('/api/workflow',json={'workflow_def':{'tasks':tasks},'input_data':{},'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert response.status_code==202,response.json
    deadline=time.monotonic()+10
    while time.monotonic()<deadline:
        status=client.get('/api/executions/'+response.json['execution_id']).json
        if status['status'] in ('success','failed'):break
        time.sleep(.01)
    assert status['status']==('failed' if case=='invalid' else 'success'),status
    assert paths and all(not p.exists() for p in paths) and path.read_bytes()==before
    if case=='invalid':assert not target.exists()
    else:
        with raster_workspace():assert validate_cog(raster_load(source_plan(target)))['valid']
        ds=gdal.Open(str(target));np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),_thin_oracle(classified,np.isfinite(classified)&(classified!=-9999),foreground));ds=None
        assert json.loads(status['result'])['size_bytes']==target.stat().st_size>0
    assert client.post('/api/operators/raster_thin/invoke',json={'params':{}}).status_code==403


def test_raster_thin_discovery_and_single_workflow_contract():
    spec=next(item for item in list_operators() if item['id']=='raster_thin')
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read']
    assert spec['attributes']['resource_groups']==['raster'] and spec['category_path']==['栅格处理','像元计算']
    assert [p['name'] for p in spec['parameters']]==['input_raster','band','foreground_value']
    assert all(p['required'] for p in spec['parameters'])
    assert spec['detailed_description']['workflow_example']['params']['foreground_value']==1


@pytest.mark.parametrize('band',[True,0,-1,1.5,'1',3])
def test_raster_thin_invalid_band_types_and_range(tmp_path,band):
    import operators.raster_compute as compute
    path=create_raster(tmp_path/'source.tif',np.ones((2,2)))
    with raster_error_language('en'),raster_workspace(),pytest.raises(ValueError,match='one-based data band'):
        compute.raster_thin(raster_load(source_plan(path)),band,1)


def test_raster_thin_complex_alpha_rejected_even_when_data_is_not_selected_alpha(tmp_path):
    import operators.raster_compute as compute
    path=tmp_path/'alpha.tif';ds=gdal.GetDriverByName('GTiff').Create(str(path),3,3,2,gdal.GDT_CFloat64)
    for index in (1,2):ds.GetRasterBand(index).Fill(1)
    ds.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand);ds=None
    with raster_error_language('en'),raster_workspace(),pytest.raises(ValueError,match='complex'):
        compute.raster_thin(raster_load(source_plan(path)),1,1)


@pytest.mark.parametrize('dtype',[gdal.GDT_Int64,gdal.GDT_UInt64])
def test_raster_thin_native_nodata_and_mask_excluded_before_safe_validation(tmp_path,dtype):
    import operators.raster_compute as compute
    values=np.full((7,9),2**53-1,dtype=np.int64 if dtype==gdal.GDT_Int64 else np.uint64)
    values[0,0]=2**53;values[2,3]=2**53+1
    mask=np.full(values.shape,255,np.uint8);mask[2,3]=0
    path=tmp_path/'native.tif';ds=gdal.GetDriverByName('GTiff').Create(str(path),9,7,1,dtype)
    ds.GetRasterBand(1).SetNoDataValue(2**53);ds.GetRasterBand(1).WriteArray(values)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
    ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);ds=None
    valid=(values!=2**53)&(mask!=0)
    with raster_workspace():
        result=compute.raster_thin(raster_load(source_plan(path)),1,2**53-1)
        ds=gdal.Open(str(result.path));np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),_thin_oracle(values,valid,2**53-1));ds=None


def _fill_level_reachability_oracle(values):
    """Independent level-set reachability: no production heap, stencil or flood helper."""
    from collections import deque
    values = np.asarray(values, dtype=np.float64)
    height, width = values.shape
    valid = np.isfinite(values)
    result = np.full(values.shape, np.nan)
    exits = set()
    for row, col in zip(*np.where(valid)):
        if row in (0, height-1) or col in (0, width-1):
            exits.add((row, col))
        elif any(not valid[y, x] for y in range(row-1, row+2) for x in range(col-1, col+2)):
            exits.add((row, col))
    for level in sorted(set(values[valid])):
        allowed = valid & (values <= level)
        reached = {point for point in exits if allowed[point]}
        queue = deque(reached)
        while queue:
            row, col = queue.popleft()
            for y in range(max(0, row-1), min(height, row+2)):
                for x in range(max(0, col-1), min(width, col+2)):
                    if allowed[y, x] and (y, x) not in reached:
                        reached.add((y, x)); queue.append((y, x))
        for point in reached:
            if np.isnan(result[point]):
                result[point] = level
    return result


@pytest.mark.parametrize('seed', range(15))
def test_dem_fill_depressions_independent_minimax_oracle(tmp_path, seed):
    import operators.raster_compute as compute
    rng = np.random.default_rng(seed)
    values = rng.integers(-5, 16, size=(9, 11)).astype(float)
    values[rng.random(values.shape) < .09] = np.nan
    path = create_raster(tmp_path/'dem.tif', values, (0, 2, 0, 100, 0, -3), 'EPSG:3857')
    before = path.read_bytes()
    expected = _fill_level_reachability_oracle(values)
    with raster_workspace():
        source = raster_load(source_plan(path)); prior = set(source.workspace.iterdir())
        result = compute.raster_fill_depressions(source, 1, 'metre')
        assert set(source.workspace.iterdir()) == prior | {result.path}
        ds = gdal.Open(str(result.path)); band = ds.GetRasterBand(1)
        actual = read_band_values(band)
        np.testing.assert_array_equal(actual, expected)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray() != 0, np.isfinite(values))
        assert np.all(actual[np.isfinite(values)] >= values[np.isfinite(values)])
        assert band.DataType == gdal.GDT_Float64 and math.isnan(band.GetNoDataValue())
        assert band.GetUnitType() == 'metre'
        assert band.GetScale() is None and band.GetOffset() is None and band.GetColorTable() is None
        assert ds.RasterCount == 1 and ds.GetGeoTransform() == (0, 2, 0, 100, 0, -3)
        assert ds.GetSpatialRef().GetAuthorityCode(None) == '3857'
        band = ds = None
        second = compute.raster_fill_depressions(result, 1, 'metre')
        ds = gdal.Open(str(second.path)); np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)), expected); ds = None
    assert path.read_bytes() == before


@pytest.mark.parametrize('case', ['bowl', 'diagonal', 'hole', 'nested', 'multiple-exits', 'flat', 'all-invalid', 'row', 'column', 'single'])
def test_dem_fill_depressions_outlets_and_degenerate_grids(tmp_path, case):
    import operators.raster_compute as compute
    values = np.full((7, 7), 10.); values[1:-1, 1:-1] = 1.
    expected = np.full(values.shape, 10.)
    if case == 'diagonal':
        values[0, 0] = 4.; expected[1:-1, 1:-1] = 4.; expected[0, 0] = 4.
    if case == 'hole':
        values[3, 3] = np.nan; expected = values.copy()
    if case == 'nested':
        values[2:5, 2:5] = 15.; values[3, 3] = -2.; expected[2:5, 2:5] = 15.
    if case == 'multiple-exits':
        values[0, 2] = 8.; values[-1, 2] = 3.; expected[1:-1, 1:-1] = 3.; expected[0, 2] = 8.; expected[-1, 2] = 3.
    if case == 'flat': values[:] = 0.; expected[:] = 0.
    if case == 'all-invalid': values[:] = np.nan; expected[:] = np.nan
    if case in ('row', 'column', 'single'):
        shape = {'row': (1, 7), 'column': (7, 1), 'single': (1, 1)}[case]
        values = np.arange(np.prod(shape), dtype=float).reshape(shape)-2; expected = values.copy()
    path = create_raster(tmp_path/'dem.tif', values, (0, 1, 0, 10, 0, -1), 'EPSG:3857')
    with raster_workspace():
        result = compute.raster_fill_depressions(raster_load(source_plan(path)), 1, 'metre')
        ds = gdal.Open(str(result.path)); np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)), expected); ds = None


@pytest.mark.parametrize('unit,factor,crs', [('metre', 1., 'EPSG:3857'), ('foot', .3048, 'EPSG:2277'), ('us_survey_foot', 1200/3937, 'EPSG:3857')])
def test_dem_fill_depressions_units_selected_band_alpha_mask_and_cog(tmp_path, unit, factor, crs):
    import operators.raster_compute as compute
    values = np.full((17, 19), 10.); values[1:-1, 1:-1] = -2.; values[0, 0] = 0.
    values[8, 9] = -9999.; values[4, 5] = np.inf; values[6, 7] = np.nan
    alpha = np.full(values.shape, 255.); alpha[5, 6] = 0; alpha[2, 3] = 128; alpha[7, 2] = np.nan
    mask = np.full(values.shape, 255, np.uint8); mask[2, 9] = 0
    path = create_raster(tmp_path/'dem.tif', np.stack([np.full(values.shape, 123.), values, alpha]), (100, 2, 0, 200, 0, -3), crs)
    ds = gdal.Open(str(path), gdal.GA_Update); ds.GetRasterBand(2).SetUnitType(unit); ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK', 'YES'): ds.CreateMaskBand(gdal.GMF_PER_DATASET)
    ds.GetRasterBand(1).GetMaskBand().WriteArray(mask); ds = None
    valid = np.isfinite(values) & (values != -9999) & np.isfinite(alpha) & (alpha > 0) & (mask > 0)
    expected = _fill_level_reachability_oracle(np.where(valid, values*factor, np.nan))
    before = path.read_bytes(); target = tmp_path/'filled.cog.tif'
    with raster_workspace():
        result = compute.raster_fill_depressions(raster_load(source_plan(path)), 2, unit)
        artifact = raster_save(result, target_plan(target), profile='cog', blocksize=128)
        assert type(artifact['size_bytes']) is int and artifact['size_bytes'] == target.stat().st_size > 0
        assert validate_cog(raster_load(source_plan(target)))['valid']
        ds = gdal.Open(str(target)); band = ds.GetRasterBand(1)
        np.testing.assert_array_equal(read_band_values(band), expected)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray() != 0, valid)
        assert ds.GetGeoTransform() == (100, 2, 0, 200, 0, -3) and ds.GetSpatialRef().IsSame(osr.SpatialReference(wkt=gdal.Open(str(path)).GetProjection()))
        assert band.GetUnitType() == 'metre' and ds.RasterCount == 1
        band = ds = None
    assert path.read_bytes() == before


def test_dem_fill_depressions_cross_block_and_bounded_io(tmp_path, monkeypatch):
    import operators.raster_compute as compute
    values = np.full((515, 519), -2.); values[[0, -1], :] = 100.; values[:, [0, -1]] = 100.; values[0, 1] = 50.
    expected = np.maximum(values, 50.)
    path = create_raster(tmp_path/'large.tif', values, (0, 2, 0, 2000, 0, -3), 'EPSG:3857')
    blocks = compute._blocks; reads = []
    def bounded(*args, **kwargs):
        for x, y, arrays, masks in blocks(*args, **kwargs):
            assert all(max(array.shape) <= 512 for array in arrays)
            reads.append((x, y)); yield x, y, arrays, masks
    monkeypatch.setattr(compute, '_blocks', bounded)
    with raster_workspace():
        result = compute.raster_fill_depressions(raster_load(source_plan(path)), 1, 'metre')
        ds = gdal.Open(str(result.path)); np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)), expected); ds = None
    assert any(x >= 512 and y >= 512 for x, y in reads)


@pytest.mark.parametrize('language,prefix', [('zh-cn', 'DEM 填洼'), ('en', 'DEM depression filling')])
@pytest.mark.parametrize('operation', ['raster_fill_depressions', 'raster_flow_direction_d8'])
@pytest.mark.parametrize('fault', ['geographic', 'rotated', 'south-up', 'missing-crs', 'missing-grid', 'unit', 'unit-conflict', 'unit-unknown', 'scale', 'offset', 'alpha', 'band', 'complex', 'complex-alpha'])
def test_dem_fill_depressions_bilingual_invalid_input_without_private_output(tmp_path, language, prefix, fault, operation):
    import operators.raster_compute as compute
    if operation == 'raster_flow_direction_d8':
        prefix = 'DEM D8 流向' if language == 'zh-cn' else 'DEM D8 flow direction'
    path = tmp_path/'bad.tif'; dtype = gdal.GDT_CFloat64 if fault in ('complex', 'complex-alpha') else gdal.GDT_Float64
    ds = gdal.GetDriverByName('GTiff').Create(str(path), 5, 5, 2 if fault == 'complex-alpha' else 1, dtype)
    if fault != 'missing-grid': ds.SetGeoTransform((0, 1, .2 if fault == 'rotated' else 0, 5, 0, 1 if fault == 'south-up' else -1))
    if fault != 'missing-crs':
        crs = osr.SpatialReference(); crs.ImportFromEPSG(4326 if fault == 'geographic' else 3857); ds.SetProjection(crs.ExportToWkt())
    band = ds.GetRasterBand(1); band.Fill(1)
    if fault in ('unit-conflict', 'unit-unknown'): band.SetUnitType('foot' if fault == 'unit-conflict' else 'fathom')
    if fault == 'scale': band.SetScale(2)
    if fault == 'offset': band.SetOffset(10)
    if fault == 'alpha': band.SetColorInterpretation(gdal.GCI_AlphaBand)
    if fault == 'complex-alpha': ds.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand)
    band = ds = None
    with raster_workspace(), raster_error_language(language):
        source = raster_load(source_plan(path)); prior = set(source.workspace.iterdir())
        with pytest.raises(ValueError, match='^'+prefix):
            getattr(compute, operation)(source, True if fault == 'band' else 1, 'bad' if fault == 'unit' else 'metre')
        assert set(source.workspace.iterdir()) == prior


@pytest.mark.parametrize('band', [True, False, 0, -1, 2, 1.5, '1', None])
def test_dem_fill_depressions_requires_explicit_valid_band(tmp_path, band):
    import operators.raster_compute as compute
    path = create_raster(tmp_path/'dem.tif', np.ones((3, 3)), (0, 1, 0, 5, 0, -1), 'EPSG:3857')
    with raster_workspace(), pytest.raises(ValueError): compute.raster_fill_depressions(raster_load(source_plan(path)), band, 'metre')


@pytest.mark.parametrize('stage', ['normalize', 'flood', 'write'])
def test_dem_fill_depressions_failure_cleans_normalized_and_output(tmp_path, monkeypatch, stage):
    import operators.raster_compute as compute
    path = create_raster(tmp_path/'dem.tif', np.ones((5, 5)), (0, 1, 0, 5, 0, -1), 'EPSG:3857')
    blocks = compute._blocks; flood = compute._priority_flood; calls = 0
    def failing_blocks(*args, **kwargs):
        nonlocal calls
        calls += 1
        for item in blocks(*args, **kwargs):
            if stage == 'normalize' and calls == 1: raise RuntimeError('injected fill failure')
            yield item
    def failing_flood(values):
        if stage == 'flood': raise RuntimeError('injected fill failure')
        flood(values)
        if stage == 'write':
            # At this point normalization has completed; fail the output writer.
            monkeypatch.setattr(gdal.Band, 'WriteArray', lambda *a, **kw: (_ for _ in ()).throw(RuntimeError('injected fill failure')))
    monkeypatch.setattr(compute, '_blocks', failing_blocks); monkeypatch.setattr(compute, '_priority_flood', failing_flood)
    with raster_workspace():
        source = raster_load(source_plan(path)); prior = set(source.workspace.iterdir())
        with pytest.raises(RuntimeError, match='injected fill failure'): compute.raster_fill_depressions(source, 1, 'metre')
        assert set(source.workspace.iterdir()) == prior


@pytest.mark.parametrize('invalid', [False, True])
def test_dem_fill_depressions_async_cog_existing_ports_and_cleanup(tmp_path, monkeypatch, invalid):
    import api_server, workflow_engine
    values = np.full((9, 11), 10.); values[1:-1, 1:-1] = 0.; values[0, 0] = np.nan
    path = create_raster(tmp_path/'dem.tif', values, (0, 2, 0, 100, 0, -3), 'EPSG:3857'); before = path.read_bytes(); target = tmp_path/'filled.cog.tif'
    tasks = [
        {'id': 'source', 'operator': 'raster_load', 'depends_on': [], 'params': {'access_plan': source_plan(path)}},
        {'id': 'fill', 'operator': 'raster_fill_depressions', 'depends_on': ['source'], 'params': {'input_raster': {'$ref': 'source'}, 'band': 99 if invalid else 1, 'elevation_unit': 'metre'}},
        {'id': 'save', 'operator': 'raster_save', 'depends_on': ['fill'], 'params': {'input_raster': {'$ref': 'fill'}, 'access_plan': target_plan(target), 'profile': 'cog', 'blocksize': 128}},
    ]
    paths = []; execute = workflow_engine._execute_operator
    def capture(name, params):
        if name == 'raster_fill_depressions': paths.append(params['input_raster'].workspace)
        return execute(name, params)
    monkeypatch.setattr(workflow_engine, '_execute_operator', capture)
    client = api_server.app.test_client()
    response = client.post('/api/workflow', json={'workflow_def': {'tasks': tasks}, 'input_data': {}, 'runtime': {'tenant_id': 7, 'execution_authorization': {'id': 1, 'effects': ['read', 'write']}}})
    assert response.status_code == 202, response.json
    deadline = time.monotonic()+10
    while time.monotonic() < deadline:
        status = client.get('/api/executions/'+response.json['execution_id']).json
        if status['status'] in ('success', 'failed'): break
        time.sleep(.01)
    assert status['status'] == ('failed' if invalid else 'success'), status
    assert paths and all(not p.exists() for p in paths) and path.read_bytes() == before
    if invalid: assert not target.exists()
    else:
        with raster_workspace(): assert validate_cog(raster_load(source_plan(target)))['valid']
        ds = gdal.Open(str(target)); np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)), _fill_level_reachability_oracle(values)); ds = None
        assert json.loads(status['result'])['size_bytes'] == target.stat().st_size > 0
    assert client.post('/api/operators/raster_fill_depressions/invoke', json={'params': {}}).status_code == 403


def test_dem_fill_depressions_discovery_and_required_parameters():
    spec = next(item for item in list_operators() if item['id'] == 'raster_fill_depressions')
    assert spec['execution_modes'] == ['workflow'] and spec['effects'] == ['read']
    assert spec['attributes']['resource_groups'] == ['raster']
    assert spec['category_path'] == ['栅格处理', '像元计算']
    params = {param['name']: param for param in spec['parameters']}
    assert set(params) == {'input_raster', 'band', 'elevation_unit'}
    assert params['band']['required'] and params['elevation_unit']['required']
    assert params['elevation_unit']['enum'] == ['metre', 'foot', 'us_survey_foot']
    assert spec['detailed_description']['workflow_example']['params'] == {'input_raster': {'$ref': 'source'}, 'band': 1, 'elevation_unit': 'metre'}


def _d8_bellman_ford_oracle(values, xsize=2., ysize=3.):
    """Independent synchronous relaxation, without production heap or neighbours."""
    from fractions import Fraction
    height, width = values.shape
    cells = [tuple(map(int, point)) for point in np.argwhere(np.isfinite(values))]
    codes = [[32, 64, 128], [16, 0, 1], [8, 4, 2]]
    def neighbours(row, col):
        for y in range(max(0, row-1), min(height, row+2)):
            for x in range(max(0, col-1), min(width, col+2)):
                if (y, x) != (row, col) and np.isfinite(values[y, x]):
                    yield (y, x), Fraction(math.hypot((x-col)*xsize, (y-row)*ysize)), codes[y-row+1][x-col+1]
    result = np.full(values.shape, np.nan)
    costs = {}
    for point in cells:
        row, col = point
        lower = [(Fraction(float(values[point]))-Fraction(float(values[p])))/distance for p,distance,_ in neighbours(*point) if values[p]<values[point]]
        if lower:
            best = max(lower)
            result[point] = next(code for p,distance,code in neighbours(*point) if values[p]<values[point] and (Fraction(float(values[point]))-Fraction(float(values[p])))/distance==best)
            costs[point] = 0
        elif row in (0,height-1) or col in (0,width-1) or any(not np.isfinite(values[y,x]) for y in range(row-1,row+2) for x in range(col-1,col+2)):
            costs[point] = 0; result[point] = 0
    seeds = set(costs)
    for _ in cells:
        before = costs.copy()
        for point in cells:
            if point in seeds:continue
            candidates = [(before[p]+distance,p,code) for p,distance,code in neighbours(*point) if values[p]==values[point] and p in before]
            if candidates:
                cost,_,code = min(candidates)
                costs[point] = cost; result[point] = code
        if costs == before:break
    if len(costs)!=len(cells):raise ValueError('closed depression')
    return result


def _assert_d8_paths(values, directions):
    codes = {1:(0,1),2:(1,1),4:(1,0),8:(1,-1),16:(0,-1),32:(-1,-1),64:(-1,0),128:(-1,1)}
    height,width=values.shape
    np.testing.assert_array_equal(np.isfinite(values), np.isfinite(directions))
    for point in map(tuple,np.argwhere(np.isfinite(values))):
        seen=set()
        while True:
            assert point not in seen, 'D8 flow cycle'
            seen.add(point);row,col=point;code=directions[point]
            if code==0:
                assert row in (0,height-1) or col in (0,width-1) or any(not np.isfinite(values[y,x]) for y in range(row-1,row+2) for x in range(col-1,col+2))
                break
            assert code in codes
            dy,dx=codes[code];next_point=(row+dy,col+dx)
            assert 0<=next_point[0]<height and 0<=next_point[1]<width
            assert np.isfinite(values[next_point]) and values[next_point]<=values[point]
            point=next_point


@pytest.mark.parametrize('seed',range(12))
def test_dem_d8_independent_flat_shortest_path_and_global_acyclic_oracle(tmp_path,seed):
    import operators.raster_compute as compute
    rng=np.random.default_rng(seed);raw=rng.integers(-4,12,size=(7,9)).astype(float)
    raw[rng.random(raw.shape)<.07]=np.nan
    values=_fill_level_reachability_oracle(raw)
    expected=_d8_bellman_ford_oracle(values)
    path=create_raster(tmp_path/'dem.tif',values,(500000,2,0,3000000,0,-3),'EPSG:3857');before=path.read_bytes()
    with raster_workspace():
        source=raster_load(source_plan(path));prior=set(source.workspace.iterdir())
        result=compute.raster_flow_direction_d8(source,1,'metre');ds=gdal.Open(str(result.path));band=ds.GetRasterBand(1)
        actual=read_band_values(band);np.testing.assert_array_equal(actual,expected);_assert_d8_paths(values,actual)
        assert band.DataType==gdal.GDT_Float64 and math.isnan(band.GetNoDataValue()) and band.GetUnitType()==''
        assert band.GetScale() is None and band.GetOffset() is None and band.GetColorTable() is None
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(values))
        assert ds.GetGeoTransform()==(500000.,2.,0.,3000000.,0.,-3.) and osr.SpatialReference(wkt=ds.GetProjection()).IsProjected()
        assert set(source.workspace.iterdir())==prior|{result.path};band=ds=None
    assert path.read_bytes()==before


@pytest.mark.parametrize('dy,dx,code',[(-1,-1,32),(-1,0,64),(-1,1,128),(0,-1,16),(0,1,1),(1,-1,8),(1,0,4),(1,1,2)])
def test_dem_d8_eight_direction_codes_and_edge_downhill_precedence(tmp_path,dy,dx,code):
    import operators.raster_compute as compute
    values=np.full((3,3),20.);values[1,1]=10;values[1+dy,1+dx]=9
    path=create_raster(tmp_path/'dem.tif',values,(0,2,0,100,0,-3),'EPSG:3857')
    with raster_workspace():
        result=compute.raster_flow_direction_d8(raster_load(source_plan(path)),1,'metre');ds=gdal.Open(str(result.path));directions=read_band_values(ds.GetRasterBand(1));ds=None
        assert directions[1,1]==code
        np.testing.assert_array_equal(directions,_d8_bellman_ford_oracle(values));_assert_d8_paths(values,directions)
        assert np.any(directions[0,:]>0)  # Higher edge cells drain toward lower valid cells.


@pytest.mark.parametrize('ysize,expected',[(1.,64),(10.,1)])
def test_dem_d8_flat_routes_use_physical_distance_not_step_count(ysize,expected):
    import operators.raster_compute as compute
    values=np.full((9,13),100.);values[1:8,1:12]=10.;values[0,6]=10.;values[3,-1]=10.
    frozen=values.copy();values.flags.writeable=False
    actual=compute._d8_directions(values,1.,ysize)
    assert actual[3,6]==expected
    np.testing.assert_array_equal(actual,_d8_bellman_ford_oracle(values,1.,ysize));_assert_d8_paths(values,actual)
    np.testing.assert_array_equal(values,frozen)


@pytest.mark.parametrize('values,xsize,ysize,code',[
    (np.array([[4.,4.,4.],[4.,5.,4.],[4.,4.,4.]]),1.,1.,64),
    (np.array([[4.,4.,4.],[4.,5.,4.],[4.,4.,4.]]),2.,3.,16),
    (np.array([[0.,0.,0.],[-1e308,1e308,0.],[0.,0.,0.]]),1.,1.,16),
    (np.array([[1e-300,1e-300,1e-300],[0.,1e-300,1e-300],[1e-300,1e-300,1e-300]]),1e200,1e200,16),
    (np.array([[2.,2.,2.],[1.,np.nextafter(1.,2.),2.],[2.,2.,2.]]),1.,1.,16),
])
def test_dem_d8_exact_slope_ties_extremes_and_no_flat_tolerance(values,xsize,ysize,code):
    import operators.raster_compute as compute
    actual=compute._d8_directions(values,xsize,ysize)
    assert actual[1,1]==code
    np.testing.assert_array_equal(actual,_d8_bellman_ford_oracle(values,xsize,ysize));_assert_d8_paths(values,actual)


@pytest.mark.parametrize('shape,empty', [((1,1),False),((1,7),False),((7,1),False),((6,8),False),((6,8),True)])
def test_dem_d8_all_flat_single_row_column_and_all_invalid(tmp_path,shape,empty):
    import operators.raster_compute as compute
    values=np.full(shape,np.nan if empty else 0.)
    path=create_raster(tmp_path/'dem.tif',values,(0,2,0,100,0,-3),'EPSG:3857')
    with raster_workspace():
        result=compute.raster_flow_direction_d8(raster_load(source_plan(path)),1,'metre');ds=gdal.Open(str(result.path));actual=read_band_values(ds.GetRasterBand(1))
        np.testing.assert_array_equal(ds.GetRasterBand(1).GetMaskBand().ReadAsArray()!=0,np.isfinite(values));ds=None
        np.testing.assert_array_equal(actual,_d8_bellman_ford_oracle(values));_assert_d8_paths(values,actual)
        if not empty:assert np.count_nonzero(actual==0)>0


@pytest.mark.parametrize('language,diagnostic',[('zh-cn','封闭洼地'),('en','closed depression')])
def test_dem_d8_closed_depression_fails_without_changing_dem_or_leaking_workspace(tmp_path,language,diagnostic):
    import operators.raster_compute as compute
    values=np.full((7,9),10.);values[1:-1,1:-1]=-2.
    path=create_raster(tmp_path/'dem.tif',values,(0,2,0,100,0,-3),'EPSG:3857');before=path.read_bytes()
    with raster_workspace(),raster_error_language(language):
        source=raster_load(source_plan(path));prior=set(source.workspace.iterdir())
        with pytest.raises(ValueError,match=diagnostic):compute.raster_flow_direction_d8(source,1,'metre')
        assert set(source.workspace.iterdir())==prior
        filled=compute.raster_fill_depressions(source,1,'metre');result=compute.raster_flow_direction_d8(filled,1,'metre')
        ds=gdal.Open(str(result.path));directions=read_band_values(ds.GetRasterBand(1));ds=None
        np.testing.assert_array_equal(directions,_d8_bellman_ford_oracle(np.full(values.shape,10.)))
        _assert_d8_paths(np.full(values.shape,10.),directions)
    assert path.read_bytes()==before


@pytest.mark.parametrize('unit,factor',[('metre',1.),('foot',.3048),('us_survey_foot',1200/3937)])
def test_dem_d8_selected_band_units_alpha_mask_nodata_and_zero(tmp_path,unit,factor):
    import operators.raster_compute as compute
    path=tmp_path/'bands.tif';values=np.tile(np.arange(9,dtype=float),(7,1))-3;values[1,1]=-9999;values[2,2]=np.inf;values[3,3]=np.nan
    alpha=np.full(values.shape,255.);alpha[1,3]=0;alpha[1,4]=np.nan;alpha[0,3]=128
    mask=np.full(values.shape,255,np.uint8);mask[4,4]=0
    ds=gdal.GetDriverByName('GTiff').Create(str(path),9,7,3,gdal.GDT_Float64);ds.SetGeoTransform((0,2,0,100,0,-3));crs=osr.SpatialReference();crs.ImportFromEPSG(3857);ds.SetProjection(crs.ExportToWkt())
    for index,array in enumerate((np.full(values.shape,np.nan),values,alpha),1):ds.GetRasterBand(index).WriteArray(array);ds.GetRasterBand(index).SetNoDataValue(-9999)
    ds.GetRasterBand(2).SetUnitType(unit);ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
    ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);ds=None;before=path.read_bytes()
    valid=np.isfinite(values)&(values!=-9999)&np.isfinite(alpha)&(alpha>0)&(mask!=0);normalized=np.where(valid,values*factor,np.nan)
    with raster_workspace():
        result=compute.raster_flow_direction_d8(raster_load(source_plan(path)),2,unit);ds=gdal.Open(str(result.path));actual=read_band_values(ds.GetRasterBand(1))
        np.testing.assert_array_equal(actual,_d8_bellman_ford_oracle(normalized));_assert_d8_paths(normalized,actual)
        assert np.isfinite(actual[0,3])  # Valid zero elevation with partial alpha survives.
        np.testing.assert_array_equal(ds.GetRasterBand(1).GetMaskBand().ReadAsArray()!=0,valid);ds=None
    assert path.read_bytes()==before


@pytest.mark.parametrize('band',[True,False,0,-1,2,1.5,'1',None])
def test_dem_d8_explicit_band_types_and_range(tmp_path,band):
    import operators.raster_compute as compute
    path=create_raster(tmp_path/'dem.tif',np.ones((3,3)),(0,1,0,100,0,-1),'EPSG:3857')
    with raster_workspace(),raster_error_language('en'),pytest.raises(ValueError,match='one-based elevation band'):
        compute.raster_flow_direction_d8(raster_load(source_plan(path)),band,'metre')


@pytest.mark.parametrize('stage',['normalized-read','routing','write','metadata'])
def test_dem_d8_injected_failure_removes_all_private_outputs(tmp_path,monkeypatch,stage):
    import operators.raster_compute as compute
    path=create_raster(tmp_path/'dem.tif',np.zeros((4,5)),(0,2,0,100,0,-3),'EPSG:3857')
    with raster_workspace():
        source=raster_load(source_plan(path));prior=set(source.workspace.iterdir());blocks=compute._blocks;route=compute._d8_directions
        def fail_blocks(dataset,indices,**kwargs):
            if stage=='normalized-read' and dataset.GetDescription()!=str(source.path):raise RuntimeError('injected D8 failure')
            yield from blocks(dataset,indices,**kwargs)
        def fail_route(*args):
            if stage=='routing':raise RuntimeError('injected D8 failure')
            result=route(*args)
            def fail(*a,**kw):raise RuntimeError('injected D8 failure')
            if stage=='write':monkeypatch.setattr(gdal.Band,'WriteArray',fail)
            if stage=='metadata':monkeypatch.setattr(compute,'_raster',fail)
            return result
        monkeypatch.setattr(compute,'_blocks',fail_blocks);monkeypatch.setattr(compute,'_d8_directions',fail_route)
        with pytest.raises(RuntimeError,match='injected D8 failure'):compute.raster_flow_direction_d8(source,1,'metre')
        assert set(source.workspace.iterdir())==prior


def test_dem_d8_cross_block_complete_cog_directions_masks_and_bounded_reads(tmp_path,monkeypatch):
    import operators.raster_compute as compute
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    values=np.full((515,519),10.);values[[0,-1],:]=50.;values[:,[0,-1]]=50.;values[0,1]=10.
    # Closed-form routing to the sole low boundary outlet. Equal-level paths
    # prefer NW while both coordinates can decrease; row 1 then follows W.
    expected=np.full(values.shape,32.);expected[1:-1,1]=64.;expected[1,3:-1]=16.
    codes=[[32,64,128],[16,0,1],[8,4,2]]
    rows,cols=np.indices(values.shape)
    for row,col in np.argwhere((rows==0)|(rows==514)|(cols==0)|(cols==518)):
        choices=[]
        for y in range(max(0,row-1),min(515,row+2)):
            for x in range(max(0,col-1),min(519,col+2)):
                if values[y,x]<values[row,col]:choices.append(((values[row,col]-values[y,x])/math.hypot((x-col)*2,(y-row)*3),-int(y),-int(x),codes[y-row+1][x-col+1]))
        expected[row,col]=max(choices)[3] if choices else 0
    assert expected[512,512]==32 and expected[1,512]==16 and expected[0,1]==0
    path=create_raster(tmp_path/'large.tif',values,(500000,2,0,3000000,0,-3),'EPSG:3857');before=path.read_bytes()
    reads=[];blocks=compute._blocks
    def bounded(*args,**kwargs):
        for x,y,arrays,masks in blocks(*args,**kwargs):
            assert all(max(array.shape)<=512 for array in arrays)
            reads.append((x,y));yield x,y,arrays,masks
    monkeypatch.setattr(compute,'_blocks',bounded);target=tmp_path/'direction.cog.tif'
    with raster_workspace():
        direction=compute.raster_flow_direction_d8(raster_load(source_plan(path)),1,'metre')
        artifact=raster_save(direction,target_plan(target),profile='cog',blocksize=128,overview_resampling='nearest')
        assert type(artifact['size_bytes']) is int and artifact['size_bytes']==target.stat().st_size>0
    ds=gdal.Open(str(target));band=ds.GetRasterBand(1);np.testing.assert_array_equal(read_band_values(band),expected)
    np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray(),np.full(values.shape,255,dtype=np.uint8))
    assert not validate(ds,full_check=True)[1] and band.GetOverviewCount()>0 and band.GetUnitType()==''
    for index in range(band.GetOverviewCount()):assert set(np.unique(band.GetOverview(index).ReadAsArray()))<={0.,1.,2.,4.,8.,16.,32.,64.,128.}
    assert ds.GetGeoTransform()==(500000.,2.,0.,3000000.,0.,-3.)
    band=ds=None;assert path.read_bytes()==before and any(x>=512 and y>=512 for x,y in reads)


@pytest.mark.parametrize('case',['direct','filled','empty','closed'])
def test_dem_d8_real_async_workflow_cog_save_exact_pixels_and_cleanup(tmp_path,monkeypatch,case):
    import api_server,workflow_engine
    values=np.tile(np.arange(11,0,-1,dtype=float),(9,1))
    if case in ('filled','closed'):values[:]=10;values[1:-1,1:-1]=-2
    if case=='empty':values[:]=np.nan
    path=create_raster(tmp_path/'dem.tif',values,(500000,2,0,3000000,0,-3),'EPSG:3857');before=path.read_bytes();target=tmp_path/'d8.cog.tif'
    tasks=[{'id':'source','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(path)}}];parent='source'
    if case=='filled':
        tasks.append({'id':'fill','operator':'raster_fill_depressions','depends_on':['source'],'params':{'input_raster':{'$ref':'source'},'band':1,'elevation_unit':'metre'}});parent='fill'
    tasks += [{'id':'direction','operator':'raster_flow_direction_d8','depends_on':[parent],'params':{'input_raster':{'$ref':parent},'band':1,'elevation_unit':'metre'}},
              {'id':'save','operator':'raster_save','depends_on':['direction'],'params':{'input_raster':{'$ref':'direction'},'access_plan':target_plan(target),'profile':'cog','blocksize':128,'overview_resampling':'nearest'}}]
    paths=[];execute=workflow_engine._execute_operator
    def capture(name,params):
        if name=='raster_flow_direction_d8':paths.append(params['input_raster'].workspace)
        return execute(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',capture)
    client=api_server.app.test_client();response=client.post('/api/workflow',json={'workflow_def':{'tasks':tasks},'input_data':{},'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert response.status_code==202,response.json
    deadline=time.monotonic()+15
    while time.monotonic()<deadline:
        status=client.get('/api/executions/'+response.json['execution_id']).json
        if status['status'] in ('success','failed'):break
        time.sleep(.01)
    assert status['status']==('failed' if case=='closed' else 'success'),status
    assert paths and all(not p.exists() for p in paths) and path.read_bytes()==before
    if case=='closed':
        assert not target.exists() and '任务 direction 执行失败' in status['error'] and '封闭洼地' in status['error']
    else:
        normalized=_fill_level_reachability_oracle(values) if case=='filled' else values
        ds=gdal.Open(str(target));actual=read_band_values(ds.GetRasterBand(1));ds=None
        np.testing.assert_array_equal(actual,_d8_bellman_ford_oracle(normalized));_assert_d8_paths(normalized,actual)
        with raster_workspace():assert validate_cog(raster_load(source_plan(target)))['valid']
        assert json.loads(status['result'])['size_bytes']==target.stat().st_size>0
    assert client.post('/api/operators/raster_flow_direction_d8/invoke',json={'params':{}}).status_code==403


def test_dem_d8_discovery_single_workflow_and_explicit_parameters():
    spec=next(item for item in list_operators() if item['id']=='raster_flow_direction_d8')
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read']
    assert spec['attributes']['resource_groups']==['raster'] and spec['category_path']==['栅格处理','像元计算']
    params={param['name']:param for param in spec['parameters']}
    assert set(params)=={'input_raster','band','elevation_unit'} and all(p['required'] for p in params.values())
    assert params['elevation_unit']['enum']==['metre','foot','us_survey_foot']
    assert spec['detailed_description']['workflow_example']['params']=={'input_raster':{'$ref':'source'},'band':1,'elevation_unit':'metre'}


@pytest.mark.parametrize('language,phrase',[('zh-cn','有限正数'),('en','finite positive')])
def test_dem_d8_unrepresentable_centre_distance_fails_explicitly(language,phrase):
    import operators.raster_compute as compute
    with raster_error_language(language),pytest.raises(ValueError,match=phrase):
        compute._d8_directions(np.zeros((2,2)),1.7e308,1.7e308)


def _accumulation_walk_oracle(directions):
    """Independent per-source path walks; no indegree/topological summation."""
    offsets = {1:(0,1),2:(1,1),4:(1,0),8:(1,-1),16:(0,-1),32:(-1,-1),64:(-1,0),128:(-1,1)}
    valid = np.isfinite(directions)
    result = np.where(valid, 0., np.nan)
    for row, col in np.argwhere(valid):
        seen = {(int(row), int(col))}
        while directions[row, col] != 0:
            dy, dx = offsets[directions[row, col]]
            row, col = row + dy, col + dx
            assert 0 <= row < directions.shape[0] and 0 <= col < directions.shape[1]
            assert valid[row, col] and (row, col) not in seen
            seen.add((row, col)); result[row, col] += 1
    assert sum(int(v) + 1 for v in result[directions == 0]) == np.count_nonzero(valid)
    return result


@pytest.mark.parametrize('seed', range(12))
def test_d8_accumulation_merging_multi_outlet_complete_walk_oracle(tmp_path, seed):
    import operators.raster_compute as compute
    rng = np.random.default_rng(seed)
    directions = np.zeros((9, 11))
    rows, cols = np.indices(directions.shape)
    ranks = np.minimum.reduce((rows, cols, 8-rows, 10-cols))
    offsets = [(-1,-1,32),(-1,0,64),(-1,1,128),(0,-1,16),(0,1,1),(1,-1,8),(1,0,4),(1,1,2)]
    for row, col in np.argwhere(ranks > 0):
        choices = [code for dy, dx, code in offsets if ranks[row+dy, col+dx] < ranks[row, col]]
        directions[row, col] = rng.choice(choices)
    directions[4,5] = np.nan  # An interior hole creates open terminals around itself.
    for row,col in np.argwhere(np.isfinite(directions) & (directions != 0)):
        dy,dx=next((dy,dx) for dy,dx,code in offsets if code==directions[row,col])
        if not np.isfinite(directions[row+dy,col+dx]):directions[row,col]=0
    path = create_raster(tmp_path/'directions.tif', directions, (500000,2,0,3000000,0,-3), 'EPSG:3857')
    before = path.read_bytes()
    with raster_workspace():
        source = raster_load(source_plan(path)); prior = set(source.workspace.iterdir())
        result = compute.raster_flow_accumulation_d8(source, 1)
        ds = gdal.Open(str(result.path)); band = ds.GetRasterBand(1)
        np.testing.assert_array_equal(read_band_values(band), _accumulation_walk_oracle(directions))
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0, np.isfinite(directions))
        assert ds.GetGeoTransform() == (500000.,2.,0.,3000000.,0.,-3.)
        assert ds.GetSpatialRef().GetAuthorityCode(None) == '3857'
        assert band.DataType == gdal.GDT_Float64 and math.isnan(band.GetNoDataValue())
        assert band.GetUnitType()=='' and band.GetScale() is None and band.GetOffset() is None and band.GetColorTable() is None
        assert set(source.workspace.iterdir()) == prior | {result.path}
        band=ds=None
    assert path.read_bytes()==before


@pytest.mark.parametrize('dy,dx,code',[(-1,-1,32),(-1,0,64),(-1,1,128),(0,-1,16),(0,1,1),(1,-1,8),(1,0,4),(1,1,2)])
def test_d8_accumulation_all_eight_codes_and_excludes_self(dy,dx,code):
    import operators.raster_compute as compute
    directions=np.full((3,3),np.nan);directions[1,1]=code;directions[1+dy,1+dx]=0
    actual=compute._d8_accumulation(directions)
    assert actual[1,1]==0 and actual[1+dy,1+dx]==1
    np.testing.assert_array_equal(actual,_accumulation_walk_oracle(directions))


@pytest.mark.parametrize('directions', [np.array([[1.,1.,0.]]),np.array([[4.],[4.],[0.]]),np.array([[0.]]),np.full((4,5),np.nan)])
def test_d8_accumulation_chain_singleton_and_all_invalid(tmp_path,directions):
    import operators.raster_compute as compute
    path=create_raster(tmp_path/'directions.tif',directions,(0,2,0,100,0,-3),'EPSG:3857')
    with raster_workspace():
        result=compute.raster_flow_accumulation_d8(raster_load(source_plan(path)),1)
        ds=gdal.Open(str(result.path));np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),_accumulation_walk_oracle(directions));ds=None


@pytest.mark.parametrize('bad',[-1.,3.,255.,1.5,2**53,float(2**64)])
@pytest.mark.parametrize('language,phrase',[('zh-cn','非法方向码'),('en','invalid direction code')])
@pytest.mark.parametrize('operator_name',['raster_flow_accumulation_d8','raster_flow_length_d8','raster_flow_length_upstream_d8','raster_drainage_basins_d8'])
def test_d8_consumers_illegal_direction_bilingual_no_private_output(tmp_path,bad,language,phrase, operator_name):
    import operators.raster_compute as compute
    path=create_raster(tmp_path/'directions.tif',np.array([[bad,0.]]),(0,1,0,100,0,-1),'EPSG:3857');before=path.read_bytes()
    with raster_workspace(),raster_error_language(language):
        source=raster_load(source_plan(path));prior=set(source.workspace.iterdir())
        with pytest.raises(ValueError,match=phrase):getattr(compute,operator_name)(source,1)
        assert set(source.workspace.iterdir())==prior
    assert path.read_bytes()==before


@pytest.mark.parametrize('case,zh,en',[
    ('terminal','有效终点0','terminal 0'),('outside','图内有效像元','in-bounds valid pixel'),
    ('invalid','图内有效像元','in-bounds valid pixel'),('cycle','方向循环','direction cycle'),
    ('cycle-with-outlet','方向循环','direction cycle'),
])
@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('operator_name',['raster_flow_accumulation_d8','raster_flow_length_d8','raster_flow_length_upstream_d8','raster_drainage_basins_d8'])
def test_d8_consumers_invalid_graph_fails_whole_workflow(tmp_path,case,zh,en,language, operator_name):
    import operators.raster_compute as compute
    directions=np.array([[1.,1.,0.]])
    if case=='terminal':directions=np.zeros((3,3))
    if case=='outside':directions[0,-1]=1
    if case=='invalid':directions[0,1]=np.nan
    if case=='cycle':directions=np.array([[1.,16.]])
    if case=='cycle-with-outlet':directions=np.array([[1.,16.,0.],[64.,64.,0.]])
    path=create_raster(tmp_path/'directions.tif',directions,(0,2,0,100,0,-3),'EPSG:3857')
    with raster_workspace(),raster_error_language(language):
        source=raster_load(source_plan(path));prior=set(source.workspace.iterdir())
        with pytest.raises(ValueError,match=zh if language=='zh-cn' else en):getattr(compute,operator_name)(source,1)
        assert set(source.workspace.iterdir())==prior


@pytest.mark.parametrize('band',[True,False,0,-1,2,1.5,'1',None])
@pytest.mark.parametrize('operator_name',['raster_flow_accumulation_d8','raster_flow_length_d8','raster_flow_length_upstream_d8','raster_drainage_basins_d8'])
def test_d8_consumers_requires_explicit_data_band(tmp_path,band, operator_name):
    import operators.raster_compute as compute
    path=create_raster(tmp_path/'directions.tif',np.array([[1.,0.]]),(0,2,0,100,0,-3),'EPSG:3857')
    with raster_workspace(),raster_error_language('en'),pytest.raises(ValueError,match='one-based direction band'):
        getattr(compute,operator_name)(raster_load(source_plan(path)),band)


@pytest.mark.parametrize('case,phrase', [('scale','scale/offset'),('offset','scale/offset'),('alpha','alpha band as direction'),('complex','complex direction'),('complex-alpha','complex direction'),('rotated','projected north-up'),('geographic','projected north-up'),('no-crs','projected north-up'),('south-up','projected north-up')])
@pytest.mark.parametrize('operator_name',['raster_flow_accumulation_d8','raster_flow_length_d8','raster_flow_length_upstream_d8','raster_drainage_basins_d8'])
def test_d8_consumers_admission_grid_and_band_encoding(tmp_path,case,phrase, operator_name):
    import operators.raster_compute as compute
    path=tmp_path/'directions.tif';complex_type=case in ('complex','complex-alpha')
    ds=gdal.GetDriverByName('GTiff').Create(str(path),3,3,2 if case=='complex-alpha' else 1,gdal.GDT_CFloat64 if complex_type else gdal.GDT_Float64)
    ds.SetGeoTransform((0,2,.1 if case=='rotated' else 0,100,0,3 if case=='south-up' else -3))
    if case!='no-crs':
        crs=osr.SpatialReference();crs.ImportFromEPSG(4326 if case=='geographic' else 3857);ds.SetProjection(crs.ExportToWkt())
    ds.GetRasterBand(1).Fill(0)
    if case=='scale':ds.GetRasterBand(1).SetScale(2)
    if case=='offset':ds.GetRasterBand(1).SetOffset(1)
    if case=='alpha':ds.GetRasterBand(1).SetColorInterpretation(gdal.GCI_AlphaBand)
    if case=='complex-alpha':ds.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand)
    ds=None
    with raster_workspace(),raster_error_language('en'),pytest.raises(ValueError,match=phrase):
        getattr(compute,operator_name)(raster_load(source_plan(path)),1)


@pytest.mark.parametrize('operator_name',['raster_flow_accumulation_d8','raster_flow_length_d8','raster_flow_length_upstream_d8','raster_drainage_basins_d8'])
def test_d8_consumers_effective_mask_nodata_alpha_nonfinite_and_valid_zero(tmp_path, operator_name):
    import operators.raster_compute as compute
    path=tmp_path/'bands.tif';values=np.zeros((3,7));values[1,:]=[np.inf,-9999,np.nan,255,255,255,0]
    alpha=np.full(values.shape,255.);alpha[1,3]=0;alpha[1,4]=np.nan;alpha[0,0]=128
    mask=np.full(values.shape,255,np.uint8);mask[1,5]=0
    ds=gdal.GetDriverByName('GTiff').Create(str(path),7,3,3,gdal.GDT_Float64);ds.SetGeoTransform((0,2,0,100,0,-3));crs=osr.SpatialReference();crs.ImportFromEPSG(3857);ds.SetProjection(crs.ExportToWkt())
    for i,array in enumerate((np.full(values.shape,255.),values,alpha),1):
        ds.GetRasterBand(i).WriteArray(array);ds.GetRasterBand(i).SetNoDataValue(-9999)
    ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
    ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);ds=None
    expected=np.where(np.isfinite(values)&(values!=-9999)&np.isfinite(alpha)&(alpha>0)&(mask!=0),0.,np.nan)
    if operator_name=='raster_drainage_basins_d8':
        expected=_drainage_basins_walk_oracle(expected)
    with raster_workspace():
        result=getattr(compute,operator_name)(raster_load(source_plan(path)),2)
        ds=gdal.Open(str(result.path));np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),expected)
        np.testing.assert_array_equal(ds.GetRasterBand(1).GetMaskBand().ReadAsArray()!=0,np.isfinite(expected));ds=None


def test_d8_accumulation_exact_count_limit_and_overflow_fail_closed(monkeypatch):
    import operators.raster_compute as compute
    assert compute._D8_MAX_EXACT_COUNT==2**53-1
    # Exercise the production bound at a small reachable graph size, without
    # claiming a 2^53-cell allocation or capacity acceptance.
    monkeypatch.setattr(compute,'_D8_MAX_EXACT_COUNT',4)
    np.testing.assert_array_equal(compute._d8_accumulation(np.array([[1.,1.,1.,1.,0.]])),[[0,1,2,3,4]])
    for language,phrase in [('zh-cn','精确整数上限'),('en','exact integer limit')]:
        with raster_error_language(language),pytest.raises(ValueError,match=phrase):
            compute._d8_accumulation(np.array([[1.,1.,1.,1.,1.,0.]]))


@pytest.mark.parametrize('stage',['read','graph','write','metadata'])
@pytest.mark.parametrize('operator_name',['raster_flow_accumulation_d8','raster_flow_length_d8','raster_flow_length_upstream_d8','raster_drainage_basins_d8'])
def test_d8_consumers_injected_failure_cleans_private_outputs(tmp_path,monkeypatch,stage, operator_name):
    import operators.raster_compute as compute
    path=create_raster(tmp_path/'directions.tif',np.array([[1.,1.,0.]]),(0,2,0,100,0,-3),'EPSG:3857')
    with raster_workspace():
        source=raster_load(source_plan(path));prior=set(source.workspace.iterdir())
        def fail(*args,**kwargs):raise RuntimeError('injected accumulation failure')
        if stage=='read':monkeypatch.setattr(compute,'_blocks',fail)
        if stage=='graph':
            helper={'raster_flow_accumulation_d8':'_d8_accumulation','raster_flow_length_d8':'_d8_flow_length','raster_flow_length_upstream_d8':'_d8_upstream_flow_length','raster_drainage_basins_d8':'_d8_drainage_basins'}[operator_name]
            monkeypatch.setattr(compute,helper,fail)
        if stage=='write':monkeypatch.setattr(gdal.Band,'WriteArray',fail)
        if stage=='metadata':monkeypatch.setattr(compute,'_raster',fail)
        with pytest.raises(RuntimeError,match='injected accumulation failure'):getattr(compute,operator_name)(source,1)
        assert set(source.workspace.iterdir())==prior


def test_d8_accumulation_long_cross_block_cog_full_counts_and_conservation(tmp_path,monkeypatch):
    import operators.raster_compute as compute
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    height,width=515,519
    directions=np.ones((height,width));directions[:,-1]=4;directions[-1,-1]=0
    expected=np.broadcast_to(np.arange(width),(height,width)).copy();expected[:,-1]=np.arange(1,height+1)*width-1
    path=create_raster(tmp_path/'directions.tif',directions,(500000,2,0,3000000,0,-3),'EPSG:3857');before=path.read_bytes();target=tmp_path/'accumulation.cog.tif'
    blocks=compute._blocks;reads=[]
    def bounded(*args,**kwargs):
        for x,y,arrays,masks in blocks(*args,**kwargs):
            assert all(max(a.shape)<=512 for a in arrays);reads.append((x,y));yield x,y,arrays,masks
    monkeypatch.setattr(compute,'_blocks',bounded)
    with raster_workspace():
        result=compute.raster_flow_accumulation_d8(raster_load(source_plan(path)),1)
        artifact=raster_save(result,target_plan(target),profile='cog',blocksize=128,overview_resampling='nearest')
        assert type(artifact['size_bytes']) is int and artifact['size_bytes']==target.stat().st_size>0
    ds=gdal.Open(str(target));band=ds.GetRasterBand(1);actual=read_band_values(band)
    np.testing.assert_array_equal(actual,expected);assert int(actual[-1,-1])+1==height*width
    assert not validate(ds,full_check=True)[1] and band.GetOverviewCount()>0
    np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray(),np.full((height,width),255,np.uint8))
    for i in range(band.GetOverviewCount()):
        overview=band.GetOverview(i).ReadAsArray();assert np.all(overview==np.floor(overview))
    band=ds=None;assert path.read_bytes()==before and (512,512) in reads


@pytest.mark.parametrize('case',['saved-direction','dem-direction','fill-direction','empty','cycle'])
@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('operator_name',['raster_flow_accumulation_d8','raster_flow_length_d8','raster_flow_length_upstream_d8','raster_drainage_basins_d8'])
def test_d8_consumers_real_async_save_and_failure_cleanup(tmp_path,monkeypatch,case,language, operator_name):
    import api_server,workflow_engine
    values=np.tile(np.arange(11,0,-1,dtype=float),(9,1));directions=np.ones(values.shape);directions[:,-1]=0
    if case=='fill-direction':values[:]=10;values[1:-1,1:-1]=-2
    if case=='empty':directions[:]=np.nan
    if case=='cycle':directions[0,1]=16
    data=directions if case in ('saved-direction','empty','cycle') else values
    path=create_raster(tmp_path/'source.tif',data,(0,2,0,100,0,-3),'EPSG:3857');before=path.read_bytes();target=tmp_path/'accumulation.cog.tif'
    tasks=[{'id':'source','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(path)}}];parent='source'
    if case=='fill-direction':
        tasks.append({'id':'fill','operator':'raster_fill_depressions','depends_on':[parent],'params':{'input_raster':{'$ref':parent},'band':1,'elevation_unit':'metre'}});parent='fill'
    if case in ('dem-direction','fill-direction'):
        tasks.append({'id':'direction','operator':'raster_flow_direction_d8','depends_on':[parent],'params':{'input_raster':{'$ref':parent},'band':1,'elevation_unit':'metre'}});parent='direction'
        directions=_d8_bellman_ford_oracle(_fill_level_reachability_oracle(values) if case=='fill-direction' else values)
    tasks += [{'id':'accumulate','operator':operator_name,'depends_on':[parent],'params':{'input_raster':{'$ref':parent},'band':1}},
              {'id':'save','operator':'raster_save','depends_on':['accumulate'],'params':{'input_raster':{'$ref':'accumulate'},'access_plan':target_plan(target),'profile':'cog','blocksize':128}}]
    workspaces=[];execute=workflow_engine._execute_operator
    def capture(name,params):
        if name==operator_name:workspaces.append(params['input_raster'].workspace)
        return execute(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',capture)
    client=api_server.app.test_client();response=client.post('/api/workflow',headers={'Accept-Language':language},json={'workflow_def':{'tasks':tasks},'input_data':{},'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert response.status_code==202,response.json
    deadline=time.monotonic()+15
    while time.monotonic()<deadline:
        status=client.get('/api/executions/'+response.json['execution_id'],headers={'Accept-Language':'en' if language=='zh-cn' else 'zh-cn'}).json
        if status['status'] in ('success','failed'):break
        time.sleep(.01)
    assert status['status']==('failed' if case=='cycle' else 'success'),status
    assert workspaces and all(not p.exists() for p in workspaces) and path.read_bytes()==before
    if case=='cycle':
        assert not target.exists() and ('方向循环' if language=='zh-cn' else 'direction cycle') in status['error']
    else:
        expected={'raster_flow_accumulation_d8':_accumulation_walk_oracle,
                  'raster_flow_length_d8':lambda v:_flow_length_walk_oracle(v,2,3),
                  'raster_flow_length_upstream_d8':lambda v:_upstream_length_walk_oracle(v,2,3),
                  'raster_drainage_basins_d8':_drainage_basins_walk_oracle}[operator_name](directions)
        ds=gdal.Open(str(target));actual=read_band_values(ds.GetRasterBand(1))
        if operator_name in ('raster_flow_length_d8','raster_flow_length_upstream_d8'):np.testing.assert_allclose(actual,expected,rtol=1e-12,atol=1e-9,equal_nan=True)
        else:np.testing.assert_array_equal(actual,expected)
        assert ds.GetRasterBand(1).GetUnitType()==('metre' if operator_name in ('raster_flow_length_d8','raster_flow_length_upstream_d8') else '');ds=None
        with raster_workspace():assert validate_cog(raster_load(source_plan(target)))['valid']
        assert json.loads(status['result'])['size_bytes']==target.stat().st_size>0
    assert client.post('/api/operators/'+operator_name+'/invoke',json={'params':{}}).status_code==403


@pytest.mark.parametrize('operator_name',['raster_flow_accumulation_d8','raster_flow_length_d8','raster_flow_length_upstream_d8','raster_drainage_basins_d8'])
def test_d8_consumers_discovery_explicit_direction_band_only(operator_name):
    spec=next(item for item in list_operators() if item['id']==operator_name)
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read']
    assert spec['attributes']['resource_groups']==['raster'] and spec['category_path']==['栅格处理','像元计算']
    params={p['name']:p for p in spec['parameters']}
    expected = {'input_raster', 'band', 'measure'} if operator_name == 'raster_flow_accumulation_d8' else {'input_raster', 'band'}
    assert set(params) == expected and params['input_raster']['required'] and params['band']['required']
    example = {'input_raster': {'$ref': 'source'}, 'band': 1}
    if operator_name == 'raster_flow_accumulation_d8':
        assert params['measure']['enum'] == ['count', 'area'] and params['measure']['default'] == 'count'
        assert not params['measure']['required']
        example['measure'] = 'count'
    assert spec['detailed_description']['workflow_example']['params'] == example


def test_d8_accumulation_cross_block_single_long_path_without_recursion():
    import operators.raster_compute as compute
    height,width=515,519
    directions=np.ones((height,width));directions[1::2,:]=16
    directions[::2,-1]=4;directions[1::2,0]=4;directions[-1,-1]=0
    expected=np.arange(height*width,dtype=np.float64).reshape(height,width)
    expected[1::2,:]=expected[1::2,::-1]
    actual=compute._d8_accumulation(directions)
    np.testing.assert_array_equal(actual,expected)
    assert actual[-1,-1]+1==height*width and np.count_nonzero(actual==0)==1


@pytest.mark.parametrize('operator_name',['raster_flow_accumulation_d8','raster_flow_length_d8','raster_flow_length_upstream_d8','raster_drainage_basins_d8'])
def test_d8_consumers_rejects_mixed_real_data_complex_alpha(monkeypatch, operator_name):
    import operators.raster_compute as compute
    ds=gdal.GetDriverByName('MEM').Create('',3,3,0)
    ds.AddBand(gdal.GDT_Float64);ds.AddBand(gdal.GDT_CFloat64)
    ds.SetGeoTransform((0,2,0,100,0,-3));crs=osr.SpatialReference();crs.ImportFromEPSG(3857);ds.SetProjection(crs.ExportToWkt())
    ds.GetRasterBand(1).Fill(0);ds.GetRasterBand(2).Fill(255);ds.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand)
    monkeypatch.setattr(compute,'_open',lambda _:ds)
    with raster_workspace(),raster_error_language('en'),pytest.raises(ValueError,match='complex direction or alpha'):
        getattr(compute,operator_name)(None,1)


@pytest.mark.parametrize('dtype',[gdal.GDT_Int64,gdal.GDT_UInt64])
@pytest.mark.parametrize('operator_name',['raster_flow_accumulation_d8','raster_flow_length_d8','raster_flow_length_upstream_d8','raster_drainage_basins_d8'])
def test_d8_consumers_native_nodata_cannot_hide_adjacent_illegal_integer(tmp_path,dtype, operator_name):
    import operators.raster_compute as compute
    path=tmp_path/'integer.tif';ds=gdal.GetDriverByName('GTiff').Create(str(path),3,1,1,dtype)
    ds.SetGeoTransform((0,2,0,100,0,-3));crs=osr.SpatialReference();crs.ImportFromEPSG(3857);ds.SetProjection(crs.ExportToWkt())
    band=ds.GetRasterBand(1);band.SetNoDataValue(2**53)
    band.WriteArray(np.array([[2**53,2**53+1,0]],dtype=np.int64 if dtype==gdal.GDT_Int64 else np.uint64));band=ds=None
    with raster_workspace(),raster_error_language('en'):
        source=raster_load(source_plan(path));prior=set(source.workspace.iterdir())
        with pytest.raises(ValueError,match='invalid direction code'):
            getattr(compute,operator_name)(source,1)
        assert set(source.workspace.iterdir())==prior



def _flow_length_walk_oracle(directions, width_metres, height_metres):
    # Independent per-origin path walk; no production graph/topological helper.
    offsets={1:(0,1),2:(1,1),4:(1,0),8:(1,-1),16:(0,-1),32:(-1,-1),64:(-1,0),128:(-1,1)}
    result=np.full(directions.shape,np.nan)
    for origin in np.argwhere(np.isfinite(directions)):
        row,col=map(int,origin);seen=set();steps=[]
        while directions[row,col]!=0:
            assert (row,col) not in seen;seen.add((row,col))
            dy,dx=offsets[directions[row,col]]
            steps.append(math.sqrt((dx*width_metres)**2+(dy*height_metres)**2))
            row,col=row+dy,col+dx
            assert 0<=row<directions.shape[0] and 0<=col<directions.shape[1] and np.isfinite(directions[row,col])
        result[tuple(origin)]=math.fsum(steps)
    return result


@pytest.mark.parametrize('seed',range(8))
@pytest.mark.parametrize('unit,factor',[('metre',1.),('foot',.3048),('US survey foot',1200/3937)])
def test_d8_flow_length_full_paths_units_holes_and_multiple_outlets(tmp_path,seed,unit,factor):
    import operators.raster_compute as compute
    rng=np.random.default_rng(seed);directions=np.zeros((9,11));rows,cols=np.indices(directions.shape)
    ranks=np.minimum.reduce((rows,cols,8-rows,10-cols))
    offsets=[(-1,-1,32),(-1,0,64),(-1,1,128),(0,-1,16),(0,1,1),(1,-1,8),(1,0,4),(1,1,2)]
    for row,col in np.argwhere(ranks>0):
        directions[row,col]=rng.choice([code for dy,dx,code in offsets if ranks[row+dy,col+dx]<ranks[row,col]])
    directions[4,5]=np.nan
    for row,col in np.argwhere(np.isfinite(directions)&(directions!=0)):
        dy,dx=next((dy,dx) for dy,dx,code in offsets if code==directions[row,col])
        if not np.isfinite(directions[row+dy,col+dx]):directions[row,col]=0
    # Boundary with a direction remains nonterminal. Includes cardinal+diagonal=8 units.
    directions[0,0]=1;directions[0,1]=2;directions[1,2]=64;directions[0,2]=0
    crs=osr.SpatialReference();crs.ImportFromEPSG(3857);crs.SetLinearUnitsAndUpdateParameters(unit,factor)
    transform=(500000,3,0,3000000,0,-4)
    path=create_raster(tmp_path/'directions.tif',directions,transform,crs.ExportToWkt());before=path.read_bytes()
    with raster_workspace():
        source=raster_load(source_plan(path));prior=set(source.workspace.iterdir());result=compute.raster_flow_length_d8(source,1)
        ds=gdal.Open(str(result.path));band=ds.GetRasterBand(1);actual=read_band_values(band)
        np.testing.assert_allclose(actual,_flow_length_walk_oracle(directions,3*factor,4*factor),rtol=1e-12,atol=1e-9,equal_nan=True)
        assert actual[0,0]==pytest.approx(12*factor) and actual[0,1]==pytest.approx(9*factor)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(directions))
        assert ds.GetGeoTransform()==transform and ds.GetSpatialRef().IsSame(crs)
        assert band.DataType==gdal.GDT_Float64 and math.isnan(band.GetNoDataValue()) and band.GetUnitType()=='metre'
        assert band.GetScale() is None and band.GetOffset() is None and band.GetColorTable() is None
        assert set(source.workspace.iterdir())==prior|{result.path};band=ds=None
    assert path.read_bytes()==before


@pytest.mark.parametrize('dy,dx,code',[(-1,-1,32),(-1,0,64),(-1,1,128),(0,-1,16),(0,1,1),(1,-1,8),(1,0,4),(1,1,2)])
def test_d8_flow_length_all_eight_steps_terminal_zero(dy,dx,code):
    import operators.raster_compute as compute
    directions=np.full((3,3),np.nan);directions[1,1]=code;directions[1+dy,1+dx]=0
    actual=compute._d8_flow_length(directions,3.,4.)
    assert actual[1,1]==(5 if dy and dx else 4 if dy else 3) and actual[1+dy,1+dx]==0
    np.testing.assert_allclose(actual,_flow_length_walk_oracle(directions,3.,4.),rtol=1e-12,atol=1e-9,equal_nan=True)


@pytest.mark.parametrize('directions',[np.array([[1.,1.,0.]]),np.array([[4.],[4.],[0.]]),np.array([[0.]]),np.full((4,5),np.nan)])
def test_d8_flow_length_single_axes_singleton_empty(tmp_path,directions):
    import operators.raster_compute as compute
    path=create_raster(tmp_path/'directions.tif',directions,(0,3,0,100,0,-4),'EPSG:3857')
    with raster_workspace():
        result=compute.raster_flow_length_d8(raster_load(source_plan(path)),1);ds=gdal.Open(str(result.path))
        np.testing.assert_allclose(read_band_values(ds.GetRasterBand(1)),_flow_length_walk_oracle(directions,3,4),rtol=1e-12,atol=1e-9,equal_nan=True);ds=None


@pytest.mark.parametrize('width,height',[(0.,1.),(-1.,1.),(1.,0.),(float('nan'),1.),(1.,float('inf')),(1.7e308,1.7e308),(1e-200,2e-200)])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_flow_length_finite_positive_steps(width,height,language):
    import operators.raster_compute as compute
    with raster_error_language(language):
        if width==1e-200:
            # hypot avoids underflow from squaring legitimate tiny steps.
            actual=compute._d8_flow_length(np.array([[2.,np.nan],[np.nan,0.]]),width,height)
            assert actual[0,0]==pytest.approx(math.hypot(width,height),rel=1e-15,abs=0)
        else:
            with pytest.raises(ValueError,match='步长必须有限' if language=='zh-cn' else 'finite, strictly positive'):
                compute._d8_flow_length(np.array([[1.,0.]]),width,height)


@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_flow_length_real_finite_range_overflow_no_output(tmp_path,language):
    import operators.raster_compute as compute
    path=create_raster(tmp_path/'directions.tif',np.array([[1.,1.,0.]]),(0,1e308,0,100,0,-1),'EPSG:3857')
    with raster_workspace(),raster_error_language(language):
        source=raster_load(source_plan(path));prior=set(source.workspace.iterdir())
        with pytest.raises(ValueError,match='累计距离超过' if language=='zh-cn' else 'accumulated distance exceeds'):
            compute.raster_flow_length_d8(source,1)
        assert set(source.workspace.iterdir())==prior


def test_d8_flow_length_cross_block_cog_full_length_mask_and_grid(tmp_path,monkeypatch):
    import operators.raster_compute as compute
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    height,width=515,519;directions=np.ones((height,width));directions[:,-1]=4;directions[-1,-1]=0
    rows,cols=np.indices(directions.shape);expected=(width-1-cols)*3.+(height-1-rows)*4.
    path=create_raster(tmp_path/'directions.tif',directions,(500000,3,0,3000000,0,-4),'EPSG:3857');before=path.read_bytes();target=tmp_path/'length.cog.tif'
    original=compute._blocks;reads=[]
    def bounded(*args,**kwargs):
        for x,y,arrays,masks in original(*args,**kwargs):
            assert all(max(a.shape)<=512 for a in arrays);reads.append((x,y));yield x,y,arrays,masks
    monkeypatch.setattr(compute,'_blocks',bounded)
    with raster_workspace():
        result=compute.raster_flow_length_d8(raster_load(source_plan(path)),1)
        artifact=raster_save(result,target_plan(target),profile='cog',blocksize=128,overview_resampling='nearest')
        assert type(artifact['size_bytes']) is int and artifact['size_bytes']==target.stat().st_size>0
    ds=gdal.Open(str(target));band=ds.GetRasterBand(1);actual=read_band_values(band)
    np.testing.assert_allclose(actual,expected,rtol=1e-12,atol=1e-9)
    np.testing.assert_allclose(actual[:,:-1],3+actual[:,1:],rtol=1e-12,atol=1e-9)
    np.testing.assert_allclose(actual[:-1,-1],4+actual[1:,-1],rtol=1e-12,atol=1e-9)
    assert actual[-1,-1]==0 and not validate(ds,full_check=True)[1] and band.GetOverviewCount()>0
    assert band.GetUnitType()=='metre' and ds.GetGeoTransform()==(500000.,3.,0.,3000000.,0.,-4.) and ds.GetSpatialRef().GetAuthorityCode(None)=='3857'
    np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray(),np.full((height,width),255,np.uint8))
    band=ds=None;assert path.read_bytes()==before and (512,512) in reads


def test_d8_flow_length_long_single_path_without_recursion():
    import operators.raster_compute as compute
    height,width=515,519;directions=np.ones((height,width));directions[1::2,:]=16
    directions[::2,-1]=4;directions[1::2,0]=4;directions[-1,-1]=0
    rows,cols=np.indices(directions.shape);remaining_row=np.where(rows%2==0,width-1-cols,cols)
    expected=remaining_row*3.+(height-1-rows)*((width-1)*3.+4.)
    actual=compute._d8_flow_length(directions,3.,4.)
    np.testing.assert_allclose(actual,expected,rtol=1e-12,atol=1e-9)
    assert actual[-1,-1]==0 and np.count_nonzero(actual==0)==1



def test_d8_flow_length_long_irrational_diagonal_route():
    import operators.raster_compute as compute
    width=8193;directions=np.full((2,width),np.nan);cols=np.arange(width);rows=cols%2
    directions[rows,cols]=np.where(rows==0,2.,128.);directions[rows[-1],-1]=0
    expected=np.full(directions.shape,np.nan);expected[rows,cols]=(width-1-cols)*math.sqrt(13.)
    actual=compute._d8_flow_length(directions,2.,3.)
    np.testing.assert_allclose(actual,expected,rtol=1e-12,atol=1e-9,equal_nan=True)


def _drainage_basins_walk_oracle(directions):
    # Independent walks from every origin, without product graph/topology helpers.
    offsets={1:(0,1),2:(1,1),4:(1,0),8:(1,-1),16:(0,-1),32:(-1,-1),64:(-1,0),128:(-1,1)}
    outlets={tuple(position):label for label,position in enumerate(np.argwhere(directions==0),1)}
    result=np.full(directions.shape,np.nan)
    for origin in np.argwhere(np.isfinite(directions)):
        row,col=map(int,origin);seen=set()
        while directions[row,col]!=0:
            assert (row,col) not in seen;seen.add((row,col))
            dy,dx=offsets[directions[row,col]];row,col=row+dy,col+dx
            assert 0<=row<directions.shape[0] and 0<=col<directions.shape[1] and np.isfinite(directions[row,col])
        result[tuple(origin)]=outlets[row,col]
    assert np.array_equal(np.unique(result[np.isfinite(result)]),np.arange(1,len(outlets)+1))
    assert sum(np.count_nonzero(result==label) for label in outlets.values())==np.count_nonzero(np.isfinite(directions))
    return result


@pytest.mark.parametrize('seed',range(12))
@pytest.mark.parametrize('unit,factor',[('metre',1.),('foot',.3048),('US survey foot',1200/3937)])
def test_d8_drainage_basins_complete_walk_holes_outlet_order_and_units(tmp_path,seed,unit,factor):
    import operators.raster_compute as compute
    rng=np.random.default_rng(seed);directions=np.zeros((9,11));rows,cols=np.indices(directions.shape)
    ranks=np.minimum.reduce((rows,cols,8-rows,10-cols))
    offsets=[(-1,-1,32),(-1,0,64),(-1,1,128),(0,-1,16),(0,1,1),(1,-1,8),(1,0,4),(1,1,2)]
    for row,col in np.argwhere(ranks>0):
        directions[row,col]=rng.choice([code for dy,dx,code in offsets if ranks[row+dy,col+dx]<ranks[row,col]])
    directions[4,5]=np.nan
    for row,col in np.argwhere(np.isfinite(directions)&(directions!=0)):
        dy,dx=next((dy,dx) for dy,dx,code in offsets if code==directions[row,col])
        if not np.isfinite(directions[row+dy,col+dx]):directions[row,col]=0
    # An edge continuation reaches a later outlet; an internal hole has its own outlet.
    directions[0,0]=1;directions[0,1]=2;directions[1,2]=64;directions[0,2]=0;directions[4,4]=0
    crs=osr.SpatialReference();crs.ImportFromEPSG(3857);crs.SetLinearUnitsAndUpdateParameters(unit,factor)
    transform=(500000,3,0,3000000,0,-4)
    path=create_raster(tmp_path/'directions.tif',directions,transform,crs.ExportToWkt());before=path.read_bytes()
    with raster_workspace():
        source=raster_load(source_plan(path));prior=set(source.workspace.iterdir());result=compute.raster_drainage_basins_d8(source,1)
        ds=gdal.Open(str(result.path));band=ds.GetRasterBand(1);actual=read_band_values(band)
        np.testing.assert_array_equal(actual,_drainage_basins_walk_oracle(directions))
        assert actual[0,0]==actual[0,1]==actual[1,2]==actual[0,2]==1
        np.testing.assert_array_equal(actual[directions==0],np.arange(1,np.count_nonzero(directions==0)+1))
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(directions))
        assert ds.GetGeoTransform()==transform and ds.GetSpatialRef().IsSame(crs)
        assert band.DataType==gdal.GDT_Float64 and math.isnan(band.GetNoDataValue()) and band.GetUnitType()==''
        assert band.GetScale() is None and band.GetOffset() is None and band.GetColorTable() is None
        assert set(source.workspace.iterdir())==prior|{result.path};band=ds=None
    assert path.read_bytes()==before and not result.path.exists()


@pytest.mark.parametrize('dy,dx,code',[(-1,-1,32),(-1,0,64),(-1,1,128),(0,-1,16),(0,1,1),(1,-1,8),(1,0,4),(1,1,2)])
def test_d8_drainage_basins_all_eight_directions(dy,dx,code):
    import operators.raster_compute as compute
    directions=np.full((3,3),np.nan);directions[1,1]=code;directions[1+dy,1+dx]=0
    actual=compute._d8_drainage_basins(directions)
    np.testing.assert_array_equal(actual,_drainage_basins_walk_oracle(directions))
    assert actual[1,1]==actual[1+dy,1+dx]==1


@pytest.mark.parametrize('directions',[
    np.array([[1.,1.,0.]]),np.array([[4.],[4.],[0.]]),np.array([[0.]]),np.full((4,5),np.nan),
    np.array([[0.,0.,0.,np.nan,0.]]),np.array([[0.],[0.],[np.nan],[0.]]),
    np.array([[2.,4.,8.],[1.,0.,16.],[128.,64.,32.]]),
])
def test_d8_drainage_basins_single_axes_adjacent_outlets_empty_and_hole(tmp_path,directions):
    import operators.raster_compute as compute
    # A central zero needs an invalid neighbour to be a legal open terminal.
    if directions.shape==(3,3):directions[0,0]=np.nan
    path=create_raster(tmp_path/'directions.tif',directions,(0,2,0,100,0,-3),'EPSG:3857')
    with raster_workspace():
        result=compute.raster_drainage_basins_d8(raster_load(source_plan(path)),1);ds=gdal.Open(str(result.path))
        np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),_drainage_basins_walk_oracle(directions));ds=None


def test_d8_drainage_basins_exact_id_limit_before_float_conversion(monkeypatch):
    import operators.raster_compute as compute
    assert compute._D8_MAX_EXACT_COUNT==2**53-1
    monkeypatch.setattr(compute,'_D8_MAX_EXACT_COUNT',4)
    np.testing.assert_array_equal(compute._d8_drainage_basins(np.zeros((1,4))),[[1.,2.,3.,4.]])
    for language,phrase in [('zh-cn','D8 汇水分区的出口数超过'),('en','D8 drainage basins outlet count exceeds')]:
        with raster_error_language(language),pytest.raises(ValueError,match=phrase):
            compute._d8_drainage_basins(np.zeros((1,5)))


def test_d8_drainage_basins_cross_block_complete_cog_categories_and_conservation(tmp_path,monkeypatch):
    import operators.raster_compute as compute
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    height,width=515,519;directions=np.ones((height,width));directions[:,-1]=0
    directions[257,257]=np.nan;directions[257,256]=0
    expected=np.broadcast_to(np.arange(1,height+1)[:,None],directions.shape).astype(float).copy()
    expected[257,:257]=258;expected[257,257]=np.nan;expected[257,258:]=259;expected[258:]+=1
    transform=(500000.,3.,0.,3000000.,0.,-4.)
    path=create_raster(tmp_path/'directions.tif',directions,transform,'EPSG:3857');before=path.read_bytes();target=tmp_path/'basins.cog.tif'
    original=compute._blocks;reads=[]
    def bounded(*args,**kwargs):
        for x,y,arrays,masks in original(*args,**kwargs):
            assert all(max(a.shape)<=512 for a in arrays);reads.append((x,y));yield x,y,arrays,masks
    monkeypatch.setattr(compute,'_blocks',bounded)
    with raster_workspace():
        result=compute.raster_drainage_basins_d8(raster_load(source_plan(path)),1)
        artifact=raster_save(result,target_plan(target),profile='cog',blocksize=128,overview_resampling='nearest')
        assert type(artifact['size_bytes']) is int and artifact['size_bytes']==target.stat().st_size>0
    assert not result.path.exists()
    ds=gdal.Open(str(target));band=ds.GetRasterBand(1);actual=read_band_values(band)
    np.testing.assert_array_equal(actual,expected)
    np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(directions))
    labels,counts=np.unique(actual[np.isfinite(actual)],return_counts=True)
    np.testing.assert_array_equal(labels,np.arange(1,height+2));assert counts.sum()==height*width-1
    assert not validate(ds,full_check=True)[1] and band.GetOverviewCount()>0
    assert band.DataType==gdal.GDT_Float64 and math.isnan(band.GetNoDataValue()) and band.GetUnitType()==''
    assert ds.GetGeoTransform()==transform and ds.GetSpatialRef().GetAuthorityCode(None)=='3857'
    assert band.GetScale() is None and band.GetOffset() is None and band.GetColorTable() is None
    for i in range(band.GetOverviewCount()):
        overview=read_band_values(band.GetOverview(i));assert np.isin(overview[np.isfinite(overview)],labels).all()
    band=ds=None;assert path.read_bytes()==before and (512,512) in reads


def test_d8_drainage_basins_long_single_path_without_recursion():
    import operators.raster_compute as compute
    height,width=515,519;directions=np.ones((height,width));directions[1::2,:]=16
    directions[::2,-1]=4;directions[1::2,0]=4;directions[-1,-1]=0
    np.testing.assert_array_equal(compute._d8_drainage_basins(directions),np.ones(directions.shape))


def _upstream_length_walk_oracle(directions, width_metres, height_metres):
    # Independently walk every origin, measuring each visited prefix with fsum.
    # No production graph, indegrees, queue or topological ordering is used.
    offsets={1:(0,1),2:(1,1),4:(1,0),8:(1,-1),16:(0,-1),32:(-1,-1),64:(-1,0),128:(-1,1)}
    valid=np.isfinite(directions);result=np.where(valid,0.,np.nan)
    for origin in np.argwhere(valid):
        row,col=map(int,origin);seen={(row,col)};edges=[]
        while directions[row,col]!=0:
            dy,dx=offsets[directions[row,col]]
            edges.append(math.sqrt((dx*width_metres)**2+(dy*height_metres)**2))
            row,col=row+dy,col+dx
            assert 0<=row<directions.shape[0] and 0<=col<directions.shape[1] and valid[row,col]
            assert (row,col) not in seen;seen.add((row,col))
            result[row,col]=max(result[row,col],math.fsum(edges))
    return result


@pytest.mark.parametrize('seed',range(12))
@pytest.mark.parametrize('unit,factor',[('metre',1.),('foot',.3048),('US survey foot',1200/3937)])
def test_d8_upstream_length_full_paths_units_holes_and_multi_outlets(tmp_path,seed,unit,factor):
    import operators.raster_compute as compute
    rng=np.random.default_rng(seed);directions=np.zeros((9,11));rows,cols=np.indices(directions.shape)
    ranks=np.minimum.reduce((rows,cols,8-rows,10-cols))
    offsets=[(-1,-1,32),(-1,0,64),(-1,1,128),(0,-1,16),(0,1,1),(1,-1,8),(1,0,4),(1,1,2)]
    for row,col in np.argwhere(ranks>0):
        directions[row,col]=rng.choice([code for dy,dx,code in offsets if ranks[row+dy,col+dx]<ranks[row,col]])
    directions[4,5]=np.nan
    for row,col in np.argwhere(np.isfinite(directions)&(directions!=0)):
        dy,dx=next((dy,dx) for dy,dx,code in offsets if code==directions[row,col])
        if not np.isfinite(directions[row+dy,col+dx]):directions[row,col]=0
    directions[0,0]=1;directions[0,1]=2;directions[1,2]=64;directions[0,2]=0
    crs=osr.SpatialReference();crs.ImportFromEPSG(3857);crs.SetLinearUnitsAndUpdateParameters(unit,factor)
    transform=(500000,3,0,3000000,0,-4)
    path=create_raster(tmp_path/'directions.tif',directions,transform,crs.ExportToWkt());before=path.read_bytes()
    with raster_workspace():
        source=raster_load(source_plan(path));prior=set(source.workspace.iterdir());result=compute.raster_flow_length_upstream_d8(source,1)
        ds=gdal.Open(str(result.path));band=ds.GetRasterBand(1);actual=read_band_values(band)
        np.testing.assert_allclose(actual,_upstream_length_walk_oracle(directions,3*factor,4*factor),rtol=1e-12,atol=1e-9,equal_nan=True)
        assert actual[0,2]>=12*factor and np.any(actual[directions==0]>0)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(directions))
        assert ds.GetGeoTransform()==transform and ds.GetSpatialRef().IsSame(crs)
        assert band.DataType==gdal.GDT_Float64 and math.isnan(band.GetNoDataValue()) and band.GetUnitType()=='metre'
        assert band.GetScale() is None and band.GetOffset() is None and band.GetColorTable() is None
        assert set(source.workspace.iterdir())==prior|{result.path};band=ds=None
    assert path.read_bytes()==before and not result.path.exists()


def test_d8_upstream_length_confluence_takes_maximum_not_sum_or_shortest():
    import operators.raster_compute as compute
    directions=np.array([[1.,1.,4.,np.nan],[np.nan,np.nan,1.,0.],[np.nan,128.,np.nan,32.]])
    actual=compute._d8_upstream_flow_length(directions,3.,4.)
    expected=np.array([[0.,3.,6.,np.nan],[np.nan,np.nan,10.,13.],[np.nan,0.,np.nan,0.]])
    np.testing.assert_array_equal(actual,expected)
    np.testing.assert_array_equal(actual,_upstream_length_walk_oracle(directions,3.,4.))
    assert actual[1,3]>0  # An open zero-direction outlet retains upstream length.


@pytest.mark.parametrize('dy,dx,code',[(-1,-1,32),(-1,0,64),(-1,1,128),(0,-1,16),(0,1,1),(1,-1,8),(1,0,4),(1,1,2)])
def test_d8_upstream_length_all_eight_steps_and_no_inflow_zero(dy,dx,code):
    import operators.raster_compute as compute
    directions=np.full((3,3),np.nan);directions[1,1]=code;directions[1+dy,1+dx]=0
    actual=compute._d8_upstream_flow_length(directions,3.,4.)
    assert actual[1,1]==0 and actual[1+dy,1+dx]==math.sqrt((dx*3)**2+(dy*4)**2)
    np.testing.assert_array_equal(actual,_upstream_length_walk_oracle(directions,3.,4.))


@pytest.mark.parametrize('directions',[np.array([[1.,1.,0.]]),np.array([[4.],[4.],[0.]]),np.array([[0.]]),np.full((4,5),np.nan)])
def test_d8_upstream_length_single_axes_singleton_and_all_invalid(tmp_path,directions):
    import operators.raster_compute as compute
    path=create_raster(tmp_path/'directions.tif',directions,(0,3,0,100,0,-4),'EPSG:3857')
    with raster_workspace():
        result=compute.raster_flow_length_upstream_d8(raster_load(source_plan(path)),1);ds=gdal.Open(str(result.path))
        np.testing.assert_allclose(read_band_values(ds.GetRasterBand(1)),_upstream_length_walk_oracle(directions,3.,4.),rtol=1e-12,atol=1e-9,equal_nan=True);ds=None


@pytest.mark.parametrize('width,height',[(0.,1.),(-1.,1.),(1.,0.),(float('nan'),1.),(1.,float('inf')),(1.7e308,1.7e308),(1e-200,2e-200)])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_upstream_length_finite_positive_steps_and_tiny_diagonal(width,height,language):
    import operators.raster_compute as compute
    with raster_error_language(language):
        if width==1e-200:
            actual=compute._d8_upstream_flow_length(np.array([[2.,np.nan],[np.nan,0.]]),width,height)
            assert actual[0,0]==0 and actual[1,1]==pytest.approx(math.hypot(width,height),rel=1e-15,abs=0)
        else:
            with pytest.raises(ValueError,match='步长必须有限' if language=='zh-cn' else 'finite, strictly positive'):
                compute._d8_upstream_flow_length(np.array([[1.,0.]]),width,height)


@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_upstream_length_real_overflow_fails_without_private_output(tmp_path,language):
    import operators.raster_compute as compute
    path=create_raster(tmp_path/'directions.tif',np.array([[1.,1.,0.]]),(0,1e308,0,100,0,-1),'EPSG:3857')
    with raster_workspace(),raster_error_language(language):
        source=raster_load(source_plan(path));prior=set(source.workspace.iterdir())
        with pytest.raises(ValueError,match='D8 上游最长流长累计距离超过' if language=='zh-cn' else 'D8 longest upstream flow length accumulated distance exceeds'):
            compute.raster_flow_length_upstream_d8(source,1)
        assert set(source.workspace.iterdir())==prior


def test_d8_upstream_length_cross_block_cog_complete_pixels_mask_grid_and_units(tmp_path,monkeypatch):
    import operators.raster_compute as compute
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    height,width=515,519;directions=np.ones((height,width));directions[:,-1]=0
    directions[257,257]=np.nan;directions[257,256]=0
    expected=np.broadcast_to(np.arange(width)*3.,directions.shape).copy()
    expected[257,257]=np.nan;expected[257,258:]=np.arange(width-258)*3.
    transform=(500000.,3.,0.,3000000.,0.,-4.)
    path=create_raster(tmp_path/'directions.tif',directions,transform,'EPSG:3857');before=path.read_bytes();target=tmp_path/'upstream.cog.tif'
    original=compute._blocks;reads=[]
    def bounded(*args,**kwargs):
        for x,y,arrays,masks in original(*args,**kwargs):
            assert all(max(a.shape)<=512 for a in arrays);reads.append((x,y));yield x,y,arrays,masks
    monkeypatch.setattr(compute,'_blocks',bounded)
    with raster_workspace():
        result=compute.raster_flow_length_upstream_d8(raster_load(source_plan(path)),1)
        artifact=raster_save(result,target_plan(target),profile='cog',blocksize=128,overview_resampling='nearest')
        assert type(artifact['size_bytes']) is int and artifact['size_bytes']==target.stat().st_size>0
    ds=gdal.Open(str(target));band=ds.GetRasterBand(1);actual=read_band_values(band)
    np.testing.assert_allclose(actual,expected,rtol=1e-12,atol=1e-9,equal_nan=True)
    np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(directions))
    assert actual[257,256]==768 and actual[257,258]==0
    assert not validate(ds,full_check=True)[1] and band.GetOverviewCount()>0
    assert band.DataType==gdal.GDT_Float64 and math.isnan(band.GetNoDataValue()) and band.GetUnitType()=='metre'
    assert ds.GetGeoTransform()==transform and ds.GetSpatialRef().GetAuthorityCode(None)=='3857'
    assert band.GetScale() is None and band.GetOffset() is None and band.GetColorTable() is None
    band=ds=None;assert path.read_bytes()==before and not result.path.exists() and (512,512) in reads


def test_d8_upstream_length_long_single_path_without_recursion():
    import operators.raster_compute as compute
    height,width=515,519;directions=np.ones((height,width));directions[1::2,:]=16
    directions[::2,-1]=4;directions[1::2,0]=4;directions[-1,-1]=0
    rows,cols=np.indices(directions.shape);row_distance=np.where(rows%2==0,cols,width-1-cols)*3.
    expected=rows*((width-1)*3.+4.)+row_distance
    np.testing.assert_allclose(compute._d8_upstream_flow_length(directions,3.,4.),expected,rtol=1e-12,atol=1e-9)


def test_d8_upstream_length_long_irrational_diagonal_path():
    import operators.raster_compute as compute
    width=8193;directions=np.full((2,width),np.nan);cols=np.arange(width);rows=cols%2
    directions[rows,cols]=np.where(rows==0,2.,128.);directions[rows[-1],-1]=0
    expected=np.full(directions.shape,np.nan);expected[rows,cols]=cols*math.sqrt(13.)
    np.testing.assert_allclose(compute._d8_upstream_flow_length(directions,2.,3.),expected,rtol=1e-12,atol=1e-9,equal_nan=True)


_STRAHLER_STEPS = {1:(0,1),2:(1,1),4:(1,0),8:(1,-1),16:(0,-1),32:(-1,-1),64:(-1,0),128:(-1,1)}


def _strahler_recursive_oracle(directions, streams):
    """Independent backward recursion on small trees; no production graph/queue."""
    from functools import lru_cache
    river = streams == 1
    @lru_cache(None)
    def level(row, col):
        donors=[]
        for code,(dy,dx) in _STRAHLER_STEPS.items():
            y,x=row-dy,col-dx
            if 0<=y<river.shape[0] and 0<=x<river.shape[1] and river[y,x] and directions[y,x]==code:
                donors.append(level(y,x))
        if not donors:return 1
        highest=max(donors)
        return highest+int(donors.count(highest)>=2)
    result=np.where(np.isfinite(directions),0.,np.nan)
    for row,col in np.argwhere(river):result[row,col]=level(int(row),int(col))
    return result


def _strahler_tree():
    directions=np.full((7,9),np.nan)
    for cell,code in { (1,1):2,(1,3):8,(2,2):2,(5,1):128,(5,3):32,(4,2):128,(2,4):8,
                       (3,3):1,(3,4):1,(3,5):1,(3,6):1,(3,7):1,(3,8):0,(2,6):4}.items():
        directions[cell]=code
    return directions,np.where(np.isfinite(directions),1.,np.nan)


def _strahler_files(tmp_path,directions,streams):
    transform=(500000,2,0,3000000,0,-3)
    return (create_raster(tmp_path/'directions.tif',directions,transform,'EPSG:3857'),
            create_raster(tmp_path/'streams.tif',streams,transform,'EPSG:3857'))


@pytest.mark.parametrize('prune_lower',[False,True])
def test_d8_strahler_nested_confluences_maximum_ties_and_background(tmp_path,prune_lower):
    import operators.raster_compute as compute
    directions,streams=_strahler_tree()
    if prune_lower:
        for cell in ((5,1),(5,3),(4,2)):streams[cell]=0
    paths=_strahler_files(tmp_path,directions,streams);before=[p.read_bytes() for p in paths]
    expected=_strahler_recursive_oracle(directions,streams)
    assert expected[2,2]==2 and expected[3,3]==(2 if prune_lower else 3)
    assert np.all(expected[3,3:]==(2 if prune_lower else 3)) # Lower-order side tributaries do not upgrade.
    with raster_workspace():
        source,mask=[raster_load(source_plan(p)) for p in paths];prior=set(source.workspace.iterdir())
        result=compute.raster_stream_order_d8(source,mask,1,1);ds=gdal.Open(str(result.path));band=ds.GetRasterBand(1)
        np.testing.assert_array_equal(read_band_values(band),expected)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected))
        assert band.DataType==gdal.GDT_Float64 and math.isnan(band.GetNoDataValue())
        assert band.GetUnitType()=='' and band.GetScale() is None and band.GetOffset() is None and band.GetColorTable() is None
        assert ds.GetGeoTransform()==(500000.,2.,0.,3000000.,0.,-3.) and ds.GetSpatialRef().GetAuthorityCode(None)=='3857'
        assert set(source.workspace.iterdir())==prior|{result.path};band=ds=None
    assert [p.read_bytes() for p in paths]==before


@pytest.mark.parametrize('seed',range(12))
def test_d8_strahler_random_forest_matches_independent_recursive_oracle(seed):
    import operators.raster_compute as compute
    rng=np.random.default_rng(seed);rows,cols=np.indices((9,11));ranks=np.minimum.reduce((rows,cols,8-rows,10-cols))
    directions=np.zeros((9,11))
    for row,col in np.argwhere(ranks>0):
        choices=[code for code,(dy,dx) in _STRAHLER_STEPS.items() if ranks[row+dy,col+dx]<ranks[row,col]]
        directions[row,col]=rng.choice(choices)
    directions[4,5]=np.nan
    for row,col in np.argwhere(np.isfinite(directions)&(directions!=0)):
        dy,dx=_STRAHLER_STEPS[directions[row,col]]
        if not np.isfinite(directions[row+dy,col+dx]):directions[row,col]=0
    # Independent threshold from per-origin counts produces downstream-closed streams.
    counts=_accumulation_walk_oracle(directions);streams=np.where(np.isfinite(directions),(counts>=seed%4).astype(float),np.nan)
    np.testing.assert_array_equal(compute._d8_stream_order(directions,streams==1),_strahler_recursive_oracle(directions,streams))


@pytest.mark.parametrize('dy,dx,code',[(dy,dx,code) for code,(dy,dx) in _STRAHLER_STEPS.items()])
def test_d8_strahler_all_eight_routes_source_and_outlet_grade_one(dy,dx,code):
    import operators.raster_compute as compute
    directions=np.full((3,3),np.nan);directions[1,1]=code;directions[1+dy,1+dx]=0
    expected=np.where(np.isfinite(directions),1.,np.nan)
    np.testing.assert_array_equal(compute._d8_stream_order(directions,np.isfinite(directions)),expected)


def test_d8_strahler_diagonal_crossings_and_neighbours_are_not_confluences():
    import operators.raster_compute as compute
    directions=np.array([[2.,8.],[0.,0.]])
    np.testing.assert_array_equal(compute._d8_stream_order(directions,np.ones((2,2),bool)),np.ones((2,2)))
    np.testing.assert_array_equal(compute._d8_stream_order(np.zeros((1,3)),np.ones((1,3),bool)),np.ones((1,3)))


@pytest.mark.parametrize('directions,streams',[(np.array([[1.,1.,0.]]),np.array([[0.,1.,1.]])),
    (np.array([[4.],[4.],[0.]]),np.ones((3,1))), (np.array([[0.]]),np.array([[1.]])),
    (np.array([[0.]]),np.array([[0.]])),(np.full((4,5),np.nan),np.full((4,5),np.nan))])
def test_d8_strahler_axes_empty_streams_and_all_invalid(tmp_path,directions,streams):
    import operators.raster_compute as compute
    paths=_strahler_files(tmp_path,directions,streams)
    with raster_workspace():
        result=compute.raster_stream_order_d8(*[raster_load(source_plan(p)) for p in paths],1,1)
        ds=gdal.Open(str(result.path));np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),_strahler_recursive_oracle(directions,streams));ds=None


@pytest.mark.parametrize('operator,method',[('raster_stream_order_d8','strahler'),('raster_stream_order_d8','shreve'),('raster_stream_link_d8',None),('raster_stream_to_vector_d8',None),('raster_distance_to_stream_d8',None)])
@pytest.mark.parametrize('role',['direction','mask'])
@pytest.mark.parametrize('band',[True,False,0,-1,2,1.5,'1',None])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_streams_both_bands_explicit_valid_and_bilingual(tmp_path,role,band,language,operator,method):
    import operators.raster_compute as compute
    messages=compute._D8_STREAM_DISTANCE_MESSAGES if operator=='raster_distance_to_stream_d8' else compute._D8_STREAM_VECTOR_MESSAGES if operator=='raster_stream_to_vector_d8' else compute._D8_STREAM_LINK_MESSAGES if operator=='raster_stream_link_d8' else compute._D8_SHREVE_MESSAGES if method=='shreve' else compute._D8_STREAM_ORDER_MESSAGES
    paths=_strahler_files(tmp_path,np.array([[1.,0.]]),np.ones((1,2)))
    with raster_workspace(),raster_error_language(language):
        source,mask=[raster_load(source_plan(p)) for p in paths];prior=set(source.workspace.iterdir())
        with pytest.raises(ValueError,match='波段' if language=='zh-cn' else 'band') as error:
            getattr(compute,operator)(source,mask,band if role=='direction' else 1,band if role=='mask' else 1,**({'method':method} if method else {}))
        assert str(error.value)==messages['band' if role=='direction' else 'mask_band'][language=='en']
        assert set(source.workspace.iterdir())==prior




@pytest.mark.parametrize('operator,method',[('raster_stream_order_d8','strahler'),('raster_stream_order_d8','shreve'),('raster_stream_link_d8',None),('raster_stream_to_vector_d8',None),('raster_distance_to_stream_d8',None)])
@pytest.mark.parametrize('case',['code','terminal','outside','invalid','cycle','cycle-with-outlet','break'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_streams_full_graph_and_stream_break_fail_even_off_network(tmp_path,case,language,operator,method):
    import operators.raster_compute as compute
    messages=compute._D8_STREAM_DISTANCE_MESSAGES if operator=='raster_distance_to_stream_d8' else compute._D8_STREAM_VECTOR_MESSAGES if operator=='raster_stream_to_vector_d8' else compute._D8_STREAM_LINK_MESSAGES if operator=='raster_stream_link_d8' else compute._D8_SHREVE_MESSAGES if method=='shreve' else compute._D8_STREAM_ORDER_MESSAGES
    directions=np.array([[1.,1.,0.]])
    if case=='code':directions[0,0]=3
    if case=='terminal':directions=np.zeros((3,3))
    if case=='outside':directions[0,-1]=1
    if case=='invalid':directions[0,1]=np.nan
    if case=='cycle':directions=np.array([[1.,16.]])
    if case=='cycle-with-outlet':directions=np.array([[1.,16.,0.],[64.,64.,0.]])
    streams=np.where(np.isfinite(directions),0.,np.nan)
    if case=='break':streams=np.array([[1.,0.,1.]])
    paths=_strahler_files(tmp_path,directions,streams);before=[p.read_bytes() for p in paths]
    key={'invalid':'target','outside':'target','cycle-with-outlet':'cycle'}.get(case,case)
    with raster_workspace(),raster_error_language(language):
        source,mask=[raster_load(source_plan(p)) for p in paths];prior=set(source.workspace.iterdir())
        with pytest.raises(ValueError) as error:getattr(compute,operator)(source,mask,1,1,**({'method':method} if method else {}))
        assert str(error.value)==messages[key][language=='en']
        assert set(source.workspace.iterdir())==prior
    assert [p.read_bytes() for p in paths]==before




@pytest.mark.parametrize('operator,method',[('raster_stream_order_d8','strahler'),('raster_stream_order_d8','shreve'),('raster_stream_link_d8',None),('raster_stream_to_vector_d8',None),('raster_distance_to_stream_d8',None)])
@pytest.mark.parametrize('bad',[-1,2,255,.25,2**53,np.nan,np.inf])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_streams_nonbinary_mask_and_coverage_mismatch_fail_closed(tmp_path,bad,language,operator,method):
    import operators.raster_compute as compute
    messages=compute._D8_STREAM_DISTANCE_MESSAGES if operator=='raster_distance_to_stream_d8' else compute._D8_STREAM_VECTOR_MESSAGES if operator=='raster_stream_to_vector_d8' else compute._D8_STREAM_LINK_MESSAGES if operator=='raster_stream_link_d8' else compute._D8_SHREVE_MESSAGES if method=='shreve' else compute._D8_STREAM_ORDER_MESSAGES
    paths=_strahler_files(tmp_path,np.array([[1.,0.]]),np.array([[bad,1.]]));key='mask_values' if math.isfinite(bad) else 'coverage'
    with raster_workspace(),raster_error_language(language):
        source,mask=[raster_load(source_plan(p)) for p in paths];prior=set(source.workspace.iterdir())
        with pytest.raises(ValueError) as error:getattr(compute,operator)(source,mask,1,1,**({'method':method} if method else {}))
        assert str(error.value)==messages[key][language=='en']
        assert set(source.workspace.iterdir())==prior




@pytest.mark.parametrize('operator,method',[('raster_stream_order_d8','strahler'),('raster_stream_order_d8','shreve'),('raster_stream_link_d8',None),('raster_stream_to_vector_d8',None),('raster_distance_to_stream_d8',None)])
@pytest.mark.parametrize('role',['direction','mask'])
@pytest.mark.parametrize('case',['scale','offset','alpha','complex','rotated','geographic','no-crs','south-up'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_streams_band_encoding_and_projected_grid_admission(tmp_path,role,case,language,operator,method):
    import operators.raster_compute as compute
    messages=compute._D8_STREAM_DISTANCE_MESSAGES if operator=='raster_distance_to_stream_d8' else compute._D8_STREAM_VECTOR_MESSAGES if operator=='raster_stream_to_vector_d8' else compute._D8_STREAM_LINK_MESSAGES if operator=='raster_stream_link_d8' else compute._D8_SHREVE_MESSAGES if method=='shreve' else compute._D8_STREAM_ORDER_MESSAGES
    paths=_strahler_files(tmp_path,np.array([[1.,0.]]),np.ones((1,2)))
    path=paths[role=='mask'];ds=gdal.GetDriverByName('GTiff').Create(str(path),2,1,1,gdal.GDT_CFloat64 if case=='complex' else gdal.GDT_Float64)
    ds.SetGeoTransform((500000,2,.1 if case=='rotated' else 0,3000000,0,3 if case=='south-up' else -3))
    if case!='no-crs':
        crs=osr.SpatialReference();crs.ImportFromEPSG(4326 if case=='geographic' else 3857);ds.SetProjection(crs.ExportToWkt())
    ds.GetRasterBand(1).WriteArray(np.array([[1,0]]) if role=='direction' else np.ones((1,2)))
    if case=='scale':ds.GetRasterBand(1).SetScale(2)
    if case=='offset':ds.GetRasterBand(1).SetOffset(1)
    if case=='alpha':ds.GetRasterBand(1).SetColorInterpretation(gdal.GCI_AlphaBand)
    ds=None
    key={'scale':'scale','offset':'scale','alpha':'alpha','complex':'complex'}.get(case,'grid')
    if role=='mask':key='alignment' if key=='grid' else 'mask_'+key
    with raster_workspace(),raster_error_language(language):
        source,mask=[raster_load(source_plan(p)) for p in paths];prior=set(source.workspace.iterdir())
        with pytest.raises(ValueError) as error:getattr(compute,operator)(source,mask,1,1,**({'method':method} if method else {}))
        assert str(error.value)==messages[key][language=='en']
        assert set(source.workspace.iterdir())==prior




@pytest.mark.parametrize('operator,method',[('raster_stream_order_d8','strahler'),('raster_stream_order_d8','shreve'),('raster_stream_link_d8',None),('raster_stream_to_vector_d8',None),('raster_distance_to_stream_d8',None)])
@pytest.mark.parametrize('case',['dimensions','origin','resolution','crs','mask_missing','direction_missing'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_streams_requires_exact_grid_and_effective_coverage(tmp_path,case,language,operator,method):
    import operators.raster_compute as compute
    messages=compute._D8_STREAM_DISTANCE_MESSAGES if operator=='raster_distance_to_stream_d8' else compute._D8_STREAM_VECTOR_MESSAGES if operator=='raster_stream_to_vector_d8' else compute._D8_STREAM_LINK_MESSAGES if operator=='raster_stream_link_d8' else compute._D8_SHREVE_MESSAGES if method=='shreve' else compute._D8_STREAM_ORDER_MESSAGES
    directions=np.array([[1.,1.,0.]]);streams=np.ones((1,3))
    if case=='mask_missing':streams[0,1]=np.nan
    if case=='direction_missing':directions[0,0]=np.nan
    paths=_strahler_files(tmp_path,directions,streams)
    if case=='dimensions':create_raster(paths[1],np.ones((1,4)),(500000,2,0,3000000,0,-3),'EPSG:3857')
    elif case in ('origin','resolution','crs'):
        ds=gdal.Open(str(paths[1]),gdal.GA_Update)
        if case=='origin':ds.SetGeoTransform((500001,2,0,3000000,0,-3))
        if case=='resolution':ds.SetGeoTransform((500000,3,0,3000000,0,-3))
        if case=='crs':crs=osr.SpatialReference();crs.ImportFromEPSG(32650);ds.SetProjection(crs.ExportToWkt())
        ds=None
    key='coverage' if case.endswith('missing') else 'alignment'
    with raster_workspace(),raster_error_language(language),pytest.raises(ValueError) as error:
        getattr(compute,operator)(*[raster_load(source_plan(p)) for p in paths],1,1,**({'method':method} if method else {}))
    assert str(error.value)==messages[key][language=='en']




@pytest.mark.parametrize('operator,method',[('raster_stream_order_d8','strahler'),('raster_stream_order_d8','shreve'),('raster_stream_link_d8',None),('raster_stream_to_vector_d8',None),('raster_distance_to_stream_d8',None)])
@pytest.mark.parametrize('role',['direction','mask'])
@pytest.mark.parametrize('dtype',[gdal.GDT_Int64,gdal.GDT_UInt64])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_streams_native_nodata_does_not_hide_adjacent_large_invalid_code(tmp_path,role,dtype,language,operator,method):
    import operators.raster_compute as compute
    messages=compute._D8_STREAM_DISTANCE_MESSAGES if operator=='raster_distance_to_stream_d8' else compute._D8_STREAM_VECTOR_MESSAGES if operator=='raster_stream_to_vector_d8' else compute._D8_STREAM_LINK_MESSAGES if operator=='raster_stream_link_d8' else compute._D8_SHREVE_MESSAGES if method=='shreve' else compute._D8_STREAM_ORDER_MESSAGES
    paths=_strahler_files(tmp_path,np.array([[np.nan,1.,0.]]),np.array([[np.nan,1.,1.]]));path=paths[role=='mask']
    ds=gdal.GetDriverByName('GTiff').Create(str(path),3,1,1,dtype);ds.SetGeoTransform((500000,2,0,3000000,0,-3));crs=osr.SpatialReference();crs.ImportFromEPSG(3857);ds.SetProjection(crs.ExportToWkt())
    band=ds.GetRasterBand(1);array=np.array([[2**53,2**53+1,0 if role=='direction' else 1]],dtype=np.int64 if dtype==gdal.GDT_Int64 else np.uint64);band.WriteArray(array)
    (band.SetNoDataValueAsInt64 if dtype==gdal.GDT_Int64 else band.SetNoDataValueAsUInt64)(2**53);band=ds=None
    with raster_workspace(),raster_error_language(language),pytest.raises(ValueError) as error:
        getattr(compute,operator)(*[raster_load(source_plan(p)) for p in paths],1,1,**({'method':method} if method else {}))
    assert str(error.value)==messages['code' if role=='direction' else 'mask_values'][language=='en']




@pytest.mark.parametrize('operator,method',[('raster_stream_order_d8','strahler'),('raster_stream_order_d8','shreve'),('raster_stream_link_d8',None)])
def test_d8_streams_effective_mask_alpha_nodata_valid_zero_and_selected_bands(tmp_path,operator,method):
    import operators.raster_compute as compute
    messages=compute._D8_STREAM_LINK_MESSAGES if operator=='raster_stream_link_d8' else compute._D8_SHREVE_MESSAGES if method=='shreve' else compute._D8_STREAM_ORDER_MESSAGES
    values=np.zeros((3,7));values[1,:]=[np.inf,-9999,np.nan,255,255,255,0]
    alpha=np.full(values.shape,255.);alpha[1,3]=0;alpha[1,4]=np.nan;alpha[0,0]=128
    mask=np.full(values.shape,255,np.uint8);mask[1,5]=0
    valid=np.isfinite(values)&(values!=-9999)&np.isfinite(alpha)&(alpha>0)&(mask!=0)
    paths=[]
    for label,data in (('direction',values),('mask',np.where(valid,0.,values))):
        path=create_raster(tmp_path/(label+'.tif'),np.stack([np.full(values.shape,3.),data,alpha]),(500000,2,0,3000000,0,-3),'EPSG:3857');ds=gdal.Open(str(path),gdal.GA_Update);ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
        ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);ds=None;paths.append(path)
    with raster_workspace():
        result=getattr(compute,operator)(*[raster_load(source_plan(p)) for p in paths],2,2,**({'method':method} if method else {}));ds=gdal.Open(str(result.path))
        np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),np.where(valid,0.,np.nan));ds=None




@pytest.mark.parametrize('operator,method',[('raster_stream_order_d8','strahler'),('raster_stream_order_d8','shreve'),('raster_stream_link_d8',None)])
@pytest.mark.parametrize('stage',['read','graph','write','metadata'])
def test_d8_streams_injected_failure_cleans_private_outputs(tmp_path,monkeypatch,stage,operator,method):
    import operators.raster_compute as compute
    messages=compute._D8_STREAM_LINK_MESSAGES if operator=='raster_stream_link_d8' else compute._D8_SHREVE_MESSAGES if method=='shreve' else compute._D8_STREAM_ORDER_MESSAGES
    paths=_strahler_files(tmp_path,np.array([[1.,0.]]),np.ones((1,2)));before=[p.read_bytes() for p in paths]
    with raster_workspace():
        source,mask=[raster_load(source_plan(p)) for p in paths];prior=set(source.workspace.iterdir())
        def fail(*args,**kwargs):raise RuntimeError('injected stream-order failure')
        if stage=='read':monkeypatch.setattr(compute,'_blocks',fail)
        if stage=='graph':monkeypatch.setattr(compute,'_d8_stream_graph',fail)
        if stage=='write':monkeypatch.setattr(gdal.Band,'WriteArray',fail)
        if stage=='metadata':monkeypatch.setattr(compute,'_raster',fail)
        with pytest.raises(RuntimeError,match='injected stream-order failure'):getattr(compute,operator)(source,mask,1,1,**({'method':method} if method else {}))
        assert set(source.workspace.iterdir())==prior
    assert [p.read_bytes() for p in paths]==before




def test_d8_strahler_cross_block_cog_full_grades_mask_and_nearest_overviews(tmp_path,monkeypatch):
    import operators.raster_compute as compute
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    height,width=517,519;directions=np.ones((height,width));directions[:,-1]=0
    streams=np.zeros(directions.shape);streams[255,:513]=streams[257,:513]=1;streams[256,512:]=1
    directions[255,512]=4;directions[257,512]=64;directions[510,510]=np.nan;directions[510,509]=0;streams[510,510]=np.nan
    expected=streams.copy();expected[256,512:]=2
    paths=_strahler_files(tmp_path,directions,streams);before=[p.read_bytes() for p in paths];target=tmp_path/'ordered.cog.tif'
    with raster_workspace():
        source,mask=[raster_load(source_plan(p)) for p in paths]
        calls=[];original=gdal.Band.ReadAsArray
        # The source reader must stay within its established 512x512 blocks.
        def read(self,*args,**kwargs):
            if len(args)>=4:calls.append(args[2:4])
            return original(self,*args,**kwargs)
        monkeypatch.setattr(gdal.Band,'ReadAsArray',read)
        result=compute.raster_stream_order_d8(source,mask,1,1)
        assert calls and all(w<=512 and h<=512 for w,h in calls)
        artifact=raster_save(result,target_plan(target),profile='cog',blocksize=128,overview_resampling='nearest')
        assert type(artifact['size_bytes']) is int and artifact['size_bytes']==target.stat().st_size>0
        ds=gdal.Open(str(target));band=ds.GetRasterBand(1)
        np.testing.assert_array_equal(read_band_values(band),expected)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected))
        assert band.GetUnitType()=='' and band.GetOverviewCount()>0
        for index in range(band.GetOverviewCount()):
            values=read_band_values(band.GetOverview(index));assert set(np.unique(values[np.isfinite(values)]))<={0.,1.,2.}
        _,errors,_=validate(ds,full_check=True);assert not errors;band=ds=None
        snapshot=target.read_bytes()
        # A second successful result must preserve already published output bytes.
        compute.raster_stream_order_d8(source,mask,1,1)
        assert target.read_bytes()==snapshot
    assert [p.read_bytes() for p in paths]==before


def test_d8_strahler_duplicate_lower_orders_do_not_upgrade_unique_highest():
    import operators.raster_compute as compute
    directions,streams=_strahler_tree()
    for cell in ((5,1),(5,3),(4,2)):streams[cell]=0
    directions[4,3]=64;streams[4,3]=1
    expected=_strahler_recursive_oracle(directions,streams)
    assert expected[2,2]==2 and expected[2,4]==expected[4,3]==1 and expected[3,3]==2
    np.testing.assert_array_equal(compute._d8_stream_order(directions,streams==1),expected)


def test_d8_strahler_multiple_equal_highest_inflows_increment_once():
    import operators.raster_compute as compute
    directions=np.full((5,6),np.nan)
    for code,(dy,dx) in _STRAHLER_STEPS.items():directions[2-dy,2-dx]=code
    directions[2,2]=1;directions[2,3:5]=1;directions[2,5]=0
    expected=_strahler_recursive_oracle(directions,np.where(np.isfinite(directions),1.,np.nan))
    assert expected[2,2]==2 and expected[2,5]==2
    np.testing.assert_array_equal(compute._d8_stream_order(directions,np.isfinite(directions)),expected)


def test_d8_strahler_long_route_avoids_recursion():
    import operators.raster_compute as compute
    directions=np.ones((2,4097));directions[0,-1]=4;directions[1,:]=16;directions[1,0]=0
    np.testing.assert_array_equal(compute._d8_stream_order(directions,np.ones(directions.shape,bool)),np.ones(directions.shape))


@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_strahler_exact_output_limit_fails_before_publication(tmp_path,monkeypatch,language):
    import operators.raster_compute as compute
    directions=np.array([[2.,8.],[np.nan,0.]]) # Both sources must target the same outlet.
    directions[0,1]=4
    paths=_strahler_files(tmp_path,directions,np.where(np.isfinite(directions),1.,np.nan))
    monkeypatch.setattr(compute,'_D8_MAX_EXACT_COUNT',1)
    with raster_workspace(),raster_error_language(language):
        source,mask=[raster_load(source_plan(p)) for p in paths];prior=set(source.workspace.iterdir())
        with pytest.raises(ValueError,match='精确整数上限' if language=='zh-cn' else 'exact integer limit'):
            compute.raster_stream_order_d8(source,mask,1,1)
        assert set(source.workspace.iterdir())==prior


@pytest.mark.parametrize('operator,method',[('raster_stream_order_d8','strahler'),('raster_stream_order_d8','shreve'),('raster_stream_link_d8',None)])
@pytest.mark.parametrize('epsg',[3857,2222,2277])
def test_d8_streams_linear_unit_independence_preserves_source_crs(tmp_path,epsg,operator,method):
    import operators.raster_compute as compute
    messages=compute._D8_STREAM_LINK_MESSAGES if operator=='raster_stream_link_d8' else compute._D8_SHREVE_MESSAGES if method=='shreve' else compute._D8_STREAM_ORDER_MESSAGES
    directions,streams=_strahler_tree();paths=_strahler_files(tmp_path,directions,streams)
    for path in paths:
        ds=gdal.Open(str(path),gdal.GA_Update);crs=osr.SpatialReference();crs.ImportFromEPSG(epsg);ds.SetProjection(crs.ExportToWkt());ds.GetRasterBand(1).SetUnitType('classification');ds=None
    with raster_workspace():
        result=getattr(compute,operator)(*[raster_load(source_plan(p)) for p in paths],1,1,**({'method':method} if method else {}));ds=gdal.Open(str(result.path));band=ds.GetRasterBand(1)
        np.testing.assert_array_equal(read_band_values(band),(_stream_link_walk_oracle(directions,streams) if operator=='raster_stream_link_d8' else _shreve_headwater_walk_oracle(directions,streams) if method=='shreve' else _strahler_recursive_oracle(directions,streams)))
        assert ds.GetSpatialRef().GetAuthorityCode(None)==str(epsg) and band.GetUnitType()=='';band=ds=None




@pytest.mark.parametrize('method',['strahler','shreve'])
@pytest.mark.parametrize('case',['saved','threshold','dem','filled','empty','break','cycle','coverage','method'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_strahler_real_async_two_input_save_threshold_composition_and_failure_cleanup(tmp_path,monkeypatch,case,language,method):
    import api_server,workflow_engine
    directions=np.ones((9,11));directions[:,-1]=0;streams=np.ones(directions.shape)
    if case=='cycle':directions[0,1]=16;streams[:]=0
    if case=='empty':directions[:]=np.nan;streams[:]=np.nan
    if case=='break':streams[2,5]=0
    if case=='coverage':streams[2,5]=np.nan
    values=np.tile(np.arange(11,0,-1,dtype=float),(9,1))
    if case=='filled':values[:]=10;values[1:-1,1:-1]=-2
    input_values=values if case in ('dem','filled') else directions
    paths=_strahler_files(tmp_path,input_values,streams);before=[p.read_bytes() for p in paths];target=tmp_path/'strahler.cog.tif'
    tasks=[{'id':'source','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(paths[0])}}];prior='source'
    if case=='filled':
        tasks.append({'id':'fill','operator':'raster_fill_depressions','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':1,'elevation_unit':'metre'}});prior='fill'
    if case in ('dem','filled'):
        tasks.append({'id':'direction','operator':'raster_flow_direction_d8','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':1,'elevation_unit':'metre'}});prior='direction'
        directions=_d8_bellman_ford_oracle(_fill_level_reachability_oracle(values) if case=='filled' else values)
    if case=='threshold':
        tasks.extend([{'id':'accumulate','operator':'raster_flow_accumulation_d8','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':1}},
            {'id':'streams','operator':'raster_reclassify','depends_on':['accumulate'],'params':{'input_raster':{'$ref':'accumulate'},'band':1,'rules':[{'min':None,'max':2,'class':0},{'min':2,'max':None,'class':1}]}}])
        streams=np.where(_accumulation_walk_oracle(directions)>=2,1.,0.)
    else:tasks.append({'id':'streams','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(paths[1])}})
    tasks.extend([{'id':'grade','operator':'raster_stream_order_d8','depends_on':[prior,'streams'],'params':{'input_raster':{'$ref':prior},'stream_raster':{'$ref':'streams'},'band':1,'stream_band':1,'method':'invalid' if case=='method' else method}},
        {'id':'save','operator':'raster_save','depends_on':['grade'],'params':{'input_raster':{'$ref':'grade'},'access_plan':target_plan(target),'profile':'cog','blocksize':128,'overview_resampling':'nearest'}}])
    workspaces=[];execute=workflow_engine._execute_operator
    def capture(name,params):
        if name=='raster_stream_order_d8':workspaces.extend([params['input_raster'].workspace,params['stream_raster'].workspace])
        return execute(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',capture)
    client=api_server.app.test_client();response=client.post('/api/workflow',headers={'Accept-Language':language},json={'workflow_def':{'tasks':tasks},'input_data':{},'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert response.status_code==202,response.json
    deadline=time.monotonic()+15
    while time.monotonic()<deadline:
        status=client.get('/api/executions/'+response.json['execution_id'],headers={'Accept-Language':'en' if language=='zh-cn' else 'zh-cn'}).json
        if status['status'] in ('success','failed'):break
        time.sleep(.01)
    failed=case in ('break','cycle','coverage','method');assert status['status']==('failed' if failed else 'success'),status
    assert workspaces and all(not p.exists() for p in workspaces) and [p.read_bytes() for p in paths]==before
    if failed:
        assert not target.exists()
        import operators.raster_compute as compute
        assert '任务 grade 执行失败: '+(compute._D8_SHREVE_MESSAGES if method=='shreve' else compute._D8_STREAM_ORDER_MESSAGES)[case][language=='en'] in status['error']
    else:
        ds=gdal.Open(str(target));np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),(_shreve_headwater_walk_oracle(directions,streams) if method=='shreve' else _strahler_recursive_oracle(directions,streams)))
        assert ds.GetRasterBand(1).GetUnitType()=='';ds=None
        with raster_workspace():assert validate_cog(raster_load(source_plan(target)))['valid']
        artifact=json.loads(status['result']);assert type(artifact['size_bytes']) is int and artifact['size_bytes']==target.stat().st_size>0
    assert client.post('/api/operators/raster_stream_order_d8/invoke',json={'params':{}}).status_code==403


@pytest.mark.parametrize('operator',['raster_stream_order_d8','raster_stream_link_d8'])
def test_d8_streams_discovery_single_route_and_two_required_raster_inputs(operator):
    spec=next(item for item in list_operators() if item['id']==operator)
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read'] and spec['attributes']['resource_groups']==['raster']
    assert spec['category_path']==['栅格处理','像元计算']
    params={p['name']:p for p in spec['parameters']}
    required={'input_raster','stream_raster','band','stream_band'}
    assert set(params)==required|({'method'} if operator=='raster_stream_order_d8' else set())
    assert all(params[p]['required'] for p in required)
    if operator=='raster_stream_order_d8':assert params['method']['required'] is False and params['method']['default']=='strahler' and params['method']['enum']==['strahler','shreve']
    assert all(params[p]['type']=='raster' and params[p]['param_type']=='input' for p in ('input_raster','stream_raster'))
    assert spec['detailed_description']['workflow_example']['params']=={'input_raster':{'$ref':'source'},'stream_raster':{'$ref':'streams'},'band':1,'stream_band':1,**({'method':'strahler'} if operator=='raster_stream_order_d8' else {})}



# D8 weighted accumulation: independent per-origin walks, separate from the
# production indegree/topological propagation and its intermediate sums.
def _weighted_walk_oracle(directions, weights):
    offsets = {1:(0,1),2:(1,1),4:(1,0),8:(1,-1),16:(0,-1),32:(-1,-1),64:(-1,0),128:(-1,1)}
    valid = np.isfinite(directions)
    result = np.where(valid, 0., np.nan)
    for start_row, start_col in np.argwhere(valid):
        row, col = int(start_row), int(start_col)
        visited = {(row, col)}
        weight = float(weights[row, col])
        while directions[row, col] != 0:
            dy, dx = offsets[directions[row, col]]
            row, col = row+dy, col+dx
            assert 0 <= row < directions.shape[0] and 0 <= col < directions.shape[1]
            assert valid[row,col] and (row,col) not in visited
            visited.add((row,col)); result[row,col] += weight
    outlets = directions == 0
    assert math.fsum(map(float,result[outlets])) + math.fsum(map(float,weights[outlets])) == pytest.approx(math.fsum(map(float,weights[valid])),rel=1e-12,abs=1e-12)
    return result


def _weighted_files(tmp_path, directions, weights, unit='kg'):
    transform = (500000,2,0,3000000,0,-3)
    paths = (create_raster(tmp_path/'directions.tif',directions,transform,'EPSG:3857'),
             create_raster(tmp_path/'weights.tif',weights,transform,'EPSG:3857'))
    ds=gdal.Open(str(paths[1]),gdal.GA_Update);ds.GetRasterBand(1).SetUnitType(unit);ds=None
    return paths


@pytest.mark.parametrize('seed',range(12))
@pytest.mark.parametrize('unit',['','kg','foot'])
def test_d8_weighted_random_forest_complete_oracle_units_and_ones(tmp_path,seed,unit):
    import operators.raster_compute as compute
    rng=np.random.default_rng(seed);directions=np.zeros((9,11));rows,cols=np.indices(directions.shape)
    ranks=np.minimum.reduce((rows,cols,8-rows,10-cols))
    for row,col in np.argwhere(ranks>0):
        choices=[code for code,(dy,dx) in _STRAHLER_STEPS.items() if ranks[row+dy,col+dx]<ranks[row,col]]
        directions[row,col]=rng.choice(choices)
    directions[4,5]=np.nan
    for row,col in np.argwhere(np.isfinite(directions)&(directions!=0)):
        dy,dx=_STRAHLER_STEPS[directions[row,col]]
        if not np.isfinite(directions[row+dy,col+dx]):directions[row,col]=0
    weights=rng.integers(0,20,size=directions.shape).astype(float)/10;weights[~np.isfinite(directions)]=np.nan
    paths=_weighted_files(tmp_path,directions,weights,unit);before=[p.read_bytes() for p in paths]
    with raster_workspace():
        source,values=[raster_load(source_plan(p)) for p in paths];prior=set(source.workspace.iterdir())
        result=compute.raster_flow_accumulation_weighted_d8(source,values,1,1)
        ds=gdal.Open(str(result.path));band=ds.GetRasterBand(1)
        np.testing.assert_allclose(read_band_values(band),_weighted_walk_oracle(directions,weights),rtol=1e-13,atol=1e-13,equal_nan=True)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(directions))
        assert band.DataType==gdal.GDT_Float64 and math.isnan(band.GetNoDataValue())
        assert band.GetUnitType()==unit and band.GetScale() is None and band.GetOffset() is None and band.GetColorTable() is None
        assert ds.GetGeoTransform()==(500000.,2.,0.,3000000.,0.,-3.) and ds.GetSpatialRef().GetAuthorityCode(None)=='3857'
        assert set(source.workspace.iterdir())==prior|{result.path};band=ds=None
    ones=np.where(np.isfinite(directions),1.,np.nan)
    np.testing.assert_array_equal(compute._d8_weighted_accumulation(directions,ones),compute._d8_accumulation(directions))
    assert [p.read_bytes() for p in paths]==before


@pytest.mark.parametrize('dy,dx,code',[(dy,dx,code) for code,(dy,dx) in _STRAHLER_STEPS.items()])
def test_d8_weighted_eight_routes_excludes_own_and_counts_zero(dy,dx,code):
    import operators.raster_compute as compute
    directions=np.full((3,3),np.nan);directions[1,1]=code;directions[1+dy,1+dx]=0
    weights=np.where(np.isfinite(directions),5.,np.nan);weights[1,1]=0
    np.testing.assert_array_equal(compute._d8_weighted_accumulation(directions,weights),_weighted_walk_oracle(directions,weights))
    weights[1,1]=2.5;actual=compute._d8_weighted_accumulation(directions,weights)
    assert actual[1,1]==0 and actual[1+dy,1+dx]==2.5


@pytest.mark.parametrize('weights',[[2.,3.,5.],[0.,0.,0.],[1.,1e308,0.],[0.,0.,1.7e308]])
def test_d8_weighted_chain_no_total_minus_self_cancellation(weights):
    import operators.raster_compute as compute
    actual=compute._d8_weighted_accumulation(np.array([[1.,1.,0.]]),np.array([weights]))
    np.testing.assert_array_equal(actual,np.array([[0.,weights[0],weights[0]+weights[1]]]))


def test_d8_weighted_cross_blocks_holes_all_invalid_and_real_cog(tmp_path):
    import operators.raster_compute as compute
    directions=np.full((515,519),np.nan);directions[508:]=1;directions[508:,-1]=0
    directions[510,510]=np.nan;directions[510,509]=0
    rows,cols=np.indices(directions.shape);weights=np.where(np.isfinite(directions),(rows+cols)%13/8,np.nan)
    paths=_weighted_files(tmp_path,directions,weights);before=[p.read_bytes() for p in paths];target=tmp_path/'weighted.cog.tif'
    expected=_weighted_walk_oracle(directions,weights)
    with raster_workspace():
        result=compute.raster_flow_accumulation_weighted_d8(*[raster_load(source_plan(p)) for p in paths],1,1)
        artifact=raster_save(result,target_plan(target),profile='cog',blocksize=128,overview_resampling='nearest')
        assert type(artifact['size_bytes']) is int and artifact['size_bytes']==target.stat().st_size>0
        assert validate_cog(raster_load(source_plan(target)))['valid']
    ds=gdal.Open(str(target));np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),expected)
    np.testing.assert_array_equal(ds.GetRasterBand(1).GetMaskBand().ReadAsArray()!=0,np.isfinite(expected))
    assert ds.GetRasterBand(1).GetUnitType()=='kg';ds=None
    assert [p.read_bytes() for p in paths]==before
    empty=np.full((3,4),np.nan)
    paths=_weighted_files(tmp_path,empty,empty)
    with raster_workspace():
        result=compute.raster_flow_accumulation_weighted_d8(*[raster_load(source_plan(p)) for p in paths],1,1)
        ds=gdal.Open(str(result.path));assert np.isnan(read_band_values(ds.GetRasterBand(1))).all();assert not ds.GetRasterBand(1).GetMaskBand().ReadAsArray().any();ds=None


@pytest.mark.parametrize('role',['direction','weights'])
@pytest.mark.parametrize('band',[True,False,0,-1,2,1.5,'1',None])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_weighted_explicit_both_bands_bilingual(tmp_path,role,band,language):
    import operators.raster_compute as compute
    paths=_weighted_files(tmp_path,np.array([[1.,0.]]),np.ones((1,2)))
    with raster_workspace(),raster_error_language(language):
        source,weights=[raster_load(source_plan(p)) for p in paths];prior=set(source.workspace.iterdir())
        with pytest.raises(ValueError,match='波段' if language=='zh-cn' else 'band'):
            compute.raster_flow_accumulation_weighted_d8(source,weights,band if role=='direction' else 1,band if role=='weights' else 1)
        assert set(source.workspace.iterdir())==prior


@pytest.mark.parametrize('change',['size','origin','pixel','rotation','crs'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_weighted_complete_grid_mismatch_rejected(tmp_path,change,language):
    import operators.raster_compute as compute
    paths=_weighted_files(tmp_path,np.array([[1.,0.]]),np.ones((1,3) if change=='size' else (1,2)))
    ds=gdal.Open(str(paths[1]),gdal.GA_Update);transform=list(ds.GetGeoTransform())
    if change in ('origin','pixel','rotation'):
        index={'origin':0,'pixel':1,'rotation':2}[change];transform[index]+=.001;ds.SetGeoTransform(transform)
    if change=='crs':srs=osr.SpatialReference();srs.ImportFromEPSG(32650);ds.SetProjection(srs.ExportToWkt())
    ds=None
    with raster_workspace(),raster_error_language(language),pytest.raises(ValueError,match='显式对齐' if language=='zh-cn' else 'align explicitly'):
        compute.raster_flow_accumulation_weighted_d8(*[raster_load(source_plan(p)) for p in paths],1,1)


@pytest.mark.parametrize('invalid',['nan','inf','negative_inf','nodata','mask','alpha','extra_weight','missing_weight'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_weighted_exact_coverage_no_missing_weight_as_zero(tmp_path,invalid,language):
    import operators.raster_compute as compute
    directions=np.array([[1.,0.]]);weights=np.ones((1,2))
    if invalid in ('extra_weight','missing_weight'):directions[0,0]=np.nan
    if invalid=='missing_weight':weights[:]=np.nan
    if invalid in ('nan','inf','negative_inf'):weights[0,0]={'nan':np.nan,'inf':np.inf,'negative_inf':-np.inf}[invalid]
    if invalid=='nodata':weights[0,0]=-99
    paths=_weighted_files(tmp_path,directions,weights)
    if invalid in ('nodata','mask','alpha'):
        ds=gdal.Open(str(paths[1]),gdal.GA_Update);band=ds.GetRasterBand(1)
        if invalid=='nodata':band.SetNoDataValue(-99)
        if invalid=='mask':
            # GDAL <=3.8 defaults to an external .msk, outside the single-file input contract.
            with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):
                assert band.CreateMaskBand(gdal.GMF_PER_DATASET)==gdal.CE_None
            band.GetMaskBand().WriteArray(np.array([[0,255]],dtype=np.uint8))
        band=ds=None
        if invalid=='mask':
            ds=gdal.Open(str(paths[1]))
            assert ds.GetFileList()==[str(paths[1])]
            np.testing.assert_array_equal(ds.GetRasterBand(1).GetMaskBand().ReadAsArray(),[[0,255]])
            ds=None
        if invalid=='alpha':
            path=paths[1];ds=gdal.GetDriverByName('GTiff').Create(str(path),2,1,2,gdal.GDT_Float64)
            ds.SetGeoTransform((500000,2,0,3000000,0,-3));srs=osr.SpatialReference();srs.ImportFromEPSG(3857);ds.SetProjection(srs.ExportToWkt())
            ds.GetRasterBand(1).WriteArray(weights);ds.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand);ds.GetRasterBand(2).WriteArray(np.array([[0.,255.]]));ds=None
    with raster_workspace(),raster_error_language(language):
        inputs=[raster_load(source_plan(p)) for p in paths];prior=set(inputs[0].workspace.iterdir())
        with pytest.raises(ValueError,match='有效覆盖' if language=='zh-cn' else 'effective coverage'):compute.raster_flow_accumulation_weighted_d8(*inputs,1,1)
        assert set(inputs[0].workspace.iterdir())==prior


@pytest.mark.parametrize('mask_storage',['YES','NO'],ids=['internal','external'])
def test_d8_weighted_mask_storage_respects_single_file_admission(tmp_path,mask_storage):
    import operators.raster_compute as compute
    paths=_weighted_files(tmp_path,np.array([[1.,0.]]),np.ones((1,2)))
    ds=gdal.Open(str(paths[1]),gdal.GA_Update)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK',mask_storage):
        assert ds.CreateMaskBand(gdal.GMF_PER_DATASET)==gdal.CE_None
    ds.GetRasterBand(1).GetMaskBand().WriteArray(np.array([[0,255]],dtype=np.uint8));ds=None
    ds=gdal.Open(str(paths[1]));files=set(ds.GetFileList())
    assert files=={str(paths[1])}|({str(paths[1])+'.msk'} if mask_storage=='NO' else set())
    np.testing.assert_array_equal(ds.GetRasterBand(1).GetMaskBand().ReadAsArray(),[[0,255]]);ds=None
    original={name:Path(name).read_bytes() for name in files}
    with raster_workspace():
        source=raster_load(source_plan(paths[0]));prior=set(source.workspace.iterdir())
        if mask_storage=='NO':
            with pytest.raises(ValueError,match='self-contained; external sidecars'):
                raster_load(source_plan(paths[1]))
        else:
            weights=raster_load(source_plan(paths[1]));prior=set(source.workspace.iterdir())
            with raster_error_language('en'),pytest.raises(ValueError,match='effective coverage'):
                compute.raster_flow_accumulation_weighted_d8(source,weights,1,1)
        assert set(source.workspace.iterdir())==prior
    assert {name:Path(name).read_bytes() for name in files}==original


@pytest.mark.parametrize('role',['direction','weights'])
@pytest.mark.parametrize('change',['alpha','complex','complex_alpha','scale','offset'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_weighted_data_admission_rejects_implicit_transforms(tmp_path,role,change,language):
    import operators.raster_compute as compute
    paths=_weighted_files(tmp_path,np.array([[1.,0.]]),np.ones((1,2)));path=paths[role=='weights']
    ds=gdal.Open(str(path),gdal.GA_Update);band=ds.GetRasterBand(1)
    if change=='alpha':band.SetColorInterpretation(gdal.GCI_AlphaBand)
    if change=='scale':band.SetScale(2)
    if change=='offset':band.SetOffset(1)
    band=ds=None
    if change in ('complex','complex_alpha'):
        ds=gdal.GetDriverByName('GTiff').Create(str(path),2,1,2 if change=='complex_alpha' else 1,gdal.GDT_CFloat64)
        ds.SetGeoTransform((500000,2,0,3000000,0,-3));srs=osr.SpatialReference();srs.ImportFromEPSG(3857);ds.SetProjection(srs.ExportToWkt());ds.GetRasterBand(1).WriteArray(np.array([[1+0j,0+0j]]))
        if change=='complex_alpha':ds.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand);ds.GetRasterBand(2).Fill(255)
        ds=None
    with raster_workspace(),raster_error_language(language):
        inputs=[raster_load(source_plan(p)) for p in paths]
        with pytest.raises(ValueError,match={'alpha':'alpha','complex':'复数' if language=='zh-cn' else 'complex','complex_alpha':'复数' if language=='zh-cn' else 'complex','scale':'scale/offset','offset':'scale/offset'}[change]):compute.raster_flow_accumulation_weighted_d8(*inputs,1,1)


@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('case',['code','terminal','outside','invalid_target','cycle','negative','overflow'])
def test_d8_weighted_graph_and_weights_fail_before_output(tmp_path,language,case):
    import operators.raster_compute as compute
    directions=np.array([[1.,1.,0.]]);weights=np.ones((1,3))
    if case=='code':directions[0,0]=3
    if case=='terminal':directions=np.ones((3,3));directions[:,-1]=0;directions[1,1]=0;weights=np.ones((3,3))
    if case=='outside':directions[0,-1]=1
    if case=='invalid_target':directions[0,1]=np.nan;weights[0,1]=np.nan
    if case=='cycle':directions[0,1]=16
    if case=='negative':weights[0,1]=-1
    if case=='overflow':weights[0,:2]=1.7e308
    paths=_weighted_files(tmp_path,directions,weights)
    key='target' if case in ('outside','invalid_target') else case
    with raster_workspace(),raster_error_language(language):
        inputs=[raster_load(source_plan(p)) for p in paths];prior=set(inputs[0].workspace.iterdir())
        with pytest.raises(ValueError) as caught:compute.raster_flow_accumulation_weighted_d8(*inputs,1,1)
        assert str(caught.value)==compute._D8_WEIGHTED_MESSAGES[key][language=='en']
        assert set(inputs[0].workspace.iterdir())==prior


@pytest.mark.parametrize('dtype,nodata,valid',[('UInt64',2**53,2**53+1),('Int64',-(2**53),-(2**53)-1)])
@pytest.mark.parametrize('role',['direction','weights'])
def test_d8_weighted_native_64bit_nodata_adjacent_value_not_erased(tmp_path,dtype,nodata,valid,role):
    import operators.raster_compute as compute
    directions=np.array([[0.,np.nan]]);weights=np.array([[1.,np.nan]])
    if role=='direction':directions=np.array([[1.,0.]]) ;weights=np.ones((1,2))
    paths=_weighted_files(tmp_path,directions,weights);path=paths[role=='weights']
    ds=gdal.GetDriverByName('GTiff').Create(str(path),2,1,1,getattr(gdal,'GDT_'+dtype))
    ds.SetGeoTransform((500000,2,0,3000000,0,-3));srs=osr.SpatialReference();srs.ImportFromEPSG(3857);ds.SetProjection(srs.ExportToWkt())
    band=ds.GetRasterBand(1);band.SetNoDataValue(nodata);band.WriteArray(np.array([[valid,nodata]],dtype=np.uint64 if dtype=='UInt64' else np.int64));band=ds=None
    with raster_workspace():
        inputs=[raster_load(source_plan(p)) for p in paths]
        if role=='direction' or dtype=='Int64':
            with pytest.raises(ValueError,match='非法方向码' if role=='direction' else '非负'):compute.raster_flow_accumulation_weighted_d8(*inputs,1,1)
        else:
            result=compute.raster_flow_accumulation_weighted_d8(*inputs,1,1);ds=gdal.Open(str(result.path));np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),np.array([[0.,np.nan]]));ds=None


def test_d8_weighted_explicit_multiband_same_file_partial_alpha_and_cleanup(tmp_path,monkeypatch):
    import operators.raster_compute as compute
    path=tmp_path/'multi.tif';ds=gdal.GetDriverByName('GTiff').Create(str(path),3,1,4,gdal.GDT_Float64)
    ds.SetGeoTransform((500000,2,0,3000000,0,-3));srs=osr.SpatialReference();srs.ImportFromEPSG(3857);ds.SetProjection(srs.ExportToWkt())
    for i,values in enumerate(([99.,99.,99.],[1.,1.,0.],[2.,3.,5.],[128.,128.,128.]),1):ds.GetRasterBand(i).WriteArray(np.array([values]))
    ds.GetRasterBand(3).SetUnitType('m3');ds.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand);ds=None;before=path.read_bytes()
    with raster_workspace():
        source=raster_load(source_plan(path));result=compute.raster_flow_accumulation_weighted_d8(source,source,2,3)
        ds=gdal.Open(str(result.path));np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),[[0.,2.,5.]]);assert ds.GetRasterBand(1).GetUnitType()=='m3';ds=None
        prior=set(source.workspace.iterdir());real=compute._raster
        def fail(path):raise RuntimeError('injected weighted wrap failure')
        monkeypatch.setattr(compute,'_raster',fail)
        with pytest.raises(RuntimeError,match='injected weighted wrap failure'):compute.raster_flow_accumulation_weighted_d8(source,source,2,3)
        assert set(source.workspace.iterdir())==prior;monkeypatch.setattr(compute,'_raster',real)
    assert path.read_bytes()==before


@pytest.mark.parametrize('case',['direct','dem','filled','reclass','empty','negative','coverage','cycle','overflow'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_weighted_async_two_input_cog_composition_and_cleanup(tmp_path,monkeypatch,case,language):
    import api_server,workflow_engine
    import operators.raster_compute as compute
    directions=np.ones((9,11));directions[:,-1]=0;weights=np.tile(np.arange(11,dtype=float)/4,(9,1))
    if case=='cycle':directions[0,1]=16
    if case=='empty':directions[:]=np.nan;weights[:]=np.nan
    if case=='negative':weights[0,0]=-1
    if case=='coverage':weights[0,0]=np.nan
    if case=='overflow':weights[:]=1.7e308
    values=np.tile(np.arange(11,0,-1,dtype=float),(9,1))
    if case=='filled':values[:]=10;values[1:-1,1:-1]=-2
    input_values=values if case in ('dem','filled') else directions
    paths=_weighted_files(tmp_path,input_values,weights,'kg');before=[p.read_bytes() for p in paths];target=tmp_path/'weighted.cog.tif'
    tasks=[{'id':'source','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(paths[0])}},
           {'id':'weights','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(paths[1])}}];prior='source';weight_node='weights'
    if case=='filled':tasks.append({'id':'fill','operator':'raster_fill_depressions','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':1,'elevation_unit':'metre'}});prior='fill'
    if case in ('dem','filled'):
        tasks.append({'id':'direction','operator':'raster_flow_direction_d8','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':1,'elevation_unit':'metre'}});prior='direction'
        directions=_d8_bellman_ford_oracle(_fill_level_reachability_oracle(values) if case=='filled' else values)
    if case=='reclass':
        tasks.append({'id':'classes','operator':'raster_reclassify','depends_on':['weights'],'params':{'input_raster':{'$ref':'weights'},'band':1,'rules':[{'min':None,'max':1,'class':0},{'min':1,'max':None,'class':2}]}});weight_node='classes';weights=np.where(weights<1,0.,2.)
    tasks.extend([{'id':'weighted','operator':'raster_flow_accumulation_weighted_d8','depends_on':[prior,weight_node],'params':{'input_raster':{'$ref':prior},'weights_raster':{'$ref':weight_node},'band':1,'weights_band':1}},
        {'id':'save','operator':'raster_save','depends_on':['weighted'],'params':{'input_raster':{'$ref':'weighted'},'access_plan':target_plan(target),'profile':'cog','blocksize':128,'overview_resampling':'nearest'}}])
    workspaces=[];execute=workflow_engine._execute_operator
    def capture(name,params):
        if name=='raster_flow_accumulation_weighted_d8':workspaces.extend([params['input_raster'].workspace,params['weights_raster'].workspace])
        return execute(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',capture)
    client=api_server.app.test_client();response=client.post('/api/workflow',headers={'Accept-Language':language},json={'workflow_def':{'tasks':tasks},'input_data':{},'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert response.status_code==202,response.json
    deadline=time.monotonic()+15
    while time.monotonic()<deadline:
        status=client.get('/api/executions/'+response.json['execution_id'],headers={'Accept-Language':'en' if language=='zh-cn' else 'zh-cn'}).json
        if status['status'] in ('success','failed'):break
        time.sleep(.01)
    failed=case in ('negative','coverage','cycle','overflow');assert status['status']==('failed' if failed else 'success'),status
    assert workspaces and all(not p.exists() for p in workspaces) and [p.read_bytes() for p in paths]==before
    if failed:
        assert not target.exists();assert '任务 weighted 执行失败: '+compute._D8_WEIGHTED_MESSAGES[case][language=='en'] in status['error']
    else:
        ds=gdal.Open(str(target));np.testing.assert_allclose(read_band_values(ds.GetRasterBand(1)),_weighted_walk_oracle(directions,weights),rtol=1e-13,atol=1e-13,equal_nan=True)
        np.testing.assert_array_equal(ds.GetRasterBand(1).GetMaskBand().ReadAsArray()!=0,np.isfinite(directions))
        assert ds.GetRasterBand(1).GetUnitType()==('' if case=='reclass' else 'kg');ds=None
        with raster_workspace():assert validate_cog(raster_load(source_plan(target)))['valid']
        artifact=json.loads(status['result']);assert type(artifact['size_bytes']) is int and artifact['size_bytes']==target.stat().st_size>0
    assert client.post('/api/operators/raster_flow_accumulation_weighted_d8/invoke',json={'params':{}}).status_code==403


def test_d8_weighted_discovery_two_inputs_required_and_single_route():
    spec=next(item for item in list_operators() if item['id']=='raster_flow_accumulation_weighted_d8')
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read'] and spec['attributes']['resource_groups']==['raster']
    assert spec['category_path']==['栅格处理','像元计算']
    params={p['name']:p for p in spec['parameters']}
    assert set(params)=={'input_raster','weights_raster','band','weights_band'} and all(p['required'] for p in params.values())
    assert all(params[p]['type']=='raster' and params[p]['param_type']=='input' for p in ('input_raster','weights_raster'))
    assert spec['detailed_description']['workflow_example']['params']=={'input_raster':{'$ref':'source'},'weights_raster':{'$ref':'weights'},'band':1,'weights_band':1}


def _point_catchment_messages(compute, operator):
    return compute._D8_WATERSHED_MESSAGES if operator == 'raster_watershed_d8' else compute._D8_SUBCATCHMENTS_MESSAGES


# Independent per-origin walks: no production graph, reverse ordering or labels.
def _watershed_walk_oracle(directions, points):
    steps={1:(0,1),2:(1,1),4:(1,0),8:(1,-1),16:(0,-1),32:(-1,-1),64:(-1,0),128:(-1,1)}
    valid=np.isfinite(directions);result=np.where(valid,0.,np.nan)
    for row,col in np.argwhere(valid):
        y,x=int(row),int(col);seen=set()
        while True:
            assert (y,x) not in seen and valid[y,x];seen.add((y,x))
            if points[y,x]>0:result[row,col]=points[y,x];break
            if directions[y,x]==0:break
            dy,dx=steps[directions[y,x]];y,x=y+dy,x+dx
            assert 0<=y<directions.shape[0] and 0<=x<directions.shape[1]
    return result


def _watershed_files(tmp_path,directions,points):
    return tuple(create_raster(tmp_path/(name+'.tif'),values,(500000,2,0,3000000,0,-3),'EPSG:3857')
                 for name,values in (('directions',directions),('points',points)))


@pytest.mark.parametrize('seed',range(12))
def test_d8_watershed_random_forest_complete_walk_oracle(tmp_path,seed):
    import operators.raster_compute as compute
    rng=np.random.default_rng(seed);directions=np.zeros((9,11));rows,cols=np.indices(directions.shape)
    rank=np.minimum.reduce((rows,cols,8-rows,10-cols))
    for row,col in np.argwhere(rank>0):
        candidates=[code for code,(dy,dx) in _STRAHLER_STEPS.items() if rank[row+dy,col+dx]<rank[row,col]]
        directions[row,col]=rng.choice(candidates)
    directions[4,5]=np.nan
    for row,col in np.argwhere(np.isfinite(directions)&(directions!=0)):
        dy,dx=_STRAHLER_STEPS[directions[row,col]]
        if not np.isfinite(directions[row+dy,col+dx]):directions[row,col]=0
    points=np.where(np.isfinite(directions),0.,np.nan)
    outlets=np.argwhere(directions==0);chosen=rng.choice(len(outlets),size=5,replace=False)
    for label,index in zip((900,7,2**53-1,83,2),chosen):points[tuple(outlets[index])]=label
    paths=_watershed_files(tmp_path,directions,points);before=[p.read_bytes() for p in paths]
    with raster_workspace():
        result=compute.raster_watershed_d8(*[raster_load(source_plan(p)) for p in paths],1,1)
        ds=gdal.Open(str(result.path));actual=read_band_values(ds.GetRasterBand(1))
        np.testing.assert_array_equal(actual,_watershed_walk_oracle(directions,points))
        np.testing.assert_array_equal(ds.GetRasterBand(1).GetMaskBand().ReadAsArray()!=0,np.isfinite(directions))
        for y,x in np.argwhere(points>0):assert actual[y,x]==points[y,x]
        assert set(actual[np.isfinite(actual)])<=set([0.,900.,7.,float(2**53-1),83.,2.]);ds=None
    assert [p.read_bytes() for p in paths]==before


def test_d8_watershed_intermediate_points_share_final_outlet_without_overlap():
    import operators.raster_compute as compute
    directions=np.full((3,5),np.nan);directions[0,0]=2;directions[1,1]=2;directions[2,:]=[1,1,1,1,0]
    points=np.where(np.isfinite(directions),0.,np.nan);points[1,1]=900;points[2,1]=7
    expected=np.full((3,5),np.nan);expected[0,0]=expected[1,1]=900;expected[2,:]=[7,7,0,0,0]
    np.testing.assert_array_equal(compute._d8_watershed(directions,points),expected)


@pytest.mark.parametrize('dy,dx,code',[(dy,dx,code) for code,(dy,dx) in _STRAHLER_STEPS.items()])
def test_d8_watershed_all_eight_directions_includes_selected_cell(dy,dx,code):
    import operators.raster_compute as compute
    directions=np.full((3,3),np.nan);directions[1,1]=code;directions[1+dy,1+dx]=0
    points=np.where(np.isfinite(directions),0.,np.nan);points[1+dy,1+dx]=2**53-1
    np.testing.assert_array_equal(compute._d8_watershed(directions,points),np.where(np.isfinite(directions),float(2**53-1),np.nan))


@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('case',['nested','nested_gap','duplicate','empty','all_invalid','negative','fraction','too_large','coverage','code','terminal','outside','invalid_target','cycle'])
def test_d8_watershed_invalid_points_and_complete_graph_fail_before_output(tmp_path,language,case):
    import operators.raster_compute as compute
    directions=np.array([[1.,1.,1.,0.]]);points=np.array([[0.,777.,0.,0.]])
    if case=='nested':points[0,2]=9
    if case=='nested_gap':points[0,3]=9
    if case=='duplicate':directions=np.array([[0.,0.,0.,0.]]);points[0,3]=777
    if case=='empty':points[:]=0
    if case=='all_invalid':directions[:]=np.nan;points[:]=np.nan
    if case=='negative':points[0,0]=-1
    if case=='fraction':points[0,0]=.5
    if case=='too_large':points[0,0]=2**53
    if case=='coverage':points[0,3]=np.nan
    if case=='code':directions[0,2]=3
    if case=='terminal':directions=np.ones((3,4));directions[:,-1]=0;directions[1,1]=0;points=np.zeros((3,4));points[0,0]=777
    if case=='outside':directions[0,3]=1
    if case=='invalid_target':directions[0,2]=np.nan;points[0,2]=np.nan
    if case=='cycle':directions[0,3]=16
    key={'nested_gap':'nested','all_invalid':'empty','negative':'points_values','fraction':'points_values','too_large':'points_values','outside':'target','invalid_target':'target'}.get(case,case)
    paths=_watershed_files(tmp_path,directions,points)
    with raster_workspace(),raster_error_language(language):
        inputs=[raster_load(source_plan(p)) for p in paths];prior=set(inputs[0].workspace.iterdir())
        with pytest.raises(ValueError) as caught:compute.raster_watershed_d8(*inputs,1,1)
        assert str(caught.value)==compute._D8_WATERSHED_MESSAGES[key][language=='en']
        assert set(inputs[0].workspace.iterdir())==prior


@pytest.mark.parametrize('role',['direction','points'])
@pytest.mark.parametrize('band',[None,0,-1,True,1.,'1',2])
@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('operator',['raster_watershed_d8','raster_subcatchments_d8'])
def test_d8_watershed_both_bands_explicit_bilingual(tmp_path,role,band,language,operator):
    import operators.raster_compute as compute
    paths=_watershed_files(tmp_path,np.array([[1.,0.]]),np.array([[0.,42.]]))
    with raster_workspace(),raster_error_language(language):
        inputs=[raster_load(source_plan(p)) for p in paths]
        with pytest.raises(ValueError) as caught:getattr(compute,operator)(*inputs,band if role=='direction' else 1,band if role=='points' else 1)
        assert str(caught.value)==_point_catchment_messages(compute,operator)['band' if role=='direction' else 'points_band'][language=='en']


@pytest.mark.parametrize('change',['dimensions','origin','resolution','crs','rotation'])
@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('operator',['raster_watershed_d8','raster_subcatchments_d8'])
def test_d8_watershed_full_grid_must_match(tmp_path,change,language,operator):
    import operators.raster_compute as compute
    paths=_watershed_files(tmp_path,np.array([[1.,0.]]),np.array([[0.,42.]]))
    if change=='dimensions':create_raster(paths[1],np.array([[0.,0.,42.]]),(500000,2,0,3000000,0,-3),'EPSG:3857')
    else:
        ds=gdal.Open(str(paths[1]),gdal.GA_Update)
        if change=='origin':ds.SetGeoTransform((500001,2,0,3000000,0,-3))
        if change=='resolution':ds.SetGeoTransform((500000,3,0,3000000,0,-3))
        if change=='rotation':ds.SetGeoTransform((500000,2,.1,3000000,0,-3))
        if change=='crs':srs=osr.SpatialReference();srs.ImportFromEPSG(32650);ds.SetProjection(srs.ExportToWkt())
        ds=None
    with raster_workspace(),raster_error_language(language),pytest.raises(ValueError) as caught:
        getattr(compute,operator)(*[raster_load(source_plan(p)) for p in paths],1,1)
    assert str(caught.value)==_point_catchment_messages(compute,operator)['alignment'][language=='en']


@pytest.mark.parametrize('role',['direction','points'])
@pytest.mark.parametrize('change',['alpha','complex','complex_alpha','scale','offset','geographic','no_crs','south_up','rotation'])
@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('operator',['raster_watershed_d8','raster_subcatchments_d8'])
def test_d8_watershed_admission_no_implicit_transform(tmp_path,role,change,language,operator):
    import operators.raster_compute as compute
    paths=_watershed_files(tmp_path,np.array([[1.,0.]]),np.array([[0.,42.]]));path=paths[role=='points']
    ds=gdal.Open(str(path),gdal.GA_Update)
    if change=='alpha':ds.GetRasterBand(1).SetColorInterpretation(gdal.GCI_AlphaBand)
    if change=='scale':ds.GetRasterBand(1).SetScale(2)
    if change=='offset':ds.GetRasterBand(1).SetOffset(1)
    if change=='no_crs':ds.SetProjection('')
    if change=='geographic':srs=osr.SpatialReference();srs.ImportFromEPSG(4326);ds.SetProjection(srs.ExportToWkt())
    if change=='south_up':ds.SetGeoTransform((500000,2,0,3000000,0,3))
    if change=='rotation':ds.SetGeoTransform((500000,2,.1,3000000,0,-3))
    ds=None
    if change in ('complex','complex_alpha'):
        ds=gdal.GetDriverByName('GTiff').Create(str(path),2,1,2 if change=='complex_alpha' else 1,gdal.GDT_CFloat64)
        ds.SetGeoTransform((500000,2,0,3000000,0,-3));srs=osr.SpatialReference();srs.ImportFromEPSG(3857);ds.SetProjection(srs.ExportToWkt());ds.GetRasterBand(1).WriteArray(np.array([[1+0j,0+0j]]))
        if change=='complex_alpha':ds.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand);ds.GetRasterBand(2).Fill(255)
        ds=None
    key='scale' if change in ('scale','offset') else 'complex' if change.startswith('complex') else 'alpha' if change=='alpha' else 'grid'
    if role=='points':key='alignment' if key=='grid' else 'points_'+key
    with raster_workspace(),raster_error_language(language),pytest.raises(ValueError) as caught:
        getattr(compute,operator)(*[raster_load(source_plan(p)) for p in paths],1,1)
    assert str(caught.value)==_point_catchment_messages(compute,operator)[key][language=='en']


@pytest.mark.parametrize('dtype',['Int64','UInt64'])
@pytest.mark.parametrize('value',[2**53-1,2**53,2**53+1])
@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('operator',['raster_watershed_d8','raster_subcatchments_d8'])
def test_d8_watershed_native_nodata_and_exact_id_limit(tmp_path,dtype,value,language,operator):
    import operators.raster_compute as compute
    paths=_watershed_files(tmp_path,np.array([[0.,np.nan]]),np.array([[1.,np.nan]]))
    ds=gdal.GetDriverByName('GTiff').Create(str(paths[1]),2,1,1,getattr(gdal,'GDT_'+dtype))
    ds.SetGeoTransform((500000,2,0,3000000,0,-3));srs=osr.SpatialReference();srs.ImportFromEPSG(3857);ds.SetProjection(srs.ExportToWkt())
    nodata=2**53+2;band=ds.GetRasterBand(1)
    (band.SetNoDataValueAsInt64 if dtype=='Int64' else band.SetNoDataValueAsUInt64)(nodata)
    band.WriteArray(np.array([[value,nodata]],dtype=np.int64 if dtype=='Int64' else np.uint64));band=ds=None
    with raster_workspace(),raster_error_language(language):
        inputs=[raster_load(source_plan(p)) for p in paths]
        if value>2**53-1:
            with pytest.raises(ValueError) as caught:getattr(compute,operator)(*inputs,1,1)
            assert str(caught.value)==_point_catchment_messages(compute,operator)['points_values'][language=='en']
        else:
            result=getattr(compute,operator)(*inputs,1,1);ds=gdal.Open(str(result.path))
            np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),[[float(value),np.nan]]);ds=None


@pytest.mark.parametrize('operator',['raster_watershed_d8','raster_subcatchments_d8'])
def test_d8_watershed_multiband_same_file_alpha_mask_and_valid_zero(tmp_path,operator):
    import operators.raster_compute as compute
    values=np.zeros((3,7));values[1,:]=[np.inf,-9999,np.nan,255,255,255,0]
    alpha=np.full(values.shape,255.);alpha[1,3]=0;alpha[1,4]=np.nan;alpha[0,0]=128
    mask=np.full(values.shape,255,np.uint8);mask[1,5]=0
    valid=np.isfinite(values)&(values!=-9999)&np.isfinite(alpha)&(alpha>0)&(mask!=0)
    points=np.where(valid,0.,values);points[0,0]=2**53-1;points[2,2]=42
    path=create_raster(tmp_path/'multi.tif',np.stack([np.full(values.shape,3.),values,points,alpha]),(500000,2,0,3000000,0,-3),'EPSG:3857')
    ds=gdal.Open(str(path),gdal.GA_Update);ds.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand);ds.GetRasterBand(3).SetUnitType('foot')
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
    ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);ds=None;before=path.read_bytes()
    with raster_workspace():
        source=raster_load(source_plan(path));result=getattr(compute,operator)(source,source,2,3);ds=gdal.Open(str(result.path))
        np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),np.where(valid,points,np.nan))
        assert ds.GetRasterBand(1).GetUnitType()=='';ds=None
    assert path.read_bytes()==before


@pytest.mark.parametrize('stage',['read','graph','write','metadata'])
@pytest.mark.parametrize('operator',['raster_watershed_d8','raster_subcatchments_d8'])
def test_d8_watershed_injected_failure_cleans_private_outputs(tmp_path,monkeypatch,stage,operator):
    import operators.raster_compute as compute
    paths=_watershed_files(tmp_path,np.array([[1.,0.]]),np.array([[0.,42.]]));before=[p.read_bytes() for p in paths]
    with raster_workspace():
        inputs=[raster_load(source_plan(p)) for p in paths];prior=set(inputs[0].workspace.iterdir())
        def fail(*args,**kwargs):raise RuntimeError('injected watershed '+stage)
        if stage=='read':monkeypatch.setattr(compute,'_blocks',fail)
        if stage=='graph':monkeypatch.setattr(compute,'_d8_graph',fail)
        if stage=='write':monkeypatch.setattr(gdal.Band,'WriteArray',fail)
        if stage=='metadata':monkeypatch.setattr(compute,'_raster',fail)
        with pytest.raises(RuntimeError,match='injected watershed'):getattr(compute,operator)(*inputs,1,1)
        assert set(inputs[0].workspace.iterdir())==prior
    assert [p.read_bytes() for p in paths]==before


@pytest.mark.parametrize('operator',['raster_watershed_d8','raster_subcatchments_d8'])
def test_d8_watershed_cross_block_complete_cog_and_long_path(tmp_path,operator):
    import operators.raster_compute as compute
    directions=np.full((515,519),np.nan);directions[508:,:]=1;directions[508:,-1]=0;directions[510,0]=np.nan
    points=np.where(np.isfinite(directions),0.,np.nan)
    for row,label in ((508,91),(510,7),(512,2**53-1),(514,3)):points[row,512]=label
    paths=_watershed_files(tmp_path,directions,points);before=[p.read_bytes() for p in paths];target=tmp_path/'watershed.cog.tif'
    with raster_workspace():
        result=getattr(compute,operator)(*[raster_load(source_plan(p)) for p in paths],1,1)
        artifact=raster_save(result,target_plan(target),profile='cog',blocksize=128,overview_resampling='nearest')
        assert artifact['size_bytes']==target.stat().st_size>0 and validate_cog(raster_load(source_plan(target)))['valid']
        ds=gdal.Open(str(target));actual=read_band_values(ds.GetRasterBand(1));expected=_watershed_walk_oracle(directions,points)
        np.testing.assert_array_equal(actual,expected);np.testing.assert_array_equal(ds.GetRasterBand(1).GetMaskBand().ReadAsArray()!=0,np.isfinite(directions))
        band=ds.GetRasterBand(1);assert band.GetOverviewCount()>0 and band.GetUnitType()=='' and band.GetColorTable() is None
        for i in range(band.GetOverviewCount()):
            overview=band.GetOverview(i).ReadAsArray();assert set(overview[np.isfinite(overview)])<=set([0.,91.,7.,float(2**53-1),3.])
        band=ds=None
    assert [p.read_bytes() for p in paths]==before
    route=np.ones((1,100001));route[0,-1]=0;markers=np.zeros_like(route);markers[0,-2]=777
    output=compute._d8_watershed(route,markers);np.testing.assert_array_equal(output[0,:-1],np.full(100000,777.));assert output[0,-1]==0


@pytest.mark.parametrize('case',['direct','dem','filled','reclass','nested','duplicate','empty','cycle'])
@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('operator',['raster_watershed_d8','raster_subcatchments_d8'])
def test_d8_watershed_real_async_composition_cog_and_failure_cleanup(tmp_path,monkeypatch,case,language,operator):
    import api_server,workflow_engine
    import operators.raster_compute as compute
    directions=np.ones((9,11));directions[:,-1]=0;points=np.zeros((9,11));points[4,5]=777
    if case=='nested':points[4,9]=9
    if case=='duplicate':points[0,0]=777
    if case=='empty':points[:]=0
    if case=='cycle':directions[0,1]=16
    values=np.tile(np.arange(11,0,-1,dtype=float),(9,1))
    if case=='filled':values[:]=10;values[1:-1,1:-1]=-2
    if case=='reclass':points[4,5]=1
    paths=_watershed_files(tmp_path,values if case in ('dem','filled') else directions,points)
    before=[p.read_bytes() for p in paths];target=tmp_path/'watershed.cog.tif'
    tasks=[{'id':'source','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(paths[0])}},
           {'id':'points','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(paths[1])}}];prior='source';point_node='points'
    if case=='filled':tasks.append({'id':'fill','operator':'raster_fill_depressions','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':1,'elevation_unit':'metre'}});prior='fill'
    if case in ('dem','filled'):
        tasks.append({'id':'direction','operator':'raster_flow_direction_d8','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':1,'elevation_unit':'metre'}});prior='direction'
        directions=_d8_bellman_ford_oracle(_fill_level_reachability_oracle(values) if case=='filled' else values)
    if case=='reclass':
        tasks.append({'id':'classes','operator':'raster_reclassify','depends_on':['points'],'params':{'input_raster':{'$ref':'points'},'band':1,'rules':[{'value':0,'class':0},{'value':1,'class':777}]}});point_node='classes';points=np.where(points==1,777.,0.)
    tasks.extend([{'id':'watershed','operator':operator,'depends_on':[prior,point_node],'params':{'input_raster':{'$ref':prior},'points_raster':{'$ref':point_node},'band':1,'points_band':1}},
        {'id':'save','operator':'raster_save','depends_on':['watershed'],'params':{'input_raster':{'$ref':'watershed'},'access_plan':target_plan(target),'profile':'cog','blocksize':128,'overview_resampling':'nearest'}}])
    workspaces=[];execute=workflow_engine._execute_operator
    def capture(name,params):
        if name==operator:workspaces.extend([params['input_raster'].workspace,params['points_raster'].workspace])
        return execute(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',capture)
    client=api_server.app.test_client();response=client.post('/api/workflow',headers={'Accept-Language':language},json={'workflow_def':{'tasks':tasks},'input_data':{},'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert response.status_code==202,response.json
    deadline=time.monotonic()+15
    while time.monotonic()<deadline:
        status=client.get('/api/executions/'+response.json['execution_id'],headers={'Accept-Language':'en' if language=='zh-cn' else 'zh-cn'}).json
        if status['status'] in ('success','failed'):break
        time.sleep(.01)
    failed=case in ('duplicate','empty','cycle') or (case=='nested' and operator=='raster_watershed_d8');assert status['status']==('failed' if failed else 'success'),status
    assert workspaces and all(not p.exists() for p in workspaces) and [p.read_bytes() for p in paths]==before
    if failed:
        assert not target.exists();assert '任务 watershed 执行失败: '+_point_catchment_messages(compute,operator)[case][language=='en'] in status['error']
    else:
        ds=gdal.Open(str(target));np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),_watershed_walk_oracle(directions,points))
        np.testing.assert_array_equal(ds.GetRasterBand(1).GetMaskBand().ReadAsArray()!=0,np.isfinite(directions));assert ds.GetRasterBand(1).GetUnitType()=='';ds=None
        with raster_workspace():assert validate_cog(raster_load(source_plan(target)))['valid']
        artifact=json.loads(status['result']);assert type(artifact['size_bytes']) is int and artifact['size_bytes']==target.stat().st_size>0
    assert client.post('/api/operators/'+operator+'/invoke',json={'params':{}}).status_code==403


@pytest.mark.parametrize('operator',['raster_watershed_d8','raster_subcatchments_d8'])
def test_d8_watershed_discovery_required_dual_inputs_and_single_route(operator):
    spec=next(item for item in list_operators() if item['id']==operator)
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read'] and spec['attributes']['resource_groups']==['raster']
    assert spec['category_path']==['栅格处理','像元计算']
    params={p['name']:p for p in spec['parameters']}
    assert set(params)=={'input_raster','points_raster','band','points_band'} and all(p['required'] for p in params.values())
    assert all(params[p]['type']=='raster' and params[p]['param_type']=='input' for p in ('input_raster','points_raster'))
    assert spec['detailed_description']['workflow_example']['params']=={'input_raster':{'$ref':'source'},'points_raster':{'$ref':'points'},'band':1,'points_band':1}


@pytest.mark.parametrize('dtype',['Int64','UInt64'])
@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('operator',['raster_watershed_d8','raster_subcatchments_d8'])
def test_d8_watershed_direction_native_nodata_cannot_hide_adjacent_illegal_code(tmp_path,dtype,language,operator):
    import operators.raster_compute as compute
    paths=_watershed_files(tmp_path,np.array([[np.nan,1.,0.]]),np.array([[np.nan,0.,42.]]))
    ds=gdal.GetDriverByName('GTiff').Create(str(paths[0]),3,1,1,getattr(gdal,'GDT_'+dtype));ds.SetGeoTransform((500000,2,0,3000000,0,-3))
    srs=osr.SpatialReference();srs.ImportFromEPSG(3857);ds.SetProjection(srs.ExportToWkt());band=ds.GetRasterBand(1)
    (band.SetNoDataValueAsInt64 if dtype=='Int64' else band.SetNoDataValueAsUInt64)(2**53)
    band.WriteArray(np.array([[2**53,2**53+1,0]],dtype=np.int64 if dtype=='Int64' else np.uint64));band=ds=None
    with raster_workspace(),raster_error_language(language),pytest.raises(ValueError) as caught:
        getattr(compute,operator)(*[raster_load(source_plan(p)) for p in paths],1,1)
    assert str(caught.value)==_point_catchment_messages(compute,operator)['code'][language=='en']


@pytest.mark.parametrize('epsg',[3857,2277,2227])
@pytest.mark.parametrize('operator',['raster_watershed_d8','raster_subcatchments_d8'])
def test_d8_watershed_linear_units_do_not_change_user_ids(tmp_path,epsg,operator):
    import operators.raster_compute as compute
    paths=_watershed_files(tmp_path,np.array([[1.,1.,0.]]),np.array([[0.,42.,0.]]))
    srs=osr.SpatialReference();srs.ImportFromEPSG(epsg)
    for path in paths:
        ds=gdal.Open(str(path),gdal.GA_Update);ds.SetProjection(srs.ExportToWkt());ds.GetRasterBand(1).SetUnitType('foot');ds=None
    with raster_workspace():
        result=getattr(compute,operator)(*[raster_load(source_plan(p)) for p in paths],1,1);ds=gdal.Open(str(result.path))
        np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),[[42.,42.,0.]])
        assert ds.GetSpatialRef().IsSame(srs) and ds.GetRasterBand(1).GetUnitType()=='';ds=None


def test_d8_watershed_distant_nested_points_rejected_without_recursion():
    import operators.raster_compute as compute
    directions=np.ones((1,100001));directions[0,-1]=0;points=np.zeros_like(directions);points[0,0]=91;points[0,-1]=7
    with pytest.raises(ValueError,match='嵌套'):compute._d8_watershed(directions,points)




def _stream_link_walk_oracle(directions, streams):
    """Trace unique upstream neighbours per cell, without the production graph/queue."""
    river=streams==1
    def donors(row,col):
        return [(row-dy,col-dx) for code,(dy,dx) in _STRAHLER_STEPS.items()
                if 0<=row-dy<river.shape[0] and 0<=col-dx<river.shape[1]
                and river[row-dy,col-dx] and directions[row-dy,col-dx]==code]
    starts=sorted((int(y),int(x)) for y,x in np.argwhere(river) if len(donors(int(y),int(x)))!=1)
    ids={cell:i+1 for i,cell in enumerate(starts)}
    result=np.where(np.isfinite(directions),0.,np.nan)
    for y,x in np.argwhere(river):
        cell=(int(y),int(x));seen=set()
        while cell not in ids:
            assert cell not in seen;seen.add(cell)
            previous=donors(*cell);assert len(previous)==1;cell=previous[0]
        result[y,x]=ids[cell]
    return result


@pytest.mark.parametrize('seed',range(12))
@pytest.mark.parametrize('epsg',[3857,2277,2227])
def test_d8_stream_link_random_forest_independent_walk_and_units(tmp_path,seed,epsg):
    import operators.raster_compute as compute
    rng=np.random.default_rng(seed);directions=np.zeros((9,11));rows,cols=np.indices(directions.shape)
    rank=np.minimum.reduce((rows,cols,8-rows,10-cols))
    for y,x in np.argwhere(rank>0):
        choices=[code for code,(dy,dx) in _STRAHLER_STEPS.items() if rank[y+dy,x+dx]<rank[y,x]]
        directions[y,x]=rng.choice(choices)
    directions[4,5]=np.nan
    for y,x in np.argwhere(np.isfinite(directions)&(directions!=0)):
        dy,dx=_STRAHLER_STEPS[directions[y,x]]
        if not np.isfinite(directions[y+dy,x+dx]):directions[y,x]=0
    streams=np.where(np.isfinite(directions),0.,np.nan)
    for y,x in np.argwhere(np.isfinite(directions)&(rng.random(directions.shape)<.15)):
        y,x=int(y),int(x)
        while True:
            streams[y,x]=1
            if directions[y,x]==0:break
            dy,dx=_STRAHLER_STEPS[directions[y,x]];y+=dy;x+=dx
    expected=_stream_link_walk_oracle(directions,streams)
    paths=_strahler_files(tmp_path,directions,streams);crs=osr.SpatialReference();crs.ImportFromEPSG(epsg)
    for path in paths:
        ds=gdal.Open(str(path),gdal.GA_Update);ds.SetProjection(crs.ExportToWkt());ds.GetRasterBand(1).SetUnitType('foot');ds=None
    before=[p.read_bytes() for p in paths]
    with raster_workspace():
        inputs=[raster_load(source_plan(p)) for p in paths];prior=set(inputs[0].workspace.iterdir())
        output=compute.raster_stream_link_d8(*inputs,1,1);ds=gdal.Open(str(output.path));band=ds.GetRasterBand(1)
        np.testing.assert_array_equal(read_band_values(band),expected)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(directions))
        assert band.DataType==gdal.GDT_Float64 and math.isnan(band.GetNoDataValue()) and band.GetUnitType()==''
        assert band.GetScale() is None and band.GetOffset() is None and band.GetColorTable() is None
        assert ds.GetGeoTransform()==(500000.,2.,0.,3000000.,0.,-3.) and ds.GetSpatialRef().IsSame(crs)
        assert set(inputs[0].workspace.iterdir())==prior|{output.path};band=ds=None
    assert [p.read_bytes() for p in paths]==before
    labels=np.unique(expected[expected>0]);np.testing.assert_array_equal(labels,np.arange(1,len(labels)+1))


def test_d8_stream_link_confluence_owns_downstream_with_row_order_not_topological_order():
    import operators.raster_compute as compute
    directions=np.zeros((3,3));directions[2,0]=128;directions[2,2]=32;directions[1,1]=64
    streams=np.array([[0,1,0],[0,1,0],[1,0,1]],float)
    expected=np.array([[0,1,0],[0,1,0],[2,0,3]],float)
    np.testing.assert_array_equal(compute._d8_stream_links(directions,streams==1),expected)
    # A confluence at an outlet starts a one-cell downstream segment.
    directions=np.array([[np.nan,0,np.nan],[128,np.nan,32]],float)
    streams=np.where(np.isfinite(directions),1.,np.nan)
    np.testing.assert_array_equal(compute._d8_stream_links(directions,streams==1),[[np.nan,1,np.nan],[2,np.nan,3]])


def test_d8_stream_link_diagonal_crossing_nonriver_donors_and_separate_outlets():
    import operators.raster_compute as compute
    directions=np.array([[2.,8.],[0.,0.]])
    np.testing.assert_array_equal(compute._d8_stream_links(directions,np.ones((2,2),bool)),[[1,2],[2,1]])
    directions=np.array([[2.,4.,8.],[1.,1.,0.]])
    streams=np.array([[0,0,0],[1,1,1]],bool)
    np.testing.assert_array_equal(compute._d8_stream_links(directions,streams),[[0,0,0],[1,1,1]])
    directions=np.zeros((1,3));streams=np.ones((1,3),bool)
    np.testing.assert_array_equal(compute._d8_stream_links(directions,streams),[[1,2,3]])


@pytest.mark.parametrize('dy,dx,code',[(dy,dx,code) for code,(dy,dx) in _STRAHLER_STEPS.items()])
def test_d8_stream_link_all_eight_routes_and_nonterminal_edges(dy,dx,code):
    import operators.raster_compute as compute
    directions=np.full((3,3),np.nan);directions[1,1]=code;directions[1+dy,1+dx]=0
    expected=np.where(np.isfinite(directions),1.,np.nan)
    np.testing.assert_array_equal(compute._d8_stream_links(directions,np.isfinite(directions)),expected)


def test_d8_stream_link_nested_confluences_and_multiple_inflows():
    import operators.raster_compute as compute
    directions,streams=_strahler_tree();expected=_stream_link_walk_oracle(directions,streams)
    np.testing.assert_array_equal(compute._d8_stream_links(directions,streams==1),expected)
    assert expected[3,3]!=expected[3,6] and expected[3,5]!=expected[3,6]
    directions=np.full((5,6),np.nan)
    for code,(dy,dx) in _STRAHLER_STEPS.items():directions[2-dy,2-dx]=code
    directions[2,2]=1;directions[2,3:5]=1;directions[2,5]=0
    expected=_stream_link_walk_oracle(directions,np.where(np.isfinite(directions),1.,np.nan))
    np.testing.assert_array_equal(compute._d8_stream_links(directions,np.isfinite(directions)),expected)
    assert expected[2,2]==expected[2,5] and len(np.unique(expected[expected>0]))==8


@pytest.mark.parametrize('shape',[(1,1),(1,9),(9,1),(5,7)])
@pytest.mark.parametrize('empty',['network','coverage'])
def test_d8_stream_link_empty_and_all_invalid(shape,empty):
    import operators.raster_compute as compute
    directions=np.full(shape,np.nan) if empty=='coverage' else np.zeros(shape)
    if empty=='network' and min(shape)>2:directions[1:-1,1:-1]=1
    streams=np.zeros(shape,bool)
    np.testing.assert_array_equal(compute._d8_stream_links(directions,streams),np.where(np.isfinite(directions),0.,np.nan))


@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_stream_link_count_limit_failure_before_publication(tmp_path,monkeypatch,language):
    import operators.raster_compute as compute
    paths=_strahler_files(tmp_path,np.zeros((1,3)),np.ones((1,3)));before=[p.read_bytes() for p in paths]
    with raster_workspace(),raster_error_language(language):
        inputs=[raster_load(source_plan(p)) for p in paths];prior=set(inputs[0].workspace.iterdir());monkeypatch.setattr(compute,'_D8_MAX_EXACT_COUNT',2)
        with pytest.raises(ValueError) as caught:compute.raster_stream_link_d8(*inputs,1,1)
        assert str(caught.value)==compute._D8_STREAM_LINK_MESSAGES['overflow'][language=='en']
        assert set(inputs[0].workspace.iterdir())==prior
    assert [p.read_bytes() for p in paths]==before


def test_d8_stream_link_cross_block_complete_cog_and_long_route(tmp_path,monkeypatch):
    import operators.raster_compute as compute
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    height,width=517,519;directions=np.ones((height,width));directions[:,-1]=0
    streams=np.zeros(directions.shape);streams[255,:513]=streams[257,:513]=1;streams[256,512:]=1
    directions[255,512]=4;directions[257,512]=64;directions[510,510]=np.nan;directions[510,509]=0;streams[510,510]=np.nan
    expected=np.where(np.isfinite(directions),0.,np.nan);expected[255,:513]=1;expected[256,512:]=2;expected[257,:513]=3
    paths=_strahler_files(tmp_path,directions,streams);before=[p.read_bytes() for p in paths];target=tmp_path/'links.cog.tif'
    with raster_workspace():
        inputs=[raster_load(source_plan(p)) for p in paths];reads=[];original=compute._blocks
        def bounded(*args,**kwargs):
            for x,y,arrays,masks in original(*args,**kwargs):
                assert all(max(a.shape)<=512 for a in arrays);reads.append((x,y));yield x,y,arrays,masks
        monkeypatch.setattr(compute,'_blocks',bounded)
        result=compute.raster_stream_link_d8(*inputs,1,1);artifact=raster_save(result,target_plan(target),profile='cog',blocksize=128,overview_resampling='nearest')
        assert type(artifact['size_bytes']) is int and artifact['size_bytes']==target.stat().st_size>0
        ds=gdal.Open(str(target));band=ds.GetRasterBand(1)
        np.testing.assert_array_equal(read_band_values(band),expected)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(directions))
        assert band.GetOverviewCount()>0 and band.GetUnitType()=='' and band.DataType==gdal.GDT_Float64 and math.isnan(band.GetNoDataValue())
        assert ds.GetGeoTransform()==(500000.,2.,0.,3000000.,0.,-3.) and ds.GetSpatialRef().GetAuthorityCode(None)=='3857'
        for i in range(band.GetOverviewCount()):
            values=read_band_values(band.GetOverview(i));assert set(values[np.isfinite(values)])<={0.,1.,2.,3.}
        assert not validate(ds,full_check=True)[1];band=ds=None
        old=target.read_bytes();compute.raster_stream_link_d8(*inputs,1,1);assert old==target.read_bytes()
        assert (512,512) in reads
    assert [p.read_bytes() for p in paths]==before and not result.path.exists()
    route=np.ones((1,100001));route[0,-1]=0
    np.testing.assert_array_equal(compute._d8_stream_links(route,np.ones(route.shape,bool)),np.ones(route.shape))


@pytest.mark.parametrize('case',['saved','threshold','dem','filled','empty','break','cycle','coverage'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_stream_link_real_async_two_input_save_threshold_composition_and_failure_cleanup(tmp_path,monkeypatch,case,language):
    import api_server,workflow_engine
    directions=np.ones((9,11));directions[:,-1]=0;streams=np.ones(directions.shape)
    if case=='cycle':directions[0,1]=16;streams[:]=0
    if case=='empty':directions[:]=np.nan;streams[:]=np.nan
    if case=='break':streams[2,5]=0
    if case=='coverage':streams[2,5]=np.nan
    values=np.tile(np.arange(11,0,-1,dtype=float),(9,1))
    if case=='filled':values[:]=10;values[1:-1,1:-1]=-2
    input_values=values if case in ('dem','filled') else directions
    paths=_strahler_files(tmp_path,input_values,streams);before=[p.read_bytes() for p in paths];target=tmp_path/'links.cog.tif'
    tasks=[{'id':'source','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(paths[0])}}];prior='source'
    if case=='filled':
        tasks.append({'id':'fill','operator':'raster_fill_depressions','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':1,'elevation_unit':'metre'}});prior='fill'
    if case in ('dem','filled'):
        tasks.append({'id':'direction','operator':'raster_flow_direction_d8','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':1,'elevation_unit':'metre'}});prior='direction'
        directions=_d8_bellman_ford_oracle(_fill_level_reachability_oracle(values) if case=='filled' else values)
    if case=='threshold':
        tasks.extend([{'id':'accumulate','operator':'raster_flow_accumulation_d8','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':1}},
            {'id':'streams','operator':'raster_reclassify','depends_on':['accumulate'],'params':{'input_raster':{'$ref':'accumulate'},'band':1,'rules':[{'min':None,'max':2,'class':0},{'min':2,'max':None,'class':1}]}}])
        streams=np.where(_accumulation_walk_oracle(directions)>=2,1.,0.)
    else:tasks.append({'id':'streams','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(paths[1])}})
    tasks.extend([{'id':'links','operator':'raster_stream_link_d8','depends_on':[prior,'streams'],'params':{'input_raster':{'$ref':prior},'stream_raster':{'$ref':'streams'},'band':1,'stream_band':1}},
        {'id':'save','operator':'raster_save','depends_on':['links'],'params':{'input_raster':{'$ref':'links'},'access_plan':target_plan(target),'profile':'cog','blocksize':128,'overview_resampling':'nearest'}}])
    workspaces=[];execute=workflow_engine._execute_operator
    def capture(name,params):
        if name=='raster_stream_link_d8':workspaces.extend([params['input_raster'].workspace,params['stream_raster'].workspace])
        return execute(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',capture)
    client=api_server.app.test_client();response=client.post('/api/workflow',headers={'Accept-Language':language},json={'workflow_def':{'tasks':tasks},'input_data':{},'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert response.status_code==202,response.json
    deadline=time.monotonic()+15
    while time.monotonic()<deadline:
        status=client.get('/api/executions/'+response.json['execution_id'],headers={'Accept-Language':'en' if language=='zh-cn' else 'zh-cn'}).json
        if status['status'] in ('success','failed'):break
        time.sleep(.01)
    failed=case in ('break','cycle','coverage');assert status['status']==('failed' if failed else 'success'),status
    assert workspaces and all(not p.exists() for p in workspaces) and [p.read_bytes() for p in paths]==before
    if failed:
        assert not target.exists()
        import operators.raster_compute as compute
        assert '任务 links 执行失败: '+compute._D8_STREAM_LINK_MESSAGES[case][language=='en'] in status['error']
    else:
        ds=gdal.Open(str(target));np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),_stream_link_walk_oracle(directions,streams))
        assert ds.GetRasterBand(1).GetUnitType()=='';ds=None
        with raster_workspace():assert validate_cog(raster_load(source_plan(target)))['valid']
        artifact=json.loads(status['result']);assert type(artifact['size_bytes']) is int and artifact['size_bytes']==target.stat().st_size>0
    assert client.post('/api/operators/raster_stream_link_d8/invoke',json={'params':{}}).status_code==403


# Shreve oracle counts headwater contributions by independent forward walks,
# rather than reusing the production topological sums or intermediate levels.
def _shreve_headwater_walk_oracle(directions,streams):
    valid=np.isfinite(directions);river=streams==1;heads=[]
    for row,col in np.argwhere(river):
        incoming=[]
        for code,(dy,dx) in _STRAHLER_STEPS.items():
            y,x=int(row)-dy,int(col)-dx
            if 0<=y<river.shape[0] and 0<=x<river.shape[1] and river[y,x] and directions[y,x]==code:incoming.append((y,x))
        if not incoming:heads.append((int(row),int(col)))
    result=np.where(valid,0.,np.nan)
    for row,col in heads:
        seen=set()
        while True:
            assert river[row,col] and (row,col) not in seen
            seen.add((row,col));result[row,col]+=1
            if directions[row,col]==0:break
            dy,dx=_STRAHLER_STEPS[directions[row,col]];row,col=row+dy,col+dx
    assert sum(result[(directions==0)&river])==len(heads)
    return result


@pytest.mark.parametrize('prune_lower',[False,True])
def test_d8_shreve_actual_headwaters_nested_confluences_and_strahler_default_preserved(tmp_path,prune_lower):
    import operators.raster_compute as compute
    directions,streams=_strahler_tree()
    if prune_lower:
        for cell in ((5,1),(5,3),(4,2)):streams[cell]=0
    paths=_strahler_files(tmp_path,directions,streams);before=[p.read_bytes() for p in paths]
    expected=_shreve_headwater_walk_oracle(directions,streams)
    assert expected[2,2]==2 and expected[3,3]==(3 if prune_lower else 5)
    assert expected[3,4]==(3 if prune_lower else 5) and np.all(expected[3,6:]==(4 if prune_lower else 6))
    with raster_workspace():
        source,mask=[raster_load(source_plan(p)) for p in paths]
        for method in (None,'strahler','shreve'):
            result=compute.raster_stream_order_d8(source,mask,1,1,**({'method':method} if method else {}))
            ds=gdal.Open(str(result.path));band=ds.GetRasterBand(1)
            np.testing.assert_array_equal(read_band_values(band),expected if method=='shreve' else _strahler_recursive_oracle(directions,streams))
            np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(directions))
            assert band.DataType==gdal.GDT_Float64 and math.isnan(band.GetNoDataValue()) and band.GetUnitType()==''
            assert band.GetScale() is None and band.GetOffset() is None and band.GetColorTable() is None
            band=ds=None
    assert [p.read_bytes() for p in paths]==before


@pytest.mark.parametrize('seed',range(36))
def test_d8_shreve_random_forest_full_magnitudes_and_headwater_conservation(seed):
    import operators.raster_compute as compute
    rng=np.random.default_rng(seed);rows,cols=np.indices((9,11));rank=np.minimum.reduce((rows,cols,8-rows,10-cols));directions=np.zeros((9,11))
    for row,col in np.argwhere(rank>0):
        choices=[code for code,(dy,dx) in _STRAHLER_STEPS.items() if rank[row+dy,col+dx]<rank[row,col]];directions[row,col]=rng.choice(choices)
    directions[4,5]=np.nan
    for row,col in np.argwhere(np.isfinite(directions)&(directions!=0)):
        dy,dx=_STRAHLER_STEPS[directions[row,col]]
        if not np.isfinite(directions[row+dy,col+dx]):directions[row,col]=0
    counts=_accumulation_walk_oracle(directions);streams=np.where(np.isfinite(directions),(counts>=seed%5).astype(float),np.nan)
    np.testing.assert_array_equal(compute._d8_stream_order(directions,streams==1,'shreve'),_shreve_headwater_walk_oracle(directions,streams))
    np.testing.assert_array_equal(compute._d8_stream_order(directions,streams==1),compute._d8_stream_order(directions,streams==1,'strahler'))
    np.testing.assert_array_equal(compute._d8_stream_order(directions,streams==1),_strahler_recursive_oracle(directions,streams))


@pytest.mark.parametrize('dy,dx,code',[(dy,dx,code) for code,(dy,dx) in _STRAHLER_STEPS.items()])
def test_d8_shreve_eight_directions_centres_and_open_outlet_one(dy,dx,code):
    import operators.raster_compute as compute
    directions=np.full((3,3),np.nan);directions[1,1]=code;directions[1+dy,1+dx]=0
    np.testing.assert_array_equal(compute._d8_stream_order(directions,np.isfinite(directions),'shreve'),np.where(np.isfinite(directions),1.,np.nan))


@pytest.mark.parametrize('directions,streams',[(np.array([[1.,1.,0.]]),np.array([[0.,1.,1.]])),(np.array([[4.],[4.],[0.]]),np.ones((3,1))),
    (np.array([[0.]]),np.array([[1.]])),(np.zeros((2,3)),np.zeros((2,3))), (np.full((4,5),np.nan),np.full((4,5),np.nan)),
    (np.array([[2.,8.],[0.,0.]]),np.ones((2,2)))])
def test_d8_shreve_axes_empty_invalid_crossing_and_adjacent_outlets(tmp_path,directions,streams):
    import operators.raster_compute as compute
    paths=_strahler_files(tmp_path,directions,streams)
    with raster_workspace():
        result=compute.raster_stream_order_d8(*[raster_load(source_plan(p)) for p in paths],1,1,method='shreve');ds=gdal.Open(str(result.path))
        np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),_shreve_headwater_walk_oracle(directions,streams));ds=None


def _shreve_seven_sources():
    directions=np.full((3,3),np.nan);directions[1,1]=0
    for code,(dy,dx) in list(_STRAHLER_STEPS.items())[:-1]:directions[1-dy,1-dx]=code
    return directions,np.where(np.isfinite(directions),1.,np.nan)


def test_d8_shreve_all_tributaries_add_even_when_equal_and_open_terminal_is_confluence():
    import operators.raster_compute as compute
    directions,streams=_shreve_seven_sources();expected=_shreve_headwater_walk_oracle(directions,streams)
    assert expected[1,1]==7 and compute._d8_stream_order(directions,streams==1)[1,1]==2
    np.testing.assert_array_equal(compute._d8_stream_order(directions,streams==1,'shreve'),expected)


@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('limit',[0,1,6,7])
def test_d8_shreve_exact_integer_limit_at_source_and_confluence_before_output(tmp_path,monkeypatch,language,limit):
    import operators.raster_compute as compute
    directions,streams=_shreve_seven_sources();paths=_strahler_files(tmp_path,directions,streams);before=[p.read_bytes() for p in paths]
    monkeypatch.setattr(compute,'_D8_MAX_EXACT_COUNT',limit)
    with raster_workspace(),raster_error_language(language):
        source,mask=[raster_load(source_plan(p)) for p in paths];prior=set(source.workspace.iterdir())
        if limit<7:
            with pytest.raises(ValueError) as caught:compute.raster_stream_order_d8(source,mask,1,1,method='shreve')
            assert str(caught.value)==compute._D8_SHREVE_MESSAGES['overflow'][language=='en'] and set(source.workspace.iterdir())==prior
        else:
            result=compute.raster_stream_order_d8(source,mask,1,1,method='shreve');ds=gdal.Open(str(result.path))
            np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),_shreve_headwater_walk_oracle(directions,streams));ds=None
    assert [p.read_bytes() for p in paths]==before


@pytest.mark.parametrize('method',[None,True,False,0,1,1.5,[],{},'','SHREVE','Shreve',' shreve','shreve ','strahler\n','unknown'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_shreve_method_is_exact_and_rejected_before_opening_inputs(monkeypatch,method,language):
    import operators.raster_compute as compute
    def unexpected(*args,**kwargs):raise AssertionError('invalid method must not open inputs')
    monkeypatch.setattr(compute,'_d8_stream_sources',unexpected)
    with raster_error_language(language),pytest.raises(ValueError) as caught:compute.raster_stream_order_d8(None,None,1,1,method=method)
    assert str(caught.value)==compute._D8_STREAM_ORDER_MESSAGES['method'][language=='en']


def test_d8_shreve_cross_block_complete_cog_mask_nearest_pyramid_and_unchanged_prior_output(tmp_path,monkeypatch):
    import operators.raster_compute as compute
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    height,width=517,519;directions=np.ones((height,width));directions[:,-1]=0
    streams=np.zeros(directions.shape);streams[255,:513]=streams[257,:513]=streams[254,:513]=1;streams[256,512:]=1
    directions[255,512]=4;directions[257,512]=64;directions[254,512]=4
    directions[510,510]=np.nan;directions[510,509]=0;streams[510,510]=np.nan
    expected=streams.copy();expected[255,512]=2;expected[256,512:]=3
    paths=_strahler_files(tmp_path,directions,streams);before=[p.read_bytes() for p in paths];target=tmp_path/'shreve.cog.tif'
    with raster_workspace():
        source,mask=[raster_load(source_plan(p)) for p in paths];calls=[];original=gdal.Band.ReadAsArray
        def read(self,*args,**kwargs):
            if len(args)>=4:calls.append(args[2:4])
            return original(self,*args,**kwargs)
        monkeypatch.setattr(gdal.Band,'ReadAsArray',read)
        result=compute.raster_stream_order_d8(source,mask,1,1,method='shreve');assert calls and all(w<=512 and h<=512 for w,h in calls)
        artifact=raster_save(result,target_plan(target),profile='cog',blocksize=128,overview_resampling='nearest')
        assert type(artifact['size_bytes']) is int and artifact['size_bytes']==target.stat().st_size>0
        ds=gdal.Open(str(target));band=ds.GetRasterBand(1);np.testing.assert_array_equal(read_band_values(band),expected)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected))
        assert ds.GetGeoTransform()==(500000.,2.,0.,3000000.,0.,-3.) and ds.GetSpatialRef().GetAuthorityCode(None)=='3857'
        assert band.DataType==gdal.GDT_Float64 and math.isnan(band.GetNoDataValue()) and band.GetUnitType()==''
        assert band.GetScale() is None and band.GetOffset() is None and band.GetColorTable() is None and band.GetOverviewCount()>0
        for index in range(band.GetOverviewCount()):
            overview=read_band_values(band.GetOverview(index));assert set(np.unique(overview[np.isfinite(overview)]))<={0.,1.,2.,3.}
        assert not validate(ds,full_check=True)[1];band=ds=None;snapshot=target.read_bytes()
        compute.raster_stream_order_d8(source,mask,1,1,method='shreve');assert target.read_bytes()==snapshot
    assert [p.read_bytes() for p in paths]==before and not result.path.exists()


def test_d8_shreve_long_single_route_is_one_headwater_without_recursion():
    import operators.raster_compute as compute
    directions=np.ones((1,100001));directions[0,-1]=0
    np.testing.assert_array_equal(compute._d8_stream_order(directions,np.ones(directions.shape,bool),'shreve'),np.ones(directions.shape))


# Vectorization oracle follows incoming edges back to a segment start. It does
# not use the production topological graph, numbering helper or forward labels.
def _stream_vector_edge_oracle(directions, streams, transform, metres):
    from collections import defaultdict
    river = {tuple(map(int, cell)) for cell in np.argwhere(streams == 1)}
    edges, donors = {}, defaultdict(list)
    for cell in river:
        if directions[cell] != 0:
            dy, dx = _STRAHLER_STEPS[directions[cell]]
            target = cell[0]+dy, cell[1]+dx
            assert target in river
            edges[cell] = target
            donors[target].append(cell)
    starts = sorted(cell for cell in river if len(donors[cell]) != 1)
    ids = {cell: index+1 for index, cell in enumerate(starts)}
    assert all(cell in edges or donors[cell] for cell in river), 'isolated stream cell'
    origin, groups = {}, defaultdict(dict)
    for cell, target in edges.items():
        upstream, walked = cell, []
        while upstream not in ids and upstream not in origin:
            walked.append(upstream)
            upstream = donors[upstream][0]
        start = upstream if upstream in ids else origin[upstream]
        for member in walked:
            origin[member] = start
        groups[start][cell] = target
    expected = []
    for start, component in sorted(groups.items()):
        vertices = [start]
        while vertices[-1] in component:
            vertices.append(component[vertices[-1]])
        coords = [(transform[0]+(col+.5)*transform[1], transform[3]+(row+.5)*transform[5])
                  for row, col in vertices]
        length = math.fsum(math.hypot((b[1]-a[1])*transform[1], (b[0]-a[0])*transform[5])*metres
                           for a, b in zip(vertices, vertices[1:]))
        expected.append((ids[start], *start, *vertices[-1], length, coords))
    return edges, expected


def _assert_stream_vector_complete(actual, directions, streams, transform, metres, epsg):
    from collections import Counter
    edges, expected = _stream_vector_edge_oracle(directions, streams, transform, metres)
    assert list(actual.columns) == ['link_id','from_row','from_col','to_row','to_col','length_m','geometry']
    assert all(str(actual[name].dtype) == 'int64' for name in actual.columns[:5])
    assert str(actual.length_m.dtype) == 'float64' and actual.crs.to_epsg() == epsg
    assert len(actual) == len(expected)
    observed = Counter()
    for row, truth in zip(actual.itertuples(index=False), expected):
        assert tuple(row[:5]) == truth[:5]
        assert row.length_m == pytest.approx(truth[5], rel=1e-12, abs=1e-9)
        assert row.geometry.geom_type == 'LineString' and not row.geometry.has_z
        assert list(row.geometry.coords) == truth[6] and row.length_m > 0
        cells = [(round((y-transform[3])/transform[5]-.5),round((x-transform[0])/transform[1]-.5))
                 for x,y in row.geometry.coords]
        observed.update(zip(cells, cells[1:]))
    assert observed == Counter(edges.items())


@pytest.mark.parametrize('epsg,metres',[(3857,1.),(2222,.3048),(2277,1200/3937)])
@pytest.mark.parametrize('case',['tree','crossing','terminal-gap','empty','invalid','adjacent-confluences'])
def test_d8_stream_vector_full_geometry_edges_ids_units_and_geopackage(tmp_path,epsg,metres,case):
    import geopandas as gpd
    import operators.raster_compute as compute
    from operators.io_operators import save
    directions, streams = _strahler_tree()
    if case == 'crossing':
        directions = np.array([[2.,8.],[0.,0.]])
        streams = np.ones_like(directions)
    if case == 'terminal-gap':
        directions = np.full((2,6),np.nan)
        directions[0,:3]=[1,1,0];directions[1,1]=128;directions[0,4:]=[1,0]
        streams=np.where(np.isfinite(directions),1.,np.nan)
    if case == 'adjacent-confluences':
        directions=np.array([[2.,4.,8.,np.nan],[1.,1.,0.,np.nan]])
        streams=np.where(np.isfinite(directions),1.,np.nan)
    if case in ('empty','invalid'):
        directions=np.array([[1.,0.]]) if case=='empty' else np.full((2,3),np.nan)
        streams=np.where(np.isfinite(directions),0.,np.nan)
    paths=_strahler_files(tmp_path,directions,streams)
    for path in paths:
        ds=gdal.Open(str(path),gdal.GA_Update);crs=osr.SpatialReference();crs.ImportFromEPSG(epsg);ds.SetProjection(crs.ExportToWkt());ds.GetRasterBand(1).SetUnitType('classification');ds=None
    before=[p.read_bytes() for p in paths];target=tmp_path/'river.gpkg'
    transform=(500000,2,0,3000000,0,-3)
    with raster_workspace():
        source,mask=[raster_load(source_plan(p)) for p in paths];prior=set(source.workspace.iterdir())
        result=compute.raster_stream_to_vector_d8(source,mask,1,1)
        _assert_stream_vector_complete(result,directions,streams,transform,metres,epsg)
        assert set(source.workspace.iterdir())==prior
        if case=='terminal-gap':assert list(result.link_id)==[1,3,4]
        save(result,path=target.name,connection_info={'mount_path':str(tmp_path)})
        assert target.stat().st_size>0
        saved=gpd.read_file(target)
        _assert_stream_vector_complete(saved,directions,streams,transform,metres,epsg)
        ds=ogr.Open(str(target));layer=ds.GetLayer(0)
        assert all(layer.GetLayerDefn().GetFieldDefn(i).GetType()==ogr.OFTInteger64 for i in range(5))
        assert layer.GetLayerDefn().GetFieldDefn(5).GetType()==ogr.OFTReal
        assert layer.GetSpatialRef().IsSame(crs)
        layer=ds=None
        snapshot=target.read_bytes();compute.raster_stream_to_vector_d8(source,mask,1,1)
        assert target.read_bytes()==snapshot
    assert [p.read_bytes() for p in paths]==before


@pytest.mark.parametrize('seed',range(12))
def test_d8_stream_vector_random_forests_independent_complete_edge_coverage(tmp_path,seed):
    import operators.raster_compute as compute
    rng=np.random.default_rng(seed);directions=np.zeros((8,11))
    for row in range(8):
        for col in range(10):
            choices=[code for code,(dy,dx) in _STRAHLER_STEPS.items() if dx==1 and 0<=row+dy<8]
            directions[row,col]=rng.choice(choices)
    # No source may point out of the effective domain. Nonriver isolated outlets
    # remain valid direction cells and must not become spurious vector features.
    streams=np.ones_like(directions)
    targets={(row+_STRAHLER_STEPS[directions[row,col]][0],col+1) for row in range(8) for col in range(10)}
    for row in range(8):
        if (row,10) not in targets:streams[row,10]=0
    paths=_strahler_files(tmp_path,directions,streams)
    with raster_workspace():
        result=compute.raster_stream_to_vector_d8(*[raster_load(source_plan(p)) for p in paths],1,1)
        _assert_stream_vector_complete(result,directions,streams,(500000,2,0,3000000,0,-3),1.,3857)


@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('case',['isolated','mixed','nonriver-donor','step-zero','step-infinite','coordinates-collapse','coordinates-infinite','length','overflow'])
def test_d8_stream_vector_precise_degenerate_numeric_failures(language,case,monkeypatch):
    import operators.raster_compute as compute
    directions=np.array([[1.,1.,0.]])
    streams=np.ones_like(directions,dtype=bool)
    transform=(500000,2,0,3000000,0,-3);factor=1.;key=case
    if case in ('isolated','nonriver-donor'):
        streams[:]=False;streams[0,-1]=True;key='isolated'
    elif case=='mixed':
        directions=np.array([[1.,0.,np.nan,0.]])
        streams=np.isfinite(directions);key='isolated'
    elif case=='step-zero':transform=(0,0,0,0,0,-3);key='step'
    elif case=='step-infinite':factor=float('inf');key='step'
    elif case=='coordinates-collapse':transform=(1e308,2,0,3000000,0,-3);key='coordinates'
    elif case=='coordinates-infinite':transform=(1e308,1e308,0,3000000,0,-3);key='coordinates'
    elif case=='length':transform=(0,1,0,0,0,-1);monkeypatch.setattr(compute,'_d8_length_steps',lambda *args:{1:1e308})
    elif case=='overflow':monkeypatch.setattr(compute,'_D8_MAX_EXACT_COUNT',0)
    with raster_error_language(language),pytest.raises(ValueError) as error:
        compute._d8_stream_lines(directions,streams,transform,factor,'EPSG:3857')
    assert str(error.value)==compute._D8_STREAM_VECTOR_MESSAGES[key][language=='en']


def test_d8_stream_vector_long_route_cross_block_and_exact_original_vertices(tmp_path,monkeypatch):
    import operators.raster_compute as compute
    width=10001;directions=np.ones((1,width));directions[0,-1]=0;streams=np.ones_like(directions)
    paths=_strahler_files(tmp_path,directions,streams);reads=[];original=gdal.Band.ReadAsArray
    def read(self,*args,**kwargs):
        if len(args)>=4:reads.append(args[2:4])
        return original(self,*args,**kwargs)
    monkeypatch.setattr(gdal.Band,'ReadAsArray',read)
    with raster_workspace():
        result=compute.raster_stream_to_vector_d8(*[raster_load(source_plan(p)) for p in paths],1,1)
        _assert_stream_vector_complete(result,directions,streams,(500000,2,0,3000000,0,-3),1.,3857)
        assert len(result)==1 and len(result.geometry.iloc[0].coords)==width
    assert reads and all(w<=512 and h<=512 for w,h in reads)


@pytest.mark.parametrize('stage',['read','graph','line','frame'])
def test_d8_stream_vector_injected_failures_no_private_outputs_or_source_mutation(tmp_path,monkeypatch,stage):
    import operators.raster_compute as compute
    paths=_strahler_files(tmp_path,np.array([[1.,0.]]),np.ones((1,2)));before=[p.read_bytes() for p in paths]
    with raster_workspace():
        source,mask=[raster_load(source_plan(p)) for p in paths];prior=set(source.workspace.iterdir())
        def fail(*args,**kwargs):raise RuntimeError('injected stream vector failure')
        if stage=='read':monkeypatch.setattr(compute,'_blocks',fail)
        if stage=='graph':monkeypatch.setattr(compute,'_d8_stream_graph',fail)
        if stage=='line':monkeypatch.setattr(compute,'LineString',fail)
        if stage=='frame':monkeypatch.setattr(compute.gpd,'GeoDataFrame',fail)
        with pytest.raises(RuntimeError,match='injected stream vector failure'):compute.raster_stream_to_vector_d8(source,mask,1,1)
        assert set(source.workspace.iterdir())==prior
    assert [p.read_bytes() for p in paths]==before


@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('case',['direct','threshold','filled','empty','invalid','isolated','break','cycle'])
def test_d8_stream_vector_real_async_save_composition_bilingual_failure_and_cleanup(tmp_path,monkeypatch,case,language):
    import api_server
    import workflow_engine
    import geopandas as gpd
    import operators.raster_compute as compute
    values=np.array([[3.,2.,1.,0.]]) if case=='filled' else np.array([[1.,1.,1.,0.]])
    directions=np.array([[1.,1.,1.,0.]])
    streams=np.ones_like(directions)
    if case=='empty':streams[:]=0
    if case=='invalid':values[:]=np.nan;directions[:]=np.nan;streams[:]=np.nan
    if case=='isolated':streams[:]=0;streams[0,-1]=1
    if case=='break':streams[0,1]=0
    if case=='cycle':values[0,:2]=[1,16]
    paths=_strahler_files(tmp_path,values,streams);before=[p.read_bytes() for p in paths];target=tmp_path/'river.gpkg'
    tasks=[{'id':'source','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(paths[0])}}];prior='source'
    if case=='filled':
        tasks.extend([{'id':'fill','operator':'raster_fill_depressions','depends_on':['source'],'params':{'input_raster':{'$ref':'source'},'band':1,'elevation_unit':'metre'}},
          {'id':'direction','operator':'raster_flow_direction_d8','depends_on':['fill'],'params':{'input_raster':{'$ref':'fill'},'band':1,'elevation_unit':'metre'}}]);prior='direction'
        directions=_d8_bellman_ford_oracle(_fill_level_reachability_oracle(values))
    if case=='threshold':
        tasks.extend([{'id':'accumulate','operator':'raster_flow_accumulation_d8','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':1}},
          {'id':'streams','operator':'raster_reclassify','depends_on':['accumulate'],'params':{'input_raster':{'$ref':'accumulate'},'band':1,'rules':[{'min':None,'max':1,'class':0},{'min':1,'max':None,'class':1}]}}]);streams=np.where(_accumulation_walk_oracle(directions)>=1,1.,0.)
    else:tasks.append({'id':'streams','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(paths[1])}})
    tasks.extend([{'id':'lines','operator':'raster_stream_to_vector_d8','depends_on':[prior,'streams'],'params':{'input_raster':{'$ref':prior},'stream_raster':{'$ref':'streams'},'band':1,'stream_band':1}},
      {'id':'save','operator':'save','depends_on':['lines'],'params':{'input_df':{'$ref':'lines'},'path':target.name,'connection_info':{'mount_path':str(tmp_path)}}}])
    workspaces=[];execute=workflow_engine._execute_operator
    def capture(name,params):
        if name=='raster_stream_to_vector_d8':workspaces.extend([params['input_raster'].workspace,params['stream_raster'].workspace])
        return execute(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',capture)
    client=api_server.app.test_client();response=client.post('/api/workflow',headers={'Accept-Language':language},json={'workflow_def':{'tasks':tasks},'input_data':{},'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert response.status_code==202,response.json
    deadline=time.monotonic()+15
    while time.monotonic()<deadline:
        status=client.get('/api/executions/'+response.json['execution_id'],headers={'Accept-Language':'en' if language=='zh-cn' else 'zh-cn'}).json
        if status['status'] in ('success','failed'):break
        time.sleep(.01)
    failed=case in ('isolated','break','cycle');assert status['status']==('failed' if failed else 'success'),status
    assert workspaces and all(not p.exists() for p in workspaces) and [p.read_bytes() for p in paths]==before
    if failed:
        assert not target.exists()
        assert '任务 lines 执行失败: '+compute._D8_STREAM_VECTOR_MESSAGES[case][language=='en'] in status['error']
    else:
        _assert_stream_vector_complete(gpd.read_file(target),directions,streams,(500000,2,0,3000000,0,-3),1.,3857)
        assert target.stat().st_size>0
    assert client.post('/api/operators/raster_stream_to_vector_d8/invoke',json={'params':{}}).status_code==403


@pytest.mark.parametrize('code,offset',list(_STRAHLER_STEPS.items()))
def test_d8_stream_vector_eight_direction_centres_and_anisotropic_diagonal(code,offset):
    import operators.raster_compute as compute
    directions=np.full((3,3),np.nan);directions[1,1]=code
    dy,dx=offset;directions[1+dy,1+dx]=0
    streams=np.where(np.isfinite(directions),1.,np.nan);transform=(500000,7,0,3000000,0,-11)
    result=compute._d8_stream_lines(directions,streams==1,transform,1.,'EPSG:3857')
    _assert_stream_vector_complete(result,directions,streams,transform,1.,3857)
    assert result.length_m.iloc[0]==pytest.approx(math.hypot(dx*7,dy*11))


def test_d8_stream_vector_selected_second_band_exact_mask_alpha_nodata_and_zero(tmp_path):
    import operators.raster_compute as compute
    values=np.zeros((3,7));values[0,:3]=[1,1,0];values[1,:]=[np.inf,-9999,np.nan,255,255,255,0]
    alpha=np.full(values.shape,255.);alpha[1,3]=0;alpha[1,4]=np.nan;alpha[0,0]=128
    mask=np.full(values.shape,255,np.uint8);mask[1,5]=0
    valid=np.isfinite(values)&(values!=-9999)&np.isfinite(alpha)&(alpha>0)&(mask!=0)
    streams=np.where(valid,0.,values);streams[0,:3]=1
    paths=[]
    for label,data in (('direction',values),('mask',streams)):
        path=create_raster(tmp_path/(label+'.tif'),np.stack([np.full(values.shape,3.),data,alpha]),(500000,2,0,3000000,0,-3),'EPSG:3857')
        ds=gdal.Open(str(path),gdal.GA_Update);ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
        ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);ds=None;paths.append(path)
    before=[p.read_bytes() for p in paths]
    with raster_workspace():
        source,river=[raster_load(source_plan(p)) for p in paths];prior=set(source.workspace.iterdir())
        result=compute.raster_stream_to_vector_d8(source,river,2,2)
        _assert_stream_vector_complete(result,np.where(valid,values,np.nan),np.where(valid,streams,np.nan),(500000,2,0,3000000,0,-3),1.,3857)
        assert len(result)==1 and result.length_m.iloc[0]==4 and set(source.workspace.iterdir())==prior
    assert [p.read_bytes() for p in paths]==before


@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_stream_vector_rejects_partial_coordinate_precision_collapse(language):
    import operators.raster_compute as compute
    directions=np.array([[2.,np.nan],[np.nan,0.]])
    with raster_error_language(language),pytest.raises(ValueError) as error:
        compute._d8_stream_lines(directions,np.isfinite(directions),(1e308,2,0,0,0,-3),1.,'EPSG:3857')
    assert str(error.value)==compute._D8_STREAM_VECTOR_MESSAGES['coordinates'][language=='en']


@pytest.mark.parametrize('warm_cache',[False,True])
def test_profile_environment_never_starts_implicit_subprocess(monkeypatch,warm_cache):
    import subprocess
    monkeypatch.setattr(platform,'_uname_cache',None)
    monkeypatch.setattr(platform,'_platform_cache',{})
    if warm_cache:platform.system();platform.release();platform.machine()
    attempts=[]
    def forbidden(*args,**kwargs):
        attempts.append((args,kwargs))
        raise AssertionError('Raster environment reporting must not create a subprocess')
    monkeypatch.setattr(subprocess,'Popen',forbidden)
    result=_profile_environment()
    assert attempts==[]
    assert result=={'platform':'-'.join((platform.system(),platform.release(),platform.machine())),
        'machine':platform.machine(),'cpu_count':os.cpu_count(),'python':platform.python_version(),
        'gdal':gdal.VersionInfo('RELEASE_NAME'),'gdal_cache_max_bytes':gdal.GetCacheMax()}


# Pour-point snapping oracle deliberately scans all cells with scalar distances.
def _snap_scalar_oracle(counts, points, xstep, ystep, radius):
    valid=np.isfinite(counts);result=np.where(valid,0.,np.nan)
    for row,col in np.argwhere(points>0):
        candidates=[]
        for y,x in np.argwhere(valid):
            distance=math.hypot(int(x-col)*xstep,int(y-row)*ystep)
            if distance<=radius:candidates.append((-counts[y,x],distance,int(y),int(x)))
        _,_,y,x=min(candidates)
        if result[y,x]>0:raise ValueError('oracle collision')
        result[y,x]=points[row,col]
    return result


@pytest.mark.parametrize('seed',range(20))
@pytest.mark.parametrize('radius',[0.,2.,5.,1e308])
def test_snap_pour_points_random_complete_scalar_oracle(tmp_path,seed,radius):
    import operators.raster_compute as compute
    rng=np.random.default_rng(seed);counts=rng.integers(0,5,(7,9)).astype(float);counts[rng.random(counts.shape)<.15]=np.nan
    points=np.where(np.isfinite(counts),0.,np.nan)
    for cell,id in zip(rng.choice(np.flatnonzero(np.isfinite(counts)),3,replace=False),[91,2,2**53-1]):points.flat[cell]=id
    paths=_watershed_files(tmp_path,counts,points);before=[p.read_bytes() for p in paths]
    try:expected=_snap_scalar_oracle(counts,points,2.,3.,radius)
    except ValueError:expected=None
    with raster_workspace():
        inputs=[raster_load(source_plan(p)) for p in paths];workspace=inputs[0].workspace
        if expected is None:
            with pytest.raises(ValueError,match='碰撞'):compute.raster_snap_pour_points(*inputs,1,1,radius)
        else:
            result=compute.raster_snap_pour_points(*inputs,1,1,radius);ds=gdal.Open(str(result.path))
            np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),expected)
            np.testing.assert_array_equal(ds.GetRasterBand(1).GetMaskBand().ReadAsArray()!=0,np.isfinite(counts));ds=None
    assert not workspace.exists() and [p.read_bytes() for p in paths]==before


@pytest.mark.parametrize('radius',[math.nextafter(5.,0.),5.,math.nextafter(5.,math.inf)])
def test_snap_pour_points_inclusive_circle_distance_row_col_decisions(radius):
    import operators.raster_compute as compute
    counts=np.zeros((5,5));points=np.zeros_like(counts);points[2,2]=42
    # 3/4/5 triangle in a rectangular grid. Equal maxima at the four diagonal candidates.
    for row,col in [(1,1),(1,3),(3,1),(3,3)]:counts[row,col]=7
    result=compute._snap_pour_points(counts,points,3.,4.,radius)
    np.testing.assert_array_equal(result,_snap_scalar_oracle(counts,points,3.,4.,radius))
    assert result[1,1]==42 if radius>=5 else result[2,2]==42
    # Equal counts prefer the original centre even if a smaller row occurs first.
    counts[:]=7;result=compute._snap_pour_points(counts,points,3.,4.,radius)
    assert result[2,2]==42


def test_snap_pour_points_crosses_divide_and_invalid_hole_keeps_original_snapshot():
    import operators.raster_compute as compute
    counts=np.array([[1.,np.nan,9.,0.,0.,0.,0.,7.]])
    points=np.array([[777.,np.nan,0.,0.,0.,0.,42.,0.]])
    expected=np.array([[0.,np.nan,777.,0.,0.,0.,0.,42.]])
    for ids in ([777,42],[42,777]):
        changed=points.copy();changed[0,0],changed[0,6]=ids
        output=compute._snap_pour_points(counts,changed,2,3,4)
        np.testing.assert_array_equal(output,np.where(expected==777,ids[0],np.where(expected==42,ids[1],expected)))
    # No direction input or original-point priority can prevent two IDs choosing one cell.
    with pytest.raises(ValueError,match='碰撞'):compute._snap_pour_points(counts,points,2,3,100)
    np.testing.assert_array_equal(counts,[[1.,np.nan,9.,0.,0.,0.,0.,7.]])
    np.testing.assert_array_equal(points,[[777.,np.nan,0.,0.,0.,0.,42.,0.]])


@pytest.mark.parametrize('radius',[None,True,False,'5',-1,math.inf,-math.inf,math.nan,10**400,[],{}])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_snap_pour_points_invalid_radius_bilingual(radius,language):
    import operators.raster_compute as compute
    with raster_error_language(language),pytest.raises(ValueError) as caught:
        compute._snap_pour_points(np.array([[1.]]),np.array([[42.]]),2,3,radius)
    assert str(caught.value)==compute._SNAP_MESSAGES['radius'][language=='en']


@pytest.mark.parametrize('case',['negative_count','fraction_count','unsafe_count','negative_point','fraction_point','unsafe_point','duplicate','empty','all_invalid','coverage','collision','distance'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_snap_pour_points_invalid_values_no_private_output(tmp_path,case,language):
    import operators.raster_compute as compute
    counts=np.array([[0.,3.,1.]]);points=np.array([[42.,0.,0.]])
    if case.endswith('_count'):counts[0,2]={'negative_count':-1,'fraction_count':.5,'unsafe_count':2**53}[case]
    if case.endswith('_point'):points[0,2]={'negative_point':-1,'fraction_point':.5,'unsafe_point':2**53}[case]
    if case=='duplicate':points[0,2]=42
    if case=='empty':points[:]=0
    if case=='all_invalid':counts[:]=points[:]=np.nan
    if case=='coverage':points[0,2]=np.nan
    if case=='collision':points[0,2]=7
    paths=_watershed_files(tmp_path,counts,points)
    if case=='distance':
        for path in paths:
            ds=gdal.Open(str(path),gdal.GA_Update);ds.SetGeoTransform((0,1e100,0,0,0,-3));srs=osr.SpatialReference();srs.ImportFromEPSG(3857);srs.SetLinearUnits('huge',1e250);ds.SetProjection(srs.ExportToWkt());ds=None
    key='counts_values' if case.endswith('_count') else 'points_values' if case.endswith('_point') else 'empty' if case=='all_invalid' else case
    with raster_workspace(),raster_error_language(language):
        inputs=[raster_load(source_plan(p)) for p in paths];workspace=inputs[0].workspace;before=set(workspace.iterdir())
        with pytest.raises(ValueError) as caught:compute.raster_snap_pour_points(*inputs,1,1,20.)
        assert str(caught.value)==compute._SNAP_MESSAGES[key][language=='en']
        assert set(workspace.iterdir())==before
    assert not workspace.exists()


@pytest.mark.parametrize('role',['count','points'])
@pytest.mark.parametrize('band',[None,0,-1,True,1.,'1',2])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_snap_pour_points_both_bands_explicit(tmp_path,role,band,language):
    import operators.raster_compute as compute
    paths=_watershed_files(tmp_path,np.array([[0.,3.]]),np.array([[42.,0.]]))
    with raster_workspace(),raster_error_language(language),pytest.raises(ValueError) as caught:
        compute.raster_snap_pour_points(*[raster_load(source_plan(p)) for p in paths],band if role=='count' else 1,band if role=='points' else 1,2)
    assert str(caught.value)==compute._SNAP_MESSAGES['band' if role=='count' else 'points_band'][language=='en']


@pytest.mark.parametrize('change',['dimensions','origin','resolution','crs','rotation'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_snap_pour_points_same_full_grid(tmp_path,change,language):
    import operators.raster_compute as compute
    paths=_watershed_files(tmp_path,np.array([[0.,3.]]),np.array([[42.,0.]]))
    if change=='dimensions':create_raster(paths[1],np.array([[42.,0.,0.]]),(500000,2,0,3000000,0,-3),'EPSG:3857')
    else:
        ds=gdal.Open(str(paths[1]),gdal.GA_Update)
        if change=='origin':ds.SetGeoTransform((500001,2,0,3000000,0,-3))
        if change=='resolution':ds.SetGeoTransform((500000,3,0,3000000,0,-3))
        if change=='rotation':ds.SetGeoTransform((500000,2,.1,3000000,0,-3))
        if change=='crs':srs=osr.SpatialReference();srs.ImportFromEPSG(32650);ds.SetProjection(srs.ExportToWkt())
        ds=None
    with raster_workspace(),raster_error_language(language),pytest.raises(ValueError) as caught:
        compute.raster_snap_pour_points(*[raster_load(source_plan(p)) for p in paths],1,1,2)
    assert str(caught.value)==compute._SNAP_MESSAGES['alignment'][language=='en']


@pytest.mark.parametrize('role',['count','points'])
@pytest.mark.parametrize('change',['alpha','complex','complex_alpha','scale','offset','geographic','no_crs','south_up','rotation'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_snap_pour_points_grid_and_data_admission(tmp_path,role,change,language):
    import operators.raster_compute as compute
    paths=_watershed_files(tmp_path,np.array([[0.,3.]]),np.array([[42.,0.]]));path=paths[role=='points']
    ds=gdal.Open(str(path),gdal.GA_Update)
    if change=='alpha':ds.GetRasterBand(1).SetColorInterpretation(gdal.GCI_AlphaBand)
    if change=='scale':ds.GetRasterBand(1).SetScale(2)
    if change=='offset':ds.GetRasterBand(1).SetOffset(1)
    if change=='no_crs':ds.SetProjection('')
    if change=='geographic':srs=osr.SpatialReference();srs.ImportFromEPSG(4326);ds.SetProjection(srs.ExportToWkt())
    if change=='south_up':ds.SetGeoTransform((500000,2,0,3000000,0,3))
    if change=='rotation':ds.SetGeoTransform((500000,2,.1,3000000,0,-3))
    ds=None
    if change in ('complex','complex_alpha'):
        ds=gdal.GetDriverByName('GTiff').Create(str(path),2,1,2 if change=='complex_alpha' else 1,gdal.GDT_CFloat64)
        ds.SetGeoTransform((500000,2,0,3000000,0,-3));srs=osr.SpatialReference();srs.ImportFromEPSG(3857);ds.SetProjection(srs.ExportToWkt());ds.GetRasterBand(1).WriteArray(np.array([[1+0j,0+0j]]))
        if change=='complex_alpha':ds.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand);ds.GetRasterBand(2).Fill(255)
        ds=None
    key='scale' if change in ('scale','offset') else 'complex' if change.startswith('complex') else 'alpha' if change=='alpha' else 'grid'
    if role=='points':key='alignment' if key=='grid' else 'points_'+key
    with raster_workspace(),raster_error_language(language),pytest.raises(ValueError) as caught:
        compute.raster_snap_pour_points(*[raster_load(source_plan(p)) for p in paths],1,1,2)
    assert str(caught.value)==compute._SNAP_MESSAGES[key][language=='en']


@pytest.mark.parametrize('role',['count','points'])
@pytest.mark.parametrize('dtype',['Int64','UInt64'])
@pytest.mark.parametrize('value',[2**53-1,2**53,2**53+1,2**63-2])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_snap_pour_points_native_nodata_before_float_safe_integer_admission(tmp_path,role,dtype,value,language):
    import operators.raster_compute as compute
    paths=_watershed_files(tmp_path,np.array([[0.,np.nan]]),np.array([[42.,np.nan]]));path=paths[role=='points']
    ds=gdal.GetDriverByName('GTiff').Create(str(path),2,1,1,getattr(gdal,'GDT_'+dtype));ds.SetGeoTransform((500000,2,0,3000000,0,-3));srs=osr.SpatialReference();srs.ImportFromEPSG(3857);ds.SetProjection(srs.ExportToWkt())
    nodata=2**63-1;band=ds.GetRasterBand(1);(band.SetNoDataValueAsInt64 if dtype=='Int64' else band.SetNoDataValueAsUInt64)(nodata)
    band.WriteArray(np.array([[value,nodata]],dtype=np.int64 if dtype=='Int64' else np.uint64));band=ds=None
    with raster_workspace(),raster_error_language(language):
        inputs=[raster_load(source_plan(p)) for p in paths];workspace=inputs[0].workspace
        if value>2**53-1:
            with pytest.raises(ValueError) as caught:compute.raster_snap_pour_points(*inputs,1,1,0)
            assert str(caught.value)==compute._SNAP_MESSAGES['counts_values' if role=='count' else 'points_values'][language=='en']
        else:
            result=compute.raster_snap_pour_points(*inputs,1,1,0);ds=gdal.Open(str(result.path))
            np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),[[42. if role=='count' else float(value),np.nan]]);ds=None


@pytest.mark.parametrize('unit_name,unit',[('metre',1.),('foot',.3048),('us_survey_foot',1200/3937)])
def test_snap_pour_points_linear_units_radius_and_cross_block_cog(tmp_path,unit_name,unit):
    import operators.raster_compute as compute
    counts=np.zeros((3,515));counts[1,513]=7;points=np.zeros_like(counts);points[1,511]=2**53-1;counts[0,0]=points[0,0]=np.nan
    srs=osr.SpatialReference();srs.ImportFromEPSG(3857);srs.SetLinearUnitsAndUpdateParameters(unit_name,unit)
    paths=tuple(create_raster(tmp_path/(name+'.tif'),values,(500000,2,0,3000000,0,-3),srs.ExportToWkt()) for name,values in [('counts',counts),('points',points)])
    # Radius is expressed against the CRS fact actually persisted by GDAL, without tolerance.
    stored=gdal.Open(str(paths[0]));srs=stored.GetSpatialRef().Clone();unit=srs.GetLinearUnits();stored=None
    before=[p.read_bytes() for p in paths];target=tmp_path/'snap.cog.tif'
    with raster_workspace():
        inputs=[raster_load(source_plan(p)) for p in paths];workspace=inputs[0].workspace
        result=compute.raster_snap_pour_points(*inputs,1,1,4*unit)
        artifact=raster_save(result,target_plan(target),profile='cog',blocksize=128,overview_resampling='nearest')
        ds=gdal.Open(str(target));expected=_snap_scalar_oracle(counts,points,2*unit,3*unit,4*unit)
        np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),expected);assert expected[1,513]==2**53-1
        np.testing.assert_array_equal(ds.GetRasterBand(1).GetMaskBand().ReadAsArray()!=0,np.isfinite(counts))
        assert ds.GetGeoTransform()==(500000,2,0,3000000,0,-3) and ds.GetSpatialRef().IsSame(srs)
        band=ds.GetRasterBand(1);assert band.DataType==gdal.GDT_Float64 and math.isnan(band.GetNoDataValue())
        assert band.GetUnitType()=='' and band.GetScale() in (None,1) and band.GetOffset() in (None,0) and band.GetColorTable() is None
        band=ds=None;assert validate_cog(raster_load(source_plan(target)))['valid']
        assert type(artifact['size_bytes']) is int and artifact['size_bytes']==target.stat().st_size>0
        saved=target.read_bytes()
        with pytest.raises(ValueError):compute.raster_snap_pour_points(*inputs,1,1,-1)
        assert target.read_bytes()==saved
    assert [p.read_bytes() for p in paths]==before


def test_snap_pour_points_multiband_alpha_mask_nonfinite_and_zero(tmp_path):
    import operators.raster_compute as compute
    counts=np.zeros((3,7));counts[1,:]=[np.inf,-9999,np.nan,255,255,255,0]
    alpha=np.full(counts.shape,255.);alpha[1,3]=0;alpha[1,4]=np.nan;alpha[0,0]=128
    mask=np.full(counts.shape,255,np.uint8);mask[1,5]=0
    valid=np.isfinite(counts)&(counts!=-9999)&np.isfinite(alpha)&(alpha>0)&(mask!=0)
    points=np.where(valid,0.,counts);points[0,0]=2**53-1;points[2,2]=42
    path=create_raster(tmp_path/'multi.tif',np.stack([np.full(counts.shape,-1.),counts,points,alpha]),(500000,2,0,3000000,0,-3),'EPSG:3857')
    ds=gdal.Open(str(path),gdal.GA_Update);ds.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand);ds.GetRasterBand(3).SetUnitType('foot')
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
    ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);ds=None;before=path.read_bytes()
    with raster_workspace():
        source=raster_load(source_plan(path));result=compute.raster_snap_pour_points(source,source,2,3,0);ds=gdal.Open(str(result.path))
        np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),np.where(valid,points,np.nan));ds=None
    assert path.read_bytes()==before


@pytest.mark.parametrize('stage',['read','search','write','metadata'])
def test_snap_pour_points_injected_failure_cleanup(tmp_path,monkeypatch,stage):
    import operators.raster_compute as compute
    paths=_watershed_files(tmp_path,np.array([[0.,3.]]),np.array([[42.,0.]]));before=[p.read_bytes() for p in paths]
    with raster_workspace():
        inputs=[raster_load(source_plan(p)) for p in paths];workspace=inputs[0].workspace;existing=set(workspace.iterdir())
        def fail(*a,**kw):raise RuntimeError('injected snapping '+stage)
        if stage=='read':monkeypatch.setattr(compute,'_blocks',fail)
        elif stage=='search':monkeypatch.setattr(compute,'_snap_pour_points',fail)
        elif stage=='write':monkeypatch.setattr(gdal.Band,'WriteArray',fail)
        else:monkeypatch.setattr(compute,'_raster',fail)
        with pytest.raises(RuntimeError,match='injected snapping'):compute.raster_snap_pour_points(*inputs,1,1,2)
        assert set(workspace.iterdir())==existing
    assert not workspace.exists() and [p.read_bytes() for p in paths]==before


@pytest.mark.parametrize('case',['direct','accumulate','watershed','nested','collision','radius','counts_values'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_snap_pour_points_real_async_composition_and_language_freeze(tmp_path,monkeypatch,case,language):
    import api_server,workflow_engine
    import operators.raster_compute as compute
    directions=np.ones((9,11));directions[:,-1]=0;counts=np.tile(np.arange(11,dtype=float),(9,1));points=np.zeros_like(counts);points[4,5]=777
    if case=='nested':points[4,1]=7
    if case=='collision':points[4,6]=7;counts[4,6]=99
    if case=='counts_values':counts[0,0]=-.5
    paths=_watershed_files(tmp_path,directions if case in ('accumulate','watershed','nested') else counts,points);before=[p.read_bytes() for p in paths];target=tmp_path/'snap.cog.tif'
    tasks=[{'id':'source','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(paths[0])}}, {'id':'points','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(paths[1])}}];prior='source'
    if case in ('accumulate','watershed','nested'):
        tasks.append({'id':'counts','operator':'raster_flow_accumulation_d8','depends_on':['source'],'params':{'input_raster':{'$ref':'source'},'band':1}});prior='counts'
    tasks.append({'id':'snap','operator':'raster_snap_pour_points','depends_on':[prior,'points'],'params':{'input_raster':{'$ref':prior},'points_raster':{'$ref':'points'},'band':1,'points_band':1,'radius_metres':-1 if case=='radius' else 2}});prior='snap'
    if case in ('watershed','nested'):
        tasks.append({'id':'watershed','operator':'raster_watershed_d8','depends_on':['source','snap'],'params':{'input_raster':{'$ref':'source'},'points_raster':{'$ref':'snap'},'band':1,'points_band':1}});prior='watershed'
    tasks.append({'id':'save','operator':'raster_save','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'access_plan':target_plan(target),'profile':'cog','blocksize':128,'overview_resampling':'nearest'}})
    workspaces=[];execute=workflow_engine._execute_operator
    def capture(name,params):
        if name=='raster_snap_pour_points':workspaces.extend([params['input_raster'].workspace,params['points_raster'].workspace])
        return execute(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',capture)
    client=api_server.app.test_client();response=client.post('/api/workflow',headers={'Accept-Language':language},json={'workflow_def':{'tasks':tasks},'input_data':{},'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}});assert response.status_code==202,response.json
    deadline=time.monotonic()+15
    while time.monotonic()<deadline:
        status=client.get('/api/executions/'+response.json['execution_id'],headers={'Accept-Language':'en' if language=='zh-cn' else 'zh-cn'}).json
        if status['status'] in ('success','failed'):break
        time.sleep(.01)
    failed=case in ('nested','collision','radius','counts_values');assert status['status']==('failed' if failed else 'success'),status
    assert workspaces and all(not p.exists() for p in workspaces) and [p.read_bytes() for p in paths]==before
    if failed:
        assert not target.exists();messages=compute._D8_WATERSHED_MESSAGES if case=='nested' else compute._SNAP_MESSAGES
        node='watershed' if case=='nested' else 'snap';assert '任务 '+node+' 执行失败: '+messages[case][language=='en'] in status['error']
    else:
        expected=_snap_scalar_oracle(counts,points,2,3,2)
        if case=='watershed':expected=_watershed_walk_oracle(directions,expected)
        ds=gdal.Open(str(target));np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),expected);ds=None
        with raster_workspace():assert validate_cog(raster_load(source_plan(target)))['valid']
        artifact=json.loads(status['result']);assert type(artifact['size_bytes']) is int and artifact['size_bytes']==target.stat().st_size>0
    assert client.post('/api/operators/raster_snap_pour_points/invoke',json={'params':{}}).status_code==403


def test_snap_pour_points_discovery_required_inputs_and_single_owner():
    spec=next(item for item in list_operators() if item['id']=='raster_snap_pour_points')
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read'] and spec['attributes']['resource_groups']==['raster']
    assert spec['category_path']==['栅格处理','像元计算']
    params={p['name']:p for p in spec['parameters']}
    assert set(params)=={'input_raster','points_raster','band','points_band','radius_metres'} and all(p['required'] for p in params.values())
    assert all(params[p]['type']=='raster' and params[p]['param_type']=='input' for p in ('input_raster','points_raster'))
    assert spec['detailed_description']['workflow_example']['params']=={'input_raster':{'$ref':'source'},'points_raster':{'$ref':'points'},'band':1,'points_band':1,'radius_metres':100.}


@pytest.mark.parametrize('seed',range(24))
def test_d8_subcatchments_nested_random_forest_independent_walk_and_conservation(tmp_path,seed):
    import operators.raster_compute as compute
    rng=np.random.default_rng(seed);directions=np.zeros((13,15));rows,cols=np.indices(directions.shape)
    rank=np.minimum.reduce((rows,cols,12-rows,14-cols))
    for row,col in np.argwhere(rank>0):
        candidates=[code for code,(dy,dx) in _STRAHLER_STEPS.items() if rank[row+dy,col+dx]<rank[row,col]]
        directions[row,col]=rng.choice(candidates)
    directions[6,7]=np.nan
    for row,col in np.argwhere(np.isfinite(directions)&(directions!=0)):
        dy,dx=_STRAHLER_STEPS[directions[row,col]]
        if not np.isfinite(directions[row+dy,col+dx]):directions[row,col]=0
    points=np.where(np.isfinite(directions),0.,np.nan)
    # Guarantee a nested pair along an actual downstream edge, plus unrelated branches.
    origin=tuple(rng.choice(np.argwhere(np.isfinite(directions)&(directions!=0))))
    dy,dx=_STRAHLER_STEPS[directions[origin]];target=(origin[0]+dy,origin[1]+dx)
    chosen=[origin,target]
    remaining=[tuple(cell) for cell in np.argwhere(np.isfinite(directions)) if tuple(cell) not in chosen]
    chosen.extend(remaining[i] for i in rng.choice(len(remaining),size=7,replace=False))
    labels=[2**53-1,7,991,3,56,8001,2,44,123]
    for cell,label in zip(chosen,labels):points[cell]=label
    expected=_watershed_walk_oracle(directions,points)
    paths=_watershed_files(tmp_path,directions,points);before=[p.read_bytes() for p in paths]
    with raster_workspace():
        result=compute.raster_subcatchments_d8(*[raster_load(source_plan(p)) for p in paths],1,1);ds=gdal.Open(str(result.path))
        actual=read_band_values(ds.GetRasterBand(1));np.testing.assert_array_equal(actual,expected)
        np.testing.assert_array_equal(ds.GetRasterBand(1).GetMaskBand().ReadAsArray()!=0,np.isfinite(directions));ds=None
    assert [p.read_bytes() for p in paths]==before
    assert sum(np.count_nonzero(actual==label) for label in [0,*labels])==np.count_nonzero(np.isfinite(directions))
    for cell,label in zip(chosen,labels):assert actual[cell]==label
    # Renumbering changes labels only; neither numeric priority nor point scan order decides ownership.
    changed=points.copy()
    for cell,label in zip(chosen,reversed(labels)):changed[cell]=label
    renamed=compute._d8_subcatchments(directions,changed)
    for cell,label in zip(chosen,labels):np.testing.assert_array_equal(renamed[actual==label],np.full(np.count_nonzero(actual==label),changed[cell]))
    with pytest.raises(ValueError,match='嵌套'):compute._d8_watershed(directions,points)


@pytest.mark.parametrize('dy,dx,code',[(dy,dx,code) for code,(dy,dx) in _STRAHLER_STEPS.items()])
def test_d8_subcatchments_all_directions_adjacent_points_each_own_self(dy,dx,code):
    import operators.raster_compute as compute
    directions=np.full((3,3),np.nan);directions[1,1]=code;directions[1+dy,1+dx]=0
    points=np.where(np.isfinite(directions),0.,np.nan);points[1,1]=2**53-1;points[1+dy,1+dx]=7
    np.testing.assert_array_equal(compute._d8_subcatchments(directions,points),points)
    with pytest.raises(ValueError,match='嵌套'):compute._d8_watershed(directions,points)


def test_d8_subcatchments_parallel_branches_and_three_nested_points():
    import operators.raster_compute as compute
    directions=np.full((5,9),np.nan)
    directions[0,0]=2;directions[1,1]=2;directions[2,:]=[1,1,1,1,1,1,1,1,0]
    directions[4,0]=128;directions[3,1]=128
    points=np.where(np.isfinite(directions),0.,np.nan);points[1,1]=900;points[2,3]=7;points[2,6]=2**53-1;points[3,1]=2
    actual=compute._d8_subcatchments(directions,points)
    expected=np.full((5,9),np.nan);expected[0,0]=expected[1,1]=900;expected[4,0]=expected[3,1]=2
    expected[2,:]=[7,7,7,7,2**53-1,2**53-1,2**53-1,0,0]
    np.testing.assert_array_equal(actual,expected)
    # With nonnested selections both public meanings agree, including unselected valid zeros.
    points[2,3]=points[2,6]=0
    np.testing.assert_array_equal(compute._d8_subcatchments(directions,points),compute._d8_watershed(directions,points))


@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('case',['duplicate','empty','all_invalid','negative','fraction','too_large','coverage','code','terminal','outside','invalid_target','cycle'])
def test_d8_subcatchments_invalid_points_and_full_graph_not_hidden_by_selected_point(tmp_path,language,case):
    import operators.raster_compute as compute
    directions=np.array([[1.,1.,1.,0.]]);points=np.array([[0.,777.,0.,9.]])
    if case=='duplicate':points[0,3]=777
    if case=='empty':points[:]=0
    if case=='all_invalid':directions[:]=np.nan;points[:]=np.nan
    if case=='negative':points[0,0]=-1
    if case=='fraction':points[0,0]=.5
    if case=='too_large':points[0,0]=2**53
    if case=='coverage':points[0,3]=np.nan
    if case=='code':directions[0,2]=3
    if case=='terminal':directions=np.ones((3,4));directions[:,-1]=0;directions[1,1]=0;points=np.zeros((3,4));points[0,0]=777
    if case=='outside':directions[0,3]=1
    if case=='invalid_target':directions[0,2]=np.nan;points[0,2]=np.nan
    if case=='cycle':directions[0,3]=16
    key={'all_invalid':'empty','negative':'points_values','fraction':'points_values','too_large':'points_values','outside':'target','invalid_target':'target'}.get(case,case)
    paths=_watershed_files(tmp_path,directions,points);before=[p.read_bytes() for p in paths]
    with raster_workspace(),raster_error_language(language):
        inputs=[raster_load(source_plan(p)) for p in paths];prior=set(inputs[0].workspace.iterdir())
        with pytest.raises(ValueError) as caught:compute.raster_subcatchments_d8(*inputs,1,1)
        assert str(caught.value)==compute._D8_SUBCATCHMENTS_MESSAGES[key][language=='en']
        assert set(inputs[0].workspace.iterdir())==prior
    assert [p.read_bytes() for p in paths]==before


def test_d8_subcatchments_nested_cross_block_cog_long_chain_no_recursion(tmp_path):
    import operators.raster_compute as compute
    directions=np.full((515,519),np.nan);directions[508:,:]=1;directions[508:,-1]=0;directions[510,0]=np.nan
    points=np.where(np.isfinite(directions),0.,np.nan)
    points[508,127]=2**53-1;points[508,512]=7;points[508,518]=900;points[512,256]=42
    paths=_watershed_files(tmp_path,directions,points);before=[p.read_bytes() for p in paths];target=tmp_path/'subcatchments.cog.tif'
    with raster_workspace():
        result=compute.raster_subcatchments_d8(*[raster_load(source_plan(p)) for p in paths],1,1)
        artifact=raster_save(result,target_plan(target),profile='cog',blocksize=128,overview_resampling='nearest')
        assert artifact['size_bytes']==target.stat().st_size>0 and validate_cog(raster_load(source_plan(target)))['valid']
        ds=gdal.Open(str(target));np.testing.assert_array_equal(read_band_values(ds.GetRasterBand(1)),_watershed_walk_oracle(directions,points))
        np.testing.assert_array_equal(ds.GetRasterBand(1).GetMaskBand().ReadAsArray()!=0,np.isfinite(directions))
        source_ds=gdal.Open(str(paths[0]));assert ds.GetGeoTransform()==(500000,2,0,3000000,0,-3) and ds.GetSpatialRef().IsSame(source_ds.GetSpatialRef());source_ds=None
        band=ds.GetRasterBand(1);assert band.GetOverviewCount()>0 and band.GetUnitType()=='' and band.GetColorTable() is None
        assert band.GetScale() in (None,1) and band.GetOffset() in (None,0) and math.isnan(band.GetNoDataValue())
        for i in range(band.GetOverviewCount()):
            overview=band.GetOverview(i).ReadAsArray();assert set(overview[np.isfinite(overview)])<=set([0.,float(2**53-1),7.,900.,42.])
        band=ds=None
    assert [p.read_bytes() for p in paths]==before
    directions=np.ones((1,100001));directions[0,-1]=0;points=np.zeros_like(directions)
    points[0,1]=91;points[0,50000]=7;points[0,-1]=2**53-1
    actual=compute._d8_subcatchments(directions,points)
    np.testing.assert_array_equal(actual[0,:2],np.full(2,91.));np.testing.assert_array_equal(actual[0,2:50001],np.full(49999,7.))
    np.testing.assert_array_equal(actual[0,50001:],np.full(50000,float(2**53-1)))
    with pytest.raises(ValueError,match='嵌套'):compute._d8_watershed(directions,points)


def _d8_area_walk_oracle(directions, cell_area):
    """Each source contributes its footprint to itself and every downstream cell."""
    offsets = {1:(0,1), 2:(1,1), 4:(1,0), 8:(1,-1), 16:(0,-1), 32:(-1,-1), 64:(-1,0), 128:(-1,1)}
    contributions = {}
    for origin in np.argwhere(np.isfinite(directions)):
        row, col = map(int, origin)
        seen = set()
        while True:
            assert (row, col) not in seen and np.isfinite(directions[row, col])
            seen.add((row, col))
            contributions.setdefault((row, col), []).append(cell_area)
            if directions[row, col] == 0:
                break
            dy, dx = offsets[directions[row, col]]
            row, col = row + dy, col + dx
            assert 0 <= row < directions.shape[0] and 0 <= col < directions.shape[1]
    result = np.full(directions.shape, np.nan)
    for cell, areas in contributions.items():
        result[cell] = math.fsum(areas)
    return result


@pytest.mark.parametrize('seed', range(8))
@pytest.mark.parametrize('unit,factor', [('metre', 1.), ('foot', .3048), ('US survey foot', 1200/3937)])
def test_d8_area_random_forests_full_oracle_rectangular_units_count_equivalence(tmp_path, seed, unit, factor):
    import operators.raster_compute as compute
    rng = np.random.default_rng(seed)
    directions = np.zeros((9, 11))
    rows, cols = np.indices(directions.shape)
    ranks = np.minimum.reduce((rows, cols, 8-rows, 10-cols))
    offsets = [(-1,-1,32),(-1,0,64),(-1,1,128),(0,-1,16),(0,1,1),(1,-1,8),(1,0,4),(1,1,2)]
    for row, col in np.argwhere(ranks > 0):
        directions[row, col] = rng.choice([code for dy, dx, code in offsets if ranks[row+dy,col+dx] < ranks[row,col]])
    directions[4,5] = np.nan
    for row, col in np.argwhere(np.isfinite(directions) & (directions != 0)):
        dy, dx = next((dy,dx) for dy,dx,code in offsets if code == directions[row,col])
        if not np.isfinite(directions[row+dy,col+dx]):
            directions[row,col] = 0
    # A directed boundary cell must continue, rather than terminate at the edge.
    directions[0,0] = 1; directions[0,1] = 2; directions[1,2] = 64; directions[0,2] = 0
    directions[1,1] = 64; directions[7,9] = 4  # Keep the two asserted corner sources independent.
    crs = osr.SpatialReference(); crs.ImportFromEPSG(3857); crs.SetLinearUnitsAndUpdateParameters(unit, factor)
    grid = (500000, 3, 0, 3000000, 0, -4)
    path = create_raster(tmp_path/'source.tif', directions, grid, crs.ExportToWkt())
    before = path.read_bytes()
    with raster_workspace():
        source = raster_load(source_plan(path)); prior = set(source.workspace.iterdir())
        result = compute.raster_flow_accumulation_d8(source, 1, 'area')
        ds = gdal.Open(str(result.path)); band = ds.GetRasterBand(1)
        actual = read_band_values(band); cell_area = 12 * factor * factor
        np.testing.assert_allclose(actual, _d8_area_walk_oracle(directions, cell_area), rtol=2e-15, atol=0, equal_nan=True)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0, np.isfinite(directions))
        assert math.fsum(actual[directions==0]) == pytest.approx(np.count_nonzero(np.isfinite(directions))*cell_area, rel=2e-15)
        assert band.GetUnitType() == 'm2' and band.DataType == gdal.GDT_Float64 and math.isnan(band.GetNoDataValue())
        assert band.GetScale() is None and band.GetOffset() is None and band.GetColorTable() is None
        assert ds.GetGeoTransform() == grid and ds.GetSpatialRef().IsSame(crs)
        assert set(source.workspace.iterdir()) == prior | {result.path}
        band = ds = None
        for args in ((), ('count',)):
            result = compute.raster_flow_accumulation_d8(source, 1, *args)
            ds = gdal.Open(str(result.path)); band = ds.GetRasterBand(1)
            np.testing.assert_array_equal(read_band_values(band), _accumulation_walk_oracle(directions))
            assert band.GetUnitType() == ''
            band = ds = None
    assert path.read_bytes() == before


@pytest.mark.parametrize('directions', [np.array([[1.,1.,0.]]), np.array([[4.],[4.],[0.]]), np.array([[0.]]), np.full((4,5), np.nan)])
def test_d8_area_chain_self_terminal_singleton_and_all_invalid(tmp_path, directions):
    import operators.raster_compute as compute
    path = create_raster(tmp_path/'source.tif', directions, (0,2,0,100,0,-3), 'EPSG:3857')
    with raster_workspace():
        result = compute.raster_flow_accumulation_d8(raster_load(source_plan(path)), 1, 'area')
        ds = gdal.Open(str(result.path)); band = ds.GetRasterBand(1)
        np.testing.assert_array_equal(read_band_values(band), _d8_area_walk_oracle(directions, 6))
        assert band.GetUnitType() == 'm2'
        band = ds = None


@pytest.mark.parametrize('bad', ['AREA', 'Count', ' area', 'area ', '', None, True, 1, [], {}, b'area'])
@pytest.mark.parametrize('language,phrase', [('zh-cn', '只允许精确小写'), ('en', 'must be exactly count or area')])
def test_d8_area_invalid_measure_before_input_read(monkeypatch, bad, language, phrase):
    import operators.raster_compute as compute
    def forbidden(*args):
        pytest.fail('invalid measure read a source')
    monkeypatch.setattr(compute, '_open', forbidden)
    with raster_error_language(language), pytest.raises(ValueError, match=phrase):
        compute.raster_flow_accumulation_d8(None, 1, bad)


@pytest.mark.parametrize('language', ['zh-cn', 'en'])
@pytest.mark.parametrize('case,phrase', [('code','invalid direction code'), ('terminal','terminal 0'), ('outside','in-bounds valid pixel'), ('invalid','in-bounds valid pixel'), ('cycle','direction cycle'), ('band','one-based direction band'), ('rotation','projected north-up')])
def test_d8_area_full_graph_or_band_failure_no_output(tmp_path, language, case, phrase):
    import operators.raster_compute as compute
    directions = np.array([[1., 1., 0.]])
    grid = (0,2,0,100,0,-3); band = 1
    if case == 'code': directions[0,0] = 3
    if case == 'terminal': directions = np.zeros((3,3))
    if case == 'outside': directions[0,-1] = 1
    if case == 'invalid': directions[0,1] = np.nan
    if case == 'cycle': directions[0,1] = 16
    if case == 'band': band = 0
    if case == 'rotation': grid = (0,2,.1,100,0,-3)
    zh = {'code':'非法方向码', 'terminal':'有效终点0', 'outside':'图内有效像元', 'invalid':'图内有效像元', 'cycle':'方向循环', 'band':'一基方向数据波段', 'rotation':'北向无旋转'}
    path = create_raster(tmp_path/'source.tif', directions, grid, 'EPSG:3857'); before = path.read_bytes()
    with raster_workspace(), raster_error_language(language):
        source = raster_load(source_plan(path)); prior = set(source.workspace.iterdir())
        with pytest.raises(ValueError, match=zh[case] if language == 'zh-cn' else phrase):
            compute.raster_flow_accumulation_d8(source, band, 'area')
        assert set(source.workspace.iterdir()) == prior
    assert path.read_bytes() == before


@pytest.mark.parametrize('language', ['zh-cn', 'en'])
@pytest.mark.parametrize('case', ['overflow', 'underflow', 'sum-overflow'])
def test_d8_area_numeric_failures_preserve_count_and_no_output(tmp_path, case, language):
    import operators.raster_compute as compute
    width, height = {'overflow':(1e200,1e200), 'underflow':(1e-200,1e-200), 'sum-overflow':(1e154,1e154)}[case]
    path = create_raster(tmp_path/'source.tif', np.array([[1.,0.]]), (0,width,0,0,0,-height), 'EPSG:3857')
    with raster_workspace(), raster_error_language(language):
        source = raster_load(source_plan(path)); prior = set(source.workspace.iterdir())
        phrase = ('累计结果' if language == 'zh-cn' else 'accumulated result') if case == 'sum-overflow' else ('严格正数平方米' if language == 'zh-cn' else 'strictly positive square metres')
        with pytest.raises(ValueError, match=phrase):
            compute.raster_flow_accumulation_d8(source, 1, 'area')
        assert set(source.workspace.iterdir()) == prior
        result = compute.raster_flow_accumulation_d8(source, 1)
        ds = gdal.Open(str(result.path)); np.testing.assert_array_equal(ds.GetRasterBand(1).ReadAsArray(), [[0.,1.]]); ds = None


@pytest.mark.parametrize('width,height,factor,expected', [(1e200,1e-200,1e100,1e200), (1e-200,1e-200,1e200,1.), (1e200,1e200,1e-200,1.), (float.fromhex('0x0.0000000000001p-1022'),1.,1.,float.fromhex('0x0.0000000000001p-1022'))])
def test_d8_area_exact_product_avoids_intermediate_numeric_loss(width, height, factor, expected):
    import operators.raster_compute as compute
    assert compute._d8_cell_area(width, height, factor) == pytest.approx(expected, rel=1e-15, abs=0)


@pytest.mark.parametrize('stage', ['read', 'graph', 'write', 'metadata'])
def test_d8_area_injected_failure_cleans_private_output(tmp_path, monkeypatch, stage):
    import operators.raster_compute as compute
    path = create_raster(tmp_path/'source.tif', np.array([[1.,0.]]), (0,2,0,100,0,-3), 'EPSG:3857')
    with raster_workspace():
        source = raster_load(source_plan(path)); prior = set(source.workspace.iterdir())
        def fail(*args, **kwargs): raise RuntimeError('injected area failure')
        if stage == 'read': monkeypatch.setattr(compute, '_blocks', fail)
        if stage == 'graph': monkeypatch.setattr(compute, '_d8_accumulation', fail)
        if stage == 'write': monkeypatch.setattr(gdal.Band, 'WriteArray', fail)
        if stage == 'metadata': monkeypatch.setattr(compute, '_raster', fail)
        with pytest.raises(RuntimeError, match='injected area failure'):
            compute.raster_flow_accumulation_d8(source, 1, 'area')
        assert set(source.workspace.iterdir()) == prior


@pytest.mark.parametrize('dtype', [gdal.GDT_Int64, gdal.GDT_UInt64])
@pytest.mark.parametrize('language', ['zh-cn', 'en'])
def test_d8_area_native_nodata_does_not_hide_adjacent_illegal_integer(tmp_path, dtype, language):
    import operators.raster_compute as compute
    path = tmp_path/'integer.tif'; ds = gdal.GetDriverByName('GTiff').Create(str(path), 3, 1, 1, dtype)
    ds.SetGeoTransform((0,2,0,100,0,-3)); crs = osr.SpatialReference(); crs.ImportFromEPSG(3857); ds.SetProjection(crs.ExportToWkt())
    band = ds.GetRasterBand(1); band.SetNoDataValue(2**53)
    band.WriteArray(np.array([[2**53,2**53+1,0]], dtype=np.int64 if dtype == gdal.GDT_Int64 else np.uint64)); band = ds = None
    with raster_workspace(), raster_error_language(language):
        source = raster_load(source_plan(path)); prior = set(source.workspace.iterdir())
        with pytest.raises(ValueError, match='非法方向码' if language == 'zh-cn' else 'invalid direction code'):
            compute.raster_flow_accumulation_d8(source, 1, 'area')
        assert set(source.workspace.iterdir()) == prior


def test_d8_area_cross_block_cog_full_mask_overviews_grid_and_strict_size(tmp_path, monkeypatch):
    import operators.raster_compute as compute
    height, width = 515, 519
    directions = np.ones((height,width)); directions[:,-1] = 4; directions[-1,-1] = 0
    expected = np.broadcast_to(np.arange(1,width+1), directions.shape).copy()*6.
    expected[:,-1] = np.arange(1,height+1)*width*6.
    directions[0,0] = np.nan; expected[0,0] = np.nan; expected[0,1:] -= 6; expected[1:,-1] -= 6
    path = create_raster(tmp_path/'source.tif', directions, (500000,2,0,3000000,0,-3), 'EPSG:3857')
    before = path.read_bytes(); target = tmp_path/'area.cog.tif'
    reads = []; blocks = compute._blocks
    def bounded(*args, **kwargs):
        for x,y,arrays,masks in blocks(*args, **kwargs):
            assert all(max(a.shape) <= 512 for a in arrays); reads.append((x,y)); yield x,y,arrays,masks
    monkeypatch.setattr(compute, '_blocks', bounded)
    with raster_workspace():
        result = compute.raster_flow_accumulation_d8(raster_load(source_plan(path)), 1, 'area')
        artifact = raster_save(result, target_plan(target), profile='cog', blocksize=128, overview_resampling='nearest')
        assert type(artifact['size_bytes']) is int and artifact['size_bytes'] == target.stat().st_size > 0
    ds = gdal.Open(str(target)); band = ds.GetRasterBand(1)
    np.testing.assert_array_equal(read_band_values(band), expected)
    np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0, np.isfinite(directions))
    assert band.GetUnitType() == 'm2' and ds.GetGeoTransform() == (500000.,2.,0.,3000000.,0.,-3.)
    assert ds.GetSpatialRef().GetAuthorityCode(None) == '3857' and band.GetOverviewCount() > 0
    for i in range(band.GetOverviewCount()):
        overview = band.GetOverview(i)
        # Nearest-neighbour output remains a source value; it never averages areas.
        actual_overview = overview.ReadAsArray()
        assert np.all(np.isin(actual_overview[np.isfinite(actual_overview)], expected[np.isfinite(expected)]))
    band = ds = None
    with raster_workspace(): assert validate_cog(raster_load(source_plan(target)))['valid']
    assert path.read_bytes() == before and (512,512) in reads


@pytest.mark.parametrize('language', ['zh-cn', 'en'])
@pytest.mark.parametrize('case', ['saved-direction', 'dem-direction', 'fill-direction', 'empty', 'cycle'])
def test_d8_area_real_async_save_composition_and_failure_cleanup(tmp_path, monkeypatch, language, case):
    import api_server, workflow_engine
    values = np.tile(np.arange(11,0,-1,dtype=float), (9,1)); directions = np.ones(values.shape); directions[:,-1] = 0
    if case == 'fill-direction': values[:] = 10; values[1:-1,1:-1] = -2
    if case == 'empty': directions[:] = np.nan
    if case == 'cycle': directions[0,1] = 16
    data = directions if case in ('saved-direction', 'empty', 'cycle') else values
    path = create_raster(tmp_path/'source.tif', data, (0,2,0,100,0,-3), 'EPSG:3857'); before = path.read_bytes(); target = tmp_path/'area.cog.tif'
    tasks = [{'id':'source', 'operator':'raster_load', 'depends_on':[], 'params':{'access_plan':source_plan(path)}}]; parent = 'source'
    if case == 'fill-direction':
        tasks.append({'id':'fill', 'operator':'raster_fill_depressions', 'depends_on':[parent], 'params':{'input_raster':{'$ref':parent}, 'band':1, 'elevation_unit':'metre'}}); parent = 'fill'
    if case in ('dem-direction', 'fill-direction'):
        tasks.append({'id':'direction', 'operator':'raster_flow_direction_d8', 'depends_on':[parent], 'params':{'input_raster':{'$ref':parent}, 'band':1, 'elevation_unit':'metre'}}); parent = 'direction'
        directions = _d8_bellman_ford_oracle(_fill_level_reachability_oracle(values) if case == 'fill-direction' else values)
    tasks.extend([{'id':'accumulate', 'operator':'raster_flow_accumulation_d8', 'depends_on':[parent], 'params':{'input_raster':{'$ref':parent}, 'band':1, 'measure':'area'}},
                  {'id':'save', 'operator':'raster_save', 'depends_on':['accumulate'], 'params':{'input_raster':{'$ref':'accumulate'}, 'access_plan':target_plan(target), 'profile':'cog', 'blocksize':128}}])
    workspaces = []; execute = workflow_engine._execute_operator
    def capture(name, params):
        if name == 'raster_flow_accumulation_d8': workspaces.append(params['input_raster'].workspace)
        return execute(name, params)
    monkeypatch.setattr(workflow_engine, '_execute_operator', capture)
    client = api_server.app.test_client()
    response = client.post('/api/workflow', headers={'Accept-Language':language}, json={'workflow_def':{'tasks':tasks}, 'input_data':{}, 'runtime':{'tenant_id':7, 'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert response.status_code == 202, response.json
    deadline = time.monotonic()+15
    while time.monotonic() < deadline:
        status = client.get('/api/executions/'+response.json['execution_id'], headers={'Accept-Language':'en' if language == 'zh-cn' else 'zh-cn'}).json
        if status['status'] in ('success','failed'): break
        time.sleep(.01)
    assert status['status'] == ('failed' if case == 'cycle' else 'success'), status
    assert workspaces and all(not p.exists() for p in workspaces) and path.read_bytes() == before
    if case == 'cycle':
        assert not target.exists() and ('方向循环' if language == 'zh-cn' else 'direction cycle') in status['error']
    else:
        ds = gdal.Open(str(target)); band = ds.GetRasterBand(1)
        np.testing.assert_array_equal(read_band_values(band), _d8_area_walk_oracle(directions, 6))
        assert band.GetUnitType() == 'm2'; band = ds = None
        with raster_workspace(): assert validate_cog(raster_load(source_plan(target)))['valid']
        artifact = json.loads(status['result']); assert type(artifact['size_bytes']) is int and artifact['size_bytes'] == target.stat().st_size > 0


def test_d8_area_multiband_effective_mask_alpha_nodata_and_legal_terminal(tmp_path):
    import operators.raster_compute as compute
    path = tmp_path/'bands.tif'
    values = np.zeros((3,7)); values[1,:] = [np.inf,-9999,np.nan,255,255,255,0]
    alpha = np.full(values.shape,255.); alpha[1,3] = 0; alpha[1,4] = np.nan; alpha[0,0] = 128
    mask = np.full(values.shape,255,np.uint8); mask[1,5] = 0
    ds = gdal.GetDriverByName('GTiff').Create(str(path),7,3,3,gdal.GDT_Float64)
    ds.SetGeoTransform((0,2,0,100,0,-3)); crs = osr.SpatialReference(); crs.ImportFromEPSG(3857); ds.SetProjection(crs.ExportToWkt())
    for i,array in enumerate((np.full(values.shape,.25), values, alpha),1):
        ds.GetRasterBand(i).WriteArray(array); ds.GetRasterBand(i).SetNoDataValue(-9999)
    ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'): ds.CreateMaskBand(gdal.GMF_PER_DATASET)
    ds.GetRasterBand(1).GetMaskBand().WriteArray(mask); ds = None
    before = path.read_bytes()
    expected = np.where(np.isfinite(values)&(values!=-9999)&np.isfinite(alpha)&(alpha>0)&(mask!=0),6.,np.nan)
    with raster_workspace():
        result = compute.raster_flow_accumulation_d8(raster_load(source_plan(path)),2,'area')
        ds = gdal.Open(str(result.path)); band = ds.GetRasterBand(1)
        np.testing.assert_array_equal(read_band_values(band),expected)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected))
        assert band.GetUnitType() == 'm2'; band = ds = None
    assert path.read_bytes() == before


@pytest.mark.parametrize('case,phrase', [('scale','scale/offset'), ('offset','scale/offset'), ('alpha','alpha'), ('complex','complex'), ('complex-alpha','complex'), ('geographic','north-up'), ('no-crs','north-up'), ('south-up','north-up')])
def test_d8_area_preserves_direction_encoding_and_grid_admission(tmp_path,case,phrase):
    import operators.raster_compute as compute
    dtype = gdal.GDT_CFloat64 if case in ('complex','complex-alpha') else gdal.GDT_Float64
    path = tmp_path/'source.tif'; ds = gdal.GetDriverByName('GTiff').Create(str(path),3,1,2 if case=='complex-alpha' else 1,dtype)
    ds.SetGeoTransform((0,2,0,0,0,3 if case=='south-up' else -3))
    if case != 'no-crs':
        crs = osr.SpatialReference(); crs.ImportFromEPSG(4326 if case=='geographic' else 3857); ds.SetProjection(crs.ExportToWkt())
    band = ds.GetRasterBand(1); band.WriteArray(np.array([[1.,1.,0.]]))
    if case == 'scale': band.SetScale(2)
    if case == 'offset': band.SetOffset(1)
    if case == 'alpha': band.SetColorInterpretation(gdal.GCI_AlphaBand)
    if case == 'complex-alpha': ds.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand)
    band = ds = None
    with raster_workspace(), raster_error_language('en'):
        source = raster_load(source_plan(path)); prior = set(source.workspace.iterdir())
        with pytest.raises(ValueError,match=phrase): compute.raster_flow_accumulation_d8(source,1,'area')
        assert set(source.workspace.iterdir()) == prior


@pytest.mark.parametrize('language', ['zh-cn','en'])
def test_d8_area_count_exact_limit_still_applies_before_area_output(tmp_path,monkeypatch,language):
    import operators.raster_compute as compute
    path = create_raster(tmp_path/'source.tif',np.array([[1.,1.,0.]]),(0,2,0,100,0,-3),'EPSG:3857')
    monkeypatch.setattr(compute,'_D8_MAX_EXACT_COUNT',1)
    with raster_workspace(), raster_error_language(language):
        source = raster_load(source_plan(path)); prior = set(source.workspace.iterdir())
        with pytest.raises(ValueError,match='精确整数上限' if language=='zh-cn' else 'exact integer limit'):
            compute.raster_flow_accumulation_d8(source,1,'area')
        assert set(source.workspace.iterdir()) == prior



def _stream_distance_walk_oracle(directions, streams, width_metres=2., height_metres=3.):
    """Independent path from every origin, stopping before leaving the first stream cell."""
    offsets = {1:(0,1), 2:(1,1), 4:(1,0), 8:(1,-1), 16:(0,-1), 32:(-1,-1), 64:(-1,0), 128:(-1,1)}
    result = np.full(directions.shape, np.nan)
    for origin in np.argwhere(np.isfinite(directions)):
        row, col = map(int, origin); seen = set(); lengths = []
        while True:
            assert (row,col) not in seen and np.isfinite(directions[row,col])
            seen.add((row,col))
            if streams[row,col] == 1:
                result[tuple(origin)] = math.fsum(lengths); break
            if directions[row,col] == 0: break
            dy,dx = offsets[directions[row,col]]
            lengths.append(math.hypot(width_metres if dx else 0., height_metres if dy else 0.))
            row,col = row+dy,col+dx
            assert 0 <= row < directions.shape[0] and 0 <= col < directions.shape[1]
    return result


@pytest.mark.parametrize('seed', range(10))
@pytest.mark.parametrize('epsg,factor', [(3857,1.), (2222,.3048), (2277,1200/3937)])
def test_d8_stream_distance_full_random_forest_units_first_hit_and_unreachable(tmp_path,seed,epsg,factor):
    import operators.raster_compute as compute
    rng = np.random.default_rng(seed); rows,cols = np.indices((9,11)); ranks = np.minimum.reduce((rows,cols,8-rows,10-cols))
    directions = np.zeros((9,11)); directions[4,5] = np.nan
    offsets = {1:(0,1), 2:(1,1), 4:(1,0), 8:(1,-1), 16:(0,-1), 32:(-1,-1), 64:(-1,0), 128:(-1,1)}
    for row,col in np.argwhere((ranks>0)&np.isfinite(directions)):
        choices = [code for code,(dy,dx) in offsets.items() if ranks[row+dy,col+dx]<ranks[row,col] and np.isfinite(directions[row+dy,col+dx])]
        directions[row,col] = rng.choice(choices)
    directions[0,0] = 1; directions[0,1] = 2; directions[1,2] = 64; directions[0,2] = 0
    directions[1,1] = 64; directions[7,9] = 4  # Keep the two asserted corner sources independent.
    streams = np.where(np.isfinite(directions),0.,np.nan)
    # Mark complete downstream routes; other components intentionally miss all streams.
    for origin in np.argwhere((rng.random(directions.shape)<.1)&np.isfinite(directions)):
        row,col = map(int,origin)
        while True:
            streams[row,col] = 1
            if directions[row,col] == 0: break
            dy,dx = offsets[directions[row,col]]; row,col = row+dy,col+dx
    streams[0,0] = 0; streams[0,1] = 1; streams[1,2] = 1; streams[0,2] = 1
    streams[-1,-1] = 0
    paths = _strahler_files(tmp_path,directions,streams)
    crs = osr.SpatialReference(); crs.ImportFromEPSG(epsg)
    for path in paths:
        ds = gdal.Open(str(path),gdal.GA_Update); ds.SetSpatialRef(crs); ds.GetRasterBand(1).SetUnitType('classification'); ds=None
    before = [p.read_bytes() for p in paths]
    expected = _stream_distance_walk_oracle(directions,streams,2*factor,3*factor)
    with raster_workspace():
        source,mask = [raster_load(source_plan(p)) for p in paths]; prior=set(source.workspace.iterdir())
        result = compute.raster_distance_to_stream_d8(source,mask,1,1)
        ds=gdal.Open(str(result.path)); band=ds.GetRasterBand(1); actual=read_band_values(band)
        np.testing.assert_allclose(actual,expected,rtol=1e-12,atol=1e-9,equal_nan=True)
        assert actual[0,0] == pytest.approx(2*factor) and actual[0,1] == actual[1,2] == actual[0,2] == 0
        assert np.isnan(actual[-1,-1]) and np.isnan(actual[4,5])
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected))
        assert band.GetUnitType()=='metre' and band.DataType==gdal.GDT_Float64 and math.isnan(band.GetNoDataValue())
        assert band.GetScale() is None and band.GetOffset() is None and band.GetColorTable() is None
        assert ds.GetGeoTransform()==(500000.,2.,0.,3000000.,0.,-3.) and ds.GetSpatialRef().IsSame(crs)
        assert set(source.workspace.iterdir())==prior|{result.path}; band=ds=None
    assert [p.read_bytes() for p in paths]==before


@pytest.mark.parametrize('dy,dx,code',[(-1,-1,32),(-1,0,64),(-1,1,128),(0,-1,16),(0,1,1),(1,-1,8),(1,0,4),(1,1,2)])
def test_d8_stream_distance_all_eight_steps_first_hit_zero(dy,dx,code):
    import operators.raster_compute as compute
    directions=np.full((3,3),np.nan); directions[1,1]=code; directions[1+dy,1+dx]=0
    streams=np.zeros((3,3),bool); streams[1+dy,1+dx]=True
    actual=compute._d8_distance_to_stream(directions,streams,3.,4.)
    np.testing.assert_allclose(actual,_stream_distance_walk_oracle(directions,streams,3,4),rtol=1e-12,atol=1e-9,equal_nan=True)
    assert actual[1,1]==(5 if dy and dx else 4 if dy else 3) and actual[1+dy,1+dx]==0


@pytest.mark.parametrize('directions,streams',[
    (np.array([[1.,1.,0.]]),np.array([[0.,1.,1.]])),
    (np.array([[4.],[4.],[0.]]),np.array([[0.],[0.],[1.]])),
    (np.array([[0.]]),np.array([[1.]])),(np.array([[0.]]),np.array([[0.]])),
    (np.array([[1.,1.,0.]]),np.zeros((1,3))),
    (np.full((4,5),np.nan),np.full((4,5),np.nan)),
])
def test_d8_stream_distance_empty_network_all_invalid_and_single_axes(tmp_path,directions,streams):
    import operators.raster_compute as compute
    paths=_strahler_files(tmp_path,directions,streams)
    with raster_workspace():
        result=compute.raster_distance_to_stream_d8(*[raster_load(source_plan(p)) for p in paths],1,1)
        ds=gdal.Open(str(result.path)); band=ds.GetRasterBand(1); expected=_stream_distance_walk_oracle(directions,streams)
        np.testing.assert_allclose(read_band_values(band),expected,rtol=1e-12,atol=1e-9,equal_nan=True)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected)); band=ds=None


@pytest.mark.parametrize('case',['cycle-after-hit','cycle-off-network','terminal-after-hit','break-after-hit','invalid-after-hit'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_stream_distance_validates_entire_graph_after_first_hit(tmp_path,case,language):
    import operators.raster_compute as compute
    directions=np.array([[1.,1.,0.]])
    streams=np.ones(directions.shape)
    if case=='cycle-after-hit':directions[0,1]=16
    elif case=='cycle-off-network':directions=np.array([[1.,16.,0.]]);streams=np.array([[0.,0.,1.]])
    elif case=='terminal-after-hit':directions=np.zeros((3,3));streams=np.ones((3,3))
    elif case=='break-after-hit':streams[0,1]=0
    else:directions[0,2]=3
    key={'cycle-after-hit':'cycle','cycle-off-network':'cycle','terminal-after-hit':'terminal','break-after-hit':'break','invalid-after-hit':'code'}[case]
    paths=_strahler_files(tmp_path,directions,streams)
    with raster_workspace(),raster_error_language(language):
        source,mask=[raster_load(source_plan(p)) for p in paths]; prior=set(source.workspace.iterdir())
        with pytest.raises(ValueError) as error:compute.raster_distance_to_stream_d8(source,mask,1,1)
        assert str(error.value)==compute._D8_STREAM_DISTANCE_MESSAGES[key][language=='en']
        assert set(source.workspace.iterdir())==prior


@pytest.mark.parametrize('width,height',[(0.,1.),(-1.,1.),(1.,0.),(float('nan'),1.),(1.,float('inf')),(1.7e308,1.7e308)])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_stream_distance_requires_finite_positive_steps_even_empty_network(width,height,language):
    import operators.raster_compute as compute
    with raster_error_language(language),pytest.raises(ValueError) as error:
        compute._d8_distance_to_stream(np.array([[1.,0.]]),np.zeros((1,2),bool),width,height)
    assert str(error.value)==compute._D8_STREAM_DISTANCE_MESSAGES['step'][language=='en']


@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_stream_distance_numeric_range_and_no_hit_not_false_overflow(tmp_path,language):
    import operators.raster_compute as compute
    directions=np.array([[1.,1.,0.]])
    paths=_strahler_files(tmp_path,directions,np.array([[0.,0.,1.]]))
    for path in paths:
        ds=gdal.Open(str(path),gdal.GA_Update);ds.SetGeoTransform((0,1e308,0,100,0,-1));ds=None
    with raster_workspace(),raster_error_language(language):
        source,mask=[raster_load(source_plan(p)) for p in paths]; prior=set(source.workspace.iterdir())
        with pytest.raises(ValueError) as error:compute.raster_distance_to_stream_d8(source,mask,1,1)
        assert str(error.value)==compute._D8_STREAM_DISTANCE_MESSAGES['overflow'][language=='en']
        assert set(source.workspace.iterdir())==prior
    assert np.isnan(compute._d8_distance_to_stream(directions,np.zeros((1,3),bool),1e308,1.)).all()
    # A first hit stops accumulation even when the remaining route would overflow.
    np.testing.assert_array_equal(compute._d8_distance_to_stream(directions,np.ones((1,3),bool),1e308,1.),np.zeros((1,3)))
    tiny=compute._d8_distance_to_stream(np.array([[2.,np.nan],[np.nan,0.]]),np.array([[False,False],[False,True]]),1e-200,2e-200)
    assert tiny[0,0]==pytest.approx(math.hypot(1e-200,2e-200),rel=1e-15,abs=0)


@pytest.mark.parametrize('stage',['read','graph','write','metadata'])
def test_d8_stream_distance_injected_failure_cleans_private_outputs(tmp_path,monkeypatch,stage):
    import operators.raster_compute as compute
    paths=_strahler_files(tmp_path,np.array([[1.,0.]]),np.array([[0.,1.]]))
    with raster_workspace():
        source,mask=[raster_load(source_plan(p)) for p in paths]; prior=set(source.workspace.iterdir())
        def fail(*args,**kwargs):raise RuntimeError('injected stream distance failure')
        if stage=='read':monkeypatch.setattr(compute,'_blocks',fail)
        elif stage=='graph':monkeypatch.setattr(compute,'_d8_stream_graph',fail)
        elif stage=='write':monkeypatch.setattr(gdal.Band,'WriteArray',fail)
        else:monkeypatch.setattr(compute,'_raster',fail)
        with pytest.raises(RuntimeError,match='injected stream distance failure'):compute.raster_distance_to_stream_d8(source,mask,1,1)
        assert set(source.workspace.iterdir())==prior


def test_d8_stream_distance_long_irrational_route_without_recursion():
    import operators.raster_compute as compute
    width=100001; directions=np.full((2,width),np.nan);cols=np.arange(width);rows=cols%2
    directions[rows,cols]=np.where(rows==0,2.,128.);directions[rows[-1],-1]=0
    streams=np.zeros(directions.shape,bool);streams[rows[70000:],cols[70000:]]=True
    expected=np.full(directions.shape,np.nan);expected[rows,cols]=np.maximum(70000-cols,0)*math.sqrt(13.)
    actual=compute._d8_distance_to_stream(directions,streams,2.,3.)
    np.testing.assert_allclose(actual,expected,rtol=1e-12,atol=1e-9,equal_nan=True)


def test_d8_stream_distance_multiband_native_mask_alpha_zero_and_partial_coverage(tmp_path):
    import operators.raster_compute as compute
    directions=np.ones((5,9));directions[:,-1]=0;directions[1,3]=0;directions[1,4]=np.nan
    directions[2,4]=64;directions[2,3]=0;directions[2,5]=0
    streams=np.zeros(directions.shape);streams[0,4:]=1;streams[1,7:]=1;streams[2,4]=1;streams[3,3:]=1
    # Invalid alpha, explicit mask and nonfinite values remove matching cells in both roles.
    mask=np.full(directions.shape,255,np.uint8);mask[1,4]=0
    alpha=np.full(directions.shape,255.);alpha[4,4]=128;alpha[2,4]=0
    directions[4,4]=1
    expected_dirs=directions.copy();expected_dirs[1,4]=np.nan;expected_dirs[2,4]=np.nan
    expected_streams=streams.copy();expected_streams[1,4]=np.nan;expected_streams[2,4]=np.nan
    paths=[]
    for name,values in [('directions',directions),('streams',streams)]:
        path=tmp_path/(name+'.tif');ds=gdal.GetDriverByName('GTiff').Create(str(path),9,5,3,gdal.GDT_Float64)
        ds.SetGeoTransform((500000,2,0,3000000,0,-3));crs=osr.SpatialReference();crs.ImportFromEPSG(3857);ds.SetSpatialRef(crs)
        ds.GetRasterBand(1).WriteArray(np.full(values.shape,.25));ds.GetRasterBand(2).WriteArray(values);ds.GetRasterBand(2).SetNoDataValue(float('nan'))
        ds.GetRasterBand(3).WriteArray(alpha);ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):
            ds.CreateMaskBand(gdal.GMF_PER_DATASET);ds.GetRasterBand(2).GetMaskBand().WriteArray(mask)
        ds=None;paths.append(path)
    # Row 2 points next to the invalid alpha hole must be legal open terminals.
    expected=_stream_distance_walk_oracle(expected_dirs,expected_streams)
    with raster_workspace():
        result=compute.raster_distance_to_stream_d8(*[raster_load(source_plan(p)) for p in paths],2,2)
        ds=gdal.Open(str(result.path));band=ds.GetRasterBand(1)
        np.testing.assert_allclose(read_band_values(band),expected,rtol=1e-12,atol=1e-9,equal_nan=True)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected));band=ds=None


def test_d8_stream_distance_cross_block_cog_complete_pixels_masks_nearest_overviews(tmp_path,monkeypatch):
    import operators.raster_compute as compute
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    height,width=515,519;directions=np.ones((height,width));directions[:,-1]=4;directions[-1,-1]=0
    streams=np.zeros(directions.shape);streams[:400,400:]=1;streams[:,-1]=1
    # Rows below 400 do not hit the river until the final column.
    rows,cols=np.indices(directions.shape);expected=np.maximum(np.where(rows<400,400,width-1)-cols,0)*2.
    paths=_strahler_files(tmp_path,directions,streams);before=[p.read_bytes() for p in paths]
    blocks=compute._blocks;reads=[]
    def bounded(*args,**kwargs):
        for x,y,arrays,masks in blocks(*args,**kwargs):
            assert all(max(a.shape)<=512 for a in arrays);reads.append((x,y));yield x,y,arrays,masks
    monkeypatch.setattr(compute,'_blocks',bounded);target=tmp_path/'stream-distance.cog.tif'
    with raster_workspace():
        result=compute.raster_distance_to_stream_d8(*[raster_load(source_plan(p)) for p in paths],1,1)
        artifact=raster_save(result,target_plan(target),profile='cog',blocksize=128,overview_resampling='nearest')
        assert type(artifact['size_bytes']) is int and artifact['size_bytes']==target.stat().st_size>0
    ds=gdal.Open(str(target));band=ds.GetRasterBand(1)
    np.testing.assert_allclose(read_band_values(band),expected,rtol=1e-12,atol=1e-9)
    np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray(),np.full(directions.shape,255,np.uint8))
    assert band.GetUnitType()=='metre' and band.GetOverviewCount()>0 and not validate(ds,full_check=True)[1]
    previous=expected
    for i in range(band.GetOverviewCount()):
        overview=band.GetOverview(i);h,w=overview.YSize,overview.XSize
        ys=np.floor(.5+np.arange(h)*(previous.shape[0]/h)).astype(int);xs=np.floor(.5+np.arange(w)*(previous.shape[1]/w)).astype(int)
        wanted=previous[np.ix_(ys,xs)]
        np.testing.assert_allclose(overview.ReadAsArray(),wanted,rtol=1e-12,atol=1e-9)
        np.testing.assert_array_equal(overview.GetMaskBand().ReadAsArray(),np.full((h,w),255,np.uint8));previous=wanted
    assert ds.GetGeoTransform()==(500000.,2.,0.,3000000.,0.,-3.) and ds.GetSpatialRef().GetAuthorityCode(None)=='3857'
    band=ds=None;assert [p.read_bytes() for p in paths]==before and (512,512) in reads


@pytest.mark.parametrize('case',['saved','threshold','dem','filled','empty-network','all-invalid','break','cycle'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_stream_distance_real_async_two_input_compositions_and_failure_cleanup(tmp_path,monkeypatch,case,language):
    import api_server,workflow_engine
    values=np.tile(np.arange(11,0,-1,dtype=float),(9,1));directions=np.ones(values.shape);directions[:,-1]=0
    if case=='filled':values[:]=10;values[1:-1,1:-1]=-2
    if case=='cycle':directions[0,1]=16
    if case=='all-invalid':directions[:]=np.nan
    streams=np.where(np.isfinite(directions),0.,np.nan);streams[:,5:]=np.where(np.isfinite(directions[:,5:]),1.,np.nan)
    if case=='empty-network':streams[:]=0
    if case=='break':streams[2,6]=0
    paths=_strahler_files(tmp_path,values if case in ('dem','filled') else directions,streams)
    before=[p.read_bytes() for p in paths];target=tmp_path/'distance.cog.tif'
    tasks=[{'id':'source','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(paths[0])}}];prior='source'
    if case=='filled':
        tasks.append({'id':'fill','operator':'raster_fill_depressions','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':1,'elevation_unit':'metre'}});prior='fill'
    if case in ('dem','filled'):
        tasks.append({'id':'direction','operator':'raster_flow_direction_d8','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':1,'elevation_unit':'metre'}});prior='direction'
        directions=_d8_bellman_ford_oracle(_fill_level_reachability_oracle(values) if case=='filled' else values)
    if case in ('threshold','dem','filled'):
        tasks.extend([{'id':'accumulate','operator':'raster_flow_accumulation_d8','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':1}},
            {'id':'streams','operator':'raster_reclassify','depends_on':['accumulate'],'params':{'input_raster':{'$ref':'accumulate'},'band':1,'rules':[{'min':None,'max':2,'class':0},{'min':2,'max':None,'class':1}]}}])
        streams=np.where(_accumulation_walk_oracle(directions)>=2,1.,0.)
    else:tasks.append({'id':'streams','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(paths[1])}})
    tasks.extend([{'id':'distance','operator':'raster_distance_to_stream_d8','depends_on':[prior,'streams'],'params':{'input_raster':{'$ref':prior},'stream_raster':{'$ref':'streams'},'band':1,'stream_band':1}},
        {'id':'save','operator':'raster_save','depends_on':['distance'],'params':{'input_raster':{'$ref':'distance'},'access_plan':target_plan(target),'profile':'cog','blocksize':128,'overview_resampling':'nearest'}}])
    workspaces=[];execute=workflow_engine._execute_operator
    def capture(name,params):
        if name=='raster_distance_to_stream_d8':workspaces.extend([params['input_raster'].workspace,params['stream_raster'].workspace])
        return execute(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',capture)
    client=api_server.app.test_client()
    assert client.post('/api/operators/raster_distance_to_stream_d8/invoke',json={'params':{}}).status_code==403
    response=client.post('/api/workflow',headers={'Accept-Language':language},json={'workflow_def':{'tasks':tasks},'input_data':{},'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert response.status_code==202,response.json
    deadline=time.monotonic()+15
    while time.monotonic()<deadline:
        status=client.get('/api/executions/'+response.json['execution_id'],headers={'Accept-Language':'en' if language=='zh-cn' else 'zh-cn'}).json
        if status['status'] in ('success','failed'):break
        time.sleep(.01)
    assert status['status']==('failed' if case in ('break','cycle') else 'success'),status
    assert workspaces and all(not p.exists() for p in workspaces) and [p.read_bytes() for p in paths]==before
    if case in ('break','cycle'):
        assert not target.exists()
        phrase=('河网断流' if case=='break' else '方向循环') if language=='zh-cn' else ('stream break' if case=='break' else 'direction cycle')
        assert phrase in status['error']
    else:
        ds=gdal.Open(str(target));band=ds.GetRasterBand(1)
        np.testing.assert_allclose(read_band_values(band),_stream_distance_walk_oracle(directions,streams),rtol=1e-12,atol=1e-9,equal_nan=True)
        assert band.GetUnitType()=='metre';band=ds=None
        with raster_workspace():assert validate_cog(raster_load(source_plan(target)))['valid']
        artifact=json.loads(status['result']);assert type(artifact['size_bytes']) is int and artifact['size_bytes']==target.stat().st_size>0


def _hand_walk_oracle(directions, streams, heights, factor=1.):
    """Independent per-source walking, with no product graph or propagation helper."""
    from fractions import Fraction
    result = np.full(directions.shape, np.nan)
    offsets = {1:(0,1),2:(1,1),4:(1,0),8:(1,-1),16:(0,-1),32:(-1,-1),64:(-1,0),128:(-1,1)}
    for origin in np.argwhere(np.isfinite(directions)):
        row, col = map(int, origin); seen = set()
        while True:
            assert (row,col) not in seen; seen.add((row,col))
            if streams[row,col] == 1:
                a, b = heights[tuple(origin)].item(), heights[row,col].item()
                result[tuple(origin)] = float((Fraction(a)-Fraction(b))*Fraction(factor))
                break
            code = directions[row,col]
            if code == 0: break
            dy, dx = offsets[code]; row, col = row+dy, col+dx
    return result


def _hand_files(tmp_path, directions=None, streams=None, heights=None, unit='metre', epsg=3857):
    directions = np.array([[1.,1.,0.]]) if directions is None else directions
    streams = np.array([[0.,1.,1.]]) if streams is None else streams
    heights = np.array([[120.,95.,80.]]) if heights is None else heights
    grid = (500000.,2.,0.,3000000.,0.,-3.)
    paths = tuple(create_raster(tmp_path/(name+'.tif'), values, grid, 'EPSG:'+str(epsg)) for name,values in zip(('directions','streams','elevation'), (directions, streams, heights)))
    ds = gdal.Open(str(paths[2]), gdal.GA_Update); ds.GetRasterBand(1).SetUnitType(unit); ds=None
    return paths


@pytest.mark.parametrize('seed',range(24))
@pytest.mark.parametrize('unit,factor',[('metre',1.),('foot',.3048),('us_survey_foot',1200/3937)])
def test_d8_hand_random_forests_first_hit_flats_negative_heights_and_no_hit(seed,unit,factor):
    import operators.raster_compute as compute
    rng=np.random.default_rng(seed); rows,cols=np.indices((9,11));rank=np.minimum.reduce((rows,cols,8-rows,10-cols))
    directions=np.zeros(rank.shape);directions[4,5]=np.nan
    for row,col in np.argwhere((rank>0)&np.isfinite(directions)):
        choices=[code for code,(dy,dx) in _STRAHLER_STEPS.items() if rank[row+dy,col+dx]<rank[row,col] and np.isfinite(directions[row+dy,col+dx])]
        directions[row,col]=rng.choice(choices)
    # All heights depend only on descending rank; duplicate ranks force exact flats.
    heights=(rank//2).astype(float)*7-80
    counts=_accumulation_walk_oracle(directions);streams=np.where(np.isfinite(directions),(counts>=seed%5).astype(float),np.nan)
    expected=_hand_walk_oracle(directions,streams,heights,factor)
    actual=compute._d8_hand(directions,streams==1,heights,factor)
    np.testing.assert_allclose(actual,expected,rtol=1e-12,atol=1e-9,equal_nan=True)
    np.testing.assert_array_equal(np.isfinite(actual),np.isfinite(expected))
    assert np.all(actual[streams==1]==0)


@pytest.mark.parametrize('epsg',[3857,2222,2277])
@pytest.mark.parametrize('unit,factor',[('metre',1.),('foot',.3048),('us_survey_foot',1200/3937)])
def test_d8_hand_vertical_units_independent_of_horizontal_crs_and_output_facts(tmp_path,epsg,unit,factor):
    import operators.raster_compute as compute
    directions=np.array([[1.,1.,0.],[1.,1.,0.],[1.,1.,0.]])
    streams=np.array([[0.,1.,1.],[0.,0.,0.],[0.,1.,1.]])
    heights=np.array([[120.,95.,80.],[-10.,-11.,-12.],[5.,5.,0.]])
    paths=_hand_files(tmp_path,directions,streams,heights,unit,epsg);before=[p.read_bytes() for p in paths]
    with raster_workspace():
        inputs=[raster_load(source_plan(p)) for p in paths];prior=set(inputs[0].workspace.iterdir())
        result=compute.raster_hand_d8(*inputs,1,1,1,unit);ds=gdal.Open(str(result.path));band=ds.GetRasterBand(1)
        expected=_hand_walk_oracle(directions,streams,heights,factor)
        np.testing.assert_allclose(read_band_values(band),expected,rtol=1e-12,atol=1e-9,equal_nan=True)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected))
        assert band.GetUnitType()=='metre' and band.DataType==gdal.GDT_Float64 and math.isnan(band.GetNoDataValue())
        assert band.GetScale() is None and band.GetOffset() is None and band.GetColorTable() is None
        assert ds.GetGeoTransform()==(500000.,2.,0.,3000000.,0.,-3.) and ds.GetSpatialRef().GetAuthorityCode(None)==str(epsg)
        assert read_band_values(band)[2,0]==0 and np.isnan(read_band_values(band)[1]).all()
        assert set(inputs[0].workspace.iterdir())==prior|{result.path};band=ds=None
    assert [p.read_bytes() for p in paths]==before


@pytest.mark.parametrize('code,dy,dx',[(k,*v) for k,v in _STRAHLER_STEPS.items()])
def test_d8_hand_all_eight_routes_and_diagonal_crossings(code,dy,dx):
    import operators.raster_compute as compute
    directions=np.full((3,3),np.nan);directions[1,1]=code;directions[1+dy,1+dx]=0
    streams=np.zeros((3,3),bool);streams[1+dy,1+dx]=True
    heights=np.full((3,3),-99.);heights[1,1]=23.;heights[1+dy,1+dx]=3.
    expected=_hand_walk_oracle(directions,streams,heights)
    np.testing.assert_array_equal(compute._d8_hand(directions,streams,heights,1.),expected)
    directions=np.array([[2.,8.],[0.,0.]]);streams=np.array([[0.,0.],[1.,1.]]);heights=np.array([[100.,50.],[10.,20.]])
    np.testing.assert_array_equal(compute._d8_hand(directions,streams==1,heights,1.),np.array([[80.,40.],[0.,0.]]))


@pytest.mark.parametrize('case',['single-river','single-no-river','empty','invalid','row','column'])
def test_d8_hand_single_axes_empty_and_all_invalid(tmp_path,case):
    import operators.raster_compute as compute
    directions=np.array([[1.,1.,0.]]);heights=np.array([[3.,2.,1.]]);streams=np.array([[0.,1.,1.]])
    if case.startswith('single'):directions=np.zeros((1,1));heights=np.full((1,1),-100.);streams=np.full((1,1),case=='single-river')
    if case=='empty':streams[:]=0
    if case=='invalid':directions[:]=heights[:]=streams[:]=np.nan
    if case=='column':directions=np.array([[4.],[4.],[0.]]);heights=heights.T;streams=streams.T
    paths=_hand_files(tmp_path,directions,streams,heights)
    with raster_workspace():
        result=compute.raster_hand_d8(*[raster_load(source_plan(p)) for p in paths],1,1,1,'metre')
        ds=gdal.Open(str(result.path));actual=read_band_values(ds.GetRasterBand(1));ds=None
        np.testing.assert_array_equal(actual,_hand_walk_oracle(directions,streams,heights))


@pytest.mark.parametrize('case',['code','terminal','outside','invalid','cycle','break'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_hand_full_graph_and_break_strict_bilingual(tmp_path,case,language):
    import operators.raster_compute as compute
    directions=np.array([[1.,1.,0.]])
    if case=='code':directions[0,0]=3
    if case=='terminal':directions=np.zeros((3,3))
    if case=='outside':directions[0,-1]=1
    if case=='invalid':directions[0,1]=np.nan
    if case=='cycle':directions=np.array([[1.,16.]])
    streams=np.where(np.isfinite(directions),0.,np.nan);heights=np.where(np.isfinite(directions),0.,np.nan)
    if case=='break':streams=np.array([[1.,0.,1.]])
    paths=_hand_files(tmp_path,directions,streams,heights);key={'outside':'target','invalid':'target'}.get(case,case)
    with raster_workspace(),raster_error_language(language):
        inputs=[raster_load(source_plan(p)) for p in paths];prior=set(inputs[0].workspace.iterdir())
        with pytest.raises(ValueError) as error:compute.raster_hand_d8(*inputs,1,1,1,'metre')
        assert str(error.value)==compute._D8_HAND_MESSAGES[key][language=='en']
        assert set(inputs[0].workspace.iterdir())==prior


@pytest.mark.parametrize('location',['before-hit','after-hit','off-network','empty-network','tiny-uphill'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_hand_uphill_rejected_on_every_edge_without_tolerance(tmp_path,location,language):
    import operators.raster_compute as compute
    directions=np.tile([1.,1.,0.],(2,1));heights=np.tile([3.,2.,1.],(2,1));streams=np.tile([0.,1.,1.],(2,1))
    if location=='before-hit':heights[0,0]=0
    if location=='after-hit':heights[0,2]=2.01
    if location in ('off-network','empty-network'):streams[1,:]=0;heights[1,1]=4
    if location=='empty-network':streams[:]=0
    if location=='tiny-uphill':heights[0,1]=3.;heights[0,2]=np.nextafter(3.,np.inf)
    paths=_hand_files(tmp_path,directions,streams,heights)
    with raster_workspace(),raster_error_language(language):
        inputs=[raster_load(source_plan(p)) for p in paths];prior=set(inputs[0].workspace.iterdir())
        with pytest.raises(ValueError) as error:compute.raster_hand_d8(*inputs,1,1,1,'metre')
        assert str(error.value)==compute._D8_HAND_MESSAGES['uphill'][language=='en']
        assert set(inputs[0].workspace.iterdir())==prior


@pytest.mark.parametrize('role',range(3))
@pytest.mark.parametrize('bad',[True,False,0,-1,2,1.5,'1',None])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_hand_three_explicit_band_parameters(tmp_path,role,bad,language):
    import operators.raster_compute as compute
    paths=_hand_files(tmp_path);bands=[1,1,1];bands[role]=bad;key=('band','mask_band','dem_band')[role]
    with raster_workspace(),raster_error_language(language):
        inputs=[raster_load(source_plan(p)) for p in paths];prior=set(inputs[0].workspace.iterdir())
        with pytest.raises(ValueError) as error:compute.raster_hand_d8(*inputs,*bands,'metre')
        assert str(error.value)==compute._D8_HAND_MESSAGES[key][language=='en']
        assert set(inputs[0].workspace.iterdir())==prior


@pytest.mark.parametrize('unit',[None,True,1,'','meter','Metre','feet'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_hand_requires_explicit_vertical_unit(tmp_path,unit,language):
    import operators.raster_compute as compute
    paths=_hand_files(tmp_path)
    with raster_workspace(),raster_error_language(language):
        with pytest.raises(ValueError) as error:compute.raster_hand_d8(*[raster_load(source_plan(p)) for p in paths],1,1,1,unit)
        assert str(error.value)==compute._D8_HAND_MESSAGES['dem_unit'][language=='en']


@pytest.mark.parametrize('role',range(3))
@pytest.mark.parametrize('case',['scale','offset','alpha','complex','complex-alpha','rotated','geographic','no-crs','south-up'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_hand_all_roles_encoding_and_grid_admission(tmp_path,role,case,language):
    import operators.raster_compute as compute
    paths=_hand_files(tmp_path)
    ds=gdal.GetDriverByName('GTiff').Create(str(paths[role]),3,1,2 if case=='complex-alpha' else 1,gdal.GDT_CFloat64 if case in ('complex','complex-alpha') else gdal.GDT_Float64)
    ds.SetGeoTransform((500000,2,.1 if case=='rotated' else 0,3000000,0,3 if case=='south-up' else -3))
    if case!='no-crs':
        crs=osr.SpatialReference();crs.ImportFromEPSG(4326 if case=='geographic' else 3857);ds.SetSpatialRef(crs)
    ds.GetRasterBand(1).WriteArray(np.array([[1,1,0]]) if role==0 else np.array([[0,1,1]]) if role==1 else np.array([[3,2,1]]))
    if case=='scale':ds.GetRasterBand(1).SetScale(2)
    if case=='offset':ds.GetRasterBand(1).SetOffset(1)
    if case=='alpha':ds.GetRasterBand(1).SetColorInterpretation(gdal.GCI_AlphaBand)
    if case=='complex-alpha':ds.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand)
    ds=None
    key={'offset':'scale','complex-alpha':'complex'}.get(case,case)
    if case in ('rotated','geographic','no-crs','south-up'):key='grid'
    if role==1:key='alignment' if key=='grid' else 'mask_'+key
    if role==2:key='dem_'+key
    with raster_workspace(),raster_error_language(language):
        inputs=[raster_load(source_plan(p)) for p in paths];prior=set(inputs[0].workspace.iterdir())
        with pytest.raises(ValueError) as error:compute.raster_hand_d8(*inputs,1,1,1,'metre')
        assert str(error.value)==compute._D8_HAND_MESSAGES[key][language=='en']
        assert set(inputs[0].workspace.iterdir())==prior


@pytest.mark.parametrize('role',[1,2])
@pytest.mark.parametrize('case,mask_default',[
    ('dimensions',None),('origin',None),('resolution',None),('crs',None),
    ('mask','NO'),('mask','YES'),('nonfinite',None),('nodata',None)])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_hand_alignment_and_effective_coverage_are_exact(tmp_path,role,case,mask_default,language):
    import operators.raster_compute as compute
    paths=_hand_files(tmp_path);ds=gdal.Open(str(paths[role]),gdal.GA_Update)
    if case=='dimensions':
        ds=None;create_raster(paths[role],np.ones((2,3)),(500000,2,0,3000000,0,-3),'EPSG:3857')
    elif case in ('origin','resolution'):
        grid=list(ds.GetGeoTransform());grid[0 if case=='origin' else 1]+=1;ds.SetGeoTransform(grid)
    elif case=='crs':crs=osr.SpatialReference();crs.ImportFromEPSG(32650);ds.SetSpatialRef(crs)
    elif case=='mask':
        previous=gdal.GetConfigOption('GDAL_TIFF_INTERNAL_MASK')
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK',mask_default):
            # A single-file fixture must not depend on the GDAL version's default.
            with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):
                assert ds.GetRasterBand(1).CreateMaskBand(gdal.GMF_PER_DATASET)==gdal.CE_None
            assert gdal.GetConfigOption('GDAL_TIFF_INTERNAL_MASK')==mask_default
            ds.GetRasterBand(1).GetMaskBand().WriteArray(np.array([[255,0,255]],np.uint8))
        assert gdal.GetConfigOption('GDAL_TIFF_INTERNAL_MASK')==previous
    else:
        values=np.array([[0,1,1.]]) if role==1 else np.array([[3,2,1.]])
        values[0,1]=np.inf if case=='nonfinite' else -9999;ds.GetRasterBand(1).WriteArray(values)
    ds=None
    if case=='mask':
        ds=gdal.Open(str(paths[role]));assert ds.GetFileList()==[str(paths[role])]
        assert not Path(str(paths[role])+'.msk').exists()
        assert ds.GetRasterBand(1).GetMaskFlags()==gdal.GMF_PER_DATASET
        np.testing.assert_array_equal(ds.GetRasterBand(1).GetMaskBand().ReadAsArray(),[[255,0,255]])
        ds=None
    key=('alignment' if role==1 else 'dem_alignment') if case in ('dimensions','origin','resolution','crs') else ('coverage' if role==1 else 'dem_coverage')
    with raster_workspace(),raster_error_language(language):
        with pytest.raises(ValueError) as error:compute.raster_hand_d8(*[raster_load(source_plan(p)) for p in paths],1,1,1,'metre')
        assert str(error.value)==compute._D8_HAND_MESSAGES[key][language=='en']


@pytest.mark.parametrize('bad',[-1,2,.5,2**53,np.nan,np.inf])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_hand_binary_stream_mask_strict(tmp_path,bad,language):
    import operators.raster_compute as compute
    paths=_hand_files(tmp_path,streams=np.array([[bad,1.,1.]]));key='mask_values' if math.isfinite(bad) else 'coverage'
    with raster_workspace(),raster_error_language(language):
        with pytest.raises(ValueError) as error:compute.raster_hand_d8(*[raster_load(source_plan(p)) for p in paths],1,1,1,'metre')
        assert str(error.value)==compute._D8_HAND_MESSAGES[key][language=='en']


@pytest.mark.parametrize('unit,declared',[('', 'metre'),('m','metre'),('ft','foot'),('us survey foot','us_survey_foot'),('foot','metre'),('unknown','metre')])
def test_d8_hand_elevation_band_unit_conflict_and_aliases(tmp_path,unit,declared):
    import operators.raster_compute as compute
    paths=_hand_files(tmp_path,unit=unit)
    with raster_workspace():
        inputs=[raster_load(source_plan(p)) for p in paths]
        if unit in ('foot','unknown'):
            with pytest.raises(ValueError,match='单位'):compute.raster_hand_d8(*inputs,1,1,1,declared)
        else:
            result=compute.raster_hand_d8(*inputs,1,1,1,declared);ds=gdal.Open(str(result.path))
            assert read_band_values(ds.GetRasterBand(1))[0,0]==pytest.approx(25*{'metre':1,'foot':.3048,'us_survey_foot':1200/3937}[declared]);ds=None


@pytest.mark.parametrize('dtype',[np.int64,np.uint64])
@pytest.mark.parametrize('unit,factor',[('metre',1.),('foot',.3048),('us_survey_foot',1200/3937)])
def test_d8_hand_original_64bit_elevations_difference_and_nodata_neighbour(tmp_path,dtype,unit,factor):
    import operators.raster_compute as compute
    from osgeo import gdal_array
    paths=_hand_files(tmp_path,directions=np.array([[1.,0.,np.nan]]),streams=np.array([[0.,1.,np.nan]]))
    base=2**63 if dtype==np.uint64 else -2**63
    nodata=base+8;heights=np.array([[base+10,base+9,nodata]],dtype=dtype)
    ds=gdal.GetDriverByName('GTiff').Create(str(paths[2]),3,1,1,gdal_array.NumericTypeCodeToGDALTypeCode(dtype));ds.SetGeoTransform((500000,2,0,3000000,0,-3));crs=osr.SpatialReference();crs.ImportFromEPSG(3857);ds.SetSpatialRef(crs)
    ds.GetRasterBand(1).WriteArray(heights);ds.GetRasterBand(1).SetNoDataValue(int(nodata));ds=None
    with raster_workspace():
        inputs=[raster_load(source_plan(p)) for p in paths];result=compute.raster_hand_d8(*inputs,1,1,1,unit)
        ds=gdal.Open(str(result.path));np.testing.assert_allclose(read_band_values(ds.GetRasterBand(1)),np.array([[factor,0,np.nan]]),rtol=1e-12,atol=1e-9,equal_nan=True);ds=None
    # Swap adjacent native heights; Float64 would falsely treat this as flat.
    ds=gdal.Open(str(paths[2]),gdal.GA_Update);ds.GetRasterBand(1).WriteArray(heights[:,[1,0,2]]);ds=None
    with raster_workspace(),raster_error_language('en'):
        with pytest.raises(ValueError,match='uphill'):compute.raster_hand_d8(*[raster_load(source_plan(p)) for p in paths],1,1,1,unit)


@pytest.mark.parametrize('role',[0,1])
def test_d8_hand_raw_64bit_direction_and_mask_illegal_nodata_neighbour(tmp_path,role):
    import operators.raster_compute as compute
    paths=_hand_files(tmp_path);ds=gdal.GetDriverByName('GTiff').Create(str(paths[role]),3,1,1,gdal.GDT_UInt64)
    ds.SetGeoTransform((500000,2,0,3000000,0,-3));crs=osr.SpatialReference();crs.ImportFromEPSG(3857);ds.SetSpatialRef(crs)
    ds.GetRasterBand(1).SetNoDataValue(2**53);ds.GetRasterBand(1).WriteArray(np.array([[2**53+1,1,0 if role==0 else 1]],np.uint64));ds=None
    with raster_workspace(),raster_error_language('en'):
        with pytest.raises(ValueError) as error:compute.raster_hand_d8(*[raster_load(source_plan(p)) for p in paths],1,1,1,'metre')
        assert str(error.value)==compute._D8_HAND_MESSAGES['code' if role==0 else 'mask_values'][1]


@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_hand_float_range_overflow_underflow_and_finite_scaled_difference(language):
    import operators.raster_compute as compute
    directions=np.array([[1.,0.]]);streams=np.array([[False,True]])
    with raster_error_language(language):
        for heights,factor in ((np.array([[1.7e308,-1.7e308]]),1.),(np.array([[np.nextafter(0.,1.),0.]]),.3048)):
            with pytest.raises(ValueError) as error:compute._d8_hand(directions,streams,heights,factor)
            assert str(error.value)==compute._D8_HAND_MESSAGES['range'][language=='en']
        heights=np.array([[1.7e308,-1.7e308]])
        expected=_hand_walk_oracle(directions,streams,heights,.3048)
        np.testing.assert_allclose(compute._d8_hand(directions,streams,heights,.3048),expected,rtol=1e-15,atol=0)
        # Close high elevations must be subtracted before conversion, not two rounded metre values.
        heights=np.array([[np.nextafter(1e20,np.inf),1e20]])
        np.testing.assert_array_equal(compute._d8_hand(directions,streams,heights,.3048),_hand_walk_oracle(directions,streams,heights,.3048))


def test_d8_hand_long_path_no_recursion_and_first_hit_not_final_outlet():
    import operators.raster_compute as compute
    size=100001;directions=np.ones((1,size));directions[0,-1]=0
    streams=np.zeros((1,size),bool);streams[0,50000:]=True
    heights=np.arange(size,0,-1,dtype=np.int64).reshape(1,size)
    actual=compute._d8_hand(directions,streams,heights,.3048)
    expected=np.zeros((1,size));expected[0,:50000]=np.arange(50000,0,-1)*.3048
    np.testing.assert_allclose(actual,expected,rtol=1e-12,atol=1e-9)


@pytest.mark.parametrize('mask_default',['NO','YES'])
def test_d8_hand_cross_block_three_roles_masks_alpha_nodata_full_cog_and_prior_unchanged(tmp_path,monkeypatch,mask_default):
    import operators.raster_compute as compute
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    h,w=519,515;rows,cols=np.indices((h,w));directions=np.ones((h,w));directions[:,-1]=0
    # Each row is independent; valid coverage begins/ends at distinct original holes.
    directions[10,30]=np.nan;directions[10,29]=0
    streams=np.where(np.isfinite(directions),(cols>=512).astype(float),np.nan)
    heights=np.where(np.isfinite(directions),-cols.astype(float),np.nan)
    paths=_hand_files(tmp_path,directions,streams,heights,unit='foot')
    for path,values in zip(paths,(directions,streams,heights)):
        cube=np.stack((np.full((h,w),.25),values,np.ones((h,w))))
        cube[2,20,:]=0;cube[2,21,:]=.5
        create_raster(path,cube,(500000,2,0,3000000,0,-3),'EPSG:3857')
        ds=gdal.Open(str(path),gdal.GA_Update);ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
        previous=gdal.GetConfigOption('GDAL_TIFF_INTERNAL_MASK')
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK',mask_default):
            with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):
                assert ds.GetRasterBand(1).CreateMaskBand(gdal.GMF_PER_DATASET)==gdal.CE_None
            assert gdal.GetConfigOption('GDAL_TIFF_INTERNAL_MASK')==mask_default
            mask=np.full((h,w),255,np.uint8);mask[22,:]=0;ds.GetRasterBand(1).GetMaskBand().WriteArray(mask)
        assert gdal.GetConfigOption('GDAL_TIFF_INTERNAL_MASK')==previous
        if path==paths[2]:ds.GetRasterBand(2).SetUnitType('foot')
        ds=None
        ds=gdal.Open(str(path));assert ds.GetFileList()==[str(path)]
        assert not Path(str(path)+'.msk').exists()
        assert ds.GetRasterBand(2).GetMaskFlags()==gdal.GMF_PER_DATASET
        np.testing.assert_array_equal(ds.GetRasterBand(2).GetMaskBand().ReadAsArray(),mask)
        ds=None
    directions[20,:]=directions[22,:]=np.nan;streams[20,:]=streams[22,:]=np.nan
    before=[p.read_bytes() for p in paths];target=tmp_path/'hand.cog.tif';previous=tmp_path/'prior.bin';previous.write_bytes(b'prior output')
    reads=[];blocks=compute._blocks
    def capture(ds,indices,**kw):
        for block in blocks(ds,indices,**kw):reads.append(block[:2]);yield block
    monkeypatch.setattr(compute,'_blocks',capture)
    expected=_hand_walk_oracle(directions,streams,heights,.3048)
    with raster_workspace():
        result=compute.raster_hand_d8(*[raster_load(source_plan(p)) for p in paths],2,2,2,'foot')
        artifact=raster_save(result,target_plan(target),profile='cog',blocksize=128,overview_resampling='nearest')
        assert type(artifact['size_bytes']) is int and artifact['size_bytes']==target.stat().st_size>0
    ds=gdal.Open(str(target));band=ds.GetRasterBand(1)
    np.testing.assert_allclose(read_band_values(band),expected,rtol=1e-12,atol=1e-9,equal_nan=True)
    np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected))
    assert band.GetUnitType()=='metre' and ds.GetGeoTransform()==(500000.,2.,0.,3000000.,0.,-3.)
    assert ds.GetSpatialRef().GetAuthorityCode(None)=='3857' and band.GetOverviewCount()>0
    assert not validate(ds,full_check=True)[1]
    previous_level=expected
    for i in range(band.GetOverviewCount()):
        overview=band.GetOverview(i);oh,ow=overview.YSize,overview.XSize
        ys=np.floor(.5+np.arange(oh)*(previous_level.shape[0]/oh)).astype(int);xs=np.floor(.5+np.arange(ow)*(previous_level.shape[1]/ow)).astype(int)
        wanted=previous_level[np.ix_(ys,xs)]
        np.testing.assert_allclose(overview.ReadAsArray(),wanted,rtol=1e-12,atol=1e-9,equal_nan=True)
        np.testing.assert_array_equal(overview.GetMaskBand().ReadAsArray()!=0,np.isfinite(wanted));previous_level=wanted
    band=ds=None
    assert [p.read_bytes() for p in paths]==before and previous.read_bytes()==b'prior output' and (512,512) in reads


def test_d8_hand_injected_compute_failure_leaves_only_inputs(tmp_path,monkeypatch):
    import operators.raster_compute as compute
    paths=_hand_files(tmp_path);before=[p.read_bytes() for p in paths]
    def fail(*args):raise RuntimeError('injected HAND failure')
    monkeypatch.setattr(compute,'_d8_hand',fail)
    with raster_workspace():
        inputs=[raster_load(source_plan(p)) for p in paths];prior=set(inputs[0].workspace.iterdir())
        with pytest.raises(RuntimeError,match='injected HAND failure'):compute.raster_hand_d8(*inputs,1,1,1,'metre')
        assert set(inputs[0].workspace.iterdir())==prior
    assert [p.read_bytes() for p in paths]==before


@pytest.mark.parametrize('case',['saved','threshold','dem','filled','empty','invalid','uphill','mixed-original','break','cycle'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_d8_hand_real_async_three_input_compositions_frozen_language_and_cleanup(tmp_path,monkeypatch,case,language):
    import api_server,workflow_engine
    heights=np.tile(np.arange(11,0,-1,dtype=float),(9,1));directions=np.ones(heights.shape);directions[:,-1]=0
    if case in ('filled','mixed-original'):heights[:]=10;heights[1:-1,1:-1]=-2
    if case=='uphill':heights[0,1]=20
    if case=='cycle':directions[0,1]=16
    if case=='invalid':directions[:]=heights[:]=np.nan
    streams=np.where(np.isfinite(directions),0.,np.nan);streams[:,5:]=np.where(np.isfinite(directions[:,5:]),1.,np.nan)
    if case=='empty':streams[:]=0
    if case=='break':streams[2,6]=0
    paths=_hand_files(tmp_path,directions,streams,heights);before=[p.read_bytes() for p in paths];target=tmp_path/'hand.cog.tif'
    tasks=[{'id':name,'operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(path)}} for name,path in zip(('direction','streams','elevation'),paths)]
    route='direction';dem='elevation'
    if case in ('filled','mixed-original'):
        tasks.append({'id':'fill','operator':'raster_fill_depressions','depends_on':[dem],'params':{'input_raster':{'$ref':dem},'band':1,'elevation_unit':'metre'}})
        dem='fill'
    if case in ('dem','filled','mixed-original'):
        tasks.append({'id':'flow','operator':'raster_flow_direction_d8','depends_on':[dem],'params':{'input_raster':{'$ref':dem},'band':1,'elevation_unit':'metre'}});route='flow'
        heights_used=_fill_level_reachability_oracle(heights) if dem=='fill' else heights
        directions=_d8_bellman_ford_oracle(heights_used)
    if case in ('threshold','dem','filled','mixed-original'):
        tasks=[t for t in tasks if t['id']!='streams']
        tasks.extend([{'id':'acc','operator':'raster_flow_accumulation_d8','depends_on':[route],'params':{'input_raster':{'$ref':route},'band':1}},
            {'id':'streams','operator':'raster_reclassify','depends_on':['acc'],'params':{'input_raster':{'$ref':'acc'},'band':1,'rules':[{'min':None,'max':2,'class':0},{'min':2,'max':None,'class':1}]}}])
        streams=np.where(_accumulation_walk_oracle(directions)>=2,1.,0.)
    if case=='mixed-original':dem='elevation'
    tasks.extend([{'id':'hand','operator':'raster_hand_d8','depends_on':[route,'streams',dem],'params':{'input_raster':{'$ref':route},'stream_raster':{'$ref':'streams'},'elevation_raster':{'$ref':dem},'band':1,'stream_band':1,'elevation_band':1,'elevation_unit':'metre'}},
        {'id':'save','operator':'raster_save','depends_on':['hand'],'params':{'input_raster':{'$ref':'hand'},'access_plan':target_plan(target),'profile':'cog','blocksize':128}}])
    workspaces=[];execute=workflow_engine._execute_operator
    def capture(name,params):
        if name=='raster_hand_d8':workspaces.extend(params[n].workspace for n in ('input_raster','stream_raster','elevation_raster'))
        return execute(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',capture)
    client=api_server.app.test_client();assert client.post('/api/operators/raster_hand_d8/invoke',json={'params':{}}).status_code==403
    response=client.post('/api/workflow',headers={'Accept-Language':language},json={'workflow_def':{'tasks':tasks},'input_data':{},'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert response.status_code==202,response.json
    deadline=time.monotonic()+15
    while time.monotonic()<deadline:
        status=client.get('/api/executions/'+response.json['execution_id'],headers={'Accept-Language':'en' if language=='zh-cn' else 'zh-cn'}).json
        if status['status'] in ('success','failed'):break
        time.sleep(.01)
    failed=case in ('uphill','mixed-original','break','cycle')
    assert status['status']==('failed' if failed else 'success'),status
    assert workspaces and all(not p.exists() for p in workspaces) and [p.read_bytes() for p in paths]==before
    if failed:
        assert not target.exists()
        phrase=('逆坡' if case in ('uphill','mixed-original') else '河网断流' if case=='break' else '方向循环') if language=='zh-cn' else ('uphill' if case in ('uphill','mixed-original') else 'stream break' if case=='break' else 'direction cycle')
        assert phrase in status['error']
        original_error=status['error'];assert client.get('/api/executions/'+response.json['execution_id'],headers={'Accept-Language':language}).json['error']==original_error
    else:
        expected=_hand_walk_oracle(directions,streams,heights_used if case in ('dem','filled') else heights)
        ds=gdal.Open(str(target));band=ds.GetRasterBand(1)
        np.testing.assert_allclose(read_band_values(band),expected,rtol=1e-12,atol=1e-9,equal_nan=True)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected));assert band.GetUnitType()=='metre';band=ds=None
        with raster_workspace():assert validate_cog(raster_load(source_plan(target)))['valid']
        artifact=json.loads(status['result']);assert type(artifact['size_bytes']) is int and artifact['size_bytes']==target.stat().st_size>0


def _curvature_lstsq_oracle(values, dx, dy, factor=1.):
    """Independent full-window six-column least squares + fundamental forms."""
    rows,cols=np.indices(values.shape);height,width=values.shape
    result=np.full(values.shape,np.nan)
    if min(values.shape)<3:return result
    samples=np.lib.stride_tricks.sliding_window_view(values,(3,3))
    valid=np.isfinite(samples).all(axis=(-2,-1))
    if not valid.any():return result
    x=np.tile(np.arange(-1,2),3);y=np.repeat(np.arange(1,-2,-1),3)
    design=np.column_stack((x*x,x*y,y*y,x,y,np.ones(9)))
    window=samples[valid].reshape(-1,9)
    from fractions import Fraction
    # Exact input-relative differences for native 64-bit numerical samples.
    if np.issubdtype(values.dtype,np.integer):relative=(window.astype(object)-window[:,4:5].astype(object)).astype(float)*factor
    else:relative=(window-window[:,4:5])*factor
    coefficients=np.linalg.lstsq(design,relative.T,rcond=None)[0].T
    a,b,c,p,q,_=coefficients.T;p=p/dx;q=q/dy;r=2*a/dx**2;s=b/dx/dy;t=2*c/dy**2
    first=np.empty((len(p),2,2));second=np.empty_like(first)
    first[:,0,0]=1+p*p;first[:,1,1]=1+q*q;first[:,0,1]=first[:,1,0]=p*q
    norm=np.sqrt(1+p*p+q*q)
    second[:,0,0]=-r/norm;second[:,1,1]=-t/norm;second[:,0,1]=second[:,1,0]=-s/norm
    shaped=np.linalg.solve(first,second)
    result[1:-1,1:-1][valid]=np.trace(shaped,axis1=1,axis2=2)/2
    return result


def _curvature_file(tmp_path,values,dx=2.,dy=3.,epsg=3857,unit='metre'):
    from osgeo import osr
    crs=osr.SpatialReference();crs.ImportFromEPSG(epsg);horizontal=crs.GetLinearUnits()
    path=create_raster(tmp_path/'curvature-source.tif',values,(500000,dx/horizontal,0,3000000,0,-dy/horizontal),'EPSG:'+str(epsg))
    ds=gdal.Open(str(path),gdal.GA_Update);ds.GetRasterBand(1).SetUnitType(unit);ds=None
    return path


@pytest.mark.parametrize('coefficients',[(0,0,0,0,0),(0,0,0,2,-3),(1,0,2,0,0),(-1,0,-2,0,0),(1,0,-1,0,0),(.02,.03,-.04,.2,-.3)])
@pytest.mark.parametrize('dx,dy',[(1.,1.),(2.,3.),(.25,4.)])
def test_dem_curvature_analytic_quadratic_sign_planes_saddles_and_rectangular_grid(tmp_path,coefficients,dx,dy):
    import operators.raster_compute as compute
    a,b,c,d,e=coefficients;rows,cols=np.indices((9,11));x=(cols-5)*dx;y=(4-rows)*dy
    values=a*x*x+b*x*y+c*y*y+d*x+e*y+100
    path=_curvature_file(tmp_path,values,dx,dy);before=path.read_bytes()
    with raster_workspace():
        source=raster_load(source_plan(path));prior=set(source.workspace.iterdir());result=compute.raster_curvature(source,1,'metre')
        ds=gdal.Open(str(result.path));band=ds.GetRasterBand(1);actual=read_band_values(band)
        expected=_curvature_lstsq_oracle(values,dx,dy)
        np.testing.assert_allclose(actual,expected,rtol=1e-12,atol=1e-12,equal_nan=True)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected))
        assert actual[4,5]==pytest.approx(-((1+e*e)*2*a-2*d*e*b+(1+d*d)*2*c)/(2*(1+d*d+e*e)**1.5),rel=1e-12,abs=1e-12)
        if a==b==c==0:np.testing.assert_array_equal(actual[1:-1,1:-1],np.zeros((7,9)))
        assert band.GetUnitType()=='1/metre' and band.DataType==gdal.GDT_Float64 and math.isnan(band.GetNoDataValue())
        assert band.GetScale() is None and band.GetOffset() is None and band.GetColorTable() is None
        assert ds.GetGeoTransform()==(500000.,dx,0.,3000000.,0.,-dy) and ds.GetSpatialRef().GetAuthorityCode(None)=='3857'
        assert set(source.workspace.iterdir())==prior|{result.path};band=ds=None
    assert path.read_bytes()==before


@pytest.mark.parametrize('seed',range(12))
def test_dem_curvature_nine_equal_weights_free_intercept_and_independent_lstsq(tmp_path,seed):
    import operators.raster_compute as compute
    rng=np.random.default_rng(seed);values=rng.normal(size=(9,11))*7+1000
    if seed==0:values[:]=0;values[4,5]=1
    path=_curvature_file(tmp_path,values)
    with raster_workspace():
        result=compute.raster_curvature(raster_load(source_plan(path)),1,'metre');ds=gdal.Open(str(result.path));actual=read_band_values(ds.GetRasterBand(1));ds=None
    np.testing.assert_allclose(actual,_curvature_lstsq_oracle(values,2,3),rtol=1e-12,atol=1e-12,equal_nan=True)
    if seed==0:assert actual[4,5]==pytest.approx(1/12+1/27)


@pytest.mark.parametrize('epsg',[3857,2222,2277])
@pytest.mark.parametrize('unit,factor',[('metre',1.),('foot',.3048),('us_survey_foot',1200/3937)])
def test_dem_curvature_vertical_horizontal_units_independent(tmp_path,epsg,unit,factor):
    import operators.raster_compute as compute
    rows,cols=np.indices((7,9));x=(cols-4)*2.;y=(3-rows)*3.
    metres=.03*x*x+.04*x*y-.01*y*y+.2*x-.3*y-100
    path=_curvature_file(tmp_path,metres/factor,epsg=epsg,unit=unit)
    with raster_workspace():
        result=compute.raster_curvature(raster_load(source_plan(path)),1,unit);ds=gdal.Open(str(result.path));band=ds.GetRasterBand(1)
        np.testing.assert_allclose(read_band_values(band),_curvature_lstsq_oracle(metres/factor,2,3,factor),rtol=1e-11,atol=1e-12,equal_nan=True)
        assert ds.GetSpatialRef().GetAuthorityCode(None)==str(epsg) and band.GetUnitType()=='1/metre';band=ds=None


@pytest.mark.parametrize('shape',[(1,1),(1,8),(7,1),(2,5),(5,2),(3,3),(5,7)])
@pytest.mark.parametrize('empty',[False,True])
def test_dem_curvature_empty_and_small_grids_return_nodata_or_complete_centres(tmp_path,shape,empty):
    import operators.raster_compute as compute
    values=np.full(shape,np.nan if empty else 0.);path=_curvature_file(tmp_path,values)
    with raster_workspace():
        result=compute.raster_curvature(raster_load(source_plan(path)),1,'metre');ds=gdal.Open(str(result.path));band=ds.GetRasterBand(1)
        expected=_curvature_lstsq_oracle(values,2,3);np.testing.assert_array_equal(read_band_values(band),expected)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected));band=ds=None


@pytest.mark.parametrize('fault',['nan','inf','nodata','mask','alpha-zero','alpha-nan','partial-alpha'])
def test_dem_curvature_full_nine_effective_samples_and_explicit_second_band(tmp_path,fault):
    import operators.raster_compute as compute
    rows,cols=np.indices((7,9));values=(cols-4.)**2+(rows-3.)**2
    cube=np.stack((np.full(values.shape,.25),values,np.full(values.shape,-9999.),np.full(values.shape,255.)))
    if fault in ('nan','inf','nodata'):cube[1,3,4]={'nan':np.nan,'inf':np.inf,'nodata':-9999.}[fault]
    if fault.startswith('alpha-'):cube[3,3,4]=0 if fault=='alpha-zero' else np.nan
    if fault=='partial-alpha':cube[3,3,4]=128
    path=_curvature_file(tmp_path,cube,unit='')
    ds=gdal.Open(str(path),gdal.GA_Update);ds.GetRasterBand(2).SetUnitType('metre');ds.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand)
    if fault=='mask':
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
        mask=np.full(values.shape,255,np.uint8);mask[3,4]=0;ds.GetRasterBand(1).GetMaskBand().WriteArray(mask)
    ds=None
    expected_values=values.copy()
    if fault!='partial-alpha':expected_values[3,4]=np.nan
    before=path.read_bytes()
    with raster_workspace():
        result=compute.raster_curvature(raster_load(source_plan(path)),2,'metre');ds=gdal.Open(str(result.path));actual=read_band_values(ds.GetRasterBand(1));ds=None
        np.testing.assert_allclose(actual,_curvature_lstsq_oracle(expected_values,2,3),rtol=1e-12,atol=1e-12,equal_nan=True)
        if fault=='partial-alpha':assert np.isfinite(actual[3,4])
        else:assert np.isnan(actual[2:5,3:6]).all()
    assert path.read_bytes()==before


@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('fault',['geographic','rotated','south-up','missing-crs','missing-grid','unit','unit-conflict','unit-unknown','scale','offset','alpha','band','complex','complex-alpha'])
@pytest.mark.parametrize('method',['mean','profile','tangential'])
def test_dem_curvature_bilingual_admission_no_private_output(tmp_path,language,fault,method):
    import operators.raster_compute as compute
    path=tmp_path/'bad.tif';ds=gdal.GetDriverByName('GTiff').Create(str(path),5,5,2 if fault=='complex-alpha' else 1,gdal.GDT_CFloat64 if fault in ('complex','complex-alpha') else gdal.GDT_Float64)
    if fault!='missing-grid':ds.SetGeoTransform((0,1,.2 if fault=='rotated' else 0,5,0,1 if fault=='south-up' else -1))
    if fault!='missing-crs':crs=osr.SpatialReference();crs.ImportFromEPSG(4326 if fault=='geographic' else 3857);ds.SetSpatialRef(crs)
    band=ds.GetRasterBand(1);band.Fill(1)
    if fault in ('unit-conflict','unit-unknown'):band.SetUnitType('foot' if fault=='unit-conflict' else 'fathom')
    if fault=='scale':band.SetScale(2)
    if fault=='offset':band.SetOffset(10)
    if fault=='alpha':band.SetColorInterpretation(gdal.GCI_AlphaBand)
    if fault=='complex-alpha':ds.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand)
    band=ds=None
    key={'unit-unknown':'unit-conflict','offset':'scale','missing-grid':'grid','missing-crs':'grid','geographic':'grid','rotated':'grid','south-up':'grid','complex-alpha':'complex'}.get(fault,fault)
    with raster_workspace(),raster_error_language(language):
        source=raster_load(source_plan(path));prior=set(source.workspace.iterdir())
        with pytest.raises(ValueError) as error:compute.raster_curvature(source,True if fault=='band' else 1,'bad' if fault=='unit' else 'metre',method=method)
        assert str(error.value)==str(compute._curvature_error(key,method))
        assert ('DEM '+method+' curvature' if language=='en' else 'DEM '+{'profile':'剖面曲率','tangential':'切向曲率'}[method]) in str(error.value) if method!='mean' else True
        assert set(source.workspace.iterdir())==prior


@pytest.mark.parametrize('band',[True,False,0,-1,2,1.5,'1',None])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_dem_curvature_requires_explicit_valid_band(tmp_path,band,language):
    import operators.raster_compute as compute
    path=_curvature_file(tmp_path,np.zeros((5,5)))
    with raster_workspace(),raster_error_language(language):
        with pytest.raises(ValueError) as error:compute.raster_curvature(raster_load(source_plan(path)),band,'metre')
        assert str(error.value)==compute._CURVATURE_MESSAGES['band'][language=='en']


@pytest.mark.parametrize('unit',[None,True,1,'','Metre','feet'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_dem_curvature_requires_exact_vertical_unit(tmp_path,unit,language):
    import operators.raster_compute as compute
    path=_curvature_file(tmp_path,np.zeros((5,5)))
    with raster_workspace(),raster_error_language(language):
        with pytest.raises(ValueError) as error:compute.raster_curvature(raster_load(source_plan(path)),1,unit)
        assert str(error.value)==compute._CURVATURE_MESSAGES['unit'][language=='en']


@pytest.mark.parametrize('dtype',[np.int64,np.uint64])
def test_dem_curvature_original_64bit_relative_differences_and_adjacent_nodata(tmp_path,dtype):
    import operators.raster_compute as compute
    from osgeo import gdal_array
    rows,cols=np.indices((7,7));small=(rows-3)**2+(cols-3)**2+100;base=2**63 if dtype==np.uint64 else -2**63
    values=np.array([[base+int(n) for n in line] for line in small],dtype=dtype);nodata=base+99;values[0,0]=nodata
    path=tmp_path/'integer.tif';ds=gdal.GetDriverByName('GTiff').Create(str(path),7,7,1,gdal_array.NumericTypeCodeToGDALTypeCode(dtype))
    ds.SetGeoTransform((500000,1,0,3000000,0,-1));crs=osr.SpatialReference();crs.ImportFromEPSG(3857);ds.SetSpatialRef(crs);band=ds.GetRasterBand(1)
    band.WriteArray(values)
    if dtype==np.uint64:band.SetNoDataValueAsUInt64(nodata)
    else:band.SetNoDataValueAsInt64(nodata)
    band=ds=None
    expected_values=small.astype(float);expected_values[0,0]=np.nan
    with raster_workspace():
        result=compute.raster_curvature(raster_load(source_plan(path)),1,'metre');ds=gdal.Open(str(result.path));actual=read_band_values(ds.GetRasterBand(1));ds=None
        np.testing.assert_allclose(actual,_curvature_lstsq_oracle(expected_values,1,1),rtol=1e-12,atol=1e-12,equal_nan=True)
        assert actual[3,3]==-2.


@pytest.mark.parametrize('p,q,r,s,t',[(1e200,0.,1e308,0.,0.),(1.7e308,1.7e308,1.6e308,0.,1.6e308),(1e100,-1e100,1e250,3e249,-2e250),(.2,.3,1.7e308,-1e308,1.7e308)])
def test_dem_curvature_stable_geometric_formula_against_decimal_native_extremes(p,q,r,s,t):
    import operators.raster_compute as compute
    from decimal import Decimal,localcontext
    with localcontext() as ctx:
        ctx.prec=100;pD,qD,rD,sD,tD=map(Decimal.from_float,(p,q,r,s,t))
        norm=1+pD*pD+qD*qD;expected=float(-((1+qD*qD)*rD-2*pD*qD*sD+(1+pD*pD)*tD)/(2*norm*norm.sqrt()))
    actual=float(compute._mean_curvature(*[np.array([n]) for n in (p,q,r,s,t)])[0])
    assert actual==pytest.approx(expected,rel=2e-14,abs=0)


@pytest.mark.parametrize('dx,dy',[(math.inf,1.),(0.,1.),(1.,math.inf)])
@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('method',['mean','profile','tangential'])
def test_dem_curvature_unrepresentable_physical_step_rejects_before_output(tmp_path,monkeypatch,dx,dy,language,method):
    import operators.raster_compute as compute
    path=_curvature_file(tmp_path,np.zeros((5,5)))
    with raster_workspace(),raster_error_language(language):
        source=raster_load(source_plan(path));dataset=compute._open(source)
        class Grid:
            def GetGeoTransform(self):return (0,dx,0,0,0,-dy)
        monkeypatch.setattr(compute,'_dem_admission',lambda *a,**k:(Grid(),1.))
        prior=set(source.workspace.iterdir())
        with pytest.raises(ValueError) as error:compute.raster_curvature(source,1,'metre',method=method)
        assert str(error.value)==str(compute._curvature_error('step',method));assert set(source.workspace.iterdir())==prior


@pytest.mark.parametrize('kind',['difference','derivative'])
@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('method',['mean','profile','tangential'])
def test_dem_curvature_nonfinite_intermediate_fails_not_nodata(tmp_path,kind,language,method):
    import operators.raster_compute as compute
    values=np.full((3,3),1.7e308);values[1,1]=-1.7e308
    if kind=='derivative':values=np.array([[1.,2.,1.],[2.,3.,2.],[1.,2.,1.]])
    path=_curvature_file(tmp_path,values,dx=1e-200 if kind=='derivative' else 1.,dy=1.)
    with raster_workspace(),raster_error_language(language):
        source=raster_load(source_plan(path));prior=set(source.workspace.iterdir())
        with pytest.raises(ValueError) as error:compute.raster_curvature(source,1,'metre',method=method)
        assert str(error.value)==str(compute._curvature_error('numeric',method));assert set(source.workspace.iterdir())==prior


@pytest.mark.parametrize('method',['mean','profile','tangential'])
def test_dem_curvature_cross_block_complete_cog_pyramid_bounded_halo_and_source_prior_unchanged(tmp_path,monkeypatch,method):
    import operators.raster_compute as compute
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    rows,cols=np.indices((519,515));x=cols*2.;y=-rows*3.
    values=.0001*x*x+.00004*x*y-.0002*y*y+.02*x-.03*y+100
    cube=np.stack((np.full(values.shape,.25),values,np.full(values.shape,-9999.),np.full(values.shape,255.)))
    cube[1,100,100]=np.inf;cube[3,510,512]=0;cube[3,30,30]=128
    path=_curvature_file(tmp_path,cube,unit='');ds=gdal.Open(str(path),gdal.GA_Update);ds.GetRasterBand(2).SetUnitType('metre');ds.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
    mask=np.full(values.shape,255,np.uint8);mask[512,510]=0;ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);ds=None
    values[100,100]=values[510,512]=values[512,510]=np.nan
    expected=_curvature_lstsq_oracle(values,2,3) if method=='mean' else _directional_curvature_lstsq_oracle(values,2,3,method=method);before=path.read_bytes();prior=tmp_path/'prior.bin';prior.write_bytes(b'old output');target=tmp_path/'curvature.cog.tif'
    reads=[];blocks=compute._blocks
    def capture(*args,**kwargs):
        for block in blocks(*args,**kwargs):assert max(block[2][0].shape)<=512;reads.append(block[:2]);yield block
    monkeypatch.setattr(compute,'_blocks',capture)
    with raster_workspace():
        result=compute.raster_curvature(raster_load(source_plan(path)),2,'metre',method=method);artifact=raster_save(result,target_plan(target),profile='cog',blocksize=128,overview_resampling='nearest')
        assert type(artifact['size_bytes']) is int and artifact['size_bytes']==target.stat().st_size>0
    ds=gdal.Open(str(target));band=ds.GetRasterBand(1)
    np.testing.assert_allclose(read_band_values(band),expected,rtol=1e-10,atol=1e-11,equal_nan=True)
    np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected));assert not validate(ds,full_check=True)[1]
    assert band.GetUnitType()=='1/metre' and band.GetOverviewCount()>0
    previous=expected
    for i in range(band.GetOverviewCount()):
        overview=band.GetOverview(i);h,w=overview.YSize,overview.XSize
        ys=np.floor(.5+np.arange(h)*(previous.shape[0]/h)).astype(int);xs=np.floor(.5+np.arange(w)*(previous.shape[1]/w)).astype(int);wanted=previous[np.ix_(ys,xs)]
        np.testing.assert_allclose(overview.ReadAsArray(),wanted,rtol=1e-10,atol=1e-11,equal_nan=True)
        np.testing.assert_array_equal(overview.GetMaskBand().ReadAsArray()!=0,np.isfinite(wanted));previous=wanted
    band=ds=None
    assert path.read_bytes()==before and prior.read_bytes()==b'old output' and any(x>=509 and y>=509 for x,y in reads)


@pytest.mark.parametrize('stage',['read','compute','write'])
@pytest.mark.parametrize('method',['mean','profile','tangential'])
def test_dem_curvature_injected_failure_cleans_private_output(tmp_path,monkeypatch,stage,method):
    import operators.raster_compute as compute
    path=_curvature_file(tmp_path,np.ones((519,515)));windows=compute._neighbourhood_windows;block=compute._curvature_block;calls=0
    def fail_windows(*a,**k):
        for item in windows(*a,**k):
            if stage=='read':raise RuntimeError('injected curvature failure')
            yield item
    def fail_block(*a):
        nonlocal calls;calls+=1
        if stage=='compute' or (stage=='write' and calls==2):raise RuntimeError('injected curvature failure')
        return block(*a)
    monkeypatch.setattr(compute,'_neighbourhood_windows',fail_windows);monkeypatch.setattr(compute,'_curvature_block',fail_block)
    with raster_workspace():
        source=raster_load(source_plan(path));prior=set(source.workspace.iterdir())
        with pytest.raises(RuntimeError,match='injected curvature failure'):compute.raster_curvature(source,1,'metre',method=method)
        assert set(source.workspace.iterdir())==prior


@pytest.mark.parametrize('case',['direct','feet','filled','reclassified','invalid','empty-neighbourhood','bad-unit','bad-method'])
@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('method',['mean','profile','tangential'])
def test_dem_curvature_real_async_existing_ports_cog_and_frozen_failure_language(tmp_path,monkeypatch,case,language,method):
    import api_server,workflow_engine
    rows,cols=np.indices((9,11));values=.02*(cols-5)**2+.03*(rows-4)**2-20
    if case=='invalid':values[:]=np.nan
    if case=='empty-neighbourhood':values[:]=np.nan;values[4,5]=0
    factor=.3048 if case=='feet' else 1.;path=_curvature_file(tmp_path,values/factor,unit='foot' if case=='feet' else 'metre');before=path.read_bytes();target=tmp_path/'curvature.cog.tif'
    tasks=[{'id':'source','operator':'raster_load','depends_on':[],'params':{'access_plan':source_plan(path)}}];prior='source';band_unit='foot' if case=='feet' else 'metre'
    if case=='filled':
        tasks.append({'id':'fill','operator':'raster_fill_depressions','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':1,'elevation_unit':'metre'}});prior='fill';values=_fill_level_reachability_oracle(values)
    if case=='reclassified':
        tasks.append({'id':'classes','operator':'raster_reclassify','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':1,'rules':[{'min':None,'max':-19.5,'class':0},{'min':-19.5,'max':None,'class':1}]}});prior='classes';values=np.where(values<-19.5,0.,1.)
    tasks.extend([{'id':'curvature','operator':'raster_curvature','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':1,'elevation_unit':'yard' if case=='bad-unit' else band_unit,'method':'bad' if case=='bad-method' else method}},
        {'id':'save','operator':'raster_save','depends_on':['curvature'],'params':{'input_raster':{'$ref':'curvature'},'access_plan':target_plan(target),'profile':'cog','blocksize':128}}])
    workspaces=[];execute=workflow_engine._execute_operator
    def capture(name,params):
        if name=='raster_curvature':workspaces.append(params['input_raster'].workspace)
        return execute(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',capture);client=api_server.app.test_client()
    assert client.post('/api/operators/raster_curvature/invoke',json={'params':{}}).status_code==403
    response=client.post('/api/workflow',headers={'Accept-Language':language},json={'workflow_def':{'tasks':tasks},'input_data':{},'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}});assert response.status_code==202,response.json
    deadline=time.monotonic()+15
    while time.monotonic()<deadline:
        status=client.get('/api/executions/'+response.json['execution_id'],headers={'Accept-Language':'en' if language=='zh-cn' else 'zh-cn'}).json
        if status['status'] in ('success','failed'):break
        time.sleep(.01)
    assert status['status']==('failed' if case in ('bad-unit','bad-method') else 'success'),status
    assert workspaces and all(not p.exists() for p in workspaces) and path.read_bytes()==before
    if case in ('bad-unit','bad-method'):
        assert not target.exists();phrase='method' if case=='bad-method' else ('高程单位' if language=='zh-cn' else 'elevation_unit');assert phrase in status['error']
        assert client.get('/api/executions/'+response.json['execution_id'],headers={'Accept-Language':language}).json['error']==status['error']
    else:
        ds=gdal.Open(str(target));band=ds.GetRasterBand(1)
        np.testing.assert_allclose(read_band_values(band),(_curvature_lstsq_oracle(values/factor,2,3,factor) if method=='mean' else _directional_curvature_lstsq_oracle(values/factor,2,3,factor,method=method)),rtol=1e-11,atol=1e-11,equal_nan=True)
        assert band.GetUnitType()=='1/metre';band=ds=None
        with raster_workspace():assert validate_cog(raster_load(source_plan(target)))['valid']
        artifact=json.loads(status['result']);assert type(artifact['size_bytes']) is int and artifact['size_bytes']==target.stat().st_size>0


def _directional_curvature_lstsq_oracle(values, dx, dy, factor=1., method="profile"):
    """Independent least squares and directional first/second fundamental forms."""
    result=np.full(values.shape,np.nan)
    if min(values.shape)<3:return result
    samples=np.lib.stride_tricks.sliding_window_view(values,(3,3))
    valid=np.isfinite(samples).all(axis=(-2,-1))
    if not valid.any():return result
    windows=samples[valid].reshape(-1,9)
    x=np.tile(np.arange(-1,2),3);y=np.repeat(np.arange(1,-2,-1),3)
    design=np.column_stack((x*x,x*y,y*y,x,y,np.ones(9)))
    relative=((windows.astype(object)-windows[:,4:5].astype(object)).astype(float)
              if np.issubdtype(values.dtype,np.integer) else windows-windows[:,4:5])*factor
    coefficients=np.linalg.lstsq(design,relative.T,rcond=None)[0].T
    a,b,c,p,q,_=coefficients.T;p=p/dx;q=q/dy;r=2*a/dx**2;s=b/dx/dy;t=2*c/dy**2
    flat=(windows==windows[:,4:5]).all(axis=1)
    # Odd design columns are orthogonal; exact symmetric moments classify stationary samples.
    stationary=np.array([math.fsum(row*x)==0 and math.fsum(row*y)==0 for row in relative])
    active=~stationary
    answer=np.full(len(p),np.nan);answer[flat]=0
    if active.any():
        p,q,r,s,t=[v[active] for v in (p,q,r,s,t)]
        first=np.empty((len(p),2,2));second=np.empty_like(first)
        first[:,0,0]=1+p*p;first[:,1,1]=1+q*q;first[:,0,1]=first[:,1,0]=p*q
        norm=np.sqrt(1+p*p+q*q)
        second[:,0,0]=-r/norm;second[:,1,1]=-t/norm;second[:,0,1]=second[:,1,0]=-s/norm
        tangent=np.column_stack((p,q) if method=="profile" else (-q,p))
        answer[active]=np.einsum('bi,bij,bj->b',tangent,second,tangent)/np.einsum('bi,bij,bj->b',tangent,first,tangent)
    result[1:-1,1:-1][valid]=answer
    return result


@pytest.mark.parametrize('coefficients',[(0,0,0,0,0),(0,0,0,2,-3),(1,0,2,0,0),(-1,0,-2,0,0),(1,0,-1,0,0),(.125,.25,-.5,2,-3)])
@pytest.mark.parametrize('dx,dy',[(1.,1.),(2.,3.),(.25,4.)])
@pytest.mark.parametrize('method',['profile','tangential'])
def test_dem_directional_curvature_analytic_directional_form_and_mean_unchanged(tmp_path,coefficients,dx,dy,method):
    import operators.raster_compute as compute
    a,b,c,d,e=coefficients;rows,cols=np.indices((9,11));x=(cols-5)*dx;y=(4-rows)*dy
    values=a*x*x+b*x*y+c*y*y+d*x+e*y+100
    path=_curvature_file(tmp_path,values,dx,dy);before=path.read_bytes()
    with raster_workspace():
        source=raster_load(source_plan(path));result=compute.raster_curvature(source,1,'metre',method=method)
        ds=gdal.Open(str(result.path));band=ds.GetRasterBand(1);actual=read_band_values(band)
        expected=_directional_curvature_lstsq_oracle(values,dx,dy,method=method)
        np.testing.assert_allclose(actual,expected,rtol=1e-11,atol=1e-12,equal_nan=True)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected))
        assert band.GetUnitType()=='1/metre' and band.DataType==gdal.GDT_Float64 and math.isnan(band.GetNoDataValue())
        assert band.GetScale() is None and band.GetOffset() is None and band.GetColorTable() is None
        band=ds=None
        if d==e==0 and (a!=0 or b!=0 or c!=0):assert np.isnan(actual[4,5])
        if a==b==c==0:np.testing.assert_array_equal(actual[1:-1,1:-1],np.zeros((7,9)))
        default=compute.raster_curvature(source,1,'metre');explicit=compute.raster_curvature(source,1,'metre',method='mean')
        left=gdal.Open(str(default.path));right=gdal.Open(str(explicit.path))
        assert left.GetRasterBand(1).ReadRaster()==right.GetRasterBand(1).ReadRaster()
        np.testing.assert_allclose(read_band_values(left.GetRasterBand(1)),_curvature_lstsq_oracle(values,dx,dy),rtol=1e-12,atol=1e-12,equal_nan=True)
        left=right=None
    assert path.read_bytes()==before


@pytest.mark.parametrize('kind',['flat','hill','bowl','saddle','horizontal-fit-residual'])
@pytest.mark.parametrize('method',['profile','tangential'])
def test_dem_directional_curvature_stationary_centres_only_true_flat_is_zero(tmp_path,kind,method):
    import operators.raster_compute as compute
    y,x=np.indices((3,3));x=x-1;y=1-y
    values={'flat':np.full((3,3),42.),'hill':42-x*x-2*y*y,'bowl':x*x+2*y*y,
            'saddle':x*x-y*y,'horizontal-fit-residual':np.outer([1,-2,1],[1,-2,1])}[kind]
    path=_curvature_file(tmp_path,values)
    with raster_workspace():
        result=compute.raster_curvature(raster_load(source_plan(path)),1,'metre',method=method);ds=gdal.Open(str(result.path));band=ds.GetRasterBand(1)
        actual=read_band_values(band);expected=_directional_curvature_lstsq_oracle(values,2,3,method=method)
        np.testing.assert_array_equal(actual,expected);np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected));band=ds=None
    assert actual[1,1]==0 if kind=='flat' else np.isnan(actual[1,1])


@pytest.mark.parametrize('seed',range(8))
@pytest.mark.parametrize('method',['profile','tangential'])
def test_dem_directional_curvature_random_nine_point_lstsq(tmp_path,seed,method):
    import operators.raster_compute as compute
    values=np.random.default_rng(seed).normal(size=(9,11))*7+100
    path=_curvature_file(tmp_path,values)
    with raster_workspace():
        result=compute.raster_curvature(raster_load(source_plan(path)),1,'metre',method=method);ds=gdal.Open(str(result.path))
        np.testing.assert_allclose(read_band_values(ds.GetRasterBand(1)),_directional_curvature_lstsq_oracle(values,2,3,method=method),rtol=1e-11,atol=1e-12,equal_nan=True);ds=None


@pytest.mark.parametrize('epsg',[3857,2222,2277])
@pytest.mark.parametrize('unit,factor',[('metre',1.),('foot',.3048),('us_survey_foot',1200/3937)])
@pytest.mark.parametrize('method',['profile','tangential'])
def test_dem_directional_curvature_units_and_height_translation(tmp_path,epsg,unit,factor,method):
    import operators.raster_compute as compute
    rows,cols=np.indices((7,9));x=(cols-4)*2.;y=(3-rows)*3.
    values=(.125*x*x+.25*x*y-.5*y*y+20*x-30*y-100)/factor
    path=_curvature_file(tmp_path,values,epsg=epsg,unit=unit)
    with raster_workspace():
        result=compute.raster_curvature(raster_load(source_plan(path)),1,unit,method=method);ds=gdal.Open(str(result.path));band=ds.GetRasterBand(1)
        np.testing.assert_allclose(read_band_values(band),_directional_curvature_lstsq_oracle(values,2,3,factor,method=method),rtol=1e-11,atol=1e-12,equal_nan=True)
        assert band.GetUnitType()=='1/metre' and ds.GetSpatialRef().GetAuthorityCode(None)==str(epsg);band=ds=None


@pytest.mark.parametrize('shape',[(1,1),(1,8),(7,1),(2,5),(5,2),(3,3),(5,7)])
@pytest.mark.parametrize('empty',[False,True])
@pytest.mark.parametrize('method',['profile','tangential'])
def test_dem_directional_curvature_empty_small_and_horizontal_flat(tmp_path,shape,empty,method):
    import operators.raster_compute as compute
    values=np.full(shape,np.nan if empty else 7.);path=_curvature_file(tmp_path,values)
    with raster_workspace():
        result=compute.raster_curvature(raster_load(source_plan(path)),1,'metre',method=method);ds=gdal.Open(str(result.path));band=ds.GetRasterBand(1)
        expected=_directional_curvature_lstsq_oracle(values,2,3,method=method);np.testing.assert_array_equal(read_band_values(band),expected)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected));band=ds=None


@pytest.mark.parametrize('fault',['nan','inf','nodata','mask','alpha-zero','alpha-nan','partial-alpha'])
@pytest.mark.parametrize('method',['profile','tangential'])
def test_dem_directional_curvature_full_neighbourhood_explicit_band_and_mask(tmp_path,fault,method):
    import operators.raster_compute as compute
    rows,cols=np.indices((7,9));values=(cols-4.)**2+(rows-3.)**2+20*cols
    cube=np.stack((np.full(values.shape,.25),values,np.full(values.shape,-9999.),np.full(values.shape,255.)))
    if fault in ('nan','inf','nodata'):cube[1,3,4]={'nan':np.nan,'inf':np.inf,'nodata':-9999.}[fault]
    if fault.startswith('alpha-'):cube[3,3,4]=0 if fault=='alpha-zero' else np.nan
    if fault=='partial-alpha':cube[3,3,4]=128
    path=_curvature_file(tmp_path,cube,unit='');ds=gdal.Open(str(path),gdal.GA_Update);ds.GetRasterBand(2).SetUnitType('metre');ds.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand)
    if fault=='mask':
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
        mask=np.full(values.shape,255,np.uint8);mask[3,4]=0;ds.GetRasterBand(1).GetMaskBand().WriteArray(mask)
    ds=None;wanted=values.copy()
    if fault!='partial-alpha':wanted[3,4]=np.nan
    before=path.read_bytes()
    with raster_workspace():
        result=compute.raster_curvature(raster_load(source_plan(path)),2,'metre',method=method);ds=gdal.Open(str(result.path));band=ds.GetRasterBand(1);expected=_directional_curvature_lstsq_oracle(wanted,2,3,method=method)
        np.testing.assert_allclose(read_band_values(band),expected,rtol=1e-11,atol=1e-12,equal_nan=True)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected));band=ds=None
    assert path.read_bytes()==before


@pytest.mark.parametrize('method',[None,True,1,'','MEAN','plan',[],{}])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_dem_profile_curvature_rejects_invalid_method_before_output(tmp_path,method,language):
    import operators.raster_compute as compute
    path=_curvature_file(tmp_path,np.ones((5,5)))
    with raster_workspace(),raster_error_language(language):
        source=raster_load(source_plan(path));prior=set(source.workspace.iterdir())
        with pytest.raises(ValueError,match='method'):compute.raster_curvature(source,1,'metre',method=method)
        assert set(source.workspace.iterdir())==prior


@pytest.mark.parametrize('dtype',[np.int64,np.uint64])
@pytest.mark.parametrize('method',['profile','tangential'])
def test_dem_directional_curvature_native_64bit_differences_nodata_and_stationary(tmp_path,dtype,method):
    import operators.raster_compute as compute
    from osgeo import gdal_array
    rows,cols=np.indices((7,7));small=(rows-3)**2+(cols-3)**2+100;base=2**63 if dtype==np.uint64 else -2**63
    values=np.array([[base+int(n) for n in line] for line in small],dtype=dtype);nodata=base+99;values[0,0]=nodata
    path=tmp_path/'profile-integer.tif';ds=gdal.GetDriverByName('GTiff').Create(str(path),7,7,1,gdal_array.NumericTypeCodeToGDALTypeCode(dtype));ds.SetGeoTransform((500000,1,0,3000000,0,-1));crs=osr.SpatialReference();crs.ImportFromEPSG(3857);ds.SetSpatialRef(crs)
    band=ds.GetRasterBand(1);band.WriteArray(values)
    if dtype==np.uint64:band.SetNoDataValueAsUInt64(nodata)
    else:band.SetNoDataValueAsInt64(nodata)
    band=ds=None;wanted=small.astype(float);wanted[0,0]=np.nan;before=path.read_bytes()
    with raster_workspace():
        result=compute.raster_curvature(raster_load(source_plan(path)),1,'metre',method=method);ds=gdal.Open(str(result.path));band=ds.GetRasterBand(1);expected=_directional_curvature_lstsq_oracle(wanted,1,1,method=method)
        np.testing.assert_allclose(read_band_values(band),expected,rtol=1e-12,atol=1e-12,equal_nan=True)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected));band=ds=None
    assert path.read_bytes()==before and np.isnan(expected[3,3])


@pytest.mark.parametrize('p,q,r,s,t',[(1e-200,0.,2.,0.,0.),(1e-200,-2e-200,2.,.5,-3.),(1e200,0.,1e308,0.,0.),(1.7e308,1.7e308,1.6e308,0.,1.6e308),(1e100,-1e100,1e250,3e249,-2e250),(.2,.3,1.7e308,-1e308,1.7e308)])
def test_dem_profile_curvature_stable_extremes_against_decimal(p,q,r,s,t):
    import operators.raster_compute as compute
    from decimal import Decimal,localcontext
    with localcontext() as ctx:
        ctx.prec=120;P,Q,R,S,T=map(Decimal.from_float,(p,q,r,s,t));slope=P*P+Q*Q;norm=1+slope
        expected=float(-(R*P*P+2*S*P*Q+T*Q*Q)/(slope*norm*norm.sqrt()))
    actual=float(compute._profile_curvature(*[np.array([n]) for n in (p,q,r,s,t)])[0])
    assert math.isfinite(actual) and actual==pytest.approx(expected,rel=3e-14,abs=0)


@pytest.mark.parametrize('slope',[1e-200,1e-300])
@pytest.mark.parametrize('method',['profile','tangential'])
def test_dem_directional_curvature_tiny_nonzero_slope_remains_valid_zero(tmp_path,slope,method):
    import operators.raster_compute as compute
    values=np.tile(np.array([-slope,0.,slope]),(3,1));path=_curvature_file(tmp_path,values,1,1)
    with raster_workspace():
        result=compute.raster_curvature(raster_load(source_plan(path)),1,'metre',method=method);ds=gdal.Open(str(result.path));band=ds.GetRasterBand(1)
        actual=read_band_values(band);assert actual[1,1]==0 and band.GetMaskBand().ReadAsArray()[1,1]==255;band=ds=None


@pytest.mark.parametrize('number',[np.inf,np.nan])
@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('method',['profile','tangential'])
def test_dem_directional_curvature_numerical_nonfinite_is_failure_not_stationary_nodata(tmp_path,monkeypatch,number,language,method):
    import operators.raster_compute as compute
    values=np.tile(np.arange(5.),(5,1));path=_curvature_file(tmp_path,values)
    monkeypatch.setattr(compute,'_profile_curvature' if method=='profile' else '_tangential_curvature',lambda p,q,r,s,t:np.full(p.shape,number))
    with raster_workspace(),raster_error_language(language):
        source=raster_load(source_plan(path));prior=set(source.workspace.iterdir())
        with pytest.raises(ValueError) as error:compute.raster_curvature(source,1,'metre',method=method)
        assert ('DEM '+method+' curvature' if language=='en' else 'DEM '+{'profile':'剖面曲率','tangential':'切向曲率'}[method]) in str(error.value)
        assert set(source.workspace.iterdir())==prior


@pytest.mark.parametrize('kind',['flat','hill','bowl','saddle','horizontal-fit-residual'])
def test_dem_tangential_curvature_stationary_and_mean_profile_results(tmp_path,kind):
    import operators.raster_compute as compute
    yy,xx=np.indices((3,3));x=xx-1.;y=1.-yy
    values=np.full((3,3),42.) if kind=='flat' else (-(x*x+y*y) if kind=='hill' else x*x+y*y if kind=='bowl' else x*x-y*y if kind=='saddle' else np.outer([1.,-2.,1.],[1.,-2.,1.]))
    path=_curvature_file(tmp_path,values);before=path.read_bytes()
    with raster_workspace():
        source=raster_load(source_plan(path));old=[]
        for method in ('mean','profile'):
            ds=gdal.Open(str(compute.raster_curvature(source,1,'metre',method=method).path));old.append(ds.GetRasterBand(1).ReadRaster());ds=None
        result=compute.raster_curvature(source,1,'metre',method='tangential');ds=gdal.Open(str(result.path));band=ds.GetRasterBand(1)
        actual=read_band_values(band);mask=band.GetMaskBand().ReadAsArray();band=ds=None
        assert actual[1,1]==0. if kind=='flat' else np.isnan(actual[1,1])
        assert mask[1,1]==(255 if kind=='flat' else 0)
        assert np.isnan(actual[[0,-1],:]).all() and np.isnan(actual[:,[0,-1]]).all()
        for method,wanted in zip(('mean','profile'),old):
            ds=gdal.Open(str(compute.raster_curvature(source,1,'metre',method=method).path));assert ds.GetRasterBand(1).ReadRaster()==wanted;ds=None
    assert path.read_bytes()==before


@pytest.mark.parametrize('p,q,r,s,t',[(1e-200,0.,2.,0.,3.),(1e-200,-2e-200,2.,.5,-3.),(1e200,0.,1e308,0.,1e308),(1.7e308,1.7e308,1.6e308,0.,1.6e308),(1e100,-1e100,1e250,3e249,-2e250),(.2,.3,1.7e308,-1e308,1.7e308),(1e-200,1e-200,1.7e308,1.7e308,1.7e308)])
def test_dem_tangential_curvature_stable_extremes_against_decimal(p,q,r,s,t):
    import operators.raster_compute as compute
    from decimal import Decimal,localcontext
    with localcontext() as ctx:
        ctx.prec=2000;P,Q,R,S,T=map(Decimal.from_float,(p,q,r,s,t));slope=P*P+Q*Q
        expected=float(-(R*Q*Q-2*S*P*Q+T*P*P)/(slope*(1+slope).sqrt()))
    with np.errstate(over='ignore',under='ignore',invalid='ignore'):
        actual=float(compute._tangential_curvature(*[np.array([n]) for n in (p,q,r,s,t)])[0])
    if math.isfinite(expected):assert math.isfinite(actual) and actual==pytest.approx(expected,rel=3e-14,abs=0)
    else:assert actual==expected  # The public operator rejects a nonfinite result.


@pytest.mark.parametrize('slope',[1e-200,1e-300])
def test_dem_tangential_curvature_tiny_nonzero_gradient_retains_nonzero_contour_curvature(tmp_path,slope):
    import operators.raster_compute as compute
    values=np.array([[1.,1.,1.],[-slope,0.,slope],[1.,1.,1.]])
    path=_curvature_file(tmp_path,values,1.,1.)
    with raster_workspace():
        ds=gdal.Open(str(compute.raster_curvature(raster_load(source_plan(path)),1,'metre',method='tangential').path))
        band=ds.GetRasterBand(1);assert band.ReadAsArray()[1,1]==-2. and band.GetMaskBand().ReadAsArray()[1,1]==255;band=ds=None


@pytest.mark.parametrize('seed',range(10))
def test_dem_tangential_curvature_independent_oracle_euler_identity_and_plan_distinction(tmp_path,seed):
    import operators.raster_compute as compute
    rng=np.random.default_rng(seed);values=rng.normal(size=(9,11))*2+50.;path=_curvature_file(tmp_path,values)
    outputs={}
    with raster_workspace():
        source=raster_load(source_plan(path))
        for method in ('mean','profile','tangential'):
            ds=gdal.Open(str(compute.raster_curvature(source,1,'metre',method=method).path));outputs[method]=read_band_values(ds.GetRasterBand(1));ds=None
    want=_directional_curvature_lstsq_oracle(values,2,3,method='tangential')
    np.testing.assert_allclose(outputs['tangential'],want,rtol=1e-11,atol=1e-12,equal_nan=True)
    active=np.isfinite(outputs['profile']) & np.isfinite(outputs['tangential'])
    np.testing.assert_allclose((outputs['profile']+outputs['tangential'])[active],(2*outputs['mean'])[active],rtol=1e-11,atol=1e-12)
    # A steep analytic surface distinguishes geometric normal contour curvature
    # from the horizontal projection (plan curvature).
    rows,cols=np.indices((7,9));x=(cols-4)*2.;y=(3-rows)*3.;surface=.25*x*x+.5*y*y+3*x+4*y
    other=tmp_path/'steep';other.mkdir();path=_curvature_file(other,surface)
    with raster_workspace():
        ds=gdal.Open(str(compute.raster_curvature(raster_load(source_plan(path)),1,'metre',method='tangential').path));actual=ds.GetRasterBand(1).ReadAsArray()[3,4];ds=None
    numerator=.5*4**2+1.*3**2
    assert actual==pytest.approx(-numerator/(25*math.sqrt(26)),rel=1e-12)
    assert not np.isclose(actual,-numerator/25**1.5,rtol=1e-6,atol=0)
