#!/usr/bin/env python3
"""Eight real platform executions against one bounded temporary filesystem."""
from __future__ import annotations
import importlib
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time
import urllib.parse

raster = importlib.import_module('scripts.test.raster-workflow-online')
rasterize = importlib.import_module('scripts.test.vector-rasterize-persistence-online')
fixture = importlib.import_module('business.scripts.online-raster-disk-pressure-fixture')
capacity = importlib.import_module('scripts.test.raster-nibble-capacity-online')
scan_ids = importlib.import_module('scripts.test.vector-pmtiles-workflow-online').scan_ids
SuiteError, obj = raster.SuiteError, raster.obj
SUITE = fixture.SUITE


def definition(source, parent, op, stage):
    if op not in ('nibble', 'proximity') or stage not in ('baseline', 'distance', 'encoding', 'recovered'):
        raise SuiteError('unknown pressure case')
    name = f'{op}-{"recovered" if stage == "encoding" else stage}.cog.tif'
    return {'tasks': [
        {'id': 'source', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': source}},
        {'id': 'compute', 'operator': 'raster_'+op, 'depends_on': ['source'],
         'params': {'input_raster': {'$ref': 'source'}, 'band': 2, 'target_values': [2] if op == 'nibble' else [0, 1]}},
        {'id': 'save', 'operator': 'raster_save', 'depends_on': ['compute'],
         'params': {'input_raster': {'$ref': 'compute'}, 'target_parent_locator': parent, 'target_name': name,
                    'write_mode': 'create', 'profile': 'cog', 'blocksize': 4096}}]}


def tool(repository, env, action, *names):
    p = subprocess.run([sys.executable, 'business/scripts/online-raster-disk-pressure-fixture.py', action, *names],
                       cwd=repository, env=dict(env), capture_output=True, text=True, timeout=150)
    if p.returncode:
        raise SuiteError('owned pressure fixture failed: '+p.stderr[-1000:])
    return obj(json.loads(p.stdout), 'pressure fixture result')


def verify_failure(execution, phase, observation, language):
    if execution.get('status') != 'failed' or execution.get('outputs'):
        raise SuiteError('pressure did not fail without outputs')
    metadata = obj(execution.get('metadata'), 'failed metadata')
    result = obj(metadata.get('result') or {}, 'failed result')
    facts = obj(metadata.get('lineage_facts') or {}, 'failed lineage facts')
    if result.get('meta_scan_runs') or facts.get('outputs'):
        raise SuiteError('pressure rejection published scan or output lineage')
    message = execution.get('error_msg')
    if not isinstance(message, str):
        raise SuiteError('pressure failure message missing')
    expected_phase = 'distance transform' if phase == 'distance' else 'raster encoding'
    prefix = {'zh-cn': '栅格临时磁盘空间不足', 'en': 'Insufficient raster temporary disk space'}[language]
    if expected_phase not in message or prefix not in message:
        raise SuiteError('failure is not the expected submitted-language disk guard phase')
    values = {k: int(v) for k, v in re.findall(r'(required_bytes|available_bytes|pending_bytes|safety_bytes)=(\d+)', message)}
    pixels = fixture.WIDTH*fixture.HEIGHT
    # Independent fixed-grid arithmetic, not imports of private product estimates.
    working = 2*(16*(256**2*9+64))+8*1024**2
    encoding = 2*(2*(4096**2*9+64)+8*1024**2)
    if (set(values) != {'required_bytes', 'available_bytes', 'pending_bytes', 'safety_bytes'}
        or values['required_bytes'] != (32*pixels if phase == 'distance' else encoding)
        or values['pending_bytes'] != (working if phase == 'distance' else 0)
        or values['safety_bytes'] != fixture.SAFETY
        or abs(values['available_bytes']-observation['available_bytes']) > 8*1024**2
        or values['available_bytes'] >= sum(values[k] for k in ('required_bytes', 'pending_bytes', 'safety_bytes'))):
        raise SuiteError('actual filesystem capacity and guard budget disagree')
    if any(v in message for v in ('/raster-pressure-tmp', '/home/runner/', 'No space left on device', 'ENOSPC')):
        raise SuiteError('rejection exposes a private path or is an uncontrolled write failure')
    return {'phase': expected_phase, **values, 'language': language}


class SubmittedLanguageClient:
    def __init__(self, client, language):
        self.client, self.language = client, language

    def request(self, method, path, expected, body=None, headers=None):
        return self.client.request(method, path, expected, body,
                                   {**(headers or {}), 'Accept-Language': self.language})


def source_edges(client, source_id):
    q = urllib.parse.urlencode({'subject_kind': 'data_item', 'item_id': source_id, 'direction': 'downstream'})
    graph = obj(client.request('GET', '/api/v1/meta/lineage/graph?'+q, (200,)).payload, 'lineage graph')
    if graph.get('truncated'):
        raise SuiteError('source lineage graph truncated')
    return sorted(json.dumps(v, sort_keys=True) for v in raster.array(graph.get('edges'), 'edges')
                  if v.get('status') == 'active' and v.get('relation_kind') == 'derive')


def run_scenario(repository, env, client, operate=tool):
    deadline = time.monotonic()+float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '600'))
    def remaining():
        value = deadline-time.monotonic()
        if value <= 0: raise SuiteError('disk pressure business budget exceeded')
        return value
    tenant = raster.positive(int(env['ADDP_ONLINE_TEST_TENANT_ID']), 'tenant')
    if tenant <= 1: raise SuiteError('pressure acceptance forbids default tenant')
    identity = raster.validate_identity(client, tenant, required_permissions=raster.EDITOR_PERMISSIONS)
    engine = raster.positive(int(env['ADDP_ONLINE_DISK_PRESSURE_NFS_ENGINE_ID']), 'NFS engine')
    source = f'addp://engine/{engine}/path/{fixture.SOURCE}?type=file'
    scan = raster.support.wait_for_meta_scan(client, engine, deadline, scope={'targets': [source]})
    item = raster.support.find_fixture_item(client, engine, fixture.SOURCE, 'pressure source')
    original = client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload
    source += f'&item_id={item["id"]}'
    engines = raster.array(client.request('GET', '/api/v1/develop/workflow-engines', (200,)).payload, 'engines')
    selected = [v for v in engines if v.get('engine_type') == 'geopython_workflow' and v.get('connection_status') == 'online']
    if len(selected) != 1: raise SuiteError('expected one online product Runtime')
    runtime = raster.positive(selected[0]['id'], 'Runtime engine')
    runtime_identity = operate(repository, env, 'runtime-identity')
    parent = f'addp://engine/{engine}/path/outputs?type=directory'
    cases, failures, names, scans, previous = [], [], [], [scan], {}
    observations = []
    try:
        for op in ('nibble', 'proximity'):
            for stage in ('baseline', 'distance', 'encoding', 'recovered'):
                remaining()
                phase = stage if stage in ('distance', 'encoding') else 'release'
                observation = operate(repository, env, phase); observations.append(observation)
                old_scans, old_edges = scan_ids(client), source_edges(client, item['id'])
                language = 'zh-cn' if op == 'nibble' else 'en'
                workflow = definition(source, parent, op, stage)
                workflow_hash = hashlib.sha256(json.dumps(workflow, sort_keys=True).encode()).hexdigest()
                identifier = raster.submit(SubmittedLanguageClient(client, language), runtime, workflow)
                failure = stage in ('distance', 'encoding')
                execution = raster.wait_execution(client, 'develop', identifier, remaining(), expected='failed' if failure else 'success')
                name = workflow['tasks'][-1]['params']['target_name']; target = parent.replace('?type=directory', '/'+name+'?type=file')
                if failure:
                    diagnostic = verify_failure(execution, stage, observation, language)
                    opposite = SubmittedLanguageClient(client, 'en' if language == 'zh-cn' else 'zh-cn')
                    reread = opposite.request('GET', '/api/v1/develop/executions/'+identifier, (200,)).payload
                    if reread.get('error_msg') != execution.get('error_msg'):
                        raise SuiteError('query language rewrote the recorded failure')
                    if scan_ids(client) != old_scans or source_edges(client, item['id']) != old_edges:
                        raise SuiteError('rejected workflow added a scan or derived lineage')
                    failures.append({'execution_id': identifier, 'operator': op, **diagnostic,
                                     'no_publication_or_scan_or_lineage': True, 'workflow_sha256': workflow_hash, 'target_locator': target})
                else:
                    facts, output_scan = raster.validate_success(execution, [source], target, 'create', fixture.expectation(), resource_type='file')
                    if output_scan in scans: raise SuiteError('automatic scans must be distinct')
                    scanned = raster.wait_execution(client, 'meta', output_scan, remaining())
                    scan_fact = capacity.validate_scan(scanned, output_scan, identifier, tenant, int(identity['principal_id']), engine)
                    monitor = obj(client.request('GET', '/api/v1/monitor/executions/by-execution-id/'+identifier, (200,)).payload, 'Monitor')
                    if monitor.get('status') != 'success' or monitor.get('module') != 'develop' or monitor.get('metadata', {}).get('lineage_facts') != facts:
                        raise SuiteError('Monitor status/owner/facts differ')
                    scans.append(output_scan); names.append(name)
                native = operate(repository, env, 'verify', *names)
                rasterize.check_physical(native, previous)
                if failure:
                    catalog = client.request('GET', f'/api/v1/meta/engines/{engine}/items', (200,)).payload
                    if any(v.get('full_name') == 'outputs/'+name for v in raster.array(catalog, 'catalog items')):
                        raise SuiteError('rejected target registered in Meta')
                else:
                    target_item = raster.support.find_fixture_item(client, engine, 'outputs/'+name, 'pressure COG')
                    detail = obj(client.request('GET', f'/api/v1/meta/items/{target_item["id"]}', (200,)).payload, 'target')
                    validate_target(detail, fixture.expectation(), native['targets'][name])
                    sizes = raster.strict_sizes(execution['metadata']['result']['final_result'], native['targets'][name], detail, name)
                    lineage = raster.wait_lineage(client, item['id'], detail['id'], identifier, remaining())
                    cases.append({'name': name, 'execution_id': identifier, 'automatic_scan_execution_id': output_scan,
                                  'automatic_scan_observation': scan_fact, 'size_evidence': sizes,
                                  'physical': native['targets'][name], 'lineage': lineage, 'monitor_verified': True,
                                  'workflow_sha256': workflow_hash, 'target_locator': target})
                previous = native['targets'].copy()
                if operate(repository, env, 'runtime-identity') != runtime_identity:
                    raise SuiteError('product Runtime or master/worker restarted')
                remaining()
        if scan_ids(client) != sorted(scans) or len(cases) != 4 or len(failures) != 4:
            raise SuiteError('pressure inventory differs from eight executions')
        for op in ('nibble', 'proximity'):
            rejected = next(v for v in failures if v['operator'] == op and v['phase'] == 'raster encoding')
            recovered = next(v for v in cases if v['name'] == op+'-recovered.cog.tif')
            if any(rejected[k] != recovered[k] for k in ('workflow_sha256', 'target_locator')):
                raise SuiteError('recovery is not a fresh execution of the exact rejected workflow/target')
        if client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload != original:
            raise SuiteError('source metadata changed')
        return {'schema_version': 'addp.raster-disk-pressure/v1', 'suite': SUITE, 'result': 'passed',
                'identity': identity, 'source': source, 'source_scan_execution_id': scan, 'source_unchanged': True,
                'previous_artifacts_unchanged': True, 'runtime_identity': runtime_identity,
                'observations': observations, 'cases': cases, 'failures': failures, 'physical': native,
                'coverage': 'API low-disk rejection and recovery; no concurrent race or unreadable-capacity Hosted claim'}
    finally:
        # The lifecycle unmounts even if release fails; never mask the business failure.
        failure = sys.exc_info()[1]
        try: operate(repository, env, 'release')
        except Exception as error:
            if failure is None: raise
            raise SuiteError(f'{failure}; pressure release also failed: {error}') from failure


def validate_target(detail, expectation, physical):
    # A 1024² image with explicit 4096 tiles correctly has no overview level.
    raster.validate_target_item(detail, expectation, physical)


def main():
    env = dict(os.environ)
    result = run_scenario(Path(__file__).resolve().parents[2], env,
        raster.GatewayClient(env['GATEWAY_URL'], env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 30))
    (Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'disk-pressure.json').write_text(json.dumps(result, ensure_ascii=False, indent=2)+'\n')
    print(json.dumps(result, ensure_ascii=False, sort_keys=True))


if __name__ == '__main__':
    try: main()
    except Exception as error:
        print(f'Disk pressure acceptance failed: {error if isinstance(error, SuiteError) else type(error).__name__}', file=sys.stderr)
        raise SystemExit(1)
