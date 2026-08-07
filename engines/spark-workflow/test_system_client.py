import os
import unittest
from unittest.mock import patch

from system_client import get_engine


class FakeResponse:
    def __init__(self, status_code, payload=None, text=""):
        self.status_code = status_code
        self._payload = payload or {}
        self.text = text

    def json(self):
        return self._payload


class FakeTokenSource:
    def __init__(self, tokens):
        self.tokens = list(tokens)
        self.tenant_ids = []
        self.invalidations = []

    def token(self, tenant_id):
        self.tenant_ids.append(tenant_id)
        return self.tokens.pop(0)

    def invalidate(self, tenant_id, token):
        self.invalidations.append((tenant_id, token))


class SystemClientTest(unittest.TestCase):
    def tearDown(self):
        os.environ.pop("SYSTEM_URL", None)

    @patch("system_client._service_token_source")
    @patch("system_client.requests.get")
    def test_get_engine_uses_tenant_service_access_token(self, mock_get, mock_token_source):
        os.environ["SYSTEM_URL"] = "http://system-backend:8180/"
        source = FakeTokenSource(["addp_at_test"])
        mock_token_source.return_value = source
        mock_get.return_value = FakeResponse(200, {"id": 34, "engine_type": "spark"})

        engine = get_engine(34, 7)

        self.assertEqual(engine["id"], 34)
        self.assertEqual(source.tenant_ids, [7])
        mock_get.assert_called_once_with(
            "http://system-backend:8180/api/v1/system/engines/34",
            headers={"Authorization": "Bearer addp_at_test"},
            timeout=10,
        )

    @patch("system_client._service_token_source")
    @patch("system_client.requests.get")
    def test_get_engine_retries_once_after_unauthorized(self, mock_get, mock_token_source):
        source = FakeTokenSource(["addp_at_expired", "addp_at_refreshed"])
        mock_token_source.return_value = source
        mock_get.side_effect = [
            FakeResponse(401, text="expired"),
            FakeResponse(200, {"id": 34, "engine_type": "spark"}),
        ]

        engine = get_engine(34, 7)

        self.assertEqual(engine["id"], 34)
        self.assertEqual(source.tenant_ids, [7, 7])
        self.assertEqual(source.invalidations, [(7, "addp_at_expired")])
        self.assertEqual(mock_get.call_count, 2)

    @patch("system_client._service_token_source")
    @patch("system_client.requests.get")
    def test_get_engine_raises_for_non_success_response(self, mock_get, mock_token_source):
        os.environ["SYSTEM_URL"] = "http://localhost:8180"
        mock_token_source.return_value = FakeTokenSource(["addp_at_test"])
        mock_get.return_value = FakeResponse(404, text="not found")

        with self.assertRaisesRegex(ValueError, "Failed to get engine 99"):
            get_engine(99, 7)

    def test_get_engine_rejects_missing_tenant_context(self):
        with self.assertRaisesRegex(ValueError, "tenant_id must be a positive integer"):
            get_engine(34, 0)


if __name__ == "__main__":
    unittest.main()
