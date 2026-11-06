import base64
import copy
import hashlib
import importlib
import json
import tempfile
from pathlib import Path
from types import SimpleNamespace
import unittest
import urllib.parse
import uuid

scene = importlib.import_module('scripts.test.raster-line-of-sight-persistence-online')
fixture = scene.fixture


def expectations():
    result = {}
    for name in fixture.TARGETS:
        spec = fixture.source_spec(name.replace('.csv', '.tif'))
        row = fixture.reference(name, spec['grid'], spec['horizontal'])
        result[name] = {'row': row, 'preview_row': row}
    return result


FROZEN = expectations()
SOURCES = {name: {'size_bytes': 4096, 'sha256': hashlib.sha256(name.encode()).hexdigest()} for name in fixture.SOURCES}


def physical(repository, env, action, names, timeout):
    assert 0 < timeout <= 600
    targets = {}
    for name in names:
        row = FROZEN[name]['row']
        payload = f'distance_m,status\n{row["distance_m"]},{row["status"]}\n'.encode()
        targets[name] = {**fixture.fingerprint(payload), 'csv_base64': base64.b64encode(payload).decode()}
    result = {'sources': copy.deepcopy(SOURCES), 'targets': targets, 'runtime_workspaces_removed': True}
    if action == 'oracle':
        result['expectations'] = copy.deepcopy(FROZEN)
    return result


def diagnostic(repository, env, identifier, problem, language, timeout):
    return scene.diagnostic_fact(identifier, problem, language)


class Client:
    timeout = 30

    def __init__(self, fault=None):
        self.fault = fault
        self.scans = {}
        self.executions = {}
        self.relationships = []
        self.items = {name: {'id': i+10, 'full_name': name, 'item_type': 'file', 'size_bytes': 4096,
                             'fingerprint': name, 'scanned_at': 'now'} for i, name in enumerate(fixture.SOURCES)}

    def submit(self, body, headers):
        dag = body['content']['workflow_definition']
        name = dag['tasks'][-1]['params']['target_name']
        success = name in fixture.CASES
        identifier = str(uuid.UUID(int=len(self.executions)+1))
        language = headers['Accept-Language']
        execution = {'execution_id': identifier, 'tenant_id': 2, 'module': 'develop', 'task_type': 'workflow',
                     'status': 'success' if success else 'failed', 'triggered_by': 33,
                     'execution_config': {'engine_id': 7, 'display_language': language,
                                          'content': {'workflow_definition': dag}},
                     'error_msg': None if success else 'frozen-'+language,
                     'error_details': {} if success else {'category': 'execution_failed'},
                     'outputs': {}, 'metadata': {'result': {}}}
        if success:
            target = 'addp://engine/3/path/outputs/'+name+'?type=file'
            scan = 'scan-'+str(len(self.executions)+1)
            source = dag['tasks'][0]['params']['locator']
            expected = scene.expected_lineage(source, target)
            execution['outputs'] = {'save': {'resource': {'locator': target, 'type': 'file', 'write_mode': 'create'}}}
            execution['metadata'] = {'lineage_facts': {**expected, 'operations': None}, 'result': {
                'final_result': {'type': 'pandas_dataframe', 'schema': fixture.SCHEMA, 'row_count': 1,
                                 'preview_rows': [FROZEN[name]['preview_row']], 'preview_limit': 50, 'truncated': False},
                'meta_scan_runs': [{'status': 'submitted', 'target_locator': target, 'execution_id': scan}]}}
            self.scans[scan] = identifier
            size = physical(None, None, 'verify', [name], 1)['targets'][name]['size_bytes']
            item_id = 1000+len(self.executions)
            self.items['outputs/'+name] = {'id': item_id, 'full_name': 'outputs/'+name, 'item_type': 'file',
                                           'size_bytes': size+int(self.fault == 'size'), 'scanned_at': 'now', 'fingerprint': name,
                                           'attributes': {'item': {'format': 'csv'}}}
            source_id = self.items[name.replace('.csv', '.tif')]['id']
            self.relationships.append({'source': {'item_id': source_id}, 'target': {'item_id': item_id},
                                       'evidence': {'execution_id': identifier}, 'status': 'active', 'relation_kind': 'derive'})
            if self.fault == 'extra-meta-relationship':
                self.relationships.append({**self.relationships[-1], 'source': {'item_id': 9999}})
            if self.fault == 'public-lineage':
                execution['metadata']['lineage_facts']['operations'] = expected['operations']
        elif self.fault == 'negative-scan':
            self.scans['unexpected'] = identifier
        elif self.fault == 'negative-edge':
            self.relationships.append({'source': {'item_id': self.items['flat.tif']['id']},
                                       'target': {'item_id': 9999}, 'evidence': {'execution_id': identifier},
                                       'status': 'active', 'relation_kind': 'derive'})
        elif self.fault == 'source-meta':
            self.items['flat.tif']['fingerprint'] = 'changed'
        elif self.fault == 'target-meta':
            self.items['outputs/flat.csv']['fingerprint'] = 'changed'
        self.executions[identifier] = copy.deepcopy(execution)
        return {'execution_id': identifier}

    def request(self, method, path, expected, body=None, headers=None):
        if path.endswith('/auth/context'):
            data = {'principal': {'id': 33, 'type': 'user'}, 'context': {'type': 'tenant', 'tenant_id': '2'},
                    'token': {'type': 'first_party_access_token'}, 'authorization': {'role_assignments': [
                        {'role_key': 'raster-consumer', 'permissions': sorted(scene.raster.EDITOR_PERMISSIONS)}]}}
        elif path.endswith('/scan/run/manual'):
            assert body['targets'] == [f'addp://engine/3/path/{n}?type=file' for n in fixture.SOURCES]
            self.scans['source-scan'] = None
            data = {'execution_id': 'source-scan'}
        elif '/meta/executions/' in path:
            identifier = path.rsplit('/', 1)[-1]
            parent = self.scans[identifier]
            data = {'execution_id': identifier, 'parent_execution_id': parent,
                    'module': 'meta', 'task_type': 'scan', 'status': 'success', 'tenant_id': 2,
                    'triggered_by': 33, 'scan_context': {'engine_id': 4 if parent and self.fault == 'scan-engine' else 3}}
        elif path.endswith('/workflow-engines'):
            data = [{'id': 7, 'engine_type': 'geopython_workflow', 'connection_status': 'online'}]
        elif path.endswith('/engines/3/items'):
            data = list(self.items.values())
        elif '/meta/items/' in path:
            data = next(v for v in self.items.values() if v['id'] == int(path.rsplit('/', 1)[-1]))
        elif '/meta/scan/runs?' in path:
            data = {'total': len(self.scans), 'items': [{'execution_id': n} for n in self.scans]}
        elif '/meta/lineage/graph?' in path:
            query = urllib.parse.parse_qs(path.split('?', 1)[1])
            side = 'target' if query['direction'] == ['upstream'] else 'source'
            data = {'truncated': False, 'edges': [edge for edge in self.relationships
                    if edge[side]['item_id'] == int(query['item_id'][0])]}
        elif path == '/api/v1/develop/executions' and method == 'POST':
            data = self.submit(body, headers)
        elif '/develop/executions/' in path or '/monitor/executions/' in path:
            data = copy.deepcopy(self.executions[path.rsplit('/', 1)[-1]])
            if self.fault == 'language' and headers and data['status'] == 'failed':
                data['error_msg'] = 'rewritten'
            if self.fault == 'monitor' and '/monitor/' in path:
                data['execution_id'] = 'other'
        else:
            raise AssertionError((method, path, body))
        return SimpleNamespace(payload=copy.deepcopy(data), raw=json.dumps(data).encode(), status=expected[0])


class LineOfSightWorkflowTest(unittest.TestCase):
    def run_case(self, fault=None, worker=physical, diagnostic_runner=diagnostic, budget='600'):
        root = Path(self.enterContext(tempfile.TemporaryDirectory()))
        env = {'ONLINE_SUITE': scene.SUITE, 'ADDP_ONLINE_TEST_RUN_ID': 'line-of-sight-123-1',
               'ADDP_ONLINE_TEST_TENANT_ID': '2', 'ADDP_ONLINE_LINE_OF_SIGHT_NFS_ENGINE_ID': '3',
               'ADDP_ONLINE_TEST_TIMEOUT_SECONDS': budget, 'ADDP_ONLINE_ARTIFACT_DIR': str(root/'evidence')}
        client = Client(fault)
        self.evidence = root/'evidence'

        def reader(env, identifier, tenant, principal, dag):
            self.assertEqual((tenant, principal), (2, 33))
            execution = client.executions[identifier]
            self.assertEqual(execution['execution_config']['content']['workflow_definition'], dag)
            name = dag['tasks'][-1]['params']['target_name']
            expected = scene.expected_lineage(dag['tasks'][0]['params']['locator'],
                                              'addp://engine/3/path/outputs/'+name+'?type=file')
            if fault == 'internal-lineage':
                expected['operations'] = []
            return expected

        return scene.run_scenario(root, env, client, worker, diagnostic_runner, reader)

    def test_complete_fixed_matrix_rereads_and_original_manifest(self):
        calls = []

        def tracked(*args):
            calls.append(tuple(args[3]))
            return physical(*args)

        result = self.run_case(worker=tracked)
        self.assertEqual([len(names) for names in calls], [0, *range(1, 17), *([16]*36)])
        self.assertEqual((len(result['cases']), len(result['failed_parameters']),
                          result['full_target_checks'], result['physical_verification_calls']), (16, 36, 712, 53))
        self.assertEqual({(case['problem'], case['language']) for case in result['failed_parameters']},
                         {(problem, language) for problem in scene.FAILURES for language in ('zh-cn', 'en')})
        self.assertEqual(len({case['execution_id'] for case in result['cases']+result['failed_parameters']}), 52)
        self.assertEqual(len(result['archived_csvs']), 16)
        manifest = json.loads((self.evidence/'line-of-sight-csv-manifest.json').read_bytes())
        self.assertEqual(manifest, result['archived_csvs'])
        for name, value in manifest.items():
            data = (self.evidence/value['path']).read_bytes()
            self.assertEqual(fixture.parse_csv(data, FROZEN[name]['row']), FROZEN[name]['row'])
            self.assertEqual(fixture.fingerprint(data), {key: value[key] for key in ('size_bytes', 'sha256')})
        for case in result['cases']:
            self.assertEqual(case['meta_relation']['execution_id'], case['execution_id'])
            self.assertEqual(case['scan_identity']['parent_execution_id'], case['execution_id'])
            self.assertEqual(case['meta_size_bytes'], case['physical_size_bytes'])

    def test_identity_language_lineage_scan_size_and_metadata_fail_closed(self):
        for fault in ('size', 'public-lineage', 'internal-lineage', 'extra-meta-relationship', 'scan-engine', 'monitor',
                      'negative-scan', 'negative-edge', 'source-meta', 'target-meta', 'language'):
            with self.subTest(fault=fault), self.assertRaises(scene.SuiteError):
                self.run_case(fault=fault)

    def test_wrong_exact_cause_and_expanded_budget_fail(self):
        def wrong(*args):
            return {**diagnostic(*args), 'exact_cause': 'different'}
        with self.assertRaisesRegex(scene.SuiteError, 'wrong exact failure'):
            self.run_case(diagnostic_runner=wrong)
        with self.assertRaisesRegex(scene.SuiteError, 'exactly 600'):
            self.run_case(budget='601')

    def test_missing_actual_reread_or_changed_source_or_raw_bytes_fails(self):
        for fault in ('missing', 'source', 'raw', 'size', 'cleanup'):
            def broken(repository, env, action, names, timeout):
                report = physical(repository, env, action, names, timeout)
                if len(names) == 2:
                    if fault == 'missing':
                        report['targets'].pop(names[0])
                    elif fault == 'source':
                        report['sources']['flat.tif']['size_bytes'] += 1
                    elif fault == 'raw':
                        report['targets'][names[0]]['csv_base64'] = 'aGkK'
                    elif fault == 'size':
                        report['targets'][names[0]]['size_bytes'] = True
                    else:
                        report['runtime_workspaces_removed'] = False
                return report
            with self.subTest(fault=fault), self.assertRaises(scene.SuiteError):
                self.run_case(worker=broken)


if __name__ == '__main__':
    unittest.main()
