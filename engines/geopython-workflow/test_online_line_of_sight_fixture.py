"""Real GDAL, workflow and file evidence for the independent line-of-sight matrix."""
import base64
import importlib
import json
import math
import os
from pathlib import Path
import subprocess
import sys

import pytest

fixture = importlib.import_module('business.scripts.online-raster-line-of-sight-fixture')
scene = importlib.import_module('scripts.test.raster-line-of-sight-persistence-online')


def localized(root, name, **kwargs):
    dag = scene.definition({n: n for n in fixture.SOURCES}, 'parent', name, **kwargs)
    load, _, save = dag['tasks']
    path = root/load['params'].pop('locator')
    load['params']['access_plan'] = {'schema_version': 'addp.workflow.access-plan/v1',
        'source': {'kind': 'file', 'format': 'tiff', 'access': {'method': 'mounted_path', 'path': str(path)}}}
    params = save['params']
    output = params.pop('target_name')
    params.pop('target_parent_locator')
    params.update(connection_info={'mount_path': str(root/'outputs')}, path=output)
    return dag


@pytest.fixture(scope='module')
def line_of_sight_export(tmp_path_factory):
    root = tmp_path_factory.mktemp('line-of-sight-export')
    (root/'.addp-online-owner').write_text('line-of-sight-123-1')
    (root/'outputs').mkdir()
    return root, fixture.seed(root)


def test_line_of_sight_real_sixteen_csv_chains_preview_original_bytes_and_fail_closed_archive(line_of_sight_export, tmp_path):
    from workflow_engine import execute_workflow

    root, frozen = line_of_sight_export
    # The shared DEM fixture is immutable; give this test its own outputs.
    import shutil
    root = Path(shutil.copytree(root, tmp_path/'export'))
    archive = tmp_path/'line-of-sight-csv'
    archive.mkdir()
    previous = {}
    checked = 0
    for count, name in enumerate(fixture.TARGETS, 1):
        result = execute_workflow(localized(root, name))
        assert result['status'] == 'success', result
        scene.validate_preview(json.loads(result['all_results']['sight']), frozen['expectations'][name]['preview_row'])
        names = fixture.TARGETS[:count]
        physical = {**fixture.verify(root, names), 'runtime_workspaces_removed': True}
        previous = scene.check_and_archive(physical, names, frozen['sources'], frozen['expectations'], archive, previous)
        checked += len(names)
    assert checked == 136 and set(previous) == set(fixture.CASES)
    assert {receipt['row']['status'] for receipt in previous.values()} == {'visible', 'blocked', 'indeterminate'}
    for name, facts in previous.items():
        raw = (root/'outputs'/name).read_bytes()
        assert raw == (archive/name).read_bytes()
        assert {k: facts[k] for k in ('sha256', 'size_bytes')} == fixture.fingerprint(raw)
    code = Path(fixture.__file__).read_text()
    isolated = tmp_path/'worker'
    isolated.mkdir()
    result = subprocess.run([sys.executable, '-', '--worker', 'verify', str(root), *fixture.TARGETS],
                            input=code, text=True, capture_output=True, cwd=isolated,
                            env={**os.environ, 'TMPDIR': str(isolated)}, timeout=30)
    assert result.returncode == 0, result.stderr
    receipt = json.loads(result.stdout)
    assert receipt['runtime_workspaces_removed'] is True
    assert scene.check_and_archive(receipt, fixture.TARGETS, frozen['sources'], frozen['expectations'], archive, previous) == previous
    (archive/'flat.csv').unlink()
    with pytest.raises(scene.SuiteError, match='inventory'):
        scene.check_and_archive(receipt, fixture.TARGETS, frozen['sources'], frozen['expectations'], archive, previous)


@pytest.mark.parametrize('language', ['zh-cn', 'en'])
@pytest.mark.parametrize('problem', scene.FAILURES)
def test_line_of_sight_real_exact_failure_node_language_and_no_publication(line_of_sight_export, monkeypatch, language, problem):
    from addp_common.workflow_runtime import WorkflowExecutionError
    import workflow_engine

    root, frozen = line_of_sight_export
    dag = localized(root, 'flat.csv', invalid=problem, language=language)
    workspaces = []
    execute = workflow_engine._execute_operator

    def capture(name, params):
        if name == 'raster_line_of_sight':
            workspaces.append(params['input_raster'].workspace)
        return execute(name, params)

    monkeypatch.setattr(workflow_engine, '_execute_operator', capture)
    with pytest.raises(WorkflowExecutionError) as failure:
        workflow_engine.GeoPythonWorkflowRunner(language=language).execute(dag)
    assert failure.value.error_code == 'EXECUTION_FAILED'
    assert str(failure.value) == '任务 sight 执行失败: '+scene.diagnostic_fact('local', problem, language)['exact_cause']
    assert workspaces and all(not path.exists() for path in workspaces)
    assert fixture.verify(root, [])['sources'] == frozen['sources']


@pytest.mark.parametrize('payload', [
    b'status,distance_m\nvisible,4.0\n', b'distance_m,status\n4.0,blocked\n',
    ('distance_m,status\n'+repr(math.nextafter(4., math.inf))+',visible\n').encode(),
    b'distance_m,status\n4.0,visible\n4.0,visible\n', b'distance_m,status\nNaN,visible\n',
    b'distance_m,status\n4.0,visible,\n', b'distance_m,status\n4.0,\n',
])
def test_line_of_sight_complete_csv_oracle_rejects_corruption(payload):
    with pytest.raises(fixture.FixtureError):
        fixture.parse_csv(payload, {'distance_m': 4., 'status': 'visible'})
