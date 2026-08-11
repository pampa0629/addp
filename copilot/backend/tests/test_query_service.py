import asyncio

import pytest

from langchain_core.messages import HumanMessage, SystemMessage

from services.query_service import QueryService


def test_parse_output_accepts_structured_query_candidate():
    result = QueryService._parse_output('''```json
    {
      "query": "SELECT SUM(ST_Area(ST_Intersection(r.buffer, f.geometry))) FROM railway r JOIN farmland f ON ST_Intersects(r.buffer, f.geometry)",
      "explanation": "计算相交面积",
      "warnings": []
    }
    ```''')

    assert result["query"].startswith("SELECT SUM")
    assert result["explanation"] == "计算相交面积"


def test_parse_output_rejects_internal_locator():
    with pytest.raises(ValueError, match="internal resource facts"):
        QueryService._parse_output('''{
          "query": "SELECT * FROM 'addp://engine/8/path/public/railway?type=table'",
          "explanation": "",
          "warnings": []
        }''')


def test_parse_output_accepts_json_mql_command_object():
    result = QueryService._parse_output(
        '{"query":"{\\"find\\":\\"Persons\\",\\"filter\\":{},\\"limit\\":10}","warnings":[]}',
        query_language="mql",
    )

    assert result["query"] == '{"find":"Persons","filter":{},"limit":10}'


def test_parse_output_rejects_mongo_shell_mql():
    with pytest.raises(ValueError, match="JSON command object"):
        QueryService._parse_output(
            '{"query":"db.getSiblingDB(\\"Outdoor\\").Persons.find({})","warnings":[]}',
            query_language="mql",
        )


def test_generate_includes_current_query_as_non_resource_context(monkeypatch):
    captured = {}

    class FakeLLM:
        async def ainvoke(self, messages):
            captured["messages"] = messages
            return type("Response", (), {
                "content": '{"query":"{\\"find\\":\\"Persons\\",\\"filter\\":{\\"age\\":{\\"$gte\\":18}},\\"limit\\":10}","warnings":[]}',
            })()

    monkeypatch.setattr(
        "services.query_service.CopilotInferenceService.chat_model",
        lambda *_args, **_kwargs: FakeLLM(),
    )

    result = asyncio.run(QueryService().generate(
        query="只保留成年人",
        engine={"id": 11, "engine_type": "mongodb", "capabilities": {}},
        query_language="mql",
        resources=[],
        current_query='{"find":"Persons","filter":{},"limit":10}',
        tenant_id=1,
        db=None,
    ))

    assert result["query"].startswith('{"find":"Persons"')
    assert isinstance(captured["messages"][0], SystemMessage)
    assert "默认保留 current_query 已声明的主 collection" in captured["messages"][0].content
    assert "resources 为空时只能复用 current_query 中已经出现的字段" in captured["messages"][0].content
    assert isinstance(captured["messages"][1], HumanMessage)
    assert '"current_query": "{\\"find\\":\\"Persons\\"' in captured["messages"][1].content
