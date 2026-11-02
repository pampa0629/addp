"""Independent line-chainage/point-support oracles and actual file pipelines."""
from decimal import Decimal, localcontext
from fractions import Fraction
import hashlib
import json
import math
from pathlib import Path
import time

import geopandas as gpd
import numpy as np
import pandas as pd
import pytest
from osgeo import gdal, osr
from shapely import from_wkt
from shapely.geometry import LineString, MultiLineString, Point, Polygon

import operators.raster_compute as c


def _source(path, values=None, grid=(0., 1., 0., 8., 0., -1.), unit=1., unit_name='metre', epsg=3857, coverage=False):
    values = np.arange(64, dtype=float).reshape(8, 8) if values is None else np.asarray(values)
    dtype = gdal.GDT_UInt64 if values.dtype == np.uint64 else gdal.GDT_Int64 if values.dtype == np.int64 else gdal.GDT_Float64
    nodata = 2**64-1 if dtype == gdal.GDT_UInt64 else -2**63 if dtype == gdal.GDT_Int64 else -9999.
    first = np.full(values.shape, 1.5) if dtype == gdal.GDT_Float64 else np.zeros(values.shape, dtype=values.dtype)
    alpha = np.full(values.shape, 128, dtype=values.dtype)
    mask = np.full(values.shape, 255, np.uint8)
    values = values.copy()
    if coverage:
        values[1, 1] = nodata; mask[2, 2] = 0; alpha[3, 3] = 0
        alpha[4, 4] = nodata; alpha[5, 5] = 1
        if dtype == gdal.GDT_Float64: alpha[6, 6] = np.nan
    ds = gdal.GetDriverByName('GTiff').Create(str(path), values.shape[1], values.shape[0], 3, dtype)
    reference = osr.SpatialReference(); reference.ImportFromEPSG(epsg)
    if epsg != 4326: reference.SetLinearUnits(unit_name, unit)
    ds.SetProjection(reference.ExportToWkt()); ds.SetGeoTransform(grid)
    for i, data in enumerate((first, values, alpha), 1):
        band = ds.GetRasterBand(i); band.WriteArray(data)
        if dtype == gdal.GDT_UInt64: band.SetNoDataValueAsUInt64(nodata)
        elif dtype == gdal.GDT_Int64: band.SetNoDataValueAsInt64(nodata)
        else: band.SetNoDataValue(nodata)
        band.SetScale(17.); band.SetOffset(500.)
    ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK', 'YES'): ds.CreateMaskBand(gdal.GMF_PER_DATASET)
    ds.GetRasterBand(1).GetMaskBand().WriteArray(mask); band = ds = None
    ds = gdal.Open(str(path)); projection = ds.GetProjection(); ds = None
    return path, projection, values, mask, alpha, nodata


def _plan(path):
    return {'schema_version': 'addp.workflow.access-plan/v1', 'source': {'kind': 'file', 'format': 'tiff', 'access': {'method': 'mounted_path', 'path': str(path)}}}


def _frame(lines, crs, *, ids=None):
    size = len(lines)
    frame = gpd.GeoDataFrame({'line_id': pd.array(range(11, 11+size) if ids is None else ids, dtype='Int64'),
                             'nullable': pd.array([None if i % 2 else i for i in range(size)], dtype='Int64'),
                             'enabled': pd.array([True if i % 2 else None for i in range(size)], dtype='boolean'),
                             'geometry': ['ordinary geometry attribute']*size,
                             'trace': gpd.GeoSeries(lines, crs=crs)}, geometry='trace', crs=crs)
    frame.index = pd.Index(['same']*size, name='old_index')
    return frame


def _world(grid, pixel):
    col, row = pixel
    return (grid[0]+grid[1]*col+grid[2]*row, grid[3]+grid[4]*col+grid[5]*row)


def _chain_oracle(line, spacing, unit):
    """Restart a scalar segment walk per sample; Decimal computes the roots."""
    coordinates = list(line.coords); edges = []
    with localcontext() as context:
        context.prec = 500
        for a, b in zip(coordinates, coordinates[1:]):
            dx = Decimal.from_float(b[0])-Decimal.from_float(a[0]); dy = Decimal.from_float(b[1])-Decimal.from_float(a[1])
            squared = (dx*dx+dy*dy)*Decimal.from_float(unit)**2
            if squared: edges.append((a, b, Fraction(float(squared.sqrt()))))
    length = sum((edge[2] for edge in edges), Fraction(0)); step = Fraction(spacing)
    count = (length/step).numerator//(length/step).denominator
    positions = [i*step for i in range(count+1) if i*step < length]+[length]
    answer = []
    for distance in positions:
        if distance == 0: xy = coordinates[0]
        elif distance == length: xy = coordinates[-1]
        else:
            previous = Fraction(0)
            for a, b, segment in edges:
                if distance <= previous+segment:
                    t = (distance-previous)/segment
                    xy = tuple(float(Fraction(a[i])+(Fraction(b[i])-Fraction(a[i]))*t) for i in (0, 1)); break
                previous += segment
        answer.append((float(distance), xy))
    return answer


def _value_oracle(xy, grid, data, mask, alpha, nodata, method):
    ox, a, b, oy, d, e = map(Fraction, grid); x, y = Fraction(xy[0])-ox, Fraction(xy[1])-oy
    col, row = (e*x-b*y)/(a*e-b*d), (a*y-d*x)/(a*e-b*d)
    height, width = data.shape
    if not(0 <= col < width and 0 <= row < height): return None, 'outside'
    if method == 'cell': members = [(math.floor(col), math.floor(row), Fraction(1))]
    else:
        left, top = math.floor(col-Fraction(1, 2)), math.floor(row-Fraction(1, 2))
        dx, dy = col-Fraction(1, 2)-left, row-Fraction(1, 2)-top
        members = [(left+i, top+j, wx*wy) for i, wx in enumerate((1-dx, dx)) for j, wy in enumerate((1-dy, dy)) if wx*wy > 0]
    total = Fraction(0)
    for x, y, weight in members:
        if not(0 <= x < width and 0 <= y < height): return None, 'nodata'
        if (data[y, x] == nodata or not np.isfinite(data[y, x]) or not mask[y, x]
                or alpha[y, x] == nodata or not np.isfinite(alpha[y, x]) or alpha[y, x] <= 0): return None, 'nodata'
        total += Fraction(float(data[y, x]))*weight
    return float(total), 'ok'


@pytest.mark.parametrize('method', ['cell', 'bilinear'])
@pytest.mark.parametrize('unit,name', [(1., 'metre'), (.3048, 'foot'), (1200/3937, 'US survey foot')])
@pytest.mark.parametrize('grid', [(0.,1.,0.,8.,0.,-1.), (10.,2.,.5,30.,.25,-1.5), (10.,-1.,0.,0.,0.,1.)])
def test_sample_lines_complete_independent_chainage_point_support_attributes_and_units(tmp_path, method, unit, name, grid):
    path, crs, values, mask, alpha, nodata = _source(tmp_path/'scene.tif', grid=grid, unit=unit, unit_name=name, coverage=True)
    routes = [[(.5,.5),(2.5,.5),(2.5,.5),(2.5,3.5)], [(-1.,1.5),(1.5,1.5),(5.5,5.5)],
              [(1.,1.),(2.,1.),(2.,2.),(1.,1.)], [(4.5,4.5),(4.75,4.5)]]
    lines = [LineString([_world(grid, v) for v in points]) for points in routes]; source = _frame(lines, crs); before = source.copy(); raw = path.read_bytes()
    factor = source.crs.axis_info[0].unit_conversion_factor; spacing = float(Fraction(3, 4)*Fraction(factor)); expected = []
    for i, line in enumerate(lines):
        for distance, xy in _chain_oracle(line, spacing, factor): expected.append((i, distance, xy, *_value_oracle(xy, grid, values, mask, alpha, nodata, method)))
    with c.raster_workspace(): result = c.raster_sample_lines(c.raster_load(_plan(path)), source, 2, 'line_id', spacing, method)
    assert result.columns.tolist() == [*source.columns, 'distance_m', 'sample_value', 'sample_status']
    assert result.geometry.name == 'trace' and result.crs == source.crs and result.index.equals(pd.RangeIndex(len(expected)))
    assert str(result.distance_m.dtype) == 'Float64' and str(result.sample_value.dtype) == 'Float64' and str(result.sample_status.dtype) == 'string'
    assert result.distance_m.notna().all()
    for index, (parent, distance, xy, value, status) in enumerate(expected):
        assert result.line_id.iloc[index] == source.line_id.iloc[parent] and result.distance_m.iloc[index] == distance
        assert result.geometry.iloc[index].coords[0] == xy and result.sample_status.iloc[index] == status
        assert pd.isna(result.sample_value.iloc[index]) if value is None else result.sample_value.iloc[index] == value
        for field in ('nullable', 'enabled', 'geometry'):
            left, right = result[field].iloc[index], source[field].iloc[parent]
            assert pd.isna(left) if pd.isna(right) else left == right
        assert result.geometry.iloc[index].geom_type == 'Point'
    pd.testing.assert_frame_equal(source, before); assert path.read_bytes() == raw
    for code in source.line_id:
        distances = result.loc[result.line_id == code, 'distance_m'].tolist(); assert distances[0] == 0 and all(a < b for a, b in zip(distances, distances[1:]))
    closed = result[result.line_id == source.line_id.iloc[2]]
    assert closed.geometry.iloc[0].equals_exact(closed.geometry.iloc[-1], 0) and closed.distance_m.iloc[-1] > 0
    assert len(result[result.line_id == source.line_id.iloc[3]]) == 2


@pytest.mark.parametrize('shape', [[(0.,0.), (1.,0.)], [(0.,0.), (1.,0.), (1.,1.)], [(0.,0.), (1.,0.), (1.,0.), (2.,0.)]])
def test_sample_lines_exact_endpoint_no_extra_vertices_and_no_duplicates(shape):
    line = LineString(shape); result = c._line_sample_positions(line, Fraction(1), Fraction(1))
    expected = _chain_oracle(line, 1., 1.)
    assert [(d, p.coords[0]) for d, p in result] == expected
    assert len({d for d, _ in result}) == len(result)


def test_sample_lines_multisegment_small_differences_and_nonaxial_no_premature_loss():
    lines = [LineString([(0.,0.),(3.,4.),(3.+2**-40,4.+2**-40),(7.,7.)]),
             LineString([(2**500, 0.),(2**500+2**450, 2**450),(2**500+2**451, 0.)])]
    for line in lines:
        spacing = 1. if line == lines[0] else math.ldexp(1.,449)
        actual = c._line_sample_positions(line, Fraction(spacing), Fraction(.3048))
        assert [(d, p.coords[0]) for d, p in actual] == _chain_oracle(line, spacing, .3048)


@pytest.mark.parametrize('spacing', [None, True, False, np.bool_(True), 0, -1, float('nan'), float('inf'), '1', [1], {}, 2**53+1])
@pytest.mark.parametrize('language', ['zh-cn','en'])
def test_sample_lines_rejects_bad_spacing_before_open(monkeypatch, spacing, language):
    monkeypatch.setattr(c, '_open', lambda _: pytest.fail('opened before spacing admission'))
    with c.raster_error_language(language), pytest.raises(ValueError) as error: c.raster_sample_lines(None, None, 1, 'id', spacing)
    assert str(error.value) == c._SAMPLE_LINES_MESSAGES['spacing'][language == 'en']


@pytest.mark.parametrize('method', [None, True, 1, '', 'CELL', 'nearest', [], {}])
@pytest.mark.parametrize('language', ['zh-cn','en'])
def test_sample_lines_rejects_bad_method_before_open(monkeypatch, method, language):
    monkeypatch.setattr(c, '_open', lambda _: pytest.fail('opened before method admission'))
    with c.raster_error_language(language), pytest.raises(ValueError) as error: c.raster_sample_lines(None, None, 1, 'id', 1., method)
    assert str(error.value) == c._SAMPLE_LINES_MESSAGES['method'][language == 'en']


@pytest.mark.parametrize('fault', ['not-gdf','no-geometry','point','multi','polygon','none','empty','zero','z','m','nonfinite','columns','id-missing','id-geometry','id-null','id-repeat','id-blank','id-list'])
@pytest.mark.parametrize('language', ['zh-cn','en'])
def test_sample_lines_geometry_identifier_and_schema_rejection_before_open(monkeypatch, fault, language):
    frame = _frame([LineString([(0.,0.),(1.,1.)]), LineString([(0.,1.),(2.,2.)])], 'EPSG:3857'); field = 'line_id'; code = 'geometry'
    geometries = {'point':Point(0,0), 'multi':MultiLineString([[(0,0),(1,1)]]), 'polygon':Polygon([(0,0),(1,0),(0,1)]), 'none':None,
                  'empty':LineString(), 'zero':LineString([(0,0),(0,0)]), 'z':LineString([(0,0,1),(1,1,2)]), 'm':from_wkt('LINESTRING M (0 0 1, 1 1 2)'), 'nonfinite':LineString([(0,0),(float('inf'),1)])}
    if fault == 'not-gdf': frame = pd.DataFrame(); code = 'gdf'
    elif fault == 'no-geometry': frame = gpd.GeoDataFrame({'line_id':[1]}); code = 'gdf'
    elif fault in geometries: frame.loc['same', 'trace'] = [geometries[fault], frame.geometry.iloc[1]]
    elif fault == 'columns': frame['distance_m'] = 1.; code = 'columns'
    else:
        code = 'id'
        if fault == 'id-missing': field = 'absent'
        elif fault == 'id-geometry': field = 'trace'
        elif fault == 'id-null': frame['line_id'] = pd.array([1,None], dtype='Int64')
        elif fault == 'id-repeat': frame['line_id'] = [1,1]
        elif fault == 'id-blank': frame['line_id'] = ['one','  ']
        elif fault == 'id-list': frame['line_id'] = [[1],[2]]
    monkeypatch.setattr(c, '_open', lambda _: pytest.fail('opened before vector admission'))
    with c.raster_error_language(language), pytest.raises(ValueError) as error: c.raster_sample_lines(None, frame, 2, field, 1.)
    assert str(error.value) == c._SAMPLE_LINES_MESSAGES[code][language == 'en']


@pytest.mark.parametrize('fault', ['geographic','unknown','missing','mismatch','compound'])
@pytest.mark.parametrize('language', ['zh-cn','en'])
def test_sample_lines_crs_admission_is_bilingual(tmp_path, fault, language):
    path, crs, *_ = _source(tmp_path/'crs.tif', epsg=4326 if fault == 'geographic' else 3857, unit=.7 if fault == 'unknown' else 1., unit_name='unknown' if fault == 'unknown' else 'metre')
    frame = _frame([LineString([(.5,.5),(1.5,1.5)])], crs)
    if fault == 'missing': frame = frame.set_crs(None, allow_override=True)
    if fault == 'mismatch': frame = frame.set_crs('EPSG:32650', allow_override=True)
    if fault == 'compound': frame = frame.set_crs('EPSG:7415', allow_override=True)
    with c.raster_workspace(), c.raster_error_language(language), pytest.raises(ValueError) as error: c.raster_sample_lines(c.raster_load(_plan(path)), frame, 2, 'line_id', 1.)
    assert str(error.value) == c._SAMPLE_LINES_MESSAGES['crs'][language == 'en']


@pytest.mark.parametrize('fault', ['length-overflow','length-underflow','endpoint-alias','sample-index','all-zero'])
@pytest.mark.parametrize('language', ['zh-cn','en'])
def test_sample_lines_numeric_extremes_fail_without_large_allocation(fault, language):
    shape, unit, spacing = {'length-overflow':([(-1e308,0.),(1e308,0.)],1.,1.),
        'length-underflow':([(0.,0.),(np.nextafter(0.,1.),0.)],.3048,1.),
        'endpoint-alias':([(0.,0.),(1.,0.),(1.,2**-54)],1.,1.),
        'sample-index':([(0.,0.),(1.,0.)],1.,np.nextafter(0.,1.)),
        'all-zero':([(0.,0.),(0.,0.)],1.,1.)}[fault]
    with c.raster_error_language(language), pytest.raises(ValueError) as error: c._line_sample_positions(LineString(shape), Fraction(spacing), Fraction(unit))
    assert str(error.value) == c._SAMPLE_LINES_MESSAGES['geometry' if fault == 'all-zero' else 'numeric'][language == 'en']


def test_sample_lines_empty_typed_result_and_original_points_preserved(tmp_path):
    path, crs, *_ = _source(tmp_path/'empty.tif'); empty = _frame([], crs)
    with c.raster_workspace():
        loaded = c.raster_load(_plan(path)); result = c.raster_sample_lines(loaded, empty, 2, 'line_id', 1.)
        points = _frame([Point(.5,7.5)], crs); before = points.copy()
        original = c.raster_sample_points(loaded, points, 2); explicit = c.raster_sample_points(loaded, points, 2, 'cell')
    assert result.empty and result.geometry.name == 'trace' and result.crs == empty.crs
    assert result.columns.tolist() == [*empty.columns,'distance_m','sample_value','sample_status']
    assert str(result.distance_m.dtype) == 'Float64' and str(result.sample_status.dtype) == 'string'
    pd.testing.assert_frame_equal(original, explicit); pd.testing.assert_frame_equal(points, before)


@pytest.mark.parametrize('method', ['cell','bilinear'])
def test_sample_lines_cross_blocks_single_point_sampler_and_bounded_reads(tmp_path, monkeypatch, method):
    y,x = np.indices((515,519)); path,crs,values,mask,alpha,nodata = _source(tmp_path/'blocks.tif',x+2.*y,grid=(0.,1.,0.,0.,0.,1.)); frame = _frame([LineString([(510.5,510.5),(514.5,514.5)])],crs)
    calls=[]; sampler=c.raster_sample_points; blocks=c._blocks; reads=[]
    def delegated(*args, **kwargs): calls.append((args[1].copy(),kwargs)); return sampler(*args,**kwargs)
    def bounded(ds, indices, **kw):
        assert kw['native'] is True and indices == [2,3]
        reads.append(kw['window'])
        for result in blocks(ds,indices,**kw): assert max(result[2][0].shape) <= 512; yield result
    monkeypatch.setattr(c,'raster_sample_points',delegated); monkeypatch.setattr(c,'_blocks',bounded)
    with c.raster_workspace(): result=c.raster_sample_lines(c.raster_load(_plan(path)),frame,2,'line_id',1.,method)
    assert len(calls)==1 and calls[0][1]=={'method':method} and len(reads)==len(set(reads))
    for point,value,status in zip(result.geometry,result.sample_value,result.sample_status):
        expected,tag=_value_oracle(point.coords[0],(0.,1.,0.,0.,0.,1.),values,mask,alpha,nodata,method)
        assert status==tag=='ok' and value==expected


@pytest.mark.parametrize('case', ['success','empty','failure','read','save-existing'])
@pytest.mark.parametrize('language', ['zh-cn','en'])
def test_sample_lines_async_real_gpkg_preview_language_immutability_and_cleanup(tmp_path, monkeypatch, case, language):
    import api_server,workflow_engine
    path,crs,*_ = _source(tmp_path/'scene.tif'); frame=_frame([] if case=='empty' else [LineString([(.5,7.5),(5.5,2.5)])],crs)
    source=tmp_path/'lines.gpkg';frame.to_file(source,engine='pyogrio',driver='GPKG',layer='lines',GEOMETRY_NAME='trace',index=False)
    target=tmp_path/'result.gpkg';old=tmp_path/'old.gpkg';old.write_bytes(b'previous result untouched')
    if case=='save-existing': target.write_bytes(b'existing target untouched')
    before={p:p.read_bytes() for p in (path,source,old)}
    tasks=[{'id':'raster','operator':'raster_load','depends_on':[],'params':{'access_plan':_plan(path)}},
           {'id':'lines','operator':'load','depends_on':[],'params':{'connection_info':{'mount_path':str(tmp_path)},'path':source.name}},
           {'id':'sample','operator':'raster_sample_lines','depends_on':['raster','lines'],'params':{'input_raster':{'$ref':'raster'},'input_gdf':{'$ref':'lines'},'band':2,'id_field':'line_id','spacing_metres':0.125,'method':'wrong' if case=='failure' else 'cell'}},
           {'id':'save','operator':'save','depends_on':['sample'],'params':{'input_df':{'$ref':'sample'},'connection_info':{'mount_path':str(tmp_path)},'path':target.name,'mode':'fail'}}]
    workspaces=[]; original=workflow_engine._execute_operator
    def observe(name,params):
        if name=='raster_sample_lines': workspaces.append(params['input_raster'].workspace)
        return original(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',observe)
    if case=='read':
        def broken(*args,**kwargs): raise RuntimeError('injected line read failure')
        monkeypatch.setattr(c,'_blocks',broken)
    client=api_server.app.test_client();response=client.post('/api/workflow',headers={'Accept-Language':language},json={'workflow_def':{'tasks':tasks},'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}});assert response.status_code==202,response.json
    deadline=time.monotonic()+15
    while time.monotonic()<deadline:
        status=client.get('/api/executions/'+response.json['execution_id'],headers={'Accept-Language':'en' if language=='zh-cn' else 'zh-cn'}).json
        if status['status'] in ('success','failed'): break
        time.sleep(.01)
    assert status['status']==('success' if case in ('success','empty') else 'failed'),status
    assert workspaces and all(not p.exists() for p in workspaces) and {p:p.read_bytes()for p in before}==before
    if case in ('success','empty'):
        from osgeo import ogr
        ds=ogr.Open(str(target));layer=ds.GetLayer(0)
        expected=[] if case=='empty' else _chain_oracle(frame.geometry.iloc[0],.125,1.)
        assert layer.GetFeatureCount()==len(expected)
        fields=[layer.GetLayerDefn().GetFieldDefn(i).GetName() for i in range(layer.GetLayerDefn().GetFieldCount())]
        assert fields==['line_id','nullable','enabled','geometry','distance_m','sample_value','sample_status']
        assert layer.GetGeometryColumn()=='geom'
        assert layer.GetLayerDefn().GetFieldDefn(fields.index('line_id')).GetType()==ogr.OFTInteger64
        assert layer.GetLayerDefn().GetFieldDefn(fields.index('enabled')).GetSubType()==ogr.OFSTBoolean
        assert layer.GetLayerDefn().GetFieldDefn(fields.index('distance_m')).GetType()==ogr.OFTReal
        assert layer.GetSpatialRef().IsSame(osr.SpatialReference(wkt=frame.crs.to_wkt()))
        preview=json.loads(status['all_results']['sample']);assert preview['type']=='FeatureCollection' and len(preview['features'])==len(expected)
        for feature,shown,(distance,xy) in zip(layer,preview['features'],expected):
            geometry=feature.GetGeometryRef();assert (geometry.GetX(),geometry.GetY())==xy
            assert feature.GetField('distance_m')==distance and feature.GetField('line_id')==11
            assert feature.GetField('geometry')=='ordinary geometry attribute'
            assert shown['geometry']['coordinates']==list(xy) and shown['properties']['distance_m']==distance
        feature=geometry=layer=ds=None
        assert target.stat().st_size>0
    elif case=='save-existing': assert target.read_bytes()==b'existing target untouched'
    else:
        cause='injected line read failure' if case=='read' else c._SAMPLE_LINES_MESSAGES['method'][language=='en']
        assert not target.exists() and status['error']=='任务 sample 执行失败: '+cause
        queried=client.get('/api/executions/'+response.json['execution_id'],headers={'Accept-Language':language}).json;assert queried['error']==status['error']
    assert client.post('/api/operators/raster_sample_lines/invoke',json={'params':{}}).status_code==403

@pytest.mark.parametrize('fault', ['zero','high','boolean','string','null','alpha','complex','complex-alpha'])
@pytest.mark.parametrize('language', ['zh-cn','en'])
def test_sample_lines_delegates_exact_selected_band_and_alpha_admission(monkeypatch, fault, language):
    reference=osr.SpatialReference();reference.ImportFromEPSG(3857)
    ds=gdal.GetDriverByName('MEM').Create('',4,4,0)
    for i in (1,2,3):
        ds.AddBand(gdal.GDT_CFloat64 if (fault=='complex' and i==2)or(fault=='complex-alpha'and i==3)else gdal.GDT_Float64)
        ds.GetRasterBand(i).Fill(128 if i==3 else 1)
    ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand);ds.SetProjection(reference.ExportToWkt());ds.SetGeoTransform((0,1,0,4,0,-1))
    frame=_frame([LineString([(.5,3.5),(1.5,2.5)])],reference.ExportToWkt());band={'zero':0,'high':4,'boolean':True,'string':'2','null':None,'alpha':3}.get(fault,2)
    monkeypatch.setattr(c,'_open',lambda _:ds)
    code='complex' if fault.startswith('complex')else'alpha' if fault=='alpha'else'band'
    with c.raster_error_language(language),pytest.raises(ValueError)as error:c.raster_sample_lines(object(),frame,band,'line_id',1.)
    assert str(error.value)==c._SAMPLE_POINTS_MESSAGES[code][language=='en']


@pytest.mark.parametrize('dtype', [np.int64,np.uint64])
@pytest.mark.parametrize('case', ['exact','inexact','nodata'])
@pytest.mark.parametrize('language', ['zh-cn','en'])
def test_sample_lines_native_integer_precision_and_nodata_uses_existing_sampler(tmp_path,dtype,case,language):
    nodata=-2**63 if dtype==np.int64 else 2**64-1
    values=np.full((8,8),2**53+2,dtype=dtype)
    if case=='inexact':values[0,0]=2**53+1
    if case=='nodata':values[0,0]=nodata
    path,crs,*_=_source(tmp_path/'integer.tif',values);frame=_frame([LineString([(.5,7.5),(.75,7.5)])],crs)
    with c.raster_workspace(),c.raster_error_language(language):
        if case=='inexact':
            with pytest.raises(ValueError)as error:c.raster_sample_lines(c.raster_load(_plan(path)),frame,2,'line_id',1.)
            assert str(error.value)==c._SAMPLE_POINTS_MESSAGES['numeric'][language=='en']
        else:
            result=c.raster_sample_lines(c.raster_load(_plan(path)),frame,2,'line_id',1.)
            assert len(result)==2
            if case=='nodata':assert result.sample_value.isna().all()and result.sample_status.tolist()==['nodata','nodata']
            else:assert result.sample_value.tolist()==[float(2**53+2)]*2 and result.sample_status.tolist()==['ok','ok']

@pytest.mark.parametrize('ids,dtype', [(['left','right'],'string'),([2**63+1,2**63+3],'UInt64'),([True,False],'boolean')])
def test_sample_lines_explicit_identifier_scalar_types_preserved_in_memory(tmp_path,ids,dtype):
    path,crs,*_=_source(tmp_path/'ids.tif');frame=_frame([LineString([(.5,7.5),(1.5,7.5)])]*2,crs)
    frame['line_id']=pd.array(ids,dtype=dtype);raw=frame.copy()
    with c.raster_workspace():result=c.raster_sample_lines(c.raster_load(_plan(path)),frame,2,'line_id',2.)
    assert str(result.line_id.dtype)==dtype and result.line_id.tolist()==[ids[0],ids[0],ids[1],ids[1]]
    pd.testing.assert_frame_equal(frame,raw)


@pytest.mark.parametrize('field',['distance_m','sample_value','sample_status'])
@pytest.mark.parametrize('role',['attribute','geometry'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_sample_lines_all_output_fields_conflict_before_open(monkeypatch,field,role,language):
    frame=_frame([LineString([(0,0),(1,1)])],'EPSG:3857')
    if role=='geometry':frame=frame.rename_geometry(field)
    else:frame[field]=None
    monkeypatch.setattr(c,'_open',lambda _:pytest.fail('opened before field collision'))
    with c.raster_error_language(language),pytest.raises(ValueError)as error:c.raster_sample_lines(None,frame,2,'line_id',1.)
    assert str(error.value)==c._SAMPLE_LINES_MESSAGES['columns'][language=='en']


@pytest.mark.parametrize('fault',['missing','singular','nonfinite'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_sample_lines_delegates_grid_admission_without_relaxing_it(monkeypatch,fault,language):
    ds=gdal.GetDriverByName('MEM').Create('',4,4,1,gdal.GDT_Float64);ref=osr.SpatialReference();ref.ImportFromEPSG(3857);ds.SetProjection(ref.ExportToWkt())
    if fault!='missing':ds.SetGeoTransform((0,1,2,0,2,4)if fault=='singular'else(0,float('nan'),0,0,0,1))
    frame=_frame([LineString([(.5,.5),(1.5,1.5)])],ref.ExportToWkt());monkeypatch.setattr(c,'_open',lambda _:ds)
    with c.raster_error_language(language),pytest.raises(ValueError)as error:c.raster_sample_lines(object(),frame,1,'line_id',1.)
    assert str(error.value)==c._SAMPLE_POINTS_MESSAGES['grid'][language=='en']


@pytest.mark.parametrize('missing',[False,True])
def test_sample_lines_multiple_native_alpha_nodata_keeps_adjacent_large_coverage(monkeypatch,missing):
    ds=gdal.GetDriverByName('MEM').Create('',4,4,4,gdal.GDT_UInt64);ref=osr.SpatialReference();ref.ImportFromEPSG(3857);ds.SetProjection(ref.ExportToWkt());ds.SetGeoTransform((0,1,0,4,0,-1))
    for i in range(1,5):ds.GetRasterBand(i).Fill(8 if i<3 else 128)
    for i in (3,4):ds.GetRasterBand(i).SetColorInterpretation(gdal.GCI_AlphaBand)
    nodata=2**64-1;alpha=np.full((4,4),nodata-1,dtype=np.uint64)
    if missing:alpha[0,0]=nodata
    ds.GetRasterBand(4).WriteArray(alpha);ds.GetRasterBand(4).SetNoDataValueAsUInt64(nodata)
    frame=_frame([LineString([(.5,3.5),(.75,3.5)])],ref.ExportToWkt());monkeypatch.setattr(c,'_open',lambda _:ds)
    result=c.raster_sample_lines(object(),frame,2,'line_id',1.)
    assert result.sample_status.tolist()==(['nodata']if missing else['ok'])*2
    assert result.sample_value.isna().all()if missing else result.sample_value.tolist()==[8.,8.]


@pytest.mark.parametrize('field',['fid','FID','FiD'])
@pytest.mark.parametrize('method',['cell','bilinear'])
def test_sample_lines_repeated_business_fid_real_save_and_source_old_immutability(tmp_path,field,method):
    from operators.io_operators import save,load
    from osgeo import ogr
    source,crs,*_=_source(tmp_path/'source.tif');frame=_frame([LineString([(.5,7.5),(3.5,4.5)])],crs)
    frame[field]=pd.array([2**53+1],dtype='Int64');original=frame.copy(deep=True);source_bytes=source.read_bytes()
    with c.raster_workspace():got=c.raster_sample_lines(c.raster_load(_plan(source)),frame,2,'line_id',1.,method)
    assert len(got)>2 and got[field].tolist()==[2**53+1]*len(got)
    old=tmp_path/'previous.gpkg';target=tmp_path/'samples.gpkg'
    save(got,connection_info={'mount_path':str(tmp_path)},path=old.name,mode='fail');old_bytes=old.read_bytes()
    save(got,connection_info={'mount_path':str(tmp_path)},path=target.name,mode='fail')
    ds=ogr.Open(str(target));layer=ds.GetLayer(0);features=list(layer)
    assert len(features)==len(got)and len({f.GetFID()for f in features})==len(got)
    assert layer.GetFIDColumn().casefold()not in {str(n).casefold()for n in got.columns}
    assert [f.GetField(field)for f in features]==[2**53+1]*len(got)
    loaded=load(connection_info={'mount_path':str(tmp_path)},path=target.name)
    for name in got.columns:
        if name!=got.geometry.name:
            expected=got[name].reset_index(drop=True)
            # The existing Arrow reader exposes strings as object, integers and
            # booleans as their declared nullable types. Check physical schema.
            field_schema=layer.GetLayerDefn().GetFieldDefn(layer.GetLayerDefn().GetFieldIndex(name))
            if pd.api.types.is_bool_dtype(expected.dtype):
                assert field_schema.GetType()==ogr.OFTInteger and field_schema.GetSubType()==ogr.OFSTBoolean
                expected=expected.astype('boolean')
            elif pd.api.types.is_integer_dtype(expected.dtype):
                assert field_schema.GetType()==ogr.OFTInteger64
                expected=expected.astype('Int64')
            elif pd.api.types.is_float_dtype(expected.dtype):
                assert field_schema.GetType()==ogr.OFTReal
                expected=expected.astype('float64')
            else:
                assert field_schema.GetType()==ogr.OFTString
                expected=expected.astype(object)
            pd.testing.assert_series_equal(loaded[name],expected,check_exact=True)
    assert loaded.geometry.to_wkb().tolist()==got.geometry.to_wkb().tolist()and loaded.crs==got.crs
    assert source.read_bytes()==source_bytes and old.read_bytes()==old_bytes
    pd.testing.assert_frame_equal(frame,original,check_exact=True)
    assert {p.name for p in tmp_path.iterdir()}=={source.name,target.name,old.name}
