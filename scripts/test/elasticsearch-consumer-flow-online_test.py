import importlib.util
from pathlib import Path
import unittest

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


if __name__ == '__main__':
    unittest.main()
