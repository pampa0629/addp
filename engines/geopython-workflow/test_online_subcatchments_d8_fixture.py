"""Real GDAL regression for the independent subcatchments_d8 Hosted fixture."""
import importlib
import json
from pathlib import Path
import numpy as np
from osgeo import gdal
import pytest

@pytest.fixture(scope='module')
def subcatchments_d8_persistence(tmp_path_factory):
    module=importlib.import_module('business.scripts.online-raster-subcatchments-d8-fixture')
    scene=importlib.import_module('scripts.test.raster-subcatchments-d8-persistence-online')
    from workflow_engine import execute_workflow
    root=tmp_path_factory.mktemp('subcatchments_d8-export');(root/'outputs').mkdir()
    # The real NFS lifecycle always creates this ownership marker.
    (root/'.addp-online-owner').write_text('subcatchments-d8-123-1')
    initial=module.seed(root)
    for name in module.TARGETS:
        definition=scene.definition({n:n for n in module.SOURCES},'parent',name)
        for task in definition['tasks']:
            params=task['params']
            if task['operator']=='raster_load':
                path=root/params.pop('locator')
                params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','source':{
                    'kind':'file','format':'tiff','access':{'method':'mounted_path','path':str(path)}}}
            elif task['operator']=='raster_save':
                params.pop('target_parent_locator');target=root/'outputs'/params.pop('target_name')
                mode=params.pop('write_mode')
                params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','target':{
                    'kind':'file','format':'tiff','name':target.name,'write_mode':mode,
                    'access':{'method':'mounted_path','path':str(target)}}}
        result=execute_workflow(definition)
        assert result['status']=='success',result
        artifact=json.loads(result['final_result'])
        assert type(artifact['size_bytes']) is int and artifact['size_bytes']==target.stat().st_size>0
    return module,root,initial



def test_subcatchments_d8_persistence_real_gdal_seven_cog_chains_and_complete_pixels(subcatchments_d8_persistence):
    module,root,initial=subcatchments_d8_persistence;actual=module.verify(root,list(module.TARGETS))
    assert actual['sources']==initial['sources'] and len(actual['targets'])==7 and actual['nonnested_reference_equal'] is True
    for name,fact in actual['targets'].items():
        assert all(fact[key] for key in ('full_pixel_and_mask_verification','cog_valid','has_overviews','subcatchments_verified','point_ids_verified','point_self_verified','unselected_zero_verified','first_downstream_point_verified','disjoint_verified','partition_conservation_verified','nested_supported_verified','unit_cleared_verified'))
        assert fact['valid_pixels']>0 and fact['zero_pixels']>0 and fact['unit']==''
        assert sum(fact['subcatchments_pixel_counts'])+fact['zero_pixels']==fact['valid_pixels']
        assert fact['size_bytes']==(root/'outputs'/name).stat().st_size>0
    assert actual['targets']['subcatchments.cog.tif']['valid_pixels']==3628
    assert actual['targets']['dem-subcatchments.cog.tif']['valid_pixels']==3633
    assert actual['targets']['subcatchments.cog.tif']['point_ids']==[5,7,91,2**53-1]
    assert actual['targets']['reclass.cog.tif']['point_ids']==[55,70,90,830,901,2**53-1]


@pytest.mark.parametrize('corruption',['value','one_unit','fraction','seam','invalid_center','mask','crs','grid','dtype','nodata','scale','unit','overviews','overview_sum','sidecar','source_sidecar','missing_owner','missing','extra'])
def test_subcatchments_d8_persistence_oracle_rejects_corruption(subcatchments_d8_persistence,tmp_path,corruption):
    import shutil
    module,original,_=subcatchments_d8_persistence
    root=tmp_path/'changed';shutil.copytree(original,root);path=root/'outputs'/'subcatchments.cog.tif'
    if corruption in ('value','one_unit','fraction','seam','invalid_center','mask','crs','grid','nodata','scale','unit','overview_sum'):
        ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES'])
        band=ds.GetRasterBand(1)
        if corruption in ('value','one_unit','fraction','seam','invalid_center'):
            y,x=(100,100) if corruption=='invalid_center' else (511,512) if corruption=='seam' else (511,518)
            value=module.expected_pixels('subcatchments.cog.tif')[y,x]+(1e-6 if corruption=='fraction' else 1) if corruption in ('fraction','one_unit') else 12345.
            band.WriteRaster(x,y,1,1,np.array([value],np.float64).tobytes(),buf_type=gdal.GDT_Float64)
        elif corruption=='mask':
            with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
            band.GetMaskBand().Fill(255)
        elif corruption=='crs':ds.SetProjection('')
        elif corruption=='grid':ds.SetGeoTransform((1,1,0,1,0,-1))
        elif corruption=='nodata':band.SetNoDataValue(-999)
        elif corruption=='unit':band.SetUnitType('metre')
        elif corruption=='overview_sum':band.GetOverview(0).Fill(12345.125)
        else:band.SetScale(2.)
        band=ds=None
    elif corruption in ('dtype','overviews'):
        alternate=root/'replacement.tif';ds=gdal.Translate(str(alternate),str(path),format='GTiff',outputType=gdal.GDT_Float32 if corruption=='dtype' else gdal.GDT_Float64);ds=None;alternate.replace(path)
    elif corruption=='sidecar':(root/'outputs'/'subcatchments.cog.tif.aux.xml').write_text('unexpected')
    elif corruption=='source_sidecar':(root/'directions.tif.aux.xml').write_text('unexpected')
    elif corruption=='missing_owner':(root/'.addp-online-owner').unlink()
    elif corruption=='missing':path.unlink()
    else:(root/'outputs'/'extra.tif').write_bytes(path.read_bytes())
    with pytest.raises(module.FixtureError):module.verify(root,list(module.TARGETS))


@pytest.mark.parametrize('problem',['duplicate','empty','all_invalid','points_values','coverage','alignment','code','terminal','outside','invalid_target','cycle','band','points_band','rotation','full_nested'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_subcatchments_d8_persistence_invalid_parameters_fail_without_publication(subcatchments_d8_persistence,tmp_path,problem,language):
    import shutil
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime.errors import WorkflowExecutionError
    module,original,initial=subcatchments_d8_persistence;root=tmp_path/'failed';shutil.copytree(original,root)
    scene=importlib.import_module('scripts.test.raster-subcatchments-d8-persistence-online')
    definition=scene.definition({n:n for n in module.SOURCES},'parent','subcatchments.cog.tif',invalid=problem,language=language)
    for task in definition['tasks']:
        params=task['params']
        if task['operator']=='raster_load':
            source=root/params.pop('locator');params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','source':{'kind':'file','format':'tiff','access':{'method':'mounted_path','path':str(source)}}}
        elif task['operator']=='raster_save':
            params.pop('target_parent_locator');target=root/'outputs'/params.pop('target_name');mode=params.pop('write_mode')
            params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','target':{'kind':'file','format':'tiff','name':target.name,'write_mode':mode,'access':{'method':'mounted_path','path':str(target)}}}
    with pytest.raises(WorkflowExecutionError) as caught:GeoPythonWorkflowRunner(language=language).execute(definition)
    assert caught.value.error_code=='EXECUTION_FAILED'
    assert str(caught.value)=='任务 '+scene.failure_task(problem)+' 执行失败: '+scene.MESSAGES[problem][language=='en']
    assert not target.exists() and module.verify(root,list(module.TARGETS))['sources']==initial['sources']


@pytest.mark.parametrize('fault',['renumber','exclude_point','unselected_label','zero_nodata','invalid_zero','fraction','missing_tributary'])
def test_subcatchments_d8_independent_walk_oracle_rejects_wrong_ids(fault):
    module=importlib.import_module('business.scripts.online-raster-subcatchments-d8-fixture')
    actual=module.expected_pixels('subcatchments.cog.tif').copy()
    if fault=='renumber':actual[actual==91]=1
    elif fault=='exclude_point':actual[510,512]=0
    elif fault=='unselected_label':actual[511,518]=91
    elif fault=='zero_nodata':actual[508,0]=np.nan
    elif fault=='invalid_zero':actual[100,100]=0
    elif fault=='missing_tributary':actual[509,511]=0
    else:actual[511,518]=1e-6
    with pytest.raises(module.FixtureError):module.subcatchments_evidence(actual,'subcatchments.cog.tif')


def test_subcatchments_d8_fixture_nonterminal_points_shared_outlet_zero_and_max_id():
    module=importlib.import_module('business.scripts.online-raster-subcatchments-d8-fixture')
    actual=module.expected_pixels('subcatchments.cog.tif')
    np.testing.assert_array_equal(actual[508:513,50],np.full(5,float(2**53-1)))
    assert actual[513,50]==actual[514,50]==actual[511,518]==0
    assert actual[509,511]==actual[510,512]==91 and actual[513,511]==actual[512,512]==7
    assert module.expected_directions('subcatchments.cog.tif')[510,512]!=0


def test_subcatchments_d8_fixture_validity_zero_alpha_cross_block_and_bands(subcatchments_d8_persistence):
    module,root,_=subcatchments_d8_persistence
    for name in ('directions.tif','points.tif'):
        ds=gdal.Open(str(root/name));assert ds.RasterCount==4
        assert np.all(ds.GetRasterBand(1).ReadAsArray()==.25) and np.all(ds.GetRasterBand(3).ReadAsArray()==-9999)
        assert ds.GetRasterBand(4).ReadAsArray()[511,515]==128
        assert ds.GetRasterBand(1).GetMaskBand().ReadAsArray()[514,510]==0;ds=None
    assert module.expected_points('subcatchments.cog.tif')[508,0]==module.expected_pixels('subcatchments.cog.tif')[508,0]==0
    with pytest.raises(module.FixtureError):module.seed(root)


def test_subcatchments_d8_fixture_source_receipt_rejects_byte_change(subcatchments_d8_persistence,tmp_path,monkeypatch):
    import shutil
    module,original,initial=subcatchments_d8_persistence;root=tmp_path/'sources';shutil.copytree(original,root);secret=tmp_path/'secret';secret.mkdir()
    (secret/'subcatchments-d8-source.json').write_text(json.dumps(initial));monkeypatch.setattr(module,'guard',lambda:secret);monkeypatch.setattr(module,'owned_root',lambda _:root)
    monkeypatch.setattr(module,'runtime_worker',lambda action,owned,names:module.verify(owned,names));monkeypatch.setattr(module.sys,'argv',['fixture','verify',*module.TARGETS])
    assert module.main('verify')['sources']==initial['sources']
    with (root/'points.tif').open('ab') as stream:stream.write(b'changed source bytes')
    with pytest.raises(module.FixtureError,match='source file changed'):module.main('verify')


@pytest.mark.parametrize('name',['code.tif','invalid-points.tif'])
def test_subcatchments_d8_fixture_preserves_uint64_nodata_adjacent_illegal_value(subcatchments_d8_persistence,name):
    module,root,_=subcatchments_d8_persistence;ds=gdal.Open(str(root/name));band=ds.GetRasterBand(2)
    assert band.DataType==gdal.GDT_UInt64 and band.GetNoDataValueAsUInt64()==2**53
    assert int(band.ReadAsArray()[509,100])==2**53+1 and int(band.ReadAsArray()[0,0])==2**53;band=ds=None


def test_subcatchments_d8_fixture_fresh_physical_checks(subcatchments_d8_persistence):
    module,root,initial=subcatchments_d8_persistence
    for method in (module.expected_directions,module.expected_points,module.expected_pixels):method.cache_clear()
    result=module.verify(root,list(module.TARGETS))
    assert result==module.verify(root,list(module.TARGETS)) and result['sources']==initial['sources']


def test_subcatchments_d8_fixture_nested_levels_parallel_branches_and_first_point():
    module=importlib.import_module('business.scripts.online-raster-subcatchments-d8-fixture')
    points=module.expected_points('nested.cog.tif');actual=module.expected_pixels('nested.cog.tif')
    for row,col,label in ((510,512,91),(512,512,7),(511,516,83),(511,518,9)):
        assert points[row,col]==actual[row,col]==label
    np.testing.assert_array_equal(actual[511,513:517],np.full(4,83.));assert actual[511,517]==9
    assert module.subcatchments_evidence(actual,'nested.cog.tif')['nested_point_routes']>=3
    with pytest.raises(module.FixtureError,match='nested'):
        module.walk_oracle(module.expected_directions('nested.cog.tif'),points,full=True)


@pytest.mark.parametrize('corruption',['whole_downstream_region','point_renumber','point_excluded','branch_missing','downstream_segment'])
def test_subcatchments_d8_nested_oracle_rejects_full_basin_or_later_point(corruption):
    module=importlib.import_module('business.scripts.online-raster-subcatchments-d8-fixture')
    actual=module.expected_pixels('nested.cog.tif').copy()
    if corruption=='whole_downstream_region':actual[np.isin(actual,[91,7,83])]=9
    elif corruption=='point_renumber':actual[actual==83]=1
    elif corruption=='point_excluded':actual[511,516]=0
    elif corruption=='branch_missing':actual[513,511]=0
    else:actual[511,515]=9
    with pytest.raises(module.FixtureError):module.subcatchments_evidence(actual,'nested.cog.tif')


@pytest.mark.parametrize('fault',['code','cycle'])
def test_subcatchments_d8_oracle_validates_route_after_first_selected_point(fault):
    module=importlib.import_module('business.scripts.online-raster-subcatchments-d8-fixture')
    directions=module.expected_directions('nested.cog.tif').copy()
    directions[511,518]=16 if fault=='cycle' else 3
    with pytest.raises(module.FixtureError):module.walk_oracle(directions,module.expected_points('nested.cog.tif'))


def test_subcatchments_d8_snapping_really_moves_points_and_preserves_nested_ids(subcatchments_d8_persistence):
    module,root,_=subcatchments_d8_persistence
    points=module.expected_points('snapped.cog.tif')
    assert points[509,50]==2**53-1 and points[512,50]==83 and points[509,60]==7
    fact=module.verify(root,list(module.TARGETS))['targets']['snapped.cog.tif']
    assert fact['snapped_points_moved']==3 and fact['nested_point_routes']>=1


def test_subcatchments_d8_stdin_worker_is_self_contained_in_empty_directory(subcatchments_d8_persistence,tmp_path):
    import os,subprocess,sys
    module,root,initial=subcatchments_d8_persistence
    empty=tmp_path/'empty';empty.mkdir();scratch=tmp_path/'private-tmp';scratch.mkdir()
    env=dict(os.environ,PYTHONPATH='',TMPDIR=str(scratch));script=Path(module.__file__).read_text()
    fresh=tmp_path/'fresh';fresh.mkdir();(fresh/'outputs').mkdir();(fresh/'.addp-online-owner').write_text('subcatchments-d8-123-1')
    seeded=subprocess.run([sys.executable,'-','--worker','seed',str(fresh)],input=script,cwd=empty,env=env,text=True,capture_output=True,timeout=60)
    assert seeded.returncode==0,seeded.stderr
    assert len(json.loads(seeded.stdout)['sources'])==len(module.SOURCES)
    verified=subprocess.run([sys.executable,'-','--worker','verify',str(root),*module.TARGETS],input=script,cwd=empty,env=env,text=True,capture_output=True,timeout=60)
    assert verified.returncode==0,verified.stderr
    result=json.loads(verified.stdout);assert result['nonnested_reference_equal'] and result['sources']==initial['sources']
    assert len(result['targets'])==7 and result['runtime_workspaces_removed'] is True
