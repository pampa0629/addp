"""Verify target selection and converter smoke checks without changing Docker state."""
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / 'engines/model3d-workflow/scripts/build-linux-images.sh'


class Model3DLinuxImagesTest(unittest.TestCase):
    def run_build(self, architecture, platform=None, fail_build=False):
        with tempfile.TemporaryDirectory(prefix='addp-model3d-build-') as temporary:
            root = Path(temporary)
            path = root / 'engines/model3d-workflow/scripts/build-linux-images.sh'
            path.parent.mkdir(parents=True)
            shutil.copy2(SCRIPT, path)
            bin_dir = root / 'bin'
            bin_dir.mkdir()
            for name, source in {
                'uname': '#!/bin/bash\nprintf "%s\\n" "$ADDP_TEST_ARCH"\n',
                'docker': '#!/bin/bash\nprintf "%s\\n" "$*" >> "$ADDP_TEST_TRACE"\n[ "$1" != build ] || [ "$ADDP_TEST_BUILD_FAIL" != 1 ]\n',
            }.items():
                command = bin_dir / name
                command.write_text(source)
                command.chmod(0o755)
            env = dict(os.environ, PATH=f'{bin_dir}:{os.environ["PATH"]}',
                       ADDP_TEST_ARCH=architecture, ADDP_TEST_TRACE=str(root / 'trace'),
                       ADDP_TEST_BUILD_FAIL='1' if fail_build else '0')
            for key in ('MODEL3D_DOCKER_PLATFORM', 'MODEL3D_CONVERTER_IMAGE', 'MODEL3D_RUNTIME_IMAGE'):
                env.pop(key, None)
            if platform:
                env['MODEL3D_DOCKER_PLATFORM'] = platform
            result = subprocess.run(['bash', str(path)], env=env, capture_output=True, text=True)
            trace = (root / 'trace').read_text() if (root / 'trace').exists() else ''
            return result, trace

    def test_native_architecture_drives_both_images_and_all_smoke_checks(self):
        for architecture, target in (('x86_64', 'amd64'), ('aarch64', 'arm64'), ('arm64', 'arm64')):
            with self.subTest(architecture=architecture):
                result, trace = self.run_build(architecture)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(trace.count(f'build --platform linux/{target}'), 2)
                self.assertIn(f'addp/model3d-converter:linux-{target}', trace)
                self.assertIn(f'addp/model3d-workflow:linux-{target}', trace)
                self.assertIn('IfcConvert', trace)
                self.assertIn('--entrypoint python', trace)

    def test_converter_wrappers_use_native_platform_unless_explicitly_selected(self):
        with tempfile.TemporaryDirectory(prefix='addp-model3d-wrapper-') as directory:
            root = Path(directory)
            docker = root / 'docker'
            docker.write_text('#!/bin/bash\nprintf "%s\n" "$*" > "$ADDP_TEST_TRACE"\n')
            docker.chmod(0o755)
            env = dict(os.environ, PATH=f'{root}:{os.environ["PATH"]}', ADDP_TEST_TRACE=str(root / 'trace'))
            env.pop('MODEL3D_CONVERTER_PLATFORM', None)
            for name in ('_3dtile', 'assimp', 'IfcConvert'):
                command = ROOT / 'engines/model3d-workflow/scripts/converters' / name
                result = subprocess.run(['bash', str(command), '--help'], env=env, capture_output=True)
                self.assertEqual(result.returncode, 0)
                native_trace = (root / 'trace').read_text()
                self.assertNotIn('--platform', native_trace)
                self.assertIn('--entrypoint /', native_trace)
                self.assertIn(name + ' ', native_trace)
                explicit = dict(env, MODEL3D_CONVERTER_PLATFORM='linux/amd64')
                result = subprocess.run(['bash', str(command), '--help'], env=explicit, capture_output=True)
                self.assertEqual(result.returncode, 0)
                self.assertIn('--platform=linux/amd64', (root / 'trace').read_text())

    def test_invalid_platforms_reject_before_docker(self):
        for target in ('linux/386', 'windows/amd64', 'linux/amd64,linux/arm64'):
            result, trace = self.run_build('x86_64', target)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(trace, '')

    def test_converter_build_failure_prevents_runtime_build_and_smoke_checks(self):
        result, trace = self.run_build('x86_64', fail_build=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(trace.count('build --platform'), 1)
        self.assertNotIn('run --rm', trace)


if __name__ == '__main__':
    unittest.main()
