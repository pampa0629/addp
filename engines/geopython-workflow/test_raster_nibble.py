"""Independent class-allocation oracles and real workflow persistence for nibble."""
import itertools
import json
from pathlib import Path
import threading
import time
from types import SimpleNamespace

import numpy as np
import pytest
from osgeo import gdal, gdal_array, osr

import operators.raster_compute as compute
from operators import list_operators
from operators.raster_compute import (
    raster_load, raster_nibble, raster_reclassify, raster_save, raster_workspace, validate_cog,
)


@pytest.fixture(autouse=True)
def english_disk_diagnostics():
    with compute.raster_error_language('en'):
        yield


def _source(path, values, *, crs='EPSG:3857', transform=(100, 2, 0, 200, 0, -2),
            dtype=gdal.GDT_Float64, nodata=-9999, alpha=None, mask=None):
    data = np.asarray(values, dtype=gdal_array.GDALTypeCodeToNumericTypeCode(dtype))
    data = data[None] if data.ndim == 2 else data
    dataset = gdal.GetDriverByName('GTiff').Create(str(path), data.shape[2], data.shape[1],
                                                 len(data) + (alpha is not None), dtype)
    if transform is not None:
        dataset.SetGeoTransform(transform)
    if crs:
        reference = osr.SpatialReference()
        reference.SetFromUserInput(crs)
        dataset.SetProjection(reference.ExportToWkt())
    for index, array in enumerate(data, 1):
        band = dataset.GetRasterBand(index)
        if nodata is not None:
            band.SetNoDataValue(nodata)
        band.WriteArray(array)
    if alpha is not None:
        band = dataset.GetRasterBand(len(data) + 1)
        band.WriteArray(alpha)
        band.SetColorInterpretation(gdal.GCI_AlphaBand)
    if mask is not None:
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK', 'YES'):
            dataset.CreateMaskBand(gdal.GMF_PER_DATASET)
        dataset.GetRasterBand(1).GetMaskBand().WriteArray(np.asarray(mask, dtype=np.uint8))
    band = dataset = None
    return path


def _source_plan(path):
    return {'schema_version': 'addp.workflow.access-plan/v1', 'source': {
        'kind': 'file', 'format': 'tiff', 'access': {'method': 'mounted_path', 'path': str(path)}}}


def _target_plan(path):
    return {'schema_version': 'addp.workflow.access-plan/v1', 'target': {
        'kind': 'file', 'format': 'tiff', 'name': path.name, 'write_mode': 'create',
        'access': {'method': 'mounted_path', 'path': str(path)}}}


def _values(path):
    dataset = gdal.Open(str(path))
    values = dataset.GetRasterBand(1).ReadAsArray()
    dataset = None
    return values


def _oracle(values, target_values, valid):
    """Brute-force all original donors, retaining row-major first equal distance."""
    target = valid & np.isin(values, target_values)
    y, x = np.indices(values.shape, dtype=np.int64)
    best = np.full(values.shape, np.iinfo(np.int64).max, dtype=np.int64)
    result = values.astype(np.float64).copy()
    for row, column in np.argwhere(valid & ~target):
        squared = (y - row) ** 2 + (x - column) ** 2
        use = target & (squared < best)
        result[use] = values[row, column]
        best[use] = squared[use]
    if np.any(target & (best == np.iinfo(np.int64).max)):
        raise ValueError('no donor')
    result[~valid] = np.nan
    return result


def _assert_clean(workspace):
    assert not list(workspace.glob('*.distance'))
    assert not list(workspace.glob('*.sites'))


def test_disk_budget_atomic_concurrent_admission_and_exception_release(monkeypatch):
    monkeypatch.setattr(compute.shutil, 'disk_usage', lambda path: SimpleNamespace(free=2 * 1024**3 + 200))
    entered, release = threading.Event(), threading.Event()
    failures = []
    def first():
        try:
            with raster_workspace(), compute._disk_budget(150, 'first'):
                entered.set()
                assert release.wait(5)
                raise LookupError('business failure')
        except LookupError:
            pass
        except BaseException as exc:
            failures.append(exc)
    worker = threading.Thread(target=first)
    worker.start()
    try:
        assert entered.wait(5)
        with raster_workspace():
            with pytest.raises(RuntimeError, match='pending_bytes=150'):
                with compute._disk_budget(150, 'second'):
                    pytest.fail('The same free space cannot admit both tasks')
    finally:
        release.set()
        worker.join(5)
    assert not worker.is_alive() and not failures and not compute._DISK_RESERVATIONS
    with raster_workspace(), compute._disk_budget(200, 'recovered at exact threshold'):
        pass
    assert not compute._DISK_RESERVATIONS


def test_disk_budget_actual_filesystem_identity_and_one_safety_margin(monkeypatch):
    original_stat = Path.stat
    devices = {}
    def stat(path, *args, **kwargs):
        return SimpleNamespace(st_dev=devices[path]) if path in devices else original_stat(path, *args, **kwargs)
    monkeypatch.setattr(Path, 'stat', stat)
    monkeypatch.setattr(compute.shutil, 'disk_usage', lambda path: SimpleNamespace(free=2 * 1024**3 + 100))
    with raster_workspace():
        devices[compute._WORKSPACE.get()] = 1
        with compute._disk_budget(100, 'device one'), raster_workspace():
            devices[compute._WORKSPACE.get()] = 2
            with compute._disk_budget(100, 'independent device'):
                pass
            devices[compute._WORKSPACE.get()] = 1
            with pytest.raises(RuntimeError, match='pending_bytes=100'):
                with compute._disk_budget(1, 'same device'):
                    pytest.fail('Filesystem aliases must share pending capacity')
    assert not compute._DISK_RESERVATIONS


def test_disk_budget_probe_and_registration_share_the_same_lock(monkeypatch):
    first_probe, allow_probe, second_probe, release = (threading.Event() for _ in range(4))
    outcomes = []
    def usage(path):
        if threading.current_thread().name == 'disk-first':
            first_probe.set()
            assert allow_probe.wait(5)
        else:
            second_probe.set()
        return SimpleNamespace(free=2 * 1024**3 + 150)
    monkeypatch.setattr(compute.shutil, 'disk_usage', usage)
    def run():
        try:
            with raster_workspace(), compute._disk_budget(150, 'concurrent probe'):
                outcomes.append('admitted')
                assert release.wait(5)
        except RuntimeError as exc:
            outcomes.append(str(exc))
        except BaseException as exc:
            outcomes.append(repr(exc))
    first = threading.Thread(target=run, name='disk-first')
    second = threading.Thread(target=run, name='disk-second')
    first.start()
    try:
        assert first_probe.wait(5)
        second.start()
        assert not second_probe.wait(.1), 'The capacity observation must hold the registration lock'
        allow_probe.set()
        second.join(5)
        assert not second.is_alive()
        assert outcomes[0] == 'admitted' and 'pending_bytes=150' in outcomes[1]
    finally:
        allow_probe.set()
        release.set()
        first.join(5)
        if second.ident is not None:
            second.join(5)
    assert not first.is_alive() and not compute._DISK_RESERVATIONS


def test_disk_budget_sparse_allocation_not_logical_length_and_probe_order(monkeypatch):
    budget, addition = 16 * 1024**2, 4096
    free = 2 * 1024**3 + budget
    events = []
    def usage(path):
        events.append('free')
        return SimpleNamespace(free=free)
    monkeypatch.setattr(compute.shutil, 'disk_usage', usage)
    with raster_workspace():
        private = compute._path('.distance')
        original_stat = Path.stat
        def stat(path, *args, **kwargs):
            if path == private:
                events.append('allocated')
            return original_stat(path, *args, **kwargs)
        monkeypatch.setattr(Path, 'stat', stat)
        with compute._disk_budget(budget, 'owner', private_paths=(private,)):
            with private.open('wb') as stream:
                stream.truncate(budget)
            assert private.stat().st_size == budget and private.stat().st_blocks * 512 < budget
            free = 2 * 1024**3 + addition
            with pytest.raises(RuntimeError, match='Insufficient'):
                with compute._disk_budget(addition, 'sparse file is not allocated'):
                    pytest.fail('Sparse logical length cannot be credited')
            with private.open('r+b') as stream:
                stream.write(b'x' * 1024**2)
            allocated = private.stat().st_blocks * 512
            assert 0 < allocated <= budget
            free = 2 * 1024**3 + addition + budget - allocated
            events.clear()
            with compute._disk_budget(addition, 'allocated credit at exact threshold'):
                assert events == ['allocated', 'free']
            with compute._DISK_LOCK:
                private.unlink()
            with pytest.raises(RuntimeError, match=f'pending_bytes={budget}'):
                with compute._disk_budget(addition, 'deleted storage is still pending'):
                    pytest.fail('Deletion must restore pending commitment')
    assert not compute._DISK_RESERVATIONS


@pytest.mark.parametrize('fault', ['probe', 'negative', 'noninteger', 'private-stat'])
def test_disk_budget_observation_failure_is_closed_and_does_not_leak(monkeypatch, fault):
    original_usage, original_stat = compute.shutil.disk_usage, Path.stat
    with raster_workspace():
        private = compute._path('.sites')
        with compute._disk_budget(10, 'owner', private_paths=(private,)):
            def stat(path, *args, **kwargs):
                if path == private:
                    raise PermissionError('private host path')
                return original_stat(path, *args, **kwargs)
            def usage(path):
                if fault == 'probe':
                    raise OSError('private host path')
                return SimpleNamespace(free=-1 if fault == 'negative' else .5)
            monkeypatch.setattr(Path, 'stat', stat if fault == 'private-stat' else original_stat)
            monkeypatch.setattr(compute.shutil, 'disk_usage', original_usage if fault == 'private-stat' else usage)
            with pytest.raises(RuntimeError, match='capacity unavailable during child') as error:
                with compute._disk_budget(10, 'child'):
                    pytest.fail('An unavailable observation cannot admit writes')
            assert 'private host path' not in str(error.value)
            assert len(compute._DISK_RESERVATIONS) == 1
    assert not compute._DISK_RESERVATIONS


@pytest.mark.parametrize('language', ['zh-cn', 'en'])
@pytest.mark.parametrize('unavailable', [False, True])
def test_disk_diagnostics_language_scope_and_unavailable_translation(monkeypatch, language, unavailable):
    def usage(path):
        if unavailable:
            raise OSError('host-private-detail')
        return SimpleNamespace(free=0)
    monkeypatch.setattr(compute.shutil, 'disk_usage', usage)
    expected = ({'zh-cn': '无法读取栅格临时磁盘容量', 'en': 'Raster temporary disk capacity unavailable'} if unavailable else
                {'zh-cn': '栅格临时磁盘空间不足', 'en': 'Insufficient raster temporary disk space'})[language]
    with compute.raster_error_language(language), raster_workspace():
        with pytest.raises(RuntimeError, match=expected) as error:
            with compute._disk_budget(128, 'distance transform'):
                pytest.fail('No writes are allowed')
        assert 'host-private-detail' not in str(error.value)
    assert compute._DISK_LANGUAGE.get() == 'en' and not compute._DISK_RESERVATIONS


@pytest.mark.parametrize('operator', ['nibble', 'proximity'])
def test_distance_disk_failure_precedes_private_files_and_output(tmp_path, monkeypatch, operator):
    source = _source(tmp_path / 'source.tif', [[7, 1], [7, 2]], dtype=gdal.GDT_Int16)
    before = source.read_bytes()
    with raster_workspace():
        loaded = raster_load(_source_plan(source))
        dataset = compute._open(loaded)
        free = 2 * 1024**3 + compute._raster_disk_bytes(dataset, numeric=True) + 32 * 4 - 1
        dataset = None
        initial_files = set(loaded.workspace.iterdir())
        monkeypatch.setattr(compute.shutil, 'disk_usage', lambda path: SimpleNamespace(free=free))
        monkeypatch.setattr(compute, '_numeric_output', lambda *args: pytest.fail('Output created before disk check'))
        function = raster_nibble if operator == 'nibble' else compute.raster_proximity
        with pytest.raises(RuntimeError, match='distance transform.*required_bytes=128'):
            function(loaded, 1, [7])
        assert set(loaded.workspace.iterdir()) == initial_files
        _assert_clean(loaded.workspace)
    assert not compute._DISK_RESERVATIONS and not loaded.workspace.exists()
    assert source.read_bytes() == before


def test_nibble_no_target_only_reserves_output_and_workspace_resets_protection(tmp_path, monkeypatch):
    source = _source(tmp_path / 'source.tif', [[0, 1], [2, -9999]])
    with raster_workspace():
        loaded = raster_load(_source_plan(source))
        dataset = compute._open(loaded)
        free = 2 * 1024**3 + compute._raster_disk_bytes(dataset, numeric=True)
        dataset = None
        monkeypatch.setattr(compute.shutil, 'disk_usage', lambda path: SimpleNamespace(free=free))
        monkeypatch.setattr(compute, '_nearest_pixel_sites', lambda *args, **kwargs: pytest.fail('No distance transform is needed'))
        result = raster_nibble(loaded, 1, [7])
        np.testing.assert_array_equal(_values(result.path), [[0, 1], [2, np.nan]])
        assert compute._DISK_PROTECTED.get() and not compute._DISK_RESERVATIONS
        with raster_workspace():
            assert not compute._DISK_PROTECTED.get()
        assert compute._DISK_PROTECTED.get()
    assert not compute._DISK_PROTECTED.get() and not compute._DISK_RESERVATIONS


@pytest.mark.parametrize('profile,blocksize', [('geotiff', 512), ('cog', 128), ('cog', 512), ('cog', 4096)])
def test_distance_encoding_rechecks_disk_before_encoder_or_publication(tmp_path, monkeypatch, profile, blocksize):
    source = _source(tmp_path / 'source.tif', [[7, 1], [7, 2]])
    target = tmp_path / 'target.tif'
    with raster_workspace():
        result = raster_nibble(raster_load(_source_plan(source)), 1, [7])
        before = result.path.read_bytes()
        dataset = compute._open(result)
        free = 2 * 1024**3 + compute._raster_disk_bytes(dataset, profile=profile, blocksize=blocksize) - 1
        dataset = None
        monkeypatch.setattr(compute.shutil, 'disk_usage', lambda path: SimpleNamespace(free=free))
        monkeypatch.setattr(compute, '_cog', lambda *args: pytest.fail('Encoding must not start'))
        monkeypatch.setattr(compute, '_translated', lambda *args, **kwargs: pytest.fail('Encoding must not start'))
        monkeypatch.setattr(compute, 'publish_target_file', lambda *args: pytest.fail('Publishing must not start'))
        with pytest.raises(RuntimeError, match='Insufficient.*raster encoding'):
            raster_save(result, _target_plan(target), profile=profile, blocksize=blocksize)
        assert not target.exists() and result.path.read_bytes() == before
        assert not compute._DISK_RESERVATIONS


@pytest.mark.parametrize('profile,compression', [('geotiff', 'NONE'), ('cog', 'NONE'), ('cog', 'LZW'), ('cog', 'DEFLATE'), ('cog', 'ZSTD')])
def test_disk_encoding_estimate_covers_real_tiled_multiband_mask_overviews(tmp_path, profile, compression):
    values = np.random.default_rng(8871).normal(size=(2, 513, 517))
    source = _source(tmp_path / 'source.tif', values, mask=np.where(values[0] < 0, 0, 255))
    dataset = gdal.Open(str(source), gdal.GA_Update)
    dataset.BuildOverviews('NEAREST', [2, 3, 4, 8])
    dataset = None
    with raster_workspace():
        loaded = raster_load(_source_plan(source))
        compute._DISK_PROTECTED.set(True)
        dataset = compute._open(loaded)
        estimate = compute._raster_disk_bytes(dataset, profile=profile, blocksize=128)
        dataset = None
        target = tmp_path / 'target.tif'
        artifact = raster_save(loaded, _target_plan(target), profile=profile, compression=compression, blocksize=128)
        assert 0 < artifact['size_bytes'] == target.stat().st_size < estimate
        if profile == 'cog':
            assert validate_cog(raster_load(_source_plan(target)))['valid']
    assert not compute._DISK_RESERVATIONS


@pytest.mark.parametrize('failure', [False, True])
def test_disk_cog_tempfiles_use_checked_workspace_and_restore_caller_setting(tmp_path, monkeypatch, failure):
    values = np.arange(513 * 517).reshape(513, 517) % 3
    source = _source(tmp_path / 'source.tif', values)
    before = source.read_bytes()
    external = tmp_path / 'uncreated-external-directory'
    original_encode = compute.translate_cog
    observed = []
    def encode(*args, **kwargs):
        observed.append(gdal.GetConfigOption('CPL_TMPDIR'))
        if failure:
            raise RuntimeError('injected encoding failure')
        return original_encode(*args, **kwargs)
    monkeypatch.setattr(compute, 'translate_cog', encode)
    previous = gdal.GetConfigOption('CPL_TMPDIR')
    with gdal.config_option('CPL_TMPDIR', str(external), thread_local=True), raster_workspace():
        result = raster_nibble(raster_load(_source_plan(source)), 1, [7])
        target = tmp_path / 'result.tif'
        if failure:
            with pytest.raises(RuntimeError, match='injected encoding failure'):
                raster_save(result, _target_plan(target), profile='cog', blocksize=128)
            assert not target.exists()
        else:
            artifact = raster_save(result, _target_plan(target), profile='cog', blocksize=128)
            assert artifact['size_bytes'] == target.stat().st_size > 0
            np.testing.assert_array_equal(_values(target), values)
            assert validate_cog(raster_load(_source_plan(target)))['valid']
        assert observed == [str(result.workspace)]
        assert gdal.GetConfigOption('CPL_TMPDIR') == str(external)
        assert not external.exists() and not compute._DISK_RESERVATIONS
    assert gdal.GetConfigOption('CPL_TMPDIR') == previous
    assert not result.workspace.exists() and source.read_bytes() == before


@pytest.mark.parametrize('operator', ['raster_nibble', 'raster_proximity'])
@pytest.mark.parametrize('stage', ['output', 'distance', 'encoding'])
@pytest.mark.parametrize('language', ['zh-cn', 'en'])
def test_distance_disk_real_async_failure_no_publication_cleanup_and_recovery(tmp_path, monkeypatch, operator, stage, language):
    import api_server
    import workflow_engine
    source = _source(tmp_path / 'source.tif', [[7, 1], [7, 2]], dtype=gdal.GDT_Int16)
    before = source.read_bytes()
    prior = tmp_path / 'prior.tif'
    prior.write_bytes(b'previous confirmed artifact')
    target = tmp_path / 'result.tif'
    tasks = [
        {'id': 'source', 'operator': 'raster_load', 'depends_on': [], 'params': {'access_plan': _source_plan(source)}},
        {'id': 'calculate', 'operator': operator, 'depends_on': ['source'], 'params': {
            'input_raster': {'$ref': 'source'}, 'band': 1, 'target_values': [7]}},
        {'id': 'save', 'operator': 'raster_save', 'depends_on': ['calculate'], 'params': {
            'input_raster': {'$ref': 'calculate'}, 'access_plan': _target_plan(target), 'profile': 'cog', 'blocksize': 128}},
    ]
    original_execute, original_usage = workflow_engine._execute_operator, compute.shutil.disk_usage
    original_publish = compute.publish_target_file
    free, inject, published, workspaces = None, True, [], []
    def usage(path):
        return original_usage(path) if free is None else SimpleNamespace(free=free)
    def execute(name, params):
        nonlocal free
        if name == operator:
            workspaces.append(params['input_raster'].workspace)
        if inject and ((name == operator and stage != 'encoding') or (name == 'raster_save' and stage == 'encoding')):
            dataset = compute._open(params['input_raster'])
            estimate = (compute._raster_disk_bytes(dataset, profile='cog', blocksize=128) if stage == 'encoding'
                        else compute._raster_disk_bytes(dataset, numeric=True))
            free = 2 * 1024**3 + estimate - 1 + (32 * 4 if stage == 'distance' else 0)
            dataset = None
        return original_execute(name, params)
    def publish(*args):
        published.append(args)
        return original_publish(*args)
    monkeypatch.setattr(workflow_engine, '_execute_operator', execute)
    monkeypatch.setattr(compute.shutil, 'disk_usage', usage)
    monkeypatch.setattr(compute, 'publish_target_file', publish)
    client = api_server.app.test_client()
    headers = {} if language == 'zh-cn' and stage == 'output' else {'Accept-Language': language}
    response = client.post('/api/workflow', headers=headers, json={'workflow_def': {'tasks': tasks}, 'input_data': {}, 'runtime': {
        'tenant_id': 7, 'execution_authorization': {'id': 1, 'effects': ['read', 'write']}}})
    assert response.status_code == 202, response.json
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        # Querying in the other language must not alter the recorded failure.
        status = client.get('/api/executions/' + response.json['execution_id'], headers={
            'Accept-Language': 'en' if language == 'zh-cn' else 'zh-cn'}).json
        if status['status'] in ('success', 'failed'):
            break
        time.sleep(.01)
    expected = '栅格临时磁盘空间不足' if language == 'zh-cn' else 'Insufficient raster temporary disk space'
    assert status['status'] == 'failed' and expected in status['error'], status
    assert not target.exists() and not published and not compute._DISK_RESERVATIONS
    assert workspaces and all(not path.exists() for path in workspaces)
    assert source.read_bytes() == before and prior.read_bytes() == b'previous confirmed artifact'

    free, inject = None, False
    recovered = workflow_engine.execute_workflow({'tasks': tasks})
    assert recovered['status'] == 'success', recovered
    assert len(published) == 1 and not compute._DISK_RESERVATIONS
    assert all(not path.exists() for path in workspaces)
    artifact = json.loads(recovered['final_result'])
    assert artifact['size_bytes'] == target.stat().st_size > 0
    np.testing.assert_array_equal(_values(target), [[1, 1], [2, 2]] if operator == 'raster_nibble' else [[0, 2], [0, 2]])
    with raster_workspace():
        assert validate_cog(raster_load(_source_plan(target)))['valid']
    assert source.read_bytes() == before and prior.read_bytes() == b'previous confirmed artifact'


def test_distance_disk_tile_encoding_failure_retains_confirmed_publication(tmp_path, monkeypatch):
    from addp_common.workflow_runtime import WorkflowExecutionError
    source = _source(tmp_path / 'source.tif', [[7, 1], [7, 2]])
    before = source.read_bytes()
    target = tmp_path / 'tiles'
    target.mkdir()
    plan = {'schema_version': 'addp.workflow.access-plan/v1', 'target': {
        'kind': 'directory', 'format': 'tiff', 'name': target.name, 'write_mode': 'create',
        'access': {'method': 'mounted_path', 'path': str(target)}}}
    original_publish, original_usage = compute.publish_target_file, compute.shutil.disk_usage
    published = []
    def publish(*args):
        result = original_publish(*args)
        published.append(result)
        return result
    monkeypatch.setattr(compute, 'publish_target_file', publish)
    monkeypatch.setattr(compute.shutil, 'disk_usage', lambda path: (
        SimpleNamespace(free=2 * 1024**3) if published else original_usage(path)))
    with raster_workspace():
        result = raster_nibble(raster_load(_source_plan(source)), 1, [7])
        with pytest.raises(WorkflowExecutionError) as failure:
            compute.raster_save_tiles(compute.raster_tile(result, [1, 1]), plan, profile='cog', blocksize=128)
        facts = failure.value.partial_result
        assert facts['completed_tiles'] == 1 and facts['failed_tile'] == 'r0_c1'
        assert facts['publication'] == 'not_attempted'
        assert list(facts['tiles']) == ['r0_c0'] and len(published) == 1
        assert 'Insufficient raster temporary disk space' in str(failure.value.__cause__)
        saved = target / 'r0_c0.tif'
        assert facts['tiles']['r0_c0']['size_bytes'] == saved.stat().st_size > 0
        np.testing.assert_array_equal(_values(saved), [[1]])
        assert list(target.iterdir()) == [saved] and not compute._DISK_RESERVATIONS
    assert source.read_bytes() == before and not result.workspace.exists()


def test_nibble_integer_envelope_large_costs_near_ties_and_all_masks():
    rng = np.random.default_rng(146271)
    for length in (1, 2, 3, 17):
        for offset in (0, 2**53, 2**62):
            for _ in range(12):
                cost = rng.integers(0, 20, length, dtype=np.int64) + offset
                valid = rng.random(length) > .35
                distance, owners = compute._squared_distance_line(cost, valid=valid, return_sites=True)
                expected = [min((int(cost[site]) + (x - int(site)) ** 2, int(site))
                                for site in np.flatnonzero(valid))
                            for x in range(length)] if valid.any() else None
                if expected is None:
                    assert np.isinf(distance).all() and (owners == -1).all()
                else:
                    np.testing.assert_array_equal(owners, [value[1] for value in expected])
                    np.testing.assert_array_equal(distance, np.asarray([value[0] for value in expected], dtype=np.float64))
    _, owners = compute._squared_distance_line(np.array([2**62 + 2, 2**62]), return_sites=True)
    np.testing.assert_array_equal(owners, [1, 1])
    with pytest.raises(ValueError, match='integer'):
        compute._squared_distance_line(np.array([0., .5]))


def test_nibble_exhaustive_three_by_three_original_site_identities(tmp_path):
    for bits in itertools.product((False, True), repeat=9):
        if not any(bits):
            continue
        values = np.arange(9, dtype=np.float64).reshape(3, 3) - 4
        donor = np.asarray(bits).reshape(3, 3)
        codes = values[~donor].astype(int).tolist()
        path = _source(tmp_path / 'source.tif', values)
        before = path.read_bytes()
        with raster_workspace():
            loaded = raster_load(_source_plan(path))
            if not codes:
                np.testing.assert_array_equal(_values(raster_nibble(loaded, 1, [99]).path), values)
            else:
                result = raster_nibble(loaded, 1, codes)
                np.testing.assert_array_equal(_values(result.path), _oracle(values, codes, np.ones((3, 3), bool)))
            _assert_clean(loaded.workspace)
        assert path.read_bytes() == before


@pytest.mark.parametrize('values,targets', [
    ([[0, 7, -3]], [7]),
    ([[9, 7, 7, 7, -3]], [7]),
    ([[7, 0, 7], [-3, 7, 9], [7, 5, 7]], [7]),
    ([[2**53 - 1, 7, -(2**53 - 1)]], [7]),
    ([[0, 0, 9], [7, -3, 9]], [0, 7]),
])
@pytest.mark.parametrize('crs', ['EPSG:3857', 'EPSG:2277'])
def test_nibble_ties_use_original_row_column_not_label_or_propagation(tmp_path, values, targets, crs):
    values = np.array(values, dtype=np.float64)
    source = _source(tmp_path / 'source.tif', values, crs=crs)
    before = source.read_bytes()
    expected = _oracle(values, targets, np.ones(values.shape, bool))
    with raster_workspace():
        result = raster_nibble(raster_load(_source_plan(source)), 1, targets)
        np.testing.assert_array_equal(_values(result.path), expected)
        _assert_clean(result.workspace)
        target = tmp_path / 'result.cog.tif'
        artifact = raster_save(result, _target_plan(target), profile='cog', blocksize=128)
        assert artifact['size_bytes'] == target.stat().st_size > 0
        assert validate_cog(raster_load(_source_plan(target)))['valid']
        dataset = gdal.Open(str(target))
        band = dataset.GetRasterBand(1)
        assert dataset.RasterCount == 1 and band.DataType == gdal.GDT_Float64
        assert np.isnan(band.GetNoDataValue()) and band.GetUnitType() == ''
        assert band.GetScale() is None and band.GetOffset() is None
        reference = osr.SpatialReference(wkt=dataset.GetProjection())
        assert reference.GetAuthorityCode(None) == crs.split(':')[1]
        assert dataset.GetGeoTransform() == (100, 2, 0, 200, 0, -2)
        band = dataset = None
    assert source.read_bytes() == before


@pytest.mark.parametrize('shape', [(1, 1), (1, 11), (11, 1), (3, 4)])
@pytest.mark.parametrize('empty_valid', [False, True])
def test_nibble_no_targets_equivalent_single_band_output_preserves_invalid(tmp_path, shape, empty_valid):
    values = np.arange(np.prod(shape), dtype=np.float64).reshape(shape)
    valid = np.zeros(shape, bool) if empty_valid else np.ones(shape, bool)
    if values.size > 2:
        valid.flat[1] = False
    source = _source(tmp_path / 'source.tif', values, mask=valid.astype(np.uint8) * 255)
    with raster_workspace():
        loaded = raster_load(_source_plan(source))
        opened = compute._open(loaded)
        np.testing.assert_array_equal(opened.GetRasterBand(1).GetMaskBand().ReadAsArray(), valid.astype(np.uint8) * 255)
        block = next(compute._blocks(opened, [1]))
        np.testing.assert_array_equal(block[2][0], values)
        np.testing.assert_array_equal(block[3][0], valid)
        opened = None
        result = raster_nibble(loaded, 1, [90])
        expected = values.copy()
        expected[~valid] = np.nan
        np.testing.assert_array_equal(_values(result.path), expected)
        _assert_clean(loaded.workspace)


@pytest.mark.parametrize('shape', [(1, 11), (11, 1)])
def test_nibble_narrow_grids_cross_invalid_gap_without_filling_it(tmp_path, shape):
    values = np.full(shape, 7., dtype=np.float64)
    values.flat[0], values.flat[-1], values.flat[5] = 0, -3, -9999
    valid = values != -9999
    source = _source(tmp_path / 'narrow.tif', values)
    before = source.read_bytes()
    with raster_workspace():
        result = raster_nibble(raster_load(_source_plan(source)), 1, [7])
        np.testing.assert_array_equal(_values(result.path), _oracle(values, [7], valid))
        _assert_clean(result.workspace)
    assert source.read_bytes() == before


def test_nibble_cross_blocks_multiband_invalid_donors_and_transparent_barrier(tmp_path, monkeypatch):
    height, width = 515, 519
    values = np.full((height, width), 7., dtype=np.float64)
    labels = [0, -3, 9, 2**53 - 1, -(2**53 - 1)]
    positions = [(0, 0), (0, 518), (514, 0), (514, 518), (257, 260)]
    for position, label in zip(positions, labels):
        values[position] = label
    values[0, 1] = -9999
    values[10, 10], values[11, 11], values[12, 12] = np.nan, np.inf, -np.inf
    # Invalid fractional and unsafe cells must not participate in validation or allocation.
    values[20, 20], values[21, 21] = .5, 2**53
    alpha = np.full(values.shape, 128.)
    alpha[20, 20] = 0
    alpha[100:103] = 0
    alpha[25, 25], alpha[26, 26] = -1, np.nan
    mask = np.full(values.shape, 255, np.uint8)
    mask[21, 21] = 0
    # A closer apparent non-target class is invalid and cannot become a donor.
    values[50, 50] = -10
    mask[50, 50] = 0
    source = _source(tmp_path / 'source.tif', np.stack([np.full(values.shape, .25), values]), alpha=alpha, mask=mask)
    dataset = gdal.Open(str(source), gdal.GA_Update)
    dataset.GetRasterBand(2).SetScale(17)
    dataset.GetRasterBand(2).SetOffset(500)
    dataset = None
    before = source.read_bytes()
    valid = np.isfinite(values) & (values != -9999) & np.isfinite(alpha) & (alpha > 0) & (mask > 0)
    expected = _oracle(values, [7], valid)
    reads = []
    original = compute._read_values
    def bounded(band, x, y, width, height):
        reads.append((x, y, width, height))
        assert 0 < width <= 512 and 0 < height <= 512
        return original(band, x, y, width, height)
    monkeypatch.setattr(compute, '_read_values', bounded)
    with raster_workspace():
        result = raster_nibble(raster_load(_source_plan(source)), 2, [7])
        np.testing.assert_array_equal(_values(result.path), expected)
        target = tmp_path / 'large.cog.tif'
        artifact = raster_save(result, _target_plan(target), profile='cog')
        assert artifact['size_bytes'] == target.stat().st_size > 0
        assert validate_cog(raster_load(_source_plan(target)))['valid']
        saved = gdal.Open(str(target))
        np.testing.assert_array_equal(saved.GetRasterBand(1).GetMaskBand().ReadAsArray(), valid.astype(np.uint8) * 255)
        assert saved.GetRasterBand(1).GetOverviewCount() > 0
        saved = None
        _assert_clean(result.workspace)
    assert reads and any(x > 0 for x, _, _, _ in reads) and any(y > 0 for _, y, _, _ in reads)
    assert source.read_bytes() == before


@pytest.mark.parametrize('dtype,values', [
    (gdal.GDT_Int64, [[-(2**53 - 1), 7, 2**53 - 1]]),
    (gdal.GDT_UInt64, [[2**53 - 1, 7, 0]]),
    (gdal.GDT_Int32, [[-(2**31), 7, 2**31 - 1]]),
])
def test_nibble_exact_safe_integer_native_types(tmp_path, dtype, values):
    source = _source(tmp_path / 'source.tif', np.asarray(values, dtype=np.int64), dtype=dtype, nodata=None)
    with raster_workspace():
        result = raster_nibble(raster_load(_source_plan(source)), 1, [7])
        np.testing.assert_array_equal(_values(result.path), [[values[0][0], values[0][0], values[0][2]]])


@pytest.mark.parametrize('value', [2**53 - 1, 2**53, 2**53 + 1, 2**64 - 1])
@pytest.mark.parametrize('masked', [False, True])
def test_nibble_uint64_stored_values_and_valid_domain(tmp_path, value, masked):
    values = [[7, value, 1]]
    coverage = np.array([[255, 0 if masked else 255, 255]], dtype=np.uint8)
    source = _source(tmp_path / 'uint64.tif', values, dtype=gdal.GDT_UInt64,
                     nodata=None, mask=coverage if masked else None)
    dataset = gdal.Open(str(source))
    band = dataset.GetRasterBand(1)
    assert band.DataType == gdal.GDT_UInt64 and band.GetNoDataValue() is None
    assert band.ReadAsArray().dtype == np.dtype(np.uint64)
    np.testing.assert_array_equal(band.ReadAsArray(), np.asarray(values, dtype=np.uint64))
    np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray(), coverage)
    band = dataset = None
    before = source.read_bytes()
    with raster_workspace():
        loaded = raster_load(_source_plan(source))
        workspace = loaded.workspace
        if not masked and value > 2**53 - 1:
            with pytest.raises(ValueError, match='classification'):
                raster_nibble(loaded, 1, [7])
        else:
            result = raster_nibble(loaded, 1, [7])
            expected = [[1, np.nan, 1]] if masked else [[value, value, 1]]
            np.testing.assert_array_equal(_values(result.path), expected)
        _assert_clean(workspace)
    assert not workspace.exists() and source.read_bytes() == before


@pytest.mark.parametrize('operator', ['nibble', 'mode'])
@pytest.mark.parametrize('dtype,value,nodata', [
    (gdal.GDT_UInt64, 2**64 - 2, 2**64 - 1),
    (gdal.GDT_Int64, 2**53 + 1, 2**53),
    (gdal.GDT_Int64, -(2**53 + 1), -(2**53)),
])
@pytest.mark.parametrize('explicit_mask', [False, True])
def test_nibble_and_mode_native_nodata_does_not_hide_unsafe_classes(tmp_path, operator, dtype, value, nodata, explicit_mask):
    native_type = np.uint64 if dtype == gdal.GDT_UInt64 else np.int64
    values = np.array([[7, value, 1, nodata]], dtype=native_type)
    source = _source(tmp_path / 'aliased.tif', values, dtype=dtype, nodata=None,
                     mask=np.full(values.shape, 255, np.uint8) if explicit_mask else None)
    dataset = gdal.Open(str(source), gdal.GA_Update)
    band = dataset.GetRasterBand(1)
    setter = band.SetNoDataValueAsUInt64 if dtype == gdal.GDT_UInt64 else band.SetNoDataValueAsInt64
    setter(nodata)
    band = dataset = None
    dataset = gdal.Open(str(source))
    band = dataset.GetRasterBand(1)
    assert band.GetNoDataValue() == nodata
    np.testing.assert_array_equal(band.ReadAsArray(), values)
    assert band.GetMaskBand().ReadAsArray()[0, 1] == 255
    band = dataset = None
    before = source.read_bytes()
    with raster_workspace():
        loaded = raster_load(_source_plan(source))
        workspace = loaded.workspace
        with pytest.raises(ValueError, match='classification'):
            if operator == 'nibble':
                raster_nibble(loaded, 1, [7])
            else:
                compute.raster_focal_statistics(loaded, 1, 3, 'mode')
        _assert_clean(workspace)
    assert not workspace.exists() and source.read_bytes() == before


@pytest.mark.parametrize('operator', ['nibble', 'mode'])
@pytest.mark.parametrize('dtype,nodata', [
    (gdal.GDT_UInt64, 2**64 - 1), (gdal.GDT_Int64, 2**53), (gdal.GDT_Int64, -(2**53)),
])
def test_nibble_and_mode_ignore_exact_native_nodata_with_explicit_mask(tmp_path, operator, dtype, nodata):
    native_type = np.uint64 if dtype == gdal.GDT_UInt64 else np.int64
    values = np.array([[7, nodata, 1]], dtype=native_type)
    source = _source(tmp_path / 'nodata.tif', values, dtype=dtype, nodata=None,
                     mask=np.full(values.shape, 255, np.uint8))
    dataset = gdal.Open(str(source), gdal.GA_Update)
    band = dataset.GetRasterBand(1)
    setter = band.SetNoDataValueAsUInt64 if dtype == gdal.GDT_UInt64 else band.SetNoDataValueAsInt64
    setter(nodata)
    band = dataset = None
    before = source.read_bytes()
    with raster_workspace():
        loaded = raster_load(_source_plan(source))
        workspace = loaded.workspace
        result = (raster_nibble(loaded, 1, [7]) if operator == 'nibble'
                  else compute.raster_focal_statistics(loaded, 1, 3, 'mode'))
        np.testing.assert_array_equal(_values(result.path), [[1 if operator == 'nibble' else 7, np.nan, 1]])
        _assert_clean(workspace)
    assert not workspace.exists() and source.read_bytes() == before


@pytest.mark.parametrize('fault', ['geographic', 'missing-crs', 'missing-grid', 'rotated', 'south-up', 'west-up',
    'nonsquare', 'unknown-unit', 'compound', 'complex', 'alpha', 'complex-alpha', 'band-zero', 'band-bool', 'band-float',
    'band-missing', 'empty-targets', 'tuple-targets', 'float-target', 'bool-target', 'unsafe-target', 'negative-unsafe-target',
    'duplicate-target', 'none-targets', 'fraction', 'unsafe', 'negative-unsafe', 'int64-unsafe', 'uint64-unsafe', 'no-donor'])
def test_nibble_rejects_unapproved_inputs_and_cleans_workspace(tmp_path, fault):
    transform = {'missing-grid': None, 'rotated': (0, 2, .1, 0, 0, -2), 'south-up': (0, 2, 0, 0, 0, 2),
                 'west-up': (0, -2, 0, 0, 0, -2), 'nonsquare': (0, 2, 0, 0, 0, -3)}.get(fault, (0, 2, 0, 0, 0, -2))
    crs = 'EPSG:4326' if fault == 'geographic' else '' if fault == 'missing-crs' else 'EPSG:3857'
    dtype = {'complex': gdal.GDT_CFloat32, 'complex-alpha': gdal.GDT_CFloat32,
             'int64-unsafe': gdal.GDT_Int64, 'uint64-unsafe': gdal.GDT_UInt64}.get(fault, gdal.GDT_Float64)
    values = [[7, 1, 1]]
    if fault in ('fraction', 'unsafe', 'negative-unsafe', 'int64-unsafe', 'uint64-unsafe'):
        values = [[7, {'fraction': .5, 'unsafe': 2**53, 'negative-unsafe': -(2**53),
                       'int64-unsafe': 2**53 + 1, 'uint64-unsafe': 2**64 - 1}[fault], 1]]
    if fault == 'no-donor':
        values = [[7, 7, 7]]
    alpha = np.full((1, 3), 255.) if fault == 'complex-alpha' else None
    source = _source(tmp_path / 'bad.tif', values, crs=crs, transform=transform, dtype=dtype, nodata=None, alpha=alpha)
    if fault in ('alpha', 'unknown-unit', 'compound'):
        dataset = gdal.Open(str(source), gdal.GA_Update)
        if fault == 'alpha':
            dataset.GetRasterBand(1).SetColorInterpretation(gdal.GCI_AlphaBand)
        elif fault == 'compound':
            reference = osr.SpatialReference()
            reference.SetFromUserInput('EPSG:7405')
            dataset.SetProjection(reference.ExportToWkt())
        else:
            reference = osr.SpatialReference()
            reference.SetProjCS('unknown unit grid')
            reference.SetWellKnownGeogCS('WGS84')
            reference.SetMercator(0, 13, 1, 0, 0)
            reference.SetLinearUnits('unknown', 1.23456789)
            dataset.SetProjection(reference.ExportToWkt())
        dataset = None
    band = {'band-zero': 0, 'band-bool': True, 'band-float': 1., 'band-missing': 4}.get(fault, 1)
    targets = {'empty-targets': [], 'tuple-targets': (7,), 'float-target': [7.], 'bool-target': [True],
               'unsafe-target': [2**53], 'negative-unsafe-target': [-(2**53)],
               'duplicate-target': [7, 7], 'none-targets': None}.get(fault, [7])
    before = source.read_bytes()
    with raster_workspace():
        loaded = raster_load(_source_plan(source))
        workspace = loaded.workspace
        with pytest.raises(ValueError):
            raster_nibble(loaded, band, targets)
        _assert_clean(workspace)
    assert not workspace.exists() and source.read_bytes() == before


@pytest.mark.parametrize('failure', ['transpose', 'sites-transpose', 'line', 'write', 'truncated-sites', 'bad-coordinates'])
def test_nibble_injected_failure_closes_all_private_files(tmp_path, monkeypatch, failure):
    source = _source(tmp_path / 'source.tif', [[7, 1], [7, 2]])
    before = source.read_bytes()
    def fail(*args, **kwargs):
        raise RuntimeError('injected nearest-site failure')
    if failure == 'transpose':
        monkeypatch.setattr(compute, '_transpose_distance_file', fail)
    elif failure == 'sites-transpose':
        original = compute._transpose_private_file
        def transpose(*args):
            if np.dtype(args[-1]) == np.dtype(np.int64):
                fail()
            return original(*args)
        monkeypatch.setattr(compute, '_transpose_private_file', transpose)
    elif failure == 'line':
        monkeypatch.setattr(compute, '_squared_distance_line', fail)
    elif failure == 'write':
        class FailingBand:
            WriteArray = fail
        class FailingOutput:
            def GetRasterBand(self, *args):
                return FailingBand()
        monkeypatch.setattr(compute, '_numeric_output', lambda *args: FailingOutput())
    else:
        original = compute._read_site_values
        def damaged(stream, count):
            if failure == 'truncated-sites':
                stream.seek(0, 2)
                return original(stream, count)
            values = original(stream, count).copy()
            values[:] = 2**40
            return values
        monkeypatch.setattr(compute, '_read_site_values', damaged)
    with raster_workspace():
        loaded = raster_load(_source_plan(source))
        workspace = loaded.workspace
        with pytest.raises(RuntimeError):
            raster_nibble(loaded, 1, [7])
        _assert_clean(workspace)
    assert not workspace.exists() and source.read_bytes() == before


@pytest.mark.parametrize('mode', ['direct', 'reclassified', 'invalid'])
def test_nibble_real_async_cog_metadata_example_and_failure_cleanup(tmp_path, monkeypatch, mode):
    import api_server
    import workflow_engine
    values = np.array([[0., 7, 7], [7, 7, -3]])
    source = _source(tmp_path / 'source.tif', values)
    before = source.read_bytes()
    target = tmp_path / 'nibble.cog.tif'
    spec = next(item for item in list_operators() if item['name'] == 'raster_nibble')
    assert spec['execution_modes'] == ['workflow'] and spec['effects'] == ['read']
    assert spec['attributes']['resource_groups'] == ['raster']
    assert spec['output_ports'][0]['type'] == 'raster'
    assert all(p['required'] for p in spec['parameters'])
    params = dict(spec['detailed_description']['workflow_example']['params'])
    params['band'] = 99 if mode == 'invalid' else 1
    tasks = [{'id': 'source', 'operator': 'raster_load', 'depends_on': [], 'params': {'access_plan': _source_plan(source)}}]
    input_id = 'source'
    if mode == 'reclassified':
        input_id = 'classes'
        tasks.append({'id': 'classes', 'operator': 'raster_reclassify', 'depends_on': ['source'], 'params': {
            'input_raster': {'$ref': 'source'}, 'rules': [{'value': 7, 'class': 7}], 'unmatched': 'keep'}})
    params['input_raster'] = {'$ref': input_id}
    tasks.extend([{'id': 'nibble', 'operator': 'raster_nibble', 'depends_on': [input_id], 'params': params},
                  {'id': 'save', 'operator': 'raster_save', 'depends_on': ['nibble'], 'params': {
                      'input_raster': {'$ref': 'nibble'}, 'access_plan': _target_plan(target), 'profile': 'cog', 'blocksize': 128}}])
    workspaces = []
    original = workflow_engine._execute_operator
    def execute(name, params):
        if name == 'raster_nibble':
            workspaces.append(params['input_raster'].workspace)
        return original(name, params)
    monkeypatch.setattr(workflow_engine, '_execute_operator', execute)
    client = api_server.app.test_client()
    response = client.post('/api/workflow', json={'workflow_def': {'tasks': tasks}, 'input_data': {}, 'runtime': {
        'tenant_id': 7, 'execution_authorization': {'id': 1, 'effects': ['read', 'write']}}})
    assert response.status_code == 202, response.json
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        status = client.get('/api/executions/' + response.json['execution_id']).json
        if status['status'] in ('success', 'failed'):
            break
        time.sleep(.01)
    assert status['status'] == ('failed' if mode == 'invalid' else 'success'), status
    assert workspaces and all(not path.exists() for path in workspaces)
    assert source.read_bytes() == before
    if mode == 'invalid':
        assert not target.exists()
    else:
        artifact = json.loads(status['result'])
        assert artifact['size_bytes'] == target.stat().st_size > 0
        np.testing.assert_array_equal(_values(target), _oracle(values, [7], np.ones(values.shape, bool)))
        with raster_workspace():
            assert validate_cog(raster_load(_source_plan(target)))['valid']
    assert client.post('/api/operators/raster_nibble/invoke', json={'params': {}}).status_code == 403
