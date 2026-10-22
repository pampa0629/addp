import importlib
import subprocess
from unittest.mock import patch
base=importlib.import_module('scripts.test.online-raster-polygonize-fixture_test')


class FlowDirectionD8FixtureTest(base.PolygonizeFixtureTest):
    fixture=importlib.import_module('business.scripts.online-raster-flow-direction-d8-fixture')

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
