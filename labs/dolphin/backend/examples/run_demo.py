#!/usr/bin/env python3
"""
测试空间算子和血缘追踪系统
不需要真实的地理数据，使用模拟数据演示
"""

import sys
import json
from datetime import datetime
from pathlib import Path

print("="*70)
print("DolphinScheduler 学习实验室 - 空间算子和血缘追踪演示")
print("="*70)

# 检查依赖
print("\n[步骤 1/4] 检查依赖...")
missing_deps = []

try:
    import geopandas as gpd
    print("  ✓ geopandas 已安装")
except ImportError:
    missing_deps.append("geopandas")
    print("  ✗ geopandas 未安装")

try:
    from shapely.geometry import Point, Polygon
    print("  ✓ shapely 已安装")
except ImportError:
    missing_deps.append("shapely")
    print("  ✗ shapely 未安装")

if missing_deps:
    print(f"\n⚠️  缺少依赖: {', '.join(missing_deps)}")
    print("\n自动切换到简化版演示（不需要地理数据库）")
    print("\n提示: 完整版需要 geopandas，但简化版已足够展示核心功能")

    # 自动运行简化版
    import subprocess
    simple_script = Path(__file__).parent / "simple_demo.py"
    if simple_script.exists():
        print(f"\n正在运行简化版: {simple_script}")
        print("=" * 70)
        subprocess.run([sys.executable, str(simple_script)])
    sys.exit(0)

# 导入我们的模块
sys.path.insert(0, str(Path(__file__).parent))
from lineage_tracker import (
    SpatialPipelineWithLineage,
    LineageGraph,
    DataAsset
)

print("\n[步骤 2/4] 创建模拟数据...")

# 创建模拟的 POI 点数据
poi_data = gpd.GeoDataFrame({
    'id': range(1, 11),
    'name': [f'POI_{i}' for i in range(1, 11)],
    'type': ['park', 'school', 'hospital', 'shop', 'restaurant'] * 2,
    'geometry': [
        Point(120.0 + i*0.01, 30.0 + i*0.01)
        for i in range(10)
    ]
}, crs="EPSG:4326")

print(f"  ✓ 创建了 {len(poi_data)} 个 POI 点")
print(f"  类型: {poi_data['type'].unique().tolist()}")
print(f"  坐标系: {poi_data.crs}")

# 定义算子函数
def reproject(gdf, to_crs):
    """投影转换算子"""
    return gdf.to_crs(to_crs)

def buffer(gdf, distance):
    """缓冲区算子"""
    gdf = gdf.copy()
    gdf['geometry'] = gdf.geometry.buffer(distance)
    return gdf

def filter_by_area(gdf, min_area):
    """面积过滤算子"""
    gdf = gdf.copy()
    gdf['area'] = gdf.geometry.area
    result = gdf[gdf['area'] >= min_area].copy()
    return result

def add_centroid(gdf):
    """添加质心坐标"""
    gdf = gdf.copy()
    gdf['centroid_x'] = gdf.geometry.centroid.x
    gdf['centroid_y'] = gdf.geometry.centroid.y
    return gdf

print("\n[步骤 3/4] 执行空间分析流水线...")

# 构建带血缘追踪的流水线
pipeline = SpatialPipelineWithLineage(
    "POI缓冲区分析演示",
    enable_lineage=True
)

# 添加算子
pipeline.add_step(reproject, "投影转换", "reproject", to_crs="EPSG:3857")
pipeline.add_step(buffer, "500米缓冲区", "buffer", distance=500)
pipeline.add_step(filter_by_area, "面积过滤", "filter", min_area=500000)
pipeline.add_step(add_centroid, "添加质心", "add_centroid")

# 执行流水线
result = pipeline.execute(poi_data, input_name="POI点数据")

# 保存结果
output_dir = Path(__file__).parent / "output"
output_dir.mkdir(exist_ok=True)

output_file = output_dir / "poi_buffer_result.geojson"
lineage_file = output_dir / "poi_buffer_lineage.json"

pipeline.save_result(result, str(output_file), lineage_path=str(lineage_file))

print("\n[步骤 4/4] 分析血缘数据...")

# 加载并分析血缘图
graph = LineageGraph.load(str(lineage_file))

print(f"\n血缘图分析:")
print(f"  流水线名称: {graph.pipeline_name}")
print(f"  数据资产数: {len(graph.assets)}")
print(f"  算子执行数: {len(graph.executions)}")

# 数据血缘链
print(f"\n数据流转链:")
final_asset_id = graph.leaf_assets[0]
source_chain = graph.trace_to_source(final_asset_id)
for i, asset_id in enumerate(reversed(source_chain)):
    asset = graph.assets[asset_id]
    count = asset.statistics['record_count']
    print(f"  {i+1}. {asset.name}: {count} 条记录")

# 每步数据量变化
print(f"\n数据量变化分析:")
for exec_id, execution in graph.executions.items():
    input_asset = graph.assets[execution.input_assets[0]]
    output_asset = graph.assets[execution.output_assets[0]]

    input_count = input_asset.statistics['record_count']
    output_count = output_asset.statistics['record_count']
    ratio = output_count / input_count if input_count > 0 else 0

    print(f"  {execution.operator_name}:")
    print(f"    输入: {input_count} 条 → 输出: {output_count} 条")
    print(f"    保留率: {ratio:.1%}")
    print(f"    耗时: {execution.elapsed_seconds:.3f}s")

# 性能分析
total_time = sum(ex.elapsed_seconds for ex in graph.executions.values())
print(f"\n性能分析:")
print(f"  总执行时间: {total_time:.3f}s")
for ex in graph.executions.values():
    percent = ex.elapsed_seconds / total_time * 100 if total_time > 0 else 0
    print(f"    {ex.operator_name}: {ex.elapsed_seconds:.3f}s ({percent:.1f}%)")

# 导出 Mermaid 流程图
mermaid_code = graph.export_mermaid()
mermaid_file = output_dir / "lineage_graph.mmd"
with open(mermaid_file, 'w', encoding='utf-8') as f:
    f.write(mermaid_code)

print(f"\n生成的文件:")
print(f"  ✓ 空间数据结果: {output_file}")
print(f"  ✓ 血缘图 JSON: {lineage_file}")
print(f"  ✓ Mermaid 流程图: {mermaid_file}")

print("\n" + "="*70)
print("演示完成！")
print("="*70)

print("\n提示:")
print("  - 结果文件保存在: backend/examples/output/")
print("  - 血缘图可以在 Meta 模块中可视化")
print("  - Mermaid 流程图可以复制到在线编辑器查看:")
print("    https://mermaid.live/")

print("\n血缘图预览 (Mermaid):")
print("-" * 70)
print(mermaid_code)
print("-" * 70)
