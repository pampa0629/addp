import asyncio
from types import SimpleNamespace

import pytest
from pydantic import ValidationError

from models.standard_document_models import (
    StandardDocumentCandidate,
    StandardDocumentEvidence,
    StandardDocumentExtractRequest,
    StandardDocumentExtractResponse,
    StandardDocumentKnownCandidate,
    StandardDocumentSection,
)
from chains.standard_document_extraction_chain import StandardDocumentExtractionChain
from services.standard_document_extraction_service import StandardDocumentExtractionService


class FakeChain:
    async def extract(self, _request):
        return StandardDocumentExtractResponse(
            candidates=[
                StandardDocumentCandidate(
                    candidate_type="metric",
                    code="actual_participation_count",
                    name="实际参加活动数",
                    definition="用户实际参加的户外活动数量。",
                    payload={"aggregation": "count"},
                    evidences=[
                        StandardDocumentEvidence(
                            section_path="指标口径", start_line=31, end_line=32
                        ),
                        StandardDocumentEvidence(
                            section_path="指标口径", start_line=99, end_line=99
                        ),
                    ],
                ),
                StandardDocumentCandidate(
                    candidate_type="glossary",
                    code="invented_term",
                    name="无证据术语",
                    definition="应被丢弃。",
                    evidences=[
                        StandardDocumentEvidence(
                            section_path="不存在", start_line=1, end_line=1
                        )
                    ],
                ),
            ]
        )


class CapturingLLM:
    def __init__(self):
        self.messages = []

    async def ainvoke(self, messages, **_kwargs):
        self.messages = messages
        return SimpleNamespace(content='{"candidates": []}')


def test_service_keeps_only_evidence_inside_authoritative_sections():
    request = StandardDocumentExtractRequest(
        document_name="Outdoor 业务数据治理推进方案",
        code_namespace=None,
        known_candidates=[],
        sections=[
            StandardDocumentSection(
                section_path="指标口径",
                start_line=30,
                end_line=40,
                text="## 指标口径\n实际参加活动数",
            ),
            StandardDocumentSection(
                section_path="指标口径",
                start_line=90,
                end_line=100,
                text="## 指标口径\n活动统计补充说明",
            ),
        ],
    )
    result = asyncio.run(
        StandardDocumentExtractionService(None, chain=FakeChain()).run(request)
    )

    assert len(result.candidates) == 1
    assert result.candidates[0].code == "actual_participation_count"
    assert [(item.start_line, item.end_line) for item in result.candidates[0].evidences] == [
        (31, 32),
        (99, 99),
    ]


def test_request_rejects_known_candidate_outside_domain_namespace():
    with pytest.raises(ValidationError):
        StandardDocumentExtractRequest(
            document_name="Outdoor 业务数据治理推进方案",
            code_namespace="outdoor",
            known_candidates=[
                StandardDocumentKnownCandidate(
                    candidate_type="metric",
                    code="actual_participation_count",
                    name="实际参加活动数",
                    definition="用户实际参加的户外活动数量。",
                )
            ],
            sections=[
                StandardDocumentSection(
                    section_path="指标口径",
                    start_line=30,
                    end_line=40,
                    text="## 指标口径\n实际参加活动数",
                )
            ],
        )


def test_request_requires_explicit_namespace_and_known_candidate_context():
    with pytest.raises(ValidationError):
        StandardDocumentExtractRequest(
            document_name="Outdoor 业务数据治理推进方案",
            sections=[
                StandardDocumentSection(
                    section_path="指标口径",
                    start_line=30,
                    end_line=40,
                    text="## 指标口径\n实际参加活动数",
                )
            ],
        )


def test_service_rejects_generated_candidate_outside_domain_namespace():
    request = StandardDocumentExtractRequest(
        document_name="Outdoor 业务数据治理推进方案",
        code_namespace="outdoor",
        known_candidates=[],
        sections=[
            StandardDocumentSection(
                section_path="指标口径",
                start_line=30,
                end_line=40,
                text="## 指标口径\n实际参加活动数",
            )
        ],
    )

    with pytest.raises(RuntimeError, match="candidate code must belong"):
        asyncio.run(
            StandardDocumentExtractionService(None, chain=FakeChain()).run(request)
        )


def test_extraction_response_schema_is_strict_and_requires_complete_payload_shape():
    response_schema = StandardDocumentExtractionChain._response_schema()
    candidate_schema = response_schema.schema_value["properties"]["candidates"]["items"]

    assert response_schema.strict is True
    assert len(candidate_schema["anyOf"]) == 4
    variants = {
        variant["properties"]["candidate_type"]["enum"][0]: variant
        for variant in candidate_schema["anyOf"]
    }
    assert set(variants) == {"glossary", "element", "code_set", "metric"}
    for variant in variants.values():
        payload_schema = variant["properties"]["payload"]
        assert variant["additionalProperties"] is False
        assert set(variant["required"]) == set(variant["properties"])
        assert payload_schema["additionalProperties"] is False
        assert set(payload_schema["required"]) == set(payload_schema["properties"])

    assert variants["element"]["properties"]["payload"]["properties"]["data_type"] == {
        "anyOf": [
            {
                "type": "string",
                "enum": [
                    "string",
                    "int",
                    "bigint",
                    "float",
                    "decimal",
                    "date",
                    "datetime",
                    "bool",
                    "json",
                    "text",
                ],
            },
            {"type": "null"},
        ]
    }
    assert variants["element"]["properties"]["payload"]["properties"]["value_domain_kind"] == {
        "anyOf": [
            {
                "type": "string",
                "enum": ["unrestricted", "range", "enumeration"],
            },
            {"type": "null"},
        ]
    }
    assert variants["element"]["properties"]["payload"]["properties"]["code_set_code"] == {
        "anyOf": [
            {
                "type": "string",
                "minLength": 1,
                "maxLength": 100,
                "pattern": "^[a-z][a-z0-9_]*$",
            },
            {"type": "null"},
        ]
    }
    assert variants["code_set"]["properties"]["payload"]["properties"]["data_type"] == {
        "anyOf": [
            {"type": "string", "enum": ["string", "int", "bigint"]},
            {"type": "null"},
        ]
    }
    for candidate_type in ("glossary", "metric"):
        payload = variants[candidate_type]["properties"]["payload"]["properties"]
        assert payload["data_type"] == {"type": "null"}
        assert payload["value_domain_kind"] == {"type": "null"}
        assert payload["code_set_code"] == {"type": "null"}
    assert variants["code_set"]["properties"]["payload"]["properties"]["value_domain_kind"] == {
        "type": "null"
    }
    assert variants["code_set"]["properties"]["payload"]["properties"]["code_set_code"] == {
        "type": "null"
    }


def test_extraction_response_schema_applies_domain_namespace_to_every_candidate_variant():
    response_schema = StandardDocumentExtractionChain._response_schema("outdoor")
    variants = response_schema.schema_value["properties"]["candidates"]["items"]["anyOf"]

    for variant in variants:
        assert variant["properties"]["code"] == {
            "type": "string",
            "maxLength": 100,
            "pattern": "^outdoor_[a-z0-9_]+$",
        }


def test_candidate_model_rejects_nonstandard_value_domain_kind():
    with pytest.raises(ValidationError):
        StandardDocumentCandidate(
            candidate_type="element",
            code="outdoor_person_id",
            name="人员标识",
            definition="户外参与人员的稳定标识。",
            payload={"value_domain_kind": "identifier"},
            evidences=[
                StandardDocumentEvidence(
                    section_path="数据元", start_line=10, end_line=10
                )
            ],
        )


def test_candidate_model_rejects_value_domain_kind_on_non_element_candidate():
    with pytest.raises(ValidationError):
        StandardDocumentCandidate(
            candidate_type="metric",
            code="activity_count",
            name="活动次数",
            definition="人员参加活动的次数。",
            payload={"value_domain_kind": "unrestricted"},
            evidences=[
                StandardDocumentEvidence(
                    section_path="指标", start_line=20, end_line=20
                )
            ],
        )


@pytest.mark.parametrize(
    "payload",
    [
        {"value_domain_kind": "enumeration"},
        {"value_domain_kind": "unrestricted", "code_set_code": "outdoor_status"},
        {"value_domain_kind": "range", "code_set_code": "outdoor_status"},
    ],
)
def test_element_candidate_requires_code_set_code_only_for_enumeration(payload):
    with pytest.raises(ValidationError):
        StandardDocumentCandidate(
            candidate_type="element",
            code="outdoor_activity_status",
            name="活动状态",
            definition="户外活动的状态。",
            payload=payload,
            evidences=[
                StandardDocumentEvidence(
                    section_path="数据元", start_line=10, end_line=10
                )
            ],
        )


def test_non_element_candidate_rejects_code_set_code():
    with pytest.raises(ValidationError):
        StandardDocumentCandidate(
            candidate_type="metric",
            code="activity_count",
            name="活动次数",
            definition="人员参加活动的次数。",
            payload={"code_set_code": "outdoor_status"},
            evidences=[
                StandardDocumentEvidence(
                    section_path="指标", start_line=20, end_line=20
                )
            ],
        )


def test_extraction_response_requires_enumeration_reference_to_same_batch_code_set():
    element = StandardDocumentCandidate(
        candidate_type="element",
        code="outdoor_activity_status",
        name="活动状态",
        definition="户外活动的状态。",
        payload={
            "data_type": "string",
            "value_domain_kind": "enumeration",
            "code_set_code": "outdoor_activity_status_codes",
        },
        evidences=[
            StandardDocumentEvidence(
                section_path="数据元", start_line=10, end_line=10
            )
        ],
    )

    with pytest.raises(ValidationError):
        StandardDocumentExtractResponse(candidates=[element])

    code_set = StandardDocumentCandidate(
        candidate_type="code_set",
        code="outdoor_activity_status_codes",
        name="活动状态码值集",
        definition="户外活动允许使用的状态。",
        payload={"data_type": "string"},
        evidences=[
            StandardDocumentEvidence(
                section_path="码值集", start_line=20, end_line=25
            )
        ],
    )
    response = StandardDocumentExtractResponse(candidates=[element, code_set])
    assert response.candidates[0].payload.code_set_code == code_set.code

    with pytest.raises(ValidationError):
        StandardDocumentExtractResponse(
            candidates=[element, code_set, code_set.model_copy()]
        )


@pytest.mark.parametrize(
    ("candidate_type", "data_type"),
    [
        ("element", "date_or_datetime"),
        ("code_set", "decimal"),
        ("metric", "integer"),
        ("glossary", "string"),
    ],
)
def test_candidate_model_rejects_noncanonical_or_inapplicable_data_type(
    candidate_type, data_type
):
    with pytest.raises(ValidationError):
        StandardDocumentCandidate(
            candidate_type=candidate_type,
            code="outdoor_candidate",
            name="户外候选",
            definition="户外业务候选定义。",
            payload={"data_type": data_type},
            evidences=[
                StandardDocumentEvidence(
                    section_path="候选", start_line=20, end_line=20
                )
            ],
        )


@pytest.mark.parametrize(
    ("candidate_type", "data_type"),
    [("element", "decimal"), ("code_set", "bigint")],
)
def test_candidate_model_accepts_type_specific_canonical_data_type(
    candidate_type, data_type
):
    candidate = StandardDocumentCandidate(
        candidate_type=candidate_type,
        code="outdoor_candidate",
        name="户外候选",
        definition="户外业务候选定义。",
        payload={"data_type": data_type},
        evidences=[
            StandardDocumentEvidence(
                section_path="候选", start_line=20, end_line=20
            )
        ],
    )

    assert candidate.payload.data_type == data_type


def test_extraction_prompt_separates_data_type_value_domain_and_business_semantics():
    llm = CapturingLLM()
    request = StandardDocumentExtractRequest(
        document_name="Outdoor 业务数据治理推进方案",
        code_namespace=None,
        known_candidates=[],
        sections=[
            StandardDocumentSection(
                section_path="数据元",
                start_line=10,
                end_line=10,
                text="L10: 人员标识是户外参与人员的稳定标识。",
            )
        ],
    )

    asyncio.run(StandardDocumentExtractionChain(llm).extract(request))

    prompt = llm.messages[1].content
    assert "data_type 只允许 string、int、bigint、float、decimal、date、datetime、bool、json、text" in prompt
    assert "码值集的 data_type 只允许 string、int、bigint" in prompt
    assert "业务术语和指标的 data_type 必须为 null" in prompt
    assert "date_or_datetime 等上位类型" in prompt
    assert "value_domain_kind 只允许 unrestricted、range、enumeration" in prompt
    assert "枚举数据元的 code_set_code 必须引用同一响应中的码值集候选" in prompt
    assert "identifier 是业务语义" in prompt
    assert "numeric、date_or_datetime 不是值域类型" in prompt


def test_extraction_prompt_requires_domain_prefix_and_known_code_reuse():
    llm = CapturingLLM()
    request = StandardDocumentExtractRequest(
        document_name="Outdoor 业务数据治理推进方案",
        code_namespace="outdoor",
        known_candidates=[
            StandardDocumentKnownCandidate(
                candidate_type="metric",
                code="outdoor_participation_count",
                name="实际参加活动数",
                definition="用户实际参加的户外活动数量。",
            )
        ],
        sections=[
            StandardDocumentSection(
                section_path="指标口径",
                start_line=30,
                end_line=31,
                text="L30: ## 指标口径\nL31: 实际参加活动数",
            )
        ],
    )

    asyncio.run(StandardDocumentExtractionChain(llm).extract(request))

    prompt = llm.messages[1].content
    assert "候选编码命名空间：outdoor" in prompt
    assert "outdoor_participation_count" in prompt
    assert "所有候选 code 必须以 `outdoor_` 开头" in prompt
    assert "相同时必须逐字复用既有 code" in prompt
