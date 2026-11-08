import importlib
base=importlib.import_module('scripts.test.online-hosted-raster-line-of-sight-gate_test')

class HostedSightOrthogonalGateTest(base.HostedLineOfSightGateTest):
    prefix='sight-orthogonal'

    def test_exact_preparation_suite_and_identity_failure_cleanup(self):
        result=self.run_gate()
        self.assertEqual(result.returncode,0,result.stderr+result.stdout)
        trace=self.host.trace.read_text()
        self.assertIn('--suite raster-sight-orthogonal-persistence --wait-protection-owners develop,manager',trace)
        self.assertNotIn('--suite raster-workflow ',trace)
        self.assertFalse(self.host.secrets.exists())

    def test_identity_preparation_failure_stops_before_business_and_cleans(self):
        result=self.run_gate(ADDP_TEST_IDENTITY_FAIL='1')
        self.assertNotEqual(result.returncode,0)
        trace=self.host.trace.read_text()
        self.assertNotIn('make:test-online',trace)
        self.assertIn('sight-orthogonal:stop',trace)
        self.assertFalse(self.host.secrets.exists())
