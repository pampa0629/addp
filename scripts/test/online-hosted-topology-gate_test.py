import importlib
import os
from pathlib import Path
import shutil
import subprocess
import unittest

base = importlib.import_module('scripts.test.online-hosted-raster-gate_test')


class HostedTopologyGateTest(unittest.TestCase):
    def setUp(self):
        self.base = base.HostedRasterGateTest(); self.base.setUp(); self.addCleanup(self.base.doCleanups)
        self.host = self.base.host
        source = Path(__file__).with_name('online-hosted-topology-gate.sh')
        shutil.copy2(source, self.host.repository / 'scripts/test' / source.name)
        self.host._write_repository_script('business/scripts/online-vector-topology-fixture.py', '''
            import os, sys
            from pathlib import Path
            action = sys.argv[1]
            with open(os.environ['ADDP_TEST_GATE_TRACE'], 'a') as s: s.write('topology:' + action + '\\n')
            if action == 'start':
                root = Path(os.environ['ADDP_ONLINE_SECRET_DIR'])
                (root/'topology-root').write_text('/tmp/owned-topology')
                (root/'topology-nfs-engine.json').write_text('{}')
            if os.environ.get('ADDP_TEST_FAIL_ACTION') == action: sys.exit(1)
        ''')
        self.host._write_repository_script('scripts/test/online-engine-registration.py', '''
            import sys
            from pathlib import Path
            Path(sys.argv[-1]).write_text('export ADDP_ONLINE_CONSUMER_ENGINE_ID=15\\n')
        ''')
        fixture = importlib.import_module('scripts.test.online-hosted-hdfs-gate_test')
        fixture.install_spark_container_fixture(self.host, 'vector-topology-persistence', 'addp-topology-runtime')
        docker = self.host.bin / 'docker'; docker.write_text(docker.read_text().replace('["python","api_server.py"]', '["sh","container_entrypoint.sh"]'))
        self.host._executable('make', '''
            #!/usr/bin/env bash
            echo "make:$*" >> "$ADDP_TEST_GATE_TRACE"
            if [ "$1" = build-images ]; then touch "${ADDP_TEST_GATE_TRACE}.image"; exit 0; fi
            [ "$ADDP_ONLINE_TOPOLOGY_NFS_ENGINE_ID" = 15 ] && [ "$ADDP_ONLINE_TEST_TIMEOUT_SECONDS" = 600 ]
            [ -z "${ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN:-}" ] && [ -z "${POSTGRES_PASSWORD:-}" ]
            [ "${ADDP_TEST_SUITE_FAIL:-0}" != 1 ]
        ''')
        subprocess.run(['git', 'add', '.'], cwd=self.host.repository, check=True)
        subprocess.run(['git', 'commit', '-qm', 'topology fixture'], cwd=self.host.repository, check=True)

    def run_gate(self, check=False, **overrides):
        env = dict(os.environ, PATH=f'{self.host.bin}:{os.environ["PATH"]}', GITHUB_ACTIONS='true', GITHUB_RUN_ID='123',
            GITHUB_RUN_ATTEMPT='1', RUNNER_OS='Linux', RUNNER_TEMP=str(self.host.root), ADDP_ONLINE_HOST='1',
            ADDP_ONLINE_HOSTED='1', ONLINE_SUITE_INPUT='vector-topology-persistence',
            ADDP_ONLINE_ARTIFACT_DIR=str(self.host.artifacts), ADDP_ONLINE_SECRET_DIR=str(self.host.secrets),
            ADDP_TEST_GATE_TRACE=str(self.host.trace), ADDP_TEST_BACKGROUND_PIDS=str(self.host.background_pids))
        env.update(overrides)
        return subprocess.run(['bash', 'scripts/test/online-hosted-topology-gate.sh'] + (['--check-only'] if check else []),
            cwd=self.host.repository, env=env, capture_output=True, text=True, timeout=60)

    def test_admission_is_read_only_and_rejects_personal_context(self):
        self.assertEqual(self.run_gate(check=True).returncode, 0)
        self.assertFalse(self.host.secrets.exists())
        self.assertNotEqual(self.run_gate(GITHUB_ACTIONS='false').returncode, 0)

    def test_default_product_entry_and_same_raster_identity_precede_business(self):
        result = self.run_gate(); self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        trace = self.host.trace.read_text()
        for step in ('topology:start', 'topology:seed', '--suite vector-topology-persistence', '--wait-protection-owners develop,manager',
                     'make:test-online ONLINE_SUITE=vector-topology-persistence', 'topology:stop', 'infra-down'):
            self.assertIn(step, trace)
        self.assertFalse(self.host.secrets.exists())

    def test_seed_and_business_failure_always_clean(self):
        for overrides in ({'ADDP_TEST_FAIL_ACTION': 'seed'}, {'ADDP_TEST_SUITE_FAIL': '1'}):
            # Each lifecycle needs fresh directories and a clean checkout.
            result = self.run_gate(**overrides)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('topology:stop', self.host.trace.read_text())
            self.assertFalse(self.host.secrets.exists())
            if self.host.artifacts.exists(): shutil.rmtree(self.host.artifacts)

    def test_cleanup_failure_invalidates_success(self):
        result = self.run_gate(ADDP_TEST_FAIL_ACTION='stop')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('cleanup=failed', (self.host.artifacts / 'summary.txt').read_text())

    def test_wrong_suite_and_occupied_runtime_are_rejected_before_fixture_start(self):
        for env in ({'ONLINE_SUITE_INPUT': 'raster-workflow'}, {'ADDP_TEST_PREEXIST_CONTAINER': 'addp-topology-runtime'}):
            result = self.run_gate(**env)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(self.host.secrets.exists())
            self.assertNotIn('topology:start', self.host.trace.read_text() if self.host.trace.exists() else '')

    def test_buffer_case_is_bound_without_old_case_execution(self):
        result=self.run_gate(ADDP_ONLINE_TOPOLOGY_CASE='multi-distance-buffer')
        self.assertEqual(result.returncode,0,result.stderr+result.stdout)
        import json
        self.assertEqual(json.loads((self.host.artifacts/'topology-case.json').read_text()),{'suite':'vector-topology-persistence','case':'multi-distance-buffer'})
        self.assertFalse(self.host.secrets.exists())

    def test_composition_case_is_bound_without_old_case_execution(self):
        result=self.run_gate(ADDP_ONLINE_TOPOLOGY_CASE='line-network-normalize-chains')
        self.assertEqual(result.returncode,0,result.stderr+result.stdout)
        import json
        record=json.loads((self.host.artifacts/'topology-case.json').read_text())
        self.assertEqual(record,{'suite':'vector-topology-persistence','case':'line-network-normalize-chains'})
        self.assertFalse(self.host.secrets.exists())

    def test_normalize_case_is_bound_and_unknown_case_has_no_side_effect(self):
        result=self.run_gate(ADDP_ONLINE_TOPOLOGY_CASE='line-network-normalize')
        self.assertEqual(result.returncode,0,result.stderr+result.stdout)
        import json
        record=json.loads((self.host.artifacts/'topology-case.json').read_text())
        self.assertEqual(record,{'suite':'vector-topology-persistence','case':'line-network-normalize'})
        self.assertFalse(self.host.secrets.exists())
        if self.host.artifacts.exists():shutil.rmtree(self.host.artifacts)
        self.host.trace.unlink(missing_ok=True)
        result=self.run_gate(ADDP_ONLINE_TOPOLOGY_CASE='unknown')
        self.assertNotEqual(result.returncode,0)
        self.assertFalse(self.host.secrets.exists())
        self.assertFalse(self.host.trace.exists())
