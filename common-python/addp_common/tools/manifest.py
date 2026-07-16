import json
from functools import lru_cache
from importlib.resources import files
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator


class ToolDefinition(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    version: str
    description: str
    owner: str
    risk: Literal["read", "propose", "write"]
    approval: dict[str, Any]
    auth: Literal["user_jwt"]
    permission_enforced_by: Literal["owner"]
    audit: dict[str, Any]
    result_ref: dict[str, Any]
    limits: dict[str, int]
    errors: list[str]
    input_schema: dict[str, Any]
    output_schema: dict[str, Any]


class ToolManifest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_id: Literal["addp.tool-manifest/v1"] = Field(alias="schema")
    version: str
    tools: list[ToolDefinition]

    @model_validator(mode="after")
    def validate_unique_names(self):
        names = [tool.name for tool in self.tools]
        if len(names) != len(set(names)):
            raise ValueError("tool manifest contains duplicate names")
        return self


@lru_cache(maxsize=1)
def load_manifest() -> ToolManifest:
    manifest_path = files("addp_common.tools").joinpath("manifest.json")
    return ToolManifest.model_validate(json.loads(manifest_path.read_text(encoding="utf-8")))


def get_tool(name: str) -> ToolDefinition:
    for tool in load_manifest().tools:
        if tool.name == name:
            return tool
    raise KeyError(name)
