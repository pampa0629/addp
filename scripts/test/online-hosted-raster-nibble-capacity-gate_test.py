import importlib
import os
import subprocess

base = importlib.import_module('scripts.test.online-hosted-raster-contour-gate_test')


class HostedNibbleCapacityGateTest(base.HostedContourGateTest):
    prefix = 'nibble-capacity'

    def setUp(self):
        super().setUp()
        docker = self.host.bin/'docker'
        docker.write_text(docker.read_text().replace('raster-nibble-capacity-persistence', 'raster-nibble-capacity')
                          .replace('echo "docker-run:$name"', 'echo "docker-args:$*" >> "$ADDP_TEST_GATE_TRACE"\n              echo "docker-run:$name"'))
        make = self.host.bin/'make'; make.write_text(make.read_text().replace('= 600', '= 10800'))

    def run_gate(self, check=False, argument=None, **overrides):
        env = dict(os.environ, PATH=f'{self.host.bin}:{os.environ["PATH"]}', GITHUB_ACTIONS='true', GITHUB_RUN_ID='123',
                   GITHUB_RUN_ATTEMPT='1', RUNNER_OS='Linux', RUNNER_TEMP=str(self.host.root), ADDP_ONLINE_HOST='1',
                   ADDP_ONLINE_HOSTED='1', ONLINE_SUITE_INPUT='raster-nibble-capacity',
                   ADDP_ONLINE_ARTIFACT_DIR=str(self.host.artifacts), ADDP_ONLINE_SECRET_DIR=str(self.host.secrets),
                   ADDP_TEST_GATE_TRACE=str(self.host.trace), ADDP_TEST_BACKGROUND_PIDS=str(self.host.background_pids))
        env.update(overrides)
        return subprocess.run(['bash', 'scripts/test/online-hosted-raster-nibble-capacity-gate.sh']+([argument] if argument else ['--check-only'] if check else []),
                              cwd=self.host.repository, env=env, capture_output=True, text=True, timeout=60)

    def test_default_product_entry_and_same_raster_identity_precede_business(self):
        result = self.run_gate(); self.assertEqual(result.returncode, 0, result.stderr+result.stdout)
        trace = self.host.trace.read_text()
        for part in ('--cpus 4 --memory 2g --memory-swap 2g', 'TMPDIR=/tmp/owned-nibble-capacity/runtime-tmp',
                     '--suite raster-workflow --wait-protection-owners develop,manager',
                     'nibble-capacity:seed', 'make:test-online ONLINE_SUITE=raster-nibble-capacity',
                     'nibble-capacity:stop', 'infra-down'):
            self.assertIn(part, trace)
        self.assertFalse(self.host.secrets.exists())

    def test_relaunch_rejects_foreign_identity_and_overlapping_evidence_before_container_mutation(self):
        self.host.secrets.mkdir(mode=0o700); self.host.artifacts.mkdir()
        for override in ({'ADDP_ONLINE_TEST_RUN_ID': 'foreign'},
                         {'ADDP_ONLINE_HOSTED': '0'},
                         {'ADDP_ONLINE_ARTIFACT_DIR': str(self.host.secrets)}):
            result = self.run_gate(argument='--start-runtime', ADDP_ONLINE_TEST='1',
                                   ADDP_ONLINE_TEST_RUN_ID='nibble-capacity-123-1', GEOPYTHON_WORKFLOW_PORT='8099',
                                   **{key:value for key,value in override.items() if key != 'ADDP_ONLINE_TEST_RUN_ID'},
                                   **({'GITHUB_RUN_ID':'456'} if 'ADDP_ONLINE_TEST_RUN_ID' in override else {}))
            self.assertNotEqual(result.returncode, 0)
            trace = self.host.trace.read_text() if self.host.trace.exists() else ''
            self.assertNotIn('docker-run:', trace)
