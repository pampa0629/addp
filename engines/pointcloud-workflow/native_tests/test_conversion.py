"""Real CLI conversions in the private runtime, with disposable input/output only."""
import json
import os
import subprocess
from pathlib import Path

import pytest

from operators import invoke_operator


@pytest.mark.parametrize('source_format', ['las', 'laz', 'e57', 'pcd', 'xyz'])
def test_real_operator_publishes_copc_and_keeps_source(tmp_path, source_format):
    pdal = os.environ['POINTCLOUD_PDAL_BIN']
    xyz = tmp_path / 'points.xyz'
    xyz.write_text('1 2 3\n4 5 6\n7 8 9\n')
    source = tmp_path / ('points.' + source_format)
    if source_format != 'xyz':
        command = [pdal, 'translate', str(xyz), str(source), '--reader', 'readers.text',
                   '--readers.text.header=X Y Z', '--readers.text.skip=0']
        if source_format == 'pcd':
            source.write_text('VERSION .7\nFIELDS x y z\nSIZE 4 4 4\nTYPE F F F\nCOUNT 1 1 1\nWIDTH 3\nHEIGHT 1\nVIEWPOINT 0 0 0 1 0 0 0\nPOINTS 3\nDATA ascii\n1 2 3\n4 5 6\n7 8 9\n')
        else:
            subprocess.run(command, check=True, capture_output=True, text=True, timeout=60)
    initial = source.read_bytes()
    target = tmp_path / 'published/points.copc.laz'
    plan = {
        'schema_version': 'addp.workflow.access-plan/v1',
        'source': {'kind': 'file', 'format': source_format,
                   'access': {'method': 'mounted_path', 'path': str(source)},
                   'metadata': {'source_size_bytes': source.stat().st_size}},
        'target': {'kind': 'file', 'format': 'copc', 'name': target.name,
                   'write_mode': 'create', 'content_type': 'application/vnd.laszip+copc',
                   'access': {'method': 'mounted_path', 'path': str(target)}},
    }
    work = tmp_path / 'work'
    facts = invoke_operator(source_format + '_to_copc', {'access_plan': plan, 'options': {'threads': 2}},
                            env=dict(os.environ, POINTCLOUD_WORK_DIR=str(work)))
    assert facts['copc_uri'] == str(target)
    result = subprocess.run([pdal, 'info', '--summary', str(target)], check=True,
                            capture_output=True, text=True, timeout=60)
    assert json.loads(result.stdout)['summary']['num_points'] == 3
    assert source.read_bytes() == initial
    assert not list(work.iterdir())
