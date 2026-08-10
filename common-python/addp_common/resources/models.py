"""Stable, owner-verified resource facts exchanged with AI capabilities."""

from typing import Any

from pydantic import BaseModel, ConfigDict, Field


class ResourceFact(BaseModel):
    """A bounded projection of a resource confirmed by its owner."""

    model_config = ConfigDict(extra="forbid")

    role: str = Field(min_length=1, description="资源在本次任务中的稳定用途")
    locator: str = Field(min_length=1, description="已验证的 ADDP ResourceLocator")
    engine_id: int | None = Field(default=None, ge=1, description="资源所属引擎实例 ID")
    data_type: str | None = Field(default=None, description="资源数据类型")
    geometry_column: str | None = Field(default=None, description="已验证的几何列")
    geometry_type: str | None = Field(default=None, description="已验证的几何类型")
    crs: str | None = Field(default=None, description="已验证的 CRS")
    fields: list[dict[str, Any]] = Field(default_factory=list, max_length=200, description="受限字段事实")
