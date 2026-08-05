import pytest

from tools import develop_tools


class FakeDevelopClient:
    instances = []

    def __init__(self, **kwargs):
        self.kwargs = kwargs
        self.instances.append(self)

    async def __aenter__(self):
        return self

    async def __aexit__(self, *_args):
        return None

    async def list_operators(self, workflow_engine_id):
        assert workflow_engine_id == 12
        return [{"name": "load", "description": "Load", "category": "IO"}]


@pytest.mark.asyncio
async def test_operator_discovery_uses_copilot_tenant_service_identity(monkeypatch):
    token_source = object()
    FakeDevelopClient.instances.clear()
    monkeypatch.setattr(develop_tools, "DevelopClient", FakeDevelopClient)
    monkeypatch.setattr(
        develop_tools.CopilotInferenceService,
        "token_source",
        classmethod(lambda _cls: token_source),
    )

    result = await develop_tools.OperatorDiscoveryTool()._arun(
        workflow_engine_id=12,
        tenant_id=7,
    )

    assert result == [{"name": "load", "brief": "Load", "category": "IO"}]
    assert FakeDevelopClient.instances[0].kwargs == {
        "base_url": develop_tools.settings.get_develop_url(),
        "tenant_id": 7,
        "service_token_source": token_source,
    }

