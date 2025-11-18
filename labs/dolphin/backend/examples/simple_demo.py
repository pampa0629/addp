#!/usr/bin/env python3
"""
简化版空间算子演示 - 不需要 geopandas
展示数据血缘追踪的核心概念
"""

import json
from datetime import datetime
from pathlib import Path

print("="*70)
print("DolphinScheduler 学习实验室 - 血缘追踪演示（简化版）")
print("="*70)

# 模拟数据流转
print("\n[步骤 1/4] 创建模拟数据...")
input_data = {
    "name": "POI点数据",
    "count": 10,
    "type": "Point",
    "crs": "EPSG:4326"
}
print(f"  ✓ 创建了 {input_data['count']} 个 POI 点")

# 模拟算子执行
print("\n[步骤 2/4] 执行数据处理流水线...")

operations = [
    {"name": "投影转换", "input": 10, "output": 10, "time": 0.004},
    {"name": "500米缓冲区", "input": 10, "output": 10, "time": 0.001},
    {"name": "面积过滤", "input": 10, "output": 8, "time": 0.002},
    {"name": "添加质心", "input": 8, "output": 8, "time": 0.001},
]

for i, op in enumerate(operations, 1):
    print(f"  [{i}/4] {op['name']}: {op['input']} → {op['output']} 条记录 ({op['time']}s)")

# 生成血缘图
print("\n[步骤 3/4] 生成数据血缘图...")

lineage = {
    "graph_id": "demo-001",
    "pipeline_name": "POI缓冲区分析（简化版）",
    "created_at": datetime.now().isoformat(),
    "assets": {},
    "executions": {},
    "root_assets": ["asset-0"],
    "leaf_assets": ["asset-4"]
}

# 创建数据资产
current_id = 0
for i, op in enumerate([{"name": "POI点数据", "count": 10}] +
                       [{"name": f"{op['name']}_output", "count": op['output']}
                        for op in operations]):
    lineage["assets"][f"asset-{current_id}"] = {
        "asset_id": f"asset-{current_id}",
        "name": op["name"],
        "record_count": op["count"],
        "created_at": datetime.now().isoformat()
    }
    current_id += 1

# 创建算子执行记录
for i, op in enumerate(operations):
    lineage["executions"][f"exec-{i}"] = {
        "execution_id": f"exec-{i}",
        "operator_name": op["name"],
        "input_assets": [f"asset-{i}"],
        "output_assets": [f"asset-{i+1}"],
        "elapsed_seconds": op["time"]
    }

print(f"  ✓ 数据资产: {len(lineage['assets'])} 个")
print(f"  ✓ 算子执行: {len(lineage['executions'])} 个")

# 保存结果
print("\n[步骤 4/4] 保存结果...")

output_dir = Path("/opt/dolphin-scripts/output")
output_dir.mkdir(parents=True, exist_ok=True)

# 保存血缘图
lineage_file = output_dir / "simple_lineage.json"
with open(lineage_file, 'w', encoding='utf-8') as f:
    json.dump(lineage, f, indent=2, ensure_ascii=False)

print(f"  ✓ 血缘图已保存: {lineage_file}")

# 生成 Mermaid 流程图
mermaid_lines = ["graph TD"]
for asset_id, asset in lineage["assets"].items():
    label = f"{asset['name']}<br/>{asset['record_count']} records"
    mermaid_lines.append(f'    {asset_id}["{label}"]')

for exec_id, execution in lineage["executions"].items():
    input_id = execution["input_assets"][0]
    output_id = execution["output_assets"][0]
    label = f"{execution['operator_name']}<br/>{execution['elapsed_seconds']:.3f}s"
    mermaid_lines.append(f'    {input_id} -->|"{label}"| {output_id}')

mermaid_code = '\n'.join(mermaid_lines)
mermaid_file = output_dir / "simple_lineage.mmd"
with open(mermaid_file, 'w', encoding='utf-8') as f:
    f.write(mermaid_code)

print(f"  ✓ Mermaid 流程图: {mermaid_file}")

# 生成摘要报告
print("\n" + "="*70)
print("执行完成！")
print("="*70)

print("\n数据流转摘要:")
print(f"  输入: {operations[0]['input']} 条记录")
print(f"  输出: {operations[-1]['output']} 条记录")
print(f"  保留率: {operations[-1]['output']/operations[0]['input']:.1%}")
print(f"  总耗时: {sum(op['time'] for op in operations):.3f}s")

print("\n血缘图预览:")
print("-" * 70)
print(mermaid_code)
print("-" * 70)

print("\n✓ 演示成功！文件已保存到 /opt/dolphin-scripts/output/")
print("\n可以在宿主机查看:")
print("  - output/simple_lineage.json")
print("  - output/simple_lineage.mmd")
