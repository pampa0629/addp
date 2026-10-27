import importlib
import unittest
from types import SimpleNamespace
from unittest.mock import patch

FIXTURE=importlib.import_module('scripts.test.catalog-browser-local-fixture')

class BrowserFixtureTest(unittest.TestCase):
    def test_failed_readiness_does_not_become_browser_success(self):
        class Client:
            def request(self,method,path,status,body=None,**kwargs):
                if path.endswith('/units'):return SimpleNamespace(payload=[{'id':7,'code':'m'}])
                if path.endswith('/elements/2'):return SimpleNamespace(payload={'id':2,'owner_domain_id':9,'version':1})
                if path.endswith('/overview?view=inventory'):return SimpleNamespace(payload={'total_entries':0})
                if path.endswith('/entries/resolve-sources'):raise AssertionError('must not reach ready')
                return SimpleNamespace(payload={'id':3,'version':1})
        with patch.object(FIXTURE,'login',return_value=Client()),patch.object(FIXTURE.DOMAIN,'prepare_reference',return_value=2),\
             patch.object(FIXTURE.time,'monotonic',side_effect=[0,200]):
            with self.assertRaisesRegex(FIXTURE.API.SuiteError,'synchronization'):
                FIXTURE.prepare('http://127.0.0.1:1',{'run_id':'run'})

    def test_preparation_binds_cross_domain_unit_document_and_reference_without_t4(self):
        calls=[]
        class Client:
            def request(self,method,path,status,body=None,**kwargs):
                calls.append((method,path,body))
                if path.endswith('/units'):payload=[{'id':7,'code':'m'}]
                elif path.endswith('/elements/2'):payload={'id':2,'owner_domain_id':9,'version':1}
                elif path.endswith('/overview?view=inventory'):payload={'total_entries':2}
                elif path.endswith('/entries/resolve-sources'):payload={'results':[{'found':True,'entry':{'id':'local'}},{'found':True,'entry':{'id':'source'}}]}
                else:payload={'id':3,'version':1}
                return SimpleNamespace(payload=payload)
        with patch.object(FIXTURE,'login',return_value=Client()),patch.object(FIXTURE.DOMAIN,'prepare_reference',return_value=2) as reference,\
             patch.object(FIXTURE.DOMAIN,'run_suite',side_effect=AssertionError('API T4 forbidden')):
            result=FIXTURE.prepare('http://127.0.0.1:1',{'run_id':'run'})
        reference.assert_called_once_with(unittest.mock.ANY,'run',unit_id=7)
        self.assertEqual((result['domain_a'],result['domain_b'],result['relation_state']),(3,9,'draft'))
        self.assertIn(('POST','/api/v1/standard/elements/2/documents/link',{'doc_id':3,'version':1}),calls)

    def test_source_changes_fail_final_verification(self):
        client=SimpleNamespace(request=lambda *args:SimpleNamespace(payload={'changed':True}))
        with patch.object(FIXTURE,'login',return_value=client):
            with self.assertRaisesRegex(FIXTURE.API.SuiteError,'changed'):
                FIXTURE.verify_source('http://127.0.0.1',{}, {'source_element':2,'source_digest':'before'})
