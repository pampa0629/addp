"""Exercise the standard Hosted exit trap with native phase failures; no Docker."""
from importlib import import_module
import os
from pathlib import Path
import select
import shutil
import subprocess
import sys
import time
import unittest

SUPPORT = import_module('scripts.test.online-hosted-security-gate_test')
ROOT = Path(__file__).resolve().parents[2]


class HostedNativeCleanupTest(unittest.TestCase):
    def setUp(self):
        support = SUPPORT.HostedSecurityGateTest(); support.setUp()
        self.addCleanup(support.doCleanups)
        self.host = support.host
        repo = self.host.repository
        shutil.copy2(ROOT / 'scripts/test/online-hosted-cleanup-native-gate.sh', repo / 'scripts/test/online-hosted-cleanup-native-gate.sh')
        (repo / 'engines/model3d-workflow').mkdir(parents=True)
        (repo / 'engines/model3d-workflow/requirements.txt').write_text('minio==7.2.12\n')
        self.host._executable('python3', '''#!/usr/bin/env bash
if [ "$1 $2" = '-m venv' ]; then mkdir -p "$3/bin"; exit 0; fi
if [ "$1 $2" = '-m pip' ]; then exit 0; fi
exec REAL_PYTHON "$@"
'''.replace('REAL_PYTHON', sys.executable))
        self.host._write_repository_script('scripts/test/cleanup-native-infra.py', '''
import os,sys
with open(os.environ['ADDP_TEST_GATE_TRACE'],'a') as out: out.write('native:'+sys.argv[1]+'\\n')
if os.environ.get('NATIVE_FAIL')==sys.argv[1]:sys.exit(1)
''')
        for script in ('online-manager-minio-fixture.sh', 'online-continuous-fixture.sh'):
            self.host._write_repository_script('business/scripts/' + script, '''
#!/usr/bin/env bash
echo "business:$1" >> "$ADDP_TEST_GATE_TRACE"
''')
        docker = (self.host.bin / 'docker').read_text()
        self.host._executable('docker', docker.replace('echo "docker:$*"', '''if [ "$1" = inspect ]; then echo "$ADDP_ONLINE_TEST_RUN_ID"; exit 0; fi
echo "docker:$*"'''))
        self.host._executable('make', '''
#!/usr/bin/env bash
set -euo pipefail
echo "make:$*" >> "$ADDP_TEST_GATE_TRACE"
if [ "$ADDP_ONLINE_NATIVE_CASE" = manager-late-writer ]; then
  [ "$MANAGER_EXECUTION_LEASE_DURATION" = 30s ] && [ "$MANAGER_EXECUTION_HEARTBEAT_INTERVAL" = 5s ]
else
  [ -z "${MANAGER_EXECUTION_LEASE_DURATION:-}" ] && [ -z "${MANAGER_EXECUTION_HEARTBEAT_INTERVAL:-}" ]
fi
if [ "$1" = test-online ]; then
  [ "$ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN" = source-initializer-token ]
  [ "${NATIVE_FAIL:-}" != scenario ]
fi
if [ "$1" = build-images ]; then
  if [ "${NATIVE_FAIL:-}" = images ]; then exit 17; fi
  if [ "${NATIVE_REAL_SCHEDULER:-}" = 1 ]; then
    bash scripts/test/fixture-image-scheduler.sh "${2#IMAGE_BUILD_ARGS=}"
  fi
fi
''')
        # Exercise the actual public scheduler, with owned leaf processes instead
        # of Docker. Model3D stays alive after all ordinary images have returned.
        builder = (ROOT / 'scripts/build/build-images.sh').read_text()
        scheduler = 'schedule_image_builds() {' + builder.split('schedule_image_builds() {', 1)[1].split('\n# Pull base images', 1)[0]
        self.host._write_repository_script('scripts/test/fixture-image-scheduler.sh', '''
#!/usr/bin/env bash
set -euo pipefail
BUILD_JOBS=2
IMAGE_SCHEDULER_PID=''
IMAGE_WORK_DIR=$(mktemp -d)
trap 'rm -rf "$IMAGE_WORK_DIR"' EXIT
build_service() {
  printf 'fixture image started: %s\\n' "$1"
  if [ "$1" = model3d-workflow-engine ]; then
    touch "$NATIVE_IMAGE_ENTERED"
    printf 'Model3D delegated build entered\\n'
    until [ -f "$NATIVE_IMAGE_RELEASE" ]; do sleep 0.05; done
  fi
  printf 'fixture image returned: %s\\n' "$1"
}
read -r -a arguments <<< "$1"
services=''
for ((i=0;i<${#arguments[@]};i++)); do
  case "${arguments[i]}" in
    --jobs) BUILD_JOBS="${arguments[i+1]}" ;;
    --services) services="${arguments[i+1]}" ;;
  esac
done
IFS=, read -r -a names <<< "$services"
definitions=()
for name in "${names[@]}"; do definitions+=("$name:fixture"); done
''' + scheduler + '\nschedule_image_builds "${definitions[@]}"\n')
        subprocess.run(['git', 'add', '.'], cwd=repo, check=True)
        subprocess.run(['git', 'commit', '-qm', 'native lifecycle fixture'], cwd=repo, check=True)

    def gate_environment(self, case='manager-success', **extra):
        return {**os.environ, 'PATH': f'{self.host.bin}:{os.environ["PATH"]}', 'GITHUB_ACTIONS': 'true',
               'RUNNER_OS': 'Linux', 'RUNNER_TEMP': str(self.host.root), 'ADDP_ONLINE_HOST': '1',
               'ADDP_ONLINE_HOSTED': '1', 'ONLINE_SUITE_INPUT': 'cleanup-native-effects',
               'ADDP_ONLINE_NATIVE_CASE': case, 'GITHUB_RUN_ID': '1234', 'GITHUB_RUN_ATTEMPT': '1',
               'ADDP_ONLINE_ARTIFACT_DIR': str(self.host.artifacts), 'ADDP_ONLINE_SECRET_DIR': str(self.host.secrets),
               'ADDP_TEST_GATE_TRACE': str(self.host.trace), 'ADDP_TEST_BACKGROUND_PIDS': str(self.host.background_pids), **extra}

    def run_gate(self, case='manager-success', **extra):
        env = self.gate_environment(case, **extra)
        return subprocess.run(['bash', 'scripts/test/online-hosted-cleanup-native-gate.sh'], cwd=self.host.repository,
                              env=env, capture_output=True, text=True, timeout=20)

    def test_delegated_image_log_visible_before_process_returns_and_next_phase_waits(self):
        entered, release = self.host.root / 'image-entered', self.host.root / 'image-release'
        process = subprocess.Popen(['bash', 'scripts/test/online-hosted-cleanup-native-gate.sh'],
            cwd=self.host.repository, env=self.gate_environment(NATIVE_REAL_SCHEDULER='1',
                NATIVE_IMAGE_ENTERED=str(entered), NATIVE_IMAGE_RELEASE=str(release)),
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, bufsize=0)
        output = b''
        try:
            deadline = time.monotonic() + 8
            while time.monotonic() < deadline:
                if select.select([process.stdout], [], [], 0.1)[0]:
                    chunk = os.read(process.stdout.fileno(), 65536)
                    if not chunk: break
                    output += chunk
                if entered.exists() and b'Model3D delegated build entered' in output: break
            self.assertTrue(entered.exists(), output.decode(errors='replace'))
            self.assertIsNone(process.poll())
            self.assertIn(b'Model3D delegated build entered', output)
            self.assertNotIn('make:test-online', self.host.trace.read_text())
            self.assertNotIn('up -d --no-deps --wait system-backend', self.host.trace.read_text())
        finally:
            release.touch()
            remaining, _ = process.communicate(timeout=20)
            output += remaining
        self.assertEqual(0, process.returncode, output.decode(errors='replace'))
        self.assertEqual(1, output.count(b'Model3D delegated build entered'))
        self.assertFalse(self.host.secrets.exists())

    def test_image_failure_preserves_exit_code_and_destroys_without_starting_application(self):
        result = self.run_gate(NATIVE_FAIL='images')
        self.assertEqual(17, result.returncode, result.stdout + result.stderr)
        self.assertIn('native_cleanup_phase=images state=returned exit=17', result.stdout)
        trace = self.host.trace.read_text()
        self.assertNotIn('up -d --no-deps --wait system-backend', trace)
        self.assertNotIn('make:test-online', trace)
        self.assertIn('native:stop', trace)
        self.assertIn('business:stop', trace)
        self.assertIn('infra-down', trace)
        self.assertIn('result=failed', (self.host.artifacts / 'summary.txt').read_text())
        self.assertIn('infra_cleanup=zero_residuals', (self.host.artifacts / 'summary.txt').read_text())
        self.assertFalse(self.host.secrets.exists())

    def test_each_explicit_case_starts_only_needed_runtime_and_cleans_in_order(self):
        cases = ('manager-success', 'manager-object-unknown', 'manager-index-unknown',
                 'manager-late-writer', 'transfer-engine', 'transfer-dlq-unknown', 'transfer-capture-unknown')
        for case in cases:
            with self.subTest(case=case):
                for suffix in ('', '.containers', '.remaining', '.down'):
                    self.host.trace.with_name(self.host.trace.name + suffix).unlink(missing_ok=True)
                result = self.run_gate(case)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                trace = self.host.trace.read_text()
                order = ['infra-up', 'native:prepare', 'business:start-obj' if case.startswith('manager-') else 'business:start',
                         'native:manifest', 'make:build ', 'make:build-images ', 'up -d --no-deps --wait system-backend',
                         'make:test-online', 'execution-resume', 'down --remove-orphans --volumes', 'native:stop', 'business:stop', 'infra-down']
                self.assertEqual([trace.index(x) for x in order], sorted(trace.index(x) for x in order))
                self.assertIn('up -d --no-deps --wait model3d-workflow-engine' if case.startswith('manager-') else
                              'up -d --no-deps --wait transfer-continuous-worker', trace)
                self.assertNotIn('geopython', trace)
                self.assertNotIn('start:-', trace)
                self.assertNotIn('chromium', trace)
                self.assertFalse(self.host.secrets.exists())
                self.assertIn('result=passed', (self.host.artifacts / 'summary.txt').read_text())

    def test_prepare_scenario_recovery_and_destruction_failures_still_destroy_and_fail(self):
        for failure in ('prepare', 'scenario', 'recovery', 'stop', 'residual'):
            with self.subTest(failure=failure):
                for suffix in ('', '.containers', '.remaining', '.down'):
                    self.host.trace.with_name(self.host.trace.name + suffix).unlink(missing_ok=True)
                env = {'NATIVE_FAIL': failure}
                if failure == 'recovery': env['ADDP_TEST_RESUME_FAIL'] = '1'
                if failure == 'residual': env['ADDP_TEST_RESIDUAL'] = '1'
                result = self.run_gate(**env)
                self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
                trace = self.host.trace.read_text()
                self.assertIn('native:stop', trace)
                self.assertIn('business:stop', trace)
                self.assertIn('infra-down', trace)
                self.assertFalse(self.host.secrets.exists())
                self.assertIn('result=failed', (self.host.artifacts / 'summary.txt').read_text())
                if failure == 'prepare': self.assertNotIn('make:test-online', trace)

    def test_late_writer_recovery_precedes_destruction_and_failure_remains_failed(self):
        result = self.run_gate('manager-late-writer', NATIVE_FAIL='recover-writer')
        self.assertNotEqual(result.returncode, 0)
        trace = self.host.trace.read_text()
        order = ['execution-resume', 'native:recover-writer', 'down --remove-orphans --volumes', 'native:stop', 'business:stop', 'infra-down']
        self.assertEqual([trace.index(x) for x in order], sorted(trace.index(x) for x in order))
        self.assertIn('control_recovery=failed', (self.host.artifacts / 'execution-recovery.txt').read_text())
        self.assertIn('cleanup=failed', (self.host.artifacts / 'summary.txt').read_text())
        self.assertFalse(self.host.secrets.exists())

    def test_empty_all_and_unrelated_cases_rejected_before_admission_or_mutation(self):
        for case in ('', 'all', 'manager-other', 'full'):
            result = self.run_gate(case)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(self.host.trace.exists())
            self.assertFalse(self.host.secrets.exists())
