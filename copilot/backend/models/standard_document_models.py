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

    data_type: Literal[
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
    ] | None = None
    value_domain_kind: Literal["unrestricted", "range", "enumeration"] | None = None
    code_set_code: str | None = Field(
        default=None,
        min_length=1,
        max_length=100,
        pattern=r"^[a-z][a-z0-9_]*$",
    )
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

    @model_validator(mode="after")
    def validate_type_specific_payload(self):
        if self.candidate_type == "code_set" and self.payload.data_type not in {
            None,
            "string",
            "int",
            "bigint",
        }:
            raise ValueError(
                "code_set data_type must be string, int, bigint, or null"
            )
        if (
            self.candidate_type not in {"element", "code_set"}
            and self.payload.data_type is not None
        ):
            raise ValueError(
                "data_type is only valid for element or code_set candidates"
            )
        if (
            self.candidate_type != "element"
            and self.payload.value_domain_kind is not None
        ):
            raise ValueError("value_domain_kind is only valid for element candidates")
        if self.candidate_type != "element" and self.payload.code_set_code is not None:
            raise ValueError("code_set_code is only valid for element candidates")
        if self.candidate_type == "element":
            if (
                self.payload.value_domain_kind == "enumeration"
                and self.payload.code_set_code is None
            ):
                raise ValueError(
                    "enumeration element candidate requires code_set_code"
                )
            if (
                self.payload.value_domain_kind != "enumeration"
                and self.payload.code_set_code is not None
            ):
                raise ValueError(
                    "code_set_code is only valid for enumeration element candidates"
                )
        return self


class StandardDocumentExtractResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    candidates: list[StandardDocumentCandidate] = Field(default_factory=list, max_length=200)

    @model_validator(mode="after")
    def validate_candidate_references(self):
        code_set_counts: dict[str, int] = {}
        for candidate in self.candidates:
            if candidate.candidate_type == "code_set":
                code_set_counts[candidate.code] = code_set_counts.get(candidate.code, 0) + 1
        for candidate in self.candidates:
            code_set_code = candidate.payload.code_set_code
            if code_set_code is not None and code_set_counts.get(code_set_code) != 1:
                raise ValueError(
                    "code_set_code must reference exactly one code_set candidate in the same response"
                )
        return self
