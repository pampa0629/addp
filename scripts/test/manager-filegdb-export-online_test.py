import importlib
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock
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

    def test_source_scan_addresses_the_table_and_proves_one_item(self):
        helper = Mock()
        helper.wait_for_meta_scan.return_value = 'execution-7'
        initializer = Mock()
        initializer.request.return_value = SimpleNamespace(payload={
            'metadata': {'items_scanned': 1, 'catalog_nodes_scanned': 0},
        })
        locator, execution = owner.scan_source(helper, initializer, 2)
        self.assertEqual(locator, 'addp://engine/2/path/public/filegdb_export_points?type=table')
        self.assertEqual(execution, 'execution-7')
        self.assertEqual(helper.wait_for_meta_scan.call_args.kwargs, {'scope': {'targets': [locator]}})
        initializer.request.return_value.payload['metadata']['items_scanned'] = 0
        with self.assertRaisesRegex(ValueError, 'requires one scanned source table'):
            owner.scan_source(helper, initializer, 2)
