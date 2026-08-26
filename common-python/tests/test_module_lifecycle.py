import asyncio
import unittest

from addp_common.client import ModuleRegistration
from addp_common.module_lifecycle import register_after_listener


class _RegistryClient:
    def __init__(self) -> None:
        self.called = asyncio.Event()

    async def run(self, _registration: ModuleRegistration) -> None:
        self.called.set()


class ModuleLifecycleTest(unittest.IsolatedAsyncioTestCase):
    async def test_registration_waits_until_listener_is_bound(self):
        reservation = await asyncio.start_server(lambda _reader, _writer: None, "127.0.0.1", 0)
        port = reservation.sockets[0].getsockname()[1]
        reservation.close()
        await reservation.wait_closed()

        client = _RegistryClient()
        registration = ModuleRegistration(
            module_name="agent",
            module_url=f"http://127.0.0.1:{port}",
            route_prefix="/agent",
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
