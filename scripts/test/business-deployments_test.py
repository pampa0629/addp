"""Business inventory boundary tests; never access a real Docker daemon."""
import copy
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location('deployments', ROOT / 'business/scripts/deployments.py')
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
            return self.ids
        if args[2] == 'inspect':
            value = copy.deepcopy(self.container)
            if self.after:
                self.after(self)
            return json.dumps(value)
        raise AssertionError('Unexpected/mutating Docker command: ' + args[2])


class DeploymentInventoryTests(unittest.TestCase):
    def export(self, source=None, system='Darwin'):
        return json.loads(inventory.export_inventory('postgres', env={}, reader=source or Source(), system=system))

    def test_desktop_projection_uses_actual_port_without_credentials_or_authority(self):
        source = Source()
        result = self.export(source)
        self.assertEqual(set(result), {'schema', 'observed_at', 'source', 'services'})
        self.assertEqual(result['schema'], 'addp.business-deployment-inventory/v1')
        self.assertEqual(result['source'], {'type': 'docker_compose', 'runtime': 'desktop_linux_vm',
                                           'docker_engine_id': 'daemon-identity', 'project': 'business'})
        service = result['services'][0]
        self.assertEqual(set(service), {'service', 'engine_type', 'containers'})
        self.assertEqual(service['engine_type'], 'postgresql')
        self.assertEqual(service['containers'][0]['published_ports'], [
            {'address': '127.0.0.1', 'port': 15433, 'container_port': 5432, 'protocol': 'tcp'}])
        encoded = json.dumps(result)
        for forbidden in ('password', '"tenant_id"', '"engine_id"', '"node_id"', 'working_dir', str(ROOT)):
            self.assertNotIn(forbidden, encoded)
        self.assertLess(len(encoded), inventory.LIMIT)
        self.assertTrue(result['observed_at'].endswith('Z'))
        self.assertEqual({args[2] for args in source.commands if args[0] == '--context'}, {'info', 'ps', 'inspect'})
        self.assertNotIn('.Config.Env', inventory.CONTAINER)
        self.assertNotIn('json .Config.Labels', inventory.CONTAINER)

    def test_linux_and_ipv6(self):
        source = Source()
        source.info['operating_system'] = 'Ubuntu 24.04.3 LTS'
        source.container['ports']['5432/tcp'] = [{'HostIp': '::1', 'HostPort': '5433'}]
        self.assertEqual(self.export(source, system='Linux')['source']['runtime'], 'linux_docker')

    def test_unknown_service_and_environment_override_fail_before_docker(self):
        cases = [('mysql', {})] + [('postgres', {key: 'sensitive-value'}) for key in
                                  ('DOCKER_HOST', 'DOCKER_TLS_VERIFY', 'DOCKER_CERT_PATH')]
        for service, env in cases:
            with self.subTest(service=service, env=env):
                source = Source()
                with self.assertRaises(ValueError):
                    inventory.export_inventory(service, env=env, reader=source)
                self.assertEqual(source.commands, [])

    def test_remote_and_malformed_endpoints_fail_before_info(self):
        for endpoint in ('tcp://127.0.0.1:2375', 'ssh://host', 'unix://remote/socket',
                         'unix:///tmp/socket?secret=1', 'unix:///tmp/socket\n', None):
            with self.subTest(endpoint=endpoint):
                source = Source()
                source.endpoint = endpoint
                with self.assertRaises(ValueError):
                    self.export(source)
                self.assertEqual(len(source.commands), 2)

    def test_unsupported_runtime_and_daemon_identity(self):
        for system, os_type, name, daemon in (
                ('Darwin', 'linux', 'Ubuntu', 'id'), ('Linux', 'linux', 'Docker Desktop', 'id'),
                ('Windows', 'linux', 'Docker Desktop', 'id'), ('Linux', 'windows', 'Windows', 'id'),
                ('Darwin', 'linux', 'Docker Desktop', ''), ('Darwin', 'linux', 'Docker Desktop', 'x\n')):
            with self.subTest(system=system, name=name, daemon=daemon):
                source = Source()
                source.info.update(id=daemon, os_type=os_type, operating_system=name)
                with self.assertRaises(ValueError):
                    self.export(source, system=system)

    def test_foreign_workspace_and_invalid_container_state(self):
        for key, value in (('working_dir', '/tmp/foreign'), ('working_dir', 'business'),
                           ('project', 'infra'), ('service', 'mysql'), ('oneoff', 'True'), ('id', 'short'),
                           ('running', False), ('status', 'restarting'), ('restart_count', True),
                           ('started_at', '0001-01-01T00:00:00Z'), ('started_at', '2026-99-18T00:00:00Z')):
            with self.subTest(key=key, value=value):
                source = Source()
                source.container[key] = value
                with self.assertRaises(ValueError):
                    self.export(source)

    def test_missing_multiple_or_invalid_container_ids(self):
        for ids in ('', CID + '\n' + 'b' * 64, 'not-a-container'):
            with self.subTest(ids=ids):
                source = Source()
                source.ids = ids
                with self.assertRaises(ValueError):
                    self.export(source)

    def test_invalid_port_projection(self):
        good = {'HostIp': '127.0.0.1', 'HostPort': '5432'}
        for bindings in (None, [], [dict(good, HostIp='')], [dict(good, HostPort='65536')],
                         [dict(good, HostPort='05432')], [dict(good, credential='secret')], [good, good]):
            with self.subTest(bindings=bindings):
                source = Source()
                source.container['ports']['5432/tcp'] = bindings
                with self.assertRaises(ValueError):
                    self.export(source)

    def test_restart_replacement_ports_and_daemon_change_reject_whole_export(self):
        def start(source):
            source.container['started_at'] = '2026-10-18T13:30:00Z'

        def replacement(source):
            source.ids = 'b' * 64

        def port(source):
            source.container['ports']['5432/tcp'][0]['HostPort'] = '5434'

        def daemon(source):
            source.info['id'] = 'different-daemon'

        for mutate in (start, replacement, port, daemon):
            with self.subTest(mutate=mutate.__name__):
                source = Source()
                source.after = mutate
                with self.assertRaises(ValueError):
                    self.export(source)

    def test_docker_failures_timeout_and_size_are_bounded_and_redacted(self):
        for failure in (subprocess.CalledProcessError(1, 'docker', output=b'password-secret'),
                        subprocess.TimeoutExpired('docker', 10), FileNotFoundError()):
            with patch.object(inventory.subprocess, 'run', side_effect=failure):
                with self.assertRaises(ValueError) as error:
                    inventory.DockerReader()('info')
                self.assertNotIn('password-secret', str(error.exception))
        with patch.object(inventory.subprocess, 'run', return_value=subprocess.CompletedProcess(
                ['docker'], 0, b'x' * (64 * 1024 + 1))):
            with self.assertRaises(ValueError):
                inventory.DockerReader()('info')

    def test_cli_failure_is_atomic_and_redacts_source_errors(self):
        with patch.object(inventory.sys, 'argv', ['deployments.py', 'export', '--service', 'postgres']), \
                patch.object(inventory, 'export_inventory', side_effect=ValueError('secret-value')), \
                patch.object(inventory.sys, 'stdout', new_callable=io.StringIO) as out, \
                patch.object(inventory.sys, 'stderr', new_callable=io.StringIO) as err:
            self.assertEqual(inventory.main(), 1)
            self.assertEqual(out.getvalue(), '')
            self.assertNotIn('secret-value', err.getvalue())

    def test_cli_explains_safe_source_rejection_without_partial_output(self):
        with patch.object(inventory.sys, 'argv', ['deployments.py', 'export', '--service', 'postgres']), \
                patch.object(inventory, 'export_inventory', side_effect=inventory.InventoryError('容器不属于当前工作区的 Business 服务')), \
                patch.object(inventory.sys, 'stdout', new_callable=io.StringIO) as out, \
                patch.object(inventory.sys, 'stderr', new_callable=io.StringIO) as err:
            self.assertEqual(inventory.main(), 1)
            self.assertEqual(out.getvalue(), '')
            self.assertIn('不属于当前工作区', err.getvalue())


if __name__ == '__main__':
    unittest.main()
