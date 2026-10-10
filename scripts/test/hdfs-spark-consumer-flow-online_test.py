import importlib.util
import json
import os
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location('hdfs_online', Path(__file__).with_name('hdfs-spark-consumer-flow-online.py'))
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


def final_result():
    rows = []
    for region, amount in (('east', 1100), ('west', 1000)):
        row = {'region': region}
        for name in ('csv', 'json', 'parquet'):
            row[name + '_rows'], row[name + '_amount_sum'] = 10, amount
        rows.append(row)
    return {'type': 'spark_dataframe', 'preview_rows': rows}


class HDFSOnlineTest(unittest.TestCase):
    def test_source_preparation_refuses_personal_environment_before_any_request(self):
        with patch.dict(os.environ, {}, clear=True), patch.object(MODULE.sys, 'argv', ['hdfs', '--initialize-table-read']), patch.object(MODULE.support, 'GatewayClient') as client:
            with self.assertRaises(MODULE.REGISTRATION.RegistrationError):
                MODULE.main()
            client.assert_not_called()

    def test_locator_keeps_dot_and_encodes_original_filename_once(self):
        self.assertEqual(MODULE.locator(7, {'full_name': 'samples/orders.parquet', 'id': 91}),
                         'addp://engine/7/path/samples/orders.parquet?type=file&item_id=91')
        self.assertIn('%E8%AE%A2%E5%8D%95%20100%25.csv?', MODULE.locator(7, {'full_name': '订单 100%.csv', 'id': 91}))

    def test_workflow_contains_only_locator_bindings_and_matches_real_operator_signatures(self):
        definition = MODULE.workflow({name: 'addp://engine/7/path/orders.' + name + '?type=file&item_id=91'
                                      for name in ('csv', 'json', 'parquet')})
        self.assertEqual(len(definition['tasks']), 8)
        for task in definition['tasks']:
            params = task['params']
            self.assertFalse(set(params) & {'connection_info', 'path', 'format', 'engine_id'})
            if task['operator'] == 'load':
                self.assertEqual(set(params), {'locator', 'source_type'})
            if task['operator'] == 'join':
                self.assertEqual((params['on'], params['how']), ('region', 'inner'))
                self.assertNotIn('join_type', params)

    def test_result_rejects_wrong_total_missing_format_runtime_or_persistent_outputs(self):
        execution = {'status': 'success', 'metadata': {'result': {'final_result': final_result(), 'runtime_execution_id': 'runtime'}}}
        self.assertEqual(MODULE.validate_result(execution), final_result())
        for mutation in ('total', 'missing', 'runtime', 'outputs', 'failed'):
            value = json.loads(json.dumps(execution))
            result = value['metadata']['result']
            if mutation == 'total': result['final_result']['preview_rows'][0]['csv_amount_sum'] = 2100
            if mutation == 'missing': result['final_result']['preview_rows'][1].pop('parquet_rows')
            if mutation == 'runtime': result.pop('runtime_execution_id')
            if mutation == 'outputs': value['outputs'] = {'save': {}}
            if mutation == 'failed': value['status'] = 'failed'
            with self.subTest(mutation=mutation), self.assertRaises(MODULE.SuiteError): MODULE.validate_result(value)

    def test_regular_user_has_exact_read_scene_permissions(self):
        context = {'principal': {'type': 'user', 'id': 42}, 'context': {'type': 'tenant', 'tenant_id': '2'},
                   'authorization': {'role_assignments': [{'role_key': 'tenant.hdfs-reader', 'permissions': sorted(MODULE.PERMISSIONS)}]}}
        client = SimpleNamespace(request=lambda *args: SimpleNamespace(payload=context))
        self.assertEqual(MODULE.validate_identity(client, 2), '42')
        context['authorization']['role_assignments'][0]['permissions'].append('develop.task.create')
        with self.assertRaises(MODULE.SuiteError): MODULE.validate_identity(client, 2)

    def test_physical_evidence_requires_exact_application_owned_worker_and_finished_tasks(self):
        for failure in ('missing', 'local', 'foreign', 'no_tasks', ''):
            master = {'aliveworkers': 1, 'activeapps': [{'id': 'app-20261004-0001', 'name': 'ADDP-Workflow-Engine-18', 'cores': 1, 'state': 'RUNNING'}]}
            if failure == 'missing': master['activeapps'] = []
            if failure == 'local': master['aliveworkers'] = 0
            import io
            def docker(command, **kwargs):
                if 'inspect' in command:
                    return SimpleNamespace(stdout='foreign' if failure == 'foreign' else 'hdfs-spark-consumer-flow')
                self.assertEqual(command[-1], 'app-20261004-0001')
                return SimpleNamespace(stdout='' if failure == 'no_tasks' else 'Finished task 0.0 in stage 1.0 (TID 2)')
            with self.subTest(failure=failure), patch.object(MODULE.urllib.request, 'urlopen', return_value=io.BytesIO(json.dumps(master).encode())), patch.object(MODULE.subprocess, 'run', side_effect=docker):
                if failure:
                    with self.assertRaises(MODULE.SuiteError): MODULE.worker_evidence(18)
                else:
                    self.assertEqual(MODULE.worker_evidence(18)['completed_tasks'], 1)

    def test_runtime_status_uses_exact_formal_id_and_shared_canonical_results(self):
        import io
        expected = final_result()
        for failure in ('wrong_id', 'missing_results', 'nested_ports', ''):
            snapshot = {'execution_id': 'runtime-id', 'status': 'success', 'progress': 100,
                        'result': expected, 'task_order': list(range(8)),
                        'all_results': {str(i): expected for i in range(8)}}
            if failure == 'wrong_id': snapshot['execution_id'] = 'other'
            if failure == 'missing_results': snapshot['all_results'] = {}
            if failure == 'nested_ports': snapshot['all_results']['0'] = {'default': expected}
            with self.subTest(failure=failure), patch.dict(os.environ, ADDP_ONLINE_SPARK_RUNTIME_URL='http://127.0.0.1:8098'), patch.object(MODULE.urllib.request, 'urlopen', side_effect=lambda *a, **k: io.BytesIO(json.dumps(snapshot).encode())) as request:
                if failure:
                    with self.assertRaises(MODULE.SuiteError): MODULE.runtime_status_evidence('runtime-id', expected)
                else:
                    self.assertEqual(MODULE.runtime_status_evidence('runtime-id', expected)['queries'], 8)
                    self.assertEqual(request.call_count, 8)
                    self.assertEqual(request.call_args.args[0], 'http://127.0.0.1:8098/api/executions/runtime-id')

    def test_persistence_requires_commit_scan_stable_output_and_exact_lineage(self):
        locators = {name: 'addp://engine/7/path/orders.' + name + '?type=file' for name in ('csv', 'json', 'parquet')}
        target = 'addp://engine/9/path/results/hdfs_totals?type=table'
        definition = MODULE.persistence_workflow(locators, 9)
        self.assertEqual(len(definition['tasks']), 9)
        self.assertEqual(definition['tasks'][-1]['params']['target_parent_locator'], 'addp://engine/9/path/results?type=schema')
        final = {'status': 'success', 'rows': 2, 'target': 'results.hdfs_totals'}
        execution = {'status': 'success', 'outputs': {'save': {'resource': {'locator': target, 'type': 'table', 'write_mode': 'replace'}}},
            'metadata': {'lineage_facts': {'schema_version': 'addp.lineage-facts/v1',
                'inputs': [{'locator': value} for value in locators.values()],
                'outputs': [{'locator': target, 'write_mode': 'replace'}]},
                'result': {'final_result': final, 'produced_targets': [{'locator': target, 'task_id': 'save'}],
                    'meta_scan_runs': [{'status': 'submitted', 'target_locator': target, 'execution_id': 'scan'}]}}}
        self.assertEqual(MODULE.validate_persistence(execution, locators, target), (final, 'scan'))
        for invalid in ('rows', 'output', 'source', 'mode', 'target', 'scan'):
            value = json.loads(json.dumps(execution))
            if invalid == 'rows': value['metadata']['result']['final_result']['rows'] = 1
            if invalid == 'output': value['outputs']['save']['resource']['locator'] = target + '&other=1'
            if invalid == 'source': value['metadata']['lineage_facts']['inputs'].pop()
            if invalid == 'mode': value['metadata']['lineage_facts']['outputs'][0]['write_mode'] = 'append'
            if invalid == 'target': value['metadata']['result']['produced_targets'] = []
            if invalid == 'scan': value['metadata']['result']['meta_scan_runs'][0]['status'] = 'failed'
            with self.subTest(invalid=invalid), self.assertRaises(MODULE.SuiteError):
                MODULE.validate_persistence(value, locators, target)

    def test_persistence_compares_rows_by_region_and_keeps_original_runtime_order(self):
        from unittest.mock import Mock
        expected = final_result()
        expected['preview_rows'].reverse()
        report = {'engine_id': 7, 'cluster_id': 8, 'runtime_id': 10,
                  'locators': {name: 'addp://engine/7/path/orders.' + name + '?type=file' for name in ('csv', 'json', 'parquet')},
                  'item_ids': {'csv': 1, 'json': 2, 'parquet': 3}, 'final_result': expected}
        receipt = {'status': 'success', 'rows': 2, 'target': 'results.hdfs_totals'}
        saved = {'metadata': {'result': {'runtime_execution_id': 'runtime-save'}}}
        reused = {'metadata': {'result': {'runtime_execution_id': 'runtime-reuse', 'final_result': final_result()}}}
        graph = {'edges': [{'source': {'item_id': source}, 'target': {'item_id': 5},
                 'evidence': {'execution_id': 'saved'}, 'status': 'active', 'relation_kind': 'derive'} for source in (1, 2, 3)]}
        for wrong in ('', 'amount', 'missing', 'duplicate'):
            rows = final_result()['preview_rows']
            if wrong == 'amount': rows[0]['csv_amount_sum'] += 1
            if wrong == 'missing': rows[0].pop('parquet_rows')
            if wrong == 'duplicate': rows.append(rows[0].copy())
            client = Mock()
            client.request.side_effect = [SimpleNamespace(payload={'status': 'success'}), SimpleNamespace(payload=graph)]
            with self.subTest(wrong=wrong), patch.dict(os.environ, ADDP_ONLINE_SPARK_RUNTIME_URL='http://127.0.0.1:8098'), \
                 patch.object(MODULE, 'submit_and_wait', side_effect=[('saved', saved), ('reused', reused)]), \
                 patch.object(MODULE, 'validate_persistence', return_value=(receipt, 'scan')), \
                 patch.object(MODULE.SPARK, 'runtime_status_evidence', return_value={}), \
                 patch.object(MODULE.support, 'find_item', return_value={'id': 5}), \
                 patch.object(MODULE.support, 'preview_rows', return_value=(list(rows[0]), rows)), \
                 patch.object(MODULE, 'persistence_physical', return_value={}):
                if wrong:
                    with self.assertRaisesRegex(MODULE.SuiteError, 'Manager persisted result'):
                        MODULE.run_persistence(client, report, 9, 30)
                else:
                    result = MODULE.run_persistence(client, report, 9, 30)
                    self.assertEqual(result['lineage_sources'], [1, 2, 3])
                    self.assertEqual(result['reuse_final_result'], final_result())
                    self.assertEqual(report['final_result']['preview_rows'][0]['region'], 'west')

    def test_persisted_runtime_http_snapshots_require_exact_save_receipt(self):
        import io
        receipt = {'status': 'success', 'rows': 2, 'target': 'results.hdfs_totals'}
        for invalid in ('lost_save', 'wrong_rows', ''):
            snapshot = {'execution_id': 'runtime-save', 'status': 'success', 'progress': 100,
                'result': receipt, 'task_order': ['load', 'save'], 'all_results': {'load': final_result(), 'save': receipt.copy()}}
            if invalid == 'lost_save': snapshot['all_results']['save'] = {'type': 'spark_dataframe'}
            if invalid == 'wrong_rows': snapshot['all_results']['save']['rows'] = 1
            def validate(nodes):
                if nodes['save'] != receipt: raise ValueError('save receipt changed')
            with self.subTest(invalid=invalid), patch.object(MODULE.urllib.request, 'urlopen', side_effect=lambda *a, **k: io.BytesIO(json.dumps(snapshot).encode())):
                if invalid:
                    with self.assertRaises(ValueError):
                        MODULE.SPARK.runtime_status_evidence('http://runtime', 'runtime-save', receipt, 2, validate, {'load': 'spark_dataframe', 'save': 'save'})
                else:
                    self.assertEqual(MODULE.SPARK.runtime_status_evidence('http://runtime', 'runtime-save', receipt, 2, validate, {'load': 'spark_dataframe', 'save': 'save'})['queries'], 8)

    def test_browser_requires_matching_identity_execution_and_all_screenshots(self):
        report = {'engine_id': 7, 'tenant_id': 2, 'principal_id': '42', 'execution_id': 'execution', 'persistence': {'execution_id': 'save-execution', 'reuse_execution_id': 'reuse-execution'}}
        for failure in ('process', 'missing_report', 'identity', 'execution', 'screenshot', ''):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                evidence = {key: value for key, value in report.items() if key != 'persistence'}
                evidence.update(run_id='hdfs-run', meta_ui_scan=True, previews=5, develop_result=True,
                                persist_execution_id='save-execution', reuse_execution_id='reuse-execution')
                if failure in ('identity', 'execution'): evidence['principal_id' if failure == 'identity' else 'execution_id'] = 'other'
                def browser(command, cwd, env):
                    self.assertIn('e2e/online/hdfs-spark-consumer-flow.spec.js', command)
                    if failure != 'missing_report': Path(env['ADDP_ONLINE_HDFS_BROWSER_REPORT']).write_text(json.dumps(evidence))
                    for name in ('meta', 'csv', 'json', 'parquet', 'original', 'workflow', 'persisted', 'save', 'reuse'):
                        if failure != 'screenshot' or name != 'workflow': (root / ('hdfs-' + name + '-console.png')).write_bytes(b'proof')
                    return SimpleNamespace(returncode=int(failure == 'process'))
                with patch.dict(os.environ, ADDP_ONLINE_ARTIFACT_DIR=temporary, ADDP_ONLINE_TEST_RUN_ID='hdfs-run'), patch.object(MODULE.subprocess, 'run', side_effect=browser):
                    if failure:
                        with self.assertRaises(MODULE.SuiteError): MODULE.run_browser(report)
                    else: self.assertEqual(MODULE.run_browser(report), evidence)


if __name__ == '__main__': unittest.main()
