import importlib.util
from pathlib import Path
from types import SimpleNamespace

BACKEND_DIR = Path(__file__).resolve().parents[1]


def load_module(name: str, relative_path: str):
    spec = importlib.util.spec_from_file_location(name, BACKEND_DIR / relative_path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


data_source_candidates = load_module("data_source_candidates_module", "pipelines/data_source_candidates.py")

build_data_source_candidates = data_source_candidates.build_data_source_candidates
metadata_search_query = data_source_candidates.metadata_search_query
metadata_type_filter = data_source_candidates.metadata_type_filter


def test_metadata_search_query_prefers_specific_resource_name():
    analysis = SimpleNamespace(table_name="roads", file_path=None, bucket_name=None)

    assert metadata_search_query("缓冲 roads 表", analysis) == "roads"
    assert metadata_type_filter(analysis) == "table"


def test_build_data_source_candidates_maps_schema_to_namespace_contract():
    candidates = build_data_source_candidates(
        metadata_results=[{
            "name": "roads",
            "type": "table",
            "engine_id": 12,
            "schema": "public",
            "score": 0.76,
        }],
        engines=[{
            "id": 12,
            "name": "城市 PostgreSQL",
            "type": "postgresql",
        }],
    )

    payload = [candidate.model_dump(exclude_none=True) for candidate in candidates]

    assert payload == [{
        "engine_id": 12,
        "engine_name": "城市 PostgreSQL",
        "engine_type": "postgresql",
        "location": {
            "namespace": "public",
            "table": "roads",
            "locator": "addp://engine/12/path/public/roads?type=table",
            "target_parent_locator": "addp://engine/12/path/public?type=schema",
        },
        "confidence": 0.76,
        "reason": "元数据搜索匹配",
    }]
    assert "schema" not in payload[0]["location"]


def test_build_data_source_candidates_maps_object_storage_location():
    candidates = build_data_source_candidates(
        metadata_results=[{
            "name": "roads.parquet",
            "type": "object",
            "engine_id": 8,
            "metadata": {"bucket": "gis", "path": "roads/roads.parquet"},
        }],
        engines=[{
            "id": 8,
            "name": "对象存储",
            "type": "minio",
        }],
    )

    payload = candidates[0].model_dump(exclude_none=True)

    assert payload["location"] == {
        "bucket": "gis",
        "path": "roads/roads.parquet",
        "locator": "addp://engine/8/path/gis/roads/roads.parquet?type=object",
        "target_parent_locator": "addp://engine/8/path/gis?type=bucket",
    }


def test_build_data_source_candidates_skips_results_without_engine_context():
    candidates = build_data_source_candidates(
        metadata_results=[{
            "name": "roads",
            "type": "table",
            "engine_id": 12,
        }],
        engines=[],
    )

    assert candidates == []
