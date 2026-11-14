import importlib
from contextlib import contextmanager
from copy import deepcopy
import json
import hashlib
import io
import os
from pathlib import Path
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from types import SimpleNamespace
import unittest
import zipfile
from unittest.mock import Mock, patch
from urllib.parse import unquote

M = importlib.import_module('scripts.test.manager-filegdb-online')
P = importlib.import_module('scripts.test.manager-filegdb-physical')
OWNER = importlib.import_module('scripts.test.manager-internal-artifact-lineage-online')


def facts():
    return dict(schema_version='meta.data_item_authorization_facts/v1', engine_id='7',
        item_fingerprint='a' * 64, item_type='file', full_name=M.NAME, basis_state='complete',
        observed_at='2026-10-30T00:00:00Z',
        boundary_hash='b' * 64, boundary={'layout': 'whole', 'scope': {
            'format': 'filegdb', 'rule_version': 1, 'root': M.target(7)}})


def authorization():
    auth = object.__new__(M.Authorization)
    auth.engine_id = 7; auth.base = '/api/v1/system/engines/7'
    auth.target = lambda _: M.target(7)
    auth.operator = '9'; auth.recipient = '11'; auth.frozen = None
    auth.grants = []; auth.confirmed = set(); auth.denies = []; auth.confirmed_denies = set()
    auth.node_receipts = {}; auth.exact_records = {}
    auth.issuer = Mock()
    return auth


class FileGDBAcceptanceTest(unittest.TestCase):
    def browser_proof(self, directory, phase='granted'):
        context = {'run_id': 'manager-1-1', 'phase': phase, 'locator': 'exact-locator',
            'status': 200 if phase == 'granted' else 403,
            'identity': {'principal_id': '11', 'tenant_id': '2', 'permissions_verified': sorted(M.PERMISSIONS)},
            'source': {'a.gdbtable': hashlib.sha256(b'original').hexdigest()},
            'screenshot': phase + '.png', 'download_file': phase + '.zip'}
        payload = {**context, 'schema_version': 'addp.manager-filegdb-browser/v1', 'result': 'passed',
            'actual_button_clicked': True, 'current_user_bearer': True, 'cookie_absent': True,
            'credential_in_url': False, 'business_writes': [], 'unexpected_responses': [], 'warnings': [],
            'page_errors': [], 'anonymous_refresh_401': 1, 'download_requests': 1,
            'download_events': int(phase == 'granted')}
        (directory / context['screenshot']).write_bytes(b'screenshot-fixture')
        if phase == 'granted':
            payload['child_preview'] = {'requests': [dict(child=child, locator=context['locator'], status=200,
                current_user_bearer=True, cookie_absent=True, credential_in_url=False)
                for child in ('initial_points', 'added_points')]}
            for key, child, count, previous in (('initial', 'initial_points', 2, []),
                    ('added', 'added_points', 1, ['initial_points_0', 'initial_points_1'])):
                screenshot = 'manager-filegdb-preview-' + key + '.png'
                (directory / screenshot).write_bytes(b'screenshot-fixture')
                payload['child_preview'][key] = dict(child=child, current_rows=count, total_rows=count,
                    rows=[dict(label=child+'_'+str(i), value=str(i+1)) for i in range(count)],
                    previous_labels_absent=previous, screenshot=screenshot)
            first = [dict(label='paging_points_' + str(i), value=str(i + 1)) for i in range(20)]
            last = [dict(label='paging_points_' + str(i), value=str(i + 1)) for i in range(20, 23)]
            payload['child_pagination'] = {'states': [], 'requests': []}
            for key, child, rows, previous, total, page in (
                    ('first', 'paging_points', first, ['added_points_0'], 23, 1),
                    ('next', 'paging_points', last, [r['label'] for r in first], 23, 2),
                    ('previous', 'paging_points', first, [r['label'] for r in last], 23, 1),
                    ('next-again', 'paging_points', last, [r['label'] for r in first], 23, 2),
                    ('switch-small', 'added_points', [dict(label='added_points_0', value='1')], [r['label'] for r in last], 1, 1),
                    ('switch-back', 'paging_points', first, ['added_points_0'], 23, 1)):
                screenshot = 'manager-filegdb-pagination-' + key + '.png'
                (directory / screenshot).write_bytes(b'screenshot-fixture')
                payload['child_pagination']['states'].append(dict(key=key, child=child, rows=rows,
                    current_rows=len(rows), total_rows=total, previous_labels_absent=previous,
                    page=page, page_size=20, previous_disabled=page == 1, next_disabled=page == (total + 19) // 20,
                    screenshot=screenshot))
                payload['child_pagination']['requests'].append(dict(child=child, page=page, page_size=20,
                    locator=context['locator'], status=200, current_user_bearer=True, cookie_absent=True, credential_in_url=False))
            raw = io.BytesIO()
            with zipfile.ZipFile(raw, 'w') as archive:
                archive.writestr(M.NAME + '/', b''); archive.writestr(M.NAME + '/a.gdbtable', b'original')
            (directory / context['download_file']).write_bytes(raw.getvalue())
            (directory / (context['download_file'] + '.response')).write_bytes(raw.getvalue())
            payload.update(suggested_filename=M.NAME+'.zip', size=len(raw.getvalue()),
                           sha256=hashlib.sha256(raw.getvalue()).hexdigest(),
                           response_capture='same_response_clone', response_file=context['download_file']+'.response',
                           response_size=len(raw.getvalue()), response_sha256=hashlib.sha256(raw.getvalue()).hexdigest())
        else:
            payload.update(error_visible=True, success_visible=False)
        return context, payload

    def test_browser_proof_binds_current_user_phase_physical_bytes_and_denials(self):
        for phase in ('ungranted', 'granted', 'revoked', 'expired'):
            with self.subTest(phase=phase), tempfile.TemporaryDirectory() as name:
                root = Path(name); context, payload = self.browser_proof(root, phase)
                self.assertEqual(M.validate_browser_report(payload, context, root)['result'], 'passed')
                changes = [lambda p: p.update(identity={}), lambda p: p.update(locator='other'),
                    lambda p: p.update(current_user_bearer=False), lambda p: p.update(cookie_absent=False),
                    lambda p: p.update(credential_in_url=True), lambda p: p.update(actual_button_clicked=False),
                    lambda p: p.update(business_writes=['grant']), lambda p: p.update(unexpected_responses=[503]),
                    lambda p: p.update(warnings=['error']), lambda p: p.update(page_errors=['crash']),
                    lambda p: p.update(anonymous_refresh_401=2), lambda p: p.update(result='failed'),
                    lambda p: p.pop('download_requests'), lambda p: p.update(download_requests=2)]
                changes += ([lambda p: p.update(sha256='wrong'), lambda p: p.update(size=0),
                             lambda p: p.update(response_capture='refetch'), lambda p: p.update(response_file='other'),
                             lambda p: p.update(response_size=0), lambda p: p.update(response_sha256='wrong'),
                             lambda p: p.update(download_events=0), lambda p: p.update(suggested_filename='other.zip')]
                            if phase == 'granted' else [lambda p: p.update(download_events=1),
                             lambda p: p.update(error_visible=False), lambda p: p.update(success_visible=True)])
                for change in changes:
                    wrong = deepcopy(payload); change(wrong)
                    with self.assertRaises(ValueError): M.validate_browser_report(wrong, context, root)
                if phase == 'granted':
                    # Rewriting the receipt's hash does not conceal changed source bytes.
                    raw = io.BytesIO()
                    with zipfile.ZipFile(raw, 'w') as archive:
                        archive.writestr(M.NAME+'/', b''); archive.writestr(M.NAME+'/a.gdbtable', b'changed')
                    (root/context['download_file']).write_bytes(raw.getvalue())
                    (root/(context['download_file']+'.response')).write_bytes(raw.getvalue())
                    payload.update(size=len(raw.getvalue()), sha256=hashlib.sha256(raw.getvalue()).hexdigest(),
                                   response_size=len(raw.getvalue()), response_sha256=hashlib.sha256(raw.getvalue()).hexdigest())
                else:
                    (root/context['download_file']).write_bytes(b'leaked')
                with self.assertRaises(ValueError): M.validate_browser_report(payload, context, root)

    def test_browser_requires_real_child_cells_switch_identity_and_screenshots(self):
        with tempfile.TemporaryDirectory() as name:
            root = Path(name); context, payload = self.browser_proof(root)
            mutations = [lambda p: p.pop('child_preview'),
                lambda p: p['child_preview']['initial'].update(current_rows=0, rows=[]),
                lambda p: p['child_preview']['initial']['rows'][0].update(value='2'),
                lambda p: p['child_preview']['added'].update(rows=p['child_preview']['initial']['rows']),
                lambda p: p['child_preview']['added'].update(previous_labels_absent=[]),
                lambda p: p['child_preview']['requests'].append(p['child_preview']['requests'][0]),
                lambda p: p['child_preview']['requests'][1].update(locator='other'),
                lambda p: p['child_preview']['requests'][1].update(current_user_bearer=False),
                lambda p: p['child_preview']['requests'][1].update(status=403)]
            for mutation in mutations:
                wrong = deepcopy(payload); mutation(wrong)
                with self.assertRaisesRegex(ValueError, 'FileGDB browser child'):
                    M.validate_browser_report(wrong, context, root)
            (root / payload['child_preview']['added']['screenshot']).unlink()
            with self.assertRaisesRegex(ValueError, 'child screenshot missing'):
                M.validate_browser_report(payload, context, root)

    def test_native_fixture_preserves_original_layers_and_adds_twenty_three_rows(self):
        created = {}
        dataset = Mock()
        dataset.GetLayerByName.return_value = None
        def layer(name, **_):
            created[name] = []
            result = Mock()
            result.CreateField.return_value = 0
            result.CreateFeature.side_effect = lambda feature: created[name].append(dict(feature.fields)) or 0
            return result
        dataset.CreateLayer.side_effect = layer
        ogr = Mock()
        ogr.GetDriverByName.return_value.CreateDataSource.return_value = dataset
        ogr.GetDriverByName.return_value.Open.return_value = dataset
        def feature(_):
            result = Mock(); result.fields = {}
            result.SetField.side_effect = result.fields.__setitem__
            return result
        ogr.Feature.side_effect = feature
        osr = Mock(); osr.SpatialReference.return_value.ImportFromEPSG.return_value = 0
        with tempfile.TemporaryDirectory() as name, patch.dict(os.environ, ADDP_ONLINE_TEST_RUN_ID='fixture-run'), \
                patch.dict('sys.modules', osgeo=SimpleNamespace(ogr=ogr, osr=osr)):
            root = Path(name)
            with patch.object(P, 'owned_root', return_value=(root, root)):
                P.native('seed', root); P.native('add', root)
        self.assertEqual(created, {child: [dict(label=child + '_' + str(i), value=i + 1) for i in range(count)]
                                  for child, count in [('initial_points', 2), ('added_points', 1), ('paging_points', 23)]})

    def test_browser_requires_pagination_cells_requests_reset_and_screenshots(self):
        with tempfile.TemporaryDirectory() as name:
            root = Path(name); context, payload = self.browser_proof(root)
            mutations = [lambda p: p.pop('child_pagination'),
                lambda p: p['child_pagination']['states'].pop(),
                lambda p: p['child_pagination']['states'][1].update(rows=p['child_pagination']['states'][0]['rows']),
                lambda p: p['child_pagination']['states'][1]['rows'][0].update(value='1'),
                lambda p: p['child_pagination']['states'][1].update(total_rows=3),
                lambda p: p['child_pagination']['states'][4].update(page=2),
                lambda p: p['child_pagination']['states'][5].update(page_size=10),
                lambda p: p['child_pagination']['states'][4].update(previous_labels_absent=[]),
                lambda p: p['child_pagination']['requests'].append(p['child_pagination']['requests'][0]),
                lambda p: p['child_pagination']['requests'][1].update(page=1),
                lambda p: p['child_pagination']['requests'][1].update(current_user_bearer=False),
                lambda p: p['child_pagination']['requests'][4].update(child='paging_points')]
            for mutation in mutations:
                wrong = deepcopy(payload); mutation(wrong)
                with self.assertRaisesRegex(ValueError, 'FileGDB browser pagination'):
                    M.validate_browser_report(wrong, context, root)
            (root / payload['child_pagination']['states'][4]['screenshot']).unlink()
            with self.assertRaisesRegex(ValueError, 'pagination screenshot missing'):
                M.validate_browser_report(payload, context, root)

    def test_browser_copy_must_exist_and_match_every_saved_byte(self):
        with tempfile.TemporaryDirectory() as name:
            root = Path(name); context, payload = self.browser_proof(root)
            path = root / payload['response_file']
            original = path.read_bytes()
            # Equal sizes and a matching receipt hash cannot hide a changed byte.
            changed = bytes([original[0] ^ 1]) + original[1:]
            path.write_bytes(changed)
            payload['response_sha256'] = hashlib.sha256(changed).hexdigest()
            with self.assertRaisesRegex(ValueError, 'same response copy'):
                M.validate_browser_report(payload, context, root)
            path.unlink()
            with self.assertRaisesRegex(ValueError, 'copy is missing'):
                M.validate_browser_report(payload, context, root)
        with tempfile.TemporaryDirectory() as name:
            root = Path(name); context, payload = self.browser_proof(root, 'revoked')
            (root / (context['download_file'] + '.response')).write_bytes(b'leaked')
            with self.assertRaisesRegex(ValueError, 'saved a file'):
                M.validate_browser_report(payload, context, root)

    def test_browser_runner_drops_issuer_and_tokens_uses_current_login_and_rejects_failure(self):
        with tempfile.TemporaryDirectory() as name:
            root=Path(name); context, payload=self.browser_proof(root, 'revoked')
            environment={'ADDP_ONLINE_ARTIFACT_DIR':name, 'ADDP_ONLINE_TEST_USER_USERNAME':'ordinary',
                'ADDP_ONLINE_TEST_USER_PASSWORD':'private', 'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN':'api-token',
                'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN':'issuer', 'ADDP_ONLINE_ADMIN_USER_PASSWORD':'admin'}
            path=root/'manager-filegdb-browser-revoked.json'
            def execute(args, **kwargs):
                self.assertEqual(args, ['npm', 'exec', '--', 'playwright', 'test',
                    'e2e/online/manager-filegdb.spec.js', '--config=playwright.online.config.js'])
                self.assertEqual(kwargs['cwd'],root/'console/frontend')
                self.assertTrue(kwargs['check'])
                env=kwargs['env']; self.assertEqual(env['ADDP_ONLINE_TEST_USER_USERNAME'],'ordinary')
                self.assertEqual(env['ADDP_ONLINE_TEST_USER_PASSWORD'],'private')
                self.assertFalse(any(k.startswith(('ADDP_ONLINE_FIXTURE_','ADDP_ONLINE_ADMIN_USER_')) for k in env))
                self.assertNotIn('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN',env)
                self.assertEqual(json.loads(env['ADDP_ONLINE_MANAGER_FILEGDB_JSON']),context)
                path.write_text(json.dumps(payload))
            with patch.object(M.subprocess,'run',side_effect=execute):
                self.assertEqual(M.run_browser(root,environment,context)['result'],'passed')
            with patch.object(M.subprocess,'run') as execute:
                with self.assertRaisesRegex(ValueError,'already exists'):M.run_browser(root,environment,context)
                execute.assert_not_called()
            path.unlink()
            with patch.object(M.subprocess,'run',side_effect=M.subprocess.CalledProcessError(1,['browser'])):
                with self.assertRaises(M.subprocess.CalledProcessError):M.run_browser(root,environment,context)

    def test_failed_directory_download_never_accepts_content_or_another_denial_gate(self):
        for status in (403, 409, 503):
            for mutation in (None, 'data', 'archive', 'security'):
                with self.subTest(status=status, mutation=mutation), tempfile.TemporaryDirectory() as directory:
                    payload = {'error': 'explicit failure'}
                    if mutation == 'data': payload['data'] = 'private'
                    if mutation == 'security': payload['error_code'] = 'security_protection_required'
                    raw = b'PK-private' if mutation == 'archive' else json.dumps(payload).encode()
                    binary = Mock(); binary.request.return_value = SimpleNamespace(status=status, raw=raw, payload=payload)
                    client = SimpleNamespace(base_url='http://localhost:8000', token='unchanged', timeout=30)
                    destination = Path(directory) / 'failed.zip'
                    with patch.object(OWNER, 'GatewayClient', return_value=binary):
                        if mutation in {'data', 'archive'} or (status == 403 and mutation == 'security'):
                            with self.assertRaises(ValueError): M.directory_download(client, 'locator', {}, status, destination)
                        else:
                            self.assertEqual(M.directory_download(client, 'locator', {}, status, destination)['status'], status)
                        self.assertFalse(destination.exists())

    def test_complete_directory_archive_checks_exact_members_original_bytes_and_headers(self):
        for mutation in (None, 'missing', 'outside', 'duplicate', 'wrong_bytes', 'wrong_length'):
            with self.subTest(mutation=mutation), tempfile.TemporaryDirectory() as directory:
                data = io.BytesIO()
                with zipfile.ZipFile(data, 'w') as archive:
                    archive.writestr(M.NAME + '/', b'')
                    if mutation != 'missing':
                        archive.writestr(M.NAME + '/a.gdbtable', b'changed' if mutation == 'wrong_bytes' else b'original')
                    if mutation == 'outside': archive.writestr('../outside', b'external')
                    if mutation == 'duplicate': archive.writestr(M.NAME + '/a.gdbtable', b'original')
                raw = data.getvalue()
                binary = Mock(); binary.request.return_value = SimpleNamespace(status=200, raw=raw,
                    headers={'Content-Type': 'application/zip', 'Content-Disposition': 'attachment; filename=authorization.gdb.zip',
                             'Content-Length': str(len(raw) + (1 if mutation == 'wrong_length' else 0))})
                client = SimpleNamespace(base_url='http://localhost:8000', token='unchanged', timeout=30)
                destination = Path(directory) / 'complete.zip'
                with patch.object(OWNER, 'GatewayClient', return_value=binary):
                    if mutation:
                        with self.assertRaises(ValueError):
                            M.directory_download(client, 'locator', {'a.gdbtable':hashlib.sha256(b'original').hexdigest()}, 200, destination)
                        self.assertFalse(destination.exists())
                    else:
                        result = M.directory_download(client, 'locator', {'a.gdbtable':hashlib.sha256(b'original').hexdigest()}, 200, destination)
                        self.assertEqual(result['sha256'],hashlib.sha256(destination.read_bytes()).hexdigest())
                        self.assertEqual(result['status'],200)

    def test_archive_handle_observation_is_exact_manager_and_checkout_owned(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); repository=root/'repository'; repository.mkdir()
            (repository/'.dev-pids').mkdir(); (repository/'.dev-pids/manager.pid').write_text('123')
            (repository/'.dev-bins').mkdir(); binary=repository/'.dev-bins/addp-manager'; binary.write_bytes(b'fixture')
            proc=root/'proc'; process=proc/'123'; process.mkdir(parents=True); (process/'fd').mkdir()
            (process/'cwd').symlink_to(repository); (process/'exe').symlink_to(binary)
            descriptor=process/'fd/1'; descriptor.symlink_to('/tmp/addp-manager-filegdb-download-owned.zip (deleted)')
            with patch.object(P,'owned_root',return_value=(root,root)):
                self.assertEqual(P.archive_state(repository,{},proc),{'pid':123,'open_archives':1})
                descriptor.unlink()
                self.assertEqual(P.archive_state(repository,{},proc),{'pid':123,'open_archives':0})
                (process/'cwd').unlink(); (process/'cwd').symlink_to(root)
                with self.assertRaisesRegex(ValueError,'another checkout'): P.archive_state(repository,{},proc)

    def test_raw_members_require_exact_source_bytes_ranges_and_denial_without_content(self):
        for mutation in (None, 'wrong_bytes', 'wrong_range', 'leaked_denial', 'security_denial'):
            with self.subTest(mutation=mutation):
                binary = Mock()
                def response(method, path, statuses, headers=None):
                    if statuses == (403,):
                        payload = {'error': 'denied'}
                        if mutation == 'leaked_denial': payload['data'] = 'private'
                        if mutation == 'security_denial': payload['error_code'] = 'security_protection_required'
                        return SimpleNamespace(status=403, payload=payload, raw=json.dumps(payload).encode())
                    raw = b'abcd-private-member'
                    if headers: raw = b'bad!' if mutation == 'wrong_range' else raw[:4]
                    elif mutation == 'wrong_bytes': raw = b'other-source'
                    return SimpleNamespace(status=statuses[0], payload={}, raw=raw)
                binary.request.side_effect = response
                user = SimpleNamespace(base_url='http://localhost:8000', token='unchanged', timeout=30)
                member = dict(name='a00000001.gdbtable', sha256=M.hashlib.sha256(b'abcd-private-member').hexdigest(), item_id=17)
                with patch.object(OWNER, 'GatewayClient', return_value=binary) as factory:
                    if mutation is None:
                        allowed = M.raw_member(user, 'locator', 7, member, 200)
                        denied = M.raw_member(user, 'locator', 7, member, 403)
                        self.assertEqual([r['range_status'] for r in allowed], [206, 206])
                        self.assertEqual([r['range_status'] for r in denied], [403, 403])
                        factory.assert_called_with(user.base_url, user.token, user.timeout)
                        paths = [call.args[1] for call in binary.request.call_args_list]
                        self.assertTrue(any('/storage-stream?' in path for path in paths))
                        self.assertTrue(any('/storage-assets/7/items/17/' in path for path in paths))
                    else:
                        with self.assertRaises(ValueError):
                            M.raw_member(user, 'locator', 7, member, 403 if mutation.endswith('denial') else 200)

    def test_real_http_item_array_is_explicit_and_other_endpoints_remain_object_typed(self):
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                self.send_response(200); self.send_header('Content-Type', 'application/json'); self.end_headers()
                self.wfile.write(json.dumps([{'id': 17, 'item_type': 'file', 'full_name': M.NAME,
                    'fingerprint': 'a' * 64}]).encode())
            def log_message(self, *args): pass
        server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        thread = threading.Thread(target=server.serve_forever); thread.start()
        try:
            client = M.API.GatewayClient(f'http://127.0.0.1:{server.server_port}', 'synthetic', 3)
            with self.assertRaisesRegex(M.API.SuiteError, 'JSON object'):
                client.request('GET', '/api/v1/meta/engines/7/items', (200,))
            self.assertEqual(M.find_item(client, 7)['id'], 17)
            # An unrelated object endpoint cannot silently accept a list.
            with self.assertRaisesRegex(M.API.SuiteError, 'JSON object'):
                client.request('GET', '/api/v1/system/auth/context', (200,))
        finally:
            server.shutdown(); server.server_close(); thread.join()

    def test_explicit_manager_case_keeps_all_previous_cases(self):
        for case in ('full', 'native-raster-media', 'native-table-orc', 'kml-export', 'filegdb-authorization'):
            self.assertEqual(OWNER.manager_case({'ADDP_ONLINE_MANAGER_CASE': case}), case)
        with self.assertRaises(OWNER.SuiteError):
            OWNER.manager_case({'ADDP_ONLINE_MANAGER_CASE': 'arbitrary'})

    def test_developer_environment_cannot_inject_faults_or_mounts(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(ValueError):
                M.guard(Path(directory), {})
            with patch.dict(os.environ, {}, clear=True), self.assertRaises(ValueError):
                P.owned_root()

    def test_facts_require_exact_whole_scope_and_stable_hash(self):
        for field, changed in [('basis_state', 'needs_refresh'), ('engine_id', '8'),
                               ('item_type', 'directory'), ('boundary_hash', 'c' * 64),
                               ('boundary', {'layout': 'single'})]:
            with self.subTest(field=field):
                auth = authorization(); auth.frozen = facts()
                bad = facts(); bad[field] = changed
                auth.issuer.request.return_value = SimpleNamespace(payload=bad)
                with self.assertRaises(ValueError):
                    auth.facts({'fingerprint': 'a' * 64})
        auth = authorization(); auth.issuer.request.return_value = SimpleNamespace(payload=facts())
        self.assertEqual(auth.facts({'fingerprint': 'a' * 64}), facts())

    def test_grant_registers_unknown_write_before_send_and_cleanup_only_owns_that_id(self):
        auth = authorization()
        auth.facts = Mock(return_value=facts())
        auth.issuer.request.side_effect = TimeoutError('uncertain outcome')
        with self.assertRaises(TimeoutError):
            auth.grant({'fingerprint': 'a' * 64})
        self.assertEqual(len(auth.grants), 1)
        command = auth.grants[0]
        auth.issuer.request.side_effect = None
        auth.revoke(command, cleanup=True)
        self.assertEqual(auth.grants, [])
        self.assertEqual(auth.issuer.request.call_args.args[1], auth.base + '/access_grants/' + command + '/revoke')
        self.assertEqual(auth.issuer.request.call_args.args[2], (200, 404))
        with self.assertRaises(M.FILES.SuiteError):
            auth.revoke('foreign')

    def test_grant_cannot_accept_changed_scope_or_recipient_receipt(self):
        for field in ('scope', 'recipient_id', 'boundary_hash'):
            auth = authorization(); auth.facts = Mock(return_value=facts())
            def response(method, path, statuses, body):
                row = dict(body, scope=facts()['boundary']['scope'], boundary_hash='b' * 64,
                           operator_principal_id='9', revocation=None)
                row[field] = 'wrong'
                return SimpleNamespace(payload=row)
            auth.issuer.request.side_effect = response
            with self.assertRaises(ValueError): auth.grant({'fingerprint': 'a' * 64})
            self.assertEqual(len(auth.grants), 1)
            self.assertFalse(auth.confirmed)

    def test_ungranted_account_is_denied_while_original_reader_stays_allowed(self):
        self.isolation_flow()

    def test_isolation_rejects_shared_identity_credentials_grant_or_wrong_denial(self):
        for mutation in ('same_user', 'issuer_user', 'same_token', 'issuer_token', 'has_grant',
                         'has_unknown_relation', 'leaked_read', 'security_not_ready', 'grant_changed', 'relation_created'):
            with self.subTest(mutation=mutation), self.assertRaises((ValueError, M.API.SuiteError)):
                self.isolation_flow(mutation)

    def isolation_flow(self, mutation=None):
        before = {'data': [{'grant_count': 1, 'inspection': {'observed_at': '2026-10-30T10:00:00Z',
            'reason': 'grant', 'sources': [{'request_id': 'original'}]}}], 'total': 1}
        after = deepcopy(before)
        if mutation == 'grant_changed': after['data'][0]['inspection']['sources'][0]['request_id'] = 'new'
        zero = {'data': [], 'total': 0}
        outsider_before = {'data': [{'grant_count': 1}], 'total': 1} if mutation == 'has_grant' else zero
        if mutation == 'has_unknown_relation': outsider_before = {'data': [{'grant_count': 0}], 'total': 1}
        outsider_after = {'data': [{'grant_count': 1}], 'total': 1} if mutation == 'relation_created' else zero
        auth = Mock(recipient='11', operator='9'); auth.issuer.token = 'issuer'
        auth.current.side_effect = [before, outsider_before, after, outsider_after]
        auth.history.return_value = {'request_id': 'original', 'revocation': None}
        reader = Mock(token='reader'); outsider = Mock(token='outsider')
        outsider_id = '11' if mutation == 'same_user' else ('9' if mutation == 'issuer_user' else '12')
        if mutation == 'same_token': outsider.token = reader.token
        if mutation == 'issuer_token': outsider.token = auth.issuer.token
        def read_response(method, path, statuses):
            child = 'initial_points' if 'initial_points' in path else 'added_points'
            rows = 2 if child == 'initial_points' else 1
            return SimpleNamespace(status=200, payload={'data': {'total': rows, 'rows': [
                {'label': child + '_' + str(i), 'value': i + 1} for i in range(rows)]}})
        reader.request.side_effect = read_response
        if mutation == 'leaked_read': outsider.request.side_effect = read_response
        else:
            outsider.request.return_value = SimpleNamespace(status=403, payload={'error': 'denied', **(
                {'error_code': 'security_protection_required'} if mutation == 'security_not_ready' else {})})
        report = {'tenant_id': 2}
        with patch.object(M.FILES, 'exact_user_principal', return_value=outsider_id) as identity:
            evidence = M.verify_account_isolation(auth, reader, outsider, outsider_id, 'locator',
                {'request_id': 'original'}, [('initial_points', 2), ('added_points', 1)], report)
        self.assertEqual([r['outsider_denied']['status'] for r in evidence['reads']], [403, 403])
        self.assertEqual([r['allowed_after']['rows'] for r in evidence['reads']], [2, 1])
        self.assertTrue(evidence['same_grant']); self.assertTrue(evidence['same_credentials'])
        identity.assert_called_once_with(outsider, 2, M.OUTSIDER_PERMISSIONS)
        self.assertEqual(auth.current.call_args_list[1].kwargs, {'recipient': '12'})
        self.assertEqual(report['account_isolation']['principal_id'], '12')
        auth.grant.assert_not_called(); auth.revoke.assert_not_called(); auth.deny.assert_not_called()

    def test_unknown_grant_outcome_is_owned_and_cleaned(self):
        auth = authorization(); auth.facts = Mock(return_value=facts())
        auth.issuer.request.side_effect = TimeoutError('unknown Grant response')
        with self.assertRaises(TimeoutError):
            auth.grant({'fingerprint': 'a' * 64})
        command, = auth.grants
        auth.issuer.request.side_effect = None
        auth.issuer.request.return_value = SimpleNamespace(status=404, payload={'error': 'not found'})
        auth.current = Mock(return_value={'total': 0, 'data': []})
        auth.close()
        self.assertEqual(auth.grants, [])
        self.assertTrue(auth.issuer.request.call_args.args[1].endswith('/' + command + '/revoke'))
        self.assertEqual(auth.issuer.request.call_args.args[2], (200, 404))

    def test_metadata_visibility_is_separate_from_read_relations(self):
        auth = authorization(); auth.facts = Mock(return_value=facts())
        def issue(method, path, statuses, body):
            return SimpleNamespace(status=201, payload=dict(body, boundary_hash='b' * 64,
                scope=facts()['boundary']['scope'], operator_principal_id='9', revocation=None))
        auth.issuer.request.side_effect = issue
        receipt = auth.grant({'fingerprint': 'a' * 64}, action='metadata_read')
        self.assertEqual(receipt['action'], 'metadata_read')
        auth.issuer.request.side_effect = None
        rows = [{'action': 'metadata_read'}, {'action': 'read'}]
        auth.issuer.request.return_value = SimpleNamespace(payload={'total': 2, 'data': rows})
        self.assertEqual(auth.current()['data'], [{'action': 'read'}])
        self.assertEqual(auth.current(action=None)['data'], rows)
        self.assertIn('page_size=100', auth.issuer.request.call_args.args[1])

    def test_expired_exact_history_is_verified_without_forging_revocation(self):
        for fault in (None, 'scope', 'action', 'expiry', 'revoked', 'truncated'):
            with self.subTest(fault=fault):
                auth = authorization(); auth.facts = Mock(return_value=facts())
                expiry = '2026-10-30T00:00:00+00:00'
                def issue(method, path, statuses, body):
                    return SimpleNamespace(status=201, payload=dict(body, boundary_hash='b' * 64,
                        scope=facts()['boundary']['scope'], operator_principal_id='9', revocation=None))
                auth.issuer.request.side_effect = issue
                receipt = auth.grant({'fingerprint': 'a' * 64}, expiry)
                auth.issuer.request.reset_mock()
                row = deepcopy(receipt)
                if fault == 'scope': row['scope'] = {}
                if fault == 'action': row['action'] = 'metadata_read'
                if fault == 'expiry': row['expires_at'] = '2026-10-29T00:00:00+00:00'
                if fault == 'revoked': row['revocation'] = {'reason': 'unexpected'}
                auth.issuer.request.side_effect = None
                auth.issuer.request.return_value = SimpleNamespace(payload={'data': [row], 'total': 2 if fault == 'truncated' else 1})
                if fault:
                    with self.assertRaises(ValueError): auth.cleanup()
                    self.assertEqual(auth.grants, [receipt['request_id']])
                else:
                    auth.cleanup()
                    self.assertEqual(auth.grants, [])
                self.assertTrue(all(call.args[0] == 'GET' for call in auth.issuer.request.call_args_list))

    def test_engine_root_future_empty_withdrawal_and_expiry(self):
        self.root_flow()

    def test_engine_root_rejects_leaks_wrong_scope_history_identity_and_scan(self):
        for fault in ('empty_hidden', 'future_precise_grant', 'future_read_failed', 'wrong_receipt',
                      'future_leaked_after_withdrawal', 'independent_read_failed', 'expiry_forged_revocation', 'credential', 'empty_is_item', 'empty_scan_has_item'):
            with self.subTest(fault=fault), self.assertRaises((ValueError, M.API.SuiteError)):
                self.root_flow(fault)

    def root_flow(self, fault=None):
        auth = authorization(); auth.source_type = 'nfs'
        auth.facts = Mock(return_value=facts())
        client, outsider = Mock(token='reader'), Mock(token='outsider')
        records, commands, clock, identified = {}, [], [0], {M.NAME}
        start = M.datetime.now(M.timezone.utc)
        class Clock(M.datetime):
            @classmethod
            def now(cls, tz=None): return start + M.timedelta(seconds=clock[0])
        def active():
            return [row for row in records.values() if row['revocation'] is None and
                (row.get('expires_at') is None or Clock.fromisoformat(row['expires_at']) > Clock.now())]
        def physical(action):
            commands.append(action)
            return {}
        def scan(actor, engine, deadline, *, depth, scope):
            self.assertIs(actor, auth.issuer); self.assertEqual(engine, 7); self.assertEqual(depth, 'basic')
            name, = scope['catalog_paths']; self.assertIn(name, (M.EMPTY_NAME, M.FUTURE_NAME))
            if name == M.FUTURE_NAME: identified.add(name)
            return 'empty-scan' if name == M.EMPTY_NAME else 'future-scan'
        owner = Mock(); owner.wait_for_meta_scan.side_effect = scan
        def issuer_request(method, url, statuses, body=None):
            if method == 'POST' and url.endswith('/access_grants'):
                row = dict(body, engine_id='7', approval_mode='independent', operator_principal_id='9', revocation=None)
                if 'item_fingerprint' in body:
                    row.update(boundary_hash='b' * 64, scope=facts()['boundary']['scope'])
                records[row['request_id']] = deepcopy(row)
                if fault == 'wrong_receipt' and 'node_recursive' in body: row['node_recursive'] = False
                return SimpleNamespace(status=201, payload=row)
            if method == 'POST' and url.endswith('/revoke'):
                command = url.split('/')[-2]; records[command]['revocation'] = {'reason': 'owned withdrawal'}
                return SimpleNamespace(status=200, payload=records[command])
            if '/data-items' in url:
                rows = [{'full_name': name, 'item_type': 'file', 'item_fingerprint': ('a' if name == M.NAME else 'f') * 64}
                        for name in sorted(identified)]
                if 'search=' in url: rows = [row for row in rows if row['full_name'] == M.NAME]
                return SimpleNamespace(status=200, payload={'data': rows, 'total': len(rows)})
            if '/meta/executions/' in url:
                return SimpleNamespace(status=200, payload={'status': 'success', 'metadata': {
                    'items_scanned': int(fault == 'empty_scan_has_item'), 'catalog_nodes_scanned': 1}})
            if '/access_grants/history' in url:
                rows = deepcopy(list(records.values()))
                if 'state=expired' in url:
                    rows = [row for row in rows if row.get('expires_at') and Clock.fromisoformat(row['expires_at']) <= Clock.now() and row['revocation'] is None]
                if fault == 'future_precise_grant' and M.FUTURE_NAME in identified:
                    rows.append({'request_id': 'foreign', 'item_fingerprint': 'f' * 64})
                if fault == 'expiry_forged_revocation':
                    for row in rows:
                        if row.get('expires_at') and Clock.fromisoformat(row['expires_at']) <= Clock.now(): row['revocation'] = {'reason': 'unexpected'}
                return SimpleNamespace(status=200, payload={'data': rows, 'total': len(rows)})
            if '/access_grants?' in url:
                rows = deepcopy(active()); return SimpleNamespace(status=200, payload={'data': rows, 'total': len(rows)})
            raise AssertionError((method, url))
        auth.issuer.request.side_effect = issuer_request
        def user_request(actor, method, url, statuses, body=None, **kwargs):
            allowed = [] if actor is outsider else active()
            root = any(row.get('node_recursive') is True and len(row['catalog_path']['segments']) == 1 for row in allowed)
            leaf = any(row.get('item_fingerprint') == 'a' * 64 for row in allowed)
            empty = any(row['catalog_path']['segments'][-1]['name'] == M.EMPTY_NAME for row in allowed)
            visible = bool(allowed)
            if url == '/api/v1/manager/engines':
                return SimpleNamespace(status=200, payload={'data': [{'id': 7}] if visible else []})
            if '/meta/resource-tree/' in url:
                if not visible: return SimpleNamespace(status=404, payload={'error': 'not found'})
                names = sorted(identified) if root else ([M.NAME] if leaf else [])
                if fault == 'future_leaked_after_withdrawal' and not root and leaf: names.append(M.FUTURE_NAME)
                children = [{'type': 'file', 'metadata': {'item_id': i + 1, 'full_name': name}} for i, name in enumerate(names)]
                if (root or empty) and fault != 'empty_hidden':
                    children.append({'type': 'directory', 'children': [], 'hasChildren': False,
                        'metadata': {'full_name': M.EMPTY_NAME, 'item_count': 0, **({'item_id': 99} if fault == 'empty_is_item' else {})}})
                return SimpleNamespace(status=200, payload={'metadata': {'engine_id': 7}, 'children': children})
            if '/meta/engines/' in url:
                return SimpleNamespace(status=200, payload=[{'id': i + 8, 'full_name': name,
                    'item_type': 'file', 'fingerprint': ('a' if name == M.NAME else 'f') * 64}
                    for i, name in enumerate(sorted(identified))])
            if '/manager/preview?' in url:
                if fault == 'credential': client.token = 'changed'
                original = ('/path/' + M.NAME + '?') in unquote(url)
                success = (root and fault != 'future_read_failed') or (original and leaf and fault != 'independent_read_failed')
                if (200 if success else 403) not in statuses:
                    raise M.API.SuiteError('unexpected status at GatewayClient boundary')
                rows = [{'label': 'initial_points_' + str(i), 'value': i + 1} for i in range(2)]
                return SimpleNamespace(status=200 if success else 403,
                    payload={'data': {'rows': rows, 'total': 2}} if success else {'error': 'denied'})
            raise AssertionError((method, url))
        for actor in (client, outsider):
            actor.request.side_effect = lambda *a, actor=actor, **kw: user_request(actor, *a, **kw)
        report = {'checks': []}
        with patch.object(M, 'datetime', Clock), patch.object(M.FILES, 'datetime', Clock), \
                patch.object(M.time, 'sleep', side_effect=lambda delay: clock.__setitem__(0, clock[0] + delay)):
            try:
                M.verify_engine_root(auth, client, outsider, report, physical, owner)
            finally:
                if fault not in {'wrong_receipt', 'expiry_forged_revocation'}: auth.cleanup()
        self.assertEqual(set(report['checks']), M.ROOT_CHECKS)
        self.assertEqual(len(report['checks']), 6)
        self.assertEqual(commands, ['root-seed', 'root-future'])
        self.assertTrue(report['engine_navigation']['same_credential'])
        self.assertTrue(report['engine_navigation']['no_leaf_grants'])
        self.assertEqual(report['engine_navigation']['independent_item_read']['rows'], 2)
        self.assertEqual(auth.grants, [])
        self.assertTrue(all(row['revocation'] is None for row in records.values() if row.get('expires_at')))
        return report

    def test_root_source_partial_copy_and_cleanup_preserve_original_and_unowned_files(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); (root / M.NAME).mkdir()
            (root / M.NAME / 'a.gdbtable').write_bytes(b'original'); (root / 'foreign').write_bytes(b'keep')
            with patch.object(P, 'owned_root', return_value=(root, root)), patch.dict(os.environ, ADDP_ONLINE_TEST_RUN_ID='run'):
                self.assertEqual(P.native('root-seed', root), {'empty_directory': M.EMPTY_NAME})
                self.assertEqual(P.native('root-future', root), {'future_data_item': M.FUTURE_NAME})
                self.assertEqual((root / M.FUTURE_NAME / 'a.gdbtable').read_bytes(), b'original')
                self.assertEqual(P.native('root-delete', root), {'root_source_cleanup': 'passed'})
                self.assertEqual((root / M.NAME / 'a.gdbtable').read_bytes(), b'original')
                self.assertEqual((root / 'foreign').read_bytes(), b'keep')
                P.native('root-seed', root)
                def interrupted(source, dest): dest.mkdir(); (dest / 'partial').write_bytes(b'x'); raise OSError('copy interrupted')
                with patch.object(P.shutil, 'copytree', side_effect=interrupted), self.assertRaises(OSError):
                    P.native('root-future', root)
                P.native('root-delete', root)
                self.assertFalse((root / M.FUTURE_NAME).exists())
                self.assertFalse((root / M.EMPTY_NAME).exists())
                (root / M.EMPTY_NAME).mkdir()
                with self.assertRaisesRegex(ValueError, 'unowned root fixture'): P.native('root-delete', root)

    def test_environment_failure_is_not_counted_as_permission_denial_or_partial_content(self):
        client = Mock()
        client.request.return_value = SimpleNamespace(status=503, payload={'error': 'unavailable'})
        self.assertEqual(M.preview(client, 'exact', 'added_points', (500, 502, 503, 504))['status'], 503)
        self.assertEqual(client.request.call_args.args[2], (500, 502, 503, 504))
        client.request.return_value = SimpleNamespace(status=503, payload={'data': {'rows': []}})
        with self.assertRaises(ValueError): M.preview(client, 'exact', 'added_points', 503)
        client.request.return_value = SimpleNamespace(status=200, payload={'data': {'total': 2, 'rows': []}})
        with self.assertRaises(ValueError): M.preview(client, 'exact', 'added_points', 200, 2)

    def test_real_read_child_sigkill_leaves_no_marker_and_has_no_issuer_secret(self):
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                self.send_response(200); self.send_header('Content-Type', 'application/json'); self.end_headers()
                self.wfile.write(b'{"data":{"total":1,"rows":[{"value":1,"label":"added_points_0"}]}}')
            def log_message(self, *args): pass
        server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        thread = threading.Thread(target=server.serve_forever); thread.start()
        try:
            with tempfile.TemporaryDirectory() as directory:
                env = dict(os.environ, GATEWAY_URL=f'http://127.0.0.1:{server.server_port}',
                    ADDP_ONLINE_TEST_USER_ACCESS_TOKEN='synthetic-test-token', ADDP_ONLINE_SECRET_DIR=directory,
                    ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN='must-not-reach-child')
                real = M.subprocess.Popen
                def launch(*args, **kwargs):
                    self.assertNotIn('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN', kwargs['env'])
                    return real(*args, **kwargs)
                with patch.object(M.subprocess, 'Popen', side_effect=launch):
                    evidence = M.kill_read_child(env, 'addp://engine/7/path/authorization.gdb', Path(directory))
                self.assertTrue(evidence['killed']); self.assertEqual(evidence['rows'], 1)
                self.assertFalse((Path(directory) / 'filegdb-child-ready.json').exists())
        finally:
            server.shutdown(); server.server_close(); thread.join()

    def test_meta_explicit_reexecution_preserves_failed_terminal_and_uses_new_identity_check(self):
        self.explicit_meta_flow()

    def test_meta_reexecution_rejects_wrong_identity_status_scope_or_hidden_retry(self):
        for fault in ('first_success', 'permission_denied', 'wrong_user', 'credential', 'same_execution',
                      'wrong_scope', 'automatic_attempt', 'retry_of', 'recovery_failed', 'history_changed',
                      'missing_source_proof', 'wrong_source_execution', 'wrong_source_target', 'other_component',
                      'submit_denied', 'queued_alias'):
            with self.subTest(fault=fault), self.assertRaises((ValueError, M.API.SuiteError)):
                self.explicit_meta_flow(fault)

    def explicit_meta_flow(self, fault=None):
        auth = authorization(); auth.issuer.token = 'issuer-token'; auth.issuer_token = auth.issuer.token
        failed_id = '11111111-1111-4111-8111-111111111111'
        success_id = failed_id if fault == 'same_execution' else '22222222-2222-4222-8222-222222222222'
        identities = [dict(execution_id=failed_id, status='failed'), dict(execution_id=success_id, status='success')]
        for row in identities:
            row.update(tenant_id=2, module='meta', task_type='scan', source='meta', trigger_type='manual',
                triggered_by=9, attempt=1, max_attempts=3, execution_boundary='bounded',
                scan_context={'engine_id': 7, 'scan_depth': 'basic'},
                metadata={'items_scanned': 0 if row['status']=='failed' else 1, 'catalog_nodes_scanned': 0 if row['status']=='failed' else 1})
        identities[0]['error_details']={'category':'execution_failed','failed_targets_count':1}
        if fault=='first_success': identities[0]['status']='success'
        if fault=='permission_denied':identities[0]['error_details']['category']='permission_denied'
        if fault=='wrong_scope':identities[1]['scan_context']['engine_id']=8
        if fault=='automatic_attempt':identities[1]['attempt']=2
        if fault=='retry_of':identities[1]['retry_of_execution_id']=failed_id
        if fault=='recovery_failed':identities[1]['status']='failed'
        posts=[]; gets={}
        def request(method,path,statuses,body=None):
            if method=='POST':
                self.assertEqual(path,'/api/v1/meta/scan/run/manual');self.assertEqual(statuses,(201,))
                self.assertEqual(body,dict(engine_id=7,scan_depth='basic',trigger_type='manual',source='meta',force=True,catalog_paths=[M.NAME]))
                posts.append(body)
                if fault=='submit_denied':raise M.API.SuiteError('unexpected status 403 at GatewayClient boundary')
                if fault=='credential' and len(posts)==2:auth.issuer.token='changed'
                return SimpleNamespace(payload={'execution_id':identities[len(posts)-1]['execution_id'],'max_attempts':3})
            self.assertEqual(method,'GET');self.assertEqual(statuses,(200,))
            row=identities[0] if path.endswith(failed_id) else identities[1]
            gets[path]=gets.get(path,0)+1
            if gets[path]==1:row=dict(row,status='pending',attempt=0)
            if fault=='queued_alias' and gets[path]==1:row['status']='queued'
            if fault=='history_changed' and len(posts)==2 and path.endswith(failed_id):row=dict(row,status='success')
            return SimpleNamespace(payload=deepcopy(row))
        auth.issuer.request.side_effect=request
        report={'tenant_id':2,'engine_id':7,'checks':[]}
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory); log=root/'logs/runtime/meta/worker/run.jsonl';log.parent.mkdir(parents=True)
            error='failed to list directory '+M.NAME+': write tcp 127.0.0.1:1->127.0.0.1:2049: write: broken pipe'
            if fault=='wrong_source_target':error=error.replace(M.NAME,'foreign-'+M.NAME)
            if fault=='other_component':error='database '+M.NAME+': connection refused'
            message={'execution_id':success_id if fault=='wrong_source_execution' else failed_id,'error':error}
            if fault!='missing_source_proof':log.write_text(json.dumps({'message':json.dumps(message)})+'\n')
            users=['9','8' if fault=='wrong_user' else '9']
            with patch.object(M.FILES,'exact_user_principal',side_effect=users) as principal, \
                    patch.object(M.time,'sleep'), patch.object(M.time,'monotonic',side_effect=range(0,1000000)):
                M.verify_explicit_meta_scan(auth,report,root,'unavailable')
                M.verify_explicit_meta_scan(auth,report,root,'recovered')
            self.assertEqual(principal.call_count,2)
            self.assertEqual(len(posts),2)
            proof=report['meta_explicit_reexecution']
            self.assertEqual(proof['failed']['execution']['status'],'failed')
            self.assertEqual(proof['recovered']['execution']['status'],'success')
            self.assertEqual(proof['result'],'passed');self.assertTrue(proof['same_credential'])
            self.assertNotIn('issuer-token',json.dumps(report))
            self.assertEqual(identities[0]['status'],'failed')
            return report

    def test_outage_compares_rules_while_preserving_each_query_observation(self):
        self.outage_observations()

    def test_outage_still_rejects_changed_grant_content_and_invalid_observation(self):
        changes = [
            lambda row: row.update(expires_at='2026-11-01T00:00:00Z'),
            lambda row: row.update(grant_count=2),
            lambda row: row['inspection']['sources'][0].update(request_id='replacement'),
            lambda row: row['inspection'].update(rule_covered=False),
            lambda row: row['catalog_path'].update(engine_id=8),
            lambda row: row['inspection'].pop('observed_at'),
            lambda row: row['inspection'].update(observed_at='invalid'),
        ]
        for change in changes:
            with self.subTest(change=change), self.assertRaises(ValueError):
                self.outage_observations(change)

    def outage_observations(self, change=None):
        baseline = {'data': [{'catalog_path': M.target(7), 'grant_count': 1, 'expires_at': None,
            'inspection': {'observed_at': '2026-10-30T06:00:00+00:00', 'reason': 'grant',
                'rule_covered': True, 'sources': [{'request_id': 'original'}]}}],
            'total': 1, 'page': 1, 'page_size': 20, 'total_pages': 1}
        observations = [deepcopy(baseline) for _ in range(5)]
        for index, current in enumerate(observations):
            current['data'][0]['inspection']['observed_at'] = f'2026-10-30T06:00:0{index}+00:00'
        if change:
            change(observations[1]['data'][0])
        original = deepcopy(observations)
        before = dict(instance_id='before', git_commit='sha', build_id='build', source_fingerprint='fp')
        after = dict(before, instance_id='after')
        health = Mock(); health.request.side_effect = [SimpleNamespace(payload=value) for value in (before, after, after)]
        active = {'module': None}
        @contextmanager
        def interruption(*args):
            active['module'] = args[-1]
            try:
                yield {'module': args[-1]}
            finally:
                active['module'] = None
        current = Mock(side_effect=observations)
        download = Mock(side_effect=lambda actor, status, phase: {'status': status if isinstance(status, int) else 503})
        def meta_observation(phase):
            self.assertEqual(active['module'], 'nfs' if phase == 'unavailable' else None)
            return {'phase': phase}
        meta_scan = Mock(side_effect=meta_observation)
        with patch.object(M.API, 'GatewayClient', return_value=health), \
                patch.object(M.API, 'wait_manager_read_boundary'), patch.object(P, 'interruption', interruption):
            evidence = P.verify_outages(Path('/unused'), {'MANAGER_URL': 'http://unused',
                'ADDP_ONLINE_MANAGER_NFS_ENGINE_ID': '7'}, Mock(), 'locator', current,
                lambda: {'a.gdbtable': 'same-sha'}, Mock(return_value={'status': 503}), download, meta_scan=meta_scan)
        self.assertEqual([call.args[0] for call in meta_scan.call_args_list], ['unavailable', 'recovered'])
        self.assertNotIn('meta_scan_failure', evidence[0])
        self.assertEqual(evidence[1]['meta_scan_failure'], {'phase': 'unavailable'})
        self.assertEqual(evidence[1]['meta_explicit_reexecution'], {'phase': 'recovered'})
        self.assertEqual(observations, original)  # Raw timestamps remain available for independent review.
        for index, outage in enumerate(evidence):
            self.assertTrue(outage['same_grant']); self.assertTrue(outage['source_unchanged'])
            self.assertEqual(outage['current_before'], original[0])
            self.assertEqual(outage['current_during'], original[1 + 2 * index])
            self.assertEqual(outage['current_after'], original[2 + 2 * index])
            self.assertEqual(outage['failed_archive']['status'], 503)
            self.assertEqual(outage['recovered_archive']['status'], 200)
        self.assertEqual([call.args[2] for call in download.call_args_list],
                         ['fault-manager', 'recovered-manager', 'fault-nfs', 'recovered-nfs'])

    def test_meta_failure_proof_error_stops_requests_but_still_recovers_the_nfs_fixture(self):
        recovered = []
        @contextmanager
        def interruption(*args):
            try:
                yield {'module': args[-1]}
            finally:
                recovered.append(args[-1])
        before = dict(instance_id='before', git_commit='sha', build_id='build', source_fingerprint='fp')
        health = Mock(); health.request.side_effect = [SimpleNamespace(payload=before),
            SimpleNamespace(payload=dict(before, instance_id='after'))]
        meta_scan = Mock(side_effect=ValueError('missing source proof'))
        with patch.object(M.API, 'GatewayClient', return_value=health), \
                patch.object(M.API, 'wait_manager_read_boundary'), patch.object(P, 'interruption', interruption):
            with self.assertRaisesRegex(ValueError, 'missing source proof'):
                P.verify_outages(Path('/unused'), {'MANAGER_URL': 'http://unused',
                    'ADDP_ONLINE_MANAGER_NFS_ENGINE_ID': '7'}, Mock(), 'locator',
                    lambda: {'data': [], 'total': 0}, lambda: {}, Mock(), Mock(), meta_scan=meta_scan)
        self.assertEqual(recovered, ['manager', 'nfs'])
        meta_scan.assert_called_once_with('unavailable')

    def test_nfs_daemon_stop_is_not_accepted_when_actual_mount_is_still_readable(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); (root / M.NAME).mkdir(); (root / M.NAME / 'a.gdbtable').write_bytes(b'source')
            (root / 'filegdb-nfs-mount').write_text(str(root))
            calls = []
            def run(args, **kwargs):
                calls.append(args)
                return SimpleNamespace(returncode=0)
            with patch.object(P, 'owned_root', return_value=(root, root)), patch.object(P.subprocess, 'run', side_effect=run):
                with self.assertRaisesRegex(ValueError, 'data path did not fail'):
                    with P.interruption(root, {}, 'nfs'): self.fail('false outage accepted')
            self.assertEqual(calls[-1], ['sudo', 'systemctl', 'start', 'nfs-kernel-server.service'])

    def test_manager_lifecycle_recovers_on_assertion_failure_using_existing_exact_scripts(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); calls = []
            def run(args, **kwargs):
                calls.append(args)
                self.assertNotIn('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN', kwargs['env'])
                return SimpleNamespace(returncode=0)
            with patch.object(P, 'owned_root', return_value=(root, root)), patch.object(P.subprocess, 'run', side_effect=run), \
                    patch.object(P.socket, 'create_connection', side_effect=ConnectionRefusedError):
                with self.assertRaisesRegex(ValueError, 'probe failure'):
                    with P.interruption(root, {'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN': 'secret'}, 'manager'):
                        raise ValueError('probe failure')
            self.assertIn('--force', calls[0]); self.assertIn('--exact-process', calls[-1])
            self.assertEqual(calls[-1][-1], '-manager')

    def test_physical_snapshot_and_cleanup_preserve_unowned_sibling(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); (root / M.NAME).mkdir(); (root / M.NAME / 'a.gdbtable').write_bytes(b'exact')
            (root / '.filegdb-acceptance-owner').write_text('run'); (root / 'keep').write_text('foreign')
            with patch.object(P, 'owned_root', return_value=(root, root)), patch.dict(os.environ, ADDP_ONLINE_TEST_RUN_ID='run'):
                before = P.native('snapshot', root)
                self.assertEqual(list(before), ['a.gdbtable'])
                self.assertTrue(P.native('delete', root)['source_removed'])
            self.assertEqual((root / 'keep').read_text(), 'foreign')

    def test_cleanup_must_prove_zero_owned_grants(self):
        auth = authorization(); auth.frozen = facts(); auth.cleanup = Mock(); auth.current = Mock(return_value={'total': 1})
        with self.assertRaisesRegex(ValueError, 'survived cleanup'): auth.close()

    def test_early_scan_failure_stays_failed_but_still_closes_authorization_and_deletes_owned_source(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            env = {'ADDP_ONLINE_ARTIFACT_DIR': directory, 'ADDP_ONLINE_SECRET_DIR': directory,
                'GATEWAY_URL': 'http://localhost:8000', 'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'test',
                'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN': 'issuer', 'ADDP_ONLINE_FIXTURE_OUTSIDER_ACCESS_TOKEN': 'outsider',
                'ADDP_ONLINE_MANAGER_NFS_ENGINE_ID': '7',
                'ADDP_ONLINE_TEST_TENANT_ID': '2', 'ADDP_ONLINE_TEST_RUN_ID': 'manager-1-1'}
            auth = Mock(); auth.current.return_value = {'total': 0, 'data': []}; auth.close.return_value = None
            owner = Mock(); owner.wait_for_meta_scan.side_effect = ValueError('source scan unavailable')
            actions = []
            def physical(action, target):
                actions.append(action)
                if action == 'snapshot': raise OSError('source not readable')
                return {'source_removed': True}
            with patch.object(M, 'guard', return_value=root), patch.object(M.API, 'GatewayClient'), \
                    patch.object(M.FILES, 'exact_user_principal', side_effect=['11', '12']), \
                    patch.object(M.API, 'wait_manager_read_boundary'), patch.object(M, 'Authorization', return_value=auth), \
                    patch.object(P, 'physical', side_effect=physical):
                with self.assertRaises(ValueError): M.run(root, env, owner)
            auth.close.assert_called_once_with()
            self.assertEqual(actions, ['seed', 'snapshot', 'root-delete', 'delete'])
            report = json.loads((root / 'manager-filegdb.json').read_text())
            self.assertEqual(report['result'], 'failed')
            self.assertEqual(report['error_type'], 'ValueError')
            self.assertEqual(report['cleanup_errors'], [{'stage': 'source_snapshot', 'error_type': 'OSError'}])


if __name__ == '__main__': unittest.main()
