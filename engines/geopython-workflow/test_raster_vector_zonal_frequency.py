"""Independent full categorical polygon populations and real persistence regression."""
from fractions import Fraction
import hashlib
import json
from pathlib import Path
import numpy as np
import pandas as pd
import geopandas as gpd
import pytest
from osgeo import gdal, osr
from shapely.geometry import Point, Polygon, MultiPolygon, box
import operators.raster_compute as c
from workflow_engine import GeoPythonWorkflowRunner
from addp_common.workflow_runtime.errors import WorkflowExecutionError

COLUMNS = ['class_value','count','proportion','total_count','valid_count','invalid_count']
GRID = (100.,2.,0.,200.,0.,-3.)

def create(path, data, *, grid=GRID, crs=3857, dtype=gdal.GDT_Float64, nodata=-9999, alphas=(), mask=None):
    data=np.asarray(data)
    if data.ndim==2:data=data[None]
    ds=gdal.GetDriverByName('GTiff').Create(str(path),data.shape[2],data.shape[1],len(data),dtype)
    if grid is not None:ds.SetGeoTransform(grid)
    if crs:
        s=osr.SpatialReference();s.ImportFromEPSG(crs);ds.SetSpatialRef(s)
    for i,a in enumerate(data,1):
        b=ds.GetRasterBand(i);b.WriteArray(a)
        if nodata is not None:
            (b.SetNoDataValueAsInt64 if dtype==gdal.GDT_Int64 else b.SetNoDataValueAsUInt64 if dtype==gdal.GDT_UInt64 else b.SetNoDataValue)(nodata)
        b.SetScale(17);b.SetOffset(500)
        if i in alphas:b.SetColorInterpretation(gdal.GCI_AlphaBand)
    if mask is not None:
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
        ds.GetRasterBand(1).GetMaskBand().WriteArray(mask.astype(np.uint8)*255)
    b=ds=None
    return path

def plan(path):return {'schema_version':'addp.workflow.access-plan/v1','source':{'kind':'file','format':'tiff','access':{'method':'mounted_path','path':str(path)}}}
def world(grid,x,y):return grid[0]+x*grid[1]+y*grid[2],grid[3]+x*grid[4]+y*grid[5]
def polygon(grid,x0,y0,x1,y1):return Polygon([world(grid,x0,y0),world(grid,x1,y0),world(grid,x1,y1),world(grid,x0,y1)])
def zones(shapes, *, crs=3857):
    return gpd.GeoDataFrame({'zone':pd.array(['z'+str(i)for i in range(len(shapes))],dtype='string'),'n':pd.array([None if i%2 else i for i in range(len(shapes))],dtype='Int64'),'flag':pd.array([True if i%2 else None for i in range(len(shapes))],dtype='boolean'),'geometry':['original attribute']*len(shapes),'shape':shapes},geometry='shape',crs=crs)

def result(path,gdf,*,band=1,**kwargs):
    with c.raster_workspace():return c.raster_vector_zonal_statistics(c.raster_load(plan(path)),gdf,band,method='class_frequency',id_field='zone',**kwargs)

def oracle(data,gdf,grid,valid,all_touched=False):
    rows=[];positions=[]
    for position,geometry in enumerate(gdf.geometry):
        total=0;counts={}
        for y in range(data.shape[0]):
            for x in range(data.shape[1]):
                covered=(geometry.intersection(polygon(grid,x,y,x+1,y+1)).area>0 if all_touched else geometry.contains(Point(world(grid,x+.5,y+.5))))
                if not covered:continue
                total+=1
                if valid[y,x]:
                    code=int(data[y,x]);counts[code]=counts.get(code,0)+1
        n=sum(counts.values())
        for code in sorted(counts)if counts else [None]:
            count=counts[code]if code is not None else 0
            rows.append([code,count,float(Fraction(count,n))if n else None,total,n,total-n]);positions.append(position)
    expected=gdf.iloc[positions].copy()
    for i,key in enumerate(COLUMNS):expected['zonal_'+key]=pd.array([row[i]for row in rows],dtype='Int64'if key=='class_value'else'Float64'if key=='proportion'else'int64')
    return expected

@pytest.mark.parametrize('touched',[False,True])
@pytest.mark.parametrize('grid,crs',[(GRID,3857),((100.,2.,.4,200.,.2,-3.),3857),((110.,.01,0.,30.,0.,-.02),4326),((100.,-2.,0.,200.,0.,3.),3857)])
def test_vector_zonal_frequency_complete_independent_overlap_holes_diagnostics_and_types(tmp_path,touched,grid,crs):
    a=np.array([[0,-3,2,2,11],[2,-9999,0,11,2],[-3,2,11,0,2],[2,0,-3,11,2]],float);valid=a!=-9999
    outside=polygon(grid,20,20,21,21);shell=polygon(grid,.1,.1,4.8,3.9);hole=polygon(grid,1.1,1.1,2.8,2.8)
    gdf=zones([Polygon(shell.exterior.coords,[hole.exterior.coords]),polygon(grid,.2,.3,2.2,3.7),MultiPolygon([polygon(grid,.1,.1,.9,.9),polygon(grid,3.1,2.1,4.9,3.9)]),outside],crs=crs)
    gdf.index=pd.Index([8,8,4,8],name='original');before=gdf.copy(deep=True);path=create(tmp_path/'v.tif',a,grid=grid,crs=crs);raw=path.read_bytes()
    actual=result(path,gdf,all_touched=touched);expected=oracle(a,gdf,grid,valid,touched)
    pd.testing.assert_frame_equal(actual,expected,check_exact=True);pd.testing.assert_frame_equal(gdf,before,check_exact=True)
    assert path.read_bytes()==raw and actual.geometry.name=='shape'
    assert actual.iloc[-1]['zonal_count']==0 and pd.isna(actual.iloc[-1]['zonal_class_value'])

@pytest.mark.parametrize('case',['invalid','empty'])
def test_vector_zonal_frequency_all_invalid_and_empty_schema(tmp_path,case):
    a=np.full((3,4),-9999.);path=create(tmp_path/'v.tif',a);gdf=zones([polygon(GRID,.1,.1,3.9,2.9)])
    if case=='empty':gdf=gdf.iloc[:0].copy()
    got=result(path,gdf);pd.testing.assert_frame_equal(got,oracle(a,gdf,GRID,np.zeros(a.shape,bool)),check_exact=True)
    assert list(got.columns)==[*gdf.columns,*['zonal_'+k for k in COLUMNS]]

@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('case',['method','summary_id','percentile','percentile_null','weights','weights_null','weights_band','stddev','area','area_stddev','id_missing','id_null','id_duplicate','id_empty','id_geometry','id_list','field_conflict','geometry_conflict','prefix','coverage'])
def test_vector_zonal_frequency_parameters_and_id_before_io(tmp_path,monkeypatch,language,case):
    gdf=zones([box(100,191,108,200),box(100,191,106,200)]);kwargs={'method':'class_frequency','id_field':'zone'};code='conflict'
    if case=='method':kwargs['method']='bad';code='method'
    elif case=='summary_id':kwargs['method']='summary';code='summary_id'
    elif case=='percentile':kwargs['percentiles']=[50]
    elif case=='percentile_null':kwargs['percentiles']=None
    elif case=='weights':kwargs['weights_raster']=object()
    elif case=='weights_null':kwargs['weights_raster']=None
    elif case=='weights_band':kwargs['weights_band']=1
    elif case=='stddev':kwargs['include_weighted_stddev']=True
    elif case=='area':kwargs['include_area_weighted']=True
    elif case=='area_stddev':kwargs['include_area_weighted_stddev']=True
    elif case.startswith('id_'):
        code='id'
        if case=='id_missing':kwargs.pop('id_field')
        elif case=='id_null':kwargs['id_field']=None
        elif case=='id_duplicate':gdf['zone']=['same','same']
        elif case=='id_empty':gdf['zone']=['','b']
        elif case=='id_geometry':kwargs['id_field']='shape'
        else:gdf['zone']=[[1],[2]]
    elif case=='field_conflict':gdf['zonal_class_value']=1;code='columns'
    elif case=='geometry_conflict':gdf=gdf.rename_geometry('zonal_count');code='columns'
    elif case=='prefix':kwargs['field_prefix']=None;code='prefix'
    elif case=='coverage':kwargs['all_touched']=1;code='coverage'
    monkeypatch.setattr(c,'_open',lambda *args:pytest.fail('rejected before opening'))
    with c.raster_error_language(language),pytest.raises(ValueError)as error:c.raster_vector_zonal_statistics(object(),gdf,1,**kwargs)
    assert str(error.value)==c._VECTOR_FREQUENCY_MESSAGES[code][language=='en']

@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('case',['fractional','unsafe','native-neighbor','band_zero','band_bool','alpha','complex','complex_alpha','missing_crs','different_crs','missing_grid','singular_grid'])
def test_vector_zonal_frequency_exact_admission_bilingual(tmp_path,monkeypatch,language,case):
    dtype=gdal.GDT_UInt64 if case=='native-neighbor'else gdal.GDT_Float64;nd=2**64-1 if dtype==gdal.GDT_UInt64 else -9999
    a=np.ones((2,2),dtype=np.uint64 if dtype==gdal.GDT_UInt64 else float)
    if case=='fractional':a[0,0]=.5
    if case=='unsafe':a[0,0]=2**53
    if case=='native-neighbor':a[0]=[nd,nd-1]
    grid=None if case=='missing_grid'else (100,1,2,200,2,4)if case=='singular_grid'else GRID
    ds=gdal.GetDriverByName('MEM').Create('',2,2,0);ds.AddBand(gdal.GDT_CFloat64 if case=='complex'else dtype);ds.AddBand(gdal.GDT_CFloat64 if case=='complex_alpha'else dtype)
    ds.GetRasterBand(1).WriteArray(a);ds.GetRasterBand(2).Fill(128);ds.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand)
    b=ds.GetRasterBand(1);(b.SetNoDataValueAsUInt64 if dtype==gdal.GDT_UInt64 else b.SetNoDataValue)(nd);b=None
    if grid:ds.SetGeoTransform(grid)
    if case!='missing_crs':s=osr.SpatialReference();s.ImportFromEPSG(4326 if case=='different_crs'else 3857);ds.SetSpatialRef(s)
    monkeypatch.setattr(c,'_open',lambda _:ds);gdf=zones([box(99,190,110,201)])
    band=0 if case=='band_zero'else True if case=='band_bool'else 2 if case=='alpha'else 1
    code='classes'if case in('fractional','unsafe','native-neighbor')else'band'if case.startswith('band_')else'alpha'if case=='alpha'else'complex'if case.startswith('complex')else'crs'if case.endswith('crs')else'grid'
    with c.raster_error_language(language),pytest.raises(ValueError)as error:c.raster_vector_zonal_statistics(object(),gdf,band,method='class_frequency',id_field='zone')
    assert str(error.value)==c._VECTOR_FREQUENCY_MESSAGES[code][language=='en']

@pytest.mark.parametrize('dtype,nodata',[(gdal.GDT_Int64,-2**63),(gdal.GDT_UInt64,2**64-1)])
def test_vector_zonal_frequency_native_alpha_masks_second_band_and_no_selected_outside_checks(monkeypatch,dtype,nodata):
    ds=gdal.GetDriverByName('MEM').Create('',4,3,4,dtype);ds.SetGeoTransform((0,1,0,3,0,-1));ref=osr.SpatialReference();ref.ImportFromEPSG(3857);ds.SetSpatialRef(ref)
    tp=np.int64 if dtype==gdal.GDT_Int64 else np.uint64;values=np.array([[0,2,2,2**53],[nodata,2,0,2**53],[0,2,0,2**53]],dtype=tp)
    for i in range(1,5):ds.GetRasterBand(i).Fill(1)
    ds.GetRasterBand(1).Fill(99);ds.GetRasterBand(2).WriteArray(values);b=ds.GetRasterBand(2);(b.SetNoDataValueAsInt64 if dtype==gdal.GDT_Int64 else b.SetNoDataValueAsUInt64)(nodata);b=None
    alpha=np.full(values.shape,128,dtype=tp);alpha[0,1]=0;alpha[1,1]=nodata;alpha[2,2]=nodata-1 if dtype==gdal.GDT_UInt64 else 1
    for i in(3,4):
        b=ds.GetRasterBand(i);b.SetColorInterpretation(gdal.GCI_AlphaBand);b.WriteArray(alpha if i==4 else np.full(values.shape,128,dtype=tp));(b.SetNoDataValueAsInt64 if dtype==gdal.GDT_Int64 else b.SetNoDataValueAsUInt64)(nodata)
    b=None;monkeypatch.setattr(c,'_open',lambda _:ds);gdf=zones([box(.1,.1,2.9,2.9)]);valid=(values!=nodata)&(alpha>0)&(alpha!=nodata)
    got=c.raster_vector_zonal_statistics(object(),gdf,2,method='class_frequency',id_field='zone');pd.testing.assert_frame_equal(got,oracle(values,gdf,(0,1,0,3,0,-1),valid),check_exact=True)


def test_vector_zonal_frequency_double_axis_blocks_raw_values_and_shared_coverage(tmp_path,monkeypatch):
    y,x=np.indices((515,519));a=((x+y*3)%4-2).astype(float);a[10,10]=-9999;path=create(tmp_path/'cross.tif',np.stack([a+.5,a,np.full(a.shape,128.)]),alphas=(3,));gdf=zones([polygon(GRID,.1,.1,518.9,514.9)])
    original=c._blocks;calls=[]
    def bounded(ds,indices,**kwargs):
        for item in original(ds,indices,**kwargs):calls.append((item[0],item[1],item[2][0].shape));yield item
    monkeypatch.setattr(c,'_blocks',bounded);got=result(path,gdf,band=2);valid=a!=-9999;counts={code:int(((a==code)&valid).sum())for code in(-2,-1,0,1)};assert got['zonal_count'].tolist()==list(counts.values())
    assert max(shape[0]for _,_,shape in calls)<=512 and max(shape[1]for _,_,shape in calls)<=512 and {x for x,_,_ in calls}=={0,512}and {y for _,y,_ in calls}=={0,512}

@pytest.mark.parametrize('options',[{}, {'percentiles':[50]}, {'weights':True}, {'weights':True,'include_weighted_stddev':True}, {'include_area_weighted':True}, {'include_area_weighted':True,'include_area_weighted_stddev':True}])
def test_vector_zonal_frequency_summary_default_remains_exact(tmp_path,options):
    a=np.array([[1,2,4],[3,-9999,0],[2,4,1]],float);p=create(tmp_path/'v.tif',a);w=create(tmp_path/'w.tif',np.ones_like(a));gdf=zones([polygon(GRID,.1,.1,2.9,2.9)]);kwargs=dict(options)
    with c.raster_workspace():
        source=c.raster_load(plan(p))
        if kwargs.pop('weights',False):kwargs.update(weights_raster=c.raster_load(plan(w)),weights_band=1)
        a=c.raster_vector_zonal_statistics(source,gdf,1,**kwargs);b=c.raster_vector_zonal_statistics(source,gdf,1,method='summary',**kwargs)
    pd.testing.assert_frame_equal(a,b,check_exact=True)

@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('total,counts',[(2**63,{}),(1,{0:2}),(1,{0:0})])
def test_vector_zonal_frequency_count_boundaries_fail_exactly(language,total,counts):
    with c.raster_error_language(language),pytest.raises(ValueError)as error:c._class_frequency_rows(total,counts,allow_empty=True,error=c._vector_frequency_error)
    assert str(error.value)==c._VECTOR_FREQUENCY_MESSAGES['numeric'][language=='en']

@pytest.mark.parametrize('failure',[False,True])
def test_vector_zonal_frequency_real_geopackage_async_and_cleanup(tmp_path,monkeypatch,failure):
    import workflow_engine
    a=np.arange(12,dtype=float).reshape(3,4)%3;p=create(tmp_path/'v.tif',a);gdf=zones([polygon(GRID,.1,.1,3.9,2.9)]);source=tmp_path/'zones.gpkg';gdf.to_file(source,driver='GPKG',index=False);before={p:p.read_bytes(),source:source.read_bytes()};target=tmp_path/'out.gpkg';old=tmp_path/'old.gpkg';old.write_bytes(b'prior result')
    tasks=[{'id':'r','operator':'raster_load','depends_on':[],'params':{'access_plan':plan(p)}},{'id':'z','operator':'load','depends_on':[],'params':{'connection_info':{'mount_path':str(tmp_path)},'path':source.name}}, {'id':'analysis','operator':'raster_vector_zonal_statistics','depends_on':['r','z'],'params':{'input_raster':{'$ref':'r'},'input_gdf':{'$ref':'z'},'band':1,'method':'class_frequency','id_field':'zone'}},{'id':'save','operator':'save','depends_on':['analysis'],'params':{'input_df':{'$ref':'analysis'},'connection_info':{'mount_path':str(tmp_path)},'path':target.name,'mode':'fail'}}]
    work=[];original=workflow_engine._execute_operator
    def tracked(name,params):
        if name=='raster_vector_zonal_statistics':work.append(params['input_raster'].workspace)
        if failure and name=='save':raise OSError('injected save failure')
        return original(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',tracked)
    if failure:
        with pytest.raises(WorkflowExecutionError,match='injected save failure'):GeoPythonWorkflowRunner().execute({'tasks':tasks})
        assert not target.exists()
    else:
        result=GeoPythonWorkflowRunner().execute({'tasks':tasks});assert result.task_order[-1]=='save';expected=oracle(a,gdf,GRID,np.ones(a.shape,bool))
        from osgeo import ogr
        dataset=ogr.Open(str(target));layer=dataset.GetLayer(0);assert layer.GetFeatureCount()==len(expected)
        for feature,(_,wanted) in zip(layer,expected.iterrows()):
            for key in expected.columns:
                if key==expected.geometry.name:continue
                value=feature.GetField(key);target_value=wanted[key]
                assert (value is None and pd.isna(target_value)) or value==target_value
            assert bytes(feature.GetGeometryRef().ExportToWkb())==wanted[expected.geometry.name].wkb
        feature=layer=dataset=None;assert target.stat().st_size>0
    assert work and all(not p.exists()for p in work)and old.read_bytes()==b'prior result'
    assert all(path.read_bytes()==raw for path,raw in before.items())

@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('case',['not-frame','no-active','point','line','empty-geometry','invalid-polygon','z','duplicate-column','second-geometry'])
def test_vector_zonal_frequency_geometry_admission_bilingual_before_io(monkeypatch,language,case):
    from shapely.geometry import LineString
    gdf=zones([box(100,190,110,200)])
    if case=='not-frame':gdf=pd.DataFrame({'zone':[1]})
    elif case=='no-active':gdf=gpd.GeoDataFrame({'zone':[1]})
    elif case=='point':gdf.geometry=[Point(101,199)]
    elif case=='line':gdf.geometry=[LineString([(100,200),(110,190)])]
    elif case=='empty-geometry':gdf.geometry=[Polygon()]
    elif case=='invalid-polygon':gdf.geometry=[Polygon([(0,0),(2,2),(2,0),(0,2),(0,0)])]
    elif case=='z':gdf.geometry=[Polygon([(0,0,1),(2,0,1),(0,2,1),(0,0,1)])]
    elif case=='duplicate-column':gdf.columns=['zone','zone','flag','geometry','shape']
    else:gdf['other']=gpd.GeoSeries([box(0,0,1,1)],crs=gdf.crs)
    monkeypatch.setattr(c,'_open',lambda *args:pytest.fail('no read before geometry admission'))
    with c.raster_error_language(language),pytest.raises(ValueError)as error:c.raster_vector_zonal_statistics(object(),gdf,1,method='class_frequency',id_field='zone')
    assert str(error.value)==c._VECTOR_FREQUENCY_MESSAGES['geometry'][language=='en']

@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('stage',['read','rasterize','open'])
def test_vector_zonal_frequency_partial_failures_preserve_sources_and_prior_results(tmp_path,monkeypatch,language,stage):
    from workflow_engine import execute_workflow
    p=create(tmp_path/'v.tif',np.ones((3,4)));gdf=zones([polygon(GRID,.1,.1,3.9,2.9)]);old=tmp_path/'old.gpkg';old.write_bytes(b'prior')
    before=p.read_bytes();work=[]
    def fail(*args,**kwargs):raise OSError('injected '+stage)
    if stage=='read':monkeypatch.setattr(c,'_blocks',fail)
    elif stage=='rasterize':monkeypatch.setattr(c.gdal,'RasterizeLayer',lambda *args,**kwargs:1)
    else:monkeypatch.setattr(c,'_open',fail)
    with c.raster_workspace(),c.raster_error_language(language):
        work.append(c._WORKSPACE.get())
        if stage=='open':source=object()
        else:source=c.raster_load(plan(p))
        with pytest.raises((OSError,ValueError),match='injected|rasterization'):c.raster_vector_zonal_statistics(source,gdf,1,method='class_frequency',id_field='zone')
    assert all(not path.exists()for path in work)and old.read_bytes()==b'prior'and p.read_bytes()==before

@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('bad',[False,True])
def test_vector_zonal_frequency_async_runtime_geojson_and_accurate_failure(tmp_path,monkeypatch,language,bad):
    import time,api_server,workflow_engine
    p=create(tmp_path/'v.tif',np.array([[0.,1.,2.],[1.,-9999.,0.],[2.,0.,1.]]));gdf=zones([polygon(GRID,.1,.1,2.9,2.9)]);path=tmp_path/'zones.gpkg';gdf.to_file(path,driver='GPKG',index=False)
    target=tmp_path/'result.gpkg';before={p:p.read_bytes(),path:path.read_bytes()};work=[];original=workflow_engine._execute_operator
    def track(name,params):
        if name=='raster_vector_zonal_statistics':work.append(params['input_raster'].workspace)
        return original(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',track)
    tasks=[{'id':'r','operator':'raster_load','depends_on':[],'params':{'access_plan':plan(p)}},{'id':'z','operator':'load','depends_on':[],'params':{'connection_info':{'mount_path':str(tmp_path)},'path':path.name}},{'id':'analysis','operator':'raster_vector_zonal_statistics','depends_on':['r','z'],'params':{'input_raster':{'$ref':'r'},'input_gdf':{'$ref':'z'},'band':1,'method':'bad'if bad else'class_frequency','id_field':'zone'}},{'id':'save','operator':'save','depends_on':['analysis'],'params':{'input_df':{'$ref':'analysis'},'connection_info':{'mount_path':str(tmp_path)},'path':target.name,'mode':'fail'}}]
    client=api_server.app.test_client();response=client.post('/api/workflow',headers={'Accept-Language':language},json={'workflow_def':{'tasks':tasks},'input_data':{},'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}});assert response.status_code==202
    identifier=response.json['execution_id'];deadline=time.monotonic()+15
    while time.monotonic()<deadline:
        status=client.get('/api/executions/'+identifier,headers={'Accept-Language':'en'if language=='zh-cn'else'zh-cn'}).json
        if status['status']in('success','failed'):break
        time.sleep(.01)
    assert status['status']==('failed'if bad else'success'),status
    if bad:
        assert not target.exists()and status['error']=='任务 analysis 执行失败: '+c._VECTOR_FREQUENCY_MESSAGES['method'][language=='en']
    else:
        value=json.loads(status['all_results']['analysis']);assert value['type']=='FeatureCollection'and len(value['features'])==3
        assert [f['properties']['zonal_class_value']for f in value['features']]==[0,1,2]and target.stat().st_size>0
    assert work and all(not w.exists()for w in work)and all(path.read_bytes()==raw for path,raw in before.items())
    assert client.post('/api/operators/raster_vector_zonal_statistics/invoke',json={'params':{}}).status_code==403

@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('method',[None,False,1,[],{},'', 'CLASS_FREQUENCY'])
def test_vector_zonal_frequency_method_is_strict_before_io(monkeypatch,language,method):
    monkeypatch.setattr(c,'_open',lambda *args:pytest.fail('no source opened'))
    with c.raster_error_language(language),pytest.raises(ValueError)as error:c.raster_vector_zonal_statistics(object(),object(),1,method=method)
    assert str(error.value)==c._VECTOR_FREQUENCY_MESSAGES['method'][language=='en']

@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('field',['include_weighted_stddev','include_area_weighted','include_area_weighted_stddev'])
@pytest.mark.parametrize('flag',[None,0,1,'false',[],{}])
def test_vector_zonal_frequency_original_strict_flags_remain_before_io(monkeypatch,language,field,flag):
    monkeypatch.setattr(c,'_open',lambda *args:pytest.fail('no source opened'))
    with c.raster_error_language(language),pytest.raises(ValueError)as error:c.raster_vector_zonal_statistics(object(),object(),1,method='class_frequency',**{field:flag})
    assert str(error.value)==field+(' must be a boolean'if language=='en'else'必须是布尔值')

@pytest.mark.parametrize('field',['fid','FID','FiD'])
def test_vector_zonal_frequency_preserves_driver_reserved_business_field_and_identifier_type(tmp_path,field):
    p=create(tmp_path/'v.tif',np.array([[0,1,2],[2,0,1],[1,2,0]],float));gdf=zones([polygon(GRID,.1,.1,2.9,2.9)]);gdf['zone']=pd.array([2**63-1],dtype='Int64');gdf[field]=pd.array([123],dtype='Int64')
    original=gdf.copy(deep=True);got=result(p,gdf)
    assert len(got)==3 and got[field].tolist()==[123]*3 and str(got[field].dtype)=='Int64'and got['zone'].tolist()==[2**63-1]*3 and str(got['zone'].dtype)=='Int64'
    pd.testing.assert_frame_equal(gdf,original,check_exact=True)


@pytest.mark.parametrize('field',['fid','FID','FiD'])
def test_vector_zonal_frequency_repeated_business_fid_real_save_and_source_old_immutability(tmp_path,field):
    from operators.io_operators import save,load
    from osgeo import ogr
    source=create(tmp_path/'source.tif',np.array([[0,1,2],[2,0,1],[1,2,0]],float));gdf=zones([polygon(GRID,.1,.1,2.9,2.9)])
    gdf[field]=pd.array([2**53+1],dtype='Int64');original=gdf.copy(deep=True);source_bytes=source.read_bytes();got=result(source,gdf)
    target=tmp_path/'frequency.gpkg';old=tmp_path/'previous.gpkg'
    save(got,connection_info={'mount_path':str(tmp_path)},path=old.name,mode='fail');old_bytes=old.read_bytes()
    save(got,connection_info={'mount_path':str(tmp_path)},path=target.name,mode='fail')
    ds=ogr.Open(str(target));layer=ds.GetLayer(0);features=list(layer)
    assert layer.GetFeatureCount()==3 and len({f.GetFID()for f in features})==3
    assert layer.GetFIDColumn().casefold()not in {str(n).casefold()for n in got.columns}
    assert [f.GetField(field)for f in features]==[2**53+1]*3
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
    pd.testing.assert_frame_equal(gdf,original,check_exact=True)
    assert {p.name for p in tmp_path.iterdir()}=={source.name,target.name,old.name}
