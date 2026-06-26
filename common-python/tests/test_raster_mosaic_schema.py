import pytest

from addp_common.raster_mosaic import (
    DEFAULT_OVERVIEW_REF,
    MANIFEST_SCHEMA_VERSION,
    SOURCE_INDEX_REF,
    SOURCE_INDEX_SCHEMA_VERSION,
    RasterMosaicRefs,
    RasterMosaicSummary,
    build_manifest,
    build_source_index,
    validate_manifest,
)


def test_build_manifest_uses_canonical_identity():
    manifest = build_manifest(
        dataset_name="srtm",
        generated_at="2026-06-26T00:00:00Z",
        refs=RasterMosaicRefs(index=SOURCE_INDEX_REF, overview=DEFAULT_OVERVIEW_REF),
        summary=RasterMosaicSummary(leaf_count=2, source_count=2, extent=[0, 1, 2, 3], source_crs="EPSG:4326"),
        capabilities={"leaf_cog": True},
    )

    assert manifest["schema_version"] == MANIFEST_SCHEMA_VERSION
    assert manifest["data_type"] == "media"
    assert manifest["format"] == "raster_mosaic"
    assert manifest["layout"] == "whole"
    assert manifest["refs"]["index"] == SOURCE_INDEX_REF
    assert manifest["refs"]["overview"] == DEFAULT_OVERVIEW_REF
    assert manifest["summary"]["leaf_count"] == 2


def test_build_source_index_uses_canonical_schema():
    index = build_source_index("2026-06-26T00:00:00Z", [{"path": "leaf/a.cog.tif"}])

    assert index["schema_version"] == SOURCE_INDEX_SCHEMA_VERSION
    assert index["leaf_count"] == 1
    assert index["leaves"][0]["path"] == "leaf/a.cog.tif"


def test_validate_manifest_rejects_non_mosaic_json():
    with pytest.raises(ValueError):
        validate_manifest({"schema_version": "not-addp", "format": "json"})

