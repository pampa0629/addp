"""
批量为 Spark Sedona 算子生成元数据初稿
基于算子 ID、display_name 和 parameters 自动生成 brief_description 和 detailed_description
"""

import json
from typing import Dict, Any, List

# 算子类别到描述模板的映射
CATEGORY_TEMPLATES = {
    "数据I/O": {
        "brief_suffix": "用于数据的加载、保存和预览操作",
        "overview_prefix": "数据I/O算子提供"
    },
    "空间分析": {
        "brief_suffix": "用于空间几何计算和分析",
        "overview_prefix": "空间分析算子使用Apache Sedona的空间函数"
    },
    "数据转换": {
        "brief_suffix": "用于DataFrame的列操作和数据转换",
        "overview_prefix": "数据转换算子提供"
    },
    "聚合分析": {
        "brief_suffix": "用于数据的分组聚合和表连接",
        "overview_prefix": "聚合分析算子提供"
    },
    "Spark SQL": {
        "brief_suffix": "用于执行SQL查询和视图管理",
        "overview_prefix": "SQL算子支持"
    }
}

# 针对特定算子的use_cases模板
USE_CASES_TEMPLATES = {
    "load": [
        "从PostgreSQL加载空间数据: 加载城市POI表,包含10万条记录",
        "从MinIO读取GeoParquet文件: 读取省界行政区划数据",
        "使用SQL查询加载数据: SELECT * FROM buildings WHERE height > 100",
        "从Iceberg数据湖加载: 加载湖仓中的历史轨迹数据"
    ],
    "save": [
        "保存分析结果到PostgreSQL: 将缓冲区分析结果写入结果表",
        "导出数据到GeoParquet文件: 将处理后的数据导出到MinIO存储",
        "覆盖模式保存数据: mode='overwrite'覆盖已有表数据",
        "追加模式保存数据: mode='append'向表中追加新数据"
    ],
    "st_centroid": [
        "计算建筑物质心: 将面几何转换为点,用于标注或聚类分析",
        "计算行政区中心点: 获取省市边界的质心,用于地图标注",
        "提取道路中点: 计算道路线段的中心点位置",
        "计算地块重心: 用于土地利用分析和空间聚类"
    ],
    "spatial_join": [
        "点面叠加分析: 统计每个行政区内的POI数量,使用contains关系",
        "道路与河流交叉分析: 找出与河流相交的道路段,使用intersects关系",
        "建筑物包含关系: 查找完全位于保护区内的建筑,使用within关系",
        "地块重叠检测: 发现相互重叠的土地地块,使用overlaps关系"
    ],
    "st_distance": [
        "计算到最近地铁站距离: 每个小区到最近地铁站的直线距离",
        "分析污染源影响范围: 计算所有建筑到污染源的距离",
        "最近设施查询: 找出距离用户位置2公里内的医院",
        "道路可达性分析: 计算各地块到主干道的距离"
    ],
    "st_transform": [
        "地理坐标转投影坐标: WGS84转UTM Zone 50N,用于面积和距离计算",
        "统一坐标系: 将多源数据转换为统一的EPSG:3857 Web墨卡托坐标系",
        "转换为本地坐标系: 转换为CGCS2000国家坐标系进行精确测量",
        "投影坐标转地理坐标: UTM转WGS84,用于地图展示"
    ],
    "select": [
        "选择关键列: 从100个列中选择id、name、geom等关键字段",
        "去除冗余字段: 选择分析需要的列,减少内存占用",
        "重新排列列顺序: 将几何列放在最后,方便查看属性数据",
        "提取特定属性: 选择人口、面积、类型等业务字段"
    ],
    "filter": [
        "按属性过滤: 筛选人口大于100万的城市",
        "按类型筛选: 提取type='restaurant'的POI点",
        "按范围过滤: 筛选面积在1000到10000平方米之间的建筑",
        "组合条件筛选: category='商业' AND floor_count > 10"
    ],
    "group_by": [
        "按省份统计城市数量: GROUP BY province, 统计COUNT(*)",
        "按类型汇总面积: GROUP BY land_type, 计算SUM(area)",
        "分区域统计人口: GROUP BY district, 计算AVG(population)",
        "按年份聚合数据: GROUP BY year, 计算多个聚合指标"
    ],
    "join": [
        "关联属性数据: 将空间数据与业务属性表按ID连接",
        "合并多源数据: left join关联POI数据和行政区划",
        "数据补充: 将统计数据join到地理边界,用于专题地图",
        "多表关联分析: 连接建筑、地块、规划三张表进行综合分析"
    ]
}

def generate_brief_description(operator: Dict[str, Any]) -> str:
    """生成简要描述"""
    display_name = operator.get("display_name", "")
    category = operator.get("category", "")
    description = operator.get("description", "")

    template = CATEGORY_TEMPLATES.get(category, {})
    suffix = template.get("brief_suffix", "")

    return f"{description},{suffix}"

def generate_overview(operator: Dict[str, Any]) -> str:
    """生成概览"""
    operator_id = operator.get("id", "")
    display_name = operator.get("display_name", "")
    category = operator.get("category", "")
    description = operator.get("description", "")

    template = CATEGORY_TEMPLATES.get(category, {})
    prefix = template.get("overview_prefix", "")

    if category == "空间分析":
        return f"{prefix}实现{description}功能。支持大规模分布式计算,适用于TB级空间数据处理场景。"
    elif category == "数据I/O":
        return f"{prefix}{description}功能,支持多种数据源类型,包括数据库(PostgreSQL/MySQL/Doris)、文件(Parquet/GeoParquet/CSV)、数据湖(Iceberg/Delta/Hudi)等。"
    else:
        return f"{prefix}{description}功能,支持分布式并行处理,提高大数据场景下的处理效率。"

def generate_use_cases(operator_id: str, display_name: str) -> List[str]:
    """生成使用场景"""
    if operator_id in USE_CASES_TEMPLATES:
        return USE_CASES_TEMPLATES[operator_id]

    # 通用模板
    return [
        f"场景1: 使用{display_name}进行数据处理,处理10万条记录",
        f"场景2: 在空间分析流程中应用{display_name}算子",
        f"场景3: 结合其他算子组成工作流,{display_name}作为中间步骤",
        f"场景4: 大数据场景下使用{display_name}进行批量处理"
    ]

def generate_notes(operator: Dict[str, Any]) -> List[str]:
    """生成注意事项"""
    category = operator.get("category", "")

    if category == "空间分析":
        return [
            "地理坐标系(EPSG:4326)单位为度,进行距离或面积计算前需要先转换为投影坐标系",
            "大数据集(>100万条)建议先使用filter过滤或cache缓存中间结果,优化性能",
            "Sedona空间算子支持分布式计算,自动进行空间索引优化",
            "几何列必须是Sedona Geometry类型,从数据库加载时会自动转换"
        ]
    elif category == "数据I/O":
        return [
            "engine_id需要在System模块中预先配置数据源连接信息",
            "几何列默认名称为'geom',如果不同需要显式指定",
            "大文件读取建议使用Parquet或GeoParquet格式,性能最优",
            "支持分区读取,可指定分区列优化查询性能"
        ]
    else:
        return [
            "算子支持分布式并行处理,自动处理数据分区",
            "建议根据数据量合理设置分区数,避免数据倾斜",
            "可以使用cache或persist缓存中间结果,避免重复计算",
            "注意内存使用,大数据集建议分批处理或增加executor内存"
        ]

def generate_workflow_example(operator: Dict[str, Any]) -> Dict[str, Any]:
    """生成工作流示例"""
    operator_id = operator.get("id", "")
    params = operator.get("parameters", [])

    # 构建示例参数
    example_params = {}
    for param in params:
        param_name = param.get("name", "")
        param_type = param.get("type", "")

        if param_type == "ref":
            example_params[param_name] = {"$ref": "load_data"}
        elif param_name == "distance":
            example_params[param_name] = 500
        elif param_name == "columns":
            example_params[param_name] = ["id", "name", "geom"]
        elif param_name == "condition":
            example_params[param_name] = "population > 100000"
        elif param_name in ["geom_column", "left_geom", "right_geom"]:
            example_params[param_name] = "geom"
        elif "default" in param:
            # 跳过有默认值的可选参数
            continue

    return {
        "id": f"{operator_id}_task",
        "operator": operator_id,
        "params": example_params,
        "depends_on": ["load_data"] if any(p.get("type") == "ref" for p in params) else []
    }

def enrich_operator_metadata(operator: Dict[str, Any]) -> Dict[str, Any]:
    """为单个算子补充完整元数据"""
    operator_id = operator.get("id", "")

    # 如果已有complete元数据,跳过
    if "detailed_description" in operator:
        print(f"✓ {operator_id} 已有完整元数据,跳过")
        return operator

    print(f"→ 生成 {operator_id} 的元数据...")

    # 生成 brief_description
    if "brief_description" not in operator:
        operator["brief_description"] = generate_brief_description(operator)

    # 生成 detailed_description
    operator["detailed_description"] = {
        "overview": generate_overview(operator),
        "parameters": [
            {
                "name": p.get("name", ""),
                "type": p.get("type", ""),
                "required": p.get("required", False),
                "description": p.get("description", ""),
                "notes": f"参考参数定义中的说明"
            }
            for p in operator.get("parameters", [])
        ],
        "use_cases": generate_use_cases(operator_id, operator.get("display_name", "")),
        "notes": generate_notes(operator),
        "input": "DataFrame" if any(p.get("type") == "ref" for p in operator.get("parameters", [])) else "无输入",
        "output": "DataFrame" if operator.get("output_ports", [{}])[0].get("type") == "dataframe" else "结果字典",
        "workflow_example": generate_workflow_example(operator)
    }

    # 补充 output_ports 的 description
    for port in operator.get("output_ports", []):
        if "description" not in port:
            port["description"] = f"{operator.get('display_name', '')}结果"

    return operator

if __name__ == "__main__":
    print("=" * 80)
    print("Spark Sedona 算子元数据批量生成工具")
    print("=" * 80)
    print()

    # 导入现有元数据
    from operator_metadata import get_operator_metadata

    operators = get_operator_metadata()
    print(f"读取到 {len(operators)} 个算子")
    print()

    # 批量生成
    enriched_operators = []
    for operator in operators:
        enriched = enrich_operator_metadata(operator)
        enriched_operators.append(enriched)

    print()
    print("=" * 80)
    print(f"✓ 完成!已为 {len(enriched_operators)} 个算子生成元数据")
    print()
    print("下一步: 手动复制生成的元数据到 operator_metadata.py")
    print("建议: 对自动生成的 use_cases 和 notes 进行人工审核和优化")
    print("=" * 80)

    # 输出JSON格式(方便复制)
    output_file = "operator_metadata_enriched.json"
    with open(output_file, "w", encoding="utf-8") as f:
        json.dump(enriched_operators, f, ensure_ascii=False, indent=2)

    print(f"\n已保存到: {output_file}")
