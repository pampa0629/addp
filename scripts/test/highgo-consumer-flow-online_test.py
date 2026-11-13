"""Reject incomplete facts, broadened identities, and silent protocol substitution."""
from importlib import import_module
from pathlib import Path
import re
import json
from copy import deepcopy
import shutil
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

OWNER = import_module('scripts.test.highgo-consumer-flow-online')
CHECK = import_module('scripts.ci.check-online-ci-registration')
GATE = import_module('scripts.test.online-gate')
ROOT = Path(__file__).resolve().parents[2]


def item(table):
    constraints, indexes = OWNER.expected_facts(table)
    rows, count = OWNER.TABLES[table]
    return {'id': 7, 'node_id': 3, 'item_type': 'table', 'full_name': 'addp_business.' + table,
            'attributes': {'capabilities': {'constraints': {'constraints': constraints}, 'indexing': {'indexes': indexes}},
                'type_info': {'table': {'row_count': rows, 'primary_key': ['id'],
                    'fields': [{'name': 'id', 'primary_key': True}] + [{'name': 'f' + str(i)} for i in range(count - 1)]}}}}


class HighGoConsumerTest(unittest.TestCase):
    def test_business_contract_retains_all_eleven_constraints_and_ten_indexes(self):
        results = [OWNER.validate_item(item(table), table) for table in OWNER.TABLES]
        self.assertEqual(sum(r['constraints'] for r in results), 11)
        self.assertEqual(sum(r['indexes'] for r in results), 10)

    def test_rejects_lost_index_order_false_partition_and_wrong_reference(self):
        for corruption in ('order', 'reference', 'partition', 'pk', 'missing', 'extra'):
            value = item('orders'); attrs = value['attributes']; caps = attrs['capabilities']
            if corruption == 'order':
                next(i for i in caps['indexing']['indexes'] if i['name'].endswith('updated_at'))['fields'].reverse()
            if corruption == 'reference':
                next(i for i in caps['constraints']['constraints'] if i['constraint_type'] == 'foreign_key')['referenced_table'] = 'other'
            if corruption == 'partition': caps['partitioning'] = {}
            if corruption == 'pk': attrs['type_info']['table']['fields'][0]['primary_key'] = False
            if corruption == 'missing': caps['constraints']['constraints'].pop()
            if corruption == 'extra': caps['indexing']['indexes'].append({'name': 'invented'})
            with self.subTest(corruption=corruption), self.assertRaises(OWNER.API.SuiteError):
                OWNER.validate_item(value, 'orders')

    def test_structural_meta_rejects_content_and_statistics(self):
        record = item('orders')
        with self.assertRaises(OWNER.API.SuiteError): OWNER.validate_structure(record, 'orders')
        record['attributes'].pop('capabilities')
        record['attributes']['type_info']['table'].pop('row_count')
        OWNER.validate_structure(record, 'orders')

    def test_prepare_has_exact_six_deep_targets_and_private_intents(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            consumer, preparer = Mock(), Mock()
            env = {'GATEWAY_URL': 'http://127.0.0.1:8000','ADDP_ONLINE_TEST_TENANT_ID':'2',
                'ADDP_ONLINE_HIGHGO_ENGINE_ID':'29','ADDP_ONLINE_HIGHGO_PG_ENGINE_ID':'2',
                'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN':'consumer','ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN':'preparer'}
            def grants(*args, **kwargs):
                for engine, _, schema, table in args[3]: kwargs['on_pending'](engine, table, {'engine_id': engine})
            with patch.dict(OWNER.os.environ,env), patch.object(OWNER.FIXTURE,'secret_directory',return_value=root), \
                patch.object(OWNER.API,'validate_user_identity'), patch.object(OWNER.SUPPORT,'GatewayClient',side_effect=[consumer,preparer]), \
                patch.object(OWNER.REGISTRATION,'source_grant_participants'), patch.object(OWNER.SUPPORT,'wait_for_scan',side_effect=['hg-scan','pg-scan']) as scan, \
                patch.object(OWNER.REGISTRATION,'initialize_exact_record_read_grants',side_effect=grants) as grant:
                OWNER.prepare()
            self.assertEqual([c.args[0] for c in scan.call_args_list],[preparer,preparer])
            self.assertEqual([len(c.kwargs['targets']) for c in scan.call_args_list],[5,1])
            self.assertTrue(all(c.kwargs['scan_depth']=='deep' for c in scan.call_args_list))
            self.assertEqual(grant.call_args.args[3], [(29,'schema',OWNER.NAMESPACE,n) for n in [*OWNER.TABLES,OWNER.TARGET]]+[(2,'schema',OWNER.NAMESPACE,'orders')])
            journal=root/'highgo-business-grants.json'
            self.assertEqual(len(json.loads(journal.read_text())),6)
            self.assertEqual(journal.stat().st_mode&0o777,0o600)
            self.assertNotIn('meta.scan_task.execute',OWNER.PERMISSIONS)
            self.assertNotIn('meta.scan_task.read',OWNER.PERMISSIONS)

    def test_cleanup_revokes_all_recorded_intents_even_after_one_failure(self):
        for failed in (False, True):
            with tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                pending = [dict(engine_id=29,request_id=str(i),target={'id':i}) for i in range(6)]
                (root/'highgo-business-grants.json').write_text(json.dumps(pending))
                preparer, consumer = Mock(), Mock()
                preparer.request.side_effect = [RuntimeError('reject') if failed else None]+[None]*5
                with patch.dict(OWNER.os.environ,ADDP_ONLINE_ARTIFACT_DIR=temp), patch.object(OWNER.FIXTURE,'secret_directory',return_value=root), \
                    patch.object(OWNER.SUPPORT,'required_environment',side_effect=lambda key: temp if key=='ADDP_ONLINE_ARTIFACT_DIR' else 'token'), \
                    patch.object(OWNER.SUPPORT,'GatewayClient',side_effect=[preparer,consumer]):
                    if failed:
                        with self.assertRaisesRegex(RuntimeError,'cleanup incomplete'): OWNER.cleanup()
                    else: OWNER.cleanup()
                self.assertEqual(preparer.request.call_count,6)
                self.assertEqual(consumer.request.call_count,5 if failed else 6)
                self.assertTrue(all(call.args[2] == (403,) for call in consumer.request.call_args_list))

    def test_replace_has_one_snapshot_policy_and_preserves_decimal(self):
        payload = OWNER.replace_task('owned', 'source', 29, {'node_id': 3})
        config = payload['config']
        self.assertEqual(config['target']['policy'], {'apply_mode': 'replace'})
        self.assertEqual(config['load'], {'mode': 'snapshot'})
        self.assertEqual(config['runtime'], {'boundary': 'bounded'})
        fields = config['transforms'][0]['fields']
        self.assertEqual([f['source'] for f in fields], OWNER.ORDER_FIELDS)
        self.assertEqual((fields[4]['target_type'], fields[4]['precision'], fields[4]['scale']), ('decimal', 14, 2))

    def test_duplicate_dataset_cannot_be_selected(self):
        client = Mock()
        client.request.return_value = SimpleNamespace(payload=[item('orders'), item('orders')])
        with self.assertRaisesRegex(OWNER.API.SuiteError, 'duplicate'):
            OWNER.items(client, 29)

    def test_rejects_extra_privileges_before_scan_or_write(self):
        client = Mock()
        client.request.return_value = SimpleNamespace(payload={'authorization': {'role_assignments': [
            {'permissions': [*OWNER.PERMISSIONS, 'system.engine.create']}]}})
        with patch.object(OWNER.API, 'validate_user_identity'), self.assertRaisesRegex(OWNER.API.SuiteError, 'extra permissions'):
            OWNER.run(client, 2, 29, 2, 10)
        self.assertTrue(all(call.args[0] == 'GET' for call in client.request.call_args_list))

    def test_registration_is_manual_and_selects_real_highgo_owner(self):
        suite = GATE.SUITES['highgo-consumer-flow']
        self.assertFalse(suite.nightly)
        self.assertEqual(suite.command[-1], 'scripts.test.highgo-consumer-flow-online')
        self.assertEqual({m for m, _ in suite.services}, {'gateway', 'system', 'meta', 'manager', 'transfer'})

    def test_two_replaces_compare_values_and_cleanup_even_when_browser_or_copy_fails(self):
        for fault in (None, 'copy', 'browser'):
            hg = {name: item(name) for name in OWNER.TABLES}
            hg[OWNER.TARGET] = dict(item('orders'), full_name='addp_business.' + OWNER.TARGET)
            pg = {'orders': item('orders')}
            raw_hg = deepcopy(hg)
            for record in hg.values():
                record['attributes'].pop('capabilities')
                record['attributes']['type_info']['table'].pop('row_count')
            context = {'principal': {'id': '7'}, 'authorization': {'role_assignments': [{'permissions': list(OWNER.PERMISSIONS)}]}}
            selectors = [{'id': 29, 'engine_type': 'highgo', 'capabilities': {'storage': {
                'facts': {k: True for k in ('indexes', 'constraints', 'partitioning')}, 'store': {'delete': True}}}},
                {'id': 2, 'engine_type': 'postgresql'}]
            client = Mock()
            client.request.side_effect = lambda method, path, *args: SimpleNamespace(payload=context if path.endswith('/auth/context') else selectors)
            def preview(client, engine, record, name):
                rows = [{'id': i} for i in range(3)]
                if fault == 'copy' and name == OWNER.TARGET: rows[0]['id'] = 999
                return {'columns': OWNER.ORDER_FIELDS, 'rows': rows, 'total': 3, 'item_meta': {'attributes': [{'key': k, 'value': v} for k,v in raw_hg[name if name in raw_hg else 'orders']['attributes'].items()]}}
            def create(client, payload, tasks): tasks.append(22); return 22
            with tempfile.TemporaryDirectory() as temp, self.subTest(fault=fault):
                Path(temp, 'highgo-business-scans.json').write_text('["scan-hg","scan-pg"]')
                self.run_copy_case(fault, temp, client, hg, pg, preview, create)

    def run_copy_case(self, fault, temp, client, hg, pg, preview, create):
        time_counter = [2, 1]
        with patch.object(OWNER.FIXTURE, 'secret_directory', return_value=Path(temp)), patch.dict(OWNER.os.environ, {'GATEWAY_URL': 'http://127.0.0.1:8000', 'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN': '', 'ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN': '', 'ADDP_ONLINE_TEST_RUN_ID': 'owned'}), patch.object(OWNER.API, 'validate_user_identity', return_value={}), patch.object(OWNER.SUPPORT, 'wait_for_scan', return_value='scan'), patch.object(OWNER, 'items', side_effect=lambda client, engine: deepcopy(hg) if engine == 29 else pg), patch.object(OWNER.time, 'sleep', side_effect=lambda _: hg[OWNER.TARGET].update(scanned_at=str(time_counter.pop()))), patch.object(OWNER, 'preview', side_effect=preview), patch.object(OWNER.REGISTRATION, 'initialize_exact_record_read_grants') as grants, patch.object(OWNER.FLOW, 'create_task', side_effect=create), patch.object(OWNER.FLOW, 'run_task', return_value={'execution_id': 'execution', 'records_read': 3, 'records_written': 3}) as execute, patch.object(OWNER.SUPPORT, 'cleanup_tasks') as cleanup, patch.object(OWNER, 'run_browser', side_effect=RuntimeError('browser failed') if fault == 'browser' else None, return_value={}):
            if fault:
                with self.assertRaises(RuntimeError): OWNER.run(client, 2, 29, 2, 30)
            else:
                result = OWNER.run(client, 2, 29, 2, 30)
                self.assertEqual(len(result['executions']), 2)
                self.assertEqual(execute.call_count, 2)
            cleanup.assert_called_once_with(client, [22])
            grants.assert_not_called()


class HighGoRegistrationTest(unittest.TestCase):
    def test_registered_profile_requires_fixture_assertions_browser_and_cleanup(self):
        CHECK.validate_highgo_profile(ROOT, {'highgo-consumer-flow'})
        paths = ['Makefile', '.github/workflows/online-t4-gates.yml',
                 'scripts/test/online-hosted-highgo-gate.sh', 'business/scripts/online-highgo-consumer-fixture.py',
                 'scripts/test/highgo-consumer-flow-online.py', 'console/frontend/e2e/online/highgo-consumer-flow.spec.js',
                 'scripts/test/highgo-consumer-flow-online_test.py', 'scripts/test/online-highgo-consumer-fixture_test.py',
                 'scripts/test/online-hosted-highgo-gate_test.py', 'scripts/test/highgo-sql-timeout-online.py',
                 'scripts/test/highgo-sql-timeout-online_test.py', 'console/frontend/e2e/online/highgo-sql-timeout.spec.js']
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            for path in paths:
                dest = root / path; dest.parent.mkdir(parents=True, exist_ok=True); shutil.copyfile(ROOT / path, dest)
            CHECK.validate_highgo_profile(root, {'highgo-consumer-flow'})
            faults = [('Makefile', 'test-highgo-online-runner'),
                      ('business/scripts/online-highgo-consumer-fixture.py', "'volume', 'inspect'"),
                      ('scripts/test/highgo-consumer-flow-online.py', 'SUPPORT.cleanup_tasks'),
                      ('.github/workflows/online-t4-gates.yml', "inputs.suite != 'highgo-consumer-flow'")]
            for path, fragment in faults:
                dest = root / path; original = dest.read_text()
                if path == 'Makefile':
                    changed = re.sub(rf'(?m)^(test-online-runner:[^\n]*?)\b{re.escape(fragment)}\b',
                                     r'\1removed', original, count=1)
                else:
                    changed = original.replace(fragment, 'removed')
                self.assertNotEqual(original, changed, fragment)
                dest.write_text(changed)
                with self.subTest(path=path), self.assertRaises(CHECK.RegistrationError):
                    CHECK.validate_highgo_profile(root, {'highgo-consumer-flow'})
                dest.write_text(original)
            (root / paths[-1]).unlink()
            with self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_highgo_profile(root, {'highgo-consumer-flow'})


if __name__ == '__main__':
    unittest.main()
