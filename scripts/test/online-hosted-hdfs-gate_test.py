import importlib
import os
import shutil
import subprocess
import unittest
from pathlib import Path

SUPPORT = importlib.import_module('scripts.test.online-hosted-opengauss-gate_test')
SCRIPT = Path(__file__).with_name('online-hosted-hdfs-gate.sh')


def install_spark_container_fixture(host, suite, runtime):
    host._executable('docker', '''
            #!/usr/bin/env bash
            case "$1 $2" in
              'compose version') exit 0 ;;
              'container inspect') [ "${ADDP_TEST_PREEXIST_CONTAINER:-}" != "$3" ] || exit 0; [ -f "${ADDP_TEST_GATE_TRACE}.$3" ]; exit ;;
              'image inspect')
                [ "${ADDP_TEST_PREEXIST_IMAGE:-0}" = 1 ] && exit 0
                [ -f "${ADDP_TEST_GATE_TRACE}.image" ] || exit 1
                echo sha256:runtime-image; exit 0 ;;
              'image rm') rm -f "${ADDP_TEST_GATE_TRACE}.image"; exit 0 ;;
            esac
            if [ "$1" = run ]; then
              previous= name= label=
              for argument in "$@"; do
                [ "$previous" != --name ] || name=$argument
                [ "$previous" != --label ] || label=$argument
                previous=$argument
              done
              echo "docker-run:$name" >> "$ADDP_TEST_GATE_TRACE"
              [ "${ADDP_TEST_RUNTIME_FAIL:-0}" != 1 ] || [ "$name" != addp-hdfs-online-runtime ] || exit 1
              printf '%s\\n' "$label" > "${ADDP_TEST_GATE_TRACE}.$name"
              exit 0
            fi
            if [ "$1" = inspect ]; then
              case "$3" in
                *Config.Labels*)
                  label=$(cat "${ADDP_TEST_GATE_TRACE}.${@: -1}")
                  case "$3" in
                    *com.addp.online-runtime*) expected=com.addp.online-runtime ;;
                    *) expected=com.addp.online-fixture ;;
                  esac
                  if [ "$label" = "$expected=hdfs-spark-consumer-flow" ]; then echo hdfs-spark-consumer-flow; else echo '<no value>'; fi ;;
                *Config.Cmd*) echo '["python","api_server.py"]' ;;
                *State.Running*) echo true ;;
                *) echo sha256:runtime-image ;;
              esac
              exit 0
            fi
            if [ "$1" = ps ]; then
              for container in "${ADDP_TEST_GATE_TRACE}".addp-*; do
                [ -f "$container" ] || continue
                [ "$(cat "$container")" != "${4#label=}" ] || echo "$container"
              done
            fi
            if [ "$1" = rm ]; then
              echo "docker-rm:$3" >> "$ADDP_TEST_GATE_TRACE"
              [ "${ADDP_TEST_RUNTIME_CLEANUP_FAIL:-0}" != 1 ] || [ "$3" != addp-hdfs-online-runtime ] || exit 1
              rm -f "${ADDP_TEST_GATE_TRACE}.$3"
            fi
        '''.replace('hdfs-spark-consumer-flow', suite).replace('addp-hdfs-online-runtime', runtime))


class HostedHDFSGateTest(unittest.TestCase):
    def setUp(self):
        self.host = SUPPORT.OnlineHostedOpenGaussGateTest()
        self.host.setUp()
        self.host.prepare_run_fixture()
        shutil.copy2(SCRIPT, self.host.repository / 'scripts/test/online-hosted-hdfs-gate.sh')
        self.host._write_repository_script('business/scripts/online-hdfs-spark-fixture.sh', '''
            #!/usr/bin/env bash
            echo "hdfs-fixture:$1" >> "$ADDP_TEST_GATE_TRACE"
            if [ "$1" = start ]; then
              printf '{}\\n' > "$ADDP_ONLINE_SECRET_DIR/hdfs-engine.json"
              printf '{}\\n' > "$ADDP_ONLINE_SECRET_DIR/spark-engine.json"
              printf '{}\\n' > "$ADDP_ONLINE_SECRET_DIR/postgres-engine.json"
            fi
            if [ "$1" = stop ]; then
              remaining=$(docker ps -aq --filter label=com.addp.online-fixture=hdfs-spark-consumer-flow)
              [ -z "$remaining" ] || exit 1
            fi
            [ "${ADDP_TEST_FIXTURE_FAIL:-}" != "$1" ]
        ''')
        self.host._executable('go', '''
            #!/usr/bin/env bash
            echo "identity:$*" >> "$ADDP_TEST_GATE_TRACE"
            [ "${ADDP_TEST_IDENTITY_FAIL:-0}" != 1 ] || exit 1
            previous=
            for argument in "$@"; do
              if [ "$previous" = --output ]; then
                cat > "$argument" <<'EOF'
            export ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN=provisioner-token
            export ADDP_ONLINE_TEST_TENANT_ID=2
            export ADDP_ONLINE_TEST_USER_ACCESS_TOKEN=consumer-token
            export ADDP_ONLINE_TEST_USER_USERNAME=external-online-consumer
            export ADDP_ONLINE_TEST_USER_PASSWORD=consumer-password
            EOF
                exit 0
              fi
              previous=$argument
            done
            exit 2
        ''')
        self.host._write_repository_script('scripts/test/online-engine-registration.py', '''
            import os, sys
            from pathlib import Path
            with open(os.environ['ADDP_TEST_GATE_TRACE'], 'a') as trace:
                trace.write('engine-register\\n')
            if os.environ.get('ADDP_TEST_REGISTRATION_FAIL') == '1':
                sys.exit(1)
            assert os.environ['ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'] == 'provisioner-token'
            identifier = 17 if 'hdfs-engine.json' in sys.argv[-3] else (19 if 'postgres-engine.json' in sys.argv[-3] else 18)
            Path(sys.argv[-1]).write_text(f'export ADDP_ONLINE_CONSUMER_ENGINE_ID={identifier}\\n')
        ''')
        self.host._executable('make', '''
            #!/usr/bin/env bash
            echo "make:$*" >> "$ADDP_TEST_GATE_TRACE"
            if [ "$1" = build-images ]; then
              [ "${ADDP_TEST_BUILD_FAIL:-0}" != 1 ] || exit 1
              touch "${ADDP_TEST_GATE_TRACE}.image"
              exit 0
            fi
            [ "$ADDP_ONLINE_HDFS_ENGINE_ID" = 17 ] && [ "$ADDP_ONLINE_SPARK_ENGINE_ID" = 18 ] &&
              [ "$HADOOP_USER_NAME" = addp_business_reader ] && [ "$SPARK_WORKFLOW_SHARED_HOST" = 127.0.0.1 ] &&
              [ "$ADDP_ONLINE_TEST_USER_USERNAME" = external-online-consumer ] &&
              [ -z "${ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN:-}" ] &&
              [ -z "${ELASTICSEARCH_PASSWORD:-}" ] || exit 1
            [ "${ADDP_TEST_SUITE_FAIL:-0}" != 1 ]
        ''')
        self.host._executable('curl', '''
            #!/usr/bin/env bash
            exit 0
        ''')
        install_spark_container_fixture(self.host, 'hdfs-spark-consumer-flow', 'addp-hdfs-online-runtime')
        self.host._write_repository_script('.env.example', 'SPARK_WORKFLOW_PORT=8098\n')
        self.host._executable('npm', '''
            #!/usr/bin/env bash
            echo "npm:$*" >> "$ADDP_TEST_GATE_TRACE"
        ''')
        subprocess.run(['git', 'add', '.'], cwd=self.host.repository, check=True)
        subprocess.run(['git', 'commit', '-qm', 'elasticsearch fixture'], cwd=self.host.repository, check=True)

    def tearDown(self):
        self.host.tearDown()

    def run_gate(self, check=False, **overrides):
        env = dict(os.environ, PATH=f"{self.host.bin}:{os.environ['PATH']}",
                   GITHUB_ACTIONS='true', RUNNER_OS='Linux', RUNNER_TEMP=str(self.host.root),
                   ADDP_ONLINE_HOST='1', ADDP_ONLINE_HOSTED='1', ONLINE_SUITE_INPUT='hdfs-spark-consumer-flow',
                   ADDP_ONLINE_ARTIFACT_DIR=str(self.host.artifacts), ADDP_ONLINE_SECRET_DIR=str(self.host.secrets),
                   ADDP_TEST_GATE_TRACE=str(self.host.trace), ADDP_TEST_BACKGROUND_PIDS=str(self.host.background_pids))
        env.update(overrides)
        command = ['bash', 'scripts/test/online-hosted-hdfs-gate.sh'] + (['--check-only'] if check else [])
        return subprocess.run(command, cwd=self.host.repository, env=env, capture_output=True, text=True, timeout=20)

    def test_preflight_is_read_only_and_refuses_personal_and_other_profiles(self):
        result = self.run_gate(check=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(self.host.trace.exists())
        self.assertFalse(self.host.secrets.exists())
        for overrides in ({'ADDP_ONLINE_HOSTED': '0'}, {'GITHUB_ACTIONS': 'false'},
                          {'ADDP_ONLINE_OWNER_MANAGED': '1'}, {'ONLINE_SUITE_INPUT': 'opengauss-consumer-flow'},
                          {'ADDP_TEST_UNAME_S': 'Darwin'}):
            result = self.run_gate(check=True, **overrides)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(self.host.trace.exists())

    def test_success_uses_same_suite_then_verifies_cleanup_and_zero_residuals(self):
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        trace = self.host.trace.read_text()
        sequence = ('infra-up', 'hdfs-fixture:start', 'start:-meta', 'start:-manager', 'start:-develop', 'docker-run:addp-hdfs-online-registry', 'make:build-images', 'docker-run:addp-hdfs-online-runtime',
                    'playwright install --with-deps chromium', '--suite hdfs-spark-consumer-flow',
                    'engine-register', 'make:test-online ONLINE_SUITE=hdfs-spark-consumer-flow',
                    'docker-rm:addp-hdfs-online-runtime', 'application-stop', 'hdfs-fixture:stop', 'docker-rm:addp-hdfs-online-registry', 'infra-down')
        indices = [trace.index(step) for step in sequence]
        self.assertEqual(indices, sorted(indices))
        self.assertNotIn('start:-all', trace)
        self.assertNotIn('start:-spark-workflow', trace)
        self.assertIn('default_entry=python api_server.py', (self.host.artifacts / 'hdfs-runtime-build.txt').read_text())
        self.assertFalse(list(self.host.root.glob('trace.log.addp-hdfs-*')))
        self.assertFalse(Path(str(self.host.trace) + '.image').exists())
        self.assertFalse(self.host.secrets.exists())
        summary = (self.host.artifacts / 'summary.txt').read_text()
        self.assertIn('result=passed', summary)
        self.assertIn('infra_cleanup=zero_residuals', summary)

    def test_failures_destroy_owned_resources(self):
        for flags in ({'ADDP_TEST_FIXTURE_FAIL': 'start'}, {'ADDP_TEST_IDENTITY_FAIL': '1'},
                      {'ADDP_TEST_REGISTRATION_FAIL': '1'}, {'ADDP_TEST_SUITE_FAIL': '1'},
                      {'ADDP_TEST_BUILD_FAIL': '1'}, {'ADDP_TEST_RUNTIME_FAIL': '1'}):
            with self.subTest(flags=flags):
                self.host.trace.unlink(missing_ok=True)
                result = self.run_gate(**flags)
                self.assertNotEqual(result.returncode, 0)
                trace = self.host.trace.read_text()
                self.assertIn('hdfs-fixture:stop', trace)
                self.assertIn('infra-down', trace)
                self.assertFalse(self.host.secrets.exists())
                self.assertIn('result=failed', (self.host.artifacts / 'summary.txt').read_text())

    def test_cleanup_failure_cannot_be_reported_as_passed(self):
        result = self.run_gate(ADDP_TEST_FIXTURE_FAIL='stop')
        self.assertNotEqual(result.returncode, 0)
        summary = (self.host.artifacts / 'summary.txt').read_text()
        self.assertIn('cleanup=failed', summary)
        self.assertIn('result=failed', summary)
        self.assertFalse(self.host.secrets.exists())
        self.assertIn('infra-down', self.host.trace.read_text())

    def test_runtime_cleanup_failure_cannot_pass(self):
        result = self.run_gate(ADDP_TEST_RUNTIME_CLEANUP_FAIL='1')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('cleanup=failed', (self.host.artifacts / 'summary.txt').read_text())
        self.assertIn('docker-rm:addp-hdfs-online-registry', self.host.trace.read_text())

    def test_refuses_existing_runtime_resources_before_mutation(self):
        for flag in ({'ADDP_TEST_PREEXIST_IMAGE': '1'}, {'ADDP_TEST_PREEXIST_CONTAINER': 'addp-hdfs-online-runtime'}):
            result = self.run_gate(check=True, **flag)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(self.host.trace.exists())

    def test_interrupt_still_cleans_the_owned_fixture(self):
        self.host._executable('make', '''
            #!/usr/bin/env bash
            echo suite-interrupted >> "$ADDP_TEST_GATE_TRACE"
            kill -TERM "$PPID"
        ''')
        result = self.run_gate()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('hdfs-fixture:stop', self.host.trace.read_text())
        self.assertIn('infra-down', self.host.trace.read_text())
        self.assertFalse(self.host.secrets.exists())
        self.assertIn('result=failed', (self.host.artifacts / 'summary.txt').read_text())


if __name__ == '__main__':
    unittest.main()
