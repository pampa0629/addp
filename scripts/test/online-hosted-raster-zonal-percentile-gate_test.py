import importlib
base=importlib.import_module('scripts.test.online-hosted-raster-contour-gate_test')

class HostedZonalPercentileGateTest(base.HostedContourGateTest):
    prefix='zonal-percentile'

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
