from addp_common.tools import preview_resource_fact


def test_preview_resource_fact_normalizes_manager_table_preview():
    locator = "addp://engine/8/path/public/railway?type=table&item_id=60"

    fact = preview_resource_fact({
        "preview_type": "table",
        "metadata": {
            "locator": locator,
            "engine_name": "spatial",
            "full_name": "public.railway",
        },
        "data": {
            "column_metadata": [
                {"column_name": "id", "type": "bigint", "nullable": False},
                {"column_name": "shape", "type": "geometry(LineString,32650)", "nullable": True},
            ],
            "geometry_columns": ["shape"],
            "geometry_column": "shape",
            "source_srid": 32650,
            "source_crs": "EPSG:32650",
            "rows": [{"id": 1, "shape": "must-not-leak"}],
        },
    })

    assert fact == {
        "locator": locator,
        "preview_type": "table",
        "engine_name": "spatial",
        "full_name": "public.railway",
        "geometry_columns": ["shape"],
        "geometry_column": "shape",
        "geometry_type": "LineString",
        "source_srid": 32650,
        "source_crs": "EPSG:32650",
        "fields": [
            {"name": "id", "type": "bigint", "nullable": False},
            {"name": "shape", "type": "geometry(LineString,32650)", "nullable": True},
        ],
    }


def test_preview_resource_fact_requires_canonical_locator():
    assert preview_resource_fact({"preview_type": "table", "data": {"columns": ["id"]}}) is None
