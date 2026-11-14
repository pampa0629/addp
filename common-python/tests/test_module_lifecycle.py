import asyncio
import unittest
from unittest.mock import Mock

from addp_common.client import ModuleRegistration
from addp_common.module_lifecycle import live_response, ready_response, register_after_listener


class _RegistryClient:
    def __init__(self) -> None:
        self.called = asyncio.Event()

    async def run(self, _registration: ModuleRegistration) -> None:
        self.called.set()


class ModuleLifecycleTest(unittest.IsolatedAsyncioTestCase):
    def test_health_uses_registration_process_start(self):
        registration = ModuleRegistration(module_name="agent", module_url="http://agent:8190")
        self.assertEqual(live_response("agent")["started_at"], registration.process_started_at)

    def test_readiness_depends_only_on_local_checks(self):
        registration = ModuleRegistration(module_name="agent", module_url="http://agent:8190")
        registry = Mock()
        for state in ("starting", "registered", "recovering", "failed", "stopped"):
            registry.snapshot.return_value.state = state
            for local_ready in (True, False):
                with self.subTest(state=state, local_ready=local_ready):
                    body, ready = ready_response("agent", registration, registry, local_ready=local_ready)
                    self.assertEqual(ready, local_ready)
                    self.assertEqual(body["registration_state"], state)
                    self.assertEqual(body["status"], "ready" if local_ready else "not_ready")
                    self.assertEqual([check["name"] for check in body["checks"]], ["local_dependencies"])
                    self.assertEqual(body["checks"][0]["status"], body["status"])

    async def test_registration_waits_until_listener_is_bound(self):
        reservation = await asyncio.start_server(lambda _reader, _writer: None, "127.0.0.1", 0)
        port = reservation.sockets[0].getsockname()[1]
        reservation.close()
        await reservation.wait_closed()

        client = _RegistryClient()
        registration = ModuleRegistration(
            module_name="agent",
            module_url=f"http://127.0.0.1:{port}",

        )
        task = asyncio.create_task(
            register_after_listener(client, registration, port, retry_interval=0.01)
        )
        await asyncio.sleep(0.03)
        self.assertFalse(client.called.is_set())

        server = await asyncio.start_server(lambda _reader, writer: writer.close(), "127.0.0.1", port)
        try:
            await asyncio.wait_for(task, timeout=1)
            self.assertTrue(client.called.is_set())
        finally:
            server.close()
            await server.wait_closed()


if __name__ == "__main__":
    unittest.main()
