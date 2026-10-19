import importlib
import os
import shutil
import subprocess
import unittest
from pathlib import Path

SUPPORT = importlib.import_module('scripts.test.online-hosted-opengauss-gate_test')
SCRIPT = Path(__file__).with_name('online-hosted-administrator-governance-gate.sh')


class HostedAdministratorGovernanceTest(unittest.TestCase):
    def setUp(self):
        self.host = SUPPORT.OnlineHostedOpenGaussGateTest()
        self.host.setUp();self.addCleanup(self.host.tearDown)
        self.host.prepare_run_fixture()
        shutil.copy2(SCRIPT, self.host.repository/'scripts/test'/SCRIPT.name)
        self.host._executable('npm', '''#!/usr/bin/env bash
            echo browser-prepare >> "$ADDP_TEST_GATE_TRACE"
            [ "${ADDP_TEST_BROWSER_FAIL:-0}" != 1 ]
        ''')
        self.host._executable('make', '''#!/usr/bin/env bash
            echo "make:$*" >> "$ADDP_TEST_GATE_TRACE"
            [ "${ADDP_TEST_SCENE_FAIL:-0}" != 1 ]
        ''')
        self.host._write_repository_script('scripts/infra/down.sh', '''#!/usr/bin/env bash
            echo infra-down >> "$ADDP_TEST_GATE_TRACE"
            [ "${ADDP_TEST_CLEANUP_FAIL:-0}" != 1 ]
        ''')
        subprocess.run(['git','add','.'],cwd=self.host.repository,check=True)
        subprocess.run(['git','commit','-qm','governance lifecycle fixture'],cwd=self.host.repository,check=True)

    def run_gate(self, check=False, **overrides):
        env = dict(os.environ, PATH=f'{self.host.bin}:{os.environ["PATH"]}', GITHUB_ACTIONS='true', RUNNER_OS='Linux',
                   RUNNER_TEMP=str(self.host.root), ADDP_ONLINE_HOST='1', ADDP_ONLINE_HOSTED='1',
                   ONLINE_SUITE_INPUT='platform-administrator-governance',
                   ADDP_ONLINE_ARTIFACT_DIR=str(self.host.artifacts), ADDP_ONLINE_SECRET_DIR=str(self.host.secrets),
                   ADDP_TEST_GATE_TRACE=str(self.host.trace), ADDP_TEST_BACKGROUND_PIDS=str(self.host.background_pids))
        env.update(overrides)
        return subprocess.run(['bash', 'scripts/test/'+SCRIPT.name]+(['--check-only'] if check else []),
                              cwd=self.host.repository, env=env, capture_output=True, text=True, timeout=20)

    def test_readiness_is_readonly_and_personal_environment_is_rejected(self):
        result=self.run_gate(check=True)
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        self.assertFalse(self.host.trace.exists());self.assertFalse(self.host.secrets.exists())
        for overrides in ({'GITHUB_ACTIONS':'false'},{'RUNNER_OS':'macOS'},{'ADDP_TEST_UNAME_S':'Darwin'},
                          {'ADDP_ONLINE_HOSTED':'0'},{'ADDP_ONLINE_HOST':'0'},{'ADDP_ONLINE_OWNER_MANAGED':'1'}):
            result=self.run_gate(**overrides)
            self.assertNotEqual(result.returncode,0)
            self.assertFalse(self.host.trace.exists());self.assertFalse(self.host.secrets.exists())

    def test_existing_environment_and_dirty_checkout_are_rejected_before_lifecycle(self):
        (self.host.repository/'.env').write_text('POSTGRES_DB=addp\n')
        self.assertNotEqual(self.run_gate().returncode,0)
        self.assertFalse(self.host.trace.exists())
        (self.host.repository/'.env').unlink()
        (self.host.repository/'uncommitted.txt').write_text('other session')
        self.assertNotEqual(self.run_gate().returncode,0)
        self.assertFalse(self.host.trace.exists())
        (self.host.repository/'uncommitted.txt').unlink()
        self.host._executable('docker', '''#!/usr/bin/env bash
            if [ "$1 $2" = "compose version" ]; then exit 0; fi
            if [ "$1 $2" = "container inspect" ]; then exit 0; fi
            exit 0
        ''')
        self.assertNotEqual(self.run_gate().returncode,0)
        self.assertFalse(self.host.trace.exists());self.assertFalse(self.host.secrets.exists())

    def test_success_uses_shared_owned_cleanup_and_destroys_credentials(self):
        result=self.run_gate()
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        trace=self.host.trace.read_text()
        for expected in ('infra-up','start:-system','browser-prepare','make:test-online ONLINE_SUITE=platform-administrator-governance','application-stop','infra-down'):
            self.assertIn(expected,trace)
        self.assertLess(trace.index('application-stop'),trace.index('infra-down'))
        self.assertFalse(self.host.secrets.exists())
        self.assertIn('infra_cleanup=zero_residuals',(self.host.artifacts/'summary.txt').read_text())

    def test_scene_browser_and_cleanup_failures_cannot_pass(self):
        for override in ('ADDP_TEST_BROWSER_FAIL','ADDP_TEST_SCENE_FAIL','ADDP_TEST_CLEANUP_FAIL'):
            with self.subTest(override=override):
                result=self.run_gate(**{override:'1'})
                self.assertNotEqual(result.returncode,0)
                self.assertFalse(self.host.secrets.exists())
                self.assertIn('result=failed',(self.host.artifacts/'summary.txt').read_text())
                if self.host.trace.exists():self.host.trace.unlink()


if __name__ == '__main__':unittest.main()
