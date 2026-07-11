from pathlib import Path


SOURCE = (Path(__file__).parent / "src/main/java/com/addp/supermap/workflow/SuperMapWorkflowRuntime.java").read_text(encoding="utf-8")


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
