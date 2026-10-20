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
    raster_focal_statistics, raster_vector_zonal_statistics, raster_proximity,
)
from workflow_engine import execute_workflow
from operators.raster_operators import build_raster_mosaic



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
