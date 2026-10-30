import importlib
import os
from pathlib import Path
from unittest.mock import patch

base = importlib.import_module('scripts.test.online-raster-polygonize-fixture_test')
fixture = importlib.import_module('business.scripts.online-raster-zonal-percentile-fixture')


class ZonalPercentileFixtureTest(base.PolygonizeFixtureTest):
    fixture = fixture

    def test_worker_rejects_unknown_actions_and_missing_frozen_anchor(self):
        for action, names in [('verify', ['output.tif']), ('save', [])]:
            with self.subTest(action=action), self.assertRaises(fixture.FixtureError):
                fixture.worker(action, Path('/tmp/unused'), names)

    def test_verify_compares_seed_source_digests(self):
        import tempfile
        import json
        with tempfile.TemporaryDirectory() as directory:
            secret = Path(directory)
            (secret / 'zonal-percentile-source.json').write_text(json.dumps({'sources': {'source': {'size_bytes': 1, 'sha256': 'old'}}}))
            with patch.dict(os.environ, {'ADDP_ONLINE_ZONAL_PERCENTILE_ORACLE_SHA256': 'a'*64}), patch.object(fixture, 'guard', return_value=secret), patch.object(fixture, 'owned_root', return_value=secret), \
                    patch.object(fixture, 'runtime_worker', return_value={'sources': {'source': {'size_bytes': 1, 'sha256': 'new'}}}):
                with self.assertRaisesRegex(fixture.FixtureError, 'source file changed'):
                    fixture.main('verify')
