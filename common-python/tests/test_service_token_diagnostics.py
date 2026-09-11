import unittest

import httpx

from addp_common.client import OAuthServiceTokenSource, ServiceTokenError
from addp_common.client.service_token import SyncOAuthServiceTokenSource


TEST_CLIENT_SECRET = "test-service-client-secret-32bytes"


class ServiceTokenDiagnosticsTests(unittest.IsolatedAsyncioTestCase):
    async def test_async_source_classifies_malformed_success_response_as_retryable(self):
        malformed_body = b'{"access_token":"addp_at_sensitive_token"'

        async def handler(_request):
            return httpx.Response(
                200,
                content=malformed_body,
                headers={"Content-Type": "application/json; charset=utf-8"},
            )

        source = OAuthServiceTokenSource(
            "http://system",
            "addp-agent",
            TEST_CLIENT_SECRET,
            transport=httpx.MockTransport(handler),
        )
        try:
            with self.assertRaises(ServiceTokenError) as raised:
                await source.platform_token()
        finally:
            await source.close()

        error = raised.exception
        self.assertEqual(error.code, "service_token_response_malformed")
        self.assertTrue(error.retryable)
        self.assertEqual(error.response_reason, "json_decode_failed")
        self.assertEqual(error.response_content_type, "application/json; charset=utf-8")
        self.assertEqual(error.response_body_bytes, len(malformed_body))
        self.assertNotIn("addp_at_sensitive_token", str(error))

    def test_sync_source_classifies_semantic_success_response_as_deterministic(self):
        body = {
            "access_token": "addp_at_valid",
            "token_type": "bearer",
            "expires_in": 300,
            "scope": "addp.api",
            "refresh_token": "addp_rt_sensitive",
        }

        def handler(_request):
            return httpx.Response(200, json=body)

        source = SyncOAuthServiceTokenSource(
            "http://system",
            "addp-model3d",
            TEST_CLIENT_SECRET,
            transport=httpx.MockTransport(handler),
        )
        try:
            with self.assertRaises(ServiceTokenError) as raised:
                source.platform_token()
        finally:
            source.close()

        error = raised.exception
        self.assertEqual(error.code, "service_token_response_invalid")
        self.assertFalse(error.retryable)
        self.assertEqual(error.response_reason, "unexpected_fields")
        self.assertGreater(error.response_body_bytes, 0)
        self.assertNotIn("addp_rt_sensitive", str(error))


if __name__ == "__main__":
    unittest.main()
