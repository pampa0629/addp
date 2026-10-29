import copy
import importlib
import json
import tempfile
import unittest
import threading
import uuid
import re
from contextlib import contextmanager
from pathlib import Path
from unittest.mock import patch
from urllib.parse import parse_qs, urlsplit

REVIEW = importlib.import_module('scripts.test.graph-review-online')
EXTRACTOR = importlib.import_module('scripts.test.graph-review-extractor')
SUITE = importlib.import_module('scripts.test.graph-protection-online')
EXECUTION = importlib.import_module('scripts.test.graph-execution-online')
CLEANUP = importlib.import_module('scripts.test.cleanup-execution-online')
CONTROLS = importlib.import_module('scripts.test.execution-online-support')


def reviewed_items():
    return [dict(id=index + 1, source_text=source, status=status,
                 reviewed_by=None if status == 'pending' else 17,
                 reviewed_at=None if status == 'pending' else '2026-10-20T00:00:00Z',
                 element_id='' if status in {'pending', 'rejected'} else 'native-' + source,
                 final_content={'properties': {'name': 'Modified fixture'}} if status == 'modified' else None)
            for index, (source, status) in enumerate(REVIEW.EXPECTED_STATUS.items())]


class GraphReviewOnlineTest(unittest.TestCase):
    def test_controlled_extraction_produces_unique_entities_and_native_failure_after_valid_item(self):
        data = EXTRACTOR.extraction({'text': REVIEW.MATERIAL})
        self.assertEqual([e['properties']['id'] for e in data['entities']], ['p1', 'p2', *EXTRACTOR.REVIEW_IDS])
        self.assertEqual([e['confidence'] for e in data['entities']], [0.99, 0.99, *([0.4] * 6)])
        self.assertIsInstance(data['entities'][-2]['properties']['unsupported'], dict)
        self.assertNotIn('unsupported', data['entities'][-1]['properties'])
        self.assertEqual(data['relations'][0]['source_text'], 'review-link')
        with self.assertRaises(ValueError): EXTRACTOR.extraction({'text': 'business material'})

    def test_native_review_result_cannot_hide_lost_success_wrong_owner_or_unsupported_write(self):
        items = reviewed_items()
        self.assertEqual(len(REVIEW.verify_items(SUITE, items, '17')), 7)
        mutations = [lambda rows: rows.pop(), lambda rows: rows.append(copy.deepcopy(rows[0])),
                     lambda rows: rows[0].update(element_id=''), lambda rows: rows[0].update(reviewed_by=99),
                     lambda rows: rows[0].update(reviewed_at=None), lambda rows: rows[1].update(element_id='unexpected'),
                     lambda rows: rows[2]['final_content']['properties'].update(name='old'),
                     lambda rows: rows[4].update(status='approved'), lambda rows: rows[4].update(reviewed_by=17),
                     lambda rows: rows[5].update(status='pending')]
        for mutate in mutations:
            changed = copy.deepcopy(items)
            mutate(changed)
            with self.subTest(mutation=mutate), self.assertRaises(SUITE.SuiteError): REVIEW.verify_items(SUITE, changed, '17')

    def test_all_review_states_are_explicit_complete_and_not_the_default_pending_queue(self):
        rows = reviewed_items()
        calls = []
        class Client:
            def request(self, method, path, expected):
                calls.append((method, path, expected))
                status = parse_qs(urlsplit(path).query).get('status', ['pending'])[0]
                selected = [item for item in rows if item['status'] == status]
                page = {'data': copy.deepcopy(selected), 'total': len(selected)}
                if mutation == 'missing': page['data'].pop()
                elif mutation == 'wrong-status': page['data'][0]['status'] = 'unknown'
                elif mutation == 'wrong-total': page['total'] += 1
                return SUITE.SUPPORT.Response(200, page)
        for mutation in ('none', 'missing', 'wrong-status', 'wrong-total'):
            calls.clear()
            with self.subTest(mutation=mutation):
                if mutation == 'none':
                    all_items = REVIEW.read_all_review_items(SUITE, Client(), '/review')
                    self.assertEqual(len(REVIEW.verify_items(SUITE, all_items, '17')), 7)
                    self.assertEqual([path for _, path, _ in calls], [f'/review?status={status}&page_size=100'
                                     for status in ('approved', 'rejected', 'modified', 'pending')])
                else:
                    with self.assertRaises(SUITE.SuiteError): REVIEW.read_all_review_items(SUITE, Client(), '/review')

    def test_review_native_target_is_always_separate(self):
        with patch.dict(REVIEW.os.environ, {'ADDP_ONLINE_GRAPH_ENGINE': 'falkordb'}):
            target = REVIEW.target_path(SUITE, 13)
            self.assertEqual(target['engine_id'], 13)
            self.assertEqual(target['segments'][-1]['name'], 'graph_review_fixture')
            self.assertEqual(SUITE.target_paths(13)[0]['names'], ['graph_protection_fixture'])
        for review_id in ('13', '14', '15'):
            with patch.dict(REVIEW.os.environ, {'ADDP_ONLINE_GRAPH_ENGINE': 'neo4j', 'ADDP_ONLINE_GRAPH_CONTROL_ENGINE_ID': '14', 'ADDP_ONLINE_GRAPH_REVIEW_ENGINE_ID': review_id}):
                if review_id != '15':
                    with self.assertRaises(SUITE.SuiteError): REVIEW.target_path(SUITE, 13)
                else: self.assertEqual(REVIEW.target_path(SUITE, 13)['engine_id'], 15)

    def test_missing_or_forged_browser_evidence_never_passes_and_no_api_tokens_enter_browser(self):
        expected = dict(run_id='review-run', tenant_id='2', principal_id='17', engine_type='falkordb', graph_id=19, items={'approve': 23})
        with tempfile.TemporaryDirectory() as directory:
            environment = {'ADDP_ONLINE_ARTIFACT_DIR': directory, 'ADDP_ONLINE_GRAPH_REVIEW_USERNAME': 'reviewer',
                           'ADDP_ONLINE_GRAPH_REVIEW_PASSWORD': 'review-password', 'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'reader-secret',
                           'ADDP_ONLINE_GRAPH_PREPARER_ACCESS_TOKEN': 'preparer-secret', 'ADDP_ONLINE_TEST_USER_PASSWORD': 'reader-password'}
            for mutation, screenshots in (({}, True), ({'principal_id': 'other'}, True), ({'real_model': True}, True), ({'real_model': 0}, True),
                    ({'batch_continued': False}, True), ({'batch_status': 200}, True), ({'refreshed': False}, True),
                    ({'selection_cleared': False}, True), ({'schema_version': 'addp.graph-review-browser/v1'}, True),
                    ({'_missing_visual': True}, True), ({'_omit': ['selection_stable_visible']}, True),
                    ({'selection_stable_visible': False}, True), ({'selection_stable_visible': 1}, True), ({'_visual': {'principal_id': 'other'}}, True),
                    ({'_state': {'checked': True}}, True), ({'_state': {'visible': False}}, True), ({'_state': {'checked': 0}}, True),
                    ({'_state': {'indeterminate': True}}, True), ({'_state': {'mixedClass': True}}, True),
                    ({'_state': {'checkedClass': True}}, True), ({'_state': {'scope': 'row'}}, True),
                    ({'_state': {'tick': 'matrix(1, 0, 0, 1, 0, 0)'}}, True), ({'_state': {'animating': True}}, True),
                    ({'_state': {'background': 'rgb(0, 0, 255)'}}, True),
                    ({'_visual': {'stable_frames': 1}}, True), ({'browser_errors': 1}, True), ({}, False)):
                def launch(command, **kwargs):
                    self.assertIn('e2e/online/graph-review.spec.js', command)
                    self.assertFalse(any(k.endswith('_ACCESS_TOKEN') for k in kwargs['env']))
                    self.assertNotIn('ADDP_ONLINE_TEST_USER_PASSWORD', kwargs['env'])
                    self.assertEqual(kwargs['env']['ADDP_ONLINE_GRAPH_REVIEW_PASSWORD'], 'review-password')
                    evidence = dict(expected, schema_version='addp.graph-review-browser/v2', result='passed', permissions=sorted(REVIEW.PERMISSIONS),
                                    single_approve=True, single_reject=True, modify=True, relation_approve=True,
                                    batch_status=500, batch_continued=True, selection_cleared=True, selection_stable_visible=True, refreshed=True,
                                    unexpected_http_failures=0, browser_errors=0, controlled_extraction=True, real_model=False)
                    evidence.update({key: value for key, value in mutation.items() if not key.startswith('_')})
                    for key in mutation.get('_omit', []): evidence.pop(key, None)
                    states = [dict(scope=scope, visible=True, checked=False, indeterminate=False, checkedClass=False, mixedClass=False,
                                   background='rgb(255, 255, 255)', emptyBackground='rgb(255, 255, 255)',
                                   tick='none', emptyTick='none', animating=False) for scope in ('header', 'row')]
                    for state in states: state.update(mutation.get('_state', {}))
                    visual = dict(expected, schema_version='addp.graph-review-stable-selection/v1', stable_frames=3, states=states)
                    visual.update(mutation.get('_visual', {}))
                    if not mutation.get('_missing_visual'):
                        (Path(directory) / 'graph-review-stable-selection.json').write_text(json.dumps(visual))
                    Path(kwargs['env']['ADDP_ONLINE_GRAPH_REVIEW_REPORT']).write_text(json.dumps(evidence))
                    if screenshots:
                        for phase in ('pending', 'modified', 'partial-failure'):
                            (Path(directory) / f'graph-review-{phase}-console.png').write_bytes(b'test screenshot')
                    return type('Process', (), {'returncode': 0})()
                with self.subTest(mutation=mutation, screenshots=screenshots), patch.dict(REVIEW.os.environ, environment), patch.object(REVIEW.subprocess, 'run', side_effect=launch):
                    if mutation or not screenshots:
                        with self.assertRaises(SUITE.SuiteError): REVIEW.browser(SUITE, expected)
                    else: self.assertEqual(REVIEW.browser(SUITE, expected)['result'], 'passed')

    def test_complete_review_flow_checks_native_results_and_cleans_its_own_resources(self):
        calls, finished = [], [False]
        rows = reviewed_items()
        class Client:
            def request(self, method, path, expected, body=None):
                calls.append((method, path, body))
                data = {}
                if path.endswith('/graphs') and method == 'POST': data = {'id': 19, 'is_public': False}
                elif path == '/api/v1/graph/graphs/19' and method == 'GET': data = {'id': 19, 'version': 7}
                elif path.endswith('/run'):
                    if expected != (202,): raise AssertionError('old synchronous build contract')
                    data = {'status': 'pending', 'execution_id': '11111111-1111-4111-8111-111111111111'}
                elif path.endswith('/build/tasks') and method == 'POST': data = {'id': 23}
                elif path.endswith('/build/tasks/23') and method == 'GET' and expected == (200,):
                    data = {'status': 'success', 'execution_id': '11111111-1111-4111-8111-111111111111', 'stats': {'auto_written': 2, 'pending_review': 7}}
                elif path.endswith('/materials') and method == 'GET':
                    data = [{'id': 29, 'status': 'completed', 'total_chunks': 1, 'processed_chunks': 1}]
                elif '/review?' in path:
                    status = parse_qs(urlsplit(path).query).get('status', ['pending'])[0]
                    queue = rows if finished[0] else [dict(row, status='pending', content={}) for row in rows]
                    selected = [item for item in queue if item['status'] == status]
                    data = {'total': len(selected), 'data': selected}
                elif path.endswith('/review') and method == 'GET': data = {'total': 0}
                elif path.endswith('/entities/Person'):
                    data = {'total': 6, 'data': [{'id': 'native-' + key, 'properties': {'id': key, 'name': 'Modified fixture' if key == 'modify' else key}}
                                                for key in ('p1', 'p2', 'approve', 'modify', 'batch-before', 'batch-after')]}
                elif path.endswith('/stats'): data = {'node_count': 6, 'relationship_count': 1}
                elif path.endswith('/neighbors'): data = {'neighbors': [{'node': {'id': 'native-p2'}}]}
                elif path.endswith('/expand'): data = {'edges': [{'id': 'native-review-link', 'source': 'native-p1', 'target': 'native-p2'}]}
                return SUITE.SUPPORT.Response(expected[0], data)
        def browser(*args):
            finished[0] = True
            return {'result': 'passed'}
        report = {'identities': {'reader': {'principal_id': '4'}}}
        with patch.dict(REVIEW.os.environ, {'GATEWAY_URL': 'http://127.0.0.1:8000', 'ADDP_ONLINE_GRAPH_REVIEW_ACCESS_TOKEN': 'review-secret',
                'ADDP_ONLINE_TEST_RUN_ID': 'review-run', 'ADDP_ONLINE_GRAPH_ENGINE': 'falkordb'}), \
             patch.object(SUITE, 'Client', return_value=Client()), patch.object(SUITE, 'identity', return_value={'principal_id': '17'}), \
             patch.object(REVIEW, 'upload', return_value=29), patch.object(REVIEW, 'browser', side_effect=browser):
            REVIEW.run(SUITE, Client(), Client(), 13, 2, 9, 11, report)
        self.assertTrue(report['review']['passed'])
        self.assertTrue(all('?status=' in path for method, path, _ in calls if method == 'GET' and '/review?' in path))
        self.assertEqual(report['review']['cleanup'], {'errors': [], 'domain_resources_remaining': 0})
        self.assertIn(('PUT', '/api/v1/graph/graphs/11/review/3', {'final_content': {}}), calls)
        self.assertIn(('DELETE', '/api/v1/graph/graphs/19/build/tasks/23/materials/29', None), calls)
        self.assertIn(('DELETE', '/api/v1/graph/graphs/19/build/tasks/23', None), calls)
        self.assertIn(('DELETE', '/api/v1/graph/graphs/19', {'version': 7}), calls)
        self.assertFalse(any(method == 'DELETE' and '/graphs/11' in path for method, path, _ in calls))

    def test_material_failure_still_deletes_task_queue_and_graph(self):
        calls = []
        class Client:
            def request(self, method, path, expected, body=None):
                calls.append((method, path))
                data = {'id': 19, 'is_public': False} if path.endswith('/graphs') else {'id': 23}
                if path == '/api/v1/graph/graphs/19' and method == 'GET': data = {'version': 7}
                if '/review' in path: data = {'total': 0}
                return SUITE.SUPPORT.Response(expected[0], data)
        report = {'identities': {'reader': {'principal_id': '4'}}}
        with patch.dict(REVIEW.os.environ, {'GATEWAY_URL': 'http://127.0.0.1:8000', 'ADDP_ONLINE_GRAPH_REVIEW_ACCESS_TOKEN': 'review-secret',
                'ADDP_ONLINE_TEST_RUN_ID': 'review-run', 'ADDP_ONLINE_GRAPH_ENGINE': 'falkordb'}), \
             patch.object(SUITE, 'Client', return_value=Client()), patch.object(SUITE, 'identity', return_value={'principal_id': '17'}), \
             patch.object(REVIEW, 'upload', side_effect=SUITE.SuiteError('upload failed')):
            with self.assertRaisesRegex(SUITE.SuiteError, 'upload failed'):
                REVIEW.run(SUITE, Client(), Client(), 13, 2, 9, 11, report)
        self.assertIn(('DELETE', '/api/v1/graph/graphs/19/build/tasks/23'), calls)
        self.assertIn(('DELETE', '/api/v1/graph/graphs/19'), calls)
        self.assertEqual(report['review']['cleanup']['domain_resources_remaining'], 0)


class ExecutionRunnerContractTest(unittest.TestCase):
    """These tests run through the existing test-graph-online-runner entry.

    They prove runner oracles and fault ownership, not Hosted execution.
    """
    def test_enqueued_response_and_cancel_acceptance_are_not_terminal(self):
        eid = str(uuid.uuid4())
        self.assertEqual(EXECUTION.assert_enqueued({'execution_id': eid, 'status': 'pending'}), eid)
        for response in ({'execution_id': eid, 'status': 'running'}, {'execution_id': 'bad', 'status': 'pending'},
                         {'execution_id': eid, 'status': 'pending', 'lease_token': 'private'}):
            with self.subTest(response=response), self.assertRaises(CONTROLS.SuiteError):
                EXECUTION.assert_enqueued(response)
        running = dict(execution_id=eid, attempt=1, status='running', cancellation_requested=True)
        EXECUTION.assert_cancel(running, eid, 1, True)
        for change in ({'status': 'cancelled'}, {'attempt': 2}, {'cancellation_requested': False}):
            with self.subTest(change=change), self.assertRaises(CONTROLS.SuiteError):
                EXECUTION.assert_cancel(dict(running, **change), eid, 1, True)

    def test_unknown_or_replayed_extraction_never_passes_barrier(self):
        calls = EXTRACTOR.LifecycleCalls()
        key = str(uuid.uuid4())
        with self.assertRaises(KeyError): calls.extract(key)
        self.assertEqual(calls.control('arm', key), dict(entered=0, returned=0, released=False))
        with self.assertRaises(ValueError): calls.control('arm', key)
        entered = threading.Event()
        result = []
        def extract():
            entered.set()
            result.append(calls.extract(key))
        worker = threading.Thread(target=extract)
        worker.start()
        self.assertTrue(entered.wait(1))
        calls.control('release', key)
        worker.join(2)
        self.assertFalse(worker.is_alive())
        self.assertEqual(result, [{'entities': [], 'relations': []}])
        self.assertEqual(calls.control('read', key), dict(entered=1, returned=1, released=True))
        with self.assertRaises(ValueError): calls.extract(key)

    def test_public_response_rejects_nested_private_execution_facts(self):
        for key in ('lease_token', 'execution_config', 'raw_response', 'access_token', 'target_descriptor'):
            with self.subTest(key=key), self.assertRaises(CONTROLS.SuiteError):
                CONTROLS.safe_public({'items': [{'metadata': {key: 'never-persist'}}]})
        CONTROLS.safe_public({'items': [{'status': 'pending', 'attempt': 0}]})

    def test_fault_scope_rejects_remote_urls_and_database_pause(self):
        for url in ('https://example.com', 'http://127.0.0.1:8000/?token=private', 'http://user:pass@127.0.0.1:8000',
                    'http://127.0.0.1:8000/api', 'http://localhost:8000'):
            with self.subTest(url=url), self.assertRaises(CONTROLS.SuiteError):
                CONTROLS.local_client(url, 'private')
        self.assertEqual(CONTROLS.local_client('http://127.0.0.1:8000', 'private').base_url, 'http://127.0.0.1:8000')
        deployment = object.__new__(CONTROLS.OwnedDeployment)
        with self.assertRaises(CONTROLS.SuiteError): deployment.pause('addp-postgres')

    def test_private_execution_probe_is_read_only_and_scope_bound(self):
        eid = str(uuid.uuid4())
        deployment = object.__new__(CONTROLS.OwnedDeployment)
        with patch.object(deployment, 'inspect', return_value={'Id': 'owned-pg'}), \
             patch.object(deployment, 'command', return_value=json.dumps({'execution_id': eid})) as command, \
             patch.dict(CONTROLS.os.environ, {'POSTGRES_USER': 'fixture'}):
            deployment.execution(7, eid, 'graph', 'kg_build')
            sql = command.call_args.kwargs['input']
            self.assertTrue(sql.startswith("BEGIN READ ONLY; SET LOCAL statement_timeout='5s';"))
            self.assertIn("tenant_id=7 AND execution_id='" + eid + "'", sql)
            self.assertIn("AND module='graph' AND task_type='kg_build'", sql)
            self.assertIn("to_jsonb(t) - 'lease_token' - 'execution_config'", sql)
            count = command.call_count
            for identity, owner in (("';DELETE FROM common.task_executions;--", 'graph'), (eid, 'unregistered')):
                with self.assertRaises(CONTROLS.SuiteError): deployment.execution(7, identity, owner, 'kg_build')
            self.assertEqual(command.call_count, count)

    def test_peer_crash_restarts_only_the_pinned_container_and_checks_readiness(self):
        deployment = object.__new__(CONTROLS.OwnedDeployment)
        deployment.paused = set()
        deployment.pins = {'graph-backend-peer': ('exact-id', 'image', 'old')}
        before = dict(Id='exact-id', Image='image', State={'Running': True, 'StartedAt': 'old', 'Health': {'Status': 'healthy'}})
        after = dict(Id='exact-id', Image='image', State={'Running': True, 'StartedAt': 'new', 'Health': {'Status': 'healthy'}})
        stopped = dict(Id='exact-id', Image='image', State={'Running': False, 'ExitCode': 137})
        with patch.object(deployment, 'inspect', side_effect=[before, after, after]), \
             patch.object(deployment, 'command', side_effect=['', json.dumps([stopped]), '']) as command:
            deployment.crash_and_start_graph_peer()
        self.assertEqual([c.args[0] for c in command.call_args_list], [
            ['docker', 'kill', '--signal=KILL', 'exact-id'], ['docker', 'inspect', 'exact-id'], ['docker', 'start', 'exact-id']])

    def test_cleanup_no_start_projection_does_not_fabricate_duration_or_receipt(self):
        pending = dict(status='pending', attempt=0, started_at=None, completed_at=None, execution_time_ms=None)
        CLEANUP.assert_unstarted(pending)
        for change in ({'attempt': 1}, {'started_at': '2026-10-29T00:00:00Z'}, {'execution_time_ms': 0}):
            with self.subTest(change=change), self.assertRaises(CONTROLS.SuiteError):
                CLEANUP.assert_unstarted(dict(pending, **change))
        rejected = dict(pending, status='failed', completed_at='2026-10-29T00:00:00Z',
                        cleanup_admission={'schema': 'addp.cleanup-admission-view/v1', 'state': 'closed_rejected'})
        CLEANUP.assert_rejected(rejected)
        for change in ({'receipt': {'status': 'success'}}, {'execution_time_ms': 0}, {'status': 'cancelled'}):
            with self.subTest(change=change), self.assertRaises(CONTROLS.SuiteError):
                CLEANUP.assert_rejected(dict(rejected, **change))

    def test_cleanup_result_requires_same_committed_owner_receipt(self):
        request, parent, child = (str(uuid.uuid4()) for _ in range(3))
        result = dict(schema='addp.cleanup-professional-result/v1', request_id=request, execution_id=child,
                      module='graph', attempt=1, action='scan', status='success', summary={'scanned_items': 1}, statistics={'graphs': 1})
        receipt = {key: value for key, value in result.items() if key not in {'statistics', 'schema'}}
        receipt.update(schema='addp.cleanup-receipt/v1', tenant_id=7, parent_execution_id=parent)
        row = dict(execution_id=child, parent_execution_id=parent, module='graph', attempt=1, status='success', receipt=receipt)
        CLEANUP.assert_result(result, row, request, 7)
        for change in ({'module': 'meta'}, {'attempt': 2}, {'summary': {}}, {'statistics': {'count': True}},
                       {'statistics': {'count': -1}}, {'raw_response': 'private'}, {'request_id': str(uuid.uuid4())}):
            with self.subTest(change=change), self.assertRaises(CONTROLS.SuiteError):
                CLEANUP.assert_result(dict(result, **change), row, request, 7)

    def test_cleanup_child_set_must_be_complete_unique_and_one_shot(self):
        parent = str(uuid.uuid4())
        child = dict(module='graph', execution_id=str(uuid.uuid4()), parent_execution_id=parent,
                     task_type='cleanup_executor', execution_boundary='bounded', max_attempts=1)
        value = {'task': {'execution_id': parent}, 'module_executions': [child]}
        self.assertEqual(CLEANUP.children(value, ['graph']), [child])
        for rows in ([], [child, child], [dict(child, max_attempts=2)], [dict(child, parent_execution_id=str(uuid.uuid4()))]):
            with self.subTest(rows=rows), self.assertRaises(CONTROLS.SuiteError):
                CLEANUP.children(dict(value, module_executions=rows), ['graph'])

    def test_owned_controls_reject_replaced_container_and_recover_after_uncertain_pause(self):
        with tempfile.TemporaryDirectory() as directory:
            secret = Path(directory)
            env = {'ADDP_ONLINE_TEST_RUN_ID': 'fixture-run'}
            row = dict(Id='owned-id', Image='owned-image', Name='/graph-backend',
                       State=dict(Running=True, Paused=False, StartedAt='original'),
                       Config=dict(Labels={'com.docker.compose.project.working_dir': str(CONTROLS.ROOT)},
                                   Env=['ADDP_ONLINE_HOSTED=1', 'ADDP_ONLINE_TEST_RUN_ID=fixture-run']))
            fail_pause = [True]
            def command(args, **kwargs):
                if args[1] == 'inspect': return json.dumps([row])
                self.assertEqual(args[-1], 'owned-id')
                if args[1] == 'pause':
                    row['State']['Paused'] = True
                    if fail_pause[0]: raise CONTROLS.SuiteError('uncertain response')
                elif args[1] == 'unpause': row['State']['Paused'] = False
                return ''
            with patch.dict(CONTROLS.os.environ, env), patch.object(CONTROLS.HOSTED, 'deployment_secret', return_value=secret):
                deployment = CONTROLS.OwnedDeployment()
                with patch.object(deployment, 'command', side_effect=command):
                    with self.assertRaises(CONTROLS.SuiteError): deployment.pause('graph-backend')
                    self.assertTrue(row['State']['Paused'])
                    with self.assertRaises(CONTROLS.SuiteError): CONTROLS.OwnedDeployment()
                    recovered = CONTROLS.OwnedDeployment(recovering=True)
                    with patch.object(recovered, 'command', side_effect=command): recovered.close()
                    self.assertFalse(row['State']['Paused'])
                    row['State']['StartedAt'] = 'replaced'
                    with self.assertRaises(CONTROLS.SuiteError): deployment.inspect('graph-backend')

    def test_graph_release_failure_still_resumes_all_owned_processes(self):
        deployment = unittest.mock.Mock()
        faults = object.__new__(EXECUTION.GraphFaults)
        faults.deployment, faults.keys = deployment, {str(uuid.uuid4()), str(uuid.uuid4())}
        with patch.object(faults, 'control', side_effect=CONTROLS.SuiteError('release failed')) as release:
            with self.assertRaises(CONTROLS.SuiteError): faults.close()
        self.assertEqual(release.call_count, 2)
        deployment.close.assert_called_once()

    def test_stale_graph_completion_requires_exact_execution_and_lease_failure(self):
        eid = str(uuid.uuid4())
        deployment = unittest.mock.Mock()
        deployment.inspect.return_value = {'Id': 'owned-id', 'State': {'StartedAt': 'start'}}
        faults = object.__new__(EXECUTION.GraphFaults)
        faults.deployment = deployment
        for text, expected in (
            (f'graph cancellation remains unresolved for execution {eid}: conflict: execution lease is no longer current', True),
            (f'failed to finish graph execution {eid}: conflict: execution lease validity changed', True),
            (f'graph cancellation remains unresolved for execution {eid}: database unavailable', False),
            (f'graph cancellation remains unresolved for execution {uuid.uuid4()}: execution lease is no longer current', False),
            ('', False),
        ):
            with self.subTest(expected=expected, text=text):
                deployment.command.return_value = text
                self.assertEqual(faults.stale_runtime_rejected(eid), expected)
        self.assertTrue(deployment.command.call_args.kwargs['include_stderr'])

    def test_cleanup_orchestration_uses_real_contract_and_retains_failed_stage(self):
        for fail_owner_result in (False, True):
            with self.subTest(fail_owner_result=fail_owner_result):
                requests, bodies, paused, reads = {}, {}, set(), []
                def record(rid, scope, action):
                    parent = str(uuid.uuid5(uuid.NAMESPACE_URL, rid))
                    rows = []
                    unclaimed = scope == ['asset']
                    for owner in scope:
                        cid = str(uuid.uuid5(uuid.NAMESPACE_URL, rid + owner))
                        receipt = dict(schema='addp.cleanup-receipt/v1', request_id=rid, execution_id=cid,
                                       parent_execution_id=parent, tenant_id=7, module=owner, attempt=1,
                                       action=action, status='success', summary={})
                        row = dict(module=owner, execution_id=cid, parent_execution_id=parent,
                                   task_type='cleanup_executor', execution_boundary='bounded', max_attempts=1,
                                   status='success', attempt=1, receipt=receipt)
                        if unclaimed:
                            row.update(status='failed', attempt=0, started_at=None, completed_at='now', execution_time_ms=None,
                                       cleanup_admission={'schema': 'addp.cleanup-admission-view/v1', 'state': 'closed_rejected'})
                            del row['receipt']
                        rows.append(row)
                    return dict(task_id=rid, status='timeout' if unclaimed else 'success', cutoff_at='now',
                                task={'task_id': rid, 'execution_id': parent}, module_executions=rows)
                class Deployment:
                    def execution(self, tenant, eid, owner, task_type):
                        for value in requests.values():
                            if eid == value['task']['execution_id']: return {'status': 'timeout'}
                            for child in value['module_executions']:
                                if child['execution_id'] == eid:
                                    if child['attempt'] == 0:
                                        return dict(execution_id=eid, status='pending' if 'asset-backend' in paused else 'failed',
                                                    attempt=0, started_at=None, completed_at=None, execution_time_ms=None, event_count=0)
                                    return {'execution_id': eid, 'status': 'success', 'attempt': 1, 'event_count': 2}
                        raise AssertionError('unknown execution probe')
                    def read(self, query):
                        reads.append(query)
                        if 'count(*)' in query: return 0
                        if 'system.cleanup_requests' in query:
                            rid = re.search("request_id='([^']+)'", query)[1]
                            return {'parent_execution_id': requests[rid]['task']['execution_id']}
                        parent = re.search("parent_execution_id='([^']+)'", query)[1]
                        return {'execution_id': next(v for v in requests.values() if v['task']['execution_id'] == parent)['module_executions'][0]['execution_id']}
                    @contextmanager
                    def paused_owner(self, owner):
                        paused.add(owner)
                        try: yield
                        finally: paused.remove(owner)
                    def close(self): paused.clear()
                class Client:
                    def __init__(self, who): self.who = who
                    def request(self, method, path, expected, body=None):
                        status, payload = 200, {}
                        if method == 'POST':
                            rid = body['request_id']
                            if rid in bodies and bodies[rid] != body:
                                status = 409
                            elif path.endswith('/execute') and bodies[body['based_on_scan']]['scope'] == ['meta']:
                                status, payload = 409, {'error_code': 'cleanup_manager_scope_required'}
                            else:
                                scope = body.get('scope') or bodies[body['based_on_scan']]['scope']
                                requests[rid] = record(rid, scope, path.rsplit('/', 1)[-1])
                                bodies[rid] = body
                                payload = {'task_id': rid}
                        elif '/tasks/' in path:
                            rid = path.rsplit('/', 1)[-1]
                            if 'graph-backend' in paused:
                                status = 503
                            else:
                                payload = copy.deepcopy(requests[rid])
                                if self.who != 'owner': payload['module_executions'] = []
                        elif '/cleanup-executions/' in path:
                            cid = path.split('/')[-2]
                            child = next((c for r in requests.values() for c in r['module_executions'] if c['execution_id'] == cid), None)
                            if self.who == 'reader': status = 403
                            elif self.who != 'owner' or child is None: status = 404
                            elif child['attempt'] == 0: status = 409
                            else:
                                payload = {k: v for k, v in child['receipt'].items() if k not in {'parent_execution_id', 'tenant_id'}}
                                payload.update(schema='addp.cleanup-professional-result/v1', statistics={})
                                if fail_owner_result: payload['attempt'] = 2
                        else: raise AssertionError(path)
                        if status not in expected: raise AssertionError((path, expected, status))
                        return SUITE.SUPPORT.Response(status, payload)
                report = {}
                with patch.object(CONTROLS, 'user_identity', side_effect=lambda c, *_: c.who):
                    if fail_owner_result:
                        with self.assertRaises(CONTROLS.SuiteError):
                            CLEANUP.run(Client('owner'), Client('other'), Client('reader'), 7, Deployment(), report)
                        self.assertEqual(report['result'], 'failed')
                    else:
                        CLEANUP.run(Client('owner'), Client('other'), Client('reader'), 7, Deployment(), report)
                        self.assertEqual(report['result'], 'passed')
                        self.assertEqual(len(report['owner_results']), 20)
                        self.assertIn('meta_only_execute_refused_without_request', report['checks'])
                        self.assertEqual(len(requests), 4)
                self.assertFalse(paused)
                self.assertEqual(report['control_cleanup'], 'passed')
                self.assertEqual(report['physical_effect_acceptance'], 'not_run')

    def test_queued_revocation_is_bound_to_one_assignment_and_real_denial(self):
        for wrong_target, denial_code in ((False, 'cleanup_authorization_denied'), (True, 'cleanup_authorization_denied'), (False, 'cleanup_unstarted_rejected')):
            with self.subTest(wrong_target=wrong_target, denial_code=denial_code):
                calls, resumed = [], [False]
                parent, child = str(uuid.uuid4()), str(uuid.uuid4())
                assignment = dict(id='23', principal_id='other' if wrong_target else '17', membership_id='19', principal_type='user',
                                  username='external-online-cleanup-revokee', role_key='online.external_online_cleanup_revokee',
                                  scope_type='tenant', status='active')
                class Client:
                    def __init__(self, admin=False): self.admin = admin
                    def request(self, method, path, expected, body=None):
                        calls.append((self.admin, method, path))
                        data, status = {}, 200
                        if path.endswith('/auth/context'):
                            data = dict(principal={'type': 'user', 'id': '99' if self.admin else '17'},
                                        context={'type': 'tenant', 'tenant_id': '7', 'tenant_membership_id': '19'},
                                        token={'type': 'first_party_access_token'},
                                        authorization={'role_assignments': [{'role_key': 'tenant.administrator',
                                          'permissions': ['iam.tenant_role_assignment.read', 'iam.tenant_role_assignment.revoke']}]})
                        elif self.admin:
                            data = dict(assignment, status='revoked' if method == 'POST' else 'active')
                        elif method == 'POST': data = {'task_id': body['request_id']}
                        else: status = 401
                        if status not in expected: raise AssertionError((status, expected))
                        return SUITE.SUPPORT.Response(status, data)
                class Deployment:
                    @contextmanager
                    def paused_owner(self, owner):
                        if owner != 'asset-backend': raise AssertionError(owner)
                        yield
                        resumed[0] = True
                    def read(self, query):
                        return {'parent_execution_id': parent} if 'system.cleanup_requests' in query else {'execution_id': child}
                    def execution(self, tenant, eid, owner, kind):
                        if eid == parent: return {'status': 'running'}
                        return dict(status='failed' if resumed[0] else 'pending', attempt=0, started_at=None,
                                    completed_at=None, execution_time_ms=None, event_count=0, error_code=denial_code)
                report = {'requests': [], 'checks': []}
                with patch.object(CONTROLS, 'user_identity', return_value='17'):
                    if wrong_target or denial_code != 'cleanup_authorization_denied':
                        with self.assertRaises(CONTROLS.SuiteError):
                            CLEANUP.run_revocation(Client(), Client(True), 7, '23', Deployment(), report, lambda: None)
                    else:
                        CLEANUP.run_revocation(Client(), Client(True), 7, '23', Deployment(), report, lambda: None)
                        self.assertIn('queued_original_user_revoked_before_claim', report['checks'])
                if wrong_target: self.assertFalse(any(method == 'POST' for _, method, _ in calls))


if __name__ == '__main__': unittest.main()
