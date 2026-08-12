"""Stable, owner-verified resource facts exchanged with AI capabilities."""

from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field


class ResourceFact(BaseModel):
    """A bounded projection of a resource confirmed by its owner."""

    model_config = ConfigDict(extra="forbid")

    role: str = Field(min_length=1, description="资源在本次任务中的稳定用途")
    locator: str = Field(min_length=1, description="已验证的 ADDP ResourceLocator")
    engine_id: int | None = Field(default=None, ge=1, description="资源所属引擎实例 ID")
    source_engine_type: str | None = Field(default=None, description="资源来源引擎类型，由 Owner 校验")
    full_name: str | None = Field(default=None, description="资源在来源引擎内的规范名称")
    query_names: dict[str, str] = Field(
        default_factory=dict,
        description="按查询语言声明的原生资源名称，例如 sql=public.users、mql=users",
    )
    schema_coverage: Literal["complete", "sampled", "unknown"] | None = Field(
        default=None,
        description="字段事实覆盖范围：complete、sampled 或 unknown",
    )
    data_type: str | None = Field(default=None, description="资源数据类型")
    geometry_column: str | None = Field(default=None, description="已验证的几何列")
    geometry_type: str | None = Field(default=None, description="已验证的几何类型")
    crs: str | None = Field(default=None, description="已验证的 CRS")
    fields: list[dict[str, Any]] = Field(default_factory=list, max_length=200, description="受限字段事实")
