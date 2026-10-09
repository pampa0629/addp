"""Verify target selection and converter smoke checks without changing Docker state."""
import os
import hashlib
import json
import re
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / 'engines/model3d-workflow/scripts/build-linux-images.sh'


class Model3DLinuxImagesTest(unittest.TestCase):
    def test_tinygltf_overlay_pins_verified_source_and_installs_header(self):
        converter = ROOT / 'engines/model3d-workflow/docker/converter'
        dockerfile = (converter / 'Dockerfile').read_text()
        self.assertIn('COPY engines/model3d-workflow/docker/converter/vcpkg-overlays/ /opt/addp/vcpkg-overlays/', dockerfile)
        self.assertIn('ENV VCPKG_OVERLAY_PORTS=/opt/addp/vcpkg-overlays', dockerfile)
        self.assertLess(dockerfile.index('ENV VCPKG_OVERLAY_PORTS='), dockerfile.index('cargo build'))
        port = converter / 'vcpkg-overlays/tinygltf'
        manifest = json.loads((port / 'vcpkg.json').read_text())
        self.assertEqual(manifest['name'], 'tinygltf')
        self.assertEqual(manifest['version'], '2.9.7')
        self.assertEqual(set(manifest['dependencies']), {'nlohmann-json', 'stb'})
        source = (port / 'portfile.cmake').read_text()
        commit = re.search(r'REF ([0-9a-f]{40})', source).group(1)
        sha512 = re.search(r'SHA512 ([0-9a-f]{128})', source).group(1)
        self.assertEqual(commit, '488a70a3df62a4df1a736e9e56fb8836580c4888')
        archive = os.environ.get('MODEL3D_TINYGLTF_SOURCE_ARCHIVE')
        if archive:
            self.assertEqual(hashlib.sha512(Path(archive).read_bytes()).hexdigest(), sha512)
        with tempfile.TemporaryDirectory(prefix='addp-tinygltf-port-') as directory:
            root = Path(directory)
            original = root / 'source'
            original.mkdir()
            (original / 'tiny_gltf.h').write_text('#include "json.hpp"\n')
            (original / 'LICENSE').write_text('tinygltf license fixture')
            harness = root / 'install.cmake'
            harness.write_text('''
function(vcpkg_from_github)
  cmake_parse_arguments(P "" "OUT_SOURCE_PATH;REPO;REF;SHA512" "" ${ARGN})
  if(NOT P_REPO STREQUAL "syoyo/tinygltf" OR NOT P_REF STREQUAL "''' + commit + '''" OR NOT P_SHA512 STREQUAL "''' + sha512 + '''")
    message(FATAL_ERROR "unverified source coordinates")
  endif()
  set(${P_OUT_SOURCE_PATH} "''' + original.as_posix() + '''" PARENT_SCOPE)
endfunction()
function(vcpkg_replace_string path old new)
  file(READ "${path}" content)
  string(REPLACE "${old}" "${new}" content "${content}")
  file(WRITE "${path}" "${content}")
endfunction()
function(vcpkg_install_copyright)
  cmake_parse_arguments(P "" "" "FILE_LIST" ${ARGN})
  file(INSTALL ${P_FILE_LIST} DESTINATION "${CURRENT_PACKAGES_DIR}/share/tinygltf" RENAME copyright)
endfunction()
set(CURRENT_PACKAGES_DIR "''' + (root / 'package').as_posix() + '''")
include("''' + (port / 'portfile.cmake').as_posix() + '''")
''')
            result = subprocess.run(['cmake', '-P', str(harness)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual((root / 'package/include/tiny_gltf.h').read_text(), '#include <nlohmann/json.hpp>\n')
            self.assertEqual((root / 'package/share/tinygltf/copyright').read_text(), 'tinygltf license fixture')

    def test_product_context_and_cache_include_version_and_native_build_inputs(self):
        converter = ROOT / 'engines/model3d-workflow/docker/converter'
        self.assertIn('!engines/model3d-workflow/native-assets.json', (converter / 'Dockerfile.dockerignore').read_text())
        self.assertIn('!engines/model3d-workflow/docker/converter/**', (converter / 'Dockerfile.dockerignore').read_text())
        self.assertIn('!common-python/addp_common/**', (converter.parent / 'runtime/Dockerfile.dockerignore').read_text())
        builder = (ROOT / 'scripts/build/build-images.sh').read_text()
        block = builder[builder.index('        model3d-workflow-engine)'):builder.index('        pointcloud-workflow-engine|document-workflow-engine)')]
        for extension in ('*.json', '*.lock', '*.cpp', '*.cmake', '*.dockerignore'):
            self.assertIn(extension, block)

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
                self.assertIn('/fixture/model.osgb:ro', trace)


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
