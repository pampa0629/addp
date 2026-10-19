import importlib
import json
import os
from pathlib import Path
from unittest.mock import patch
import unittest

m = importlib.import_module('business.scripts.online-raster-polygonize-fixture')


class PolygonizeFixtureTest(unittest.TestCase):
    fixture = m
    def test_personal_environment_rejected_before_commands(self):
        with patch.object(self.fixture, 'command', side_effect=AssertionError('must be read only')):
            with self.assertRaises(self.fixture.FixtureError): self.fixture.guard({})

    def test_runtime_ownership_and_product_entry_are_mandatory(self):
        original = {'Config': {'Labels': {'com.addp.online-runtime': self.fixture.SUITE,
            'com.addp.online-run-id': 'polygonize-123-1'}, 'Cmd': ['sh', 'container_entrypoint.sh']}}
        with patch.dict(os.environ, ADDP_ONLINE_TEST_RUN_ID='polygonize-123-1'):
            with patch.object(self.fixture, 'command', side_effect=[json.dumps([original]), '{"seeded": true}']) as command:
                self.assertEqual(self.fixture.runtime_worker('seed', Path('/tmp/owned')), {'seeded': True})
                self.assertIn('--worker', command.call_args.args[0])
            for config in ({**original['Config'], 'Cmd': ['python', 'api_server.py']},
                           {**original['Config'], 'Labels': {'com.addp.online-runtime': 'raster-workflow'}}):
                with patch.object(self.fixture, 'command', return_value=json.dumps([{'Config': config}])) as command:
                    with self.assertRaises(self.fixture.FixtureError): self.fixture.runtime_worker('seed', Path('/tmp/owned'))
                    self.assertEqual(command.call_count, 1)
