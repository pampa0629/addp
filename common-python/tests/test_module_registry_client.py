import asyncio
import json
import unittest
import uuid

import httpx

from addp_common.client import (
    ConfigurationManagementDeclaration,
    ConfigurationManagementEntry,
    ModuleRegistration,
    ModuleRegistryAPIError,
    ModuleRegistryClient,
    OAuthServiceTokenSource,
)


class ModuleRegistryClientTests(unittest.IsolatedAsyncioTestCase):
    async def test_register_uses_platform_service_token_and_typed_declaration(self):
        token_requests = 0
        registered_instance_id = ""
        deregistered = False

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
            nonlocal registered_instance_id, deregistered
            self.assertEqual(request.headers["Authorization"], "Bearer addp_at_platform-token")
            if request.url.path == "/api/v1/system/runtime/modules":
                payload = json.loads(request.content)
                self.assertEqual(payload["module_name"], "agent")
                registered_instance_id = payload["instance_id"]
                uuid.UUID(registered_instance_id)
                self.assertEqual(payload["role"], "backend")
                self.assertEqual(
                    payload["configuration_management"]["schema_version"],
                    "addp.configuration-management/v1",
                )
                self.assertEqual(
                    payload["configuration_management"]["entries"][0]["frontend_route"],
                    "/configuration/agent/inference",
                )
                return httpx.Response(200, json={})
            if request.url.path == "/api/v1/system/runtime/modules/heartbeat":
                payload = json.loads(request.content)
                self.assertEqual(payload, {
                    "module_name": "agent",
                    "instance_id": registered_instance_id,
                })
                return httpx.Response(200, json={})
            if request.method == "DELETE":
                self.assertEqual(
                    request.url.path,
                    f"/api/v1/system/runtime/modules/agent/instances/{registered_instance_id}",
                )
                deregistered = True
                return httpx.Response(204)
            else:
                self.assertEqual(request.url.path, "/api/v1/system/runtime/modules/heartbeat")
                return httpx.Response(500)

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
            await client.heartbeat("agent", registration.instance_id)
        finally:
            await client.close()
            await token_source.close()

        self.assertEqual(token_requests, 1)
        self.assertTrue(deregistered)

    async def test_registration_uses_one_process_level_instance_id(self):
        first = ModuleRegistration(
            module_name="agent",
            module_url="http://agent:8190",
            route_prefix="/agent",
        )
        second = ModuleRegistration(
            module_name="copilot",
            module_url="http://copilot:8087",
            route_prefix="/copilot",
        )

        uuid.UUID(first.instance_id)
        self.assertEqual(first.instance_id, second.instance_id)
        self.assertEqual((first.role, second.role), ("backend", "backend"))

    async def test_run_deregisters_the_registered_instance_when_cancelled(self):
        registered = asyncio.Event()
        deregistered = asyncio.Event()

        async def token_handler(_request):
            return httpx.Response(200, json={
                "access_token": "addp_at_platform-token",
                "token_type": "Bearer",
                "expires_in": 120,
                "scope": "addp.api",
            })

        async def registry_handler(request):
            if request.method == "POST" and request.url.path == "/api/v1/system/runtime/modules":
                registered.set()
                return httpx.Response(200, json={})
            if request.method == "DELETE":
                deregistered.set()
                return httpx.Response(204)
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
        )
        task = asyncio.create_task(client.run(registration, interval=60.0))
        try:
            await asyncio.wait_for(registered.wait(), timeout=1.0)
            task.cancel()
            with self.assertRaises(asyncio.CancelledError):
                await task
            self.assertTrue(deregistered.is_set())
        finally:
            if not task.done():
                task.cancel()
                await task
            await client.close()
            await token_source.close()

    async def test_heartbeat_failure_reregisters_same_instance_without_waiting_for_lease_expiry(self):
        registration_count = 0
        heartbeat_failed = asyncio.Event()
        reregistered = asyncio.Event()
        registered_instance_ids = []

        async def token_handler(_request):
            return httpx.Response(200, json={
                "access_token": "addp_at_platform-token",
                "token_type": "Bearer",
                "expires_in": 120,
                "scope": "addp.api",
            })

        async def registry_handler(request):
            nonlocal registration_count
            if request.method == "POST" and request.url.path == "/api/v1/system/runtime/modules":
                registration_count += 1
                registered_instance_ids.append(json.loads(request.content)["instance_id"])
                if registration_count == 2:
                    reregistered.set()
                return httpx.Response(200, json={})
            if request.method == "POST" and request.url.path == "/api/v1/system/runtime/modules/heartbeat":
                heartbeat_failed.set()
                return httpx.Response(404, json={"error": "runtime instance not found"})
            if request.method == "DELETE":
                return httpx.Response(204)
            return httpx.Response(404)

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
        )
        task = asyncio.create_task(client.run(registration, interval=0.01))
        try:
            await asyncio.wait_for(heartbeat_failed.wait(), timeout=1.0)
            await asyncio.wait_for(reregistered.wait(), timeout=1.0)
            self.assertEqual(registered_instance_ids, [registration.instance_id, registration.instance_id])
        finally:
            task.cancel()
            with self.assertRaises(asyncio.CancelledError):
                await task
            await client.close()
            await token_source.close()

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
            await client.heartbeat("copilot", "copilot-instance")
        finally:
            await client.close()
            await token_source.close()

        self.assertEqual((issued, requests), (2, 2))

    async def test_4xx_error_and_background_log_include_response_body(self):
        async def token_handler(_request):
            return httpx.Response(200, json={
                "access_token": "addp_at_platform-token",
                "token_type": "Bearer",
                "expires_in": 120,
                "scope": "addp.api",
            })

        async def registry_handler(_request):
            return httpx.Response(400, json={
                "error": "instance_id and role are required",
                "error_code": "module_registration_invalid",
            })

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
        )
        try:
            with self.assertRaises(ModuleRegistryAPIError) as captured_error:
                await client.register(registration)
            api_error = captured_error.exception
            self.assertEqual(api_error.method, "POST")
            self.assertEqual(api_error.path, "/api/v1/system/runtime/modules")
            self.assertEqual(api_error.status_code, 400)
            self.assertEqual(api_error.error_code, "module_registration_invalid")
            self.assertEqual(api_error.error_message, "instance_id and role are required")
            self.assertEqual(
                json.loads(api_error.response_body),
                {
                    "error": "instance_id and role are required",
                    "error_code": "module_registration_invalid",
                },
            )

            with self.assertLogs("addp.module_registry", level="WARNING") as captured_logs:
                registry_task = asyncio.create_task(client.run(registration))
                await asyncio.sleep(0.05)
                registry_task.cancel()
                with self.assertRaises(asyncio.CancelledError):
                    await registry_task
            diagnostic_log = "\n".join(captured_logs.output)
            for expected in (
                "operation=register",
                "module=agent",
                f"instance_id={registration.instance_id}",
                "role=backend",
                "status_code=400",
                "error_code='module_registration_invalid'",
                "error_message='instance_id and role are required'",
                "response_body=",
            ):
                self.assertIn(expected, diagnostic_log)
        finally:
            await client.close()
            await token_source.close()


if __name__ == "__main__":
    unittest.main()
