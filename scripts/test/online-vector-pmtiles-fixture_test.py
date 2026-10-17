import copy
import importlib
import unittest
from unittest.mock import patch

fixture = importlib.import_module('business.scripts.online-vector-pmtiles-fixture')


class PMTilesFixtureTest(unittest.TestCase):
    def test_fixture_counts_fields_and_disjoint_geometries(self):
        self.assertEqual(len(fixture.features()['features']), 2)
        polygons = fixture.features(True)['features']
        self.assertEqual(len(polygons), 50)
        self.assertEqual({value['properties']['rank'] for value in polygons}, set(range(50)))
        self.assertTrue(all(value['geometry']['type'] == 'Polygon' for value in polygons))

    def test_cleanup_refuses_foreign_owned_containers_and_continues_others(self):
        with patch.object(fixture.shared, 'owned', side_effect=fixture.shared.FixtureError('foreign')), patch.object(fixture.shared, 'command') as command:
            with self.assertRaises(fixture.shared.FixtureError): fixture.stop()
            command.assert_not_called()

    def test_cleanup_does_not_report_success_with_residuals(self):
        with patch.object(fixture.shared, 'owned', return_value=True), patch.object(fixture.shared, 'command') as command:
            with self.assertRaises(fixture.shared.FixtureError): fixture.stop()
            self.assertEqual(command.call_count, 3)

    def test_coordinates_reject_geometry_with_correct_count_but_wrong_position(self):
        import math
        records = copy.deepcopy(fixture.features()['features'])
        for record in records:
            x,y = record['geometry']['coordinates']
            record['geometry']['coordinates'] = [x*math.pi*6378137/180, math.log(math.tan(math.pi/4+y*math.pi/360))*6378137]
            record['properties'] = {}
        fixture.validate_records(records, False)
        records[0]['geometry']['coordinates'][1] += 1000
        with self.assertRaises(fixture.shared.FixtureError): fixture.validate_records(records, False)


if __name__ == '__main__': unittest.main()
