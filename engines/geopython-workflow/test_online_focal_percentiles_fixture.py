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
f=importlib.import_module('business.scripts.online-raster-focal-percentiles-fixture')
m=importlib.import_module('scripts.test.raster-focal-percentiles-persistence-online')

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
 root=tmp_path_factory.mktemp('raster-focal-percentiles');(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('raster-focal-percentiles-123-1')
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
  m.raster.validate_raster_artifact(previews[name],f.artifact_expectation(name)) if f.artifact_expectation(name)['extent_srid'] else assert_unlocated_preview(previews[name],name)
  assert (tmp_path/archives[name]['path']).read_bytes()==(root/'outputs'/name).read_bytes()and archives[name]['size_bytes']>0

@pytest.mark.parametrize('problem',f.FAILURES)
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_exact_bilingual_failures_publish_nothing_and_remove_workspaces(complete_sources,tmp_path,monkeypatch,problem,language):
 import workflow_engine
 root,initial,expected,_=complete_sources;name=next(iter(f.CASES));dag=m.definition({n:n for n in f.SOURCES},'parent',name,invalid=problem,language=language);target=tmp_path/'failed.cog.tif';localize(dag,root,target)
 work=[];original=workflow_engine._execute_operator
 def track(operator,params):
  if operator=='raster_focal_statistics':work.append(params['input_raster'].workspace)
  return original(operator,params)
 monkeypatch.setattr(workflow_engine,'_execute_operator',track)
 with pytest.raises(WorkflowExecutionError)as error:GeoPythonWorkflowRunner(language=language).execute(dag)
 assert error.value.error_code=='EXECUTION_FAILED'and str(error.value)=='任务 focal 执行失败: '+m.MESSAGES[problem][language=='en']
 assert work and all(not w.exists()for w in work)and not target.exists()and list(tmp_path.iterdir())==[]
 assert f.verify(root,list(f.CASES),expected)['sources']==initial['sources']

@pytest.mark.parametrize('fault',['value','mask','grid','overview','source-value','source-alpha','extra-output','type','description'])
def test_real_physical_corruption_and_source_or_extra_files_are_rejected(complete_sources,tmp_path,fault):
 from osgeo import ogr,gdal
 root,_,expected,_=complete_sources;dest=tmp_path/'mutated';shutil.copytree(root,dest);path=dest/'outputs'/('cross-block.cog.tif'if fault=='overview'else'square3.cog.tif')
 if fault=='extra-output':(dest/'outputs'/'unrequested.tif').write_bytes(b'extra')
 else:
  if fault.startswith('source-'):path=dest/'values.tif'
  with gdal.config_option('IGNORE_COG_LAYOUT_BREAK','YES'):
   ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES'])
   if fault=='grid':ds.SetGeoTransform((0,2,0,9,0,-1))
   elif fault=='mask':ds.GetRasterBand(1).SetNoDataValue(0.)
   elif fault=='type':ds.GetRasterBand(1).SetScale(17.)
   elif fault=='description':ds.GetRasterBand(6).SetDescription('percentile=0.0')
   elif fault=='overview':ds.GetRasterBand(6).GetOverview(0).Fill(999.)
   elif fault=='source-alpha':ds.GetRasterBand(4).Fill(0.)
   else:ds.GetRasterBand(6 if fault=='value'else 2).Fill(999.)
   ds=None
 with pytest.raises(f.FixtureError):f.verify(dest,list(f.CASES),expected)

def test_archive_existing_bytes_are_not_repaired_or_overwritten(complete_sources,tmp_path):
 root,_,_,_=complete_sources;files=f.archive_files(root/'outputs',tmp_path,list(f.CASES),{},f.PREFIX+'-cogs');name=next(iter(files));p=tmp_path/files[name]['path'];p.chmod(0o644);p.write_bytes(p.read_bytes()+b'changed');changed=p.read_bytes()
 with pytest.raises(f.FixtureError):f.archive_files(root/'outputs',tmp_path,list(f.CASES),files,f.PREFIX+'-cogs')
 assert p.read_bytes()==changed

def test_worker_stdin_frozen_source_run_scope_poison_and_cleanup(tmp_path):
 script=Path(f.__file__).read_text();sha=hashlib.sha256(script.encode()).hexdigest();root=tmp_path/'owned';root.mkdir();(root/'outputs').mkdir();(root/'.addp-online-owner').write_text(f.PREFIX+'-123-1');scratch=tmp_path/'scratch';scratch.mkdir();env={**os.environ,'TMPDIR':str(scratch)}
 def call(action,*names):return subprocess.run([sys.executable,'-','--worker',action,str(root),*names],input=script.encode(),cwd=tmp_path,env=env,capture_output=True)
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
 for name in('raster_focal_statistics','_focal_rank_values','_focal_ordered_values','_open'):
  monkeypatch.setattr(c,name,forbidden)
 assert set(f.oracle())==set(f.CASES)

def test_focal_percentiles_frozen_matrix_controls_and_integer_validity(complete_sources):
 import numpy as np
 _,_,expected,_=complete_sources
 for a,b in f.COMPARISONS:assert expected[a]==expected[b]
 for a,b in f.MEDIANS:assert expected[a][3]==expected[b][0]
 assert all(v is None for band in expected['invalid.cog.tif']for row in band for v in row)
 for n in('int64-unlocated.cog.tif','uint64.cog.tif'):
  values=np.array(expected[n],dtype=float);assert np.isnan(values[:,-2,-2]).all()and np.isfinite(values[:,-2,-1]).all()
 assert np.any(np.array(expected['square3.cog.tif'],dtype=float)==0)
 assert f.offsets('circle.cog.tif')==[(-1,0),(0,-1),(0,0),(0,1),(1,0)]

@pytest.mark.parametrize('band',[0,1,3,4,5])
def test_focal_percentiles_exact_dyadic_band_rejects_one_ulp(complete_sources,band):
 import numpy as np
 _,_,expected,_=complete_sources;wanted=np.array(expected['square3.cog.tif'],dtype=float);actual=wanted.copy();actual[band,0,1]=np.nextafter(actual[band,0,1],np.inf)
 with pytest.raises(f.FixtureError,match='percentile values'):f.assert_pixels(actual,wanted,f.PERCENTILES)

def assert_unlocated_preview(value,name):
 wanted=f.artifact_expectation(name)
 assert all(value.get(k)==wanted[k]for k in('width','height','band_count','source_crs','extent_srid','transform'))
 assert len(value['bands'])==6 and all(b['dtype']=='Float64'and b['nodata_is_nan']is True for b in value['bands'])
