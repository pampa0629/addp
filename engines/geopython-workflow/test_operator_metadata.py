from pathlib import Path
import sys
import tempfile
import base64
import hashlib
import json

for parent in Path(__file__).resolve().parents:
    contract_path = parent / "docs" / "workflow_operator_contract.py"
    if contract_path.exists():
        sys.path.insert(0, str(contract_path.parent))
        break
for parent in Path(__file__).resolve().parents:
    common_python = parent / "common-python"
    if common_python.exists():
        sys.path.insert(0, str(common_python))
        break

from workflow_operator_contract import assert_operator_metadata_contract
from operators import list_operators
import operators.raster_operators as raster_operators
from operators.vector_tile_operators import (
    _build_quick_view_ogr_sql,
    _extent_to_wgs84,
    _extract_gdal_progress_values,
    _index_generated_tiles,
    _normalize_extent,
    _run_command_with_gdal_progress,
    vector_to_pmtiles,
)
from operators.spatial_transform_operators import crs_to_projjson, vector_reproject
from operators.raster_operators import _authority_code_from_wkt, _translate_to_cog, _write_json, build_raster_mosaic, tiff_to_cog
import pyarrow as pa
from geometry_batches import decode_geometry_batch_arrow, encode_geometry_batch_arrow

import geopandas as gpd
import pytest
import shapely
from pyproj import CRS
from operators.io_operators import load, save


def test_progress_reporter_uses_tenant_service_access_token(monkeypatch):
    calls = []

    class FakeTokenSource:
        def __init__(self, system_url, client_id, client_secret):
            assert system_url == "http://system:8180"
            assert client_id == "addp-geopython"
            assert client_secret == "geopython-secret"

        def token(self, tenant_id):
            assert tenant_id == 7
            return "addp_at_geopython"

        def invalidate(self, tenant_id, token):
            raise AssertionError("successful request must not invalidate token")

    class FakeResponse:
        status_code = 202

    def fake_post(endpoint, json, headers, timeout):
        calls.append((endpoint, json, headers, timeout))
        return FakeResponse()

    monkeypatch.setattr(raster_operators, "SyncOAuthServiceTokenSource", FakeTokenSource)
    monkeypatch.setattr(raster_operators.requests, "post", fake_post)
    monkeypatch.setenv("SYSTEM_URL", "http://system:8180")
    monkeypatch.setenv("GEOPYTHON_WORKFLOW_SERVICE_CLIENT_SECRET", "geopython-secret")

    emit = raster_operators._progress_reporter({
        "endpoint": "http://manager:8081/api/v1/manager/executions/exec-1/events",
        "tenant_id": 7,
    })
    emit({"phase": "convert", "event": "started"})

    assert calls == [(
        "http://manager:8081/api/v1/manager/executions/exec-1/events",
        {"phase": "convert", "event": "started"},
        {"Authorization": "Bearer addp_at_geopython"},
        5,
    )]


def test_io_metadata_exposes_runtime_contract_only():
    operators = {operator["name"]: operator for operator in list_operators()}

    load_params = {param["name"] for param in operators["load"]["parameters"]}
    save_params = {param["name"] for param in operators["save"]["parameters"]}
    assert {"connection_info", "schema", "table", "path"} <= load_params
    assert {"connection_info", "schema", "table", "path"} <= save_params
    assert not {"locator", "target_parent_locator", "target_name"} & load_params
    assert not {"locator", "target_parent_locator", "target_name"} & save_params
    assert not any(param.get("ui_type") == "resource_tree_picker" for param in operators["load"]["parameters"])
    assert not any(param.get("ui_type") == "resource_tree_picker" for param in operators["save"]["parameters"])

    load_example = operators["load"]["detailed_description"]["workflow_example"]["params"]
    save_example = operators["save"]["detailed_description"]["workflow_example"]["params"]

    assert {"connection_info", "schema", "table"} <= set(load_example)
    assert {"connection_info", "schema", "table"} <= set(save_example)
    assert "locator" not in load_example
    assert "target_parent_locator" not in save_example

    assert not {"source_type", "format", "geojson"} & load_params
    assert not {"target_type", "format"} & save_params


def test_load_infers_file_format_from_selected_resource_path():
    with tempfile.TemporaryDirectory() as directory:
        source = Path(directory) / "points.csv"
        source.write_text("name,value\na,1\n", encoding="utf-8")

        result = load(
            connection_info={"engine_type": "nfs", "mount_path": directory},
            path="points.csv",
        )

    assert result.to_dict("records") == [{"name": "a", "value": 1}]


def test_load_rejects_removed_source_type_parameter():
    with pytest.raises(TypeError, match="source_type"):
        load(source_type="file", connection_info={}, path="points.csv")


def test_save_rejects_removed_target_type_parameter():
    with pytest.raises(TypeError, match="target_type"):
        save(gpd.GeoDataFrame(), target_type="file", connection_info={}, path="result.gpkg")

def test_operator_metadata_preserves_param_type():
    operators = {operator["name"]: operator for operator in list_operators()}

    buffer_params = {param["name"]: param for param in operators["buffer"]["parameters"]}
    load_params = {param["name"]: param for param in operators["load"]["parameters"]}

    assert buffer_params["input_gdf"]["param_type"] == "input"
    assert buffer_params["distance"]["param_type"] == "param"
    assert load_params["connection_info"]["param_type"] == "param"


def test_tiff_to_cog_metadata_public_contract():
    operators = {operator["name"]: operator for operator in list_operators()}

    assert "tiff_to_cog" in operators

    params = {param["name"]: param for param in operators["tiff_to_cog"]["parameters"]}
    runtime_only = {"engine_id", "connection_info", "schema", "table", "path"}

    assert {"source_uri", "target_uri", "gdal_env", "assign_srs", "compression", "blocksize", "overview_resampling", "overwrite"} <= set(params)
    assert not runtime_only & set(params)

    assert params["source_uri"]["required"] is True
    assert params["target_uri"]["required"] is True
    assert params["gdal_env"]["required"] is False
    assert params["assign_srs"]["required"] is False
    assert params["compression"]["enum"] == ["DEFLATE", "LZW", "ZSTD", "JPEG", "NONE"]
    assert params["blocksize"]["type"] == "integer"
    assert params["overwrite"]["type"] == "boolean"

    example = operators["tiff_to_cog"]["detailed_description"]["workflow_example"]["params"]
    assert "source_uri" in example
    assert "target_uri" in example
    assert "gdal_env" in example
    assert "assign_srs" in example
    assert not runtime_only & set(example)
    assert "direct" in operators["tiff_to_cog"]["execution_modes"]


def test_vector_to_pmtiles_metadata_uses_access_plan_contract():
    operators = {operator["name"]: operator for operator in list_operators()}

    assert "vector_to_pmtiles" in operators
    operator = operators["vector_to_pmtiles"]
    assert "direct" in operator["execution_modes"]
    params = {param["name"] for param in operator["parameters"]}
    assert {"access_plan", "tile", "options"} <= params
    assert "locator" not in params
    notes = " ".join(operator["detailed_description"]["notes"])
    assert "extent_srid=4326" not in notes


def test_vector_to_pmtiles_transforms_source_extent_to_wgs84():
    extent = [570841.0277000004, 3404864.0396999996, 598936.5142999999, 3434951.8803000003]

    transformed = _extent_to_wgs84(extent, 4549, "EPSG:4549")

    assert transformed[0] == pytest.approx(120.73, abs=0.02)
    assert transformed[1] == pytest.approx(30.76, abs=0.02)
    assert transformed[2] == pytest.approx(121.02, abs=0.02)
    assert transformed[3] == pytest.approx(31.03, abs=0.02)


def test_vector_to_pmtiles_accepts_single_point_extent():
    assert _normalize_extent([116.397, 39.908, 116.397, 39.908]) == [116.397, 39.908, 116.397, 39.908]


def test_vector_to_pmtiles_rejects_reversed_or_non_finite_extent():
    with pytest.raises(ValueError, match="tile.extent is invalid"):
        _normalize_extent([121.0, 30.0, 120.0, 31.0])
    with pytest.raises(ValueError, match="tile.extent is invalid"):
        _normalize_extent([120.0, float("nan"), 121.0, 31.0])


def test_vector_to_pmtiles_parses_gdal_progress_stream():
    text = "0...10...20...50...100 - done."

    assert _extract_gdal_progress_values(text) == [0, 10, 20, 50, 100]


def test_vector_to_pmtiles_emits_progress_from_streaming_command():
    events = []

    _run_command_with_gdal_progress(
        [
            sys.executable,
            "-c",
            "import sys; sys.stdout.write('0...10...50...100 - done.'); sys.stdout.flush()",
        ],
        {},
        events.append,
        9,
        18,
    )

    overall = [round(event["overall_progress"]) for event in events]
    assert overall == [1, 5, 20, 40]
    assert all(event["phase"] == "generate" for event in events)


def test_vector_to_pmtiles_builds_geometry_only_ogr_sql_with_optional_primary_key():
    assert _build_quick_view_ogr_sql("规划用地") == 'SELECT OGR_GEOMETRY FROM "规划用地"'
    assert _build_quick_view_ogr_sql('a"b', 'SmID') == 'SELECT OGR_GEOMETRY, "SmID" FROM "a""b"'


def test_build_raster_mosaic_metadata_public_contract():
    operators = {operator["name"]: operator for operator in list_operators()}

    assert "build_raster_mosaic" in operators

    params = {param["name"]: param for param in operators["build_raster_mosaic"]["parameters"]}
    assert {"access_plan", "placement", "cog", "overview", "tiles"} <= set(params)
    assert params["access_plan"]["required"] is True
    assert params["placement"]["required"] is True
    assert params["cog"]["required"] is False
    assert params["overview"]["required"] is False
    assert params["tiles"]["required"] is False
    assert "direct" in operators["build_raster_mosaic"]["execution_modes"]

    example = operators["build_raster_mosaic"]["detailed_description"]["workflow_example"]["params"]
    assert example["access_plan"]["source"]["root_uri"]
    assert example["access_plan"]["target"]["dataset_root_uri"]
    assert example["placement"]["mode"] == "detached"


def test_build_raster_mosaic_rejects_invalid_contract():
    with pytest.raises(ValueError):
        build_raster_mosaic(
            access_plan={
                "source": {"root_uri": "/mnt/business/rasters/source"},
                "target": {"dataset_root_uri": "/vsis3/business/mosaic/output/raster_mosaic"},
            },
            placement={"mode": "unknown"},
        )


def test_build_raster_mosaic_creates_local_dataset():
    gdal = pytest.importorskip("osgeo.gdal")
    osr = pytest.importorskip("osgeo.osr")
    gdal.UseExceptions()

    with tempfile.TemporaryDirectory() as tmp:
        source_dir = Path(tmp) / "source"
        target_dir = Path(tmp) / "target" / "mosaic"
        source_dir.mkdir(parents=True)
        _create_test_geotiff(gdal, osr, source_dir / "a.tif", 0, 2)
        _create_test_geotiff(gdal, osr, source_dir / "b.tif", 2, 2)

        result = build_raster_mosaic(
            access_plan={
                "source": {
                    "root_uri": source_dir.as_posix(),
                    "gdal_env": {},
                    "recursive": True,
                    "include_patterns": ["*.tif"],
                },
                "target": {
                    "dataset_root_uri": target_dir.as_posix(),
                    "gdal_env": {},
                    "dataset_name": "mosaic",
                },
            },
            placement={"mode": "detached"},
            cog={
                "compression": "DEFLATE",
                "blocksize": 512,
                "overview_resampling": "NEAREST",
                "leaf_concurrency": 4,
                "num_threads": 2,
            },
            overview={"enabled": True, "max_pixels": 4096, "resampling": "AVERAGE"},
            tiles={"enabled": False},
        )

        assert result["status"] == "success"
        assert result["format"] == "raster_mosaic"
        assert result["leaf_count"] == 2
        assert result["stage_timings"]["leaf_cog"]["concurrency"] == 4
        assert result["stage_timings"]["total"]["duration_ms"] >= 0
        assert (target_dir / "mosaic.addp.json").exists()
        assert (target_dir / "index" / "source-index.json").exists()
        assert (target_dir / "overviews" / "overview.cog.tif").exists()

        manifest = json.loads((target_dir / "mosaic.addp.json").read_text())
        index = json.loads((target_dir / "index" / "source-index.json").read_text())
        assert manifest["data_type"] == "media"
        assert manifest["format"] == "raster_mosaic"
        assert manifest["layout"] == "whole"
        assert manifest["refs"]["overview"] == "overviews/overview.cog.tif"
        assert len(index["leaves"]) == 2
        assert all(leaf["cog_validation"]["status"] == "valid" for leaf in index["leaves"])


def test_build_raster_mosaic_assigns_wgs84_for_geographic_leaf_without_projection():
    gdal = pytest.importorskip("osgeo.gdal")
    gdal.UseExceptions()

    with tempfile.TemporaryDirectory() as tmp:
        source_dir = Path(tmp) / "source"
        target_dir = Path(tmp) / "target" / "mosaic"
        source_dir.mkdir(parents=True)
        _create_test_geotiff_without_projection(gdal, source_dir / "a.tif", 110, 4)

        result = build_raster_mosaic(
            access_plan={
                "source": {
                    "root_uri": source_dir.as_posix(),
                    "gdal_env": {},
                    "recursive": True,
                    "include_patterns": ["*.tif"],
                },
                "target": {
                    "dataset_root_uri": target_dir.as_posix(),
                    "gdal_env": {},
                    "dataset_name": "mosaic",
                },
            },
            placement={"mode": "detached"},
            cog={
                "compression": "DEFLATE",
                "blocksize": 512,
                "overview_resampling": "NEAREST",
                "leaf_concurrency": 1,
                "num_threads": 1,
            },
            overview={"enabled": False},
            tiles={"enabled": False},
        )

        assert result["status"] == "success"
        index = json.loads((target_dir / "index" / "source-index.json").read_text())
        assert index["leaves"][0]["source_crs"] == "EPSG:4326"
        leaf_path = target_dir / "leaf" / "a.cog.tif"
        ds = gdal.OpenEx(leaf_path.as_posix(), gdal.OF_RASTER)
        assert ds is not None
        assert "4326" in (ds.GetProjection() or "")
        ds = None


def test_build_raster_mosaic_reuses_existing_detached_leaf_cogs(monkeypatch):
    gdal = pytest.importorskip("osgeo.gdal")
    osr = pytest.importorskip("osgeo.osr")
    gdal.UseExceptions()

    with tempfile.TemporaryDirectory() as tmp:
        source_dir = Path(tmp) / "source"
        target_dir = Path(tmp) / "target" / "mosaic"
        source_dir.mkdir(parents=True)
        _create_test_geotiff(gdal, osr, source_dir / "a.tif", 0, 2)
        _create_test_geotiff(gdal, osr, source_dir / "b.tif", 2, 2)

        params = {
            "access_plan": {
                "source": {
                    "root_uri": source_dir.as_posix(),
                    "gdal_env": {},
                    "recursive": True,
                    "include_patterns": ["*.tif"],
                },
                "target": {
                    "dataset_root_uri": target_dir.as_posix(),
                    "gdal_env": {},
                    "dataset_name": "mosaic",
                },
            },
            "placement": {"mode": "detached"},
            "cog": {
                "compression": "DEFLATE",
                "blocksize": 512,
                "overview_resampling": "NEAREST",
                "leaf_concurrency": 1,
                "num_threads": 1,
            },
            "overview": {"enabled": False},
            "tiles": {"enabled": False},
        }
        first_result = build_raster_mosaic(**params)
        assert first_result["stage_timings"]["leaf_cog"]["generated_count"] == 2

        original_translate = raster_operators._translate_to_cog

        def reject_leaf_regeneration(source, target, *args, **kwargs):
            if "/leaf/" in str(target):
                raise AssertionError(f"leaf COG should have been reused: {target}")
            return original_translate(source, target, *args, **kwargs)

        monkeypatch.setattr(raster_operators, "_translate_to_cog", reject_leaf_regeneration)

        second_result = build_raster_mosaic(**params)

        leaf_timing = second_result["stage_timings"]["leaf_cog"]
        assert leaf_timing["generated_count"] == 0
        assert leaf_timing["reused_count"] == 2
        index = json.loads((target_dir / "index" / "source-index.json").read_text())
        assert {leaf["generation_status"] for leaf in index["leaves"]} == {"reused"}


def test_build_raster_mosaic_retries_leaf_cog_generation(monkeypatch):
    gdal = pytest.importorskip("osgeo.gdal")
    osr = pytest.importorskip("osgeo.osr")
    gdal.UseExceptions()

    with tempfile.TemporaryDirectory() as tmp:
        source_dir = Path(tmp) / "source"
        target_dir = Path(tmp) / "target" / "mosaic"
        source_dir.mkdir(parents=True)
        _create_test_geotiff(gdal, osr, source_dir / "a.tif", 0, 2)
        original_translate = raster_operators._translate_to_cog
        calls = {"leaf": 0}

        def fail_first_leaf_attempt(source, target, *args, **kwargs):
            if "/leaf/" in str(target):
                calls["leaf"] += 1
                if calls["leaf"] == 1:
                    raise RuntimeError("transient GDAL write failure")
            return original_translate(source, target, *args, **kwargs)

        monkeypatch.setattr(raster_operators, "_translate_to_cog", fail_first_leaf_attempt)

        result = build_raster_mosaic(
            access_plan={
                "source": {
                    "root_uri": source_dir.as_posix(),
                    "gdal_env": {},
                    "recursive": True,
                    "include_patterns": ["*.tif"],
                },
                "target": {
                    "dataset_root_uri": target_dir.as_posix(),
                    "gdal_env": {},
                    "dataset_name": "mosaic",
                },
            },
            placement={"mode": "detached"},
            cog={
                "compression": "DEFLATE",
                "blocksize": 512,
                "overview_resampling": "NEAREST",
                "leaf_concurrency": 1,
                "num_threads": 1,
                "leaf_retry_attempts": 2,
            },
            overview={"enabled": False},
            tiles={"enabled": False},
        )

        assert result["status"] == "success"
        assert calls["leaf"] == 2
        leaf_timing = result["stage_timings"]["leaf_cog"]
        assert leaf_timing["retry_attempts"] == 2
        assert leaf_timing["retry_count"] == 1
        assert leaf_timing["generated_count"] == 1


def test_write_json_uses_vsi_file_handle_for_virtual_paths(monkeypatch):
    class FakeStat:
        size = 12

    class FakeGDAL:
        def __init__(self):
            self.opened = ""
            self.payload = b""

        def GetConfigOption(self, key):
            return None

        def SetConfigOption(self, key, value):
            return None

        def MkdirRecursive(self, path, mode):
            return 0

        def VSIFOpenL(self, path, mode):
            self.opened = f"{path}:{mode}"
            return object()

        def VSIFWriteL(self, data, size, count, handle):
            self.payload = data
            return len(data)

        def VSIFCloseL(self, handle):
            return 0

        def VSIStatL(self, path):
            return FakeStat()

    fake_gdal = FakeGDAL()
    monkeypatch.setattr("operators.raster_operators._import_gdal", lambda: fake_gdal)

    _write_json("/vsis3/addp/mosaic/mosaic.addp.json", {"ok": True}, {"AWS_HTTPS": "NO"})

    assert fake_gdal.opened == "/vsis3/addp/mosaic/mosaic.addp.json:wb"
    assert b'"ok": true' in fake_gdal.payload


def test_translate_to_cog_enables_gdal_threads(monkeypatch):
    class FakeGDAL:
        def __init__(self):
            self.creation_options = []

        def GetConfigOption(self, key):
            return None

        def SetConfigOption(self, key, value):
            return None

        def TranslateOptions(self, **kwargs):
            self.creation_options = kwargs.get("creationOptions") or []
            return kwargs

        def Translate(self, target, source, options=None):
            return object()

    fake_gdal = FakeGDAL()
    monkeypatch.setattr("operators.raster_operators._import_gdal", lambda: fake_gdal)

    _translate_to_cog("source.tif", "target.tif", {}, {"compression": "DEFLATE", "blocksize": 512})

    assert "NUM_THREADS=2" in fake_gdal.creation_options


def test_vector_reproject_metadata_public_contract():
    operators = {operator["name"]: operator for operator in list_operators()}

    assert "vector_reproject" in operators

    params = {param["name"]: param for param in operators["vector_reproject"]["parameters"]}
    assert {"input_gdf", "source_crs", "target_crs"} <= set(params)
    assert operators["vector_reproject"]["execution_modes"] == ["workflow", "direct"]

    direct_binary = operators["vector_reproject"]["attributes"]["direct_binary"]
    assert direct_binary["content_type"] == "application/vnd.apache.arrow.stream"
    assert direct_binary["encoding"] == "arrow"
    assert direct_binary["input_name"] == "geometry_batch"
    assert direct_binary["output_name"] == "geometry_batch"
    assert direct_binary["geometry_encoding"] == "ewkb"


def test_crs_to_projjson_metadata_public_contract():
    operators = {operator["name"]: operator for operator in list_operators()}

    assert "crs_to_projjson" in operators
    params = {param["name"]: param for param in operators["crs_to_projjson"]["parameters"]}
    assert set(params) == {"crs_ref", "definition_encoding", "definition"}
    assert operators["crs_to_projjson"]["execution_modes"] == ["direct"]
    assert operators["crs_to_projjson"]["output_ports"] == [{
        "name": "default",
        "type": "object",
        "description": "PROJJSON CRS 定义",
        "is_default": True,
    }]


def test_crs_to_projjson_resolves_epsg_from_local_database():
    result = crs_to_projjson("EPSG:3857")
    definition = json.loads(result["definition"])

    assert result["crs_ref"] == "EPSG:3857"
    assert result["definition_encoding"] == "projjson"
    assert definition["type"] == "ProjectedCRS"
    assert definition["id"] == {"authority": "EPSG", "code": 3857}


def test_crs_to_projjson_converts_matching_wkt_definition():
    source_wkt = CRS.from_epsg(3857).to_wkt(version="WKT1_GDAL")

    result = crs_to_projjson("EPSG:3857", "wkt", source_wkt)

    assert json.loads(result["definition"])["id"] == {"authority": "EPSG", "code": 3857}


def test_crs_to_projjson_preserves_addp_custom_identity():
    definition = "+proj=longlat +a=6378137 +rf=298.257223563 +no_defs"
    digest = hashlib.sha256(definition.encode("utf-8")).hexdigest()

    result = crs_to_projjson(f"ADDP:CRS:{digest}", "proj4", definition)
    projjson = json.loads(result["definition"])

    assert result["crs_ref"] == f"ADDP:CRS:{digest}"
    assert projjson["id"] == {"authority": "ADDP", "code": f"CRS:{digest}"}


def test_crs_to_projjson_rejects_conflicting_epsg_definition():
    source_wkt = CRS.from_epsg(4326).to_wkt()

    with pytest.raises(ValueError, match="does not match EPSG:3857"):
        crs_to_projjson("EPSG:3857", "wkt", source_wkt)


def test_geometry_batch_arrow_round_trip_and_reproject():
    source = gpd.GeoDataFrame(
        {"name": ["a", "b"]},
        geometry=gpd.GeoSeries.from_wkt(
            ["POINT (0 0)", "POINT (1113194.9079327357 0)"],
            crs="EPSG:3857",
        ),
    )

    payload = encode_geometry_batch_arrow(
        source,
        geometry_column=source.geometry.name,
        source_crs="EPSG:3857",
        target_crs="EPSG:4326",
    )
    decoded = decode_geometry_batch_arrow(payload)
    metadata = _geometry_batch_metadata(payload)
    raw_geometry = _first_geometry_bytes(payload)

    assert decoded.geometry.name == source.geometry.name
    assert str(decoded.crs) == "EPSG:3857"
    assert metadata["addp.geometry.encoding"] == "ewkb"
    assert shapely.get_srid(shapely.from_wkb(raw_geometry)) == 3857

    result = vector_reproject(decoded, source_crs="EPSG:3857", target_crs="EPSG:4326")
    result_payload = encode_geometry_batch_arrow(
        result,
        geometry_column=result.geometry.name,
        source_crs="EPSG:4326",
        target_crs="EPSG:4326",
    )
    result_metadata = _geometry_batch_metadata(result_payload)
    result_raw_geometry = _first_geometry_bytes(result_payload)

    assert str(result.crs) == "EPSG:4326"
    assert result_metadata["addp.geometry.source_crs"] == "EPSG:4326"
    assert result_metadata["addp.geometry.target_crs"] == "EPSG:4326"
    assert result.geometry.iloc[0].x == 0
    assert abs(result.geometry.iloc[1].x - 10) < 0.05
    assert shapely.get_srid(shapely.from_wkb(result_raw_geometry)) == 4326


def _geometry_batch_metadata(payload: bytes):
    reader = pa.ipc.open_stream(pa.BufferReader(payload))
    table = reader.read_all()
    return {
        key.decode("utf-8") if isinstance(key, (bytes, bytearray)) else str(key):
        value.decode("utf-8") if isinstance(value, (bytes, bytearray)) else str(value)
        for key, value in (table.schema.metadata or {}).items()
    }


def _first_geometry_bytes(payload: bytes) -> bytes:
    reader = pa.ipc.open_stream(pa.BufferReader(payload))
    table = reader.read_all()
    return table.column(0).to_pylist()[0]


def _create_test_geotiff(gdal, osr, path: Path, origin_x: float, fill_value: int) -> None:
    driver = gdal.GetDriverByName("GTiff")
    ds = driver.Create(path.as_posix(), 16, 16, 1, gdal.GDT_Byte)
    ds.SetGeoTransform((origin_x, 0.125, 0, 2, 0, -0.125))
    srs = osr.SpatialReference()
    srs.ImportFromEPSG(4326)
    ds.SetProjection(srs.ExportToWkt())
    ds.GetRasterBand(1).Fill(fill_value)
    ds.FlushCache()
    ds = None


def _create_test_geotiff_without_projection(gdal, path: Path, origin_x: float, origin_y: float) -> None:
    driver = gdal.GetDriverByName("GTiff")
    ds = driver.Create(path.as_posix(), 16, 16, 1, gdal.GDT_Byte)
    ds.SetGeoTransform((origin_x, 0.01, 0, origin_y, 0, -0.01))
    ds.GetRasterBand(1).Fill(2)
    ds.FlushCache()
    ds = None


def test_raster_wkt_authority_ignores_unit_epsg_ids():
    wkt = '''GEOGCRS["WGS 84",
    DATUM["World Geodetic System 1984",
        ELLIPSOID["WGS 84",6378137,298.257223563,
            LENGTHUNIT["metre",1,
                ID["EPSG",9001]]]],
    PRIMEM["Greenwich",0,
        ANGLEUNIT["degree",0.0174532925199433,
            ID["EPSG",9122]]],
    CS[ellipsoidal,2],
        AXIS["latitude",north,
            ORDER[1],
            ANGLEUNIT["degree",0.0174532925199433,
                ID["EPSG",9122]]],
        AXIS["longitude",east,
            ORDER[2],
            ANGLEUNIT["degree",0.0174532925199433,
                ID["EPSG",9122]]]]'''

    assert _authority_code_from_wkt(wkt) == ""


def test_raster_wkt_authority_reads_root_epsg_id():
    wkt = '''GEOGCRS["WGS 84",
    DATUM["World Geodetic System 1984",
        ELLIPSOID["WGS 84",6378137,298.257223563,
            LENGTHUNIT["metre",1,
                ID["EPSG",9001]]]],
    CS[ellipsoidal,2],
        AXIS["latitude",north,
            ANGLEUNIT["degree",0.0174532925199433,
                ID["EPSG",9122]]],
        AXIS["longitude",east,
            ANGLEUNIT["degree",0.0174532925199433,
                ID["EPSG",9122]]],
    ID["EPSG",4326]]'''

    assert _authority_code_from_wkt(wkt) == "EPSG:4326"


def test_all_operator_metadata_declares_execution_modes_and_effects():
    operators = list_operators()
    assert_operator_metadata_contract(operators, expected_engine_type="geopython_workflow")
    by_name = {operator["name"]: operator for operator in operators}
    assert by_name["load"]["effects"] == ["read"]
    assert by_name["save"]["effects"] == ["write"]
    assert by_name["tiff_to_cog"]["effects"] == ["read", "write"]
    assert by_name["build_raster_mosaic"]["effects"] == ["read", "write"]
    assert by_name["vector_to_pmtiles"]["effects"] == ["read", "write"]


def test_api_execution_status_unknown_id():
    import api_server

    api_server.app.config["TESTING"] = True
    with api_server.app.test_client() as client:
        response = client.get("/api/executions/unknown-execution-id")

    assert response.status_code == 404
    payload = response.get_json()
    assert payload["error_code"] == "EXECUTION_NOT_FOUND"
    assert "task_status" not in payload


def test_crs_to_projjson_direct_returns_plain_json_result():
    import api_server

    api_server.app.config["TESTING"] = True
    with api_server.app.test_client() as client:
        response = client.post(
            "/api/operators/crs_to_projjson/invoke",
            json={"params": {"crs_ref": "EPSG:3857"}},
        )

    assert response.status_code == 200
    payload = response.get_json()
    assert "binary_payload" not in payload
    assert payload["result"]["crs_ref"] == "EPSG:3857"
    assert payload["result"]["definition_encoding"] == "projjson"
    assert json.loads(payload["result"]["definition"])["id"] == {
        "authority": "EPSG",
        "code": 3857,
    }


def test_vector_reproject_direct_returns_ewkb_with_target_srid():
    import api_server

    source = gpd.GeoDataFrame(
        {"name": ["a"]},
        geometry=gpd.GeoSeries.from_wkt(["POINT (1113194.9079327357 0)"], crs="EPSG:3857"),
    )
    payload = encode_geometry_batch_arrow(
        source,
        geometry_column=source.geometry.name,
        source_crs="EPSG:3857",
        target_crs="EPSG:4326",
        geometry_encoding="ewkb",
    )

    api_server.app.config["TESTING"] = True
    with api_server.app.test_client() as client:
        response = client.post(
            "/api/operators/vector_reproject/invoke",
            json={
                "params": {
                    "source_crs": "EPSG:3857",
                    "target_crs": "EPSG:4326",
                },
                "binary_payload": {
                    "content_type": "application/vnd.apache.arrow.stream",
                    "encoding": "arrow",
                    "name": "geometry_batch",
                    "data": base64.b64encode(payload).decode("ascii"),
                    "metadata": {
                        "geometry_column": source.geometry.name,
                        "geometry_encoding": "ewkb",
                        "source_crs": "EPSG:3857",
                        "target_crs": "EPSG:4326",
                    },
                },
            },
        )

    assert response.status_code == 200
    result = response.get_json()
    result_payload = base64.b64decode(result["binary_payload"]["data"])
    result_metadata = _geometry_batch_metadata(result_payload)
    result_geometry = shapely.from_wkb(_first_geometry_bytes(result_payload))

    assert result["binary_payload"]["metadata"]["geometry_encoding"] == "ewkb"
    assert result["binary_payload"]["metadata"]["crs"] == "EPSG:4326"
    assert result_metadata["addp.geometry.source_crs"] == "EPSG:4326"
    assert result_metadata["addp.geometry.target_crs"] == "EPSG:4326"
    assert shapely.get_srid(result_geometry) == 4326
    assert abs(result_geometry.x - 10) < 0.05
    assert result_geometry.y == 0


def test_vector_reproject_direct_rejects_non_ewkb_geometry_encoding():
    import api_server

    source = gpd.GeoDataFrame(
        {"name": ["a"]},
        geometry=gpd.GeoSeries.from_wkt(["POINT (0 0)"], crs="EPSG:4326"),
    )
    payload = encode_geometry_batch_arrow(
        source,
        geometry_column=source.geometry.name,
        source_crs="EPSG:4326",
        target_crs="EPSG:4326",
        geometry_encoding="ewkb",
    )

    api_server.app.config["TESTING"] = True
    with api_server.app.test_client() as client:
        response = client.post(
            "/api/operators/vector_reproject/invoke",
            json={
                "params": {
                    "source_crs": "EPSG:4326",
                    "target_crs": "EPSG:4326",
                },
                "binary_payload": {
                    "content_type": "application/vnd.apache.arrow.stream",
                    "encoding": "arrow",
                    "name": "geometry_batch",
                    "data": base64.b64encode(payload).decode("ascii"),
                    "metadata": {
                        "geometry_column": source.geometry.name,
                        "geometry_encoding": "wkb",
                        "source_crs": "EPSG:4326",
                        "target_crs": "EPSG:4326",
                    },
                },
            },
        )

    assert response.status_code == 400
    result = response.get_json()
    assert result["error_code"] == "INVALID_PARAMS"
    assert "geometry_encoding" in result["error"]


def test_vector_reproject_direct_rejects_non_ewkb_arrow_schema_encoding():
    import api_server

    source = gpd.GeoDataFrame(
        {"name": ["a"]},
        geometry=gpd.GeoSeries.from_wkt(["POINT (0 0)"], crs="EPSG:4326"),
    )
    payload = encode_geometry_batch_arrow(
        source,
        geometry_column=source.geometry.name,
        source_crs="EPSG:4326",
        target_crs="EPSG:4326",
        geometry_encoding="wkb",
    )

    api_server.app.config["TESTING"] = True
    with api_server.app.test_client() as client:
        response = client.post(
            "/api/operators/vector_reproject/invoke",
            json={
                "params": {
                    "source_crs": "EPSG:4326",
                    "target_crs": "EPSG:4326",
                },
                "binary_payload": {
                    "content_type": "application/vnd.apache.arrow.stream",
                    "encoding": "arrow",
                    "name": "geometry_batch",
                    "data": base64.b64encode(payload).decode("ascii"),
                    "metadata": {
                        "geometry_column": source.geometry.name,
                        "geometry_encoding": "ewkb",
                        "source_crs": "EPSG:4326",
                        "target_crs": "EPSG:4326",
                    },
                },
            },
        )

    assert response.status_code == 400
    result = response.get_json()
    assert result["error_code"] == "INVALID_PARAMS"
    assert "Arrow schema geometry encoding" in result["error"]


def test_tiff_to_cog_does_not_pass_gdal_overwrite():
    calls = []

    def fake_run_command(args, extra_env=None):
        calls.append(args)
        class Completed:
            stdout = "{}"
        return Completed()

    import operators.raster_operators as raster_operators

    original_run_command = raster_operators._run_command
    with tempfile.TemporaryDirectory() as tmp_dir:
        target = Path(tmp_dir) / "target.cog.tif"
        target.write_text("old")
        raster_operators._run_command = fake_run_command
        try:
            result = tiff_to_cog(
                source_uri="/tmp/source.tif",
                target_uri=str(target),
                overwrite=True,
            )
        finally:
            raster_operators._run_command = original_run_command

    assert result["status"] == "success"
    assert calls
    assert "-overwrite" not in calls[0]
    assert not target.exists()


def test_tiff_to_cog_passes_assign_srs_to_gdal_translate():
    calls = []

    def fake_run_command(args, extra_env=None):
        calls.append(args)
        class Completed:
            stdout = "{}"
        return Completed()

    import operators.raster_operators as raster_operators

    original_run_command = raster_operators._run_command
    raster_operators._run_command = fake_run_command
    try:
        result = tiff_to_cog(
            source_uri="/tmp/source.tif",
            target_uri="/tmp/target.cog.tif",
            assign_srs="+proj=longlat +datum=WGS84 +no_defs",
        )
    finally:
        raster_operators._run_command = original_run_command

    assert result["status"] == "success"
    assert "-a_srs" in calls[0]
    assert calls[0][calls[0].index("-a_srs") + 1] == "+proj=longlat +datum=WGS84 +no_defs"


def test_vector_to_pmtiles_uses_source_env_for_ogr_and_target_env_for_publish():
    import operators.vector_tile_operators as vector_tile_operators

    command_envs = []
    published = []

    def fake_run_command(args, extra_env=None, emit=None, min_zoom=0, max_zoom=0):
        command_envs.append(dict(extra_env or {}))
        assert "-progress" in args
        assert "COMPRESS=YES" in args
        assert "EXTENT=8192" in args
        assert "BUFFER=160" in args
        assert "MAX_SIZE=5000000" in args
        assert "MAX_FEATURES=1000000" in args
        assert "SIMPLIFICATION=0.0" in args
        assert "SIMPLIFICATION_MAX_ZOOM=0.0" in args
        assert "-sql" in args
        assert args[args.index("-sql") + 1] == 'SELECT OGR_GEOMETRY FROM "规划用地"'
        assert "-dialect" in args
        assert args[args.index("-dialect") + 1] == "OGRSQL"
        output_dir = Path(args[args.index("-f") + 2])
        assert not output_dir.exists()
        min_x, min_y, _, _ = vector_tile_operators._tile_bounds([110.0, 20.0, 110.1, 20.1], 9)
        tile_dir = output_dir / "9" / str(min_x)
        tile_dir.mkdir(parents=True, exist_ok=True)
        (tile_dir / f"{min_y}.mvt").write_bytes(b"\x1f\x8bcompressed-mvt")

        class Completed:
            stdout = "{}"

        return Completed()

    def fake_publish_archive(source_path, target_uri, target_env):
        published.append((target_uri, dict(target_env or {}), source_path.read_bytes()))

    original_run_command = vector_tile_operators._run_command_with_gdal_progress
    original_publish_archive = vector_tile_operators._publish_pmtiles_archive
    vector_tile_operators._run_command_with_gdal_progress = fake_run_command
    vector_tile_operators._publish_pmtiles_archive = fake_publish_archive
    try:
        result = vector_to_pmtiles(
            access_plan={
                "source": {
                    "root_uri": "/vsis3/addp/gis/规划用地.shp",
                    "gdal_env": {
                        "AWS_S3_ENDPOINT": "source-minio:9000",
                        "AWS_ACCESS_KEY_ID": "source-ak",
                        "AWS_SECRET_ACCESS_KEY": "source-sk",
                        "AWS_HTTPS": "NO",
                    },
                    "item_fingerprint": "fp",
                    "full_name": "addp/gis/规划用地.shp",
                },
                "target": {
                    "archive_uri": "/vsis3/manager/tenant_7/vector-tile-cache/fp.pmtiles",
                    "gdal_env": {
                        "AWS_S3_ENDPOINT": "target-minio:9000",
                        "AWS_ACCESS_KEY_ID": "target-ak",
                        "AWS_SECRET_ACCESS_KEY": "target-sk",
                        "AWS_HTTPS": "NO",
                    },
                    "storage_ref": "{}",
                },
            },
            tile={
                "min_zoom": 9,
                "max_zoom": 9,
                "extent": [110.0, 20.0, 110.1, 20.1],
                "extent_srid": 4326,
                "source_srs": "EPSG:4326",
            },
            options={"geometry_column": "geometry"},
        )
    finally:
        vector_tile_operators._run_command_with_gdal_progress = original_run_command
        vector_tile_operators._publish_pmtiles_archive = original_publish_archive

    assert result["stop_reason"] == "workflow_ogr2ogr_pmtiles"
    assert result["archive_format"] == "pmtiles"
    assert result["spec_version"] == 3
    assert command_envs
    assert command_envs[0]["AWS_S3_ENDPOINT"] == "source-minio:9000"
    assert command_envs[0]["AWS_ACCESS_KEY_ID"] == "source-ak"
    assert command_envs[0]["AWS_SECRET_ACCESS_KEY"] == "source-sk"
    assert command_envs[0]["GDAL_NUM_THREADS"] == "ALL_CPUS"
    assert len(published) == 1
    assert published[0][0].endswith("fp.pmtiles")
    assert published[0][1]["AWS_S3_ENDPOINT"] == "target-minio:9000"
    assert published[0][2].startswith(b"PMTiles\x03")
    assert result["mvt_options"] == {
        "extent": 8192,
        "buffer": 160,
        "max_size": 5000000,
        "max_features": 1000000,
        "simplification": 0.0,
        "simplification_max_zoom": 0.0,
        "num_threads": "ALL_CPUS",
    }


def test_vector_to_pmtiles_indexes_gdal_pbf_output(tmp_path):
    tile_path = tmp_path / "18" / "219155" / "107332.pbf"
    tile_path.parent.mkdir(parents=True)
    tile_path.write_bytes(b"tile")
    (tmp_path / "metadata.json").write_text("{}", encoding="utf-8")

    indexed = _index_generated_tiles(tmp_path)

    assert indexed[(18, 219155, 107332)] == tile_path
