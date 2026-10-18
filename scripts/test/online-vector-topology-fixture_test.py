import copy
import importlib
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

m = importlib.import_module('business.scripts.online-vector-topology-fixture')


def records(name):
    if name == 'faces.geojson':
        coordinates = [[[[x,y],[x+2,y],[x+2,y+2],[x,y+2],[x,y]]] for x in (120,122) for y in (30,32)]
        return [{'geometry': {'type': 'Polygon', 'coordinates': c}, 'properties': {}} for c in coordinates]
    return [{'geometry': {'type': 'LineString', 'coordinates': c}, 'properties': {'issue_type': 'dangle'}} for c in (
        [[119,32],[120,32]], [[124,32],[125,32]], [[122,29],[122,30]], [[122,34],[122,35]])]


class TopologyFixtureTest(unittest.TestCase):
    def test_oracle_accepts_ring_rotation_direction_and_line_direction(self):
        faces = records('faces.geojson')
        for feature in faces:
            ring = feature['geometry']['coordinates'][0][:-1]
            ring = list(reversed(ring[2:] + ring[:2]))
            feature['geometry']['coordinates'] = [ring + [ring[0]]]
        m.validate_features('faces.geojson', faces)
        diagnostics = records('diagnostics.geojson')
        for feature in diagnostics: feature['geometry']['coordinates'].reverse()
        m.validate_features('diagnostics.geojson', diagnostics)

    def test_oracle_rejects_missing_duplicate_changed_geometry_and_business_fields(self):
        for name in m.TARGETS:
            original = records(name)
            for mutate in (lambda v: v.pop(), lambda v: v.__setitem__(0, copy.deepcopy(v[1])),
                           lambda v: v[0]['properties'].update(source_name='unexpected'),
                           lambda v: v[0]['geometry'].update(type='MultiLineString')):
                value = copy.deepcopy(original); mutate(value)
                with self.subTest(name=name), self.assertRaises(m.FixtureError): m.validate_features(name, value)
        value = records('diagnostics.geojson'); value[0]['geometry']['coordinates'][0][0] = 118
        with self.assertRaises(m.FixtureError): m.validate_features('diagnostics.geojson', value)

    def test_admission_rejects_personal_environment_before_system_commands(self):
        with patch.object(m, 'command') as command:
            with self.assertRaises(m.FixtureError): m.NFSExport('topology', command).guard({})
            command.assert_not_called()

    def test_topology_scope_cannot_clean_a_contour_or_foreign_root(self):
        with tempfile.TemporaryDirectory() as directory:
            secret = Path(directory); (secret/'topology-root').write_text('/tmp/addp-contour-abcdefgh')
            with self.assertRaises(m.FixtureError): m.NFSExport('topology', m.command).owned_root(secret, 'topology-1-1')
            (secret/'topology-root').unlink(); (secret/'topology-root').symlink_to(secret/'missing')
            with self.assertRaises(m.FixtureError): m.NFSExport('topology', m.command).owned_root(secret, 'topology-1-1')

    def test_runtime_worker_refuses_a_foreign_run_before_execution(self):
        info = {'Config': {'Labels': {'com.addp.online-runtime': m.SUITE, 'com.addp.online-run-id': 'foreign'}, 'Cmd': ['sh','container_entrypoint.sh']}}
        with patch.dict(os.environ, ADDP_ONLINE_TEST_RUN_ID='topology-1-1'), patch.object(m, 'command', return_value=json.dumps([info])) as command:
            with self.assertRaises(m.FixtureError): m.runtime_worker('verify', Path('/tmp/owned'))
            self.assertEqual(command.call_count, 1)
