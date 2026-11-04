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
    _build_vector_tile_ogr_sql,
    _extent_to_wgs84,
    _extract_gdal_progress_values,
    _index_generated_tiles,
    _normalize_extent,
    _run_command_with_gdal_progress,
    vector_to_pmtiles,
)
from operators.spatial_transform_operators import crs_to_projjson, vector_reproject
from operators.raster_operators import _authority_code_from_wkt, _translate_to_cog, _write_json, build_raster_mosaic
import pyarrow as pa
from geometry_batches import decode_geometry_batch_arrow, encode_geometry_batch_arrow

import geopandas as gpd
import pytest
import shapely
from pyproj import CRS
from operators.io_operators import load, save


def test_intersection_overlay_metadata_requires_both_explicit_sources_and_full_dimensions():
    spec=next(item for item in list_operators() if item['name']=='intersection')
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read']
    params={p['name']:p for p in spec['parameters']}
    assert set(params)=={'input_gdf','gdf_b','id_field','reference_id_field'}
    assert all(p['required'] for p in params.values())
    assert all('default' not in params[name] for name in ('id_field','reference_id_field'))
    assert len(spec['output_ports'])==1 and spec['output_ports'][0]['is_default']
    assert spec['output_ports'][0]['name']=='default' and spec['output_ports'][0]['type']=='geodataframe'
    assert params['id_field']['type']==params['reference_id_field']['type']=='string'
    assert 'GeometryCollection' in spec['detailed_description']['overview']


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
        "attempt": 2,
        "lease_token": "lease-token",
    })
    emit({"phase": "convert", "event": "started"})

    assert calls == [(
        "http://manager:8081/api/v1/manager/executions/exec-1/events",
        {
            "phase": "convert",
            "event": "started",
            "attempt": 2,
            "lease_token": "lease-token",
        },
        {"Authorization": "Bearer addp_at_geopython"},
        5,
    )]


def test_io_metadata_exposes_runtime_contract_only():
    operators = {operator["name"]: operator for operator in list_operators()}

    load_params = {param["name"] for param in operators["load"]["parameters"]}
    save_params = {param["name"] for param in operators["save"]["parameters"]}
    assert {"connection_info", "schema", "table", "path", "pipeline"} <= load_params
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


@pytest.mark.parametrize('source_format', ['geojson', 'shapefile', 'flatgeobuf'])
def test_vector_to_pmtiles_standard_plan_native_attributes_and_create(tmp_path, source_format, monkeypatch):
    from osgeo import gdal
    import operators.vector_tile_operators as module
    run_subprocess = module._native_process.run

    def run_with_indexed_dataset(command, **kwargs):
        # Hosted bindings expose indexed layer access without Dataset iteration.
        # Exercise the real probe in its child process and retain all native assertions.
        if command[:2] == [sys.executable, '-c']:
            command = [*command[:2], 'from osgeo import gdal\ngdal.Dataset.__iter__ = None\n' + command[2], *command[3:]]
        return run_subprocess(command, **kwargs)

    monkeypatch.setattr(module._native_process, 'run', run_with_indexed_dataset)
    work_dirs = []
    create_work_dir = module.tempfile.mkdtemp
    def track_work_dir(*args, **kwargs):
        directory = create_work_dir(*args, **kwargs)
        work_dirs.append(Path(directory))
        return directory
    monkeypatch.setattr(module.tempfile, 'mkdtemp', track_work_dir)
    source = tmp_path / 'points.geojson'
    source.write_text(json.dumps({'type': 'FeatureCollection', 'features': [{
        'type': 'Feature', 'geometry': {'type': 'Point', 'coordinates': [116.4, 39.9]},
        'properties': {'类别': '公园', 'count': 7, 'private_note': 'excluded'}
    }]}), encoding='utf-8')
    if source_format != 'geojson':
        converted = source.with_suffix('.shp' if source_format == 'shapefile' else '.fgb')
        dataset = gdal.VectorTranslate(str(converted), str(source), format='ESRI Shapefile' if source_format == 'shapefile' else 'FlatGeobuf',
                                       layerCreationOptions=['ENCODING=UTF-8'] if source_format == 'shapefile' else [])
        dataset = None
        source = converted
    source_bytes = {path: path.read_bytes() for path in tmp_path.iterdir() if path.is_file()}
    target = tmp_path / 'points.pmtiles'
    plan = {'schema_version': 'addp.workflow.access-plan/v1',
            'source': {'kind': 'file', 'format': source_format, 'access': {'method': 'mounted_path', 'path': str(source)}},
            'target': {'kind': 'file', 'format': 'pmtiles', 'name': target.name, 'write_mode': 'create',
                       'access': {'method': 'mounted_path', 'path': str(target)}}}
    tile = {'min_zoom': 2, 'max_zoom': 2, 'extent': [116.4, 39.9, 116.4, 39.9], 'extent_srid': 4326, 'source_srs': 'EPSG:4326'}
    result = vector_to_pmtiles(plan, tile, {'layer_name': 'parks', 'attributes': ['类别', 'count']})
    assert result['archive_format'] == 'pmtiles'
    assert target.read_bytes().startswith(b'PMTiles\x03')
    dataset = gdal.OpenEx(str(target), gdal.OF_VECTOR)
    layer = dataset.GetLayer(0)
    feature = next(iter(layer))
    assert feature.GetField('类别') == '公园'
    assert feature.GetField('count') == 7
    assert layer.GetLayerDefn().GetFieldIndex('private_note') < 0
    dataset = None
    before = target.read_bytes()
    with pytest.raises(ValueError, match='already exists'):
        vector_to_pmtiles(plan, tile, {'attributes': []})
    assert target.read_bytes() == before
    with pytest.raises(ValueError, match='field'):
        vector_to_pmtiles(plan, tile, {'attributes': ['missing']})
    with pytest.raises(ValueError, match='attributes'):
        vector_to_pmtiles(plan, tile, {'attributes': '*'})
    with pytest.raises(ValueError, match='zoom'):
        vector_to_pmtiles(plan, {**tile, 'max_zoom': -1})
    with pytest.raises(ValueError, match='CRS'):
        vector_to_pmtiles(plan, {**tile, 'source_srs': 'EPSG:3857'})
    plan['target']['write_mode'] = 'replace'
    vector_to_pmtiles(plan, tile, {'layer_name': 'geometry_only'})
    dataset = gdal.OpenEx(str(target), gdal.OF_VECTOR)
    layer = dataset.GetLayer(0)
    assert next(iter(layer)).GetGeometryRef() is not None
    assert layer.GetLayerDefn().GetFieldIndex('count') < 0
    assert layer.GetLayerDefn().GetFieldIndex('OGR_GEOMETRY') < 0
    assert all(path.read_bytes() == content for path, content in source_bytes.items())
    assert work_dirs and all(not directory.exists() for directory in work_dirs)


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
    assert _build_vector_tile_ogr_sql("规划用地") == 'SELECT "_ogr_geometry_" FROM "规划用地"'
    assert _build_vector_tile_ogr_sql('a"b', 'SmID') == 'SELECT "_ogr_geometry_", "SmID" FROM "a""b"'


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


@pytest.mark.parametrize('existing_target', [False, True])
def test_translate_to_cog_virtual_target_creation_and_replacement(tmp_path, existing_target):
    from osgeo import gdal, osr
    import uuid
    gdal.UseExceptions()
    source = tmp_path / 'source.tif'
    target = '/vsimem/addp-cog-' + uuid.uuid4().hex + '.tif'
    projection = osr.SpatialReference(); projection.ImportFromEPSG(32617)
    grid = (600000, 10, 0, 4000020, 0, -10)
    dataset = gdal.GetDriverByName('GTiff').Create(str(source), 32, 16, 1, gdal.GDT_Byte)
    dataset.SetGeoTransform(grid); dataset.SetProjection(projection.ExportToWkt())
    dataset.GetRasterBand(1).Fill(7); dataset = None
    try:
        if existing_target:
            dataset = gdal.GetDriverByName('GTiff').Create(target, 2, 2, 1, gdal.GDT_Byte)
            dataset.GetRasterBand(1).Fill(1); dataset = None
        _translate_to_cog(str(source), target, {}, {'blocksize': 128, 'num_threads': 1})
        dataset = gdal.Open(target)
        assert (dataset.RasterXSize, dataset.RasterYSize, dataset.RasterCount) == (32, 16, 1)
        assert dataset.GetGeoTransform() == grid
        assert dataset.GetSpatialRef().IsSame(projection)
        assert dataset.GetRasterBand(1).ReadRaster() == bytes([7]) * (32 * 16)
        assert dataset.GetMetadata('IMAGE_STRUCTURE')['LAYOUT'] == 'COG'
        dataset = None
    finally:
        dataset = None
        if gdal.VSIStatL(target) is not None:
            gdal.Unlink(target)


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
            class Dataset:
                def FlushCache(self): pass
            return Dataset()

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


def test_crs_to_projjson_accepts_postgis_wkt1_projected_axis_order():
    source_wkt = CRS.from_epsg(4549).to_wkt(version="WKT1_GDAL")

    result = crs_to_projjson("EPSG:4549", "wkt", source_wkt)
    projjson = json.loads(result["definition"])

    assert projjson["id"] == {"authority": "EPSG", "code": 4549}
    assert [axis["abbreviation"] for axis in projjson["coordinate_system"]["axis"]] == ["X", "Y"]


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
    assert by_name["raster_to_cog"]["effects"] == ["read", "write"]
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


def test_vector_to_pmtiles_uses_source_env_for_ogr_and_target_access_for_publish(monkeypatch):
    import operators.vector_tile_operators as module
    command_envs, published = [], []
    def run(args, extra_env=None, emit=None, min_zoom=0, max_zoom=0):
        command_envs.append(extra_env)
        assert args[args.index('-sql')+1] == 'SELECT "_ogr_geometry_" FROM "规划用地"'
        output_dir = Path(args[args.index('-f')+2])
        min_x, min_y, _, _ = module._tile_bounds([110,20,110.1,20.1],9)
        tile_dir = output_dir / '9' / str(min_x)
        tile_dir.mkdir(parents=True)
        (tile_dir / f'{min_y}.mvt').write_bytes(b'\x1f\x8bcompressed-mvt')
    def publish(path, plan):
        published.append((path.read_bytes(), plan['target']['access']))
    monkeypatch.setattr(module, '_run_command_with_gdal_progress', run)
    monkeypatch.setattr(module, '_vector_source_facts', lambda *args: ('规划用地', {}, '_ogr_geometry_'))
    monkeypatch.setattr(module, 'publish_target_file', publish)
    plan = {'schema_version':'addp.workflow.access-plan/v1',
        'source': {'kind':'file','format':'shapefile','access': {'method':'object_store','endpoint':'source-minio:9000','bucket':'addp','object':'gis/规划用地.shp','access_key':'source-ak','secret_key':'source-sk'}},
        'target': {'kind':'file','format':'pmtiles','name':'result.pmtiles','write_mode':'create','access': {'method':'object_store','endpoint':'target-minio:9000','bucket':'results','object':'result.pmtiles','access_key':'target-ak','secret_key':'target-sk'}}}
    result = vector_to_pmtiles(plan, {'min_zoom':9,'max_zoom':9,'extent':[110,20,110.1,20.1],'extent_srid':4326,'source_srs':'EPSG:4326'})
    assert result['archive_format'] == 'pmtiles'
    assert command_envs[0]['AWS_ACCESS_KEY_ID'] == 'source-ak'
    assert command_envs[0]['AWS_S3_ENDPOINT'] == 'source-minio:9000'
    assert published[0][0].startswith(b'PMTiles\x03')
    assert published[0][1]['access_key'] == 'target-ak'
    with pytest.raises(ValueError, match='schema_version'):
        vector_to_pmtiles({'source':{'root_uri':'/legacy'},'target':{'archive_uri':'/legacy'}}, {})


def test_vector_to_pmtiles_indexes_gdal_pbf_output(tmp_path):
    tile_path = tmp_path / "18" / "219155" / "107332.pbf"
    tile_path.parent.mkdir(parents=True)
    tile_path.write_bytes(b"tile")
    (tmp_path / "metadata.json").write_text("{}", encoding="utf-8")

    indexed = _index_generated_tiles(tmp_path)

    assert indexed[(18, 219155, 107332)] == tile_path


def test_workflow_categories_have_one_explicit_two_level_owner():
    expected_groups = [
        (("通用处理", "数据读写"), "load save"),
        (("通用处理", "字段处理"), "add_field calculate_field rename_fields drop_fields type_cast"),
        (("通用处理", "数据清洗"), "fill_null drop_duplicates"),
        (("通用处理", "数据筛选"), "filter_by_attribute sort_by_field select_top_n sample random_split"),
        (("通用处理", "数值统计"), "normalize_field encode_categorical bin_field aggregate"),
        (("矢量处理", "几何构造"), "buffer centroid representative_point convex_hull envelope minimum_rotated_rectangle voronoi batch_buffer"),
        (("矢量处理", "几何整理"), "simplify dissolve check_geometry repair_geometry snap_geometry snap_line_endpoints extend_line_endpoints trim_line_endpoints split_multipart extract_vertices"),
        (("矢量处理", "拓扑检查"), "check_polygon_overlap check_polygon_gap check_dangling_points check_line_proximity check_line_overlap check_line_self_overlap check_line_intersection check_line_self_intersection"),
        (("矢量处理", "线网处理"), "polygonize extract_polygon_rings split_line_intersections group_line_components merge_line_chains normalize_line_network extract_line_nodes"),
        (("矢量处理", "叠加分析"), "intersection union difference clip"),
        (("矢量处理", "空间关系"), "contains intersects distance_to"),
        (("矢量处理", "几何量测"), "get_area get_length get_bounds"),
        (("矢量处理", "要素筛选"), "filter_by_geometry_type drop_null_geometry split_by_area"),
        (("矢量处理", "坐标转换"), "vector_reproject"),
        (("矢量处理", "格式转换"), "load_from_wkt export_to_wkt"),
        (("矢量处理", "成果生成"), "vector_to_pmtiles"),
        (("栅格处理", "数据读写"), "raster_load raster_save raster_save_tiles"),
        (("栅格处理", "网格处理"), "raster_reproject raster_resample raster_align"),
        (("栅格处理", "范围处理"), "raster_clip raster_mosaic raster_update raster_tile raster_footprint"),
        (("栅格处理", "波段处理"), "raster_stack raster_select_bands"),
        (("栅格处理", "基础计算"), "raster_band_math raster_aggregate raster_proximity raster_interpolate_idw raster_point_density raster_line_density"),
        (("栅格处理", "分类处理"), "raster_reclassify raster_nibble raster_boundary_clean raster_sieve raster_region_group raster_thin"),
        (("栅格处理", "地形分析"), "raster_slope raster_aspect raster_hillshade raster_curvature raster_tpi raster_tri raster_roughness raster_twi raster_spi raster_contour"),
        (("栅格处理", "水流分析"), "raster_fill_depressions raster_flow_direction_d8 raster_flow_accumulation_d8 raster_flow_accumulation_weighted_d8 raster_flow_length_d8"),
        (("栅格处理", "汇水分析"), "raster_drainage_basins_d8 raster_snap_pour_points raster_watershed_d8 raster_subcatchments_d8"),
        (("栅格处理", "河网分析"), "raster_stream_order_d8 raster_stream_link_d8 raster_stream_to_vector_d8 raster_distance_to_stream_d8 raster_hand_d8"),
        (("栅格处理", "格式转换"), "raster_polygonize vector_rasterize"),
        (("栅格处理", "栅格统计"), "raster_statistics raster_cut_fill raster_surface_area raster_zonal_statistics raster_focal_statistics raster_vector_zonal_statistics raster_sample_points raster_sample_lines raster_recommend_band_combination raster_histogram"),
        (("栅格处理", "栅格检查"), "raster_info validate_cog"),
        (("栅格处理", "成果生成"), "raster_to_cog raster_build_overviews build_raster_mosaic"),
    ]
    expected = {name: list(path) for path, names in expected_groups for name in names.split()}
    workflow_operators = [operator for operator in list_operators() if "workflow" in operator["execution_modes"]]
    assert len(expected) == len(workflow_operators)
    assert {operator["name"]: operator["category_path"] for operator in workflow_operators} == expected
    assert list(dict.fromkeys(tuple(operator["category_path"]) for operator in workflow_operators)) == [
        path for path, _ in expected_groups
    ]
    for operator in list_operators():
        path = operator["category_path"]
        assert len(path) == 2
        assert all(len(name) == 4 for name in path)
        assert operator["category"] == path[-1]
    assert "crs_to_projjson" not in expected
    # The maintained catalogue must list every workflow operator exactly once,
    # with the same ownership and counts as discovery; stale copied totals fail.
    import re
    readme = (Path(__file__).parent / "operators" / "README.md").read_text()
    documented = {}
    for line in readme.splitlines():
        columns = [column.strip() for column in line.split("|")]
        if len(columns) != 6 or columns[1] not in ("通用处理", "矢量处理", "栅格处理"):
            continue
        names = re.findall(r"`([^`]+)`", columns[4])
        assert int(columns[3]) == len(names)
        for name in names:
            assert name not in documented
            documented[name] = [columns[1], columns[2]]
    assert documented == expected


def test_d8_stream_order_exposes_one_method_selector_and_keeps_strahler_default():
    operators={item['id']:item for item in list_operators()}
    spec=operators['raster_stream_order_d8'];params={p['name']:p for p in spec['parameters']}
    assert spec['description']=='D8 河流分级'
    assert params['method']['type']=='string' and params['method']['param_type']=='param'
    assert params['method']['required'] is False and params['method']['default']=='strahler'
    assert params['method']['enum']==['strahler','shreve']
    assert spec['detailed_description']['workflow_example']['params']['method']=='strahler'
    assert 'raster_stream_shreve_d8' not in operators and 'raster_shreve_d8' not in operators
    assert 'method' not in {p['name'] for p in operators['raster_stream_link_d8']['parameters']}


def test_d8_stream_vector_single_route_two_explicit_inputs_and_vector_output():
    spec=next(item for item in list_operators() if item['id']=='raster_stream_to_vector_d8')
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read']
    assert spec['attributes']['resource_groups']==['raster'] and spec['category_path']==['栅格处理','河网分析']
    params={p['name']:p for p in spec['parameters']}
    assert set(params)=={'input_raster','stream_raster','band','stream_band'}
    assert all(p['required'] for p in params.values())
    assert all(params[name]['type']=='raster' and params[name]['param_type']=='input' for name in ('input_raster','stream_raster'))
    assert all(params[name]['type']=='integer' for name in ('band','stream_band'))
    assert spec['output_ports'][0]['type']=='GeoDataFrame'
    assert spec['detailed_description']['workflow_example']['params']=={'input_raster':{'$ref':'source'},'stream_raster':{'$ref':'streams'},'band':1,'stream_band':1}


def test_d8_accumulation_exposes_one_measure_selector_and_keeps_count_default():
    operators = {item['id']: item for item in list_operators()}
    spec = operators['raster_flow_accumulation_d8']
    params = {p['name']: p for p in spec['parameters']}
    assert set(params) == {'input_raster', 'band', 'measure'}
    assert params['measure']['type'] == 'string' and params['measure']['param_type'] == 'param'
    assert params['measure']['required'] is False and params['measure']['default'] == 'count'
    assert params['measure']['enum'] == ['count', 'area']
    assert spec['detailed_description']['workflow_example']['params']['measure'] == 'count'
    assert spec['execution_modes'] == ['workflow'] and spec['effects'] == ['read']
    assert not {'raster_flow_area_d8', 'raster_flow_accumulation_area_d8'} & operators.keys()
    assert 'measure' not in {p['name'] for p in operators['raster_flow_accumulation_weighted_d8']['parameters']}


def test_d8_distance_to_stream_explicit_two_inputs_and_metre_output_metadata():
    spec = next(item for item in list_operators() if item['id'] == 'raster_distance_to_stream_d8')
    assert spec['description'] == 'D8 到河网沿流距离'
    assert spec['execution_modes'] == ['workflow'] and spec['effects'] == ['read']
    assert spec['attributes']['resource_groups'] == ['raster'] and spec['category_path'] == ['栅格处理', '河网分析']
    params = {p['name']: p for p in spec['parameters']}
    assert set(params) == {'input_raster', 'stream_raster', 'band', 'stream_band'}
    assert all(p['required'] for p in params.values())
    assert all(params[n]['type'] == 'raster' and params[n]['param_type'] == 'input' for n in ('input_raster', 'stream_raster'))
    assert all(params[n]['type'] == 'integer' and params[n]['param_type'] == 'param' for n in ('band', 'stream_band'))
    assert len(spec['output_ports']) == 1 and spec['output_ports'][0]['name'] == 'default' and spec['output_ports'][0]['type'] == 'raster'
    assert spec['detailed_description']['workflow_example']['params'] == {'input_raster': {'$ref': 'source'}, 'stream_raster': {'$ref': 'streams'}, 'band': 1, 'stream_band': 1}


def test_d8_hand_explicit_three_inputs_bands_unit_and_workflow_only_metadata():
    spec = next(item for item in list_operators() if item['id'] == 'raster_hand_d8')
    assert spec['description'] == 'D8 到河网相对高程（HAND）'
    assert spec['execution_modes'] == ['workflow'] and spec['effects'] == ['read']
    assert spec['attributes']['resource_groups'] == ['raster'] and spec['category_path'] == ['栅格处理', '河网分析']
    params = {p['name']: p for p in spec['parameters']}
    assert set(params) == {'input_raster', 'stream_raster', 'elevation_raster', 'band', 'stream_band', 'elevation_band', 'elevation_unit'}
    assert all(p['required'] for p in params.values())
    assert all(params[n]['type'] == 'raster' and params[n]['param_type'] == 'input' for n in ('input_raster', 'stream_raster', 'elevation_raster'))
    assert all(params[n]['type'] == 'integer' and params[n]['param_type'] == 'param' for n in ('band', 'stream_band', 'elevation_band'))
    assert params['elevation_unit']['enum'] == ['metre', 'foot', 'us_survey_foot']
    assert len(spec['output_ports']) == 1 and spec['output_ports'][0]['type'] == 'raster'
    assert spec['detailed_description']['workflow_example']['params'] == {'input_raster': {'$ref': 'source'}, 'stream_raster': {'$ref': 'streams'}, 'elevation_raster': {'$ref': 'elevation'}, 'band': 1, 'stream_band': 1, 'elevation_band': 1, 'elevation_unit': 'metre'}


def test_dem_curvature_single_workflow_explicit_band_unit_and_four_methods_metadata():
    spec = next(item for item in list_operators() if item['id'] == 'raster_curvature')
    assert spec['description'] == 'DEM 曲率'
    assert spec['execution_modes'] == ['workflow'] and spec['effects'] == ['read']
    assert spec['attributes']['resource_groups'] == ['raster'] and spec['category_path'] == ['栅格处理', '地形分析']
    params = {p['name']: p for p in spec['parameters']}
    assert set(params) == {'input_raster', 'band', 'elevation_unit', 'method'}
    assert all(params[n]['required'] for n in ('input_raster', 'band', 'elevation_unit'))
    assert not params['method']['required'] and params['method']['default'] == 'mean'
    assert params['method']['enum'] == ['mean', 'profile', 'tangential', 'plan']
    assert '其他零梯度NoData' in params['method']['description']
    assert params['input_raster']['param_type'] == 'input' and params['input_raster']['type'] == 'raster'
    assert params['band']['param_type'] == 'param' and params['band']['type'] == 'integer'
    assert params['elevation_unit']['enum'] == ['metre', 'foot', 'us_survey_foot']
    assert len(spec['output_ports']) == 1 and spec['output_ports'][0]['type'] == 'raster'
    assert spec['detailed_description']['workflow_example']['params'] == {'input_raster': {'$ref': 'source'}, 'elevation_unit': 'metre', 'band': 1, 'method': 'mean'}


def test_dem_tpi_single_workflow_explicit_band_unit_metres_and_complete_window_metadata():
    spec=next(item for item in list_operators() if item['id']=='raster_tpi')
    assert spec['description']=='DEM 地形位置指数'
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read']
    assert spec['attributes']['resource_groups']==['raster'] and spec['category_path']==['栅格处理','地形分析']
    params={p['name']:p for p in spec['parameters']}
    assert set(params)=={'input_raster','band','elevation_unit'} and all(p['required'] for p in params.values())
    assert params['input_raster']['param_type']=='input' and params['input_raster']['type']=='raster'
    assert params['band']['type']=='integer' and params['elevation_unit']['enum']==['metre','foot','us_survey_foot']
    assert '周围8点' in params['band']['description'] and '合法零保留' in params['elevation_unit']['description']
    assert len(spec['output_ports'])==1 and spec['output_ports'][0]['type']=='raster'
    assert spec['detailed_description']['workflow_example']['params']=={'input_raster':{'$ref':'source'},'elevation_unit':'metre','band':1}


def test_dem_tri_single_workflow_explicit_band_unit_metres_and_complete_window_metadata():
    spec=next(item for item in list_operators() if item['id']=='raster_tri')
    assert spec['description']=='DEM 地形崎岖度'
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read']
    assert spec['attributes']['resource_groups']==['raster'] and spec['category_path']==['栅格处理','地形分析']
    params={p['name']:p for p in spec['parameters']}
    assert set(params)=={'input_raster','band','elevation_unit'} and all(p['required'] for p in params.values())
    assert params['input_raster']['param_type']=='input' and params['input_raster']['type']=='raster'
    assert params['band']['type']=='integer' and params['elevation_unit']['enum']==['metre','foot','us_survey_foot']
    assert 'Riley' in params['band']['description'] and '不除以8' in params['band']['description'] and '合法零保留' in params['elevation_unit']['description']
    assert len(spec['output_ports'])==1 and spec['output_ports'][0]['type']=='raster'
    assert spec['detailed_description']['workflow_example']['params']=={'input_raster':{'$ref':'source'},'elevation_unit':'metre','band':1}


def test_vector_geometry_groups_have_expected_counts_and_clear_display_names():
    expected = {'几何构造': 8, '几何整理': 10, '拓扑检查': 8, '线网处理': 7}
    operators = list_operators()
    moved = [op for op in operators if op['category_path'][0] == '矢量处理' and op['category'] in expected]
    assert len(moved) == len({op['id'] for op in moved}) == 33
    assert {group: sum(op['category'] == group for op in moved) for group in expected} == expected
    assert not any(op['category_path'] == ['矢量处理', '几何处理'] for op in operators)
    names = {op['id']: op['display_name'] for op in moved}
    assert {key: names[key] for key in ('batch_buffer', 'envelope', 'extract_polygon_rings')} == {
        'batch_buffer': '多距缓冲', 'envelope': '外包矩形', 'extract_polygon_rings': '面界提取',
    }


def test_distance_to_metadata_requires_shared_projected_crs_and_native_distance_units():
    spec = next(item for item in list_operators() if item['name'] == 'distance_to')
    params = {param['name']: param for param in spec['parameters']}
    assert set(params) == {'input_gdf', 'target_geom'}
    assert '相同的已知投影坐标系' in params['target_geom']['notes']
    notes = ' '.join(spec['detailed_description']['notes'])
    assert '英尺制为对应英尺' in notes and '不自动换算米' in notes
    assert '未知、地理、地心或不匹配坐标系明确拒绝' in notes
    assert '投影坐标系为米' not in notes


def test_dem_roughness_single_workflow_explicit_band_unit_metres_and_complete_window_metadata():
    spec=next(item for item in list_operators() if item['id']=='raster_roughness')
    assert spec['description']=='DEM 地形粗糙度'
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read']
    assert spec['attributes']['resource_groups']==['raster'] and spec['category_path']==['栅格处理','地形分析']
    params={p['name']:p for p in spec['parameters']}
    assert set(params)=={'input_raster','band','elevation_unit'} and all(p['required'] for p in params.values())
    assert params['input_raster']['param_type']=='input' and params['input_raster']['type']=='raster'
    assert params['band']['type']=='integer' and params['elevation_unit']['enum']==['metre','foot','us_survey_foot']
    assert '九点最大高程减最小高程' in params['band']['description'] and '不取半高差' in params['band']['description'] and '合法零保留' in params['elevation_unit']['description']
    assert len(spec['output_ports'])==1 and spec['output_ports'][0]['type']=='raster'
    assert spec['detailed_description']['workflow_example']['params']=={'input_raster':{'$ref':'source'},'elevation_unit':'metre','band':1}


def test_d8_flow_length_has_one_public_id_and_required_explicit_method():
    operators = {item['id']: item for item in list_operators()}
    assert 'raster_flow_length_upstream_d8' not in operators
    spec = operators['raster_flow_length_d8']
    assert spec['display_name'] == 'D8流长'
    assert spec['category_path'] == ['栅格处理', '水流分析']
    assert spec['execution_modes'] == ['workflow'] and spec['effects'] == ['read']
    params = {item['name']: item for item in spec['parameters']}
    assert set(params) == {'input_raster', 'band', 'method'}
    assert all(item['required'] for item in params.values())
    assert params['method']['enum'] == ['downstream', 'upstream_longest'] and 'default' not in params['method']
    assert spec['detailed_description']['workflow_example']['params']['method'] == 'downstream'


def test_multi_distance_buffer_metadata_matches_single_layer_contract_and_removed_id():
    specs = {item['id']: item for item in list_operators()}
    assert 'batch_centroid' not in specs
    spec = specs['batch_buffer']
    assert spec['execution_modes'] == ['workflow'] and spec['effects'] == ['read']
    assert spec['category_path'] == ['矢量处理', '几何构造']
    params = {item['name']: item for item in spec['parameters']}
    assert set(params) == {'input_gdf', 'distances', 'resolution'}
    assert params['input_gdf']['param_type'] == 'input'
    assert params['distances']['type'] == 'array' and params['distances']['item_type'] == 'float'
    assert params['distances']['required']
    assert params['resolution']['type'] == 'integer' and params['resolution']['default'] == 16
    assert [(port['name'], port['type'], port['is_default']) for port in spec['output_ports']] == [('default', 'geodataframe', True)]
    notes = ' '.join(spec['detailed_description']['notes'])
    assert 'distance' in notes and '重名' in notes and '完整缓冲区' in notes
    assert 'list[GeoDataFrame]' not in str(spec['detailed_description'])
    assert '更高效' not in str(spec['detailed_description'])


def test_dem_surface_area_one_plain_table_explicit_input_no_raster_artifact():
    spec=next(item for item in list_operators()if item['id']=='raster_surface_area')
    assert spec['display_name']=='DEM 三维表面积统计'and spec['category_path']==['栅格处理','栅格统计']
    assert spec['execution_modes']==['workflow']and spec['effects']==['read']and spec['attributes']['resource_groups']==['raster']
    params={p['name']:p for p in spec['parameters']};assert set(params)=={'input_raster','band','elevation_unit'}and all(p['required']for p in params.values())
    assert params['input_raster']['type']=='raster'and params['input_raster']['param_type']=='input'
    assert params['elevation_unit']['enum']==['metre','foot','us_survey_foot']
    assert spec['output_ports'][0]['type']=='DataFrame'and len(spec['output_ports'])==1
    assert '外缘不补面积'in params['band']['description']and '双对角'in params['elevation_unit']['description']
    assert spec['detailed_description']['workflow_example']['params']=={'input_raster':{'$ref':'source'},'elevation_unit':'metre','band':1}


def test_twi_single_workflow_entry_two_explicit_roles_and_units():
    spec=next(item for item in list_operators() if item['id']=='raster_twi')
    assert spec['category_path']==['栅格处理','地形分析']
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read']
    assert spec['attributes']['resource_groups']==['raster']
    params={p['name']:p for p in spec['parameters']}
    assert set(params)=={'input_raster','slope_raster','band','slope_band','slope_unit'}
    assert all(p['required'] for p in params.values())
    assert all(params[n]['type']=='raster' and params[n]['param_type']=='input' for n in ('input_raster','slope_raster'))
    assert params['slope_unit']['enum']==['degree','percent']
    assert spec['output_ports'][0]['type']=='raster' and len(spec['output_ports'])==1
    assert spec['detailed_description']['workflow_example']['params']=={'input_raster':{'$ref':'source'},'slope_raster':{'$ref':'slope'},'band':1,'slope_band':1,'slope_unit':'degree'}


def test_focal_median_keeps_one_entry_required_parameters_old_methods_and_example():
    from operators import list_operators
    specs=[s for s in list_operators()if s['id']=='raster_focal_statistics'];assert len(specs)==1;spec=specs[0]
    params={p['name']:p for p in spec['parameters']}
    assert set(params)=={'input_raster','band','window_type','window_size','radius_metres','method','percentiles','weights'}and all(params[n]['required']for n in ('band','method'))
    assert not params['window_size']['required'] and params['window_size']['show_when']=={'window_type':['square_pixels']}
    assert all(v in params['method']['description']for v in ('sum','mean','min','max','stddev','mode','median','中位数','偶数'))
    assert spec['execution_modes']==['workflow']and spec['effects']==['read']and spec['attributes']['resource_groups']==['raster']
    assert spec['output_ports'][0]['type']=='raster'and len(spec['output_ports'])==1
    assert spec['detailed_description']['workflow_example']['params']=={'input_raster':{'$ref':'source'},'band':1,'window_size':3,'method':'mean'}


def test_focal_percentile_metadata_uses_existing_entry_conditional_parameter_and_linear_method():
    from operators import list_operators
    specs=[s for s in list_operators() if s['id']=='raster_focal_statistics']
    assert len(specs)==1
    spec=specs[0];params={p['name']:p for p in spec['parameters']}
    assert params['method']['enum']==['sum','mean','min','max','stddev','mode','median','percentile','variety','weighted_mean','weighted_stddev','weighted_sum']
    assert params['percentiles']['type']=='array' and params['percentiles']['item_type']=='float' and not params['percentiles']['required']
    assert 'default' not in params['percentiles']
    assert params['percentiles']['show_when']=={'method':['percentile']}
    assert all(text in params['percentiles']['description'] for text in ('0','100','显式','50','median'))
    assert '线性' in params['method']['description']
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read']
    assert spec['attributes']['resource_groups']==['raster'] and len(spec['output_ports'])==1


def test_full_percentile_keeps_single_statistics_object_entry_and_optional_parameter():
    from operators import list_operators
    specs=[spec for spec in list_operators() if spec['id']=='raster_statistics']
    assert len(specs)==1
    spec=specs[0]
    params={param['name']:param for param in spec['parameters']}
    assert set(params)=={'input_raster','band','percentiles'}
    assert params['percentiles']['type']=='array' and params['percentiles']['item_type']=='float'
    assert not params['percentiles']['required']
    assert 'default' not in params['percentiles']
    assert all(word in params['percentiles']['description'] for word in ('0','100','50','线性','原统计'))
    assert len(spec['output_ports'])==1 and spec['output_ports'][0]['type']=='object'
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read']
    assert spec['attributes']['resource_groups']==['raster'] and len(spec['output_ports'])==1


def test_zonal_percentile_keeps_single_table_entry_optional_parameter_and_original_inputs():
    from operators import list_operators
    specs = [spec for spec in list_operators() if spec['id'] == 'raster_zonal_statistics']
    assert len(specs) == 1
    spec = specs[0]; params = {param['name']: param for param in spec['parameters']}
    assert set(params) == {'input_raster', 'zones_raster', 'band', 'zones_band', 'percentiles', 'weights_raster', 'weights_band', 'include_weighted_stddev','method'}
    assert all(params[name]['required'] for name in ('input_raster', 'zones_raster', 'band', 'zones_band'))
    assert params['percentiles']['type'] == 'array' and params['percentiles']['item_type'] == 'float' and not params['percentiles']['required']
    assert 'default' not in params['percentiles']
    assert all(word in params['percentiles']['description'] for word in ('0', '100', '50', '线性', '原统计'))
    assert spec['execution_modes'] == ['workflow'] and spec['effects'] == ['read']
    assert spec['attributes']['resource_groups'] == ['raster']
    assert len(spec['output_ports']) == 1 and spec['output_ports'][0]['type'] == 'DataFrame'


def test_vector_percentile_single_existing_entry_keeps_coverage_prefix_and_optional_parameter():
    from operators import list_operators
    specs = [spec for spec in list_operators() if spec['id']=='raster_vector_zonal_statistics']
    assert len(specs)==1
    spec = specs[0]; params = {item['name']:item for item in spec['parameters']}
    assert set(params)=={'input_raster','input_gdf','band','all_touched','field_prefix','percentiles','weights_raster','weights_band','include_weighted_stddev','include_area_weighted','include_area_weighted_stddev','method','id_field'}
    assert params['band']['required'] and params['input_gdf']['required']
    assert params['all_touched']['default'] is False and params['field_prefix']['default']=='zonal_'
    assert params['percentiles']['type']=='array' and params['percentiles']['item_type']=='float' and not params['percentiles']['required'] and 'default' not in params['percentiles']
    assert all(word in params['percentiles']['description'] for word in ('0','100','50','逐面','线性','原统计'))
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read']
    assert spec['output_ports'][0]['type']=='GeoDataFrame' and spec['attributes']['resource_groups']==['raster']


def test_focal_variety_single_existing_entry_keeps_old_eight_methods_and_port():
    specs=[spec for spec in list_operators() if spec['id']=='raster_focal_statistics']
    assert len(specs)==1;spec=specs[0];params={p['name']:p for p in spec['parameters']}
    assert params['method']['enum']==['sum','mean','min','max','stddev','mode','median','percentile','variety','weighted_mean','weighted_stddev','weighted_sum']
    assert '分类种类数' in params['method']['description'] and '去重' in params['method']['description']
    assert set(params)=={'input_raster','band','window_type','window_size','radius_metres','method','percentiles','weights'}
    assert all(params[key]['required'] for key in ('band','method'))
    assert not params['window_size']['required'] and params['window_size']['show_when']=={'window_type':['square_pixels']}
    assert params['percentiles']['show_when']=={'method':['percentile']}
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read'] and spec['attributes']['resource_groups']==['raster']
    assert len(spec['output_ports'])==1 and spec['output_ports'][0]['type']=='raster'
    assert spec['detailed_description']['workflow_example']['params']['method']=='mean'
    assert not any(spec['id'] in ('raster_variety','raster_focal_variety') for spec in list_operators())


def test_sieve_protected_values_is_one_optional_integer_array_without_new_entry():
    operators=list_operators();matches=[o for o in operators if o['id']=='raster_sieve'];assert len(matches)==1
    spec=matches[0];params={p['name']:p for p in spec['parameters']}
    assert params['protected_values']['type']=='array' and params['protected_values']['item_type']=='integer'
    assert params['protected_values']['required']is False and params['protected_values']['default']==[]
    assert all(params[k]['required']for k in ('band','connectivity'))
    assert all(not params[k]['required'] and 'default' not in params[k]for k in ('min_pixels','min_area_m2'))
    assert spec['category_path']==['栅格处理','分类处理']and spec['execution_modes']==['workflow']and spec['effects']==['read']
    assert spec['detailed_description']['workflow_example']['params']['protected_values']==[7]


def test_sieve_area_has_one_entry_two_optional_no_default_thresholds_and_original_example():
    matches = [operator for operator in list_operators() if operator['id'] == 'raster_sieve']
    assert len(matches) == 1
    spec = matches[0]; params = {param['name']:param for param in spec['parameters']}
    assert set(params) == {'input_raster','band','min_pixels','min_area_m2','connectivity','protected_values'}
    assert all(params[name]['required'] for name in ('input_raster','band','connectivity'))
    assert params['min_pixels']['type'] == 'integer' and params['min_area_m2']['type'] == 'float'
    assert all(not params[name]['required'] and 'default' not in params[name] for name in ('min_pixels','min_area_m2'))
    assert '必须且只能提供一个' in params['min_pixels']['description'] and '互斥' in params['min_area_m2']['description']
    example = spec['detailed_description']['workflow_example']['params']
    assert example['min_pixels'] == 10 and 'min_area_m2' not in example and example['protected_values'] == [7]
    assert spec['execution_modes'] == ['workflow'] and spec['effects'] == ['read']
    assert not any(operator['id'] in ('raster_sieve_area','raster_sieve_m2') for operator in list_operators())


def test_focal_weighted_mean_single_entry_conditional_matrix_and_old_mean_example():
    matches=[spec for spec in list_operators()if spec['id']=='raster_focal_statistics'];assert len(matches)==1
    spec=matches[0];params={param['name']:param for param in spec['parameters']}
    assert params['method']['enum']==['sum','mean','min','max','stddev','mode','median','percentile','variety','weighted_mean','weighted_stddev','weighted_sum']
    assert params['weights']['type']=='array' and params['weights']['item_type']=='array'
    assert params['weights']['show_when']=={'method':['weighted_mean','weighted_stddev','weighted_sum']}and not params['weights']['required']and 'default'not in params['weights']
    assert all(name in params['weights']['description']for name in ('3×3','5×5','非负','其他方法'))
    assert params['percentiles']['show_when']=={'method':['percentile']}
    assert spec['execution_modes']==['workflow']and spec['effects']==['read']and spec['attributes']['resource_groups']==['raster']
    assert len(spec['output_ports'])==1 and spec['output_ports'][0]['type']=='raster'
    assert spec['detailed_description']['workflow_example']['params']=={'input_raster':{'$ref':'source'},'band':1,'window_size':3,'method':'mean'}
    assert not any(operator['id']in('raster_weighted_mean','raster_focal_weighted_mean')for operator in list_operators())


def test_focal_weighted_stddev_preserves_single_entry_ten_methods_and_conditional_matrix():
    specs=[s for s in list_operators()if s['id']=='raster_focal_statistics'];assert len(specs)==1
    spec=specs[0];params={p['name']:p for p in spec['parameters']}
    assert params['method']['enum']==['sum','mean','min','max','stddev','mode','median','percentile','variety','weighted_mean','weighted_stddev','weighted_sum']
    assert '加权总体标准差'in params['method']['description']
    assert params['weights']['show_when']=={'method':['weighted_mean','weighted_stddev','weighted_sum']}and params['weights']['type']=='array'and params['weights']['item_type']=='array'
    assert not params['weights']['required']and 'default'not in params['weights']
    assert spec['execution_modes']==['workflow']and spec['effects']==['read']and spec['attributes']['resource_groups']==['raster']
    assert len(spec['output_ports'])==1 and spec['output_ports'][0]['type']=='raster'
    assert spec['detailed_description']['workflow_example']['params']['method']=='mean'
    assert not any(s['id']in ('raster_weighted_stddev','raster_focal_weighted_stddev')for s in list_operators())


def test_focal_weighted_sum_single_entry_preserves_eleven_methods_and_weight_visibility():
    specs=[s for s in list_operators()if s['id']=='raster_focal_statistics'];assert len(specs)==1
    spec=specs[0];params={p['name']:p for p in spec['parameters']}
    assert params['method']['enum']==['sum','mean','min','max','stddev','mode','median','percentile','variety','weighted_mean','weighted_stddev','weighted_sum']
    assert '加权求和'in params['method']['description']and '不归一化'in params['method']['description']
    assert params['weights']['show_when']=={'method':['weighted_mean','weighted_stddev','weighted_sum']}
    assert params['percentiles']['show_when']=={'method':['percentile']}
    assert spec['execution_modes']==['workflow']and spec['effects']==['read']and spec['attributes']['resource_groups']==['raster']
    assert len(spec['output_ports'])==1 and spec['output_ports'][0]['type']=='raster'
    assert spec['detailed_description']['workflow_example']['params']['method']=='mean'
    assert not any(s['id']in ('raster_weighted_sum','raster_focal_weighted_sum')for s in list_operators())


def test_zonal_weighted_single_table_entry_optional_pair_and_original_example():
    specs=[s for s in list_operators()if s['id']=='raster_zonal_statistics'];assert len(specs)==1
    spec=specs[0];params={p['name']:p for p in spec['parameters']}
    assert set(params)=={'input_raster','zones_raster','band','zones_band','percentiles','weights_raster','weights_band','include_weighted_stddev','method'}
    assert params['weights_raster']['type']=='raster'and params['weights_raster']['param_type']=='input'and not params['weights_raster']['required']
    assert params['weights_band']['type']=='integer'and not params['weights_band']['required']
    assert 'default'not in params['weights_raster']and 'default'not in params['weights_band']
    assert '互斥'in params['weights_raster']['description']and '原统计'in params['weights_band']['description']
    assert spec['effects']==['read']and spec['execution_modes']==['workflow']and spec['attributes']['resource_groups']==['raster']
    assert len(spec['output_ports'])==1 and spec['output_ports'][0]['type']=='DataFrame'
    example=spec['detailed_description']['workflow_example']['params']
    assert 'weights_raster'not in example and 'weights_band'not in example
    assert not any(s['id']in ('raster_zonal_weighted_statistics','raster_weighted_zonal_statistics')for s in list_operators())


def test_vector_zonal_weighted_single_existing_entry_optional_pair_and_original_example():
    specs=[s for s in list_operators()if s['id']=='raster_vector_zonal_statistics'];assert len(specs)==1
    spec=specs[0];params={p['name']:p for p in spec['parameters']}
    assert set(params)=={'input_raster','input_gdf','band','all_touched','field_prefix','percentiles','weights_raster','weights_band','include_weighted_stddev','include_area_weighted','include_area_weighted_stddev','method','id_field'}
    assert params['weights_raster']['type']=='raster'and params['weights_raster']['param_type']=='input'and not params['weights_raster']['required']
    assert params['weights_band']['type']=='integer'and not params['weights_band']['required']
    assert 'default'not in params['weights_raster']and 'default'not in params['weights_band']
    assert all(word in params['weights_raster']['description']for word in ('互斥','重叠','缺失'))and'原统计'in params['weights_band']['description']
    assert params['all_touched']['default']is False and params['field_prefix']['default']=='zonal_'
    assert spec['effects']==['read']and spec['execution_modes']==['workflow']and spec['attributes']['resource_groups']==['raster']
    assert len(spec['output_ports'])==1 and spec['output_ports'][0]['type']=='GeoDataFrame'
    example=spec['detailed_description']['workflow_example']['params'];assert 'weights_raster'not in example and 'weights_band'not in example
    assert not any(s['id']in ('raster_vector_zonal_weighted_statistics','vector_zonal_weighted_statistics')for s in list_operators())


def test_zonal_weighted_stddev_single_entry_explicit_bool_defaults_and_vector_extension():
    spec=next(s for s in list_operators()if s['id']=='raster_zonal_statistics')
    params={p['name']:p for p in spec['parameters']};flag=params['include_weighted_stddev']
    assert flag['type']=='boolean'and flag['default']is False and not flag['required']
    assert all(word in flag['description']for word in ('总体','全1','null','原统计'))
    assert spec['execution_modes']==['workflow']and spec['effects']==['read']
    assert spec['output_ports'][0]['type']=='DataFrame'
    vector=next(s for s in list_operators()if s['id']=='raster_vector_zonal_statistics')
    vector_flag=next(p for p in vector['parameters']if p['name']=='include_weighted_stddev')
    assert vector_flag['type']=='boolean'and vector_flag['default']is False and not vector_flag['required']
    assert all(word in vector_flag['description']for word in ('总体','全1','null','原统计','重叠'))



def test_vector_area_weighted_single_entry_explicit_flag_preserves_original_example():
    specs=[s for s in list_operators()if s['id']=='raster_vector_zonal_statistics'];assert len(specs)==1
    spec=specs[0];params={p['name']:p for p in spec['parameters']};flag=params['include_area_weighted']
    assert flag['type']=='boolean'and flag['default']is False and not flag['required']
    assert all(word in flag['description']for word in ('平方米','原八列','all_touched','互斥'))
    assert spec['execution_modes']==['workflow']and spec['effects']==['read']
    assert spec['attributes']['resource_groups']==['raster']and spec['output_ports'][0]['type']=='GeoDataFrame'
    assert 'include_area_weighted'not in spec['detailed_description']['workflow_example']['params']
    assert not any(s['id']in ('raster_vector_area_weighted_statistics','vector_area_weighted_statistics')for s in list_operators())



def test_proximity_rectangular_single_existing_entry_preserves_contract_and_example():
    specs=[s for s in list_operators()if s['id']=='raster_proximity'];assert len(specs)==1
    spec=specs[0];params={p['name']:p for p in spec['parameters']}
    assert set(params)=={'input_raster','band','target_values'}
    assert all(word in params['target_values']['description']for word in('矩形','米制','方形结果保持'))
    assert spec['execution_modes']==['workflow']and spec['effects']==['read']and spec['attributes']['resource_groups']==['raster']
    assert len(spec['output_ports'])==1 and spec['output_ports'][0]['type']=='raster'
    assert not any(s['id']in('raster_rectangular_proximity','raster_anisotropic_proximity')for s in list_operators())


def test_vector_area_stddev_single_existing_entry_explicit_flag_default_example_unchanged():
    specs=[s for s in list_operators()if s['id']=='raster_vector_zonal_statistics'];assert len(specs)==1
    spec=specs[0];params={p['name']:p for p in spec['parameters']};flag=params['include_area_weighted_stddev']
    assert flag['type']=='boolean'and flag['default']is False and not flag['required']
    assert all(word in flag['description']for word in('面积','总体','null','单位','原十一列','重叠'))
    assert spec['execution_modes']==['workflow']and spec['effects']==['read']and spec['attributes']['resource_groups']==['raster']
    assert spec['output_ports'][0]['type']=='GeoDataFrame'and'include_area_weighted_stddev'not in spec['detailed_description']['workflow_example']['params']
    assert not any(s['id']in('raster_vector_area_weighted_stddev','vector_area_weighted_stddev')for s in list_operators())


def test_focal_circle_single_entry_conditional_windows_preserves_twelve_methods():
    specs=[s for s in list_operators()if s['id']=='raster_focal_statistics'];assert len(specs)==1
    spec=specs[0];params={p['name']:p for p in spec['parameters']}
    assert params['window_type']['enum']==['square_pixels','circle_metres']and params['window_type']['default']=='square_pixels'
    assert params['window_type']['type']=='string'and not params['window_type']['required']
    for name,mode in [('window_size','square_pixels'),('radius_metres','circle_metres')]:
        assert not params[name]['required']and 'default'not in params[name]
        assert params[name]['show_when']=={'window_type':[mode]}
    assert params['radius_metres']['type']=='float'and '5×5'in params['radius_metres']['description']and 'epsilon'in params['radius_metres']['description']
    assert params['method']['enum']==['sum','mean','min','max','stddev','mode','median','percentile','variety','weighted_mean','weighted_stddev','weighted_sum']
    assert params['percentiles']['show_when']=={'method':['percentile']}and params['weights']['show_when']=={'method':['weighted_mean','weighted_stddev','weighted_sum']}
    assert spec['execution_modes']==['workflow']and spec['effects']==['read']and len(spec['output_ports'])==1
    assert spec['detailed_description']['workflow_example']['params']=={'input_raster':{'$ref':'source'},'band':1,'window_size':3,'method':'mean'}
    assert not any(s['id']in('raster_focal_circle','raster_circle_statistics')for s in list_operators())


def test_region_group_single_workflow_entry_explicit_params_example_and_shared_core():
    from pathlib import Path
    specs=[s for s in list_operators()if s['id']=='raster_region_group'];assert len(specs)==1
    spec=specs[0];params={p['name']:p for p in spec['parameters']}
    assert set(params)=={'input_raster','band','connectivity'}and all(p['required']for p in params.values())
    assert params['connectivity']['type']=='integer'and 'enum'not in params['connectivity']and 'default'not in params['connectivity']
    assert '4或8'in params['connectivity']['description']
    assert spec['category_path']==['栅格处理','分类处理']and spec['execution_modes']==['workflow']and spec['effects']==['read']
    assert spec['attributes']['resource_groups']==['raster']and len(spec['output_ports'])==1 and spec['output_ports'][0]['type']=='raster'
    assert spec['detailed_description']['workflow_example']['params']=={'input_raster':{'$ref':'source'},'band':1,'connectivity':4}
    assert not any(s['id']in('raster_clump','raster_region_labels')for s in list_operators())
    source=(Path(__file__).parent/'operators/raster_compute.py').read_text()
    assert 'def _sieve_components('not in source and 'def _sieve_rows('not in source
    assert source.count('with _classification_labels(dataset, band, connectivity, error=')==2


def test_sample_points_single_workflow_entry_two_inputs_typed_example_and_no_aliases():
    from operators import OPERATORS
    specs=[s for s in list_operators()if s['id']=='raster_sample_points'];assert len(specs)==1
    spec=specs[0];params={p['name']:p for p in spec['parameters']}
    assert set(params)=={'input_raster','input_gdf','band','method'}and all(params[k]['required']for k in ('input_raster','input_gdf','band'))
    assert params['method']['type']=='string'and params['method']['default']=='cell'and params['method']['enum']==['cell','bilinear']and not params['method']['required']
    assert params['input_raster']['type']=='raster'and params['input_gdf']['type']=='object'and params['band']['type']=='integer'
    assert params['input_gdf']['param_type']=='input'and OPERATORS['raster_sample_points']['param_schema'][1]['type']=='GeoDataFrame'
    assert params['input_raster']['type']!=params['input_gdf']['type']and 'default'not in params['band']
    assert spec['category_path']==['栅格处理','栅格统计']and spec['execution_modes']==['workflow']and spec['effects']==['read']
    assert spec['attributes']['resource_groups']==['raster']and len(spec['output_ports'])==1 and spec['output_ports'][0]['type']=='GeoDataFrame'
    assert spec['detailed_description']['workflow_example']['params']=={'input_raster':{'$ref':'source'},'input_gdf':{'$ref':'points'},'band':1}
    assert not any(s['id']in('sample_raster','raster_extract_values','raster_sample')for s in list_operators())


def test_difference_metadata_whole_polygon_mask_and_original_identity_only():
    spec=next(item for item in list_operators() if item['name']=='difference')
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read']
    params={p['name']:p for p in spec['parameters']}
    assert set(params)=={'input_gdf','gdf_b','id_field'} and all(p['required'] for p in params.values())
    assert params['id_field']['type']=='string' and 'default' not in params['id_field']
    assert len(spec['output_ports'])==1 and spec['output_ports'][0]['is_default']
    assert spec['output_ports'][0]['type']=='geodataframe'
    assert 'GeometryCollection' in spec['detailed_description']['overview']


def test_zonal_frequency_single_method_selector_and_ordinary_table_without_alias():
    specs=[spec for spec in list_operators()if spec['id']=='raster_zonal_statistics'];assert len(specs)==1
    spec=specs[0];params={p['name']:p for p in spec['parameters']}
    assert params['method']['type']=='string'and params['method']['default']=='summary'and params['method']['enum']==['summary','class_frequency']and not params['method']['required']
    assert spec['execution_modes']==['workflow']and spec['effects']==['read']and spec['category_path']==['栅格处理','栅格统计']and spec['attributes']['resource_groups']==['raster']
    assert len(spec['output_ports'])==1 and spec['output_ports'][0]['type']=='DataFrame'
    assert all(params[k]['required']for k in('input_raster','zones_raster','band','zones_band'))
    assert 'method'not in spec['detailed_description']['workflow_example']['params']
    assert not any(spec['id']in('raster_zonal_histogram','raster_zonal_class_counts','raster_class_frequency')for spec in list_operators())


def test_vector_clip_metadata_requires_explicit_source_and_full_geometry():
    spec=next(s for s in list_operators() if s['name']=='clip');params={p['name']:p for p in spec['parameters']}
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read']
    assert set(params)=={'input_gdf','mask_gdf','id_field'} and all(p['required'] for p in params.values())
    assert params['id_field']['type']=='string' and 'default' not in params['id_field']
    assert len(spec['output_ports'])==1 and spec['output_ports'][0]['is_default'] and spec['output_ports'][0]['type']=='geodataframe'
    assert 'GeometryCollection' in spec['detailed_description']['overview']


def test_vector_zonal_frequency_keeps_one_geo_entry_original_defaults_and_conditional_identifier():
    specs=[v for v in list_operators()if v['id']=='raster_vector_zonal_statistics'];assert len(specs)==1
    spec=specs[0];params={v['name']:v for v in spec['parameters']}
    assert params['method']['enum']==['summary','class_frequency'] and params['method']['default']=='summary'
    assert params['method']['type']=='string' and not params['method']['required']
    assert params['id_field']['type']=='string' and not params['id_field']['required']
    assert params['id_field']['show_when']=={'method':['class_frequency']}
    assert params['all_touched']['default']is False and params['field_prefix']['default']=='zonal_'
    assert spec['effects']==['read']and spec['execution_modes']==['workflow']and spec['attributes']['resource_groups']==['raster']
    assert spec['output_ports'][0]['type']=='GeoDataFrame'
    assert not any(v['id']in('raster_vector_class_frequency','vector_zonal_class_frequency')for v in list_operators())
    assert 'method'not in spec['detailed_description']['workflow_example']['params']


def test_raster_sample_lines_one_explicit_metre_sampling_entry_and_standard_native_recipe():
    from operators.raster_compute import OPERATORS
    from pathlib import Path
    specs=[v for v in list_operators()if v['id']=='raster_sample_lines'];assert len(specs)==1
    spec=specs[0];params={v['name']:v for v in spec['parameters']}
    assert set(params)=={'input_raster','input_gdf','band','id_field','spacing_metres','method'}
    assert all(params[n]['required']for n in('input_raster','input_gdf','band','id_field','spacing_metres'))
    assert params['input_gdf']['param_type']=='input'and params['input_gdf']['type']=='object'
    assert OPERATORS['raster_sample_lines']['param_schema'][1]['type']=='GeoDataFrame'
    assert params['method']['default']=='cell'and params['method']['enum']==['cell','bilinear']
    assert params['spacing_metres']['type']=='float'and spec['output_ports'][0]['type']=='GeoDataFrame'
    assert spec['effects']==['read']and spec['execution_modes']==['workflow']and spec['attributes']['resource_groups']==['raster']
    assert spec['category_path']==['栅格处理','栅格统计']
    recipe=(Path(__file__).resolve().parents[2]/'Makefile').read_text().split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]
    assert all(name in recipe for name in('test_raster_vector_zonal_frequency.py','test_raster_sample_lines.py','test_raster_compute.py','test_operator_metadata.py'))
    assert not any(v['id']in('raster_profile_lines','raster_line_profile')for v in list_operators())


def test_vector_union_metadata_is_explicit_binary_polygon_partition_without_list_alias():
    spec=next(s for s in list_operators()if s['id']=='union');params={p['name']:p for p in spec['parameters']}
    assert spec['display_name']=='联合叠加'and spec['category_path']==['矢量处理','叠加分析']
    assert set(params)=={'input_gdf','gdf_b','id_field','reference_id_field'}and all(p['required']for p in params.values())
    assert spec['execution_modes']==['workflow']and spec['effects']==['read']
    assert len(spec['output_ports'])==1 and spec['output_ports'][0]['is_default']
    assert 'gdf_list'not in spec['detailed_description']['workflow_example']['params']


def test_vector_dissolve_metadata_has_explicit_groups_and_default_plus_members():
    spec=next(s for s in list_operators()if s['id']=='dissolve');params={p['name']:p for p in spec['parameters']}
    assert set(params)=={'input_gdf','id_field','group_fields'}and all(p['required']for p in params.values())
    assert [(p['name'],p['is_default'])for p in spec['output_ports']]==[('result',True),('members',False)]
    assert spec['execution_modes']==['workflow']and spec['effects']==['read']
    assert params['group_fields']['type']=='array' and params['group_fields']['item_type']=='string'


@pytest.mark.parametrize('operator',['contains','intersects'])
def test_relation_filter_metadata_has_single_subset_output_and_no_boolean_field(operator):
    spec=next(item for item in list_operators() if item['name']==operator)
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read']
    assert {p['name']for p in spec['parameters']}=={'input_gdf','gdf_b'}
    assert all(p['required']for p in spec['parameters'])
    assert len(spec['output_ports'])==1 and spec['output_ports'][0]['type']=='geodataframe'
    assert '不新增布尔列' in spec['detailed_description']['overview']
    assert 'GeometryCollection' in spec['detailed_description']['overview']
    assert any('地理或投影' in note for note in spec['detailed_description']['notes'])


def test_idw_single_workflow_read_entry_two_sources_and_no_extra_parameters():
    from operators import list_operators
    spec=next(s for s in list_operators()if s['id']=='raster_interpolate_idw')
    params={p['name']:p for p in spec['parameters']}
    assert set(params)=={'input_gdf','reference_raster','field','radius_metres','min_points','max_points','power'}
    assert all(params[n]['required']for n in('input_gdf','reference_raster','field','radius_metres'))
    assert params['min_points']['type']=='integer'and params['min_points']['default']==1 and not params['min_points']['required']
    assert '精确重合' in params['min_points']['description']
    assert params['max_points']['type']=='integer'and not params['max_points']['required']and 'default'not in params['max_points']
    assert 'X' in params['max_points']['description']and 'Y' in params['max_points']['description']
    import inspect
    from operators import get_operator
    assert inspect.signature(get_operator('raster_interpolate_idw')).parameters['min_points'].default==1
    import operators.raster_compute as c
    assert inspect.signature(get_operator('raster_interpolate_idw')).parameters['max_points'].default is c._IDW_MAX_POINTS_UNSET
    assert params['input_gdf']['param_type']==params['reference_raster']['param_type']=='input'
    assert params['input_gdf']['type']=='object'and params['reference_raster']['type']=='raster'
    assert spec['execution_modes']==['workflow']and spec['effects']==['read']
    assert spec['category_path']==['栅格处理','基础计算']and spec['attributes']['resource_groups']==['raster']
    assert len(spec['output_ports'])==1 and spec['output_ports'][0]['type']=='raster'


    assert params['power']['type']=='integer'and params['power']['default']==2 and not params['power']['required']
    assert not params['power'].get('enum')and '1／2／3'in params['power']['description']
    assert inspect.signature(get_operator('raster_interpolate_idw')).parameters['power'].default==2

def test_voronoi_metadata_requires_station_identity_and_explicit_range_only():
    spec=next(s for s in list_operators()if s['id']=='voronoi')
    params={p['name']:p for p in spec['parameters']}
    assert set(params)=={'input_gdf','mask_gdf','id_field'}
    assert all(p['required']for p in params.values())
    assert params['input_gdf']['param_type']==params['mask_gdf']['param_type']=='input'
    assert params['id_field']['type']=='string'and 'default'not in params['id_field']
    assert spec['execution_modes']==['workflow']and spec['effects']==['read']
    assert spec['category_path']==['矢量处理','几何构造']
    assert len(spec['output_ports'])==1 and spec['output_ports'][0]['type']=='geodataframe'
    assert '范围外' in spec['detailed_description']['overview']


def test_simplify_metadata_declares_runtime_topology_default_and_source_crs_units():
    import inspect
    from operators import get_operator
    spec = next(item for item in list_operators() if item['name'] == 'simplify')
    params = {param['name']: param for param in spec['parameters']}
    assert set(params) == {'input_gdf', 'tolerance', 'preserve_topology'}
    topology = params['preserve_topology']
    assert topology['required'] is False and topology['type'] == 'boolean'
    assert topology['default'] is inspect.signature(get_operator('simplify')).parameters['preserve_topology'].default is True
    assert '投影坐标单位不固定为米' in params['tolerance']['notes']
    assert '投影坐标系单位为米' not in params['tolerance']['notes']


@pytest.mark.parametrize('operator', ['representative_point', 'centroid', 'convex_hull', 'minimum_rotated_rectangle'])
def test_enclosing_constructions_discovery_is_unique_workflow_read_and_one_geometry_input(operator):
    from operators import get_operator, OPERATORS

    operators = list_operators()
    matches = [item for item in operators if item['id'] == operator]
    assert len(matches) == 1
    spec = matches[0]
    assert spec['execution_modes'] == ['workflow'] and spec['effects'] == ['read']
    assert spec['category_path'] == ['矢量处理', '几何构造']
    params = {item['name']: item for item in spec['parameters']}
    assert set(params) == {'input_gdf'}
    assert params['input_gdf']['required'] is True and params['input_gdf']['param_type'] == 'input'
    assert params['input_gdf']['type'] == 'object'
    assert OPERATORS[operator]['param_schema'][0]['type'] == 'GeoDataFrame'
    assert 'default' not in params['input_gdf']
    assert len(spec['output_ports']) == 1
    port = spec['output_ports'][0]
    assert port['name'] == 'default' and port['type'].lower() == 'geodataframe' and port['is_default'] is True
    assert '投影' in spec['detailed_description']['overview']
    assert get_operator(operator) is not None


def test_centroid_metadata_keeps_one_id_no_weight_parameter_and_no_removed_batch_alias():
    from operators import get_operator
    operators = list_operators()
    spec = next(item for item in operators if item['id'] == 'centroid')
    assert sum(item['id'] == 'centroid' for item in operators) == 1
    assert set(item['name'] for item in spec['parameters']) == {'input_gdf'}
    assert spec['execution_modes'] == ['workflow'] and spec['effects'] == ['read']
    assert spec['category_path'] == ['矢量处理', '几何构造']
    assert [(port['name'], port['type'].lower(), port['is_default']) for port in spec['output_ports']] == [('default', 'geodataframe', True)]
    assert 'batch_centroid' not in {item['id'] for item in operators}
    with pytest.raises(ValueError, match='Unknown operator: batch_centroid'): get_operator('batch_centroid')


def test_representative_point_discovery_has_one_id_without_native_kernel_alias():
    from operators import get_operator
    operators = list_operators()
    spec = next(item for item in operators if item['id'] == 'representative_point')
    assert sum(item['id'] == 'representative_point' for item in operators) == 1
    assert set(item['name'] for item in spec['parameters']) == {'input_gdf'}
    assert spec['execution_modes'] == ['workflow'] and spec['effects'] == ['read']
    assert spec['category_path'] == ['矢量处理', '几何构造']
    assert [(port['name'], port['type'].lower(), port['is_default']) for port in spec['output_ports']] == [('default', 'geodataframe', True)]
    assert 'point_on_surface' not in {item['id'] for item in operators}
    with pytest.raises(ValueError, match='Unknown operator: point_on_surface'): get_operator('point_on_surface')


def test_axis_aligned_envelope_metadata_exposes_exact_degenerate_projected_policy():
    spec = next(item for item in list_operators() if item['id'] == 'envelope')
    assert spec['description'] == '外包矩形' and spec['execution_modes'] == ['workflow'] and spec['effects'] == ['read']
    assert [p['name'] for p in spec['parameters']] == ['input_gdf']
    assert len(spec['output_ports']) == 1 and spec['output_ports'][0]['type'] == 'geodataframe'
    text = str(spec['detailed_description'])
    for marker in ['水平', '垂直', 'LineString', 'Polygon', 'Point', '斜向共线', '投影', '不扩宽', '不按面积0']:
        assert marker in text
    assert '旧GeoSeries.envelope' not in text


def test_point_density_single_workflow_read_entry_exact_sources_radius_and_units():
    from operators import get_operator
    import inspect
    spec=next(s for s in list_operators()if s['id']=='raster_point_density')
    params={p['name']:p for p in spec['parameters']}
    assert set(params)=={'input_gdf','reference_raster','radius_metres','method','weight_field'}
    assert all(params[n]['required']for n in ('input_gdf','reference_raster','radius_metres'))
    assert params['method']['type']=='string'and params['method']['default']=='uniform'and params['method']['enum']==['uniform','quartic']and params['method']['required']is False
    assert inspect.signature(get_operator('raster_point_density')).parameters['method'].default=='uniform'
    assert len([s for s in list_operators()if s['id']=='raster_point_density'])==1
    assert params['weight_field']['type']=='string'and params['weight_field']['required']is False and 'default'not in params['weight_field']
    assert params['input_gdf']['param_type']==params['reference_raster']['param_type']=='input'
    assert params['input_gdf']['type']=='object'and params['reference_raster']['type']=='raster'
    assert params['radius_metres']['type']=='float'and 'default'not in params['radius_metres']
    assert spec['execution_modes']==['workflow']and spec['effects']==['read']
    assert spec['category_path']==['栅格处理','基础计算']and spec['attributes']['resource_groups']==['raster']
    assert len(spec['output_ports'])==1 and spec['output_ports'][0]['type']=='raster'
    assert set(inspect.signature(get_operator('raster_point_density')).parameters)==set(params)


@pytest.mark.parametrize('operator',['get_area','get_length','split_by_area'])
def test_measurement_discovery_projected_empty_zero_and_original_unit_policy(operator):
    spec=next(row for row in list_operators()if row['id']==operator)
    assert spec['execution_modes']==['workflow']and spec['effects']==['read']
    params={row['name']:row for row in spec['parameters']}
    assert set(params)==({'input_gdf','threshold'}if operator=='split_by_area'else{'input_gdf'})
    text=str(spec['detailed_description'])
    for marker in ['投影','EMPTY','area'if operator!='get_length'else'length']:
        assert marker in text
    if operator=='split_by_area':
        assert [port['name']for port in spec['output_ports']]==['large','small']
        assert 'threshold' in text and '非负' in text


def test_distance_admission_metadata_describes_empty_targets_sources_and_complete_geometries():
    spec=next(item for item in list_operators()if item['id']=='distance_to')
    assert spec['execution_modes']==['workflow']and spec['effects']==['read']
    assert [p['name']for p in spec['parameters']]==['input_gdf','target_geom']
    text=str(spec['detailed_description'])
    for marker in ['NULL','EMPTY','目标0行拒绝','空源','distance','英尺','不自动换算米','包含','不输出最近目标ID']:
        assert marker in text
    assert len(spec['output_ports'])==1


def test_line_density_single_read_workflow_sources_radius_no_optional_shortcuts():
    from operators import get_operator
    import inspect
    spec=next(s for s in list_operators() if s['id']=='raster_line_density')
    params={p['name']:p for p in spec['parameters']}
    assert set(params)=={'input_gdf','reference_raster','radius_metres','weight_field','method'}
    assert all(params[n]['required'] and 'default' not in params[n] for n in ('input_gdf','reference_raster','radius_metres'))
    assert params['weight_field']['type']=='string' and params['weight_field']['required'] is False and 'default' not in params['weight_field']
    assert params['input_gdf']['param_type']==params['reference_raster']['param_type']=='input'
    assert params['radius_metres']['type']=='float'
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read']
    assert spec['category_path']==['栅格处理','基础计算'] and spec['attributes']['resource_groups']==['raster']
    assert spec['output_ports'][0]['type']=='raster'
    assert len([s for s in list_operators() if s['id']=='raster_line_density'])==1
    assert set(inspect.signature(get_operator('raster_line_density')).parameters)==set(params)

    assert params['method']['type']=='string'and params['method']['required']is False and params['method']['default']=='uniform'and params['method']['enum']==['uniform','quartic']
    assert inspect.signature(get_operator('raster_line_density')).parameters['method'].default=='uniform'


def test_split_multipart_has_one_workflow_read_entry_with_explicit_source_and_no_recursive_alias():
    from operators import get_operator, split_multipart
    import inspect
    spec=next(item for item in list_operators()if item['id']=='split_multipart')
    assert sum(item['id']=='split_multipart'for item in list_operators())==1
    assert spec['description']=='多部拆分'and spec['category_path']==['矢量处理','几何整理']
    assert spec['execution_modes']==['workflow']and spec['effects']==['read']
    assert get_operator('split_multipart')is split_multipart
    assert set(inspect.signature(split_multipart).parameters)=={'input_gdf','id_field'}
    params={p['name']:p for p in spec['parameters']}
    assert set(params)=={'input_gdf','id_field'}and all(p['required']for p in params.values())
    assert params['input_gdf']['param_type']=='input'and params['id_field']['param_type']=='param'
    assert [(p['name'],p['type'],p['is_default'])for p in spec['output_ports']]==[('result','geodataframe',True)]
    for text in ['GeometryCollection','NULL','EMPTY','source_id','part_no','RangeIndex','孔洞']:
        assert text in str(spec['detailed_description'])
    for alias in ['explode','multipart_to_singlepart','split_geometry_collection']:
        assert alias not in {m['id']for m in list_operators()}
        with pytest.raises(ValueError,match='Unknown operator'):get_operator(alias)


def test_vertices_extraction_metadata_one_read_entry_original_occurrences_and_explicit_id():
    from operators import get_operator, extract_vertices
    import inspect
    spec=next(x for x in list_operators()if x['id']=='extract_vertices')
    assert sum(x['id']=='extract_vertices'for x in list_operators())==1
    assert get_operator('extract_vertices')is extract_vertices
    assert spec['description']=='顶点提取'and spec['category_path']==['矢量处理','几何整理']
    assert spec['execution_modes']==['workflow']and spec['effects']==['read']
    assert set(inspect.signature(extract_vertices).parameters)=={'input_gdf','id_field'}
    assert [p['name']for p in spec['parameters']]==['input_gdf','id_field']and all(p['required']for p in spec['parameters'])
    assert [(p['name'],p['type'],p['is_default'])for p in spec['output_ports']]==[('result','geodataframe',True)]
    for marker in ['闭环','重复','GeometryCollection','NULL','EMPTY','source_id','part_no','ring_no','vertex_no','vertex_id','RangeIndex']:
        assert marker in str(spec['detailed_description'])
    for alias in ['get_vertices','extract_vertex','extract_nodes']:
        assert alias not in {m['id']for m in list_operators()}
        with pytest.raises(ValueError,match='Unknown operator'):get_operator(alias)
