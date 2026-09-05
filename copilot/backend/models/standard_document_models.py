"""Standard 文档候选提炼的无状态请求与响应模型。"""

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator


class StandardDocumentSection(BaseModel):
    model_config = ConfigDict(extra="forbid")

    section_path: str = Field(min_length=1, max_length=500)
    start_line: int = Field(ge=1)
    end_line: int = Field(ge=1)
    text: str = Field(min_length=1, max_length=200_000)

    @model_validator(mode="after")
    def validate_interval(self):
        if self.end_line < self.start_line:
            raise ValueError("end_line must be greater than or equal to start_line")
        return self


class StandardDocumentExtractRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    document_name: str = Field(min_length=1, max_length=200)
    version_label: str = Field(default="", max_length=50)
    sections: list[StandardDocumentSection] = Field(min_length=1, max_length=200)


class StandardDocumentEvidence(BaseModel):
    model_config = ConfigDict(extra="forbid")

    section_path: str = Field(min_length=1, max_length=500)
    start_line: int = Field(ge=1)
    end_line: int = Field(ge=1)


class StandardDocumentCodeItem(BaseModel):
    model_config = ConfigDict(extra="forbid")

    code: str
    name: str
    definition: str = ""


class StandardDocumentCandidatePayload(BaseModel):
    model_config = ConfigDict(extra="forbid")

    data_type: str | None = None
    value_domain_kind: str | None = None
    unit: str | None = None
    calculation_formula: str | None = None
    statistical_scope: str | None = None
    aggregation: str | None = None
    dimensions: list[str] = Field(default_factory=list)
    items: list[StandardDocumentCodeItem] = Field(default_factory=list)


class StandardDocumentCandidate(BaseModel):
    model_config = ConfigDict(extra="forbid")

    candidate_type: Literal["glossary", "element", "code_set", "metric"]
    code: str = Field(min_length=1, max_length=100, pattern=r"^[a-z][a-z0-9_]*$")
    name: str = Field(min_length=1, max_length=200)
    definition: str = Field(min_length=1, max_length=4000)
    payload: StandardDocumentCandidatePayload = Field(
        default_factory=StandardDocumentCandidatePayload
    )
    evidences: list[StandardDocumentEvidence] = Field(min_length=1, max_length=20)


class StandardDocumentExtractResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    candidates: list[StandardDocumentCandidate] = Field(default_factory=list, max_length=200)
