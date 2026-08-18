from operators.io_operators import _map_loopback_host


def test_map_loopback_host_uses_container_shared_host(monkeypatch):
    monkeypatch.setenv('GEOPYTHON_WORKFLOW_LOOPBACK_HOST', 'host.docker.internal')

    assert _map_loopback_host('localhost') == 'host.docker.internal'
    assert _map_loopback_host('127.0.0.1') == 'host.docker.internal'
    assert _map_loopback_host('::1') == 'host.docker.internal'


def test_map_loopback_host_preserves_remote_host(monkeypatch):
    monkeypatch.setenv('GEOPYTHON_WORKFLOW_LOOPBACK_HOST', 'host.docker.internal')

    assert _map_loopback_host('database.internal') == 'database.internal'


def test_map_loopback_host_is_inactive_without_shared_host(monkeypatch):
    monkeypatch.delenv('GEOPYTHON_WORKFLOW_LOOPBACK_HOST', raising=False)

    assert _map_loopback_host('localhost') == 'localhost'
