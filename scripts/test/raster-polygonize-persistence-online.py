#!/usr/bin/env python3
"""Polygonize -> existing GeoPython save -> exact automatic Meta scan and lineage."""
from __future__ import annotations
import importlib
import json
import os
from pathlib import Path
import subprocess
import sys

raster = importlib.import_module('scripts.test.raster-workflow-online')
fixture = importlib.import_module('business.scripts.online-raster-polygonize-fixture')
from scripts.utils.online_vector_persistence import run_vector_scenario
obj, SuiteError = raster.obj, raster.SuiteError
SUITE = fixture.SUITE


def definition(source, parent, name):
    if name not in fixture.TARGETS:
        raise SuiteError('unknown polygonize target')
    tasks = [{'id': 'load', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': source}}]
    upstream = 'load'
    if name == 'reclassified.gpkg':
        tasks.append({'id': 'reclassify', 'operator': 'raster_reclassify', 'depends_on': ['load'],
            'params': {'input_raster': {'$ref': 'load'}, 'band': 1,
                'rules': [{'min': -2, 'max': 1, 'class': 10}, {'min': 1, 'max': 4, 'class': 20}],
                'unmatched': 'keep'}})
        upstream = 'reclassify'
    tasks.append({'id': 'polygonize', 'operator': 'raster_polygonize', 'depends_on': [upstream],
        'params': {'input_raster': {'$ref': upstream}, 'band': 1, 'connectivity': fixture.TARGETS[name]}})
    tasks.append({'id': 'save', 'operator': 'save', 'depends_on': ['polygonize'], 'params': {
        'input_df': {'$ref': 'polygonize'}, 'target_parent_locator': parent, 'target_name': name, 'mode': 'replace'}})
    return {'tasks': tasks}


def physical(repository, env, names):
    result = subprocess.run([sys.executable, 'business/scripts/online-raster-polygonize-fixture.py', 'verify', *names],
        cwd=repository, env=dict(env), text=True, capture_output=True, timeout=150)
    if result.returncode:
        raise SuiteError('independent polygonize physical verification failed: ' + result.stderr[-1000:])
    return obj(json.loads(result.stdout), 'physical polygonize evidence')


def run_scenario(repository, env, client, physical_runner=physical):
    return run_vector_scenario(repository, env, client, suite=SUITE, targets=fixture.TARGETS,
        source_name='classes.tif', engine_env='ADDP_ONLINE_POLYGONIZE_NFS_ENGINE_ID',
        definition=definition, physical_runner=physical_runner)


def main():
    env = dict(os.environ)
    report = run_scenario(Path(__file__).resolve().parents[2], env,
        raster.GatewayClient(env['GATEWAY_URL'], env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 30))
    path = Path(env['ADDP_ONLINE_ARTIFACT_DIR']) / 'polygonize-persistence.json'
    path.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(f'Polygonize persistence acceptance failed: {error if isinstance(error, SuiteError) else type(error).__name__}', file=sys.stderr)
        raise SystemExit(1)
