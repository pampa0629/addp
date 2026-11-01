"""Build artifact provenance/cache tests; the registered native T2 uses javac/JVM."""
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import zipfile

import query_jdbc


class QueryDialectBuildTest(unittest.TestCase):
    def test_build_is_cached_and_stale_or_tampered_artifacts_fail_closed(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'jars').mkdir()
            dependency = root / 'jars/spark-sql_2.12-3.5.0.jar'
            dependency.write_bytes(b'fixed-test-classpath')
            source = root / 'source.java'
            source.write_text('class Fixture {}')
            output = root / 'output/helper.jar'
            def compile_fixture(command, **kwargs):
                self.assertIn('--release', command)
                self.assertEqual(command[command.index('--release')+1], '11')
                classes = Path(command[command.index('-d')+1])
                (classes / 'Fixture.class').write_bytes(b'fixed-test-bytecode')
            with patch.object(query_jdbc, 'SOURCE', source), patch.object(query_jdbc.subprocess, 'run', side_effect=compile_fixture) as compiler:
                query_jdbc.build(root, output)
                self.assertEqual(query_jdbc.verified_artifact(root, output), output.resolve())
                with zipfile.ZipFile(output) as archive:
                    self.assertEqual(archive.read('Fixture.class'), b'fixed-test-bytecode')
                    self.assertEqual(archive.getinfo('Fixture.class').date_time, (1980,1,1,0,0,0))
                query_jdbc.build(root, output)
                self.assertEqual(compiler.call_count, 1)
                output.write_bytes(b'corrupt')
                with self.assertRaisesRegex(ValueError, 'missing or stale'): query_jdbc.verified_artifact(root, output)
                query_jdbc.build(root, output)
                self.assertEqual(compiler.call_count, 2)
                source.write_text('class Changed {}')
                with self.assertRaises(ValueError): query_jdbc.verified_artifact(root, output)
                query_jdbc.build(root, output)
                dependency.write_bytes(b'changed-dependency')
                with self.assertRaises(ValueError): query_jdbc.verified_artifact(root, output)
                self.assertEqual(json.loads(output.with_suffix('.json').read_text())['release'], 11)

    def test_missing_fixed_spark_version_never_compiles(self):
        with tempfile.TemporaryDirectory() as directory, patch.object(query_jdbc.subprocess, 'run') as compiler:
            with self.assertRaises(ValueError): query_jdbc.build(Path(directory), Path(directory)/'helper.jar')
            compiler.assert_not_called()


if __name__ == '__main__': unittest.main()
