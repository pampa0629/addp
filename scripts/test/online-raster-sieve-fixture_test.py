import importlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
from unittest.mock import patch
base=importlib.import_module('scripts.test.online-raster-polygonize-fixture_test')


class SieveFixtureTest(base.PolygonizeFixtureTest):
    fixture=importlib.import_module('business.scripts.online-raster-sieve-fixture')

    def test_worker_failure_preserves_actual_stderr_without_input_or_stdout(self):
        failure = subprocess.CalledProcessError(1, ['docker', 'exec'],
            output='private fixture state', stderr='Raster DEM filling fixture failed: FixtureError: complete pixel values differ\n')
        with patch.object(self.fixture.subprocess, 'run', side_effect=failure):
            with self.assertRaises(self.fixture.FixtureError) as caught:
                self.fixture.command(['docker', 'exec'], input='private stdin')
        self.assertIn('complete pixel values differ', str(caught.exception))
        self.assertIn('exit 1', str(caught.exception))
        self.assertNotIn('private', str(caught.exception))

    def test_command_failure_diagnostic_is_bounded_and_handles_missing_stderr(self):
        for stderr, expected in ((None, 'no stderr diagnostic'), ('x'*5000+'root cause', 'root cause')):
            with self.subTest(stderr_length=len(stderr or '')):
                failure = subprocess.CalledProcessError(2, ['docker'], stderr=stderr)
                with patch.object(self.fixture.subprocess, 'run', side_effect=failure):
                    with self.assertRaises(self.fixture.FixtureError) as caught:
                        self.fixture.command(['docker'])
                self.assertIn(expected, str(caught.exception))
                self.assertLessEqual(len(str(caught.exception)), 4050)

    def test_private_anchor_rejects_omission_tampering_and_cross_run_before_runtime(self):
        m=self.fixture
        with tempfile.TemporaryDirectory()as directory:
            secret=Path(directory);anchor=secret/m.CONTROL_NAME
            value={'scope':{'fixture_sha256':m.digest(Path(m.__file__))['sha256'],'run_id':'sieve-123-1'},'arrays':{}}
            with patch.dict(os.environ,ADDP_ONLINE_TEST_RUN_ID='sieve-123-1'),patch.object(m,'guard',return_value=secret),patch.object(m,'owned_root',return_value=secret),patch.object(m,'runtime_worker',side_effect=AssertionError('must reject before Runtime')):
                with patch.dict(os.environ,clear=True):
                    with self.assertRaises(m.FixtureError):m.main('verify')
                for fault in ('mode','hash','fixture','run','symlink'):
                    anchor.unlink(missing_ok=True);current=json.loads(json.dumps(value));anchor.write_bytes(m.encoded(current));anchor.chmod(0o600)
                    expected=m.digest(anchor)['sha256']
                    if fault=='mode':anchor.chmod(0o644)
                    elif fault in ('fixture','run'):
                        current['scope']['fixture_sha256'if fault=='fixture'else'run_id']='b'*64 if fault=='fixture'else'sieve-999-1';anchor.write_bytes(m.encoded(current))
                    elif fault=='hash':anchor.write_bytes(m.encoded({**current,'extra':True}))
                    else:anchor.unlink();anchor.symlink_to(secret/'absent')
                    with self.subTest(fault=fault),patch.dict(os.environ,ADDP_ONLINE_SIEVE_ORACLE_ANCHOR_SHA256=expected),self.assertRaises(m.FixtureError):m.main('verify')

    def test_partial_host_preparation_cleanup_is_mandatory_and_bound(self):
        m=self.fixture
        with tempfile.TemporaryDirectory()as directory:
            secret=Path(directory);(secret/'sieve-source.json').write_text(json.dumps({'sources':{}}));calls=[]
            def worker(action,root,args):
                calls.append(action)
                if action=='prepare-oracle':raise m.FixtureError('injected preparation failure')
                self.assertIsNone(json.loads(args[0])['arrays']);return {'oracle_cache_removed':True}
            with patch.dict(os.environ,ADDP_ONLINE_TEST_RUN_ID='sieve-123-1'),patch.object(m,'guard',return_value=secret),patch.object(m,'owned_root',return_value=secret),patch.object(m,'runtime_worker',side_effect=worker):
                with self.assertRaisesRegex(m.FixtureError,'injected preparation failure'):m.main('prepare-oracle')
                self.assertEqual((secret/m.CONTROL_NAME).stat().st_mode&0o777,0o600)
                self.assertTrue(m.main('cleanup-oracle')['oracle_cache_removed']);self.assertFalse((secret/m.CONTROL_NAME).exists())
                self.assertEqual(calls,['prepare-oracle','cleanup-oracle'])
                self.assertTrue(m.main('cleanup-oracle')['not_started'])
