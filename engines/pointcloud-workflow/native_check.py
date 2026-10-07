"""Verify PointCloud's private native runtime; no global PDAL lookup."""
from __future__ import annotations

import json
import re
import subprocess
import sys
import tempfile
from pathlib import Path

REQUIRED_DRIVERS = {'readers.las', 'readers.e57', 'readers.pcd', 'readers.text', 'writers.copc'}


def check_native(prefix: Path) -> None:
    executable = str(prefix / 'bin/pdal')

    def run(*arguments: str) -> str:
        result = subprocess.run([executable, *arguments], capture_output=True, text=True, timeout=60)
        if result.returncode:
            raise RuntimeError(f'PDAL {arguments[0]} failed: {result.stderr or result.stdout}')
        return result.stdout

    specifications = dict(line.split('=', 1) for line in Path(__file__).with_name('native-packages.txt').read_text().splitlines() if '=' in line)
    version = specifications['libpdal-core']
    if sys.version_info[:2] != tuple(map(int, specifications['python'].split('.'))):
        raise RuntimeError('PointCloud Python version mismatch')
    if not re.search(r'\bpdal\s+' + re.escape(version) + r'\b', run('--version'), re.I):
        raise RuntimeError(f'PDAL must be {version}')
    drivers = set(re.findall(r'\b(?:readers|writers)\.[\w]+', run('--drivers')))
    if REQUIRED_DRIVERS - drivers:
        raise RuntimeError(f'Missing PDAL drivers: {sorted(REQUIRED_DRIVERS - drivers)}')
    if not (prefix / 'share/proj/proj.db').is_file() or not (prefix / 'share/gdal').is_dir():
        raise RuntimeError('Missing private GDAL/PROJ resources')
    with tempfile.TemporaryDirectory(prefix='addp-pdal-check-') as directory:
        source, target = Path(directory, 'source.xyz'), Path(directory, 'target.copc.laz')
        source.write_text('1 2 3\n4 5 6\n7 8 9\n')
        run('translate', str(source), str(target), '--reader', 'readers.text',
            '--readers.text.header=X Y Z', '--readers.text.skip=0',
            '--writers.copc.forward=all', '--writers.copc.threads=2')
        if json.loads(run('info', '--summary', str(target)))['summary']['num_points'] != 3:
            raise RuntimeError('COPC preflight point count mismatch')


if __name__ == '__main__':
    check_native(Path(sys.argv[1]).resolve())
    print('PointCloud native preflight passed: pinned PDAL, required readers/writer and COPC conversion')
