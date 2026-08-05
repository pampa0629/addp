import asyncio

from addp_common.client import ChatResponse, InferenceClient
from langchain_core.messages import HumanMessage, SystemMessage

from services.inference_service import CopilotInferenceChatModel


class FakeInferenceClient(InferenceClient):
    def __init__(self):
        self.request = None

    async def chat(self, **kwargs):
        self.request = kwargs
        return ChatResponse.model_validate(
            {
                "schema_version": "addp.inference/v1",
                "message": {"role": "assistant", "content": "SELECT 1"},
                "usage": {"input_tokens": 4, "output_tokens": 2, "total_tokens": 6},
                "deployment_id": "00000000-0000-0000-0000-000000000002",
                "profile_version": 3,
            }
        )


def test_chat_model_uses_tenant_profile_without_provider_fields():
    client = FakeInferenceClient()
    model = CopilotInferenceChatModel(
        client=client,
        tenant_id=7,
        model_profile_id="00000000-0000-0000-0000-000000000001",
        temperature=0.2,
        max_output_tokens=500,
    )
    response = asyncio.run(
        model.ainvoke(
            [
                SystemMessage(content="Generate SQL"),
                HumanMessage(content="one row"),
            ]
        )
    )

    assert response.content == "SELECT 1"
    assert client.request["tenant_id"] == 7
    assert client.request["model_profile_id"] == "00000000-0000-0000-0000-000000000001"
    assert [message.role for message in client.request["messages"]] == ["system", "user"]
    assert client.request["temperature"] == 0.2
    assert client.request["max_output_tokens"] == 500
    assert "provider" not in client.request
    assert "api_key" not in client.request
