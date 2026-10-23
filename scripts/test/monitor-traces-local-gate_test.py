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
    life.minio_image = 'sha256:synthetic-pinned-minio'
    life.owned_bind_bytes = lambda **kwargs: 0
    return life


class LocalTraceAdmissionTests(unittest.TestCase):
    def test_public_build_diagnostics_keep_only_fixed_failure_categories(self):
        self.assertEqual(m.public_build_failure(b'ERROR: failed to authorize: unexpected status 429 Too Many Requests'),
                         ['registry_rate_limited', 'registry_authorization_failed'])
        self.assertEqual(m.public_build_failure(b'fatal: unable to access https://credential@example.invalid/private'),
                         ['git_transport_failed'])
        self.assertEqual(m.public_build_failure(b'password=private secret=private SQL=private'),
                         ['public_build_failure_unclassified'])

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

    def test_long_running_product_exit_zero_is_still_a_stop_condition(self):
        for name in ('system-backend', 'gateway', 'tempo', 'alloy'):
            with self.subTest(service=name), self.assertRaises(m.GateError):
                m.validate_resources([row(status='exited', service=name)])


class LocalTraceIsolationTests(unittest.TestCase):
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

    def test_actual_volume_stop_line_rejects_equal_threshold(self):
        with tempfile.TemporaryDirectory() as directory:
            for allocated in (m.VOLUME_STOP, m.VOLUME_STOP + 4096):
                life, _ = self.sample_lifecycle(directory, allocated)
                with mock.patch.object(m.time, 'monotonic', return_value=1), self.assertRaises(m.GateError):
                    life.sample()
                self.assertEqual(life.samples[-1]['volume_allocated_bytes'],allocated)
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
        self.assertLessEqual(3+3+m.SAMPLER_CPU,8)

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


class LocalTraceCleanupTests(unittest.TestCase):
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

    def test_failed_command_exposes_only_exit_and_output_hash(self):
        with tempfile.TemporaryDirectory() as directory:
            life = bare_lifecycle(directory)
            process = mock.Mock(pid=654321, returncode=17)
            process.poll.return_value = 17
            def start(*args, **kwargs):
                kwargs['stdout'].write(b'password=PRIVATE-SENTINEL token=PRIVATE-TOKEN')
                return process
            with mock.patch.object(m.time, 'monotonic', return_value=1), mock.patch.object(m.subprocess, 'Popen', side_effect=start):
                with self.assertRaisesRegex(m.GateError, '^command_failed_17$'):
                    life.command(['synthetic-private-command'])
            self.assertEqual(set(life.last_failure), {'exit_code', 'output_sha256'})
            self.assertEqual(life.last_failure['exit_code'], 17)
            self.assertRegex(life.last_failure['output_sha256'], r'^[a-f0-9]{64}$')
            self.assertNotIn('PRIVATE', repr(life.last_failure))

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
            process.wait.return_value = 0
            with mock.patch.object(m.time, 'monotonic', return_value=1), mock.patch.object(m.time, 'sleep', side_effect=KeyboardInterrupt), \
                 mock.patch.object(m.subprocess, 'Popen', return_value=process), mock.patch.object(m.os, 'killpg') as kill:
                with self.assertRaises(KeyboardInterrupt):
                    life.command(['synthetic-owned-command'])
            kill.assert_called()
            self.assertEqual(kill.call_args.args[0], process.pid)
            self.assertIn(kill.call_args.args[1], (signal.SIGTERM, signal.SIGKILL))
            process.wait.assert_called()


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
        def compose(*args):
            if args[0]=='up':
                self.assertIn('--wait',args)
                self.assertFalse({'runtime-log-store-init','trace-store-init'}.intersection(args))
            calls.append(args)
        m.start_deployment(compose)
        self.assertEqual(calls[1:3],[('run','--rm','--no-deps','runtime-log-store-init'),('run','--rm','--no-deps','trace-store-init')])
        self.assertTrue(all(name in calls[0] for name in ('postgres','business-postgres','redis','minio')))
        self.assertIn('alloy',calls[3])
        self.assertIn('system-backend',calls[4])
    def test_private_writable_directories_are_part_of_the_same_stop_line(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory)
            life.owned=lambda kind,**kw: []
            life.owned_bind_bytes=lambda **kw: m.VOLUME_STOP
            with self.assertRaisesRegex(m.GateError,'owned_volume_stop_line_reached'):life.sample()
            life.command=mock.Mock(return_value='2048 /private/work\n')
            self.assertEqual(m.Lifecycle.owned_bind_bytes(life,timeout=1),2*1024**2)
            self.assertEqual(life.command.call_args.args[0],['du','-sk',str(life.work)])
