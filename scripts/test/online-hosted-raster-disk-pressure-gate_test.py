import importlib
import subprocess

base = importlib.import_module('scripts.test.online-hosted-raster-contour-gate_test')


class HostedDiskPressureGateTest(base.HostedContourGateTest):
    prefix = 'disk-pressure'

    def setUp(self):
        super().setUp()
        script = self.host.repository/'business/scripts/online-raster-disk-pressure-fixture.py'
        lines = script.read_text().splitlines(keepends=True)
        text = ''
        for line in lines:
            if "(root/'disk-pressure-root').write_text" in line:
                text += line[:len(line)-len(line.lstrip())]+"(root/'disk-pressure-tmp-root').write_text('/tmp/owned-disk/execution')\n"
            text += line
        script.write_text(text)
        docker = self.host.bin/'docker'
        docker.write_text(docker.read_text().replace('raster-disk-pressure-persistence', 'raster-disk-pressure'))
        subprocess.run(['git', 'add', '.'], cwd=self.host.repository, check=True)
        subprocess.run(['git', 'commit', '-qm', 'pressure fixture stub'], cwd=self.host.repository, check=True)

    def run_gate(self, check=False, **overrides):
        overrides.setdefault('ONLINE_SUITE_INPUT', 'raster-disk-pressure')
        return super().run_gate(check, **overrides)

    def test_default_product_entry_and_same_raster_identity_precede_business(self):
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr+result.stdout)
        trace = self.host.trace.read_text()
        for step in ('disk-pressure:start', 'disk-pressure:seed', '--suite raster-workflow',
                     '--wait-protection-owners develop,manager', 'make:test-online ONLINE_SUITE=raster-disk-pressure',
                     'disk-pressure:stop', 'infra-down'):
            self.assertIn(step, trace)
        self.assertFalse(self.host.secrets.exists())
        gate = (self.host.repository/'scripts/test/online-hosted-raster-disk-pressure-gate.sh').read_text()
        self.assertIn('TMPDIR=/raster-pressure-tmp', gate)
        self.assertIn('--memory 2g --memory-swap 2g', gate)
