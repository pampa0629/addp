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
            "item_meta": {
                "item_type": "table",
                "attributes": [
                    {"key": "item", "value": {"data_type": "table", "layout": "single"}},
                ],
            },
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
        "data_type": "table",
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


def test_preview_resource_fact_keeps_native_item_type_separate_from_data_type():
    locator = "addp://engine/11/path/Outdoor/Persons?type=collection&item_id=51657"

    fact = preview_resource_fact({
        "preview_type": "table",
        "metadata": {"locator": locator},
        "data": {
            "item_meta": {
                "item_type": "collection",
                "attributes": [
                    {"key": "item", "value": {"data_type": "table"}},
                ],
            },
            "column_metadata": [
                {"column_name": "_id", "path": ["_id"], "type": "string"},
                {
                    "column_name": "members.userInfo.nickName",
                    "path": ["members", "userInfo", "nickName"],
                    "type": "string",
                },
            ],
        },
    })

    assert fact["data_type"] == "table"
    assert fact["fields"][1] == {
        "name": "members.userInfo.nickName",
        "path": ["members", "userInfo", "nickName"],
        "type": "string",
    }
    assert "item_type" not in fact


def test_preview_resource_fact_requires_canonical_locator():
    assert preview_resource_fact({"preview_type": "table", "data": {"columns": ["id"]}}) is None
