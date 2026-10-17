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
