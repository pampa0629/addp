"""Real GDAL/GPKG regressions for the independent D8 stream vector Hosted fixture."""
import importlib
import importlib.util
import json
import shutil
from pathlib import Path
import numpy as np
from osgeo import gdal,ogr
import pytest

ROOT=Path(__file__).resolve().parents[2]
spec=importlib.util.spec_from_file_location('online_stream_vector_fixture',ROOT/'business/scripts/online-raster-stream-to-vector-d8-fixture.py')
fixture=importlib.util.module_from_spec(spec);spec.loader.exec_module(fixture)
scene=importlib.import_module('scripts.test.raster-stream-to-vector-d8-persistence-online')


def local_definition(root,name,problem=None,language='zh-cn'):
    definition=scene.definition({n:n for n in fixture.SOURCES},'parent',name,invalid=problem,language=language)
    target=None
    for task in definition['tasks']:
        params=task['params']
        if task['operator']=='raster_load':
            path=root/params.pop('locator');params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','source':{
                'kind':'file','format':'tiff','access':{'method':'mounted_path','path':str(path)}}}
        elif task['operator']=='save':
            params.pop('target_parent_locator');name=params.pop('target_name');target=root/'outputs'/name
            params.update(path=name,connection_info={'mount_path':str(root/'outputs')})
    return definition,target


@pytest.fixture(scope='module')
def stream_to_vector_d8_persistence(tmp_path_factory):
    from workflow_engine import execute_workflow
    root=tmp_path_factory.mktemp('stream_to_vector_d8-export');(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('stream-to-vector-d8-123-1')
    initial=fixture.seed(root);prior={}
    for index,name in enumerate(fixture.TARGETS):
        definition,target=local_definition(root,name);result=execute_workflow(definition)
        assert result['status']=='success',result
        assert target.stat().st_size>0 and 'artifact_type' not in json.loads(result['final_result'])
        facts=fixture.verify(root,list(fixture.TARGETS[:index+1]));assert facts['sources']==initial['sources']
        assert all(facts['targets'][n]==fact for n,fact in prior.items());prior=facts['targets'].copy()
    return root,initial


def test_stream_to_vector_d8_persistence_real_gdal_eight_gpkg_chains_and_complete_edges(stream_to_vector_d8_persistence):
    root,initial=stream_to_vector_d8_persistence;facts=fixture.verify(root,list(fixture.TARGETS))
    assert facts['sources']==initial['sources'] and len(facts['targets'])==8
    for name,fact in facts['targets'].items():
        assert all(fact[key] is True for key in scene.REQUIRED_PHYSICAL)
        assert fact['schema']==[list(v) for v in fixture.SCHEMA] and fact['crs']=='EPSG:'+str(fixture.epsg(name))
        assert fact['vertex_count']==fact['edge_count']+fact['feature_count']
        if name in ('empty.gpkg','invalid.gpkg'):assert fact['feature_count']==fact['edge_count']==0
        else:assert fact['feature_count']>0 and fact['edge_count']>fact['feature_count']
    truth,edges,skipped=fixture.expected_lines('metre.gpkg');assert 2 in skipped
    # Handwritten edge checks cover the terminal-only confluence and the valid
    # one-cell segment between adjacent confluences, across the 512 seam.
    assert edges[(508,4)]==edges[(509,4)]==(508,5)
    assert edges[(510,8)]==(510,9) and edges[(510,9)]==(510,10)
    assert (508,5) not in edges
    assert any(len(line['cells'])==2 and line['cells']==[(510,8),(510,9)] for line in truth)
    assert any(len(line['cells'])==419 and line['cells'][0]==(508,100) for line in truth)
    assert edges[(509,20)]==(508,21) and edges[(509,21)]==(508,20)
    feet=fixture.expected_lines('foot.gpkg')[0];survey=fixture.expected_lines('survey.gpkg')[0]
    for metre,foot,us in zip(truth,feet,survey):
        assert metre['fields'][:5]==foot['fields'][:5]==us['fields'][:5]
        assert metre['coordinates']==foot['coordinates']==us['coordinates']
        assert foot['fields'][-1]==pytest.approx(metre['fields'][-1]*.3048,rel=1e-12)
        assert us['fields'][-1]==pytest.approx(metre['fields'][-1]*1200/3937,rel=1e-12)
    (root.parent/'stream-to-vector-native-facts.json').write_text(json.dumps(facts))


@pytest.mark.parametrize('problem',scene.FAILURES)
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_stream_to_vector_d8_persistence_twenty_precise_failures_no_publication(stream_to_vector_d8_persistence,tmp_path,problem,language):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime.errors import WorkflowExecutionError
    original,initial=stream_to_vector_d8_persistence;root=tmp_path/'failed';shutil.copytree(original,root)
    definition,target=local_definition(root,'metre.gpkg',problem,language)
    with pytest.raises(WorkflowExecutionError) as caught:GeoPythonWorkflowRunner(language=language).execute(definition)
    assert caught.value.error_code=='EXECUTION_FAILED'
    assert str(caught.value)=='任务 lines 执行失败: '+scene.MESSAGES[problem][language=='en']
    assert not target.exists() and fixture.verify(root,list(fixture.TARGETS))['sources']==initial['sources']


@pytest.mark.parametrize('fault',['id','from_row','to_col','length','length_nan','reverse','coordinate','missing_vertex','multi','z','null','missing_feature','duplicate_feature','field_type','field_missing','crs','layer','sidecar','source_sidecar','missing_owner','missing','extra','empty_feature'])
def test_stream_to_vector_d8_persistence_oracle_rejects_corruption(stream_to_vector_d8_persistence,tmp_path,fault):
    original,_=stream_to_vector_d8_persistence;root=tmp_path/'changed';shutil.copytree(original,root);path=root/'outputs'/('empty.gpkg' if fault=='empty_feature' else 'metre.gpkg')
    if fault in ('sidecar','source_sidecar','extra','missing_owner','missing'):
        if fault=='sidecar':Path(str(path)+'-journal').write_text('unexpected')
        elif fault=='source_sidecar':(root/'directions.tif.aux.xml').write_text('unexpected')
        elif fault=='extra':(root/'outputs'/'foreign.gpkg').write_text('unexpected')
        else:(root/'.addp-online-owner' if fault=='missing_owner' else path).unlink()
    elif fault in ('field_type','crs'):
        import geopandas as gpd
        frame=gpd.read_file(path)
        if fault=='field_type':frame['link_id']=frame.link_id.astype('float64')
        else:frame=frame.set_crs('EPSG:4326',allow_override=True)
        replacement=tmp_path/'replacement.gpkg';frame.to_file(replacement,driver='GPKG',layer='metre');replacement.replace(path)
    else:
        data=ogr.Open(str(path),1);layer=data.GetLayer(0);feature=layer.GetNextFeature()
        if fault=='missing_vertex':
            while feature.GetGeometryRef().GetPointCount()<3:feature=layer.GetNextFeature()
        if fault=='layer':data.CreateLayer('foreign',geom_type=ogr.wkbLineString)
        elif fault=='field_missing':layer.DeleteField(0)
        elif fault=='missing_feature':layer.DeleteFeature(feature.GetFID())
        elif fault in ('duplicate_feature','empty_feature'):
            if feature is None:
                feature=ogr.Feature(layer.GetLayerDefn())
                for field in fixture.FIELDS:feature.SetField(field,1 if field!='length_m' else 1.)
                feature.SetGeometry(ogr.CreateGeometryFromWkt('LINESTRING(0 0,1 1)'))
            feature.SetFID(-1);layer.CreateFeature(feature)
        elif fault in ('id','from_row','to_col','length','length_nan'):
            field={'id':'link_id','length':'length_m','length_nan':'length_m'}.get(fault,fault)
            feature.SetField(field,float('nan') if fault=='length_nan' else feature.GetField(field)+1);layer.SetFeature(feature)
        elif fault=='null':feature.SetGeometry(None);layer.SetFeature(feature)
        else:
            geometry=feature.GetGeometryRef();points=[geometry.GetPoint_2D(i) for i in range(geometry.GetPointCount())]
            if fault=='reverse':points.reverse()
            elif fault=='coordinate':points[-1]=(points[-1][0]+.01,points[-1][1])
            elif fault=='missing_vertex':points.pop(1 if len(points)>2 else -1)
            changed=ogr.Geometry(ogr.wkbLineString25D if fault=='z' else ogr.wkbLineString)
            for x,y in points:
                if fault=='z':changed.AddPoint(x,y,1)
                else:changed.AddPoint_2D(x,y)
            if fault=='multi':multi=ogr.Geometry(ogr.wkbMultiLineString);multi.AddGeometry(changed);changed=multi
            feature.SetGeometry(changed);layer.SetFeature(feature)
        feature=layer=data=None
    with pytest.raises(fixture.FixtureError):fixture.verify(root,list(fixture.TARGETS))


def test_stream_to_vector_d8_fixture_exact_nodata_bands_and_source_immutability(stream_to_vector_d8_persistence,tmp_path,monkeypatch):
    root,initial=stream_to_vector_d8_persistence
    for name in ('directions.tif','streams.tif'):
        ds=gdal.Open(str(root/name));assert ds.RasterCount==4
        assert np.all(ds.GetRasterBand(1).ReadAsArray()==.25) and np.all(ds.GetRasterBand(3).ReadAsArray()==-9999)
        assert ds.GetRasterBand(4).ReadAsArray()[511,515]==128 and ds.GetRasterBand(1).GetMaskBand().ReadAsArray()[514,510]==0;ds=None
    ds=gdal.Open(str(root/'code.tif'));band=ds.GetRasterBand(2)
    assert band.DataType==gdal.GDT_UInt64 and band.GetNoDataValueAsUInt64()==2**53 and int(band.ReadAsArray()[509,100])==2**53+1;band=ds=None
    with pytest.raises(fixture.FixtureError):fixture.seed(root)
    copied=tmp_path/'source';shutil.copytree(root,copied);secret=tmp_path/'secret';secret.mkdir();(secret/'stream-to-vector-d8-source.json').write_text(json.dumps(initial))
    monkeypatch.setattr(fixture,'guard',lambda:secret);monkeypatch.setattr(fixture,'owned_root',lambda _:copied)
    monkeypatch.setattr(fixture,'runtime_worker',lambda action,owned,names:fixture.verify(owned,names));monkeypatch.setattr(fixture.sys,'argv',['fixture','verify',*fixture.TARGETS])
    assert fixture.main('verify')['sources']==initial['sources']
    with (copied/'streams.tif').open('ab') as stream:stream.write(b'changed source bytes')
    with pytest.raises(fixture.FixtureError,match='source file changed'):fixture.main('verify')


def test_stream_to_vector_d8_fresh_oracle_preserves_prior_outputs(stream_to_vector_d8_persistence):
    root,initial=stream_to_vector_d8_persistence
    for method in (fixture.expected_directions,fixture.expected_streams,fixture.expected_lines):method.cache_clear()
    report=fixture.verify(root,list(fixture.TARGETS));assert report==fixture.verify(root,list(fixture.TARGETS)) and report['sources']==initial['sources']
