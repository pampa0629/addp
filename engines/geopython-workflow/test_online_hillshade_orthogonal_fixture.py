"""Actual GDAL hillshade paths and independent complete COG evidence."""
import importlib,json,os,subprocess,sys
from pathlib import Path
import pytest
fixture=importlib.import_module('business.scripts.online-raster-hillshade-orthogonal-fixture')
scene=importlib.import_module('scripts.test.raster-hillshade-orthogonal-persistence-online')


def localized(root,name,**kwargs):
    dag=scene.definition({n:n for n in fixture.SOURCES},'parent',name,**kwargs)
    load,save=dag['tasks'][0],dag['tasks'][-1];path=root/load['params'].pop('locator')
    load['params']['access_plan']={'schema_version':'addp.workflow.access-plan/v1','source':{'kind':'file','format':'tiff','access':{'method':'mounted_path','path':str(path)}}}
    params=save['params'];target=params.pop('target_name');params.pop('target_parent_locator')
    params.pop('write_mode');params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','target':{'kind':'file','format':'tiff','name':target,'write_mode':'create','access':{'method':'mounted_path','path':str(root/'outputs'/target)}}}
    return dag


@pytest.fixture(scope='module')
def export(tmp_path_factory):
    import tempfile
    root=tmp_path_factory.mktemp('orthogonal-export');(root/'.addp-online-owner').write_text('hillshade-orthogonal-123-1');(root/'outputs').mkdir()
    runtime=tmp_path_factory.mktemp('orthogonal-runtime')
    with pytest.MonkeyPatch.context()as patch:
        patch.setattr(tempfile,'tempdir',str(runtime));frozen=fixture.seed(root)
        scope={'schema':fixture.CACHE_SCHEMA,'run_id':'hillshade-orthogonal-123-1','source_root':str(root.resolve()),'fixture_sha256':fixture.digest(Path(fixture.__file__))['sha256'],'sources':frozen['sources']}
        control=fixture.prepare_oracle(root,scope)
        yield root,frozen,control
        assert fixture.cleanup_oracle(root,control)['oracle_cache_removed']


def test_hillshade_orthogonal_sixteen_real_chains_and_680_complete_original_checks(export,tmp_path):
    from workflow_engine import execute_workflow,GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    root,frozen,control=export;names=[];checks=0;previous={};expectations=control['expectations'];scope_hash=__import__('hashlib').sha256(fixture.encoded(control['scope'])).hexdigest()
    for name in fixture.TARGETS:
        result=execute_workflow(localized(root,name));assert result['status']=='success',result
        scene.validate_final({'final_result':json.loads(result['final_result'])},name,expectations[name]);names.append(name)
        native=fixture.verify(root,names,control);native['runtime_workspaces_removed']=True;native['archived_cogs']=fixture.archive_files(root,tmp_path,native)
        previous=scene.check_and_archive(native,names,frozen['sources'],expectations,tmp_path/scene.ARCHIVE_NAME,previous,scope_hash);checks+=len(names)
    assert checks==136 and len(previous)==16
    assert expectations['invalid.cog.tif']['valid_pixels'] == 0
    assert expectations['north-lit.cog.tif']['valid_pixels'] > 0
    np = __import__('numpy')
    shade = fixture.expected_array(control, 'north-shadow.cog.tif')
    assert np.all(shade[np.isfinite(shade)] == 1.)
    for problem in scene.FAILURES:
        for language in('zh-cn','en'):
            with pytest.raises(WorkflowExecutionError)as caught:GeoPythonWorkflowRunner(language=language).execute(localized(root,'north-lit.cog.tif',invalid=problem,language=language))
            fact=scene.diagnostic_fact('native',problem,language)
            assert str(caught.value)=='任务 '+fact['task']+' 执行失败: '+fact['exact_cause']
            native=fixture.verify(root,names,control);native['runtime_workspaces_removed']=True;native['archived_cogs']=fixture.archive_files(root,tmp_path,native)
            assert scene.check_and_archive(native,names,frozen['sources'],expectations,tmp_path/scene.ARCHIVE_NAME,previous,scope_hash)==previous;checks+=len(names)
    assert checks==680
    for name,fact in previous.items():assert (tmp_path/fact['path']).read_bytes()==(root/'outputs'/name).read_bytes()
    isolated=tmp_path/'empty-worker-cwd';isolated.mkdir()
    result=subprocess.run([sys.executable,'-','--worker','verify',str(root),json.dumps(control),*names],input=Path(fixture.__file__).read_text(),text=True,capture_output=True,cwd=isolated,env={**os.environ,'TMPDIR':str(fixture.cache_path(control['scope']).parent)},timeout=120)
    assert result.returncode==0,result.stderr
    assert json.loads(result.stdout)['runtime_workspaces_removed']is True


@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('problem', [*scene.FAILURES, 'unit-bool'])
def test_hillshade_orthogonal_exact_failure_node_language_and_zero_private_output(export,language,problem,monkeypatch):
    import workflow_engine
    from addp_common.workflow_runtime import WorkflowExecutionError
    root,_,_=export;before={p.name:fixture.digest(p)for p in(root/'outputs').iterdir()};workspaces=[];original=workflow_engine._execute_operator
    def capture(name,params):
        if name=='raster_hillshade':
            workspaces.append(params['input_raster'].workspace)
            if case == 'unit-empty':
                if problem == 'unit-bool':
                    assert params['elevation_unit'] is True
                else:
                    assert params['elevation_unit'] == ''
        return original(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',capture)
    # Develop rejects boolean values before creating an execution. Exercise
    # the operator's own boolean rejection only through the Native runner.
    case = 'unit-empty' if problem == 'unit-bool' else problem
    dag = localized(root, 'north-lit.cog.tif', invalid=case, language=language)
    if problem == 'unit-bool':
        dag['tasks'][1]['params']['elevation_unit'] = True
    with pytest.raises(WorkflowExecutionError) as caught:
        workflow_engine.GeoPythonWorkflowRunner(language=language).execute(dag)
    fact=scene.diagnostic_fact('native',case,language)
    assert str(caught.value)=='任务 '+fact['task']+' 执行失败: '+fact['exact_cause']
    assert workspaces and all(not p.exists()for p in workspaces)
    assert before=={p.name:fixture.digest(p)for p in(root/'outputs').iterdir()}


@pytest.mark.parametrize('fault',['pixel','mask','affine','crs','unit','cache','archive','overview','shadow-nodata'])
def test_hillshade_orthogonal_corrupt_actual_or_expected_never_passes(export,tmp_path,fault):
    from osgeo import gdal
    from workflow_engine import execute_workflow
    root,_,control=export
    # Each worker has an isolated module fixture. Preserve any preceding outputs.
    names=[name for name in fixture.TARGETS if (root/'outputs'/name).exists()]
    for name in('north-lit.cog.tif','coverage-cross-block.cog.tif'):
        if name not in names:
            assert execute_workflow(localized(root,name))['status']=='success';names.append(name)
    if fault == 'shadow-nodata':
        name = 'north-shadow.cog.tif'
        if name not in names:
            assert execute_workflow(localized(root, name))['status'] == 'success'
            names.append(name)
    else:
        name = 'coverage-cross-block.cog.tif' if fault == 'overview' else 'north-lit.cog.tif'
    path = root/'outputs'/name
    if fault=='cache':path=fixture.cache_directory(control['scope'])/(name+'.npy')
    if fault=='archive':
        native=fixture.verify(root,names,control);fixture.archive_files(root,tmp_path,native);path=tmp_path/scene.ARCHIVE_NAME/name
    raw=path.read_bytes();mode=path.stat().st_mode&0o777;path.chmod(0o600)
    try:
        if fault in('cache','archive'):path.write_bytes(raw+b'corrupt')
        else:
            ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES'])
            if fault=='pixel':ds.GetRasterBand(1).WriteArray(__import__('numpy').array([[2]],dtype='uint8'),3,3)
            elif fault=='mask':ds.GetRasterBand(1).SetNoDataValue(1)
            elif fault=='crs':
                from osgeo import osr
                crs = osr.SpatialReference()
                crs.ImportFromEPSG(32617)
                ds.SetSpatialRef(crs)
            elif fault=='shadow-nodata':
                ds.GetRasterBand(1).SetNoDataValue(1.)
            elif fault=='unit':ds.GetRasterBand(1).SetUnitType('wrong')
            elif fault=='overview':ds.GetRasterBand(1).GetOverview(0).WriteArray(__import__('numpy').array([[123.]],dtype='float64'),0,0)
            else:ds.SetGeoTransform((0,5,0,0,0,-10))
            ds=None
        with pytest.raises((fixture.FixtureError,RuntimeError,ValueError)):
            native=fixture.verify(root,names,control)
            if fault=='archive':fixture.archive_files(root,tmp_path,native)
    finally:path.write_bytes(raw);path.chmod(mode)


def test_reference_uses_no_product_or_gdal_terrain(monkeypatch):
    from osgeo import gdal
    from operators import raster_compute
    def forbidden(*args,**kwargs):raise AssertionError('reference used the terrain implementation')
    monkeypatch.setattr(gdal,'DEMProcessing',forbidden)
    monkeypatch.setattr(raster_compute,'raster_hillshade',forbidden)
    for name in fixture.TARGETS:
        value=fixture.reference(name)
        assert value.shape==(fixture.source_spec(fixture.CASES[name][0])['height'],fixture.source_spec(fixture.CASES[name][0])['width'])


@pytest.mark.parametrize('fault', ['rotation-omitted', 'rotation-reversed', 'square-axis-assumption'])
def test_hillshade_orthogonal_reference_rejects_light_or_axis_errors(export, fault):
    import math
    import numpy as np
    from osgeo import gdal
    from operators.raster_compute import raster_hillshade, raster_load, raster_workspace

    root, _, control = export
    name = 'rotated-lit.cog.tif'
    expected = fixture.expected_array(control, name)
    azimuth, altitude = fixture.LIGHTS['lit']
    if fault == 'square-axis-assumption':
        # With U=(3,4), V=(8,-6), falsely replacing the 10 m row length
        # with 5 m changes the projected gradient to (-1.64, 0.48).
        actual = expected.copy()
        actual[np.isfinite(actual)] = fixture.projected_light(-1.64, .48, azimuth, altitude)
    else:
        local_north = math.degrees(math.atan2(-8., 6.))
        # Substitute the externally equivalent wrong azimuth, with the
        # normal product transform still applied.
        wrong = (azimuth + (1 if fault == 'rotation-omitted' else 2)*local_north) % 360.
        source = localized(root, name)['tasks'][0]['params']['access_plan']
        with raster_workspace():
            raster = raster_hillshade(raster_load(source), 'metre', band=2, azimuth=wrong, altitude=altitude)
            data = gdal.Open(str(raster.path))
            actual = data.ReadAsArray()
            data = None
    assert np.array_equal(np.isfinite(actual), np.isfinite(expected))
    with pytest.raises(fixture.FixtureError, match='gray values'):
        fixture.check_pixels(actual, expected)


def test_frozen_pixel_archive_retains_all_exact_npy_members(tmp_path, monkeypatch):
    import zipfile

    root = tmp_path / 'export'
    root.mkdir()
    run = 'hillshade-orthogonal-123-1'
    (root / '.addp-online-owner').write_text(run)
    (root / 'outputs').mkdir()
    secret = tmp_path / 'secret'
    secret.mkdir()
    output = tmp_path / 'evidence'
    output.mkdir()
    cache = tmp_path / 'cache'
    cache.mkdir()
    monkeypatch.setattr(__import__('tempfile'), 'tempdir', str(cache))
    monkeypatch.setattr(fixture.NFSExport, 'guard', lambda *_: secret)
    monkeypatch.setattr(fixture.NFSExport, 'owned_root', lambda *_: root)
    monkeypatch.setattr(fixture, 'runtime_worker', lambda action, root, args=(): fixture.worker(action, root, args))
    monkeypatch.setenv('ADDP_ONLINE_TEST_RUN_ID', run)
    monkeypatch.setenv('ADDP_ONLINE_ARTIFACT_DIR', str(output))
    fixture.main('seed')
    try:
        frozen = fixture.main('prepare-oracle')
        archive = output / 'hillshade-orthogonal-expected.npz'
        manifest = json.loads((output / 'hillshade-orthogonal-expected-manifest.json').read_bytes())
        assert manifest['scope_sha256'] == frozen['scope_sha256']
        assert manifest['archive'] == {'path': archive.name, **fixture.digest(archive)}
        with zipfile.ZipFile(archive) as zipped:
            assert set(zipped.namelist()) == {name + '.npy' for name in fixture.TARGETS}
            assert zipped.testzip() is None
        assert not list(output.glob('*-control.json'))
    finally:
        assert fixture.main('cleanup-oracle')['oracle_cache_removed']
