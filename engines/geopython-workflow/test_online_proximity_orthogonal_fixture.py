"""Real GDAL rectangular proximity, independent complete oracle, failure and archive proof."""
import importlib,json,os,shutil,subprocess,sys
from pathlib import Path
import numpy as np
from osgeo import gdal
import pytest
from workflow_engine import execute_workflow,GeoPythonWorkflowRunner
from addp_common.workflow_runtime.errors import WorkflowExecutionError
sys.path.insert(0,str(Path(__file__).resolve().parents[2]))
f=importlib.import_module('business.scripts.online-raster-proximity-orthogonal-fixture')
m=importlib.import_module('scripts.test.raster-proximity-orthogonal-persistence-online')
helper=importlib.import_module('test_online_focal_percentile_fixture')

@pytest.fixture(scope='module')
def proximity_orthogonal_scene(tmp_path_factory,request):
    root=tmp_path_factory.mktemp('proximity-orthogonal-export');(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('proximity-orthogonal-123-1');initial=f.seed(root)
    # Freeze expected facts before executing the product.
    control=helper.cache_for(f,root,request)
    for name in f.TARGETS:
        result=execute_workflow(helper.native_definition(m,f,root,name));assert result['status']=='success',result
        assert json.loads(result['final_result'])['size_bytes']==(root/'outputs'/name).stat().st_size>0
    return root,initial,control


def test_proximity_orthogonal_hosted_eighteen_real_cogs_full_pixels_masks_units_and_raw_archive(proximity_orthogonal_scene,tmp_path):
    root,initial,control=proximity_orthogonal_scene;report=f.verify(root,list(f.TARGETS),control)
    assert report['sources']==initial['sources']and report['full_source_pixels_verified']is True and len(report['targets'])==18
    archived=f.archive_cogs(root,tmp_path,report,{});expectations=f.archive_expectations(root,tmp_path,{})
    for name,fact in report['targets'].items():
        assert fact['full_pixel_and_mask_verification']is True and fact['distance_unit']=='metre'and fact['overview_count']==m.overview_count(name)
        assert(tmp_path/archived[name]['path']).read_bytes()==(root/'outputs'/name).read_bytes()
        np.testing.assert_array_equal(np.load(tmp_path/expectations[name+'.npy']['path'],allow_pickle=False),f.expected_pixels(name))
    assert f.expected_pixels('wide.cog.tif')[0,0]==4. and f.expected_pixels('tall.cog.tif')[0,0]==10.
    assert f.expected_pixels('cross-block.cog.tif').shape==(515,519)
    assert all(f.expected_pixels(n+'.cog.tif')[0,0]==2.5 for n in ('rotate90','rotate180','rotate270'))
    assert f.expected_pixels('oblique-wide.cog.tif')[0,0]==10.
    assert all(report['targets'][n]['overview_count']==0 for n in f.TARGETS if n!='cross-block.cog.tif')

@pytest.mark.parametrize('problem',f.FAILURES)
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_proximity_orthogonal_hosted_sixty_exact_submitted_failures_without_publication(proximity_orthogonal_scene,tmp_path,monkeypatch,problem,language):
    import workflow_engine
    root,initial,control=proximity_orthogonal_scene;target=tmp_path/'failed.cog.tif';dag=helper.native_definition(m,f,root,'wide.cog.tif',invalid=problem,language=language,target=target)
    workspaces=[];original=workflow_engine._execute_operator
    def track(name,params):
        if name=='raster_proximity':workspaces.append(params['input_raster'].workspace)
        return original(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',track)
    with pytest.raises(WorkflowExecutionError)as error:GeoPythonWorkflowRunner(language=language).execute(dag)
    assert str(error.value)=='任务 distance 执行失败: '+m.MESSAGES[problem][language=='en']
    assert not target.exists()and workspaces and all(not p.exists()for p in workspaces)
    assert f.verify(root,list(f.TARGETS),control)['sources']==initial['sources']

@pytest.mark.parametrize('fault',['value','one-ulp','mask','grid','crs','nodata','unit','scale','overview','sidecar','missing','source-mask'])
def test_proximity_orthogonal_hosted_real_physical_corruption_is_rejected(proximity_orthogonal_scene,tmp_path,request,fault):
    original,_,_=proximity_orthogonal_scene;root=tmp_path/'changed';shutil.copytree(original,root);control=helper.cache_for(f,root,request);path=root/'outputs'/'cross-block.cog.tif'
    if fault=='sidecar':(root/'outputs'/'extra.tif').write_bytes(b'extra')
    elif fault=='missing':path.unlink()
    elif fault=='source-mask':
        ds=gdal.Open(str(root/'wide.tif'),gdal.GA_Update);ds.GetRasterBand(1).GetMaskBand().Fill(0);ds=None
    else:
        ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES']);band=ds.GetRasterBand(1)
        if fault in('value','one-ulp'):
            wanted=f.expected_pixels('cross-block.cog.tif')[60,60];band.WriteArray(np.array([[np.nextafter(wanted,np.inf)if fault=='one-ulp'else 123.]]),60,60)
        elif fault=='mask':
            with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
            band.GetMaskBand().Fill(255)
        elif fault=='grid':ds.SetGeoTransform((1,1,0,1,0,-1))
        elif fault=='crs':ds.SetProjection('')
        elif fault=='nodata':band.SetNoDataValue(-999.)
        elif fault=='unit':band.SetUnitType('foot')
        elif fault=='scale':band.SetScale(2.)
        else:band.GetOverview(0).Fill(99.)
        band=ds=None
    with pytest.raises(f.FixtureError):f.verify(root,list(f.TARGETS),control)

@pytest.mark.parametrize('fault',['bytes','deleted','directory','manifest','extra','symlink'])
def test_proximity_orthogonal_hosted_original_cog_archive_never_repairs_or_overwrites(proximity_orthogonal_scene,tmp_path,fault):
    root,_,control=proximity_orthogonal_scene;report=f.verify(root,list(f.TARGETS),control);previous=f.archive_cogs(root,tmp_path,report,{})
    path=tmp_path/'proximity_orthogonal-cogs'/'wide.cog.tif'
    if fault=='bytes':path.chmod(0o644);path.write_bytes(b'changed')
    elif fault=='deleted':path.unlink()
    elif fault=='directory':shutil.rmtree(path.parent);(tmp_path/'proximity_orthogonal-cogs-manifest.json').unlink()
    elif fault=='manifest':(tmp_path/'proximity_orthogonal-cogs-manifest.json').write_text('{}')
    elif fault=='extra':(path.parent/'extra.tif').write_bytes(b'extra')
    else:path.unlink();path.symlink_to(root/'outputs'/'wide.cog.tif')
    with pytest.raises(f.FixtureError):f.archive_cogs(root,tmp_path,report,previous)

@pytest.mark.parametrize('fault',['bytes','missing','extra','scope','shape'])
def test_proximity_orthogonal_hosted_frozen_cache_rejects_hash_shape_inventory_or_run_change(proximity_orthogonal_scene,tmp_path,request,fault):
    original,_,_=proximity_orthogonal_scene;root=tmp_path/'cache';shutil.copytree(original,root);control=helper.cache_for(f,root,request);directory=f.oracle_path(control['scope']);path=directory/'wide.cog.tif.npy'
    if fault=='bytes':path.chmod(0o600);path.write_bytes(path.read_bytes()+b'x')
    elif fault=='missing':path.unlink()
    elif fault=='extra':(directory/'unexpected.npy').write_bytes(b'x')
    elif fault=='scope':control['scope']['run_id']='proximity-orthogonal-999-1'
    else:
        path.chmod(0o600)
        with path.open('wb') as out:np.save(out,np.array([[1.]]))
        path.chmod(0o400);control['arrays']['wide.cog.tif']=f.digest(path)
    with pytest.raises(f.FixtureError):f.verify(root,list(f.TARGETS),control)

def test_proximity_orthogonal_hosted_stdin_worker_has_no_repository_dependency_and_exact_cleanup(proximity_orthogonal_scene,tmp_path):
    original,initial,control=proximity_orthogonal_scene;root=tmp_path/'child';shutil.copytree(original,root);control={'scope':{**control['scope'],'source_root':str(root.resolve())}};scratch=tmp_path/'scratch';scratch.mkdir();cwd=tmp_path/'empty';cwd.mkdir()
    env={**os.environ,'PYTHONPATH':'','TMPDIR':str(scratch),'PYTHONDONTWRITEBYTECODE':'1'}
    def call(action,payload,names=()):
        result=subprocess.run([sys.executable,'-I','-c','import sys;exec(compile(sys.stdin.read(),"<worker>","exec"))','--worker',action,str(root),json.dumps(payload),*names],input=Path(f.__file__).read_bytes(),cwd=cwd,env=env,capture_output=True,timeout=150)
        assert result.returncode==0,result.stderr.decode();return json.loads(result.stdout)
    child=call('prepare-oracle',control['scope'])
    try:
        report=call('verify',child,list(f.TARGETS));assert report['sources']==initial['sources'] and report['oracle_cache']['full_target_checks']==18
    finally:assert call('cleanup-oracle',child)['oracle_cache_removed'] is True
    assert not list(scratch.iterdir())

@pytest.mark.parametrize('fault',['bytes','deleted','directory','manifest','extra','symlink'])
def test_proximity_orthogonal_hosted_frozen_expected_archive_rejects_tampering(proximity_orthogonal_scene,tmp_path,fault):
    root,_,_=proximity_orthogonal_scene;previous=f.archive_expectations(root,tmp_path,{})
    path=tmp_path/'proximity_orthogonal-expectations'/'wide.cog.tif.npy'
    if fault=='bytes':path.chmod(0o644);path.write_bytes(b'changed')
    elif fault=='deleted':path.unlink()
    elif fault=='directory':shutil.rmtree(path.parent);(tmp_path/'proximity_orthogonal-expectations-manifest.json').unlink()
    elif fault=='manifest':(tmp_path/'proximity_orthogonal-expectations-manifest.json').write_text('{}')
    elif fault=='extra':(path.parent/'extra.npy').write_bytes(b'extra')
    else:path.unlink();path.symlink_to(root/f.EXPECTED_EXPORT/'wide.cog.tif.npy')
    with pytest.raises(f.FixtureError):f.archive_expectations(root,tmp_path,previous)


@pytest.mark.parametrize('fault',['pixel-metric','axis-swapped','unit','zero','mask','scale'])
def test_proximity_orthogonal_hosted_independent_oracle_rejects_wrong_semantics(fault):
    name='wide.cog.tif'if fault in('pixel-metric','axis-swapped')else'survey.cog.tif'if fault=='unit'else'nodata.cog.tif'if fault=='mask'else'second-band.cog.tif'
    wanted=f.expected_pixels(name);bad=wanted.copy()
    if fault=='pixel-metric':bad[0,0]=5.
    elif fault=='axis-swapped':bad[0,0]=2.
    elif fault=='unit':bad[np.isfinite(bad)]*=2.
    elif fault=='zero':bad[0,2]=1.
    elif fault=='mask':bad[3,3]=0.
    else:bad[np.isfinite(bad)]+=500.
    assert not np.array_equal(bad,wanted,equal_nan=True)
    with pytest.raises(f.FixtureError):f.assert_pixels(bad,wanted,name)

def test_proximity_orthogonal_hosted_oracle_never_calls_product_transform(monkeypatch):
    import operators.raster_compute as compute
    def forbidden(*args,**kwargs):raise AssertionError('oracle must not call product transform')
    monkeypatch.setattr(compute,'_nearest_line_sites',forbidden);monkeypatch.setattr(compute,'_nearest_pixel_sites',forbidden)
    f.expected_pixels.cache_clear()
    assert f.expected_pixels('wide.cog.tif')[0,0]==4. and f.expected_pixels('tall.cog.tif')[0,0]==10.


def test_proximity_orthogonal_hosted_world_centres_independently_audit_every_case():
    from fractions import Fraction as Q
    from decimal import Decimal,localcontext
    for name,(source,band,codes) in f.CASES.items():
        spec=f.source_spec(source);g=list(map(Q,spec['grid']));values,valid=f.expected_inputs(name)
        sites=np.argwhere(valid&np.isin(values,codes));wanted=f.expected_pixels(name)
        assert len(sites)>0 and band==2
        u=Q(f.reference(source).GetLinearUnits())
        def centre(row,col):
            return(g[0]+(Q(int(col))+Q(1,2))*g[1]+(Q(int(row))+Q(1,2))*g[2],
                   g[3]+(Q(int(col))+Q(1,2))*g[4]+(Q(int(row))+Q(1,2))*g[5])
        for row,col in [(0,0),(0,1),(2,2),(values.shape[0]-1,values.shape[1]-2)]:
            if not valid[row,col]:assert np.isnan(wanted[row,col]);continue
            point=centre(row,col);costs=[]
            for rr,cc in sites:
                target=centre(rr,cc);costs.append(((point[0]-target[0])**2+(point[1]-target[1])**2)*u*u)
            minimum=min(costs)
            if source.startswith('square-'):
                pixels=min((int(row)-int(rr))**2+(int(col)-int(cc))**2 for rr,cc in sites)
                expected=np.sqrt(float(pixels))*(spec['grid'][1]*f.reference(source).GetLinearUnits())
            else:
                with localcontext()as context:
                    context.prec=200
                    expected=float((Decimal(minimum.numerator)/Decimal(minimum.denominator)).sqrt())
            assert wanted[row,col]==expected,(name,row,col)


def test_proximity_orthogonal_hosted_tiff_roundtrip_exact_dot_and_three_units(proximity_orthogonal_scene):
    from fractions import Fraction as Q
    root,_,_=proximity_orthogonal_scene
    for name in f.CASES:
        ds=gdal.Open(str(root/f.CASES[name][0]));g=ds.GetGeoTransform();ds=None
        assert Q(g[1])*Q(g[2])+Q(g[4])*Q(g[5])==0
        assert Q(g[1])*Q(g[5])-Q(g[4])*Q(g[2])<0
    for source in ('tiny_shear.tif','rounded_dot.tif'):
        ds=gdal.Open(str(root/source));g=ds.GetGeoTransform();ds=None
        assert Q(g[1])*Q(g[2])+Q(g[4])*Q(g[5])!=0
        if source=='rounded_dot.tif':assert g[1]*g[2]+g[4]*g[5]==0.
    factors=[f.reference(n).GetLinearUnits()for n in('square-metre.tif','square-foot.tif','square-survey.tif')]
    assert factors[0]==1. and factors[1]!=factors[2] and all(0<v<1 for v in factors[1:])


def test_proximity_orthogonal_hosted_rectangular_nearest_reverses_pixel_metric():
    # Shorter pixel offset points to the farther physical target in both aspect directions.
    assert f.expected_pixels('oblique-wide.cog.tif')[0,0]==10.<15.
    assert f.expected_pixels('tall.cog.tif')[0,0]==10.<15.
    assert f.source_arrays('oblique-wide.tif')[0][0,2]==1 and f.source_arrays('oblique-wide.tif')[0][1,0]==1
    assert f.source_arrays('tall.tif')[0][0,1]==1 and f.source_arrays('tall.tif')[0][2,0]==1
