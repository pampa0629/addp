"""No Docker: profile, strict sampling, product projection and ownership contracts."""
from pathlib import Path
import importlib.util
import json
import tempfile
import threading
import time
import unittest
from unittest import mock
from types import SimpleNamespace
spec=importlib.util.spec_from_file_location('http_local',Path(__file__).with_name('monitor-http-local-gate.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)

class HTTPProductLocalTests(unittest.TestCase):
    def test_forbidden_profiles_and_remote_architecture(self):
        m.validate_profile({},'arm64','aarch64')
        for marker in ('GITHUB_ACTIONS','ADDP_ONLINE_HOSTED','ADDP_ONLINE_OWNER_MANAGED','ADDP_EXECUTION_TRACE_LOCAL','ADDP_CATALOG_BROWSER_LOCAL'):
            with self.subTest(marker=marker),self.assertRaises(m.core.GateError):m.validate_profile({marker:'1'},'arm64','arm64')
        with self.assertRaises(m.core.GateError):m.validate_profile({},'x86_64','arm64')

    def test_dynamic_subnet_requires_private_sufficient_ipv4(self):
        addresses=m.source_addresses('172.29.0.0/16')
        self.assertEqual(addresses,{'agent':'172.29.255.245','copilot':'172.29.255.244'})
        for subnet in ('8.8.8.0/24','172.29.0.0/28','fd00::/64','127.0.0.0/16','169.254.0.0/16'):
            with self.subTest(subnet=subnet),self.assertRaises(m.core.GateError):m.source_addresses(subnet)

    def test_lock_wait_is_inside_original_four_seconds(self):
        life=m.Lifecycle.__new__(m.Lifecycle);life.sample_lock=mock.Mock()
        life.sample_lock.acquire.return_value=False
        with mock.patch.object(m.core.time,'monotonic',return_value=13),self.assertRaisesRegex(m.core.GateError,'resource_sample_deadline_exceeded'):
            with life.sampling_lease(10):self.fail('unacquired lock admitted')
        life.sample_lock.acquire.assert_called_once_with(timeout=1)
        life.sample_lock.release.assert_not_called()

    def test_whole_running_product_plus_browser_sampler_and_identity_fits_budget(self):
        quotas=list(m.LIMITS.values())+[(.5,1024),(.5,1024),(.25,64),(.25,256)]
        rows=[{'Id':str(i),'HostConfig':{'NanoCpus':int(cpu*1e9),'Memory':memory*1024**2},
            'State':{'Running':True,'Status':'running','OOMKilled':False}} for i,(cpu,memory) in enumerate(quotas)]
        cpu,memory=m.core.validate_resources(rows,label=m.LABEL)
        self.assertLessEqual(cpu,8);self.assertLessEqual(memory,12*1024**3)
        rows.append({'HostConfig':{'NanoCpus':8*10**9,'Memory':1024},'State':{'Running':True}})
        with self.assertRaises(m.core.GateError):m.core.validate_resources(rows,label=m.LABEL)

    def test_only_exact_owned_planned_zero_exit_is_allowed(self):
        for code,oom,identifier,accepted in [(0,False,'planned',True),(1,False,'planned',False),(0,True,'planned',False),(0,False,'foreign',False)]:
            row={'Id':identifier,'Config':{'Labels':{'com.docker.compose.service':'copilot-backend'}},
                'HostConfig':{},'State':{'Status':'exited','Running':False,'ExitCode':code,'OOMKilled':oom}}
            if accepted:m.core.validate_resources([row],normal_stops={'planned'},label=m.LABEL)
            else:
                with self.assertRaises(m.core.GateError):m.core.validate_resources([row],normal_stops={'planned'},label=m.LABEL)

    def test_strict_profile_never_retries_disappeared_private_or_volume_files(self):
        fixtures=m.core.module('monitor-traces-local-gate_test')
        helper=fixtures.LocalTraceResourceGuardTests()
        with tempfile.TemporaryDirectory() as directory:
            for kind in ('private','volume'):
                life,calls=helper.sample_lifecycle(directory,1024)
                life.strict_resources=True
                original=life.docker
                def sized(*args,**kw):
                    value=original(*args,**kw)
                    if args[0]=='inspect':
                        rows=json.loads(value)
                        for row in rows:row['SizeRw']=0
                        return json.dumps(rows)
                    return value
                life.docker=sized
                if kind=='private':
                    life.last_failure={'du_missing_file':True}
                    life.owned_bind_bytes=mock.Mock(side_effect=fixtures.m.GateError('command_failed_1'))
                else:
                    underlying=life.docker
                    life.docker=lambda *args,**kw:'du: owned: No such file or directory\n512' if args[0]=='exec' else underlying(*args,**kw)
                with mock.patch.object(m.core.time,'monotonic',return_value=1),self.assertRaises(fixtures.m.GateError):life.sample()
                self.assertEqual(life.samples,[])
                if kind=='private':life.owned_bind_bytes.assert_called_once()

    def test_missing_writable_layer_size_cannot_pass_as_zero(self):
        fixtures=m.core.module('monitor-traces-local-gate_test')
        with tempfile.TemporaryDirectory() as directory:
            life,_=fixtures.LocalTraceResourceGuardTests().sample_lifecycle(directory,0)
            life.strict_resources=True
            with mock.patch.object(m.core.time,'monotonic',return_value=1),self.assertRaisesRegex(fixtures.m.GateError,'writable_layer_measurement_missing'):
                life.sample()
            self.assertEqual(life.samples,[])

    def test_sampling_cadence_and_cleanup_share_absolute_window(self):
        life=m.Lifecycle.__new__(m.Lifecycle)
        life.sample_lock=threading.Lock();life.samples=[{'sample_started_monotonic':0}]
        with mock.patch.object(m.core.time,'monotonic',return_value=6),self.assertRaisesRegex(m.core.GateError,'cadence'):
            life.sample()
        life.phase='cleanup';life.cleanup_deadline=10
        with mock.patch.object(m.core.time,'monotonic',return_value=11),self.assertRaisesRegex(m.core.GateError,'cleanup_deadline'):
            life.command(['must-never-spawn'])

    def test_http_profile_uses_single_shared_lifecycle_and_retains_trace_contract(self):
        self.assertTrue(issubclass(m.Lifecycle,m.core.Lifecycle))
        self.assertTrue(m.Lifecycle.strict_resources);self.assertFalse(m.core.Lifecycle.strict_resources)
        self.assertEqual(m.Lifecycle.work_seconds,3300);self.assertEqual(m.core.Lifecycle.work_seconds,3600)
        self.assertEqual(m.Lifecycle.compile_services,('system-backend','gateway','monitor-backend','runtime-log'))
        self.assertIn('agent-backend',m.Lifecycle.product_services);self.assertIn('copilot-backend',m.Lifecycle.product_services)

    def test_sampler_cache_requires_exact_digest_native_platform_and_safe_proof(self):
        image={'id':'sha256:'+'a'*64,'os':'linux','architecture':'arm64',
            'repo_digests':['nginx@'+m.SAMPLER_IMAGE.split('@')[1]],'private_environment':'never-export'}
        proof=m.validate_sampler_image(image)
        self.assertEqual(proof['image_id'],image['id']);self.assertNotIn('never-export',json.dumps(proof))
        for changes in ({'id':'tag:latest'},{'os':'windows'},{'architecture':'amd64'},
                {'repo_digests':[]},{'repo_digests':['other@'+m.SAMPLER_IMAGE.split('@')[1]]}):
            with self.subTest(changes=changes),self.assertRaises(m.core.GateError):m.validate_sampler_image({**image,**changes})

    def test_sampler_admission_only_inspects_cache_and_never_pulls_or_retries(self):
        image={'id':'sha256:'+'a'*64,'os':'linux','architecture':'arm64','repo_digests':['nginx@'+m.SAMPLER_IMAGE.split('@')[1]]}
        life=SimpleNamespace(docker=mock.Mock(return_value=json.dumps(image)))
        m.admit_sampler_image(life)
        self.assertEqual(life.sampler_image_proof['image_id'],image['id'])
        self.assertEqual(life.docker.call_count,1);self.assertEqual(life.docker.call_args.args[:2],('image','inspect'))
        life=SimpleNamespace(docker=mock.Mock(side_effect=m.core.GateError('command_failed_1')))
        with self.assertRaises(m.core.GateError):m.admit_sampler_image(life)
        self.assertEqual(life.docker.call_count,1)

    def test_source_tls_helpers_use_admitted_id_without_implicit_pull(self):
        image_id='sha256:'+'a'*64
        life=SimpleNamespace(work=Path('/synthetic'),run_id='owned',sampling_image=image_id,docker=mock.Mock())
        m.prepare_source_tls(life)
        self.assertEqual(life.docker.call_count,2)
        for call in life.docker.call_args_list:
            self.assertEqual(call.args[0],'run');self.assertEqual(call.args[call.args.index('--pull')+1],'never')
            self.assertIn(image_id,call.args);self.assertNotIn(m.SAMPLER_IMAGE,call.args)
        instance=m.Lifecycle.__new__(m.Lifecycle)
        with self.assertRaisesRegex(m.core.GateError,'sampler_image_not_admitted'):_=instance.sampling_image

    def test_projection_selects_the_two_product_metrics_profiles(self):
        # Compose config omits inactive profiles. Model that actual behavior,
        # rather than handing the adapter an impossible always-complete map.
        with tempfile.TemporaryDirectory() as directory:
            life=SimpleNamespace(work=Path(directory),run_id='owned',uuid='run',network='owned_data',addresses=m.source_addresses('172.29.0.0/16'))
            services={name:{'image':'product:'+name,'environment':{},'volumes':[],'command':[]} for name in m.LIMITS}
            calls=[]
            def compose(args,**kwargs):
                calls.append(args)
                if 'docker-compose.infra.yml' in args:
                    profiles={args[i+1] for i,value in enumerate(args[:-1]) if value=='--profile'}
                    included={'postgres','redis'}
                    if 'observability-metrics' in profiles:included.add('prometheus')
                    if 'observability-metrics-control' in profiles:included.add('metrics-control')
                    return json.dumps({'services':{name:services[name] for name in included}})
                return json.dumps({'services':services})
            life.command=compose;life.private_json=lambda name,value:Path(directory)/name
            config=m.project(life,{}, {})
            self.assertTrue({'prometheus','metrics-control'}<=config['services'].keys())
            self.assertEqual(sum(args.count('--profile') for args in calls),2)

    def test_projection_has_owned_static_sources_and_product_readonly_mounts(self):
        with tempfile.TemporaryDirectory() as directory:
            life=SimpleNamespace(work=Path(directory),run_id='owned',uuid='run',network='owned_data',addresses=m.source_addresses('172.29.0.0/16'))
            services={name:{'image':'product:'+name,'environment':{},'volumes':[],'command':[]} for name in m.LIMITS}
            services['prometheus']['volumes']=[{'type':'volume','source':'prometheus_data','target':'/prometheus','volume':{}}]
            services['postgres']['volumes']=[{'type':'volume','source':'postgres_data','target':'/var/lib/postgresql/data','volume':{'nocopy':True}}]
            outputs=iter([json.dumps({'services':services}),json.dumps({'services':services})])
            life.command=mock.Mock(side_effect=lambda *a,**kw:next(outputs));life.private_json=lambda name,value:Path(directory)/name
            config=m.project(life,{}, {})
            self.assertEqual(config['networks']['data'],{'external':True,'name':'owned_data'})
            self.assertFalse(config['services']['prometheus']['volumes'][0].get('volume',{}).get('nocopy',False))
            self.assertTrue(config['services']['postgres']['volumes'][0]['volume']['nocopy'])
            for module,address in life.addresses.items():
                row=config['services'][module+'-backend']
                self.assertEqual(row['networks']['data']['ipv4_address'],address)
                self.assertEqual(row['restart'],'no');self.assertEqual(row['labels'][m.LABEL],'owned')
                source=[v for v in row['volumes'] if v['target']=='/etc/addp/process-metrics/source']
                self.assertEqual(len(source),1);self.assertTrue(source[0]['read_only']);self.assertEqual(source[0]['type'],'volume')
            self.assertNotIn('entry',config['services']['agent-backend']['networks'])
            self.assertIn('cluster_name=addp-http-quality-local-run',config['services']['postgres']['command'])

if __name__=='__main__':unittest.main()
