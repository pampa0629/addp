"""Real IPC lifecycle; no JVM or personal development service needed."""
import os
from pathlib import Path
import sys
import tempfile
import unittest

from query_process import PreparedDriver, QueryDriverError


CHILD = '''import json, sys, time
from pathlib import Path
request=json.loads(sys.stdin.buffer.readline())
path=Path(sys.argv[1])
Path(str(path)+'.facts').write_text(json.dumps({'read_set':{'paths':[]},'engine_read_scope':{'engine_ids':[]}}))
if sys.stdin.buffer.readline()==b'execute\\n':
    time.sleep(request.get('delay',0))
    path.write_text(json.dumps({'pid':__import__('os').getpid(),'rows':[{'id':'9007199254740993'}]}))
'''


class PreparedQueryProcessTest(unittest.TestCase):
    def prepare(self, **options):
        directory = tempfile.TemporaryDirectory(prefix='addp-prepared-child-fixture-')
        self.addCleanup(directory.cleanup)
        child = Path(directory.name)/'child.py'
        child.write_text(CHILD)
        prepared = PreparedDriver([sys.executable, str(child)], options, timeout=options.get('timeout', 3))
        self.addCleanup(prepared.close)
        return prepared

    def test_no_rows_until_explicit_allow_then_same_process_once_and_zero_residuals(self):
        prepared = self.prepare()
        pid, directory = prepared.process.pid, prepared.directory.name
        self.assertFalse(prepared.path.exists())
        result = prepared.execute()
        self.assertEqual(result['pid'], pid)
        self.assertEqual(result['rows'], [{'id':'9007199254740993'}])
        self.assertFalse(Path(directory).exists())
        with self.assertRaises(ProcessLookupError): os.kill(pid, 0)
        with self.assertRaises(QueryDriverError): prepared.execute()

    def test_denied_preparation_closes_without_collecting_any_rows(self):
        prepared = self.prepare()
        directory, pid = prepared.directory.name, prepared.process.pid
        prepared.close()
        prepared.close()
        self.assertFalse(Path(directory).exists())
        with self.assertRaises(ProcessLookupError): os.kill(pid, 0)

    def test_large_request_cannot_block_startup_beyond_its_deadline(self):
        with tempfile.TemporaryDirectory(prefix='addp-slow-start-fixture-') as directory:
            child = Path(directory)/'slow.py'
            child.write_text('import time; time.sleep(10)')
            with self.assertRaisesRegex(QueryDriverError, 'query_timeout'):
                PreparedDriver([sys.executable,str(child)], {'query':'x'*262144}, timeout=.3)

    def test_execution_deadline_covers_retained_wait_and_cleanup(self):
        prepared = self.prepare(delay=2, timeout=.3)
        directory = prepared.directory.name
        with self.assertRaisesRegex(QueryDriverError, 'query_timeout'): prepared.execute()
        self.assertFalse(Path(directory).exists())
