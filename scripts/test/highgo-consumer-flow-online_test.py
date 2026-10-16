"""Reject incomplete facts, broadened identities, and silent protocol substitution."""
from importlib import import_module
from pathlib import Path
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
            hg['addp_engine_probe'] = {'id': 8}
            hg[OWNER.TARGET] = dict(item('orders'), full_name='addp_business.' + OWNER.TARGET)
            pg = {'orders': item('orders')}
            context = {'principal': {'id': '7'}, 'authorization': {'role_assignments': [{'permissions': list(OWNER.PERMISSIONS)}]}}
            selectors = [{'id': 29, 'engine_type': 'highgo', 'capabilities': {'storage': {
                'facts': {k: True for k in ('indexes', 'constraints', 'partitioning')}, 'store': {'delete': True}}}},
                {'id': 2, 'engine_type': 'postgresql'}]
            client = Mock()
            client.request.side_effect = lambda method, path, *args: SimpleNamespace(payload=context if path.endswith('/auth/context') else selectors)
            def preview(client, engine, record, name):
                rows = [{'id': i} for i in range(3)]
                if fault == 'copy' and name == OWNER.TARGET: rows[0]['id'] = 999
                return {'columns': OWNER.ORDER_FIELDS, 'rows': rows, 'total': 3}
            def create(client, payload, tasks): tasks.append(22); return 22
            with self.subTest(fault=fault), patch.dict(OWNER.os.environ, {'GATEWAY_URL': 'http://127.0.0.1:8000', 'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN': 'fixture-token', 'ADDP_ONLINE_TEST_RUN_ID': 'owned'}), patch.object(OWNER.API, 'validate_user_identity', return_value={}), patch.object(OWNER.SUPPORT, 'wait_for_scan', return_value='scan'), patch.object(OWNER, 'items', side_effect=lambda client, engine: hg if engine == 29 else pg), patch.object(OWNER, 'preview', side_effect=preview), patch.object(OWNER.REGISTRATION, 'initialize_exact_record_read_grants') as grants, patch.object(OWNER.FLOW, 'create_task', side_effect=create), patch.object(OWNER.FLOW, 'run_task', return_value={'execution_id': 'execution', 'records_read': 3, 'records_written': 3}) as execute, patch.object(OWNER.SUPPORT, 'cleanup_tasks') as cleanup, patch.object(OWNER, 'run_browser', side_effect=RuntimeError('browser failed') if fault == 'browser' else None, return_value={}):
                if fault:
                    with self.assertRaises(RuntimeError): OWNER.run(client, 2, 29, 2, 30)
                else:
                    result = OWNER.run(client, 2, 29, 2, 30)
                    self.assertEqual(len(result['executions']), 2)
                    self.assertEqual(execute.call_count, 2)
                cleanup.assert_called_once_with(client, [22])
                self.assertEqual(grants.call_args.args[3], [(2, 'schema', 'addp_business', 'orders')])


class HighGoRegistrationTest(unittest.TestCase):
    def test_registered_profile_requires_fixture_assertions_browser_and_cleanup(self):
        CHECK.validate_highgo_profile(ROOT, {'highgo-consumer-flow'})
        paths = ['Makefile', '.github/workflows/online-t4-gates.yml',
                 'scripts/test/online-hosted-highgo-gate.sh', 'business/scripts/online-highgo-consumer-fixture.py',
                 'scripts/test/highgo-consumer-flow-online.py', 'console/frontend/e2e/online/highgo-consumer-flow.spec.js',
                 'scripts/test/highgo-consumer-flow-online_test.py', 'scripts/test/online-highgo-consumer-fixture_test.py',
                 'scripts/test/online-hosted-highgo-gate_test.py']
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            for path in paths:
                dest = root / path; dest.parent.mkdir(parents=True, exist_ok=True); shutil.copyfile(ROOT / path, dest)
            faults = [('Makefile', '$(MAKE) test-highgo-online-runner'),
                      ('business/scripts/online-highgo-consumer-fixture.py', "'volume', 'inspect'"),
                      ('scripts/test/highgo-consumer-flow-online.py', 'SUPPORT.cleanup_tasks'),
                      ('.github/workflows/online-t4-gates.yml', "inputs.suite != 'highgo-consumer-flow'")]
            for path, fragment in faults:
                dest = root / path; original = dest.read_text(); dest.write_text(original.replace(fragment, 'removed'))
                with self.subTest(path=path), self.assertRaises(CHECK.RegistrationError):
                    CHECK.validate_highgo_profile(root, {'highgo-consumer-flow'})
                dest.write_text(original)
            (root / paths[-1]).unlink()
            with self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_highgo_profile(root, {'highgo-consumer-flow'})


if __name__ == '__main__':
    unittest.main()
