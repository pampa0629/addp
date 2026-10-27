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


def run_pdal(*args):
    return subprocess.run([os.environ['POINTCLOUD_PDAL_BIN'], *map(str, args)], check=True,
                          capture_output=True, text=True, timeout=60)


def convert_fixture(tmp_path, points, source_format='xyz', options=None, source_options=None):
    xyz = tmp_path / 'fixture.xyz'
    xyz.write_text(''.join(' '.join(repr(v) for v in point) + '\n' for point in points))
    source = tmp_path / ('fixture.' + source_format)
    if source_format != 'xyz':
        command = ['translate', xyz, source, '--reader', 'readers.text',
                   '--readers.text.header=X Y Z', '--readers.text.skip=0']
        command.extend(f'--writers.las.{key}={value}' for key, value in (source_options or {}).items())
        if source_format == 'e57':
            command.append('--writers.e57.double_precision=true')
        run_pdal(*command)
    initial = source.read_bytes()
    target = tmp_path / 'published/fixture.copc.laz'
    plan = {
        'schema_version': 'addp.workflow.access-plan/v1',
        'source': {'kind': 'file', 'format': source_format,
                   'access': {'method': 'mounted_path', 'path': str(source)}},
        'target': {'kind': 'file', 'format': 'copc', 'name': target.name,
                   'write_mode': 'create', 'content_type': 'application/vnd.laszip+copc',
                   'access': {'method': 'mounted_path', 'path': str(target)}},
    }
    facts = invoke_operator(source_format + '_to_copc', {'access_plan': plan, 'options': options or {}},
                            env=dict(os.environ, POINTCLOUD_WORK_DIR=str(tmp_path / 'work')))
    assert source.read_bytes() == initial
    assert not list((tmp_path / 'work').iterdir())
    # Decode every point, not header bounds: COPC's header may describe the pre-quantized input.
    csv_path = tmp_path / 'decoded.csv'
    run_pdal('translate', target, csv_path, '--writer', 'writers.text',
             '--writers.text.order=X,Y,Z', '--writers.text.keep_unspecified=false', '--writers.text.precision=340')
    import csv
    with csv_path.open() as stream:
        decoded = [tuple(float(row[axis]) for axis in 'XYZ') for row in csv.DictReader(stream)]
    return facts['quantization'], sorted(decoded)


@pytest.mark.parametrize('source_format', ['xyz', 'e57'])
@pytest.mark.parametrize('points', [
    [(i * 0.000001, i * 0.000002, i * 0.000003) for i in range(101)],
    [(6000000 + i * 0.00001, -4000000 + i * 0.00002, 100 + i * 0.00003) for i in range(11)],
    [(i * 0.001, i * 0.002, 1.23456789) for i in range(11)],
    [(1.23456789, -2.34567891, 3.45678912)],
    [(i * 1e-25, -i * 2e-25, i * 3e-25) for i in range(11)],
])
def test_auto_precision_preserves_small_world_planar_and_single_point_clouds(tmp_path, source_format, points):
    import math
    quantization, decoded = convert_fixture(tmp_path, points, source_format)
    assert quantization['point_count'] == len(points)
    assert len(decoded) == len(points)
    assert len(set(decoded)) == len(set(points))
    assert set(quantization['selection'].values()) == {'auto'}
    for actual, expected in zip(decoded, sorted(points)):
        for index in range(3):
            scale = quantization['scale'][index]
            assert scale > 0 and math.isfinite(scale)
            assert abs(actual[index] - expected[index]) <= scale / 2 + 4 * math.ulp(expected[index])


@pytest.mark.parametrize('source_format', ['las', 'laz'])
def test_source_quantization_is_preserved_without_json_rounding(tmp_path, source_format):
    from operators import _las_quantization
    options = dict(scale_x=1.23456789012345e-7, scale_y=2e-6, scale_z=3e-5,
                   offset_x=6000000.123456789, offset_y=-4000000.123456789, offset_z=10.123456789)
    points = [(6000000.123456789 + i * 0.00001, -4000000.123456789 + i * 0.00002, 10.123456789 + i * 0.00003) for i in range(11)]
    quantization, decoded = convert_fixture(tmp_path, points, source_format, source_options=options)
    source = _las_quantization(tmp_path / ('fixture.' + source_format))
    assert quantization['scale'] == source['scale']
    assert quantization['offset'] == source['offset']
    assert quantization['point_count'] == source['point_count'] == len(points)
    assert set(quantization['selection'].values()) == {'source'}
    assert len(set(decoded)) == len(points)


def test_explicit_precision_is_applied_and_reported(tmp_path):
    points = [(0.001, 0.002, 0.003), (0.019, 0.028, 0.037)]
    options = {f'scale_{axis}': 0.01 for axis in 'xyz'}
    options.update({f'offset_{axis}': 0 for axis in 'xyz'})
    quantization, decoded = convert_fixture(tmp_path, points, options=options)
    assert quantization['scale'] == [0.01] * 3
    assert quantization['offset'] == [0] * 3
    assert set(quantization['selection'].values()) == {'explicit'}
    assert decoded == [(0, 0, 0), (0.02, 0.03, 0.04)]


def test_explicit_precision_overflow_never_publishes(tmp_path):
    from operators import ConverterError
    with pytest.raises(ConverterError):
        convert_fixture(tmp_path, [(0, 0, 0), (1, 1, 1)], options={'scale_x': 1e-12, 'offset_x': 0})
    assert not (tmp_path / 'published/fixture.copc.laz').exists()
    assert not list((tmp_path / 'work').iterdir())


def test_range_scan_reports_linear_temporary_storage_and_cleans_it(tmp_path):
    from operators import _scan_coordinate_bounds
    count = 100000
    source = tmp_path / 'large.xyz'
    with source.open('w') as stream:
        for i in range(count):
            stream.write(f'{6000000 + i * 1e-6!r} {-4000000 + i * 2e-6!r} {i * 1e-9!r}\n')
    facts = _scan_coordinate_bounds(os.environ['POINTCLOUD_PDAL_BIN'], str(source), 'xyz', tmp_path,
                                    runner=None, env=dict(os.environ), timeout_seconds=60)
    assert facts['point_count'] == count
    assert facts['min'] == [6000000, -4000000, 0]
    assert facts['max'] == [6000000 + (count - 1) * 1e-6, -4000000 + (count - 1) * 2e-6, (count - 1) * 1e-9]
    assert count * 24 < facts['temporary_bytes'] < count * 24 + 512
    assert not (tmp_path / 'coordinate-bounds.ply').exists()
    print(f"range-scan overhead: points={count}, temporary_bytes={facts['temporary_bytes']}, header_bytes={facts['temporary_bytes'] - count * 24}")


def test_explicit_auto_is_resolved_before_writer_and_can_override_source(tmp_path):
    points = [(i * 0.000001, i * 0.000002, i * 0.000003) for i in range(11)]
    quantization, decoded = convert_fixture(tmp_path, points, 'las',
        source_options={f'scale_{a}': 1e-6 for a in 'xyz'},
        options={f'{kind}_{a}': 'auto' for kind in ('scale', 'offset') for a in 'xyz'})
    assert set(quantization['selection'].values()) == {'explicit'}
    assert all(scale < 1e-6 for scale in quantization['scale'])
    assert len(set(decoded)) == len(points)
