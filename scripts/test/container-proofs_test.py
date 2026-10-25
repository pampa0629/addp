"""Pure host-proof fixtures. Never read or change a personal container."""
import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import os

ROOT=Path(__file__).resolve().parents[2]
SPEC=importlib.util.spec_from_file_location('container_proof',ROOT/'scripts/utils/container-proof.py')
proof=importlib.util.module_from_spec(SPEC);SPEC.loader.exec_module(proof)
CID='a'*64


class ProofTests(unittest.TestCase):
    def test_production_adapter_uses_existing_opt_in_and_read_only_mounts(self):
        spec=importlib.util.spec_from_file_location('process_platform',ROOT/'scripts/prod/generate-process-metrics-platform.py')
        adapter=importlib.util.module_from_spec(spec);spec.loader.exec_module(adapter)
        with tempfile.TemporaryDirectory(prefix='addp-container-compose-') as folder:
            base=Path(folder);tls=base/'tls';directory=base/'metrics';discovery=base/'discovery'
            for p in (tls,directory):p.mkdir(mode=0o700)
            for name in ('ca.crt','server.crt','server.key'):(tls/name).write_text('deployment-owned fixture');(tls/name).chmod(0o600)
            item={'module_name':'monitor','role':'backend','listen':'0.0.0.0:9445','endpoint':'https://monitor-backend:9445/metrics','tls_dir':str(tls),'cgroup_v2':True}
            with patch.dict(os.environ,{'ADDP_METRICS_DEPLOYMENT_DIR':str(directory),'ADDP_DEPLOYMENT_DISCOVERY_DIR':str(discovery),'ADDP_PROCESS_METRICS_DEPLOYMENTS':json.dumps([item])}):adapter.render()
            result=json.loads((directory/'platform-process-metrics.json').read_text())['services']
            projected=json.loads(result['monitor-backend']['environment']['ADDP_PROCESS_METRICS_DEPLOYMENTS'])
            self.assertTrue(projected[0]['cgroup_v2']);self.assertEqual(projected[0]['tls_dir'],'/etc/addp/process-metrics/source')
            self.assertEqual(set(result), {'monitor-backend'})
            self.assertFalse(discovery.exists())
            # Discovery is owned by the standard lifecycle, not by SDK opt-in.
            for row in result.values():
                self.assertNotIn('ADDP_DEPLOYMENT_DISCOVERY_DIR', row['environment'])
                self.assertEqual({m['target'] for m in row['volumes']}, {'/etc/addp/process-metrics/source'})
                self.assertNotIn('cgroup',row);self.assertNotIn('privileged',row)
                for mount in row['volumes']:self.assertTrue(mount['read_only']);self.assertFalse(mount['bind']['create_host_path'])
            for invalid in ([dict(item,cgroup_v2='true')],[item,item],[dict(item,module_name='unknown')],[dict(item,password='sentinel')]):
                with patch.dict(os.environ,{'ADDP_METRICS_DEPLOYMENT_DIR':str(directory),'ADDP_DEPLOYMENT_DISCOVERY_DIR':str(discovery),'ADDP_PROCESS_METRICS_DEPLOYMENTS':json.dumps(invalid)}),self.assertRaises(ValueError):adapter.render()

    def test_standard_lifecycle_owns_system_discovery_with_and_without_sdk_opt_in(self):
        spec=importlib.util.spec_from_file_location('proof_lifecycle_inventory',ROOT/'scripts/utils/deployment-inventory.py')
        inventory=importlib.util.module_from_spec(spec);spec.loader.exec_module(inventory)
        spec=importlib.util.spec_from_file_location('proof_lifecycle_sdk',ROOT/'scripts/prod/generate-process-metrics-platform.py')
        adapter=importlib.util.module_from_spec(spec);spec.loader.exec_module(adapter)
        with tempfile.TemporaryDirectory(prefix='addp-proof-lifecycle-') as folder:
            base=Path(folder);directory=base/'metrics';tls=base/'tls';discovery=base/'discovery'
            for path in (directory,tls,discovery):path.mkdir(mode=0o700)
            for name in ('ca.crt','server.crt','server.key'):
                (tls/name).write_text('deployment-owned fixture');(tls/name).chmod(0o600)
            compose=base/'platform.yml';compose.write_text('services: {}')
            rendered=json.dumps({'services':{'system-backend':{'environment':{'PASSWORD':'do-not-copy'}},
                                            'meta-worker':{'deploy':{'replicas':2}}}}).encode()
            # Only config rendering is mocked; no container or personal service is used.
            with patch.object(inventory.subprocess,'run') as config:
                config.return_value.stdout=rendered
                inventory.publish_platform_service_expectations(discovery,[str(compose)])
            self.assertEqual(config.call_args.args[0],['docker','compose','-f',str(compose),'config','--format','json'])
            overlay=json.loads((discovery/'platform-service-expectations.json').read_text())
            self.assertEqual(set(overlay['services']),{'system-backend'})
            system=overlay['services']['system-backend']
            self.assertEqual(system,{
                'environment':{'ADDP_DEPLOYMENT_DISCOVERY_DIR':'/etc/addp/deployment-discovery'},
                'volumes':[{'type':'bind','source':str(discovery),'target':'/etc/addp/deployment-discovery',
                            'read_only':True,'bind':{'create_host_path':False}}]})
            self.assertNotIn('do-not-copy',json.dumps(overlay))
            original=(discovery/'platform-service-expectations.json').read_bytes()
            sdk={'module_name':'system','role':'backend','listen':'0.0.0.0:9445',
                 'endpoint':'https://system-backend:9445/metrics','tls_dir':str(tls),'cgroup_v2':True}
            for items in ([],[sdk]):
                with self.subTest(sdk_enabled=bool(items)),patch.dict(os.environ,{
                    'ADDP_METRICS_DEPLOYMENT_DIR':str(directory),'ADDP_DEPLOYMENT_DISCOVERY_DIR':str(discovery),
                    'ADDP_PROCESS_METRICS_DEPLOYMENTS':json.dumps(items)}):
                    adapter.render()
                    services=json.loads((directory/'platform-process-metrics.json').read_text())['services']
                    self.assertEqual(set(services),{'system-backend'} if items else set())
                    for row in services.values():
                        self.assertNotIn('ADDP_DEPLOYMENT_DISCOVERY_DIR',row['environment'])
                        self.assertNotIn('cgroup',row);self.assertNotIn('privileged',row)
                        self.assertEqual(row['volumes'],[{'type':'bind','source':str(tls),
                            'target':'/etc/addp/process-metrics/source','read_only':True,'bind':{'create_host_path':False}}])
                    self.assertEqual((discovery/'platform-service-expectations.json').read_bytes(),original)

    def test_kernel_reader_requires_host_root_and_every_parent(self):
        with tempfile.TemporaryDirectory(prefix='addp-cgroup-proof-') as folder:
            base=Path(folder);proc=base/'proc';root=base/'cgroup';root.mkdir()
            for name in ('self/ns','1/ns','100/ns','sys/kernel/random'):(proc/name).mkdir(parents=True)
            for name in ('pid','cgroup'):
                (proc/'self/ns'/name).write_text('identity')
                (proc/'1/ns'/name).hardlink_to(proc/'self/ns'/name)
            (proc/'100/ns/pid').write_text('container namespace')
            (proc/'sys/kernel/random/boot_id').write_text('11111111-1111-4111-8111-111111111111\n')
            (proc/'self/mountinfo').write_text(f'1 0 0:1 / {root} rw - cgroup2 cgroup rw\n')
            (proc/'100/cgroup').write_text(f'0::/docker/{CID}\n')
            leaf=root/'docker'/CID;leaf.mkdir(parents=True)
            for group,cpu,mem in [(leaf,'200000 100000','268435456'),(leaf.parent,'50000 100000','134217728')]:
                for name,value in {'cpu.max':cpu,'cpu.max.burst':'0','memory.max':mem,'cpuset.cpus':'\n','cpuset.cpus.effective':'0-3'}.items():(group/name).write_text(value)
            result=proof.kernel_proof(100,CID,proc=proc,root=root)
            self.assertEqual(result['cpu_cores'],.5);self.assertEqual(result['memory_bytes'],134217728)
            self.assertEqual(result['identity']['cgroup_id'],str(leaf.stat().st_ino))
            (leaf.parent/'memory.max').unlink()
            with self.assertRaises(OSError):proof.kernel_proof(100,CID,proc=proc,root=root)
            (proc/'self/mountinfo').write_text(f'1 0 0:1 /hidden {root} rw - cgroup2 cgroup rw\n')
            with self.assertRaises(ValueError):proof.kernel_proof(100,CID,proc=proc,root=root)

    def test_hierarchy_limits_include_stricter_parent_and_no_host_allocation_guess(self):
        leaf={'cpu.max':'200000 100000','cpu.max.burst':'0','memory.max':'268435456','cpuset.cpus':'','cpuset.cpus.effective':'0-3'}
        parent=dict(leaf,**{'cpu.max':'50000 100000','memory.max':'134217728'})
        self.assertEqual(proof.limits([leaf,parent]),(.5,134217728))
        unlimited=dict(leaf,**{'cpu.max':'max 100000','memory.max':'max'})
        self.assertEqual(proof.limits([unlimited]),(None,None))
        self.assertEqual(proof.limits([dict(unlimited,**{'cpuset.cpus':'0-1','cpuset.cpus.effective':'0-1'})]),(2,None))
        for field,bad in [('cpu.max.burst','1000'),('cpu.max','0 100000'),('cpu.max','1 0'),('memory.max','-1'),('memory.max',str(2**53)),('cpuset.cpus.effective','0-999999999'),('cpuset.cpus.effective','1-3,2')]:
            with self.subTest(field=field,bad=bad),self.assertRaises(ValueError):proof.limits([dict(leaf,**{field:bad})])

    def test_exact_container_and_stable_epoch_reset(self):
        value={'id':CID,'running':True,'pid':100,'started_at':'2026-10-24T00:00:00Z','project':'addp-platform','working_dir':str(ROOT),'module':'monitor','role':'worker'}
        current={'identity':{'kernel_boot_id':'11111111-1111-4111-8111-111111111111','cgroup_id':'10','pid_namespace_id':'20'},'cpu_cores':.5,'memory_bytes':134217728,'hierarchy_digest':'b'*64}
        def docker(*args):return CID if args[0]=='ps' else json.dumps(value)
        def discover(at,previous=None):return proof.discover(docker,{'id':'engine'},ROOT,at,previous,kernel=lambda *_:copy.deepcopy(current))
        first=discover('2026-10-24T00:01:00Z')
        second=discover('2026-10-24T00:01:30Z',first)
        self.assertEqual(second['proofs'][0]['stable_since'],'2026-10-24T00:01:00Z')
        current['hierarchy_digest']='c'*64
        self.assertEqual(discover('2026-10-24T00:02:00Z',second)['proofs'][0]['stable_since'],'2026-10-24T00:02:00Z')
        self.assertEqual(discover('2026-10-24T00:03:01Z',first)['proofs'][0]['stable_since'],'2026-10-24T00:03:01Z')
        value['working_dir']='/foreign'
        with self.assertRaises(ValueError):discover('2026-10-24T00:02:00Z')

    def test_partial_hierarchy_never_publishes_a_proof(self):
        value={'id':CID,'running':True,'pid':100,'started_at':'2026-10-24T00:00:00Z','project':'addp-platform','working_dir':str(ROOT),'module':'monitor','role':'worker'}
        def docker(*args):return CID if args[0]=='ps' else json.dumps(value)
        def missing(*_):raise OSError('private host hierarchy unavailable')
        self.assertEqual(proof.discover(docker,{'id':'engine'},ROOT,'2026-10-24T00:01:00Z',kernel=missing)['proofs'],[])


if __name__=='__main__':unittest.main()
