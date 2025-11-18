# GIS 空间计算血缘追踪可视化示例

## 示例 1: 简单线性血缘

### 场景: POI 缓冲区分析

```
输入: POI 点数据 (1000 条)
  ↓ 投影转换 (EPSG:4326 → EPSG:3857)
POI_投影后 (1000 条)
  ↓ 500米缓冲区
POI_缓冲区 (1000 条)
  ↓ 面积过滤 (>100000 m²)
POI_过滤后 (856 条)
  ↓ 保存
结果.geojson
```

### Mermaid 流程图

```mermaid
graph TD
    A["POI点数据<br/>type: Point<br/>count: 1000<br/>source: poi.shp"]
    B["POI_投影后<br/>type: Point<br/>count: 1000<br/>crs: EPSG:3857"]
    C["POI_缓冲区<br/>type: Polygon<br/>count: 1000<br/>buffer: 500m"]
    D["POI_过滤后<br/>type: Polygon<br/>count: 856<br/>area > 100000"]

    A -->|"投影转换<br/>to_crs: EPSG:3857<br/>0.52s"| B
    B -->|"缓冲区分析<br/>distance: 500m<br/>1.25s"| C
    C -->|"面积过滤<br/>min_area: 100000<br/>0.11s"| D

    style A fill:#e1f5ff
    style D fill:#c8e6c9
```

## 示例 2: 多输入血缘

### 场景: 道路与建筑物叠加分析

```
输入1: 道路数据 (5000 条线)
输入2: 建筑物数据 (20000 个多边形)
  ↓ 空间叠加 (intersection)
道路_建筑_交集 (1200 条)
  ↓ 统计分析
道路沿线建筑统计表
  ↓ 保存
分析结果.geojson
```

### Mermaid 流程图

```mermaid
graph TD
    A1["道路数据<br/>type: LineString<br/>count: 5000<br/>source: roads.shp"]
    A2["建筑物数据<br/>type: Polygon<br/>count: 20000<br/>source: buildings.shp"]
    B["道路_建筑_交集<br/>type: Polygon<br/>count: 1200"]
    C["道路沿线建筑统计<br/>type: Table<br/>count: 1200<br/>with: area, distance"]
    D["分析结果<br/>type: GeoJSON<br/>file: result.geojson"]

    A1 -->|"空间叠加<br/>op: intersection"| B
    A2 -->|"空间叠加<br/>op: intersection"| B
    B -->|"统计分析<br/>fields: area, distance<br/>2.3s"| C
    C -->|"保存<br/>format: GeoJSON<br/>0.5s"| D

    style A1 fill:#e1f5ff
    style A2 fill:#e1f5ff
    style D fill:#c8e6c9
```

## 示例 3: 复杂分支血缘

### 场景: 土地利用变化分析

```
输入: 2020年土地利用数据
  ├─ 分支1: 提取耕地 → 统计面积 → 耕地报表
  ├─ 分支2: 提取建设用地 → 缓冲区分析 → 建设用地影响范围
  └─ 分支3: 全部数据 → 投影转换 → 导出 Shapefile
```

### Mermaid 流程图

```mermaid
graph TD
    A["2020年土地利用<br/>count: 50000<br/>source: landuse_2020.shp"]

    subgraph 分支1: 耕地分析
        B1["耕地数据<br/>count: 15000"]
        C1["耕地统计<br/>total_area: 5000 km²"]
        D1["耕地报表<br/>report.xlsx"]
    end

    subgraph 分支2: 建设用地分析
        B2["建设用地<br/>count: 8000"]
        C2["建设用地缓冲区<br/>buffer: 1000m"]
        D2["影响范围图<br/>impact.geojson"]
    end

    subgraph 分支3: 数据导出
        B3["投影转换<br/>to: EPSG:4326"]
        D3["导出文件<br/>output.shp"]
    end

    A -->|"属性过滤<br/>type='耕地'"| B1
    A -->|"属性过滤<br/>type='建设用地'"| B2
    A -->|"投影转换"| B3

    B1 -->|"统计面积<br/>1.2s"| C1
    C1 -->|"导出报表"| D1

    B2 -->|"缓冲区分析<br/>3.5s"| C2
    C2 -->|"保存结果"| D2

    B3 -->|"格式转换"| D3

    style A fill:#e1f5ff
    style D1 fill:#c8e6c9
    style D2 fill:#c8e6c9
    style D3 fill:#c8e6c9
```

## 示例 4: 完整的 DolphinScheduler 工作流血缘

### 场景: 每日卫星影像处理流程

```
任务1: 下载影像
  ↓
任务2: 影像预处理 (裁剪、投影)
  ↓
任务3: 矢量化 (提取边界)
  ↓
任务4: 空间分析 (与已有数据叠加)
  ↓
任务5: 结果入库 (PostGIS)
  ↓
任务6: 通知 Meta 模块扫描
```

### Mermaid 流程图（带 DolphinScheduler 任务）

```mermaid
graph TD
    subgraph DolphinScheduler 工作流
        T1["Task 1: 下载影像<br/>Shell 任务<br/>wget http://..."]
        T2["Task 2: 影像预处理<br/>Python 任务<br/>GDAL 处理"]
        T3["Task 3: 矢量化<br/>Python 任务<br/>边界提取"]
        T4["Task 4: 空间分析<br/>Python 任务<br/>叠加分析"]
        T5["Task 5: 结果入库<br/>SQL 任务<br/>PostGIS"]
        T6["Task 6: 通知扫描<br/>HTTP 任务<br/>Meta API"]
    end

    subgraph 数据血缘
        D1["卫星影像<br/>source: remote<br/>format: TIF"]
        D2["裁剪后影像<br/>bbox: 120,30,121,31<br/>crs: EPSG:3857"]
        D3["边界矢量<br/>type: Polygon<br/>count: 150"]
        D4["叠加结果<br/>type: Polygon<br/>count: 89"]
        D5["PostGIS 表<br/>table: satellite_result<br/>schema: public"]
        D6["Meta 元数据<br/>scan_status: completed"]
    end

    T1 -->|"生成"| D1
    D1 --> T2
    T2 -->|"生成"| D2
    D2 --> T3
    T3 -->|"生成"| D3
    D3 --> T4
    T4 -->|"生成"| D4
    D4 --> T5
    T5 -->|"生成"| D5
    D5 --> T6
    T6 -->|"生成"| D6

    style T1 fill:#fff9c4
    style T2 fill:#fff9c4
    style T3 fill:#fff9c4
    style T4 fill:#fff9c4
    style T5 fill:#fff9c4
    style T6 fill:#fff9c4
    style D6 fill:#c8e6c9
```

## 示例 5: 实际 JSON 血缘文件

### 文件: `poi_buffer_analysis.lineage.json`

```json
{
  "graph_id": "lineage-2024-01-15-001",
  "pipeline_name": "POI缓冲区分析",
  "assets": {
    "asset-001": {
      "asset_id": "asset-001",
      "name": "POI点数据",
      "type": "file",
      "location": "/data/input/poi.shp",
      "schema_info": {
        "columns": ["id", "name", "type", "geometry"],
        "dtypes": {
          "id": "int64",
          "name": "object",
          "type": "object",
          "geometry": "geometry"
        },
        "crs": "EPSG:4326",
        "geometry_type": ["Point"]
      },
      "statistics": {
        "record_count": 1000,
        "bounds": [120.0, 30.0, 121.0, 31.0]
      },
      "created_at": "2024-01-15T10:00:00Z",
      "metadata": {
        "source_system": "GPS采集",
        "data_date": "2024-01-01"
      }
    },
    "asset-002": {
      "asset_id": "asset-002",
      "name": "POI_投影后",
      "type": "memory",
      "location": "memory://step_1",
      "schema_info": {
        "columns": ["id", "name", "type", "geometry"],
        "crs": "EPSG:3857",
        "geometry_type": ["Point"]
      },
      "statistics": {
        "record_count": 1000,
        "bounds": [13358338.9, 3503549.8, 13469594.4, 3621463.4]
      },
      "created_at": "2024-01-15T10:00:01Z",
      "metadata": {}
    },
    "asset-003": {
      "asset_id": "asset-003",
      "name": "POI_缓冲区",
      "type": "memory",
      "location": "memory://step_2",
      "schema_info": {
        "columns": ["id", "name", "type", "geometry"],
        "crs": "EPSG:3857",
        "geometry_type": ["Polygon"]
      },
      "statistics": {
        "record_count": 1000,
        "bounds": [13357838.9, 3503049.8, 13470094.4, 3621963.4],
        "total_area": 785398163.4
      },
      "created_at": "2024-01-15T10:00:02Z",
      "metadata": {}
    },
    "asset-004": {
      "asset_id": "asset-004",
      "name": "POI_过滤后",
      "type": "file",
      "location": "/data/output/poi_buffer_result.geojson",
      "schema_info": {
        "columns": ["id", "name", "type", "geometry"],
        "crs": "EPSG:3857",
        "geometry_type": ["Polygon"]
      },
      "statistics": {
        "record_count": 856,
        "bounds": [13357838.9, 3503049.8, 13470094.4, 3621963.4],
        "total_area": 750123456.7
      },
      "created_at": "2024-01-15T10:00:03Z",
      "metadata": {
        "output_format": "geojson",
        "file_size_mb": 5.23
      }
    }
  },
  "executions": {
    "exec-001": {
      "execution_id": "exec-001",
      "operator_name": "投影转换",
      "operator_type": "reproject",
      "parameters": {
        "from_crs": "EPSG:4326",
        "to_crs": "EPSG:3857"
      },
      "input_assets": ["asset-001"],
      "output_assets": ["asset-002"],
      "started_at": "2024-01-15T10:00:00.500Z",
      "finished_at": "2024-01-15T10:00:01.023Z",
      "elapsed_seconds": 0.523,
      "status": "success",
      "error_message": null,
      "metadata": {
        "cpu_percent": 45.2,
        "memory_mb": 128.5
      }
    },
    "exec-002": {
      "execution_id": "exec-002",
      "operator_name": "缓冲区分析",
      "operator_type": "buffer",
      "parameters": {
        "distance": 500,
        "unit": "meters"
      },
      "input_assets": ["asset-002"],
      "output_assets": ["asset-003"],
      "started_at": "2024-01-15T10:00:01.050Z",
      "finished_at": "2024-01-15T10:00:02.295Z",
      "elapsed_seconds": 1.245,
      "status": "success",
      "error_message": null,
      "metadata": {
        "cpu_percent": 78.3,
        "memory_mb": 256.8
      }
    },
    "exec-003": {
      "execution_id": "exec-003",
      "operator_name": "面积过滤",
      "operator_type": "filter",
      "parameters": {
        "min_area": 100000,
        "unit": "square_meters"
      },
      "input_assets": ["asset-003"],
      "output_assets": ["asset-004"],
      "started_at": "2024-01-15T10:00:02.320Z",
      "finished_at": "2024-01-15T10:00:02.432Z",
      "elapsed_seconds": 0.112,
      "status": "success",
      "error_message": null,
      "metadata": {
        "filtered_count": 144,
        "filter_ratio": 0.144
      }
    }
  },
  "root_assets": ["asset-001"],
  "leaf_assets": ["asset-004"],
  "created_at": "2024-01-15T10:00:00Z",
  "metadata": {
    "total_elapsed_seconds": 1.880,
    "pipeline_version": "1.0.0",
    "execution_environment": "DolphinScheduler",
    "workflow_id": "workflow-123",
    "workflow_instance_id": "instance-456"
  }
}
```

## 血缘查询示例代码

### Python 查询示例

```python
from lineage_tracker import LineageGraph

# 加载血缘图
graph = LineageGraph.load("poi_buffer_analysis.lineage.json")

# 1. 查看概览
print(f"流水线: {graph.pipeline_name}")
print(f"数据资产数: {len(graph.assets)}")
print(f"算子执行数: {len(graph.executions)}")
print(f"源数据: {graph.root_assets}")
print(f"最终结果: {graph.leaf_assets}")

# 2. 追溯源头
final_asset_id = graph.leaf_assets[0]
source_chain = graph.trace_to_source(final_asset_id)
print(f"\n数据血缘链:")
for asset_id in reversed(source_chain):
    asset = graph.assets[asset_id]
    print(f"  → {asset.name} ({asset.statistics['record_count']} 条)")

# 3. 统计分析
print(f"\n每步数据量变化:")
for exec_id, execution in graph.executions.items():
    input_asset = graph.assets[execution.input_assets[0]]
    output_asset = graph.assets[execution.output_assets[0]]

    input_count = input_asset.statistics['record_count']
    output_count = output_asset.statistics['record_count']

    print(f"{execution.operator_name}:")
    print(f"  {input_count} → {output_count} (保留 {output_count/input_count:.1%})")

# 4. 性能分析
total_time = sum(ex.elapsed_seconds for ex in graph.executions.values())
print(f"\n总执行时间: {total_time:.3f}s")
for ex in graph.executions.values():
    percent = ex.elapsed_seconds / total_time * 100
    print(f"  {ex.operator_name}: {ex.elapsed_seconds:.3f}s ({percent:.1f}%)")
```

### 输出示例

```
流水线: POI缓冲区分析
数据资产数: 4
算子执行数: 3
源数据: ['asset-001']
最终结果: ['asset-004']

数据血缘链:
  → POI点数据 (1000 条)
  → POI_投影后 (1000 条)
  → POI_缓冲区 (1000 条)
  → POI_过滤后 (856 条)

每步数据量变化:
投影转换:
  1000 → 1000 (保留 100.0%)
缓冲区分析:
  1000 → 1000 (保留 100.0%)
面积过滤:
  1000 → 856 (保留 85.6%)

总执行时间: 1.880s
  投影转换: 0.523s (27.8%)
  缓冲区分析: 1.245s (66.2%)
  面积过滤: 0.112s (6.0%)
```

## 总结

这些示例展示了：

1. ✅ **简单线性血缘** - 单输入单输出
2. ✅ **多输入血缘** - 空间叠加等场景
3. ✅ **分支血缘** - 一个数据源产生多个结果
4. ✅ **工作流级血缘** - DolphinScheduler 任务链
5. ✅ **完整 JSON 格式** - 实际存储格式
6. ✅ **查询和分析** - 血缘追溯和统计

通过这些可视化示例，可以清晰地理解 GIS 空间计算的数据血缘追踪机制。
