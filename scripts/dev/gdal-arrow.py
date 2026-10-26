#!/usr/bin/env python3
"""Native GDAL/Arrow identity and same-process admission (no service lifecycle)."""
from __future__ import annotations

import argparse
import ctypes
import hashlib
import json
import os
import platform
import re
import shlex
import subprocess
import sys
import tempfile
from pathlib import Path

# Arrow 22's Cython sources require the 3.1 compiler line. Build inputs, not a
# runtime dependency; newer native releases must pass this same source build.
BUILD_REQUIREMENTS = ("cython==3.1.8", "cmake==4.4.0", "numpy==2.2.6",
                      "setuptools>=67", "setuptools_scm[toml]>=8", "wheel")
ARROW_LIBRARY = re.compile(r"^lib(?:arrow(?:_dataset|_acero|_compute|_flight(?:_sql)?|_substrait)?|parquet)(?:[.\d]|\.so)")


def output(*args):
    return subprocess.check_output(args, text=True).strip()


def sha256(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def linked_libraries(path):
    if sys.platform == 'darwin':
        lines = output('otool', '-L', str(path)).splitlines()[1:]
        return [Path(line.strip().split(' (', 1)[0]) for line in lines]
    lines = output('ldd', str(path)).splitlines()
    return [Path(match.group(1)) for line in lines
            if (match := re.search(r'(?:=>\s*)?(/\S+)', line))]


def native_manifest():
    flags = shlex.split(output('gdal-config', '--libs'))
    directories = [Path(flag[2:]) for flag in flags if flag.startswith('-L')]
    libraries = [directory / name for directory in directories
                 for name in ('libgdal.dylib', 'libgdal.so') if (directory / name).is_file()]
    if len(libraries) != 1:
        raise RuntimeError(f'不能唯一定位 gdal-config 声明的原生 GDAL: {libraries}')
    gdal = libraries[0].resolve()
    linked_arrow = [path for path in linked_libraries(gdal) if ARROW_LIBRARY.match(path.name)]
    manifest = {'gdal_version': output('gdal-config', '--version'),
                'gdal': [str(gdal), sha256(gdal)], 'arrow': None,
                'python': [sys.version, sys.implementation.cache_tag, platform.machine()],
                'preparer': sha256(__file__), 'build_requirements': BUILD_REQUIREMENTS}
    if linked_arrow:
        # Resolve the development metadata beside GDAL's actual linked Arrow,
        # never from a Conda-first inherited PKG_CONFIG_PATH.
        arrow_pc = linked_arrow[0].resolve().parent / 'pkgconfig/arrow.pc'
        prefix = Path(output('pkg-config', '--variable=prefix', str(arrow_pc))).resolve()
        libdir = Path(output('pkg-config', '--variable=libdir', str(arrow_pc))).resolve()
        if any(not path.is_file() or path.resolve().parent != libdir for path in linked_arrow):
            raise RuntimeError('GDAL 链接的 Arrow 与 pkg-config Arrow 来源不一致')
        libraries = sorted({path.resolve() for path in libdir.iterdir()
                            if ARROW_LIBRARY.match(path.name) and path.is_file()
                            and ('.so' in path.name or path.suffix == '.dylib')})
        manifest['arrow'] = {'version': output('pkg-config', '--modversion', str(arrow_pc)),
                             'prefix': str(prefix), 'libdir': str(libdir),
                             'pkg_config': [str(arrow_pc), sha256(arrow_pc)],
                             'libraries': [[str(path), sha256(path)] for path in libraries]}
    return manifest


def fingerprint(manifest):
    return hashlib.sha256(json.dumps(manifest, sort_keys=True).encode()).hexdigest()


def build_wheel(manifest, cache):
    # A private compiler venv avoids both mutable runtime build dependencies and
    # invoking a pip-installed CMake script with another interpreter's isolated
    # import path. Nothing is installed in Homebrew or the running runtime.
    with tempfile.TemporaryDirectory(prefix='addp-pyarrow-build-') as directory:
        subprocess.run([sys.executable, '-m', 'venv', directory], check=True)
        python = str(Path(directory) / 'bin/python')
        subprocess.run([python, '-m', 'pip', 'install', *manifest['build_requirements']], check=True)
        environment = dict(os.environ, ARROW_HOME=manifest['arrow']['prefix'],
                           CMAKE_PREFIX_PATH=manifest['arrow']['prefix'],
                           PYARROW_BUNDLE_ARROW_CPP='0', PYARROW_PARALLEL='4',
                           PATH=str(Path(directory) / 'bin') + os.pathsep + os.environ['PATH'])
        subprocess.run([python, '-m', 'pip', 'wheel', '--no-deps', '--no-cache-dir',
                        '--no-binary=pyarrow', '--no-build-isolation', '--wheel-dir', str(cache),
                        'pyarrow==' + manifest['arrow']['version']], check=True, env=environment)


def loaded_libraries():
    if sys.platform == 'darwin':
        dyld = ctypes.CDLL(None)
        dyld._dyld_image_count.restype = ctypes.c_uint32
        dyld._dyld_get_image_name.argtypes = [ctypes.c_uint32]
        dyld._dyld_get_image_name.restype = ctypes.c_char_p
        paths = [Path(dyld._dyld_get_image_name(i).decode())
                 for i in range(dyld._dyld_image_count())]
    else:
        paths = [Path(line.split()[-1]) for line in Path('/proc/self/maps').read_text().splitlines()
                 if '/' in line]
    return sorted({path.resolve() for path in paths})


def admit(manifest, order):
    # Both orders run in fresh interpreters. No import-order workaround and no
    # memory-buffer/file-descriptor bypass of PyArrow's native filesystem.
    if order == 'gdal-first':
        from osgeo import gdal, gdal_array, ogr, osr
    import pyarrow as pa
    import pyarrow.dataset  # Exercise dataset and Parquet linked libraries too.
    import pyarrow.fs as fs
    import pyarrow.parquet as pq
    if order == 'arrow-first':
        fs.LocalFileSystem()
        from osgeo import gdal, gdal_array, ogr, osr
    assert gdal.VersionInfo('RELEASE_NAME') == manifest['gdal_version']
    table = pa.table({'number': pa.array([2**53 + 1, None, 2**63 - 1], type=pa.int64()),
                      'flag': pa.array([True, None, False], type=pa.bool_())})
    with tempfile.TemporaryDirectory(prefix='addp-arrow-admission-') as directory:
        path = Path(directory) / '原生写入.parquet'
        fs.LocalFileSystem()
        pq.write_table(table, path)
        assert path.stat().st_size > 0
        assert pq.read_table(path).equals(table), 'Parquet 精确整数/布尔值重读失败'
    images = loaded_libraries()
    assert Path(manifest['gdal'][0]) in images, 'GDAL Python 绑定未加载声明的原生 GDAL'
    loaded = [path for path in images if ARROW_LIBRARY.match(path.name)]
    core = [path for path in loaded if re.match(r'^libarrow(?:\.\d|\.so)', path.name)]
    assert len(core) == 1, f'进程必须仅加载一套 Arrow C++: {core}'
    if manifest['arrow']:
        assert pa.cpp_version == manifest['arrow']['version']
        expected = {Path(path) for path, _ in manifest['arrow']['libraries']}
        assert set(loaded) <= expected, f'加载了其他来源 Arrow: {set(loaded) - expected}'
    print(json.dumps({'result': 'passed', 'order': order, 'gdal': manifest['gdal_version'],
                      'pyarrow': pa.__version__, 'libraries': list(map(str, loaded))}, ensure_ascii=False))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('command', choices=('manifest', 'fingerprint', 'build', 'admit'))
    parser.add_argument('--manifest', type=Path)
    parser.add_argument('--cache', type=Path)
    parser.add_argument('--order', choices=('gdal-first', 'arrow-first'), default='gdal-first')
    args = parser.parse_args()
    manifest = json.loads(args.manifest.read_text()) if args.manifest else native_manifest()
    if args.command == 'manifest':
        print(json.dumps(manifest, ensure_ascii=False, sort_keys=True))
    elif args.command == 'fingerprint':
        print(fingerprint(manifest))
    elif args.command == 'build':
        if args.cache is None or manifest['arrow'] is None:
            parser.error('source build requires Arrow and --cache')
        build_wheel(manifest, args.cache)
    else:
        admit(manifest, args.order)


if __name__ == '__main__':
    main()
