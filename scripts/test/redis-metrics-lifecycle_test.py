"""Deterministic Redis deployment transitions; no personal Docker or database."""
import copy
from contextlib import nullcontext
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

ROOT=Path(__file__).resolve().parents[2]
spec=importlib.util.spec_from_file_location('redis_metrics_lifecycle',ROOT/'scripts/infra/redis-metrics.py')
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)


def plan(scope,password,owner):
    digest=hashlib.sha256(password.encode()).hexdigest();user='addp_'+scope+'_observer'
    enabled=['ACL','SETUSER',user,'reset','on','#'+digest,'+hello','+ping','+info']
    disabled=['ACL','SETUSER',user,'reset','off']
    record=lambda args:'addp.redis-observation/v1:'+scope+':'+owner+'\nuser '+user+' '+' '.join(args[3:])+'\n'
    return {'enabled':enabled,'disabled':disabled,'enabled_record':record(enabled),'disabled_record':record(disabled),'password_hash':digest}


class RedisLifecycleTest(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
        self.state=Path(self.tmp.name).resolve()/'state'
        self.events=[];self.account=None;self.record=''
        self.docker=mock.Mock(info={'id':'fixture-engine'})
        self.docker.run.side_effect=lambda args,**kwargs:subprocess.CompletedProcess(args,0,self.db.image,'') if args[:2]==['image','inspect'] else '' if args[0] in ('ps','volume') else self.fail('Unexpected transport command')
        self.db=mock.Mock(identity='a'*64,volume='owned-volume',image='sha256:'+'c'*64)
        self.db.facts={'labels':{'com.docker.compose.project':'fixture','com.docker.compose.service':'redis'},'started_at':'2026-11-01T00:00:00Z'}
        self.db.locked.side_effect=lambda:nullcontext()
        self.db.account.side_effect=lambda:copy.deepcopy(self.account)
        self.db.record.side_effect=lambda:self.record
        self.db.replace_record.side_effect=self.replace
        self.db.cli.side_effect=self.apply
        self.manager=module.Lifecycle(self.docker,'business',self.state,lambda:self.db,project='addp-redis-t2-lifecycle')
        self.manager.image=mock.Mock(return_value='sha256:'+'d'*64)
        self.manager.plan=mock.Mock(side_effect=lambda image,state:plan(state['scope'],state['password'],state['owner']))
        self.manager.stop_sources=mock.Mock(side_effect=lambda:self.events.append('stop'))
        self.manager.start_source=mock.Mock(side_effect=lambda *args:self.events.append('start'))
        self.real_transport=module.transport
        self.transport=mock.patch.object(module,'transport',return_value=('127.0.0.1',9443,{}));self.transport.start();self.addCleanup(self.transport.stop)

    def replace(self,expected,value):
        self.assertEqual(expected,self.record)
        self.record=value;self.events.append('persist-on' if ' reset on ' in value else 'persist-off')

    def apply(self,args,last=None):
        enabled='on' in args
        self.account={'flags':['on' if enabled else 'off'],'passwords':[last[1:]] if enabled else [],
            'commands':'-@all +hello +ping +info' if enabled else '-@all','keys':'','channels':'','selectors':[]}
        self.events.append('enable' if enabled else 'disable');return 'OK'

    def up(self):self.manager.run('up',{'ADDP_REDIS_METRICS_ENABLED':'true'})
    def state_json(self):return module.decode(module.private.read(self.state/'owner.json',0o600)[0])

    def test_installation_persists_credentials_and_disable_precedes_start(self):
        self.up();first=self.state_json()
        self.assertEqual(self.events,['stop','disable','persist-off','enable','persist-on','start'])
        self.assertEqual(first['record'],self.record);self.assertEqual(first['pending_record'],'')
        self.events.clear();self.up();second=self.state_json()
        self.assertEqual(first['password'],second['password']);self.assertEqual(first['owner'],second['owner'])
        self.assertEqual(first['source_key'],second['source_key'])

    def test_down_needs_no_tls_build_or_secret_rotation(self):
        self.up();before=self.state_json();self.events.clear();self.manager.image.reset_mock()
        self.manager.run('down',{})
        self.assertEqual(self.events,['stop','disable','persist-off']);self.manager.image.assert_not_called()
        self.assertEqual(self.state_json()['password'],before['password']);self.assertTrue(self.record.endswith(' reset off\n'))

    def test_source_failure_disables_both_runtime_and_persisted_account(self):
        self.manager.start_source.side_effect=module.Error('source start failed')
        with self.assertRaises(module.Error):self.up()
        state=self.state_json();self.assertEqual(state['pending_record'],'');self.assertTrue(self.record.endswith(' reset off\n'))
        self.assertEqual(self.account['commands'],'-@all');self.assertEqual(self.account['passwords'],[])

    def test_preparation_failure_preserves_source_account_and_installation(self):
        self.up(); before = (self.state/'owner.json').read_bytes()
        account = copy.deepcopy(self.account); record = self.record
        for step in ('transport','build','plan'):
            self.events.clear()
            target = module if step=='transport' else self.manager
            name = {'transport':'transport','build':'image','plan':'plan'}[step]
            with self.subTest(step=step), mock.patch.object(target,name,side_effect=module.Error('preparation failed')):
                with self.assertRaisesRegex(module.Error,'preparation failed'): self.up()
            self.assertEqual(self.events,[])
            self.assertEqual(self.account,account);self.assertEqual(self.record,record)
            self.assertEqual((self.state/'owner.json').read_bytes(),before)

    def test_account_failure_keeps_its_stage_and_cleanup_result(self):
        failure=module.Error('account operation failed')
        self.db.cli.side_effect=failure
        with self.assertRaises(module.Error) as result:self.up()
        self.assertIs(result.exception,failure)
        self.assertEqual(result.exception.observation_stage,'account-update')
        self.assertTrue(result.exception.redis_cleanup_failed)
        self.manager.start_source.assert_not_called()

    def test_refuses_existing_account_or_record_without_private_ownership(self):
        for which in ('account','record'):
            with self.subTest(which=which):
                if self.state.exists():
                    for path in self.state.iterdir():path.unlink()
                self.account={} if which=='account' else None;self.record='foreign-record' if which=='record' else ''
                self.db.cli.reset_mock()
                with self.assertRaises(module.Error):self.up()
                self.db.cli.assert_not_called()

    def test_permission_volume_and_persisted_record_drift_are_not_adopted(self):
        self.up();baseline=self.state_json();account=copy.deepcopy(self.account);record=self.record
        for issue in ('permission','volume','record','password','duplicate-json'):
            with self.subTest(issue=issue):
                self.account=copy.deepcopy(account);self.record=record;self.db.volume='owned-volume'
                module.write(self.state/'owner.json',json.dumps(baseline))
                if issue=='permission':self.account['commands']='-@all +get +info'
                if issue=='volume':self.db.volume='foreign-volume'
                if issue=='record':self.record='unowned-record'
                if issue=='password':
                    value=dict(baseline);value.pop('password');module.write(self.state/'owner.json',json.dumps(value))
                if issue=='duplicate-json':module.write(self.state/'owner.json','{"scope":"business","scope":"infra"}')
                self.db.cli.reset_mock()
                with self.assertRaises((module.Error,KeyError)):self.up()
                self.db.cli.assert_not_called()

    def test_journal_accepts_only_exact_previous_or_pending_owned_record(self):
        self.up();state=self.state_json();p=plan('business',state['password'],state['owner'])
        state['pending_record']=p['disabled_record'];module.write(self.state/'owner.json',json.dumps(state));self.record=p['disabled_record']
        self.account={'flags':['off'],'passwords':[],'commands':'-@all','keys':'','channels':'','selectors':[]}
        self.up();self.assertEqual(self.state_json()['pending_record'],'');self.assertTrue(self.record.endswith(' +info\n'))

    def test_private_state_lock_and_link_permissions_remain_strict(self):
        self.up()
        with module.private.locked(self.state),self.assertRaisesRegex(ValueError,'busy'):self.up()
        path=self.state/'owner.json';target=self.state/'real.json';path.rename(target);path.symlink_to(target)
        with self.assertRaises(OSError):self.up()

    def test_unconfigured_scope_does_not_contact_docker_or_redis(self):
        with mock.patch.object(module.deployment,'Docker',side_effect=AssertionError('must not contact Docker')):
            module.run('up','infra',{'ADDP_REDIS_METRICS_STATE_DIR':str(self.state.parent/'uninstalled')})

    def test_recovery_never_changes_the_account_record_or_private_credentials(self):
        self.up(); self.events.clear(); self.manager.image.reset_mock(); self.db.locked.reset_mock()
        before = (self.state/'owner.json').read_bytes()
        source = mock.Mock()
        source.recover.side_effect = lambda *args,**kwargs:(args[5](),'healthy; source unchanged')[1]
        with mock.patch.object(self.manager,'container_source',return_value=source):
            self.assertIn('unchanged',self.manager.run('recover',{'ADDP_REDIS_METRICS_ENABLED':'true'}))
        self.assertEqual(self.events,[])
        self.assertEqual((self.state/'owner.json').read_bytes(),before)
        self.manager.image.assert_not_called(); self.db.locked.assert_not_called()
        source.stop.assert_not_called(); source.build.assert_not_called()

    def test_recovery_refuses_disabled_drifted_and_incomplete_accounts_without_repair(self):
        self.up(); baseline=self.state_json(); account=copy.deepcopy(self.account); record=self.record
        for problem in ('disabled','record','pending','image','volume','failure'):
            with self.subTest(problem=problem):
                self.account=copy.deepcopy(account);self.record=record;self.db.volume='owned-volume'
                state=dict(baseline)
                if problem=='disabled': self.account['flags']=['off']
                if problem=='record': self.record='foreign'
                if problem=='pending': state['pending_record']=record
                if problem=='image': state['image']=''
                if problem=='volume': self.db.volume='another-volume'
                module.write(self.state/'owner.json',json.dumps(state)); self.events.clear()
                source=mock.Mock();source.recover.side_effect=module.Error('source unavailable')
                with mock.patch.object(self.manager,'container_source',return_value=source),self.assertRaises(ValueError):
                    self.manager.run('recover',{'ADDP_REDIS_METRICS_ENABLED':'true'})
                self.assertEqual(self.events,[])
                source.stop.assert_not_called()
                if problem!='failure': source.recover.assert_not_called()

    def test_unselected_and_absent_recovery_do_not_construct_docker(self):
        with mock.patch.object(module.deployment,'Docker') as docker:
            for enabled in ('false','true'):
                module.run('recover','infra',{'ADDP_REDIS_METRICS_ENABLED':enabled,'ADDP_REDIS_METRICS_STATE_DIR':str(self.state)})
            docker.assert_not_called()

    def test_foreign_source_or_volume_is_never_removed(self):
        manager=module.Lifecycle(self.docker,'business',self.state,lambda:self.db,project='owned')
        self.docker.run.side_effect=lambda args,**kwargs:'a'*64 if args[0]=='ps' else self.fail('mutating foreign resources')
        self.docker.fact.return_value={'labels':{module.OWNER:'/foreign',module.SCOPE:'business','com.docker.compose.service':'redis-metrics'}}
        with self.assertRaisesRegex(module.Error,'foreign'):manager.stop_sources()

    def test_publication_rejects_noncanonical_public_or_unbounded_ports_before_tls(self):
        for listen in ('0.0.0.0:9443','8.8.8.8:9443','127.0.0.1:65536','127.0.0.1:0','[::ffff:127.0.0.1]:9443','[fe80::1]:9443'):
            with self.subTest(listen=listen),self.assertRaises(ValueError):self.real_transport({'ADDP_REDIS_METRICS_LISTEN':listen})


class RedisLifecycleScriptTest(unittest.TestCase):
    def test_business_unsets_both_scope_inputs_before_configuration(self):
        start=(ROOT/'business/scripts/start.sh').read_text()
        self.assertIn('unset ADDP_REDIS_METRICS_ENABLED ADDP_REDIS_METRICS_TLS_DIR ADDP_REDIS_METRICS_LISTEN ADDP_REDIS_METRICS_STATE_DIR',start)
        start=start[start.index('# Redis\n'):]
        self.assertLess(start.index('metrics-sources.py" down --resolved-env --scope business --kind redis'),start.index('docker compose up'))
        stop=(ROOT/'business/scripts/stop.sh').read_text()
        self.assertIn('observation_kinds+=(--kind redis)',stop)
        self.assertIn('metrics-sources.py" down --scope business',stop)


if __name__=='__main__':unittest.main()
