import asyncio
import json

import pytest

from services.mql_compiler import MQLCompiler, MQLPlanError
from services.query_clarification import QueryClarificationRequired
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


def outdoors_resource():
    return {
        "role": "户外活动",
        "query_names": {"mql": "Outdoors"},
        "schema_coverage": "sampled",
        "fields": [
            {"name": "_id", "path": ["_id"], "type": "string"},
            {"name": "status", "path": ["status"], "type": "string"},
            {"name": "title.date", "path": ["title", "date"], "type": "string"},
            {"name": "members", "path": ["members"], "type": "array", "element_type": "json"},
            {"name": "members.personid", "path": ["members", "personid"], "type": "string"},
            {
                "name": "members.entryInfo.status",
                "path": ["members", "entryInfo", "status"],
                "type": "string",
            },
            {"name": "leader.personid", "path": ["leader", "personid"], "type": "string"},
        ],
    }


def actual_participation_plan():
    return {
        "collection": "Outdoors",
        "filters": [
            {"field": "status", "operator": "ne", "value": "拟定中"},
            {"field": "status", "operator": "ne", "value": "已取消"},
            {"field": "title.date", "operator": "not_empty", "value": True},
        ],
        "select_fields": [],
        "sort": [],
        "limit": None,
        "metric": {
            "operation": "count_distinct_array_elements",
            "field": "members",
            "group_by": ["members.personid"],
            "distinct_by": ["_id"],
            "element_filters": [{
                "field": "members.entryInfo.status",
                "operator": "in",
                "value": ["报名中", "领队", "领队组"],
            }],
        },
        "set_comparison": None,
        "assumptions": [],
        "clarification": None,
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


def outdoor_directional_overlap_plan():
    return {
        "collection": "Outdoors",
        "filters": [
            {"field": "status", "operator": "ne", "value": "拟定中"},
            {"field": "status", "operator": "ne", "value": "已取消"},
            {"field": "title.date", "operator": "not_empty", "value": True},
        ],
        "select_fields": [],
        "sort": [],
        "limit": None,
        "metric": {
            "operation": "directional_overlap_rate",
            "field": "members",
            "entity_field": "members.personid",
            "entity_values": ["A", "B"],
            "activity_id_field": "_id",
            "element_filters": [{
                "field": "members.entryInfo.status",
                "operator": "in",
                "value": ["报名中", "领队", "领队组"],
            }],
        },
        "set_comparison": None,
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


def test_compile_actual_participation_groups_by_person_and_deduplicates_activity():
    result = MQLCompiler.compile(actual_participation_plan(), [outdoors_resource()])
    command = json.loads(result["query"])

    assert command["aggregate"] == "Outdoors"
    assert command["pipeline"][0] == {
        "$match": {
            "$and": [
                {"status": {"$ne": {"$param": "status"}}},
                {"status": {"$ne": {"$param": "status_2"}}},
                {"title.date": {"$exists": True, "$nin": [None, ""]}},
            ],
        },
    }
    assert command["pipeline"][1] == {"$unwind": "$members"}
    assert command["pipeline"][2] == {
        "$match": {
            "members.entryInfo.status": {
                "$in": [
                    {"$param": "status_3"},
                    {"$param": "status_4"},
                    {"$param": "status_5"},
                ],
            },
        },
    }
    assert command["pipeline"][3] == {
        "$match": {
            "members.personid": {"$exists": True, "$nin": [None, ""]},
            "_id": {"$exists": True, "$nin": [None, ""]},
        },
    }
    assert command["pipeline"][4] == {
        "$group": {"_id": {"group_0": "$members.personid", "distinct_0": "$_id"}},
    }
    assert command["pipeline"][5] == {
        "$group": {"_id": {"group_0": "$_id.group_0"}, "_distinct_count": {"$sum": 1}},
    }
    assert command["pipeline"][6] == {
        "$project": {
            "_id": 0,
            "personid": "$_id.group_0",
            "members_count_distinct_array_elements": "$_distinct_count",
        },
    }
    assert result["query_parameters"] == [
        {"name": "status", "type": "string", "default": "拟定中"},
        {"name": "status_2", "type": "string", "default": "已取消"},
        {"name": "status_3", "type": "string", "default": "报名中"},
        {"name": "status_4", "type": "string", "default": "领队"},
        {"name": "status_5", "type": "string", "default": "领队组"},
    ]


def test_compile_rejects_distinct_array_group_outside_array():
    plan = actual_participation_plan()
    plan["metric"]["group_by"] = ["status"]

    with pytest.raises(MQLPlanError, match="group_by field must belong to members"):
        MQLCompiler.compile(plan, [outdoors_resource()])


def test_compile_distinct_documents_groups_by_current_leader_and_deduplicates_activity():
    plan = {
        "collection": "Outdoors",
        "filters": [
            {"field": "status", "operator": "ne", "value": "拟定中"},
            {"field": "status", "operator": "ne", "value": "已取消"},
            {"field": "title.date", "operator": "not_empty", "value": True},
        ],
        "select_fields": [],
        "sort": [],
        "limit": None,
        "metric": {
            "operation": "count_distinct_documents",
            "field": "",
            "group_by": ["leader.personid"],
            "distinct_by": ["_id"],
        },
        "set_comparison": None,
        "assumptions": [],
        "clarification": None,
    }

    result = MQLCompiler.compile(plan, [outdoors_resource()])
    command = json.loads(result["query"])

    assert command["aggregate"] == "Outdoors"
    assert command["pipeline"][1] == {
        "$match": {
            "leader.personid": {"$exists": True, "$nin": [None, ""]},
            "_id": {"$exists": True, "$nin": [None, ""]},
        },
    }
    assert command["pipeline"][2] == {
        "$group": {"_id": {"group_0": "$leader.personid", "distinct_0": "$_id"}},
    }
    assert command["pipeline"][3] == {
        "$group": {"_id": {"group_0": "$_id.group_0"}, "_distinct_count": {"$sum": 1}},
    }
    assert command["pipeline"][4] == {
        "$project": {
            "_id": 0,
            "personid": "$_id.group_0",
            "distinct_document_count": "$_distinct_count",
        },
    }


def test_compile_distinct_document_and_array_elements_unions_leader_and_participants():
    plan = {
        "collection": "Outdoors",
        "filters": [
            {"field": "status", "operator": "ne", "value": "拟定中"},
            {"field": "status", "operator": "ne", "value": "已取消"},
            {"field": "title.date", "operator": "not_empty", "value": True},
        ],
        "select_fields": [],
        "sort": [],
        "limit": None,
        "metric": {
            "operation": "count_distinct_document_and_array_elements",
            "field": "members",
            "group_by": ["members.personid"],
            "document_group_by": ["leader.personid"],
            "distinct_by": ["_id"],
            "element_filters": [{
                "field": "members.entryInfo.status",
                "operator": "in",
                "value": ["报名中", "领队", "领队组"],
            }],
        },
        "set_comparison": None,
        "assumptions": [],
        "clarification": None,
    }

    result = MQLCompiler.compile(plan, [outdoors_resource()])
    command = json.loads(result["query"])
    pipeline = command["pipeline"]

    assert command["aggregate"] == "Outdoors"
    assert {"$unwind": "$members"} in pipeline
    union_stage = next(stage["$unionWith"] for stage in pipeline if "$unionWith" in stage)
    assert union_stage["coll"] == "Outdoors"
    assert union_stage["pipeline"][-1]["$project"] == {
        "_metric_group": "$leader.personid",
        "_metric_distinct": "$_id",
    }
    assert pipeline[-2] == {
        "$group": {"_id": "$_id.group", "_distinct_count": {"$sum": 1}},
    }
    assert pipeline[-1]["$project"]["distinct_document_and_array_count"] == "$_distinct_count"


def test_compile_directional_overlap_rate_from_activity_members_returns_both_directions():
    result = MQLCompiler.compile(outdoor_directional_overlap_plan(), [outdoors_resource()])
    command = json.loads(result["query"])
    pipeline_text = json.dumps(command["pipeline"], ensure_ascii=False)

    assert command["aggregate"] == "Outdoors"
    assert {"$unwind": "$members"} in command["pipeline"]
    assert any("$facet" in stage for stage in command["pipeline"])
    assert '"$setIntersection"' in pipeline_text
    assert '"overlap_rate_from_left"' in pipeline_text
    assert '"overlap_rate_from_right"' in pipeline_text
    assert '"$eq": ["$left_count", 0]' in pipeline_text
    assert '"$eq": ["$right_count", 0]' in pipeline_text
    assert result["query_parameters"] == [
        {"name": "status", "type": "string", "default": "拟定中"},
        {"name": "status_2", "type": "string", "default": "已取消"},
        {"name": "status_3", "type": "string", "default": "报名中"},
        {"name": "status_4", "type": "string", "default": "领队"},
        {"name": "status_5", "type": "string", "default": "领队组"},
        {"name": "entity_1", "type": "string", "default": "A"},
        {"name": "entity_2", "type": "string", "default": "B"},
    ]


def test_directional_overlap_rate_rejects_non_member_entity_field():
    plan = outdoor_directional_overlap_plan()
    plan["metric"]["entity_field"] = "leader.personid"
    with pytest.raises(MQLPlanError, match="entity_field must belong to members"):
        MQLCompiler.compile(plan, [outdoors_resource()])


def test_compile_rejects_distinct_documents_object_group():
    plan = {
        "collection": "Outdoors",
        "filters": [],
        "select_fields": [],
        "sort": [],
        "limit": None,
        "metric": {
            "operation": "count_distinct_documents",
            "field": "",
            "group_by": ["members"],
            "distinct_by": ["_id"],
        },
        "set_comparison": None,
        "assumptions": [],
        "clarification": None,
    }

    with pytest.raises(MQLPlanError, match="field must be scalar"):
        MQLCompiler.compile(plan, [outdoors_resource()])


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

    with pytest.raises(QueryClarificationRequired, match="未经确认的假设") as captured:
        MQLCompiler.compile(plan, [persons_resource()])

    assert captured.value.clarification.key == "query.assumptions"
    assert captured.value.clarification.control == "text"


def test_any_unclosed_calculation_rule_becomes_generic_text_clarification():
    plan = participation_plan()
    plan["clarification"] = "请明确增长率使用哪一个基期和分母"

    with pytest.raises(QueryClarificationRequired, match="增长率") as captured:
        MQLCompiler.compile(plan, [persons_resource()])

    clarification = captured.value.clarification
    assert clarification.key == "query.semantic_details"
    assert clarification.category == "semantic_ambiguity"
    assert clarification.control == "text"


def test_compile_rejects_array_child_as_owner_filter():
    plan = participation_plan()
    plan["filters"][0] = {"field": "entriedOutdoors.title", "operator": "eq", "value": "攀爬"}

    with pytest.raises(MQLPlanError, match="owning record"):
        MQLCompiler.compile(plan, [persons_resource()])


def test_compile_rejects_unknown_schema_coverage():
    resource = persons_resource()
    resource["schema_coverage"] = "unknown"

    with pytest.raises(QueryClarificationRequired, match="先扫描元数据"):
        MQLCompiler.compile(participation_plan(), [resource])


def test_set_overlap_requires_metric_clarification():
    with pytest.raises(QueryClarificationRequired, match="多种计算规则") as captured:
        MQLCompiler.compile(overlap_plan("unspecified"), [persons_resource()])

    clarification = captured.value.clarification
    assert clarification.key == "set_comparison.metric"
    assert clarification.category == "calculation_rule"
    assert clarification.control == "single_choice"
    assert [option.value for option in clarification.options] == [
        "intersection_count",
        "jaccard",
        "overlap_coefficient",
    ]


def test_compile_applies_confirmed_metric_as_deterministic_constraint():
    result = MQLCompiler.compile(
        overlap_plan("unspecified"),
        [persons_resource()],
        clarification_answers={"set_comparison.metric": "jaccard"},
    )

    pipeline_text = json.dumps(json.loads(result["query"])["pipeline"], ensure_ascii=False)
    assert "$setIntersection" in pipeline_text
    assert "$setUnion" in pipeline_text
    assert "$divide" in pipeline_text


def test_compile_rejects_answer_outside_declared_calculation_rules():
    with pytest.raises(MQLPlanError, match="clarification answer is unsupported"):
        MQLCompiler.compile(
            overlap_plan("unspecified"),
            [persons_resource()],
            clarification_answers={"set_comparison.metric": "cosine"},
        )


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


def test_compile_set_overlap_excludes_null_missing_and_empty_identity_values():
    result = MQLCompiler.compile(overlap_plan("overlap_coefficient"), [persons_resource()])
    command = json.loads(result["query"])
    pipeline_text = json.dumps(command["pipeline"], ensure_ascii=False)

    assert '"$filter"' in pipeline_text
    assert '"$type"' in pipeline_text
    assert '"string"' in pipeline_text
    assert '"$ne": ["$$value", ""]' in pipeline_text


def test_compile_set_overlap_uses_one_shared_entity_pipeline():
    result = MQLCompiler.compile(overlap_plan("overlap_coefficient"), [persons_resource()])
    command = json.loads(result["query"])

    assert command["pipeline"][0] == {
        "$match": {
            "userInfo.nickName": {
                "$in": [
                    {"$param": "entity_1"},
                    {"$param": "entity_2"},
                ],
            },
        },
    }
    pipeline_text = json.dumps(command["pipeline"], ensure_ascii=False)
    assert '"$facet"' not in pipeline_text
    assert pipeline_text.count('"$myOutdoors"') == 1
    assert pipeline_text.count('"$entriedOutdoors"') == 1


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


def test_generate_mql_compiles_distinct_array_metric_without_model_generated_pipeline(monkeypatch):
    captured = []

    class FakeLLM:
        async def ainvoke(self, messages, **_kwargs):
            captured.append(messages)
            return type("Response", (), {"content": json.dumps(actual_participation_plan(), ensure_ascii=False)})()

    monkeypatch.setattr(
        "services.query_service.CopilotInferenceService.chat_model",
        lambda *_args, **_kwargs: FakeLLM(),
    )
    result = asyncio.run(QueryService().generate(
        query="按人员统计有效活动中的实际参加活动数",
        engine={"id": 11, "engine_type": "mongodb", "capabilities": {}},
        query_language="mql",
        resources=[outdoors_resource()],
        current_query=None,
        tenant_id=1,
        db=None,
    ))

    command = json.loads(result["query"])
    assert len(captured) == 1
    assert command["aggregate"] == "Outdoors"
    assert "$unwind" in json.dumps(command["pipeline"], ensure_ascii=False)
    assert "$setUnion" not in json.dumps(command["pipeline"], ensure_ascii=False)
    assert "members_count_distinct_array_elements" in result["query"]


def test_query_service_continues_with_confirmed_generic_calculation_rule(monkeypatch):
    class FakeLLM:
        async def ainvoke(self, _messages, **_kwargs):
            return type("Response", (), {"content": json.dumps(overlap_plan("unspecified"), ensure_ascii=False)})()

    monkeypatch.setattr(
        "services.query_service.CopilotInferenceService.chat_model",
        lambda *_args, **_kwargs: FakeLLM(),
    )
    result = asyncio.run(QueryService().generate(
        query="查询攀爬和神采发起或者参加的活动重叠度",
        engine={"id": 11, "engine_type": "mongodb", "capabilities": {}},
        query_language="mql",
        resources=[persons_resource()],
        current_query=None,
        tenant_id=1,
        db=None,
        clarification_answers={"set_comparison.metric": "jaccard"},
    ))

    command = json.loads(result["query"])
    pipeline_text = json.dumps(command["pipeline"], ensure_ascii=False)
    assert command["aggregate"] == "Persons"
    assert "$setIntersection" in pipeline_text
    assert "$setUnion" in pipeline_text
    assert "$divide" in pipeline_text
    assert result["query_parameters"] == [
        {"name": "entity_1", "type": "string", "default": "攀爬"},
        {"name": "entity_2", "type": "string", "default": "神采"},
    ]
