import copy
import hashlib
import importlib
import json
import math
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

m = importlib.import_module('scripts.test.raster-statistics-percentile-workflow-online')


def expectations():
    result = {}
    for name, case in m.fixture.CASES.items():
        item = {'band': 1 if case.get('stage') else 2, 'strategy': 'full', 'valid_count': 4, 'invalid_count': 0,
                'nodata_ratio': 0., 'min': -4., 'max': 8., 'mean': 2., 'stddev': math.sqrt(20.)}
        if 'percentile' in case: item.update(percentile=float(case['percentile']), percentile_value=-4+12*case['percentile']/100)
        if name == 'invalid': item.update(valid_count=0, invalid_count=4, nodata_ratio=1., min=None, max=None, mean=None, stddev=None, percentile_value=None)
        if name == 'single': item.update(valid_count=1, invalid_count=3, nodata_ratio=.75, min=0., max=0., mean=0., stddev=0., percentile_value=0.)
        result[name] = item
    return result


def physical(repository, env, action, timeout):
    assert action in ('oracle', 'verify') and 0 < timeout <= 600
    sources = {name: {'size_bytes': 4096, 'sha256': hashlib.sha256(name.encode()).hexdigest()} for name in m.fixture.SOURCES}
    result = {'sources': sources, 'source_physical': {name: {**fact, 'full_pixels_and_mask_verified': True, 'grid_crs_scale_verified': True}
              for name, fact in sources.items()}, 'archived_sources': {name: {'path': 'statistics-sources/'+name, **fact} for name, fact in sources.items()},
              'no_outputs': True, 'runtime_workspaces_removed': True}
    if action == 'oracle': result['expectations'] = expectations()
    return result


def diagnostic(repository, env, identifier, problem, language, timeout):
    return {'execution_id': identifier, 'task': 'analysis', 'problem': problem, 'language': language,
            'exact_cause': m.MESSAGES[problem][language == 'en'], 'source': 'develop_runtime_log'}


class Client:
    mutation = None
    monitor_mutation = None
    adds_scan = False
    changed_graph = False
    changed_meta = False
    previous_changed = False
    language_rewrite = False
    scan_mutation = None
    timeout = 30

    def __init__(self):
        self.scans, self.executions, self.calls = [], {}, []
        self.sources = {name: {'id': i+10, 'full_name': name, 'item_type': 'file', 'size_bytes': 4096,
                              'fingerprint': name, 'scanned_at': '2026-10-30T00:00:00Z'} for i, name in enumerate(m.fixture.SOURCES)}

    def request(self, method, path, expected, body=None, headers=None):
        self.calls.append((method, path, copy.deepcopy(body), headers))
        if path.endswith('/auth/context'):
            data = {'principal': {'id': 33, 'type': 'user'}, 'context': {'type': 'tenant', 'tenant_id': '2'},
                    'token': {'type': 'first_party_access_token'}, 'authorization': {'role_assignments': [
                        {'role_key': 'raster-consumer', 'permissions': sorted(m.raster.EDITOR_PERMISSIONS)}]}}
        elif path.endswith('/scan/run/manual'):
            assert body['targets'] == ['addp://engine/3/path/'+name+'?type=file' for name in m.fixture.SOURCES]
            self.scans.append('source-scan'); data = {'execution_id': 'source-scan'}
        elif '/meta/executions/' in path:
            data = {'execution_id': 'source-scan', 'parent_execution_id': None, 'module': 'meta', 'task_type': 'scan',
                    'tenant_id': 2, 'status': 'success', 'triggered_by': 33, 'scan_context': {'engine_id': 3}}
            if self.scan_mutation: self.scan_mutation(data)
        elif path.endswith('/workflow-engines'):
            data = [{'id': 7, 'engine_type': 'geopython_workflow', 'connection_status': 'online'}]
        elif path.endswith('/engines/3/items'):
            data = list(self.sources.values())
        elif '/meta/items/' in path:
            data = copy.deepcopy(next(v for v in self.sources.values() if v['id'] == int(path.rsplit('/', 1)[-1])))
            if self.changed_meta and len(self.executions) == 40: data['size_bytes'] += 1
        elif '/meta/lineage/graph?' in path:
            data = {'nodes': [], 'edges': [{'relation_kind': 'derive'}] if self.changed_graph and self.executions else [], 'truncated': False}
        elif '/meta/scan/runs?' in path:
            data = {'total': len(self.scans), 'items': [{'execution_id': v} for v in self.scans]}
        elif path == '/api/v1/develop/executions' and method == 'POST':
            locators = {name: f'addp://engine/3/path/{name}?type=file&item_id={item["id"]}' for name, item in self.sources.items()}
            dag = body['content']['workflow_definition']; name = next((n for n in m.fixture.CASES if m.definition(locators, n) == dag), None)
            problem = next((p for p in m.FAILURES if m.definition(locators, 'p25', invalid=p) == dag), None)
            assert name or problem
            identifier = 'exec-'+str(len(self.executions)+1); language = headers['Accept-Language']
            data = {'execution_id': identifier, 'tenant_id': 2, 'module': 'develop', 'task_type': 'workflow',
                    'triggered_by': 33, 'status': 'success' if name else 'failed', 'outputs': {},
                    'execution_config': {'engine_id': 7, 'display_language': language, 'content': {'workflow_definition': copy.deepcopy(dag)}},
                    'metadata': {'result': {'summary': {'has_result': True}, 'final_result': expectations()[name]}} if name else {'result': {}},
                    'error_msg': None if name else 'recorded-'+language, 'error_details': {} if name else {'frozen': language}}
            if self.mutation: self.mutation(data)
            self.executions[identifier] = copy.deepcopy(data)
            if self.adds_scan: self.scans.append('unexpected-'+identifier)
            data = {'execution_id': identifier}
        elif '/develop/executions/' in path or '/monitor/executions/' in path:
            identifier = path.rsplit('/', 1)[-1]; data = copy.deepcopy(self.executions[identifier])
            if '/monitor/' in path and self.monitor_mutation: self.monitor_mutation(data)
            if self.previous_changed and identifier == 'exec-1' and len(self.executions) > 1:
                data['metadata']['result']['final_result']['mean'] += 1.
            if self.language_rewrite and headers and data['status'] == 'failed': data['error_msg'] = 'query-mutated'
        else:
            raise AssertionError((method, path, body))
        data = copy.deepcopy(data)
        return SimpleNamespace(payload=data, raw=json.dumps(data, allow_nan=False).encode(), status=expected[0])


class StatisticsPercentileWorkflowTest(unittest.TestCase):
    def setUp(self):
        self.work = tempfile.TemporaryDirectory(); self.addCleanup(self.work.cleanup)
        self.root = Path(self.work.name)
        self.env = {'ADDP_ONLINE_TEST_TENANT_ID': '2', 'ADDP_ONLINE_STATISTICS_PERCENTILE_NFS_ENGINE_ID': '3',
                    'ADDP_ONLINE_TEST_TIMEOUT_SECONDS': '600', 'ADDP_ONLINE_TEST_RUN_ID': 'statistics-percentile-123-1',
                    'ADDP_ONLINE_ARTIFACT_DIR': str(self.root/'evidence')}

    def run_case(self, client=None, worker=physical, diagnostic_runner=diagnostic):
        return m.run_scenario(self.root, self.env, client or Client(), worker, diagnostic_runner)

    def test_complete_16_success_24_failures_600_seconds_and_raw_archive(self):
        client = Client(); report = self.run_case(client)
        self.assertEqual((len(report['cases']), len(report['failed_parameters'])), (16,24))
        self.assertEqual(report['physical_verification_calls'],41)
        self.assertEqual(report['previous_result_rechecks'],504)
        self.assertEqual(report['budget_ms'],600000)
        self.assertEqual(len(report['identity']['permissions_verified']),12)
        self.assertTrue(report['only_source_scan_verified'] and report['no_resource_outputs_or_derive'])
        self.assertEqual(client.scans,['source-scan'])
        self.assertEqual(len(report['raw_response_receipts']),80)
        for receipt in report['raw_response_receipts'].values():
            path = Path(self.env['ADDP_ONLINE_ARTIFACT_DIR'])/'statistics-responses'/receipt['path']
            self.assertEqual(receipt['size_bytes'],path.stat().st_size)
            self.assertEqual(receipt['sha256'],hashlib.sha256(path.read_bytes()).hexdigest())
        self.assertEqual(json.loads((Path(self.env['ADDP_ONLINE_ARTIFACT_DIR'])/'statistics-percentile-progress.json').read_text())['result'],'passed')
        self.assertFalse(any(task['operator']=='raster_save' for _,_,body,_ in client.calls if body and 'content' in body for task in body['content']['workflow_definition']['tasks']))

    def test_current_identity_dag_runtime_language_and_no_publication_are_strict(self):
        mutations = [lambda e:e.update(triggered_by=True),lambda e:e.update(tenant_id=3),lambda e:e.update(task_type='query'),
                     lambda e:e['execution_config'].update(engine_id=8),lambda e:e['execution_config'].update(display_language='wrong'),
                     lambda e:e['execution_config']['content']['workflow_definition']['tasks'][0]['params'].update(locator='wrong'),
                     lambda e:e.update(outputs={'unexpected':'resource'}),lambda e:e['metadata'].update(lineage_facts={'derive':True}),
                     lambda e:e['metadata']['result'].update(meta_scan_runs=[{'execution_id':'wrong'}])]
        for index, mutation in enumerate(mutations):
            with self.subTest(index=index):
                client=Client();client.mutation=mutation
                self.env['ADDP_ONLINE_ARTIFACT_DIR']=str(self.root/f'mutation-{index}')
                with self.assertRaises(m.SuiteError):self.run_case(client)

    def test_scan_lineage_previous_result_and_reverse_language_cannot_change(self):
        for index, attribute in enumerate(('adds_scan','changed_graph','changed_meta','previous_changed','language_rewrite')):
            client=Client();setattr(client,attribute,True)
            self.env['ADDP_ONLINE_ARTIFACT_DIR']=str(self.root/f'changed-{index}')
            with self.subTest(attribute=attribute),self.assertRaises(m.SuiteError):self.run_case(client)

    def test_missing_physical_flags_source_hash_or_archive_fail(self):
        for index, mutation in enumerate((lambda d:d.update(no_outputs=False),lambda d:d.update(runtime_workspaces_removed=False),
            lambda d:d['source_physical']['values.tif'].update(full_pixels_and_mask_verified=False),
            lambda d:d['source_physical']['values.tif'].update(full_pixels_and_mask_verified=1),
            lambda d:d['sources']['values.tif'].update(size_bytes=True),lambda d:d['archived_sources'].pop('values.tif'))):
            self.env['ADDP_ONLINE_ARTIFACT_DIR']=str(self.root/f'physical-{index}')
            def worker(*args):
                result=physical(*args);mutation(result);return result
            with self.subTest(index=index),self.assertRaises(m.SuiteError):self.run_case(worker=worker)

    def test_budget_is_fixed_and_expiration_cannot_pass(self):
        for value in ('601','0','nan','inf'):
            self.env['ADDP_ONLINE_TEST_TIMEOUT_SECONDS']=value
            with self.subTest(value=value),self.assertRaises(m.SuiteError):self.run_case()
        self.env['ADDP_ONLINE_TEST_TIMEOUT_SECONDS']='600'
        with patch.object(m.time,'monotonic',side_effect=[0.,0.,601.,602.]):
            with self.assertRaises(m.SuiteError):self.run_case()

    def test_original_response_is_not_reconstructed_or_silently_repaired(self):
        value={'size':1};response=SimpleNamespace(status=200,payload=value,raw=b'{ "size": 1 }')
        receipt=m.archive_response(self.root,'response',response)
        self.assertEqual((self.root/'response.json').read_bytes(),response.raw)
        for changed in (SimpleNamespace(status=200,payload=value,raw=b''),SimpleNamespace(status=200,payload=value,raw=b'{"size":2}'),
                        SimpleNamespace(status=True,payload=value,raw=response.raw),SimpleNamespace(status=200,payload={'access_token':'secret'},raw=b'{"access_token":"secret"}')):
            with self.assertRaises(m.SuiteError):m.archive_response(self.root,'invalid',changed)
        with self.assertRaises(m.SuiteError):m.archive_response(self.root,'invalid',SimpleNamespace(status=200,payload={'v':float('nan')},raw=b'{"v":NaN}'))
        (self.root/receipt['path']).chmod(0o644);(self.root/receipt['path']).write_bytes(b'changed')
        with self.assertRaises(m.SuiteError):m.archive_response(self.root,'response',response)

    def test_diagnostic_requires_exact_current_execution_task_cause_and_language(self):
        directory=self.root/'logs/runtime/develop/2026-10-30';directory.mkdir(parents=True)
        cause=m.MESSAGES['percentile_string'][1]
        event={'module_name':'develop','role':'backend','message':'[DevExecutor] 工作流引擎调用失败: execution_id=exec-1 err=任务 analysis 执行失败: '+cause}
        path=directory/'events.jsonl';path.write_text(json.dumps(event)+'\n')
        result=m.invalid_diagnostic(self.root,{},'exec-1','percentile_string','en',.1)
        self.assertEqual(result['exact_cause'],cause)
        for key,value in [('role','worker'),('message',event['message'].replace('analysis','source')),('message',event['message'].replace('exec-1','exec-2'))]:
            changed={**event,key:value};path.write_text(json.dumps(changed)+'\n')
            with self.assertRaises(m.SuiteError):m.invalid_diagnostic(self.root,{},'exec-1','percentile_string','en',.01)

    def test_initial_scan_identity_parent_tenant_user_and_engine_are_exact(self):
        mutations=[lambda d:d.update(execution_id='other'),lambda d:d.update(parent_execution_id='unexpected'),
                   lambda d:d.update(tenant_id=True),lambda d:d.update(triggered_by=34),lambda d:d['scan_context'].update(engine_id=8)]
        for index,mutation in enumerate(mutations):
            self.env['ADDP_ONLINE_ARTIFACT_DIR']=str(self.root/f'scan-{index}')
            client=Client();client.scan_mutation=mutation
            with self.subTest(index=index),self.assertRaises(m.SuiteError):self.run_case(client)


if __name__=='__main__':unittest.main()
