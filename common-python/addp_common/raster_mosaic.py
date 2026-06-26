"""Shared raster mosaic manifest and source index helpers."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any


MANIFEST_FILE_NAME = "mosaic.addp.json"
MANIFEST_SCHEMA_VERSION = "addp.raster_mosaic.v1"
SOURCE_INDEX_SCHEMA_VERSION = "addp.raster_mosaic.source_index.v1"
SOURCE_INDEX_REF = "index/source-index.json"
DEFAULT_OVERVIEW_REF = "overviews/overview.cog.tif"


@dataclass
class RasterMosaicRefs:
    index: str = SOURCE_INDEX_REF
    overview: str = ""

    def to_dict(self) -> dict[str, Any]:
        return {
            "index": self.index,
            "overview": self.overview,
        }


@dataclass
class RasterMosaicSummary:
    leaf_count: int = 0
    source_count: int = 0
    failed_count: int = 0
    extent: list[float] = field(default_factory=list)
    source_crs: str = ""
    vrt_width: int = 0
    vrt_height: int = 0
    overview_width: int = 0
    overview_height: int = 0

    def to_dict(self) -> dict[str, Any]:
        return {
            "leaf_count": self.leaf_count,
            "source_count": self.source_count,
            "failed_count": self.failed_count,
            "extent": self.extent,
            "source_crs": self.source_crs,
            "vrt_width": self.vrt_width,
            "vrt_height": self.vrt_height,
            "overview_width": self.overview_width,
            "overview_height": self.overview_height,
        }


def build_source_index(generated_at: str, leaves: list[dict[str, Any]]) -> dict[str, Any]:
    return {
        "schema_version": SOURCE_INDEX_SCHEMA_VERSION,
        "generated_at": generated_at,
        "leaf_count": len(leaves),
        "leaves": leaves,
    }


def build_manifest(
    *,
    dataset_name: str,
    generated_at: str,
    refs: RasterMosaicRefs,
    summary: RasterMosaicSummary,
    capabilities: dict[str, Any] | None = None,
) -> dict[str, Any]:
    manifest = {
        "schema_version": MANIFEST_SCHEMA_VERSION,
        "data_type": "media",
        "format": "raster_mosaic",
        "layout": "whole",
        "dataset_name": dataset_name,
        "generated_at": generated_at,
        "refs": refs.to_dict(),
        "summary": summary.to_dict(),
        "capabilities": capabilities or {},
    }
    validate_manifest(manifest)
    return manifest


def validate_manifest(manifest: dict[str, Any]) -> None:
    if manifest.get("schema_version") != MANIFEST_SCHEMA_VERSION:
        raise ValueError("raster mosaic manifest schema_version is invalid")
    if manifest.get("data_type") != "media":
        raise ValueError("raster mosaic manifest data_type must be media")
    if manifest.get("format") != "raster_mosaic":
        raise ValueError("raster mosaic manifest format must be raster_mosaic")
    if manifest.get("layout") != "whole":
        raise ValueError("raster mosaic manifest layout must be whole")
    refs = manifest.get("refs")
    if not isinstance(refs, dict) or not str(refs.get("index") or "").strip():
        raise ValueError("raster mosaic manifest refs.index is required")

