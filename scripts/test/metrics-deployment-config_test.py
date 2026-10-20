import importlib.util
import io
import json
import os
import re
import sys
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock
from contextlib import redirect_stdout

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('metrics_config', ROOT / 'scripts/infra/generate-metrics-config.py')
config = importlib.util.module_from_spec(spec)
spec.loader.exec_module(config)
source_spec = importlib.util.spec_from_file_location('addp_postgres_source_config', ROOT / 'scripts/infra/postgres-observation/source.py')
postgres_source = importlib.util.module_from_spec(source_spec)
source_spec.loader.exec_module(postgres_source)
auth_spec = importlib.util.spec_from_file_location('addp_postgres_auth_directory', ROOT / 'scripts/infra/postgres-observation/authentication.py')
postgres_auth = importlib.util.module_from_spec(auth_spec)
auth_spec.loader.exec_module(postgres_auth)


class MetricsDeploymentConfigTest(unittest.TestCase):
    def test_http_quality_uses_native_source_and_standard_lifecycle_registration(self):
        probe = (ROOT / 'scripts/test/monitor-metrics-probe.py').read_text()
        selected = re.findall(r"'\^TestIntegrationMetrics\(([^)]+)\)\$'", probe)
        self.assertEqual(len(selected), 1)
        self.assertIn('HTTPWindow', selected[0].split('|'))
        self.assertIn('^TestIntegrationMetricsHTTPRealSource$', probe)
        self.assertIn('^TestIntegrationMetricsHTTPAlertLifecycle$', probe)
        self.assertIn("'ADDP_PROCESS_T2_ROLE': 'backend' if index == 1 else 'worker'", probe)
        gate = (ROOT / 'scripts/test/monitor-metrics-gate.sh').read_text()
        self.assertIn('common/httpmetrics/ common/modulelifecycle/', gate)
        self.assertIn('--case=http-quality) metrics_case=http-quality ;;', gate)
        self.assertIn('export ADDP_METRICS_T2_CASE="$metrics_case"', gate)
        self.assertIn("os.environ.get('ADDP_METRICS_T2_CASE') == 'http-quality'", probe)
        pg_scope = re.search(r'(?ms)^if \[\[ "\$metrics_case" != http-quality \]\]; then\n  export METRICS_T2_POSTGRES_SOURCE_ENABLED=true\n.*?^fi$', gate)
        self.assertIsNotNone(pg_scope)
        self.assertIn('postgres-observation-probe.py', pg_scope.group())
        self.assertIn('postgres_directory_probe', pg_scope.group())
        stub = 'python3(){ printf "probe\\n"; }; postgres_directory_probe(){ printf "directory\\n"; }; metrics_case="$1"\n'
        for case in ('http-quality', 'all', 'postgres-source'):
            completed = subprocess.run(['bash', '-c', stub + pg_scope.group(), '_', case], capture_output=True, text=True, timeout=5)
            self.assertEqual(completed.returncode, 0)
            expected = ['probe'] * pg_scope.group().count('\n  python3 ') + ['directory']
            self.assertEqual(completed.stdout.splitlines(), [] if case == 'http-quality' else expected)

    def test_postgres_auth_directory_reuses_runtime_and_preserves_managed_policy(self):
        directory = self.directory.resolve() / 'authentication'
        baseline = 'local all all trust\nhost all all 0.0.0.0/0 scram-sha-256\n'
        runtime = postgres_auth.prepare_business(directory, baseline)
        hba = runtime / 'pg_hba.conf'
        original_stat = hba.stat()
        policy = postgres_source.authentication_configuration(baseline, 'business', ['172.20.0.0/16'])
        postgres_auth.replace(directory, baseline, policy)
        self.assertEqual(hba.read_text(), policy)
        self.assertEqual((hba.stat().st_uid, hba.stat().st_gid, hba.stat().st_mode),
                         (original_stat.st_uid, original_stat.st_gid, original_stat.st_mode))
        unchanged = hba.stat()
        self.assertEqual(postgres_auth.prepare_business(directory, baseline), runtime)
        self.assertEqual(hba.stat().st_ino, unchanged.st_ino)
        updated = baseline + '# deployment owner updated ordinary-user policy\n'
        postgres_auth.prepare_business(directory, updated)
        self.assertEqual(hba.read_text(), policy[:-len(baseline)] + updated)
        self.assertEqual(directory.stat().st_mode & 0o777, 0o700)
        self.assertEqual(runtime.stat().st_mode & 0o777, 0o755)
        self.assertEqual((directory / 'owner.json').stat().st_mode & 0o777, 0o600)
        self.assertEqual(hba.stat().st_mode & 0o777, 0o644)

    def test_postgres_auth_directory_rejects_stale_inputs_and_external_drift(self):
        directory = self.directory.resolve() / 'authentication'
        baseline = 'local all all trust\n'
        hba = postgres_auth.prepare_business(directory, baseline) / 'pg_hba.conf'
        policy = postgres_source.authentication_configuration(baseline, 'business', ['127.0.0.1/32'])
        postgres_auth.replace(directory, baseline, policy)
        with self.assertRaises(ValueError):
            postgres_auth.replace(directory, baseline, policy)
        self.assertEqual(hba.read_text(), policy)
        with self.assertRaises(ValueError):
            postgres_auth.replace(directory, policy, policy + '# unowned modification\n')
        hba.write_text(policy + '# external operator drift\n')
        with self.assertRaises(ValueError):
            postgres_auth.prepare_business(directory, baseline)
        self.assertTrue(hba.read_text().endswith('# external operator drift\n'))

    def test_postgres_auth_directory_rejects_foreign_workspace_and_scope(self):
        directory = self.directory.resolve() / 'authentication'
        baseline = 'local all all trust\n'
        hba = postgres_auth.prepare_business(directory, baseline) / 'pg_hba.conf'
        with self.assertRaises(ValueError):
            postgres_auth.prepare_business(directory, baseline, workspace=ROOT / 'other-workspace')
        with self.assertRaises(ValueError):
            postgres_auth.replace(directory, baseline, baseline, workspace=ROOT / 'other-workspace')
        hba.write_text(postgres_source.authentication_configuration(baseline, 'infra', ['127.0.0.1/32']))
        with self.assertRaises(ValueError):
            postgres_auth.prepare_business(directory, baseline)

    def test_postgres_auth_directory_rejects_links_modes_and_lock_contention(self):
        directory = self.directory.resolve() / 'authentication'
        baseline = 'local all all trust\n'
        hba = postgres_auth.prepare_business(directory, baseline) / 'pg_hba.conf'
        with postgres_auth.locked(directory):
            with self.assertRaises(ValueError):
                postgres_auth.prepare_business(directory, baseline)
        hba.chmod(0o666)
        with self.assertRaises(ValueError):
            postgres_auth.prepare_business(directory, baseline)
        hba.chmod(0o644)
        extra = self.directory / 'extra-link'
        os.link(hba, extra)
        with self.assertRaises(ValueError):
            postgres_auth.prepare_business(directory, baseline)
        extra.unlink()
        hba.unlink()
        hba.symlink_to(extra)
        with self.assertRaises(OSError):
            postgres_auth.prepare_business(directory, baseline)
        linked = self.directory.resolve() / 'linked'
        linked.symlink_to(directory, target_is_directory=True)
        with self.assertRaises(ValueError):
            postgres_auth.prepare_business(linked, baseline)

    def test_postgres_auth_directory_rejects_unowned_files_and_invalid_declarations(self):
        directory = self.directory.resolve() / 'authentication'
        directory.mkdir(mode=0o700)
        (directory / 'runtime').mkdir(mode=0o755)
        (directory / 'runtime/pg_hba.conf').write_text('unowned')
        with self.assertRaises(ValueError):
            postgres_auth.prepare_business(directory, 'local all all trust\n')
        for value in ('', 'local all addp_business_observer trust\n', '\x00', '#' * postgres_source.HBA_LIMIT):
            with self.subTest(value=value[:25]), self.assertRaises(ValueError):
                postgres_auth.prepare_business(directory, value)
        for path in ('relative', str(ROOT / '.dev-state/postgres-auth')):
            with self.assertRaises(ValueError):
                postgres_auth.state_directory({'ADDP_POSTGRES_AUTH_STATE_DIR': path})

    def test_postgres_auth_directory_interrupted_template_update_fails_closed(self):
        directory = self.directory.resolve() / 'authentication'
        baseline = 'local all all trust\n'
        hba = postgres_auth.prepare_business(directory, baseline) / 'pg_hba.conf'
        changed = baseline + '# next deployment template\n'
        atomic = postgres_auth._atomic

        def interrupt_owner(path, *args, **kwargs):
            if path.name == 'owner.json':
                raise OSError('controlled fixture interruption')
            return atomic(path, *args, **kwargs)

        with mock.patch.object(postgres_auth, '_atomic', side_effect=interrupt_owner):
            with self.assertRaises(OSError):
                postgres_auth.prepare_business(directory, changed)
        self.assertEqual(hba.read_text(), changed)
        with self.assertRaises(ValueError):
            postgres_auth.prepare_business(directory, changed)
        self.assertEqual(hba.read_text(), changed)

    def test_postgres_auth_directory_standard_entrypoint_uses_generated_mount(self):
        directory = self.directory.resolve() / 'authentication'
        environment = dict(os.environ, ADDP_POSTGRES_AUTH_STATE_DIR=str(directory))
        environment.pop('ADDP_BUSINESS_POSTGRES_AUTH_DIR', None)
        command = [sys.executable, str(ROOT / 'scripts/infra/postgres-observation/authentication.py'), 'prepare-business']
        completed = subprocess.run(command, env=environment, capture_output=True, text=True, timeout=5)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertEqual(completed.stdout.strip(), str(directory / 'runtime'))
        environment['ADDP_BUSINESS_POSTGRES_AUTH_DIR'] = str(self.directory)
        refused = subprocess.run(command, env=environment, capture_output=True, text=True, timeout=5)
        self.assertNotEqual(refused.returncode, 0)
        self.assertNotIn(str(self.directory), refused.stderr)
        business = (ROOT / 'business/docker-compose.yml').read_text().split('\n  postgres:\n', 1)[1].split('\n  oracle:', 1)[0]
        self.assertIn('hba_file=/etc/addp-postgres-auth/pg_hba.conf', business)
        self.assertNotIn('./postgres/pg_hba.conf:/etc/postgresql/pg_hba.conf', business)

    def test_business_postgres_compose_mount_is_readonly_and_requires_preparation(self):
        environment = {name: os.environ[name] for name in ('PATH', 'HOME') if name in os.environ}
        environment['ADDP_BUSINESS_POSTGRES_AUTH_DIR'] = str(self.directory.resolve() / 'prepared')
        command = ['docker', 'compose', '--env-file', '/dev/null', '-f', str(ROOT / 'business/docker-compose.yml'),
                   'config', '--format', 'json', 'postgres']
        rendered = subprocess.run(command, env=environment, capture_output=True, text=True, timeout=15)
        self.assertEqual(rendered.returncode, 0, rendered.stderr)
        service = json.loads(rendered.stdout)['services']['postgres']
        mounts = [value for value in service['volumes'] if value['target'] == '/etc/addp-postgres-auth']
        self.assertEqual(len(mounts), 1)
        self.assertEqual(mounts[0]['source'], environment['ADDP_BUSINESS_POSTGRES_AUTH_DIR'])
        self.assertTrue(mounts[0]['read_only'])
        # Compose JSON omits the false long-syntax default; true is explicit.
        self.assertFalse(mounts[0].get('bind', {}).get('create_host_path', False))
        self.assertIn('hba_file=/etc/addp-postgres-auth/pg_hba.conf', service['command'])
        environment.pop('ADDP_BUSINESS_POSTGRES_AUTH_DIR')
        rendered = subprocess.run(command, env=environment, capture_output=True, text=True, timeout=15)
        self.assertEqual(rendered.returncode, 0, rendered.stderr)
        mount = next(value for value in json.loads(rendered.stdout)['services']['postgres']['volumes']
                     if value['target'] == '/etc/addp-postgres-auth')
        self.assertEqual(mount['source'], '/nonexistent/addp-business-postgres-auth')
        self.assertFalse(mount.get('bind', {}).get('create_host_path', False))

    def test_postgres_source_configuration_is_fixed_and_bounded(self):
        source = postgres_source
        infra = source.configuration('infra', 'postgres', 5432, 'a' * 64)
        business = source.configuration('business', 'business-postgres', 5432, 'b' * 64)
        self.assertEqual(infra['collectors'], business['collectors'])
        self.assertNotEqual(infra['target']['data_source_name'], business['target']['data_source_name'])
        self.assertNotIn('name', infra['target'])
        self.assertFalse(infra['target']['enable_ping'])
        self.assertEqual(infra['global']['max_connections'], 1)
        self.assertEqual(infra['global']['min_interval'], '0s')
        for host, port, password in [('127.0.0.1', 5432, 'a' * 64), ('8.8.8.8', 5432, 'a' * 64),
                                     ('postgres/path', 5432, 'a' * 64), ('postgres', True, 'a' * 64),
                                     ('postgres', 5432, 'short')]:
            with self.subTest(host=host, port=port), self.assertRaises(ValueError):
                source.configuration('infra', host, port, password)
        with self.assertRaises(ValueError):
            source.configuration('tenant', 'postgres', 5432, 'a' * 64)

    def test_postgres_authentication_preserves_other_users_and_is_idempotent(self):
        original = '# addp_infra_observer is documented only\nlocal all all trust\nhost all all 0.0.0.0/0 scram-sha-256\nhost "db#name" ordinary_user 10.0.0.0/8 scram-sha-256\n'
        for scope in ('infra', 'business'):
            rendered = postgres_source.authentication_configuration(original, scope, ['172.20.0.0/16', '127.0.0.1/32'])
            self.assertTrue(rendered.endswith(original))
            self.assertEqual(postgres_source.authentication_configuration(rendered, scope, ['127.0.0.1/32', '172.20.0.0/16']), rendered)
            self.assertIn(f'host "postgres" addp_{scope}_observer 172.20.0.0/16 scram-sha-256\n', rendered)
            self.assertIn(f'local all addp_{scope}_observer reject\n', rendered)
            for network in ('0.0.0.0/0', '::/0'):
                self.assertIn(f'host all addp_{scope}_observer {network} reject\n', rendered)
                self.assertIn(f'host replication addp_{scope}_observer {network} reject\n', rendered)
            changed = postgres_source.authentication_configuration(rendered, scope, ['192.168.10.0/24'])
            self.assertTrue(changed.endswith(original))
            self.assertNotIn('172.20.0.0/16', changed)
        literal = postgres_source.authentication_configuration(original, 'infra', ['::1/128', 'fd00::/64'], database='all')
        self.assertIn('host "all" addp_infra_observer ::1/128 scram-sha-256', literal)

    def test_postgres_authentication_rejects_ambiguous_or_excessive_policy(self):
        render = postgres_source.authentication_configuration
        valid = render('local all all trust\n', 'infra', ['172.20.0.0/16'])
        for original in (valid.replace('scram-sha-256', 'trust'), valid.replace('"database":', '"extra":true,"database":'),
                         '# prior operator policy\n'+valid, valid.replace(postgres_source.HBA_END, ''), valid+valid,
                         'local all addp_infra_observer trust\n', 'host all addp_\\\ninfra_observer all trust\n',
                         'host "db#name" addp_infra_observer 10.0.0.0/8 trust\n', 'host "unterminated all all trust\n',
                         '\x00', '#' * postgres_source.HBA_LIMIT):
            with self.subTest(original=original[:50]), self.assertRaises(ValueError):
                render(original, 'infra', ['172.20.0.0/16'])
        for networks in ([], ['0.0.0.0/0'], ['::/0'], ['8.8.8.8/32'], ['169.254.0.0/16'],
                         ['172.20.0.1/16'], ['172.20.0.0/16']*2, ['10.0.0.0/8', '10.1.0.0/16'],
                         ['127.0.0.1'], ['172.20.0.0/16\nlocal all all trust'], [False], ['10.0.0.0/32']*17):
            with self.subTest(networks=networks), self.assertRaises(ValueError):
                render('', 'infra', networks)
        for scope, database in [('tenant', 'postgres'), ('infra', 'postgres\nlocal'), ('infra', '')]:
            with self.assertRaises(ValueError):
                render('', scope, ['172.20.0.0/16'], database=database)
        with self.assertRaises(ValueError):
            render(valid, 'business', ['172.20.0.0/16'])
        with self.assertRaises(ValueError):
            render(valid, 'infra', ['172.20.0.0/16'], database='other')

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
        for path in ('common/processmetrics/sample_darwin.go',
                     'scripts/infra/postgres-observation/000001_init.sql',
                     'scripts/infra/postgres-observation/000002_lifecycle.sql',
                     'scripts/infra/postgres-observation/lifecycle.py',
                     'scripts/test/postgres-metrics-lifecycle_test.py',
                     'scripts/test/postgres-lifecycle-probe.py',
                     'business/scripts/stop.sh',
                     'scripts/test/postgres-observation-probe.py',
                     'scripts/infra/postgres-metrics.yml',
                     'scripts/infra/postgres-observation/source.py',
                     'scripts/infra/postgres-observation/source-web.conf',
                     'scripts/dev/process-metrics-launch.py',
                     'scripts/test/dev-process-metrics-plan_test.py',
                     'common/processmetrics/sample_linux.go',
                     'common/config/process_metrics.go',
                     'common/models/process_metrics.go',
                     'common/client/system_service.go',
                     'common/client/system_runtime_instances.go',
                     'common/client/system_observability_identity.go',
                     'common/client/system_host_node.go',
                     'common/client/system_host_node_scope.go',
                     'common/models/module_runtime_query.go',
                     'monitor/backend/internal/metricsdiscovery/node_projection.go',
                     'monitor/backend/internal/resourcequery/process.go',
                     'monitor/backend/internal/resourcequery/client.go',
                     'monitor/backend/internal/resourcequery/device_counter.go',
                     'monitor/backend/internal/resourcequery/disk_test.go',
                     'monitor/backend/internal/resourcequery/disk_io_test.go',
                     'monitor/backend/internal/resourcequery/deleted-or-future.go',
                     'monitor/backend/internal/config/config.go',
                     'monitor/backend/internal/service/resource_observation.go',
                     'monitor/backend/internal/service/resource_observation_test.go',
                     'monitor/backend/internal/service/resource_summary.go',
                     'monitor/backend/internal/service/resource_alert_evaluation.go',
                     'monitor/backend/internal/service/resource_pressure_evaluator.go',
                     'monitor/backend/internal/service/resource_pressure_prometheus_test.go',
                     'monitor/backend/internal/service/resource_alert_lifecycle_prometheus_test.go',
                     'monitor/backend/internal/service/platform_log_retry_postgres_test.go',
                     'monitor/backend/internal/service/platform_alert_notifications.go',
                     'monitor/backend/internal/service/webhook_sender.go',
                     'monitor/backend/internal/models/resource_alert_rule.go',
                     'monitor/backend/internal/models/resource_query_policy.go',
                     'monitor/backend/internal/repository/resource_query_policy.go',
                     'monitor/backend/internal/service/resource_overview.go',
                     'monitor/backend/internal/service/resource_overview_test.go',
                     'monitor/backend/internal/service/resource_overview_prometheus_test.go',
                     'monitor/backend/internal/service/monitoring_target_test.go',
                     'scripts/prod/metrics-query.yml',
                     'scripts/infra/node-metrics-desktop.yml'):
            self.assertTrue(gate_module.gate_input_covers(inputs, path), path)
        workflow = (ROOT / '.github/workflows/release-and-t2-gates.yml').read_text()
        job = re.search(r'^  monitor-metrics:\n(.*?)(?=^  \S|\Z)', workflow, re.M | re.S).group(1)
        self.assertIn('uses: actions/setup-go@', job)
        self.assertIn('go-version-file: common/go.mod', job)
        self.assertLess(job.index('uses: actions/setup-go@'), job.index('run: make test-monitor-metrics'))
        probe = (ROOT / 'scripts/test/monitor-metrics-probe.py').read_text()
        selected = re.findall(r"'\^TestIntegrationMetrics\(([^)]+)\)\$'", probe)
        self.assertEqual(len(selected), 1)
        self.assertTrue({'CPUWindow', 'ProcessWindow', 'DiskWindow', 'DiskIOWindow', 'NetworkWindow',
                         'Filesystem', 'FilesystemInodes'}.issubset(set(selected[0].split('|'))))
        self.assertIn("'^TestIntegrationMetricsResourceOverview$'", probe)
        self.assertEqual(probe.count("'^TestIntegrationMetricsResourcePressureEvidence$'"), 2)
        self.assertIn("'^TestIntegrationMetricsResourceAlertLifecycle$'", probe)
        self.assertIn('postgres', re.search(r'^# ADDP_T2_OWNED_SERVICES=(.+)$', script, re.M).group(1).split(','))

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

    def test_desktop_loopback_rules_keep_the_original_identity_and_exact_ports(self):
        env = dict(self.env, ADDP_METRICS_DESKTOP_LOOPBACK_PORTS='19102,19091,19101')
        replies = [json.dumps([{'Endpoints': {'docker': {'Host': 'unix:///local/docker.sock'}}}]),
                   json.dumps({'OperatingSystem': 'Docker Desktop', 'OSType': 'linux'})]
        with mock.patch.object(config.platform, 'system', return_value='Darwin'), mock.patch.object(config.subprocess, 'check_output', side_effect=replies):
            config.render(env)
        data = (self.directory / 'prometheus.yml').read_text()
        self.assertIn('target_label: instance', data)
        for port in ('19091', '19101', '19102'):
            self.assertIn('host.docker.internal:' + port, data)
            self.assertIn('127\\\\.0\\\\.0\\\\.1:' + port, data)
        self.assertEqual(data.count('target_label: instance'), 1)
        self.assertEqual(data.count('target_label: __address__'), 3)
        rules = [json.loads(line.strip().removeprefix('regex: ')) for line in data.splitlines()
                 if line.strip().startswith('regex: "127')]
        for address in ('127.0.0.1:19091', '127.0.0.1:19101', '127.0.0.1:19102'):
            self.assertEqual(sum(re.fullmatch(rule, address) is not None for rule in rules), 1)
        for address in ('127.0.0.1:19103', '127.0.0.11:19101', '192.168.1.33:19101',
                        '[::1]:19101', 'host.docker.internal:19101', '127.0.0.1:191010'):
            self.assertFalse(any(re.fullmatch(rule, address) for rule in rules), address)
        self.assertEqual(data.count('job_name: addp_nodes'), 1)
        self.assertNotIn('server_name:', data)
        before = data
        with mock.patch.object(config.platform, 'system', return_value='Darwin'), mock.patch.object(config.subprocess, 'check_output', side_effect=replies):
            config.render(dict(env, ADDP_METRICS_DESKTOP_LOOPBACK_PORTS='19091,19101,19102'))
        self.assertEqual((self.directory / 'prometheus.yml').read_text(), before)
        for ports in ('019091', '0', '65536', '19091\n', '19091:123', '19091,19091',
                      ',19091', '19091,', '19091,,19101', '19091, 19101',
                      ','.join(str(i) for i in range(1, 66))):
            with self.subTest(ports=ports), self.assertRaises(ValueError):
                config.render(dict(env, ADDP_METRICS_DESKTOP_LOOPBACK_PORTS=ports))
            self.assertEqual((self.directory / 'prometheus.yml').read_text(), before)
        with mock.patch.object(config.platform, 'system', return_value='Linux'), self.assertRaises(ValueError): config.render(env)
        with mock.patch.object(config.platform, 'system', return_value='Darwin'), self.assertRaises(ValueError): config.render(dict(env, DOCKER_HOST='tcp://remote:2376'))
        self.assertEqual((self.directory / 'prometheus.yml').read_text(), before)
        config.render(self.env)
        self.assertNotIn('relabel_configs:\n      - source_labels: [__address__]', (self.directory / 'prometheus.yml').read_text())

    def test_desktop_transport_accepts_64_ports_and_rejects_non_desktop_engine(self):
        env = dict(self.env, ADDP_METRICS_DESKTOP_LOOPBACK_PORTS=','.join(str(i) for i in range(1, 65)))
        replies = [json.dumps([{'Endpoints': {'docker': {'Host': 'unix:///local/docker.sock'}}}]),
                   json.dumps({'OperatingSystem': 'Docker Desktop', 'OSType': 'linux'})]
        with mock.patch.object(config.platform, 'system', return_value='Darwin'), mock.patch.object(config.subprocess, 'check_output', side_effect=replies):
            config.render(env)
        before = (self.directory / 'prometheus.yml').read_text()
        self.assertEqual(before.count('target_label: __address__'), 64)
        for info in ({'OperatingSystem': 'Linux', 'OSType': 'linux'},
                     {'OperatingSystem': 'Docker Desktop', 'OSType': 'windows'}):
            with mock.patch.object(config.platform, 'system', return_value='Darwin'), mock.patch.object(config.subprocess, 'check_output', return_value=json.dumps(info)), self.assertRaises(ValueError):
                config.render(dict(env, DOCKER_HOST='unix:///local/docker.sock'))
            self.assertEqual((self.directory / 'prometheus.yml').read_text(), before)

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

    def test_admission_export_is_a_secret_free_proposal_without_host_identity(self):
        value = node_config.admission_document(dict(self.env, PASSWORD='secret-never-export', ADDP_HOST_NODE_ID='not-this-vm'), 'desktop')
        self.assertEqual(value, {'schema': 'addp.host-admission/v2', 'host': {
            'display_name': 'Docker Desktop Linux VM', 'node_kind': 'virtual', 'addresses': [], 'allowed_module_bindings': []},
            'collection': {'monitor_kind': 'host_resources', 'source': {'type': 'node_exporter', 'endpoint': 'https://192.0.2.10:9100/metrics'}}})
        serialized = json.dumps(value)
        for forbidden in ('secret-never-export', 'not-this-vm', str(self.directory), 'server.key', 'node_id', 'PASSWORD'):
            self.assertNotIn(forbidden, serialized)
        value = node_config.admission_document(self.env, 'linux', 'physical', 'Linux host', ['192.0.2.10', 'host.example'])
        self.assertEqual(value['host']['addresses'], ['192.0.2.10', 'host.example'])
        self.assertEqual(value['host']['node_kind'], 'physical')

    def test_admission_host_kind_and_attributes_must_be_explicit_and_bounded(self):
        for arguments in [(self.env, 'desktop', 'physical'), (self.env, 'linux'), (self.env, 'other', 'virtual'),
                          (self.env, 'desktop', 'virtual', ''), (self.env, 'linux', 'physical', ''),
                          (self.env, 'linux', 'virtual', 'name\ncontrol'), (self.env, 'linux', 'physical', 'x' * 256),
                          (self.env, 'linux', 'physical', 'host', ['same', 'same']),
                          (self.env, 'linux', 'physical', 'host', ['https://not-an-address']),
                          (self.env, 'linux', 'physical', 'host', [f'host-{i}' for i in range(33)])]:
            with self.subTest(arguments=arguments[1:]), self.assertRaises(ValueError):
                node_config.admission_document(*arguments)

    def test_native_host_only_export_has_no_docker_or_collection_dependency(self):
        output = io.StringIO()
        with mock.patch.object(node_config.platform, 'system', return_value='Darwin'), mock.patch.object(node_config, 'docker_json', side_effect=AssertionError('Docker called')), redirect_stdout(output):
            node_config.run('export-admission', {'PASSWORD': 'never-export'}, host_only=True, node_kind='physical', display_name='Native Mac', modules=['monitor', 'meta'])
        value = json.loads(output.getvalue())
        self.assertEqual(value['schema'], 'addp.host-admission/v2')
        self.assertNotIn('collection', value)
        self.assertEqual(value['host']['allowed_module_bindings'], [{'module_name': n, 'client_id': 'addp-'+n} for n in ['monitor', 'meta']])
        self.assertNotIn('never-export', output.getvalue())
        for modules in (['monitor', 'monitor'], ['bad/name'], ['x'+str(i) for i in range(65)]):
            with self.assertRaises(ValueError): node_config.admission_document({}, 'native', 'physical', modules=modules)
        with self.assertRaises(ValueError): node_config.run('up', {}, host_only=True)

    def test_admission_export_only_reads_the_owned_local_running_source(self):
        context = [{'Endpoints': {'docker': {'Host': 'unix:///local/docker.sock'}}}]
        info = {'KernelVersion': '6.10.0-linuxkit', 'OperatingSystem': 'Docker Desktop', 'OSType': 'linux'}
        item = {'Config': {'Labels': {'io.addp.node-metrics.owner': str(ROOT), 'com.docker.compose.service': 'node-exporter'}}, 'State': {'Running': True}}
        for change in (None, 'owner', 'stopped', 'service'):
            inspected = json.loads(json.dumps(item))
            if change == 'owner': inspected['Config']['Labels']['io.addp.node-metrics.owner'] = '/another-workspace'
            if change == 'stopped': inspected['State']['Running'] = False
            if change == 'service': inspected['Config']['Labels']['com.docker.compose.service'] = 'unrelated'
            output = io.StringIO()
            with self.subTest(change=change), mock.patch.object(node_config.platform, 'system', return_value='Darwin'), \
                    mock.patch.object(node_config, 'docker_json', side_effect=[context, info, [inspected]]), \
                    mock.patch.object(node_config.subprocess, 'check_output', return_value='source-id\n'), \
                    mock.patch.object(node_config.subprocess, 'run') as mutate, redirect_stdout(output):
                if change is None:
                    node_config.run('export-admission', self.env)
                    self.assertEqual(json.loads(output.getvalue()), node_config.admission_document(self.env, 'desktop'))
                else:
                    with self.assertRaises(ValueError): node_config.run('export-admission', self.env)
                    self.assertEqual(output.getvalue(), '')
                mutate.assert_not_called()
        with mock.patch.object(node_config, 'docker_json') as docker:
            for env in ({'ADDP_NODE_METRICS_ENABLED': 'false'}, dict(self.env, DOCKER_HOST='ssh://remote')):
                with self.assertRaises(ValueError): node_config.run('export-admission', env)
            docker.assert_not_called()

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
