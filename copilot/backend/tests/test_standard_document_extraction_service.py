import asyncio

from models.standard_document_models import (
    StandardDocumentCandidate,
    StandardDocumentEvidence,
    StandardDocumentExtractRequest,
    StandardDocumentExtractResponse,
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


def test_service_keeps_only_evidence_inside_authoritative_sections():
    request = StandardDocumentExtractRequest(
        document_name="Outdoor 业务数据治理推进方案",
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


def test_extraction_response_schema_is_strict_and_requires_complete_payload_shape():
    response_schema = StandardDocumentExtractionChain._response_schema()
    candidate_schema = response_schema.schema_value["properties"]["candidates"]["items"]
    payload_schema = candidate_schema["properties"]["payload"]

    assert response_schema.strict is True
    assert payload_schema["additionalProperties"] is False
    assert set(payload_schema["required"]) == set(payload_schema["properties"])
