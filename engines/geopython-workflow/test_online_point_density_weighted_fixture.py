"""Actual native workflow files, independent full oracle and rejection barriers."""
import copy
import hashlib
import importlib
import json
import math
import os
from pathlib import Path
import shutil
import subprocess
import sys
import pytest
sys.path.insert(0,str(Path(__file__).resolve().parents[2]))
from workflow_engine import execute_workflow,GeoPythonWorkflowRunner
from addp_common.workflow_runtime.errors import WorkflowExecutionError
f=importlib.import_module('business.scripts.online-raster-point-density-weighted-fixture')
m=importlib.import_module('scripts.test.raster-point-density-weighted-persistence-online')

def localize(dag,root,target):
 for task in dag['tasks']:
  p=task['params']
  if task['operator']=='raster_load':
   source=root/p.pop('locator');p['access_plan']={'schema_version':'addp.workflow.access-plan/v1','source':{'kind':'file','format':'tiff','access':{'method':'mounted_path','path':str(source)}}}
  elif task['operator']=='load':p['path']=p.pop('locator');p['connection_info']={'mount_path':str(root)}
  elif task['operator']=='raster_save':
   p.pop('target_parent_locator');p.pop('target_name');p.pop('write_mode');p['access_plan']={'schema_version':'addp.workflow.access-plan/v1','target':{'kind':'file','format':'tiff','name':target.name,'write_mode':'create','access':{'method':'mounted_path','path':str(target)}}}

@pytest.fixture(scope='module')
def complete_sources(tmp_path_factory):
 root=tmp_path_factory.mktemp('raster-point-density-weighted');(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('raster-point-density-weighted-123-1')
 initial=f.seed(root);expected=json.loads(f.encoded(f.oracle()));(root/f.ORACLE).write_bytes(f.encoded(expected));previews={}
 for name in f.CASES:
  dag=m.definition({n:n for n in f.SOURCES},'parent',name);localize(dag,root,root/'outputs'/name)
  result=execute_workflow(dag);assert result['status']=='success',result;previews[name]=json.loads(result['final_result'])
  proof=f.verify(root,list(previews),expected);assert proof['sources']==initial['sources']and proof['full_cog_checks']==len(previews)
 return root,initial,expected,previews

def test_twenty_four_complete_cogs_independent_oracle_and_raw_archives(complete_sources,tmp_path):
 root,initial,expected,previews=complete_sources;assert len(expected)==24
 proof=f.verify(root,list(f.CASES),expected);assert proof['full_cog_checks']==24 and proof['sources']==initial['sources']
 archives=f.archive_files(root/'outputs',tmp_path,list(f.CASES),{},f.PREFIX+'-cogs');assert len(archives)==24
 for name in f.CASES:
  m.raster.validate_raster_artifact(previews[name],f.artifact_expectation(name))
  assert (tmp_path/archives[name]['path']).read_bytes()==(root/'outputs'/name).read_bytes()and archives[name]['size_bytes']>0

@pytest.mark.parametrize('problem',f.FAILURES)
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_exact_bilingual_failures_publish_nothing_and_remove_workspaces(complete_sources,tmp_path,monkeypatch,problem,language):
 import workflow_engine
 root,initial,expected,_=complete_sources;name='uniform-mixed.cog.tif';dag=m.definition({n:n for n in f.SOURCES},'parent',name,invalid=problem,language=language);target=tmp_path/'failed.cog.tif';localize(dag,root,target)
 work=[];original=workflow_engine._execute_operator
 def track(operator,params):
  if operator=='raster_point_density':work.append(params['reference_raster'].workspace)
  return original(operator,params)
 monkeypatch.setattr(workflow_engine,'_execute_operator',track)
 with pytest.raises(WorkflowExecutionError)as error:GeoPythonWorkflowRunner(language=language).execute(dag)
 assert error.value.error_code=='EXECUTION_FAILED'and str(error.value)=='任务 density 执行失败: '+m.MESSAGES[problem][language=='en']
 assert work and all(not w.exists()for w in work)and not target.exists()and list(tmp_path.iterdir())==[]
 assert f.verify(root,list(f.CASES),expected)['sources']==initial['sources']

@pytest.mark.parametrize('fault',['value','mask','grid','overview','source-point','source-template','extra-output','type'])
def test_real_physical_corruption_and_source_or_extra_files_are_rejected(complete_sources,tmp_path,fault):
 from osgeo import ogr,gdal
 root,_,expected,_=complete_sources;dest=tmp_path/'mutated';shutil.copytree(root,dest);path=dest/'outputs'/('uniform-rectangular.cog.tif'if fault=='overview'else'uniform-mixed.cog.tif')
 if fault=='extra-output':(dest/'outputs'/'unrequested.tif').write_bytes(b'extra')
 elif fault=='source-point':
  ds=ogr.Open(str(dest/'points.gpkg'),1);layer=ds.GetLayer(0);feature=next(iter(layer));feature.SetField('mass',123.);layer.SetFeature(feature);feature=layer=ds=None
 else:
  if fault=='source-template':path=dest/'template.tif'
  with gdal.config_option('IGNORE_COG_LAYOUT_BREAK','YES'):
   ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES'])
   if fault=='grid':ds.SetGeoTransform((0,2,0,9,0,-1))
   elif fault=='mask':ds.GetRasterBand(1).SetNoDataValue(0.)
   elif fault=='type':ds.GetRasterBand(1).SetScale(17.)
   elif fault=='overview':ds.GetRasterBand(1).GetOverview(0).Fill(999.)
   else:ds.GetRasterBand(1).Fill(999.)
   ds=None
 with pytest.raises(f.FixtureError):f.verify(dest,list(f.CASES),expected)

def test_archive_existing_bytes_are_not_repaired_or_overwritten(complete_sources,tmp_path):
 root,_,_,_=complete_sources;files=f.archive_files(root/'outputs',tmp_path,list(f.CASES),{},f.PREFIX+'-cogs');name=next(iter(files));p=tmp_path/files[name]['path'];p.chmod(0o644);p.write_bytes(p.read_bytes()+b'changed');changed=p.read_bytes()
 with pytest.raises(f.FixtureError):f.archive_files(root/'outputs',tmp_path,list(f.CASES),files,f.PREFIX+'-cogs')
 assert p.read_bytes()==changed

def test_worker_stdin_frozen_source_run_scope_poison_and_cleanup(tmp_path):
 script=Path(f.__file__).read_text();sha=hashlib.sha256(script.encode()).hexdigest();root=tmp_path/'owned';root.mkdir();(root/'outputs').mkdir();(root/'.addp-online-owner').write_text(f.PREFIX+'-123-1');scratch=tmp_path/'scratch';scratch.mkdir();env={**os.environ,'TMPDIR':str(scratch)}
 def call(action,*names):return subprocess.run([sys.executable,'-','--worker',action,str(root),*names],input=script.encode(),cwd=tmp_path,env=env,capture_output=True,timeout=90)
 seeded=call('seed');assert seeded.returncode==0,seeded.stderr
 baseline=call('oracle',sha);assert baseline.returncode==0,baseline.stderr;anchor=json.loads(baseline.stdout)['oracle_sha256']
 assert call('verify',anchor,sha).returncode==0
 assert call('verify',anchor,'b'*64).returncode!=0
 (root/'.addp-online-owner').write_text(f.PREFIX+'-124-1');assert call('verify',anchor,sha).returncode!=0;(root/'.addp-online-owner').write_text(f.PREFIX+'-123-1')
 (root/f.ORACLE).chmod(0o644);assert call('verify',anchor,sha).returncode!=0
 (root/f.ORACLE).write_bytes(b'{}');assert call('verify',anchor,sha).returncode!=0
 assert json.loads(call('cleanup-oracle').stdout)['oracle_cache_removed']is True

def test_oracle_is_independent_of_product_computation_and_readers(monkeypatch):
 import operators.raster_compute as c
 def forbidden(*a,**kw):raise AssertionError('product cannot define expected output')
 for name in('raster_point_density','_point_density_value','_point_density_mass','_circle_sites','_point_grid_output','STRtree','_open'):
  monkeypatch.setattr(c,name,forbidden)
 assert set(f.oracle())==set(f.CASES)


def test_weighted_fixed_matrix_independent_formulas_controls_units_and_tails(complete_sources):
    import numpy as np
    root,initial,expected,previews=complete_sources
    assert len(f.CASES)==24 and len(f.FAILURES)==32
    proof=f.verify(root,list(f.CASES),expected)
    assert proof['actual_control_pairs_verified']==[list(v)for v in f.CONTROLS]
    assert proof['actual_scale_controls_verified']==[list(v)for v in f.SCALE_CONTROLS]
    for method in ('uniform','quartic'):
        for left,right in [('omitted','ones'),('mixed','reversed'),('integer','replicated'),('mixed','masked')]:
            assert expected[method+'-'+left+'.cog.tif']==expected[method+'-'+right+'.cog.tif']
        mixed=np.array(expected[method+'-mixed.cog.tif'])
        assert np.array_equal(np.array(expected[method+'-scaled.cog.tif']),mixed*8)
        assert not np.array_equal(mixed,np.array(expected[method+'-omitted.cog.tif']))
        assert np.any(mixed==0)and np.any(mixed>0)
        for case in ('zero','empty'):assert np.all(np.array(expected[method+'-'+case+'.cog.tif'])==0)
        assert expected[method+'-rectangular.cog.tif'][0][514]>0
        assert expected[method+'-rotated.cog.tif'][514][0]>0
    assert {v['unit']for v in proof['targets'].values()}=={'points/m2','weight/m2'}
    assert f.raster_spec('rectangular.tif')['epsg']==2222 and f.raster_spec('sheared.tif')['epsg']==2277

@pytest.mark.parametrize('fault',['one-ulp','zero-to-nodata','deduplicated','normalized','wrong-unit','weight-ignored','wrong-method','wrong-field'])
def test_weighted_oracle_rejects_actual_saved_semantic_corruption(complete_sources,tmp_path,fault):
    import numpy as np
    from osgeo import gdal
    root,_,expected,_=complete_sources;dest=tmp_path/'wrong-weighted';shutil.copytree(root,dest)
    name='uniform-zero.cog.tif'if fault=='zero-to-nodata'else'quartic-mixed.cog.tif'
    with gdal.config_option('IGNORE_COG_LAYOUT_BREAK','YES'):
        ds=gdal.OpenEx(str(dest/'outputs'/name),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES']);band=ds.GetRasterBand(1)
        if fault=='wrong-unit':band.SetUnitType('points/m2')
        else:
            value=expected[name][0][0]
            wrong=float('nan')if fault=='zero-to-nodata'else np.nextafter(value,float('inf'))if fault=='one-ulp'else value/2 if fault in('deduplicated','normalized')else expected['uniform-mixed.cog.tif'][0][0]if fault=='wrong-method'else expected['quartic-omitted.cog.tif'][0][0]
            band.WriteArray(np.array([[wrong]]),0,0)
        band=ds=None
    with pytest.raises(f.FixtureError):f.verify(dest,list(f.CASES),expected)


def test_weighted_boundary_and_adjacent_float_witnesses_have_no_epsilon():
    from fractions import Fraction as Q
    rows=f.vector_spec('points.gpkg');x,y=Q(.5),Q(8.5);squared=[(Q(p.x)-x)**2+(Q(p.y)-y)**2 for p,_ in rows]
    assert squared[6]==16 and squared[7]>16 and squared[8]<16
    assert f.vector_spec('outside_missing.gpkg')[0][0].x>9999
    assert f.vector_spec('zero_line.gpkg')[0][1]['mass']==0


def test_input_weight_values_schema_and_load_failures_preserve_expected_stage(complete_sources):
    import geopandas as gpd
    root,_,_,_=complete_sources
    for name in ('missing.gpkg','outside_missing.gpkg'):
        value=gpd.read_file(root/name);assert value['mass'].isna().all()
    assert gpd.read_file(root/'string.gpkg')['mass'].iloc[0]=='1'
    empty=gpd.read_file(root/'empty.gpkg');assert len(empty)==0 and 'mass'in empty
    assert gpd.read_file(root/'points.gpkg').geometry.name=='geometry'


def test_weighted_source_matrix_keeps_fraction_zero_duplicate_and_integer_witnesses():
    rows=f.vector_spec('points.gpkg')
    assert [v['mass']for _,v in rows]==[1.5,0.,2.,4.,.25,3.,2.,.5,1.]
    assert rows[0][0]==rows[5][0]and rows[0][1]['mass']!=rows[5][1]['mass']
    assert all(v['ones']==1 and v['zero']==0 and v['scaled']==v['mass']*8 for _,v in rows)
    assert f.vector_spec('reversed.gpkg')==rows[::-1]
    integer=f.vector_spec('integer.gpkg');assert [v['mass']for _,v in integer]==[0,1,3,2,1,2,1,0,1]
    expanded=[(p,dict(v))for p,v in integer for _ in range(v['mass'])]
    assert f.vector_spec('replicated.gpkg')==expanded
