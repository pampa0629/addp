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
f=importlib.import_module('business.scripts.online-vector-zonal-weighted-fixture')
m=importlib.import_module('scripts.test.vector-zonal-weighted-persistence-online')

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
def vector_zonal_sources(tmp_path_factory):
    root=tmp_path_factory.mktemp('vector-zonal-weighted');(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('owned')
    initial=f.seed(root);expected=f.oracle();(root/f.ORACLE).write_bytes(f.encoded(expected));previews={}
    for name in f.CASES:
        dag=m.definition({n:n for n in f.SOURCES},'parent',name);localize(dag,root,root/'outputs'/name)
        result=execute_workflow(dag);assert result['status']=='success',result
        previews[name]=json.loads(result['final_result'])
    return root,initial,expected,previews

def test_vector_zonal_weighted_sixteen_real_gpkgs_complete_independent_oracle_and_raw_archive(vector_zonal_sources,tmp_path):
    root,initial,expected,previews=vector_zonal_sources;report=f.verify(root,list(f.CASES),expected)
    assert report['sources']==initial['sources'] and report['full_gpkg_checks']==16
    archive=f.archive_files(root/'outputs',tmp_path,list(f.CASES),{},'vector-zonal-weighted-gpkgs')
    for name in f.CASES:
        m.validate_preview(previews[name],expected[name],name)
        assert (tmp_path/archive[name]['path']).read_bytes()==(root/'outputs'/name).read_bytes()
    assert expected['empty-zones.gpkg']==[] and all(r['properties']['zonal_weighted_mean']is None for r in expected['invalid-values.gpkg'])
    assert len(f.CASES)==16 and len(f.FAILURES)==43 and sum(c[2]is not None for c in f.CASES.values())==14
    assert expected['weighted-touched.gpkg'][5]['properties']['zonal_total_count']>0 and expected['weighted-center.gpkg'][5]['properties']['zonal_total_count']==0
    for row in expected['zero.gpkg']:
        props=row['properties'];assert [props['zonal_'+k]for k in f.WEIGHT_COLUMNS]==([0.,0.,None]if props['zonal_valid_count']else[None]*3)
    assert any(r['properties']['zonal_weighted_sum']<0 for r in expected['weighted-center.gpkg']if r['properties']['zonal_weighted_sum']is not None)

@pytest.mark.parametrize('problem',f.FAILURES)
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_vector_zonal_weighted_eighty_six_exact_submitted_failures_and_no_output(vector_zonal_sources,tmp_path,monkeypatch,problem,language):
    import workflow_engine
    root,initial,expected,_=vector_zonal_sources
    dag=m.definition({n:n for n in f.SOURCES},'parent','weighted-center.gpkg',invalid=problem,language=language);target=tmp_path/'failed.gpkg';localize(dag,root,target)
    workspaces=[];original=workflow_engine._execute_operator
    def track(name,params):
        if name=='raster_vector_zonal_statistics':workspaces.append(params['input_raster'].workspace)
        return original(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',track)
    with pytest.raises(WorkflowExecutionError) as error:GeoPythonWorkflowRunner(language=language).execute(dag)
    assert error.value.error_code=='EXECUTION_FAILED' and str(error.value)=='任务 analysis 执行失败: '+m.MESSAGES[problem][language=='en']
    assert workspaces and all(not p.exists() for p in workspaces) and not target.exists()
    assert f.verify(root,list(f.CASES),expected)['sources']==initial['sources']

@pytest.mark.parametrize('fault',['weighted-mean','one-ulp','geometry','count','null','property','schema','source-mask','sidecar'])
def test_vector_zonal_weighted_real_physical_corruption_is_rejected(vector_zonal_sources,tmp_path,fault):
    from osgeo import ogr,gdal
    import numpy as np
    original,_,expected,_=vector_zonal_sources;root=tmp_path/'changed';shutil.copytree(original,root)
    if fault=='source-mask':
        ds=gdal.Open(str(root/'values.tif'),gdal.GA_Update);ds.GetRasterBand(1).GetMaskBand().WriteArray(np.zeros((1,1),np.uint8),10,10);ds=None
    elif fault=='sidecar':(root/'outputs'/'unrequested.gpkg').write_bytes(b'bad')
    else:
        ds=ogr.Open(str(root/'outputs'/'weighted-center.gpkg'),1);layer=ds.GetLayer(0)
        if fault=='schema':
            # CreateField invalidates Features using the old layer definition.
            # This branch must not fetch a Feature, including for cleanup.
            before_schema=[(layer.GetLayerDefn().GetFieldDefn(i).GetName(),layer.GetLayerDefn().GetFieldDefn(i).GetType()) for i in range(layer.GetLayerDefn().GetFieldCount())]
            assert layer.CreateField(ogr.FieldDefn('unexpected',ogr.OFTString))==ogr.OGRERR_NONE
        else:
            feature=layer.GetNextFeature();assert feature is not None
            if fault in ('weighted-mean','one-ulp'):
                current=feature.GetField('zonal_weighted_mean');assert current is not None
                feature.SetField('zonal_weighted_mean',current+1 if fault=='weighted-mean'else math.nextafter(current,math.inf))
            elif fault=='geometry':feature.SetGeometry(ogr.CreateGeometryFromWkt('POLYGON ((0 0,1 0,1 1,0 0))'))
            elif fault=='count':feature.SetField('zonal_valid_count',99)
            elif fault=='null':feature.SetFieldNull('zonal_weighted_mean')
            else:feature.SetField('label','changed')
            assert layer.SetFeature(feature)==ogr.OGRERR_NONE
            feature=None
        layer=ds=None
        if fault=='schema':
            reopened=ogr.Open(str(root/'outputs'/'weighted-center.gpkg'),0);layer=reopened.GetLayer(0)
            actual_schema=[(layer.GetLayerDefn().GetFieldDefn(i).GetName(),layer.GetLayerDefn().GetFieldDefn(i).GetType()) for i in range(layer.GetLayerDefn().GetFieldCount())]
            assert actual_schema==before_schema+[('unexpected',ogr.OFTString)]
            layer=reopened=None
    reason={'weighted-mean':'exact weighted','one-ulp':'exact weighted','geometry':'full geometry','count':'exact count','null':'nonnumeric or nonfinite statistic','property':'original property','schema':'field names/order/types','source-mask':'complete source pixels','sidecar':'unexpected output'}[fault]
    with pytest.raises(f.FixtureError,match=reason):f.verify(root,list(f.CASES),expected)


def test_vector_zonal_weighted_schema_change_has_no_live_feature(vector_zonal_sources,tmp_path,monkeypatch):
    """OGR schema changes invalidate live Features; reject before native damage."""
    import weakref
    from osgeo import ogr
    original_next=ogr.Layer.GetNextFeature;original_create=ogr.Layer.CreateField
    observed=[];changes=[]
    def next_feature(layer,*args,**kwargs):
        feature=original_next(layer,*args,**kwargs)
        if feature is not None:observed.append((int(layer.this),weakref.ref(feature)))
        return feature
    def create_field(layer,definition,*args,**kwargs):
        live=sum(reference() is not None for owner,reference in observed if owner==int(layer.this))
        assert live==0,'schema mutation would invalidate a live OGR Feature'
        changes.append((definition.GetName(),definition.GetType(),live))
        return original_create(layer,definition,*args,**kwargs)
    monkeypatch.setattr(ogr.Layer,'GetNextFeature',next_feature)
    monkeypatch.setattr(ogr.Layer,'CreateField',create_field)
    test_vector_zonal_weighted_real_physical_corruption_is_rejected(vector_zonal_sources,tmp_path,'schema')
    assert changes==[('unexpected',ogr.OFTString,0)]


@pytest.mark.parametrize('fault',['changed','deleted','manifest','extra'])
def test_vector_zonal_weighted_gpkg_archive_never_repairs_or_overwrites(vector_zonal_sources,tmp_path,fault):
    root,_,_,_=vector_zonal_sources;receipts=f.archive_files(root/'outputs',tmp_path,list(f.CASES),{},'vector-zonal-weighted-gpkgs');path=tmp_path/receipts['weighted-center.gpkg']['path']
    if fault=='changed':path.chmod(0o644);path.write_bytes(path.read_bytes()+b' ')
    elif fault=='deleted':path.unlink()
    elif fault=='manifest':(tmp_path/'vector-zonal-weighted-gpkgs-manifest.json').write_text('{}')
    else:(path.parent/'extra.gpkg').write_bytes(b'extra')
    with pytest.raises(f.FixtureError):f.archive_files(root/'outputs',tmp_path,list(f.CASES),receipts,'vector-zonal-weighted-gpkgs')

def test_vector_zonal_weighted_worker_stdin_frozen_oracle_poison_and_cleanup(tmp_path):
    root=tmp_path/'source';root.mkdir();(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('owned')
    script=Path(f.__file__).read_bytes();env=os.environ.copy();env.pop('PYTHONPATH',None)
    scratch=tmp_path/'runtime-tmp';scratch.mkdir();env['TMPDIR']=str(scratch)
    def call(action,*names):return subprocess.run([sys.executable,'-','--worker',action,str(root),*names],input=script,cwd=tmp_path,env=env,capture_output=True)
    assert call('seed').returncode==0
    result=call('oracle');assert result.returncode==0,result.stderr.decode();anchor=json.loads(result.stdout)['oracle_sha256']
    assert call('verify',anchor).returncode==0
    (root/f.ORACLE).chmod(0o644);(root/f.ORACLE).write_bytes(b'{}')
    result=call('verify',anchor);assert result.returncode!=0 and b'oracle cache hash changed' in result.stderr
    assert json.loads(call('cleanup-oracle').stdout)['oracle_cache_removed'] is True


@pytest.mark.parametrize('fault',['normalize-sum','drop-scale','zero-as-null','empty-as-zero','ignore-weights','ones-mean','ones-and-original-mean','source-vector','source-symlink','target-symlink','directory-symlink'])
def test_vector_zonal_weighted_independent_oracle_rejects_wrong_semantics_and_links(vector_zonal_sources,tmp_path,fault):
    from osgeo import ogr
    original,_,expected,_=vector_zonal_sources;root=tmp_path/'changed';shutil.copytree(original,root)
    name={'drop-scale':'scaled.gpkg','zero-as-null':'zero.gpkg','empty-as-zero':'invalid-values.gpkg','ones-mean':'ones.gpkg','ones-and-original-mean':'ones.gpkg'}.get(fault,'weighted-center.gpkg');path=root/'outputs'/name
    if fault=='source-vector':
        ds=ogr.Open(str(root/'zones.geojson'),1);layer=ds.GetLayer(0);feature=layer.GetNextFeature();feature.SetField('label','changed source');assert layer.SetFeature(feature)==ogr.OGRERR_NONE;feature=layer=ds=None
    elif fault.endswith('symlink'):
        chosen=root/'values.tif'if fault=='source-symlink'else root/'outputs'if fault=='directory-symlink'else path
        moved=tmp_path/'moved';chosen.rename(moved);chosen.symlink_to(moved,target_is_directory=fault=='directory-symlink')
    else:
        ds=ogr.Open(str(path),1);layer=ds.GetLayer(0);feature=layer.GetNextFeature();assert feature is not None
        if fault=='normalize-sum':feature.SetField('zonal_weighted_sum',feature.GetField('zonal_weighted_mean'))
        elif fault=='drop-scale':feature.SetField('zonal_weight_sum',feature.GetField('zonal_weight_sum')/8)
        elif fault=='zero-as-null':feature.SetFieldNull('zonal_weight_sum')
        elif fault=='empty-as-zero':feature.SetField('zonal_weight_sum',0.)
        elif fault in('ones-mean','ones-and-original-mean'):
            value=math.nextafter(feature.GetField('zonal_mean'),math.inf);feature.SetField('zonal_weighted_mean',value)
            if fault=='ones-and-original-mean':feature.SetField('zonal_mean',value)
        else:feature.SetField('zonal_weighted_sum',feature.GetField('zonal_sum'))
        assert layer.SetFeature(feature)==ogr.OGRERR_NONE;feature=layer=ds=None
    with pytest.raises(f.FixtureError):f.verify(root,list(f.CASES),expected)
