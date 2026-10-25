from importlib import import_module
import os
from pathlib import Path
import shutil
import subprocess

HIGHGO=import_module('scripts.test.online-hosted-highgo-gate_test')
ROOT=Path(__file__).resolve().parents[2]


class HostedFTPTest(HIGHGO.HostedHighGoTest):
    """Reuse the existing disposable Hosted harness, with FTP-only wiring."""
    def setUp(self):
        super().setUp()
        shutil.copyfile(ROOT/'scripts/test/online-hosted-ftp-gate.sh',self.host.repository/'scripts/test/online-hosted-ftp-gate.sh')
        self.host._write_repository_script('business/ftp/admission.py','''
            import os,sys
            assert os.environ['ADDP_FTP_ADMISSION_DIR']==os.environ['ADDP_ONLINE_SECRET_DIR']
            with open(os.environ['ADDP_TEST_GATE_TRACE'],'a') as trace:trace.write('ftp-fixture:'+sys.argv[1]+'\\n')
            if os.environ.get('ADDP_TEST_FIXTURE_FAIL')==sys.argv[1]:sys.exit(1)
        ''')
        self.host._write_repository_script('scripts/test/online-file-model-nfs-fixture.py', """
            import os,sys
            from pathlib import Path
            action=sys.argv[1]
            with open(os.environ['ADDP_TEST_GATE_TRACE'],'a') as trace:trace.write('nfs-fixture:'+action+'\\n')
            if action=='start':
                Path(os.environ['ADDP_ONLINE_SECRET_DIR'],'ftp-root').write_text('/tmp/owned-nfs-fixture')
            if os.environ.get('ADDP_TEST_NFS_FIXTURE_FAIL')==action:sys.exit(1)
        """)
        self.host._write_repository_script('scripts/test/online-file-model-minio-fixture.py', """
            import os,sys
            with open(os.environ['ADDP_TEST_GATE_TRACE'],'a') as trace:trace.write('minio-fixture:'+sys.argv[1]+'\\n')
            if os.environ.get('ADDP_TEST_MINIO_FIXTURE_FAIL')==sys.argv[1]:sys.exit(1)
        """)
        # Retain the shared identity fixture while enforcing the existing
        # System-owned readiness observation before any consumer is exported.
        identity=(self.host.bin/'go').read_text()
        self.host._executable('go',identity.replace('previous=', '''
            [[ "$*" == *'--wait-protection-owners manager'* ]] || exit 1
            [ "${ADDP_TEST_PROTECTION_WAIT_FAIL:-0}" != 1 ] || exit 1
            previous=''',1))
        self.host._executable('make','''
            #!/usr/bin/env bash
            echo "make:$*" >> "$ADDP_TEST_GATE_TRACE"
            if [ "$1" = test-common-nfs-integration ]; then
              [ -n "$ADDP_NFS_TEST_EXPORT" ] && [ "${ADDP_TEST_NFS_PROTOCOL_FAIL:-0}" != 1 ]; exit $?
            fi
            [ "$ADDP_ONLINE_TEST_TIMEOUT_SECONDS" = 1200 ] || exit 1
            [ "${ADDP_ONLINE_CONSUMER_ENGINE_ID:-17}" = 17 ] &&
              [ "$SECURITY_URL" = http://127.0.0.1:8194 ] &&
              [ "$ADDP_ONLINE_TEST_USER_USERNAME" = external-online-consumer ] &&
              [ -z "${ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN:-}" ] || exit 1
            [ "${ADDP_TEST_SUITE_FAIL:-0}" != 1 ]
        ''')
        import_module('scripts.test.online-hosted-hdfs-gate_test').install_spark_container_fixture(
            self.host, 'ftp-consumer-flow', 'addp-ftp-model3d-runtime')
        docker = self.host.bin / 'docker'
        docker.write_text(docker.read_text()
            .replace("*Config.Cmd*) echo '[\"python\",\"api_server.py\"]'",
                     "*Config.Cmd*) if [ \"${ADDP_TEST_ENTRY_MISMATCH:-0}\" = 1 ]; then echo '[\"wrong-entry\"]'; else echo '[\"python\",\"api_server.py\"]'; fi")
            .replace('case "$1 $2" in', '''
            if [ "$1" = logs ]; then
              echo "runtime-logs:$2" >> "$ADDP_TEST_GATE_TRACE"
              echo "runtime diagnostic for $2"
              [ "${ADDP_TEST_MODEL3D_LOG_FAIL:-0}" != 1 ] || exit 1
              exit 0
            fi
            case "$1 $2" in''', 1))
        self.host._write_repository_script('scripts/build/build-images.sh', '''
            #!/usr/bin/env bash
            echo "build:$*" >> "$ADDP_TEST_GATE_TRACE"
            [ "$MODEL3D_DOCKER_PLATFORM" = linux/amd64 ]
            [ "${ADDP_TEST_BUILD_FAIL:-0}" != 1 ] || exit 1
            touch "${ADDP_TEST_GATE_TRACE}.image"
        ''')
        self.host._executable('curl', '#!/usr/bin/env bash\nexit 0\n')
        self.host._write_repository_script('.env.example', 'MODEL3D_WORKFLOW_PORT=8101\n')
        subprocess.run(['git','add','.'],cwd=self.host.repository,check=True)
        subprocess.run(['git','commit','-qm','ftp gate fixture'],cwd=self.host.repository,check=True)

    def run_gate(self,check=False,**overrides):
        environment={**os.environ,'PATH':f'{self.host.bin}:{os.environ["PATH"]}',
            'GITHUB_RUN_ID':'4242','GITHUB_RUN_ATTEMPT':'1',
            'GITHUB_ACTIONS':'true','RUNNER_OS':'Linux','RUNNER_TEMP':str(self.host.root),
            'ADDP_ONLINE_HOST':'1','ADDP_ONLINE_HOSTED':'1','ONLINE_SUITE_INPUT':'ftp-consumer-flow',
            'ADDP_ONLINE_ARTIFACT_DIR':str(self.host.artifacts),'ADDP_ONLINE_SECRET_DIR':str(self.host.secrets),
            'ADDP_TEST_GATE_TRACE':str(self.host.trace),'ADDP_TEST_BACKGROUND_PIDS':str(self.host.background_pids),**overrides}
        try:
            return subprocess.run(['bash','scripts/test/online-hosted-ftp-gate.sh']+(['--check-only'] if check else []),cwd=self.host.repository,env=environment,capture_output=True,text=True,timeout=20)
        except subprocess.TimeoutExpired:
            # This harness contains only disposable fake commands. Report its
            # owned lifecycle facts, never the runtime env or captured credentials.
            trace = self.host.trace.read_text() if self.host.trace.exists() else '<no trace>'
            summary_path = self.host.artifacts / 'summary.txt'
            summary = summary_path.read_text() if summary_path.exists() else '<no summary>'
            self.fail(f'FTP lifecycle harness exceeded 20s; trace={trace!r}; summary={summary!r}')

    def test_success_registers_two_engines_and_checks_native_then_cleans_everything(self):
        # Override the inherited two-engine case: FTP admits only reads.
        result=self.run_gate()
        self.assertEqual(result.returncode,0,result.stderr+result.stdout)
        trace=self.host.trace.read_text()
        order=['docker-run:addp-ftp-online-registry','build:','infra-up','ftp-fixture:start','start:-security','start:-meta','start:-manager','docker-run:addp-ftp-model3d-runtime','--suite ftp-consumer-flow',
            '--wait-protection-owners manager',
            'register:ftp-engine.json','register:ftp-nfs-engine.json','register:ftp-minio-engine.json','make:test-online ONLINE_SUITE=ftp-consumer-flow',
            'application-stop','docker-rm:addp-ftp-model3d-runtime','ftp-fixture:stop','docker-rm:addp-ftp-online-registry','infra-down']
        positions=[trace.index(step) for step in order]
        self.assertEqual(positions,sorted(positions))
        self.assertEqual(trace.count('register:'),3)
        self.assertNotIn('start:-transfer',trace)
        self.assertFalse(self.host.secrets.exists())
        self.assertIn('infra_cleanup=zero_residuals',(self.host.artifacts/'summary.txt').read_text())

    def test_failure_at_each_owned_stage_still_cleans_platform_and_fixture(self):
        for flags in ({'ADDP_TEST_FIXTURE_FAIL':'start'},{'ADDP_TEST_IDENTITY_FAIL':'1'},
                      {'ADDP_TEST_PROTECTION_WAIT_FAIL':'1'},{'ADDP_TEST_MINIO_FIXTURE_FAIL':'start'},
                      {'ADDP_TEST_REGISTRATION_FAIL':'1'},{'ADDP_TEST_SUITE_FAIL':'1'}):
            with self.subTest(flags=flags):
                self.host.trace.unlink(missing_ok=True)
                result=self.run_gate(**flags)
                self.assertNotEqual(result.returncode,0)
                trace=self.host.trace.read_text()
                self.assertIn('ftp-fixture:stop',trace)
                self.assertIn('infra-down',trace)
                self.assertFalse(self.host.secrets.exists())
                if flags.get('ADDP_TEST_PROTECTION_WAIT_FAIL')=='1':
                    self.assertNotIn('register:',trace)
                    self.assertNotIn('make:test-online',trace)

    def test_build_failure_cleans_registry_before_infra_start(self):
        result = self.run_gate(ADDP_TEST_BUILD_FAIL='1')
        self.assertNotEqual(result.returncode, 0)
        trace = self.host.trace.read_text()
        self.assertIn('docker-rm:addp-ftp-online-registry', trace)
        self.assertNotIn('infra-up', trace)
        self.assertFalse(self.host.secrets.exists())

    def test_runtime_failure_still_cleans_source_platform_and_registry(self):
        result = self.run_gate(ADDP_TEST_RUNTIME_FAIL='1')
        self.assertNotEqual(result.returncode, 0)
        trace = self.host.trace.read_text()
        for step in ('ftp-fixture:stop', 'docker-rm:addp-ftp-online-registry', 'infra-down'):
            self.assertIn(step, trace)
        self.assertNotIn('make:test-online', trace)
        self.assertFalse(self.host.secrets.exists())

    def test_runtime_log_failure_is_failure_but_does_not_skip_cleanup(self):
        result = self.run_gate(ADDP_TEST_MODEL3D_LOG_FAIL='1')
        self.assertNotEqual(result.returncode, 0)
        trace = self.host.trace.read_text()
        for step in ('docker-rm:addp-ftp-model3d-runtime', 'ftp-fixture:stop',
                     'docker-rm:addp-ftp-online-registry', 'infra-down'):
            self.assertIn(step, trace)
        self.assertIn('cleanup=failed', (self.host.artifacts/'summary.txt').read_text())
        self.assertFalse(self.host.secrets.exists())

    def test_run_identity_required_before_any_owned_mutation(self):
        for key in ('GITHUB_RUN_ID', 'GITHUB_RUN_ATTEMPT'):
            with self.subTest(key=key):
                result = self.run_gate(**{key: ''})
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(self.host.trace.exists())
                self.assertFalse(self.host.secrets.exists())

    def test_occupied_model_runtime_or_image_rejected_before_mutation(self):
        for flags in ({'ADDP_TEST_PREEXIST_CONTAINER': 'addp-ftp-model3d-runtime'},
                      {'ADDP_TEST_PREEXIST_IMAGE': '1'}):
            with self.subTest(flags=flags):
                result = self.run_gate(check=True, **flags)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(self.host.trace.exists())
                self.assertFalse(self.host.secrets.exists())

    def test_wrong_default_entry_rejected_before_user_or_converter_requests(self):
        result = self.run_gate(ADDP_TEST_ENTRY_MISMATCH='1')
        self.assertNotEqual(result.returncode, 0)
        trace = self.host.trace.read_text()
        self.assertNotIn('identity:', trace)
        self.assertNotIn('make:test-online', trace)
        for step in ('docker-rm:addp-ftp-model3d-runtime', 'ftp-fixture:stop',
                     'docker-rm:addp-ftp-online-registry', 'infra-down'):
            self.assertIn(step, trace)
