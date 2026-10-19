from importlib import import_module
import os
from pathlib import Path
import shutil
import subprocess

HIGHGO=import_module('scripts.test.online-hosted-highgo-gate_test')
ROOT=Path(__file__).resolve().parents[2]


class HostedStarRocksTest(HIGHGO.HostedHighGoTest):
    """Reuse the existing disposable Hosted harness, with StarRocks-only wiring."""
    def setUp(self):
        super().setUp()
        shutil.copyfile(ROOT/'scripts/test/online-hosted-starrocks-gate.sh',self.host.repository/'scripts/test/online-hosted-starrocks-gate.sh')
        self.host._write_repository_script('business/starrocks/admission.py','''
            import os,sys
            assert os.environ['ADDP_STARROCKS_ADMISSION_DIR']==os.environ['ADDP_ONLINE_SECRET_DIR']
            with open(os.environ['ADDP_TEST_GATE_TRACE'],'a') as trace:trace.write('starrocks-fixture:'+sys.argv[1]+'\\n')
            if os.environ.get('ADDP_TEST_FIXTURE_FAIL')==sys.argv[1]:sys.exit(1)
        ''')
        # Retain the shared identity fixture while enforcing the existing
        # System-owned readiness observation before any consumer is exported.
        identity=(self.host.bin/'go').read_text()
        self.host._executable('go',identity.replace('previous=', '''
            [[ "$*" == *'--wait-protection-owners manager,develop'* ]] || exit 1
            [ "${ADDP_TEST_PROTECTION_WAIT_FAIL:-0}" != 1 ] || exit 1
            previous=''',1))
        self.host._executable('make','''
            #!/usr/bin/env bash
            echo "make:$*" >> "$ADDP_TEST_GATE_TRACE"
            [ "$ADDP_ONLINE_CONSUMER_ENGINE_ID" = 17 ] &&
              [ "$SECURITY_URL" = http://127.0.0.1:8194 ] &&
              [ "$DEVELOP_URL" = http://127.0.0.1:8185 ] &&
              [ "$ADDP_ONLINE_TEST_USER_USERNAME" = external-online-consumer ] &&
              [ -z "${ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN:-}" ] || exit 1
            [ "${ADDP_TEST_SUITE_FAIL:-0}" != 1 ]
        ''')
        subprocess.run(['git','add','.'],cwd=self.host.repository,check=True)
        subprocess.run(['git','commit','-qm','starrocks gate fixture'],cwd=self.host.repository,check=True)

    def run_gate(self,check=False,**overrides):
        environment={**os.environ,'PATH':f'{self.host.bin}:{os.environ["PATH"]}',
            'GITHUB_ACTIONS':'true','RUNNER_OS':'Linux','RUNNER_TEMP':str(self.host.root),
            'ADDP_ONLINE_HOST':'1','ADDP_ONLINE_HOSTED':'1','ONLINE_SUITE_INPUT':'starrocks-consumer-flow',
            'ADDP_ONLINE_ARTIFACT_DIR':str(self.host.artifacts),'ADDP_ONLINE_SECRET_DIR':str(self.host.secrets),
            'ADDP_TEST_GATE_TRACE':str(self.host.trace),'ADDP_TEST_BACKGROUND_PIDS':str(self.host.background_pids),**overrides}
        return subprocess.run(['bash','scripts/test/online-hosted-starrocks-gate.sh']+(['--check-only'] if check else []),cwd=self.host.repository,env=environment,capture_output=True,text=True,timeout=20)

    def test_success_registers_two_engines_and_checks_native_then_cleans_everything(self):
        # Override the inherited two-engine case: StarRocks admits only reads.
        result=self.run_gate()
        self.assertEqual(result.returncode,0,result.stderr+result.stdout)
        trace=self.host.trace.read_text()
        order=['infra-up','starrocks-fixture:start','starrocks-fixture:persistence','start:-security','start:-manager','start:-develop','--suite starrocks-consumer-flow',
            '--wait-protection-owners manager,develop',
            'register:starrocks-engine.json','make:test-online ONLINE_SUITE=starrocks-consumer-flow',
            'application-stop','starrocks-fixture:stop','infra-down']
        positions=[trace.index(step) for step in order]
        self.assertEqual(positions,sorted(positions))
        self.assertEqual(trace.count('register:'),1)
        self.assertNotIn('start:-transfer',trace)
        self.assertFalse(self.host.secrets.exists())
        self.assertIn('infra_cleanup=zero_residuals',(self.host.artifacts/'summary.txt').read_text())

    def test_failure_at_each_owned_stage_still_cleans_platform_and_fixture(self):
        for flags in ({'ADDP_TEST_FIXTURE_FAIL':'start'},{'ADDP_TEST_FIXTURE_FAIL':'persistence'},{'ADDP_TEST_IDENTITY_FAIL':'1'},
                      {'ADDP_TEST_PROTECTION_WAIT_FAIL':'1'},
                      {'ADDP_TEST_REGISTRATION_FAIL':'1'},{'ADDP_TEST_SUITE_FAIL':'1'}):
            with self.subTest(flags=flags):
                self.host.trace.unlink(missing_ok=True)
                result=self.run_gate(**flags)
                self.assertNotEqual(result.returncode,0)
                trace=self.host.trace.read_text()
                self.assertIn('starrocks-fixture:stop',trace)
                self.assertIn('infra-down',trace)
                self.assertFalse(self.host.secrets.exists())
                if flags.get('ADDP_TEST_PROTECTION_WAIT_FAIL')=='1':
                    self.assertNotIn('register:',trace)
                    self.assertNotIn('make:test-online',trace)
