"""Reuse Hosted admission fixtures; simulate only owned lifecycle effects for T1."""
from importlib import import_module
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import unittest

SUPPORT = import_module('scripts.test.online-hosted-opengauss-gate_test')
ROOT = Path(__file__).resolve().parents[2]


class HostedWorkflowHealthTest(unittest.TestCase):
    def setUp(self):
        self.host = SUPPORT.OnlineHostedOpenGaussGateTest()
        self.host.setUp()
        self.addCleanup(self.host.tearDown)
        self.host.prepare_run_fixture()
        shutil.copy2(ROOT/'scripts/test/online-hosted-workflow-health-gate.sh',self.host.repository/'scripts/test/online-hosted-workflow-health-gate.sh')
        shutil.copy2(ROOT/'scripts/test/workflow-runtime-health-tool.py',self.host.repository/'scripts/test/workflow-runtime-health-tool.py')
        self.state = self.host.root/'state'
        self.state.mkdir()
        self.host._write_repository_script('.env.example','MODEL3D_WORKFLOW_PORT=8101\nPOINTCLOUD_WORKFLOW_PORT=8102\nDOCUMENT_WORKFLOW_PORT=8105\n')
        self.host._write_repository_script('scripts/test/workflow-runtime-health-recovery-online.py', '''
            import os,sys
            event='assert-empty' if '--assert-empty' in sys.argv else 'restore-tools'
            with open(os.environ['ADDP_TEST_GATE_TRACE'],'a') as stream:stream.write(event+'\\n')
            if '--restore-only' in sys.argv and os.environ.get('ADDP_TEST_RESTORE_FAIL')=='1':raise SystemExit(1)
        ''')
        self.host._executable('python3', f'''#!{sys.executable}
import os,sys,pathlib
if sys.argv[1:3]==['-m','venv']:
 p=pathlib.Path(sys.argv[3])/'bin';p.mkdir(parents=True)
 (p/'python').write_text('#!/bin/bash\\nif [ "$1" = -m ]; then exit 0; fi\\nexec {sys.executable} "$@"\\n');(p/'python').chmod(0o755)
else:os.execv({sys.executable!r},[{sys.executable!r},*sys.argv[1:]])
''')
        self.host._executable('curl','#!/bin/bash\nexit 0\n')
        go = (self.host.bin/'go').read_text()
        self.host._executable('go',go.replace('previous=', 'echo "identity:$*" >> "$ADDP_TEST_GATE_TRACE"\nprevious=',1))
        self.host._executable('make', f'''#!{sys.executable}
import os,sys,pathlib
trace=pathlib.Path(os.environ['ADDP_TEST_GATE_TRACE'])
with trace.open('a') as s:s.write('make:'+sys.argv[1]+'\\n')
if sys.argv[1]=='build-images':
 if os.environ.get('ADDP_TEST_BUILD_FAIL')=='1':sys.exit(1)
 for name in ('model3d-converter','model3d-workflow-engine','pointcloud-workflow-engine','document-workflow-engine'):
  (pathlib.Path(os.environ['ADDP_TEST_STATE'])/('image-'+name+':online')).touch()
else:sys.exit(int(os.environ.get('ADDP_TEST_SUITE_FAIL','0')))
''')
        self.host._executable('docker', f'''#!{sys.executable}
import os,sys,pathlib,json
args=sys.argv[1:];state=pathlib.Path(os.environ['ADDP_TEST_STATE']);trace=pathlib.Path(os.environ['ADDP_TEST_GATE_TRACE'])
def log(s):
 with trace.open('a') as f:f.write(s+'\\n')
def image(name):return state/('image-'+name.split('/addp-')[-1])
if args[:2]==['compose','version']:sys.exit(0)
if args[0]=='ps' or args[:2] in (['network','ls'],['volume','ls']):sys.exit(0)
if args[:2]==['container','inspect']:sys.exit(0 if (state/args[2]).exists() else 1)
if args[:2]==['image','inspect']:
 if not image(args[-1]).exists():sys.exit(1)
 print('10001' if '{{{{.Config.User}}}}' in args else 'sha256:product-image');sys.exit(0)
if args[:2]==['image','rm']:
 log('image-rm');image(args[2]).unlink(missing_ok=True);sys.exit(0)
if args[0]=='build':
 log('fault-image:'+args[args.index('--tag')+1].split('/addp-')[-1])
 assert 'RUNTIME_USER=10001' in args
 source=(pathlib.Path(args[-1])/'Dockerfile').read_text()
 assert 'image-missing' in source and 'USER ${{RUNTIME_USER}}' in source and 'CMD' not in source and 'ENTRYPOINT' not in source
 if os.environ.get('ADDP_TEST_FAULT_IMAGE_FAIL')=='1':sys.exit(1)
 image(args[args.index('--tag')+1]).touch();sys.exit(0)
if args[0]=='run':
 name=args[args.index('--name')+1];log('docker-run:'+name)
 (state/name).touch()
 if os.environ.get('ADDP_TEST_RUNTIME_FAIL')=='1' and name=='addp-health-model3d':sys.exit(1)
 sys.exit(0)
if args[0]=='inspect':
 name=args[-1]
 if not (state/name).exists():sys.exit(1)
 template=args[args.index('-f')+1]
 if 'com.addp.online-runtime' in template:print('workflow-runtime-health-recovery')
 elif 'com.addp.online-run-id' in template:print(os.environ['ADDP_ONLINE_TEST_RUN_ID'])
 elif 'Config.Cmd' in template:print('["python","api_server.py"]')
 elif 'State.Running' in template:print('true')
 else:print('sha256:product-image')
 sys.exit(0)
if args[0]=='logs':log('runtime-logs:'+args[1]);print('safe runtime log');sys.exit(0)
if args[0]=='rm':
 name=args[-1];log('docker-rm:'+name)
 if os.environ.get('ADDP_TEST_CLEANUP_FAIL')=='1' and name=='addp-health-pointcloud':sys.exit(1)
 (state/name).unlink(missing_ok=True);sys.exit(0)
sys.exit(1)
''')
        subprocess.run(['git','add','.'],cwd=self.host.repository,check=True)
        subprocess.run(['git','commit','-qm','health fixture'],cwd=self.host.repository,check=True)

    def run_gate(self, check=False, **flags):
        env=dict(os.environ, PATH=f'{self.host.bin}:{os.environ["PATH"]}',GITHUB_ACTIONS='true',RUNNER_OS='Linux',
                 RUNNER_TEMP=str(self.host.root),GITHUB_RUN_ID='42',GITHUB_RUN_ATTEMPT='1',
                 ADDP_ONLINE_HOST='1',ADDP_ONLINE_HOSTED='1',ONLINE_SUITE_INPUT='workflow-runtime-health-recovery',
                 ADDP_ONLINE_ARTIFACT_DIR=str(self.host.artifacts),ADDP_ONLINE_SECRET_DIR=str(self.host.secrets),
                 ADDP_TEST_GATE_TRACE=str(self.host.trace),ADDP_TEST_STATE=str(self.state),
                 ADDP_TEST_BACKGROUND_PIDS=str(self.host.background_pids))
        env.update(flags)
        return subprocess.run(['bash','scripts/test/online-hosted-workflow-health-gate.sh']+(['--check-only'] if check else []),
                              cwd=self.host.repository,env=env,capture_output=True,text=True,timeout=25)

    def test_read_only_admission_rejects_personal_and_wrong_profiles(self):
        result=self.run_gate(check=True)
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertFalse(self.host.trace.exists())
        for flags in ({'ADDP_ONLINE_HOSTED':'0'},{'GITHUB_ACTIONS':'false'},{'ONLINE_SUITE_INPUT':'other-suite'}):
            with self.subTest(flags=flags):
                self.assertNotEqual(self.run_gate(check=True,**flags).returncode,0)
                self.assertFalse(self.host.trace.exists())
        (self.host.repository/'.env').touch()
        self.assertNotEqual(self.run_gate(check=True).returncode,0)

    def test_real_entry_wiring_and_owned_cleanup_order(self):
        result=self.run_gate()
        self.assertEqual(result.returncode,0,result.stderr+result.stdout)
        trace=self.host.trace.read_text()
        order=['docker-run:addp-health-registry','make:build-images','infra-up','start:--exact-process -system',
               'start:--exact-process -gateway','--suite workflow-runtime-health-recovery','assert-empty',
               'fault-image:pointcloud-workflow-engine:startup-missing','docker-run:addp-health-pointcloud',
               'docker-run:addp-health-model3d','docker-run:addp-health-document','make:test-online',
               'restore-tools','application-stop','docker-rm:addp-health-pointcloud','docker-rm:addp-health-registry','infra-down']
        offsets=[trace.index(item) for item in order]
        self.assertEqual(offsets,sorted(offsets))
        self.assertFalse(list(self.state.iterdir()))
        self.assertFalse(self.host.secrets.exists())
        self.assertIn('infra_cleanup=zero_residuals',(self.host.artifacts/'summary.txt').read_text())
        self.assertIn('runtime_cleanup=zero_residuals',(self.host.artifacts/'runtime-cleanup.txt').read_text())

    def test_owned_failures_cleanup_and_residuals_cannot_pass(self):
        for key in ('ADDP_TEST_BUILD_FAIL','ADDP_TEST_FAULT_IMAGE_FAIL','ADDP_TEST_RUNTIME_FAIL','ADDP_TEST_SUITE_FAIL','ADDP_TEST_RESTORE_FAIL','ADDP_TEST_CLEANUP_FAIL'):
            with self.subTest(key=key):
                self.host.trace.unlink(missing_ok=True)
                result=self.run_gate(**{key:'1'})
                self.assertNotEqual(result.returncode,0,result.stdout+result.stderr)
                self.assertFalse(self.host.secrets.exists())
                if key=='ADDP_TEST_CLEANUP_FAIL':
                    self.assertIn('cleanup=failed',(self.host.artifacts/'summary.txt').read_text())
                    for path in self.state.iterdir():path.unlink()
                else:self.assertFalse(list(self.state.iterdir()))

    def test_shared_healthy_readiness_still_checks_product_identity(self):
        # Cover the existing healthy caller after extracting identity checks.
        (self.state/'image-pointcloud-workflow-engine:online').touch()
        (self.state/'addp-health-pointcloud').touch()
        self.host.artifacts.mkdir(exist_ok=True)
        env=dict(os.environ, PATH=f'{self.host.bin}:{os.environ["PATH"]}',ADDP_TEST_STATE=str(self.state),
                 ADDP_TEST_GATE_TRACE=str(self.host.trace),ADDP_ONLINE_ARTIFACT_DIR=str(self.host.artifacts))
        script='''source scripts/utils/hosted-product-runtime.sh
RUNTIME_IMAGE=127.0.0.1:5001/addp-pointcloud-workflow-engine:online
RUNTIME_CONTAINER=addp-health-pointcloud
RUNTIME_REPORT=pointcloud
fail() { exit 1; }
verify_online_product_runtime '["python","api_server.py"]' http://127.0.0.1/health
'''
        result=subprocess.run(['bash','-euc',script],cwd=self.host.repository,env=env,capture_output=True,text=True,timeout=10)
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertIn('default_entry=python api_server.py',(self.host.artifacts/'pointcloud-runtime-build.txt').read_text())

if __name__=='__main__':unittest.main()
