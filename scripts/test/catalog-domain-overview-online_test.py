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
  calls=[];reference={'id':51,'tenant_id':7,'version':1,'lifecycle_state':'active','scope_type':'domain','owner_domain_id':20,'current_revision':{'id':52,'status':'published'}}
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


class HostedCatalogContractTest(unittest.TestCase):
 def test_run_id_code_is_bounded_and_collision_resistant(self):
  self.assertLess(len(SUITE.fixture_prefix('a'*64)+'_source'),50)
  self.assertNotEqual(SUITE.fixture_prefix('a'*64),SUITE.fixture_prefix('a'*63+'b'))

 def test_reader_must_have_exact_permissions_and_user_identity(self):
  def context(permissions):
   return {'principal':{'type':'user','id':'71'},'context':{'type':'tenant','tenant_id':'7'},'token':{'type':'first_party_access_token'},'authorization':{'role_assignments':[{'role_key':'online.reader','permissions':permissions}]}}
  class Client:
   def __init__(self,permissions):self.payload=context(permissions)
   def request(self,*args):return SimpleNamespace(payload=self.payload)
  self.assertEqual(SUITE.validate_identity(Client(['catalog.entry.read']),7,{'catalog.entry.read'})[1],'71')
  with self.assertRaisesRegex(SUITE.SuiteError,'exactly'):
   SUITE.validate_identity(Client(['catalog.entry.read','catalog.inventory.read']),7,{'catalog.entry.read'})

 def test_external_standard_is_published_through_versioned_owner_actions(self):
  calls=[]
  class Client:
   def request(self,method,path,status,body=None):
    calls.append((method,path,body))
    if path.endswith('/domains'):return SimpleNamespace(payload={'id':41})
    if path.endswith('/elements'):return SimpleNamespace(payload={'id':51,'version':1,'latest_revision':{'id':52}})
    if path.endswith('/submit'):
     self_version=body['version'];assert self_version==1
     return SimpleNamespace(payload={'id':51,'version':2})
    if path.endswith('/publish'):
     assert body['version']==2
     return SimpleNamespace(payload={'id':51,'owner_domain_id':41,'current_revision':{'status':'published'}})
    raise AssertionError(path)
  self.assertEqual(SUITE.prepare_reference(Client(),'run'),51)
  self.assertEqual([path.rsplit('/',1)[-1] for _,path,_ in calls],['domains','elements','submit','publish'])

 def run_fixture(self, corrupt=None):
  reference={'id':51,'tenant_id':7,'version':3,'lifecycle_state':'active','scope_type':'domain','owner_domain_id':41,'current_revision':{'id':52,'status':'published'}}
  calls=[]
  class Client:
   def request(self,method,path,expected,body=None):
    calls.append((method,path))
    if method=='GET' and path=='/api/v1/standard/elements/51':return SimpleNamespace(payload=reference)
    if method=='POST' and path=='/api/v1/standard/domains':return SimpleNamespace(payload={'id':31,'version':1})
    if method=='POST' and path=='/api/v1/model/entities':return SimpleNamespace(payload={'id':61,'version':1})
    if method=='POST' and path.endswith('/attributes'):return SimpleNamespace(payload={})
    if method=='POST' and path.startswith('/api/v1/standard/'):return SimpleNamespace(payload={'id':71,'version':1})
    if path.startswith('/api/v1/catalog/domains/31/overview'):
     kinds=['glossary','element','code_set','standard_document','metric','business_entity']
     return SimpleNamespace(payload={'view':'inventory','total_entries':6,'entry_types':[{'entry_type':k,'count':1} for k in kinds],'coverage':{'total_entries':6}})
    if path.startswith('/api/v1/catalog/entries?'):return SimpleNamespace(payload={})
    if path.endswith('/resolve-sources'):
     if len(body['references'])==5:return SimpleNamespace(payload={'results':[{'found':True,'entry':{'id':str(i)}} for i in range(5)]})
     return SimpleNamespace(payload={'results':[{'found':True,'scope_type':'domain','owner_domain_id':'31' if corrupt=='local' else '41'}]})
    if path.startswith('/api/v1/model/entities/standard_references'):
     return SimpleNamespace(payload={'total':1,'data':[{'source_identity':'51','evidence':[{'resource_id':'61','owner_module':'model','resource_type':'entity','position':'Reference attribute','relation_kind':'model.entity.element','state':'draft','revision_id':'52' if corrupt=='frozen' else ''}]}]})
    raise AssertionError((method,path))
  class Reader:
   def request(self,method,path,expected,body=None):
    calls.append(('reader:'+method,path))
    if '/standard_references?' in path and not path.startswith('/api/v1/catalog/'):
     assert expected==(403,)
     if corrupt=='allowed':raise SUITE.SuiteError('reader endpoint unexpectedly allowed')
    if 'view=inventory' in path:assert expected==(403,)
    if path.endswith('/resolve-sources'):return SimpleNamespace(payload={'results':[{'found':corrupt=='visible'} for _ in range(6)]})
    if path.startswith('/api/v1/catalog/domain_standard_references'):return SimpleNamespace(payload={'total':0,'data':[]})
    return SimpleNamespace(payload={'total_entries':0})
  with patch.object(SUITE._API,'cleanup_resource',return_value=True) as cleaned:
   try:
    result=SUITE.run_suite(Client(),Reader(),7,'test-run',51)
    return result,calls
   finally:
    self.assertEqual(cleaned.call_count,7)
    self.assertEqual(calls[-1],('GET','/api/v1/standard/elements/51'))

 def test_complete_contract_checks_cross_domain_and_all_six_forbidden_owner_routes(self):
  report,calls=self.run_fixture()
  self.assertTrue(report['external_domain_ownership'])
  self.assertTrue(report['reader_professional_references_denied'])
  self.assertEqual(sum(method=='reader:GET' and '/standard_references?' in path and not path.startswith('/api/v1/catalog/') for method,path in calls),6)
  self.assertEqual(report['cleanup'],'passed')

 def test_wrong_domain_frozen_draft_visibility_or_permission_regression_fails_and_cleans(self):
  for fault in ('local','frozen','visible','allowed'):
   with self.subTest(fault=fault),self.assertRaises(SUITE.SuiteError):self.run_fixture(fault)

if __name__=='__main__':unittest.main()
