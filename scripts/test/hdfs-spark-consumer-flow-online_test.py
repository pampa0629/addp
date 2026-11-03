import importlib.util
import json
import os
from pathlib import Path
import tempfile
import uuid
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

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


def persistence_flow(client, report, engine, timeout, target_type="table"):
    publication = MODULE.publish_persistence(client, report, engine, timeout, target_type)
    return MODULE.consume_persistence(client, report, engine, publication, timeout, target_type)


def preparation_fixture():
    identity = {'run_id': 'run-files', 'tenant_id': 2, 'principal_id': '42', 'engine_id': 17,
                'cluster_id': 18, 'minio_engine_id': 20}
    sources = [MODULE.REGISTRATION.ExactFileMetadataGrants.target(17, name, 'hdfs') for name in MODULE.FILES.values()]
    publications = []
    for number in (1, 2):
        execution = str(uuid.UUID(int=number))
        name = 'result/results/hdfs_totals/' + execution + '-' + str(uuid.uuid5(uuid.NAMESPACE_OID, 'save'))
        sources.append(MODULE.REGISTRATION.ExactFileMetadataGrants.target(20, name, 'minio', layout='whole'))
        publications.append({'execution_id': execution, 'target_locator': f'addp://engine/20/path/{name}?type=object',
                             'remaining_seconds': 60})
    publications[0]['manager_preview_before_second'] = [{key: value if key == 'region' else str(value) for key, value in row.items()} for row in final_result()['preview_rows']]
    rows = [{'engine': path['engine_id'], 'target': path, 'request_id': str(uuid.UUID(int=number + 10)), 'confirmed': True}
            for number, path in enumerate(sources)]
    journal = dict(identity, schema_version='addp.hdfs-metadata-grants/v1', pending=rows, issued_count=6)
    prepared = dict(identity, runtime_id=3, scan_execution_id='source-scan', source_remaining_seconds=60,
        locators={key: MODULE.locator(17, {'full_name': name, 'id': i}) for i, (key, name) in enumerate(MODULE.FILES.items(), 1)},
        item_ids={key: i for i, key in enumerate(MODULE.FILES, 1)}, minio_publications=publications)
    return identity, journal, prepared


class HDFSOnlineTest(unittest.TestCase):
    def test_source_preparation_scans_once_then_grants_each_exact_file_before_lookup(self):
        identity, _, _ = preparation_fixture()
        events, granted = [], set()
        grants = Mock()
        def grant(engine, name, storage, **kwargs):
            self.assertEqual((engine, storage), (17, 'hdfs'))
            self.assertEqual(kwargs, {'data_type': 'table', 'format_name': name.rsplit('.', 1)[1], 'layout': 'single'})
            events.append(('grant', name)); granted.add(name)
        grants.grant.side_effect = grant
        def find(client, engine, name, kind):
            self.assertIn(name, granted)
            self.assertEqual((engine, kind), (17, 'file'))
            events.append(('ordinary_lookup', name))
            return {'id': list(MODULE.FILES.values()).index(name) + 1, 'full_name': name}
        with tempfile.TemporaryDirectory() as temporary, patch.dict(os.environ, ADDP_ONLINE_SECRET_DIR=temporary), \
             patch.object(MODULE, 'preparation_identity', return_value=identity), \
             patch.object(MODULE, 'metadata_grants', return_value=grants), \
             patch.object(MODULE.support, 'wait_for_scan', return_value='source-scan') as scan, \
             patch.object(MODULE.support, 'find_item', side_effect=find), \
             patch.object(MODULE.support, 'preview_rows') as preview, patch.object(MODULE, 'submit_and_wait') as submit:
            prepared, result = MODULE.prepare_file_metadata(Mock(), Mock(), 2, '42', 3, 18, 60)
            self.assertIs(result, grants)
            self.assertEqual(scan.call_count, 1)
            self.assertEqual(scan.call_args.args[1], 17)
            self.assertEqual(prepared['scan_execution_id'], 'source-scan')
            self.assertTrue(0 < prepared['source_remaining_seconds'] <= 60)
            self.assertEqual(set(prepared['locators']), set(MODULE.FILES))
            self.assertEqual(events, [(event, name) for name in MODULE.FILES.values() for event in ('grant', 'ordinary_lookup')])
            self.assertEqual(MODULE.read_private('hdfs-metadata-grants.json')['issued_count'], 0)
            preview.assert_not_called(); submit.assert_not_called()

    def test_metadata_journal_refuses_extra_scope_cross_run_and_duplicate_commands(self):
        for mutation in ('run', 'tenant', 'user', 'engine', 'source', 'broad', 'duplicate', 'unconfirmed_type', 'count', ''):
            identity, journal, _ = preparation_fixture()
            if mutation in ('run', 'tenant', 'engine'): journal[{'run':'run_id','tenant':'tenant_id','engine':'engine_id'}[mutation]] = 'other'
            if mutation == 'user': journal['principal_id'] = 'other'
            if mutation == 'source': journal['pending'][0]['target']['segments'][-1]['name'] = 'other.csv'
            if mutation == 'broad': journal['pending'][-1]['target']['segments'].pop()
            if mutation == 'duplicate': journal['pending'][1]['request_id'] = journal['pending'][0]['request_id']
            if mutation == 'unconfirmed_type': journal['pending'][0]['confirmed'] = 1
            if mutation == 'count': journal['issued_count'] = 7
            with self.subTest(mutation=mutation):
                if mutation:
                    with self.assertRaises(MODULE.SuiteError): MODULE.validate_metadata_journal(journal, identity)
                else: MODULE.validate_metadata_journal(journal, identity)

    def test_ordinary_scenario_uses_original_publications_and_never_repeats_source_scan_or_minio_save(self):
        identity, journal, prepared = preparation_fixture()
        report = {key: prepared[key] for key in ('principal_id','tenant_id','engine_id','cluster_id','runtime_id','locators','item_ids')}
        report['final_result'] = final_result()
        def publish(client, current, engine, timeout):
            self.assertEqual(engine, 19)
            return {'execution_id': 'pg-published'}
        def consume(client, current, engine, publication, timeout, target_type='table'):
            if engine == 20:
                self.assertEqual(target_type, 'file')
                self.assertIn(publication, prepared['minio_publications'])
            return {'execution_id': publication['execution_id'], 'target_locator': publication.get('target_locator', 'pg')}
        with tempfile.TemporaryDirectory() as temporary, patch.dict(os.environ, ADDP_ONLINE_SECRET_DIR=temporary,
                ADDP_ONLINE_ARTIFACT_DIR=temporary, ADDP_ONLINE_POSTGRES_ENGINE_ID='19', ADDP_ONLINE_MINIO_ENGINE_ID='20'), \
             patch.object(MODULE, 'preparation_identity', return_value=identity), patch.object(MODULE, 'run', return_value=report) as run, \
             patch.object(MODULE, 'publish_persistence', side_effect=publish) as publication, \
             patch.object(MODULE, 'consume_persistence', side_effect=consume) as consumption, \
             patch.object(MODULE, 'validate_minio_independent_results'), patch.object(MODULE.TABLES, 'run', return_value={}), \
             patch.object(MODULE, 'run_browser', return_value={}), patch.object(MODULE, 'run_multicluster_browser', return_value={}), \
             patch.object(MODULE.support, 'wait_for_scan') as scan, patch('builtins.print'):
            MODULE.write_private('hdfs-file-prepared.json', prepared, new=True)
            MODULE.write_private('hdfs-metadata-grants.json', journal, new=True)
            self.assertEqual(MODULE.run_scenario(Mock(), 60), 0)
            self.assertEqual(publication.call_count, 1)
            self.assertEqual(consumption.call_count, 3)
            self.assertEqual(run.call_args.args[-2:], ('source-scan', 3))
            scan.assert_not_called()
            result = json.loads(Path(temporary, 'hdfs-spark-consumer-flow.json').read_text())
            self.assertEqual(result['metadata_preparation']['publication_execution_ids'],
                             [row['execution_id'] for row in prepared['minio_publications']])

    def test_missing_unconfirmed_or_foreign_preparation_fails_before_workflow_submission(self):
        for mutation in ('missing', 'foreign', 'grant', 'duplicate'):
            identity, journal, prepared = preparation_fixture()
            if mutation == 'foreign': prepared['principal_id'] = '99'
            if mutation == 'grant': journal['pending'][0]['confirmed'] = False
            if mutation == 'duplicate': prepared['minio_publications'][1] = prepared['minio_publications'][0]
            with self.subTest(mutation=mutation), tempfile.TemporaryDirectory() as temporary, \
                 patch.dict(os.environ, ADDP_ONLINE_SECRET_DIR=temporary), \
                 patch.object(MODULE, 'preparation_identity', return_value=identity), patch.object(MODULE, 'run') as run:
                if mutation != 'missing': MODULE.write_private('hdfs-file-prepared.json', prepared, new=True)
                MODULE.write_private('hdfs-metadata-grants.json', journal, new=True)
                with self.assertRaises((MODULE.SuiteError, FileNotFoundError)): MODULE.run_scenario(Mock(), 60)
                run.assert_not_called()

    def test_private_preparation_refuses_replacement_symlinks_and_permissive_files(self):
        with tempfile.TemporaryDirectory() as temporary, patch.dict(os.environ, ADDP_ONLINE_SECRET_DIR=temporary):
            MODULE.write_private('prepared.json', {'id': 1}, new=True)
            self.assertEqual(Path(temporary, 'prepared.json').stat().st_mode & 0o777, 0o600)
            with self.assertRaises(MODULE.SuiteError): MODULE.write_private('prepared.json', {'id': 2}, new=True)
            Path(temporary, 'prepared.json').chmod(0o644)
            with self.assertRaises(MODULE.SuiteError): MODULE.read_private('prepared.json')
            Path(temporary, 'link.json').symlink_to(Path(temporary, 'prepared.json'))
            with self.assertRaises(OSError): MODULE.read_private('link.json')

    def test_cleanup_reloads_only_owned_journal_and_refuses_foreign_targets(self):
        for foreign in (False, True):
            identity, journal, _ = preparation_fixture()
            if foreign: journal['pending'][0]['target']['segments'][-1]['name'] = 'foreign.csv'
            factory = Mock()
            factory.return_value.cleanup.return_value = {'grants_revoked': True, 'metadata_hidden': True}
            with self.subTest(foreign=foreign), tempfile.TemporaryDirectory() as temporary, \
                 patch.dict(os.environ, ADDP_ONLINE_SECRET_DIR=temporary, ADDP_ONLINE_ARTIFACT_DIR=temporary,
                    GATEWAY_URL='http://127.0.0.1:8000', ADDP_ONLINE_TEST_USER_ACCESS_TOKEN='consumer',
                    ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN='issuer'), \
                 patch.object(MODULE.REGISTRATION, 'require_external_environment'), \
                 patch.object(MODULE.support, 'GatewayClient') as clients, \
                 patch.object(MODULE, 'preparation_identity', return_value=identity), patch.object(MODULE, 'metadata_grants', factory):
                MODULE.write_private('hdfs-metadata-grants.json', journal, new=True)
                if foreign:
                    with self.assertRaises(MODULE.SuiteError): MODULE.cleanup_metadata_access()
                    factory.assert_not_called()
                else:
                    MODULE.cleanup_metadata_access()
                    self.assertEqual(clients.call_args_list[0].args[1], 'consumer')
                    self.assertEqual(clients.call_args_list[1].args[1], 'issuer')
                    result = json.loads(Path(temporary, 'hdfs-metadata-cleanup.json').read_text())
                    self.assertEqual(result, {'targets': 6, 'grants_revoked': True, 'metadata_hidden': True})

    def test_prepared_publication_cannot_change_target_execution_or_budget(self):
        for mutation in ('engine', 'execution', 'path', 'budget', 'infinite', 'boolean'):
            _, _, prepared = preparation_fixture()
            publication = prepared['minio_publications'][0]
            if mutation == 'engine': publication['target_locator'] = publication['target_locator'].replace('engine/20', 'engine/99')
            if mutation == 'execution': publication['execution_id'] = str(uuid.uuid4())
            if mutation == 'path': publication['target_locator'] = 'addp://engine/20/path/result?type=bucket'
            if mutation == 'budget': publication['remaining_seconds'] = 61
            if mutation == 'infinite': publication['remaining_seconds'] = float('inf')
            if mutation == 'boolean': publication['remaining_seconds'] = True
            with self.subTest(mutation=mutation):
                client = Mock()
                with self.assertRaises(MODULE.SuiteError): MODULE.consume_persistence(client, {}, 20, publication, 60, 'file')
                client.request.assert_not_called()

    def test_first_publication_baseline_is_taken_by_an_unprivileged_child_before_second(self):
        identity, journal, prepared = preparation_fixture()
        publication = prepared['minio_publications'][0]
        baseline = publication.pop('manager_preview_before_second')
        journal['pending'].pop(); journal['issued_count'] = 5
        with tempfile.TemporaryDirectory() as temporary, patch.dict(os.environ,
                ADDP_ONLINE_SECRET_DIR=temporary, ADDP_ONLINE_TEST_USER_ACCESS_TOKEN='ordinary',
                ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN='provisioner', ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN='issuer'), \
             patch.object(MODULE, 'preparation_identity', return_value=identity), \
             patch.object(MODULE.support, 'find_item', return_value={'id': 7}) as find, \
             patch.object(MODULE.support, 'preview_rows', return_value=([], baseline)) as preview:
            MODULE.write_private('hdfs-metadata-grants.json', journal, new=True)
            def child(command, env, check, timeout):
                self.assertEqual(command[-1], '--observe-first-minio')
                self.assertNotIn('ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN', env)
                self.assertNotIn('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN', env)
                self.assertEqual(env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 'ordinary')
                self.assertTrue(0 < timeout <= 60)
                with patch.dict(os.environ, env, clear=True):
                    self.assertEqual(MODULE.observe_first_minio(Mock()), 0)
                return SimpleNamespace(returncode=0)
            with patch.object(MODULE.subprocess, 'run', side_effect=child):
                MODULE.capture_first_minio(Mock(), publication)
            self.assertEqual(publication['manager_preview_before_second'], baseline)
            self.assertTrue(0 < publication['remaining_seconds'] < 60)
            self.assertEqual(find.call_args.args[1:3], (20, MODULE.publication_item_path(publication, 20, 'file')))
            preview.assert_called_once()

    def test_prepare_entry_publishes_twice_and_captures_first_before_second(self):
        identity, _, prepared = preparation_fixture()
        originals = json.loads(json.dumps(prepared['minio_publications']))
        for row in originals: row.pop('manager_preview_before_second', None)
        events = []
        grants = Mock()
        grants.grant.side_effect = lambda *args, **kwargs: events.append('metadata_grant')
        def publish(client, source, engine, timeout, target_type):
            self.assertEqual((engine, target_type), (20, 'file'))
            events.append('publish')
            return originals.pop(0)
        def baseline(client, publication):
            self.assertEqual(events, ['publish', 'metadata_grant'])
            events.append('baseline')
            publication['manager_preview_before_second'] = final_result()['preview_rows']
        environment = {'GITHUB_ACTIONS':'true', 'RUNNER_OS':'Linux', 'ADDP_ONLINE_HOSTED':'1',
            'SYSTEM_URL':'http://127.0.0.1:8180', 'GATEWAY_URL':'http://127.0.0.1:8000',
            'ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN':'provisioner', 'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN':'issuer',
            'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN':'ordinary', 'ADDP_ONLINE_TEST_TENANT_ID':'2',
            'ADDP_ONLINE_POSTGRES_ENGINE_ID':'19', 'ADDP_ONLINE_SPARK_ENGINE_ID':'18'}
        with tempfile.TemporaryDirectory() as temporary, patch.dict(os.environ, dict(environment, ADDP_ONLINE_SECRET_DIR=temporary), clear=True), \
             patch.object(MODULE.sys, 'argv', ['hdfs', '--prepare-data-access']), \
             patch.object(MODULE.support, 'GatewayClient') as clients, patch.object(MODULE, 'validate_identity', return_value='42'), \
             patch.object(MODULE, 'prepare_file_metadata', return_value=(prepared, grants)) as files, \
             patch.object(MODULE.support, 'wait_for_scan') as scan, \
             patch.object(MODULE.REGISTRATION, 'initialize_exact_record_read_grants') as reads, \
             patch.object(MODULE.TABLES, 'prepare') as tables, patch.object(MODULE, 'publish_persistence', side_effect=publish), \
             patch.object(MODULE, 'capture_first_minio', side_effect=baseline), patch('builtins.print'):
            clients.return_value.request.return_value.payload = [{'id':3,'engine_type':'spark_workflow','connection_status':'online'}]
            self.assertEqual(MODULE.main(), 0)
            self.assertEqual(events, ['publish','metadata_grant','baseline','publish','metadata_grant'])
            files.assert_called_once(); scan.assert_called_once(); tables.assert_called_once()
            self.assertEqual(reads.call_args.args[-1], [(19,'schema','results','hdfs_totals')])
            self.assertEqual(len(MODULE.read_private('hdfs-file-prepared.json')['minio_publications']), 2)

    def test_changed_first_result_baseline_is_not_replaced_by_later_preview(self):
        _, _, prepared = preparation_fixture()
        publication = prepared['minio_publications'][0]
        publication.update(scan_execution_id='scan', runtime_status={}, runtime_execution_id='runtime-save',
                           final_result={'status':'success','rows':2})
        publication['manager_preview_before_second'][0]['csv_rows'] = '11'
        execution = {'status':'success','metadata':{'result':{'runtime_execution_id':'runtime-save'}}}
        report = {'locators':{}, 'final_result':final_result()}
        rows = [{key:value if key=='region' else str(value) for key,value in row.items()} for row in final_result()['preview_rows']]
        client = Mock(); client.request.return_value.payload = execution
        with patch.object(MODULE, 'validate_persistence', return_value=(publication['final_result'],'scan')), \
             patch.object(MODULE.support, 'find_item', return_value={'id':7,'attributes':{'item':{'data_type':'table','format':'parquet','layout':'whole'}}}), \
             patch.object(MODULE.support, 'preview_rows', return_value=([],rows)), patch.object(MODULE, 'submit_and_wait') as submit:
            with self.assertRaisesRegex(MODULE.SuiteError, 'original first-result Manager baseline'):
                MODULE.consume_persistence(client, report, 20, publication, 60, 'file')
            submit.assert_not_called()
    def test_minio_physical_reports_committed_objects_without_claiming_multipart_absence(self):
        calls = []
        def run(command, **kwargs):
            calls.append(command)
            if 'ls' in command:
                return SimpleNamespace(stdout='{"key":"part-0.parquet"}\n{"key":"_SUCCESS"}\n')
            return SimpleNamespace(stdout='{"committer":"magic"}')
        with patch.object(MODULE.subprocess, 'run', side_effect=run):
            self.assertEqual(MODULE.minio_physical('result/results/owned'), {'committer': 'magic', 'objects': 2})
        self.assertEqual(len(calls), 2)
        self.assertFalse(any('--incomplete' in command for command in calls))

    def test_second_minio_publication_preserves_exact_manager_json_of_first_result(self):
        first = {'target_locator': 'addp://engine/9/path/result/first?type=object',
                 'preview_locator': 'addp://engine/9/path/result/first?type=object&item_id=5',
                 'manager_preview_rows': [{key: value if key == 'region' else str(value)
                                           for key, value in row.items()} for row in final_result()['preview_rows']]}
        second = {'target_locator': 'addp://engine/9/path/result/second?type=object'}
        for mutation in ('', 'number', 'value', 'missing', 'duplicate'):
            rows = json.loads(json.dumps(first['manager_preview_rows']))
            rows.reverse()
            if mutation == 'number': rows[0]['csv_rows'] = 10
            if mutation == 'value': rows[0]['csv_amount_sum'] = '1'
            if mutation == 'missing': rows.pop()
            if mutation == 'duplicate': rows.append(rows[0].copy())
            with self.subTest(mutation=mutation), patch.object(MODULE.support, 'preview_rows', return_value=([], rows)):
                if mutation:
                    with self.assertRaisesRegex(MODULE.SuiteError, 'Second publication changed'):
                        MODULE.validate_minio_independent_results(None, first, second)
                else:
                    MODULE.validate_minio_independent_results(None, first, second)
        with patch.object(MODULE.support, 'preview_rows') as preview:
            with self.assertRaisesRegex(MODULE.SuiteError, 'same MinIO result directory'):
                MODULE.validate_minio_independent_results(None, first, first)
            preview.assert_not_called()

    def test_source_preparation_refuses_personal_environment_before_any_request(self):
        with patch.dict(os.environ, {}, clear=True), patch.object(MODULE.sys, 'argv', ['hdfs', '--prepare-data-access']), patch.object(MODULE.support, 'GatewayClient') as client:
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
        context['authorization']['role_assignments'][0]['permissions'].append('develop.task.delete')
        with self.assertRaises(MODULE.SuiteError): MODULE.validate_identity(client, 2)

    def test_physical_evidence_requires_exact_application_owned_worker_and_finished_tasks(self):
        for failure in ('missing', 'local', 'foreign', 'no_tasks', ''):
            master = {'status': 'ALIVE', 'aliveworkers': 1, 'activeapps': [], 'completedapps': [{'id': 'app-20261004-0001', 'name': 'ADDP-Workflow-Engine-18', 'state': 'FINISHED'}]}
            if failure == 'missing': master['completedapps'] = []
            if failure == 'local': master['aliveworkers'] = 0
            import io
            def docker(command, **kwargs):
                if 'inspect' in command:
                    return SimpleNamespace(stdout='foreign' if failure == 'foreign' else 'hdfs-spark-consumer-flow')
                self.assertEqual(command[-1], 'app-20261004-0001')
                return SimpleNamespace(stdout='' if failure == 'no_tasks' else 'Finished task 0.0 in stage 1.0 (TID 2)')
            with self.subTest(failure=failure), patch.object(MODULE.urllib.request, 'urlopen', return_value=io.BytesIO(json.dumps(master).encode())), patch.object(MODULE.subprocess, 'run', side_effect=docker):
                if failure:
                    with self.assertRaises(MODULE.SuiteError): MODULE.worker_evidence(18, set())
                else:
                    self.assertEqual(MODULE.worker_evidence(18, set())['completed_tasks'], 1)

    def test_completed_invocation_rejects_cached_running_reused_extra_or_wrong_application(self):
        master = {'status': 'ALIVE', 'aliveworkers': 1, 'activeapps': [],
                  'completedapps': [{'id': 'app-20261103-0001', 'name': 'ADDP-Workflow-Engine-18', 'state': 'FINISHED'}]}
        app = MODULE.SPARK.completed_application(master, set(), 'ADDP-Workflow-Engine-18')
        self.assertEqual(app['id'], 'app-20261103-0001')
        for failure in ('active', 'reused', 'extra', 'wrong', 'unfinished', 'bad_id'):
            value = json.loads(json.dumps(master))
            before = set()
            if failure == 'active': value['activeapps'] = [value['completedapps'].pop()]
            if failure == 'reused': before = {'app-20261103-0001'}
            if failure == 'extra': value['completedapps'].append(dict(app, id='app-20261103-0002'))
            if failure == 'wrong': value['completedapps'][0]['name'] = 'foreign'
            if failure == 'unfinished': value['completedapps'][0]['state'] = 'FAILED'
            if failure == 'bad_id': value['completedapps'][0]['id'] = '../../foreign'
            with self.subTest(failure=failure), self.assertRaises(ValueError):
                MODULE.SPARK.completed_application(value, before, 'ADDP-Workflow-Engine-18')

    def test_dual_receipt_requires_both_page_sequences_concurrency_failure_recovery_and_distinct_executions(self):
        report = {'tenant_id': 2, 'principal_id': '42', 'runtime_id': 8,
                  'multicluster': {'query': 'SELECT valid', 'failure_query': 'SELECT missing',
                                  'clusters': {'a': {'id': 18}, 'b': {'id': 19}}}}
        receipt = {'run_id': 'dual-run', 'tenant_id': 2, 'principal_id': '42', 'runtime_id': 8,
                   'tasks': {'a': 71, 'b': 72}, 'observations': [], 'manager_no_spark': True}
        for index in range(12):
            kind, step = ('workflow' if index < 6 else 'query'), index % 6
            pairs = [[('a', 'success')], [('b', 'success')], [('a', 'success')],
                     [('a', 'success'), ('b', 'success')], [('a', 'failed'), ('b', 'success')], [('a', 'success')]][step]
            observation = {'kind': kind, 'phase': f'sequence-{index}' if step < 3 else {3: 'concurrent', 4: 'failure-isolation', 5: 'recovery'}[step],
                           'operations': [], 'before': {'a': [], 'b': []}, 'applications': {'a': [], 'b': []}, 'overlap': step in (3, 4)}
            for key, status in pairs:
                identifier = f'{kind}-{index}-{key}'
                operation = {'cluster': key, 'status': status, 'execution_id': identifier}
                if kind == 'workflow': operation['task_id'] = receipt['tasks'][key]
                else: operation['query'] = report['multicluster']['failure_query' if status == 'failed' else 'query']
                observation['operations'].append(operation)
                observation['applications'][key].append({'id': f'app-20261103-{index:04d}', 'state': 'FINISHED',
                    'name': 'ADDP-Workflow-Engine-' + str(report['multicluster']['clusters'][key]['id']) if kind == 'workflow' else 'ADDP-federated-query'})
            receipt['observations'].append(observation)
        with patch.dict(os.environ, ADDP_ONLINE_TEST_RUN_ID='dual-run'):
            self.assertEqual({key: len(values) for key, values in MODULE.validate_multicluster_receipt(receipt, report).items()}, {'a': 10, 'b': 6})
            for failure in ('identity', 'runtime', 'one_task', 'missing_phase', 'overlap', 'wrong_master', 'reused_app', 'task', 'duplicate_execution', 'query', 'status', 'phase'):
                value = json.loads(json.dumps(receipt))
                if failure == 'identity': value['principal_id'] = '43'
                if failure == 'runtime': value['runtime_id'] = 9
                if failure == 'one_task': value['tasks']['b'] = value['tasks']['a']
                if failure == 'missing_phase': value['observations'].pop()
                if failure == 'overlap': value['observations'][3]['overlap'] = False
                if failure == 'wrong_master': value['observations'][0]['applications']['b'] = value['observations'][0]['applications']['a']; value['observations'][0]['applications']['a'] = []
                if failure == 'reused_app': value['observations'][2]['applications']['a'][0]['id'] = value['observations'][0]['applications']['a'][0]['id']
                if failure == 'task': value['observations'][0]['operations'][0]['task_id'] = 72
                if failure == 'duplicate_execution': value['observations'][2]['operations'][0]['execution_id'] = value['observations'][0]['operations'][0]['execution_id']
                if failure == 'query': value['observations'][6]['operations'][0]['query'] = 'SELECT other'
                if failure == 'status': value['observations'][4]['operations'][0]['status'] = 'success'
                if failure == 'phase': value['observations'][5]['phase'] = 'concurrent'
                with self.subTest(failure=failure), self.assertRaises(MODULE.SuiteError):
                    MODULE.validate_multicluster_receipt(value, report)

    def test_runtime_cleanup_requires_owned_container_zero_drivers_and_private_directories(self):
        for failure in ('foreign', 'driver', 'directory', ''):
            calls = []
            def docker(command, **kwargs):
                calls.append(command)
                if 'inspect' in command:
                    return SimpleNamespace(stdout='foreign' if failure == 'foreign' else 'hdfs-spark-consumer-flow')
                self.assertEqual(command[:4], ['docker', 'exec', 'owned-runtime', 'python'])
                return SimpleNamespace(stdout=json.dumps({'driver_processes': int(failure == 'driver'), 'private_directories': int(failure == 'directory')}))
            with self.subTest(failure=failure), patch.object(MODULE.SPARK.subprocess, 'run', side_effect=docker), patch.object(MODULE.SPARK.time, 'monotonic', side_effect=[0, 31]):
                if failure:
                    with self.assertRaises(ValueError): MODULE.SPARK.runtime_cleanup_evidence('hdfs-spark-consumer-flow', 'owned-runtime')
                else:
                    self.assertEqual(MODULE.SPARK.runtime_cleanup_evidence('hdfs-spark-consumer-flow', 'owned-runtime'), {'driver_processes': 0, 'private_directories': 0})
                if failure == 'foreign': self.assertEqual(len(calls), 1)

    def test_actual_listener_oracle_requires_each_approved_loopback_port(self):
        ipv4 = 'header\n 0: 0100007F:1BA6 00000000:0000 0A rest\n'
        ipv6 = 'header\n 0: 0000000000000000FFFF00000100007F:46A2 00000000000000000000000000000000:0000 0A rest\n'
        self.assertEqual(MODULE.SPARK.loopback_listener_evidence([(False, ipv4), (True, ipv6)], (7078, 18082)),
                         {'7078': '127.0.0.1', '18082': '127.0.0.1'})
        for failure in ('missing', 'public', 'duplicate'):
            text = ipv4
            if failure == 'missing': text = 'header\n'
            if failure == 'public': text = ipv4.replace('0100007F', '00000000')
            if failure == 'duplicate': text += ipv4.splitlines()[1] + '\n'
            with self.subTest(failure=failure), self.assertRaises(ValueError):
                MODULE.SPARK.loopback_listener_evidence([(False, text), (True, ipv6)], (7078, 18082))

    def test_worker_receipt_waits_for_the_exact_unregister_event(self):
        app = {'id': 'app-20261103-0001', 'name': 'ADDP-Workflow-Engine-18', 'state': 'RUNNING'}
        states = [{'status': 'ALIVE', 'aliveworkers': 1, 'activeapps': [app], 'completedapps': []},
                  {'status': 'ALIVE', 'aliveworkers': 1, 'activeapps': [], 'completedapps': [dict(app, state='FINISHED')]}]
        with patch.object(MODULE.SPARK, 'application_snapshot', side_effect=states), patch.object(MODULE.SPARK.time, 'sleep') as wait, patch.object(MODULE.SPARK, 'worker_application_evidence', return_value={'application_id': app['id']}):
            evidence = MODULE.SPARK.worker_evidence(18, 'owned', 'worker', 'http://master', set())
            self.assertEqual(evidence['application_id'], app['id'])
            self.assertEqual(evidence['state'], 'FINISHED')
            wait.assert_called_once_with(.1)

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

    def test_minio_requires_whole_layout_create_identity_and_exact_lineage(self):
        locators = {name: 'addp://engine/7/path/orders.' + name + '?type=file' for name in ('csv', 'json', 'parquet')}
        target = 'addp://engine/9/path/result/results/hdfs_totals/unique-result?type=object'
        definition = MODULE.persistence_workflow(locators, 9, 'file')
        params = definition['tasks'][-1]['params']
        self.assertEqual((params['target_type'], params['mode'], params['format']), ('file', 'create', 'parquet'))
        self.assertEqual(params['target_parent_locator'], 'addp://engine/9/path/result?type=bucket')
        final = {'status': 'success', 'rows': 2, 'target': 's3a://result/results/hdfs_totals/unique-result'}
        execution = {'status': 'success', 'outputs': {'save': {'resource': {'locator': target, 'type': 'object', 'write_mode': 'create'}}},
            'metadata': {'lineage_facts': {'schema_version': 'addp.lineage-facts/v1',
                'inputs': [{'locator': value} for value in locators.values()], 'outputs': [{'locator': target, 'write_mode': 'create'}]},
                'result': {'final_result': final, 'produced_targets': [{'locator': target, 'task_id': 'save', 'layout': 'whole'}],
                    'meta_scan_runs': [{'status': 'submitted', 'target_locator': target, 'execution_id': 'scan'}]}}}
        self.assertEqual(MODULE.validate_persistence(execution, locators, target, 'file'), (final, 'scan'))
        for invalid in ('layout', 'target', 'mode', 'scan'):
            value = json.loads(json.dumps(execution))
            if invalid == 'layout': value['metadata']['result']['produced_targets'][0]['layout'] = 'single'
            if invalid == 'target': value['metadata']['result']['final_result']['target'] = 's3a://result/parent'
            if invalid == 'mode': value['outputs']['save']['resource']['write_mode'] = 'replace'
            if invalid == 'scan': value['metadata']['result']['meta_scan_runs'] = []
            with self.subTest(invalid=invalid), self.assertRaises(MODULE.SuiteError):
                MODULE.validate_persistence(value, locators, target, 'file')

    def test_persistence_compares_rows_by_region_and_keeps_original_runtime_order(self):
        from unittest.mock import Mock
        expected = final_result()
        expected['preview_rows'].reverse()
        report = {'engine_id': 7, 'cluster_id': 8, 'runtime_id': 10,
                  'locators': {name: 'addp://engine/7/path/orders.' + name + '?type=file' for name in ('csv', 'json', 'parquet')},
                  'item_ids': {'csv': 1, 'json': 2, 'parquet': 3}, 'final_result': expected}
        receipt = {'status': 'success', 'rows': 2, 'target': 'results.hdfs_totals'}
        saved = {'status': 'success', 'metadata': {'result': {'runtime_execution_id': 'runtime-save'}}}
        reused = {'metadata': {'result': {'runtime_execution_id': 'runtime-reuse', 'final_result': final_result()}}}
        graph = {'edges': [{'source': {'item_id': source}, 'target': {'item_id': 5},
                 'evidence': {'execution_id': 'saved'}, 'status': 'active', 'relation_kind': 'derive'} for source in (1, 2, 3)]}
        for wrong in ('', 'amount', 'missing', 'duplicate', 'number', 'noncanonical'):
            rows = final_result()['preview_rows']
            rows = [{key: value if key == 'region' else str(value) for key, value in row.items()} for row in rows]
            if wrong == 'amount': rows[0]['csv_amount_sum'] = '1101'
            if wrong == 'number': rows[0]['csv_rows'] = 10
            if wrong == 'noncanonical': rows[0]['csv_rows'] = '010'
            if wrong == 'missing': rows[0].pop('parquet_rows')
            if wrong == 'duplicate': rows.append(rows[0].copy())
            client = Mock()
            client.request.side_effect = [SimpleNamespace(payload={'status': 'success'}), SimpleNamespace(payload=saved), SimpleNamespace(payload=graph)]
            with self.subTest(wrong=wrong), patch.dict(os.environ, ADDP_ONLINE_SPARK_RUNTIME_URL='http://127.0.0.1:8098'), \
                 patch.object(MODULE, 'submit_and_wait', side_effect=[('saved', saved), ('reused', reused)]), \
                 patch.object(MODULE, 'validate_persistence', return_value=(receipt, 'scan')), \
                 patch.object(MODULE.SPARK, 'runtime_status_evidence', return_value={}), \
                 patch.object(MODULE.support, 'find_item', return_value={'id': 5}), \
                 patch.object(MODULE.support, 'preview_rows', return_value=(list(rows[0]), rows)), \
                 patch.object(MODULE, 'persistence_physical', return_value={}):
                if wrong:
                    with self.assertRaisesRegex(MODULE.SuiteError, 'Manager persisted result'):
                        persistence_flow(client, report, 9, 30)
                else:
                    result = persistence_flow(client, report, 9, 30)
                    self.assertEqual(result['lineage_sources'], [1, 2, 3])
                    self.assertEqual(result['manager_preview_rows'], rows)
                    self.assertEqual(result['reuse_final_result'], final_result())
                    self.assertEqual(report['final_result']['preview_rows'][0]['region'], 'west')

    def test_minio_auto_scan_preview_lineage_and_downstream_use_the_real_directory(self):
        from unittest.mock import Mock
        report = {'engine_id': 7, 'cluster_id': 8, 'runtime_id': 10,
                  'locators': {name: 'addp://engine/7/path/orders.' + name + '?type=file' for name in ('csv', 'json', 'parquet')},
                  'item_ids': {'csv': 1, 'json': 2, 'parquet': 3}, 'final_result': final_result()}
        saved = {'status': 'success', 'metadata': {'result': {'runtime_execution_id': 'runtime-save'}}}
        reused = {'metadata': {'result': {'runtime_execution_id': 'runtime-reuse', 'final_result': final_result()}}}
        graph = {'edges': [{'source': {'item_id': source}, 'target': {'item_id': 5},
                 'evidence': {'execution_id': 'saved'}, 'status': 'active', 'relation_kind': 'derive'} for source in (1, 2, 3)]}
        for layout in ('whole', 'single'):
            client = Mock()
            client.request.side_effect = [SimpleNamespace(payload={'status': 'success'}), SimpleNamespace(payload=saved), SimpleNamespace(payload=graph)]
            item = {'id': 5, 'attributes': {'item': {'data_type': 'table', 'format': 'parquet', 'layout': layout}}}
            with self.subTest(layout=layout), patch.dict(os.environ, ADDP_ONLINE_SPARK_RUNTIME_URL='http://runtime'), \
                 patch.object(MODULE, 'submit_and_wait', side_effect=[('saved', saved), ('reused', reused)]) as submit, \
                 patch.object(MODULE, 'validate_persistence', return_value=({'status': 'success', 'rows': 2}, 'scan')), \
                 patch.object(MODULE.SPARK, 'runtime_status_evidence', return_value={}), \
                 patch.object(MODULE.support, 'find_item', return_value=item) as find, \
                 patch.object(MODULE.support, 'preview_rows', return_value=([], [{key: value if key == 'region' else str(value) for key, value in row.items()} for row in final_result()['preview_rows']])), \
                 patch.object(MODULE, 'minio_physical', return_value={'committer': 'magic'}):
                if layout != 'whole':
                    with self.assertRaisesRegex(MODULE.SuiteError, 'table/parquet/whole'):
                        persistence_flow(client, report, 9, 30, 'file')
                else:
                    result = persistence_flow(client, report, 9, 30, 'file')
                    self.assertIn('/hdfs_totals/saved-', result['target_locator'])
                    self.assertEqual(result['lineage_sources'], [1, 2, 3])
                    self.assertEqual(find.call_args.args[-1], 'object')
                    load = submit.call_args.args[1]['tasks'][0]['params']
                    self.assertEqual(load, {'locator': result['target_locator'], 'source_type': 'file'})

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
        report = {'engine_id': 7, 'tenant_id': 2, 'principal_id': '42', 'execution_id': 'execution', 'persistence': {'execution_id': 'save-execution', 'reuse_execution_id': 'reuse-execution'}, 'minio': {'execution_id': 'minio-save', 'reuse_execution_id': 'minio-reuse'}}
        report['tables'] = {engine: {phase: {'execution_id': engine + '-' + phase} for phase in ('created', 'overwrite', 'reuse')} for engine in ('mysql', 'doris')}
        for failure in ('process', 'missing_report', 'identity', 'execution', 'screenshot', 'table-screenshot', 'table-execution', ''):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                evidence = {key: value for key, value in report.items() if key not in ('persistence', 'minio', 'tables')}
                evidence['tables'] = {engine: {phase: facts['execution_id'] for phase, facts in table.items()} for engine, table in report['tables'].items()}
                if failure == 'table-execution': evidence['tables']['doris']['reuse'] = 'other'
                evidence.update(run_id='hdfs-run', meta_ui_scan=True, previews=6, develop_result=True,
                                persist_execution_id='save-execution', reuse_execution_id='reuse-execution', minio_execution_id='minio-save', minio_reuse_execution_id='minio-reuse')
                if failure in ('identity', 'execution'): evidence['principal_id' if failure == 'identity' else 'execution_id'] = 'other'
                def browser(command, cwd, env):
                    self.assertIn('e2e/online/hdfs-spark-consumer-flow.spec.js', command)
                    if failure != 'missing_report': Path(env['ADDP_ONLINE_HDFS_BROWSER_REPORT']).write_text(json.dumps(evidence))
                    for name in ('meta', 'csv', 'json', 'parquet', 'original', 'workflow', 'persisted', 'save', 'reuse', 'minio', 'minio-save', 'minio-reuse'):
                        if failure != 'screenshot' or name != 'workflow': (root / ('hdfs-' + name + '-console.png')).write_bytes(b'proof')
                    for engine in ('mysql', 'doris'):
                        for phase in ('created', 'overwrite', 'reuse', 'preview'):
                            if failure != 'table-screenshot' or (engine, phase) != ('doris', 'preview'):
                                (root / f'hdfs-{engine}-{phase}-console.png').write_bytes(b'proof')
                    return SimpleNamespace(returncode=int(failure == 'process'))
                with patch.dict(os.environ, ADDP_ONLINE_ARTIFACT_DIR=temporary, ADDP_ONLINE_TEST_RUN_ID='hdfs-run'), patch.object(MODULE.subprocess, 'run', side_effect=browser):
                    if failure:
                        with self.assertRaises(MODULE.SuiteError): MODULE.run_browser(report)
                    else: self.assertEqual(MODULE.run_browser(report), evidence)


if __name__ == '__main__': unittest.main()


class ComputeClusterRoleTest(unittest.TestCase):
    def test_selector_is_independent_of_storage_and_keeps_credentials_private(self):
        from copy import deepcopy
        cluster = {'id': 8, 'engine_type': 'spark', 'engine_origin': 'general', 'is_builtin': False,
                   'lifecycle_state': 'active', 'connection_status': 'online',
                   'capabilities': {'schema_version': 'engine.capabilities/v1', 'engine_type': 'spark', 'engine_family': 'compute', 'compute': {}}}
        self.assertEqual(MODULE.SPARK.require_compute_cluster(8, [{'id': 7}], [cluster]), cluster)
        for mutation in ('catalog', 'missing', 'duplicate', 'offline', 'storage', 'credentials', 'builtin', 'endpoint'):
            item = deepcopy(cluster)
            catalog, selectors = [{'id': 7}], [item]
            if mutation == 'catalog': catalog.append({'id': 8})
            if mutation == 'missing': selectors = []
            if mutation == 'duplicate': selectors.append(deepcopy(item))
            if mutation == 'offline': item['connection_status'] = 'offline'
            if mutation == 'storage': item['capabilities']['storage'] = {}
            if mutation == 'credentials': item['connection_info'] = {}
            if mutation == 'builtin': item['is_builtin'] = True
            if mutation == 'endpoint': item['runtime_endpoint'] = {'port': 7077}
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                MODULE.SPARK.require_compute_cluster(8, catalog, selectors)
