"""Owned per-query Driver lifecycle inside the existing Spark HTTP runtime.

The command is chosen by the runtime, never supplied by a query request. The
future HTTP adapter must authorize the cluster and sources before invoking it.
Credentials travel only over stdin; child output is not a public diagnostic.
"""
from decimal import Decimal
import json
import math
import os
from pathlib import Path
import signal
import subprocess
import tempfile
import time


DRIVER_ENV_KEYS = frozenset({
    'PATH', 'JAVA_HOME', 'SPARK_HOME', 'PYTHONPATH', 'PYSPARK_SUBMIT_ARGS',
    'PYSPARK_PYTHON', 'PYSPARK_DRIVER_PYTHON', 'JAVA_TOOL_OPTIONS',
    'SPARK_WORKFLOW_SHARED_HOST', 'HADOOP_USER_NAME', 'LANG', 'LC_ALL',
    'NO_PROXY', 'no_proxy',
})


class QueryDriverError(Exception):
    """Closed diagnostic code; never embeds Spark stderr, SQL or credentials."""

    def __init__(self, code):
        self.code = code
        super().__init__(code)


def _reject_json_constant(_value):
    raise ValueError('non-finite JSON value')


def _stop_group(process):
    try:
        os.killpg(process.pid, signal.SIGTERM)
    except ProcessLookupError:
        pass
    try:
        process.wait(timeout=1)
    except subprocess.TimeoutExpired:
        pass
    finally:
        # The parent may have exited while a JVM descendant ignored SIGTERM.
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        process.wait()


def run_driver(command, request, *, timeout, cancel=None, max_result_bytes=4 * 1024 * 1024):
    """Run an owner-selected Driver, appending a private result-file argument.

    A query Driver reads one JSON request from stdin and writes one JSON object
    to that file. This is an internal process contract, not an HTTP API or a
    second service. Its caller owns request authorization and result schemas.
    """
    if (isinstance(timeout, bool) or not isinstance(timeout, (int, float))
            or not math.isfinite(timeout) or timeout <= 0):
        raise ValueError('timeout must be finite and positive')
    if (not isinstance(max_result_bytes, int) or isinstance(max_result_bytes, bool)
            or max_result_bytes <= 0):
        raise ValueError('max_result_bytes must be positive')
    if not isinstance(command, (list, tuple)) or not command or any(
        not isinstance(part, str) or not part or '\x00' in part for part in command
    ):
        raise ValueError('command must be an owner-selected argument sequence')
    if cancel is not None and cancel.is_set():
        raise QueryDriverError('query_cancelled')
    payload = json.dumps(request, ensure_ascii=False, allow_nan=False).encode('utf-8')
    if len(payload) > 1024 * 1024:
        raise QueryDriverError('query_request_too_large')
    deadline = time.monotonic() + timeout
    environment = {key: value for key, value in os.environ.items() if key in DRIVER_ENV_KEYS}
    with tempfile.TemporaryDirectory(prefix='addp-spark-query-') as directory:
        result_path = Path(directory) / 'result.json'
        process = None
        try:
            process = subprocess.Popen(
                [*command, str(result_path)], cwd=directory, env=environment,
                stdin=subprocess.PIPE, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                start_new_session=True,
            )
            first = True
            while True:
                if cancel is not None and cancel.is_set():
                    raise QueryDriverError('query_cancelled')
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise QueryDriverError('query_timeout')
                try:
                    process.communicate(input=payload if first else None, timeout=min(remaining, 0.1))
                    break
                except subprocess.TimeoutExpired:
                    first = False
            if cancel is not None and cancel.is_set():
                raise QueryDriverError('query_cancelled')
            if process.returncode != 0:
                raise QueryDriverError('query_driver_failed')
            if not result_path.is_file() or result_path.is_symlink():
                raise QueryDriverError('query_result_invalid')
            with result_path.open('rb') as result_file:
                result_bytes = result_file.read(max_result_bytes + 1)
            if len(result_bytes) > max_result_bytes:
                raise QueryDriverError('query_result_too_large')
            try:
                result = json.loads(result_bytes, parse_float=Decimal,
                                    parse_constant=_reject_json_constant)
            except (ValueError, UnicodeError):
                raise QueryDriverError('query_result_invalid') from None
            if not isinstance(result, dict):
                raise QueryDriverError('query_result_invalid')
            return result
        except OSError:
            raise QueryDriverError('query_driver_io_failed') from None
        finally:
            if process is not None:
                _stop_group(process)
                if process.stdin is not None:
                    process.stdin.close()
