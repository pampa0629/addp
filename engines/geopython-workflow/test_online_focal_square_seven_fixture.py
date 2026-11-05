"""Seven-pixel T4 fixture against the actual Native workflow and physical COGs."""

import hashlib
import importlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time

import numpy as np
import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from addp_common.workflow_runtime.errors import WorkflowExecutionError
from workflow_engine import execute_workflow, GeoPythonWorkflowRunner

f = importlib.import_module('business.scripts.online-raster-focal-square-seven-fixture')
m = importlib.import_module('scripts.test.raster-focal-square-seven-persistence-online')
prior = importlib.import_module('test_online_focal_percentiles_fixture')


@pytest.fixture(scope='module', autouse=True)
def owned_runtime_scratch(tmp_path_factory):
    # Hosted already has a private Runtime. Give each xdist process the same
    # isolation without clearing unrelated temporary workspaces on this host.
    scratch = tmp_path_factory.mktemp('focal-square-seven-runtime')
    with pytest.MonkeyPatch.context() as patch:
        patch.setattr(tempfile, 'tempdir', str(scratch))
        yield scratch


@pytest.fixture(scope='module')
def results(tmp_path_factory, owned_runtime_scratch):
    root = tmp_path_factory.mktemp('focal-square-seven').resolve()
    (root / 'outputs').mkdir()
    (root / '.addp-online-owner').write_text(f.PREFIX + '-123-1')
    sources = f.seed(root)['sources']
    script_sha = f.digest(Path(f.__file__))['sha256']
    started = time.monotonic()
    baseline = f.worker('oracle', root, [script_sha])
    print(f'7x7 independent oracle and baseline: {time.monotonic()-started:.3f}s')
    names = []
    full_checks = 0
    previews = {}
    for name in f.CASES:
        dag = m.definition({source: source for source in f.SOURCES}, 'parent', name)
        prior.localize(dag, root, root / 'outputs' / name)
        result = execute_workflow(dag)
        assert result['status'] == 'success', result
        previews[name] = json.loads(result['final_result'])
        names.append(name)
        proof = f.worker('verify', root, [baseline['oracle_sha256'], script_sha, *names])
        assert proof['sources'] == sources and proof['full_cog_checks'] == len(names)
        full_checks += proof['full_cog_checks']
    assert full_checks == 171
    return root, sources, script_sha, baseline['oracle_sha256'], previews


def test_eighteen_real_cogs_original_archives_and_1251_full_reads(results, tmp_path):
    root, sources, script_sha, anchor, previews = results
    archives = f.archive_files(root / 'outputs', tmp_path, list(f.CASES), {}, f.PREFIX + '-cogs')
    assert len(archives) == 18
    for name in f.CASES:
        m.raster.validate_raster_artifact(previews[name], f.artifact_expectation(name))
        archive = tmp_path / archives[name]['path']
        assert archive.read_bytes() == (root / 'outputs' / name).read_bytes()
        assert f.digest(archive) == {key: archives[name][key] for key in ('size_bytes', 'sha256')}
    started = time.monotonic()
    full_checks = 171
    for _ in range(60):
        proof = f.worker('verify', root, [anchor, script_sha, *f.CASES])
        full_checks += proof['full_cog_checks']
        assert proof['sources'] == sources
        assert proof['actual_control_pairs_verified'] == f.actual_pairs(f.CASES)
        assert {name: proof['targets'][name]['sha256'] for name in f.CASES} == {
            name: archives[name]['sha256'] for name in f.CASES
        }
    assert full_checks == 1251
    print(f'7x7 real local physical reread, 60 × 18 COGs: {time.monotonic()-started:.3f}s')


@pytest.mark.parametrize('problem', f.FAILURES)
@pytest.mark.parametrize('language', ['zh-cn', 'en'])
def test_sixty_accurate_failures_are_at_focal_and_clear_workspace(results, tmp_path, monkeypatch, problem, language):
    import workflow_engine

    root = results[0]
    dag = m.definition(
        {source: source for source in f.SOURCES}, 'parent', 'mean.cog.tif', invalid=problem, language=language
    )
    target = tmp_path / 'failed.cog.tif'
    prior.localize(dag, root, target)
    workspaces = []
    execute = workflow_engine._execute_operator

    def track(operator, params):
        if operator == 'raster_focal_statistics':
            workspaces.append(params['input_raster'].workspace)
        return execute(operator, params)

    monkeypatch.setattr(workflow_engine, '_execute_operator', track)
    with pytest.raises(WorkflowExecutionError) as error:
        GeoPythonWorkflowRunner(language=language).execute(dag)
    assert error.value.error_code == 'EXECUTION_FAILED'
    assert str(error.value) == '任务 focal 执行失败: ' + m.MESSAGES[problem][language == 'en']
    assert workspaces and all(not path.exists() for path in workspaces)
    assert list(tmp_path.iterdir()) == []


@pytest.mark.parametrize('name', ['asymmetric-mean', 'asymmetric-stddev', 'asymmetric-sum', 'percentiles'])
def test_seam_sensitive_oracle_and_weight_direction(results, name):
    name += '.cog.tif'
    expected = f.expected_pixels(name)
    wrong = f.expected_pixels(name, no_halo=True)
    differing = ~((expected == wrong) | (np.isnan(expected) & np.isnan(wrong)))
    assert differing[:, 358:363, :355].any()
    assert differing[:, :355, 358:363].any()
    if name.startswith('asymmetric'):
        reversed_weights = f.expected_pixels(name, flip_weights=True)
        with pytest.raises(f.FixtureError, match='pixel values'):
            f.assert_pixels(reversed_weights, expected, name)


def test_window_controls_and_all_invalid(results):
    three, five, seven = (
        f.expected_pixels(name)[0] for name in ('square3.cog.tif', 'square5.cog.tif', 'mean.cog.tif')
    )
    assert (three[16, 4], five[16, 4], seven[16, 4]) == (0, 2, 2)
    assert (three[16, 18], five[16, 18], seven[16, 18]) == (0, 0, 4)
    assert np.isnan(f.expected_pixels('invalid.cog.tif')).all()


@pytest.mark.parametrize(
    'fault',
    [
        'seam-value',
        'band-mask',
        'overview',
        'grid',
        'description',
        'source-bytes',
        'extra-output',
        'oracle-bytes',
        'oracle-mode',
    ],
)
def test_physical_corruption_is_rejected(results, tmp_path, fault):
    from osgeo import gdal

    root, _, script_sha, anchor, _ = results
    # A second directory must also carry its own exact oracle scope.
    dest = tmp_path / 'owned'
    shutil.copytree(root, dest, ignore=shutil.ignore_patterns(f.ORACLE))
    # Oracle generation requires an empty target inventory. Freeze an equivalent
    # archive with only the root changed, then verify the intact copy first.
    archive = dest / f.ORACLE
    with np.load(root / f.ORACLE, allow_pickle=False) as expected:
        sources = {name: f.digest(dest / name) for name in f.SOURCES}
        scope = f.oracle_scope(dest, script_sha, sources)
        with archive.open('xb') as stream:
            np.savez(
                stream,
                scope=np.frombuffer(f.encoded(scope), dtype=np.uint8),
                **{name: expected[name] for name in f.CASES},
            )
    archive.chmod(0o444)
    new_anchor = f.digest(archive)['sha256']
    arguments = [new_anchor, script_sha, *f.CASES]
    if fault == 'source-bytes':
        with (dest / 'control.tif').open('ab') as stream:
            stream.write(b'changed')
    elif fault == 'extra-output':
        (dest / 'outputs/unrequested.tif').write_bytes(b'unrequested')
    elif fault in ('oracle-bytes', 'oracle-mode'):
        archive.chmod(0o644)
        if fault == 'oracle-bytes':
            with archive.open('ab') as stream:
                stream.write(b'changed')
            archive.chmod(0o444)
    else:
        path = dest / 'outputs/percentiles.cog.tif'
        with gdal.config_option('IGNORE_COG_LAYOUT_BREAK', 'YES'):
            dataset = gdal.OpenEx(
                str(path), gdal.OF_RASTER | gdal.OF_UPDATE, open_options=['IGNORE_COG_LAYOUT_BREAK=YES']
            )
            band = dataset.GetRasterBand(3)
            if fault == 'seam-value':
                band.WriteArray(np.array([[999.0]]), 360, 360)
            elif fault == 'band-mask':
                band.SetNoDataValue(0.0)
            elif fault == 'overview':
                band.GetOverview(0).Fill(999.0)
            elif fault == 'grid':
                grid = list(f.GRID)
                grid[2] += 0.125
                dataset.SetGeoTransform(grid)
            else:
                band.SetDescription('percentile=25.0')
            band = dataset = None
    with pytest.raises(f.FixtureError):
        f.worker('verify', dest, arguments)


def test_worker_stdin_is_repository_independent_and_frozen_scope_cleans(results, tmp_path):
    script = Path(f.__file__).read_bytes()
    sha = hashlib.sha256(script).hexdigest()
    root = (tmp_path / 'owned').resolve()
    root.mkdir()
    (root / 'outputs').mkdir()
    (root / '.addp-online-owner').write_text(f.PREFIX + '-123-1')
    scratch = tmp_path / 'scratch'
    scratch.mkdir()
    env = {**os.environ, 'TMPDIR': str(scratch)}

    def call(action, *names):
        return subprocess.run(
            [sys.executable, '-', '--worker', action, str(root), *names],
            input=script,
            cwd=tmp_path,
            env=env,
            capture_output=True,
            timeout=60,
        )

    seed = call('seed')
    assert seed.returncode == 0, seed.stderr
    baseline = call('oracle', sha)
    assert baseline.returncode == 0, baseline.stderr
    anchor = json.loads(baseline.stdout)['oracle_sha256']
    assert call('verify', anchor, sha).returncode == 0
    assert call('verify', anchor, 'b' * 64).returncode != 0
    (root / '.addp-online-owner').write_text(f.PREFIX + '-124-1')
    assert call('verify', anchor, sha).returncode != 0
    assert json.loads(call('cleanup-oracle').stdout)['oracle_cache_removed'] is True
    assert not (root / f.ORACLE).exists()
