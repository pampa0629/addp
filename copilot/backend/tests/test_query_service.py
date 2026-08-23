import asyncio

import pytest

from langchain_core.messages import HumanMessage, SystemMessage

from services.query_clarification import QueryClarificationRequired
from services.query_service import QueryService


@pytest.mark.parametrize("prefix", ["collections", 'collections":["'])
def test_decode_object_recovers_single_schema_object_from_model_prefix(prefix):
    result = QueryService._decode_object(
        prefix
        + '{"collections":["Persons"],"field_paths":["entriedOutdoors"],'
        '"operations":["filter","count"],"result_keys":["count"],"assumptions":[]}'
    )

    assert result["collections"] == ["Persons"]
    assert result["operations"] == ["filter", "count"]


def test_decode_object_rejects_ambiguous_multiple_objects():
    with pytest.raises(ValueError, match="exactly one JSON object"):
        QueryService._decode_object('{"query":"SELECT 1"} {"query":"SELECT 2"}')


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
        '{"query":{"find":"Persons","filter":{},"limit":10},"warnings":[]}',
        query_language="mql",
    )

    assert result["query"] == '{"find":"Persons","filter":{},"limit":10}'
    assert result["query_parameters"] == []


def test_parse_output_preserves_query_parameter_definitions():
    result = QueryService._parse_output(
        '{"query":{"find":"Persons","filter":{"userInfo.nickName":{"$param":"nickname"}}},'
        '"query_parameters":[{"name":"nickname","type":"string","default":"PiPi"}],"warnings":[]}',
        query_language="mql",
    )

    assert result["query_parameters"] == [{
        "name": "nickname",
        "type": "string",
        "default": "PiPi",
    }]


def test_sql_parameter_references_ignore_postgresql_casts():
    assert QueryService._query_parameter_references(
        "sql",
        "SELECT created_at::date FROM events WHERE status = :status",
    ) == {"status"}


def test_parse_output_rejects_mongo_shell_mql():
    with pytest.raises(ValueError, match="JSON command object"):
        QueryService._parse_output(
            '{"query":"db.getSiblingDB(\\"Outdoor\\").Persons.find({})","warnings":[]}',
            query_language="mql",
        )


def test_generate_uses_federated_sql_query_name(monkeypatch):
    captured = []

    class FakeLLM:
        responses = [
            '{"collections":["source_pg.analytics.users"],"field_paths":["id"],'
            '"operations":["list"],"result_keys":[],"assumptions":[]}',
            '{"query":"SELECT id FROM source_pg.analytics.users","query_parameters":[],"explanation":"","warnings":[]}',
        ]

        async def ainvoke(self, messages, **_kwargs):
            captured.append(messages)
            return type("Response", (), {"content": self.responses.pop(0)})()

    monkeypatch.setattr(
        "services.query_service.CopilotInferenceService.chat_model",
        lambda *_args, **_kwargs: FakeLLM(),
    )
    engine = {
        "id": 99,
        "engine_type": "duckdb",
        "capabilities": {"compute": {"query": {"federation": {"supported": True}}}},
    }
    resources = [{
        "query_names": {"sql": "analytics.users", "federated_sql": "source_pg.analytics.users"},
        "fields": [{"name": "id"}],
    }]

    result = asyncio.run(QueryService().generate(
        query="查询用户", engine=engine, query_language="sql", resources=resources,
        current_query=None, tenant_id=1, db=None,
    ))

    assert result["query"] == "SELECT id FROM source_pg.analytics.users"
    assert '"query_name": "source_pg.analytics.users"' in captured[0][1].content


def test_generate_requests_generic_clarification_for_unresolved_sql_assumptions(monkeypatch):
    class FakeLLM:
        async def ainvoke(self, _messages, **_kwargs):
            return type("Response", (), {"content": (
                '{"collections":["analytics.activities"],'
                '"field_paths":["participant_id"],'
                '"operations":["aggregate","ratio"],'
                '"result_keys":["overlap"],'
                '"assumptions":["重叠度的分母应采用并集还是较小集合"]}'
            )})()

    monkeypatch.setattr(
        "services.query_service.CopilotInferenceService.chat_model",
        lambda *_args, **_kwargs: FakeLLM(),
    )
    resources = [{
        "query_names": {"sql": "analytics.activities"},
        "fields": [{"name": "participant_id"}],
    }]

    with pytest.raises(QueryClarificationRequired) as raised:
        asyncio.run(QueryService().generate(
            query="计算两组活动的重叠度",
            engine={"id": 1, "engine_type": "postgresql", "capabilities": {}},
            query_language="sql",
            resources=resources,
            current_query=None,
            tenant_id=1,
            db=None,
        ))

    clarification = raised.value.clarification
    assert clarification.key == "query.assumptions"
    assert clarification.control == "text"
    assert "分母应采用并集还是较小集合" in clarification.prompt


def test_parse_plan_rejects_non_string_array_items():
    with pytest.raises(ValueError, match="operations items must be strings"):
        QueryService._parse_plan(
            '{"collections":["Outdoors"],"field_paths":[],'
            '"operations":[{"op":"filter"}],"result_keys":[],"assumptions":[]}'
        )


def test_validate_candidate_rejects_unverified_mql_collection():
    candidate = {"query": '{"aggregate":"Other","pipeline":[]}', "warnings": []}
    plan = {
        "collections": ["Outdoors"],
        "field_paths": [],
        "operations": ["aggregate"],
        "result_keys": [],
        "assumptions": [],
    }
    errors = QueryService._validate_candidate(candidate, "mql", [{
        "locator": "addp://engine/11/path/Outdoor/Outdoors?type=collection&item_id=51659",
        "query_names": {"mql": "Outdoors"},
        "fields": [],
    }], plan)

    assert "MQL collection is not verified: Other" in errors
    assert "MQL does not cover planned collection: Outdoors" in errors


def test_validate_candidate_rejects_sql_write_and_unverified_table():
    candidate = {"query": "DROP TABLE analytics.users", "query_parameters": []}
    resources = [{
        "query_names": {"sql": "public.users"},
        "fields": [{"name": "id"}],
    }]
    plan = {"collections": ["public.users"], "field_paths": [], "operations": ["list"], "result_keys": [], "assumptions": []}

    errors = QueryService._validate_candidate(candidate, "sql", resources, plan)

    assert "SQL must be a read-only query" in errors
    assert "SQL table is not verified: analytics.users" in errors


def test_validate_candidate_accepts_schema_qualified_sql_fact():
    candidate = {"query": "SELECT id FROM analytics.users", "query_parameters": []}
    resources = [{
        "query_names": {"sql": "analytics.users"},
        "fields": [{"name": "id"}],
    }]
    plan = {"collections": ["analytics.users"], "field_paths": ["id"], "operations": ["list"], "result_keys": [], "assumptions": []}

    assert QueryService._validate_candidate(candidate, "sql", resources, plan) == []


def test_validate_candidate_normalizes_quoted_sql_table_and_cte():
    candidate = {
        "query": 'WITH selected AS (SELECT id FROM "analytics"."users") SELECT id FROM selected',
        "query_parameters": [],
    }
    resources = [{"query_names": {"sql": "analytics.users"}, "fields": [{"name": "id"}]}]
    plan = {"collections": ["analytics.users"], "field_paths": ["id"], "operations": ["list"], "result_keys": [], "assumptions": []}

    assert QueryService._validate_candidate(candidate, "sql", resources, plan) == []


def test_validate_candidate_rejects_cypher_write_and_unverified_property():
    candidate = {"query": "MATCH (n:Person) SET n.password = 'x' RETURN n.password", "query_parameters": []}
    resources = [{"query_names": {"cypher": "Person"}, "fields": [{"name": "nickname"}]}]
    plan = {"collections": ["Person"], "field_paths": [], "operations": ["list"], "result_keys": [], "assumptions": []}

    errors = QueryService._validate_candidate(candidate, "cypher", resources, plan)

    assert "Cypher contains a write operation" in errors
    assert "Cypher field is not verified: password" in errors


def test_validate_candidate_rejects_unregistered_query_language():
    errors = QueryService._validate_candidate(
        {"query": "anything", "query_parameters": []},
        "promql",
        [{"query_names": {"promql": "metric"}, "fields": []}],
        {"collections": ["metric"], "field_paths": [], "operations": ["list"], "result_keys": [], "assumptions": []},
    )

    assert errors == ["query language is unsupported: promql"]


def test_validate_plan_rejects_unsupported_operation():
    plan = {
        "collections": ["Outdoors"],
        "field_paths": ["title.whole"],
        "operations": ["filter", "compare_semantically"],
        "result_keys": ["activities"],
        "assumptions": [],
    }
    resources = [{
        "locator": "addp://engine/11/path/Outdoor/Outdoors?type=collection&item_id=51659",
        "query_names": {"mql": "Outdoors"},
        "fields": [{"name": "title.whole", "path": ["title", "whole"]}],
    }]

    errors = QueryService._validate_plan(plan, resources)

    assert "plan operation is not supported: compare_semantically" in errors


def test_validate_plan_requires_fields_for_field_dependent_operations():
    plan = {
        "collections": ["Outdoors"],
        "field_paths": [],
        "operations": ["filter", "project"],
        "result_keys": ["activities"],
        "assumptions": [],
    }
    resources = [{
        "locator": "addp://engine/11/path/Outdoor/Outdoors?type=collection&item_id=51659",
        "fields": [{"name": "members.userInfo.nickName"}],
    }]

    errors = QueryService._validate_plan(plan, resources)

    assert "plan field_paths must not be empty for field-dependent operations" in errors


def test_validate_plan_requires_fields_for_set_ratio_operations():
    plan = {
        "collections": ["Outdoors"],
        "field_paths": [],
        "operations": ["aggregate", "intersection", "ratio"],
        "result_keys": ["activities", "overlap_ratio"],
        "assumptions": ["Jaccard intersection/union"],
    }
    resources = [{
        "locator": "addp://engine/11/path/Outdoor/Outdoors?type=collection&item_id=51659",
        "query_names": {"mql": "Outdoors"},
        "fields": [
            {"name": "members.userInfo.nickName"},
            {"name": "title.whole"},
        ],
    }]

    errors = QueryService._validate_plan(plan, resources)

    assert "plan field_paths must not be empty for field-dependent operations" in errors


def test_validate_candidate_rejects_unverified_mql_field():
    candidate = {
        "query": '{"aggregate":"Outdoors","pipeline":['
        '{"$match":{"members":{"$elemMatch":{"userInfo.displayName":"PiPi"}}}},'
        '{"$project":{"activity":"$title.whole"}}]}',
        "warnings": [],
    }
    plan = {
        "collections": ["Outdoors"],
        "field_paths": ["members.userInfo.nickName", "title.whole"],
        "operations": ["filter", "project", "aggregate"],
        "result_keys": ["activity"],
        "assumptions": [],
    }
    resources = [{
        "locator": "addp://engine/11/path/Outdoor/Outdoors?type=collection&item_id=51659",
        "query_names": {"mql": "Outdoors"},
        "fields": [
            {"name": "members.userInfo.nickName", "path": ["members", "userInfo", "nickName"]},
            {"name": "title.whole", "path": ["title", "whole"]},
        ],
    }]

    errors = QueryService._validate_candidate(candidate, "mql", resources, plan)

    assert "MQL field is not verified: members.userInfo.displayName" in errors


def test_validate_candidate_recognizes_nested_mql_field_as_plan_coverage():
    candidate = {
        "query": '{"aggregate":"Outdoors","pipeline":['
        '{"$match":{"members":{"$elemMatch":{"userInfo.nickName":"PiPi"}}}},'
        '{"$project":{"activity":"$title.whole"}}]}',
        "warnings": [],
    }
    plan = {
        "collections": ["Outdoors"],
        "field_paths": ["members.userInfo.nickName", "title.whole"],
        "operations": ["filter", "project", "aggregate"],
        "result_keys": ["activity"],
        "assumptions": [],
    }
    resources = [{
        "locator": "addp://engine/11/path/Outdoor/Outdoors?type=collection&item_id=51659",
        "query_names": {"mql": "Outdoors"},
        "fields": [
            {"name": "members", "path": ["members"]},
            {"name": "members.userInfo.nickName", "path": ["members", "userInfo", "nickName"]},
            {"name": "title.whole", "path": ["title", "whole"]},
        ],
    }]

    errors = QueryService._validate_candidate(candidate, "mql", resources, plan)

    assert errors == []


def test_validate_candidate_accepts_derived_mql_fields_between_stages():
    candidate = {
        "query": '{"aggregate":"Outdoors","pipeline":['
        '{"$project":{"activity":"$title.whole",'
        '"both":{"$in":["PiPi","$members.userInfo.nickName"]}}},'
        '{"$facet":{"activities":[{"$match":{"both":true}},{"$project":{"activity":1}}],'
        '"summary":[{"$group":{"_id":null,"common":{"$sum":{"$cond":["$both",1,0]}}}},'
        '{"$project":{"overlap_ratio":{"$divide":["$common",1]}}}]}}]}',
        "warnings": [],
    }
    plan = {
        "collections": ["Outdoors"],
        "field_paths": ["members.userInfo.nickName", "title.whole"],
        "operations": ["filter", "project", "group", "aggregate", "ratio"],
        "result_keys": ["activities", "summary"],
        "assumptions": [],
    }
    resources = [{
        "locator": "addp://engine/11/path/Outdoor/Outdoors?type=collection&item_id=51659",
        "query_names": {"mql": "Outdoors"},
        "fields": [
            {"name": "members.userInfo.nickName", "path": ["members", "userInfo", "nickName"]},
            {"name": "title.whole", "path": ["title", "whole"]},
        ],
    }]

    errors = QueryService._validate_candidate(candidate, "mql", resources, plan)

    assert errors == []


def test_validate_candidate_rejects_parameterized_mql_field_name():
    candidate = {
        "query": '{"aggregate":"Outdoors","pipeline":['
        '{"$match":{"$expr":{"$eq":['
        '{"$getField":{"field":{"$param":"nickname_field"},"input":"$$CURRENT"}},'
        '"PiPi"]}}}]}',
        "warnings": [],
    }
    plan = {
        "collections": ["Outdoors"],
        "field_paths": ["members.userInfo.nickName"],
        "operations": ["filter", "aggregate"],
        "result_keys": [],
        "assumptions": [],
    }
    resources = [{
        "locator": "addp://engine/11/path/Outdoor/Outdoors?type=collection&item_id=51659",
        "fields": [{"name": "members.userInfo.nickName"}],
    }]

    errors = QueryService._validate_candidate(candidate, "mql", resources, plan)

    assert "MQL contains a dynamic field name" in errors


def test_validate_candidate_rejects_undefined_query_parameter():
    candidate = {
        "query": '{"find":"Persons","filter":{"userInfo.nickName":{"$param":"nickname"}}}',
        "query_parameters": [],
        "warnings": [],
    }
    plan = {
        "collections": ["Persons"],
        "field_paths": ["userInfo.nickName"],
        "operations": ["filter"],
        "result_keys": [],
        "assumptions": [],
    }
    resources = [{
        "locator": "addp://engine/11/path/Outdoor/Persons?type=collection&item_id=51657",
        "fields": [{"name": "userInfo.nickName"}],
    }]

    errors = QueryService._validate_candidate(candidate, "mql", resources, plan)

    assert "MQL query parameter is undefined: nickname" in errors


def test_validate_candidate_rejects_dynamic_record_key_enumeration():
    candidate = {
        "query": '{"aggregate":"Outdoors","pipeline":[{"$match":{"$expr":{"$in":[{"$param":"nickname"},{"$map":{"input":{"$objectToArray":"$$ROOT"},"as":"entry","in":"$$entry.v"}}]}}}]}',
        "query_parameters": [{"name": "nickname", "type": "string", "default": "PiPi"}],
        "warnings": [],
    }
    plan = {
        "collections": ["Outdoors"],
        "field_paths": ["members.userInfo.nickName"],
        "operations": ["filter"],
        "result_keys": [],
        "assumptions": [],
    }
    resources = [{
        "locator": "addp://engine/11/path/Outdoor/Outdoors?type=collection&item_id=51659",
        "fields": [{"name": "members.userInfo.nickName"}],
    }]

    errors = QueryService._validate_candidate(candidate, "mql", resources, plan)

    assert "MQL contains dynamic record key enumeration" in errors


def test_validate_candidate_rejects_non_positive_mql_limit():
    candidate = {
        "query": '{"aggregate":"Outdoors","pipeline":[{"$facet":{"activities":[{"$limit":0}]}}]}',
        "query_parameters": [],
        "warnings": [],
    }
    plan = {
        "collections": ["Outdoors"],
        "field_paths": [],
        "operations": ["aggregate", "limit"],
        "result_keys": ["activities"],
        "assumptions": [],
    }
    resources = [{
        "locator": "addp://engine/11/path/Outdoor/Outdoors?type=collection&item_id=51659",
        "fields": [],
    }]

    errors = QueryService._validate_candidate(candidate, "mql", resources, plan)

    assert "MQL contains a non-positive limit" in errors
