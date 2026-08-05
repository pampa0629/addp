import unittest
from types import SimpleNamespace

from repositories.inference_scenario_binding_repository import InferenceScenarioBindingRepository


class ScalarSequenceSession:
    def __init__(self, *values):
        self.values = list(values)
        self.calls = 0

    async def scalar(self, _statement):
        self.calls += 1
        return self.values.pop(0)


class InferenceScenarioBindingRepositoryTests(unittest.IsolatedAsyncioTestCase):
    async def test_resolve_prefers_explicit_tenant_binding(self):
        tenant_binding = SimpleNamespace(scope_type="tenant", tenant_id=7)
        session = ScalarSequenceSession(tenant_binding)

        resolved = await InferenceScenarioBindingRepository(session).resolve(7, "reasoning")

        self.assertIs(resolved, tenant_binding)
        self.assertEqual(session.calls, 1)

    async def test_resolve_uses_platform_default_only_when_tenant_binding_is_absent(self):
        platform_binding = SimpleNamespace(scope_type="platform", tenant_id=None)
        session = ScalarSequenceSession(None, platform_binding)

        resolved = await InferenceScenarioBindingRepository(session).resolve(7, "general-chat")

        self.assertIs(resolved, platform_binding)
        self.assertEqual(session.calls, 2)

    async def test_resolve_returns_none_without_hidden_fallback(self):
        session = ScalarSequenceSession(None, None)

        resolved = await InferenceScenarioBindingRepository(session).resolve(7, "reasoning")

        self.assertIsNone(resolved)
        self.assertEqual(session.calls, 2)
