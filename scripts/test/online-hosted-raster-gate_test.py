import importlib
import os
from pathlib import Path
import shutil
import subprocess
import unittest

support = importlib.import_module('scripts.test.online-hosted-opengauss-gate_test')
SCRIPT = Path(__file__).with_name('online-hosted-raster-gate.sh')


class HostedRasterGateTest(unittest.TestCase):
    def setUp(self):
        self.host = support.OnlineHostedOpenGaussGateTest()
        self.host.setUp()
        self.addCleanup(self.host.tearDown)
        self.host.prepare_run_fixture()
        shutil.copy2(SCRIPT, self.host.repository / 'scripts/test/online-hosted-raster-gate.sh')
        self.host._write_repository_script('business/scripts/online-raster-minio-fixture.py', '''
            #!/usr/bin/env python3
            import os, sys
            from pathlib import Path
            action = sys.argv[1]
            with open(os.environ['ADDP_TEST_GATE_TRACE'], 'a') as stream: stream.write('raster-fixture:' + action + '\\n')
            if action == 'start':
                for role in ('source', 'target'):
                    Path(os.environ['ADDP_ONLINE_SECRET_DIR'], 'raster-' + role + '-engine.json').write_text('{}')
            if os.environ.get('ADDP_TEST_FAIL_ACTION') == action: sys.exit(1)
        ''')
        self.host._write_repository_script('scripts/test/online-engine-registration.py', '''
            import argparse
            parser = argparse.ArgumentParser()
            parser.add_argument('--descriptor'); parser.add_argument('--output')
            args = parser.parse_args()
            value = 11 if 'source' in args.descriptor else 12
            with open(args.output, 'w') as stream: stream.write(f'export ADDP_ONLINE_CONSUMER_ENGINE_ID={value}\\n')
        ''')
        self.host._executable('docker', '''
            #!/usr/bin/env bash
            case "$1 $2" in
              'container inspect') [ "${ADDP_TEST_OCCUPIED:-}" = "$3" ]; exit ;;
              'compose version') exit 0 ;;
              'image inspect')
                image=${!#}
                if [ -f "$ADDP_TEST_GATE_TRACE" ] && grep -Fq "docker:image rm $image" "$ADDP_TEST_GATE_TRACE"; then exit 1; fi
                if [ -f "$ADDP_TEST_GATE_TRACE" ] && grep -q 'start:-geopython-workflow' "$ADDP_TEST_GATE_TRACE"; then echo sha256:built; exit 0; fi
                exit 1 ;;
            esac
            if [ "$1" = inspect ]; then echo sha256:built; exit 0; fi
            if [ "$1" = ps ] || [ "${2:-}" = ls ]; then exit 0; fi
            echo "docker:$*" >> "$ADDP_TEST_GATE_TRACE"
            [ "${ADDP_TEST_MIRROR_FAIL:-0}" != 1 ] || [ "$1" != pull ]
        ''')
        self.host._executable('make', '''
            #!/usr/bin/env bash
            [ -z "${ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN:-}" ]
            [ -z "${POSTGRES_PASSWORD:-}" ]
            [ "$ADDP_ONLINE_RASTER_SOURCE_ENGINE_ID" = 11 ]
            [ "$ADDP_ONLINE_RASTER_TARGET_ENGINE_ID" = 12 ]
            echo "make:$*" >> "$ADDP_TEST_GATE_TRACE"
            [ "${ADDP_TEST_SUITE_FAIL:-0}" != 1 ]
        ''')
        for command in ('curl', 'npm'):
            self.host._executable(command, '#!/usr/bin/env bash\nexit 0\n')
        subprocess.run(['git', 'add', '.'], cwd=self.host.repository, check=True)
        subprocess.run(['git', 'commit', '-qm', 'fixture'], cwd=self.host.repository, check=True)

    def run_gate(self, check=False, **overrides):
        env = dict(os.environ, PATH=f'{self.host.bin}:{os.environ["PATH"]}', GITHUB_ACTIONS='true',
                   GITHUB_RUN_ID='123', GITHUB_RUN_ATTEMPT='1', RUNNER_OS='Linux', RUNNER_TEMP=str(self.host.root),
                   ADDP_ONLINE_HOST='1', ADDP_ONLINE_HOSTED='1', ONLINE_SUITE_INPUT='raster-workflow',
                   ADDP_ONLINE_ARTIFACT_DIR=str(self.host.artifacts), ADDP_ONLINE_SECRET_DIR=str(self.host.secrets),
                   ADDP_TEST_GATE_TRACE=str(self.host.trace), ADDP_TEST_BACKGROUND_PIDS=str(self.host.background_pids),
                   POSTGRES_PASSWORD='private-database-marker')
        env.update(overrides)
        return subprocess.run(['bash', 'scripts/test/online-hosted-raster-gate.sh'] + (['--check-only'] if check else []),
                              cwd=self.host.repository, env=env, text=True, capture_output=True, timeout=60)

    def test_admission_rejects_personal_or_occupied_environment_without_mutation(self):
        for changes in ({'RUNNER_OS': 'macOS'}, {'GITHUB_ACTIONS': 'false'},
                        {'ADDP_TEST_OCCUPIED': 'addp-raster-source'}, {'ONLINE_SUITE_INPUT': 'other'}):
            result = self.run_gate(check=True, **changes)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(self.host.trace.exists())

    def test_readiness_is_read_only(self):
        result = self.run_gate(check=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(self.host.trace.exists())
        self.assertFalse(self.host.secrets.exists())

    def test_startup_registers_distinct_engines_and_removes_credentials_before_business_scene(self):
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        trace = self.host.trace.read_text()
        for step in ('raster-fixture:start', 'raster-fixture:seed', 'start:-develop', 'start:-manager', 'start:-monitor',
                     'start:-geopython-workflow', 'make:test-online ONLINE_SUITE=raster-workflow',
                     'application-stop', 'raster-fixture:stop', 'docker:rm -fv addp-raster-registry', 'infra-down'):
            self.assertIn(step, trace)
        self.assertLess(trace.index('application-stop'), trace.index('raster-fixture:stop'))
        self.assertFalse(self.host.secrets.exists())
        self.assertIn('cleanup=passed', (self.host.artifacts / 'summary.txt').read_text())
        evidence = result.stdout + ''.join(path.read_text() for path in self.host.artifacts.rglob('*') if path.is_file())
        self.assertNotIn('private-database-marker', evidence)
        self.assertNotIn('provisioner-token', evidence)

    def test_business_failure_still_cleans_application_business_registry_infra_and_secrets(self):
        result = self.run_gate(ADDP_TEST_SUITE_FAIL='1')
        self.assertNotEqual(result.returncode, 0)
        trace = self.host.trace.read_text()
        for step in ('application-stop', 'raster-fixture:stop', 'docker:rm -fv addp-raster-registry', 'infra-down'):
            self.assertIn(step, trace)
        self.assertFalse(self.host.secrets.exists())
        self.assertIn('result=failed', (self.host.artifacts / 'summary.txt').read_text())

    def test_seed_failure_cleans_partial_deployment(self):
        result = self.run_gate(ADDP_TEST_FAIL_ACTION='seed')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('infra-down', self.host.trace.read_text())
        self.assertFalse(self.host.secrets.exists())

    def test_mirror_failure_cleans_without_starting_infra(self):
        result = self.run_gate(ADDP_TEST_MIRROR_FAIL='1')
        self.assertNotEqual(result.returncode, 0)
        trace = self.host.trace.read_text()
        self.assertNotIn('infra-up', trace)
        self.assertIn('docker:rm -fv addp-raster-registry', trace)
        self.assertFalse(self.host.secrets.exists())

    def test_cleanup_failure_invalidates_successful_business_scene(self):
        result = self.run_gate(ADDP_TEST_FAIL_ACTION='stop')
        self.assertNotEqual(result.returncode, 0)
        summary = (self.host.artifacts / 'summary.txt').read_text()
        self.assertIn('result=failed', summary)
        self.assertIn('cleanup=failed', summary)
        self.assertIn('infra-down', self.host.trace.read_text())
        self.assertFalse(self.host.secrets.exists())


if __name__ == '__main__': unittest.main()
