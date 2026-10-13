import importlib
import os
from pathlib import Path
import shutil
import subprocess
import unittest

support=importlib.import_module('scripts.test.online-hosted-raster-gate_test')
SCRIPT=Path(__file__).with_name('online-hosted-raster-capacity-gate.sh')


class HostedCapacityGateTest(unittest.TestCase):
    def setUp(self):
        self.base=support.HostedRasterGateTest(); self.base.setUp()
        self.host=self.base.host
        self.addCleanup(self.host.tearDown)
        shutil.copy2(SCRIPT,self.host.repository/'scripts/test'/SCRIPT.name)
        source=self.host.repository/'business/scripts/online-raster-minio-fixture.py'
        target=source.with_name('online-raster-capacity-fixture.py')
        target.write_text(source.read_text().replace('raster-online','raster-capacity'))
        docker=self.host.bin/'docker'
        docker.write_text(docker.read_text().replace('raster-workflow','raster-workflow-capacity')
                         .replace('addp-raster-online-runtime','addp-raster-capacity-runtime')
                         .replace('echo "docker-run:$name"', 'echo "docker-args:$*" >> "$ADDP_TEST_GATE_TRACE"\n              echo "docker-run:$name"'))
        go=self.host.bin/'go'
        go.write_text(go.read_text().replace('previous=', 'echo \"identity:$*\" >> \"$ADDP_TEST_GATE_TRACE\"\n            previous=',1))
        make=self.host.bin/'make'
        make.write_text(make.read_text().replace('1740','10800'))
        subprocess.run(['git','add','.'],cwd=self.host.repository,check=True)
        subprocess.run(['git','commit','-qm','capacity fixture'],cwd=self.host.repository,check=True)

    def run_gate(self,argument=None,**overrides):
        env=dict(os.environ,PATH=f'{self.host.bin}:{os.environ["PATH"]}',GITHUB_ACTIONS='true',
                 GITHUB_RUN_ID='123',GITHUB_RUN_ATTEMPT='1',RUNNER_OS='Linux',RUNNER_TEMP=str(self.host.root),
                 ADDP_ONLINE_HOST='1',ADDP_ONLINE_HOSTED='1',ADDP_ONLINE_TEST='1',ONLINE_SUITE_INPUT='raster-workflow-capacity',
                 ADDP_ONLINE_ARTIFACT_DIR=str(self.host.artifacts),ADDP_ONLINE_SECRET_DIR=str(self.host.secrets),
                 ADDP_ONLINE_TEST_RUN_ID='raster-capacity-123-1',
                 ADDP_TEST_GATE_TRACE=str(self.host.trace),ADDP_TEST_BACKGROUND_PIDS=str(self.host.background_pids),
                 POSTGRES_PASSWORD='private-database-marker',GEOPYTHON_WORKFLOW_PORT='8099')
        env.update(overrides)
        return subprocess.run(['bash','scripts/test/'+SCRIPT.name]+([argument] if argument else []),
                              cwd=self.host.repository,env=env,text=True,capture_output=True,timeout=60)

    def test_preflight_is_read_only_and_rejects_occupied_and_personal_environments(self):
        result=self.run_gate('--check-only')
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertFalse(self.host.trace.exists())
        for changes in ({'GITHUB_ACTIONS':'false'},{'RUNNER_OS':'macOS'},
                        {'ADDP_TEST_PREEXIST_CONTAINER':'addp-raster-capacity-target'}, {'ONLINE_SUITE_INPUT':'raster-workflow'}):
            result=self.run_gate('--check-only',**changes)
            self.assertNotEqual(result.returncode,0)
            self.assertFalse(self.host.trace.exists())

    def test_success_uses_product_limits_new_budget_and_cleans_everything(self):
        result=self.run_gate()
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        trace=self.host.trace.read_text()
        for step in ('--cpus 4 --memory 2g --memory-swap 2g localhost:5001/addp-geopython-workflow-engine:raster-capacity',
                     '--suite raster-workflow-capacity','make:test-online ONLINE_SUITE=raster-workflow-capacity',
                     'docker-rm:addp-raster-capacity-runtime','docker-rm:addp-raster-capacity-registry',
                     'raster-fixture:stop','application-stop','infra-down'):
            self.assertIn(step,trace)
        self.assertNotIn('start:-geopython-workflow',trace)
        self.assertFalse(self.host.secrets.exists())
        self.assertIn('cleanup=passed',(self.host.artifacts/'summary.txt').read_text())
        evidence=result.stdout+''.join(p.read_text() for p in self.host.artifacts.rglob('*') if p.is_file())
        self.assertNotIn('private-database-marker',evidence)
        self.assertNotIn('provisioner-token',evidence)

    def test_suite_and_cleanup_failures_cannot_be_reported_as_success(self):
        for flags in ({'ADDP_TEST_SUITE_FAIL':'1'},{'ADDP_TEST_FAIL_ACTION':'seed'},
                      {'ADDP_TEST_FAIL_ACTION':'stop'},{'ADDP_TEST_BAD_IMAGE':'1'},{'ADDP_TEST_BAD_ENTRY':'1'}):
            with self.subTest(flags=flags):
                self.host.trace.unlink(missing_ok=True)
                result=self.run_gate(**flags)
                self.assertNotEqual(result.returncode,0,result.stdout+result.stderr)
                self.assertIn('infra-down',self.host.trace.read_text())
                self.assertIn('result=failed',(self.host.artifacts/'summary.txt').read_text())
                self.assertFalse(self.host.secrets.exists())

    def test_relaunch_requires_verified_owner_scope_and_never_builds_again(self):
        self.host.secrets.mkdir(mode=0o700); self.host.artifacts.mkdir()
        (self.host.secrets/'capacity-fixture.json').write_text('{}')
        Path(str(self.host.trace)+'.image').touch()
        for flags in ({'ADDP_ONLINE_HOST':'0'},{'ADDP_ONLINE_TEST_RUN_ID':'foreign'},
                      {'ADDP_ONLINE_SECRET_DIR':str(self.host.repository)},
                      {'ADDP_ONLINE_ARTIFACT_DIR':str(self.host.secrets)}):
            result=self.run_gate('--start-runtime',**flags)
            self.assertNotEqual(result.returncode,0)
            self.assertFalse(self.host.trace.exists())
        result=self.run_gate('--start-runtime')
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertIn('docker-run:addp-raster-capacity-runtime',self.host.trace.read_text())
        self.assertNotIn('build-images',self.host.trace.read_text())
        result=self.run_gate('--start-runtime')
        self.assertNotEqual(result.returncode,0)
        self.assertEqual(self.host.trace.read_text().count('docker-run:addp-raster-capacity-runtime'),1)


if __name__=='__main__': unittest.main()
