import base64
import copy
import hashlib
import importlib
import json
import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path
from types import SimpleNamespace
from unittest import mock
from urllib.parse import parse_qs, urlsplit

IMG = importlib.import_module('scripts.test.manager-img-online')
OWNER = importlib.import_module('scripts.test.manager-internal-artifact-lineage-online')


def detail(spec, index=1):
    attrs = {'item': {'data_type': 'media', 'format': spec['source_format'], 'layout': 'single'},
             'type_info': {'media': {'kind': 'image', 'encoding': spec['driver'], 'width': spec.get('width',256), 'height': spec.get('height',256)}},
             'format_info': {spec['source_format']: {'scan_complete': True, 'driver': spec['driver'], 'band_count': spec['band_count'], **spec.get('grid_facts',{})}}}
    if spec['source_format']=='tiff':
        attrs['type_info']['media']['encoding']='tiff'
        attrs['format_info']['tiff']={'profile':'cog','is_cloud_optimized':True,'cog_check_level':'heuristic',
            'is_tiled':True,'has_overviews':False,'big_tiff':False,'page_count':1,'page_summary_status':'parsed',
            'pages':[{'ifd_index':0,'width':240,'height':120}]}
        attrs['storage'] = {'last_modified_at': '2026-10-29T23:59:00.691Z',
                            'content_hash': hashlib.sha256(b'test').hexdigest()}
    if spec['source_srid']:
        attrs['capabilities'] = {'spatial': {'srid': spec['source_srid'], 'extent': list(spec['extent'])}}
    if spec['source_format']=='grd':
        attrs['format_info']['grd']['bands']=[{'dtype':'Float32' if spec['driver']=='GSBG' else 'Float64',
            'nodata':spec['grid_facts']['blank_value'],'nodata_is_nan':False}]
    return {'id': index, 'item_type': 'object', 'fingerprint': str(index) * 64,'scanned_at':'2026-10-30T00:00:00Z',
            'full_name': 'bucket/' + spec['object'], 'size_bytes': 4, 'attributes': attrs}


def structural_detail(item):
    result = {key: value for key, value in item.items() if key not in ('size_bytes', 'attributes')}
    attrs = item['attributes']
    result['attributes'] = {'schema_version': 1, 'item': {
        key: attrs['item'][key] for key in ('data_type', 'format', 'layout')}}
    if 'capabilities' in attrs:
        result['attributes']['capabilities'] = {'spatial': {'srid': attrs['capabilities']['spatial']['srid']}}
    return result


def preview_payload(item):
    return {'data': {'item_meta': {'full_name': item['full_name'], 'fingerprint': item['fingerprint'],
        'attributes': [{'key': key, 'value': value} for key, value in item['attributes'].items()]},
        'object': {'size_bytes': item['size_bytes']}}}


def composition_browser_report(evidence):
    names = ['mask-center', 'mask-left', 'mask-right', 'middle', 'bottom', 'outside']
    pixels = []
    for name in names:
        opaque = name != 'outside' and (name != 'mask-center' or evidence['profile'] == 'sidecars')
        pixels.append({'name': name, 'in_canvas': True, 'opaque': opaque, 'rgba': [128, 128, 128, 209] if opaque else [0, 0, 0, 0]})
    render = {'projection': 'EPSG:4326', 'extent': [113, 27.9744, 113.0256, 28], 'pixels': pixels}
    return {'schema_version': 'addp.manager-tiff-composition-browser/v1', 'result': 'passed',
            **{key: evidence[key] for key in ('run_id', 'profile', 'identity', 'source')},
            'range_status': 206, 'reopened': True, 'reused_result': True,
            'generation_requests': 0, 'failed_business_responses': 0, 'warnings_errors': 0,
            'renders': [copy.deepcopy(render), copy.deepcopy(render)]}


def browser_report(evidence):
    cases = []
    for source in evidence['cases']:
        case = {key: source[key] for key in ('case', 'item_id', 'locator', 'sha256', 'source_srid')}
        case.update(download_sha256=source['sha256'], metadata_verified=True, source_preview_verified=True,
                    metadata_projection_verified=True, content_metadata_verified=True)
        if source['source_srid']:
            case.update(preview_url=source['preview_url'], range_loaded=True, map_loaded=True,
                        reopened=True, reused_result=True, pixels={'cyan': 128, 'magenta': 128, 'yellow': 128})
        elif evidence['source_format']=='tiff':
            case.update(map_absent=True,source_image_absent=True,range_loaded=True,tiff_pixels_verified=True,
                tiff_pages_verified=True,cog_metadata_visible=True,zero_overviews_verified=True,
                refreshed_metadata_verified=True,reopened=True,scanned_at=source['refresh']['scanned_at_after'])
        else:
            case.update(generation_unavailable=True, map_absent=True, source_image_absent=True, unsupported_message_visible=True)
        cases.append(case)
    return {'schema_version': 'addp.manager-raster-media-browser/v4', 'result': 'passed', 'run_id': evidence['run_id'],
            'source_format':evidence['source_format'], 'identity': evidence['identity'], 'cases': cases, 'generation_requests': 0,
            'failed_business_responses': 0, 'warnings_errors': 0}


class ManagerIMGTests(unittest.TestCase):
    def test_nfs_tiff_grants_require_the_exact_single_image_and_expiry(self):
        grants = importlib.import_module('scripts.test.ftp-consumer-flow-online')
        fingerprint = hashlib.sha256(f'3:{IMG.NFS_TIFF_NAME}'.encode()).hexdigest()
        target = grants.file_target(3, IMG.NFS_TIFF_NAME, 'nfs')
        composition = {'format': 'tiff', 'source_layout': 'single',
                       'refs': [{'target': target, 'role': 'image', 'primary': True, 'required': True}]}
        facts = {'schema_version': 'meta.data_item_authorization_facts/v1', 'engine_id': '3',
                 'item_fingerprint': fingerprint, 'full_name': IMG.NFS_TIFF_NAME, 'item_type': 'file',
                 'basis_state': 'complete', 'boundary_hash': 'a' * 64, 'observed_at': '2026-11-10T09:00:00Z',
                 'boundary': {'layout': 'single', 'file_composition': composition}}
        model = {'format': 'tiff', 'layout': 'single', 'full_name': IMG.NFS_TIFF_NAME, 'fingerprint': fingerprint,
                 'composition': [{'path': IMG.NFS_TIFF_NAME, 'role': 'image', 'primary': True, 'required': True}]}
        issuer = mock.Mock()
        def request(method, path, statuses, body=None):
            if method == 'GET':
                return SimpleNamespace(payload=facts)
            return SimpleNamespace(payload={**body, 'engine_id': '3', 'approval_mode': 'independent',
                'operator_principal_id': '11', 'revocation': None, 'boundary_hash': facts['boundary_hash'],
                'file_composition': composition})
        issuer.request.side_effect = request
        with mock.patch.object(grants, 'exact_user_principal', side_effect=['11', '22']):
            auth = grants.ModelSourceAuthorization(issuer, mock.Mock(),
                {'tenant_id': 4, 'engine_id': 3, 'principal_id': '22'}, source_type='nfs')
        auth.grant(model, action='metadata_read')
        expiry = datetime(2026, 11, 10, 9, 1, tzinfo=timezone.utc)
        self.assertEqual(auth.grant(model, expires_at=expiry)['expires_at'], expiry.isoformat())
        commands = [call.args[3] for call in issuer.request.call_args_list if call.args[0] == 'POST']
        self.assertEqual([(row['action'], row['expiry_mode']) for row in commands],
                         [('metadata_read', 'until_revoked'), ('read', 'at_time')])
        self.assertEqual([row['catalog_path'] for row in commands], [target, target])
        composition['source_layout'] = 'multi'
        with self.assertRaises(grants.SuiteError):
            auth.grant(model, expires_at=expiry)
        model['layout'] = facts['boundary']['layout'] = 'multi'
        model['composition'].append({'path': IMG.NFS_TIFF_NAME + '.msk', 'role': 'mask', 'required': False, 'primary': False})
        composition['refs'].append({'target': auth.target(IMG.NFS_TIFF_NAME + '.msk'),
                                    'role': 'mask', 'required': False, 'primary': False})
        auth.grant(model)
        self.assertEqual(issuer.request.call_args.args[3]['expected_boundary_hash'], facts['boundary_hash'])
        self.assertNotIn('file_composition', issuer.request.call_args.args[3])
        good_facts = copy.deepcopy(facts)
        for mutate in (lambda f: f['boundary']['file_composition']['refs'].pop(),
                       lambda f: f['boundary']['file_composition']['refs'][1].update(required=True),
                       lambda f: f['boundary']['file_composition']['refs'][1].update(primary=True),
                       lambda f: f['boundary']['file_composition'].update(source_layout='single'),
                       lambda f: f.update(basis_state='incomplete')):
            facts.clear(); facts.update(copy.deepcopy(good_facts)); mutate(facts)
            count = sum(call.args[0] == 'POST' for call in issuer.request.call_args_list)
            with self.assertRaises(grants.SuiteError): auth.grant(model)
            self.assertEqual(sum(call.args[0] == 'POST' for call in issuer.request.call_args_list), count)

    def test_nfs_tiff_requires_range_denial_expiry_and_real_browser_proof(self):
        grants = importlib.import_module('scripts.test.ftp-consumer-flow-online')
        env = {'ADDP_ONLINE_MANAGER_NFS_ENGINE_ID': '3', 'ADDP_ONLINE_TEST_TENANT_ID': '4',
               'GATEWAY_URL': 'http://127.0.0.1', 'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN': 'fixture'}
        raw = bytes(range(128))
        now = datetime(2026, 11, 10, 9, 0, tzinfo=timezone.utc)
        expiry = now + timedelta(seconds=90)
        fingerprint = hashlib.sha256(f'3:{IMG.NFS_TIFF_NAME}'.encode()).hexdigest()
        item = {'id': 5, 'item_type': 'file', 'full_name': IMG.NFS_TIFF_NAME, 'fingerprint': fingerprint}
        receipt = {'request_id': 'read-command', 'item_fingerprint': fingerprint,
                   'boundary_hash': 'a' * 64, 'expires_at': expiry.isoformat()}
        for invalid in ('', 'browser', 'withdrawn'):
            with self.subTest(invalid=invalid):
                client, issuer, auth = mock.Mock(), mock.Mock(), mock.Mock()
                auth.operator, auth.recipient, auth.base = '11', '22', '/api/v1/system/engines/3'
                auth.target.return_value = grants.file_target(3, IMG.NFS_TIFF_NAME, 'nfs')
                auth.grants, auth.confirmed = ['read-command'], {'read-command'}
                auth.grant.side_effect = [{'request_id': 'metadata-command'}, receipt]
                issuer.request.side_effect = [
                    OWNER.Response(200, {'triggered_by': 11, 'metadata': {'items_scanned': 1}}, {}),
                    OWNER.Response(200, {'total': 1, 'data': [{**receipt, 'action': 'read',
                        'revocation': {} if invalid == 'withdrawn' else None}]}, {}),
                    OWNER.Response(200, {'total': 0, 'data': []}, {}),
                ] if invalid != 'browser' else [
                    OWNER.Response(200, {'triggered_by': 11, 'metadata': {'items_scanned': 1}}, {}),
                    OWNER.Response(200, {'total': 0, 'data': []}, {}),
                ]
                client.request.side_effect = [
                    OWNER.Response(404, {}, {}),
                    OWNER.Response(403, {'error': 'denied'}, {}),
                    OWNER.Response(206, {}, {'Content-Range': 'bytes 0-63/128'}, raw[:64]),
                    OWNER.Response(403, {'error': 'denied'}, {}),
                    OWNER.Response(404, {}, {}),
                ]
                evidence = {'source_format': 'tiff', 'identity': {'principal_id': '22'}, 'run_id': 'run', 'cases': []}
                report = {}
                def browser(data):
                    outcome = browser_report(data)
                    nfs = data['nfs_tiff_authorization']
                    if invalid != 'browser':
                        outcome['nfs_tiff_authorization'] = {
                            'locator': nfs['locator'], 'item_id': 5, 'range_status': 206, 'pixels_verified': True,
                        }
                    return outcome
                with mock.patch.object(grants, 'ModelSourceAuthorization', return_value=auth), \
                     mock.patch.object(OWNER, 'GatewayClient', return_value=issuer), \
                     mock.patch.object(OWNER, 'wait_for_meta_scan', return_value='scan'), \
                     mock.patch.object(OWNER, 'find_fixture_item', return_value=item), \
                     mock.patch.object(IMG, 'seed_nfs_tiff', return_value=SimpleNamespace(read_bytes=lambda: raw)), \
                     mock.patch.object(IMG, 'datetime') as clock, mock.patch.object(IMG.time, 'sleep'):
                    clock.now.side_effect = [now, now if invalid == 'browser' else expiry + timedelta(seconds=1)]
                    if invalid:
                        with self.assertRaises(OWNER.SuiteError):
                            IMG.verify_nfs_tiff_authorization(OWNER, env, client, evidence, raw, browser, report, 30)
                    else:
                        IMG.verify_nfs_tiff_authorization(OWNER, env, client, evidence, raw, browser, report, 30)
                auth.revoke.assert_called_once_with('metadata-command')
                auth.cleanup.assert_called_once()
                self.assertEqual(report['result'], 'failed' if invalid else 'passed')
                if not invalid:
                    self.assertEqual(report['range_statuses'], {'before_grant': 403, 'granted': 206, 'expired': 403})
                    self.assertEqual(auth.grants, [])
                    self.assertTrue(report['natural_expiry_verified'])
                    self.assertTrue(report['zero_current_grants'])
                self.assertEqual([call.kwargs for call in client.request.call_args_list if 'storage-stream' in call.args[1]],
                    [{'headers': {'Range': 'bytes=0-63'}}] * (2 if invalid == 'browser' else 3))

    def test_four_member_grant_uses_complete_meta_boundary_and_rejects_missing_sidecars(self):
        grants = importlib.import_module('scripts.test.ftp-consumer-flow-online')
        spec = IMG.TIFF_COMPOSITIONS['sidecars']; name = spec['directory'] + '/' + spec['multi']
        model = {'format': 'tiff', 'layout': 'multi', 'full_name': name, 'fingerprint': 'a' * 64,
                 'composition': [{'path': name, 'role': 'image', 'primary': True, 'required': True},
                     *({'path': spec['directory'] + '/' + path, 'role': role, 'primary': False, 'required': False}
                       for path, role in spec['auxiliary'])]}
        refs = [{'target': grants.file_target(3, ref['path'], 'nfs'),
                 **{key: ref[key] for key in ('role', 'primary', 'required')}} for ref in model['composition']]
        refs.sort(key=lambda ref: json.dumps(ref['target'], ensure_ascii=False, separators=(',', ':')))
        composition = {'format': 'tiff', 'source_layout': 'multi', 'refs': refs}
        facts = {'schema_version': 'meta.data_item_authorization_facts/v1', 'engine_id': '3',
                 'item_fingerprint': model['fingerprint'], 'full_name': name, 'item_type': 'file',
                 'basis_state': 'complete', 'boundary_hash': 'b' * 64, 'observed_at': '2026-11-13T03:00:00Z',
                 'boundary': {'layout': 'multi', 'file_composition': composition}}
        issuer = mock.Mock()
        def request(method, path, statuses, body=None):
            if method == 'GET': return SimpleNamespace(payload=facts)
            return SimpleNamespace(payload={**body, 'engine_id': '3', 'approval_mode': 'independent',
                'operator_principal_id': '11', 'revocation': None, 'boundary_hash': facts['boundary_hash'],
                'file_composition': composition, 'expires_at': None})
        issuer.request.side_effect = request
        with mock.patch.object(grants, 'exact_user_principal', side_effect=['11', '22']):
            auth = grants.ModelSourceAuthorization(issuer, mock.Mock(),
                {'tenant_id': 4, 'engine_id': 3, 'principal_id': '22'}, source_type='nfs')
        auth.grant(model)
        command = issuer.request.call_args.args[3]
        self.assertEqual(command['expected_boundary_hash'], facts['boundary_hash'])
        self.assertEqual(command['item_fingerprint'], facts['item_fingerprint'])
        self.assertNotIn('file_composition', command)
        original = copy.deepcopy(facts)
        for role in ('header', 'world_file', 'auxiliary_metadata'):
            for mutation in ('missing', 'required', 'primary'):
                facts.clear(); facts.update(copy.deepcopy(original))
                current = facts['boundary']['file_composition']['refs']
                ref = next(ref for ref in current if ref['role'] == role)
                if mutation == 'missing': current.remove(ref)
                else: ref[mutation] = True
                count = sum(call.args[0] == 'POST' for call in issuer.request.call_args_list)
                with self.subTest(role=role, mutation=mutation), self.assertRaises(grants.SuiteError): auth.grant(model)
                self.assertEqual(sum(call.args[0] == 'POST' for call in issuer.request.call_args_list), count)

    def test_tiff_compositions_complete_reads_revocation_and_failure_cleanup(self):
        grants = importlib.import_module('scripts.test.ftp-consumer-flow-online')
        for profile, spec in IMG.TIFF_COMPOSITIONS.items():
            raw = {spec['single']: b'TIFF' * 32, spec['multi']: b'TIFF' * 32,
                   **{name: role.encode() * 32 for name, role in spec['auxiliary']}}
            source = {'primary_bytes_equal': True, 'files': {
                name: {'base64': base64.b64encode(data).decode(), 'size_bytes': len(data),
                       'sha256': hashlib.sha256(data).hexdigest()} for name, data in raw.items()},
                'proofs': {'single': {'invalid_mask_pixels': 1}, 'multi': {'invalid_mask_pixels': spec['invalid_mask_pixels']}}}
            scan_faults = {'missing_file', 'extra_file', 'extra_directory', 'unknown_files', 'unknown_items',
                           'extra_logical_item', 'incomplete_items', 'failed_item', 'physical_as_logical',
                           'missing_progress', 'wrong_actor', 'wrong_scan_status', 'unfinished_scan', 'integer_known_flag'}
            for fault in (None, 'range_bytes', 'revoked', 'generation', 'cleanup', 'source_changed', 'extra_meta', 'browser', 'opaque_mask', 'shifted_grid', *sorted(scan_faults)):
                with self.subTest(profile=profile, fault=fault), tempfile.TemporaryDirectory() as temporary:
                    root = Path(temporary); archive = root / 'archive'; archive.mkdir()
                    env = {'ADDP_ONLINE_TEST_RUN_ID': 'composition-1', 'ADDP_ONLINE_ARTIFACT_DIR': str(archive), 'ADDP_ONLINE_MANAGER_NFS_ENGINE_ID': '3',
                           'ADDP_ONLINE_TEST_TENANT_ID': '4', 'GATEWAY_URL': 'http://fixture',
                           'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN': 'issuer'}
                    auth = mock.Mock(operator='11', recipient='22', base='/api/v1/system/engines/3')
                    auth.target.side_effect = lambda path: grants.file_target(3, path, 'nfs')
                    active, commands, models = set(), [], []
                    cleaned = False
                    def grant(model, *, action='read'):
                        command = f'{action}-{model["layout"]}'; commands.append(command)
                        if action == 'read': active.add(model['layout'])
                        return {'request_id': command}
                    auth.grant.side_effect = grant
                    def facts(model):
                        models.append(copy.deepcopy(model))
                        return {'full_name': model['full_name'], 'boundary': {'layout': model['layout']},
                                'boundary_hash': 'a' * 64}
                    auth.facts.side_effect = facts
                    def cleanup():
                        nonlocal cleaned
                        cleaned = True
                        if fault == 'cleanup': raise OWNER.SuiteError('grant cleanup failed')
                        active.clear()
                    auth.cleanup.side_effect = cleanup
                    items = [{'id': index, 'full_name': spec['directory'] + '/' + name, 'item_type': 'file',
                              'fingerprint': str(index) * 64} for index, name in enumerate((spec['single'], spec['multi']), 1)]
                    issuer = mock.Mock()
                    def issuer_request(method, path, statuses):
                        if '/executions/' in path:
                            payload = {'status': 'success', 'triggered_by': 11, 'metadata': {'items_scanned': 2},
                                       'scan_progress': {'phase': 'finalizing', 'files_discovered': len(raw),
                                           'directories_discovered': 1, 'files_total_known': True,
                                           'items_planned': 2, 'items_total_known': True, 'items_completed': 2,
                                           'items_succeeded': 2, 'items_failed': 0}}
                            for label, field, value in (
                                    ('missing_file', 'files_discovered', len(raw)-1), ('extra_file', 'files_discovered', len(raw)+1),
                                    ('extra_directory', 'directories_discovered', 2), ('unknown_files', 'files_total_known', False),
                                    ('unknown_items', 'items_total_known', False), ('extra_logical_item', 'items_planned', 3),
                                    ('incomplete_items', 'items_completed', 1), ('failed_item', 'items_failed', 1),
                                    ('unfinished_scan', 'phase', 'processing'), ('integer_known_flag', 'files_total_known', 1)):
                                if fault == label: payload['scan_progress'][field] = value
                            if fault == 'missing_progress': payload.pop('scan_progress')
                            if fault == 'wrong_actor': payload['triggered_by'] = 22
                            if fault == 'wrong_scan_status': payload['status'] = 'failed'
                            if fault == 'physical_as_logical': payload['metadata']['items_scanned'] = len(raw)
                        elif '/data-items?' in path:
                            payload = {'total': 3 if fault == 'extra_meta' else 2,
                                       'data': [{'full_name': item['full_name'], 'item_fingerprint': item['fingerprint']} for item in items]}
                        else: payload = {'total': 0, 'data': []}
                        return OWNER.Response(200, payload, {})
                    issuer.request.side_effect = issuer_request
                    client = mock.Mock()
                    def request(method, path, statuses, **kwargs):
                        status, = statuses
                        if '/storage-stream?' in path:
                            query = parse_qs(urlsplit(path).query); member = query['storage_ref'][0]
                            data = raw[Path(member).name]
                            self.assertTrue((root / member).is_file(), 'revocation must be tested before source removal')
                            layout = 'single' if spec['single'] in query['locator'][0] else 'multi'
                            if status == 206:
                                self.assertIn(layout, active)
                                body = b'bad' if fault == 'range_bytes' else data
                                return OWNER.Response(206, {}, {'Content-Range': f'bytes 0-{len(data)-1}/{len(data)}'}, body)
                            if status == 403:
                                self.assertNotIn(layout, active)
                                if cleaned and fault == 'revoked': return OWNER.Response(206, {}, {}, data)
                            return OWNER.Response(status, {'error': 'denied'}, {})
                        if '/meta/items/' in path:
                            return OWNER.Response(200, {'attributes': {'item': {'layout': 'single' if path.endswith('/1') else 'multi'}}}, {})
                        return OWNER.Response(200, {}, {}, b'COG')
                    client.request.side_effect = request
                    def generate(client, fixture, tenant, timeout, physical, **kwargs):
                        self.assertEqual(active, {'single', 'multi'})
                        fixture.task_id = 9; fixture.execution_id = 'execution'; fixture.result_id = 10
                        fixture.artifact = {'physical': {'mask_verified': True, 'grid_verified': True, 'invalid_mask_pixels': spec['invalid_mask_pixels']},
                                            'physical_request': {'source_case': 'msk'}, 'preview_url': '/cog/content',
                                            'size_bytes': 3, 'sha256': hashlib.sha256(b'COG').hexdigest()}
                        if fault == 'generation': raise OWNER.SuiteError('original generation failure')
                        if fault == 'source_changed': (root / spec['directory'] / spec['auxiliary'][-1][0]).write_bytes(b'changed')
                    physical = mock.Mock(side_effect=lambda action, request: source if action == profile + '-source' else {
                        'object_deleted': True, 'source_unchanged': True, 'residual_objects': 0})
                    def browser(evidence):
                        self.assertFalse(cleaned, 'browser must run before grant cleanup')
                        self.assertEqual(active, {'single', 'multi'})
                        result = composition_browser_report(evidence)
                        if fault == 'browser': raise OWNER.SuiteError('original browser failure')
                        if fault == 'opaque_mask': result['renders'][1]['pixels'][0]['rgba'][3] = 255 if profile == 'msk' else 0
                        if fault == 'shifted_grid': result['renders'][0]['extent'][0] += .1
                        return result
                    report = {}
                    with mock.patch.object(IMG, 'nfs_tiff_root', return_value=root), \
                         mock.patch.object(grants, 'ModelSourceAuthorization', return_value=auth), \
                         mock.patch.object(OWNER, 'GatewayClient', return_value=issuer), \
                         mock.patch.object(OWNER, 'wait_for_meta_scan', return_value='scan'), \
                         mock.patch.object(OWNER, 'find_fixture_item', side_effect=lambda client, engine, name, label: next(i for i in items if i['full_name'] == name)), \
                         mock.patch.object(OWNER, 'generate_raster_cog', side_effect=generate), \
                         mock.patch.object(OWNER, 'cleanup_managed_artifact') as delete:
                        if fault:
                            with self.assertRaises(OWNER.SuiteError) as error:
                                IMG.verify_nfs_tiff_composition(OWNER, env, client, {'principal_id': '22'}, physical, report, 30, profile=profile, browser=browser)
                            if fault == 'generation': self.assertIn('original generation failure', str(error.exception))
                        else:
                            IMG.verify_nfs_tiff_composition(OWNER, env, client, {'principal_id': '22'}, physical, report, 30, profile=profile, browser=browser)
                    auth.cleanup.assert_called_once()
                    self.assertEqual(report['result'], 'failed' if fault else 'passed')
                    if fault in scan_faults:
                        auth.grant.assert_not_called()
                        delete.assert_not_called()
                        self.assertEqual(models, [])
                    elif fault not in ('range_bytes', 'extra_meta'):
                        delete.assert_called_once()
                    if not fault:
                        self.assertEqual(report['scan']['items_scanned'], 2)
                        self.assertEqual(report['scan']['scan_progress']['files_discovered'], len(raw))
                        self.assertEqual(report['scan']['scan_progress']['items_completed'], 2)
                        self.assertEqual([m['layout'] for m in models], ['single', 'multi'])
                        self.assertEqual(models[1]['composition'][1:], [
                            {'path': spec['directory'] + '/' + name, 'role': role, 'required': False, 'primary': False}
                            for name, role in spec['auxiliary']])
                        self.assertEqual(commands, ['metadata_read-single', 'read-single', 'metadata_read-multi', 'read-multi'])
                        self.assertTrue(report['source_unchanged'] and report['zero_current_grants'] and report['object_deleted'])
                        self.assertEqual([r['status'] for item in report['items'] for r in item['revoked']], [403] * len(raw))
                    if fault in ('cleanup', 'revoked', 'source_changed'): self.assertTrue(report['cleanup_errors'])

    def test_composition_browser_rejects_wrong_identity_grid_alpha_and_cached_result(self):
        for profile in IMG.TIFF_COMPOSITIONS:
            evidence = {'run_id': 'run', 'profile': profile, 'identity': {'principal_id': '22'},
                        'source': {'item_id': 2, 'locator': 'exact', 'result_id': 10, 'preview_url': '/cog', 'size_bytes': 3, 'sha256': 'hash'}}
            good = composition_browser_report(evidence)
            IMG.validate_composition_browser(OWNER, good, evidence)
            mutations = [lambda r: r.update(schema_version='old'), lambda r: r.update(run_id='other'),
                lambda r: r.update(identity={}), lambda r: r['source'].update(result_id=11),
                lambda r: r.update(range_status=200), lambda r: r.update(generation_requests=1),
                lambda r: r.update(warnings_errors=1), lambda r: r.update(failed_business_responses=1),
                lambda r: r.update(reopened=False), lambda r: r.update(reused_result=False),
                lambda r: r['renders'].pop(), lambda r: r['renders'][0].update(projection='EPSG:3857'),
                lambda r: r['renders'][0]['extent'].__setitem__(0, 114),
                lambda r: r['renders'][0]['extent'].__setitem__(0, float('nan')),
                lambda r: r['renders'][1]['pixels'].pop(), lambda r: r['renders'][1]['pixels'][1].update(in_canvas=False),
                lambda r: r['renders'][1]['pixels'][1]['rgba'].__setitem__(3, 0),
                lambda r: r['renders'][1]['pixels'][-1]['rgba'].__setitem__(3, 209),
                lambda r: r['renders'][1]['pixels'][0]['rgba'].__setitem__(3, 209 if profile == 'msk' else 0)]
            for index, mutate in enumerate(mutations):
                bad = copy.deepcopy(good); mutate(bad)
                with self.subTest(profile=profile, mutation=index), self.assertRaises(OWNER.SuiteError):
                    IMG.validate_composition_browser(OWNER, bad, evidence)

    def test_browser_locators_match_actual_chinese_and_english_labels(self):
        import re
        root=Path(__file__).resolve().parents[2]
        script=(root/'console/frontend/e2e/online/manager-img.spec.js').read_text()
        locators=re.findall(r"getByRole\('(tab|button)', \{ name: (.*?), exact: true \}\)",script)
        for locale in ('zh-cn','en'):
            labels=json.loads((root/f'manager/frontend/src/i18n/{locale}.json').read_text())['manager']
            expected={'tab':[labels['explorer']['attributesTab'],labels['explorer']['previewTab']],
                      'button':[labels['spatialPreview']['generateRasterCOG'],labels['vectorTile']['fitExtent'],labels['explorer']['downloadPage']]}
            for role,names in expected.items():
                for name in names:
                    patterns=[value for kind,value in locators if kind==role]
                    matches=[re.fullmatch(value[1:-1],name) if value.startswith('/') else value.strip("'\"")==name for value in patterns]
                    self.assertTrue(any(matches),(locale,role,name,patterns))

    def test_real_preview_response_matcher_accepts_grd_without_capability_request(self):
        import re,subprocess
        root=Path(__file__).resolve().parents[2]
        script=(root/'console/frontend/e2e/online/manager-img.spec.js').read_text()
        function=re.search(r'export function matchesSourcePreviewResponse\(response, locator\) \{.*?\n\}',script,re.S).group(0)
        program="import assert from 'node:assert/strict';\n"+function.replace('export ','',1)+"""
const locator='addp://engine/5/path/bucket/raster/grd-v1.grd?type=object&item_id=71'
const response=(path,source=locator,method='GET')=>({
  url:()=>`http://gateway${path}?page=1&locator=${encodeURIComponent(source)}&page_size=20`,
  request:()=>({method:()=>method})})
// GRD's actual UI emits a preview response, but has no quick-view target.
assert.equal(matchesSourcePreviewResponse(response('/api/v1/manager/preview'),locator),true)
for(const candidate of [response('/api/v1/manager/quick-view/capability'),
 response('/api/v1/meta/items/9'),response('/api/v1/manager/preview','other'),
 response('/api/v1/manager/preview',locator.replace('item_id=71','item_id=72')),
 response('/api/v1/manager/preview',locator,'POST')])
 assert.equal(matchesSourcePreviewResponse(candidate,locator),false)
for(const extension of ['img','jp2','j2k']) {
 const source=locator.replace('grd-v1.grd','source.'+extension)
 assert.equal(matchesSourcePreviewResponse(response('/api/v1/manager/preview',source),source),true)
}
"""
        result=subprocess.run(['node','--input-type=module','-e',program],capture_output=True,text=True)
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertIn('page.waitForResponse(response => matchesSourcePreviewResponse(response, source.locator))',script)

    def test_meta_accepts_located_and_unknown_facts_but_rejects_false_claims(self):
        for spec in (*IMG.SOURCES, *IMG.JPEG2000_SOURCES, *IMG.GRD_SOURCES):
            good = detail(spec)
            IMG.validate_meta(OWNER, good, spec)
            mutations = [lambda d: d['attributes']['format_info'][spec['source_format']].update(scan_complete=False),
                         lambda d: d['attributes']['type_info']['media'].update(encoding='GTiff'),
                         lambda d: d['attributes']['item'].update(format='tiff')]
            if spec['source_srid']:
                mutations.append(lambda d: d['attributes']['capabilities']['spatial'].update(srid=3857))
            else:
                mutations.append(lambda d: d['attributes'].update(capabilities={'spatial': {'srid': 4326}}))
            for mutate in mutations:
                bad = copy.deepcopy(good); mutate(bad)
                with self.assertRaises(OWNER.SuiteError): IMG.validate_meta(OWNER, bad, spec)

    def test_browser_requires_each_source_real_colors_range_reopen_and_no_generation(self):
        evidence = {'source_format':'img', 'run_id': 'run', 'identity': {'principal_id': '3'},
                    'cases': [{**spec, 'item_id': i, 'locator': f'item:{i}', 'sha256': 'a' * 64,
                               'preview_url': '/api/v1/manager/raster_cog/1/content'} for i, spec in enumerate(IMG.SOURCES, 1)]}
        report = browser_report(evidence)
        IMG.validate_browser(OWNER, report, evidence)
        for mutate in (
            lambda r: r.update(generation_requests=1), lambda r: r['cases'][0].pop('source_preview_verified'),
            lambda r: r['cases'][0].update(source_preview_verified=False),
            lambda r: r.update(schema_version='addp.manager-raster-media-browser/v2'), lambda r: r.update(schema_version='old'),
            lambda r: r['cases'][0].update(range_loaded=False), lambda r: r['cases'][1].update(reused_result=False),
            lambda r: r['cases'][1]['pixels'].update(cyan=0), lambda r: r['cases'][2].update(map_absent=False),
            lambda r: r['cases'][2].update(source_image_absent=False), lambda r: r['cases'][2].pop('unsupported_message_visible'),
            lambda r: r['cases'][0].update(download_sha256='wrong'), lambda r: r.update(cases=r['cases'][:2]),
        ):
            bad = copy.deepcopy(report); mutate(bad)
            with self.assertRaises(OWNER.SuiteError): IMG.validate_browser(OWNER, bad, evidence)

    def test_unlocated_jp2_and_j2k_require_unsupported_renderer_evidence(self):
        evidence = {'source_format': 'jpeg2000', 'run_id': 'run', 'identity': {},
                    'cases': [{**spec, 'item_id': i, 'locator': f'item:{i}', 'sha256': 'a' * 64,
                               'preview_url': '/api/v1/manager/raster_cog/1/content'} for i, spec in enumerate(IMG.JPEG2000_SOURCES)]}
        good = browser_report(evidence)
        IMG.validate_browser(OWNER, good, evidence)
        for index in (2, 3):
            for field in ('source_image_absent', 'unsupported_message_visible'):
                for missing in (False, True):
                    bad = copy.deepcopy(good)
                    if missing: bad['cases'][index].pop(field)
                    else: bad['cases'][index][field] = False
                    with self.assertRaises(OWNER.SuiteError): IMG.validate_browser(OWNER, bad, evidence)
        bad = copy.deepcopy(good); bad['schema_version'] = 'addp.manager-raster-media-browser/v1'
        with self.assertRaises(OWNER.SuiteError): IMG.validate_browser(OWNER, bad, evidence)

    def scenario(self, root, *, fail_browser=False, fail_delete=False, wrong_task_format=False, source_format="img"):
        sources = {"img": IMG.SOURCES, "jpeg2000": IMG.JPEG2000_SOURCES, "grd":IMG.GRD_SOURCES,'tiff':IMG.TIFF_SOURCES}[source_format]
        items = [detail(spec, i) for i, spec in enumerate(sources, 1)]
        seed = {'source_unchanged': True, source_format+'_sources': [{**{k: s[k] for k in ('case', 'object', 'source_srid')},
                 'size_bytes': 4, 'sha256': hashlib.sha256(b'test').hexdigest()} for s in sources]}
        if source_format=='tiff':seed['tiff_sources'][0].update(driver='GTiff',width=240,height=120,band_count=1,
            dtype='Float64',nodata_is_nan=True,overview_count=0,block_size=[512,512],cog_valid=True,
            cog_errors=0,cog_warnings=0,pixels_verified=28800,valid_pixels=28765,complete_mask_verified=True)
        generated, cleaned = [], []
        def request(method, path, expected, body=None):
            parsed = urlsplit(path)
            if parsed.path == '/api/v1/meta/engines/2/items': payload = [structural_detail(item) for item in items]
            elif parsed.path=='/api/v1/manager/engines/2/items/refresh':
                self.assertEqual(method,'POST');self.assertIn('item_id=1',parse_qs(parsed.query)['locator'][0])
                items[0]['scanned_at']='2026-10-30T00:00:01Z'
                items[0]['attributes']['storage']['last_modified_at'] = '2026-10-30T07:59:00.691+08:00'
                payload={'status':'success','items_scanned':1,'started_at':'2026-10-30T00:00:00.5Z'}
            elif parsed.path.startswith('/api/v1/meta/items/'):
                payload = structural_detail(items[int(parsed.path.split('/')[-1]) - 1])
            elif parsed.path == '/api/v1/manager/preview':
                locator = parse_qs(parsed.query)['locator'][0]
                payload = preview_payload(next(item for item in items if OWNER.build_item_locator(2, item) == locator))
            elif parsed.path == '/api/v1/manager/downloads/file':
                return OWNER.Response(200, {}, {}, b'test')
            elif parsed.path == '/api/v1/manager/quick-view/capability': payload = {'available_actions': [], 'can_use_quick_view': False}
            elif parsed.path.startswith('/api/v1/manager/tasks/'):
                payload = {'config': {'raster': {'source_format': 'tiff' if wrong_task_format else source_format}}}
            elif parsed.path.endswith('/content'): return OWNER.Response(200, {}, {}, b'test')
            elif parsed.path == '/api/v1/manager/raster_cog':
                task = int(parse_qs(parsed.query)['task_id'][0]);payload = {'total': 1, 'data': [{'id': task + 10, 'last_execution_id': f'exec-{task}'}]}
            else: raise AssertionError((method, path))
            return OWNER.Response(200, copy.deepcopy(payload), {})
        client = SimpleNamespace(request=request)
        def generate(_client, fixture, tenant, timeout, physical, source_facts=None):
            generated.append(fixture.format);fixture.task_id = fixture.item['id'];fixture.result_id = fixture.task_id + 10
            fixture.execution_id = f'exec-{fixture.task_id}'
            fixture.artifact = {'size_bytes':4, 'sha256':hashlib.sha256(b'test').hexdigest(), 'preview_url': f'/api/v1/manager/raster_cog/{fixture.result_id}/content',
                                'physical_request': {'source_case': source_facts['case'], 'locator': 'owned'}}
        def clean(_client, fixture, *args):
            cleaned.append(fixture.format)
            if fail_delete: raise OWNER.SuiteError('owned deletion failed')
            fixture.cleanup.update(result_deleted=True, task_deleted=True, content_unavailable=True, preview_mode_restored=True, residual_resources=0)
        def browser(evidence):
            if fail_browser: raise OWNER.SuiteError('original browser assertion')
            return browser_report(evidence)
        def physical(action, request):
            return copy.deepcopy(seed) if action == source_format+'-source' else {'object_deleted': True, 'source_unchanged': True, 'residual_objects': 0}
        owner = SimpleNamespace(**{key: getattr(OWNER, key) for key in ('SuiteError', '_object', 'positive_int', 'find_fixture_item', 'ArtifactFixture', 'build_item_locator', 'RASTER_TASK_TYPE','validate_model_bundle')},
            GatewayClient=lambda *args: client, validate_user_identity=lambda *args: {'principal_id': '3'},
            generate_raster_cog=generate, cleanup_managed_artifact=clean)
        env = {'ADDP_ONLINE_TEST_TENANT_ID': '4', 'ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID': '2', 'GATEWAY_URL': 'http://127.0.0.1',
               'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'synthetic-unit-token', 'ADDP_ONLINE_TEST_RUN_ID': 'run',
               'ADDP_ONLINE_MANAGER_MINIO_BUCKET': 'bucket', 'ADDP_ONLINE_ARTIFACT_DIR': str(root)}
        return lambda: IMG.run(Path('/repository'), env, owner, browser_runner=browser, physical_runner=physical, source_format=source_format), generated, cleaned

    def test_grd_reads_all_profiles_without_generation_and_preserves_sources(self):
        with tempfile.TemporaryDirectory() as root:
            run, generated, cleaned = self.scenario(root, source_format='grd')
            report=run()
            self.assertEqual(report['result'],'passed')
            self.assertEqual([c['case'] for c in report['cases']],['v1','v2','surfer6','surfer6-ascii'])
            self.assertEqual(generated,[])
            self.assertEqual(cleaned,[])
            self.assertTrue(report['source_unchanged'])
        for spec in IMG.GRD_SOURCES:
            good=detail(spec)
            for key in spec['grid_facts']:
                bad=copy.deepcopy(good)
                bad['attributes']['format_info']['grd'].pop(key)
                with self.assertRaises(OWNER.SuiteError):IMG.validate_meta(OWNER,bad,spec)
            for key,value in (('dtype','Float64' if spec['driver']=='GSBG' else 'Float32'),('nodata',None),('nodata_is_nan',True)):
                bad=copy.deepcopy(good);bad['attributes']['format_info']['grd']['bands'][0][key]=value
                with self.assertRaises(OWNER.SuiteError):IMG.validate_meta(OWNER,bad,spec)
        bad=detail(IMG.GRD_SOURCES[-1]);bad['attributes']['format_info']['grd']['version']=6
        with self.assertRaises(OWNER.SuiteError):IMG.validate_meta(OWNER,bad,IMG.GRD_SOURCES[-1])

    def test_full_orchestration_generates_exactly_two_and_cleans_both(self):
        with tempfile.TemporaryDirectory() as root:
            run, generated, cleaned = self.scenario(root, source_format='jpeg2000')
            report = run()
            self.assertEqual(report['result'], 'passed')
            self.assertEqual(generated, ['jpeg2000-wgs84', 'jpeg2000-utm50n'])
            self.assertEqual(cleaned, list(reversed(generated)))
            self.assertEqual(len(report['cases']), 4)
            self.assertTrue(report['source_unchanged'])

    def test_original_failure_survives_cleanup_failure_and_evidence_is_saved(self):
        with tempfile.TemporaryDirectory() as root:
            run, generated, cleaned = self.scenario(root, fail_browser=True, fail_delete=True)
            with self.assertRaisesRegex(OWNER.SuiteError, 'original browser assertion; IMG cleanup failed:'):
                run()
            self.assertEqual(cleaned, list(reversed(generated)))
            self.assertEqual(json.loads((Path(root) / 'manager-img.json').read_text())['result'], 'failed')

    def test_task_source_format_must_still_be_img_and_failure_cleans_partial_work(self):
        with tempfile.TemporaryDirectory() as root:
            run, generated, cleaned = self.scenario(root, wrong_task_format=True)
            with self.assertRaisesRegex(OWNER.SuiteError, 'canonical source_format'): run()
            self.assertEqual(generated, ['img-wgs84'])
            self.assertEqual(cleaned, generated)

    def test_tiff_exact_refresh_raw_download_and_browser_do_not_generate(self):
        with tempfile.TemporaryDirectory()as root:
            run,generated,cleaned=self.scenario(root,source_format='tiff');report=run()
            self.assertEqual(report['result'],'passed');self.assertEqual(generated,[]);self.assertEqual(cleaned,[])
            self.assertTrue(report['source_unchanged']);case=report['cases'][0]
            self.assertEqual(case['refresh']['response']['items_scanned'],1)
            self.assertEqual(case['refresh']['scanned_at_after'],'2026-10-30T00:00:01Z')
            self.assertEqual(case['sha256'],hashlib.sha256(b'test').hexdigest())
            self.assertEqual(Path(root,'raster-source-tiff-unlocated-small-cog.tif').read_bytes(),b'test')
            evidence={'source_format':'tiff','run_id':'run','identity':report['identity'],'cases':report['cases']}
            for fault in ['map_absent','source_image_absent','range_loaded','tiff_pixels_verified','tiff_pages_verified',
                          'cog_metadata_visible','zero_overviews_verified','refreshed_metadata_verified','reopened']:
                bad=copy.deepcopy(report['browser']);bad['cases'][0][fault]=False
                with self.subTest(fault=fault),self.assertRaises(OWNER.SuiteError):IMG.validate_browser(OWNER,bad,evidence)
            for mutate in (lambda d:d.update(schema_version='addp.manager-raster-media-browser/v3'),
                           lambda d:d['cases'][0].pop('download_sha256'),
                           lambda d:d['cases'][0].update(scanned_at='old'),lambda d:d.update(generation_requests=1)):
                bad=copy.deepcopy(report['browser']);mutate(bad)
                with self.assertRaises(OWNER.SuiteError):IMG.validate_browser(OWNER,bad,evidence)

    def test_tiff_rejects_old_hint_unknown_level_overviews_and_fabricated_location(self):
        good=detail(IMG.TIFF_SOURCES[0]);IMG.validate_tiff_meta(OWNER,good)
        for mutate in (lambda a:a['format_info']['tiff'].update(profile='plain_tiff'),
                       lambda a:a['format_info']['tiff'].update(is_cloud_optimized=False),
                       lambda a:a['format_info']['tiff'].pop('is_cloud_optimized'),
                       lambda a:a['format_info']['tiff'].pop('cog_check_level'),
                       lambda a:a['format_info']['tiff'].update(has_overviews=True),
                       lambda a:a['type_info']['media'].update(width=241),
                       lambda a:a.update(capabilities={'spatial':{'srid':4326}}),
                       lambda a:a['type_info'].update(spatial={'srid':4326})):
            bad=copy.deepcopy(good);mutate(bad['attributes'])
            with self.assertRaises(OWNER.SuiteError):IMG.validate_tiff_meta(OWNER,bad)

    def test_tiff_physical_and_synchronous_refresh_proof_cannot_be_partial(self):
        spec=IMG.TIFF_SOURCES[0];good=detail(spec)
        response={'status':'success','items_scanned':1,'started_at':'2026-10-30T00:00:00.5Z'}
        after=copy.deepcopy(good);after['scanned_at']='2026-10-30T00:00:01Z'
        after['attributes']['storage']['last_modified_at'] = '2026-10-30T07:59:00.691+08:00'
        model=OWNER.ArtifactFixture('tiff',good,'item:1')
        for mutate in (lambda r,a:r.update(status='failed'),lambda r,a:r.update(items_scanned=2),
                       lambda r,a:r.update(items_scanned=True),lambda r,a:r.pop('started_at'),
                       lambda r,a:a.update(scanned_at=good['scanned_at']),lambda r,a:a.update(id=2),
                       lambda r,a:a.update(full_name='other.tif'),
                       lambda r,a:a['attributes']['item'].update(full_name='other.tif'),
                       lambda r,a:a['attributes']['storage'].update(last_modified_at='2026-10-30T07:59:00.692+08:00'),
                       lambda r,a:a['attributes']['storage'].update(last_modified_at='2026-10-29T23:59:00.691'),
                       lambda r,a:a['attributes']['storage'].update(content_hash='a' * 64),
                       lambda r,a:a['attributes']['format_info']['tiff'].update(profile='plain_tiff')):
            r,a=copy.deepcopy(response),copy.deepcopy(after);mutate(r,a)
            def request(method, path, expected):
                return SimpleNamespace(payload=r if method=='POST' else preview_payload(a)
                    if path.startswith('/api/v1/manager/preview?') else structural_detail(a))
            client=SimpleNamespace(request=request)
            content = {key: good[key] for key in ('attributes', 'size_bytes')}
            with tempfile.TemporaryDirectory() as root, self.assertRaises(OWNER.SuiteError):
                IMG.refresh_tiff(OWNER, client, 2, model, structural_detail(good), content, Path(root))
        with tempfile.TemporaryDirectory()as root:
            run,_,_=self.scenario(root,source_format='tiff');facts=run()['physical_sources'][0]
            for key in ('cog_valid','cog_errors','cog_warnings','pixels_verified','complete_mask_verified','overview_count','source_srid'):
                bad=copy.deepcopy(facts);bad.pop(key)
                with self.assertRaises(OWNER.SuiteError):IMG.validate_tiff_physical(OWNER,bad)

    def test_structural_meta_rejects_native_format_details_and_spatial_extent(self):
        spec = IMG.SOURCES[0]
        good = structural_detail(detail(spec))
        IMG.validate_structural_meta(OWNER, good, spec)
        for mutate in (lambda row: row.update(size_bytes=4),
                       lambda row: row['attributes'].update(format_info={'img': {'band_count': 3}}),
                       lambda row: row['attributes']['capabilities']['spatial'].update(extent=spec['extent'])):
            bad = copy.deepcopy(good)
            mutate(bad)
            with self.assertRaises(OWNER.SuiteError):
                IMG.validate_structural_meta(OWNER, bad, spec)


if __name__ == '__main__':
    unittest.main()
