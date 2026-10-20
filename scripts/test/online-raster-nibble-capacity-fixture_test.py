import importlib
import json
import os
from pathlib import Path
import unittest
from unittest.mock import patch

m = importlib.import_module('business.scripts.online-raster-nibble-capacity-fixture')


class NibbleCapacityFixtureTest(unittest.TestCase):
    def test_personal_environment_rejected_before_commands(self):
        with patch.object(m, 'command', side_effect=AssertionError('must not run commands')):
            with self.assertRaises(m.FixtureError): m.NFSExport(m.PREFIX, m.command).guard({})

    def test_runtime_owner_and_default_entry_required_before_worker(self):
        original = {'Config': {'Labels': {'com.addp.online-runtime': m.SUITE,
                    'com.addp.online-run-id': 'nibble-capacity-123-1'}, 'Cmd': ['sh', 'container_entrypoint.sh']}}
        with patch.dict(os.environ, ADDP_ONLINE_TEST_RUN_ID='nibble-capacity-123-1'):
            with patch.object(m, 'command', side_effect=[json.dumps([original]), '{"seeded":true}']) as call:
                self.assertEqual(m.run_runtime_worker(call, m.RUNTIME, m.SUITE, Path(m.__file__), 'seed', Path('/tmp/owned'), ()), {'seeded': True})
                self.assertIn('--worker', call.call_args.args[0])
            for info in ({'Config': {**original['Config'], 'Cmd': ['python', 'api_server.py']}},
                         {'Config': {**original['Config'], 'Labels': {'com.addp.online-runtime': 'foreign'}}}):
                with patch.object(m, 'command', return_value=json.dumps([info])) as call:
                    with self.assertRaises(m.FixtureError): m.run_runtime_worker(call, m.RUNTIME, m.SUITE, Path(m.__file__), 'seed', Path('/tmp/owned'), ())
                    self.assertEqual(call.call_count, 1)
