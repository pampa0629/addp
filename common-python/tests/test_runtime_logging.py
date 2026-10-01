import json
import logging
import unittest
from contextlib import contextmanager
from unittest.mock import patch
from io import StringIO
from addp_common.runtime_logging import RuntimeFormatter, setup_runtime_logging


@contextmanager
def runtime_output(level=logging.INFO):
    root = logging.getLogger()
    previous, previous_level = root.handlers[:], root.level
    root.handlers = []
    output = StringIO()
    try:
        with patch("sys.stdout", output):
            setup_runtime_logging(level)
            yield output
    finally:
        for handler in root.handlers:
            handler.close()
        root.handlers = previous
        root.setLevel(previous_level)

class RuntimeLoggingTests(unittest.TestCase):
    def test_stack_is_one_json_event_and_plain_stderr_has_no_implied_level(self):
        try:
            raise ValueError("diagnostic detail")
        except ValueError:
            import sys
            record = logging.LogRecord("test", logging.ERROR, __file__, 1, "failed %s", ("operation",), sys.exc_info())
        line = RuntimeFormatter().format(record)
        self.assertNotIn("\n", line)
        value = json.loads(line)
        self.assertEqual(value["level"], "error")
        self.assertEqual(value["message"], "failed operation")
        self.assertIn("ValueError: diagnostic detail", value["stack"])

    def test_setup_has_only_one_stdout_handler_after_reconfiguration(self):
        root = logging.getLogger()
        previous, level = root.handlers[:], root.level
        root.handlers = []
        output = StringIO()
        try:
            with patch("sys.stdout", output):
                setup_runtime_logging(); setup_runtime_logging()
                logging.info("one event")
            self.assertEqual(len(root.handlers), 1)
            self.assertEqual(len(output.getvalue().splitlines()), 1)
            self.assertEqual(json.loads(output.getvalue())["message"], "one event")
        finally:
            root.handlers = previous
            root.setLevel(level)

    def test_launch_identity_is_stable_until_process_changes(self):
        from addp_common.client import module_registry as registry
        with patch.dict("os.environ", {"ADDP_PROCESS_INSTANCE_ID": "launch-instance"}), patch.object(registry, "_PROCESS_INSTANCE_PID", 0), patch.object(registry, "_PROCESS_INSTANCE_ID", ""), patch("os.getpid", return_value=123):
            self.assertEqual(registry._process_instance_id(), "launch-instance")
            with patch.dict("os.environ", {"ADDP_PROCESS_INSTANCE_ID": "later-value"}):
                self.assertEqual(registry._process_instance_id(), "launch-instance")
            with patch("os.getpid", return_value=124):
                self.assertNotEqual(registry._process_instance_id(), "launch-instance")


class RoutineHTTPRequestTests(unittest.IsolatedAsyncioTestCase):
    async def test_default_output_keeps_failures_and_business_requests(self):
        import httpx
        cases = [
            ("POST", "/api/v1/system/runtime/modules/heartbeat", 200, False),
            ("POST", "/api/v1/system/oauth/token", 200, False),
            ("GET", "/health/live?probe=1", 200, False),
            ("GET", "/health/ready", 204, False),
            ("POST", "/api/v1/system/runtime/modules/heartbeat", 404, True),
            ("POST", "/api/v1/system/oauth/token", 401, True),
            ("GET", "/health/ready", 503, True),
            ("GET", "/health/live", 307, True),
            ("GET", "/api/v1/system/runtime/modules/heartbeat", 200, True),
            ("POST", "/health/ready", 200, True),
            ("POST", "/api/v1/copilot/query/generate", 200, True),
        ]
        with runtime_output() as output:
            for method, path, status, visible in cases:
                with self.subTest(method=method, path=path, status=status):
                    output.seek(0)
                    output.truncate(0)
                    transport = httpx.MockTransport(lambda request: httpx.Response(status))
                    async with httpx.AsyncClient(transport=transport) as client:
                        await client.request(method, "http://system.test" + path)
                    events = [json.loads(line) for line in output.getvalue().splitlines()]
                    requests = [event for event in events if event["logger"] == "httpx"]
                    self.assertEqual(len(requests), int(visible))
                    if visible:
                        self.assertEqual(requests[0]["level"], "info")
                        self.assertIn(str(status), requests[0]["message"])
            logging.getLogger("addp.module_registry").warning("heartbeat failed")
            logging.getLogger("copilot.main").info("module registered")
            self.assertIn("heartbeat failed", output.getvalue())
            self.assertIn("module registered", output.getvalue())

    async def test_debug_output_keeps_routine_request_at_debug(self):
        import httpx
        with runtime_output(logging.DEBUG) as output:
            async with httpx.AsyncClient(transport=httpx.MockTransport(lambda request: httpx.Response(200))) as client:
                await client.post("http://system.test/api/v1/system/runtime/modules/heartbeat")
                await client.get("http://system.test/business")
            requests = [json.loads(line) for line in output.getvalue().splitlines()
                        if json.loads(line)["logger"] == "httpx"]
            self.assertEqual([event["level"] for event in requests], ["debug", "info"])
