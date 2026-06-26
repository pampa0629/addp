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


class SystemClientTest(unittest.TestCase):
    @patch("system_client.requests.get")
    def test_get_engine_uses_internal_api_with_internal_key(self, mock_get):
        os.environ["SYSTEM_URL"] = "http://system-backend:8180/"
        os.environ["INTERNAL_API_KEY"] = "secret"
        mock_get.return_value = FakeResponse(200, {"id": 34, "engine_type": "spark"})

        engine = get_engine(34)

        self.assertEqual(engine["id"], 34)
        mock_get.assert_called_once_with(
            "http://system-backend:8180/api/v1/internal/engines/34",
            headers={"X-Internal-API-Key": "secret"},
            timeout=10,
        )

    @patch("system_client.requests.get")
    def test_get_engine_raises_for_non_success_response(self, mock_get):
        os.environ["SYSTEM_URL"] = "http://localhost:8180"
        os.environ["INTERNAL_API_KEY"] = ""
        mock_get.return_value = FakeResponse(404, text="not found")

        with self.assertRaisesRegex(ValueError, "Failed to get engine 99"):
            get_engine(99)


if __name__ == "__main__":
    unittest.main()

