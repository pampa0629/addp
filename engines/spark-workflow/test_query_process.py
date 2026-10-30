"""Real subprocess checks for deadlines, secrets and process-tree cleanup."""
from decimal import Decimal
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest.mock import patch

from query_process import QueryDriverError, run_driver


class QueryProcessTest(unittest.TestCase):
    def command(self, source):
        return [sys.executable, '-c', source]

    def test_request_is_private_and_result_preserves_numeric_precision(self):
        source = '''
import json, os, pathlib, sys
request=json.load(sys.stdin)
assert request['password']=='private-fixture-secret'
assert 'private-fixture-secret' not in repr(sys.argv)
assert 'SPARK_WORKFLOW_SERVICE_CLIENT_SECRET' not in os.environ
assert 'UNRELATED_SECRET' not in os.environ
assert pathlib.Path.cwd().stat().st_mode & 0o777 == 0o700
pathlib.Path(sys.argv[1]).write_text('{"id":9007199254740993,"amount":100.123456789012345678,"directory":' + json.dumps(os.getcwd()) + '}')
'''
        with patch.dict(os.environ, SPARK_WORKFLOW_SERVICE_CLIENT_SECRET='runtime-secret',
                        UNRELATED_SECRET='unrelated'):
            result = run_driver(self.command(source), {'password': 'private-fixture-secret'}, timeout=5)
        self.assertEqual(result['id'], 9007199254740993)
        self.assertEqual(result['amount'], Decimal('100.123456789012345678'))
        self.assertFalse(Path(result['directory']).exists())

    def test_success_failure_and_bad_results_do_not_leak_child_output(self):
        for source, code in (
            ("import sys; print('secret'); sys.exit(7)", 'query_driver_failed'),
            ("pass", 'query_result_invalid'),
            ("import pathlib,sys; pathlib.Path(sys.argv[1]).write_text('[]')", 'query_result_invalid'),
            ("import pathlib,sys; pathlib.Path(sys.argv[1]).write_text('{\"x\":NaN}')", 'query_result_invalid'),
            ("import pathlib,sys; pathlib.Path(sys.argv[1]).write_bytes(b'x'*33)", 'query_result_too_large'),
        ):
            with self.subTest(code=code), self.assertRaises(QueryDriverError) as raised:
                run_driver(self.command(source), {}, timeout=5, max_result_bytes=32)
            self.assertEqual(str(raised.exception), code)

    def test_timeout_and_cancellation_kill_even_signal_ignoring_descendants(self):
        for cancellation in (False, True):
            with self.subTest(cancellation=cancellation), tempfile.TemporaryDirectory() as directory:
                pid_path = Path(directory) / 'pid'
                source = '''
import pathlib, subprocess, sys, time
child=subprocess.Popen([sys.executable,'-c', 'import signal,time; signal.signal(signal.SIGTERM,signal.SIG_IGN); print("ready",flush=True); time.sleep(30)'], stdout=subprocess.PIPE)
assert child.stdout.readline() == b'ready\\n'
pathlib.Path(sys.argv[1]).write_text(str(child.pid))
time.sleep(30)
'''
                event = threading.Event()
                errors = []
                def execute():
                    try:
                        run_driver([*self.command(source), str(pid_path)], {},
                                   timeout=5 if cancellation else 1, cancel=event)
                    except QueryDriverError as error:
                        errors.append(error.code)
                thread = threading.Thread(target=execute)
                thread.start()
                deadline = time.monotonic() + 3
                while not pid_path.exists() and time.monotonic() < deadline:
                    time.sleep(0.01)
                self.assertTrue(pid_path.exists())
                if cancellation:
                    event.set()
                thread.join(timeout=7)
                self.assertFalse(thread.is_alive())
                self.assertEqual(errors, ['query_cancelled' if cancellation else 'query_timeout'])
                child_pid = int(pid_path.read_text())
                state = subprocess.run(['ps', '-o', 'stat=', '-p', str(child_pid)],
                                       capture_output=True, text=True, check=False).stdout.strip()
                self.assertTrue(not state or state.startswith('Z'), state)

    def test_invalid_inputs_or_already_cancelled_never_start_a_driver(self):
        event = threading.Event()
        event.set()
        with patch('query_process.subprocess.Popen') as spawn:
            with self.assertRaises(QueryDriverError):
                run_driver(self.command('pass'), {}, timeout=5, cancel=event)
            for timeout in (0, -1, float('nan'), float('inf'), True):
                with self.assertRaises(ValueError):
                    run_driver(self.command('pass'), {}, timeout=timeout)
            spawn.assert_not_called()


if __name__ == '__main__':
    unittest.main()
