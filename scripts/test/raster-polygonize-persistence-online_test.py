import importlib
from pathlib import Path
import unittest

m = importlib.import_module('scripts.test.raster-polygonize-persistence-online')
shared = importlib.import_module('scripts.test.raster-contour-persistence-online_test')


class PolygonizePersistenceTest(unittest.TestCase):
    def test_three_exact_dags_use_required_band_and_connectivity(self):
        self.assertEqual(m.fixture.TARGETS, {'four.gpkg': 4, 'eight.gpkg': 8, 'reclassified.gpkg': 4})
        for name, connectivity in m.fixture.TARGETS.items():
            tasks = m.definition('source', 'parent', name)['tasks']
            operators = [entry['operator'] for entry in tasks]
            self.assertEqual(operators, ['raster_load'] + (['raster_reclassify'] if name == 'reclassified.gpkg' else []) + ['raster_polygonize', 'save'])
            self.assertEqual(tasks[-2]['params']['connectivity'], connectivity)
            self.assertEqual(tasks[-2]['params']['band'], 1)
            self.assertEqual(tasks[-1]['params']['mode'], 'replace')
        with self.assertRaises(m.SuiteError): m.definition('source', 'parent', '../other.gpkg')

    def test_three_outputs_use_the_same_strict_persistence_chain(self):
        client = shared.Client(source_name='classes.tif')
        env = {'ADDP_ONLINE_TEST_TENANT_ID': '2', 'ADDP_ONLINE_POLYGONIZE_NFS_ENGINE_ID': '3', 'ADDP_ONLINE_TEST_RUN_ID': 'polygonize-123-1'}
        def verify(repository, env, names):
            return {'runtime_workspaces_removed': True, 'targets': {name: {
                'size_bytes': 98304, 'sha256': name, 'layer_name': name[:-5]} for name in names}}
        report = m.run_scenario(Path('.'), env, client, verify)
        self.assertEqual(report['schema_version'], 'addp.raster-polygonize-persistence/v1')
        self.assertEqual([case['name'] for case in report['cases']], list(m.fixture.TARGETS))
        self.assertTrue(all(case['size_evidence']['meta_size_bytes'] == 98304 for case in report['cases']))
        self.assertEqual(len([entry for entry in client.calls if entry[0] == 'POST' and '/scan/' in entry[1]]), 1)
        self.assertTrue(report['source_unchanged'])
