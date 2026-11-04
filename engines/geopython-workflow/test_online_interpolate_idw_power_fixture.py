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
f=importlib.import_module('business.scripts.online-raster-interpolate-idw-power-fixture')
m=importlib.import_module('scripts.test.raster-interpolate-idw-power-persistence-online')

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
 root=tmp_path_factory.mktemp('raster-interpolate-idw-power');(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('raster-interpolate-idw-power-123-1')
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
 root,_,expected,_=complete_sources;dest=tmp_path/'mutated';shutil.copytree(root,dest);path=dest/'outputs'/('p1-foot-block.cog.tif'if fault=='overview'else'p2-default.cog.tif')
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
 for name in('raster_interpolate_idw','_idw_value','_idw_bound','_open','_idw_float','_idw_radical_add','_idw_inverse_root_bounds','_idw_radical_mean','_circle_sites'):
  if hasattr(c,name):monkeypatch.setattr(c,name,forbidden)
 assert set(f.oracle())==set(f.CASES)


def test_power_matrix_controls_analytic_values_support_boundaries_units_and_tails(complete_sources):
 import math,numpy as np
 from fractions import Fraction as Q
 root,_,expected,_=complete_sources;assert len(f.CASES)==24 and len(f.FAILURES)==40
 assert [expected[n][0][0]for n in ('p1-base.cog.tif','p2-default.cog.tif','p3-base.cog.tif')]==[3.,1.8,1.]
 assert expected['p2-default.cog.tif']==expected['p2-explicit.cog.tif']
 for left,right in f.CONTROLS:assert expected[left]==expected[right]
 for power in (1,2,3):
  base=f'p{power}-'+('default'if power==2 else'base')+'.cog.tif';assert expected[base][8][1]==0. and expected[base][8][0]==-7.
  row=expected[f'p{power}-nearest.cog.tif'][0];assert row[0]==14. and row[2]is None and row[3]==4. and row[4]is None and row[1]>0
  answer=expected[f'p{power}-coincident.cog.tif'];assert answer[0][0]==0. and answer[0][2]==-7. and sum(v is not None for r in answer for v in r)==2
  assert all(v is None for r in expected[f'p{power}-empty.cog.tif']for v in r)
  for suffix in ('foot-block','survey-block'):
   a=np.array(expected[f'p{power}-{suffix}.cog.tif'],float);assert np.isfinite(a).all()and a.min()==-4. and a.max()==12.
 proof=f.verify(root,list(f.CASES),expected);assert proof['actual_control_pairs_verified']==[list(p)for p in f.CONTROLS]
 assert proof['actual_power_witness_verified']==['p1-base.cog.tif','p2-default.cog.tif','p3-base.cog.tif']
 assert all(t['distance_power_verified']is True for t in proof['targets'].values())
 assert f.raster_spec('rectangular.tif')['epsg']==2222 and f.raster_spec('sheared.tif')['epsg']==2277

@pytest.mark.parametrize('fault',['one-ulp','wrong-power','default-changed','zero-to-nodata','shortfall-filled','coincidence-lost','nearest-x','nearest-y','boundary-epsilon','mask-inherited'])
def test_saved_power_cogs_reject_specific_value_support_mask_and_default_corruption(complete_sources,tmp_path,fault):
 from osgeo import gdal
 import numpy as np
 root,_,expected,_=complete_sources;dest=tmp_path/'wrong-power';shutil.copytree(root,dest)
 name='p1-base.cog.tif';row,col=0,0;wrong=3.
 if fault=='one-ulp':wrong=np.nextafter(3.,math.inf)
 elif fault=='wrong-power':wrong=1.8
 elif fault=='default-changed':name='p2-default.cog.tif';wrong=3.
 elif fault=='zero-to-nodata':row,col,wrong=8,1,float('nan')
 elif fault=='shortfall-filled':name='p3-coincident.cog.tif';row,col,wrong=1,1,1.
 elif fault=='coincidence-lost':name='p1-coincident.cog.tif';wrong=float('nan')
 elif fault in('nearest-x','nearest-y'):name='p3-nearest.cog.tif';wrong=8.
 elif fault=='boundary-epsilon':name='p1-nearest.cog.tif';col=2;wrong=999.
 elif fault=='mask-inherited':name='p1-masked.cog.tif';wrong=float('nan')
 with gdal.config_option('IGNORE_COG_LAYOUT_BREAK','YES'):
  ds=gdal.OpenEx(str(dest/'outputs'/name),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES']);ds.GetRasterBand(1).WriteArray(np.array([[wrong]]),col,row);ds=None
 with pytest.raises(f.FixtureError):f.verify(dest,list(f.CASES),expected)


def test_input_schema_exact_bad_values_and_radius_witnesses_survive_file_load(complete_sources):
 import geopandas as gpd
 from fractions import Fraction as Q
 root,_,_,_=complete_sources
 for name in ('outside_null.gpkg','unselected_null.gpkg'):
  frame=gpd.read_file(root/name);assert frame.height_value.iloc[0]==0. and math.isnan(frame.height_value.iloc[1]);assert frame.geometry.iloc[0].x==.5
 assert gpd.read_file(root/'outside_null.gpkg').geometry.iloc[1].x==999.
 assert gpd.read_file(root/'unselected_null.gpkg').geometry.iloc[1].x==2.5
 assert len(gpd.read_file(root/'empty.gpkg'))==0 and 'height_value'in gpd.read_file(root/'empty.gpkg').columns
 rows=f.vector_spec('ties.gpkg');assert Q(rows[5][0].x)-3==-1 and abs(Q(rows[6][0].x)-3)<1 and abs(Q(rows[8][0].x)-6)>1


def test_p2_expected_grid_uses_original_exact_fraction_formula(complete_sources):
 from fractions import Fraction as Q
 root,_,expected,_=complete_sources
 for row in range(9):
  for col in range(9):
   samples=[]
   for point,props in f.vector_spec('points.gpkg'):
    d=(Q(point.x)-Q(2*col+1,2))**2+(Q(point.y)-(9-Q(2*row+1,2)))**2
    if d<=16:samples.append((d,Q(props['height_value'])))
   hit=[v for d,v in samples if d==0]
   value=float(hit[0])if hit else float(sum((v/d for d,v in samples),Q(0))/sum((1/d for d,v in samples),Q(0)))if samples else None
   assert expected['p2-default.cog.tif'][row][col]==value
