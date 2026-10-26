import importlib
from pathlib import Path
import unittest
from datetime import datetime,timezone,timedelta

M=importlib.import_module('scripts.test.execution-events-pagination-online')

class EventPaginationOnlineTest(unittest.TestCase):
 def page(self):
  return dict(items=[dict(id=i,execution_id='e',attempt=1,occurred_at=datetime.now(timezone.utc).isoformat(),kind='progress',counters={'records_read':i}) for i in range(1,101)],next_cursor=100,has_more=True,retained_after=(datetime.now(timezone.utc)-timedelta(days=30)).isoformat())
 def test_closed_page_and_strict_cursor(self):
  M.assert_page(self.page(),'e',0,list(range(1,102)))
  for mutate in [lambda p:p.update(execution_config={}),lambda p:p.update(next_cursor=99),lambda p:p.update(has_more=False),lambda p:p['items'][1].update(id=1),lambda p:p['items'][0].update(counters={'secret':1}),lambda p:p['items'][0].update(execution_id='other')]:
   p=self.page();mutate(p)
   with self.assertRaises(M.API.SuiteError):M.assert_page(p,'e',0,list(range(1,102)))
 def test_invalid_truncation_shape_and_private_keys_rejected(self):
  p=self.page();p['items'][-1].update(kind='truncated',counters={'records_read':1})
  with self.assertRaises(M.API.SuiteError):M.assert_page(p,'e',0,list(range(1,102)))
  with self.assertRaises(M.API.SuiteError):M.assert_safe({'nested':{'lease_token':'private'}})
 def test_empty_terminal_page_has_zero_cursor(self):
  page=dict(items=[],next_cursor=0,has_more=False,retained_after=(datetime.now(timezone.utc)-timedelta(days=30)).isoformat())
  self.assertEqual(M.assert_page(page,'e',100,list(range(1,101))),[])
  page['next_cursor']=100
  with self.assertRaises(M.API.SuiteError):M.assert_page(page,'e',100,list(range(1,101)))
 def test_profile_requires_manual_job_real_owner_and_fixture_entry(self):
  import tempfile,shutil
  check=importlib.import_module('scripts.ci.check-online-ci-registration')
  check.validate_event_pagination_profile(M.ROOT,{M.SUITE})
  with tempfile.TemporaryDirectory() as tmp:
   root=Path(tmp)
   for n in ('.github/workflows/online-t4-gates.yml','Makefile','scripts/test/online-hosted-event-pagination-gate.sh'):
    p=root/n;p.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(M.ROOT/n,p)
   for n,part in [('.github/workflows/online-t4-gates.yml',"inputs.suite != 'execution-events-pagination'"),
                  ('scripts/test/online-hosted-event-pagination-gate.sh','scripts/dev/start.sh -meta -monitor'),
                  ('Makefile','ADDP_EXECUTION_EVENT_FIXTURE=1'),
                  ('Makefile','$(MAKE) test-execution-events-pagination-runner')]:
    p=root/n;original=p.read_text();p.write_text(original.replace(part,'removed'))
    with self.subTest(part=part),self.assertRaises(check.RegistrationError):check.validate_event_pagination_profile(root,{M.SUITE})
    p.write_text(original)
 def test_private_process_timeout_reaps_only_its_owned_process_group(self):
  import tempfile,sys,os,subprocess
  with tempfile.TemporaryDirectory() as tmp:
   output=Path(tmp)/'private.log'
   with self.assertRaises(subprocess.TimeoutExpired):
    M.run_private([sys.executable,'-u','-c','import os,time;print(os.getpid());time.sleep(60)'],Path(tmp),dict(os.environ),output,.3)
   pid=int(output.read_text().strip())
   with self.assertRaises(ProcessLookupError):os.kill(pid,0)
 def test_browser_and_fixture_are_bound_to_standard_entrypoints(self):
  source=Path(M.__file__).read_text()
  self.assertIn('test-execution-event-pagination-fixture',source)
  self.assertIn('playwright.online.config.js',source)
  self.assertNotIn('INSERT INTO',source)
  self.assertNotIn('DELETE FROM',source)
  browser=(M.ROOT/'console/frontend/e2e/online/execution-events-pagination.spec.js').read_text()
  self.assertNotIn('.route(',browser)
  self.assertIn('login(',browser)
  self.assertIn('toHaveCount(1000)',browser)
 def test_history_tail_rebinds_the_current_browser_response(self):
  browser=(M.ROOT/'console/frontend/e2e/online/execution-events-pagination.spec.js').read_text()
  self.assertIn('browserAPIFromResponse(currentResponse',browser)
  self.assertIn('currentResponse = response',browser)
  self.assertNotIn('await api.get(`${pathFor(id)}',browser)
  for stage in ('_initial', '_first_page', '_dom', '_tail_identity', '_tail'):
   self.assertIn(stage,browser)

if __name__=='__main__':unittest.main()
