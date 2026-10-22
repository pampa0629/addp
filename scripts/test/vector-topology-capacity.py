#!/usr/bin/env python3
"""Manual, bounded Native line-topology capacity; deliberately not business T4."""
from __future__ import annotations

import argparse
from dataclasses import asdict, dataclass
import hashlib
import json
import math
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import uuid

CPUS = 4
MEMORY = 2 << 30
SECONDS = 180
SCHEMA = 'addp.vector-topology-capacity/v1'


@dataclass(frozen=True)
class Case:
    kind: str
    size: int
    operator: str

    @property
    def key(self):
        return f'{self.kind}-{self.size}-{self.operator}'


def matrix():
    return [Case(kind, size, operator)
            for kind, sizes in [('many', (1000, 10000, 100000)),
                                ('long', (1000, 10000, 100000)),
                                ('dense', (100, 300, 1000))]
            for size in sizes for operator in ('overlap', 'self')
            if kind != 'long' or operator == 'self']


def coordinates(case, row):
    if case.kind == 'many':
        y = (row // 2) * 20
        return [[x, y] for x in range(9)] + [[4, y], [0, y]]
    if case.kind == 'long':
        return [[x, 0] for x in range(case.size)] + [[case.size - 2, 0]]
    return [[0, 0], [8, 0]]


def source_rows(case):
    return case.size if case.kind == 'many' or case.operator == 'overlap' else 1


def source_id(index):
    return f'route-{index:09d}'


def expected_rows(case):
    if case.operator == 'self':
        return source_rows(case)
    return case.size // 2 if case.kind == 'many' else case.size * (case.size - 1) // 2


def expected(case):
    """Closed-form identities and coordinates, independent of GEOS intersection."""
    if case.operator == 'self':
        for i in range(source_rows(case)):
            if case.kind == 'many':
                y = (i // 2) * 20
                parts = [[[x, y], [x + 1, y]] for x in range(8)]
            elif case.kind == 'long':
                parts = [[[case.size - 2, 0], [case.size - 1, 0]]]
            else:
                parts = [[[0, 0], [8, 0]]]
            yield {'source_id': source_id(i), 'overlap_length': 1.0 if case.kind == 'long' else 8.0}, parts
    else:
        pairs = ((i, i + 1) for i in range(0, case.size, 2)) if case.kind == 'many' else (
            (i, j) for i in range(case.size) for j in range(i + 1, case.size))
        for i, j in pairs:
            y = (i // 2) * 20 if case.kind == 'many' else 0
            parts = [[[x, y], [x + 1, y]] for x in range(8)] if case.kind == 'many' else [[[0, 0], [8, 0]]]
            yield {'source_id_a': source_id(i), 'source_id_b': source_id(j), 'overlap_length': 8.0}, parts


def canonical(parts):
    return sorted(min(tuple(map(tuple, p)), tuple(map(tuple, reversed(p)))) for p in parts)


def digest(path):
    checksum = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1 << 20), b''):
            checksum.update(block)
    return {'size_bytes': path.stat().st_size, 'sha256': checksum.hexdigest()}


def write_json(path, value):
    pending = path.with_suffix('.pending')
    pending.write_text(json.dumps(value, ensure_ascii=False, indent=2))
    pending.replace(path)


def seed(case, path):
    from osgeo import ogr, osr
    ogr.UseExceptions()
    crs = osr.SpatialReference(); crs.ImportFromEPSG(3857)
    ds = ogr.GetDriverByName('GPKG').CreateDataSource(str(path))
    layer = ds.CreateLayer('routes', crs, ogr.wkbUnknown, options=['GEOMETRY_NAME=shape'])
    for name in ('route_key', 'note'):
        layer.CreateField(ogr.FieldDefn(name, ogr.OFTString))
    layer.StartTransaction()
    for i in range(source_rows(case)):
        feature = ogr.Feature(layer.GetLayerDefn())
        feature.SetField('route_key', source_id(i)); feature.SetField('note', f'original-{i}')
        points = coordinates(case, i)
        shape = {'type': 'MultiLineString', 'coordinates': [points] * case.size} if case.kind == 'dense' and case.operator == 'self' else {'type': 'LineString', 'coordinates': points}
        feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps(shape)))
        if layer.CreateFeature(feature) != 0:
            raise RuntimeError('cannot seed source')
    layer.CommitTransaction(); ds = None


def frame_digest(frame):
    checksum = hashlib.sha256()
    checksum.update(str((frame.columns.tolist(), frame.dtypes.astype(str).tolist(), str(frame.crs))).encode())
    for index, row in frame.iterrows():
        checksum.update(str((index, row['route_key'], row['note'])).encode())
        checksum.update(row[frame.geometry.name].wkb)
    return checksum.hexdigest()


def verify_source(frame, case):
    assert len(frame) == source_rows(case) and frame.crs.to_epsg() == 3857
    for i, row in enumerate(frame.itertuples(index=False, name=None)):
        values = dict(zip(frame.columns, row)); shape = values[frame.geometry.name]
        assert values['route_key'] == source_id(i) and values['note'] == f'original-{i}'
        points = coordinates(case, i)
        if case.kind == 'dense' and case.operator == 'self':
            assert shape.geom_type == 'MultiLineString' and len(shape.geoms) == case.size
            assert all([list(p) for p in part.coords] == points for part in shape.geoms)
        else:
            assert shape.geom_type == 'LineString' and [list(p) for p in shape.coords] == points


def verify_output(case, path):
    from osgeo import ogr
    ds = ogr.Open(str(path), 0); assert ds and ds.GetLayerCount() == 1
    layer = ds.GetLayer(0); assert layer.GetSpatialRef().GetAuthorityCode(None) == '3857'
    fields = ['source_id', 'overlap_length'] if case.operator == 'self' else ['source_id_a', 'source_id_b', 'overlap_length']
    assert [f.GetName() for f in layer.schema] == fields
    assert all(layer.GetLayerDefn().GetFieldDefn(i).GetType() == (ogr.OFTReal if name == 'overlap_length' else ogr.OFTString) for i, name in enumerate(fields))
    assert layer.GetFeatureCount() == expected_rows(case)
    checksum = hashlib.sha256(); iterator = iter(layer)
    for i, (properties, parts) in enumerate(expected(case)):
        feature = next(iterator); shape = feature.GetGeometryRef()
        assert feature.GetFID() == i + 1 and shape.GetGeometryName() == 'MULTILINESTRING'
        assert shape.GetCoordinateDimension() == 2 and shape.IsValid()
        actual = {name: feature.GetField(name) for name in fields}; assert actual == properties
        actual_parts = [[list(p[:2]) for p in shape.GetGeometryRef(j).GetPoints()] for j in range(shape.GetGeometryCount())]
        assert canonical(actual_parts) == canonical(parts)
        length = math.fsum(math.hypot(b[0] - a[0], b[1] - a[1]) for part in actual_parts for a, b in zip(part, part[1:]))
        assert length == properties['overlap_length'] and all(actual[k] != str(feature.GetFID()) for k in fields if k != 'overlap_length')
        checksum.update(json.dumps([actual, canonical(actual_parts)], sort_keys=True).encode())
    assert next(iterator, None) is None
    return {'rows': expected_rows(case), 'full_result_sha256': checksum.hexdigest(), 'all_coordinates_ids_lengths_verified': True, 'crs': 'EPSG:3857'}


def run_worker(case, root):
    """One process, one full measurement including generation and independent checks."""
    import resource
    sys.path.insert(0, '/app')
    from operators.io_operators import load, save
    from operators.geometric_operators import check_line_overlap, check_line_self_overlap
    started = time.monotonic(); report = {'case': asdict(case), 'result': 'incomplete', 'timings': {}}
    progress = root / 'progress.json'

    def phase(name, operation):
        report['stage'] = name; write_json(progress, report); before = time.monotonic()
        value = operation(); report['timings'][name] = time.monotonic() - before
        write_json(progress, report); return value

    source = root / 'source.gpkg'; target = root / 'result.gpkg'
    phase('generate', lambda: seed(case, source)); original = digest(source)
    connection = {'engine_type': 'nfs', 'mount_path': str(root)}
    frame = phase('read', lambda: load(connection_info=connection, path='source.gpkg'))
    phase('source_oracle', lambda: verify_source(frame, case)); before = frame_digest(frame)
    operator = check_line_self_overlap if case.operator == 'self' else check_line_overlap
    result = phase('compute', lambda: operator(frame, 'route_key'))
    assert len(result) == expected_rows(case)
    phase('save', lambda: save(result, connection_info=connection, path='result.gpkg', mode='fail'))
    physical = phase('reload_oracle', lambda: verify_output(case, target))
    assert frame_digest(frame) == before and digest(source) == original
    report.update(result='passed', stage='complete', source=original, output=digest(target), physical=physical,
                  duration_seconds=time.monotonic() - started,
                  worker_peak_rss_bytes=resource.getrusage(resource.RUSAGE_SELF).ru_maxrss * (1 if sys.platform == 'darwin' else 1024))
    import platform
    import geopandas
    import shapely
    from osgeo import gdal
    report['versions'] = {'python': platform.python_version(), 'machine': platform.machine(),
                          'geopandas': geopandas.__version__, 'shapely': shapely.__version__,
                          'geos': shapely.geos_version_string, 'gdal': gdal.VersionInfo('RELEASE_NAME')}
    write_json(progress, report); return report


def command(args, timeout=60):
    result = subprocess.run(args, text=True, capture_output=True, timeout=timeout)
    if result.returncode:
        raise RuntimeError(f'{args[:3]} exited {result.returncode}: {result.stderr[-1200:]}')
    return result.stdout


def inspect(name):
    return json.loads(command(['docker', 'inspect', name]))[0]


def owned_remove(name, run_id):
    listing = ['docker', 'container', 'ls', '--all', '--filter', f'name=^/{name}$', '--format', '{{.Names}}']
    names = command(listing).splitlines()
    if not names:
        return
    if names != [name]:
        raise RuntimeError('ambiguous container name; cleanup refused')
    value = inspect(name)
    if value['Config']['Labels'].get('com.addp.capacity-run') != run_id:
        raise RuntimeError('foreign container; cleanup refused')
    command(['docker', 'rm', '-fv', name])
    if command(listing).strip():
        raise RuntimeError('owned container remains')


def classify(returncode, timed_out, state, metrics):
    if state.get('OOMKilled') or metrics.get('oom_kill', 0) or metrics.get('oom', 0):
        return 'memory_limit'
    if timed_out:
        return 'time_limit'
    return 'passed' if returncode == 0 and state.get('Running') else 'failed'


def validate_worker(report, case):
    assert report['result'] == 'passed' and report['stage'] == 'complete' and report['case'] == asdict(case)
    assert report['physical']['rows'] == expected_rows(case) and report['physical']['all_coordinates_ids_lengths_verified'] is True
    assert report['physical']['crs'] == 'EPSG:3857' and 0 < report['duration_seconds'] <= SECONDS
    assert report['worker_peak_rss_bytes'] > 0
    assert set(report['timings']) == {'generate', 'read', 'source_oracle', 'compute', 'save', 'reload_oracle'}
    assert all(math.isfinite(v) and v >= 0 for v in report['timings'].values())
    for field in ('source', 'output'):
        assert type(report[field]['size_bytes']) is int and report[field]['size_bytes'] > 0
        checksum = report[field]['sha256']; assert len(checksum) == 64 and all(c in '0123456789abcdef' for c in checksum)


def run_case(case, image, run_id, artifact):
    from scripts.utils.capacity_resources import cgroup_directory, read_metrics
    name = f'addp-topology-capacity-{run_id}-{case.key}'
    root = artifact / case.key; root.mkdir(mode=0o700)
    workspace = root / 'data'; workspace.mkdir(mode=0o700)
    driver = Path(__file__).resolve(); outcome = {'case': asdict(case), 'result': 'incomplete'}
    created = False; process = None
    try:
        created = True  # Record the attempt before mutation, including an unknown CLI outcome.
        command(['docker', 'create', '--name', name, '--network', 'none', '--user', f'{os.getuid()}:{os.getgid()}',
                 '--label', f'com.addp.capacity-run={run_id}', '--cpus', str(CPUS), '--memory', str(MEMORY), '--memory-swap', str(MEMORY),
                 '--mount', f'type=bind,source={workspace},destination=/capacity-data',
                 '--mount', f'type=bind,source={driver},destination=/capacity.py,readonly',
                 '-e', 'WORKFLOW_BIND_HOST=127.0.0.1', '-e', 'SYSTEM_URL=http://127.0.0.1:1', image])
        config = inspect(name)
        assert config['Config']['Cmd'] == ['sh', 'container_entrypoint.sh']
        assert config['HostConfig']['NanoCpus'] == CPUS * 1_000_000_000 and config['HostConfig']['Memory'] == config['HostConfig']['MemorySwap'] == MEMORY
        command(['docker', 'start', name])
        ready = False
        for _ in range(60):
            probe = subprocess.run(['docker', 'exec', name, 'python', '-c', "import urllib.request;urllib.request.urlopen('http://127.0.0.1:8099/health',timeout=1).read()"], capture_output=True, timeout=5)
            if probe.returncode == 0:
                ready = True; break
            time.sleep(1)
        if not ready:
            raise RuntimeError('product Runtime readiness failed')
        directory = cgroup_directory(inspect(name)['State']['Pid']); before = read_metrics(directory)
        timed_out = False; started = time.monotonic(); returncode = None
        metrics = before; peak_rss = before['process_peak_rss_bytes']; samples = 0
        with (root / 'worker.log').open('w') as log, (root / 'resources.jsonl').open('w') as series:
            process = subprocess.Popen(['docker', 'exec', name, 'python', '/capacity.py', '--worker', case.key], stdout=log, stderr=subprocess.STDOUT)
            while process.poll() is None:
                try:
                    metrics = read_metrics(directory)
                    peak_rss = max(peak_rss, metrics['process_peak_rss_bytes']); samples += 1
                    series.write(json.dumps({'elapsed_seconds': time.monotonic() - started, **metrics}) + '\n'); series.flush()
                except FileNotFoundError:
                    # The cgroup may disappear after OOM kills the whole container.
                    # Docker's terminal OOMKilled fact remains authoritative below.
                    pass
                if time.monotonic() - started >= SECONDS:
                    timed_out = True
                    command(['docker', 'kill', name])
                    process.kill(); process.wait(); break
                time.sleep(.25)
            returncode = process.poll()
        elapsed = time.monotonic() - started
        try: metrics = read_metrics(directory)
        except FileNotFoundError: pass
        state = inspect(name)['State']
        outcome.update(result=classify(returncode, timed_out, state, metrics), wall_seconds=elapsed,
                       resource_before=before, resource_after=metrics, sampled_peak_rss_bytes=peak_rss,
                       resource_samples=samples, timed_out=timed_out, exit_code=returncode,
                       image_id=config['Image'], container_state=state,
                       rss_method='sum-of-observed-container-process-VmHWM; not simultaneous RSS',
                       sampling_interval_seconds=.25)
        progress = workspace / 'progress.json'
        outcome['worker'] = json.loads(progress.read_text()) if progress.is_file() else None
        if progress.is_file(): shutil.copyfile(progress, root / 'worker-progress.json')
        if outcome['result'] == 'passed':
            validate_worker(outcome['worker'], case)
            assert elapsed <= SECONDS
            assert metrics['swap_current_bytes'] == 0
    except Exception as error:
        outcome.update(result='failed', error=str(error))
    finally:
        if process is not None and process.poll() is None:
            process.kill(); process.wait()
        if created:
            try:
                logs = subprocess.run(['docker', 'logs', name], text=True, capture_output=True, timeout=5)
                (root / 'runtime.log').write_text(logs.stdout + logs.stderr)
            except Exception as error:
                outcome['runtime_log_error'] = str(error)
            try:
                owned_remove(name, run_id)
                outcome['cleanup'] = 'passed'
            except Exception as error:
                outcome.update(cleanup='failed', cleanup_error=str(error), result='failed')
        # All synthetic datasets and failure sidecars belong exclusively to this directory.
        shutil.rmtree(workspace)
        if workspace.exists(): outcome.update(result='failed', cleanup='failed')
        write_json(root / 'result.json', outcome)
    return outcome


def run_gate():
    repository = Path(__file__).resolve().parents[2]
    if (os.getenv('GITHUB_ACTIONS') != 'true' or os.getenv('RUNNER_OS') != 'Linux'
            or os.getenv('GITHUB_EVENT_NAME') != 'workflow_dispatch' or sys.platform != 'linux'):
        raise RuntimeError('full capacity requires the manual isolated GitHub Linux job')
    if len(os.sched_getaffinity(0)) < CPUS:
        raise RuntimeError('capacity host has fewer than four available CPUs')
    source = command(['git', 'rev-parse', 'HEAD']).strip()
    if source != os.environ['GITHUB_SHA']:
        raise RuntimeError('capacity source differs from checkout')
    if command(['git', 'status', '--porcelain', '--untracked-files=normal']).strip():
        raise RuntimeError('capacity requires an unchanged checkout')
    run_id = uuid.uuid4().hex[:12]
    artifact = Path(os.environ['RUNNER_TEMP']) / f'addp-topology-capacity-{os.environ["GITHUB_RUN_ID"]}-{os.environ["GITHUB_RUN_ATTEMPT"]}'
    artifact.mkdir(mode=0o700)
    image = f'localhost:5001/addp-geopython-workflow-engine:topology-capacity-{run_id}'
    registry = f'addp-topology-capacity-registry-{run_id}'
    cases = matrix()
    report = {'schema_version': SCHEMA, 'source': source, 'profile': {'cpu': CPUS, 'memory_bytes': MEMORY, 'swap_bytes': 0, 'seconds_per_case': SECONDS},
              'planned_cases': [asdict(case) for case in cases], 'groups': [], 'result': 'incomplete'}
    registry_created = False; image_attempted = False
    try:
        registry_created = True
        command(['docker', 'create', '--name', registry, '--label', f'com.addp.capacity-run={run_id}', '-p', '127.0.0.1:5001:5000', 'registry:2'])
        command(['docker', 'start', registry])
        import urllib.request
        for attempt in range(30):
            try:
                with urllib.request.urlopen('http://127.0.0.1:5001/v2/', timeout=1) as response:
                    assert response.status == 200
                break
            except OSError:
                if attempt == 29: raise RuntimeError('private registry readiness failed')
                time.sleep(.5)
        command(['docker', 'pull', 'python:3.11-slim'], timeout=180)
        command(['docker', 'tag', 'python:3.11-slim', 'localhost:5001/python:3.11-slim'])
        command(['docker', 'push', 'localhost:5001/python:3.11-slim'], timeout=180)
        image_attempted = True
        build = subprocess.run(['make', 'build-images', f'IMAGE_BUILD_ARGS=--services geopython-workflow-engine --tag topology-capacity-{run_id} --verify --jobs 1'], cwd=repository)
        if build.returncode:
            raise RuntimeError('product image build failed')
        for index, case in enumerate(cases):
            report['groups'].append(run_case(case, image, run_id, artifact))
            write_json(artifact / 'report.json', report)
            if report['groups'][-1].get('cleanup') != 'passed':
                report['not_run'] = {'reason': 'prior_case_cleanup_failed', 'cases': [asdict(c) for c in cases[index + 1:]]}
                break
        report['result'] = 'passed' if len(report['groups']) == len(cases) and all(group['result'] == 'passed' for group in report['groups']) else 'failed'
    except Exception as error:
        report.update(result='failed', error=str(error))
    finally:
        errors = [f'case cleanup failed: {group["case"]}' for group in report['groups'] if group.get('cleanup') != 'passed']
        if image_attempted and subprocess.run(['docker', 'image', 'inspect', image], capture_output=True).returncode == 0:
            try:
                command(['docker', 'image', 'rm', image])
                if subprocess.run(['docker', 'image', 'inspect', image], capture_output=True).returncode == 0:
                    raise RuntimeError('private product image remains')
            except Exception as error: errors.append(str(error))
        if registry_created:
            try: owned_remove(registry, run_id)
            except Exception as error: errors.append(str(error))
        report['cleanup'] = 'failed' if errors else 'passed'; report['cleanup_errors'] = errors
        if errors: report['result'] = 'failed'
        write_json(artifact / 'report.json', report)
    return 0 if report['result'] == 'passed' else 1


if __name__ == '__main__':
    def terminate(*_):
        raise KeyboardInterrupt('capacity interrupted')
    signal.signal(signal.SIGTERM, terminate)
    parser = argparse.ArgumentParser()
    parser.add_argument('--worker', choices=[case.key for case in matrix()])
    args = parser.parse_args()
    if args.worker:
        run_worker(next(case for case in matrix() if case.key == args.worker), Path('/capacity-data'))
    else:
        raise SystemExit(run_gate())
