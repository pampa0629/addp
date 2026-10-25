"""Shared Infra/Business discovery and admission tests; no real Docker daemon."""
import copy
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import tempfile
import os
import sys
import time
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location('deployments', ROOT / 'scripts/utils/deployment-inventory.py')
inventory = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(inventory)
CID = 'a' * 64


class Source:
    def __init__(self):
        self.info = {'id': 'daemon-identity', 'os_type': 'linux', 'operating_system': 'Docker Desktop'}
        self.endpoint = 'unix:///Users/example/.docker/run/docker.sock'
        self.container = {'id': CID, 'running': True, 'status': 'running',
                          'started_at': '2026-10-18T12:30:00.123456789Z', 'restart_count': 0,
                          'project': 'business', 'service': 'postgres',
                          'working_dir': str(ROOT / 'business'), 'oneoff': 'False',
                          'ports': {'5432/tcp': [{'HostIp': '127.0.0.1', 'HostPort': '15433'}]}}
        self.ids, self.commands, self.after = CID, [], None

    def __call__(self, *args):
        self.commands.append(args)
        if args == ('context', 'show'):
            return 'desktop-linux'
        if args[:2] == ('context', 'inspect'):
            return json.dumps(self.endpoint)
        assert args[:2] == ('--context', 'desktop-linux'), args
        if args[2] == 'info':
            return json.dumps(self.info)
        if args[2] == 'ps':
            if 'label=com.docker.compose.project=addp-postgres-metrics-' in ' '.join(args):
                return ''
            return self.ids
        if args[2] == 'inspect':
            value = copy.deepcopy(self.container)
            if self.after:
                self.after(self)
            return json.dumps(value)
        raise AssertionError('Unexpected/mutating Docker command: ' + args[2])


class MetricsSource(Source):
    def __init__(self):
        super().__init__()
        self.metrics = {}
        for name, cid in [('postgres-metrics', 'b'*64), ('postgres-metrics-web', 'c'*64)]:
            self.metrics[cid] = dict(self.container, id=cid, service=name,
                project='addp-postgres-metrics-business', owner=str(ROOT), scope='business',
                database_id=CID, database_started_at=self.container['started_at'],
                network_mode='container:'+'b'*64 if name.endswith('-web') else 'fixture-database',
                ports={} if name.endswith('-web') else {'9399/tcp': None, '9443/tcp': [{'HostIp': '127.0.0.1', 'HostPort': '19443'}]})

    def __call__(self, *args):
        if len(args) > 2 and args[2] == 'ps' and 'addp-postgres-metrics-business' in ' '.join(args):
            self.commands.append(args)
            return '\n'.join(self.metrics)
        if len(args) > 2 and args[2] == 'inspect' and args[-1] in self.metrics:
            self.commands.append(args)
            return json.dumps(self.metrics[args[-1]])
        return super().__call__(*args)


class DockerReaderTests(unittest.TestCase):
    def test_refresh_failure_never_echoes_raw_source_or_partial_output(self):
        with patch.object(inventory.sys, 'argv', ['deployment-inventory.py', 'refresh', '--scope', 'infra']), \
                patch.object(inventory, 'publish', side_effect=ValueError('credential-sentinel')), \
                patch.object(inventory.sys, 'stdout', new_callable=io.StringIO) as out, \
                patch.object(inventory.sys, 'stderr', new_callable=io.StringIO) as err:
            self.assertEqual(inventory.main(), 1)
        self.assertEqual(out.getvalue(), '')
        self.assertNotIn('credential-sentinel', err.getvalue())

    def test_docker_failures_timeout_and_size_are_bounded_and_redacted(self):
        for failure in (subprocess.CalledProcessError(1, 'docker', output=b'password-secret'),
                        subprocess.TimeoutExpired('docker', 10), FileNotFoundError()):
            with patch.object(inventory.subprocess, 'Popen', side_effect=failure):
                with self.assertRaises(ValueError) as error:
                    inventory.DockerReader()('info')
                self.assertNotIn('password-secret', str(error.exception))
        with patch.object(inventory.subprocess, 'Popen') as popen:
            process=popen.return_value.__enter__.return_value
            process.returncode=0
            process.communicate.return_value=(b'x'*(64*1024+1),None)
            with self.assertRaises(ValueError):
                inventory.DockerReader()('info')

    def test_docker_communication_keeps_original_deadline_and_reaps_on_timeout(self):
        with patch.object(inventory.subprocess, 'Popen') as popen, \
                patch.object(inventory.time,'monotonic',side_effect=range(0,40,4)):
            process=popen.return_value.__enter__.return_value
            process.communicate.side_effect=subprocess.TimeoutExpired('docker',.05)
            process.poll.return_value=None
            with self.assertRaises(ValueError):
                inventory.DockerReader()('info')
        popen.assert_called_once()
        process.kill.assert_called_once()
        process.wait.assert_called_once()
        self.assertLessEqual(process.communicate.call_count,3)
        self.assertTrue(all(call.kwargs['timeout']<=.05 for call in process.communicate.call_args_list))



class DiscoveryTests(unittest.TestCase):
    def source(self, scope='business'):
        source = Source()
        source.container['project'], root = inventory.SCOPES[scope]
        source.container['working_dir'] = str(root)
        return source

    def discover(self, source=None, scope='business'):
        return inventory.discover(scope, env={}, reader=source or self.source(scope), system='Darwin')

    def test_metrics_are_bound_to_exact_database_without_secret_reads(self):
        source = MetricsSource()
        value = self.discover(source)
        metrics = value['postgresql_metrics']
        self.assertEqual(metrics, {'state': 'available', 'endpoint': 'https://127.0.0.1:19443/metrics',
                                  'container_id': CID, 'started_at': source.container['started_at']})
        self.assertNotIn('.Config.Env', inventory.METRICS_CONTAINER)
        self.assertNotIn('.Mounts', inventory.METRICS_CONTAINER)
        self.assertEqual(self.discover()['postgresql_metrics'], {'state': 'not_deployed'})

    def test_invalid_optional_metrics_preserve_core_discovery(self):
        for key, value in [('owner', '/foreign'), ('scope', 'infra'), ('database_id', 'd'*64),
                           ('database_started_at', '2026-10-18T00:00:00Z'), ('running', False),
                           ('network_mode', 'container:'+'d'*64)]:
            source = MetricsSource()
            source.metrics['c'*64][key] = value
            result = self.discover(source)
            self.assertEqual(result['state'], 'available')
            self.assertEqual(len(result['services']), 1)
            self.assertEqual(result['postgresql_metrics'], {'state': 'unavailable'}, key)
        for address in ['0.0.0.0', '8.8.8.8', 'fe80::1', '::ffff:127.0.0.1']:
            source = MetricsSource()
            source.metrics['b'*64]['ports']['9443/tcp'][0]['HostIp'] = address
            self.assertEqual(self.discover(source)['postgresql_metrics'], {'state': 'unavailable'})
        source = MetricsSource()
        del source.metrics['c'*64]
        self.assertEqual(self.discover(source)['postgresql_metrics'], {'state': 'unavailable'})

    def test_both_scopes_use_actual_runtime_and_project(self):
        for scope in inventory.SCOPES:
            with self.subTest(scope=scope):
                source = self.source(scope)
                value = self.discover(source, scope)
                self.assertEqual(value['schema'], 'addp.deployment-discovery/v2')
                self.assertEqual(value['scope'], scope)
                self.assertEqual(value['state'], 'available')
                self.assertEqual(value['source']['project'], inventory.SCOPES[scope][0])
                self.assertEqual(value['source']['runtime'], 'desktop_linux_vm')
                self.assertEqual(value['services'][0]['engine_type'], 'postgresql')
                self.assertEqual(value['services'][0]['containers'][0]['published_ports'][0]['port'], 15433)
                encoded = json.dumps(value)
                for forbidden in ('password', 'working_dir', '"tenant_id"', '"node_id"', '"engine_id"', str(ROOT)):
                    self.assertNotIn(forbidden, encoded)
                self.assertEqual({args[2] for args in source.commands if args[0] == '--context'}, {'info', 'ps', 'inspect'})
                self.assertNotIn('.Config.Env', inventory.CONTAINER)

    def test_native_linux_ipv6_and_unsupported_runtime(self):
        source = self.source('infra')
        source.info['operating_system'] = 'Ubuntu 24.04 LTS'
        source.container['ports'] = {'5432/tcp': [{'HostIp': '::1', 'HostPort': '25432'}]}
        value = inventory.discover('infra', env={}, reader=source, system='Linux')
        self.assertEqual(value['source']['runtime'], 'linux_docker')
        self.assertEqual(value['services'][0]['containers'][0]['published_ports'][0]['address'], '::1')
        for system in ('Windows', 'Linux'):
            with self.subTest(system=system), self.assertRaises(ValueError):
                inventory.discover('infra', env={}, reader=self.source('infra'), system=system)

    def test_successful_empty_discovery_is_not_failure(self):
        source = self.source()
        source.ids = ''
        value = self.discover(source)
        self.assertEqual(value['state'], 'available')
        self.assertEqual(value['services'], [])

    def test_stopped_or_created_services_remain_source_facts(self):
        for state in ('exited', 'created', 'dead'):
            source = self.source()
            source.container.update(status=state, running=False, started_at='0001-01-01T00:00:00Z', ports={})
            container = self.discover(source)['services'][0]['containers'][0]
            self.assertEqual(container['status'], state)
            self.assertEqual(container['started_at'], '')
            self.assertEqual(container['published_ports'], [])

    def test_unknown_service_is_unclassified_not_guessed_from_container(self):
        source = self.source()
        source.container['service'] = 'unadapted-component'
        value = self.discover(source)['services'][0]
        self.assertEqual(value['engine_type'], 'unclassified')
        self.assertEqual(value['service'], 'unadapted-component')

    def test_wrong_scope_foreign_workdir_and_oneoff_reject_entire_snapshot(self):
        for key, value in (('project', 'addp-infra'), ('working_dir', '/different/workspace'), ('oneoff', 'True')):
            source = self.source()
            source.container[key] = value
            with self.subTest(key=key), self.assertRaises(ValueError):
                self.discover(source)

    def test_source_changes_reject_without_partial_snapshot(self):
        source = self.source()
        source.after = lambda s: s.container.update(restart_count=1)
        with self.assertRaises(ValueError):
            self.discover(source)

    def test_invalid_scope_and_remote_docker_never_discover(self):
        source = self.source()
        with self.assertRaises(ValueError):
            self.discover(source, 'outside')
        self.assertEqual(source.commands, [])
        source.endpoint = 'ssh://different-host'
        with self.assertRaises(ValueError):
            self.discover(source)
        self.assertEqual(len(source.commands), 2)

    def test_environment_overrides_fail_before_docker(self):
        for key in ('DOCKER_HOST', 'DOCKER_TLS_VERIFY', 'DOCKER_CERT_PATH'):
            source = self.source()
            with self.subTest(key=key), self.assertRaises(ValueError):
                inventory.discover('infra', env={key: 'credential-sentinel'}, reader=source)
            self.assertEqual(source.commands, [])

    def test_runtime_and_id_bounds(self):
        for ids in ('not-an-id', CID+'\n'+CID, '\n'.join(f'{i:064x}' for i in range(129))):
            source = self.source()
            source.ids = ids
            with self.subTest(ids=ids[:10]), self.assertRaises(ValueError):
                self.discover(source)
        source = self.source()
        source.container['started_at'] = 'credential-sentinel'
        with self.assertRaises(ValueError):
            self.discover(source)

    def test_all_published_ports_are_projected_without_protocol_guessing(self):
        source = self.source()
        source.container['ports'].update({'9000/tcp': None, '9001/udp': [{'HostIp': '::1', 'HostPort': '19001'}]})
        value = self.discover(source)['services'][0]['containers'][0]['published_ports']
        self.assertEqual(len(value), 2)
        self.assertEqual(value[1], {'address': '::1', 'port': 19001, 'container_port': 9001, 'protocol': 'udp'})

    def test_multiple_services_and_replicas_are_sorted_without_collapsing_instances(self):
        class MultipleSource(Source):
            def __call__(self, *args):
                if len(args) > 2 and args[2] == 'inspect':
                    value = copy.deepcopy(self.container)
                    value['id'] = args[-1]
                    if args[-1] == 'c'*64:
                        value['service'] = 'redis'
                    return json.dumps(value)
                return super().__call__(*args)
        source = MultipleSource()
        source.ids = '\n'.join(letter*64 for letter in ('c', 'b', 'a'))
        values = self.discover(source)['services']
        self.assertEqual([s['service'] for s in values], ['postgres', 'redis'])
        self.assertEqual([c['container_id'] for c in values[0]['containers']], ['a'*64, 'b'*64])
        self.assertEqual(len(values[1]['containers']), 1)

    def test_publication_is_private_and_scope_isolation_is_preserved(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)/'discovery'
            for scope in inventory.SCOPES:
                inventory.publish(scope, directory, env={}, reader=self.source(scope), system='Darwin')
            first = (directory/'infra.json').read_bytes()
            value = self.source()
            value.container['restart_count'] = 8
            inventory.publish('business', directory, env={}, reader=value, system='Darwin')
            self.assertEqual((directory/'infra.json').read_bytes(), first)
            self.assertEqual(json.loads((directory/'business.json').read_text())['services'][0]['containers'][0]['restart_count'], 8)
            self.assertEqual(directory.stat().st_mode & 0o777, 0o700)
            self.assertEqual((directory/'business.json').stat().st_mode & 0o777, 0o600)
            self.assertEqual(list(directory.glob('.*')), [])

    def test_failed_refresh_replaces_old_success_with_redacted_failure(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)/'discovery'
            inventory.publish('business', directory, env={}, reader=self.source(), system='Darwin')
            source = self.source()
            source.container['ports']['bad-credential-sentinel'] = []
            with self.assertRaises(ValueError):
                inventory.publish('business', directory, env={}, reader=source, system='Darwin')
            value = json.loads((directory/'business.json').read_text())
            self.assertEqual(value['state'], 'unavailable')
            self.assertEqual(value['services'], [])
            self.assertNotIn('source', value)
            self.assertNotIn('credential-sentinel', json.dumps(value))

    def test_atomic_replacement_failure_preserves_previous_file_and_removes_temporary(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)/'discovery'
            inventory.publish('business', directory, env={}, reader=self.source(), system='Darwin')
            previous = (directory/'business.json').read_bytes()
            with patch.object(inventory.os, 'replace', side_effect=OSError('simulated IO failure')):
                with self.assertRaises(OSError):
                    inventory.publish('business', directory, env={}, reader=self.source(), system='Darwin')
            self.assertEqual((directory/'business.json').read_bytes(), previous)
            self.assertEqual(list(directory.glob('.*')), [])

    def test_symlinks_shared_permissions_and_nonregular_lock_are_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)/'discovery'
            directory.mkdir(mode=0o700)
            alias = Path(tmp)/'alias'
            alias.symlink_to(directory)
            with self.assertRaises(ValueError):
                inventory.publish('business', alias, env={}, reader=self.source(), system='Darwin')
            directory.chmod(0o755)
            with self.assertRaises(ValueError):
                inventory.publish('business', directory, env={}, reader=self.source(), system='Darwin')
            directory.chmod(0o700)
            victim = Path(tmp)/'victim'
            victim.write_text('unchanged')
            (directory/'business.json').symlink_to(victim)
            with self.assertRaises(ValueError):
                inventory.publish('business', directory, env={}, reader=self.source(), system='Darwin')
            self.assertEqual(victim.read_text(), 'unchanged')
            (directory/'business.json').unlink()
            (directory/'business.lock').unlink()
            os.mkfifo(directory/'business.lock', 0o600)
            with self.assertRaises(ValueError):
                inventory.publish('business', directory, env={}, reader=self.source(), system='Darwin')

    def test_cli_refresh_has_no_source_output(self):
        with patch.object(inventory.sys, 'argv', ['deployment-inventory.py', 'refresh', '--scope', 'infra']), \
                patch.object(inventory, 'publish', return_value={'private': 'not-for-stdout'}) as publish, \
                patch.object(inventory.sys, 'stdout', new_callable=io.StringIO) as output:
            self.assertEqual(inventory.main(), 0)
            self.assertEqual(publish.call_args.args[0], 'infra')
            self.assertEqual(output.getvalue(), '')

    def test_empty_directory_environment_uses_private_workspace_default(self):
        with patch.dict(os.environ, {'ADDP_DEPLOYMENT_DISCOVERY_DIR': ''}), \
                patch.object(inventory.sys, 'argv', ['deployment-inventory.py', 'refresh', '--scope', 'infra']), \
                patch.object(inventory, 'publish') as publish:
            self.assertEqual(inventory.main(), 0)
            self.assertEqual(publish.call_args.args[1], str(ROOT/'.dev-state/deployment-discovery'))

    def test_lock_contention_is_bounded_without_changing_previous_snapshot(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)/'discovery'
            inventory.publish('business', directory, env={}, reader=self.source(), system='Darwin')
            previous = (directory/'business.json').read_bytes()
            with patch.object(inventory.fcntl, 'flock', side_effect=BlockingIOError), \
                    patch.object(inventory.time, 'monotonic', side_effect=[0, 6]):
                with self.assertRaises(ValueError):
                    inventory.publish('business', directory, env={}, reader=self.source(), system='Darwin')
            self.assertEqual((directory/'business.json').read_bytes(), previous)


class ObserverLifecycleTests(unittest.TestCase):
    def test_proc_read_rejects_oversized_non_utf8_and_symlink_sources(self):
        target=self.root/'proc-fixture'
        real_open=inventory.os.open
        with patch.object(inventory.os,'open',side_effect=lambda path,flags:real_open(target,flags)):
            target.write_text('safe')
            self.assertEqual(inventory.observer_proc_text(123,'state'),'safe')
            for content in (b'x'*8193,b'\xff'):
                target.write_bytes(content)
                self.assertIsNone(inventory.observer_proc_text(123,'state'))
            target.unlink()
            victim=self.root/'private-sentinel'
            victim.write_text('private-sentinel')
            target.symlink_to(victim)
            self.assertIsNone(inventory.observer_proc_text(123,'state'))

    def test_exit_diagnostic_redacts_proc_text_and_uses_only_fixed_fields(self):
        files = {
            'state': '123 (private-command ) --observer-token private-sentinel) S 1 2 3',
            'wait': 'private-sentinel-wait',
            'signals': 'Name: private-sentinel\nSigPnd:\t0000000000004000\nShdPnd:\t0000000000000000\nSigBlk:\t0000000000004000\nSigIgn:\t0000000000000000\nSigCgt:\t0000000000004000\n',
            'children': '1234 5678\n',
        }
        with patch.object(inventory.platform, 'system', return_value='Linux'), \
                patch.object(inventory, 'observer_proc_text', side_effect=lambda pid, name: files[name]):
            result=inventory.observer_exit_diagnostic({'pid':123}, 'owned')
        self.assertEqual(result, {'last_identity':'owned', 'proc_state':'S', 'wait':'other',
                                 'term_pending':True,'term_blocked':True,'term_ignored':False,'term_caught':True,'children':2})
        self.assertNotIn('private-sentinel', json.dumps(result))
        self.assertNotIn('1234', json.dumps(result))

    def test_exit_diagnostic_known_wait_and_malformed_sources(self):
        for channel in ['do_wait','wait_for_vfork_done','pipe_read','futex_wait_queue','io_schedule']:
            with patch.object(inventory.platform, 'system', return_value='Linux'), \
                    patch.object(inventory,'observer_proc_text',side_effect=lambda pid,name:channel if name=='wait' else None):
                result=inventory.observer_exit_diagnostic({'pid':123},'foreign')
            self.assertEqual(result['wait'],channel)
            self.assertEqual(result['proc_state'],'unknown')
            self.assertEqual(result['term_blocked'],'unknown')
            self.assertEqual(result['children'],'unknown')
        with patch.object(inventory.platform,'system',return_value='Darwin'):
            self.assertEqual(inventory.observer_exit_diagnostic({'pid':123},'owned'),{'last_identity':'owned','proc_state':'unavailable'})

    def test_exit_diagnostic_does_not_turn_failed_cleanup_into_success(self):
        record={'pid':2147483647}
        with patch.object(inventory,'observer_state',return_value='owned'), \
                patch.object(inventory.os,'kill') as kill,patch.object(inventory.time,'sleep'), \
                patch.object(inventory,'observer_exit_diagnostic',return_value={'last_identity':'owned','proc_state':'S','wait':'do_wait'}):
            with self.assertRaisesRegex(ValueError,'尚未退出.*do_wait'):
                inventory.terminate_observer(record)
        kill.assert_called_once_with(record['pid'],inventory.signal.SIGTERM)

    def test_signal_exit_waits_through_transient_foreign_observation_without_another_signal(self):
        record = {'pid': 2147483647}
        with patch.object(inventory, 'observer_state', side_effect=['owned', 'foreign', 'absent']), \
                patch.object(inventory.os, 'kill') as kill, patch.object(inventory.time, 'sleep'):
            inventory.terminate_observer(record)
        kill.assert_called_once_with(record['pid'], inventory.signal.SIGTERM)

    def test_disappearance_before_signal_is_rechecked_before_cleanup(self):
        record = {'pid': 2147483647}
        with patch.object(inventory, 'observer_state', side_effect=['owned', 'absent']), \
                patch.object(inventory.os, 'kill', side_effect=ProcessLookupError) as kill:
            inventory.terminate_observer(record)
        kill.assert_called_once_with(record['pid'], inventory.signal.SIGTERM)

    def test_persistent_foreign_observation_after_signal_retains_runtime_record(self):
        inventory.private_directory(self.directory)
        record = {'pid': 2147483647, 'token': 'a'*32, 'script': str(Path(inventory.__file__).resolve()),
                  'directory': str(self.directory.resolve()), 'configuration': 'b'*64}
        inventory.write_document(self.directory, 'observer.json', record)
        states = iter(['owned'] + ['foreign']*100)
        try:
            with patch.object(inventory, 'observer_state', side_effect=lambda _: next(states)), \
                    patch.object(inventory.os, 'kill') as kill, patch.object(inventory.time, 'sleep'):
                with self.assertRaisesRegex(ValueError, '尚未退出'):
                    inventory.stop_observer(self.directory)
            kill.assert_called_once_with(record['pid'], inventory.signal.SIGTERM)
            self.assertEqual(inventory.observer_record(self.directory.resolve()), record)
        finally:
            (self.directory/'observer.json').unlink(missing_ok=True)

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='addp-discovery-observer-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.directory = self.root/'snapshots'
        self.bin = self.root/'bin'
        self.bin.mkdir()
        docker = self.bin/'docker'
        # Reuse an installed executable: macOS may defer first execution of
        # newly written shebang files independently of observer readiness.
        docker.symlink_to('/usr/bin/false' if Path('/usr/bin/false').exists() else '/bin/false')
        self.environment = dict(os.environ, PATH=str(self.bin)+os.pathsep+os.environ['PATH'])
        self.addCleanup(self.stop)

    def stop(self):
        inventory.stop_observer(self.directory)

    def test_start_is_idempotent_and_stop_clears_only_owned_process(self):
        with patch.dict(os.environ, self.environment, clear=True):
            inventory.start_observer(self.directory)
            record = inventory.observer_record(self.directory.resolve())
            self.assertEqual(inventory.observer_state(record), 'owned')
            inventory.start_observer(self.directory)
            self.assertEqual(inventory.observer_record(self.directory.resolve()), record)
            deadline = time.monotonic()+3
            while not (self.directory/'business.json').exists() and time.monotonic()<deadline:
                time.sleep(0.02)
            self.assertEqual(json.loads((self.directory/'business.json').read_text())['state'], 'unavailable')
            inventory.stop_observer(self.directory)
            self.assertEqual(inventory.observer_state(record), 'absent')
            self.assertFalse((self.directory/'observer.json').exists())
            self.assertFalse((self.directory/'observer-ready.json').exists())
            for scope in inventory.SCOPES:
                self.assertEqual(json.loads((self.directory/(scope+'.json')).read_text())['state'], 'unavailable')

    def test_observer_does_not_inherit_application_credentials_or_lifecycle_lock(self):
        real_popen = subprocess.Popen
        processes = []
        def launch(*args, **kwargs):
            if 'watch' in args[0]:
                processes.append(kwargs['env'])
            return real_popen(*args, **kwargs)
        with patch.dict(os.environ, dict(self.environment, POSTGRES_PASSWORD='private-sentinel', SYSTEM_SERVICE_CLIENT_SECRET='private-sentinel', ADDP_DEV_LIFECYCLE_LOCK_OWNER='private-sentinel'), clear=True), \
                patch.object(inventory.subprocess, 'Popen', side_effect=launch):
            inventory.start_observer(self.directory)
        self.assertEqual(len(processes), 1)
        self.assertNotIn('private-sentinel', json.dumps(processes[0]))
        self.assertEqual(processes[0]['PATH'], self.environment['PATH'])

    def test_stop_interrupts_inflight_docker_and_reaps_its_child(self):
        marker, release = self.root/'docker-pid', self.root/'release'
        (self.bin/'docker').unlink()
        (self.bin/'docker').write_text(
            '#!'+sys.executable+'\nimport os, time\nfrom pathlib import Path\n'
            f'Path({str(marker)!r}).write_text(str(os.getpid()))\n'
            f'while not Path({str(release)!r}).exists(): time.sleep(0.02)\n')
        (self.bin/'docker').chmod(0o755)
        with patch.dict(os.environ, self.environment, clear=True):
            inventory.start_observer(self.directory)
        try:
            # The reader permits ten seconds for its Docker command.
            deadline = time.monotonic()+10
            while not marker.exists() and time.monotonic()<deadline:
                time.sleep(0.02)
            self.assertTrue(marker.exists(), 'observer did not enter the Docker read')
            pid = int(marker.read_text())
            inventory.stop_observer(self.directory)
            status = subprocess.run(['ps', '-p', str(pid), '-o', 'stat='], capture_output=True, text=True)
            self.assertTrue(status.returncode == 1 or status.stdout.strip().startswith('Z'),
                            'Docker child survived observer shutdown')
        finally:
            release.touch()
            inventory.stop_observer(self.directory)

    def test_stop_signal_during_popen_finalizer_is_not_lost(self):
        # Signal exceptions raised in a real __del__ are unraisable. Hold the
        # completed Popen finalizer so the controller's one TERM hits that edge.
        marker, received, lost = (self.root/name for name in ('in-finalizer','term-received','unraisable'))
        runner = r"""
import importlib.util, os, signal, subprocess, sys, time
from pathlib import Path
spec=importlib.util.spec_from_file_location('observer',sys.argv[1])
observer=importlib.util.module_from_spec(spec);spec.loader.exec_module(observer)
directory=Path(sys.argv[2]);marker,received,lost=map(Path,sys.argv[3:6])
original=subprocess.Popen.__del__
armed=True
sys.unraisablehook=lambda event: lost.write_text('SystemExit') if event.exc_type is SystemExit else None
def finalizer(process):
    global armed
    if armed:
        armed=False
        handler=signal.getsignal(signal.SIGTERM)
        def receive(signum,frame):
            received.write_text('received')
            handler(signum,frame)
        signal.signal(signal.SIGTERM,receive)
        marker.write_text('ready')
        deadline=time.monotonic()+10
        while not received.exists() and time.monotonic()<deadline:
            time.sleep(.005)
    original(process)
subprocess.Popen.__del__=finalizer
def publish(*_):
    subprocess.run([sys.executable,'-c','pass'],check=True)
observer.publish=publish
observer.publish_container_proofs=lambda *_:None
observer.watch(directory,sys.argv[-1])
"""
        inventory.private_directory(self.directory)
        with patch.dict(os.environ,self.environment,clear=True):
            env=inventory.observer_environment()
            token='f'*32
            process=subprocess.Popen([sys.executable,'-c',runner,str(Path(inventory.__file__).resolve()),
                str(self.directory.resolve()),str(marker),str(received),str(lost),'--observer-token',token],
                env=env,stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
            record={'pid':process.pid,'token':token,'script':str(Path(inventory.__file__).resolve()),
                    'directory':str(self.directory.resolve()),'configuration':inventory.observer_configuration(env)}
            inventory.write_document(self.directory,'observer.json',record)
            try:
                deadline=time.monotonic()+10
                while not marker.exists() and process.poll() is None and time.monotonic()<deadline:
                    time.sleep(.02)
                self.assertTrue(marker.exists(),'observer did not reach the finalizer boundary')
                try:
                    inventory.stop_observer(self.directory)
                except inventory.InventoryError as error:
                    self.fail(str(error)+'; signal_received='+str(received.exists())+'; unraisable='+str(lost.exists()))
                self.assertTrue(received.exists(),'TERM delivery was not observed')
                self.assertFalse(lost.exists(),'TERM became an ignored SystemExit in a finalizer')
                self.assertEqual(process.wait(timeout=3),0)
                self.assertFalse((self.directory/'observer.json').exists())
                self.assertFalse((self.directory/'observer-ready.json').exists())
            finally:
                # This Popen remains unreaped and cannot have its PID reused.
                # On a red result kill only this test child, then verify absence
                # through the real controller before allowing temporary cleanup.
                if process.poll() is None:
                    process.kill()
                process.wait(timeout=3)
                inventory.stop_observer(self.directory)

    def test_observer_exit_does_not_wait_for_snapshot_writers(self):
        inventory.private_directory(self.directory)
        with inventory.private_lock(self.directory, 'infra.lock'), \
                inventory.private_lock(self.directory, 'business.lock'):
            with patch.dict(os.environ, self.environment, clear=True):
                inventory.start_observer(self.directory)
            record = inventory.observer_record(self.directory.resolve())
            inventory.terminate_observer(record)
        self.assertEqual(inventory.observer_state(record), 'absent')
        self.assertTrue((self.directory/'observer.json').exists())
        inventory.stop_observer(self.directory)
        self.assertFalse((self.directory/'observer.json').exists())
        for scope in inventory.SCOPES:
            self.assertEqual(json.loads((self.directory/(scope+'.json')).read_text())['state'], 'unavailable')

    def test_failed_runtime_record_publication_reaps_new_child(self):
        real_popen = subprocess.Popen
        children = []
        def launch(*args, **kwargs):
            child = real_popen(*args, **kwargs)
            children.append(child)
            return child
        with patch.dict(os.environ, self.environment, clear=True), \
                patch.object(inventory.subprocess, 'Popen', side_effect=launch), \
                patch.object(inventory, 'write_document', side_effect=OSError('record publication failed')):
            with self.assertRaises(OSError):
                inventory.start_observer(self.directory)
        self.assertEqual(len(children), 1)
        self.assertIsNotNone(children[0].poll())

    def test_cli_process_inspection_failure_is_redacted(self):
        with patch.object(inventory.sys, 'argv', ['deployment-inventory.py', 'stop', '--directory', str(self.directory)]), \
                patch.object(inventory, 'stop_observer', side_effect=subprocess.TimeoutExpired(['ps','private-sentinel'],3)), \
                patch.object(inventory.sys, 'stderr', new_callable=io.StringIO) as error:
            self.assertEqual(inventory.main(), 1)
            self.assertNotIn('private-sentinel', error.getvalue())

    def test_foreign_pid_is_never_reused_or_stopped(self):
        child = subprocess.Popen([sys.executable, '-c', 'import time; time.sleep(30)'])
        try:
            inventory.private_directory(self.directory)
            inventory.write_document(self.directory, 'observer.json', {'pid':child.pid, 'token':'a'*32,
                'script':str(Path(inventory.__file__).resolve()),'directory':str(self.directory.resolve()),'configuration':'b'*64})
            with self.assertRaises(ValueError):
                inventory.start_observer(self.directory)
            with self.assertRaises(ValueError):
                inventory.stop_observer(self.directory)
            self.assertIsNone(child.poll())
            (self.directory/'observer.json').unlink()
        finally:
            child.terminate()
            child.wait(timeout=5)

    def test_source_environment_change_replaces_owned_observer_but_credentials_do_not(self):
        with patch.dict(os.environ, self.environment, clear=True):
            inventory.start_observer(self.directory)
            first = inventory.observer_record(self.directory.resolve())
            with patch.dict(os.environ, {'POSTGRES_PASSWORD': 'changed-credential'}):
                inventory.start_observer(self.directory)
            self.assertEqual(inventory.observer_record(self.directory.resolve()), first)
            with patch.dict(os.environ, {'DOCKER_CONTEXT': 'explicit-local-context'}):
                inventory.start_observer(self.directory)
                second = inventory.observer_record(self.directory.resolve())
            self.assertNotEqual(first['pid'], second['pid'])
            self.assertEqual(inventory.observer_state(first), 'absent')
            self.assertEqual(inventory.observer_state(second), 'owned')


if __name__ == '__main__':
    unittest.main()
