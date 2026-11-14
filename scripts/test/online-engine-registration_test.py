import importlib.util
import json
import hashlib
import stat
import sys
import tempfile
import unittest
import urllib.parse
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock, patch


SCRIPT = Path(__file__).with_name("online-engine-registration.py")
SPEC = importlib.util.spec_from_file_location("online_engine_registration", SCRIPT)
REGISTRATION = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = REGISTRATION
SPEC.loader.exec_module(REGISTRATION)


class OnlineEngineRegistrationTest(unittest.TestCase):
    def test_native_metadata_preview_uses_exact_namespace_and_cleans_up_on_failure(self):
        for namespace_term in ('schema', 'database'):
            for outcome in ('success', 'browser_failure', 'wrong_report', 'missing_item'):
                with self.subTest(namespace_term=namespace_term, outcome=outcome), tempfile.TemporaryDirectory() as temp:
                    target = REGISTRATION.ExactRecordGrants.target(9, 'business', 'source', 'table', namespace_term)
                    self.assertEqual(target['segments'][1]['term'], namespace_term)
                    receipt = dict(catalog_path=target, item_fingerprint='a' * 64)
                    grants = Mock()
                    grants.grant.return_value = receipt
                    record = dict(id=7, full_name='business.source', fingerprint='a' * 64, item_type='table')
                    consumer, preparer = Mock(), Mock()
                    consumer.request.side_effect = [SimpleNamespace(payload={}),
                        SimpleNamespace(payload=[] if outcome == 'missing_item' else [record]),
                        SimpleNamespace(payload={'principal': {'id': '33'}, 'authorization': {
                            'role_assignments': [{'permissions': ['manager.data_item.read', 'meta.catalog.read']}]}})]
                    expected = dict(locator='addp://engine/9/path/business/source?type=table&item_id=7', engine_id=9,
                        tenant_id='2', principal_id='33', permissions=['manager.data_item.read', 'meta.catalog.read'])
                    def browser(*args, env, **kwargs):
                        self.assertNotIn('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN', env)
                        self.assertNotIn('ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN', env)
                        self.assertNotIn('ADDP_ONLINE_POOL_OPERATOR_USERNAME', env)
                        self.assertNotIn('ADDP_ONLINE_POOL_OPERATOR_PASSWORD', env)
                        self.assertEqual(json.loads(env['ADDP_ONLINE_NATIVE_PREVIEW_EXPECTATIONS']), expected)
                        report = dict(expected, preview_status=200 if outcome == 'wrong_report' else 403, rows_visible=False)
                        Path(env['ADDP_ONLINE_NATIVE_PREVIEW_REPORT']).write_text(json.dumps(report))
                        return SimpleNamespace(returncode=1 if outcome == 'browser_failure' else 0)
                    with patch.dict(REGISTRATION.os.environ, ADDP_ONLINE_ARTIFACT_DIR=temp,
                            ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN='preparer', ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN='provisioner',
                            ADDP_ONLINE_POOL_OPERATOR_USERNAME='operator', ADDP_ONLINE_POOL_OPERATOR_PASSWORD='operator-private'), \
                         patch.object(REGISTRATION, 'ExactRecordGrants', return_value=grants) as lifecycle, \
                         patch.object(REGISTRATION.subprocess, 'run', side_effect=browser) as run:
                        operation = lambda: REGISTRATION.verify_native_preview_metadata_only(
                            preparer, consumer, 2, 9, namespace_term, 'business', 'source', on_change=None)
                        if outcome == 'success':
                            self.assertEqual(operation(), dict(expected, preview_status=403, rows_visible=False))
                        else:
                            with self.assertRaises(REGISTRATION.RegistrationError): operation()
                        lifecycle.assert_called_once_with(preparer, consumer, 2, action='metadata_read', scan_preparation=True, on_change=None)
                        grants.grant.assert_called_once_with(9, 'business', 'source', 'table', namespace_term=namespace_term)
                        grants.cleanup.assert_called_once_with()
                        self.assertEqual(run.call_count, 0 if outcome == 'missing_item' else 1)
                        self.assertEqual(consumer.request.call_args_list[0].args[2:], ((403,), {'targets': [target]}))

    def test_scan_preparation_requires_exact_explicit_permissions(self):
        for scan_preparation, extra, accepted in (
                (False, set(), True),
                (False, {'meta.scan_task.execute', 'meta.scan_task.read'}, False),
                (True, set(), False),
                (True, {'meta.scan_task.execute'}, False),
                (True, {'meta.scan_task.execute', 'meta.scan_task.read'}, True),
                (True, {'meta.scan_task.execute', 'meta.scan_task.read', 'system.engine.create'}, False)):
            with self.subTest(scan_preparation=scan_preparation, extra=extra):
                current, issuer, consumer = self.grant_clients()
                current['authorization']['role_assignments'][0]['permissions'] += sorted(extra)
                if accepted:
                    self.assertEqual(REGISTRATION.source_grant_participants(
                        issuer, consumer, 2, scan_preparation=scan_preparation), ('8', '7'))
                else:
                    with self.assertRaises(REGISTRATION.RegistrationError):
                        REGISTRATION.source_grant_participants(issuer, consumer, 2, scan_preparation=scan_preparation)

    def test_scan_preparation_profile_reaches_both_grant_helpers(self):
        current, issuer, consumer = self.grant_clients()
        current['authorization']['role_assignments'][0]['permissions'] += ['meta.scan_task.execute', 'meta.scan_task.read']
        grants = REGISTRATION.ExactFileMetadataGrants(issuer, consumer, 2, scan_preparation=True)
        self.assertEqual((grants.operator, grants.recipient), ('8', '7'))
        result = REGISTRATION.initialize_exact_record_read_grants(
            issuer, consumer, 2, [(4, 'schema', 'results', 'hdfs_totals')], scan_preparation=True)
        self.assertEqual(len(result), 1)

    def grant_clients(self):
        current = {'principal': {'type': 'user', 'id': '8'},
                   'context': {'type': 'tenant', 'tenant_id': '2'},
                   'authorization': {'role_assignments': [{'role_key': REGISTRATION.SOURCE_INITIALIZER_ROLE, 'permissions': sorted(REGISTRATION.SOURCE_INITIALIZER_PERMISSIONS)}]}}
        consumer_current = {'principal': {'type': 'user', 'id': '7'}, 'context': {'type': 'tenant', 'tenant_id': '2'}}
        authorizer, consumer = Mock(), Mock()
        records = []
        for engine, namespace_term, namespace, name, term in (
                (4, 'schema', 'results', 'hdfs_totals', 'table'),
                (23, 'database', 'business', 'customers', 'table'),
                (29, 'database', 'Outdoor', 'Persons', 'collection')):
            full_name = namespace + '.' + name
            records.append({'engine_id': engine, 'term': term, 'full_name': full_name,
                'fingerprint': hashlib.sha256(f'{engine}:{full_name}'.encode()).hexdigest(),
                'path': {'version': 'catalog.path/v1', 'engine_id': engine, 'segments': [
                    {'term': 'server', 'kind': 'server', 'name': ''},
                    {'term': namespace_term, 'kind': 'namespace', 'name': namespace},
                    {'term': term, 'kind': term, 'name': name}]}})
        granted = set()
        def prepare(method, path, expected, body=None):
            if path == '/api/v1/system/auth/context':
                return SimpleNamespace(payload=current)
            if method == 'GET':
                fingerprint = path.split('/')[-2]
                matches = [item for item in records if item['fingerprint'] == fingerprint]
                if not matches:
                    raise REGISTRATION.RegistrationError('authorization_target_not_found: HTTP 404')
                item = matches[0]
                return SimpleNamespace(payload={'schema_version': 'meta.data_item_authorization_facts/v1',
                    'item_fingerprint': fingerprint, 'engine_id': str(item['engine_id']), 'item_type': item['term'],
                    'full_name': item['full_name'], 'basis_state': 'complete', 'observed_at': '2026-10-16T00:00:00Z',
                    'boundary_hash': 'a' * 64, 'boundary': {'layout': 'single', 'native_target': item['path']}})
            self.assertEqual(expected, (201,))
            granted.add(body['item_fingerprint'])
            return SimpleNamespace(payload=dict(body, engine_id=str(body['catalog_path']['engine_id']), approval_mode='independent', revocation=None, boundary_hash=body['expected_boundary_hash']))
        def read(method, path, expected, body=None):
            if path == '/api/v1/system/auth/context':
                return SimpleNamespace(payload=consumer_current)
            if method == 'GET':
                engine = int(path.split('/')[5])
                namespace = urllib.parse.unquote(path.split('branch=')[1])
                items = [item for item in records if item['engine_id'] == engine and item['path']['segments'][1]['name'] == namespace]
                if not items or not any(item['fingerprint'] in granted for item in items):
                    raise REGISTRATION.RegistrationError('recipient Meta branch hidden: HTTP 404')
                return SimpleNamespace(payload=[{'full_name': item['full_name'], 'item_type': item['term'],
                    'fingerprint': item['fingerprint']} for item in items if item['fingerprint'] in granted])
            self.assertEqual(path, '/api/v1/system/engine-access/read-observations/current-source')
            self.assertEqual(len(body['targets']), 1)
            target = body['targets'][0]
            self.assertEqual(expected, (200,) if any(item['path'] == target and item['fingerprint'] in granted for item in records) else (403,))
            return SimpleNamespace(payload={'observed_at': '2026-10-10T00:00:00Z'} if expected == (200,) else {})
        authorizer.request.side_effect, consumer.request.side_effect = prepare, read
        return current, authorizer, consumer

    def metadata_clients(self, engine=14, full_name='grib/weather.grb2', storage='nfs',
                         data_type='container', format_name='grib', layout='single'):
        current, issuer, consumer = self.grant_clients()
        target = REGISTRATION.ExactFileMetadataGrants.target(engine, full_name, storage, layout=layout)
        fingerprint = hashlib.sha256(f'{engine}:{full_name}'.encode()).hexdigest()
        metadata = dict(target=target, layout=layout, data_type=data_type, format=format_name, composition_digest='b'*64)
        facts = dict(schema_version='meta.data_item_authorization_facts/v1', engine_id=str(engine), item_fingerprint=fingerprint,
            item_type='object' if storage == 'minio' else 'file', full_name=full_name, basis_state='complete', boundary_hash='a'*64,
            observed_at='2026-11-03T00:00:00Z', boundary=dict(layout=layout,metadata_target=metadata))
        issuer_identity, consumer_identity = issuer.request.side_effect, consumer.request.side_effect
        state = dict(visible=False, events=[])
        def authorize(method, path, expected, body=None):
            if path.endswith('/auth/context'):return issuer_identity(method,path,expected,body)
            state['events'].append((method,path,body))
            if path.endswith('/authorization-facts'):return SimpleNamespace(payload=facts)
            if path.endswith('/revoke'):
                state['visible']=False
                return SimpleNamespace(payload={})
            self.assertEqual((method,expected),('POST',(201,)))
            self.assertEqual(body['action'],'metadata_read')
            self.assertEqual(body['catalog_path'],target)
            self.assertFalse(state['visible'])
            state['visible']=True
            return SimpleNamespace(payload=dict(body,engine_id=str(engine),boundary_hash='a'*64,metadata_target=metadata,
                approval_mode='independent',operator_principal_id='8',revocation=None,expires_at=body.get('expires_at')))
        def observe(method,path,expected,body=None):
            if path.endswith('/auth/context'):return consumer_identity(method,path,expected,body)
            self.assertEqual((method,path,expected),('POST','/api/v1/system/engine-access/metadata-checks/meta-query',(200,)))
            self.assertEqual(body,dict(targets=[target]))
            state['events'].append(('observe',state['visible'],None))
            return SimpleNamespace(payload=dict(observed_at='2026-11-03T00:00:00Z',targets=[dict(target=target,visible=state['visible'])]))
        issuer.request.side_effect, consumer.request.side_effect = authorize, observe
        return issuer,consumer,facts,state

    def test_exact_metadata_grant_keeps_explicit_bounded_expiry_and_revokes(self):
        from datetime import datetime, timedelta, timezone
        expiry=(datetime.now(timezone.utc)+timedelta(seconds=3600)).isoformat()
        issuer,consumer,_,state=self.metadata_clients()
        grants=REGISTRATION.ExactFileMetadataGrants(issuer,consumer,2)
        receipt=grants.grant(14,'grib/weather.grb2','nfs',data_type='container',format_name='grib',layout='single',expires_at=expiry)
        self.assertEqual(receipt['expiry_mode'],'at_time')
        self.assertEqual(receipt['expires_at'],expiry)
        self.assertEqual(grants.cleanup(),{'grants_revoked':True,'metadata_hidden':True})
        for bad in ('invalid','2000-01-01T00:00:00+00:00',
                    (datetime.now(timezone.utc)+timedelta(seconds=10801)).isoformat()):
            with self.subTest(bad=bad),self.assertRaises(REGISTRATION.RegistrationError):
                grants.grant(14,'grib/weather.grb2','nfs',data_type='container',format_name='grib',layout='single',expires_at=bad)

    def test_metadata_grant_exact_facts_hidden_visible_then_revoked(self):
        issuer,consumer,_,state=self.metadata_clients()
        grants=REGISTRATION.ExactFileMetadataGrants(issuer,consumer,2)
        receipt=grants.grant(14,'grib/weather.grb2','nfs',data_type='container',format_name='grib',layout='single')
        self.assertEqual(receipt['action'],'metadata_read')
        self.assertEqual([e[1] for e in state['events'] if e[0]=='observe'],[False,True])
        self.assertEqual(grants.cleanup(),dict(grants_revoked=True,metadata_hidden=True))
        self.assertFalse(state['visible']);self.assertFalse(grants.pending)
        self.assertEqual([e[1] for e in state['events'] if e[0]=='observe'],[False,True,False])
        self.assertFalse(any('/manager/' in e[1] for e in state['events'] if e[0] in {'GET','POST'}))

    def test_hdfs_and_minio_whole_metadata_grants_keep_exact_item_shapes_and_journal(self):
        for engine, name, storage, fmt, layout in (
            (17, 'samples/orders.csv', 'hdfs', 'csv', 'single'),
            (20, 'result/results/hdfs_totals/one-result', 'minio', 'parquet', 'whole'),
        ):
            with self.subTest(storage=storage):
                issuer, consumer, _, state = self.metadata_clients(engine, name, storage, 'table', fmt, layout)
                snapshots = []
                grants = REGISTRATION.ExactFileMetadataGrants(issuer, consumer, 2,
                    on_change=lambda rows: snapshots.append(json.loads(json.dumps(rows))))
                receipt = grants.grant(engine, name, storage, data_type='table', format_name=fmt, layout=layout)
                segments = receipt['catalog_path']['segments']
                self.assertEqual(segments[-1]['term'], 'prefix' if layout == 'whole' else 'file')
                self.assertEqual(receipt['action'], 'metadata_read')
                self.assertFalse(snapshots[0][0]['confirmed'])
                self.assertTrue(snapshots[1][0]['confirmed'])
                grants.cleanup()
                self.assertEqual(snapshots[-1], [])
                self.assertEqual([e[1] for e in state['events'] if e[0] == 'observe'], [False, True, False])

    def test_pending_metadata_grant_can_be_recovered_after_lost_write_response(self):
        issuer, consumer, _, state = self.metadata_clients(17, 'samples/orders.csv', 'hdfs', 'table', 'csv')
        original, durable = issuer.request.side_effect, []
        def lose_response(method, path, expected, body=None):
            response = original(method, path, expected, body)
            if method == 'POST' and path.endswith('/access_grants'):
                raise TimeoutError('response lost after actual write')
            return response
        issuer.request.side_effect = lose_response
        grants = REGISTRATION.ExactFileMetadataGrants(issuer, consumer, 2,
            on_change=lambda rows: durable.append(json.loads(json.dumps(rows))))
        with self.assertRaises(TimeoutError):
            grants.grant(17, 'samples/orders.csv', 'hdfs', data_type='table', format_name='csv', layout='single')
        self.assertTrue(state['visible'])
        self.assertEqual(len(durable), 1)
        self.assertFalse(durable[0][0]['confirmed'])
        recovered = REGISTRATION.ExactFileMetadataGrants(issuer, consumer, 2)
        recovered.pending = durable[0]
        self.assertEqual(recovered.cleanup(), dict(grants_revoked=True, metadata_hidden=True))
        self.assertFalse(state['visible'])

    def test_failed_metadata_checkpoint_prevents_a_grant_write(self):
        issuer, consumer, _, state = self.metadata_clients()
        grants = REGISTRATION.ExactFileMetadataGrants(issuer, consumer, 2,
            on_change=Mock(side_effect=OSError('checkpoint unavailable')))
        with self.assertRaises(OSError):
            grants.grant(14, 'grib/weather.grb2', 'nfs', data_type='container', format_name='grib', layout='single')
        self.assertFalse(state['visible'])
        self.assertFalse(any(e[0] == 'POST' for e in state['events']))

    def test_metadata_grant_rejects_untrusted_or_content_boundary_before_write(self):
        for mutation in ('fingerprint','identity','composition','content','missing','scope','null_boundary','null_metadata','boundary_shape','metadata_shape'):
            with self.subTest(mutation=mutation):
                issuer,consumer,facts,state=self.metadata_clients()
                if mutation=='fingerprint':facts['item_fingerprint']='c'*64
                elif mutation=='identity':facts['boundary']['metadata_target']['format']='netcdf'
                elif mutation=='composition':facts['boundary']['metadata_target']['composition_digest']='invalid'
                elif mutation=='content':facts['boundary']['file_composition']={'format':'grib'}
                elif mutation=='missing':facts['basis_state']='incomplete'
                elif mutation=='null_boundary':facts['boundary']=None
                elif mutation=='null_metadata':facts['boundary']['metadata_target']=None
                elif mutation=='boundary_shape':facts['boundary']=[]
                elif mutation=='metadata_shape':facts['boundary']['metadata_target']=[]
                else:facts['boundary']['metadata_target']['target']['segments'].pop()
                grants=REGISTRATION.ExactFileMetadataGrants(issuer,consumer,2)
                with self.assertRaises(REGISTRATION.RegistrationError):
                    grants.grant(14,'grib/weather.grb2','nfs',data_type='container',format_name='grib',layout='single')
                self.assertFalse(grants.pending)
                self.assertFalse(any(e[0]=='POST' for e in state['events']))

    def test_metadata_grant_unknown_write_outcome_is_revoked(self):
        issuer,consumer,_,state=self.metadata_clients();original=issuer.request.side_effect
        def lost(method,path,expected,body=None):
            result=original(method,path,expected,body)
            if path.endswith('/access_grants'):raise TimeoutError('lost receipt')
            return result
        issuer.request.side_effect=lost
        grants=REGISTRATION.ExactFileMetadataGrants(issuer,consumer,2)
        with self.assertRaises(TimeoutError):
            grants.grant(14,'grib/weather.grb2','nfs',data_type='container',format_name='grib',layout='single')
        self.assertTrue(state['visible']);self.assertEqual(len(grants.pending),1)
        grants.cleanup();self.assertFalse(state['visible']);self.assertFalse(grants.pending)

    def test_metadata_grant_receipt_content_escalation_rejected_and_cleaned(self):
        issuer,consumer,_,state=self.metadata_clients();original=issuer.request.side_effect
        def escalated(method,path,expected,body=None):
            result=original(method,path,expected,body)
            if path.endswith('/access_grants'):result.payload['action']='read'
            return result
        issuer.request.side_effect=escalated
        grants=REGISTRATION.ExactFileMetadataGrants(issuer,consumer,2)
        with self.assertRaises(REGISTRATION.RegistrationError):
            grants.grant(14,'grib/weather.grb2','nfs',data_type='container',format_name='grib',layout='single')
        grants.cleanup();self.assertFalse(state['visible'])

    def test_metadata_grant_exact_object_and_invalid_leaf_paths(self):
        target=REGISTRATION.ExactFileMetadataGrants.target(12,'bucket/grib/weather.grb2','minio')
        self.assertEqual([s['term'] for s in target['segments']],['service','bucket','prefix','object'])
        for name in ('','/grib/a','grib//a','grib/../a','grib/a\\b','grib/a:b'):
            with self.assertRaises(REGISTRATION.RegistrationError):REGISTRATION.ExactFileMetadataGrants.target(14,name,'nfs')
        with self.assertRaises(REGISTRATION.RegistrationError):REGISTRATION.ExactFileMetadataGrants.target(12,'bucket','minio')

    def test_consumer_metadata_hidden_until_grant_uses_administrator_facts_first(self):
        _, authorizer, consumer = self.grant_clients()
        events = []
        issued = False
        prepare, read = authorizer.request.side_effect, consumer.request.side_effect
        def authorize(method, path, expected, body=None):
            nonlocal issued
            if path.endswith('/authorization-facts'): events.append('administrator-facts')
            if method == 'POST':
                self.assertEqual(events, ['administrator-facts', 'consumer-denied'])
                issued = True
                events.append('grant')
            return prepare(method, path, expected, body)
        def consume(method, path, expected, body=None):
            if '/items?branch=' in path:
                if not issued:
                    raise REGISTRATION.RegistrationError('consumer metadata branch is hidden: HTTP 404')
                events.append('consumer-catalog')
            elif method == 'POST':
                self.assertEqual(expected, (200,) if issued else (403,))
                events.append('consumer-allowed' if issued else 'consumer-denied')
            return read(method, path, expected, body)
        authorizer.request.side_effect, consumer.request.side_effect = authorize, consume
        receipts = REGISTRATION.initialize_exact_record_read_grants(authorizer, consumer, 2,
            [(4, 'schema', 'results', 'hdfs_totals')])
        self.assertEqual(len(receipts), 1)
        self.assertEqual(events, ['administrator-facts', 'consumer-denied', 'grant', 'consumer-allowed', 'consumer-catalog'])

    def test_exact_table_grant_uses_separate_preparer_and_checks_denial_then_read(self):
        _, authorizer, consumer = self.grant_clients()
        receipts = REGISTRATION.initialize_exact_record_read_grants(authorizer, consumer, 2, [(4, 'schema', 'results', 'hdfs_totals')])
        self.assertEqual(len(receipts), 1)
        writes = [call for call in authorizer.request.call_args_list if call.args[0] == 'POST']
        self.assertEqual(len(writes), 1)
        body = writes[0].args[3]
        self.assertEqual(body['recipient_id'], '7')
        self.assertEqual(body['action'], 'read')
        self.assertNotIn('direct_permission_source', body)
        self.assertNotIn('direct_permission_version', body)
        self.assertFalse(any('direct-permission' in call.args[1] for call in authorizer.request.call_args_list))
        self.assertNotIn('initialize_approval', body)
        self.assertEqual(body['item_fingerprint'], hashlib.sha256(b'4:results.hdfs_totals').hexdigest())
        self.assertEqual(body['expected_boundary_hash'], 'a' * 64)
        self.assertEqual(body['catalog_path']['segments'][-1], {'term': 'table', 'kind': 'table', 'name': 'hdfs_totals'})
        checks = [call for call in consumer.request.call_args_list if call.args[0] == 'POST']
        self.assertEqual([call.args[2] for call in checks], [(403,), (200,)])
        self.assertTrue(all(call.args[3]['targets'] == [body['catalog_path']] for call in checks))

    def test_grant_preparation_rejects_self_wrong_tenant_and_extra_permissions_before_writes(self):
        for invalid in ('self', 'tenant', 'extra', 'missing', 'custom_role'):
            current, authorizer, consumer = self.grant_clients()
            if invalid == 'self': current['principal']['id'] = '7'
            if invalid == 'tenant': current['context']['tenant_id'] = '9'
            permissions = current['authorization']['role_assignments'][0]['permissions']
            if invalid == 'extra': permissions.append('system.engine.create')
            if invalid == 'missing': permissions.pop()
            if invalid == 'custom_role': current['authorization']['role_assignments'][0]['role_key'] = 'online.custom'
            with self.subTest(invalid=invalid), self.assertRaises(REGISTRATION.RegistrationError):
                REGISTRATION.initialize_exact_record_read_grants(authorizer, consumer, 2, [(4, 'schema', 'results', 'hdfs_totals')])
            self.assertTrue(all(call.args[0] == 'GET' for call in authorizer.request.call_args_list))

    def test_exact_collection_grant_uses_provider_catalog_terms(self):
        _, authorizer, consumer = self.grant_clients()
        REGISTRATION.initialize_exact_record_read_grants(authorizer, consumer, 2,
            [(29, 'database', 'Outdoor', 'Persons')], item_term='collection')
        write = next(call for call in authorizer.request.call_args_list if call.args[0] == 'POST')
        self.assertEqual(write.args[3]['catalog_path']['segments'][-1],
                         {'term': 'collection', 'kind': 'collection', 'name': 'Persons'})

    def test_rejects_unknown_item_term_and_collection_schema_before_any_request(self):
        for term, namespace in (('unknown', 'database'), ('collection', 'schema')):
            _, authorizer, consumer = self.grant_clients()
            with self.subTest(term=term, namespace=namespace), self.assertRaises(REGISTRATION.RegistrationError):
                REGISTRATION.initialize_exact_record_read_grants(authorizer, consumer, 2,
                    [(29, namespace, 'Outdoor', 'Persons')], item_term=term)
            authorizer.request.assert_not_called()
            consumer.request.assert_not_called()

    def test_mysql_table_uses_database_namespace_and_bound_receipt(self):
        _, authorizer, consumer = self.grant_clients()
        receipts = REGISTRATION.initialize_exact_record_read_grants(authorizer, consumer, 2, [(23, 'database', 'business', 'customers')])
        self.assertEqual(receipts[0]['boundary_hash'], 'a' * 64)
        self.assertEqual(receipts[0]['catalog_path']['segments'][1]['term'], 'database')

    def test_unresolved_or_substituted_facts_cannot_issue_a_grant(self):
        for corruption in ('basis_state', 'engine_id', 'item_type', 'item_fingerprint', 'full_name', 'boundary', 'boundary_hash', 'observed_at'):
            _, authorizer, consumer = self.grant_clients()
            original = authorizer.request.side_effect
            def corrupted(method, path, expected, body=None):
                response = original(method, path, expected, body)
                if path.endswith('/authorization-facts'):
                    response.payload[corruption] = '' if corruption == 'observed_at' else 'invalid'
                return response
            authorizer.request.side_effect = corrupted
            with self.subTest(corruption=corruption), self.assertRaises(REGISTRATION.RegistrationError):
                REGISTRATION.initialize_exact_record_read_grants(authorizer, consumer, 2, [(4, 'schema', 'results', 'hdfs_totals')])
            self.assertFalse(any(call.args[0] == 'POST' for call in authorizer.request.call_args_list))

    def test_missing_system_facts_never_issue_or_browse_as_recipient(self):
        _, authorizer, consumer = self.grant_clients()
        original = authorizer.request.side_effect
        def unavailable(method, path, expected, body=None):
            if path.endswith('/authorization-facts'):
                raise REGISTRATION.RegistrationError('authorization_target_not_found: HTTP 404')
            return original(method, path, expected, body)
        authorizer.request.side_effect = unavailable
        with self.assertRaises(REGISTRATION.RegistrationError):
            REGISTRATION.initialize_exact_record_read_grants(authorizer, consumer, 2, [(4, 'schema', 'results', 'hdfs_totals')])
        self.assertFalse(any(call.args[0] == 'POST' for call in authorizer.request.call_args_list))
        self.assertEqual([call.args[1] for call in consumer.request.call_args_list], ['/api/v1/system/auth/context'])

    def test_post_grant_catalog_must_match_the_authoritative_identity(self):
        for corruption in ('missing', 'duplicate', 'fingerprint', 'hidden'):
            _, authorizer, consumer = self.grant_clients()
            original = consumer.request.side_effect
            def corrupted(method, path, expected, body=None):
                response = original(method, path, expected, body)
                if '/items?branch=' in path:
                    if corruption == 'hidden': raise REGISTRATION.RegistrationError('post-grant Meta catalog HTTP 404')
                    if corruption == 'missing': response.payload = []
                    if corruption == 'duplicate': response.payload *= 2
                    if corruption == 'fingerprint': response.payload[0]['fingerprint'] = 'b' * 64
                return response
            consumer.request.side_effect = corrupted
            with self.subTest(corruption=corruption), self.assertRaises(REGISTRATION.RegistrationError):
                REGISTRATION.initialize_exact_record_read_grants(authorizer, consumer, 2, [(4, 'schema', 'results', 'hdfs_totals')])
            self.assertEqual(sum(call.args[0] == 'POST' for call in authorizer.request.call_args_list), 1)
            self.assertFalse(any('/items?branch=' in call.args[1] for call in authorizer.request.call_args_list))

    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(
            prefix="addp-online-engine-registration-"
        )
        self.root = Path(self.temporary.name)
        self.descriptor = self.root / "engine.json"
        self.payload = {
            "name": "Hosted Online Engine",
            "engine_type": "sampledb",
            "engine_origin": "general",
            "connection_info": {"host": "127.0.0.1", "port": 15432},
            "description": "Disposable fixture",
        }
        self.descriptor.write_text(json.dumps(self.payload), encoding="utf-8")
        self.descriptor.chmod(0o600)

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def test_registers_descriptor_and_requires_successful_connection_test(self) -> None:
        requester = Mock(
            side_effect=[
                {"id": 17, "engine_type": "sampledb"},
                {"success": True},
            ]
        )

        engine_id = REGISTRATION.register_engine(
            "http://127.0.0.1:8180", "addp_at_engine", self.payload, requester
        )

        self.assertEqual(engine_id, 17)
        self.assertEqual(requester.call_count, 2)
        self.assertEqual(
            requester.call_args_list[0].args[2:4],
            ("POST", "/api/v1/system/engines"),
        )
        self.assertEqual(
            requester.call_args_list[1].args[2:4],
            ("POST", "/api/v1/system/engines/17/test"),
        )

        requester = Mock(
            side_effect=[
                {"id": 17, "engine_type": "sampledb"},
                {"success": False},
            ]
        )
        with self.assertRaisesRegex(
            REGISTRATION.RegistrationError, "connection test did not succeed"
        ):
            REGISTRATION.register_engine(
                "http://127.0.0.1:8180", "addp_at_engine", self.payload, requester
            )

    def test_failed_probe_reports_reason_without_configured_credentials(self) -> None:
        secret = "secret 密码/&+"
        self.payload["connection_info"]["credentials"] = {"password": secret}
        variants = (secret, urllib.parse.quote(secret, safe=""),
                    urllib.parse.quote_plus(secret), json.dumps(secret)[1:-1])
        requester = Mock(side_effect=[{"id": 17, "engine_type": "sampledb"},
                         {"success": False, "error": "SASL negotiation failed\n" + " ".join(variants)}])
        with self.assertRaises(REGISTRATION.RegistrationError) as raised:
            REGISTRATION.register_engine("http://127.0.0.1:8180", "token", self.payload, requester)
        detail = str(raised.exception)
        self.assertIn("type=sampledb, id=17", detail)
        self.assertIn("SASL negotiation failed", detail)
        for value in variants:
            self.assertNotIn(value, detail)
        self.assertNotIn("\n", detail)

    def test_descriptor_requires_owner_only_canonical_payload(self) -> None:
        self.assertEqual(
            REGISTRATION.load_descriptor(self.descriptor)["engine_type"], "sampledb"
        )
        self.descriptor.chmod(0o644)
        with self.assertRaisesRegex(REGISTRATION.RegistrationError, "owner-only"):
            REGISTRATION.load_descriptor(self.descriptor)

    def test_external_environment_rejects_external_system(self) -> None:
        base, token = REGISTRATION.require_external_environment(
            {
                "GITHUB_ACTIONS": "true",
                "RUNNER_OS": "Linux",
                "ADDP_ONLINE_HOSTED": "1",
                "SYSTEM_URL": "http://127.0.0.1:8180",
                "ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN": "addp_at_engine",
            }
        )
        self.assertEqual(base, "http://127.0.0.1:8180")
        self.assertEqual(token, "addp_at_engine")
        with self.assertRaisesRegex(REGISTRATION.RegistrationError, "loopback"):
            REGISTRATION.require_external_environment(
                {
                    "GITHUB_ACTIONS": "true",
                    "RUNNER_OS": "Linux",
                    "ADDP_ONLINE_HOSTED": "1",
                    "SYSTEM_URL": "https://system.example.com",
                    "ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN": "addp_at_engine",
                }
            )

    def test_owner_managed_environment_is_an_independent_external_profile(self) -> None:
        base, token = REGISTRATION.require_external_environment(
            {
                "GITHUB_ACTIONS": "true",
                "RUNNER_OS": "Linux",
                "ADDP_ONLINE_OWNER_MANAGED": "1",
                "SYSTEM_URL": "http://localhost:8180",
                "ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN": "addp_at_engine",
            }
        )
        self.assertEqual(base, "http://localhost:8180")
        self.assertEqual(token, "addp_at_engine")
        with self.assertRaisesRegex(REGISTRATION.RegistrationError, "exactly one"):
            REGISTRATION.require_external_environment(
                {
                    "GITHUB_ACTIONS": "true",
                    "RUNNER_OS": "Linux",
                    "ADDP_ONLINE_HOSTED": "1",
                    "ADDP_ONLINE_OWNER_MANAGED": "1",
                }
            )

    def test_result_contains_only_engine_id_with_owner_permissions(self) -> None:
        output = self.root / "secret/engine.env"

        REGISTRATION.write_result(output, 17)

        self.assertEqual(stat.S_IMODE(output.stat().st_mode), 0o600)
        self.assertEqual(
            output.read_text(encoding="utf-8"),
            "export ADDP_ONLINE_CONSUMER_ENGINE_ID='17'\n",
        )
        self.assertNotIn("addp_at_", output.read_text(encoding="utf-8"))

    def test_request_rejects_non_json_response(self) -> None:
        class Response:
            status = 200

            def __enter__(self):
                return self

            def __exit__(self, *_args):
                return None

            @staticmethod
            def read() -> bytes:
                return b"not-json"

        original = REGISTRATION.urllib.request.urlopen
        REGISTRATION.urllib.request.urlopen = lambda *_args, **_kwargs: Response()
        try:
            with self.assertRaisesRegex(
                REGISTRATION.RegistrationError, "did not return valid JSON"
            ):
                REGISTRATION.request_json(
                    "http://127.0.0.1:8180", "token", "GET", "/health", None, (200,)
                )
        finally:
            REGISTRATION.urllib.request.urlopen = original


if __name__ == "__main__":
    unittest.main()
