import importlib
import os
from pathlib import Path
from unittest.mock import patch

base = importlib.import_module('scripts.test.online-raster-polygonize-fixture_test')
fixture = importlib.import_module('business.scripts.online-vector-zonal-percentiles-fixture')


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
            (secret / 'vector-zonal-percentiles-source.json').write_text(json.dumps({'sources': {'source': {'size_bytes': 1, 'sha256': 'old'}}}))
            with patch.dict(os.environ, {'ADDP_ONLINE_VECTOR_ZONAL_PERCENTILES_ORACLE_SHA256': 'a'*64}), patch.object(fixture, 'guard', return_value=secret), patch.object(fixture, 'owned_root', return_value=secret), \
                    patch.object(fixture, 'runtime_worker', return_value={'sources': {'source': {'size_bytes': 1, 'sha256': 'new'}}}):
                with self.assertRaisesRegex(fixture.FixtureError, 'source file changed'):
                    fixture.main('verify')

    def test_main_archives_frozen_oracle_sources_and_sixteen_gpkg_original_bytes(self):
        import tempfile,json,hashlib,sys
        with tempfile.TemporaryDirectory()as directory:
            root=Path(directory)/'source';root.mkdir();(root/'outputs').mkdir()
            secret=Path(directory)/'secret';secret.mkdir();artifacts=Path(directory)/'artifacts';artifacts.mkdir()
            for name in fixture.SOURCES:(root/name).write_bytes(('source:'+name).encode())
            (root/fixture.ORACLE).write_bytes(b'{"frozen":"independent"}\n')
            sources={name:fixture.digest(root/name)for name in fixture.SOURCES}
            (secret/'vector-zonal-percentiles-source.json').write_text(json.dumps({'sources':sources}))
            def worker(action,actual_root,names):
                self.assertEqual(actual_root,root)
                if action=='verify':self.assertEqual(names[0],hashlib.sha256((root/fixture.ORACLE).read_bytes()).hexdigest())
                return {'sources':sources}
            env={'ADDP_ONLINE_ARTIFACT_DIR':str(artifacts),'ADDP_ONLINE_VECTOR_ZONAL_PERCENTILES_ORACLE_SHA256':hashlib.sha256((root/fixture.ORACLE).read_bytes()).hexdigest()}
            with patch.dict(os.environ,env),patch.object(fixture,'guard',return_value=secret),patch.object(fixture,'owned_root',return_value=root),patch.object(fixture,'runtime_worker',side_effect=worker):
                with patch.object(sys,'argv',['fixture','oracle']):initial=fixture.main('oracle')
                self.assertEqual(initial['archived_gpkgs'],{})
                self.assertEqual(set(initial['archived_oracle']),{fixture.ORACLE})
                for name in fixture.CASES:(root/'outputs'/name).write_bytes(('raw,csv\n'+name+'\n').encode())
                with patch.object(sys,'argv',['fixture','verify',*fixture.CASES]):report=fixture.main('verify')
                self.assertEqual(set(report['archived_gpkgs']),set(fixture.CASES))
                for name,fact in report['archived_gpkgs'].items():
                    self.assertEqual(fact['path'],'vector-zonal-percentiles-gpkgs/'+name)
                    self.assertEqual((artifacts/fact['path']).read_bytes(),(root/'outputs'/name).read_bytes())
                    self.assertEqual({key:fact[key]for key in('size_bytes','sha256')},fixture.digest(root/'outputs'/name))
                victim=artifacts/report['archived_gpkgs']['batch.gpkg']['path'];victim.chmod(0o644);victim.write_bytes(b'changed')
                with patch.object(sys,'argv',['fixture','verify',*fixture.CASES]),self.assertRaises(fixture.FixtureError):fixture.main('verify')
