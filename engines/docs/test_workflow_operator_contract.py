from workflow_operator_contract import validate_operator_metadata_contract


def test_contract_accepts_multi_output_without_default_port():
    errors = validate_operator_metadata_contract([
        {
            "id": "split",
            "name": "split",
            "display_name": "拆分",
            "engine_type": "geopython_workflow",
            "category": "数据操作",
            "category_path": ["数据操作"],
            "description": "拆分数据",
            "execution_modes": ["workflow"],
            "effects": ["read"],
            "parameters": [],
            "output_ports": [
                {"name": "left", "type": "geodataframe", "is_default": False},
                {"name": "right", "type": "geodataframe", "is_default": False},
            ],
        }
    ])

    assert errors == []


def test_contract_requires_single_output_default_port():
    errors = validate_operator_metadata_contract([
        {
            "id": "buffer",
            "name": "buffer",
            "display_name": "缓冲区",
            "engine_type": "geopython_workflow",
            "category": "空间分析",
            "category_path": ["空间分析"],
            "description": "缓冲区分析",
            "execution_modes": ["workflow"],
            "effects": ["read"],
            "parameters": [],
            "output_ports": [
                {"name": "default", "type": "geodataframe", "is_default": False},
            ],
        }
    ])

    assert any("single output operator" in error for error in errors)


def test_contract_rejects_module_and_unknown_execution_mode():
    errors = validate_operator_metadata_contract([
        {
            "id": "buffer",
            "name": "buffer",
            "display_name": "缓冲区",
            "engine_type": "geopython_workflow",
            "module": "legacy_runtime_module",
            "category": "空间分析",
            "category_path": ["空间分析"],
            "description": "缓冲区分析",
            "execution_modes": ["batch"],
            "effects": ["read"],
            "parameters": [],
            "outputs": ["geodataframe"],
            "output_ports": [
                {"name": "default", "type": "geodataframe", "is_default": True},
            ],
        }
    ])

    assert any("module is not allowed" in error for error in errors)
    assert any("outputs is not allowed" in error for error in errors)
    assert any("unsupported execution_modes" in error for error in errors)


def test_contract_requires_valid_unique_effects():
    missing_errors = validate_operator_metadata_contract([
        {
            "id": "missing",
            "name": "missing",
            "display_name": "缺少效果",
            "engine_type": "geopython_workflow",
            "category": "测试",
            "category_path": ["测试"],
            "description": "缺少效果",
            "execution_modes": ["workflow"],
            "parameters": [],
            "output_ports": [{"name": "default", "type": "object", "is_default": True}],
        }
    ])
    assert any("missing required field effects" in error for error in missing_errors)

    invalid_errors = validate_operator_metadata_contract([
        {
            "id": "invalid",
            "name": "invalid",
            "display_name": "错误效果",
            "engine_type": "geopython_workflow",
            "category": "测试",
            "category_path": ["测试"],
            "description": "错误效果",
            "execution_modes": ["workflow"],
            "effects": ["read", "read", "network"],
            "parameters": [],
            "output_ports": [{"name": "default", "type": "object", "is_default": True}],
        }
    ])
    assert any("unsupported effects" in error for error in invalid_errors)
    assert any("must not contain duplicates" in error for error in invalid_errors)
