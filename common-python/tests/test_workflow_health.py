import pytest

from addp_common.workflow_runtime.health import health_dependency, runtime_health


@pytest.mark.parametrize("required,available,code", [
    (True, True, 200), (True, False, 503), (False, False, 200),
])
def test_health_aggregates_only_required_dependencies(required, available, code):
    body, actual = runtime_health("test", "1", 10, lambda: {
        "operators_count": 2,
        "dependencies": {"sdk": health_dependency(available, required=required, version="2")},
    })
    assert actual == code
    assert body["status"] == ("healthy" if code == 200 else "unhealthy")
    assert body["service"] == "test" and body["version"] == "1"
    assert body["dependencies"]["sdk"]["version"] == "2"
    assert "conversion_ready" not in body


def test_probe_exception_keeps_identity_without_leaking_diagnostics():
    def probe():
        raise RuntimeError("secret in /private/runtime")
    body, code = runtime_health("test", "1", 10, probe)
    assert code == 503
    assert body == {"status": "unhealthy", "service": "test", "version": "1",
                    "uptime": 10, "error_code": "health_check_failed"}


def test_dependency_requires_boolean_facts():
    with pytest.raises(ValueError):
        health_dependency("true")
