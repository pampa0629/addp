import json
import unittest

import httpx

from addp_common.client import (
    ConfigurationManagementDeclaration,
    ConfigurationManagementEntry,
    ModuleRegistration,
    ModuleRegistryClient,
    OAuthServiceTokenSource,
)


class ModuleRegistryClientTests(unittest.IsolatedAsyncioTestCase):
    async def test_register_uses_platform_service_token_and_typed_declaration(self):
        token_requests = 0

        async def token_handler(request):
            nonlocal token_requests
            token_requests += 1
            form = dict(item.split("=", 1) for item in request.content.decode().split("&"))
            self.assertEqual(form["context_type"], "platform")
            self.assertNotIn("tenant_id", form)
            return httpx.Response(200, json={
                "access_token": "addp_at_platform-token",
                "token_type": "Bearer",
                "expires_in": 120,
                "scope": "addp.api",
            })

        async def registry_handler(request):
            self.assertEqual(request.headers["Authorization"], "Bearer addp_at_platform-token")
            payload = json.loads(request.content)
            self.assertEqual(payload["module_name"], "agent")
            if request.url.path == "/api/v1/system/runtime/modules":
                self.assertEqual(
                    payload["configuration_management"]["schema_version"],
                    "addp.configuration-management/v1",
                )
                self.assertEqual(
                    payload["configuration_management"]["entries"][0]["frontend_route"],
                    "/configuration/agent/inference",
                )
            else:
                self.assertEqual(request.url.path, "/api/v1/system/runtime/modules/heartbeat")
            return httpx.Response(200, json={})

        token_source = OAuthServiceTokenSource(
            "http://system",
            "addp-agent",
            "test-service-client-secret-32bytes",
            transport=httpx.MockTransport(token_handler),
        )
        client = ModuleRegistryClient(
            "http://system",
            token_source,
            transport=httpx.MockTransport(registry_handler),
        )
        registration = ModuleRegistration(
            module_name="agent",
            module_url="http://agent:8190",
            route_prefix="/agent",
            configuration_management=ConfigurationManagementDeclaration(entries=[
                ConfigurationManagementEntry(
                    id="agent.inference_bindings",
                    owner_module="agent",
                    scope_types=["platform_default_with_tenant_override"],
                    frontend_route="/configuration/agent/inference",
                    read_permission="agent.configuration.read",
                    update_permission="agent.configuration.update",
                ),
            ]),
        )
        try:
            await client.register(registration)
            await client.heartbeat("agent")
        finally:
            await client.close()
            await token_source.close()

        self.assertEqual(token_requests, 1)

    async def test_registry_retries_once_with_refreshed_platform_token_on_401(self):
        issued = 0
        requests = 0

        async def token_handler(_request):
            nonlocal issued
            issued += 1
            return httpx.Response(200, json={
                "access_token": f"addp_at_platform-{issued}",
                "token_type": "Bearer",
                "expires_in": 120,
                "scope": "addp.api",
            })

        async def registry_handler(_request):
            nonlocal requests
            requests += 1
            return httpx.Response(401 if requests == 1 else 200, json={})

        token_source = OAuthServiceTokenSource(
            "http://system",
            "addp-copilot",
            "test-service-client-secret-32bytes",
            transport=httpx.MockTransport(token_handler),
        )
        client = ModuleRegistryClient(
            "http://system",
            token_source,
            transport=httpx.MockTransport(registry_handler),
        )
        try:
            await client.heartbeat("copilot")
        finally:
            await client.close()
            await token_source.close()

        self.assertEqual((issued, requests), (2, 2))


if __name__ == "__main__":
    unittest.main()
