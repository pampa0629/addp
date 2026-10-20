from importlib import import_module
import copy
import unittest
from pathlib import Path

FLOW = import_module('scripts.test.readonly-sql-consumer-flow-online')
GAUSS = import_module('scripts.test.gaussdb-consumer-flow-online_test')
PROFILE = FLOW.PROFILES['starrocks']


class StarRocksConsumerTest(unittest.TestCase):
    def test_source_selection_requires_every_demo_table_and_rejects_unexpected_items(self):
        items=[{'full_name':'business.'+name,'item_type':'table'} for name in ('orders','source','products','customers')]
        self.assertIs(FLOW.select_source(items,PROFILE),items[1])
        for bad in (items[:-1],items+[items[1]],items+[{'full_name':'business.ungranted','item_type':'table'}],
                    [{**item,'item_type':'view'} for item in items]):
            with self.subTest(items=bad), self.assertRaises(FLOW.API.SuiteError):
                FLOW.select_source(bad,PROFILE)
        gauss=[{'full_name':'addp_acceptance.source','item_type':'table'}]
        self.assertIs(FLOW.select_source(gauss,FLOW.PROFILES['gaussdb']),gauss[0])

    def sample(self):
        value = GAUSS.GaussDBConsumerTest().sample()
        value['data']['columns'] = list(PROFILE.source_columns)
        value['data']['rows'][0]['name'] = PROFILE.sample_name
        value['data']['rows'][1]['optional_text'] = ''
        return value

    def test_native_empty_null_precision_and_spaces(self):
        FLOW.validate_preview(self.sample(), PROFILE)
        for field, value, row in [('id', 9007199254740992, 0), ('amount', 123.5, 0),
                                  ('optional_text', None, 1), ('name', '保留尾空格', 1)]:
            bad = self.sample()
            bad['data']['rows'][row][field] = value
            with self.subTest(field=field), self.assertRaises(FLOW.API.SuiteError):
                FLOW.validate_preview(bad, PROFILE)
        with self.assertRaises(FLOW.API.SuiteError):
            FLOW.validate_preview(self.sample(), FLOW.PROFILES['gaussdb'])

    def test_physical_field_order_is_distinct_from_primary_key_order(self):
        item = {'item_type':'table', 'full_name':'business.source', 'fingerprint':'native',
                'attributes':{'schema_version':1, 'item':{'data_type':'table'}, 'type_info':{'table':{
                    'primary_key':['name','id'], 'row_count':2, 'fields':[
                        {'name':'name','type':'string','primary_key':True},
                        {'name':'id','type':'bigint','primary_key':True},
                        {'name':'amount','type':'decimal','precision':38,'scale':18},
                        {'name':'optional_text','type':'string'}]}}}}
        FLOW.validate_item(item, PROFILE)
        for key in ('order','precision','key','enrichment'):
            bad = copy.deepcopy(item)
            table = bad['attributes']['type_info']['table']
            if key == 'order': table['fields'].reverse()
            elif key == 'precision': table['fields'][2]['precision'] = 40
            elif key == 'key': table['primary_key'] = ['id','name']
            else: bad['attributes']['capabilities'] = {'indexing':{}}
            with self.subTest(key=key), self.assertRaises(FLOW.API.SuiteError):
                FLOW.validate_item(bad, PROFILE)

    def test_two_real_query_results_require_the_same_identity_and_exact_parameters(self):
        report = {'run_id':'r','tenant_id':2,'principal_id':'4','engine_id':17}
        rows = self.sample()['data']['rows']
        fields = [{'name':'id','type':'bigint'},{'name':'name','type':'string'},
                  {'name':'amount','type':'decimal','precision':38,'scale':18},{'name':'optional_text','type':'string'}]
        evidence = {**report, 'queries':[
            {'execution_id':'a','status':'success','effect':'read','rows':rows,'fields':fields},
            {'execution_id':'b','status':'success','effect':'read','rows':[{**rows[0], 'bound_text':"中文' OR 1=1 --"}],
             'fields':fields+[{'name':'bound_text','type':'string'}]}]}
        FLOW.validate_browser_report(evidence, report, PROFILE)
        for change in ('principal','parameter','precision','execution'):
            bad = copy.deepcopy(evidence)
            if change == 'principal': bad['principal_id'] = 'other'
            elif change == 'parameter': bad['queries'][1]['rows'][0]['bound_text'] = 'interpolated'
            elif change == 'precision': bad['queries'][0]['rows'][0]['id'] = 9007199254740992
            else: bad['queries'][1]['execution_id'] = 'a'
            with self.subTest(change=change), self.assertRaises(FLOW.API.SuiteError):
                FLOW.validate_browser_report(bad, report, PROFILE)

    def test_one_shared_consumer_and_browser_implementation(self):
        root = Path(__file__).resolve().parents[2]
        for old in ('scripts/test/gaussdb-consumer-flow-online.py', 'console/frontend/e2e/online/gaussdb-consumer-flow.spec.js'):
            self.assertFalse((root/old).exists())
        self.assertEqual(len(FLOW.PERMISSIONS), 10)
        self.assertFalse(any(p.startswith(('transfer.', 'service.', 'system.engine.')) for p in FLOW.PERMISSIONS))
