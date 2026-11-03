#!/usr/bin/env python3
"""Independent multi-percentile matrix over the existing physical source fixture."""
import hashlib
import importlib
import json
import os
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace

if sys.argv[1:2] == ['--worker']:
    source_fixture = sys.modules['_addp_statistics_source_fixture']
else:
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    source_fixture = importlib.import_module('business.scripts.online-raster-statistics-percentile-fixture')
    from scripts.utils.online_nfs_fixture import NFSExport, run_runtime_worker

FixtureError = source_fixture.FixtureError
SUITE = 'raster-statistics-percentiles-workflow'
RUNTIME = 'addp-raster-statistics-percentiles-runtime'
PREFIX = 'statistics-percentiles'
CASES = {
    'original': {'source': 'values.tif', 'band': 2},
    'endpoints': {'source': 'values.tif', 'band': 2, 'percentiles': [0, 100]},
    'quartiles': {'source': 'values.tif', 'band': 2, 'percentiles': [0, 25, 50, 75, 100]},
    'p50-control': {'source': 'values.tif', 'band': 2, 'percentiles': [50]},
    'fractional': {'source': 'values.tif', 'band': 2, 'percentiles': [10, 33.3, 90]},
    'reversed': {'source': 'reversed.tif', 'band': 2, 'percentiles': [10, 33.3, 90]},
    'odd': {'source': 'odd.tif', 'band': 2, 'percentiles': [0, 25, 50, 75, 100]},
    'even': {'source': 'even.tif', 'band': 2, 'percentiles': [0, 50, 100]},
    'constant': {'source': 'values.tif', 'band': 2, 'percentiles': [0, 25, 50, 75, 100], 'stage': 'reclassify', 'constant': True},
    'reclassified': {'source': 'values.tif', 'band': 2, 'percentiles': [0, 25, 50, 75, 100], 'stage': 'reclassify'},
    'int64': {'source': 'int64.tif', 'band': 2, 'percentiles': [0, 25, 50, 75, 100]},
    'uint64': {'source': 'uint64.tif', 'band': 2, 'percentiles': [0, 25, 50, 75, 100]},
    'coverage': {'source': 'coverage.tif', 'band': 2, 'percentiles': [0, 25, 50, 100]},
    'single': {'source': 'single.tif', 'band': 2, 'percentiles': [0, 33.3, 50, 100]},
    'invalid': {'source': 'invalid.tif', 'band': 2, 'percentiles': [0, 33.3, 50, 100]},
    'unlocated': {'source': 'unlocated.tif', 'band': 2, 'percentiles': [0, 25, 50, 100]},
}
FAILURES = ('list_null', 'list_boolean', 'list_number', 'list_string', 'list_object', 'list_empty',
            'item_boolean', 'item_string', 'item_null', 'item_list', 'item_object', 'item_low', 'item_high',
            'duplicate', 'descending', 'band_zero', 'band_high', 'band_boolean', 'band_string', 'complex')
SOURCES = source_fixture.SOURCES
source_arrays, seed, verify, digest, archive_sources = (source_fixture.source_arrays, source_fixture.seed,
    source_fixture.verify, source_fixture.digest, source_fixture.archive_sources)
command = source_fixture.command


def class_rules(case):
    return [{'min': None, 'max': None, 'class': 0}] if case.get('constant') else source_fixture.class_rules()


def expected_population(case):
    samples, total = source_fixture.expected_population(case)
    return ([0.] * len(samples) if case.get('constant') else samples), total


expected_result = source_fixture.expected_result


def oracle():
    populations, results = {}, {}
    for name, case in CASES.items():
        key = (case['source'], case.get('stage'), bool(case.get('constant')))
        if key not in populations: populations[key] = expected_population(case)
        results[name] = expected_result(case, *populations[key])
    return results


def fixture_sources():
    return {name: hashlib.sha256(path.read_bytes()).hexdigest() for name, path in (
        ('source_fixture', Path(source_fixture.__file__)), ('matrix_fixture', Path(__file__)))}


def worker_source():
    original_raw = Path(source_fixture.__file__).read_bytes()
    own_raw = Path(__file__).read_bytes()
    original, own = original_raw.decode('utf-8'), own_raw.decode('utf-8')
    captured = {'source_fixture': hashlib.sha256(original_raw).hexdigest(),
                'matrix_fixture': hashlib.sha256(own_raw).hexdigest()}
    # Two exact source files, one standalone stdin payload; no mutation of the old module.
    prefix = ('import types, sys\n'
              '_source = types.ModuleType("_addp_statistics_source_fixture")\n'
              f'_source.__file__ = {source_fixture.__file__!r}\n'
              f'exec(compile({original!r}, _source.__file__, "exec"), _source.__dict__)\n'
              'sys.modules[_source.__name__] = _source\n'
              f'_FIXTURE_SOURCES = {captured!r}\n')
    return prefix + own


def worker(action, root, names):
    if names or action not in {'seed', 'verify', 'oracle'}: raise FixtureError('unknown action or extra names')
    if action == 'seed': result = seed(root)
    else:
        if list(Path(tempfile.gettempdir()).glob('addp-raster-*')): raise FixtureError('Runtime execution workspace residual')
        result = {**verify(root), 'runtime_workspaces_removed': True}
        if action == 'oracle': result['expectations'] = oracle()
    return {**result, 'fixture_sources_sha256': _FIXTURE_SOURCES}


def guard(env=os.environ):
    return NFSExport(PREFIX, command).guard(env)


def owned_root(secret):
    return NFSExport(PREFIX, command).owned_root(secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])


def runtime_worker(action, root, names=()):
    source = worker_source()
    result = run_runtime_worker(command, RUNTIME, SUITE, SimpleNamespace(read_text=lambda: source), action, root, names)
    if result.get('fixture_sources_sha256') != fixture_sources(): raise FixtureError('Worker source binding differs')
    return result


def main(action):
    secret = guard()
    if action in {'start', 'stop'}: return NFSExport(PREFIX, command).lifecycle(action, secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root = owned_root(secret); state = secret/(PREFIX+'-source.json')
    result = runtime_worker(action, root, sys.argv[2:])
    if action == 'seed':
        with state.open('x') as stream: json.dump(result, stream)
    else:
        baseline = json.loads(state.read_text())
        if (result['sources'] != baseline['sources']
            or result['fixture_sources_sha256'] != baseline['fixture_sources_sha256']):
            raise FixtureError('source file or fixture source changed')
        result['archived_sources'] = archive_sources(root, Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']), result['sources'])
    return result


if __name__ == '__main__':
    try:
        value = worker(sys.argv[2], Path(sys.argv[3]), sys.argv[4:]) if sys.argv[1:2] == ['--worker'] else main(sys.argv[1])
        print(json.dumps(value, allow_nan=False, sort_keys=True))
    except Exception as error:
        print(f'statistics percentiles fixture failed: {type(error).__name__}: {error}', file=sys.stderr)
        raise SystemExit(1)
