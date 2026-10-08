import json
import math
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
    raster_align, raster_stack, raster_select_bands,
    _classification_rules, raster_reclassify, raster_aggregate,
)
from workflow_engine import execute_workflow
from operators.raster_operators import build_raster_mosaic



def source_plan(path, format='tiff'):
    return {'schema_version': 'addp.workflow.access-plan/v1', 'source': {
        'kind': 'file', 'format': format, 'access': {'method': 'mounted_path', 'path': str(path)}}}


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


def _raster_profile_worker(source, targets, scratch, connection, profile_case, execution_mode):
    """Measure one process independently of the parent's pixel oracle."""
    import traceback
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
                         'environment': {'platform': platform.platform(), 'machine': platform.machine(),
                                         'cpu_count': os.cpu_count(), 'python': platform.python_version(),
                                         'gdal': gdal.VersionInfo('RELEASE_NAME'),
                                         'gdal_cache_max_bytes': gdal.GetCacheMax()}})
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
        try:
            assert receiver.poll(300), 'Raster profile worker exceeded 300 seconds'
            report = receiver.recv()
            process.join(10)
            assert process.exitcode == 0
        finally:
            if process.is_alive():
                process.terminate()
                process.join(10)
            receiver.close()
    assert 'error' not in report, report.get('error')
    return report


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
        mosaic = raster_mosaic(clipped,rasters[1],'EPSG:4326',[1,1],overlap)
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
        mosaic = raster_mosaic(warped,other_raster,'EPSG:3857',[resolution,resolution],overlap)
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
        last = raster_mosaic(raster, other, 'EPSG:4326', [1,1], 'last')
        first = raster_mosaic(raster, other, 'EPSG:4326', [1,1], 'first')
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
        result = raster_mosaic(raster_load(source_plan(left)), raster_load(source_plan(right)), 'EPSG:4326', [1, 1])
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
