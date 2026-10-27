"""Deterministic local Trace lifecycle contracts; never execute Docker/services."""
from pathlib import Path
import copy
import importlib.util
import json
import os
import signal
import tempfile
import threading
import unittest
import urllib.parse
from types import SimpleNamespace
from unittest import mock

spec = importlib.util.spec_from_file_location('monitor_traces_local_gate', Path(__file__).with_name('monitor-traces-local-gate.py'))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


def row(cpu=1, memory=512 * 1024**2, *, status='running', exit_code=0, oom=False, service='system-backend'):
    return {'Id': 'owned-id', 'Config': {'Labels': {'com.docker.compose.service': service}},
            'HostConfig': {'NanoCpus': int(cpu * 1e9), 'Memory': memory},
            'State': {'Running': status == 'running', 'Status': status, 'ExitCode': exit_code, 'OOMKilled': oom}}


def bare_lifecycle(directory):
    life = m.Lifecycle.__new__(m.Lifecycle)
    life.run_id = 'addp-trace-local-synthetic'
    life.uuid = '11111111-1111-4111-8111-111111111111'
    life.work = Path(directory)
    life.checkout = life.work / 'source'
    life.started = 0.0
    life.phase = 'synthetic'
    life.builder = life.run_id
    life.builder_ids = set()
    life.builder_volumes = set()
    life.builder_created = False
    life.builder_stopped = False
    life.tags = []
    life.env = {'PATH': '/synthetic'}
    life.stop = threading.Event()
    life.failure = None
    life.samples = []
    life.thread = None
    life.sample_lock = threading.Lock()
    life.sampler_volumes = ()
    life.sampler_started = False
    life.sample_thread_id = None
    life.sample_deadline = None
    life.command_evidence = m.CommandEvidence()
    life.minio_image = 'sha256:synthetic-pinned-minio'
    life.owned_bind_bytes = lambda **kwargs: 0
    return life


class VolumePreparationContractTests(unittest.TestCase):
    def test_protection_readiness_failure_exports_only_fixed_markers(self):
        markers=m.diagnostic_markers(b'execution trace Develop protection readiness: Tenant PRIVATE-TOKEN protection initialization incomplete')
        self.assertEqual(markers,['protection readiness','protection initialization'])

    def test_dependency_failure_names_the_exact_stage_and_only_fixed_public_markers(self):
        stages=[]
        def compose(*args,**kwargs):
            if args[0]=='exec':raise m.GateError('command_failed_2')
        with self.assertRaisesRegex(m.GateError,'command_failed_2'):
            m.start_dependencies(compose,'private-sql',stages.append)
        self.assertEqual(stages,['isolated_databases_start','isolated_platform_database_initialization'])
        markers=m.diagnostic_markers(b'psql: connection to server: PRIVATE-PASSWORD: server closed the connection unexpectedly')
        self.assertEqual(markers,['psql:','connection to server','server closed the connection unexpectedly'])
        self.assertNotIn('PRIVATE',str(markers))

    def test_first_sample_failure_reports_the_exact_operation_without_waiting_for_monitor(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);life.sample_operation='measure_private_bindings'
            life._sample=mock.Mock(side_effect=m.GateError('command_deadline_exceeded'))
            with self.assertRaisesRegex(m.GateError,'command_deadline_exceeded'):life.sample()
            self.assertEqual(life.resource_failure['operation'],'measure_private_bindings')
            self.assertEqual(life.resource_failure['error_code'],'command_deadline_exceeded')
            self.assertEqual(life.samples,[])

    def test_complete_owned_compose_volumes_are_created_before_sampling_and_startup(self):
        for wrong_owner in (False,True):
            with self.subTest(wrong_owner=wrong_owner),tempfile.TemporaryDirectory() as directory:
                life=bare_lifecycle(directory);life.deployment_config={'volumes':{'runtime-logs':{},'postgres-data':{}}};calls=[]
                def docker(*args,**kwargs):
                    calls.append(args);name=args[-1]
                    if args[1]=='create':return name+'\n'
                    logical=name.removeprefix(life.run_id+'_')
                    return json.dumps([{'Name':name,'Labels':{m.LABEL:'foreign' if wrong_owner else life.run_id,
                        'com.docker.compose.project':life.run_id,'com.docker.compose.volume':logical}}])
                life.docker=docker
                if wrong_owner:
                    with self.assertRaisesRegex(m.GateError,'owned_volume_preparation_ownership_mismatch'):m.prepare_sampling_volumes(life)
                else:
                    m.prepare_sampling_volumes(life)
                    self.assertEqual([args[-1] for args in calls if args[1]=='create'],
                        [life.run_id+'_postgres-data',life.run_id+'_runtime-logs'])
        source=__import__('inspect').getsource(m.build)
        order=['prepare_isolated_configuration(', 'prepare_sampling_volumes(', 'prepare_builder(', 'life.sample()', 'life.thread.start()', 'start_dependencies(']
        positions=[source.index(token) for token in order]
        self.assertEqual(positions,sorted(positions))
        builder=__import__('inspect').getsource(m.prepare_builder)
        self.assertLess(builder.index('life.builder_ids ='),builder.index('life.prepare_volume_sampler('))

    def test_sampler_admission_is_bounded_readonly_and_cannot_replace_a_running_helper(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);life.docker=mock.Mock(return_value='owned')
            life.prepare_volume_sampler(['z','a','z'],timeout=30)
            args=life.docker.call_args.args
            self.assertEqual(life.sampler_volumes,('a','z'))
            self.assertEqual(life.docker.call_args.kwargs,{'guarded':False,'timeout':30})
            self.assertIn(str(m.SAMPLER_CPU),args);self.assertIn('64m',args);self.assertIn('none',args)
            self.assertEqual([a for a in args if a.startswith('type=volume,')],
                ['type=volume,source=a,target=/space/0,readonly','type=volume,source=z,target=/space/1,readonly'])
            with self.assertRaisesRegex(m.GateError,'volume_sampler_already_started'):
                life.prepare_volume_sampler(['foreign'],timeout=30)
            self.assertEqual(life.docker.call_count,1)


class BuilderResumeContractTests(unittest.TestCase):
    def test_planned_stop_is_cleared_only_after_exact_healthy_builder_resumes(self):
        for running,oom in ((True,False),(False,False),(True,True)):
            with self.subTest(running=running,oom=oom),tempfile.TemporaryDirectory() as directory:
                life=bare_lifecycle(directory);life.builder_stopped=True;life.builder_ids={'owned-id'};life.sample=mock.Mock()
                life.docker=mock.Mock(side_effect=['bootstrapped',json.dumps([{'Id':'owned-id','State':{'Running':running,'OOMKilled':oom}}])])
                if running and not oom:
                    life.resume_builder();self.assertFalse(life.builder_stopped);life.sample.assert_called_once()
                else:
                    with self.assertRaisesRegex(m.GateError,'owned_builder_resume_not_healthy'):life.resume_builder()
                    self.assertTrue(life.builder_stopped);life.sample.assert_not_called()

    def test_two_cpu_compilation_with_stopped_builder_fits_aggregate_budget(self):
        dependencies=('postgres','business-postgres','redis','minio','loki','runtime-log-api','tempo','trace-query','alloy')
        rows=[row(cpu=m.LIMITS[name][0],memory=m.LIMITS[name][1]*1024**2,service=name) for name in dependencies]
        rows += [row(cpu=2,memory=3*1024**3,service=None),row(cpu=.25,memory=64*1024**2,service=None)]
        cpu,memory=m.validate_resources(rows)
        self.assertLessEqual(cpu,8);self.assertLessEqual(memory,12*1024**3)


class NginxRouteContractTests(unittest.TestCase):
    def probe(self,directory,strip_query=False):
        life=bare_lifecycle(directory);calls=[];life.sample=mock.Mock()
        def docker(*args,**kwargs):
            calls.append(args)
            if args[0] in ('network','run','kill'):return 'owned'
            if args[0]=='wait':return '0\n0\n'
            if args[0]=='exec':
                if args[2]=='sh':return '/route-ready' if 'n=0;' in args[-1] else 'HTTP/1.1 502 Bad Gateway\n'
                url=urllib.parse.urlsplit(args[-1]);path=url.path
                if path.startswith('/module-ui/'):path='/'+path.split('/',3)[3]
                return path+('?' + url.query if url.query and not strip_query else '')
            raise AssertionError(args)
        life.docker=docker
        return life,calls

    def test_real_probe_is_bounded_isolated_and_rejects_missing_modules(self):
        with tempfile.TemporaryDirectory() as directory:
            life,calls=self.probe(directory);m.verify_nginx_routes(life,'sha256:product-nginx')
            self.assertEqual(life.nginx_route_assertions,{'uri_and_query_cases':11,'missing_module_502':True,'selected_module_remains_readable':True})
            self.assertEqual(life.sample.call_count,2)
            self.assertIn('--internal',calls[0])
            for args in (call for call in calls if call[0]=='run'):
                self.assertEqual(args[args.index('--cpus')+1],'.1');self.assertEqual(args[args.index('--memory')+1],'64m')
                self.assertNotIn('--publish',args);self.assertIn(m.LABEL+'.role=oneshot',args)
            aliases=calls[1];self.assertIn('gateway',aliases);self.assertNotIn('service-frontend',aliases)
            self.assertIn('listen 8000',(life.work/'route-echo.conf').read_text())

    def test_query_loss_cannot_be_counted_as_route_success(self):
        with tempfile.TemporaryDirectory() as directory:
            life,_=self.probe(directory,strip_query=True)
            with self.assertRaisesRegex(m.GateError,'nginx_proxy_uri_or_query_changed'):
                m.verify_nginx_routes(life,'sha256:product-nginx')
            self.assertFalse(hasattr(life,'nginx_route_assertions'))

    def test_local_scope_and_existing_ci_entry_cover_product_routes(self):
        self.assertEqual(m.FRONTENDS,('console','system-frontend','monitor-frontend','nginx'))
        self.assertEqual(len(m.BACKENDS),6)
        configuration=(m.ROOT/'nginx/nginx.conf').read_text()
        for name in ('system','manager','meta','transfer','orchestrator','develop','service'):
            self.assertNotRegex(configuration,r'(?m)^\s*upstream '+name+r'-frontend\s*\{')
            self.assertIn('rewrite ^/module-ui/'+name+'/(.*)$ /$1 break;',configuration)
            self.assertIn('proxy_pass http://$'+name+'_frontend_upstream;',configuration)
        make=(m.ROOT/'Makefile').read_text()
        self.assertIn('@$(MAKE) test-nginx-routes',make)
        self.assertIn('nginx/',(m.ROOT/'scripts/test/monitor-traces-gate.sh').read_text().splitlines()[3])


class MinioProductContractTests(unittest.TestCase):
    recipe = (m.ROOT/'scripts/infra/Dockerfile.minio').read_text()
    image_id = 'sha256:'+'a'*64

    def fixture(self,directory,*,metadata=None,versions=None):
        life=bare_lifecycle(directory)
        path=life.checkout/'scripts/infra/Dockerfile.minio'
        path.parent.mkdir(parents=True);path.write_text(self.recipe)
        expected=m.minio_product_contract(self.recipe)
        calls=[]
        def docker(*args,**kwargs):
            calls.append((args,kwargs))
            if args[:2]==('image','inspect'):
                self.assertEqual(args[2],'addp-minio:'+expected['minio']['release'])
                return json.dumps(metadata or {'id':self.image_id,'architecture':'arm64','os':'linux'})
            if args[0]=='run':
                name=args[args.index('--entrypoint')+1].rsplit('/',1)[-1]
                self.assertEqual(args[-2:],(self.image_id,'--version'))
                value=expected[name]
                return (versions or {}).get(name,name+' version '+value['release']+' (commit-id='+value['source_commit']+')\nPRIVATE-SENTINEL\n')
            raise AssertionError(args)
        life.docker=docker
        return life,calls

    def test_authoritative_recipe_pins_both_release_source_commits(self):
        expected=m.minio_product_contract(self.recipe)
        self.assertEqual(expected['minio']['source_commit'],'9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a')
        self.assertEqual(expected['mc']['source_commit'],'7394ce0dd2a80935aded936b09fa12cbb3cb8096')

    def test_ambiguous_or_unpinned_recipe_is_rejected(self):
        for value in (self.recipe+self.recipe,self.recipe.replace('github.com/minio/minio.git','github.com/other/minio.git'),
                      self.recipe.replace('9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a','unpinned')):
            with self.subTest(value=value[:20]),self.assertRaises(m.GateError):m.minio_product_contract(value)

    def test_probes_bind_the_same_id_are_readonly_offline_and_keep_only_metadata(self):
        with tempfile.TemporaryDirectory() as directory:
            life,calls=self.fixture(directory)
            m.verify_minio_product(life)
            self.assertEqual(life.minio_image,self.image_id)
            self.assertEqual(set(life.minio_provenance['components']),{'minio','mc'})
            self.assertNotIn('PRIVATE-SENTINEL',json.dumps(life.minio_provenance))
            runs=[args for args,_ in calls if args[0]=='run']
            self.assertEqual(len(runs),2)
            for args in runs:
                self.assertEqual(args[args.index('--pull')+1],'never')
                self.assertEqual(args[args.index('--network')+1],'none')
                self.assertIn('--read-only',args)
                self.assertIn('/data:rw,noexec,nosuid,size=1m',args)
                self.assertEqual(args[args.index('--cpus')+1],'.25')
                self.assertEqual(args[args.index('--memory')+1],'256m')

    def test_wrong_platform_or_mutable_identity_never_runs_a_probe(self):
        for metadata in ({'id':self.image_id,'architecture':'amd64','os':'linux'},
                         {'id':'mutable:tag','architecture':'arm64','os':'linux'},
                         {'id':self.image_id,'architecture':'arm64','os':'darwin'}):
            with tempfile.TemporaryDirectory() as directory:
                life,calls=self.fixture(directory,metadata=metadata)
                with self.assertRaisesRegex(m.GateError,'minio_product_image_identity_invalid'):m.verify_minio_product(life)
                self.assertEqual(len(calls),1)

    def test_wrong_release_or_embedded_source_is_rejected_without_fallback(self):
        for name,expected in m.minio_product_contract(self.recipe).items():
            versions=(name+' version RELEASE.2000-01-01T00-00-00Z (commit-id='+expected['source_commit']+')\n',
                      name+' version '+expected['release']+' (commit-id='+'b'*40+')\n')
            for value in versions:
                with tempfile.TemporaryDirectory() as directory:
                    life,calls=self.fixture(directory,versions={name:value})
                    with self.assertRaisesRegex(m.GateError,'minio_product_'+name+'_version_mismatch'):m.verify_minio_product(life)
                    self.assertEqual([args[0] for args,_ in calls],['image']+['run']*(1 if name=='minio' else 2))
                    self.assertFalse(hasattr(life,'minio_provenance'))

    def test_missing_product_image_refuses_without_pulling_or_building(self):
        with tempfile.TemporaryDirectory() as directory:
            life,_=self.fixture(directory)
            life.docker=mock.Mock(side_effect=m.GateError('command_failed_1'))
            with self.assertRaises(m.GateError):m.verify_minio_product(life)
            life.docker.assert_called_once()
            self.assertEqual(life.docker.call_args.args[:2],('image','inspect'))


class LocalTraceAdmissionTests(unittest.TestCase):
    def test_build_proxy_uses_only_credential_free_local_daemon_transport(self):
        self.assertEqual(m.daemon_build_proxies({}), {})
        proxy = 'http://http.docker.internal:3128'
        self.assertEqual(m.daemon_build_proxies({'HTTPProxy': proxy, 'HTTPSProxy': proxy}),
                         {'HTTP_PROXY': proxy, 'HTTPS_PROXY': proxy})
        self.assertEqual(m.daemon_build_proxies({'HTTPSProxy': 'http.docker.internal:3128'}),
                         {'HTTPS_PROXY': proxy})
        self.assertEqual(m.daemon_build_proxies({'HTTPSProxy': 'http://127.0.0.1:17890'},client=True),
                         {'HTTPS_PROXY':'http://127.0.0.1:17890'})
        with self.assertRaises(m.GateError):
            m.daemon_build_proxies({'HTTPSProxy':proxy},client=True)
        for value in ('http://user:secret@http.docker.internal:3128', 'http://foreign.invalid:3128',
                      'http://http.docker.internal:3128/?secret=value', 'file:///private/proxy'):
            with self.subTest(value=value), self.assertRaises(m.GateError):
                m.daemon_build_proxies({'HTTPSProxy': value})

    def test_only_native_local_profile_is_admitted(self):
        m.validate_profile({}, 'arm64', 'aarch64')
        for key in ('GITHUB_ACTIONS', 'ADDP_ONLINE_HOSTED', 'ADDP_ONLINE_OWNER_MANAGED', 'ADDP_ONLINE_HOST', 'ADDP_ONLINE_LOCAL_SGM'):
            for value in ('true', 'false', '1'):
                with self.subTest(key=key, value=value), self.assertRaises(m.GateError):
                    m.validate_profile({key: value}, 'arm64', 'arm64')
        for machine, daemon in [('x86_64', 'arm64'), ('arm64', 'amd64'), ('amd64', 'amd64')]:
            with self.subTest(machine=machine, daemon=daemon), self.assertRaises(m.GateError):
                m.validate_profile({}, machine, daemon)

    def test_effective_docker_context_cannot_hide_a_remote_daemon(self):
        reader = mock.Mock(return_value='ssh://remote-daemon')
        with self.assertRaises(m.GateError):
            m.effective_docker_endpoint({'DOCKER_HOST': 'unix:///local.sock', 'DOCKER_CONTEXT': 'remote'}, reader)
        reader.assert_called_once_with('remote')
        reader.reset_mock()
        self.assertEqual(m.effective_docker_endpoint({'DOCKER_HOST': 'unix:///private/docker.sock'}, reader), 'unix:///private/docker.sock')
        reader.assert_not_called()
        reader.return_value = 'unix:///desktop/docker.sock'
        self.assertEqual(m.effective_docker_endpoint({}, reader), 'unix:///desktop/docker.sock')
        reader.assert_called_once_with(None)
        for endpoint in ('tcp://127.0.0.1:2375', 'ssh://host', 'unix://relative', '', None):
            with self.subTest(endpoint=endpoint), self.assertRaises(m.GateError):
                m.effective_docker_endpoint({'DOCKER_HOST': endpoint}, lambda name: endpoint)

    def test_user_secrets_and_infra_inputs_are_not_inherited(self):
        environment = {'PATH': '/tools', 'HOME': '/private-home', 'TMPDIR': '/tmp', 'DOCKER_CONTEXT': 'desktop-linux',
                       'POSTGRES_PASSWORD': 'private-password', 'POSTGRES_HOST': 'shared-postgres',
                       'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'private-token', 'HTTP_PROXY': 'private-proxy'}
        with mock.patch.dict(os.environ, environment, clear=True):
            selected = m.safe_environment()
        self.assertEqual(set(selected), {'PATH', 'HOME', 'TMPDIR', 'DOCKER_CONTEXT'})
        self.assertNotIn('private-password', repr(selected))
        self.assertNotIn('private-token', repr(selected))

    def test_zero_or_unbounded_resources_oom_and_nonzero_exit_fail(self):
        cpu, memory = m.validate_resources([row(3, 5 * 1024**3), row(3, 5 * 1024**3)])
        self.assertEqual(cpu, 6)
        self.assertEqual(memory, 10 * 1024**3)
        cases = [row(cpu=0), row(memory=0), row(cpu=8.1), row(memory=12 * 1024**3 + 1), row(oom=True),
                 row(status='exited', exit_code=2), row(status='dead', exit_code=1)]
        for bad in cases:
            with self.subTest(bad=bad), self.assertRaises(m.GateError):
                m.validate_resources([bad])
        legacy = row(cpu=0)
        legacy['HostConfig'].update(CpuQuota=25000, CpuPeriod=100000)
        self.assertEqual(m.validate_resources([legacy])[0], .25)

    def test_aggregate_hard_limits_cannot_hide_behind_individual_limits(self):
        with self.assertRaises(m.GateError):
            m.validate_resources([row(4.1), row(4.1)])
        with self.assertRaises(m.GateError):
            m.validate_resources([row(memory=7 * 1024**3), row(memory=6 * 1024**3)])

    def test_successful_init_is_not_an_application_failure(self):
        m.validate_resources([row(status='exited', service='trace-store-init'),
                              row(status='exited', service='runtime-log-store-init')])

    def test_only_explicit_owned_oneoffs_and_stopped_builder_may_exit_zero(self):
        builder = row(status='exited', service='builder')
        m.validate_resources([builder], allowed_exits=('owned-id',))
        with self.assertRaises(m.GateError):
            m.validate_resources([builder], allowed_exits=('foreign-id',))
        helper = row(status='exited', service='identity')
        helper['Config']['Labels'][m.LABEL + '.role'] = 'oneshot'
        m.validate_resources([helper])
        helper['State']['ExitCode'] = 1
        with self.assertRaises(m.GateError):
            m.validate_resources([helper])

    def test_owned_oneoff_cannot_remove_exit_state_before_guard_inspection(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);life.command=mock.Mock(return_value='completed')
            with self.assertRaisesRegex(m.GateError,'oneshot_exit_state_must_be_retained'):
                life.docker('run','--rm','--label',m.LABEL+'.role=oneshot','owned-image')
            life.command.assert_not_called()
            self.assertEqual(life.docker('run','--label',m.LABEL+'.role=oneshot','owned-image'),'completed')

    def test_builder_stop_captures_exact_fixed_state_before_validating(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);life.builder_ids={'owned-id'}
            life.docker=mock.Mock(side_effect=[json.dumps([row()]),'',json.dumps([row(status='exited',exit_code=137)])])
            with self.assertRaisesRegex(m.GateError,'owned_container_failed'):life.stop_builder()
            self.assertTrue(life.builder_stopped)
            self.assertEqual(life.builder_stop_states,[{'id':'owned-id','status':'exited','exit_code':137,'oom':False}])

    def test_only_exact_planned_builder_may_use_verified_stop_exit_one(self):
        m.validate_resources([row(status='exited',exit_code=1)],allowed_exits=('owned-id',))
        for bad,allowed in [(row(status='exited',exit_code=1),('foreign-id',)),
                            (row(status='exited',exit_code=1,oom=True),('owned-id',)),
                            (row(status='dead',exit_code=1),('owned-id',)),
                            (row(status='exited',exit_code=137),('owned-id',))]:
            with self.subTest(bad=bad),self.assertRaises(m.GateError):m.validate_resources([bad],allowed)

    def test_long_running_product_exit_zero_is_still_a_stop_condition(self):
        for name in ('system-backend', 'gateway', 'tempo', 'alloy'):
            with self.subTest(service=name), self.assertRaises(m.GateError):
                m.validate_resources([row(status='exited', service=name)])


class LocalTraceIsolationTests(unittest.TestCase):
    def test_telemetry_configuration_probes_use_product_mounts_identity_and_no_network(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);life.docker=mock.Mock(return_value='verified')
            life.deployment_documents={name:{'image':'pinned-'+name,'user':'501:20','cap_drop':['ALL'],'command':['-config.file=/etc/loki/runtime.yml','-config.expand-env=true'],
                'environment':{'TEST_KEY':'private-placeholder'},'volumes':[{'type':'bind','source':directory,'target':'/configuration','read_only':True}]} for name in ('loki','trace-query')}
            m.verify_telemetry_configuration(life)
            self.assertEqual(life.docker.call_count,2)
            for call in life.docker.call_args_list:
                args=call.args
                self.assertNotIn('--rm',args);self.assertIn('none',args);self.assertIn('--read-only',args)
                self.assertIn('501:20',args);self.assertIn('ALL',args);self.assertIn(m.LABEL+'.role=oneshot',args)
            self.assertIn('-verify-config=true',life.docker.call_args_list[0].args)
            self.assertEqual(life.docker.call_args_list[1].args[-1],'-t')

    def test_product_trace_template_workspace_is_declared_without_selecting_capacity(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);life.command=mock.Mock(return_value='PATH=/tools\0')
            env=m.defaults(life)
            self.assertEqual(env['TRACE_T2_WORK'],str(life.work))
            self.assertNotIn('trace-load',m.TRACE)
            self.assertNotIn('trace-load',m.LIMITS)

    def test_deployment_configuration_failure_prevents_builder_and_compilation(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);life.command=mock.Mock(return_value='frozen-commit')
            life.docker=mock.Mock()
            with mock.patch.object(m,'committed_inputs',return_value={}),mock.patch.object(m,'defaults',return_value={}),\
                 mock.patch.object(m,'verify_minio_product'),mock.patch.object(m,'prepare_deployment_documents',side_effect=m.GateError('configuration_rejected')):
                with self.assertRaisesRegex(m.GateError,'configuration_rejected'):m.build(life)
            life.docker.assert_not_called()

    def test_setup_deployment_input_is_accepted_by_actual_product_renderer(self):
        renderer_spec=importlib.util.spec_from_file_location('trace_platform_renderer',m.ROOT/'scripts/prod/generate-trace-platform.py')
        renderer=importlib.util.module_from_spec(renderer_spec);renderer_spec.loader.exec_module(renderer)
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);env={'ADDP_TRACE_DEPLOYMENT_DIR':str(life.work/'deployment')}
            def command(args,**kwargs):
                if args[1]=='scripts/test/trace-deployment-fixture.py':
                    (life.work/'deployment').mkdir(mode=0o700)
                    for name in ('source-tls','query-tls'):
                        tls=life.work/name;tls.mkdir(mode=0o700)
                        for filename in ('ca.crt','client.crt','client.key'):
                            p=tls/filename;p.write_text('public-contract-test');p.chmod(0o600)
                elif args[1]=='scripts/prod/generate-trace-platform.py':
                    with mock.patch.dict(os.environ,kwargs['env'],clear=True):renderer.render()
                    config=json.loads((life.work/'deployment/platform-traces.json').read_text())
                    self.assertEqual(set(config['services']),set(m.BACKENDS))
                    self.assertEqual(len(config['services']['monitor-backend']['volumes']),2)
                elif args[:2]==['docker','compose']:
                    services={name:{'environment':{},'volumes':[]} for name in m.BACKENDS+m.FRONTENDS+m.TRACE+('postgres','redis')}
                    services['postgres'].update(command=[],volumes=[{'type':'volume','source':'postgres-data','target':'/data'}])
                    services['trace-query']['user']='0:0'
                    return json.dumps({'services':services})
                else:raise AssertionError(args)
            life.command=command
            documents=m.prepare_deployment_documents(life,env)
            self.assertEqual(documents['trace-query']['user'],str(os.getuid())+':'+str(os.getgid()))

    def document(self):
        return {'system-backend': {
            'image': 'old-product-tag', 'build': '.', 'restart': 'always', 'ports': ['8180:8180'],
            'depends_on': ['postgres'], 'profiles': ['production'], 'networks': ['personal-network'],
            'volumes': [{'type': 'volume', 'source': 'personal-runtime-logs', 'target': '/var/log/addp'},
                        {'type': 'volume', 'source': 'postgres-data', 'target': '/data'},
                        {'type': 'bind', 'source': '/private/synthetic/cert', 'target': '/cert', 'read_only': True}]}}

    def test_names_networks_ports_and_volumes_are_owned_and_loopback_only(self):
        source = self.document()
        original = copy.deepcopy(source)
        result = m.isolate(source, 'synthetic-owned', {'system-backend': 'sha256:owned-product'})
        self.assertEqual(source, original)
        service = result['services']['system-backend']
        self.assertEqual(service['container_name'], 'synthetic-owned-system-backend')
        self.assertEqual(service['image'], 'sha256:owned-product')
        self.assertEqual(service['restart'], 'no')
        self.assertEqual(service['labels'][m.LABEL], 'synthetic-owned')
        self.assertEqual(service['ports'], [{'target': 8180, 'host_ip': '127.0.0.1', 'published': '0'}])
        self.assertEqual(set(service['networks']), {'data', 'entry'})
        self.assertTrue(result['networks']['data']['internal'])
        for declaration in result['volumes'].values():
            self.assertEqual(declaration['labels'][m.LABEL], 'synthetic-owned')
            self.assertFalse(declaration.get('external'))
        self.assertEqual(service['volumes'][0]['source'], 'runtime-logs')
        self.assertTrue(all(mount['volume']['nocopy'] for mount in service['volumes'] if mount['type']=='volume'))
        for key in ('build', 'depends_on', 'profiles', 'extends'):
            self.assertNotIn(key, service)
        self.assertGreater(service['cpus'], 0)
        self.assertRegex(service['mem_limit'], r'^[1-9][0-9]*m$')

    def test_privileged_host_network_and_writable_bind_drift_fail_closed(self):
        for mutate in [lambda s: s.update(privileged=True), lambda s: s.update(network_mode='host'),
                       lambda s: s['volumes'].append({'type': 'bind', 'source': '/personal', 'target': '/writable', 'read_only': False})]:
            source = self.document()
            mutate(source['system-backend'])
            with self.assertRaises(m.GateError):
                m.isolate(source, 'owned', {})
        with self.assertRaises(m.GateError):
            m.isolate({'unknown-personal-service': {}}, 'owned', {})

    def test_application_services_have_no_external_network_route(self):
        source = {'develop-backend': {'image': 'product', 'volumes': []},
                  'alloy': {'image': 'alloy', 'volumes': []}, 'tempo': {'image': 'tempo', 'volumes': []}}
        isolated = m.isolate(source, 'owned', {})
        for service in isolated['services'].values():
            self.assertEqual(set(service['networks']), {'data'})
            self.assertNotIn('ports', service)


class LocalTraceResourceGuardTests(unittest.TestCase):
    def sample_lifecycle(self, directory, allocation):
        life = bare_lifecycle(directory)
        calls = []
        def docker(*args, **kwargs):
            calls.append((args, kwargs))
            if args[:2] == ('container', 'ls'):
                return 'owned-id'
            if args[:2] == ('volume', 'ls'):
                return 'owned-volume'
            if args[0] == 'inspect':
                return json.dumps([row()])
            if args[0] == 'run':
                return 'owned-sampler-id'
            if args[0] == 'exec':
                return str(allocation)
            if args[0] == 'rm':
                return ''
            raise AssertionError(args)
        life.docker = docker
        return life, calls

    def test_last_read_cannot_turn_an_expired_scan_into_success(self):
        for finished in (4, 4.01):
            with self.subTest(finished=finished), tempfile.TemporaryDirectory() as directory:
                life, _ = self.sample_lifecycle(directory, 1024)
                clock = [0.0]
                def read(**kwargs):
                    clock[0] = finished
                    return 0
                life.owned_bind_bytes = read
                with mock.patch.object(m.time, 'monotonic', side_effect=lambda: clock[0]):
                    with self.assertRaisesRegex(m.GateError, 'resource_sample_deadline_exceeded'):
                        life.sample()
                self.assertEqual(life.samples, [])

    def test_success_preserves_step_costs_counts_and_shared_remaining_budget(self):
        with tempfile.TemporaryDirectory() as directory:
            life, calls = self.sample_lifecycle(directory, 1024)
            life.sampler_volumes = ('owned-volume',)
            underlying = life.docker
            clock = [0.0]
            def docker(*args, **kwargs):
                clock[0] += .5
                return underlying(*args, **kwargs)
            life.docker = docker
            with mock.patch.object(m.time, 'monotonic', side_effect=lambda: clock[0]):
                life.sample()
            sample = life.samples[-1]
            self.assertEqual(sample['container_count'], 1)
            self.assertEqual(sample['volume_count'], 1)
            self.assertEqual(sample['sample_duration_seconds'], 2)
            self.assertEqual([step['operation'] for step in sample['steps']], [
                'list_owned_containers', 'inspect_owned_containers', 'validate_container_states',
                'list_owned_volumes', 'measure_owned_volumes', 'measure_private_bindings', 'final_validation'])
            self.assertTrue(all(step['result'] == 'passed' for step in sample['steps']))
            self.assertEqual([kwargs['timeout'] for _, kwargs in calls], [4, 3.5, 3, 2.5])

    def test_failed_tick_keeps_completed_and_failed_steps_through_monitor(self):
        with tempfile.TemporaryDirectory() as directory:
            life, _ = self.sample_lifecycle(directory, 1024)
            life.sampler_volumes = ('owned-volume',)
            underlying = life.docker
            clock = [0.0]
            def docker(*args, **kwargs):
                if args[0] == 'exec':
                    clock[0] = 4.05
                    raise m.GateError('command_deadline_exceeded')
                clock[0] += .1
                return underlying(*args, **kwargs)
            life.docker = docker
            life.owned_bind_bytes = mock.Mock(side_effect=AssertionError('must stop'))
            with mock.patch.object(m.time, 'monotonic', side_effect=lambda: clock[0]):
                life.monitor()
            failure = life.resource_failure
            self.assertEqual(failure['error_code'], 'resource_sample_deadline_exceeded')
            self.assertEqual(failure['operation'], 'measure_owned_volumes')
            self.assertEqual(failure['container_count'], 1)
            self.assertEqual(failure['volume_count'], 1)
            self.assertEqual(failure['steps'][-1]['result'], 'failed')
            self.assertAlmostEqual(failure['steps'][-1]['duration_seconds'], 3.75)
            self.assertTrue(all(step['result'] == 'passed' for step in failure['steps'][:-1]))
            life.owned_bind_bytes.assert_not_called()
            self.assertEqual(life.samples, [])

    def test_lock_wait_is_separate_from_scan_duration(self):
        with tempfile.TemporaryDirectory() as directory:
            life, _ = self.sample_lifecycle(directory, 1024)
            clock = [0.0]
            def entered(**kwargs): clock[0] = 2; return True
            def read(**kwargs): clock[0] = 3; return 0
            life.sample_lock = mock.MagicMock()
            life.sample_lock.acquire.side_effect = entered
            life.owned_bind_bytes = read
            with mock.patch.object(m.time, 'monotonic', side_effect=lambda: clock[0]):
                life.sample()
            self.assertEqual(life.samples[-1]['lock_wait_seconds'], 2)
            self.assertEqual(life.samples[-1]['sample_duration_seconds'], 1)

    def test_inspect_json_parsing_must_finish_within_original_deadline(self):
        with tempfile.TemporaryDirectory() as directory:
            life, _ = self.sample_lifecycle(directory, 1024)
            clock = [0.0]
            loads = m.json.loads
            def parse(raw): clock[0] = 4.01; return loads(raw)
            with mock.patch.object(m.time, 'monotonic', side_effect=lambda: clock[0]), \
                 mock.patch.object(m.json, 'loads', side_effect=parse):
                with self.assertRaisesRegex(m.GateError, 'resource_sample_deadline_exceeded'):
                    life.sample()
            self.assertEqual(life.samples, [])
            self.assertEqual(life.resource_failure['operation'], 'inspect_owned_containers')
            self.assertEqual(life.resource_failure['steps'][-1]['result'], 'failed')

    def test_actual_volume_stop_line_rejects_equal_threshold(self):
        with tempfile.TemporaryDirectory() as directory:
            for allocated in (m.VOLUME_STOP, m.VOLUME_STOP + 4096):
                life, _ = self.sample_lifecycle(directory, allocated)
                with mock.patch.object(m.time, 'monotonic', return_value=1), self.assertRaises(m.GateError):
                    life.sample()
                self.assertEqual(life.samples, [])
                self.assertEqual(life.resource_failure['measured_resources']['volume_allocated_bytes'], allocated)
            life, calls = self.sample_lifecycle(directory, m.VOLUME_STOP - 4096)
            with mock.patch.object(m.time, 'monotonic', return_value=1):
                life.sample()
            self.assertEqual(life.samples[-1]['volume_allocated_bytes'], m.VOLUME_STOP - 4096)
            usage = next(args for args, _ in calls if args[0] == 'run')
            self.assertIn('none', usage)
            self.assertIn(str(m.SAMPLER_CPU), usage)
            self.assertIn('64m', usage)
            self.assertTrue(all('readonly' in arg for arg in usage if arg.startswith('type=volume,')))

    def test_repeated_sampling_reuses_readonly_helper_and_remounts_changed_owned_set(self):
        with tempfile.TemporaryDirectory() as directory:
            life,calls=self.sample_lifecycle(directory,1024)
            with mock.patch.object(m.time,'monotonic',return_value=1):
                life.sample()
                life.sample()
                life.owned=lambda kind,**kw:['owned-id'] if kind=='container' else ['owned-volume','new-owned-volume']
                life.sample()
            runs=[args for args,_ in calls if args[0]=='run']
            executions=[args for args,_ in calls if args[0]=='exec']
            removals=[args for args,_ in calls if args[0]=='rm']
            self.assertEqual(len(runs),2)
            self.assertEqual(len(executions),3)
            self.assertEqual(len(removals),1)
            self.assertTrue(all('readonly' in arg for args in runs for arg in args if arg.startswith('type=volume,')))
            self.assertTrue(all('/data:rw,noexec,nosuid,size=1m' in args for args in runs))

    def test_builder_volume_ownership_does_not_capture_application_volumes(self):
        with tempfile.TemporaryDirectory() as directory:
            life, calls = self.sample_lifecycle(directory, 1024)
            life.builder_ids = {'owned-builder'}
            underlying = life.docker
            def docker(*args, **kwargs):
                if args[0] == 'inspect':
                    return json.dumps([row(), dict(row(), Id='owned-builder', Mounts=[
                        {'Type': 'volume', 'Name': 'owned-buildkit-state'},
                        {'Type': 'bind', 'Source': '/not-a-builder-volume'},
                    ])])
                return underlying(*args, **kwargs)
            life.docker = docker
            with mock.patch.object(m.time, 'monotonic', return_value=1):
                life.sample()
            self.assertEqual(life.builder_volumes, {'owned-buildkit-state'})

    def test_invalid_volume_measurement_preserves_only_safe_diagnostics_and_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            life, _ = self.sample_lifecycle(directory, 'du: PRIVATE-SENTINEL: missing\n1024')
            with mock.patch.object(m.time,'monotonic',return_value=1):
                life.monitor()
            self.assertEqual(life.failure,'owned_volume_measurement_invalid')
            self.assertEqual(life.resource_failure['exception_type'],'GateError')
            measured=life.resource_failure['measurement']
            self.assertEqual(measured['line_count'],2)
            self.assertTrue(measured['du_diagnostic'])
            self.assertEqual(len(measured['output_sha256']),64)
            self.assertNotIn('PRIVATE-SENTINEL',json.dumps(life.resource_failure))
            self.assertEqual(life.samples,[])

    def test_disappeared_volume_file_requires_a_complete_rescan(self):
        with tempfile.TemporaryDirectory() as directory:
            life,calls=self.sample_lifecycle(directory,1024)
            docker=life.docker;values=iter(['du: owned: No such file or directory\n512','1024'])
            life.docker=lambda *args,**kw: next(values) if args[0]=='exec' else docker(*args,**kw)
            with mock.patch.object(m.time,'monotonic',return_value=1):
                life.sample()
            self.assertEqual(life.samples[-1]['volume_allocated_bytes'],1024)

    def test_disappeared_private_file_requires_a_complete_rescan(self):
        with tempfile.TemporaryDirectory() as directory:
            life,_=self.sample_lifecycle(directory,1024)
            life.last_failure={'du_missing_file':True}
            life.owned_bind_bytes=mock.Mock(side_effect=[m.GateError('command_failed_1'),2048])
            with mock.patch.object(m.time,'monotonic',return_value=1):life.sample()
            self.assertEqual(life.samples[-1]['volume_allocated_bytes'],3072)

    def test_continuous_file_churn_exhausts_original_sample_deadline(self):
        with tempfile.TemporaryDirectory() as directory:
            life,_=self.sample_lifecycle(directory,'du: owned: No such file or directory\n512')
            tick=[0]
            def now():tick[0]+=.1;return tick[0]
            with mock.patch.object(m.time,'monotonic',side_effect=now):
                with self.assertRaisesRegex(m.GateError,'resource_sample_deadline_exceeded'):life.sample()
            self.assertEqual(life.samples,[])

    def test_every_resource_sampling_command_has_a_short_timeout(self):
        with tempfile.TemporaryDirectory() as directory:
            life, calls = self.sample_lifecycle(directory, 1024)
            with mock.patch.object(m.time, 'monotonic', return_value=1):
                life.sample()
            self.assertTrue(calls)
            for args, kwargs in calls:
                self.assertGreater(kwargs.get('timeout', 0), 0, args)
                self.assertLessEqual(kwargs['timeout'], 5, args)

    def test_guard_sample_error_stops_future_guarded_commands(self):
        with tempfile.TemporaryDirectory() as directory:
            life = bare_lifecycle(directory)
            life.sample = mock.Mock(side_effect=m.GateError('owned_volume_stop_line_reached'))
            with mock.patch.object(m.time,'monotonic',return_value=1):
                life.monitor()
            self.assertEqual(life.failure, 'owned_volume_stop_line_reached')
            with mock.patch.object(m.time, 'monotonic', return_value=1), mock.patch.object(m.subprocess, 'Popen') as start:
                with self.assertRaises(m.GateError):
                    life.command(['never-start-this'])
            start.assert_not_called()

    def test_sampler_reserve_fits_runtime_browser_and_build_aggregate_limits(self):
        running={k:v for k,v in m.LIMITS.items() if k not in ('runtime-log-store-init','trace-store-init')}
        self.assertLessEqual(sum(v[0] for v in running.values()) + .5 + m.SAMPLER_CPU, 8)
        self.assertLessEqual(sum(v[1] for v in running.values()) + 1024 + 64, 12*1024)
        dependencies={k:v for k,v in m.LIMITS.items() if k in m.TRACE+('postgres','business-postgres','redis') and k not in ('runtime-log-store-init','trace-store-init')}
        rows=[row(cpu,memory*1024**2) for cpu,memory in dependencies.values()]+[row(1,2*1024**3),row(1,2*1024**3),row(m.SAMPLER_CPU,64*1024**2)]
        m.validate_resources(rows)
        with self.assertRaisesRegex(m.GateError,'aggregate_hard_limit_exceeded'):m.validate_resources(rows+[row(1)])

    def test_sampler_timeout_reports_the_failed_operation_and_stops_requests(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory)
            life.sample_operation='measure_owned_volumes'
            life.sample=mock.Mock(side_effect=m.GateError('command_deadline_exceeded'))
            with mock.patch.object(m.time,'monotonic',return_value=1):
                life.monitor()
                self.assertEqual(life.failure,'resource_sample_deadline_exceeded')
                self.assertEqual(life.resource_failure['operation'],'measure_owned_volumes')
                with self.assertRaises(m.GateError):life.request_timeout()

    def test_sampling_duration_is_deducted_from_the_five_second_interval(self):
        with tempfile.TemporaryDirectory() as directory:
            life = bare_lifecycle(directory)
            stop = mock.Mock()
            stop.is_set.side_effect = [False, True]
            life.stop = stop
            life.sample = mock.Mock()
            with mock.patch.object(m.time, 'monotonic', side_effect=[10, 12, 12]):
                life.monitor()
            stop.wait.assert_called_once()
            self.assertLessEqual(stop.wait.call_args.args[0], 3)


class ResourceDiagnosticContractTests(unittest.TestCase):
    @staticmethod
    def scan(phase, started, duration=.1):
        return {'phase':phase, 'sample_started_monotonic':started, 'sample_finished_monotonic':started+duration,
            'lock_wait_seconds':0, 'container_count':20, 'volume_count':8,
            'complete_scan_retries':{'volume':0,'private':0},
            'steps':[{'operation':'inspect_owned_containers','duration_seconds':duration,'processing_seconds':.01,
                'commands':[{'spawn_seconds':.02,'wait_seconds':.03,'output_seconds':.01,'cancel_cleanup_seconds':0}]}]}

    def test_phase_counts_and_step_distributions(self):
        rows=[]
        for phase,count in [('product_compile',12),('product_image_gateway',12),('diagnostic_idle',25),('diagnostic_steady',65)]:
            for _ in range(count): rows.append(self.scan(phase,len(rows)*4))
        rows.append(self.scan('diagnostic_startup_1',len(rows)*4))
        summary=m.summarize_resource_diagnostic(rows)
        m.validate_resource_diagnostic(summary,('compile','images','idle','steady'))
        self.assertEqual(summary['phases']['compile']['sample_count'],12)
        self.assertEqual(summary['phases']['startup']['sample_count'],1)
        costs=summary['phases']['compile']['operations']['inspect_owned_containers']
        self.assertEqual(costs['wait_seconds']['p99'],.03)
        self.assertEqual(summary['max_start_interval_seconds'],4)

    def test_missing_phase_and_insufficient_samples_are_rejected(self):
        summary=m.summarize_resource_diagnostic([self.scan('product_compile',0)])
        for phases in [('compile',),('images',),('idle',),('steady',)]:
            with self.subTest(phases=phases),self.assertRaisesRegex(m.GateError,'diagnostic_phase_samples_insufficient'):
                m.validate_resource_diagnostic(summary,phases)

    def test_four_second_and_five_second_boundaries(self):
        for duration,gap,allowed in [(3.999,5,True),(4,4,False),(.1,5.001,False)]:
            summary=m.summarize_resource_diagnostic([self.scan('product_compile',0,duration),self.scan('product_compile',gap)])
            with self.subTest(duration=duration,gap=gap):
                if allowed:m.validate_resource_diagnostic(summary,())
                else:
                    with self.assertRaises(m.GateError):m.validate_resource_diagnostic(summary,())

    def fixture(self,directory):
        life=bare_lifecycle(directory);clock=[0.0];life.command=mock.Mock(return_value='')
        life.compose=mock.Mock(side_effect=lambda *args,**kwargs:life.command(['docker','compose'],timeout=300))
        life.input_hashes={'owned':'hash'};life.module_commits={'system':'source'}
        def sample():
            life.samples.append(self.scan(life.phase,clock[0]));clock[0]+=.1
        def wait(seconds):
            end=clock[0]+seconds
            while clock[0]+4<end:
                clock[0]+=4;sample()
            clock[0]=end
        def build(life):
            life.command(['make','build'],timeout=2200)
            for phase in ('product_compile','product_image_gateway'):
                life.phase=phase
                for _ in range(12):clock[0]+=4;sample()
            return 'source',{},{}
        def setup(life,env,images):
            m.start_deployment(life.compose)
        life.sample=sample;life.wait=wait
        return life,clock,build,setup

    def test_one_round_uses_shared_build_deploy_and_six_checks_without_identity(self):
        with tempfile.TemporaryDirectory() as directory:
            life,clock,build,setup=self.fixture(directory);command=life.command;compose=life.compose;report={}
            with mock.patch.object(m.time,'monotonic',side_effect=lambda:clock[0]), \
                 mock.patch.object(m,'build',side_effect=build) as built, mock.patch.object(m,'setup',side_effect=setup), \
                 mock.patch.object(m,'identity',side_effect=AssertionError('identity forbidden')):
                m.run_resource_guard_diagnostic(life,report,0)
            built.assert_called_once()
            self.assertEqual(len(report['startup_checks']),6)
            self.assertEqual(compose.call_count,3)
            self.assertIs(life.command,command);self.assertIs(life.compose,compose)
            self.assertEqual(command.call_args_list[0].kwargs['timeout'],1800)
            self.assertTrue(all(0 < call.kwargs['timeout'] <= 180 for call in command.call_args_list[1:]))
            self.assertGreaterEqual(report['summary']['phases']['idle']['sample_count'],25)
            self.assertGreaterEqual(report['summary']['phases']['steady']['sample_count'],65)

    def test_first_failure_propagates_without_retry_and_restores_wrapper(self):
        with tempfile.TemporaryDirectory() as directory:
            life,clock,_,_=self.fixture(directory);command=life.command
            with mock.patch.object(m.time,'monotonic',side_effect=lambda:clock[0]), \
                 mock.patch.object(m,'build',side_effect=m.GateError('resource_sample_deadline_exceeded')) as build, \
                 mock.patch.object(m,'setup') as setup:
                with self.assertRaisesRegex(m.GateError,'resource_sample_deadline_exceeded'):
                    m.run_resource_guard_diagnostic(life,{},0)
            build.assert_called_once();setup.assert_not_called();self.assertIs(life.command,command)

    def test_build_startup_and_whole_window_deadlines_are_absolute(self):
        for stage in ('build','startup','whole'):
            with self.subTest(stage=stage),tempfile.TemporaryDirectory() as directory:
                life,clock,build,setup=self.fixture(directory);command=life.command;compose=life.compose
                def bounded_build(life):
                    result=build(life)
                    if stage=='build':clock[0]=1800
                    return result
                def bounded_setup(life,env,images):
                    setup(life,env,images)
                    if stage=='startup':clock[0]+=180
                    if stage=='whole':clock[0]=2399
                with mock.patch.object(m.time,'monotonic',side_effect=lambda:clock[0]), \
                     mock.patch.object(m,'build',side_effect=bounded_build),mock.patch.object(m,'setup',side_effect=bounded_setup):
                    with self.assertRaises(m.GateError):m.run_resource_guard_diagnostic(life,{},0)
                self.assertIs(life.command,command);self.assertIs(life.compose,compose)

    def test_changed_startup_groups_are_rejected_and_not_retried(self):
        with tempfile.TemporaryDirectory() as directory:
            life,clock,build,_=self.fixture(directory);compose=life.compose
            def setup(life,env,images):life.compose('up','-d','--wait','--no-build','monitor-backend')
            with mock.patch.object(m.time,'monotonic',side_effect=lambda:clock[0]), \
                 mock.patch.object(m,'build',side_effect=build),mock.patch.object(m,'setup',side_effect=setup):
                with self.assertRaisesRegex(m.GateError,'diagnostic_startup_group_mismatch'):
                    m.run_resource_guard_diagnostic(life,{},0)
            compose.assert_not_called();self.assertIs(life.compose,compose)

    def test_cadence_failure_stops_during_build_without_waiting_for_summary(self):
        with tempfile.TemporaryDirectory() as directory:
            life,clock,_,_=self.fixture(directory);original_sample=life.sample
            def build(life):
                life.phase='product_compile';life.sample();clock[0]+=6;life.sample()
                raise AssertionError('must stop at cadence violation')
            with mock.patch.object(m.time,'monotonic',side_effect=lambda:clock[0]), \
                 mock.patch.object(m,'build',side_effect=build) as built,mock.patch.object(m,'setup') as setup:
                with self.assertRaisesRegex(m.GateError,'diagnostic_sample_cadence_exceeded'):
                    m.run_resource_guard_diagnostic(life,{},0)
            built.assert_called_once();setup.assert_not_called();self.assertIs(life.sample,original_sample)


class HostCLIComparisonTests(unittest.TestCase):
    def fixture(self, directory, *, command_seconds=.01, cpu_seconds=.02):
        life=bare_lifecycle(directory);clock=[0.0];cpu=[0.0]
        process=mock.Mock(pid=1234,returncode=0);process.poll.return_value=0
        def wait(**kwargs):clock[0]+=command_seconds;cpu[0]+=cpu_seconds
        process.wait.side_effect=wait
        def usage():
            return {'parent_user_seconds':0.0,'parent_system_seconds':0.0,
                'children_user_seconds':cpu[0],'children_system_seconds':0.0,
                'parent_rss_high_water_bytes':1024**2,'children_rss_high_water_bytes':2*1024**2}
        return life,clock,cpu,process,usage

    def invoke(self, life, clock, process, usage, report):
        def sleep(seconds):clock[0]+=seconds
        with mock.patch.object(m.time,'monotonic',side_effect=lambda:clock[0]), \
             mock.patch.object(m.time,'monotonic_ns',side_effect=lambda:round(clock[0]*1e9)), \
             mock.patch.object(m.time,'sleep',side_effect=sleep), \
             mock.patch.object(m.subprocess,'Popen',return_value=process), \
             mock.patch.object(m,'host_cli_usage',side_effect=usage), \
             mock.patch.object(m,'require_no_child_processes'):
            m.run_host_cli_comparison(life,report,0,usage())

    def test_twenty_serial_groups_preserve_spacing_and_allow_multithread_cpu_above_wall(self):
        with tempfile.TemporaryDirectory() as directory:
            life,clock,_,process,usage=self.fixture(directory);report={}
            self.invoke(life,clock,process,usage,report)
            self.assertEqual(report['measurement_result'],'passed')
            self.assertEqual(len(report['groups']),20)
            self.assertEqual(len(report['commands']),40)
            starts=[r['spawn_started_monotonic_ns'] for r in report['commands']]
            self.assertTrue(all(b-a>=10**9 for a,b in zip(starts,starts[1:])))
            self.assertTrue(all(r['child_user_cpu_seconds']>r['duration_seconds'] for r in report['commands']))
            self.assertTrue(all(g['duration_seconds']<4 for g in report['groups']))
            self.assertFalse(any('wait_estimate' in k for r in report['commands'] for k in r))

    def test_first_query_failure_stops_without_retry_and_keeps_cpu_record(self):
        with tempfile.TemporaryDirectory() as directory:
            life,clock,_,process,usage=self.fixture(directory);process.returncode=17;report={}
            with self.assertRaisesRegex(m.GateError,'command_failed_17'):
                self.invoke(life,clock,process,usage,report)
            self.assertEqual(len(report['commands']),1)
            self.assertIn('child_user_cpu_seconds',report['commands'][0])
            self.assertEqual(report['measurement_result'],'failed')

    def test_group_records_and_waiting_count_towards_four_seconds(self):
        with tempfile.TemporaryDirectory() as directory:
            life,clock,_,process,usage=self.fixture(directory,command_seconds=2.1);report={}
            with self.assertRaisesRegex(m.GateError,'command_deadline_exceeded'):
                self.invoke(life,clock,process,usage,report)
            self.assertEqual(len(report['commands']),2)
            self.assertLess(len(report['groups']),20)

    def test_expired_request_budget_does_not_spawn_any_query(self):
        with tempfile.TemporaryDirectory() as directory:
            life,clock,_,process,usage=self.fixture(directory);clock[0]=118;report={}
            with self.assertRaisesRegex(m.GateError,'host_cli_request_deadline_exceeded'):
                self.invoke(life,clock,process,usage,report)
            process.wait.assert_not_called()
            self.assertEqual(report['commands'],[])

    def test_cpu_and_rss_stop_lines_use_totals_and_high_water_not_rss_delta(self):
        origin={'parent_user_seconds':0.,'parent_system_seconds':0.,'children_user_seconds':0.,
                'children_system_seconds':0.,'parent_rss_high_water_bytes':200*1024**2,'children_rss_high_water_bytes':0}
        for changes,code in (({'children_user_seconds':30.},'host_cli_cpu_limit'),
                             ({'parent_rss_high_water_bytes':256*1024**2},'host_cli_rss_limit'),
                             ({'children_system_seconds':-1.},'host_cli_cpu_counter_invalid')):
            with self.subTest(changes=changes),self.assertRaisesRegex(m.GateError,code):
                m.check_host_cli_usage(origin,{**origin,**changes},{})

    def test_extra_or_unreaped_children_refuse_cpu_attribution(self):
        with mock.patch.object(m.os,'waitpid',side_effect=ChildProcessError):m.require_no_child_processes()
        for result in ((0,0),(1234,0)):
            with mock.patch.object(m.os,'waitpid',return_value=result),self.assertRaisesRegex(m.GateError,'host_cli_extra_child'):
                m.require_no_child_processes()

    def test_cpu_usage_normalizes_platform_rss_units(self):
        row=SimpleNamespace(ru_utime=.1,ru_stime=.2,ru_maxrss=123)
        with mock.patch.object(m.resource,'getrusage',return_value=row):
            with mock.patch.object(m.sys,'platform','darwin'):self.assertEqual(m.host_cli_usage()['parent_rss_high_water_bytes'],123)
            with mock.patch.object(m.sys,'platform','linux'):self.assertEqual(m.host_cli_usage()['parent_rss_high_water_bytes'],123*1024)

    def test_unexpected_objects_stop_without_exporting_names(self):
        with tempfile.TemporaryDirectory() as directory:
            life,clock,_,process,usage=self.fixture(directory);report={}
            def start(*args,**kwargs):kwargs['stdout'].write(b'PRIVATE-OBJECT\n');return process
            # invoke owns its mock; return a process whose wait writes the output.
            def wait(**kwargs):clock[0]+=.01
            def owned(kind,**kwargs):
                with mock.patch.object(m.subprocess,'Popen',side_effect=start):
                    return m.Lifecycle.owned(life,kind,**kwargs)
            life.owned=owned;process.wait.side_effect=wait
            with self.assertRaisesRegex(m.GateError,'host_cli_unexpected_objects'):
                self.invoke(life,clock,process,usage,report)
            self.assertNotIn('PRIVATE',json.dumps(report))

    def test_buffer_and_output_stop_lines_are_checked_without_truncating(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory)
            with mock.patch.object(m,'HOST_CLI_OUTPUT_LIMIT',100),self.assertRaisesRegex(m.GateError,'host_cli_output_limit'):
                m.check_host_cli_buffers(life,{'large':'x'*200})
            with mock.patch.object(m,'HOST_CLI_BUFFER_LIMIT',100),self.assertRaisesRegex(m.GateError,'host_cli_buffer_limit'):
                m.check_host_cli_buffers(life,{})

    def test_context_resolution_respects_precedence_and_does_not_copy_auth(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);name='synthetic-context'
            (root/'config.json').write_text(json.dumps({'currentContext':name,'auths':{'PRIVATE':'PRIVATE-TOKEN'}}))
            metadata=root/'contexts/meta'/m.hashlib.sha256(name.encode()).hexdigest()/'meta.json'
            metadata.parent.mkdir(parents=True)
            metadata.write_text(json.dumps({'Name':name,'Endpoints':{'docker':{'Host':'unix:///synthetic'}}}))
            self.assertEqual(m.readonly_docker_endpoint({},root),'unix:///synthetic')
            self.assertEqual(m.readonly_docker_endpoint({'DOCKER_HOST':'unix:///explicit'},root),'unix:///explicit')
            self.assertEqual(m.readonly_docker_endpoint({'DOCKER_CONTEXT':name,'DOCKER_HOST':'unix:///ignored'},root),'unix:///synthetic')
            metadata.write_text(json.dumps({'Name':'wrong'}))
            with self.assertRaisesRegex(m.GateError,'host_cli_context_identity_mismatch'):m.readonly_docker_endpoint({},root)
            (root/'config.json').write_text('x'*65537)
            with self.assertRaisesRegex(m.GateError,'host_cli_context_file_invalid'):m.readonly_docker_endpoint({},root)

    def test_bounded_output_failure_preserves_diagnostic_and_never_passes_sample(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);process=mock.Mock(pid=1234,returncode=0);process.poll.return_value=0
            def start(*args,**kwargs):kwargs['stdout'].write(b'PRIVATE-SENTINEL'*100);return process
            with mock.patch.object(m.subprocess,'Popen',side_effect=start),self.assertRaisesRegex(m.GateError,'command_output_limit_exceeded'):
                life.owned('container',timeout=4,output_limit=100)
            record=life.command_evidence.snapshot()[0]
            self.assertEqual(record['result'],'failed')
            self.assertTrue(record['record_complete'])
            self.assertNotIn('PRIVATE',json.dumps(record))


class CommandBoundaryEvidenceTests(unittest.TestCase):
    def test_formal_report_exports_cleanup_records_even_on_admission_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            work=Path(directory)/'owned';work.mkdir()
            life=bare_lifecycle(work);target=Path(directory)/'report.json'
            def cleanup():
                record=life.command_evidence.begin(['docker','volume','ls','-q','--filter',
                    f'label={m.LABEL}={life.run_id}'],life.run_id,'cleanup')
                life.command_evidence.finish(record)
                return []
            life.cleanup=mock.Mock(side_effect=cleanup)
            with mock.patch.object(m,'Lifecycle',return_value=life), \
                 mock.patch.object(m.sys,'argv',['runner']), \
                 mock.patch.dict(m.os.environ,{'MONITOR_TRACE_LOCAL_REPORT':str(target)},clear=True), \
                 mock.patch.object(m,'effective_docker_endpoint',return_value='unix:///synthetic'), \
                 mock.patch.object(m,'private_docker_client',side_effect=m.GateError('synthetic_admission_failure')), \
                 mock.patch('builtins.print'):
                self.assertEqual(m.main(),1)
            report=json.loads(target.read_text())
            self.assertEqual(report['owned_list_commands'][0]['phase'],'cleanup')
            self.assertTrue(report['owned_list_commands'][0]['record_complete'])
            self.assertEqual(report['result'],'failed')

    def test_standalone_nginx_cleanup_does_not_require_a_formal_report(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);life.cleanup=mock.Mock(return_value=[])
            life.nginx_route_assertions={'synthetic':True}
            with mock.patch.object(m,'Lifecycle',return_value=life), \
                 mock.patch.object(m,'effective_docker_endpoint',return_value='unix:///synthetic'), \
                 mock.patch.object(m,'private_docker_client'),mock.patch.object(m,'verify_nginx_routes'), \
                 mock.patch('builtins.print'):
                m.nginx_route_contract()
            life.cleanup.assert_called_once()

    def test_owned_lists_are_registered_outside_sampling_and_contain_no_arguments(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory)
            process=mock.Mock(pid=1234,returncode=0);process.poll.return_value=0
            with mock.patch.object(m.subprocess,'Popen',return_value=process):
                life.owned('container',timeout=4)
                life.phase='cleanup';life.owned('volume',timeout=4)
            records=life.command_evidence.snapshot()
            self.assertEqual([r['phase'] for r in records],['synthetic','cleanup'])
            self.assertEqual(len({r['command_id'] for r in records}),2)
            self.assertEqual([r['operation'] for r in records],['list_owned_containers','list_owned_volumes'])
            for r in records:
                self.assertEqual(r['pid'],1234)
                self.assertRegex(r['request_signature_sha256'],r'^[a-f0-9]{64}$')
                points=[r[k] for k in ('entered_monotonic_ns','spawn_started_monotonic_ns',
                    'spawn_finished_monotonic_ns','exit_observed_monotonic_ns','output_finished_monotonic_ns',
                    'decision_monotonic_ns','finished_monotonic_ns')]
                self.assertTrue(all(isinstance(v,int) for v in points))
                self.assertEqual(points,sorted(points))
                self.assertEqual(r['duration_seconds'],round((points[-1]-points[0])/1e9,6))
                self.assertNotIn('args',r);self.assertNotIn('env',r)

    def test_spawn_failure_retains_boundary_and_releases_signature(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory)
            with mock.patch.object(m.subprocess,'Popen',side_effect=OSError('PRIVATE-TOKEN')):
                with self.assertRaises(OSError):life.owned('container',timeout=4)
                with self.assertRaises(OSError):life.owned('container',timeout=4)
            rows=life.command_evidence.snapshot()
            self.assertEqual(len(rows),2)
            self.assertTrue(all(r['result']=='failed' and r['pid'] is None for r in rows))
            self.assertTrue(all(r['spawn_finished_monotonic_ns'] is not None and r['exit_observed_monotonic_ns'] is None for r in rows))
            self.assertNotIn('PRIVATE',json.dumps(rows))

    def test_ledger_rejects_overlap_and_reserves_bounded_cleanup_space(self):
        ledger=m.CommandEvidence(limit=1)
        args=['docker','container','ls','-a','-q','--filter',f'label={m.LABEL}=owned']
        first=ledger.begin(args,'owned','preparation')
        with self.assertRaisesRegex(m.GateError,'owned_list_command_overlap'):
            ledger.begin(args,'owned','sampling')
        ledger.finish(first)
        with self.assertRaisesRegex(m.GateError,'command_evidence_limit'):
            ledger.begin(args,'owned','sampling')
        cleanup=ledger.begin(args,'owned','cleanup');ledger.finish(cleanup)
        self.assertEqual(len(ledger.snapshot()),2)
        for _ in range(7):
            cleanup=ledger.begin(args,'owned','cleanup');ledger.finish(cleanup)
        with self.assertRaisesRegex(m.GateError,'command_evidence_limit'):
            ledger.begin(args,'owned','cleanup')

    def test_unfinished_worker_does_not_hide_failure_report(self):
        ledger=m.CommandEvidence()
        args=['docker','volume','ls','-q','--filter',f'label={m.LABEL}=owned']
        pending=ledger.begin(args,'owned','preparation')
        self.assertFalse(ledger.snapshot()[0]['record_complete'])
        ledger.finish(pending)
        self.assertTrue(ledger.snapshot()[0]['record_complete'])

    def test_signature_separates_runs_and_does_not_accept_broader_queries(self):
        def args(run):return ['docker','volume','ls','-q','--filter',f'label={m.LABEL}={run}']
        a=m.owned_list_identity(args('one'),'one')
        b=m.owned_list_identity(args('two'),'two')
        self.assertNotEqual(a['request_signature_sha256'],b['request_signature_sha256'])
        self.assertIsNone(m.owned_list_identity(args('other'),'owned'))
        self.assertIsNone(m.owned_list_identity(args('owned')+['--filter','name=PRIVATE'],'owned'))

    def test_raw_secrets_and_unowned_queries_never_enter_ledger(self):
        ledger=m.CommandEvidence()
        record=ledger.begin(['docker','inspect','PRIVATE-ID'],'owned','preparation')
        ledger.finish(record)
        record=ledger.begin(['docker','container','ls','-a','-q','--filter','label=PRIVATE-TOKEN'],
                            'owned','preparation')
        ledger.finish(record)
        self.assertEqual(ledger.snapshot(),[])

    def test_clock_interval_uses_bounds_not_symmetric_midpoint(self):
        self.assertEqual(m.apiproxy_clock_interval(100,200,1000,1050),(-900,-850))
        with self.assertRaisesRegex(m.GateError,'clock_event_order_invalid'):
            m.apiproxy_clock_interval(200,100,1000,1050)
        with self.assertRaisesRegex(m.GateError,'clock_interval_empty'):
            m.apiproxy_clock_interval(100,120,1000,1050)

    def test_clock_envelope_rejects_excess_uncertainty_and_wall_jump(self):
        self.assertEqual(m.apiproxy_clock_envelope((10,20),(15,25)),(10,25))
        with self.assertRaisesRegex(m.GateError,'clock_uncertainty_exceeded'):
            m.apiproxy_clock_envelope((0,100),(0,50000001))
        with self.assertRaisesRegex(m.GateError,'clock_wall_jump'):
            m.validate_clock_anchors([(100,1000,101),(200,1000000000,201)])
        with self.assertRaisesRegex(m.GateError,'clock_intervals_inconsistent'):
            m.apiproxy_clock_envelope((0,10),(11,20))

    def test_pairing_refuses_overlap_or_missing_without_request_ids(self):
        def event(direction,t):return {'direction':direction,'timestamp_ns':t,'request_signature_sha256':'a'*64}
        self.assertEqual(len(m.pair_apiproxy_events([event('>>',1),event('<<',2)])),1)
        for events in ([event('>>',1)],[event('<<',1)],
                       [event('>>',1),event('>>',2),event('<<',3),event('<<',4)],
                       [event('>>',1),event('<<',2),event('<<',2)]):
            with self.subTest(events=events),self.assertRaises(m.GateError):m.pair_apiproxy_events(events)

    def test_request_attribution_rejects_ambiguity_missing_and_reuse(self):
        command={'command_id':1,'request_signature_sha256':'a'*64,'entered_monotonic_ns':100,
                 'decision_monotonic_ns':200,'result':'passed'}
        pair={'request_signature_sha256':'a'*64,'started_ns':1000,'finished_ns':1010}
        self.assertEqual(m.attribute_apiproxy_requests([command],[pair],(-900,-850)),{1:0})
        for commands,pairs in (([command],[]),([command],[pair,pair]),([command,{**command,'command_id':2}],[pair]),
                               ([command],[{**pair,'started_ns':5000,'finished_ns':5010}])):
            with self.subTest(commands=commands,pairs=pairs),self.assertRaises(m.GateError):
                m.attribute_apiproxy_requests(commands,pairs,(-900,-850))

    def test_clock_proof_requires_version_bound_event_time_evidence(self):
        with self.assertRaisesRegex(m.GateError,'apiproxy_clock_semantics_unconfirmed'):
            m.require_apiproxy_clock_semantics(None,'4.49.0')
        with self.assertRaisesRegex(m.GateError,'apiproxy_clock_semantics_unconfirmed'):
            m.require_apiproxy_clock_semantics({'version':'4.49.0','source':'log_path_only'},'4.49.0')


class LocalTraceCommandBudgetTests(unittest.TestCase):
    def setUp(self):
        clock = mock.patch.object(m.time, 'monotonic_ns', side_effect=lambda: round(m.time.monotonic()*1e9))
        clock.start()
        self.addCleanup(clock.stop)

    def test_other_threads_do_not_inherit_observer_deadline_or_diagnostics(self):
        with tempfile.TemporaryDirectory() as directory:
            life = bare_lifecycle(directory)
            life.sample_thread_id = -1
            life.sample_deadline = 0
            life.sample_step = {'commands': []}
            process = mock.Mock(pid=654321, returncode=0)
            process.poll.return_value = 0
            with mock.patch.object(m.time, 'monotonic', return_value=1), \
                 mock.patch.object(m.subprocess, 'Popen', return_value=process):
                self.assertEqual(life.command(['synthetic-product-command']), '')
            self.assertEqual(life.sample_step['commands'], [])

    def test_process_creation_counts_towards_command_deadline(self):
        with tempfile.TemporaryDirectory() as directory:
            life = bare_lifecycle(directory)
            clock = [0.0]
            process = mock.Mock(pid=654321, returncode=0)
            process.poll.return_value = 0
            def start(*args, **kwargs): clock[0] = 4.01; return process
            with mock.patch.object(m.time, 'monotonic', side_effect=lambda: clock[0]), \
                 mock.patch.object(m.subprocess, 'Popen', side_effect=start):
                with self.assertRaisesRegex(m.GateError, 'command_deadline_exceeded'):
                    life.command(['synthetic-owned-command'], timeout=4)

    def test_output_processing_cannot_pass_after_deadline(self):
        with tempfile.TemporaryDirectory() as directory, tempfile.TemporaryFile() as output:
            life = bare_lifecycle(directory)
            clock = [0.0]
            process = mock.Mock(pid=654321, returncode=0)
            process.poll.return_value = 0
            wrapped = mock.Mock(wraps=output)
            def read(): clock[0] = 4.01; return output.read()
            wrapped.read.side_effect = read
            context = mock.MagicMock()
            context.__enter__.return_value = wrapped
            with mock.patch.object(m.time, 'monotonic', side_effect=lambda: clock[0]), \
                 mock.patch.object(m.subprocess, 'Popen', return_value=process), \
                 mock.patch.object(m.tempfile, 'TemporaryFile', return_value=context):
                with self.assertRaisesRegex(m.GateError, 'command_deadline_exceeded'):
                    life.command(['synthetic-owned-command'], timeout=4)

    def test_wait_returns_on_child_exit_without_fixed_sleep(self):
        with tempfile.TemporaryDirectory() as directory:
            life = bare_lifecycle(directory)
            clock = [0.0]
            process = mock.Mock(pid=654321, returncode=0)
            process.poll.return_value = None
            def wait(**kwargs):
                self.assertGreater(kwargs['timeout'], 0)
                self.assertLessEqual(kwargs['timeout'], .05)
                clock[0] += .004
                process.poll.return_value = 0
            process.wait.side_effect = wait
            with mock.patch.object(m.time, 'monotonic', side_effect=lambda: clock[0]), \
                 mock.patch.object(m.subprocess, 'Popen', return_value=process), \
                 mock.patch.object(m.time, 'sleep', side_effect=AssertionError('fixed sleep forbidden')):
                self.assertEqual(life.command(['synthetic-owned-command']), '')
            process.wait.assert_called_once()
            self.assertEqual(clock[0], .004)

    def test_guard_failure_interrupts_wait_and_reaps_child(self):
        with tempfile.TemporaryDirectory() as directory:
            life = bare_lifecycle(directory)
            process = mock.Mock(pid=654321, returncode=0)
            process.poll.return_value = None
            calls = []
            def wait(**kwargs):
                calls.append(kwargs['timeout'])
                if len(calls) == 1:
                    life.failure = 'owned_volume_stop_line_reached'
                    raise m.subprocess.TimeoutExpired('private', kwargs['timeout'])
                process.poll.return_value = 0
            process.wait.side_effect = wait
            with mock.patch.object(m.time, 'monotonic', return_value=1), \
                 mock.patch.object(m.subprocess, 'Popen', return_value=process), \
                 mock.patch.object(m.time, 'sleep', side_effect=AssertionError('fixed sleep forbidden')), \
                 mock.patch.object(m.os, 'killpg') as kill:
                with self.assertRaisesRegex(m.GateError, 'owned_volume_stop_line_reached'):
                    life.command(['synthetic-owned-command'])
            self.assertLessEqual(calls[0], .05)
            kill.assert_called_once_with(process.pid, signal.SIGTERM)
            self.assertEqual(calls[1], 1)

    def test_sample_command_timings_never_expose_command_or_environment(self):
        with tempfile.TemporaryDirectory() as directory:
            life = bare_lifecycle(directory)
            life.env = {'PRIVATE-ENV': 'PRIVATE-TOKEN'}
            life.owned = lambda kind, **kw: life.command(['PRIVATE-COMMAND'], guarded=False, **kw).split()
            clock = [0.0]
            process = mock.Mock(pid=654321, returncode=0)
            process.poll.return_value = 0
            def start(*args, **kwargs): clock[0] += .01; return process
            with mock.patch.object(m.time, 'monotonic', side_effect=lambda: clock[0]), \
                 mock.patch.object(m.subprocess, 'Popen', side_effect=start):
                life.sample()
            commands = [command for step in life.samples[-1]['steps'] for command in step['commands']]
            self.assertEqual(len(commands), 2)
            self.assertTrue(all(command['spawn_seconds'] == .01 for command in commands))
            self.assertNotIn('PRIVATE', json.dumps(life.samples))

    def test_timeout_keeps_spawn_wait_and_cleanup_costs_without_success_sample(self):
        with tempfile.TemporaryDirectory() as directory:
            life = bare_lifecycle(directory)
            clock = [0.0]
            process = mock.Mock(pid=654321, returncode=0)
            process.poll.return_value = None
            def start(*args, **kwargs): clock[0] = .01; return process
            def wait(**kwargs):
                if kwargs['timeout'] <= .05:
                    clock[0] = 4
                    raise m.subprocess.TimeoutExpired('PRIVATE-COMMAND', kwargs['timeout'])
                clock[0] += .2
                process.poll.return_value = 0
            process.wait.side_effect = wait
            with mock.patch.object(m.time, 'monotonic', side_effect=lambda: clock[0]), \
                 mock.patch.object(m.subprocess, 'Popen', side_effect=start), \
                 mock.patch.object(m.os, 'killpg') as kill:
                life.monitor()
            failure = life.resource_failure
            self.assertEqual(failure['error_code'], 'resource_sample_deadline_exceeded')
            command = failure['steps'][-1]['commands'][-1]
            self.assertEqual(command['result'], 'failed')
            self.assertEqual(command['spawn_seconds'], .01)
            self.assertEqual(command['wait_seconds'], 3.99)
            self.assertEqual(command['cancel_cleanup_seconds'], .2)
            self.assertEqual(command['entered_monotonic_ns'],0)
            self.assertEqual(command['cancel_started_monotonic_ns'],4_000_000_000)
            self.assertEqual(command['cancel_finished_monotonic_ns'],4_200_000_000)
            self.assertEqual(command['exit_observed_monotonic_ns'],4_200_000_000)
            self.assertEqual(command['decision_monotonic_ns'],4_200_000_000)
            self.assertEqual(command['finished_monotonic_ns'],4_200_000_000)
            self.assertEqual(failure['sample_duration_seconds'], 4.2)
            self.assertEqual(failure['steps'][-1]['processing_seconds'], 0)
            self.assertNotIn('PRIVATE', json.dumps(failure))
            self.assertEqual(life.samples, [])
            kill.assert_called_once_with(process.pid, signal.SIGTERM)


class LocalTraceCleanupTests(unittest.TestCase):
    def setUp(self):
        clock = mock.patch.object(m.time, 'monotonic_ns', side_effect=lambda: round(m.time.monotonic()*1e9))
        clock.start()
        self.addCleanup(clock.stop)

    def test_sampler_mount_is_released_before_builder_state_volume_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            work=Path(directory)/'owned';work.mkdir()
            life=bare_lifecycle(work)
            alive=True
            def docker(*args,**kwargs):
                nonlocal alive
                if args[0]=='ps':
                    return 'owned-sampler' if alive and 'name=^'+life.run_id+'-space$' in args else ''
                if args[:2]==('rm','-fv'):
                    self.assertEqual(args[2:],('owned-sampler',));alive=False;return ''
                if args[:2]==('buildx','rm'):
                    self.assertFalse(alive,'sampler still pins builder state volume');return ''
                if args[0] in ('container','network','volume') and args[1]=='ls':return ''
                raise AssertionError(args)
            life.docker=docker
            self.assertEqual(life.cleanup(),[])
            self.assertFalse(alive)

    def test_removal_is_filtered_and_zero_is_verified(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory) / 'private'; work.mkdir()
            life = bare_lifecycle(work)
            resources = {'container': ['owned-container'], 'network': ['owned-network'], 'volume': ['owned-volume']}
            calls = []
            def docker(*args, **kwargs):
                calls.append((args, kwargs))
                if len(args) > 1 and args[1] == 'ls' and args[0] in resources:
                    self.assertIn('label=' + m.LABEL + '=' + life.run_id, args)
                    return '\n'.join(resources[args[0]])
                if args[0] in resources and args[1] == 'rm':
                    self.assertTrue(set(args[2:]) - {'-fv'} <= set(resources[args[0]]))
                    resources[args[0]] = []
                    return ''
                if args[:2] == ('buildx', 'rm') or args[0] == 'ps':
                    return ''
                raise AssertionError(args)
            life.docker = docker
            self.assertEqual(life.cleanup(), [])
            self.assertFalse(work.exists())
            self.assertTrue(all(not values for values in resources.values()))
            self.assertTrue(all(not kwargs.get('guarded', True) for _, kwargs in calls))

    def test_cleanup_failure_does_not_prevent_remaining_owned_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory) / 'private'; work.mkdir()
            life = bare_lifecycle(work)
            life.builder_ids = {'owned-builder'}
            life.builder_created = True
            resources = {'container': ['owned-container'], 'network': ['owned-network'], 'volume': ['owned-volume']}
            removed = []
            def docker(*args, **kwargs):
                if args[0] == 'ps':
                    return ''
                if args[:2] == ('buildx', 'rm'):
                    raise m.GateError('synthetic_builder_refusal')
                if args[0] in resources and args[1] == 'ls':
                    return '\n'.join(resources[args[0]])
                if args[0] in resources and args[1] == 'rm':
                    if args[0] == 'container':
                        raise m.GateError('synthetic_owned_removal_refusal')
                    resources[args[0]] = []
                    removed.append(args[0])
                    return ''
                raise AssertionError(args)
            life.docker = docker
            errors = life.cleanup()
            self.assertIn('builder', errors)
            self.assertIn('container', errors)
            self.assertEqual(removed, ['network', 'volume'])
            self.assertFalse(work.exists())

    def test_failed_command_exposes_only_exit_hash_and_fixed_diagnostic_markers(self):
        with tempfile.TemporaryDirectory() as directory:
            life = bare_lifecycle(directory)
            process = mock.Mock(pid=654321, returncode=17)
            process.poll.return_value = 17
            def start(*args, **kwargs):
                kwargs['stdout'].write(b'permission denied password=PRIVATE-SENTINEL token=PRIVATE-TOKEN')
                return process
            with mock.patch.object(m.time, 'monotonic', return_value=1), mock.patch.object(m.subprocess, 'Popen', side_effect=start):
                with self.assertRaisesRegex(m.GateError, '^command_failed_17$'):
                    life.command(['synthetic-private-command'])
            self.assertEqual(set(life.last_failure), {'exit_code', 'output_sha256','diagnostic_markers'})
            self.assertEqual(life.last_failure['exit_code'], 17)
            self.assertEqual(life.last_failure['diagnostic_markers'],['permission denied'])
            self.assertRegex(life.last_failure['output_sha256'], r'^[a-f0-9]{64}$')
            self.assertNotIn('PRIVATE', repr(life.last_failure))

    def test_build_failure_classification_excludes_arbitrary_output(self):
        cases = [
            (b'failed to compute cache key: failed to calculate checksum: PRIVATE-PATH not found', ['not found', 'failed to compute cache key', 'failed to calculate checksum']),
            (b'failed to solve: content digest sha256:PRIVATE-TOKEN: not found', ['failed to solve', 'not found', 'content digest']),
            (b'failed to solve: failed to fetch anonymous token: unexpected status from HEAD request: PRIVATE-TOKEN 404 Not Found', ['failed to solve', 'not found', 'failed to fetch anonymous token', 'unexpected status from head request']),
        ]
        for raw, expected in cases:
            with self.subTest(raw=raw):
                markers = m.diagnostic_markers(raw)
                self.assertEqual(markers, expected)
                self.assertNotIn('PRIVATE', repr(markers))

    def test_public_build_diagnostic_distinguishes_missing_input_from_content(self):
        missing = m.public_build_failure_diagnostic(b'#7 ERROR: failed to calculate checksum: "/dist/release-linux-arm64/gateway": not found PRIVATE-TOKEN\n > [2/3] COPY dist/release-linux-arm64/gateway ./server:\nDockerfile.prebuilt:14')
        self.assertEqual(missing['reasons'], ['context_input_missing'])
        self.assertEqual(missing['steps'], ['copy'])
        self.assertEqual(missing['inputs'], ['dist/release-linux-arm64/gateway'])
        self.assertEqual(missing['dockerfile_lines'], [14])
        content = m.public_build_failure_diagnostic(('ERROR: failed to solve: content digest sha256:' + 'a' * 64 + ': not found PRIVATE-TOKEN\n > exporting to image:').encode())
        self.assertEqual(content['reasons'], ['content_digest_missing'])
        self.assertEqual(content['steps'], ['export'])
        self.assertNotIn('PRIVATE', repr(missing) + repr(content))
        self.assertNotIn('a' * 64, repr(content))

    def test_public_build_diagnostic_retains_bounded_codes_without_bodies(self):
        value = m.public_build_failure_diagnostic(b'ERROR: unexpected status from HEAD request: https://PRIVATE-TOKEN/repo: 404 Not Found\n > [internal] load metadata for localhost:5001/debian-slim:bookworm:\nDockerfile.prebuilt:1\nSQL=PRIVATE-SQL password=PRIVATE-PASSWORD')
        self.assertEqual(value['reasons'], ['registry_http_status'])
        self.assertEqual(value['steps'], ['base_metadata'])
        self.assertEqual(value['http_statuses'], [404])
        self.assertNotIn('PRIVATE', repr(value))
        self.assertNotIn('https', repr(value))
        unknown = m.public_build_failure_diagnostic(b'ERROR: PRIVATE-TOKEN /private/password SQL=SECRET')
        self.assertEqual(unknown, {'reasons':['unclassified'], 'steps':[], 'inputs':[], 'dockerfile_lines':[], 'http_statuses':[], 'process_exit_codes':[]})

    def test_failed_product_image_command_adds_only_structured_public_diagnostic(self):
        with tempfile.TemporaryDirectory() as directory:
            life = bare_lifecycle(directory); life.phase = 'product_image_gateway'
            process = mock.Mock(pid=654321, returncode=17); process.poll.return_value = 17
            def start(*args, **kwargs):
                kwargs['stdout'].write(b'ERROR: content digest sha256:' + b'a' * 64 + b': not found PRIVATE-TOKEN')
                return process
            with mock.patch.object(m.time, 'monotonic', return_value=1), mock.patch.object(m.subprocess, 'Popen', side_effect=start):
                with self.assertRaisesRegex(m.GateError, '^command_failed_17$'):
                    life.command(['make', 'build-images'])
            self.assertEqual(life.last_failure['public_build']['reasons'], ['content_digest_missing'])
            self.assertNotIn('PRIVATE', repr(life.last_failure))

    def test_public_build_failure_diagnostics_remain_fixed_markers(self):
        markers = m.diagnostic_markers(b'ERROR: failed to solve: proxyconnect tcp PRIVATE-TOKEN: connection refused')
        self.assertEqual(markers, ['failed to solve', 'connection refused', 'proxyconnect'])
        self.assertNotIn('PRIVATE', repr(markers))

    def test_invalid_report_admission_removes_private_work_before_any_docker_call(self):
        with tempfile.TemporaryDirectory() as directory:
            root = (Path(directory) / 'repository').resolve(); root.mkdir()
            work = Path(directory) / 'private'; work.mkdir()
            life = bare_lifecycle(work)
            life.docker = mock.Mock(side_effect=AssertionError('Docker must not run'))
            with mock.patch.object(m, 'ROOT', root), mock.patch.object(m, 'Lifecycle', return_value=life), \
                 mock.patch.dict(m.os.environ, {'MONITOR_TRACE_LOCAL_REPORT': str(root / 'forbidden.json')}, clear=True), \
                 mock.patch.object(m.sys, 'argv', ['monitor-traces-local-gate.py']):
                with self.assertRaises(m.GateError):
                    m.main()
            life.docker.assert_not_called()
            self.assertFalse(work.exists())
            self.assertFalse((root / 'forbidden.json').exists())

    def test_unstopped_guard_is_a_cleanup_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory) / 'private'; work.mkdir()
            life = bare_lifecycle(work)
            life.thread = mock.Mock()
            life.thread.is_alive.return_value = True
            life.docker = mock.Mock(return_value='')
            self.assertIn('resource_guard_not_stopped', life.cleanup())
            life.thread.join.assert_called_once()

    def test_command_interruption_reaps_its_process_group(self):
        with tempfile.TemporaryDirectory() as directory:
            life = bare_lifecycle(directory)
            process = mock.Mock(pid=654321)
            process.poll.return_value = None
            process.wait.side_effect = [KeyboardInterrupt, m.subprocess.TimeoutExpired('owned', 1), 0]
            with mock.patch.object(m.time, 'monotonic', return_value=1), \
                 mock.patch.object(m.subprocess, 'Popen', return_value=process), mock.patch.object(m.os, 'killpg') as kill:
                with self.assertRaises(KeyboardInterrupt):
                    life.command(['synthetic-owned-command'])
            self.assertEqual(kill.call_args_list, [mock.call(process.pid, signal.SIGTERM), mock.call(process.pid, signal.SIGKILL)])
            self.assertEqual(process.wait.call_count, 3)


if __name__ == '__main__':
    unittest.main()


class LocalTraceCommittedSourceTests(unittest.TestCase):
    def test_private_input_drift_fails_before_clone_or_build(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);(root/'owned.py').write_text('committed source')
            life=mock.Mock();life.command.return_value='committed source'
            with mock.patch.object(m,'ROOT',root),mock.patch.object(m,'OWN_INPUTS',('owned.py',)):
                result=m.committed_inputs(life,'fixed-commit')
                self.assertRegex(result['owned.py'],r'^[0-9a-f]{64}$')
                (root/'owned.py').write_text('uncommitted source')
                with self.assertRaisesRegex(m.GateError,'commit_all_local_inputs'):m.committed_inputs(life,'fixed-commit')
            self.assertTrue(all(call.args[0][0:2]==['git','show'] for call in life.command.call_args_list))
    def test_shared_wip_is_not_built_and_missing_committed_integration_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);(root/'Makefile').write_text('other session WIP')
            life=mock.Mock();life.command.return_value='test-monitor-traces-local: committed-entry'
            with mock.patch.object(m,'ROOT',root),mock.patch.object(m,'OWN_INPUTS',('Makefile',)):
                self.assertIn('Makefile',m.committed_inputs(life,'fixed-commit'))
                life.command.return_value='old unrelated entry'
                with self.assertRaisesRegex(m.GateError,'committed_local_integration_missing'):m.committed_inputs(life,'fixed-commit')


class LocalTracePrivateFilesTests(unittest.TestCase):
    def test_private_json_is_exclusive_and_owner_only(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory)
            path=life.private_json('input.json',{'safe':'value'})
            self.assertEqual(path.stat().st_mode&0o777,0o600)
            self.assertEqual(path.stat().st_nlink,1)
            self.assertEqual(json.loads(path.read_text()),{'safe':'value'})
            with self.assertRaises(FileExistsError):life.private_json('input.json',{})
    def test_public_docker_client_never_loads_personal_credential_helpers(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);life.env={'DOCKER_CONTEXT':'personal','HOME':'/personal'}
            path=m.private_docker_client(life,'unix:///local/socket')
            self.assertEqual(path.stat().st_mode&0o777,0o700)
            config=json.loads((path/'config.json').read_text())
            self.assertEqual(config['auths'],{})
            self.assertNotIn('credsStore',config);self.assertNotIn('credHelpers',config)
            self.assertEqual((path/'config.json').stat().st_mode&0o777,0o600)
            self.assertEqual(life.env['DOCKER_HOST'],'unix:///local/socket')
            self.assertEqual(life.env['DOCKER_CONFIG'],str(path))
            self.assertNotIn('DOCKER_CONTEXT',life.env)


class LocalTraceStartupAndWritableBudgetTests(unittest.TestCase):
    def test_init_commands_finish_successfully_before_long_running_consumers(self):
        calls=[]
        def compose(*args,**kwargs):
            if args[0]=='exec':
                self.assertEqual(kwargs,{'data':'standard-initialization-sql'})
                self.assertIn('ON_ERROR_STOP=1',args[-1])
                self.assertIn('$POSTGRES_DB',args[-1])
            if args[0]=='up':
                self.assertIn('--wait',args)
                self.assertFalse({'runtime-log-store-init','trace-store-init'}.intersection(args))
            calls.append(args)
        m.start_dependencies(compose,'standard-initialization-sql')
        m.start_deployment(compose)
        self.assertEqual(calls[2:4],[('run','--no-deps','runtime-log-store-init'),('run','--no-deps','trace-store-init')])
        self.assertTrue(all(name in calls[0] for name in ('postgres','business-postgres','redis','minio')))
        self.assertEqual(calls[1][:3],('exec','-T','postgres'))
        self.assertIn('alloy',calls[4])
        self.assertIn('system-backend',calls[5])
        self.assertFalse(any(set(call).intersection(m.TRACE+('postgres','business-postgres','redis')) for call in calls[5:]))
    def test_database_initialization_failure_stops_all_product_startup(self):
        calls=[]
        def compose(*args,**kwargs):
            calls.append(args)
            if args[0]=='exec':raise m.GateError('initialization_rejected')
        with self.assertRaisesRegex(m.GateError,'initialization_rejected'):
            m.start_dependencies(compose,'standard-initialization-sql')
        self.assertEqual(len(calls),2)

    def test_private_writable_directories_are_part_of_the_same_stop_line(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory)
            life.owned=lambda kind,**kw: []
            life.owned_bind_bytes=lambda **kw: m.VOLUME_STOP
            with self.assertRaisesRegex(m.GateError,'owned_volume_stop_line_reached'):life.sample()
            life.command=mock.Mock(return_value='2048 /private/work\n')
            self.assertEqual(m.Lifecycle.owned_bind_bytes(life,timeout=1),2*1024**2)
            self.assertEqual(life.command.call_args.args[0],['du','-sk',str(life.work)])
