import re
from pathlib import Path


def _normalize_java(source: str) -> str:
    normalized = re.sub(r"\s+", " ", source)
    return re.sub(r"\(\s+", "(", normalized)


SOURCE_ROOT = Path(__file__).parent / "src/main/java"
SOURCES = {
    source.name: _normalize_java(source.read_text(encoding="utf-8"))
    for source in sorted(SOURCE_ROOT.rglob("*.java"))
}
SOURCE = "\n".join(SOURCES.values())
REGISTRY_SOURCE = SOURCES["SuperMapOperatorRegistry.java"]
S3M_SOURCE = SOURCES["SuperMapS3MConversionService.java"]
CAD_SOURCE = SOURCES["SuperMapCadService.java"]


def _open_postgis_operator_block() -> str:
    start = SOURCE.index('result.put("datasource.open_postgis"')
    end = SOURCE.index('result.put("datasource.create"', start)
    return SOURCE[start:end]


def _operator_block(operator_id: str) -> str:
    start = SOURCE.index(f'result.put("{operator_id}"')
    next_start = SOURCE.find('result.put("', start + 1)
    end = next_start if next_start != -1 else SOURCE.index("return result;", start)
    return SOURCE[start:end]


def test_open_postgis_metadata_exposes_runtime_contract_only():
    block = _open_postgis_operator_block()

    for runtime_param in ("connection_info", "schema", "table", "alias", "read_only"):
        assert f'param("{runtime_param}"' in block

    assert "resource_tree_picker" not in block
    assert 'param("locator"' not in block
    assert "uiParam(" not in block


def test_enable_postgis_metadata_is_direct_only_and_locator_free():
    start = SOURCE.index('result.put("datasource.enable_postgis"')
    end = SOURCE.index('result.put("datasource.create"', start)
    block = SOURCE[start:end]

    assert 'param("connection_info"' in block
    assert 'param("alias"' in block
    assert '"direct"' in block
    assert '"workflow"' not in block
    assert 'param("locator"' not in block


def test_upgrade_udbx_metadata_is_direct_only_and_explicit():
    block = _operator_block("datasource.upgrade_udbx")

    assert 'param("connection_info", "object", false, true' in block
    assert 'param("path", "string", false, true' in block
    assert 'param("alias", "string", false, false' in block
    assert 'output("upgrade", "supermap.udbx_upgrade"' in block
    assert 'List.of("direct")' in block
    assert '"workflow"' not in block
    assert 'param("locator"' not in block


def test_upgrade_udbx_checks_schema_and_uses_writable_sdk_open():
    assert '"SmAdditionalInfo"' in SOURCE
    assert '"SmRelationship"' in SOURCE
    assert "inspectUdbxSchema(path)" in SOURCE
    assert "boolean readOnly = before.current();" in SOURCE
    assert "context.openUdbx(path.toString(), alias, readOnly)" in SOURCE
    assert 'dependencies.set("sqlite3"' in SOURCE
    assert "sqliteCheck.available" in SOURCE
    assert 'failed("INVALID_PARAMS", ex.getMessage())' in SOURCE
    assert 'failed("EXECUTION_FAILED", "SuperMap direct operator execution failed")' in SOURCE


def test_first_batch_spatial_operators_are_registered():
    for operator_id in (
        "dataset.info",
        "vector.filter",
        "vector.buffer",
        "overlay.clip",
        "overlay.erase",
        "overlay.union",
    ):
        assert f'result.put("{operator_id}"' in SOURCE


def test_create_datasource_metadata_exposes_dynamic_storage_binding():
    block = _operator_block("datasource.create")

    assert 'param("connection_info", "object", false, true' in block
    assert 'param("path", "string", false, true' in block
    assert 'param("target_parent_locator"' not in block
    assert 'param("target_name"' not in block
    assert "resource_tree_picker" not in block


def test_dataset_info_returns_lightweight_summary():
    block = _operator_block("dataset.info")

    assert 'param("dataset", "supermap.dataset", true, true' in block
    assert 'output("info", "supermap.dataset_info"' in block
    assert 'List.of("direct")' not in block


def test_second_batch_spatial_operators_are_registered():
    for operator_id in (
        "dataset.project",
        "vector.spatial_filter",
        "vector.dissolve",
        "vector.merge",
        "vector.feature_envelope",
        "vector.inner_point",
    ):
        assert f'result.put("{operator_id}"' in SOURCE


def test_dataset_project_metadata_returns_dataset_ref():
    block = _operator_block("dataset.project")

    assert 'param("dataset", "supermap.dataset", true, true' in block
    assert 'param("output_datasource", "supermap.datasource", true, true' in block
    assert 'param("target_epsg", "integer", false, true' in block
    assert 'param("method", "string", false, false' in block
    assert 'output("result_dataset", "supermap.dataset"' in block
    assert 'List.of("direct")' not in block


def test_spatial_filter_dissolve_and_merge_return_dataset_refs():
    expected_runtime_params = {
        "vector.spatial_filter": (
            'param("input_dataset", "supermap.dataset", true, true',
            'param("filter_dataset", "supermap.dataset", true, true',
            'param("relation", "string", false, true',
        ),
        "vector.dissolve": (
            'param("input_dataset", "supermap.dataset", true, true',
            'param("field_names", "array", false, false',
            'param("dissolve_type", "string", false, false',
        ),
        "vector.merge": (
            'param("primary_dataset", "supermap.dataset", true, true',
            'param("append_dataset", "supermap.dataset", true, true',
        ),
        "vector.feature_envelope": (
            'param("input_dataset", "supermap.dataset", true, true',
        ),
        "vector.inner_point": (
            'param("input_dataset", "supermap.dataset", true, true',
        ),
    }

    for operator_id, snippets in expected_runtime_params.items():
        block = _operator_block(operator_id)

        assert 'param("output_datasource", "supermap.datasource", true, true' in block
        assert 'param("output_dataset_name", "string", false, true' in block
        assert 'output("result_dataset", "supermap.dataset"' in block
        assert 'List.of("direct")' not in block
        for snippet in snippets:
            assert snippet in block


def test_vector_filter_and_buffer_return_dataset_refs():
    for operator_id in ("vector.filter", "vector.buffer"):
        block = _operator_block(operator_id)

        assert 'param("output_datasource", "supermap.datasource", true, true' in block
        assert 'param("output_dataset_name", "string", false, true' in block
        assert 'output("result_dataset", "supermap.dataset"' in block
        assert 'List.of("direct")' not in block


def test_overlay_expansion_uses_dataset_inputs_and_outputs():
    for operator_id in ("overlay.clip", "overlay.erase", "overlay.union"):
        block = _operator_block(operator_id)

        assert "overlayParameters()" in block
        assert 'output("result_dataset", "supermap.dataset"' in block
        assert 'List.of("direct")' not in block


def test_osgb_scene_to_s3m_exposes_access_plan_and_both_execution_modes():
    block = _operator_block("osgb_scene_to_s3m")

    assert 'param("access_plan", "object", false, true' in block
    assert 'output("s3m", "supermap.s3m_dataset"' in block
    assert 'List.of("workflow", "direct")' in block
    assert '"osgb_scene_to_s3m", SuperMapS3MConversionService::convertOSGBSceneToS3M' in REGISTRY_SOURCE
    assert 'Map.entry("osgb_scene_to_s3m", (params, context) -> new OSGBSceneToS3MProcess(params))' in REGISTRY_SOURCE
    assert '"object_store".equals(targetAccess.path("method").asText())' in S3M_SOURCE
    assert 'publishDirectory(targetRoot, targetAccess)' in S3M_SOURCE
    assert "if (objectStoreTarget)" in S3M_SOURCE
    assert "deleteRecursively(targetRoot);" in S3M_SOURCE


def test_osgb_scene_to_s3m_stages_tiles_and_validates_the_published_dataset():
    conversion_start = S3M_SOURCE.index("static ObjectNode convertOSGBSceneToS3M")
    block = S3M_SOURCE[conversion_start:]

    assert "stageOSGBSceneData(" in block
    assert "validateS3MOutput(" in block
    assert "new ObliquePhotogrammetryBuilder" in block
    assert "TextureCompressType.TEXTURECOMPRESS_DXT" in block
    assert "VertexOptimizationType.VO_DRACO" in block
    assert "S3MVersion.VERSION_301" in block
    assert "CacheFileType.S3MB" in block
    assert "ObliqueProcessType.MODIFY_CENTER" not in block
    assert "normalizeS3MManifestGeoreference(" in block
    assert "CoordSysTranslator.convert(" in S3M_SOURCE
    assert "builder.setTargetPrjCoordSys" not in block
    assert 'position.put("unit", "Degree")' in S3M_SOURCE
    assert 'config.put("crs", "epsg:4326")' in S3M_SOURCE
    assert "CacheBuilderOSGBTool.osgb2s3m" not in block
    assert 'result.put("texture_compression", "dxt")' in block
    assert 'result.put("geometry_compression", "draco")' in block
    assert 'result.put("s3m_version", "3.01")' in block
    assert 'result.put("manifest_encoding", "json")' in block
    assert 'result.put("tile_extension", ".s3mb")' in block
    assert 'result.put("crs", "EPSG:4326")' in block
    assert 'result.put("root_tile_count", generatedRootTileCount)' in block
    assert 'result.put("source_root_candidate_count", rootTiles.size())' in block
    assert block.index("validateS3MOutput(") < block.index("publishDirectory(targetRoot, targetAccess)")
    stage_start = S3M_SOURCE.index("private static void stageOSGBSceneData")
    stage_end = S3M_SOURCE.index("private static int validateS3MOutput", stage_start)
    stage_block = S3M_SOURCE[stage_start:stage_end]
    assert "Files.copy(source, staged" in stage_block
    assert "Files.createSymbolicLink" not in stage_block
    assert "rewriteS3MManifestPaths" not in S3M_SOURCE
    assert "copyGeneratedS3MTiles" not in S3M_SOURCE


def test_cad_inspect_is_direct_only_and_does_not_traverse_geometry():
    block = _operator_block("cad.inspect")

    assert 'param("access_plan", "object", false, true' in block
    assert 'output("inspection", "addp.cad.inspect/v1"' in block
    assert 'List.of("direct")' in block
    assert '"cad.inspect", SuperMapCadService::inspectCAD' in REGISTRY_SOURCE
    assert 'interpretation.put("geometry_traversed", false)' in CAD_SOURCE
    assert 'requireCADSourceFormat(source)' in CAD_SOURCE
    assert 'result.put("format", sourceFormat)' in CAD_SOURCE
    inspect_start = CAD_SOURCE.index("static ObjectNode inspectCAD")
    inspect_end = CAD_SOURCE.index("static ObjectNode renderCADPreview", inspect_start)
    inspect_block = CAD_SOURCE[inspect_start:inspect_end]
    assert ".getGeometry(" not in inspect_block
    assert ".getRecordset(" not in inspect_block


def test_cad_render_preview_uses_map_layers_and_direct_mode():
    block = _operator_block("cad.render_preview")

    assert 'param("access_plan", "object", false, true' in block
    assert 'output("preview", "addp.cad.render-preview/v1"' in block
    assert 'List.of("direct")' in block
    assert '"cad.render_preview", SuperMapCadService::renderCADPreview' in REGISTRY_SOURCE
    render_start = CAD_SOURCE.index("static ObjectNode renderCADPreview")
    render_end = CAD_SOURCE.index("private static boolean outputCADMapToWebP", render_start)
    render_block = CAD_SOURCE[render_start:render_end]
    assert "map.getLayers().add(dataset, true)" in render_block
    assert "map.outputMapToWEBP" in CAD_SOURCE
    assert "map.outputMapToFile" not in CAD_SOURCE
    assert "map.setViewBounds(bounds)" in CAD_SOURCE
    assert "map.setBackgroundStyle(backgroundStyle)" in render_block
    assert "double renderSpan = Math.max(drawingBounds.getWidth(), drawingBounds.getHeight())" in render_block
    assert "readCADFormatVersion(sourcePath, sourceFormat)" in render_block
    assert ".getGeometry(" not in render_block


def test_cad_source_formats_accept_dwg_and_dxf_with_one_operator_path():
    assert '!"dwg".equals(sourceFormat) && !"dxf".equals(sourceFormat)' in SOURCE
    assert 'case "dwg" -> readDWGVersion(sourcePath)' in SOURCE
    assert 'case "dxf" -> readDXFVersion(sourcePath)' in SOURCE
    assert '"$ACADVER".equalsIgnoreCase(line.trim())' in SOURCE


def test_object_store_access_builds_a_container_reachable_minio_endpoint():
    assert "buildObjectStoreClient(access)" in SOURCE
    assert '.endpoint(paramText(access, "endpoint"))' not in SOURCE
    assert "normalizeResourceHost(uri.getHost())" in SOURCE
    assert 'access.path("use_ssl").asBoolean(false)' in SOURCE
