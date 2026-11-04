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
f=importlib.import_module('business.scripts.online-raster-line-kernel-density-fixture')
m=importlib.import_module('scripts.test.raster-line-kernel-density-persistence-online')

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
 root=tmp_path_factory.mktemp('raster-line-kernel-density');(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('raster-line-kernel-density-123-1')
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
 root,initial,expected,_=complete_sources;name='mixed.cog.tif';dag=m.definition({n:n for n in f.SOURCES},'parent',name,invalid=problem,language=language);target=tmp_path/'failed.cog.tif';localize(dag,root,target)
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
 root,_,expected,_=complete_sources;dest=tmp_path/'mutated';shutil.copytree(root,dest);path=dest/'outputs'/('foot-block.cog.tif'if fault=='overview'else'mixed.cog.tif')
 if fault=='extra-output':(dest/'outputs'/'unrequested.tif').write_bytes(b'extra')
 elif fault=='source-point':
  ds=ogr.Open(str(dest/'lines.gpkg'),1);layer=ds.GetLayer(0);feature=next(iter(layer));feature.SetField('mass',123.);layer.SetFeature(feature);feature=layer=ds=None
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
 for name in('raster_line_density','_line_density_float','_line_density_segment','_line_density_add','_rational_sqrt_bounds','_point_grid_output','STRtree','_open','_density_weights'):
  monkeypatch.setattr(c,name,forbidden)
 assert set(f.oracle())==set(f.CASES)



def test_weighted_line_density_frozen_matrix_units_tails_and_all_actual_controls(complete_sources):
 from decimal import Decimal as D,localcontext
 import math,numpy as np
 root,_,expected,_=complete_sources;assert len(f.CASES)==24 and len(f.FAILURES)==30 and len(f.SOURCES)==28
 for left,right in f.CONTROLS:np.testing.assert_array_equal(expected[left],expected[right])
 for left,right,multiple in f.SCALE_CONTROLS:np.testing.assert_array_equal(expected[right],np.array(expected[left])*multiple)
 profile=expected['boundary-profile.cog.tif'][0]
 assert profile[0]==profile[2]==profile[3]==profile[4]==profile[5]==0
 with localcontext()as context:
  context.prec=2000;wanted=float(48*(1-D.from_float(math.nextafter(1.,0.))**2)**(D(5)/2)/(5*D.from_float(math.pi)))
 assert profile[1]==wanted and wanted>0
 assert np.count_nonzero(expected['zero.cog.tif'])==np.count_nonzero(expected['empty.cog.tif'])==0
 assert f.raster_spec('rectangular.tif')['shape']==(2,515) and f.raster_spec('rectangular.tif')['epsg']==2222
 assert f.raster_spec('sheared.tif')['shape']==(515,2) and f.raster_spec('sheared.tif')['epsg']==2277
 assert expected['foot-block.cog.tif'][0][514]>0 and expected['survey-block.cog.tif'][514][0]>0
 assert np.ptp(expected['foot-block.cog.tif'])>0 and np.ptp(expected['survey-block.cog.tif'])>0
 proof=f.verify(root,list(f.CASES),expected)
 assert proof['actual_control_pairs_verified']==[list(v)for v in f.CONTROLS] and proof['actual_scale_controls_verified']==[list(v)for v in f.SCALE_CONTROLS]
 for name,t in proof['targets'].items():
  assert t['line_length_and_full_circle_area_verified']is True and t['line_density_units_verified']is True and t['unnormalized_weights_verified']is True
  assert t['weight_field']==f.CASES[name][3] and t['weight_field_parameter_omitted']is(f.CASES[name][3]is None) and t['unit']==f.output_unit(name)
 assert all(t['line_density_method_verified']is True and t['method']==(f.CASES[n][4]or'uniform')and t['method_parameter_omitted']is(f.CASES[n][4]is None)for n,t in proof['targets'].items())
 assert proof['actual_method_controls_verified']==[list(v)for v in f.METHOD_CONTROLS]
 for left,right in f.METHOD_CONTROLS:assert not np.array_equal(expected[left],expected[right])
 with localcontext()as context:
  context.prec=2000;pi=D.from_float(math.pi)
  assert expected['diameter.cog.tif'][0][0]==float(D(16)/(5*pi))
  assert expected['chord.cog.tif'][0][0]==float(D(16)*(D(3)/4)**(D(5)/2)/(5*pi))
 assert f.output_unit('omitted.cog.tif')=='metre/m2' and f.output_unit('ones.cog.tif')=='weight/metre'

@pytest.mark.parametrize('fault',['one-ulp','zero-to-nodata','ignored-weight','normalized-weight','integer-replication','retrace-collapsed','multipart-collapsed','tangent-filled','inside-rounded-zero','outside-epsilon','edge-corrected','wrong-unit','omitted-unit','foot-tail','survey-overview','mask-inherited'])
def test_weighted_oracle_rejects_specific_weight_normalization_boundary_unit_and_tail_errors(complete_sources,tmp_path,fault):
 import math,numpy as np
 from osgeo import gdal
 root,_,expected,_=complete_sources;dest=tmp_path/'wrong-density';shutil.copytree(root,dest)
 name={'zero-to-nodata':'zero.cog.tif','ignored-weight':'mixed.cog.tif','normalized-weight':'scaled.cog.tif','integer-replication':'replicated.cog.tif','retrace-collapsed':'retraced.cog.tif','multipart-collapsed':'multipart.cog.tif','tangent-filled':'boundary-profile.cog.tif','inside-rounded-zero':'boundary-profile.cog.tif','outside-epsilon':'boundary-profile.cog.tif','omitted-unit':'omitted.cog.tif','foot-tail':'foot-block.cog.tif','survey-overview':'survey-block.cog.tif','mask-inherited':'mask-alpha.cog.tif'}.get(fault,'mixed.cog.tif')
 row,col=(0,514)if fault=='foot-tail'else(0,1)if fault=='inside-rounded-zero'else(0,2)if fault=='outside-epsilon'else(0,0)
 with gdal.config_option('IGNORE_COG_LAYOUT_BREAK','YES'):
  ds=gdal.OpenEx(str(dest/'outputs'/name),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES']);band=ds.GetRasterBand(1)
  if fault in('wrong-unit','omitted-unit'):band.SetUnitType('metre/m2'if fault=='wrong-unit'else'weight/metre')
  elif fault=='survey-overview':
   assert band.GetOverviewCount()==f.overview_count(name)>0;band.GetOverview(band.GetOverviewCount()-1).Fill(999.)
  else:
   actual=expected[name][row][col]
   wrong=float('nan')if fault in('zero-to-nodata','mask-inherited')else 0. if fault in('inside-rounded-zero','foot-tail')else 1e-16 if fault in('tangent-filled','outside-epsilon')else expected['omitted.cog.tif'][row][col]if fault=='ignored-weight'else actual/8 if fault=='normalized-weight'else actual/2 if fault in('integer-replication','retrace-collapsed','multipart-collapsed')else actual*2 if fault=='edge-corrected'else np.nextafter(actual,math.inf)
   assert wrong!=actual or math.isnan(wrong)
   band.WriteArray(np.array([[wrong]]),col,row)
  band=ds=None
 with pytest.raises(f.FixtureError):f.verify(dest,list(f.CASES),expected)

def test_all_weight_fields_and_zero_weight_invalid_geometries_survive_real_source_loading(complete_sources):
 import geopandas as gpd
 from shapely import get_coordinates,has_z
 import math,numpy as np
 from operators.io_operators import load
 root,_,_,_=complete_sources
 for name in f.VECTORS:
  frame=gpd.read_file(root/name);rows=f.vector_spec(name);assert len(frame)==len(rows)
  for actual,(wanted,props)in zip(frame.geometry,rows):assert actual.wkb==wanted.wkb
  f.verify_sources(root)
 loaded=load(path='lines.gpkg',connection_info={'mount_path':str(root)})
 assert m.FIELD_FAILURES['field_geometry']==loaded.geometry.name
 assert loaded['mass'].tolist()==[.5,3.,1.25]
 assert list(loaded.geometry.iloc[2].coords)[-3:]==[(5.,1.5),(2.,3.),(5.,1.5)]
 assert gpd.read_file(root/'missing.gpkg')['mass'].isna().tolist()==[False,True]
 assert gpd.read_file(root/'outside_missing.gpkg')['mass'].isna().tolist()==[False,True]
 assert gpd.read_file(root/'negative.gpkg')['mass'].tolist()==[1.,-1.]
 assert gpd.read_file(root/'outside_negative.gpkg')['mass'].tolist()==[1.,-1.]
 assert gpd.read_file(root/'string.gpkg')['mass'].tolist()==['1','3']
 for name in ('zero_line.gpkg','zero_component.gpkg'):assert not gpd.read_file(root/name).geometry.iloc[0].is_valid
 assert gpd.read_file(root/'zero_point.gpkg').geometry.iloc[0].geom_type=='Point'
 assert has_z(gpd.read_file(root/'zero_outside_z.gpkg').geometry.iloc[0])
 assert gpd.read_file(root/'zero_crs.gpkg').crs.to_epsg()==32650
 frame=gpd.read_file(root/'profile.gpkg')
 for i,y in enumerate((1.,math.nextafter(1.,0.),math.nextafter(1.,math.inf))):assert np.array_equal(get_coordinates(frame.geometry.iloc[i])[:,1],np.array([y,y]))
 for name in('zero_point','zero_line','zero_component','zero_outside_z','zero_crs'):assert gpd.read_file(root/(name+'.gpkg'))['mass'].tolist()==[0.]
 assert len(gpd.read_file(root/'replicated.gpkg'))==5 and gpd.read_file(root/'empty.gpkg').empty


def test_kernel_definition_keeps_actual_uniform_defaults_and_explicit_quartic_binding():
 sources={n:n for n in f.SOURCES}
 assert len([v for v in f.CASES.values()if v[4]=='quartic'])==18
 assert len([v for v in f.CASES.values()if v[4]in(None,'uniform')])==6
 for name,case in f.CASES.items():
  params=m.definition(sources,'parent',name)['tasks'][-2]['params']
  assert ('method'not in params)if case[4]is None else(params['method']==case[4])
  assert ('weight_field'not in params)if case[3]is None else(params['weight_field']==case[3])


def test_kernel_oracle_cannot_accept_uniform_values_as_quartic_results(complete_sources,tmp_path):
 import numpy as np
 from osgeo import gdal
 root,_,expected,_=complete_sources;dest=tmp_path/'mode-changed';shutil.copytree(root,dest)
 with gdal.config_option('IGNORE_COG_LAYOUT_BREAK','YES'):
  ds=gdal.OpenEx(str(dest/'outputs'/'mixed.cog.tif'),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES']);ds.GetRasterBand(1).WriteArray(np.array(expected['uniform-weighted-default.cog.tif']));ds=None
 with pytest.raises(f.FixtureError):f.verify(dest,list(f.CASES),expected)
