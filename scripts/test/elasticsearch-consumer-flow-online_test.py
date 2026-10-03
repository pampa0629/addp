import importlib.util
from pathlib import Path
import unittest
import json
import os
import tempfile
from unittest.mock import patch
from types import SimpleNamespace

path = Path(__file__).with_name('elasticsearch-consumer-flow-online.py')
spec = importlib.util.spec_from_file_location('elasticsearch_online_test_target', path)
MODULE = importlib.util.module_from_spec(spec)
spec.loader.exec_module(MODULE)


class ElasticsearchConsumerContractTest(unittest.TestCase):
    def test_dotted_index_locator_round_trip(self):
        self.assertEqual(MODULE.locator(91, {'id': 7, 'full_name': 'addp_orders.v1'}),
                         'addp://engine/91/path/addp_orders.v1?type=index&item_id=7')

    def test_precision_and_projection_must_be_verified(self):
        row = {'order_id': '9007199254740993', 'customer': {'name': 'customer-0'}}
        MODULE.validate_rows([row], 1, projected=True)
        for bad in [dict(row, order_id=9007199254740992), dict(row, private='leak'),
                    dict(row, order_id='9007199254740994'), dict(row, customer={'name': 'wrong'})]:
            with self.assertRaises(MODULE.SuiteError):
                MODULE.validate_rows([bad], 1, projected=True)

    def test_full_fixture_detects_duplicate_records_and_incorrect_sorting(self):
        rows = [{'order_id': str(9007199254740993 + number),
                 'customer': {'name': 'customer-' + str(number)}} for number in range(25)]
        MODULE.validate_rows(rows, 25, projected=True, ordered=True)
        MODULE.validate_rows(list(reversed(rows)), 25, projected=True)
        for invalid in [rows[:-1] + [rows[0]], list(reversed(rows))]:
            with self.assertRaises(MODULE.SuiteError):
                MODULE.validate_rows(invalid, 25, projected=True, ordered=True)

    def test_nested_array_must_retain_fixture_contents(self):
        row = {'order_id': '9007199254740993', 'customer': {'name': 'customer-0'},
               'items': [{'sku': 'SKU-001', 'quantity': 1}]}
        MODULE.validate_rows([row], 1)
        for items in [[], {'sku': 'SKU-001', 'quantity': 1}, [{'sku': 'SKU-001', 'quantity': 2}]]:
            with self.assertRaises(MODULE.SuiteError):
                MODULE.validate_rows([dict(row, items=items)], 1)


class ElasticsearchBrowserEvidenceTest(unittest.TestCase):
    def test_browser_requires_success_matching_identity_and_all_screenshots(self):
        report = {'engine_id': 7, 'tenant_id': 2, 'principal_id': '42'}
        for failure in ('process', 'missing_report', 'identity', 'missing_screenshot', ''):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as temporary:
                directory = Path(temporary)
                evidence = dict(report, run_id='es-run', manager_rows=25, develop_rows=25,
                                empty_index=True, meta_ui_scan=True, develop_ui_query=True)
                if failure == 'identity': evidence['principal_id'] = 'other'
                def browser(command, cwd, env):
                    self.assertIn('e2e/online/elasticsearch-consumer-flow.spec.js', command)
                    self.assertEqual(json.loads(env['ADDP_ONLINE_ELASTICSEARCH_EXPECTATIONS']), report)
                    if failure != 'missing_report':
                        Path(env['ADDP_ONLINE_ELASTICSEARCH_BROWSER_REPORT']).write_text(json.dumps(evidence))
                    for name in ('meta', 'orders', 'empty', 'query'):
                        if failure != 'missing_screenshot' or name != 'query':
                            (directory / f'elasticsearch-{name}-console.png').write_bytes(b'screenshot')
                    return SimpleNamespace(returncode=1 if failure == 'process' else 0)
                with patch.dict(os.environ, ADDP_ONLINE_ARTIFACT_DIR=temporary, ADDP_ONLINE_TEST_RUN_ID='es-run'), patch.object(MODULE.subprocess, 'run', side_effect=browser):
                    if failure:
                        with self.assertRaises(MODULE.SuiteError): MODULE.run_browser(report)
                    else:
                        self.assertEqual(MODULE.run_browser(report), evidence)

if __name__ == '__main__':
    unittest.main()
