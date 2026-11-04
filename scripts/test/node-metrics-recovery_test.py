"""Node recovery transactions: no personal Docker or deployment state."""
from contextlib import redirect_stdout
import copy
import importlib.util
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock, patch

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('node_recovery_provider_test', ROOT/'scripts/infra/node-metrics.py')
provider = importlib.util.module_from_spec(spec); spec.loader.exec_module(provider)
M = provider.recovery


class NodeRecoveryTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(); self.addCleanup(temporary.cleanup)
        self.folder = Path(temporary.name).resolve()/'private'
        self.project = 'owned-node-test'; self.env = {'ADDP_NODE_METRICS_ENABLED':'true'}
        self.inputs = {'capacity':False, 'engine':'engine-1', 'template':'sha256-1'}
        self.items = {'node-exporter':self.fact('node-exporter')}; self.vol = None
        self.commands = []; self.start_failure = None
        self.docker = Mock(); self.docker.run.side_effect = self.command
        for target, name, value in ((M,'state_directory',lambda:self.folder), (M.deployment,'Docker',lambda env:self.docker)):
            change = patch.object(target,name,value); change.start(); self.addCleanup(change.stop)

    def fact(self,service):
        return {'id':service+'-identity', 'image':'sha256:'+service, 'running':True,
                'command':['fixed'], 'entrypoint':None, 'user':'65534:65534',
                'labels':{M.OWNER:str(ROOT), 'com.docker.compose.project':self.project,'com.docker.compose.service':service},
                'host':{'ReadonlyRootfs':True,'CapDrop':['ALL'],'Privileged':False},
                'networks':{'original':{'NetworkID':'network-id'}},
                'mounts':[{'Source':'/original','Destination':'/cert','RW':False}]}

    def command(self,args,**options):
        self.commands.append(args)
        if args[0]=='ps': return '\n'.join(item['id'] for item in self.items.values())
        if args[0]=='inspect': return json.dumps(next(item for item in self.items.values() if item['id']==args[-1]))
        if args[:2]==['volume','ls']: return self.project+'_block-capacity' if self.vol else ''
        if args[:2]==['volume','inspect']: return json.dumps([self.vol])
        if args[:2]==['image','inspect']: return json.dumps(args[-1])
        if args[0] in ('start','stop'):
            next(item for item in self.items.values() if item['id']==args[-1])['running'] = args[0]=='start'
            if self.start_failure: self.start_failure(args)
            return args[-1]
        raise AssertionError(args)

    def capacity(self):
        self.inputs['capacity'] = True; self.items['block-capacity'] = self.fact('block-capacity')
        self.vol = {'Name':self.project+'_block-capacity','Driver':'local','CreatedAt':'original',
                    'Labels':{M.OWNER:str(ROOT)}}

    def manage(self,action,operation=lambda:None):
        with redirect_stdout(io.StringIO()) as output:
            M.manage(action,self.env,self.project,lambda env,docker:copy.deepcopy(self.inputs),operation)
        return output.getvalue()

    def mutations(self): return [c for c in self.commands if c[0] in ('start','stop','run','create','pull','build')]

    def test_healthy_is_readonly_and_does_not_republish_proof(self):
        for capacity in (False, True):
            if capacity: self.capacity()
            self.manage('up'); path=self.folder/'installation.json'; before=path.stat()
            self.commands.clear(); self.assertIn('healthy; source unchanged',self.manage('recover'))
            self.assertEqual(self.mutations(),[])
            self.assertEqual((before.st_ino,before.st_mtime_ns),(path.stat().st_ino,path.stat().st_mtime_ns))
            self.assertNotIn('.Config.Env',M.FACT)

    def test_only_stopped_containers_start_capacity_first(self):
        self.capacity(); self.manage('up')
        self.items['block-capacity']['running']=False
        self.assertIn('source recovered',self.manage('recover'))
        self.assertEqual(self.mutations(),[['start','block-capacity-identity']])
        self.commands.clear()
        for item in self.items.values(): item['running']=False
        self.manage('recover')
        self.assertEqual(self.mutations(),[['start','block-capacity-identity'],['start','node-exporter-identity']])

    def test_absent_disabled_or_retired_proof_never_adopts(self):
        with self.assertRaisesRegex(ValueError,'proof missing') as result: self.manage('recover')
        self.assertEqual(result.exception.node_recovery_reason,'node-proof-missing')
        self.assertFalse(self.folder.exists()); self.assertEqual(self.commands,[])
        self.manage('up'); self.manage('down'); self.commands.clear()
        with self.assertRaisesRegex(ValueError,'disabled') as result: self.manage('recover')
        self.assertEqual(result.exception.node_recovery_reason,'node-proof-disabled')
        self.assertEqual(self.commands,[])
        self.env['ADDP_NODE_METRICS_ENABLED']='false'
        self.assertIn('not selected',self.manage('recover')); self.assertEqual(self.commands,[])

    def test_input_container_and_volume_drift_rejects_without_mutation(self):
        self.capacity(); self.manage('up')
        original = copy.deepcopy((self.inputs,self.items,self.vol))
        changes = [lambda:self.inputs.update(engine='other'),lambda:self.inputs.update(template='other'),
                   lambda:self.items['node-exporter'].update(id='replacement'),
                   lambda:self.items['node-exporter']['host'].update(Privileged=True),
                   lambda:self.items['node-exporter']['host'].update(Memory=1),
                   lambda:self.items['node-exporter']['networks'].update(foreign={'NetworkID':'other'}),
                   lambda:self.items['node-exporter']['mounts'][0].update(RW=True),
                   lambda:self.items['node-exporter']['labels'].update({M.OWNER:'/foreign'}),
                   lambda:self.items.pop('block-capacity'),lambda:self.vol.update(CreatedAt='recreated')]
        reasons = ['node-inputs-changed']*2 + ['node-installation-changed']*5 + [
            'node-ownership-rejected','node-source-incomplete','node-installation-changed']
        for change,reason in zip(changes,reasons):
            self.inputs,self.items,self.vol = copy.deepcopy(original); change(); self.commands.clear()
            with self.assertRaises(ValueError) as result: self.manage('recover')
            self.assertEqual(result.exception.node_recovery_reason,reason)
            self.assertEqual(self.mutations(),[])

    def test_invalid_proof_has_closed_diagnosis_without_docker_reads(self):
        self.manage('up'); path=self.folder/'installation.json'
        for text in ('private-invalid-json', '{"enabled":true,"enabled":false}', '[]', '{}', '{"enabled":"true"}'):
            path.write_text(text); self.commands.clear()
            with self.assertRaises(ValueError) as result:self.manage('recover')
            self.assertEqual(result.exception.node_recovery_reason,'node-proof-invalid')
            self.assertEqual(self.commands,[])

    def test_missing_image_has_closed_reason(self):
        self.manage('up'); base=self.command
        def missing(args,**options):
            if args[0]=='image': raise M.deployment.DeploymentError('private-detail')
            return base(args,**options)
        self.docker.run.side_effect=missing
        with self.assertRaises(ValueError) as result: self.manage('recover')
        self.assertEqual(result.exception.node_recovery_reason,'source-image-unavailable')
        self.assertNotIn('private',str(result.exception)); self.assertEqual(self.mutations(),[])

    def test_lost_start_reply_rolls_back_only_original_stopped_container(self):
        self.capacity(); self.manage('up'); self.items['block-capacity']['running']=False
        def fail(args):
            if args[0]=='start': raise ValueError('lost reply')
        self.start_failure=fail
        with self.assertRaisesRegex(ValueError,'lost reply') as result: self.manage('recover')
        self.assertFalse(getattr(result.exception,'node_recovery_cleanup_failed',False))
        self.assertEqual(self.mutations(),[['start','block-capacity-identity'],['stop','--time','5','block-capacity-identity']])
        self.assertTrue(self.items['node-exporter']['running']); self.assertFalse(self.items['block-capacity']['running'])

    def test_cleanup_drift_is_reported_without_stopping_unproven_container(self):
        self.manage('up'); self.items['node-exporter']['running']=False
        def fail(args):
            if args[0]=='start':
                self.items['node-exporter']['host']['Privileged']=True
                raise ValueError('lost reply')
        self.start_failure=fail
        with self.assertRaises(ValueError) as result:self.manage('recover')
        self.assertTrue(result.exception.node_recovery_cleanup_failed)
        self.assertEqual(self.mutations(),[['start','node-exporter-identity']])

    def test_interrupted_start_cleans_only_its_original_container(self):
        self.manage('up');self.items['node-exporter']['running']=False
        def fail(args):
            if args[0]=='start':raise KeyboardInterrupt()
        self.start_failure=fail
        with self.assertRaises(KeyboardInterrupt):self.manage('recover')
        self.assertFalse(self.items['node-exporter']['running'])
        self.assertEqual(self.mutations(),[['start','node-exporter-identity'],['stop','--time','5','node-exporter-identity']])

    def test_source_already_started_before_our_command_is_not_owned_by_cleanup(self):
        self.manage('up');self.items['node-exporter']['running']=False
        count=0
        def signature(env,docker):
            nonlocal count
            count+=1
            if count==3:self.items['node-exporter']['running']=True
            return copy.deepcopy(self.inputs)
        with redirect_stdout(io.StringIO()):M.manage('recover',self.env,self.project,signature,lambda:None)
        self.assertTrue(self.items['node-exporter']['running']);self.assertEqual(self.mutations(),[])

    def test_failed_up_and_changed_inputs_retire_old_proof(self):
        self.manage('up')
        def broken(): raise ValueError('compose failed')
        with self.assertRaisesRegex(ValueError,'compose failed'):self.manage('up',broken)
        with self.assertRaisesRegex(ValueError,'disabled'):self.manage('recover')
        with self.assertRaisesRegex(ValueError,'changed during up'):
            self.manage('up',lambda:self.inputs.update(engine='changed'))
        self.assertEqual(json.loads((self.folder/'installation.json').read_text()),{'enabled':False})

    def test_lock_and_proof_files_fail_closed(self):
        self.manage('up'); self.commands.clear()
        with M.private.locked(self.folder),self.assertRaisesRegex(ValueError,'busy'):self.manage('recover')
        path=self.folder/'installation.json'; path.chmod(0o644)
        with self.assertRaises(ValueError) as result:self.manage('recover')
        self.assertEqual(result.exception.node_recovery_reason,'node-proof-invalid')
        path.unlink(); path.symlink_to(self.folder/'control.lock')
        with self.assertRaises(OSError) as result:self.manage('recover')
        self.assertEqual(result.exception.node_recovery_reason,'node-proof-invalid')
        self.assertEqual(self.mutations(),[])

    def test_provider_signature_binds_files_engine_and_kernel_without_secret_content(self):
        tls=self.folder.parent/'tls'; tls.mkdir()
        for name in ('ca.crt','server.crt','server.key'): (tls/name).write_text('private-'+name)
        env=dict(self.env,ADDP_NODE_METRICS_TLS_DIR=str(tls),ADDP_NODE_METRICS_LISTEN='127.0.0.1:9100')
        docker=Mock(); info={'id':'engine','kernel':'6.12.0-test','os':'Docker Desktop','type':'linux'}
        docker.run.side_effect=lambda *a,**k:json.dumps(info)
        with patch.object(provider.platform,'system',return_value='Darwin'):
            before=provider.installation_inputs(env,docker)
            self.assertNotIn('private-',json.dumps(before))
            pending=tls/'pending'; pending.write_text('private-server.key'); pending.replace(tls/'server.key')
            self.assertNotEqual(before,provider.installation_inputs(env,docker))
            info['id']='other'; self.assertNotEqual(before,provider.installation_inputs(env,docker))
        with patch.object(provider.platform,'system',return_value='Linux'):
            with self.assertRaises(ValueError):provider.installation_inputs(env,docker)

    def test_provider_has_one_transaction_route_and_preserves_admission_boundary(self):
        with patch.object(provider.recovery,'manage') as transaction,patch.object(provider,'_run') as original:
            for action in ('recover','down'):
                provider.run(action,self.env)
                call=transaction.call_args
                self.assertEqual(call.args[:4],(action,self.env,provider.PROJECT,provider.installation_inputs))
                original.assert_not_called()
                call.args[4]();original.assert_called_once_with(action,self.env);original.reset_mock()
            with self.assertRaises(ValueError):provider.run('recover',self.env,display_name='host')

    def test_ci_and_changed_selection_include_all_recovery_inputs(self):
        gate=(ROOT/'scripts/test/monitor-metrics-gate.sh').read_text()
        self.assertIn('--case=node-recovery) metrics_case=node-recovery ;;',gate)
        self.assertIn('exec bash "$ROOT_DIR/scripts/test/node-recovery-fixture.sh"',gate)
        inputs=gate.split('# ADDP_T2_INPUT_FILES=',1)[1].splitlines()[0].split()
        for name in ('scripts/infra/node-recovery.py','scripts/test/node-metrics-recovery_test.py',
                     'scripts/test/node-metrics-recovery-probe.py','scripts/test/node-recovery-fixture.sh'):
            self.assertIn(name,inputs)
        workflow=(ROOT/'.github/workflows/release-and-t2-gates.yml').read_text()
        matrix=workflow.split('  monitor-metrics-cases:',1)[1].split('  monitor-metrics:',1)[0]
        self.assertIn('block-capacity, node-recovery]',matrix)


if __name__=='__main__':unittest.main()
