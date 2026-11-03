import importlib
from pathlib import Path
from unittest.mock import patch

base = importlib.import_module('scripts.test.online-raster-polygonize-fixture_test')
fixture = importlib.import_module('business.scripts.online-raster-statistics-percentiles-fixture')


class StatisticsPercentilesFixtureTest(base.PolygonizeFixtureTest):
    fixture = fixture

    def test_readonly_worker_rejects_target_names_and_unknown_actions(self):
        for action, names in [('verify', ['output.tif']), ('save', [])]:
            with self.subTest(action=action), self.assertRaises(fixture.FixtureError):
                fixture.worker(action, Path('/tmp/unused'), names)

    def test_verify_compares_seed_source_digests(self):
        import tempfile
        import json
        with tempfile.TemporaryDirectory() as directory:
            secret = Path(directory)
            (secret / 'statistics-percentiles-source.json').write_text(json.dumps({'sources': {'source': {'size_bytes': 1, 'sha256': 'old'}}}))
            with patch.object(fixture, 'guard', return_value=secret), patch.object(fixture, 'owned_root', return_value=secret), \
                    patch.object(fixture, 'runtime_worker', return_value={'sources': {'source': {'size_bytes': 1, 'sha256': 'new'}}}):
                with self.assertRaisesRegex(fixture.FixtureError, 'source file or fixture source changed'):
                    fixture.main('verify')


    def test_runtime_ownership_and_product_entry_are_mandatory(self):
        import os, json
        original = {'Config': {'Labels': {'com.addp.online-runtime': fixture.SUITE,
            'com.addp.online-run-id': 'multi-123-1'}, 'Cmd': ['sh', 'container_entrypoint.sh']}}
        expected = {'seeded': True, 'fixture_sources_sha256': fixture.fixture_sources()}
        with patch.dict(os.environ, ADDP_ONLINE_TEST_RUN_ID='multi-123-1'):
            with patch.object(fixture, 'command', side_effect=[json.dumps([original]), json.dumps(expected)]) as command:
                self.assertEqual(fixture.runtime_worker('seed', Path('/tmp/owned')), expected)
                self.assertEqual(command.call_args.kwargs['input'], fixture.worker_source())
            for config in ({**original['Config'], 'Cmd': ['python', 'api_server.py']},
                           {**original['Config'], 'Labels': {'com.addp.online-runtime': 'raster-workflow'}}):
                with patch.object(fixture, 'command', return_value=json.dumps([{'Config': config}])) as command:
                    with self.assertRaises(fixture.FixtureError): fixture.runtime_worker('seed', Path('/tmp/owned'))
                    self.assertEqual(command.call_count, 1)
            for response in ({'seeded':True}, {'fixture_sources_sha256': {}}):
                with patch.object(fixture, 'command', side_effect=[json.dumps([original]), json.dumps(response)]):
                    with self.assertRaises(fixture.FixtureError): fixture.runtime_worker('seed', Path('/tmp/owned'))
