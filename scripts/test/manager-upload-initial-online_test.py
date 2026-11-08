import email.parser
import hashlib
import importlib
import json
from pathlib import Path
import tempfile
import types
import unittest
from unittest import mock
from urllib.parse import parse_qs, urlsplit
import uuid

SCENE = importlib.import_module('scripts.test.manager-upload-initial-online')
API = SCENE.API


class FakeDeployment:
    """Exercise acceptance orchestration/cleanup; not a cross-service pass."""
    def __init__(self, root, fault=None):
        self.root, self.fault = root, fault
        self.grants, self.uploads, self.outcomes, self.replays = [], {}, {}, 0
        self.posts = 0
        self.known = set()
        self.item_ids = {}
        self.downloads = []

    def fingerprint(self, name):
        return hashlib.sha256(f'9:upload-initial/{name}'.encode()).hexdigest()

    def client(self, base, token, timeout):
        return types.SimpleNamespace(token=token, request=lambda *args, **kw: self.request(token, *args, **kw))

    def upload_scans(self, upload_ids):
        if self.fault == 'duplicate_upload_scan':
            raise API.SuiteError('upload child scan duplicated')
        return {value: str(uuid.uuid5(uuid.UUID(value), 'scan')) for value in upload_ids}

    def request(self, token, method, path, expected, body=None, **kw):
        url = urlsplit(path); query = parse_qs(url.query)
        data, status = {}, 200
        if url.path.endswith('/access_grants/history'):
            data = {'data': json.loads(json.dumps(self.grants)), 'total': len(self.grants)}
        elif url.path.endswith('/access_grants'):
            rows = [g for g in self.grants if g['revocation'] is None]
            data = {'data': rows, 'total': len(rows)}
        elif url.path.endswith('/revoke'):
            g, = [g for g in self.grants if g['request_id'] == url.path.split('/')[-2]]
            g['revocation'] = {'reason': 'withdrawn'}
        elif method == 'POST' and url.path == '/api/v1/manager/uploads':
            self.posts += 1; status = 201; upload_id = str(uuid.uuid4())
            message = email.parser.BytesParser().parsebytes(
                ('Content-Type: ' + kw['content_type'] + '\r\nMIME-Version: 1.0\r\n\r\n').encode() + kw['encoded_body'])
            outcomes, failed, files = [], [], []
            for part in message.walk():
                name = part.get_filename()
                if not name:
                    continue
                target = self.root / name
                if target.is_dir():
                    failed.append(name); continue
                exists = target.exists(); target.write_bytes(part.get_payload(decode=True))
                fp = self.fingerprint(name)
                if exists or name in self.known:
                    outcome = {'item_fingerprint': fp, 'outcome': 'denied', 'reason': 'not_new'}
                else:
                    action = 'metadata_read' if name.endswith('.csv') else 'read'
                    outcome = {'item_fingerprint': fp, 'outcome': 'issued', 'request_id': str(uuid.uuid4()), 'action': action}
                    self.grants.append({**outcome, 'approval_mode': 'upload_creation', 'revocation': None, 'recipient_type': 'user', 'recipient_id': '31', 'expiry_mode': 'until_revoked', 'expires_at': None, 'upload_id': upload_id, 'boundary_hash': 'a'*64})
                self.known.add(name)
                self.item_ids.setdefault(name, len(self.item_ids) + 101)
                outcome.update(tenant_id='7', upload_id=upload_id)
                outcomes.append(outcome); files.append({'file_name': name})
                self.outcomes[(upload_id, fp)] = outcome
            self.uploads[upload_id] = {'upload_id': upload_id, 'initial_status': 'settled',
                'status': 'failed' if failed else 'success', 'outcomes': outcomes}
            data = {'upload_id': upload_id, 'failed_files': failed, 'files': files}
            if self.fault == 'lost_upload_response':
                raise API.SuiteError('lost response after committed writes and Grants')
        elif url.path.startswith('/api/v1/manager/uploads/'):
            if token == 'outsider': status = 404
            else: data = self.uploads[url.path.split('/')[-1]]
        elif url.path.startswith('/api/v1/meta/items/by-catalog-path'):
            name = query['catalog_path'][0].split('/')[-1]
            readable = token != 'outsider' and any(g['item_fingerprint'] == self.fingerprint(name) and g['revocation'] is None for g in self.grants)
            status = 200 if readable or (token == 'runtime' and name in self.item_ids) else 404
            data = {'id': self.item_ids[name], 'tenant_id': 7, 'engine_id': 9,
                    'item_type': 'file', 'full_name': 'upload-initial/' + name,
                    'fingerprint': self.fingerprint(name)} if status == 200 else {}
            if self.fault == 'wrong_item_identity' and token == 'runtime': data['engine_id'] = 10
        elif url.path == '/api/v1/manager/downloads/file':
            locator = urlsplit(query['locator'][0])
            name = locator.path.split('/')[-1]
            identity = parse_qs(locator.query)
            if identity.get('item_id') != [str(self.item_ids[name])] or identity.get('type') != ['file']:
                raise API.SuiteError('download locator must bind the real scanned item_id')
            self.downloads.append((token, name, self.item_ids[name]))
            readable = token != 'outsider' and any(g['item_fingerprint'] == self.fingerprint(name) and g['revocation'] is None and g['action'] == 'read' for g in self.grants)
            status = 200 if readable else 403
            data = (self.root / name).read_bytes() if readable else {'error_code': 'permission_denied'}
            if self.fault == 'corrupt_download' and readable: data = b'corrupted'
        elif url.path.startswith('/api/v1/system/runtime/initial-upload-grants/'):
            self.replays += 1
            data = self.outcomes[(url.path.split('/')[-3], url.path.split('/')[-1])]
            if self.fault == 'replay_restores_grant':
                self.grants[0]['revocation'] = None
        else:
            raise AssertionError((method, path))
        if status not in expected:
            raise API.SuiteError('unexpected status in deterministic fixture')
        return types.SimpleNamespace(status=status, payload=data)


class UploadAcceptanceTest(unittest.TestCase):
    def exercise(self, fault=None):
        with tempfile.TemporaryDirectory() as temp:
            base = Path(temp); repo = base / 'repository'; repo.mkdir()
            root = base / 'root'; root.mkdir()
            (root / 'existing.obj').write_bytes(SCENE.MODEL)
            (root / 'blocked.obj').mkdir()
            server = FakeDeployment(root, fault)
            env = {'GATEWAY_URL': 'http://127.0.0.1:8000', 'ADDP_ONLINE_MANAGER_NFS_ENGINE_ID': '9',
                   'ADDP_ONLINE_TEST_TENANT_ID': '7', 'ADDP_ONLINE_TEST_RUN_ID': 'fixture',
                   'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'user', 'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN': 'issuer',
                   'ADDP_ONLINE_FIXTURE_OUTSIDER_ACCESS_TOKEN': 'outsider', 'ADDP_ONLINE_ARTIFACT_DIR': str(base)}
            with mock.patch.object(SCENE, 'owned_directory', return_value=root), \
                 mock.patch.object(API, 'GatewayClient', side_effect=server.client), \
                 mock.patch.object(SCENE, 'runtime_client', return_value=server.client('', 'runtime', 30)), \
                 mock.patch.object(API, 'wait_manager_read_boundary'), \
                 mock.patch.object(SCENE.REFRESH, 'PersistedScanObservation', return_value=server), \
                 mock.patch.object(SCENE.FILES, 'exact_user_principal', side_effect=lambda c, *_: '31' if c.token == 'user' else '32'):
                if fault:
                    with self.assertRaises(API.SuiteError): SCENE.run(repo, env, None)
                else:
                    report = SCENE.run(repo, env, None)
                    self.assertEqual(set(report['checks']), SCENE.REQUIRED_CHECKS)
                    self.assertEqual(server.posts, 5)
                    self.assertEqual(server.replays, 1)
                    self.assertTrue(server.downloads)
                    self.assertTrue(all(token in {'user', 'outsider'} for token, _, _ in server.downloads))
                    self.assertTrue(all(row.get('scan_execution_id') for row in report['uploads']))
            report = json.loads((base / 'manager-upload-initial.json').read_text())
            self.assertTrue(all(g['revocation'] for g in server.grants))
            self.assertFalse(root.exists())
            self.assertEqual(report['cleanup'], 'failed' if fault == 'lost_upload_response' else 'zero_current_source_rules_and_files')
            self.assertEqual(report['result'], 'failed' if fault else 'passed')
            self.assertNotIn('user', json.dumps(report.get('failure', {})))
            return report

    def test_complete_upload_overwrite_withdrawal_replay_and_partial_failure_flow(self):
        self.exercise()

    def test_failed_real_byte_assertion_still_withdraws_owned_grants_and_removes_files(self):
        self.exercise('corrupt_download')

    def test_unknown_upload_response_cleans_observed_resources_but_does_not_claim_no_late_grant(self):
        self.exercise('lost_upload_response')

    def test_replayed_revoked_grant_is_detected_and_cleanup_retires_it(self):
        self.exercise('replay_restores_grant')

    def test_download_refuses_a_meta_identity_outside_the_fixture_engine(self):
        self.exercise('wrong_item_identity')

    def test_duplicate_child_scan_fails_and_still_cleans_owned_resources(self):
        self.exercise('duplicate_upload_scan')

    def test_outcome_validator_rejects_missing_duplicated_or_wrong_action(self):
        for rows in ([], [{'item_fingerprint': 'fp', 'outcome': 'issued', 'action': 'metadata_read', 'request_id': str(uuid.uuid4())}]):
            with self.assertRaises(API.SuiteError): SCENE.verify_outcome(rows, 'fp', action='read')
        row = {'item_fingerprint': 'fp', 'outcome': 'denied', 'reason': 'not_new'}
        with self.assertRaises(API.SuiteError): SCENE.verify_outcome([row, row], 'fp', reason='not_new')

    def test_wait_refuses_explicit_unknown_state_without_retry(self):
        client = mock.Mock(); client.request.return_value.payload = {'upload_id': 'id', 'initial_status': 'unknown'}
        with self.assertRaises(API.SuiteError): SCENE.wait_upload(client, 'id', SCENE.time.monotonic()+1)
        self.assertEqual(client.request.call_count, 1)

    def test_runtime_replay_uses_existing_manager_tenant_service_identity(self):
        identity = {'principal': {'type': 'service_principal'}, 'token': {'type': 'service_access_token'},
                    'context': {'type': 'tenant', 'tenant_id': '7'}, 'client': {'client_id': 'addp-manager'}}
        import copy
        for mutation in (None, 'principal', 'tenant', 'client', 'token'):
            value = copy.deepcopy(identity)
            if mutation == 'principal': value['principal']['type'] = 'user'
            if mutation == 'tenant': value['context']['tenant_id'] = '8'
            if mutation == 'client': value['client']['client_id'] = 'addp-meta'
            if mutation == 'token': value['token']['type'] = 'first_party_access_token'
            oauth, runtime = mock.Mock(), mock.Mock()
            oauth.request.return_value.payload = {'access_token': 'runtime'}
            runtime.request.return_value.payload = value
            with mock.patch.object(API, 'GatewayClient', side_effect=[oauth, runtime]):
                if mutation:
                    with self.assertRaises(API.SuiteError): SCENE.runtime_client({'GATEWAY_URL': 'http://localhost', 'MANAGER_SERVICE_CLIENT_SECRET': 'private'}, 7)
                else:
                    self.assertIs(SCENE.runtime_client({'GATEWAY_URL': 'http://localhost', 'MANAGER_SERVICE_CLIENT_SECRET': 'private'}, 7), runtime)
                    form = parse_qs(oauth.request.call_args.kwargs['encoded_body'].decode())
                    self.assertEqual(form, {'grant_type': ['client_credentials'], 'scope': ['addp.api'], 'audience': ['addp.api'], 'tenant_id': ['7']})

    def test_shared_transport_preserves_encoded_post_identity_and_binary_response(self):
        import io
        response = mock.MagicMock(); response.status = 200
        response.read.return_value = b'\x00zip\xff'
        response.__enter__.return_value = response
        with mock.patch.object(API.urllib.request, 'urlopen', return_value=response) as send:
            client = API.GatewayClient('http://localhost', 'original-user', 3)
            result = client.request('POST', '/upload', (200,), encoded_body=b'exact-body',
                content_type='multipart/form-data; boundary=x', raw_response=True)
            self.assertEqual(result.payload, b'\x00zip\xff')
            request = send.call_args.args[0]
            self.assertEqual(request.data, b'exact-body')
            self.assertEqual(request.get_header('Authorization'), 'Bearer original-user')
            self.assertEqual(send.call_count, 1)
        with self.assertRaises(API.SuiteError):
            client.request('POST', '/upload', (200,), {}, encoded_body=b'x')

    def test_shared_transport_does_not_turn_error_json_into_download_bytes(self):
        response = mock.MagicMock(); response.status = 403
        response.read.return_value = b'{"error_code":"permission_denied"}'
        response.__enter__.return_value = response
        with mock.patch.object(API.urllib.request, 'urlopen', return_value=response):
            result = API.GatewayClient('http://localhost', 'user', 3).request('GET', '/download', iter((403,)), raw_response=True)
        self.assertEqual(result.payload, {'error_code': 'permission_denied'})
