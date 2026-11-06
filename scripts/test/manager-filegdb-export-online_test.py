import importlib
from pathlib import Path
import tempfile
import unittest
import zipfile

owner = importlib.import_module('scripts.test.manager-filegdb-export-online')


class FileGDBArchiveProofTest(unittest.TestCase):
    def test_unpack_requires_one_safe_geodatabase(self):
        for invalid in (None, 'another.gdb/extra', 'points.gdb/../escape', 'points.gdb/nested/extra'):
            with self.subTest(invalid=invalid), tempfile.TemporaryDirectory() as workspace:
                root = Path(workspace)
                archive = root / 'points.gdb.zip'
                with zipfile.ZipFile(archive, 'w') as output:
                    output.writestr('points.gdb/', '')
                    output.writestr('points.gdb/a00000001.gdbtable', 'fixture')
                    if invalid:
                        output.writestr(invalid, 'extra')
                if invalid:
                    with self.assertRaisesRegex(ValueError, 'unexpected FileGDB archive member'):
                        owner.unpack_archive(archive, root / 'output', 'points.gdb')
                    self.assertFalse((root / 'output').exists())
                else:
                    unpacked = owner.unpack_archive(archive, root / 'output', 'points.gdb')
                    self.assertEqual((unpacked / 'a00000001.gdbtable').read_text(), 'fixture')
