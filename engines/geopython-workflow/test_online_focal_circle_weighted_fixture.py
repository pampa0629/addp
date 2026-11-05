"""Actual GDAL circle/control COGs and immutable independent oracle evidence."""
import importlib,json,os,shutil,subprocess,sys
from fractions import Fraction
from hashlib import sha256
from pathlib import Path
import numpy as np
from osgeo import gdal
import pytest
from workflow_engine import execute_workflow,GeoPythonWorkflowRunner
from addp_common.workflow_runtime.errors import WorkflowExecutionError
sys.path.insert(0,str(Path(__file__).resolve().parents[2]))
f=importlib.import_module('business.scripts.online-raster-focal-circle-weighted-fixture')
m=importlib.import_module('scripts.test.raster-focal-circle-weighted-persistence-online')
helper=importlib.import_module('test_online_focal_percentile_fixture')

@pytest.fixture(scope='module')
def circle_scene(tmp_path_factory,request):
    root=tmp_path_factory.mktemp('circle-weighted-export');(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('focal-circle-weighted-123-1');initial=f.seed(root)
    control=helper.cache_for(f,root,request)
    for name in f.TARGETS:
        result=execute_workflow(helper.native_definition(m,f,root,name));assert result['status']=='success',result
        artifact = json.loads(result['final_result'])
        assert artifact['size_bytes']==(root/'outputs'/name).stat().st_size>0
        m.validate_affine(artifact['transform'], f.artifact_expectation(name), 'Native artifact')
    return root,initial,control

def test_weighted_circle_hosted_eighteen_real_cogs_complete_oracle_circle_boundaries_controls_and_original_bytes(circle_scene,tmp_path):
    root,initial,control=circle_scene;report=f.verify(root,list(f.TARGETS),control)
    assert report['sources']==initial['sources'] and report['full_source_pixels_verified'] is True and len(report['targets'])==18
    archived=f.archive_cogs(root,tmp_path,report,{})
    expectations=f.archive_expectations(root,tmp_path,{})
    assert len(expectations)==18
    for name in f.TARGETS:
        path=tmp_path/expectations[name+'.npy']['path']
        assert path.read_bytes()==(f.oracle_path(control['scope'])/(name+'.npy')).read_bytes()
        np.testing.assert_array_equal(np.load(path,allow_pickle=False),f.expected_pixels(name))
    for name,fact in report['targets'].items():
        assert fact['full_pixel_and_mask_verification'] is True and fact['overview_count']>0
        original = (root/'outputs'/name).read_bytes()
        assert (tmp_path/archived[name]['path']).read_bytes()==original
        assert archived[name]['size_bytes']==len(original)>0
        assert archived[name]['sha256']==sha256(original).hexdigest()
        assert fact['affine_transform']==f.artifact_expectation(name)['transform']
    assert json.loads((tmp_path/'circle-weighted-cogs-manifest.json').read_text())==archived
    assert report['comparisons'] == [list(pair) for pair in f.COMPARISONS]
    assert report['boundary_witness'] == {'equal': 4., 'below': 2.}
    for left, right in f.COMPARISONS:
        np.testing.assert_array_equal(f.expected_pixels(left), f.expected_pixels(right))
    for name, wanted in [('ones-mean.cog.tif', [2., 0., 2.]),
                         ('ones-stddev.cog.tif', [2., 4., 0.]),
                         ('ones-sum.cog.tif', [4., 0., 4.])]:
        assert f.expected_pixels(name)[3, [10, 20, 30]].tolist() == wanted
    assert not np.isfinite(f.expected_pixels('invalid.cog.tif')).any()
    values, valid = f.expected_inputs('center.cog.tif')
    np.testing.assert_array_equal(f.expected_pixels('center.cog.tif'), np.where(valid, 2*values, np.nan))
    assert f.source_spec('foot.tif')['epsg']==2222 and f.source_spec('survey.tif')['epsg']==2277
    assert f.case_shape('rotated-sum.cog.tif') == (515, 519)
    assert np.isnan(f.expected_pixels('north-mean.cog.tif')[23, 23])
    assert f.expected_inputs('north-mean.cog.tif')[1][0, 3]
    assert np.isfinite(f.expected_pixels('north-mean.cog.tif')[1, 3])


@pytest.mark.parametrize('problem',f.FAILURES)
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_weighted_circle_hosted_seventy_two_exact_submitted_failures_no_output_and_clean_workspace(circle_scene,tmp_path,monkeypatch,problem,language):
    import workflow_engine
    root,initial,control=circle_scene;target=tmp_path/'failed.cog.tif';dag=helper.native_definition(m,f,root,'north-mean.cog.tif',invalid=problem,language=language,target=target)
    workspaces=[];original=workflow_engine._execute_operator
    def track(name,params):
        if name=='raster_focal_statistics':workspaces.append(params['input_raster'].workspace)
        return original(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',track)
    with pytest.raises(WorkflowExecutionError) as error:GeoPythonWorkflowRunner(language=language).execute(dag)
    assert error.value.error_code=='EXECUTION_FAILED' and str(error.value)=='任务 focal 执行失败: '+m.MESSAGES[problem][language=='en']
    assert workspaces and all(not p.exists() for p in workspaces) and not target.exists()
    assert f.verify(root,list(f.TARGETS),control)['sources']==initial['sources']

@pytest.mark.parametrize('fault',['value','one-ulp','hole','mask',*[f'grid-{index}' for index in range(6)],'crs','nodata','unit','scale','overview','source-mask','sidecar','missing'])
def test_weighted_circle_hosted_actual_pixel_mask_metadata_and_overview_corruption_rejected(circle_scene,tmp_path,request,fault):
    original,_,_=circle_scene;root=tmp_path/'changed';shutil.copytree(original,root);control=helper.cache_for(f,root,request)
    path=root/'outputs'/'north-mean.cog.tif'
    if fault=='sidecar':(root/'outputs'/'extra.tif').write_bytes(b'extra')
    elif fault=='missing':path.unlink()
    elif fault=='source-mask':
        ds=gdal.Open(str(root/'north.tif'),gdal.GA_Update);ds.GetRasterBand(1).GetMaskBand().Fill(0);ds=None
    else:
        ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES']);band=ds.GetRasterBand(1)
        if fault in ('value','one-ulp','hole'):
            row,col=(0,1) if fault=='hole' else (62,62);wanted=f.expected_pixels('north-mean.cog.tif')[row,col]
            value=0. if fault=='hole' else np.nextafter(wanted,np.inf) if fault=='one-ulp' else 123.
            band.WriteArray(np.array([[value]]),col,row)
        elif fault=='mask':
            with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
            band.GetMaskBand().Fill(255)
        elif fault.startswith('grid-'):
            grid = list(ds.GetGeoTransform())
            grid[int(fault[-1])] += 1.
            ds.SetGeoTransform(grid)
        elif fault=='crs':ds.SetProjection('')
        elif fault=='nodata':band.SetNoDataValue(-999.)
        elif fault=='unit':band.SetUnitType('metre')
        elif fault=='scale':band.SetScale(2.)
        else:band.GetOverview(0).Fill(99.)
        band=ds=None
    with pytest.raises(f.FixtureError):f.verify(root,list(f.TARGETS),control)

@pytest.mark.parametrize('fault',['bytes','deleted','directory','manifest','extra','symlink'])
def test_weighted_circle_hosted_original_cog_archive_never_repairs_or_overwrites(circle_scene,tmp_path,fault):
    root,_,control=circle_scene;report=f.verify(root,list(f.TARGETS),control);previous=f.archive_cogs(root,tmp_path,report,{})
    path=tmp_path/'circle-weighted-cogs'/'north-mean.cog.tif'
    if fault=='bytes':path.chmod(0o644);path.write_bytes(b'changed')
    elif fault=='deleted':path.unlink()
    elif fault=='directory':shutil.rmtree(path.parent);(tmp_path/'circle-weighted-cogs-manifest.json').unlink()
    elif fault=='manifest':(tmp_path/'circle-weighted-cogs-manifest.json').write_text('{}')
    elif fault=='extra':(path.parent/'extra.tif').write_bytes(b'extra')
    else:path.unlink();path.symlink_to(root/'outputs'/'north-mean.cog.tif')
    with pytest.raises(f.FixtureError):f.archive_cogs(root,tmp_path,report,previous)

@pytest.mark.parametrize('fault',['bytes','missing','extra','scope','shape'])
def test_weighted_circle_hosted_frozen_cache_rejects_hash_shape_inventory_or_run_change(circle_scene,tmp_path,request,fault):
    original,_,_=circle_scene;root=tmp_path/'cache';shutil.copytree(original,root);control=helper.cache_for(f,root,request);directory=f.oracle_path(control['scope']);path=directory/'north-mean.cog.tif.npy'
    if fault=='bytes':path.chmod(0o600);path.write_bytes(path.read_bytes()+b'x')
    elif fault=='missing':path.unlink()
    elif fault=='extra':(directory/'unexpected.npy').write_bytes(b'x')
    elif fault=='scope':control['scope']['run_id']='focal-circle-weighted-999-1'
    else:
        path.chmod(0o600)
        with path.open('wb') as out:np.save(out,np.array([[1.]]))
        path.chmod(0o400);control['arrays']['north-mean.cog.tif']=f.digest(path)
    with pytest.raises(f.FixtureError):f.verify(root,list(f.TARGETS),control)

def test_weighted_circle_hosted_stdin_worker_has_no_repository_dependency_and_exact_cleanup(circle_scene,tmp_path):
    original,initial,control=circle_scene;root=tmp_path/'child';shutil.copytree(original,root);control={'scope':{**control['scope'],'source_root':str(root.resolve())}};scratch=tmp_path/'scratch';scratch.mkdir();cwd=tmp_path/'empty';cwd.mkdir()
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
def test_weighted_circle_hosted_frozen_expected_archive_rejects_tampering(circle_scene,tmp_path,fault):
    root,_,_=circle_scene;previous=f.archive_expectations(root,tmp_path,{})
    path=tmp_path/'circle-weighted-expectations'/'north-mean.cog.tif.npy'
    if fault=='bytes':path.chmod(0o644);path.write_bytes(b'changed')
    elif fault=='deleted':path.unlink()
    elif fault=='directory':shutil.rmtree(path.parent);(tmp_path/'circle-weighted-expectations-manifest.json').unlink()
    elif fault=='manifest':(tmp_path/'circle-weighted-expectations-manifest.json').write_text('{}')
    elif fault=='extra':(path.parent/'extra.npy').write_bytes(b'extra')
    else:path.unlink();path.symlink_to(root/f.EXPECTED_EXPORT/'north-mean.cog.tif.npy')
    with pytest.raises(f.FixtureError):f.archive_expectations(root,tmp_path,previous)


@pytest.mark.parametrize('name, row, col, incorrect', [
    ('north-mean.cog.tif', 62, 62, -2.),  # flipped directional weights
    ('below.cog.tif', 62, 62, 4.),        # admitted a centre beyond the exact radius
    ('north-sum.cog.tif', 62, 62, 4.),    # normalized the weighted sum
    ('ones-stddev.cog.tif', 3, 10, np.sqrt(8.)),  # used a sample denominator
    ('north-mean.cog.tif', 23, 23, 0.),   # filled a window with no effective positive weight
    ('north-mean.cog.tif', 0, 1, 0.),     # filled a NoData centre
    ('foot.cog.tif', 62, 62, np.nan),     # ignored the CRS conversion; centre has zero weight
])
def test_weighted_circle_hosted_independent_semantic_mutations_cannot_pass(name, row, col, incorrect):
    expected = f.expected_pixels(name)
    bad = expected.copy()
    bad[row, col] = incorrect
    with pytest.raises(f.FixtureError):
        f.assert_pixels(bad, expected, name)


def test_weighted_circle_hosted_oracle_never_calls_product_kernel_or_reducer(monkeypatch):
    import operators.raster_compute as product
    def forbidden(*a,**kw):raise AssertionError('product oracle dependency')
    monkeypatch.setattr(product,'_focal_circle_window',forbidden)
    monkeypatch.setattr(product,'_focal_weighted_values',forbidden)
    monkeypatch.setattr(product,'raster_focal_statistics',forbidden)
    f.expected_pixels.cache_clear()
    try:
        for name in f.TARGETS:assert f.expected_pixels(name).shape==f.case_shape(name)
    finally:f.expected_pixels.cache_clear()


def test_weighted_circle_hosted_actual_grid_witnesses(circle_scene):
    root,_,_=circle_scene

    def grid(name):
        dataset = gdal.Open(str(root/name))
        return dataset.GetGeoTransform()

    for source, _, _, _, _ in f.CASES.values():
        _, ux, vx, _, uy, vy = map(Fraction, grid(source))
        assert ux*vx + uy*vy == 0
        assert ux*vy - uy*vx < 0
    _, ux, vx, _, uy, vy = map(Fraction, grid('mirror.tif'))
    assert ux*vx + uy*vy == 0
    assert ux*vy - uy*vx > 0
