import contextlib
import importlib
import io
import os
from types import SimpleNamespace
import unittest
from unittest.mock import patch

SUITE=importlib.import_module('scripts.test.catalog-domain-overview-online')

class CatalogDomainOverviewOnlineTest(unittest.TestCase):
 def test_counts_cannot_hide_missing_types_or_invent_coverage(self):
  valid={'view':'inventory','total_entries':1,'entry_types':[{'entry_type':'element','count':1}],'coverage':{'total_entries':1}}
  SUITE.check_overview(valid,{'element'})
  for change in ({'entry_types':[{'entry_type':'metric','count':1}]},{'entry_types':[{'entry_type':'element','count':True}]},{'coverage':None},{'total_entries':2}):
   with self.subTest(change=change),self.assertRaises(SUITE.SuiteError):SUITE.check_overview({**valid,**change},{'element'})
 def test_cleanup_attempts_all_exact_resources_and_reports_failure(self):
  attempts=[]
  def remove(_,path):
   attempts.append(path)
   if path=='/owned/b':raise SUITE.SuiteError('delete unavailable')
  with patch.object(SUITE._API,'cleanup_resource',side_effect=remove),self.assertRaisesRegex(SUITE.SuiteError,'cleanup failed'):
   SUITE.cleanup(object(),['/owned/a','/owned/b','/owned/c'])
  self.assertEqual(attempts,['/owned/c','/owned/b','/owned/a'])
 def test_partial_fixture_failure_cleans_created_domain_and_leaves_reference_read_only(self):
  calls=[];reference={'id':51,'tenant_id':7,'version':1,'lifecycle_state':'active','current_revision':{'id':52,'status':'published'}}
  class Client:
   def request(self,method,path,expected,body=None):
    calls.append((method,path))
    if method=='GET' and path=='/api/v1/standard/elements/51':return SimpleNamespace(payload=reference)
    if method=='POST' and path=='/api/v1/standard/domains':return SimpleNamespace(payload={'id':31,'version':1})
    raise SUITE.SuiteError('fixture creation rejected')
  with patch.object(SUITE._API,'cleanup_resource',return_value=True) as cleanup,self.assertRaisesRegex(SUITE.SuiteError,'fixture creation rejected'):
   SUITE.run_suite(Client(),object(),7,'synthetic_run',51)
  cleanup.assert_called_once()
  self.assertEqual(cleanup.call_args.args[1],'/api/v1/standard/domains/31')
  self.assertEqual([method for method,path in calls if path.endswith('/elements/51')],['GET','GET'])
 def test_no_online_opt_in_cannot_mutate_resources(self):
  with patch.dict(os.environ,{},clear=True),contextlib.redirect_stderr(io.StringIO()):self.assertEqual(SUITE.main(),1)

if __name__=='__main__':unittest.main()
