#!/usr/bin/env python3
"""Contour -> existing GeoPython save -> exact automatic Meta scan and lineage."""
from __future__ import annotations
import importlib
import json
import os
from pathlib import Path
import subprocess
import sys

raster = importlib.import_module('scripts.test.raster-workflow-online')
fixture = importlib.import_module('business.scripts.online-raster-contour-fixture')
from scripts.utils.online_vector_persistence import run_vector_scenario
obj, SuiteError = raster.obj, raster.SuiteError
SUITE = fixture.SUITE


def definition(source, parent, name):
    if name not in fixture.TARGETS:
        raise SuiteError('unknown contour target')
    contour = {'input_raster': {'$ref': 'load'}, 'elevation_unit': 'metre', 'band': 1}
    contour.update({'levels': fixture.TARGETS[name]} if name == 'levels.gpkg' else {'interval': 100})
    return {'tasks': [
        {'id': 'load', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': source}},
        {'id': 'contour', 'operator': 'raster_contour', 'depends_on': ['load'], 'params': contour},
        {'id': 'save', 'operator': 'save', 'depends_on': ['contour'], 'params': {
            'input_df': {'$ref': 'contour'}, 'target_parent_locator': parent, 'target_name': name, 'mode': 'replace'}},
    ]}


def physical(repository, env, names):
    result = subprocess.run([sys.executable, 'business/scripts/online-raster-contour-fixture.py', 'verify', *names],
        cwd=repository, env=dict(env), text=True, capture_output=True, timeout=150)
    if result.returncode:
        raise SuiteError('independent contour physical verification failed: ' + result.stderr[-1000:])
    return obj(json.loads(result.stdout), 'physical contour evidence')


def run_scenario(repository, env, client, physical_runner=physical):
    return run_vector_scenario(repository, env, client, suite=SUITE, targets=fixture.TARGETS,
        source_name='dem.tif', engine_env='ADDP_ONLINE_CONTOUR_NFS_ENGINE_ID',
        definition=definition, physical_runner=physical_runner)


def main():
    env = dict(os.environ)
    report = run_scenario(Path(__file__).resolve().parents[2], env,
        raster.GatewayClient(env['GATEWAY_URL'], env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 30))
    path = Path(env['ADDP_ONLINE_ARTIFACT_DIR']) / 'contour-persistence.json'
    path.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(f'Contour persistence acceptance failed: {error if isinstance(error, SuiteError) else type(error).__name__}', file=sys.stderr)
        raise SystemExit(1)
