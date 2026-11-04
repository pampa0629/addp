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
f=importlib.import_module('business.scripts.online-raster-line-density-fixture')
m=importlib.import_module('scripts.test.raster-line-density-persistence-online')

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
 root=tmp_path_factory.mktemp('raster-line-density');(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('raster-line-density-123-1')
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
  if operator=='raster_line_density':work.append(params['reference_raster'].workspace)
  return original(operator,params)
 monkeypatch.setattr(workflow_engine,'_execute_operator',track)
 with pytest.raises(WorkflowExecutionError)as error:GeoPythonWorkflowRunner(language=language).execute(dag)
 assert error.value.error_code=='EXECUTION_FAILED'and str(error.value)=='任务 density 执行失败: '+m.MESSAGES[problem][language=='en']
 assert work and all(not w.exists()for w in work)and not target.exists()and list(tmp_path.iterdir())==[]
 assert f.verify(root,list(f.CASES),expected)['sources']==initial['sources']

@pytest.mark.parametrize('fault',['value','mask','grid','overview','source-point','source-template','extra-output','type'])
def test_real_physical_corruption_and_source_or_extra_files_are_rejected(complete_sources,tmp_path,fault):
 from osgeo import ogr,gdal
 root,_,expected,_=complete_sources;dest=tmp_path/'mutated';shutil.copytree(root,dest);path=dest/'outputs'/('foot-block.cog.tif'if fault=='overview'else'base.cog.tif')
 if fault=='extra-output':(dest/'outputs'/'unrequested.tif').write_bytes(b'extra')
 elif fault=='source-point':
  ds=ogr.Open(str(dest/'lines.gpkg'),1);layer=ds.GetLayer(0);feature=next(iter(layer));feature.SetField('height_value',123.);layer.SetFeature(feature);feature=layer=ds=None
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
 for name in('raster_line_density','_line_density_float','_line_density_segment','_line_density_add','_rational_sqrt_bounds','_point_grid_output','STRtree','_open'):
  monkeypatch.setattr(c,name,forbidden)
 assert set(f.oracle())==set(f.CASES)


def test_line_density_frozen_matrix_exact_boundaries_multiplicities_units_tails_and_controls(complete_sources):
 from fractions import Fraction as Q
 from decimal import Decimal as D,localcontext
 import math,numpy as np
 root,_,expected,_=complete_sources;assert len(f.CASES)==16 and len(f.FAILURES)==20
 for left,right in f.CONTROLS:assert expected[left]==expected[right]
 for left,right,multiple in f.SCALE_CONTROLS:np.testing.assert_array_equal(np.array(expected[right]),np.array(expected[left])*multiple)
 for name in ('tangent.cog.tif','outside-tangent.cog.tif','endpoint.cog.tif'):assert expected[name]==[[0.]]
 y=math.nextafter(1.,0.)
 with localcontext()as context:
  context.prec=2000;wanted=float(2*(1-D.from_float(y)**2).sqrt()/D.from_float(math.pi))
 assert expected['inside-tangent.cog.tif']==[[wanted]] and wanted>0
 assert all(v==0 for row in expected['empty.cog.tif']for v in row)
 assert np.all(np.array(expected['overlap.cog.tif'])>=np.array(expected['base.cog.tif'])) and np.any(np.array(expected['overlap.cog.tif'])>np.array(expected['base.cog.tif']))
 assert f.raster_spec('rectangular.tif')['shape']==(2,515) and f.raster_spec('rectangular.tif')['epsg']==2222
 assert f.raster_spec('sheared.tif')['shape']==(515,2) and f.raster_spec('sheared.tif')['epsg']==2277
 assert expected['foot-block.cog.tif'][0][514]>0 and expected['survey-block.cog.tif'][514][0]>0
 assert np.ptp(expected['foot-block.cog.tif'])>0 and np.ptp(expected['survey-block.cog.tif'])>0
 proof=f.verify(root,list(f.CASES),expected)
 assert proof['actual_control_pairs_verified']==[list(pair)for pair in f.CONTROLS] and proof['actual_scale_controls_verified']==[list(v)for v in f.SCALE_CONTROLS]
 assert all(t['line_length_and_full_circle_area_verified'] is True and t['unit_metres_per_square_metre_verified'] is True for t in proof['targets'].values())

@pytest.mark.parametrize('fault',['one-ulp','zero-to-nodata','duplicate-collapsed','retrace-collapsed','tangent-filled','inside-rounded-zero','outside-epsilon','edge-corrected','wrong-unit','foot-tail','survey-overview','mask-inherited'])
def test_line_density_oracle_rejects_specific_length_circle_boundary_zero_unit_and_tail_errors(complete_sources,tmp_path,fault):
 import math,numpy as np
 from osgeo import gdal
 root,_,expected,_=complete_sources;dest=tmp_path/'wrong-density';shutil.copytree(root,dest)
 name={'zero-to-nodata':'empty.cog.tif','duplicate-collapsed':'doubled.cog.tif','retrace-collapsed':'retraced.cog.tif','tangent-filled':'tangent.cog.tif','inside-rounded-zero':'inside-tangent.cog.tif','outside-epsilon':'outside-tangent.cog.tif','foot-tail':'foot-block.cog.tif','survey-overview':'survey-block.cog.tif','mask-inherited':'mask-alpha.cog.tif'}.get(fault,'base.cog.tif')
 row,col=(0,514) if fault=='foot-tail' else (0,0)
 with gdal.config_option('IGNORE_COG_LAYOUT_BREAK','YES'):
  ds=gdal.OpenEx(str(dest/'outputs'/name),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES']);band=ds.GetRasterBand(1)
  if fault=='wrong-unit':band.SetUnitType('metre/ft2')
  elif fault=='survey-overview':
   assert band.GetOverviewCount()==f.overview_count(name)>0
   band.GetOverview(band.GetOverviewCount()-1).Fill(999.)
  else:
   actual=expected[name][row][col]
   wrong=float('nan')if fault in('zero-to-nodata','mask-inherited')else 0. if fault in('inside-rounded-zero','foot-tail')else 1e-16 if fault in('tangent-filled','outside-epsilon')else actual/2 if fault in('duplicate-collapsed','retrace-collapsed')else actual*2 if fault=='edge-corrected'else np.nextafter(actual,math.inf)
   band.WriteArray(np.array([[wrong]]),col,row)
  band=ds=None
 with pytest.raises(f.FixtureError):f.verify(dest,list(f.CASES),expected)


def test_all_abnormal_geometries_and_boundary_wkb_survive_geopackage_without_normalization(complete_sources):
 import geopandas as gpd
 from shapely import get_coordinates,has_z
 import math,numpy as np
 root,_,_,_=complete_sources
 for name in f.VECTORS:
  frame=gpd.read_file(root/name);rows=f.vector_spec(name);assert len(frame)==len(rows)
  for actual,(wanted,props) in zip(frame.geometry,rows):
   assert (actual is None)==(wanted is None)
   if actual is not None:assert actual.wkb==wanted.wkb
 assert gpd.read_file(root/'null_geometry.gpkg').geometry.iloc[0] is None
 assert gpd.read_file(root/'empty_geometry.gpkg').geometry.iloc[0].is_empty
 for name in ('z_line.gpkg','outside_z_line.gpkg'):assert has_z(gpd.read_file(root/name).geometry.iloc[0])
 assert not gpd.read_file(root/'zero_line.gpkg').geometry.iloc[0].is_valid
 assert not gpd.read_file(root/'zero_component.gpkg').geometry.iloc[0].is_valid
 for name,y in [('tangent.gpkg',1.),('inside-tangent.gpkg',math.nextafter(1.,0.)),('outside-tangent.gpkg',math.nextafter(1.,math.inf))]:
  assert np.array_equal(get_coordinates(gpd.read_file(root/name).geometry.iloc[0])[:,1],np.array([y,y]))
 assert gpd.read_file(root/'different_crs.gpkg').crs.to_epsg()==32650
 assert gpd.read_file(root/'geographic.gpkg').crs.to_epsg()==4326
