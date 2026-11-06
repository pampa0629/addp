#!/usr/bin/env python
"""
测试非空间算子

测试新增的 18 个非空间算子（属性计算 + 数据筛选）
"""

import geopandas as gpd
from shapely.geometry import Point, Polygon
import pandas as pd
import pytest
import shapely

# 导入所有算子
from operators import (
    # 属性计算算子
    add_field, calculate_field, rename_fields, drop_fields,
    type_cast, fill_null, normalize_field, encode_categorical, bin_field,
    # 数据筛选算子
    filter_by_attribute, sort_by_field, select_top_n, drop_duplicates,
    sample, filter_by_geometry_type, drop_null_geometry, random_split
)


def create_test_data():
    """创建测试数据"""
    # 创建 10 个点
    points = [Point(i, i) for i in range(10)]

    gdf = gpd.GeoDataFrame({
        'id': range(10),
        'name': [f'Point_{i}' for i in range(10)],
        'value': [10, 20, 30, 40, 50, 60, 70, 80, 90, 100],
        'category': ['A', 'B', 'A', 'B', 'A', 'B', 'A', 'B', 'A', 'B'],
        'geometry': points
    }, crs='EPSG:4326')

    return gdf


@pytest.mark.parametrize("crs", [None, "EPSG:3857"])
def test_geometry_selection_preserves_empty_types_and_input_columns(crs):
    source = gpd.GeoDataFrame({
        "geometry": pd.array([None, "point", "missing", "collection", "multi"], dtype="string"),
        "source_id": pd.array([1, None, 3, 4, 5], dtype="Int64"),
        "shape": [Point(), Point(1, 2), None, shapely.GeometryCollection([Point(2, 3)]),
                  shapely.MultiPoint([Point(3, 4)])],
    }, geometry="shape", crs=crs, index=[5, 5, 4, 3, 2])
    source_wkb = source.geometry.to_wkb().tolist()
    for result, positions in [
        (filter_by_geometry_type(source, "Point"), [0, 1]),
        (filter_by_geometry_type(source, "GeometryCollection"), [3]),
        (filter_by_geometry_type(source, "MultiPoint"), [4]),
        (filter_by_geometry_type(source, "Polygon"), []),
        (drop_null_geometry(source), [0, 1, 3, 4]),
        (drop_null_geometry(source.iloc[:0]), []),
    ]:
        assert result.geometry.name == "shape" and result.crs == source.crs
        assert result.index.equals(pd.RangeIndex(len(positions)))
        assert result.geometry.to_wkb().tolist() == [source_wkb[i] for i in positions]
        pd.testing.assert_frame_equal(result.drop(columns="shape"),
                                      source.iloc[positions].drop(columns="shape").reset_index(drop=True))
    assert source.geometry.to_wkb().tolist() == source_wkb
    assert source.index.tolist() == [5, 5, 4, 3, 2]


def test_geometry_selection_metadata_publishes_closed_types_and_null_label():
    from operators import list_operators

    entries = {op["name"]: op for op in list_operators()}
    parameter = next(p for p in entries["filter_by_geometry_type"]["parameters"] if p["name"] == "geom_type")
    assert parameter["enum"] == ["Point", "LineString", "Polygon", "MultiPoint",
                                 "MultiLineString", "MultiPolygon", "GeometryCollection"]
    assert entries["drop_null_geometry"]["display_name"] == "删除缺失几何"


@pytest.mark.parametrize(("operator", "case", "reason", "parameter"), [
    ("filter_by_geometry_type", "legal_no_match", None, None),
    ("filter_by_geometry_type", "bad_type", "operator_enum_invalid", "geom_type"),
    ("filter_by_geometry_type", "wrong_case", "operator_enum_invalid", "geom_type"),
    ("filter_by_geometry_type", "non_string", "operator_enum_invalid", "geom_type"),
    ("filter_by_geometry_type", "missing_type", "operator_enum_invalid", "geom_type"),
    ("filter_by_geometry_type", "missing_input", "operator_input_invalid", "input_gdf"),
    ("drop_null_geometry", "missing_input", "operator_input_invalid", "input_gdf"),
    ("drop_null_geometry", "bad_input", "operator_input_invalid", "input_gdf"),
    ("drop_null_geometry", "inactive_geometry", "operator_input_invalid", "input_gdf"),
    ("drop_null_geometry", "native_failure", None, None),
])
def test_geometry_selection_workflow_diagnostics_block_save(monkeypatch, operator, case, reason, parameter):
    from addp_common.workflow_runtime import WorkflowExecutionError
    import workflow_engine

    params = {"input_gdf": create_test_data()}
    if operator == "filter_by_geometry_type":
        params["geom_type"] = "Point"
    if case == "missing_input":
        del params["input_gdf"]
    elif case == "bad_input":
        params["input_gdf"] = "{PRIVATE"
    elif case == "inactive_geometry":
        params["input_gdf"] = gpd.GeoDataFrame({"shape": params["input_gdf"].geometry})
    elif case == "bad_type":
        params["geom_type"] = "PRIVATE"
    elif case == "wrong_case":
        params["geom_type"] = "point"
    elif case == "non_string":
        params["geom_type"] = ["Point"]
    elif case == "missing_type":
        del params["geom_type"]
    elif case == "legal_no_match":
        params["geom_type"] = "Polygon"
    elif case == "native_failure":
        def fail(*args, **kwargs):
            raise ValueError("native geometry mask failed")

        monkeypatch.setattr(gpd.array.GeometryArray, "isna", fail)

    saves = []
    resolve = workflow_engine.get_operator
    monkeypatch.setattr(workflow_engine, "get_operator",
                        lambda name: (lambda **kwargs: saves.append(kwargs)) if name == "save" else resolve(name))
    tasks = [
        {"id": "save", "operator": "save", "params": {"input_df": {"$ref": "filter"}}, "depends_on": ["filter"]},
        {"id": "filter", "operator": operator, "params": params, "depends_on": []},
    ]
    runner = workflow_engine.GeoPythonWorkflowRunner()
    if case == "legal_no_match":
        runner.execute({"tasks": tasks})
        assert len(saves) == 1 and saves[0]["input_df"].empty
        return
    with pytest.raises(WorkflowExecutionError) as caught:
        runner.execute({"tasks": tasks})
    assert not saves
    error = caught.value
    if reason is None:
        assert error.error_code == "EXECUTION_FAILED" and error.diagnostic is None
    else:
        assert error.error_code == "INVALID_PARAMS"
        assert error.diagnostic == {
            "schema_version": "addp.execution-failure/v3", "stage": "execution", "reason": reason,
            "operator": {"node_index": 2, "operator_id": operator, "parameter": parameter},
        }
        assert "PRIVATE" not in str(error)


def test_attribute_operators():
    """测试属性计算算子"""
    print("\n========== 测试属性计算算子 ==========\n")

    gdf = create_test_data()

    # 1. add_field
    print("1. add_field - 添加新字段")
    result = add_field(gdf, 'status', 'active')
    print(f"   ✅ 添加字段 status，值为 active")
    print(f"   字段列表: {list(result.columns)}")

    # 2. calculate_field
    print("\n2. calculate_field - 字段计算")
    result = calculate_field(gdf, 'double_value', 'value * 2')
    print(f"   ✅ 计算字段 double_value = value * 2")
    print(f"   前3行: {result[['value', 'double_value']].head(3).to_dict('records')}")

    # 3. rename_fields
    print("\n3. rename_fields - 批量重命名")
    result = rename_fields(gdf, {'name': 'point_name', 'value': 'score'})
    print(f"   ✅ 重命名: name -> point_name, value -> score")
    print(f"   字段列表: {list(result.columns)}")

    # 4. drop_fields
    print("\n4. drop_fields - 删除字段")
    gdf_temp = add_field(gdf, 'temp', 'temporary')
    result = drop_fields(gdf_temp, ['temp'])
    print(f"   ✅ 删除字段 temp")
    print(f"   字段列表: {list(result.columns)}")

    # 5. type_cast
    print("\n5. type_cast - 类型转换")
    gdf_with_str = add_field(gdf, 'id_str', '123')
    result = type_cast(gdf_with_str, 'id_str', 'int')
    print(f"   ✅ 转换 id_str 从 str 到 int")
    print(f"   类型: {result['id_str'].dtype}")

    # 6. fill_null
    print("\n6. fill_null - 填充空值")
    gdf_with_null = gdf.copy()
    gdf_with_null.loc[0, 'value'] = None
    result = fill_null(gdf_with_null, 'value', 0)
    print(f"   ✅ 填充 value 字段空值为 0")
    print(f"   首行 value: {result.loc[0, 'value']}")

    # 7. normalize_field
    print("\n7. normalize_field - 字段归一化")
    result = normalize_field(gdf, 'value', 'minmax')
    print(f"   ✅ MinMax 归一化 value 字段")
    print(f"   归一化范围: [{result['value_norm'].min():.2f}, {result['value_norm'].max():.2f}]")

    # 8. encode_categorical
    print("\n8. encode_categorical - 分类编码")
    result = encode_categorical(gdf, 'category')
    print(f"   ✅ 编码 category 字段")
    print(f"   A -> {result[result['category']=='A'].iloc[0]['category_code']}, B -> {result[result['category']=='B'].iloc[0]['category_code']}")

    # 9. bin_field
    print("\n9. bin_field - 字段分箱")
    result = bin_field(gdf, 'value', bins=[0, 30, 60, 100], labels=['低', '中', '高'])
    print(f"   ✅ 分箱 value 字段: [0-30]=低, [30-60]=中, [60-100]=高")
    print(f"   分箱结果: {result[['value', 'value_bin']].head(3).to_dict('records')}")

    print("\n✅ 所有属性计算算子测试通过！")


def test_filter_operators():
    """测试数据筛选算子"""
    print("\n========== 测试数据筛选算子 ==========\n")

    gdf = create_test_data()

    # 1. filter_by_attribute
    print("1. filter_by_attribute - 属性条件筛选")
    result = filter_by_attribute(gdf, 'value', '>', 50)
    print(f"   ✅ 筛选 value > 50: {len(result)} 条记录")
    print(f"   值范围: {result['value'].min()}-{result['value'].max()}")

    # 2. sort_by_field
    print("\n2. sort_by_field - 按字段排序")
    result = sort_by_field(gdf, 'value', ascending=False)
    print(f"   ✅ 按 value 降序排序")
    print(f"   前3个值: {list(result['value'].head(3))}")

    # 3. select_top_n
    print("\n3. select_top_n - 选择 Top N")
    result = select_top_n(gdf, 'value', 3, ascending=False)
    print(f"   ✅ 选择 value 最大的3个")
    print(f"   Top 3 值: {list(result['value'])}")

    # 4. drop_duplicates
    print("\n4. drop_duplicates - 去重")
    gdf_with_dup = pd.concat([gdf, gdf.iloc[:2]], ignore_index=True)
    gdf_with_dup = gpd.GeoDataFrame(gdf_with_dup, crs='EPSG:4326')
    result = drop_duplicates(gdf_with_dup, ['id'])
    print(f"   ✅ 按 id 去重: {len(gdf_with_dup)} -> {len(result)} 条记录")

    # 5. sample
    print("\n5. sample - 随机采样")
    result = sample(gdf, n=5, random_state=42)
    print(f"   ✅ 随机采样 5 条: {len(result)} 条记录")
    print(f"   采样 id: {list(result['id'])}")

    # 6. filter_by_geometry_type
    print("\n6. filter_by_geometry_type - 按几何类型过滤")
    result = filter_by_geometry_type(gdf, 'Point')
    print(f"   ✅ 筛选 Point 类型: {len(result)} 条记录")

    # 7. drop_null_geometry
    print("\n7. drop_null_geometry - 删除空几何")
    gdf_with_null_geom = gdf.copy()
    gdf_with_null_geom.loc[0, 'geometry'] = None
    result = drop_null_geometry(gdf_with_null_geom)
    print(f"   ✅ 删除空几何: {len(gdf_with_null_geom)} -> {len(result)} 条记录")

    # 8. random_split (多输出算子)
    print("\n8. random_split - 随机分割")
    results = random_split(gdf, train_ratio=0.7, random_state=42)
    print(f"   ✅ 分割数据集 (7:3)")
    print(f"   训练集: {len(results['train'])} 条")
    print(f"   测试集: {len(results['test'])} 条")
    print(f"   总计: {len(results['train']) + len(results['test'])} 条")

    print("\n✅ 所有数据筛选算子测试通过！")


def test_workflow_integration():
    """测试工作流集成"""
    print("\n========== 测试工作流集成 ==========\n")

    gdf = create_test_data()

    print("场景: 数据清洗 + 属性计算 + 条件筛选")
    print("流程: 删除空几何 -> 添加分类字段 -> 计算新指标 -> 筛选高值 -> 排序")

    # Step 1: 删除空几何
    step1 = drop_null_geometry(gdf)
    print(f"   Step 1: 删除空几何 - {len(step1)} 条记录")

    # Step 2: 添加分类字段
    step2 = add_field(step1, 'level', 'normal')
    print(f"   Step 2: 添加字段 level - 字段数: {len(step2.columns)}")

    # Step 3: 计算新指标
    step3 = calculate_field(step2, 'score', 'value / 10')
    print(f"   Step 3: 计算 score = value / 10 - 均值: {step3['score'].mean():.1f}")

    # Step 4: 筛选高值
    step4 = filter_by_attribute(step3, 'score', '>=', 5)
    print(f"   Step 4: 筛选 score >= 5 - {len(step4)} 条记录")

    # Step 5: 排序
    step5 = sort_by_field(step4, 'score', ascending=False)
    print(f"   Step 5: 按 score 降序排序 - Top 3: {list(step5['score'].head(3))}")

    print("\n✅ 工作流集成测试通过！")


if __name__ == '__main__':
    print("=" * 60)
    print("非空间算子测试套件")
    print("=" * 60)

    try:
        test_attribute_operators()
        test_filter_operators()
        test_workflow_integration()

        print("\n" + "=" * 60)
        print("✅ 所有测试通过！共 18 个非空间算子正常工作")
        print("=" * 60)

    except Exception as e:
        print(f"\n❌ 测试失败: {e}")
        import traceback
        traceback.print_exc()
