import importlib

base = importlib.import_module('scripts.test.online-hosted-raster-contour-gate_test')


class HostedBandRecommendationGateTest(base.HostedContourGateTest):
    prefix = 'band-recommendation'

    def setUp(self):
        super().setUp()
        docker = self.host.bin / 'docker'
        docker.write_text(docker.read_text().replace('raster-band-recommendation-persistence', 'raster-band-recommendation-workflow'))

    def run_gate(self, check=False, **overrides):
        overrides.setdefault('ONLINE_SUITE_INPUT', 'raster-band-recommendation-workflow')
        return super().run_gate(check, **overrides)

    def test_default_product_entry_and_same_raster_identity_precede_business(self):
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        trace = self.host.trace.read_text()
        for step in ('band-recommendation:start', 'band-recommendation:seed', '--wait-protection-owners develop,manager',
                     'make:test-online ONLINE_SUITE=raster-band-recommendation-workflow', 'band-recommendation:stop', 'infra-down'):
            self.assertIn(step, trace)
        self.assertNotIn('make:test-online ONLINE_SUITE=raster-band-recommendation-persistence', trace)
        self.assertFalse(self.host.secrets.exists())
