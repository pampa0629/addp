import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

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

    def test_platform_certificate_mounts_are_selected_only_with_complete_inputs(self):
        source = (ROOT / 'scripts/prod/start.sh').read_text()
        phase = source.split('bash scripts/prod/wait-infra.sh\n', 1)[1].split('# Containers use', 1)[0]
        script = ('source scripts/utils/observability-env.sh\n'
                  'PLATFORM_COMPOSE_FILES=(-f docker-compose.yml)\ninfra_result=0\n'
                  + phase + '\nprintf "%s\\n" "$infra_result" "${PLATFORM_COMPOSE_FILES[*]}"\n')
        env = {key: value for key, value in os.environ.items() if not key.startswith(('ADDP_METRICS_', 'MONITOR_METRICS_'))}
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


if __name__ == '__main__':
    unittest.main()
