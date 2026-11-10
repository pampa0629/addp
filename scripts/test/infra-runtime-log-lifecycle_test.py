import ast
import importlib.util
import json
import os
import shlex
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
KEYS = ("LOKI_READ_TOKEN", "LOKI_WRITE_TOKEN", "LOKI_S3_SECRET_KEY", "LOG_OBSERVER_SERVICE_CLIENT_SECRET")
_group_spec = importlib.util.spec_from_file_location('infra_fixture_metrics_project', ROOT/'scripts/utils/metrics-project.py')
source_group = importlib.util.module_from_spec(_group_spec)
_group_spec.loader.exec_module(source_group)



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
        self.addCleanup(self.stop_discovery_observer)
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.shell_init = self.root / "mock-commands.sh"
        # Read generated mocks through the installed interpreter. Executing a
        # fresh shebang file can spend the deadline before its first command.
        # BASH_ENV also covers Bash subprocesses sourced by the real lifecycle.
        self.shell_init.write_text(''.join(
            command + '() { /bin/bash ' + shlex.quote(str(self.bin / command)) + ' "$@"; }\n'
            for command in ('docker', 'curl', 'lsof')))
        self.install_command('docker', "#!/bin/sh\nexit 44\n")
        self.secrets = self.root / "secrets"
        self.secrets.mkdir(mode=0o700)
        self.envfile = self.secrets / "runtime.env"

    def fixture_environment(self):
        env = dict(os.environ)
        for key in ('ADDP_POSTGRES_METRICS_ENABLED', 'ADDP_POSTGRES_METRICS_TLS_DIR', 'ADDP_POSTGRES_METRICS_LISTEN',
                    'DOCKER_HOST', 'DOCKER_TLS_VERIFY', 'DOCKER_CERT_PATH'):
            env.pop(key, None)
        env['ADDP_POSTGRES_METRICS_STATE_DIR'] = str((self.root / 'postgres-metrics').resolve())
        for key in ('ADDP_REDIS_METRICS_ENABLED','ADDP_REDIS_METRICS_TLS_DIR','ADDP_REDIS_METRICS_LISTEN'):
            env.pop(key,None)
        env['ADDP_REDIS_METRICS_STATE_DIR'] = str((self.root / 'redis-metrics').resolve())
        env.pop('ADDP_MINIO_METRICS_ENABLED', None)
        env.pop('ADDP_MINIO_METRICS_TLS_DIR', None)
        env.pop('ADDP_MINIO_METRICS_LISTEN', None)
        env['ADDP_MINIO_METRICS_STATE_DIR'] = str((self.root / 'minio-metrics').resolve())
        env.pop('ADDP_DEPLOYMENT_DISCOVERY_DIR', None)
        for key in (*KEYS, "LOKI_S3_ACCESS_KEY", "ADDP_HOST_NODE_NAME", "ADDP_RUNTIME_LOG_OWNER", "ADDP_ONLINE_ENV_FILE", "ADDP_ONLINE_HOST", "ADDP_OBSERVABILITY_LOGS_ENABLED", "ADDP_OBSERVABILITY_METRICS_ENABLED", "ADDP_METRICS_TLS_DIR", "ADDP_METRICS_DEPLOYMENT_DIR", "ADDP_METRICS_CONTROL_MTLS_ENABLED", "ADDP_METRICS_DESKTOP_LOOPBACK_PORTS", "PROMETHEUS_SYSTEM_URL", "PROMETHEUS_MONITOR_URL", "PROMETHEUS_SERVICE_CLIENT_SECRET", "COMPOSE_PROFILES", "ADDP_OBSERVABILITY_TRACES_ENABLED", "ADDP_TRACE_DEPLOYMENTS", "ADDP_TRACE_DEPLOYMENT_DIR", "MONITOR_TEMPO_URL", "MONITOR_TRACE_TLS_DIR", "TEMPO_S3_ACCESS_KEY", "TEMPO_S3_SECRET_KEY", "TEMPO_BUCKET", "TRACE_OTLP_PORT", "TRACE_QUERY_PORT"):
            env.pop(key, None)
        env['BASH_ENV'] = str(self.shell_init)
        return env

    def stop_discovery_observer(self):
        result = subprocess.run([sys.executable, str(self.repo/'scripts/utils/deployment-inventory.py'), 'stop',
                                 '--directory', str(self.repo/'.dev-state/deployment-discovery')],
                                capture_output=True, text=True, timeout=20)
        self.assertEqual(result.returncode, 0, result.stderr)

    def run_up(self, cli=(), **values):
        env = self.fixture_environment()
        env.update(PATH=f"{self.bin}:{env['PATH']}", ENV="development", INFRA_FALKORDB_PASSWORD="graph-test", REDIS_PASSWORD="redis-test")
        env.update(values)
        # Full startup spans core services and both optional observability
        # selections. Bound hangs without treating host scheduling as failure.
        return subprocess.run(["bash", "scripts/infra/up.sh", *cli], cwd=self.repo, env=env,
                              capture_output=True, text=True, timeout=30)

    def install_command(self, name, source):
        # The owned discovery observer can still execute an older mock.
        # Publish a new inode rather than truncate an executing file (ETXTBSY).
        descriptor, path = tempfile.mkstemp(dir=self.bin, prefix=f'.{name}-')
        temporary = Path(path)
        try:
            with os.fdopen(descriptor, 'w') as output:
                output.write(source)
            temporary.chmod(0o755)
            temporary.replace(self.bin / name)
        finally:
            temporary.unlink(missing_ok=True)

    def healthy_docker(self):
        self.commands = self.root / "commands.argv"
        # up.sh invokes Docker around 90 times. Keep the fake in shell so the
        # fixture measures lifecycle behavior rather than Python startup cost.
        members = {source_group.service(kind,'infra') for kind in ('postgres','redis','minio')}
        members |= {source_group.service('postgres','infra')+'-web'}
        members |= {source_group.service(kind,'infra')+'-init' for kind in ('redis','minio')}
        self.install_command('docker', r'''#!/bin/bash
set -eu
# Lifecycle Python children deliberately inherit no test/ADDP secrets. These
# fixed read-only context/source-list facts do not depend on MOCK_* variables.
if [[ "${1-}" == context ]]; then
    case "${2-}" in show) printf 'fixture\n' ;; inspect) printf '"unix:///fixture/docker.sock"\n' ;; esac
    exit 0
fi
if [[ "${1-}" == --context ]]; then shift 2; fi
if [[ "${1-}" == info ]]; then
    if [[ "$(uname -s)" == Darwin ]]; then operating_system='Docker Desktop'; else operating_system=Linux; fi
    printf '{"id":"fixture","os_type":"linux","operating_system":"%s"}\n' "$operating_system"
    exit 0
fi
# This fixture owns no optional source containers. Recognize only the exact
# production member query, before the branch requiring parent-only MOCK_* keys.
if [[ "${1-}" == ps && " $* " == *" label=com.docker.compose.project=addp-metrics-sources "* ]]; then
    [[ "$#" == 7 && "${2-}" == -aq && "${3-}" == --no-trunc && "${4-}" == --filter &&
       "${5-}" == @SOURCE_PROJECT_FILTER@ && "${6-}" == --filter ]] || exit 85
    case "${7-}" in
        @SOURCE_SERVICE_FILTERS@) exit 0 ;;
        *) exit 86 ;;
    esac
fi
if [[ "${1-}" == ps && "$*" == *com.docker.compose.project=addp-postgres-metrics-* ]]; then exit 0; fi
args=("$@")
printf '%s\0' "$#" "$@" >> "$MOCK_COMMANDS"
has_arg() {
    local value
    for value in "${args[@]}"; do
        if [[ "$value" == "$1" ]]; then return 0; fi
    done
    return 1
}
digest=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
marker="${MOCK_COMMANDS%.*}.metrics-started"
if [[ "$1" == compose ]] && { has_arg postgres || has_arg runtime-log-pruner; }; then
    [[ "${LOKI_PORT:-}" == 0 && "${ALLOY_PORT:-}" == 0 && "${PROMETHEUS_PORT:-}" == 0 ]] || exit 81
fi
case "$1" in
    ps)
        if [[ "${MOCK_MEILI_ABSENT:-}" != 1 ]]; then printf '%s\n' fixture-meili-container; fi
        ;;
    image)
        if has_arg --format; then
            [[ "${MOCK_MEILI_IMAGE_MISSING:-}" != 1 ]] || exit 1
            printf '%s\n' "$digest"
        fi
        ;;
    inspect)
        container="${!#}"
        service="${container#addp-}"
        logs=false
        case "$service" in loki|alloy|runtime-log-api|runtime-log-observer|runtime-log-store-init) logs=true ;; esac
        if [[ "$logs" == true && "${MOCK_EXISTING_LOGS:-}" != 1 ]]; then exit 1; fi
        if [[ "$service" == prometheus && "${MOCK_EXISTING_METRICS:-}" != 1 && ! -f "$marker" ]]; then exit 1; fi
        if has_arg --format; then
            format="$3"
            case "$format" in
                *com.docker.compose.project*)
                    root="$PWD"
                    if [[ "$logs" == true && "${MOCK_FOREIGN_LOGS:-}" == 1 ]] ||
                       [[ "$service" == prometheus && "${MOCK_FOREIGN_METRICS:-}" == 1 ]]; then root=/foreign; fi
                    printf 'addp-infra|%s|%s\n' "$service" "$root"
                    ;;
                *ExitCode*) printf '%s\n' exited:0 ;;
                *State.Running*)
                    if [[ "$format" == *Health* ]]; then printf '%s\n' true:healthy; else printf '%s\n' true; fi
                    ;;
                *Health*) printf '%s\n' healthy ;;
                '{{json .Config.Cmd}}')
                    if [[ "${MOCK_TRACE_ALLOY_LOGS_ONLY:-}" == 1 ]]; then
                        printf '%s\n' '["run","/etc/alloy/runtime.alloy"]'
                    else
                        printf '%s\n' '["run","/trace-deployment/runtime.alloy"]'
                    fi
                    ;;
                '{{.Image}}') printf '%s\n' "${MOCK_MEILI_IMAGE_ID-$digest}" ;;
            esac
        fi
        ;;
    port)
        case "${3%/*}" in
            5432) port=15432 ;;
            6379) if [[ "$2" == addp-falkordb ]]; then port=16479; else port=16379; fi ;;
            9000) port=19000 ;; 9001) port=19001 ;; 7700) port=17700 ;;
            9092) port=19092 ;; 8083) port=18083 ;; 3100) port=13100 ;;
            12345) port=12345 ;; 9090) port=19090 ;;
            4318|8443)
                [[ "${MOCK_EXISTING_TRACES:-}" == 1 && "${MOCK_MISSING_TRACE_PORT:-}" != "${3%/*}" ]] || exit 1
                if [[ "$3" == 4318/tcp ]]; then port=14318; else port=14320; fi
                ;;
            *) exit 1 ;;
        esac
        printf '127.0.0.1:%s\n' "$port"
        ;;
    compose)
        if has_arg ps && has_arg -aq; then
            service="${!#}"
            case "$service" in
                prometheus) key=MOCK_EXISTING_METRICS ;;
                trace-query|tempo|trace-store-init) key=MOCK_EXISTING_TRACES ;;
                *) key=MOCK_EXISTING_LOGS ;;
            esac
            if [[ "${!key:-}" == 1 ]]; then printf 'addp-%s\n' "$service"; fi
        fi
        if has_arg up && has_arg prometheus; then
            [[ "${MOCK_FAIL_METRICS_START:-}" != 1 ]] || exit 72
            : > "$marker"
        fi
        if has_arg build && has_arg runtime-log-observer && [[ "${MOCK_FAIL_LOG_BUILD:-}" == 1 ]]; then exit 71; fi
        if has_arg exec && has_arg redis; then
            printf '%s\n' PONG
        elif has_arg config && has_arg --images; then
            printf '%s\n' fixture-owned-image
        fi
        ;;
esac
exit 0
'''.replace('@SOURCE_PROJECT_FILTER@',shlex.quote('label=com.docker.compose.project='+source_group.PROJECT))
    .replace('@SOURCE_SERVICE_FILTERS@','|'.join(shlex.quote('label=com.docker.compose.service='+name) for name in sorted(members))))
        self.install_command('curl', '#!/bin/sh\nexit 0\n')
        self.install_command('lsof', "#!/bin/sh\n"
                        "printf '%s\\n' \"$*\" >> "
                        + shlex.quote(str(self.root / "listener-calls"))
                        + "\nexit 1\n")
        return dict(MOCK_COMMANDS=str(self.commands), SKIP_POSTGRESQL_INIT="1", SKIP_MINIO_INIT="1", SKIP_MEILISEARCH_INIT="1")

    def calls(self):
        # Count-prefixed NUL records preserve empty arguments and line breaks
        # without launching a serializer for each fake Docker invocation.
        fields = iter(self.commands.read_bytes().decode().split('\0')[:-1])
        return [[next(fields) for _ in range(int(count))] for count in fields]

    def test_source_queries_are_closed_and_work_without_parent_mock_environment(self):
        self.healthy_docker()
        env = {'PATH': str(self.bin)+os.pathsep+os.environ['PATH']}
        project = source_group.PROJECT
        prefix = ['--context', 'fixture', 'ps', '-aq', '--no-trunc']
        def request(filters):
            return subprocess.run([str(self.bin/'docker'),*prefix,*filters],env=env,
                                  capture_output=True,text=True,timeout=5)
        for name in ('infra-postgres-metrics','infra-postgres-metrics-web'):
            result=request(source_group.filters(project,{name}))
            self.assertEqual(result.returncode,0,result.stderr)
            self.assertEqual(result.stdout,'')
        for filters in (source_group.filters(project,{'business-postgres-metrics'}),
                        source_group.filters(project,{'unknown-service'}),
                        source_group.filters(project+'-foreign',{'infra-postgres-metrics'}),
                        ['--filter','label=com.docker.compose.project='+project],
                        [*source_group.filters(project,{'infra-postgres-metrics'}),'unexpected']):
            with self.subTest(filters=filters):
                result=request(filters)
                self.assertNotEqual(result.returncode,0)
                self.assertEqual(result.stdout,'')
        self.assertFalse(self.commands.exists(),'metadata probe must not enter core/mutation fixture routes')

    def test_reinstall_preserves_open_command_versions(self):
        # A deployment observer may still execute a prior command between
        # subtests. Reinstallation must not rewrite its executable inode.
        for name in ('docker', 'curl', 'lsof'):
            command = self.bin / name
            original = '#!/bin/sh\n# prior version\nexit 44\n'
            command.write_text(original)
            with command.open('rb') as previous:
                self.healthy_docker()
                self.assertEqual(previous.read(), original.encode(), name)
                self.assertNotEqual(command.read_bytes(), original.encode(), name)

    def test_mock_preserves_literal_command_arguments(self):
        values = self.healthy_docker()
        arguments = ['fixture', 'space value', '', 'quote"\\', 'line\nbreak', 'carriage\rreturn', '中文']
        env = self.fixture_environment()
        env.update(PATH=f"{self.bin}:{env['PATH']}", **values)
        result = subprocess.run(['/bin/bash', '-c', 'docker "$@"', 'fixture-docker', *arguments],
                                env=env, capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.calls(), [arguments])

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

    def test_dev_reuses_healthy_meilisearch_without_applying_image_changes(self):
        result = self.dev_start_phase(foreign=False,
                                     MOCK_MEILI_IMAGE_ID='sha256:'+'b'*64)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn('Invalid runtime log secret', result.stderr)
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

    def test_log_failure_uses_only_fixture_listener_state(self):
        result = self.run_up(**self.healthy_docker(), MOCK_FAIL_LOG_BUILD="1")
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        calls = self.root / "listener-calls"
        self.assertTrue(calls.is_file(), "Infra fixture must intercept host listener queries")
        self.assertIn("-iTCP:13100", calls.read_text())
        self.assertIn("-iTCP:12345", calls.read_text())
        self.assertNotIn("23100", calls.read_text(), "host listeners must not alter fixture port selection")

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

    def test_metrics_only_uses_no_core_log_build_init_or_secret_generation(self):
        result = self.run_up(**self.healthy_docker(), **self.metrics_certificates(),
                             cli=('--metrics',), ADDP_OBSERVABILITY_METRICS_ENABLED='true')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertFalse((self.repo / '.env').exists())
        calls = self.calls()
        self.assertTrue(any('up' in a and 'prometheus' in a for a in calls))
        for command in calls:
            if command[0] == 'compose' and any(action in command for action in ('up', 'run', 'build', 'exec', 'stop')):
                self.assertFalse(any(service in command for service in
                                     ('postgres', 'redis', 'falkordb', 'minio', 'meilisearch', 'redpanda',
                                      'kafka-connect', 'loki', 'alloy', 'runtime-log-pruner', 'runtime-log-observer')),
                                 command)
            self.assertNotIn('build', command)

    def test_metrics_fixture_ignores_parent_control_and_desktop_transport(self):
        with mock.patch.dict(os.environ, {
                'ADDP_METRICS_CONTROL_MTLS_ENABLED': 'true',
                'ADDP_METRICS_DESKTOP_LOOPBACK_PORTS': '19091'}):
            result = self.run_up(**self.healthy_docker(), **self.metrics_certificates(),
                                 cli=('--metrics',), ADDP_OBSERVABILITY_METRICS_ENABLED='true')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        calls = self.calls()
        self.assertTrue(any('up' in a and 'prometheus' in a for a in calls))
        self.assertFalse(any(a[0] in ('context', 'info') for a in calls))

    def test_metrics_only_rejects_over_budget_before_docker_changes(self):
        launcher = self.repo / 'scripts/dev/process-metrics-launch.py'
        launcher.parent.mkdir(exist_ok=True)
        shutil.copy2(ROOT / 'scripts/dev/process-metrics-launch.py', launcher)
        plan = {
            f'worker-{index}': {'module_name': 'quality', 'role': 'worker',
                                'listen': f'127.0.0.1:{19500 + index}',
                                'endpoint': f'https://127.0.0.1:{19500 + index}/metrics',
                                'tls_dir': f'/private/metrics/worker-{index}'}
            for index in range(2)
        }
        result = self.run_up(**self.healthy_docker(), cli=('--metrics',), ADDP_OBSERVABILITY_METRICS_ENABLED='true',
                             ADDP_METRICS_TARGET_LIMIT='1', ADDP_DEV_PROCESS_METRICS_PLAN=json.dumps(plan))
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn('计划 2 个', result.stderr)
        self.assertFalse(self.commands.exists())

    def test_metrics_only_disabled_and_unknown_arguments_do_not_create_credentials(self):
        result = self.run_up(**self.healthy_docker(), cli=('--metrics',))
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertFalse((self.repo / '.env').exists())
        self.assertFalse(any('up' in a or 'build' in a for a in self.calls()))
        for cli in (('--unknown',), ('--metrics', '--unknown')):
            result = self.run_up(cli=cli)
            self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
            self.assertIn('Usage:', result.stderr)
            self.assertFalse((self.repo / '.env').exists())

    def test_profiles_follow_explicit_independent_selection(self):
        for logs, metrics, expected in (
                ('false', 'false', ''), ('true', 'false', 'observability-logs'),
                ('false', 'true', 'observability-metrics'),
                ('true', 'true', 'observability-logs,observability-metrics')):
            with self.subTest(logs=logs, metrics=metrics), mock.patch.dict(
                    os.environ, ADDP_METRICS_CONTROL_MTLS_ENABLED='true'):
                env = self.fixture_environment()
                env.update(ADDP_OBSERVABILITY_LOGS_ENABLED=logs,
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
        deployment = self.root / 'deployment'
        deployment.mkdir()
        secret = 'fixture-prometheus-' + 'a' * 32
        for name in ('control-ca.crt', 'source-ca.crt', 'collector.crt', 'collector.key', 'prometheus-client-secret'):
            (deployment / name).write_text(secret if name == 'prometheus-client-secret' else name)
        return dict(ADDP_METRICS_TLS_DIR=str(directory), ADDP_METRICS_DEPLOYMENT_DIR=str(deployment),
                    PROMETHEUS_SERVICE_CLIENT_SECRET=secret,
                    PROMETHEUS_SYSTEM_URL='https://system.internal:8443',
                    PROMETHEUS_MONITOR_URL='https://monitor.internal:8443')

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

    def test_selected_postgresql_source_failure_preserves_core_services(self):
        result = self.run_up(**self.healthy_docker(), ADDP_OBSERVABILITY_LOGS_ENABLED='false',
                             ADDP_POSTGRES_METRICS_ENABLED='true')
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn('Infra PostgreSQL', result.stdout)
        self.assertIn('失败', result.stdout)
        self.assertTrue(any('up' in call and 'postgres' in call for call in self.calls()))
        self.assertFalse(any('down' in call or 'stop' in call for call in self.calls()))

    def test_metrics_start_failure_keeps_core_running(self):
        result = self.run_up(**self.healthy_docker(), ADDP_OBSERVABILITY_LOGS_ENABLED='false',
                             ADDP_OBSERVABILITY_METRICS_ENABLED='true', **self.metrics_certificates(),
                             MOCK_FAIL_METRICS_START='1')
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        calls = self.calls()
        self.assertLess(next(i for i,a in enumerate(calls) if 'up' in a and 'postgres' in a),
                        next(i for i,a in enumerate(calls) if 'up' in a and 'prometheus' in a))
        self.assertFalse(any('stop' in a or 'down' in a for a in calls))

    def test_missing_discovery_input_keeps_core_and_does_not_start_center(self):
        deployment = self.metrics_certificates()
        deployment.pop('PROMETHEUS_SYSTEM_URL')
        result = self.run_up(**self.healthy_docker(), ADDP_OBSERVABILITY_LOGS_ENABLED='false',
                             ADDP_OBSERVABILITY_METRICS_ENABLED='true', **deployment)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn('Metrics discovery deployment input is missing or invalid', result.stderr)
        calls = self.calls()
        self.assertTrue(any('up' in a and 'postgres' in a for a in calls))
        self.assertFalse(any('up' in a and 'prometheus' in a for a in calls))
        self.assertFalse(any('stop' in a or 'down' in a for a in calls))

    def test_disabled_metrics_never_stop_foreign_container(self):
        result = self.run_up(**self.healthy_docker(), ADDP_OBSERVABILITY_LOGS_ENABLED='false',
                             MOCK_EXISTING_METRICS='1', MOCK_FOREIGN_METRICS='1')
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertFalse(any('stop' in a for a in self.calls()))

    def test_logs_failure_does_not_prevent_selected_metrics_start(self):
        result = self.run_up(**self.healthy_docker(), MOCK_FAIL_LOG_BUILD='1',
                             ADDP_OBSERVABILITY_METRICS_ENABLED='true', **self.metrics_certificates())
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertTrue(any('up' in a and 'prometheus' in a for a in self.calls()), result.stdout + result.stderr)
        self.assertFalse(any('stop' in a or 'down' in a for a in self.calls()))

    def test_dev_ready_components_still_validate_selected_log_configuration(self):
        result = self.dev_start_phase(foreign=False)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn('BUSINESS_START_REACHED', result.stdout, result.stderr)
        self.assertFalse(any('up' in a or 'build' in a for a in self.calls()), self.calls())

    def test_dev_optional_mapping_failure_reaches_business_start_phase(self):
        result = self.dev_start_phase(foreign=True)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn('BUSINESS_START_REACHED', result.stdout, result.stderr)
        self.assertIn('PostgreSQL: localhost:15432', result.stdout)

    def test_restart_preflight_reads_core_ports_with_trace_mapping_missing(self):
        env = self.fixture_environment()
        env.update(PATH=f"{self.bin}:{env['PATH']}", **self.healthy_docker(),
                   ROOT_DIR=str(self.repo.resolve()), ADDP_OBSERVABILITY_TRACES_ENABLED='true',
                   MOCK_EXISTING_LOGS='1', MOCK_EXISTING_TRACES='1', MOCK_MISSING_TRACE_PORT='4318')
        source = (ROOT / 'scripts/dev/restart.sh').read_text()
        begin = source.index('source "${ROOT_DIR}/scripts/infra/ports.sh"')
        phase = source[begin:source.index('source "${SCRIPT_DIR}/ports.sh"', begin)]
        result = subprocess.run(['bash', '-ec', phase + '\nprintf "%s\\n" "$MINIO_API_PORT"'],
                                cwd=self.repo, env=env, capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), '19000')
        self.assertFalse(any(a[:2] in (['port', 'addp-alloy'], ['port', 'addp-trace-query']) for a in self.calls()))

    def test_trace_status_requires_both_mappings_and_shared_alloy_config(self):
        env = self.fixture_environment()
        env.update(PATH=f"{self.bin}:{env['PATH']}", **self.healthy_docker(),
                   ADDP_OBSERVABILITY_TRACES_ENABLED='true', ADDP_OBSERVABILITY_LOGS_ENABLED='false',
                   MOCK_EXISTING_LOGS='1', MOCK_EXISTING_TRACES='1')
        for fault in ({'MOCK_MISSING_TRACE_PORT': '4318'}, {'MOCK_MISSING_TRACE_PORT': '8443'},
                      {'MOCK_TRACE_ALLOY_LOGS_ONLY': '1'}, {}):
            with self.subTest(fault=fault):
                result = subprocess.run(['bash', 'scripts/infra/status.sh'], cwd=self.repo, env=env | fault,
                                        capture_output=True, text=True, timeout=10)
                self.assertEqual(result.returncode, 0, result.stderr)
                expected = 'Unavailable' if fault else 'Ready'
                self.assertIn(f'HTTP Trace center: {expected}', result.stdout)

    def test_trace_port_failure_preserves_log_start_and_trace_success_keeps_overlay(self):
        center = self.secrets / 'trace-center'
        center.mkdir(mode=0o700)
        for name in ('server.crt', 'server.key', 'source-ca.crt', 'query-ca.crt'):
            path = center / name
            path.write_text('synthetic material')
            path.chmod(0o600)
        policy = Path('monitor/backend/internal/tracequery/policy.json')
        (self.repo / policy).parent.mkdir(parents=True)
        shutil.copy2(ROOT / policy, self.repo / policy)
        for missing_port in ('8443', ''):
            with self.subTest(missing_port=missing_port):
                result = self.run_up(**self.healthy_docker(), ADDP_OBSERVABILITY_TRACES_ENABLED='true',
                                     ADDP_TRACE_DEPLOYMENT_DIR=str(center), MOCK_EXISTING_LOGS='1',
                                     MOCK_EXISTING_TRACES='1', MOCK_MISSING_TRACE_PORT=missing_port)
                self.assertEqual(result.returncode, int(bool(missing_port)), result.stdout + result.stderr)
                self.assertEqual('Trace center unavailable' in result.stderr, bool(missing_port))
                self.assertIn('Runtime log query and collector ready', result.stdout)
                calls = self.calls()
                log_up = next(a for a in reversed(calls) if 'up' in a and 'runtime-log-observer' in a)
                self.assertEqual('scripts/infra/traces-platform.yml' in log_up, not missing_port)
                rendered = (center / 'runtime.alloy').read_text()
                self.assertEqual(rendered.count('loki.source.file "runtime"'), 1)
                self.assertIn('otelcol.receiver.otlp', rendered)

    def dev_start_phase(self, foreign, **extra):
        values = self.healthy_docker()
        values.update(ENV="production", ADDP_HOST_NODE_NAME="fixture-node", ADDP_OBSERVABILITY_LOGS_ENABLED="true", MOCK_EXISTING_LOGS="1", MOCK_FOREIGN_LOGS=str(int(foreign)))
        values.update(extra)
        env = self.fixture_environment()
        env.update(PATH=f"{self.bin}:{env['PATH']}", **values)
        env.update(ROOT_DIR=str(self.repo.resolve()), PROJECT_ROOT=str(self.repo.resolve()), INFRA_FALKORDB_PASSWORD="graph-test", REDIS_PASSWORD="redis-test", ADDP_ONLINE_HOST="0")
        source=(ROOT/'scripts/dev/start.sh').read_text()
        phase=source[source.index('source "${ROOT_DIR}/scripts/infra/ports.sh"'):source.index('# Runtime 容器归属与清理')]
        result=subprocess.run(['bash','-c','set -euo pipefail\nGREEN= YELLOW= RED= NC=\n'+phase+'\necho BUSINESS_START_REACHED\nexit "$infra_optional_result"'],cwd=self.repo,env=env,capture_output=True,text=True,timeout=10)
        return result

    def test_compose_profiles_render_without_unselected_secrets(self):
        env = self.fixture_environment()
        env.update(REDIS_PASSWORD='redis-test', INFRA_FALKORDB_PASSWORD='graph-test', INFRA_KAFKA_ADMIN_PASSWORD='admin-test', INFRA_KAFKA_CONNECT_PASSWORD='connect-test', INFRA_KAFKA_TRANSFER_PASSWORD='transfer-test')
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

    def test_trace_compose_keeps_tempo_private_and_publishes_proxy_on_loopback(self):
        env = self.fixture_environment()
        env.update(REDIS_PASSWORD='redis-test', INFRA_FALKORDB_PASSWORD='graph-test',
                   ADDP_TRACE_DEPLOYMENT_DIR=str(self.secrets), TRACE_OTLP_PORT='14318', TRACE_QUERY_PORT='14320')
        command = ['docker', 'compose', '--env-file', '/dev/null',
                   '-f', 'docker-compose.infra.yml', '-f', 'scripts/infra/traces-platform.yml',
                   '--profile', 'observability-logs', '--profile', 'observability-traces',
                   'config', '--format', 'json']
        result = subprocess.run(command, cwd=ROOT, env=env, capture_output=True, text=True, timeout=15)
        self.assertEqual(result.returncode, 0, result.stderr)
        config = json.loads(result.stdout)
        services = config['services']
        self.assertTrue(config['networks']['trace-backend']['internal'])
        self.assertEqual(set(services['tempo']['networks']), {'trace-backend'})
        self.assertFalse(services['tempo'].get('ports'))
        self.assertEqual(set(services['trace-query']['networks']), {'addp-network', 'trace-backend'})
        self.assertEqual([(p['host_ip'], str(p['published']), p['target']) for p in services['trace-query']['ports']],
                         [('127.0.0.1', '14320', 8443)])
        alloy = services['alloy']
        self.assertEqual(set(alloy['networks']), {'addp-network', 'trace-backend'})
        self.assertEqual(alloy['command'][-1], '/trace-deployment/runtime.alloy')
        self.assertIn(('127.0.0.1', '14318', 4318),
                      [(p['host_ip'], str(p['published']), p['target']) for p in alloy['ports']])

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
        self.install_command('docker', "#!/bin/sh\nif [ \"$1 $2\" = \"compose version\" ]; then exit 0; fi\nexit 99\n")
        env = self.fixture_environment()
        env.update(PATH=f"{self.bin}:{env['PATH']}", GITHUB_ACTIONS="false", ADDP_ONLINE_HOST="0")
        result = subprocess.run(["bash", "scripts/infra/down.sh", "--volumes", "--force"], cwd=self.repo,
                                env=env,
                                capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("禁止删除", result.stderr)


if __name__ == "__main__":
    unittest.main()
