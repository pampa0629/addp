"""Actual native workflow files, independent full oracle and rejection barriers."""
import copy
import hashlib
import importlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import pytest
sys.path.insert(0,str(Path(__file__).resolve().parents[2]))
from workflow_engine import execute_workflow,GeoPythonWorkflowRunner
from addp_common.workflow_runtime.errors import WorkflowExecutionError
f=importlib.import_module('business.scripts.online-raster-interpolate-idw-max-points-fixture')
m=importlib.import_module('scripts.test.raster-interpolate-idw-max-points-persistence-online')

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
 root=tmp_path_factory.mktemp('raster-interpolate-idw-max-points');(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('raster-interpolate-idw-max-points-123-1')
 initial=f.seed(root);expected=json.loads(f.encoded(f.oracle()));(root/f.ORACLE).write_bytes(f.encoded(expected));previews={}
 for name in f.CASES:
  dag=m.definition({n:n for n in f.SOURCES},'parent',name);localize(dag,root,root/'outputs'/name)
  result=execute_workflow(dag);assert result['status']=='success',result;previews[name]=json.loads(result['final_result'])
  proof=f.verify(root,list(previews),expected);assert proof['sources']==initial['sources']and proof['full_cog_checks']==len(previews)
 return root,initial,expected,previews

def test_sixteen_complete_cogs_independent_oracle_and_raw_archives(complete_sources,tmp_path):
 root,initial,expected,previews=complete_sources;assert len(expected)==16
 proof=f.verify(root,list(f.CASES),expected);assert proof['full_cog_checks']==16 and proof['sources']==initial['sources']
 archives=f.archive_files(root/'outputs',tmp_path,list(f.CASES),{},f.PREFIX+'-cogs');assert len(archives)==16
 for name in f.CASES:
  m.raster.validate_raster_artifact(previews[name],f.artifact_expectation(name))
  assert (tmp_path/archives[name]['path']).read_bytes()==(root/'outputs'/name).read_bytes()and archives[name]['size_bytes']>0

@pytest.mark.parametrize('problem',f.FAILURES)
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_exact_bilingual_failures_publish_nothing_and_remove_workspaces(complete_sources,tmp_path,monkeypatch,problem,language):
 import workflow_engine
 root,initial,expected,_=complete_sources;name=next(iter(f.CASES));dag=m.definition({n:n for n in f.SOURCES},'parent',name,invalid=problem,language=language);target=tmp_path/'failed.cog.tif';localize(dag,root,target)
 work=[];original=workflow_engine._execute_operator
 def track(operator,params):
  if operator=='raster_interpolate_idw':work.append(params['reference_raster'].workspace)
  return original(operator,params)
 monkeypatch.setattr(workflow_engine,'_execute_operator',track)
 with pytest.raises(WorkflowExecutionError)as error:GeoPythonWorkflowRunner(language=language).execute(dag)
 assert error.value.error_code=='EXECUTION_FAILED'and str(error.value)=='任务 interpolate 执行失败: '+m.MESSAGES[problem][language=='en']
 assert work and all(not w.exists()for w in work)and not target.exists()and list(tmp_path.iterdir())==[]
 assert f.verify(root,list(f.CASES),expected)['sources']==initial['sources']

@pytest.mark.parametrize('fault',['value','mask','grid','overview','source-point','source-template','extra-output','type'])
def test_real_physical_corruption_and_source_or_extra_files_are_rejected(complete_sources,tmp_path,fault):
 from osgeo import ogr,gdal
 root,_,expected,_=complete_sources;dest=tmp_path/'mutated';shutil.copytree(root,dest);path=dest/'outputs'/('rectangular-block.cog.tif'if fault=='overview'else'base.cog.tif')
 if fault=='extra-output':(dest/'outputs'/'unrequested.tif').write_bytes(b'extra')
 elif fault=='source-point':
  ds=ogr.Open(str(dest/'points.gpkg'),1);layer=ds.GetLayer(0);feature=next(iter(layer));feature.SetField('height_value',123.);layer.SetFeature(feature);feature=layer=ds=None
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
 import heapq
 from shapely import STRtree
 monkeypatch.setattr(heapq,'nsmallest',forbidden)
 monkeypatch.setattr(c,'STRtree',forbidden)
 for name in('raster_interpolate_idw','_idw_value','_idw_bound','_open'):
  if hasattr(c,name):monkeypatch.setattr(c,name,forbidden)
 assert set(f.oracle())==set(f.CASES)

def test_idw_max_points_frozen_matrix_has_real_distance_x_y_selection_and_controls(complete_sources):
 from fractions import Fraction as Q
 root,_,expected,_=complete_sources
 assert expected['base.cog.tif']==expected['unlimited.cog.tif']
 assert expected['nearest-two.cog.tif']==expected['reversed.cog.tif']
 assert expected['nearest-one.cog.tif'][0][0]==12. and expected['nearest-two.cog.tif'][0][0]==14.
 assert expected['nearest-one.cog.tif'][0][0]!=10. and expected['nearest-two.cog.tif'][0][0]!=10.
 assert expected['base.cog.tif'][1][1]==5. and expected['mask-alpha.cog.tif'][1][1]==8.
 assert expected['boundary.cog.tif']==expected['above.cog.tif']==[[4.]] and expected['below.cog.tif']==[[None]]
 assert expected['coincident.cog.tif'][0][0]==0. and expected['coincident.cog.tif'][0][2]==-4.
 assert sum(v is not None for row in expected['coincident.cog.tif']for v in row)==2
 for name in('shortfall.cog.tif','empty.cog.tif'):assert all(v is None for row in expected[name]for v in row)
 assert any(v is not None for row in expected['mask-alpha.cog.tif']for v in row)
 assert f.raster_spec('rectangular.tif')['shape']==(2,515)and f.raster_spec('sheared.tif')['shape']==(515,2)
 proof=f.verify(root,list(f.CASES),expected)
 assert proof['actual_control_pairs_verified']==[list(pair)for pair in f.CONTROLS]
 assert all(t['minimum_support_verified']is True and t['nearest_support_verified']is True for t in proof['targets'].values())
 # These invalid samples lose nearest selection at the witness pixel, but still fail full-input validation.
 for source in('duplicate_equal.gpkg','duplicate_different.gpkg','duplicate_outside.gpkg','value_null.gpkg','value_unsafe.gpkg'):
  rows=f.vector_spec(source);assert rows[0][0].x==.5 and rows[0][0].y==8.5 and rows[0][1]['height_value']==0
  assert all(row[0].x!=.5 or row[0].y!=8.5 for row in rows[1:])

@pytest.mark.parametrize('name',['nearest-one.cog.tif','nearest-two.cog.tif','shortfall.cog.tif','coincident.cog.tif','boundary.cog.tif'])
def test_nearest_support_oracle_rejects_wrong_selection_shortfall_coincidence_and_boundary(complete_sources,tmp_path,name):
 from osgeo import gdal
 root,_,expected,_=complete_sources;dest=tmp_path/'corrupted';shutil.copytree(root,dest)
 with gdal.config_option('IGNORE_COG_LAYOUT_BREAK','YES'):
  ds=gdal.OpenEx(str(dest/'outputs'/name),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES']);ds.GetRasterBand(1).Fill(float('nan')if name!='shortfall.cog.tif'else 1.);ds=None
 with pytest.raises(f.FixtureError):f.verify(dest,list(f.CASES),expected)

@pytest.mark.parametrize('fault',['distance','x','y','unlimited'])
def test_nearest_saved_cog_rejects_specific_wrong_distance_or_tie_choices(complete_sources,tmp_path,fault):
 import numpy as np
 from osgeo import gdal
 root,_,expected,_=complete_sources;dest=tmp_path/'wrong-selection';shutil.copytree(root,dest)
 name='mask-alpha.cog.tif'if fault=='distance'else'nearest-one.cog.tif'if fault=='x'else'nearest-two.cog.tif'
 row,col=(1,1)if fault=='distance'else(0,0);wrong={'distance':5.,'x':8.,'y':8.,'unlimited':10.}[fault]
 with gdal.config_option('IGNORE_COG_LAYOUT_BREAK','YES'):
  ds=gdal.OpenEx(str(dest/'outputs'/name),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES']);ds.GetRasterBand(1).WriteArray(np.array([[wrong]]),col,row);ds=None
 with pytest.raises(f.FixtureError,match='values/mask/zero'):f.verify(dest,list(f.CASES),expected)
