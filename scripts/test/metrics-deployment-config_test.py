import importlib.util
import json
import os
import re
import sys
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('metrics_config', ROOT / 'scripts/infra/generate-metrics-config.py')
config = importlib.util.module_from_spec(spec)
spec.loader.exec_module(config)


class MetricsDeploymentConfigTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='addp-metrics-config-')
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.secret = 'test-prometheus-' + 'a' * 32
        for name in config.FILES:
            (self.directory / name).write_text(self.secret if name == 'prometheus-client-secret' else name)
        self.env = dict(ADDP_METRICS_DEPLOYMENT_DIR=str(self.directory),
                        PROMETHEUS_SERVICE_CLIENT_SECRET=self.secret,
                        PROMETHEUS_SYSTEM_URL='https://system.internal:8443',
                        PROMETHEUS_MONITOR_URL='https://monitor.internal:8443')

    def test_query_native_probe_has_registered_inputs_and_hosted_go_runtime(self):
        gate_spec = importlib.util.spec_from_file_location('metrics_module_gate', ROOT / 'scripts/test/module-gate.py')
        gate_module = importlib.util.module_from_spec(gate_spec)
        sys.modules[gate_spec.name] = gate_module
        gate_spec.loader.exec_module(gate_module)
        script = (ROOT / 'scripts/test/monitor-metrics-gate.sh').read_text()
        inputs = re.search(r'^# ADDP_T2_INPUT_FILES=(.+)$', script, re.M).group(1).split()
        for path in ('monitor/backend/internal/resourcequery/client.go',
                     'monitor/backend/internal/resourcequery/device_counter.go',
                     'monitor/backend/internal/resourcequery/disk_test.go',
                     'monitor/backend/internal/resourcequery/disk_io_test.go',
                     'monitor/backend/internal/resourcequery/deleted-or-future.go',
                     'monitor/backend/internal/config/config.go',
                     'scripts/prod/metrics-query.yml',
                     'scripts/infra/node-metrics-desktop.yml'):
            self.assertTrue(gate_module.gate_input_covers(inputs, path), path)
        workflow = (ROOT / '.github/workflows/release-and-t2-gates.yml').read_text()
        job = re.search(r'^  monitor-metrics:\n(.*?)(?=^  \S|\Z)', workflow, re.M | re.S).group(1)
        self.assertIn('uses: actions/setup-go@', job)
        self.assertIn('go-version-file: common/go.mod', job)
        self.assertLess(job.index('uses: actions/setup-go@'), job.index('run: make test-monitor-metrics'))
        probe = (ROOT / 'scripts/test/monitor-metrics-probe.py').read_text()
        self.assertIn('CPUWindow|DiskWindow|DiskIOWindow|NetworkWindow|Filesystem|FilesystemInodes', probe)

    def test_sole_config_uses_native_platform_oauth_and_independent_tls(self):
        config.render(self.env)
        data = (self.directory / 'prometheus.yml').read_text()
        self.assertNotIn(self.secret, data)
        self.assertNotIn('@@', data)
        self.assertIn('https://system.internal:8443/api/v1/system/oauth/token', data)
        self.assertIn('https://monitor.internal:8443/api/v1/monitor/platform/metrics_discovery', data)
        for value in ('client_id: addp-prometheus', 'context_type: platform', 'audience: addp.api',
                      'refresh_interval: 30s', 'target_limit: 9', 'sample_limit: 20000',
                      'collector.key', 'control-ca.crt', 'source-ca.crt'):
            self.assertIn(value, data)
        config.render(self.env)
        self.assertEqual((self.directory / 'prometheus.yml').read_text(), data)
        self.assertEqual(sorted(p.name for p in self.directory.iterdir()), sorted((*config.FILES, 'prometheus.yml')))

    def test_desktop_loopback_rules_keep_the_original_identity_and_exact_port(self):
        env = dict(self.env, ADDP_METRICS_DESKTOP_LOOPBACK_PORT='19091')
        replies = [json.dumps([{'Endpoints': {'docker': {'Host': 'unix:///local/docker.sock'}}}]),
                   json.dumps({'OperatingSystem': 'Docker Desktop', 'OSType': 'linux'})]
        with mock.patch.object(config.platform, 'system', return_value='Darwin'), mock.patch.object(config.subprocess, 'check_output', side_effect=replies):
            config.render(env)
        data = (self.directory / 'prometheus.yml').read_text()
        self.assertIn('target_label: instance', data)
        self.assertIn('host.docker.internal:19091', data)
        self.assertIn('127\\\\.0\\\\.0\\\\.1:19091', data)
        self.assertEqual(data.count('job_name: addp_nodes'), 1)
        self.assertNotIn('server_name:', data)
        before = data
        for port in ('019091', '0', '65536', '19091\n', '19091:123'):
            with self.assertRaises(ValueError): config.render(dict(env, ADDP_METRICS_DESKTOP_LOOPBACK_PORT=port))
        with mock.patch.object(config.platform, 'system', return_value='Linux'), self.assertRaises(ValueError): config.render(env)
        with mock.patch.object(config.platform, 'system', return_value='Darwin'), self.assertRaises(ValueError): config.render(dict(env, DOCKER_HOST='tcp://remote:2376'))
        self.assertEqual((self.directory / 'prometheus.yml').read_text(), before)
        config.render(self.env)
        self.assertNotIn('relabel_configs:\n      - source_labels: [__address__]', (self.directory / 'prometheus.yml').read_text())

    def test_rejects_ambiguous_origins_without_replacing_valid_config(self):
        config.render(self.env)
        before = (self.directory / 'prometheus.yml').read_bytes()
        for value in ('http://host:80', 'https://host', 'https://host:0443', 'https://host:443/',
                      'https://host:443?x=1', 'https://host:443#x', 'https://user:secret@host:443',
                      'https://HOST:443', 'https://bad..host:443', 'https://0.0.0.0:443',
                      'https://169.254.169.254:443', 'https://[::]:443', 'https://host:65536',
                      'https://host:443\nmalicious'):
            with self.subTest(value=value), self.assertRaises(ValueError):
                config.render(dict(self.env, PROMETHEUS_MONITOR_URL=value))
            self.assertEqual((self.directory / 'prometheus.yml').read_bytes(), before)

    def control_inputs(self):
        # Different mount directory; only centre files belong in deployment.
        directory = Path(self.temp.name + '-control')
        directory.mkdir()
        self.addCleanup(lambda: __import__('shutil').rmtree(directory))
        for name in ('server.crt', 'server.key'):
            (directory / name).write_text(name)
        return dict(self.env, ADDP_METRICS_CONTROL_ENABLED='true',
                    ADDP_METRICS_CONTROL_DIR=str(directory),
                    ADDP_METRICS_CONTROL_GATEWAY_URL='http://host.docker.internal:8000',
                    PROMETHEUS_SYSTEM_URL='https://metrics-control:9444',
                    PROMETHEUS_MONITOR_URL='https://metrics-control:9444')

    def test_private_control_selection_generation_and_rejection_are_atomic(self):
        env = self.control_inputs()
        config.render(env)
        control = Path(env['ADDP_METRICS_CONTROL_DIR']) / 'nginx.conf'
        original = control.read_bytes()
        prometheus = (self.directory / 'prometheus.yml').read_bytes()
        text = original.decode()
        self.assertNotIn(self.secret, text)
        self.assertIn('proxy_set_header Authorization $http_authorization;', text)
        self.assertIn('access_log off;', text)
        self.assertEqual(text.count('proxy_pass http://host.docker.internal:8000;'), 2)
        self.assertIn('location / { return 404; }', text)
        for values in ({'ADDP_METRICS_CONTROL_ENABLED': 'yes'},
                       {'ADDP_METRICS_CONTROL_DIR': str(ROOT)},
                       {'ADDP_METRICS_CONTROL_GATEWAY_URL': 'http://host:80/path'},
                       {'ADDP_METRICS_CONTROL_GATEWAY_URL': 'http://HOST:80'},
                       {'ADDP_METRICS_CONTROL_GATEWAY_URL': 'http://user:pass@host:80'},
                       {'PROMETHEUS_MONITOR_URL': 'https://other:9444'}):
            with self.subTest(values=values), self.assertRaises(ValueError):
                config.render(dict(env, **values))
            self.assertEqual(control.read_bytes(), original)
            self.assertEqual((self.directory / 'prometheus.yml').read_bytes(), prometheus)
        (control.parent / 'ca.key').write_text('private-signing-key')
        with self.assertRaises(ValueError):
            config.render(env)
        config.render(dict(self.env, ADDP_METRICS_CONTROL_ENABLED='false',
                           ADDP_METRICS_CONTROL_DIR='invalid', ADDP_METRICS_CONTROL_GATEWAY_URL='invalid'))
        self.assertEqual(control.read_bytes(), original)

    def test_control_profile_is_explicit_and_has_no_host_port_or_business_dependency(self):
        root = (ROOT / 'docker-compose.infra.yml').read_text()
        service = root.split('  metrics-control:\n', 1)[1].split('  prometheus:', 1)[0]
        self.assertNotIn('ports:', service)
        self.assertIn('profiles: [observability-metrics-control]', service)
        template = (ROOT / 'scripts/infra/metrics-control.yml').read_text()
        self.assertNotIn('ports:', template)
        self.assertNotIn('depends_on:', template)
        for key in ('65534:65534', 'read_only: true', 'cap_drop: [ALL]', 'cpus: 0.25', 'mem_limit: 128m'):
            self.assertIn(key, template)
        for metrics, control, selected in (('false', 'true', False), ('true', 'false', False), ('true', 'true', True)):
            env = dict(os.environ, ADDP_OBSERVABILITY_LOGS_ENABLED='false',
                       ADDP_OBSERVABILITY_METRICS_ENABLED=metrics, ADDP_METRICS_CONTROL_ENABLED=control)
            result = subprocess.run(['bash', '-c', 'source scripts/utils/observability-env.sh; addp_observability_profiles; printf "%s" "${COMPOSE_PROFILES-}"'],
                                    cwd=ROOT, env=env, capture_output=True, text=True, check=True)
            self.assertEqual('observability-metrics-control' in result.stdout, selected)
        gate = (ROOT / 'scripts/test/monitor-metrics-gate.sh').read_text()
        for name in ('scripts/infra/metrics-control.conf', 'scripts/infra/metrics-control.yml'):
            self.assertIn(name, gate.splitlines()[3])
        down = (ROOT / 'scripts/infra/down.sh').read_text()
        self.assertIn('--profile observability-metrics-control', down)

    def test_secret_missing_mismatched_or_shared_is_rejected(self):
        for values in ({'PROMETHEUS_SERVICE_CLIENT_SECRET': ''},
                       {'PROMETHEUS_SERVICE_CLIENT_SECRET': 'b' * 32},
                       {'MONITOR_SERVICE_CLIENT_SECRET': self.secret}):
            with self.subTest(values=list(values)), self.assertRaises(ValueError):
                config.render(dict(self.env, **values))

    def test_repository_directory_extra_private_key_and_missing_file_are_rejected(self):
        with self.assertRaises(ValueError):
            config.render(dict(self.env, ADDP_METRICS_DEPLOYMENT_DIR=str(ROOT)))
        extra = self.directory / 'signing-ca.key'
        extra.write_text('do-not-mount')
        with self.assertRaises(ValueError):
            config.render(self.env)
        extra.unlink()
        (self.directory / 'collector.key').unlink()
        with self.assertRaises(ValueError):
            config.render(self.env)

    def test_health_certificate_reuse_is_rejected(self):
        # Health directory is intentionally outside the mounted deployment directory.
        with tempfile.TemporaryDirectory() as health:
            Path(health, 'health.crt').write_bytes((self.directory / 'collector.crt').read_bytes())
            with self.assertRaises(ValueError):
                config.render(dict(self.env, ADDP_METRICS_TLS_DIR=health))

    def test_query_certificate_mount_is_independent_and_optional(self):
        source = (ROOT / 'scripts/prod/start.sh').read_text()
        phase = source.split('bash scripts/prod/wait-infra.sh\n', 1)[1].split('# Containers use', 1)[0]
        script = ('source scripts/utils/observability-env.sh\n'
                  'PLATFORM_COMPOSE_FILES=(-f docker-compose.yml)\ninfra_result=0\n'
                  + phase + '\nprintf "%s\n" "$infra_result" "${PLATFORM_COMPOSE_FILES[*]}"\n')
        env = {key: value for key, value in os.environ.items()
               if not key.startswith(('ADDP_METRICS_', 'MONITOR_METRICS_', 'MONITOR_PROMETHEUS_'))}
        env.update(ADDP_OBSERVABILITY_METRICS_ENABLED='true',
                   MONITOR_METRICS_CA_FILE=str(self.directory / 'source-ca.crt'),
                   MONITOR_METRICS_CLIENT_CERT_FILE=str(self.directory / 'collector.crt'),
                   MONITOR_METRICS_CLIENT_KEY_FILE=str(self.directory / 'collector.key'))
        for origin, complete in (('', False), ('https://prometheus:9090', False),
                                 ('https://prometheus:9090', True)):
            values = dict(env, MONITOR_PROMETHEUS_URL=origin)
            if complete:
                for key in ('CA_FILE', 'CLIENT_CERT_FILE', 'CLIENT_KEY_FILE'):
                    values['MONITOR_PROMETHEUS_' + key] = str(self.directory / 'control-ca.crt')
            result = subprocess.run(['bash', '-c', script], cwd=ROOT, env=values,
                                    text=True, capture_output=True, check=True)
            self.assertEqual('metrics-query.yml' in result.stdout, bool(origin) and complete)
            self.assertEqual(result.stdout.splitlines()[0], '1' if origin and not complete else '0')
        text = (ROOT / 'scripts/prod/metrics-query.yml').read_text()
        self.assertEqual(text.count('read_only: true'), 3)
        self.assertNotIn('health.', text)
        self.assertNotIn('collector.', text)
        self.assertNotIn('depends_on', text)

    def test_platform_certificate_mounts_are_selected_only_with_complete_inputs(self):
        source = (ROOT / 'scripts/prod/start.sh').read_text()
        phase = source.split('bash scripts/prod/wait-infra.sh\n', 1)[1].split('# Containers use', 1)[0]
        script = ('source scripts/utils/observability-env.sh\n'
                  'PLATFORM_COMPOSE_FILES=(-f docker-compose.yml)\ninfra_result=0\n'
                  + phase + '\nprintf "%s\\n" "$infra_result" "${PLATFORM_COMPOSE_FILES[*]}"\n')
        env = {key: value for key, value in os.environ.items() if not key.startswith(('ADDP_METRICS_', 'MONITOR_METRICS_', 'MONITOR_PROMETHEUS_'))}
        for selected, valid, expected in (('false', False, '0'), ('true', False, '1'), ('true', True, '0')):
            with self.subTest(selected=selected, valid=valid):
                values = dict(env, ADDP_OBSERVABILITY_METRICS_ENABLED=selected)
                if valid:
                    values.update(MONITOR_METRICS_CA_FILE=str(self.directory / 'source-ca.crt'),
                                  MONITOR_METRICS_CLIENT_CERT_FILE=str(self.directory / 'collector.crt'),
                                  MONITOR_METRICS_CLIENT_KEY_FILE=str(self.directory / 'collector.key'))
                result = subprocess.run(['bash', '-eu', '-c', script], cwd=ROOT, env=values,
                                        capture_output=True, text=True, timeout=5)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(result.stdout.splitlines()[0], expected)
                self.assertEqual('metrics-platform.yml' in result.stdout, selected == 'true' and valid)

    def test_real_compose_passes_optional_identity_and_readonly_admission_files(self):
        env = dict(os.environ, ADDP_OBSERVABILITY_METRICS_ENABLED='true',
                   PROMETHEUS_SERVICE_CLIENT_SECRET=self.secret,
                   MONITOR_METRICS_ALLOWED_CIDRS='192.0.2.0/24', MONITOR_METRICS_ALLOWED_PORTS='9443',
                   MONITOR_METRICS_CA_FILE=str(self.directory / 'source-ca.crt'),
                   MONITOR_METRICS_CLIENT_CERT_FILE=str(self.directory / 'collector.crt'),
                   MONITOR_METRICS_CLIENT_KEY_FILE=str(self.directory / 'collector.key'))
        command = ['docker', 'compose', '--env-file', '/dev/null', '-f', 'docker-compose.yml',
                   '-f', 'scripts/prod/metrics-platform.yml', 'config', '--format', 'json']
        result = subprocess.run(command, cwd=ROOT, env=env, capture_output=True, text=True, timeout=15)
        self.assertEqual(result.returncode, 0, 'Platform Compose render failed')
        services = json.loads(result.stdout)['services']
        system, monitor = services['system-backend'], services['monitor-backend']
        self.assertEqual(system['environment']['PROMETHEUS_SERVICE_CLIENT_SECRET'], self.secret)
        self.assertEqual(system['environment']['ADDP_OBSERVABILITY_METRICS_ENABLED'], 'true')
        self.assertEqual(monitor['environment']['MONITOR_METRICS_ALLOWED_CIDRS'], '192.0.2.0/24')
        mounts = [mount for mount in monitor['volumes'] if mount['target'].startswith('/etc/addp/metrics/')]
        self.assertEqual(len(mounts), 3)
        self.assertTrue(all(mount['read_only'] and not mount['bind'].get('create_host_path', False) for mount in mounts))
        self.assertNotIn('prometheus', monitor.get('depends_on', {}))


node_spec = importlib.util.spec_from_file_location('node_metrics', ROOT / 'scripts/infra/node-metrics.py')
node_config = importlib.util.module_from_spec(node_spec)
node_spec.loader.exec_module(node_config)


class NodeMetricsDeploymentTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='addp-node-config-')
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        for name in ('ca.crt', 'server.crt', 'server.key'):
            (self.directory / name).write_text(name)
        self.env = dict(ADDP_NODE_METRICS_ENABLED='true',
                        ADDP_NODE_METRICS_TLS_DIR=str(self.directory),
                        ADDP_NODE_METRICS_LISTEN='192.0.2.10:9100')

    def test_disabled_does_not_access_docker_or_validate_source_secrets(self):
        from unittest.mock import patch
        with patch.object(node_config, 'docker_json', side_effect=AssertionError('Docker called')):
            node_config.run('up', {'ADDP_NODE_METRICS_ENABLED': 'false'})
        with self.assertRaises(ValueError):
            node_config.deployment(dict(self.env, ADDP_NODE_METRICS_ENABLED='1'))

    def test_only_canonical_ip_listen_and_source_files_are_admitted(self):
        self.assertEqual(node_config.deployment(self.env), self.directory)
        for listen in ('0.0.0.0:9100', '[::]:9100', ':9100', 'host:9100', '192.0.2.10',
                       '192.0.2.10:09100', '192.0.2.10:65536', '169.254.1.1:9100',
                       '224.0.0.1:9100', '192.0.2.10:9100/metrics'):
            with self.subTest(listen=listen), self.assertRaises(ValueError):
                node_config.deployment(dict(self.env, ADDP_NODE_METRICS_LISTEN=listen))
        extra = self.directory / 'ca.key'
        extra.write_text('do-not-mount')
        with self.assertRaises(ValueError):
            node_config.deployment(self.env)
        extra.unlink()
        (self.directory / 'server.key').unlink()
        (self.directory / 'server.key').symlink_to(self.directory / 'ca.crt')
        with self.assertRaises(ValueError):
            node_config.deployment(self.env)
        with self.assertRaises(ValueError):
            node_config.deployment(dict(self.env, ADDP_NODE_METRICS_TLS_DIR=str(ROOT)))

    def test_environment_selection_uses_engine_facts_and_rejects_unknown_remote_old_kernel(self):
        self.assertEqual(node_config.deployment_runtime('Linux', '6.8.0', 'unix:///var/run/docker.sock', 'Ubuntu 24.04'), 'linux')
        self.assertEqual(node_config.deployment_runtime('Darwin', '6.10.14-linuxkit', 'unix:///Users/owner/docker.sock', 'Docker Desktop'), 'desktop')
        for args in (('Darwin', '6.8.0', 'unix:///var/run/docker.sock', 'Ubuntu'),
                     ('Linux', '6.8.0', 'unix:///var/run/docker.sock', 'Docker Desktop'),
                     ('Windows', '6.8.0', 'unix:///var/run/docker.sock', 'Docker Desktop'),
                     ('Darwin', '5.11.0', 'unix:///var/run/docker.sock', 'Docker Desktop'),
                     ('Darwin', '6.10.0', 'ssh://owner@node', 'Docker Desktop'),
                     ('Linux', '6.8.0', 'ssh://owner@node', 'Ubuntu'),
                     ('Linux', '5.11.0', 'unix:///var/run/docker.sock', 'Ubuntu')):
            with self.subTest(args=args), self.assertRaises(ValueError):
                node_config.deployment_runtime(*args)

    def test_context_precedence_ownership_and_certificate_independent_stop(self):
        from unittest.mock import patch
        context = [{'Endpoints': {'docker': {'Host': 'ssh://owner@remote'}}}]
        values = dict(self.env, DOCKER_CONTEXT='remote', DOCKER_HOST='unix:///var/run/docker.sock')
        with patch.object(node_config, 'docker_json', return_value=context), \
                patch.object(node_config.subprocess, 'run') as execute:
            with self.assertRaises(ValueError):
                node_config.run('up', values)
            execute.assert_not_called()
        local = [{'Endpoints': {'docker': {'Host': 'unix:///var/run/docker.sock'}}}]
        foreign = [{'Config': {'Labels': {'io.addp.node-metrics.owner': '/another/workspace'}}}]
        with patch.object(node_config, 'docker_json', side_effect=[local, foreign]), \
                patch.object(node_config.subprocess, 'check_output', return_value='other-container'), \
                patch.object(node_config.subprocess, 'run') as execute:
            with self.assertRaises(ValueError):
                node_config.run('down', {})
            execute.assert_not_called()
        with patch.object(node_config, 'docker_json', return_value=local), \
                patch.object(node_config.subprocess, 'check_output', return_value=''), \
                patch.object(node_config.subprocess, 'run') as execute:
            node_config.run('down', {'ADDP_NODE_METRICS_ENABLED': 'false'})
            self.assertEqual(execute.call_args.args[0][-1], 'down')
            self.assertNotIn('--volumes', execute.call_args.args[0])

    def test_native_launch_overrides_untrusted_root_paths_and_profiles(self):
        from unittest.mock import patch
        values = dict(self.env, DOCKER_HOST='unix:///var/run/docker.sock',
                      ADDP_NODE_METRICS_ROOTFS='/wrong', ADDP_NODE_METRICS_NAMESPACED_COLLECTOR_PREFIX='--no-collector',
                      COMPOSE_PROFILES='unexpected')
        info = {'KernelVersion': '6.8.0', 'OperatingSystem': 'Ubuntu', 'OSType': 'linux'}
        with patch.object(node_config, 'docker_json', return_value=info), \
                patch.object(node_config.platform, 'system', return_value='Linux'), \
                patch.object(node_config.platform, 'release', return_value='6.8.0'), \
                patch.object(node_config.subprocess, 'check_output', return_value=''), \
                patch.object(node_config.subprocess, 'run') as execute:
            node_config.run('up', values)
            self.assertIn('--force-recreate', execute.call_args.args[0])
            env = execute.call_args.kwargs['env']
            self.assertEqual(env['ADDP_NODE_METRICS_ROOTFS'], '/host')
            self.assertEqual(env['ADDP_NODE_METRICS_PROCFS'], '/host/proc')
            self.assertEqual(env['ADDP_NODE_METRICS_NAMESPACED_COLLECTOR_PREFIX'], '--collector')
            self.assertNotIn('COMPOSE_PROFILES', env)

    def test_desktop_launch_overrides_paths_collectors_and_maps_explicit_ipv6_port(self):
        from unittest.mock import patch
        values = dict(self.env, DOCKER_HOST='unix:///Users/owner/docker.sock',
                      ADDP_NODE_METRICS_LISTEN='[::1]:19100',
                      ADDP_NODE_METRICS_ROOTFS='/host', ADDP_NODE_METRICS_PROCFS='/host/proc',
                      ADDP_NODE_METRICS_NAMESPACED_COLLECTOR_PREFIX='--collector', ADDP_NODE_METRICS_PUBLISH_PORT='9999',
                      COMPOSE_PROFILES='unexpected')
        info = {'KernelVersion': '6.10.14-linuxkit', 'OperatingSystem': 'Docker Desktop', 'OSType': 'linux'}
        with patch.object(node_config, 'docker_json', return_value=info), \
                patch.object(node_config.platform, 'system', return_value='Darwin'), \
                patch.object(node_config.subprocess, 'check_output', return_value=''), \
                patch.object(node_config.subprocess, 'run') as execute:
            node_config.run('up', values)
            self.assertIn(str(ROOT / 'scripts/infra/node-metrics-desktop.yml'), execute.call_args.args[0])
            env = execute.call_args.kwargs['env']
            self.assertEqual(env['ADDP_NODE_METRICS_LISTEN'], ':9100')
            self.assertEqual(env['ADDP_NODE_METRICS_PUBLISH_IP'], '::1')
            self.assertEqual(env['ADDP_NODE_METRICS_PUBLISH_PORT'], '19100')
            self.assertEqual(env['ADDP_NODE_METRICS_ROOTFS'], '/')
            self.assertEqual(env['ADDP_NODE_METRICS_PROCFS'], '/proc')
            self.assertEqual(env['ADDP_NODE_METRICS_SYSFS'], '/sys')
            self.assertEqual(env['ADDP_NODE_METRICS_NAMESPACED_COLLECTOR_PREFIX'], '--no-collector')
            self.assertNotIn('COMPOSE_PROFILES', env)

    def test_desktop_stop_does_not_require_valid_listen_or_certificates(self):
        from unittest.mock import patch
        context = [{'Endpoints': {'docker': {'Host': 'unix:///Users/owner/docker.sock'}}}]
        with patch.object(node_config, 'docker_json', return_value=context), \
                patch.object(node_config.platform, 'system', return_value='Darwin'), \
                patch.object(node_config.subprocess, 'check_output', return_value=''), \
                patch.object(node_config.subprocess, 'run') as execute:
            node_config.run('down', {'ADDP_NODE_METRICS_LISTEN': 'invalid', 'ADDP_NODE_METRICS_TLS_DIR': '/missing'})
            self.assertEqual(execute.call_args.args[0][-1], 'down')
            self.assertNotIn('--volumes', execute.call_args.args[0])

    def test_native_kernel_mismatch_and_non_linux_engine_do_not_launch(self):
        from unittest.mock import patch
        for system, engine_os, kernel, kind in (
                ('Linux', 'Ubuntu', '6.8.1', 'linux'),
                ('Darwin', 'Docker Desktop', '6.10.14-linuxkit', 'windows')):
            with self.subTest(system=system), \
                    patch.object(node_config, 'docker_json', return_value={
                        'OperatingSystem': engine_os, 'KernelVersion': kernel, 'OSType': kind}), \
                    patch.object(node_config.platform, 'system', return_value=system), \
                    patch.object(node_config.platform, 'release', return_value='6.8.0'), \
                    patch.object(node_config.subprocess, 'run') as execute:
                with self.assertRaises(ValueError):
                    node_config.run('up', dict(self.env, DOCKER_HOST='unix:///var/run/docker.sock'))
                execute.assert_not_called()

    def test_desktop_compose_has_only_kernel_global_collectors_and_no_host_privileges(self):
        env = dict(os.environ, **self.env)
        env.update(ADDP_NODE_METRICS_OWNER=str(ROOT),
                   ADDP_NODE_METRICS_LISTEN=':9100', ADDP_NODE_METRICS_PUBLISH_IP='127.0.0.1',
                   ADDP_NODE_METRICS_PUBLISH_PORT='19100', ADDP_NODE_METRICS_NAMESPACED_COLLECTOR_PREFIX='--no-collector',
                   ADDP_NODE_METRICS_ROOTFS='/', ADDP_NODE_METRICS_PROCFS='/proc',
                   ADDP_NODE_METRICS_SYSFS='/sys')
        value = subprocess.run(['docker', 'compose', '--env-file', '/dev/null',
                                '-f', str(ROOT / 'scripts/infra/node-metrics.yml'),
                                '-f', str(ROOT / 'scripts/infra/node-metrics-desktop.yml'),
                                'config', '--format', 'json'], env=env, capture_output=True,
                               text=True, check=True, timeout=15)
        services = json.loads(value.stdout)['services']
        self.assertEqual(set(services), {'node-exporter'})
        source = services['node-exporter']
        self.assertNotEqual(source.get('network_mode'), 'host')
        self.assertNotEqual(source.get('pid'), 'host')
        self.assertFalse(source.get('privileged', False))
        self.assertEqual(source['cap_drop'], ['ALL'])
        self.assertEqual(source['security_opt'], ['no-new-privileges:true'])
        self.assertTrue(source['read_only'])
        self.assertEqual(source['user'], '65534:65534')
        self.assertEqual(len(source['ports']), 1)
        self.assertEqual(source['ports'][0]['host_ip'], '127.0.0.1')
        self.assertEqual(source['ports'][0]['published'], '19100')
        self.assertEqual(source['ports'][0]['target'], 9100)
        self.assertEqual(len(source['volumes']), 4)
        self.assertTrue(all(m['read_only'] and m['source'] != '/' for m in source['volumes']))
        for collector in ('filesystem', 'netdev', 'netstat'):
            self.assertIn('--no-collector.' + collector, source['command'])
        for collector in ('cpu', 'meminfo', 'loadavg', 'diskstats', 'stat', 'uname', 'time'):
            self.assertIn('--collector.' + collector, source['command'])

    def test_production_compose_has_sole_host_source_and_readonly_mounts(self):
        env = dict(os.environ, **self.env, ADDP_NODE_METRICS_OWNER=str(ROOT),
                   ADDP_NODE_METRICS_NAMESPACED_COLLECTOR_PREFIX='--collector',
                   ADDP_NODE_METRICS_ROOTFS='/host', ADDP_NODE_METRICS_PROCFS='/host/proc',
                   ADDP_NODE_METRICS_SYSFS='/host/sys')
        value = subprocess.run(['docker', 'compose', '--env-file', '/dev/null',
                                '-f', str(ROOT / 'scripts/infra/node-metrics.yml'),
                                '-f', str(ROOT / 'scripts/infra/node-metrics-linux.yml'),
                                'config', '--format', 'json'], env=env, capture_output=True,
                               text=True, check=True, timeout=15)
        services = json.loads(value.stdout)['services']
        self.assertEqual(set(services), {'node-exporter'})
        source = services['node-exporter']
        self.assertEqual(source['network_mode'], 'host')
        self.assertEqual(source['pid'], 'host')
        self.assertEqual(source['user'], '65534:65534')
        self.assertTrue(source['read_only'])
        self.assertEqual(source['cap_drop'], ['ALL'])
        self.assertEqual(source['cpus'], 0.25)
        self.assertEqual(source['mem_limit'], '268435456')
        self.assertFalse(source.get('privileged', False))
        self.assertNotIn('ports', source)
        volumes = source['volumes']
        self.assertTrue(all(v['read_only'] for v in volumes))
        host = next(v for v in volumes if v['target'] == '/host')
        self.assertEqual(host['source'], '/')
        self.assertEqual(host['bind']['propagation'], 'rslave')
        self.assertEqual(set(v['target'] for v in volumes), {
            '/host', '/etc/addp/node-metrics/web.yml', '/etc/addp/node-metrics/tls/ca.crt',
            '/etc/addp/node-metrics/tls/server.crt', '/etc/addp/node-metrics/tls/server.key'})
        self.assertIn('--collector.disable-defaults', source['command'])
        collectors = {c for c in source['command'] if c.startswith('--collector.')
                      and c != '--collector.disable-defaults' and '=' not in c}
        self.assertEqual(collectors, {'--collector.'+name for name in (
            'cpu', 'meminfo', 'loadavg', 'filesystem', 'diskstats', 'netdev',
            'netstat', 'stat', 'uname', 'time')})
        self.assertIn('--collector.filesystem.mount-points-exclude=^/(dev|proc|run/credentials/.+|sys|var/lib/docker/.+|var/lib/containers/storage/.+)($$|/)', source['command'])
        self.assertIn('--collector.filesystem.fs-types-exclude=^(autofs|binfmt_misc|bpf|cgroup2?|configfs|debugfs|devpts|devtmpfs|fusectl|hugetlbfs|iso9660|mqueue|nsfs|overlay|proc|procfs|pstore|rpc_pipefs|securityfs|selinuxfs|squashfs|erofs|sysfs|tracefs)$$', source['command'])


if __name__ == '__main__':
    unittest.main()
