"""Actual GDAL two-round boundary-clean COGs and immutable independent oracle evidence."""
import importlib,json,os,shutil,subprocess,sys
from hashlib import sha256
from pathlib import Path
import numpy as np
from osgeo import gdal
import pytest
from workflow_engine import execute_workflow,GeoPythonWorkflowRunner
from addp_common.workflow_runtime.errors import WorkflowExecutionError
sys.path.insert(0,str(Path(__file__).resolve().parents[2]))
f=importlib.import_module('business.scripts.online-raster-boundary-clean-two-rounds-fixture')
m=importlib.import_module('scripts.test.raster-boundary-clean-two-rounds-persistence-online')
helper=importlib.import_module('test_online_focal_percentile_fixture')

@pytest.fixture(scope='module')
def boundary_scene(tmp_path_factory,request):
    root=tmp_path_factory.mktemp('boundary-two-rounds-export');(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('boundary-clean-two-rounds-123-1');initial=f.seed(root)
    control=helper.cache_for(f,root,request)
    for name in f.TARGETS:
        result=execute_workflow(helper.native_definition(m,f,root,name));assert result['status']=='success',result
        artifact = json.loads(result['final_result'])
        assert artifact['size_bytes']==(root/'outputs'/name).stat().st_size>0
        m.validate_affine(artifact['transform'], f.artifact_expectation(name), 'Native artifact')
    return root,initial,control

def test_boundary_two_rounds_hosted_twelve_real_cogs_complete_oracle_rounds_controls_and_original_bytes(boundary_scene,tmp_path):
    root,initial,control=boundary_scene;report=f.verify(root,list(f.TARGETS),control)
    assert report['sources']==initial['sources'] and report['full_source_pixels_verified'] is True and len(report['targets'])==12
    archived=f.archive_cogs(root,tmp_path,report,{})
    expectations=f.archive_expectations(root,tmp_path,{})
    assert len(expectations)==12
    for name in f.TARGETS:
        path=tmp_path/expectations[name+'.npy']['path']
        assert path.read_bytes()==(f.oracle_path(control['scope'])/(name+'.npy')).read_bytes()
        np.testing.assert_array_equal(np.load(path,allow_pickle=False),f.expected_pixels(name))
    for name,fact in report['targets'].items():
        assert fact['full_pixel_and_mask_verification'] is True and fact['overview_count']==f.expected_overview_count(name)
        original = (root/'outputs'/name).read_bytes()
        assert (tmp_path/archived[name]['path']).read_bytes()==original
        assert archived[name]['size_bytes']==len(original)>0
        assert archived[name]['sha256']==sha256(original).hexdigest()
        assert fact['affine_transform']==f.artifact_expectation(name)['transform']
    assert json.loads((tmp_path/'boundary-two-rounds-cogs-manifest.json').read_text())==archived
    assert report['comparisons'] == [list(pair) for pair in f.COMPARISONS]
    assert report['round_witnesses'] == {n: site[2] for n, site in f.WITNESSES.items()}
    for left, right in f.COMPARISONS:
        np.testing.assert_array_equal(f.expected_pixels(left), f.expected_pixels(right))
    np.testing.assert_array_equal(f.expected_pixels('change-two.cog.tif'), np.zeros((3, 3)))
    np.testing.assert_array_equal(f.expected_pixels('frozen-two.cog.tif'), [[0]*3, [1]*3, [1]*3])
    assert not np.isfinite(f.expected_pixels('empty-two.cog.tif')).any()
    assert f.case_shape('seam-two.cog.tif') == (515, 519)
    assert f.case_shape('thin-two.cog.tif') == (1, 512)
    assert report['targets']['thin-two.cog.tif']['affine_transform'] == []


@pytest.mark.parametrize('problem',f.FAILURES)
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_boundary_two_rounds_hosted_thirty_two_exact_submitted_failures_no_output_and_clean_workspace(boundary_scene,tmp_path,monkeypatch,problem,language):
    import workflow_engine
    root,initial,control=boundary_scene;target=tmp_path/'failed.cog.tif';dag=helper.native_definition(m,f,root,'change-two.cog.tif',invalid=problem,language=language,target=target)
    workspaces=[];original=workflow_engine._execute_operator
    def track(name,params):
        if name=='raster_boundary_clean':workspaces.append(params['input_raster'].workspace)
        return original(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',track)
    with pytest.raises(WorkflowExecutionError) as error:GeoPythonWorkflowRunner(language=language).execute(dag)
    assert error.value.error_code=='EXECUTION_FAILED' and str(error.value)=='任务 clean 执行失败: '+m.MESSAGES[problem][language=='en']
    assert workspaces and all(not p.exists() for p in workspaces) and not target.exists()
    assert f.verify(root,list(f.TARGETS),control)['sources']==initial['sources']

@pytest.mark.parametrize('fault',['value','one-ulp','hole','mask',*[f'grid-{index}' for index in range(6)],'crs','nodata','unit','scale','overview','source-mask','sidecar','missing'])
def test_boundary_two_rounds_hosted_actual_pixel_mask_metadata_and_overview_corruption_rejected(boundary_scene,tmp_path,request,fault):
    original,_,_=boundary_scene;root=tmp_path/'changed';shutil.copytree(original,root);control=helper.cache_for(f,root,request)
    path=root/'outputs'/'seam-two.cog.tif'
    if fault=='sidecar':(root/'outputs'/'extra.tif').write_bytes(b'extra')
    elif fault=='missing':path.unlink()
    elif fault=='source-mask':
        ds=gdal.Open(str(root/'seam.tif'),gdal.GA_Update);ds.GetRasterBand(1).GetMaskBand().Fill(0);ds=None
    else:
        ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES']);band=ds.GetRasterBand(1)
        if fault in ('value','one-ulp','hole'):
            row,col=(508,508) if fault=='hole' else (510,510);wanted=f.expected_pixels('seam-two.cog.tif')[row,col]
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
def test_boundary_two_rounds_hosted_original_cog_archive_never_repairs_or_overwrites(boundary_scene,tmp_path,fault):
    root,_,control=boundary_scene;report=f.verify(root,list(f.TARGETS),control);previous=f.archive_cogs(root,tmp_path,report,{})
    path=tmp_path/'boundary-two-rounds-cogs'/'change-two.cog.tif'
    if fault=='bytes':path.chmod(0o644);path.write_bytes(b'changed')
    elif fault=='deleted':path.unlink()
    elif fault=='directory':shutil.rmtree(path.parent);(tmp_path/'boundary-two-rounds-cogs-manifest.json').unlink()
    elif fault=='manifest':(tmp_path/'boundary-two-rounds-cogs-manifest.json').write_text('{}')
    elif fault=='extra':(path.parent/'extra.tif').write_bytes(b'extra')
    else:path.unlink();path.symlink_to(root/'outputs'/'change-two.cog.tif')
    with pytest.raises(f.FixtureError):f.archive_cogs(root,tmp_path,report,previous)

@pytest.mark.parametrize('fault',['bytes','missing','extra','scope','shape'])
def test_boundary_two_rounds_hosted_frozen_cache_rejects_hash_shape_inventory_or_run_change(boundary_scene,tmp_path,request,fault):
    original,_,_=boundary_scene;root=tmp_path/'cache';shutil.copytree(original,root);control=helper.cache_for(f,root,request);directory=f.oracle_path(control['scope']);path=directory/'change-two.cog.tif.npy'
    if fault=='bytes':path.chmod(0o600);path.write_bytes(path.read_bytes()+b'x')
    elif fault=='missing':path.unlink()
    elif fault=='extra':(directory/'unexpected.npy').write_bytes(b'x')
    elif fault=='scope':control['scope']['run_id']='boundary-clean-two-rounds-999-1'
    else:
        path.chmod(0o600)
        with path.open('wb') as out:np.save(out,np.array([[1.]]))
        path.chmod(0o400);control['arrays']['change-two.cog.tif']=f.digest(path)
    with pytest.raises(f.FixtureError):f.verify(root,list(f.TARGETS),control)

def test_boundary_two_rounds_hosted_stdin_worker_has_no_repository_dependency_and_exact_cleanup(boundary_scene,tmp_path):
    original,initial,control=boundary_scene;root=tmp_path/'child';shutil.copytree(original,root);control={'scope':{**control['scope'],'source_root':str(root.resolve())}};scratch=tmp_path/'scratch';scratch.mkdir();cwd=tmp_path/'empty';cwd.mkdir()
    env={**os.environ,'PYTHONPATH':'','TMPDIR':str(scratch),'PYTHONDONTWRITEBYTECODE':'1'}
    def call(action,payload,names=()):
        result=subprocess.run([sys.executable,'-I','-c','import sys;exec(compile(sys.stdin.read(),"<worker>","exec"))','--worker',action,str(root),json.dumps(payload),*names],input=Path(f.__file__).read_bytes(),cwd=cwd,env=env,capture_output=True,timeout=150)
        assert result.returncode==0,result.stderr.decode();return json.loads(result.stdout)
    child=call('prepare-oracle',control['scope'])
    try:
        report=call('verify',child,list(f.TARGETS));assert report['sources']==initial['sources'] and report['oracle_cache']['full_target_checks']==12
    finally:assert call('cleanup-oracle',child)['oracle_cache_removed'] is True
    assert not list(scratch.iterdir())

@pytest.mark.parametrize('fault',['bytes','deleted','directory','manifest','extra','symlink'])
def test_boundary_two_rounds_hosted_frozen_expected_archive_rejects_tampering(boundary_scene,tmp_path,fault):
    root,_,_=boundary_scene;previous=f.archive_expectations(root,tmp_path,{})
    path=tmp_path/'boundary-two-rounds-expectations'/'change-two.cog.tif.npy'
    if fault=='bytes':path.chmod(0o644);path.write_bytes(b'changed')
    elif fault=='deleted':path.unlink()
    elif fault=='directory':shutil.rmtree(path.parent);(tmp_path/'boundary-two-rounds-expectations-manifest.json').unlink()
    elif fault=='manifest':(tmp_path/'boundary-two-rounds-expectations-manifest.json').write_text('{}')
    elif fault=='extra':(path.parent/'extra.npy').write_bytes(b'extra')
    else:path.unlink();path.symlink_to(root/f.EXPECTED_EXPORT/'change-two.cog.tif.npy')
    with pytest.raises(f.FixtureError):f.archive_expectations(root,tmp_path,previous)


@pytest.mark.parametrize('name, row, col, incorrect', [
    ('change-two.cog.tif', 1, 1, 1.),  # only ran one round
    ('frozen-two.cog.tif', 0, 1, 1.),  # recomputed the priority after one round
    ('seam-two.cog.tif', 510, 510, 1.),  # truncated the second round at both seams
    ('seam-two.cog.tif', 504, 510, 0.),  # filled an explicitly masked cell
    ('seam-two.cog.tif', 505, 511, 0.),  # filled a transparent cell
    ('empty-two.cog.tif', 1, 1, 0.),  # fabricated a class in empty coverage
])
def test_boundary_two_rounds_hosted_independent_semantic_mutations_cannot_pass(name, row, col, incorrect):
    expected = f.expected_pixels(name)
    bad = expected.copy()
    bad[row, col] = incorrect
    with pytest.raises(f.FixtureError):
        f.assert_pixels(bad, expected, name)


def test_boundary_two_rounds_hosted_oracle_never_calls_product_kernel_or_reducer(monkeypatch):
    import operators.raster_compute as product
    def forbidden(*args, **kwargs):
        raise AssertionError('product oracle dependency')
    monkeypatch.setattr(product, '_boundary_clean_round', forbidden)
    monkeypatch.setattr(product, 'raster_boundary_clean', forbidden)
    f.expected_rounds.cache_clear()
    try:
        for name in f.TARGETS:
            assert f.expected_pixels(name).shape == f.case_shape(name)
    finally:
        f.expected_rounds.cache_clear()


def test_boundary_two_rounds_hosted_actual_grid_witnesses(boundary_scene):
    root, _, _ = boundary_scene
    seam = gdal.Open(str(root/'outputs'/'seam-two.cog.tif'))
    assert seam.GetGeoTransform() == f.source_spec('seam.tif')['grid']
    assert seam.GetGeoTransform()[2] and seam.GetGeoTransform()[4]
    thin = gdal.Open(str(root/'outputs'/'thin-two.cog.tif'))
    assert thin.GetGeoTransform(can_return_null=True) is None and thin.GetSpatialRef() is None
