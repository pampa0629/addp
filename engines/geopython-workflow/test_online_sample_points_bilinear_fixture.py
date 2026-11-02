"""Actual GPKG oracle and accurate rejection tests; no Hosted claims."""
import copy
import importlib
import json
import math
from pathlib import Path
import shutil
import subprocess
import sys
import os
import pytest
from workflow_engine import execute_workflow,GeoPythonWorkflowRunner
from addp_common.workflow_runtime.errors import WorkflowExecutionError
f=importlib.import_module('business.scripts.online-raster-sample-points-bilinear-fixture')
m=importlib.import_module('scripts.test.raster-sample-points-bilinear-persistence-online')

def localize(dag,root,target):
    for task in dag['tasks']:
        p=task['params']
        if task['operator']=='load':
            source=root/p.pop('locator');p.update(connection_info={'mount_path':str(root)},path=source.name)
        elif task['operator']=='raster_load':
            source=root/p.pop('locator');fmt='tiff' if source.suffix=='.tif' else 'geopackage' if source.suffix=='.gpkg' else 'geojson'
            p['access_plan']={'schema_version':'addp.workflow.access-plan/v1','source':{'kind':'file','format':fmt,'access':{'method':'mounted_path','path':str(source)}}}
        elif task['operator']=='save':p.pop('target_parent_locator');p.pop('target_name');p.update(connection_info={'mount_path':str(target.parent)},path=target.name)

@pytest.fixture(scope='module')
def sample_sources(tmp_path_factory):
 root=tmp_path_factory.mktemp('sample-points');(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('raster-sample-points-bilinear-123-1')
 initial=f.seed(root);expected=f.oracle();(root/f.ORACLE).write_bytes(f.encoded(expected));previews={}
 for name in f.CASES:
  dag=m.definition({n:n for n in f.SOURCES},'parent',name);localize(dag,root,root/'outputs'/name)
  result=execute_workflow(dag);assert result['status']=='success',result
  previews[name]=json.loads(result['final_result'])
  proof=f.verify(root,list(previews),expected);assert proof['full_gpkg_checks']==len(previews)and proof['sources']==initial['sources']
 return root,initial,expected,previews

def test_sample_bilinear_hosted_sixteen_complete_gpkgs_and_raw_archive(sample_sources,tmp_path):
 root,initial,expected,previews=sample_sources;report=f.verify(root,list(f.CASES),expected)
 assert report['sources']==initial['sources']and report['full_gpkg_checks']==16
 archive=f.archive_files(root/'outputs',tmp_path,list(f.CASES),{},'raster-sample-points-bilinear-gpkgs')
 for name in f.CASES:
  m.validate_preview(previews[name],expected[name],name)
  assert(tmp_path/archive[name]['path']).read_bytes()==(root/'outputs'/name).read_bytes()
 assert expected['empty.gpkg']==[]
 assert{row['properties']['sample_status']for row in expected['bilinear-support.gpkg']}=={'ok','outside','nodata'}
 assert all(row['properties']['sample_status']=='nodata'for row in expected['all-nodata.gpkg'])
 assert all(row['properties']['sample_status']=='outside'for row in expected['all-outside.gpkg'])
 assert expected['int64.gpkg'][2]['properties']['sample_value']==float(2**53+4)
 assert expected['uint64.gpkg'][3]['properties']['sample_value']==float(2**60)
 assert expected['cross-block.gpkg'][0]['geometry_wkb']==expected['cross-block.gpkg'][3]['geometry_wkb']
 assert expected['cell-default.gpkg']==expected['cell-explicit.gpkg']
 assert [v['properties']['sample_value']for v in expected['bilinear-support.gpkg'][:4]]==[-4.,-2.,-3.5,-2.5]
 assert [v['properties']['sample_status']for v in expected['center-ulp.gpkg']]==['ok','nodata','nodata','nodata','nodata']
 for name in('int64.gpkg','uint64.gpkg'):
  assert expected[name][6]['properties']['sample_status']=='nodata'and expected[name][7]['properties']['sample_value']==8.

@pytest.mark.parametrize('problem',f.FAILURES)
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_sample_bilinear_hosted_fifty_two_exact_failures_no_output_or_workspace(sample_sources,tmp_path,monkeypatch,problem,language):
 import workflow_engine
 root,initial,expected,_=sample_sources
 dag=m.definition({n:n for n in f.SOURCES},'parent','bilinear-support.gpkg',invalid=problem,language=language);target=tmp_path/'failed.gpkg';localize(dag,root,target)
 workspaces=[];original=workflow_engine._execute_operator
 def track(name,params):
  if name=='raster_sample_points':workspaces.append(params['input_raster'].workspace)
  return original(name,params)
 monkeypatch.setattr(workflow_engine,'_execute_operator',track)
 with pytest.raises(WorkflowExecutionError)as error:GeoPythonWorkflowRunner(language=language).execute(dag)
 assert error.value.error_code=='EXECUTION_FAILED'and str(error.value)=='任务 sample 执行失败: '+m.MESSAGES[problem][language=='en']
 assert workspaces and all(not p.exists()for p in workspaces)and not target.exists()
 assert f.verify(root,list(f.CASES),expected)['sources']==initial['sources']

@pytest.mark.parametrize('fault',['value','one-ulp','status','geometry','null','property','schema','source-mask','sidecar','deleted'])
def test_sample_bilinear_hosted_real_corruption_rejected(sample_sources,tmp_path,fault):
 from osgeo import ogr,gdal
 import numpy as np
 original,_,expected,_=sample_sources;root=tmp_path/'changed';shutil.copytree(original,root)
 if fault=='source-mask':
  ds=gdal.Open(str(root/'values.tif'),gdal.GA_Update);ds.GetRasterBand(1).GetMaskBand().WriteArray(np.zeros((1,1),np.uint8),0,0);ds=None
 elif fault=='sidecar':(root/'outputs'/'unrequested.gpkg').write_bytes(b'bad')
 elif fault=='deleted':(root/'outputs'/'bilinear-support.gpkg').unlink()
 else:
  ds=ogr.Open(str(root/'outputs'/'bilinear-support.gpkg'),1);layer=ds.GetLayer(0)
  if fault=='schema':assert layer.CreateField(ogr.FieldDefn('unexpected',ogr.OFTString))==ogr.OGRERR_NONE
  else:
   feature=layer.GetNextFeature()
   if fault in ('value','one-ulp'):feature.SetField('sample_value',99. if fault=='value'else math.nextafter(feature.GetField('sample_value'),math.inf))
   elif fault=='status':feature.SetField('sample_status','outside')
   elif fault=='null':feature.SetFieldNull('sample_value')
   elif fault=='geometry':feature.SetGeometry(ogr.CreateGeometryFromWkt('POINT (0 0)'))
   else:feature.SetField('label','changed')
   assert layer.SetFeature(feature)==ogr.OGRERR_NONE;feature=None
  layer=ds=None
 with pytest.raises(f.FixtureError):f.verify(root,list(f.CASES),expected)

@pytest.mark.parametrize('fault',['changed','deleted','manifest','extra'])
def test_sample_bilinear_hosted_archive_never_repairs(sample_sources,tmp_path,fault):
 root,_,_,_=sample_sources;receipts=f.archive_files(root/'outputs',tmp_path,list(f.CASES),{},'raster-sample-points-bilinear-gpkgs');path=tmp_path/receipts['bilinear-support.gpkg']['path']
 if fault=='changed':path.chmod(0o644);path.write_bytes(path.read_bytes()+b' ')
 elif fault=='deleted':path.unlink()
 elif fault=='manifest':(tmp_path/'raster-sample-points-bilinear-gpkgs-manifest.json').write_text('{}')
 else:(path.parent/'extra.gpkg').write_bytes(b'extra')
 with pytest.raises(f.FixtureError):f.archive_files(root/'outputs',tmp_path,list(f.CASES),receipts,'raster-sample-points-bilinear-gpkgs')

def test_sample_bilinear_hosted_oracle_does_not_call_product_inverse_or_reader(monkeypatch):
 from operators import raster_compute
 def forbidden(*a,**k):raise AssertionError('product cannot create oracle')
 for name in ('raster_sample_points','_sample_point_groups','_blocks'):monkeypatch.setattr(raster_compute,name,forbidden)
 assert len(f.oracle())==16

def test_sample_bilinear_hosted_worker_stdin_scope_poison_and_cleanup(tmp_path):
 import hashlib
 root=tmp_path/'source';root.mkdir();(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('raster-sample-points-bilinear-123-1')
 script=Path(f.__file__).read_bytes();sha=hashlib.sha256(script).hexdigest();env=os.environ.copy();env.pop('PYTHONPATH',None)
 scratch=tmp_path/'runtime-tmp';scratch.mkdir();env['TMPDIR']=str(scratch)
 def call(action,*names):return subprocess.run([sys.executable,'-','--worker',action,str(root),*names],input=script,cwd=tmp_path,env=env,capture_output=True)
 assert call('seed').returncode==0
 result=call('oracle',sha);assert result.returncode==0,result.stderr.decode();anchor=json.loads(result.stdout)['oracle_sha256']
 assert call('verify',anchor,sha).returncode==0
 assert call('verify',anchor,'b'*64).returncode!=0
 (root/'.addp-online-owner').write_text('raster-sample-points-bilinear-124-1')
 assert call('verify',anchor,sha).returncode!=0
 (root/'.addp-online-owner').write_text('raster-sample-points-bilinear-123-1')
 (root/f.ORACLE).chmod(0o644);assert call('verify',anchor,sha).returncode!=0
 (root/f.ORACLE).write_bytes(b'{}');assert call('verify',anchor,sha).returncode!=0
 assert json.loads(call('cleanup-oracle').stdout)['oracle_cache_removed']is True

@pytest.mark.parametrize('u,v,expected',[(0.,0.,(0,0)),(1.,2.,(2,1)),(5.,2.,None),(1.,4.,None),(-.25,1.,None),(4.999,3.999,(3,4))])
def test_sample_bilinear_hosted_independent_half_open_cell_witness(u,v,expected):
 from shapely.geometry import Point
 for grid in (f.GRID,(100.,-2.,0.,200.,0.,3.),(100.,2.,.5,200.,.25,-3.)):
  assert f.cell(Point(*f.xy(grid,u,v)),grid,5,4,u,v)==expected

@pytest.mark.parametrize('fault',['left-neighbor','right-neighbor','drop-alpha','scale-value','fill-outside','overwrite-null'])
def test_sample_bilinear_hosted_semantic_mutations_cannot_change_frozen_expectations(sample_sources,tmp_path,fault):
 from osgeo import ogr
 original,_,expected,_=sample_sources;root=tmp_path/'mutated';shutil.copytree(original,root)
 name='boundaries.gpkg'if 'neighbor'in fault else 'coverage.gpkg'if fault=='drop-alpha'else 'bilinear-support.gpkg'
 rows=expected[name];index=2 if fault=='drop-alpha'else next(i for i,row in enumerate(rows)if row['properties']['sample_status']==('outside'if fault=='fill-outside'else'nodata'if fault in ('drop-alpha','overwrite-null')else'ok'))
 ds=ogr.Open(str(root/'outputs'/name),1);layer=ds.GetLayer(0);feature=list(layer)[index]
 value=feature.GetField('sample_value')
 if fault in ('drop-alpha','fill-outside','overwrite-null'):feature.SetField('sample_value',0.);feature.SetField('sample_status','ok')
 else:feature.SetField('sample_value',value+(-1 if fault=='left-neighbor'else 1)if'neighbor'in fault else value*17+500)
 assert layer.SetFeature(feature)==ogr.OGRERR_NONE;feature=None;layer=ds=None
 with pytest.raises(f.FixtureError):f.verify(root,list(f.CASES),expected)

def test_sample_bilinear_hosted_real_host_archive_history_reuses_frozen_receipt(tmp_path,monkeypatch):
 root=tmp_path/'root';root.mkdir();(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('raster-sample-points-bilinear-123-1')
 secret=tmp_path/'secret';secret.mkdir();artifact=tmp_path/'artifact';artifact.mkdir()
 scratch=tmp_path/'runtime-tmp';scratch.mkdir();monkeypatch.setattr(f.tempfile,'gettempdir',lambda:str(scratch))
 monkeypatch.setenv('ADDP_ONLINE_ARTIFACT_DIR',str(artifact));monkeypatch.setattr(f,'guard',lambda:secret);monkeypatch.setattr(f,'owned_root',lambda _:root)
 monkeypatch.setattr(f,'runtime_worker',lambda action,root,names:f.worker(action,root,names))
 monkeypatch.setattr(sys,'argv',['fixture','seed']);f.main('seed')
 monkeypatch.setattr(sys,'argv',['fixture','oracle']);baseline=f.main('oracle')
 monkeypatch.setenv('ADDP_ONLINE_RASTER_SAMPLE_POINTS_BILINEAR_ORACLE_SHA256',baseline['oracle_sha256'])
 monkeypatch.setattr(sys,'argv',['fixture','verify']);again=f.main('verify')
 assert baseline['archived_oracle']==again['archived_oracle']
 assert baseline['archived_sources']==again['archived_sources']
 residual=scratch/'addp-raster-leaked';residual.mkdir()
 with pytest.raises(f.FixtureError,match='workspace residual'):f.main('verify')
 residual.rmdir()
 cached=artifact/again['archived_oracle'][f.ORACLE]['path'];cached.chmod(0o644);cached.write_bytes(cached.read_bytes()+b' ')
 with pytest.raises(f.FixtureError,match='archived bytes changed'):f.main('verify')

@pytest.mark.parametrize('fault',['zero-weight','epsilon','renormalize','nearest','mask-priority'])
def test_sample_bilinear_hosted_new_semantics_cannot_weaken_frozen_oracle(sample_sources,tmp_path,fault):
 from osgeo import ogr
 original,_,expected,_=sample_sources;root=tmp_path/'mutated-bilinear';shutil.copytree(original,root)
 name='center-ulp.gpkg'if fault in('zero-weight','epsilon')else'int64.gpkg'if fault=='mask-priority'else'bilinear-support.gpkg'
 index=0 if fault=='zero-weight'else 1 if fault in('epsilon','nearest')else 6
 ds=ogr.Open(str(root/'outputs'/name),1);layer=ds.GetLayer(0);feature=list(layer)[index]
 if fault=='zero-weight':feature.SetFieldNull('sample_value');feature.SetField('sample_status','nodata')
 else:feature.SetField('sample_value',7. if fault=='epsilon'else 8. if fault in('mask-priority','renormalize')else 0.);feature.SetField('sample_status','ok')
 assert layer.SetFeature(feature)==ogr.OGRERR_NONE;feature=None;layer=ds=None
 with pytest.raises(f.FixtureError):f.verify(root,list(f.CASES),expected)
