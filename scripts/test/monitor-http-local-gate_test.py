"""No Docker: profile, strict sampling, product projection and ownership contracts."""
from pathlib import Path
import importlib.util
import json
import os
import subprocess
import tempfile
import threading
import time
import unittest
from unittest import mock
from types import SimpleNamespace
spec=importlib.util.spec_from_file_location('http_local',Path(__file__).with_name('monitor-http-local-gate.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)

class HTTPProductLocalTests(unittest.TestCase):
    def test_archived_list_candidate_keeps_full_deadline_failure(self):
        run = 'addp-http-local-archive-fixture'
        identity = m.core.owned_list_identity(
            ['docker', 'container', 'ls', '-a', '-q', '--filter', f'label={m.LABEL}={run}'], run, m.LABEL)
        command = {**identity, 'command_id': 363, 'run_id': run, 'record_complete': True, 'result': 'passed',
                   'phase': 'product_image_system_backend', 'entered_monotonic_ns': 10_000_000_000,
                   'decision_monotonic_ns': 13_888_400_000}
        cleanup = {**command, 'command_id': 370, 'phase': 'cleanup', 'entered_monotonic_ns': 26_000_000_000,
                   'decision_monotonic_ns': 33_000_000_000}
        report = {'result': 'failed', 'error_code': 'resource_sample_deadline_exceeded',
                  'resource_guard_failure': {'operation': 'inspect_owned_containers', 'budget_seconds': 4},
                  'owned_list_commands': [command, cleanup]}
        original = json.dumps(report, sort_keys=True)
        events = [{'direction': direction, 'timestamp_ns': stamp,
                   'request_signature_sha256': identity['request_signature_sha256'], 'raw': 'PRIVATE'}
                  for direction, stamp in [('>>', 111_375_000_000), ('<<', 113_844_591_459),
                                           ('>>', 127_000_000_000), ('<<', 130_000_000_000)]]
        result = m.core.classify_archived_apiproxy(report['owned_list_commands'], events, 363, log_contiguous=True,
                                                  desktop_version='4.49.0', coarse_offset_ns=-100_000_000_000)
        self.assertEqual(result, {'status': 'unique_candidate', 'command_id': 363, 'proxy_log_span_ns': 2_469_591_459})
        self.assertEqual(json.dumps(report, sort_keys=True), original)

    def test_forbidden_profiles_and_remote_architecture(self):
        m.validate_profile({},'arm64','aarch64')
        for marker in ('GITHUB_ACTIONS','ADDP_ONLINE_HOSTED','ADDP_ONLINE_OWNER_MANAGED','ADDP_EXECUTION_TRACE_LOCAL','ADDP_CATALOG_BROWSER_LOCAL'):
            with self.subTest(marker=marker),self.assertRaises(m.core.GateError):m.validate_profile({marker:'1'},'arm64','arm64')
        with self.assertRaises(m.core.GateError):m.validate_profile({},'x86_64','arm64')

    def test_uncommitted_shared_page_oracle_is_rejected_before_any_docker(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory)
            for relative in m.OWN_INPUTS:
                target=root/relative;target.parent.mkdir(parents=True,exist_ok=True);target.write_text('committed\n')
            (root/'console/frontend/e2e/http-quality-page-support.mjs').write_text('dirty oracle\n')
            life=SimpleNamespace(command=mock.Mock(return_value='committed\n'),docker=mock.Mock())
            with mock.patch.object(m.core,'ROOT',root),self.assertRaisesRegex(m.core.GateError,'commit_all_http_inputs_before_acceptance'):
                m.build(life)
            life.docker.assert_not_called()

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
                    life.docker=lambda *args,**kw:fixtures.m.VOLUME_SCAN_ENTERED+'\ndu: owned: No such file or directory\n512\n'+fixtures.m.VOLUME_SCAN_RETURNED+'\n' if args[0]=='exec' else underlying(*args,**kw)
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
            life=SimpleNamespace(work=Path(directory),run_id='owned',uuid='run',phase='projection',network='owned_data',addresses=m.source_addresses('172.29.0.0/16'))
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
            life=SimpleNamespace(work=Path(directory),run_id='owned',uuid='run',phase='projection',network='owned_data',addresses=m.source_addresses('172.29.0.0/16'))
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

class CompileCheckpointCaseTests(unittest.TestCase):
    def samples(self,run_id='synthetic-compile',count=12):
        return [{'phase':'product_compile','sample_duration_seconds':.5,'sample_started_monotonic':i*4,
            'steps':[{'operation':'measure_owned_volumes','commands':[{'command_id':i+1,'run_id':run_id,
                'phase':'product_compile','record_complete':True,'result':'passed','volume_measurement_checkpoints':{
                    'fragment_status':'complete','measurement_entered':'observed','pipeline_returned':'observed','examined_bytes':64}}]}]}
            for i in range(count)]

    def test_strict_case_selection_and_fixed_limits(self):
        self.assertEqual(m.local_case([]),'full');self.assertEqual(m.local_case(['--case=full']),'full')
        self.assertEqual(m.local_case(['--case=compile-checkpoints']),'compile-checkpoints')
        for args in (['--case=unknown'],['--case=compile-checkpoints','--retry'],['--case','compile-checkpoints']):
            with self.assertRaisesRegex(m.core.GateError,'local_case_invalid'):m.local_case(args)
        self.assertEqual(m.CASE_LIMITS,{'full':(3600,3300),'compile-checkpoints':(900,780)})
        with mock.patch.object(m.sys,'argv',['gate','--case=unknown']),mock.patch.object(m,'Lifecycle') as start:
            with self.assertRaisesRegex(m.core.GateError,'local_case_invalid'):m.main()
        start.assert_not_called()

    def test_work_and_cleanup_keep_the_same_absolute_cutoff(self):
        life=m.Lifecycle.__new__(m.Lifecycle);life.started=0;life._phase='product_compile'
        life.total_seconds=900;life.work_seconds=780
        with mock.patch.object(m.core.time,'monotonic',return_value=779),mock.patch.object(m.core.Lifecycle,'command',return_value='') as command:
            life.command(['synthetic'],timeout=30);self.assertEqual(command.call_args.kwargs['timeout'],1)
        with mock.patch.object(m.core.time,'monotonic',return_value=780),mock.patch.object(m.core.Lifecycle,'command') as command:
            with self.assertRaisesRegex(m.core.GateError,'total_deadline_exceeded'):life.command(['synthetic'])
        command.assert_not_called()
        with mock.patch.object(m.core.time,'monotonic',return_value=850),mock.patch.object(m.core.Lifecycle,'cleanup',return_value=[]):life.cleanup()
        self.assertEqual(life.cleanup_deadline,900)
        life._phase='cleanup'
        with mock.patch.object(m.core.time,'monotonic',return_value=899),mock.patch.object(m.core.Lifecycle,'command',return_value='') as command:
            life.command(['synthetic'],timeout=30);self.assertEqual(command.call_args.kwargs['timeout'],1)
        with mock.patch.object(m.core.time,'monotonic',return_value=900),self.assertRaisesRegex(m.core.GateError,'cleanup_deadline_exceeded'):life.command(['synthetic'])

    def test_summary_rejects_missing_wrong_phase_duplicate_or_incomplete_samples(self):
        life=SimpleNamespace(run_id='synthetic-compile',samples=self.samples())
        self.assertEqual(m.compile_checkpoint_summary(life)['completed_compile_samples'],12)
        for kind in ('count','phase','run','duplicate','missing','over_limit','deadline','cadence'):
            rows=self.samples();command=rows[-1]['steps'][0]['commands'][0]
            if kind=='count':rows.pop()
            elif kind=='phase':rows[-1]['phase']='idle'
            elif kind=='run':command['run_id']='foreign'
            elif kind=='duplicate':command['command_id']=1
            elif kind=='missing':command.pop('volume_measurement_checkpoints')
            elif kind=='over_limit':command['volume_measurement_checkpoints']['examined_bytes']=4097
            elif kind=='deadline':rows[-1]['sample_duration_seconds']=4
            elif kind=='cadence':rows[-1]['sample_started_monotonic']+=2
            with self.subTest(kind=kind),self.assertRaises(m.core.GateError):m.compile_checkpoint_summary(SimpleNamespace(run_id=life.run_id,samples=rows))

    def test_same_go_build_returns_before_cache_images_or_browser(self):
        for fail in (False,True):
            with self.subTest(fail=fail),tempfile.TemporaryDirectory() as directory:
                work=Path(directory);cache=work/'public';(cache/'cache/download').mkdir(parents=True)
                life=SimpleNamespace(work=work,checkout=work/'source',phase='prepared',env={'PATH':os.defpath},
                    run_id='owned',label=m.LABEL,builder='owned',compile_services=m.Lifecycle.compile_services,
                    product_services=m.Lifecycle.product_services,docker=mock.Mock(),command=mock.Mock(return_value=str(cache)),
                    resume_builder=mock.Mock(side_effect=AssertionError('no image build')))
                if fail:life.docker.side_effect=m.core.GateError('compile_failed')
                with mock.patch.object(m.core,'reclaim_go_build_cache',side_effect=AssertionError('no cache reclaim')) as reclaim:
                    if fail:
                        with self.assertRaisesRegex(m.core.GateError,'compile_failed'):m.core.build_products(life,life.env,{},compile_only=True)
                    else:self.assertEqual(m.core.build_products(life,life.env,{},compile_only=True),{})
                reclaim.assert_not_called();life.resume_builder.assert_not_called()
                life.docker.assert_called_once();args=life.docker.call_args.args
                self.assertEqual(args[args.index('--cpus')+1],'2');self.assertEqual(args[args.index('--memory')+1],'3g')
                self.assertIn('make',args);self.assertIn('BUILD_ARGS=--arch arm64 --services system-backend,gateway,monitor-backend,runtime-log',args)
                self.assertEqual(life.command.call_count,1)

    def test_main_compile_case_never_prepares_identity_or_business(self):
        with tempfile.TemporaryDirectory() as directory:
            life=SimpleNamespace(run_id='synthetic-compile',phase='compile_checkpoint_summary',env={},started=time.monotonic(),
                samples=self.samples(),command_evidence=SimpleNamespace(snapshot=lambda:[]),docker=mock.Mock(return_value='arm64'),
                cleanup=mock.Mock(return_value=[]),sample=mock.Mock(),check=mock.Mock())
            with mock.patch.object(m.sys,'argv',['gate','--case=compile-checkpoints']),mock.patch.object(m,'Lifecycle',return_value=life), \
                 mock.patch.object(m,'validate_profile'),mock.patch.object(m.core,'effective_docker_endpoint',return_value='unix:///synthetic'), \
                 mock.patch.object(m.core,'private_docker_client'),mock.patch.object(m,'build',return_value={}) as build, \
                 mock.patch.object(m,'setup',side_effect=AssertionError('no identity')) as setup, \
                 mock.patch.object(m.core,'module',side_effect=AssertionError('no business')) as business, \
                 mock.patch.object(m.tempfile,'gettempdir',return_value=directory),mock.patch('builtins.print'):
                self.assertEqual(m.main(),0)
            build.assert_called_once_with(life,compile_only=True);setup.assert_not_called();business.assert_not_called()
            payload=json.loads((Path(directory)/'synthetic-compile-report.json').read_text())
            self.assertEqual(payload['case'],'compile-checkpoints');self.assertEqual(payload['limits']['seconds'],900)
            self.assertEqual(payload['assertions'],{});self.assertTrue(payload['cleanup']['zero_owned_resources'])
            self.assertEqual(payload['compile_checkpoint_diagnostic']['controlled_requests'],0)
            self.assertIn('product_images',payload['not_accepted'])


    def test_main_default_full_keeps_original_setup_and_business(self):
        with tempfile.TemporaryDirectory() as directory:
            life=SimpleNamespace(run_id='synthetic-full',phase='formal',env={},started=time.monotonic(),
                samples=[],command_evidence=SimpleNamespace(snapshot=lambda:[]),docker=mock.Mock(return_value='arm64'),
                cleanup=mock.Mock(return_value=[]),sample=mock.Mock(),check=mock.Mock(),checkout=Path(directory))
            business=SimpleNamespace(run=mock.Mock())
            with mock.patch.object(m.sys,'argv',['gate']),mock.patch.object(m,'Lifecycle',return_value=life), \
                 mock.patch.object(m,'validate_profile'),mock.patch.object(m.core,'effective_docker_endpoint',return_value='unix:///synthetic'), \
                 mock.patch.object(m.core,'private_docker_client'),mock.patch.object(m,'build',return_value={}) as build, \
                 mock.patch.object(m,'setup') as setup,mock.patch.object(m.core,'module',return_value=business), \
                 mock.patch.object(m,'compile_checkpoint_summary',side_effect=AssertionError('not compile case')) as summary, \
                 mock.patch.object(m.tempfile,'gettempdir',return_value=directory),mock.patch.dict(m.os.environ,{}),mock.patch('builtins.print'):
                self.assertEqual(m.main(),0)
            build.assert_called_once_with(life,compile_only=False);setup.assert_called_once_with(life,{})
            business.run.assert_called_once();summary.assert_not_called()
            payload=json.loads((Path(directory)/'synthetic-full-report.json').read_text())
            self.assertEqual(payload['case'],'full');self.assertEqual(payload['limits']['seconds'],3600)
            self.assertNotIn('compile_checkpoint_diagnostic',payload)


class HTTPPrivateTLSConfigTests(unittest.TestCase):
    def fixture(self,directory,failure=None):
        life=SimpleNamespace(work=Path(directory),phase='metrics_deployment_projection',
            env={'PATH':os.defpath,'OPENSSL_CONF':'foreign-default-config'},
            addresses=m.source_addresses('172.29.0.0/16'))
        calls=[]
        def command(args,**kwargs):
            calls.append((life.phase,args,kwargs))
            if failure and failure(life.phase,args,kwargs):raise m.core.GateError('first_tls_failure')
            if args[0]=='openssl':
                # No crypto in the model. The real implementation must bind
                # the request and library initialization to the private file.
                config=life.work/'certificates/openssl.cnf'
                self.assertEqual(kwargs['env']['OPENSSL_CONF'],str(config))
                self.assertTrue(config.is_file())
                if args[1]=='req':self.assertEqual(args[args.index('-config')+1],str(config))
                for flag in ('-keyout','-out'):
                    if flag in args:Path(args[args.index(flag)+1]).write_text('fixture')
            return ''
        life.command=command
        return life,calls,{'PROMETHEUS_SERVICE_CLIENT_SECRET':'fixture-only'}

    def test_all_crypto_uses_private_config_without_polluting_parent_or_product(self):
        with tempfile.TemporaryDirectory() as directory:
            life,calls,env=self.fixture(directory);original=life.env.copy()
            m.tls(life,env)
            self.assertEqual(life.env,original);self.assertNotIn('OPENSSL_CONF',env)
            self.assertEqual(life.phase,'metrics_deployment_projection')
            config=life.work/'certificates/openssl.cnf'
            self.assertEqual(config.stat().st_mode&0o777,0o600)
            self.assertEqual(config.parent.stat().st_mode&0o777,0o700)
            self.assertNotIn('.include',config.read_text());self.assertNotIn('provider',config.read_text())
            crypto=[row for row in calls if row[1][0]=='openssl']
            self.assertEqual(len(crypto),19);self.assertTrue(all(row[2]['timeout']==10 for row in crypto))
            self.assertEqual(calls[-2][0],'http_metrics_config_render')
            self.assertEqual(calls[-1][0],'http_process_metrics_render')
            self.assertTrue(all('OPENSSL_CONF' not in row[2]['env'] for row in calls[-2:]))

    def test_failed_or_timed_out_step_stays_exact_and_stops_before_later_work(self):
        for phase in ('http_tls_ca_center','http_tls_csr_query','http_tls_sign_query',
                'http_metrics_config_render','http_process_metrics_render'):
            for code in ('first_tls_failure','command_deadline_exceeded'):
                with self.subTest(phase=phase,code=code),tempfile.TemporaryDirectory() as directory:
                    life,calls,env=self.fixture(directory)
                    original=life.command
                    def fail(args,**kwargs):
                        if life.phase==phase:
                            calls.append((life.phase,args,kwargs));raise m.core.GateError(code)
                        return original(args,**kwargs)
                    life.command=fail
                    with self.assertRaisesRegex(m.core.GateError,code):m.tls(life,env)
                    self.assertEqual(life.phase,phase);self.assertEqual(calls[-1][0],phase)

    def test_missing_or_invalid_config_is_not_retried_or_replaced(self):
        for invalid in (False,True):
            with self.subTest(invalid=invalid),tempfile.TemporaryDirectory() as directory:
                life,calls,env=self.fixture(directory)
                def broken(args,**kwargs):
                    config=life.work/'certificates/openssl.cnf'
                    if invalid:config.write_text('invalid configuration')
                    else:config.unlink()
                    calls.append((life.phase,args,kwargs));raise m.core.GateError('config_unusable')
                life.command=broken
                with self.assertRaisesRegex(m.core.GateError,'config_unusable'):m.tls(life,env)
                self.assertEqual(len(calls),1);self.assertEqual(life.phase,'http_tls_ca_center')
                self.assertFalse((life.work/'metrics-deployment').exists())

    def test_material_io_failure_cannot_be_mislabelled_as_previous_sign_command(self):
        with tempfile.TemporaryDirectory() as directory:
            life,calls,env=self.fixture(directory)
            with mock.patch.object(m.shutil,'copyfile',side_effect=OSError('copy_failure')):
                with self.assertRaisesRegex(OSError,'copy_failure'):m.tls(life,env)
            self.assertEqual(life.phase,'http_tls_materials')
            self.assertEqual(calls[-1][0],'http_tls_sign_metrics_control')

    def test_native_adapter_satisfies_the_real_command_kernel_before_crypto(self):
        with tempfile.TemporaryDirectory() as directory:
            life=HTTPNativeTLSConfigTest.lifecycle(Path(directory),time.monotonic())
            child=mock.Mock(pid=123,returncode=0)
            child.poll.return_value=0
            with mock.patch.object(m.core.subprocess,'Popen',return_value=child) as spawn:
                self.assertEqual(m.core.Lifecycle.command(life,['openssl','version'],timeout=10,output_limit=65536),'')
            spawn.assert_called_once()
            self.assertTrue(life.command_evidence.sequence==1)

    def test_cleanup_error_does_not_replace_the_first_tls_error_in_entry_report(self):
        # Actual HTTP main() failure handling with only command/lifecycle doubles.
        with tempfile.TemporaryDirectory() as directory:
            life=SimpleNamespace(run_id='synthetic-tls',phase='http_tls_ca_center',env={},started=time.monotonic(),
                samples=[],command_evidence=SimpleNamespace(snapshot=lambda:[]),
                docker=mock.Mock(return_value='arm64'),cleanup=mock.Mock(return_value=['cleanup_failed']))
            report=Path(directory)/'synthetic-tls-report.json'
            with mock.patch.object(m,'Lifecycle',return_value=life),mock.patch.object(m,'validate_profile'), \
                 mock.patch.object(m.core,'effective_docker_endpoint',return_value='unix:///synthetic'), \
                 mock.patch.object(m.core,'private_docker_client'),mock.patch.object(m,'build',side_effect=m.core.GateError('first_tls_failure')), \
                 mock.patch.object(m.tempfile,'gettempdir',return_value=directory),mock.patch.object(m.sys,'argv',['gate']),mock.patch('builtins.print'):
                self.assertEqual(m.main(),1)
            payload=json.loads(report.read_text())
            self.assertEqual(payload['error_code'],'first_tls_failure')
            self.assertEqual(payload['stage'],'http_tls_ca_center')
            self.assertFalse(payload['cleanup']['zero_owned_resources']);life.cleanup.assert_called_once()

class HTTPNativeTLSConfigTest(unittest.TestCase):
    @staticmethod
    def lifecycle(work,started):
        return SimpleNamespace(work=work,run_id='offline-http-tls',checkout=work/'no-checkout',
            started=started,work_seconds=60,phase='offline_tls',sample_thread_id=None,
            failure=None,command_evidence=m.core.CommandEvidence(label=m.LABEL),
            env=m.core.safe_environment(),addresses=m.source_addresses('172.29.0.0/16'))

    def test_offline_private_config_chain_purpose_and_names_within_one_bounded_group(self):
        # This is the only native crypto scene. The full production TLS function
        # runs once; the two later renderers are explicit doubles, never Docker.
        started=time.monotonic();count=0;peak=0;rejections=0;version=''
        directory=None
        try:
            with tempfile.TemporaryDirectory(prefix='addp-http-tls-t1-') as directory:
                work=Path(directory);work.chmod(0o700)
                life=self.lifecycle(work,started)
                # Deliberately unusable parent config: successful crypto must
                # come from the exact same private configuration as production.
                life.env['OPENSSL_CONF']=str(work/'missing-user-default.cnf')
                parent=life.env.copy()
                def budget():
                    nonlocal peak
                    remaining=60-(time.monotonic()-started)
                    self.assertGreater(remaining,0,'offline_tls_total_deadline')
                    amount=sum(path.stat().st_size for path in work.rglob('*') if path.is_file())+65536
                    peak=max(peak,amount)
                    self.assertLessEqual(amount,1024**2,'offline_tls_material_budget')
                    return remaining
                def command(args,**kwargs):
                    nonlocal count
                    if args[0]!='openssl':
                        self.assertEqual(args[0],m.sys.executable)
                        self.assertIn(args[1],('scripts/infra/generate-metrics-config.py','scripts/prod/generate-process-metrics-platform.py'))
                        self.assertNotIn('OPENSSL_CONF',kwargs['env'])
                        return ''
                    count+=1;self.assertLessEqual(count,40,'offline_tls_command_budget')
                    kwargs['timeout']=min(kwargs.get('timeout',10),10,budget())
                    kwargs['output_limit']=65536
                    result=m.core.Lifecycle.command(life,args,**kwargs)
                    budget()
                    return result
                life.command=command
                env={'PROMETHEUS_SERVICE_CLIENT_SECRET':'offline-fixture-only'}
                m.tls(life,env)
                self.assertEqual(life.env,parent)
                self.assertFalse((work/'missing-user-default.cnf').exists())
                private={**life.env,'OPENSSL_CONF':str(work/'certificates/openssl.cnf')}
                def openssl(*args):return command(['openssl',*args],env=private)
                version=openssl('version').split()[1]
                roots=[work/'certificates'/name for name in ('center.crt','source.crt','control.crt')]
                self.assertEqual(len({root.read_bytes() for root in roots}),3)
                for root in roots:
                    text=openssl('x509','-in',str(root),'-noout','-text')
                    for required in ('CA:TRUE','Certificate Sign','CRL Sign','Public-Key: (2048 bit)'):
                        self.assertTrue(required in text,'offline_tls_ca_contract')
                    dates=[m.datetime.strptime(m.re.search(label+r'\s*(.+)',text)[1].strip(),'%b %d %H:%M:%S %Y %Z') for label in ('Not Before:','Not After :')]
                    self.assertEqual((dates[1]-dates[0]).total_seconds(),86400)
                leaves=[('query','center','sslclient','-verify_hostname','query'),
                    ('health','center','sslclient','-verify_hostname','health'),
                    ('prometheus','center','sslserver','-verify_hostname','localhost'),
                    ('collector','source','sslclient','-verify_hostname','collector'),
                    ('admission','source','sslclient','-verify_hostname','admission'),
                    ('metrics-control','control','sslserver','-verify_hostname','metrics-control'),
                    *[(name,'source','sslserver','-verify_ip',address) for name,address in life.addresses.items()]]
                for name,root,purpose,flag,value in leaves:
                    openssl('verify','-purpose',purpose,flag,value,'-CAfile',str(work/'certificates'/(root+'.crt')),str(work/'certificates'/(name+'.crt')))
                for name,root,purpose,flag,value in (
                        ('agent','center','sslserver','-verify_ip',life.addresses['agent']),
                        ('agent','source','sslserver','-verify_ip','172.29.0.1'),
                        ('query','center','sslserver','-verify_hostname','query'),
                        ('prometheus','center','sslserver','-verify_hostname','wrong.local')):
                    with self.assertRaisesRegex(m.core.GateError,'^command_failed_[12]$'):
                        openssl('verify','-purpose',purpose,flag,value,'-CAfile',str(work/'certificates'/(root+'.crt')),str(work/'certificates'/(name+'.crt')))
                    rejections+=1
                budget()
        finally:
            elapsed=time.monotonic()-started
            removed=directory is not None and not Path(directory).exists()
            print('HTTP TLS offline T1: '+json.dumps({'commands':count,'openssl_version':version,
                'elapsed_seconds':round(elapsed,6),'material_and_output_peak_bytes':peak,
                'expected_rejections':rejections,'cleanup_removed':removed}),flush=True)
            self.assertTrue(removed,'offline_tls_cleanup_residue')
            self.assertLess(elapsed,60,'offline_tls_total_deadline')

if __name__=='__main__':unittest.main()
