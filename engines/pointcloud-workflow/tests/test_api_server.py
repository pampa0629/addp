from __future__ import annotations

import api_server


def test_register_to_system_with_retry_keeps_retrying_until_success(monkeypatch):
    attempts: list[int] = []
    waits: list[int] = []

    class FakeEvent:
        def wait(self, seconds):
            waits.append(seconds)

    def fake_register():
        attempts.append(len(attempts) + 1)
        return len(attempts) == 3

    monkeypatch.setattr(api_server, "converter_status", lambda: {"available": True})
    monkeypatch.setattr(api_server, "register_to_system", fake_register)
    monkeypatch.setattr(api_server.threading, "Event", lambda: FakeEvent())
    monkeypatch.setenv("REGISTRATION_RETRY_INTERVAL_SECONDS", "1")

    api_server.register_to_system_with_retry()

    assert attempts == [1, 2, 3]
    assert waits == [1, 1]


def test_register_to_system_with_retry_skips_when_pdal_unavailable(monkeypatch):
    called = False

    def fake_register():
        nonlocal called
        called = True
        return True

    monkeypatch.setattr(api_server, "converter_status", lambda: {"available": False, "details": "missing"})
    monkeypatch.setattr(api_server, "register_to_system", fake_register)

    api_server.register_to_system_with_retry()

    assert called is False
