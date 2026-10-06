import ast
import json
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
KEYS = ("LOKI_READ_TOKEN", "LOKI_WRITE_TOKEN", "LOKI_S3_SECRET_KEY", "LOG_OBSERVER_SERVICE_CLIENT_SECRET")


class MetricsProbeReadinessTest(unittest.TestCase):
    def readiness(self, samples):
        source = ast.parse((ROOT / 'scripts/test/monitor-metrics-probe.py').read_text())
        functions = ast.Module(body=[node for node in source.body
                                     if isinstance(node, ast.FunctionDef)
                                     and node.name in ('job_is_up', 'self_sample_ready')],
                               type_ignores=[])
        namespace = {'query': lambda expression: samples.get(expression, [])}
        exec(compile(functions, 'monitor-metrics-probe.py', 'exec'), namespace)
        return namespace['self_sample_ready']

    def test_fixture_first_sample_does_not_prove_center_self_scrape(self):
        check = self.readiness({'up{job="fixture"}': [{'value': [1, '1']}]})
        self.assertFalse(check())

    def test_self_scrape_requires_up_and_positive_memory(self):
        samples = {}
        check = self.readiness(samples)
        self.assertFalse(check())
        samples['up{job="prometheus"}'] = [{'value': [1, '1']}]
        self.assertFalse(check())
        samples['process_resident_memory_bytes{job="prometheus"}'] = [{'value': [1, '0']}]
        self.assertFalse(check())
        samples['process_resident_memory_bytes{job="prometheus"}'] = [{'value': [1, '4096']}]
        self.assertTrue(check())
        samples['up{job="prometheus"}'] = [{'value': [1, '0']}]
        self.assertFalse(check())


class InfraRuntimeLogLifecycleTest(unittest.TestCase):
    def test_meilisearch_target_has_fixed_digest_and_independent_versioned_volume(self):
        compose = (ROOT / 'docker-compose.infra.yml').read_text()
        service = compose.split('\n  meilisearch:\n', 1)[1].split('\n  redpanda:\n', 1)[0]
        self.assertIn('getmeili/meilisearch:v1.54.3@sha256:e68913ab7d6f5b159529e472cfd362ce3c741fafd3c127961b2142abbe41b3c9', service)
        self.assertIn('meilisearch_data_v1543:/meili_data', service)
        self.assertNotIn('meilisearch_data:/meili_data', service)
        self.assertIn('\n  meilisearch_data_v1543:', compose.split('\nvolumes:\n', 1)[1])
        self.assertNotIn('\n  meilisearch_data:', compose.split('\nvolumes:\n', 1)[1])

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="addp-infra-log-lifecycle-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.repo = self.root / "repository"
        shutil.copytree(ROOT / "scripts/infra", self.repo / "scripts/infra")
        shutil.copytree(ROOT / "scripts/utils", self.repo / "scripts/utils")
        self.bin = self.root / "bin"
        self.bin.mkdir()
        docker = self.bin / "docker"
        docker.write_text("#!/bin/sh\nexit 44\n")
        docker.chmod(0o755)
        self.secrets = self.root / "secrets"
        self.secrets.mkdir(mode=0o700)
        self.envfile = self.secrets / "runtime.env"

    def run_up(self, **values):
        env = dict(os.environ)
        for key in (*KEYS, "LOKI_S3_ACCESS_KEY", "ADDP_HOST_NODE_NAME", "ADDP_RUNTIME_LOG_OWNER", "ADDP_ONLINE_ENV_FILE", "ADDP_ONLINE_HOST", "ADDP_OBSERVABILITY_LOGS_ENABLED", "ADDP_OBSERVABILITY_METRICS_ENABLED", "ADDP_METRICS_TLS_DIR", "COMPOSE_PROFILES"):
            env.pop(key, None)
        env.update(PATH=f"{self.bin}:{env['PATH']}", ENV="development", INFRA_FALKORDB_PASSWORD="graph-test", REDIS_PASSWORD="redis-test")
        env.update(values)
        return subprocess.run(["bash", "scripts/infra/up.sh"], cwd=self.repo, env=env,
                              capture_output=True, text=True, timeout=10)

    def healthy_docker(self):
        self.commands = self.root / "commands.jsonl"
        docker = self.bin / "docker"
        docker.write_text("#!/usr/bin/env python3\n" + r'''import json, os, sys
from pathlib import Path
a = sys.argv[1:]
with open(os.environ['MOCK_COMMANDS'], 'a') as f: f.write(json.dumps(a)+'\n')
if a[0]=='compose' and any(x in a for x in ('postgres','runtime-log-pruner')):
    if any(os.environ.get(k) != '0' for k in ('LOKI_PORT', 'ALLOY_PORT', 'PROMETHEUS_PORT')): sys.exit(81)
if a[0]=='ps':
    if os.environ.get('MOCK_MEILI_ABSENT')!='1': print('fixture-meili-container')
elif a[0]=='image' and '--format' in a:
    if os.environ.get('MOCK_MEILI_IMAGE_MISSING')=='1': sys.exit(1)
    print('sha256:'+'a'*64)
elif a[0]=='inspect':
    container=a[-1]
    service=container.removeprefix('addp-')
    logs=service in ('loki','alloy','runtime-log-api','runtime-log-observer','runtime-log-store-init')
    if logs and os.environ.get('MOCK_EXISTING_LOGS')!='1': sys.exit(1)
    if service=='prometheus' and os.environ.get('MOCK_EXISTING_METRICS')!='1' and not Path(os.environ['MOCK_COMMANDS']).with_suffix('.metrics-started').exists(): sys.exit(1)
    if '--format' in a:
        fmt=a[a.index('--format')+1]
        if 'com.docker.compose.project' in fmt:
            root=str(Path.cwd()) if os.environ.get('MOCK_FOREIGN_LOGS')!='1' or not logs else '/foreign'
            if service=='prometheus' and os.environ.get('MOCK_FOREIGN_METRICS')=='1': root='/foreign'
            print('addp-infra|'+service+'|'+root)
        elif 'ExitCode' in fmt: print('exited:0')
        elif 'State.Running' in fmt: print('true:healthy' if 'Health' in fmt else 'true')
        elif 'Health' in fmt: print('healthy')
        elif fmt=='{{.Image}}': print(os.environ.get('MOCK_MEILI_IMAGE_ID','sha256:'+'a'*64))
elif a[0]=='port':
    internal=a[2].split('/')[0]
    print('127.0.0.1:'+{'5432':'15432','6379':('16479' if a[1]=='addp-falkordb' else '16379'),'9000':'19000','9001':'19001','7700':'17700','9092':'19092','8083':'18083','3100':'13100','12345':'12345','9090':'19090'}[internal])
elif a[0]=='compose':
    if 'ps' in a and '-aq' in a:
        key='MOCK_EXISTING_METRICS' if a[-1]=='prometheus' else 'MOCK_EXISTING_LOGS'
        if os.environ.get(key)=='1': print('addp-'+a[-1])
    if 'up' in a and 'prometheus' in a and os.environ.get('MOCK_FAIL_METRICS_START')=='1': sys.exit(72)
    if 'up' in a and 'prometheus' in a: Path(os.environ['MOCK_COMMANDS']).with_suffix('.metrics-started').touch()
    if 'build' in a and 'runtime-log-observer' in a and os.environ.get('MOCK_FAIL_LOG_BUILD')=='1': sys.exit(71)
    if 'exec' in a and 'redis' in a: print('PONG')
    elif 'config' in a and '--images' in a: print('fixture-owned-image')
''')
        docker.chmod(0o755)
        curl = self.bin / "curl"
        curl.write_text('#!/bin/sh\nexit 0\n')
        curl.chmod(0o755)
        return dict(MOCK_COMMANDS=str(self.commands), SKIP_POSTGRESQL_INIT="1", SKIP_MINIO_INIT="1", SKIP_MEILISEARCH_INIT="1")

    def calls(self):
        return [json.loads(line) for line in self.commands.read_text().splitlines()]

    def test_existing_meilisearch_image_change_is_rejected_before_any_container_mutation(self):
        result = self.run_up(**self.healthy_docker(),
                             MOCK_MEILI_IMAGE_ID='sha256:'+'b'*64)
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('Meilisearch 镜像身份不一致', result.stderr)
        self.assertFalse(any('up' in a or 'build' in a or 'pull' in a
                             or 'stop' in a or 'rm' in a for a in self.calls()))

    def test_existing_meilisearch_unknown_image_is_not_implicitly_pulled_or_replaced(self):
        for values in ({'MOCK_MEILI_IMAGE_MISSING': '1'},
                       {'MOCK_MEILI_IMAGE_ID': ''}):
            with self.subTest(values=values):
                result = self.run_up(**self.healthy_docker(), **values)
                self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn('无法核实 Meilisearch 镜像身份', result.stderr)
                self.assertFalse(any('up' in a or 'build' in a or 'pull' in a
                                     for a in self.calls()))

    def test_existing_meilisearch_migration_refusal_keeps_healthy_core_available_to_apps(self):
        result = self.dev_start_phase(foreign=False,
                                     MOCK_MEILI_IMAGE_ID='sha256:'+'b'*64)
        self.assertIn('Meilisearch 镜像身份不一致', result.stderr)
        self.assertIn('BUSINESS_START_REACHED', result.stdout, result.stderr)
        self.assertFalse(any('up' in a or 'build' in a or 'pull' in a
                             or 'stop' in a or 'rm' in a for a in self.calls()))

    def test_disabled_central_logs_need_no_credentials_and_keep_local_pruner(self):
        values = self.healthy_docker()
        result = self.run_up(**values, ADDP_OBSERVABILITY_LOGS_ENABLED="false", ADDP_HOST_NODE_NAME="fixture-node", LOKI_PORT="invalid", ALLOY_PORT="invalid", MOCK_EXISTING_LOGS="1")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertFalse((self.repo / '.env').exists())
        calls = self.calls()
        core = next(a for a in calls if 'up' in a and 'postgres' in a)
        self.assertNotIn('loki', core)
        self.assertTrue(any('up' in a and 'runtime-log-pruner' in a for a in calls))
        for service in ('loki', 'alloy', 'runtime-log-api', 'runtime-log-observer', 'runtime-log-store-init'):
            self.assertTrue(any('stop' in a and service in a for a in calls))
            self.assertFalse(any(('up' in a or 'build' in a) and service in a for a in calls))

    def test_invalid_log_configuration_reports_failure_after_core_start(self):
        values = self.healthy_docker()
        result = self.run_up(**values, ENV="production", ADDP_HOST_NODE_NAME="fixture-node", ADDP_OBSERVABILITY_LOGS_ENABLED="true", LOKI_READ_TOKEN="short")
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn('core services remain running', result.stderr)
        self.assertTrue(any('up' in a and 'postgres' in a for a in self.calls()))
        self.assertFalse(any('up' in a and 'loki' in a for a in self.calls()))

    def test_log_image_failure_does_not_stop_core(self):
        values = self.healthy_docker()
        result = self.run_up(**values, MOCK_FAIL_LOG_BUILD="1")
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertTrue(any('up' in a and 'postgres' in a for a in self.calls()))
        self.assertFalse(any('stop' in a or 'down' in a for a in self.calls()))

    def test_disabled_logs_never_stop_foreign_containers(self):
        values = self.healthy_docker()
        result = self.run_up(**values, ADDP_OBSERVABILITY_LOGS_ENABLED="false", MOCK_EXISTING_LOGS="1", MOCK_FOREIGN_LOGS="1")
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertTrue(any('up' in a and 'postgres' in a for a in self.calls()))
        self.assertFalse(any('stop' in a for a in self.calls()))

    def test_profiles_follow_explicit_independent_selection(self):
        for logs, metrics, expected in (
                ('false', 'false', ''), ('true', 'false', 'observability-logs'),
                ('false', 'true', 'observability-metrics'),
                ('true', 'true', 'observability-logs,observability-metrics')):
            with self.subTest(logs=logs, metrics=metrics):
                env = dict(os.environ, ADDP_OBSERVABILITY_LOGS_ENABLED=logs,
                           ADDP_OBSERVABILITY_METRICS_ENABLED=metrics, COMPOSE_PROFILES='unselected')
                result = subprocess.run(['bash', '-c',
                    'source scripts/utils/observability-env.sh; addp_observability_profiles; printf "%s" "${COMPOSE_PROFILES-}"'],
                    cwd=ROOT, env=env, capture_output=True, text=True, timeout=5)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(result.stdout, expected)

    def metrics_certificates(self):
        directory = self.root / 'tls'
        directory.mkdir()
        for name in ('ca.crt', 'server.crt', 'server.key', 'health.crt', 'health.key'):
            (directory / name).write_text('fixture-only')
        return str(directory)

    def test_disabled_metrics_ignore_bad_ports_and_certificates_and_preserve_volume(self):
        result = self.run_up(**self.healthy_docker(), ADDP_OBSERVABILITY_LOGS_ENABLED='false',
                             ADDP_OBSERVABILITY_METRICS_ENABLED='false', PROMETHEUS_PORT='invalid',
                             ADDP_METRICS_TLS_DIR='invalid', MOCK_EXISTING_METRICS='1')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        calls = self.calls()
        self.assertTrue(any('stop' in a and 'prometheus' in a for a in calls))
        self.assertFalse(any('down' in a or 'rm' in a or ('up' in a and 'prometheus' in a) for a in calls))
        self.assertTrue(any('up' in a and 'postgres' in a for a in calls))

    def test_invalid_metrics_selection_reports_after_core_ready(self):
        for flag in ('', 'TRUE', '1'):
            with self.subTest(flag=flag):
                result = self.run_up(**self.healthy_docker(), ADDP_OBSERVABILITY_LOGS_ENABLED='false',
                                     ADDP_OBSERVABILITY_METRICS_ENABLED=flag)
                self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
                self.assertIn('Metrics center unconfigured', result.stderr)
                self.assertTrue(any('up' in a and 'postgres' in a for a in self.calls()))
                self.assertFalse(any('up' in a and 'prometheus' in a for a in self.calls()))

    def test_metrics_missing_certificates_do_not_stop_core(self):
        result = self.run_up(**self.healthy_docker(), ADDP_OBSERVABILITY_LOGS_ENABLED='false',
                             ADDP_OBSERVABILITY_METRICS_ENABLED='true', ADDP_METRICS_TLS_DIR=str(self.root/'missing'))
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn('core services remain running', result.stderr)
        self.assertTrue(any('up' in a and 'postgres' in a for a in self.calls()))
        self.assertFalse(any('up' in a and 'prometheus' in a for a in self.calls()))

    def test_metrics_start_failure_keeps_core_running(self):
        result = self.run_up(**self.healthy_docker(), ADDP_OBSERVABILITY_LOGS_ENABLED='false',
                             ADDP_OBSERVABILITY_METRICS_ENABLED='true', ADDP_METRICS_TLS_DIR=self.metrics_certificates(),
                             MOCK_FAIL_METRICS_START='1')
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        calls = self.calls()
        self.assertLess(next(i for i,a in enumerate(calls) if 'up' in a and 'postgres' in a),
                        next(i for i,a in enumerate(calls) if 'up' in a and 'prometheus' in a))
        self.assertFalse(any('stop' in a or 'down' in a for a in calls))

    def test_disabled_metrics_never_stop_foreign_container(self):
        result = self.run_up(**self.healthy_docker(), ADDP_OBSERVABILITY_LOGS_ENABLED='false',
                             MOCK_EXISTING_METRICS='1', MOCK_FOREIGN_METRICS='1')
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertFalse(any('stop' in a for a in self.calls()))

    def test_logs_failure_does_not_prevent_selected_metrics_start(self):
        result = self.run_up(**self.healthy_docker(), MOCK_FAIL_LOG_BUILD='1',
                             ADDP_OBSERVABILITY_METRICS_ENABLED='true', ADDP_METRICS_TLS_DIR=self.metrics_certificates())
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertTrue(any('up' in a and 'prometheus' in a for a in self.calls()))
        self.assertFalse(any('stop' in a or 'down' in a for a in self.calls()))

    def test_dev_ready_components_still_validate_selected_log_configuration(self):
        result = self.dev_start_phase(foreign=False)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn('BUSINESS_START_REACHED', result.stdout, result.stderr)
        self.assertTrue(any('up' in a and 'postgres' in a for a in self.calls()))

    def test_dev_optional_mapping_failure_reaches_business_start_phase(self):
        result = self.dev_start_phase(foreign=True)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn('BUSINESS_START_REACHED', result.stdout, result.stderr)
        self.assertIn('PostgreSQL: localhost:15432', result.stdout)

    def dev_start_phase(self, foreign, **extra):
        values = self.healthy_docker()
        values.update(ENV="production", ADDP_HOST_NODE_NAME="fixture-node", ADDP_OBSERVABILITY_LOGS_ENABLED="true", MOCK_EXISTING_LOGS="1", MOCK_FOREIGN_LOGS=str(int(foreign)))
        values.update(extra)
        env = dict(os.environ, PATH=f"{self.bin}:{os.environ['PATH']}", **values)
        env.update(ROOT_DIR=str(self.repo.resolve()), PROJECT_ROOT=str(self.repo.resolve()), INFRA_FALKORDB_PASSWORD="graph-test", REDIS_PASSWORD="redis-test", ADDP_ONLINE_HOST="0")
        for key in KEYS: env.pop(key, None)
        source=(ROOT/'scripts/dev/start.sh').read_text()
        phase=source[source.index('source "${ROOT_DIR}/scripts/infra/ports.sh"'):source.index('# Runtime 容器归属与清理')]
        result=subprocess.run(['bash','-c','set -euo pipefail\nGREEN= YELLOW= RED= NC=\n'+phase+'\necho BUSINESS_START_REACHED\nexit "$infra_optional_result"'],cwd=self.repo,env=env,capture_output=True,text=True,timeout=10)
        return result

    def test_compose_profiles_render_without_unselected_secrets(self):
        env = dict(os.environ)
        for key in (*KEYS, 'LOKI_S3_ACCESS_KEY', 'COMPOSE_PROFILES'): env.pop(key, None)
        env.update(INFRA_FALKORDB_PASSWORD='graph-test', INFRA_KAFKA_ADMIN_PASSWORD='admin-test', INFRA_KAFKA_CONNECT_PASSWORD='connect-test', INFRA_KAFKA_TRANSFER_PASSWORD='transfer-test')
        args=['docker','compose','--env-file','/dev/null','-f',str(ROOT/'docker-compose.infra.yml'),'config','--format','json']
        for selected in (False, True):
            command=args[:]
            if selected: command[2:2]=['--profile','observability-logs']
            result=subprocess.run(command, cwd=ROOT, env=env, capture_output=True, text=True, timeout=15)
            self.assertEqual(result.returncode,0,result.stderr)
            services=json.loads(result.stdout)['services']
            self.assertIn('runtime-log-pruner',services)
            for service in ('loki','alloy','runtime-log-api','runtime-log-observer','runtime-log-store-init'):
                self.assertEqual(service in services,selected,service)

    def test_online_initialization_never_creates_root_env_and_preserves_existing_secrets(self):
        values = dict(ADDP_ONLINE_HOST="1", ADDP_ONLINE_ENV_FILE=str(self.envfile))
        result = self.run_up(**values)
        self.assertNotEqual(result.returncode, 0)  # Deliberately stop before Docker startup.
        self.assertFalse((self.repo / ".env").exists(), result.stdout + result.stderr)
        self.assertTrue(self.envfile.exists())
        first = self.envfile.read_bytes()
        modified = self.envfile.stat().st_mtime_ns
        self.assertEqual(self.envfile.stat().st_mode & 0o777, 0o600)
        for line in first.decode().splitlines():
            if not line.strip():
                continue
            key, value = line.split("=", 1)
            if key in KEYS:
                self.assertEqual(len(value), 64)
                self.assertNotIn(value, result.stdout + result.stderr)
        self.run_up(**values)
        self.assertEqual(self.envfile.read_bytes(), first)
        self.assertEqual(self.envfile.stat().st_mtime_ns, modified)

    def test_online_rejects_repository_secret_path_before_writing(self):
        result = self.run_up(ADDP_ONLINE_HOST="1", ADDP_ONLINE_ENV_FILE=str(self.repo / ".env"))
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.repo / ".env").exists())
        self.assertIn("outside", result.stderr)

    def test_online_rejects_secret_file_inside_artifacts(self):
        result = self.run_up(ADDP_ONLINE_HOST="1", ADDP_ONLINE_ENV_FILE=str(self.envfile),
                             ADDP_ONLINE_ARTIFACT_DIR=str(self.secrets))
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.envfile.exists())
        self.assertIn("artifacts", result.stderr)

    def test_online_requires_explicit_external_file(self):
        result = self.run_up(ADDP_ONLINE_HOST="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.repo / ".env").exists())
        self.assertIn("ADDP_ONLINE_ENV_FILE", result.stderr)

    def test_development_initializes_once_and_prepares_host_owned_log_parents(self):
        result = self.run_up()
        self.assertNotEqual(result.returncode, 0)
        envfile = self.repo / ".env"
        self.assertTrue(envfile.exists())
        first = envfile.read_bytes()
        for name in ("logs", "logs/runtime"):
            path = self.repo / name
            self.assertTrue(path.is_dir(), name)
            self.assertEqual(path.stat().st_uid, os.getuid())
            self.assertEqual(path.stat().st_mode & 0o777, 0o700)
        self.run_up()
        self.assertEqual(envfile.read_bytes(), first)

    def test_complete_environment_does_not_create_an_env_file(self):
        values = {key: "a" * 64 for key in KEYS}
        values.update(LOKI_WRITE_TOKEN="b" * 64, LOKI_S3_ACCESS_KEY="log-account", ADDP_HOST_NODE_NAME="owned-host")
        result = self.run_up(**values)
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.repo / ".env").exists())

    def test_online_rejects_existing_root_env_without_changing_it(self):
        envfile = self.repo / ".env"
        envfile.write_text("POSTGRES_DB=personal\n")
        result = self.run_up(ADDP_ONLINE_HOST="1", ADDP_ONLINE_ENV_FILE=str(self.envfile))
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(envfile.read_text(), "POSTGRES_DB=personal\n")
        self.assertFalse(self.envfile.exists())

    def test_directory_owner_mismatch_fails_before_container_start(self):
        result = self.run_up(ADDP_RUNTIME_LOG_OWNER="11001:11001")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("must match source directory ownership", result.stderr)

    def test_force_does_not_authorize_personal_volume_deletion(self):
        docker = self.bin / "docker"
        docker.write_text("#!/bin/sh\nif [ \"$1 $2\" = \"compose version\" ]; then exit 0; fi\nexit 99\n")
        result = subprocess.run(["bash", "scripts/infra/down.sh", "--volumes", "--force"], cwd=self.repo,
                                env=dict(os.environ, PATH=f"{self.bin}:{os.environ['PATH']}",
                                         GITHUB_ACTIONS="false", ADDP_ONLINE_HOST="0"),
                                capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("禁止删除", result.stderr)


if __name__ == "__main__":
    unittest.main()
