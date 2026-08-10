import json
import unittest

import httpx

from addp_common.client import (
    EmbeddingInput,
    InferenceClient,
    Message,
    OAuthServiceTokenSource,
)


class InferenceClientTests(unittest.IsolatedAsyncioTestCase):
    async def test_uses_tenant_service_token_and_versioned_contract(self):
        token_requests = 0

        async def system_handler(request):
            nonlocal token_requests
            token_requests += 1
            self.assertEqual(request.url.path, "/api/v1/system/oauth/token")
            self.assertEqual(request.headers["Authorization"], "Basic YWRkcC1hZ2VudDp0ZXN0LXNlcnZpY2UtY2xpZW50LXNlY3JldC0zMmJ5dGVz")
            form = dict(item.split("=", 1) for item in request.content.decode().split("&"))
            self.assertEqual(form["grant_type"], "client_credentials")
            self.assertEqual(form["tenant_id"], "7")
            return httpx.Response(200, json={
                "access_token": "addp_at_service-token",
                "token_type": "Bearer",
                "expires_in": 120,
                "scope": "addp.api",
            })

        async def inference_handler(request):
            if request.url.path == "/api/v1/system/runtime/engine-descriptors":
                return httpx.Response(200, json={
                    "data": [{
                        "id": 9,
                        "engine_type": "inference_runtime",
                        "is_builtin": True,
                        "lifecycle_state": "active",
                        "capabilities": {
                            "schema_version": "engine.capabilities/v1",
                            "engine_type": "inference_runtime",
                            "engine_family": "inference",
                            "compute": {"inference": {"supported": True, "runtime_api": "addp.inference/v1", "operations": ["embedding"]}},
                        },
                        "runtime_endpoint": {"protocol": "http", "host": "inference", "port": 8191},
                    }],
                    "total": 1,
                    "page": 1,
                    "page_size": 2,
                })
            self.assertEqual(request.url.path, "/api/v1/inference/internal/embeddings")
            self.assertEqual(request.headers["Authorization"], "Bearer addp_at_service-token")
            self.assertEqual(json.loads(request.content), {
                "schema_version": "addp.inference/v1",
                "tenant_id": 7,
                "model_profile_id": "profile-1",
                "inputs": [{"modality": "text", "text": "hello"}],
            })
            return httpx.Response(200, json={
                "schema_version": "addp.inference/v1",
                "vectors": [[0.1, 0.2]],
                "dimension": 2,
                "usage": {"input_tokens": 1, "total_tokens": 1},
                "deployment_id": "deployment-1",
                "profile_version": 3,
            })

        token_source = OAuthServiceTokenSource(
            "http://system",
            "addp-agent",
            "test-service-client-secret-32bytes",
            transport=httpx.MockTransport(system_handler),
        )
        client = InferenceClient(
            "http://system",
            token_source,
            transport=httpx.MockTransport(inference_handler),
        )
        try:
            first = await client.embed(
                tenant_id=7,
                model_profile_id="profile-1",
                inputs=[EmbeddingInput(modality="text", text="hello")],
            )
            await client.embed(
                tenant_id=7,
                model_profile_id="profile-1",
                inputs=[EmbeddingInput(modality="text", text="hello")],
            )
        finally:
            await client.close()
            await token_source.close()

        self.assertEqual(first.deployment_id, "deployment-1")
        self.assertEqual(first.profile_version, 3)
        self.assertEqual(token_requests, 1)

    async def test_chat_rejects_missing_tenant_before_http(self):
        token_source = OAuthServiceTokenSource(
            "http://system",
            "addp-copilot",
            "test-service-client-secret-32bytes",
            transport=httpx.MockTransport(lambda _request: httpx.Response(500)),
        )
        client = InferenceClient("http://system", token_source)
        try:
            with self.assertRaisesRegex(ValueError, "tenant ID"):
                await client.chat(
                    tenant_id=0,
                    model_profile_id="profile-1",
                    messages=[Message(role="user", content="hello")],
                )
        finally:
            await client.close()
            await token_source.close()

    async def test_retries_once_with_refreshed_token_after_401(self):
        token_requests = 0
        inference_requests = 0

        async def system_handler(_request):
            nonlocal token_requests
            token_requests += 1
            return httpx.Response(200, json={
                "access_token": f"addp_at_token-{token_requests}",
                "token_type": "Bearer",
                "expires_in": 120,
                "scope": "addp.api",
            })

        async def inference_handler(request):
            nonlocal inference_requests
            if request.url.path == "/api/v1/system/runtime/engine-descriptors":
                return httpx.Response(200, json={
                    "data": [{
                        "id": 9,
                        "engine_type": "inference_runtime",
                        "is_builtin": True,
                        "lifecycle_state": "active",
                        "capabilities": {
                            "schema_version": "engine.capabilities/v1",
                            "engine_type": "inference_runtime",
                            "engine_family": "inference",
                            "compute": {"inference": {"supported": True, "runtime_api": "addp.inference/v1", "operations": ["embedding"]}},
                        },
                        "runtime_endpoint": {"protocol": "http", "host": "inference", "port": 8191},
                    }],
                    "total": 1,
                    "page": 1,
                    "page_size": 2,
                })
            inference_requests += 1
            if request.headers["Authorization"] == "Bearer addp_at_token-1":
                return httpx.Response(401, json={"error_code": "unauthorized"})
            return httpx.Response(200, json={
                "schema_version": "addp.inference/v1",
                "model_profile_id": "profile-1",
                "profile_version": 2,
                "deployment_id": "deployment-1",
                "dimension": 2560,
            })

        token_source = OAuthServiceTokenSource(
            "http://system",
            "addp-manager",
            "test-service-client-secret-32bytes",
            transport=httpx.MockTransport(system_handler),
        )
        client = InferenceClient(
            "http://system",
            token_source,
            transport=httpx.MockTransport(inference_handler),
        )
        try:
            response = await client.resolve_profile(
                tenant_id=7,
                model_profile_id="profile-1",
                operation="embedding",
                modality="image",
            )
        finally:
            await client.close()
            await token_source.close()

        self.assertEqual(response.profile_version, 2)
        self.assertEqual(token_requests, 2)
        self.assertEqual(inference_requests, 2)


if __name__ == "__main__":
    unittest.main()
