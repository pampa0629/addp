import copy
import importlib
import json
import threading
import tempfile
import unittest
from contextlib import redirect_stdout
from http.server import BaseHTTPRequestHandler, HTTPServer
from io import StringIO
from pathlib import Path
from unittest.mock import patch

SUITE = importlib.import_module('scripts.test.graph-falkordb-protection-online')


class GraphProtectionOnlineTest(unittest.TestCase):
    def test_develop_browser_requires_its_own_five_permissions_and_both_protection_phases(self):
        with tempfile.TemporaryDirectory() as directory:
            artifacts = Path(directory)
            environment = {'ADDP_ONLINE_ARTIFACT_DIR': directory, 'ADDP_ONLINE_TEST_RUN_ID': 'develop-run',
                           'ADDP_ONLINE_GRAPH_DEVELOP_USERNAME': 'developer', 'ADDP_ONLINE_GRAPH_DEVELOP_PASSWORD': 'private-password',
                           'ADDP_ONLINE_GRAPH_DEVELOP_ACCESS_TOKEN': 'develop-secret', 'GATEWAY_URL': 'http://127.0.0.1:8000',
                           'ADDP_ONLINE_GRAPH_PREPARER_ACCESS_TOKEN': 'preparer-secret',
                           'ADDP_ONLINE_TEST_USER_PASSWORD': 'graph-password'}
            def launch(command, **kwargs):
                self.assertIn('e2e/online/develop-falkordb-query.spec.js', command)
                self.assertFalse(any(key.endswith('_ACCESS_TOKEN') for key in kwargs['env']))
                self.assertNotIn('ADDP_ONLINE_TEST_USER_PASSWORD', kwargs['env'])
                self.assertEqual(kwargs['env']['ADDP_ONLINE_GRAPH_DEVELOP_USERNAME'], 'developer')
                expected = json.loads(kwargs['env']['ADDP_ONLINE_DEVELOP_FALKORDB_EXPECTATIONS'])
                self.assertEqual(expected, dict(run_id='develop-run', tenant_id='2', principal_id='5', engine_id=13, phase=phase))
                payload = dict(expected, schema_version='addp.develop-falkordb-query-browser/v1', result='passed',
                               permissions=sorted(SUITE.DEVELOP_PERMISSIONS), graph_nodes=3, graph_relationships=2, graph_rows=2,
                               graph_canvas=True, browser_errors=0, failed_business_responses=0, unexpected_writes=0,
                               anonymous_refresh_401=1, execution_ids=['71a09155-b1ec-4ae4-af2f-6689237a62f9', 'e2429cf6-a201-4ba2-8a0b-cfeff00365ca'])
                names = ('graph', 'table') if phase == 'unmanaged' else ('protected', 'control')
                if phase == 'unmanaged': payload.update(typed_parameters=True, table_rows=1, table_visible=True, graph_table_visible=True)
                else: payload.update(protection_required=True, protected_result_absent=True, protection_stage='preparation', control_readable=True)
                payload.update(mutation)
                Path(kwargs['env']['ADDP_ONLINE_DEVELOP_FALKORDB_REPORT']).write_text(json.dumps(payload))
                if screenshots:
                    for name in names: (artifacts / f'develop-falkordb-{name}-console.png').write_bytes(b'unit-test-screenshot')
                return type('Process', (), {'returncode': 0})()
            for phase in ('unmanaged', 'protected'):
                negative = {'typed_parameters': False} if phase == 'unmanaged' else {'protected_result_absent': False}
                for mutation, screenshots in (({}, True), ({'principal_id': '4'}, True),
                        ({'permissions': ['graph.graph.read']}, True), ({'browser_errors': 1}, True),
                        (negative, True), ({'execution_ids': ['same', 'same']}, True), ({}, False)):
                    report = {'identities': {'reader': {'principal_id': '4'}}}
                    for path in artifacts.glob('*.png'): path.unlink()
                    with self.subTest(phase=phase, mutation=mutation, screenshots=screenshots), patch.dict(SUITE.os.environ, environment), \
                         patch.object(SUITE, 'identity', return_value={'principal_id': '5', 'permissions': sorted(SUITE.DEVELOP_PERMISSIONS), 'administrator': False}) as identity, \
                         patch.object(SUITE.subprocess, 'run', side_effect=launch):
                        if mutation or not screenshots:
                            with self.assertRaises(SUITE.SuiteError): SUITE.run_develop_browser(report, 13, 2, phase)
                        else:
                            self.assertEqual(SUITE.run_develop_browser(report, 13, 2, phase)['principal_id'], '5')
                            self.assertEqual(identity.call_args.args[1:], (2, SUITE.DEVELOP_PERMISSIONS))

    def test_develop_does_not_reuse_the_graph_reader_or_change_between_phases(self):
        environment = {'GATEWAY_URL': 'http://127.0.0.1:8000', 'ADDP_ONLINE_GRAPH_DEVELOP_ACCESS_TOKEN': 'develop-secret'}
        for previous, current in ((None, '4'), ('5', '6')):
            report = {'identities': {'reader': {'principal_id': '4'}}}
            if previous: report['identities']['develop'] = {'principal_id': previous}
            with self.subTest(previous=previous, current=current), patch.dict(SUITE.os.environ, environment), \
                 patch.object(SUITE, 'identity', return_value={'principal_id': current}), patch.object(SUITE.subprocess, 'run') as launch:
                with self.assertRaises(SUITE.SuiteError): SUITE.run_develop_browser(report, 13, 2, 'protected')
                launch.assert_not_called()

    def test_browser_requires_same_reader_real_ui_evidence_and_clean_diagnostics(self):
        api_report = {'identities': {'reader': {'principal_id': '4'}}}
        with tempfile.TemporaryDirectory() as directory:
            artifacts = Path(directory)
            environment = {'ADDP_ONLINE_ARTIFACT_DIR': directory, 'ADDP_ONLINE_TEST_RUN_ID': 'graph-run',
                           'ADDP_ONLINE_TEST_USER_USERNAME': 'reader', 'ADDP_ONLINE_TEST_USER_PASSWORD': 'private-password',
                           'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'reader-secret',
                           'ADDP_ONLINE_GRAPH_PREPARER_ACCESS_TOKEN': 'preparer-secret',
                           'ADDP_ONLINE_GRAPH_OUTSIDER_ACCESS_TOKEN': 'outsider-secret'}
            def launch(command, **kwargs):
                self.assertIn('e2e/online/graph-falkordb-protection.spec.js', command)
                self.assertEqual(kwargs['cwd'].name, 'frontend')
                self.assertFalse(any(key.endswith('_ACCESS_TOKEN') for key in kwargs['env']))
                expected = json.loads(kwargs['env']['ADDP_ONLINE_GRAPH_BROWSER_EXPECTATIONS'])
                self.assertEqual(expected, {'run_id': 'graph-run', 'tenant_id': '2', 'principal_id': '4', 'engine_id': 13, 'graph_id': 17})
                payload = dict(expected, schema_version='addp.graph-falkordb-protection-browser/v1', result='passed',
                               permissions=['graph.graph.read'], node_count=3, relationship_count=2,
                               browse_snapshot=True, canvas_visible=True, native_search=True, knowledge_search=True,
                               public_switch_disabled=True, graph_writes=0, failed_business_responses=0,
                               browser_errors=0, anonymous_refresh_401=1)
                payload.update(mutation)
                Path(kwargs['env']['ADDP_ONLINE_GRAPH_BROWSER_REPORT']).write_text(json.dumps(payload))
                if screenshots:
                    for name in ('graph-private-browse-console.png', 'graph-private-knowledge-console.png'):
                        (artifacts / name).write_bytes(b'unit-test-screenshot')
                return type('Process', (), {'returncode': 0})()
            for mutation, screenshots in (({}, True), ({'principal_id': 'other'}, True),
                    ({'permissions': ['graph.graph.read', 'graph.graph.update']}, True),
                    ({'public_switch_disabled': False}, True), ({'browser_errors': 1}, True), ({}, False)):
                for path in artifacts.glob('*.png'): path.unlink()
                with self.subTest(mutation=mutation, screenshots=screenshots), patch.dict(SUITE.os.environ, environment), \
                     patch.object(SUITE.subprocess, 'run', side_effect=launch):
                    if mutation or not screenshots:
                        with self.assertRaises(SUITE.SuiteError): SUITE.run_browser(api_report, 17, 13, 2)
                    else:
                        self.assertEqual(SUITE.run_browser(api_report, 17, 13, 2)['principal_id'], '4')

    def test_main_emits_the_standard_dispatcher_report_without_credentials(self):
        dispatcher = importlib.import_module('scripts.test.online-gate')
        with tempfile.TemporaryDirectory() as directory:
            environment = {'GITHUB_ACTIONS': 'true', 'ADDP_ONLINE_HOSTED': '1',
                           'ADDP_ONLINE_ARTIFACT_DIR': directory, 'GATEWAY_URL': 'http://127.0.0.1:8000',
                           'GRAPH_URL': 'http://127.0.0.1:8186', 'ADDP_ONLINE_TEST_ENGINE_ID': '13',
                           'ADDP_ONLINE_TEST_TENANT_ID': '2', 'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'reader-secret',
                           'ADDP_ONLINE_GRAPH_OUTSIDER_ACCESS_TOKEN': 'outsider-secret',
                           'ADDP_ONLINE_GRAPH_PREPARER_ACCESS_TOKEN': 'preparer-secret'}
            for failure in (False, True):
                stdout = StringIO()
                with self.subTest(failure=failure), patch.dict(SUITE.os.environ, environment), \
                     patch.object(SUITE, 'run', side_effect=SUITE.SuiteError('expected failure') if failure else None), redirect_stdout(stdout):
                    self.assertEqual(SUITE.main(), 1 if failure else 0)
                report = dispatcher._parse_stage_report('scenario', stdout.getvalue())
                self.assertEqual(report['passed'], not failure)
                self.assertEqual(report, json.loads((Path(directory) / 'graph-falkordb-protection.json').read_text()))
                for secret in ('reader-secret', 'outsider-secret', 'preparer-secret'):
                    self.assertNotIn(secret, stdout.getvalue())

    def test_browser_failure_still_closes_and_deletes_both_synthetic_graphs(self):
        deleted, closed = [], []
        class Client:
            next_graph = 0
            def request(self, method, path, expected, body=None):
                if method == 'POST' and path.endswith('/graphs'):
                    self.next_graph += 1
                    return SUITE.SUPPORT.Response(201, {'id': self.next_graph, 'is_public': False})
                if method == 'GET' and path.endswith('/entities/Person'):
                    return SUITE.SUPPORT.Response(200, {'data': [
                        {'id': str(i), 'properties': {'id': 'p' + str(i + 1)}} for i in range(3)]})
                if method == 'DELETE': deleted.append(path)
                return SUITE.SUPPORT.Response(expected[0], {})
        report = {}
        identities = [{'principal_id': str(i)} for i in range(3)]
        with patch.object(SUITE, 'identity', side_effect=identities), patch.object(SUITE, 'create_ontology', return_value=9), \
             patch.object(SUITE, 'create_schema'), patch.object(SUITE, 'probe'), \
             patch.object(SUITE, 'set_public', side_effect=lambda client, gid, public: closed.append((gid, public))), \
             patch.object(SUITE, 'run_browser', side_effect=SUITE.SuiteError('browser failed')):
            with self.assertRaisesRegex(SUITE.SuiteError, 'browser failed'):
                SUITE.run(*([Client()] * 7), 13, 2, 0, report)
        self.assertEqual(closed, [(1, False), (2, False)])
        self.assertEqual(deleted, ['/api/v1/graph/graphs/1', '/api/v1/graph/graphs/2', '/api/v1/graph/ontologies/9'])
        self.assertEqual(report['cleanup']['domain_resources_remaining'], 0)

    def test_develop_browser_failure_still_closes_and_deletes_all_graph_domain_resources(self):
        deleted, closed = [], []
        class Client:
            next_graph = 0
            def request(self, method, path, expected, body=None):
                if method == 'POST' and path.endswith('/graphs'):
                    self.next_graph += 1
                    return SUITE.SUPPORT.Response(201, {'id': self.next_graph, 'is_public': False})
                if method == 'GET' and path.endswith('/entities/Person'):
                    return SUITE.SUPPORT.Response(200, {'data': [
                        {'id': str(i), 'properties': {'id': 'p' + str(i + 1)}} for i in range(3)]})
                if method == 'DELETE': deleted.append(path)
                return SUITE.SUPPORT.Response(expected[0], {})
        report = {}
        with patch.object(SUITE, 'identity', side_effect=[{'principal_id': str(i)} for i in range(3)]), \
             patch.object(SUITE, 'create_ontology', return_value=9), patch.object(SUITE, 'create_schema'), \
             patch.object(SUITE, 'probe'), patch.object(SUITE.SUPPORT, 'wait_for_scan'), \
             patch.object(SUITE, 'set_public', side_effect=lambda client, gid, public: closed.append((gid, public))), \
             patch.object(SUITE, 'run_browser', return_value={}), \
             patch.object(SUITE, 'run_develop_browser', side_effect=SUITE.SuiteError('Develop browser failed')):
            with self.assertRaisesRegex(SUITE.SuiteError, 'Develop browser failed'):
                SUITE.run(*([Client()] * 7), 13, 2, 0, report)
        self.assertEqual(closed, [(1, False), (2, False)])
        self.assertEqual(deleted, ['/api/v1/graph/graphs/1', '/api/v1/graph/graphs/2', '/api/v1/graph/ontologies/9'])
        self.assertEqual(report['cleanup']['domain_resources_remaining'], 0)

    def progress(self):
        return {'state': 'enrolling', 'owner_progress': [
            {'consumer_owner': owner, 'projection_id': owner, 'revision': '00000000000000000001',
             'published_cursor': owner + '-cursor', 'acknowledged': True, 'validity': 'valid'}
            for owner in sorted(SUITE.OWNERS)]}

    def test_five_owner_acknowledgements_are_mandatory(self):
        self.assertTrue(SUITE.converged(self.progress()))
        for owner in SUITE.OWNERS:
            value = self.progress()
            next(p for p in value['owner_progress'] if p['consumer_owner'] == owner)['acknowledged'] = False
            self.assertFalse(SUITE.converged(value))
        for change in ('missing', 'duplicate', 'expired', 'released'):
            value = self.progress()
            if change == 'missing': value['owner_progress'].pop()
            if change == 'duplicate': value['owner_progress'][-1] = value['owner_progress'][0]
            if change == 'expired': value['owner_progress'][0]['validity'] = 'expired'
            if change == 'released': value['state'] = 'released'
            if change in {'expired'}:
                self.assertFalse(SUITE.converged(value))
            else:
                with self.assertRaises(SUITE.SuiteError): SUITE.converged(value)

    def test_activating_waits_for_the_formal_security_installation_barrier(self):
        activating = self.progress(); activating['state'] = 'activating'
        self.assertFalse(SUITE.converged(activating))
        class Client:
            def __init__(self): self.responses = iter([activating, activating, self_ready])
            def request(self, *args): return SUITE.SUPPORT.Response(200, next(self.responses))
        self_ready = self.progress()
        with patch.object(SUITE.time, 'monotonic', return_value=0), patch.object(SUITE.time, 'sleep') as sleep:
            self.assertEqual(SUITE.wait_owners(Client(), 'enrollment', 10), self_ready)
        self.assertEqual(sleep.call_count, 2)

    def test_cleanup_error_never_hides_the_original_operation_failure(self):
        class Client:
            def request(self, method, path, expected, body=None):
                if method == 'POST': return SUITE.SUPPORT.Response(201, {'id': 9})
                raise SUITE.SuiteError('ontology cleanup failed')
        report = {}
        with patch.object(SUITE, 'identity', side_effect=[{'principal_id': str(i)} for i in range(3)]), \
             patch.object(SUITE, 'create_schema', side_effect=SUITE.SuiteError('initial operation failed')):
            with self.assertRaisesRegex(SUITE.SuiteError, 'initial operation failed'):
                SUITE.run(*([Client()] * 7), 13, 2, 0, report)
        self.assertEqual(report['failure'], {'error_type': 'SuiteError', 'message': 'initial operation failed'})
        self.assertEqual(report['cleanup']['errors'], ['delete synthetic ontology'])
        self.assertIsNone(report['cleanup']['domain_resources_remaining'])

    def test_release_requires_all_owners_and_released_validity(self):
        value = self.progress(); value['state'] = 'released'
        self.assertFalse(SUITE.converged(value, True))
        for p in value['owner_progress']: p['validity'] = 'released'
        self.assertTrue(SUITE.converged(value, True))
        value['owner_progress'][0]['acknowledged'] = False
        self.assertFalse(SUITE.converged(value, True))

    def test_exact_minimum_user_permissions_reject_admin_or_extra_authority(self):
        context = {'principal': {'id': '4', 'type': 'user'}, 'context': {'type': 'tenant', 'tenant_id': '2'},
                   'token': {'type': 'first_party_access_token'}, 'authorization': {'role_assignments': [
                       {'role_key': 'online.graph_reader', 'permissions': ['graph.graph.read']}]}}
        class Client:
            def request(self, *args): return SUITE.SUPPORT.Response(200, context)
        self.assertFalse(SUITE.identity(Client(), 2, SUITE.READER_PERMISSIONS)['administrator'])
        for mutation in ('extra', 'admin', 'tenant', 'service'):
            saved = copy.deepcopy(context)
            if mutation == 'extra': context['authorization']['role_assignments'][0]['permissions'].append('graph.graph.update')
            if mutation == 'admin': context['authorization']['role_assignments'][0]['role_key'] = 'tenant.administrator'
            if mutation == 'tenant': context['context']['tenant_id'] = '1'
            if mutation == 'service': context['principal']['type'] = 'service'
            with self.assertRaises(SUITE.SuiteError): SUITE.identity(Client(), 2, SUITE.READER_PERMISSIONS)
            context.clear(); context.update(saved)

    def test_anonymous_transport_does_not_send_empty_bearer(self):
        observed = []
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                observed.append(self.headers.get('Authorization'))
                self.send_response(200); self.end_headers(); self.wfile.write(b'{}')
            def log_message(self, *args): pass
        server = HTTPServer(('127.0.0.1', 0), Handler)
        self.addCleanup(server.server_close)
        worker = threading.Thread(target=server.serve_forever); worker.start()
        try:
            for token in ('', 'fixture-token'):
                SUITE.Client(f'http://127.0.0.1:{server.server_port}', token, 5).request('GET', '/', (200,))
        finally:
            server.shutdown(); worker.join()
        self.assertEqual(observed, [None, 'Bearer fixture-token'])

    def test_denial_must_identify_security_and_never_include_data(self):
        ids = {'p1': '0', 'p3': '2'}
        class Client:
            payload = {'error': 'data protection required'}
            def request(self, *args): return SUITE.SUPPORT.Response(403, self.payload)
        client = Client()
        self.assertEqual(len(SUITE.probe(client, 1, ids, 403, 'protection')), 8)
        for payload in ({'error': 'permission denied'}, {'error': 'data protection required', 'data': []},
                        {'error': 'data protection required', 'nodes': []}):
            client.payload = payload
            with self.assertRaises(SUITE.SuiteError): SUITE.probe(client, 1, ids, 403, 'protection')

    def test_successful_status_does_not_hide_empty_or_wrong_native_data(self):
        class Client:
            payload = {}
            def request(self, *args): return SUITE.SUPPORT.Response(200, self.payload)
        client = Client()
        for payload in ({}, {'data': [], 'total': 0}, {'data': [1, 2], 'total': 3}):
            client.payload = payload
            with self.assertRaises(SUITE.SuiteError): SUITE.probe(client, 1, {'p1': '0', 'p3': '2'}, 200)

    def test_cleanup_still_deletes_ontology_after_schema_preparation_failure(self):
        from unittest.mock import patch
        deleted = []
        class Client:
            def request(self, method, path, expected, body=None):
                if method == 'POST' and path.endswith('/ontologies'): return SUITE.SUPPORT.Response(201, {'id': 9})
                if method == 'POST': raise SUITE.SuiteError('schema failed')
                if method == 'DELETE': deleted.append(path); return SUITE.SUPPORT.Response(200, {})
                return SUITE.SUPPORT.Response(404, {})
        report = {}
        identities = [{'principal_id': str(i)} for i in range(3)]
        with patch.object(SUITE, 'identity', side_effect=identities):
            with self.assertRaisesRegex(SUITE.SuiteError, 'schema failed'):
                SUITE.run(*([Client()] * 7), 13, 2, 0, report)
        self.assertEqual(deleted, ['/api/v1/graph/ontologies/9'])
        self.assertEqual(report['cleanup']['domain_resources_remaining'], 0)


if __name__ == '__main__': unittest.main()
