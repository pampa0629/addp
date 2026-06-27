from pathlib import Path
import sys
import tempfile
import base64
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
from operators.spatial_transform_operators import vector_reproject
from operators.raster_operators import _authority_code_from_wkt, _translate_to_cog, _write_json, build_raster_mosaic, tiff_to_cog
import pyarrow as pa
from geometry_batches import decode_geometry_batch_arrow, encode_geometry_batch_arrow

import geopandas as gpd
import pytest
import shapely


def test_io_metadata_uses_resource_locator_public_contract():
    operators = {operator["name"]: operator for operator in list_operators()}

    load_params = {param["name"] for param in operators["load"]["parameters"]}
    save_params = {param["name"] for param in operators["save"]["parameters"]}
    runtime_only = {"engine_id", "connection_info", "schema", "table", "path"}

    assert "locator" in load_params
    assert "target_parent_locator" in save_params
    assert "target_name" in save_params
    assert not runtime_only & load_params
    assert not runtime_only & save_params

    load_example = operators["load"]["detailed_description"]["workflow_example"]["params"]
    save_example = operators["save"]["detailed_description"]["workflow_example"]["params"]

    assert "locator" in load_example
    assert "target_parent_locator" in save_example
    assert "target_name" in save_example
    assert not runtime_only & set(load_example)
    assert not runtime_only & set(save_example)

    load_source_type = next(param for param in operators["load"]["parameters"] if param["name"] == "source_type")
    save_target_type = next(param for param in operators["save"]["parameters"] if param["name"] == "target_type")

    assert load_source_type["enum"] == ["table", "file", "geojson"]
    assert save_target_type["enum"] == ["table", "file"]
    assert "nfs" not in load_source_type["enum"]
    assert "nfs" not in save_target_type["enum"]

    picker_configs = [
        param["ui_config"]
        for operator_name in ("load", "save")
        for param in operators[operator_name]["parameters"]
        if param.get("ui_type") == "resource_tree_picker"
    ]
    assert picker_configs
    assert all("engine_types" not in config for config in picker_configs)
    assert any(config.get("engine_families") == ["tabular", "dynamic_schema"] for config in picker_configs)
    assert any(config.get("engine_families") == ["file", "object"] for config in picker_configs)


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


def test_all_operator_metadata_declares_execution_modes():
    assert_operator_metadata_contract(
        list_operators(),
        expected_engine_type="python_workflow",
    )


def test_api_execution_status_unknown_id():
    import api_server

    api_server.app.config["TESTING"] = True
    with api_server.app.test_client() as client:
        response = client.get("/api/executions/unknown-execution-id")

    assert response.status_code == 404
    payload = response.get_json()
    assert payload["error_code"] == "EXECUTION_NOT_FOUND"
    assert "task_status" not in payload


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
