import importlib.util
import json
import stat
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock


SCRIPT = Path(__file__).with_name("online-engine-registration.py")
SPEC = importlib.util.spec_from_file_location("online_engine_registration", SCRIPT)
REGISTRATION = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = REGISTRATION
SPEC.loader.exec_module(REGISTRATION)


class OnlineEngineRegistrationTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(
            prefix="addp-online-engine-registration-"
        )
        self.root = Path(self.temporary.name)
        self.descriptor = self.root / "engine.json"
        self.payload = {
            "name": "Hosted Online Engine",
            "engine_type": "sampledb",
            "engine_origin": "general",
            "connection_info": {"host": "127.0.0.1", "port": 15432},
            "description": "Disposable fixture",
        }
        self.descriptor.write_text(json.dumps(self.payload), encoding="utf-8")
        self.descriptor.chmod(0o600)

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def test_registers_descriptor_and_requires_successful_connection_test(self) -> None:
        requester = Mock(
            side_effect=[
                {"id": 17, "engine_type": "sampledb"},
                {"success": True},
            ]
        )

        engine_id = REGISTRATION.register_engine(
            "http://127.0.0.1:8180", "addp_at_engine", self.payload, requester
        )

        self.assertEqual(engine_id, 17)
        self.assertEqual(requester.call_count, 2)
        self.assertEqual(
            requester.call_args_list[0].args[2:4],
            ("POST", "/api/v1/system/engines"),
        )
        self.assertEqual(
            requester.call_args_list[1].args[2:4],
            ("POST", "/api/v1/system/engines/17/test"),
        )

        requester = Mock(
            side_effect=[
                {"id": 17, "engine_type": "sampledb"},
                {"success": False},
            ]
        )
        with self.assertRaisesRegex(
            REGISTRATION.RegistrationError, "connection test did not succeed"
        ):
            REGISTRATION.register_engine(
                "http://127.0.0.1:8180", "addp_at_engine", self.payload, requester
            )

    def test_descriptor_requires_owner_only_canonical_payload(self) -> None:
        self.assertEqual(
            REGISTRATION.load_descriptor(self.descriptor)["engine_type"], "sampledb"
        )
        self.descriptor.chmod(0o644)
        with self.assertRaisesRegex(REGISTRATION.RegistrationError, "owner-only"):
            REGISTRATION.load_descriptor(self.descriptor)

    def test_hosted_environment_rejects_external_system(self) -> None:
        base, token = REGISTRATION.require_hosted_environment(
            {
                "GITHUB_ACTIONS": "true",
                "RUNNER_OS": "Linux",
                "ADDP_ONLINE_HOSTED": "1",
                "SYSTEM_URL": "http://127.0.0.1:8180",
                "ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN": "addp_at_engine",
            }
        )
        self.assertEqual(base, "http://127.0.0.1:8180")
        self.assertEqual(token, "addp_at_engine")
        with self.assertRaisesRegex(REGISTRATION.RegistrationError, "loopback"):
            REGISTRATION.require_hosted_environment(
                {
                    "GITHUB_ACTIONS": "true",
                    "RUNNER_OS": "Linux",
                    "ADDP_ONLINE_HOSTED": "1",
                    "SYSTEM_URL": "https://system.example.com",
                    "ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN": "addp_at_engine",
                }
            )

    def test_result_contains_only_engine_id_with_owner_permissions(self) -> None:
        output = self.root / "secret/engine.env"

        REGISTRATION.write_result(output, 17)

        self.assertEqual(stat.S_IMODE(output.stat().st_mode), 0o600)
        self.assertEqual(
            output.read_text(encoding="utf-8"),
            "export ADDP_ONLINE_CONSUMER_ENGINE_ID='17'\n",
        )
        self.assertNotIn("addp_at_", output.read_text(encoding="utf-8"))

    def test_request_rejects_non_json_response(self) -> None:
        class Response:
            status = 200

            def __enter__(self):
                return self

            def __exit__(self, *_args):
                return None

            @staticmethod
            def read() -> bytes:
                return b"not-json"

        original = REGISTRATION.urllib.request.urlopen
        REGISTRATION.urllib.request.urlopen = lambda *_args, **_kwargs: Response()
        try:
            with self.assertRaisesRegex(
                REGISTRATION.RegistrationError, "did not return valid JSON"
            ):
                REGISTRATION.request_json(
                    "http://127.0.0.1:8180", "token", "GET", "/health", None, (200,)
                )
        finally:
            REGISTRATION.urllib.request.urlopen = original


if __name__ == "__main__":
    unittest.main()
