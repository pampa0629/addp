import importlib.util
import json
import stat
import sys
import tempfile
import unittest
import urllib.parse
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock


SCRIPT = Path(__file__).with_name("online-engine-registration.py")
SPEC = importlib.util.spec_from_file_location("online_engine_registration", SCRIPT)
REGISTRATION = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = REGISTRATION
SPEC.loader.exec_module(REGISTRATION)


class OnlineEngineRegistrationTest(unittest.TestCase):
    def grant_clients(self):
        current = {'principal': {'type': 'user', 'id': '8'},
                   'context': {'type': 'tenant', 'tenant_id': '2'},
                   'authorization': {'role_assignments': [{'permissions': sorted(REGISTRATION.SOURCE_INITIALIZER_PERMISSIONS)}]}}
        consumer_current = {'principal': {'type': 'user', 'id': '7'}, 'context': {'type': 'tenant', 'tenant_id': '2'}}
        authorizer, consumer = Mock(), Mock()
        def prepare(method, path, expected, body=None):
            if method == 'GET':
                return SimpleNamespace(payload=current)
            self.assertEqual(expected, (201,))
            return SimpleNamespace(payload=dict(body, engine_id=str(body['catalog_path']['engine_id']), approval_mode='independent', revocation=None))
        def read(method, path, expected, body=None):
            if method == 'GET':
                return SimpleNamespace(payload=consumer_current)
            self.assertEqual(path, '/api/v1/system/engine-access/read-checks/manager-preview')
            self.assertEqual(len(body['targets']), 1)
            return SimpleNamespace(payload={'observed_at': '2026-10-10T00:00:00Z'} if expected == (200,) else {})
        authorizer.request.side_effect, consumer.request.side_effect = prepare, read
        return current, authorizer, consumer

    def test_exact_table_grant_uses_separate_preparer_and_checks_denial_then_read(self):
        _, authorizer, consumer = self.grant_clients()
        receipts = REGISTRATION.initialize_exact_record_read_grants(authorizer, consumer, 2, [(4, 'schema', 'results', 'hdfs_totals')])
        self.assertEqual(len(receipts), 1)
        writes = [call for call in authorizer.request.call_args_list if call.args[0] == 'POST']
        self.assertEqual(len(writes), 1)
        body = writes[0].args[3]
        self.assertEqual(body['recipient_id'], '7')
        self.assertEqual(body['action'], 'read')
        self.assertIs(body['initialize_approval'], True)
        self.assertEqual(body['catalog_path']['segments'][-1], {'term': 'table', 'kind': 'table', 'name': 'hdfs_totals'})
        checks = [call for call in consumer.request.call_args_list if call.args[0] == 'POST']
        self.assertEqual([call.args[2] for call in checks], [(403,), (200,)])
        self.assertTrue(all(call.args[3]['targets'] == [body['catalog_path']] for call in checks))

    def test_grant_preparation_rejects_self_wrong_tenant_and_extra_permissions_before_writes(self):
        for invalid in ('self', 'tenant', 'extra', 'missing'):
            current, authorizer, consumer = self.grant_clients()
            if invalid == 'self': current['principal']['id'] = '7'
            if invalid == 'tenant': current['context']['tenant_id'] = '9'
            permissions = current['authorization']['role_assignments'][0]['permissions']
            if invalid == 'extra': permissions.append('system.engine.create')
            if invalid == 'missing': permissions.pop()
            with self.subTest(invalid=invalid), self.assertRaises(REGISTRATION.RegistrationError):
                REGISTRATION.initialize_exact_record_read_grants(authorizer, consumer, 2, [(4, 'schema', 'results', 'hdfs_totals')])
            self.assertTrue(all(call.args[0] == 'GET' for call in authorizer.request.call_args_list))

    def test_exact_collection_grant_uses_provider_catalog_terms(self):
        _, authorizer, consumer = self.grant_clients()
        REGISTRATION.initialize_exact_record_read_grants(authorizer, consumer, 2,
            [(29, 'database', 'Outdoor', 'Persons')], item_term='collection')
        write = next(call for call in authorizer.request.call_args_list if call.args[0] == 'POST')
        self.assertEqual(write.args[3]['catalog_path']['segments'][-1],
                         {'term': 'collection', 'kind': 'collection', 'name': 'Persons'})

    def test_rejects_unknown_item_term_and_collection_schema_before_any_request(self):
        for term, namespace in (('unknown', 'database'), ('collection', 'schema')):
            _, authorizer, consumer = self.grant_clients()
            with self.subTest(term=term, namespace=namespace), self.assertRaises(REGISTRATION.RegistrationError):
                REGISTRATION.initialize_exact_record_read_grants(authorizer, consumer, 2,
                    [(29, namespace, 'Outdoor', 'Persons')], item_term=term)
            authorizer.request.assert_not_called()
            consumer.request.assert_not_called()

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

    def test_failed_probe_reports_reason_without_configured_credentials(self) -> None:
        secret = "secret 密码/&+"
        self.payload["connection_info"]["credentials"] = {"password": secret}
        variants = (secret, urllib.parse.quote(secret, safe=""),
                    urllib.parse.quote_plus(secret), json.dumps(secret)[1:-1])
        requester = Mock(side_effect=[{"id": 17, "engine_type": "sampledb"},
                         {"success": False, "error": "SASL negotiation failed\n" + " ".join(variants)}])
        with self.assertRaises(REGISTRATION.RegistrationError) as raised:
            REGISTRATION.register_engine("http://127.0.0.1:8180", "token", self.payload, requester)
        detail = str(raised.exception)
        self.assertIn("type=sampledb, id=17", detail)
        self.assertIn("SASL negotiation failed", detail)
        for value in variants:
            self.assertNotIn(value, detail)
        self.assertNotIn("\n", detail)

    def test_descriptor_requires_owner_only_canonical_payload(self) -> None:
        self.assertEqual(
            REGISTRATION.load_descriptor(self.descriptor)["engine_type"], "sampledb"
        )
        self.descriptor.chmod(0o644)
        with self.assertRaisesRegex(REGISTRATION.RegistrationError, "owner-only"):
            REGISTRATION.load_descriptor(self.descriptor)

    def test_external_environment_rejects_external_system(self) -> None:
        base, token = REGISTRATION.require_external_environment(
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
            REGISTRATION.require_external_environment(
                {
                    "GITHUB_ACTIONS": "true",
                    "RUNNER_OS": "Linux",
                    "ADDP_ONLINE_HOSTED": "1",
                    "SYSTEM_URL": "https://system.example.com",
                    "ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN": "addp_at_engine",
                }
            )

    def test_owner_managed_environment_is_an_independent_external_profile(self) -> None:
        base, token = REGISTRATION.require_external_environment(
            {
                "GITHUB_ACTIONS": "true",
                "RUNNER_OS": "Linux",
                "ADDP_ONLINE_OWNER_MANAGED": "1",
                "SYSTEM_URL": "http://localhost:8180",
                "ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN": "addp_at_engine",
            }
        )
        self.assertEqual(base, "http://localhost:8180")
        self.assertEqual(token, "addp_at_engine")
        with self.assertRaisesRegex(REGISTRATION.RegistrationError, "exactly one"):
            REGISTRATION.require_external_environment(
                {
                    "GITHUB_ACTIONS": "true",
                    "RUNNER_OS": "Linux",
                    "ADDP_ONLINE_HOSTED": "1",
                    "ADDP_ONLINE_OWNER_MANAGED": "1",
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
