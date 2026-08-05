from types import SimpleNamespace

from repositories.inference_scenario_binding_repository import InferenceScenarioBindingRepository


class ScalarSequenceSession:
    def __init__(self, *values):
        self.values = list(values)
        self.calls = 0

    def scalar(self, _statement):
        self.calls += 1
        return self.values.pop(0)


def test_resolve_prefers_explicit_tenant_binding():
    tenant_binding = SimpleNamespace(scope_type="tenant", tenant_id=7)
    session = ScalarSequenceSession(tenant_binding)

    resolved = InferenceScenarioBindingRepository(session).resolve(7, "nl2sql")

    assert resolved is tenant_binding
    assert session.calls == 1


def test_resolve_uses_platform_default_only_when_tenant_binding_is_absent():
    platform_binding = SimpleNamespace(scope_type="platform", tenant_id=None)
    session = ScalarSequenceSession(None, platform_binding)

    resolved = InferenceScenarioBindingRepository(session).resolve(7, "nl2dag")

    assert resolved is platform_binding
    assert session.calls == 2


def test_resolve_returns_none_without_hidden_fallback():
    session = ScalarSequenceSession(None, None)

    resolved = InferenceScenarioBindingRepository(session).resolve(7, "navigation_guide")

    assert resolved is None
    assert session.calls == 2
