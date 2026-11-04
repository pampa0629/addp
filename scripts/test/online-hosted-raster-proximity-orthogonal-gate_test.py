import importlib
base=importlib.import_module('scripts.test.online-hosted-raster-contour-gate_test')

class HostedProximityOrthogonalGateTest(base.HostedContourGateTest):
    prefix='proximity-orthogonal'

    def setUp(self):
        super().setUp()
        self.host._write_repository_script('scripts/utils/online_raster_lineage.py', """
            import os,sys
            assert sys.argv[1:]==['prepare']
            assert os.environ['POSTGRES_DB']=='addp_online'
            with open(os.environ['ADDP_TEST_GATE_TRACE'],'a') as f:f.write('lineage-owner:prepare\\n')
            if os.environ.get('ADDP_TEST_OBSERVER_FAIL')=='1':sys.exit(1)
        """)
        import subprocess
        subprocess.run(['git','add','.'],cwd=self.host.repository,check=True)
        subprocess.run(['git','commit','-qm','owned lineage observer fixture'],cwd=self.host.repository,check=True)

    def test_observer_prepare_failure_cleans_before_business(self):
        result=self.run_gate(ADDP_TEST_OBSERVER_FAIL='1')
        self.assertNotEqual(result.returncode,0)
        trace=self.host.trace.read_text();self.assertIn('lineage-owner:prepare',trace)
        self.assertNotIn('make:test-online',trace);self.assertFalse(self.host.secrets.exists())

    def run_gate(self, check=False, **overrides):
        import subprocess
        try:
            return super().run_gate(check, **overrides)
        except subprocess.TimeoutExpired:
            trace=self.host.trace.read_text() if self.host.trace.exists() else 'no lifecycle trace'
            self.fail('Hosted simulation exceeded existing 60s limit; last lifecycle trace: '+trace)

    def test_timeout_retains_existing_limit_and_exposes_lifecycle_stage(self):
        import subprocess
        from unittest.mock import patch
        self.host.trace.write_text('proximity-orthogonal:start\nmake:test-online\n')
        with patch.object(base.HostedContourGateTest,'run_gate',side_effect=subprocess.TimeoutExpired(['bash'],60)):
            with self.assertRaisesRegex(AssertionError,'existing 60s limit; last lifecycle trace: proximity-orthogonal:start'):
                self.run_gate()
