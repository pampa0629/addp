import asyncio
import json

import pytest

from services.mql_compiler import MQLClarificationRequired, MQLCompiler, MQLPlanError
from services.query_service import QueryService


def persons_resource():
    return {
        "role": "人员",
        "query_names": {"mql": "Persons"},
        "schema_coverage": "sampled",
        "fields": [
            {"name": "userInfo.nickName", "path": ["userInfo", "nickName"], "type": "string"},
            {
                "name": "myOutdoors",
                "path": ["myOutdoors"],
                "type": "array",
                "element_type": "json",
            },
            {
                "name": "myOutdoors.id",
                "path": ["myOutdoors", "id"],
                "type": "string",
            },
            {
                "name": "entriedOutdoors",
                "path": ["entriedOutdoors"],
                "type": "array",
                "element_type": "json",
            },
            {
                "name": "entriedOutdoors.id",
                "path": ["entriedOutdoors", "id"],
                "type": "string",
            },
            {
                "name": "entriedOutdoors.title",
                "path": ["entriedOutdoors", "title"],
                "type": "string",
            },
        ],
    }


def participation_plan():
    return {
        "collection": "Persons",
        "filters": [{
            "field": "userInfo.nickName",
            "operator": "eq",
            "value": "攀爬",
        }],
        "select_fields": [],
        "sort": [],
        "limit": None,
        "metric": {
            "operation": "count_array_elements",
            "field": "entriedOutdoors",
        },
        "set_comparison": None,
        "assumptions": [],
        "clarification": None,
    }


def overlap_plan(metric):
    return {
        "collection": "Persons",
        "filters": [],
        "select_fields": [],
        "sort": [],
        "limit": None,
        "metric": {"operation": "none", "field": ""},
        "set_comparison": {
            "entity_field": "userInfo.nickName",
            "entity_values": ["攀爬", "神采"],
            "set_fields": ["myOutdoors.id", "entriedOutdoors.id"],
            "metric": metric,
        },
        "assumptions": [],
        "clarification": None,
    }


def test_compile_participation_count_uses_nickname_and_array_size():
    result = MQLCompiler.compile(participation_plan(), [persons_resource()])
    command = json.loads(result["query"])

    assert command == {
        "aggregate": "Persons",
        "pipeline": [
            {"$match": {"userInfo.nickName": {"$param": "nickname"}}},
            {"$project": {"_element_count": {"$size": {"$ifNull": ["$entriedOutdoors", []]}}}},
            {"$group": {"_id": None, "entriedoutdoors_count_array_elements": {"$sum": "$_element_count"}}},
            {"$project": {"_id": 0, "entriedoutdoors_count_array_elements": 1}},
        ],
    }
    assert result["query_parameters"] == [{"name": "nickname", "type": "string", "default": "攀爬"}]


def test_compile_rejects_regex_on_array_object():
    plan = participation_plan()
    plan["filters"][0] = {
        "field": "entriedOutdoors",
        "operator": "regex",
        "value": "攀爬",
    }

    with pytest.raises(MQLPlanError, match="requires a string field"):
        MQLCompiler.compile(plan, [persons_resource()])


def test_compile_requires_clarification_for_model_assumptions():
    plan = participation_plan()
    plan["assumptions"] = ["把攀爬解释为活动标题关键词"]

    with pytest.raises(MQLClarificationRequired, match="未经确认的假设"):
        MQLCompiler.compile(plan, [persons_resource()])


def test_compile_rejects_array_child_as_owner_filter():
    plan = participation_plan()
    plan["filters"][0] = {"field": "entriedOutdoors.title", "operator": "eq", "value": "攀爬"}

    with pytest.raises(MQLPlanError, match="owning record"):
        MQLCompiler.compile(plan, [persons_resource()])


def test_compile_rejects_unknown_schema_coverage():
    resource = persons_resource()
    resource["schema_coverage"] = "unknown"

    with pytest.raises(MQLClarificationRequired, match="先扫描元数据"):
        MQLCompiler.compile(participation_plan(), [resource])


def test_set_overlap_requires_metric_clarification():
    with pytest.raises(MQLClarificationRequired, match="共同活动数量、Jaccard") as captured:
        MQLCompiler.compile(overlap_plan("unspecified"), [persons_resource()])

    assert captured.value.clarification.reason == "set_comparison_metric_ambiguous"


def test_compile_jaccard_overlap_uses_verified_ids_and_deduplicates():
    result = MQLCompiler.compile(overlap_plan("jaccard"), [persons_resource()])
    command = json.loads(result["query"])

    assert command["aggregate"] == "Persons"
    pipeline_text = json.dumps(command["pipeline"], ensure_ascii=False)
    assert "$myOutdoors" in pipeline_text
    assert "$entriedOutdoors" in pipeline_text
    assert "$$item.id" in pipeline_text
    assert "$setUnion" in pipeline_text
    assert "$setIntersection" in pipeline_text
    assert "$divide" in pipeline_text
    assert result["query_parameters"] == [
        {"name": "entity_1", "type": "string", "default": "攀爬"},
        {"name": "entity_2", "type": "string", "default": "神采"},
    ]


def test_compile_intersection_count_has_no_ratio():
    result = MQLCompiler.compile(overlap_plan("intersection_count"), [persons_resource()])
    pipeline_text = json.dumps(json.loads(result["query"])["pipeline"], ensure_ascii=False)

    assert "$setIntersection" in pipeline_text
    assert "$divide" not in pipeline_text


def test_generate_mql_uses_single_semantic_plan_call(monkeypatch):
    captured = []

    class FakeLLM:
        async def ainvoke(self, messages, **_kwargs):
            captured.append(messages)
            return type("Response", (), {"content": json.dumps(participation_plan(), ensure_ascii=False)})()

    monkeypatch.setattr(
        "services.query_service.CopilotInferenceService.chat_model",
        lambda *_args, **_kwargs: FakeLLM(),
    )
    result = asyncio.run(QueryService().generate(
        query="得到攀爬参加活动的次数",
        engine={"id": 11, "engine_type": "mongodb", "capabilities": {}},
        query_language="mql",
        resources=[persons_resource()],
        current_query=None,
        tenant_id=1,
        db=None,
    ))

    assert len(captured) == 1
    assert '"userInfo.nickName"' in result["query"]
    assert '"$size"' in result["query"]
    assert "climbing" not in result["query"]
