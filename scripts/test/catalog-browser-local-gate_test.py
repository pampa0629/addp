import copy
import importlib
import json
from pathlib import Path
import shutil
import tempfile
import unittest
from unittest.mock import patch

GATE=importlib.import_module('scripts.test.catalog-browser-local-gate')
CHECK=importlib.import_module('scripts.ci.check-online-ci-registration')

class CatalogBrowserLifecycleTest(unittest.TestCase):
    def test_preflight_rejects_remote_and_foreign_profiles(self):
        GATE.validate_profile({},'unix:///local','arm64','aarch64')
        for environment,endpoint,machine,daemon in [({'GITHUB_ACTIONS':'false'},'unix:///local','arm64','arm64'),
                ({'ADDP_ONLINE_LOCAL_SGM':'1'},'unix:///local','arm64','arm64'),({},'tcp://remote','arm64','arm64'),
                ({},'unix:///local','arm64','amd64')]:
            with self.subTest(environment=environment),self.assertRaises(GATE.CORE.GateError):
                GATE.validate_profile(environment,endpoint,machine,daemon)

    def test_adapter_removes_personal_mounts_ports_and_restart_policy(self):
        doc={'services':{'postgres':{'image':'postgres','restart':'always','ports':[{'published':'25432','target':5432}],
             'volumes':[{'type':'bind','source':'/daily/database','target':'/data'}],'networks':['daily']}}}
        before=copy.deepcopy(doc)
        config=GATE.ISOLATE.isolate_compose(doc,['postgres'],'owned',{},label=GATE.LABEL)
        self.assertEqual(doc,before)
        self.assertNotIn('/daily',json.dumps(config));self.assertNotIn('25432',json.dumps(config))
        service=config['services']['postgres']
        self.assertEqual(service['restart'],'no');self.assertEqual(service['labels'][GATE.LABEL],'owned')
        self.assertEqual(service['ports'][0]['host_ip'],'127.0.0.1')
        self.assertTrue(config['networks']['isolated']['internal'])

    def test_evidence_cannot_pass_another_run_or_missing_browser_checks(self):
        ready={'run_id':'r','commit':'sha'}
        evidence={'run_id':'r','commit':'sha','browser':'Opera','checks':dict.fromkeys(GATE.CHECKS,True)}
        GATE.validate_evidence(evidence,ready)
        for change in ({'run_id':'other'},{'commit':'other'},{'browser':'Chromium'}, {'checks':{}},
                       {'checks':{**evidence['checks'],'optional_permissions':False}}):
            with self.assertRaises(GATE.CORE.GateError):GATE.validate_evidence({**evidence,**change},ready)

    def test_safe_environment_does_not_import_daily_database_or_secrets(self):
        with patch.dict('os.environ',{'PATH':'/bin','POSTGRES_HOST':'daily','POSTGRES_PASSWORD':'secret','ADDP_ONLINE_HOSTED':'1'},clear=True):
            self.assertEqual(GATE.safe_environment(),{'PATH':'/bin'})

    def test_cleanup_is_scoped_idempotent_and_records_failure(self):
        for fail in (False,True):
            life=GATE.Lifecycle();life.work.mkdir(mode=0o700)
            GATE.private_json(life.work/'state.json',{'run_id':life.uuid,'docker_host':'unix:///local','commit':'sha','initial':{}})
            calls=[];existing={'container':['owned-container'],'volume':['owned-volume'],'network':['owned-network']}
            def docker(_,kind,operation,*args,**kwargs):
                calls.append((kind,operation,args))
                if operation=='ls':return '\n'.join(existing[kind])
                if fail and kind=='volume':raise GATE.CORE.GateError('injected')
                existing[kind]=[];return ''
            try:
                with patch.object(GATE,'baseline',return_value={}),patch.object(GATE.Lifecycle,'docker',docker):
                    errors=GATE.cleanup(life.uuid,{'result':'passed'})
                result=json.loads(life.report_path.read_text())
                self.assertEqual(bool(errors),fail);self.assertEqual(result['result'],'failed' if fail else 'passed')
                self.assertEqual(result['zero_residue'],not fail)
                self.assertTrue(any(kind=='network' and operation=='rm' for kind,operation,args in calls))
                self.assertEqual(life.work.exists(),fail)
                if fail:
                    self.assertEqual({p.name for p in life.work.iterdir()},{'state.json','cleanup.lock'})
                    fail=False
                    with patch.object(GATE,'baseline',return_value={}),patch.object(GATE.Lifecycle,'docker',docker):
                        self.assertEqual(GATE.cleanup(life.uuid),[])
                self.assertEqual(GATE.cleanup(life.uuid),[])
                for kind,operation,args in calls:
                    if operation=='ls':self.assertIn('label='+GATE.LABEL+'='+life.run_id,args)
            finally:
                shutil.rmtree(life.work,ignore_errors=True);life.report_path.unlink(missing_ok=True)

    def test_watchdog_handles_parent_loss_and_expiry(self):
        for alive in (True,False):
            life=GATE.Lifecycle();life.work.mkdir(mode=0o700)
            GATE.private_json(life.work/'state.json',{'parent_pid':321,'deadline':0})
            calls=[]
            def kill(pid,sig):
                calls.append((pid,sig))
                if not alive:raise ProcessLookupError()
            try:
                with patch.object(GATE.os,'kill',side_effect=kill),patch.object(GATE.time,'sleep'),patch.object(GATE,'cleanup') as cleanup:
                    GATE.watch(life.uuid)
                    cleanup.assert_called_once_with(life.uuid)
                self.assertEqual(calls[0],(321,0))
            finally:shutil.rmtree(life.work)

    def test_registration_rejects_missing_cleanup_and_github_execution(self):
        paths=['Makefile','scripts/test/catalog-browser-local-gate.py','scripts/test/catalog-browser-local-gate_test.py',
               'scripts/test/catalog-browser-local-fixture.py','scripts/test/catalog-browser-local-fixture_test.py',
               'scripts/utils/disposable-compose.py','system/backend/cmd/online-test-fixture/catalog_browser_local.go',
               'system/backend/cmd/online-test-fixture/catalog_browser_local_postgres_test.go']
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory)
            for path in paths:
                target=root/path;target.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(GATE.ROOT/path,target)
            CHECK.validate_local_catalog_browser_profile(root)
            make=(root/'Makefile').read_text();(root/'Makefile').write_text(make.replace('clean-catalog-browser-local:','missing:'))
            with self.assertRaises(CHECK.RegistrationError):CHECK.validate_local_catalog_browser_profile(root)
            (root/'Makefile').write_text(make)
            workflows=root/'.github/workflows';workflows.mkdir(parents=True)
            (workflows/'bad.yml').write_text('run: make test-catalog-browser-local\n')
            with self.assertRaises(CHECK.RegistrationError):CHECK.validate_local_catalog_browser_profile(root)
