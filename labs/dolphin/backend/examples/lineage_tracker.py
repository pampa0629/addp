#!/usr/bin/env python3
"""
GIS 空间计算的数据血缘追踪系统

核心概念:
1. 每个算子执行都记录输入输出
2. 构建 DAG（有向无环图）表示血缘关系
3. 支持正向追踪（数据来源）和反向追踪（数据去向）
4. 与 ADDP Meta 模块集成
"""

import json
import uuid
from datetime import datetime
from pathlib import Path
from typing import Any, Dict, List, Optional
from dataclasses import dataclass, asdict
from enum import Enum

import geopandas as gpd
from shapely.geometry import shape


class DataSourceType(Enum):
    """数据源类型"""
    FILE = "file"              # 文件（Shapefile, GeoJSON, etc.）
    DATABASE = "database"       # 数据库（PostGIS, PostgreSQL）
    API = "api"                # API 接口
    MEMORY = "memory"          # 内存中的中间结果
    OBJECT_STORAGE = "object"  # 对象存储（MinIO, S3）


@dataclass
class DataAsset:
    """数据资产（血缘追踪的基本单位）"""
    asset_id: str               # 唯一标识
    name: str                   # 数据名称
    type: DataSourceType        # 数据类型
    location: str               # 存储位置（路径/URL/表名）
    schema_info: Dict           # Schema 信息（字段、类型）
    statistics: Dict            # 统计信息（记录数、范围）
    created_at: str             # 创建时间
    metadata: Dict              # 额外元数据

    def to_dict(self) -> Dict:
        d = asdict(self)
        d['type'] = self.type.value
        return d

    @classmethod
    def from_geodataframe(cls, gdf: gpd.GeoDataFrame, name: str, location: str) -> 'DataAsset':
        """从 GeoDataFrame 创建数据资产"""
        return cls(
            asset_id=str(uuid.uuid4()),
            name=name,
            type=DataSourceType.MEMORY,
            location=location,
            schema_info={
                'columns': list(gdf.columns),
                'dtypes': {col: str(dtype) for col, dtype in gdf.dtypes.items()},
                'crs': str(gdf.crs) if gdf.crs else None,
                'geometry_type': gdf.geometry.geom_type.unique().tolist()
            },
            statistics={
                'record_count': len(gdf),
                'bounds': gdf.total_bounds.tolist() if len(gdf) > 0 else None,
                'total_area': float(gdf.geometry.area.sum()) if 'area' in dir(gdf.geometry) else None
            },
            created_at=datetime.now().isoformat(),
            metadata={}
        )


@dataclass
class OperatorExecution:
    """算子执行记录（血缘链中的节点）"""
    execution_id: str           # 执行唯一标识
    operator_name: str          # 算子名称
    operator_type: str          # 算子类型（buffer, intersect, etc.）
    parameters: Dict            # 执行参数
    input_assets: List[str]     # 输入数据资产 ID 列表
    output_assets: List[str]    # 输出数据资产 ID 列表
    started_at: str             # 开始时间
    finished_at: str            # 结束时间
    elapsed_seconds: float      # 执行耗时
    status: str                 # 状态（success, failed）
    error_message: Optional[str] # 错误信息
    metadata: Dict              # 额外元数据

    def to_dict(self) -> Dict:
        return asdict(self)


@dataclass
class LineageGraph:
    """血缘图（完整的数据流转记录）"""
    graph_id: str               # 图唯一标识
    pipeline_name: str          # 流水线名称
    assets: Dict[str, DataAsset]              # 数据资产映射
    executions: Dict[str, OperatorExecution]  # 算子执行映射
    root_assets: List[str]      # 根数据资产（起点）
    leaf_assets: List[str]      # 叶数据资产（终点）
    created_at: str             # 创建时间
    metadata: Dict              # 额外元数据

    def to_dict(self) -> Dict:
        return {
            'graph_id': self.graph_id,
            'pipeline_name': self.pipeline_name,
            'assets': {k: v.to_dict() for k, v in self.assets.items()},
            'executions': {k: v.to_dict() for k, v in self.executions.items()},
            'root_assets': self.root_assets,
            'leaf_assets': self.leaf_assets,
            'created_at': self.created_at,
            'metadata': self.metadata
        }

    def save(self, path: str):
        """保存血缘图到文件"""
        with open(path, 'w', encoding='utf-8') as f:
            json.dump(self.to_dict(), f, indent=2, ensure_ascii=False)

    @classmethod
    def load(cls, path: str) -> 'LineageGraph':
        """从文件加载血缘图"""
        with open(path, 'r', encoding='utf-8') as f:
            data = json.load(f)

        # 重建对象
        assets = {
            k: DataAsset(**{**v, 'type': DataSourceType(v['type'])})
            for k, v in data['assets'].items()
        }
        executions = {
            k: OperatorExecution(**v)
            for k, v in data['executions'].items()
        }

        return cls(
            graph_id=data['graph_id'],
            pipeline_name=data['pipeline_name'],
            assets=assets,
            executions=executions,
            root_assets=data['root_assets'],
            leaf_assets=data['leaf_assets'],
            created_at=data['created_at'],
            metadata=data['metadata']
        )

    def get_upstream(self, asset_id: str) -> List[str]:
        """获取上游数据资产（正向血缘）"""
        upstream = []
        for exec_id, execution in self.executions.items():
            if asset_id in execution.output_assets:
                upstream.extend(execution.input_assets)
        return list(set(upstream))

    def get_downstream(self, asset_id: str) -> List[str]:
        """获取下游数据资产（反向血缘）"""
        downstream = []
        for exec_id, execution in self.executions.items():
            if asset_id in execution.input_assets:
                downstream.extend(execution.output_assets)
        return list(set(downstream))

    def trace_to_source(self, asset_id: str) -> List[str]:
        """追溯到源头数据（递归上游）"""
        path = [asset_id]
        upstream = self.get_upstream(asset_id)
        if not upstream:
            return path

        # DFS 追溯
        for up_id in upstream:
            path.extend(self.trace_to_source(up_id))
        return path

    def trace_to_leaf(self, asset_id: str) -> List[str]:
        """追踪到最终数据（递归下游）"""
        path = [asset_id]
        downstream = self.get_downstream(asset_id)
        if not downstream:
            return path

        # DFS 追踪
        for down_id in downstream:
            path.extend(self.trace_to_leaf(down_id))
        return path

    def export_mermaid(self) -> str:
        """导出为 Mermaid 流程图（可视化）"""
        lines = ["graph TD"]

        # 添加节点
        for asset_id, asset in self.assets.items():
            label = f"{asset.name}<br/>{asset.statistics.get('record_count', 0)} records"
            lines.append(f'    {asset_id}["{label}"]')

        # 添加边（通过执行连接）
        for exec_id, execution in self.executions.items():
            for input_id in execution.input_assets:
                for output_id in execution.output_assets:
                    label = f"{execution.operator_name}<br/>{execution.elapsed_seconds:.2f}s"
                    lines.append(f'    {input_id} -->|"{label}"| {output_id}')

        return '\n'.join(lines)


class LineageTracker:
    """
    血缘追踪器（集成到空间算子流水线）

    使用方式:
    tracker = LineageTracker("土地利用分析")
    tracker.record_input(...)
    tracker.record_execution(...)
    tracker.record_output(...)
    tracker.save_lineage("lineage.json")
    """

    def __init__(self, pipeline_name: str):
        self.graph = LineageGraph(
            graph_id=str(uuid.uuid4()),
            pipeline_name=pipeline_name,
            assets={},
            executions={},
            root_assets=[],
            leaf_assets=[],
            created_at=datetime.now().isoformat(),
            metadata={}
        )
        self.current_execution: Optional[OperatorExecution] = None

    def register_asset(self, asset: DataAsset) -> str:
        """注册数据资产"""
        self.graph.assets[asset.asset_id] = asset
        return asset.asset_id

    def start_execution(self, operator_name: str, operator_type: str, parameters: Dict) -> str:
        """开始记录算子执行"""
        self.current_execution = OperatorExecution(
            execution_id=str(uuid.uuid4()),
            operator_name=operator_name,
            operator_type=operator_type,
            parameters=parameters,
            input_assets=[],
            output_assets=[],
            started_at=datetime.now().isoformat(),
            finished_at="",
            elapsed_seconds=0.0,
            status="running",
            error_message=None,
            metadata={}
        )
        return self.current_execution.execution_id

    def add_input(self, asset_id: str):
        """添加输入数据资产"""
        if self.current_execution:
            self.current_execution.input_assets.append(asset_id)

    def add_output(self, asset_id: str):
        """添加输出数据资产"""
        if self.current_execution:
            self.current_execution.output_assets.append(asset_id)
            # 更新叶节点
            if asset_id not in self.graph.leaf_assets:
                self.graph.leaf_assets.append(asset_id)

    def finish_execution(self, success: bool = True, error_message: str = None):
        """结束算子执行记录"""
        if self.current_execution:
            now = datetime.now()
            started = datetime.fromisoformat(self.current_execution.started_at)
            self.current_execution.finished_at = now.isoformat()
            self.current_execution.elapsed_seconds = (now - started).total_seconds()
            self.current_execution.status = "success" if success else "failed"
            self.current_execution.error_message = error_message

            # 保存到图中
            self.graph.executions[self.current_execution.execution_id] = self.current_execution
            self.current_execution = None

    def save_lineage(self, path: str):
        """保存血缘图"""
        self.graph.save(path)
        print(f"✓ 血缘图已保存: {path}")

    def export_to_meta_module(self, meta_api_url: str):
        """导出血缘数据到 ADDP Meta 模块"""
        import requests

        payload = {
            'lineage_graph': self.graph.to_dict(),
            'timestamp': datetime.now().isoformat()
        }

        response = requests.post(
            f"{meta_api_url}/api/lineage",
            json=payload
        )

        if response.status_code == 200:
            print(f"✓ 血缘数据已上传到 Meta 模块")
        else:
            print(f"✗ 上传失败: {response.text}")


# ============================================
# 集成到空间算子流水线
# ============================================

class SpatialPipelineWithLineage:
    """
    支持血缘追踪的空间算子流水线
    """

    def __init__(self, name: str, enable_lineage: bool = True):
        self.name = name
        self.steps = []
        self.enable_lineage = enable_lineage

        if enable_lineage:
            self.tracker = LineageTracker(name)
        else:
            self.tracker = None

    def add_step(self, operator, name: str, operator_type: str, **kwargs):
        """添加算子"""
        self.steps.append((operator, name, operator_type, kwargs))
        return self

    def execute(self, input_data: gpd.GeoDataFrame, input_name: str = "input") -> gpd.GeoDataFrame:
        """执行流水线（带血缘追踪）"""
        print(f"\n{'='*60}")
        print(f"执行流水线: {self.name}")
        print(f"血缘追踪: {'启用' if self.enable_lineage else '禁用'}")
        print(f"{'='*60}\n")

        # 记录输入资产
        current_data = input_data.copy()
        if self.tracker:
            input_asset = DataAsset.from_geodataframe(current_data, input_name, "memory://input")
            current_asset_id = self.tracker.register_asset(input_asset)
            self.tracker.graph.root_assets.append(current_asset_id)
            print(f"📊 输入数据资产 ID: {current_asset_id}")

        # 执行每个算子
        for i, (operator, name, op_type, kwargs) in enumerate(self.steps, 1):
            print(f"\n[步骤 {i}] 执行算子: {name}")

            # 开始血缘记录
            if self.tracker:
                exec_id = self.tracker.start_execution(name, op_type, kwargs)
                self.tracker.add_input(current_asset_id)

            try:
                # 执行算子
                start = datetime.now()
                result_data = operator(current_data, **kwargs)
                elapsed = (datetime.now() - start).total_seconds()

                print(f"  ✓ 完成，耗时 {elapsed:.3f}s，记录数: {len(result_data)}")

                # 记录输出资产
                if self.tracker:
                    output_asset = DataAsset.from_geodataframe(
                        result_data,
                        f"{name}_output",
                        f"memory://step_{i}"
                    )
                    output_asset_id = self.tracker.register_asset(output_asset)
                    self.tracker.add_output(output_asset_id)
                    self.tracker.finish_execution(success=True)
                    print(f"  📊 输出数据资产 ID: {output_asset_id}")

                    # 更新当前资产
                    current_asset_id = output_asset_id

                current_data = result_data

            except Exception as e:
                print(f"  ✗ 失败: {str(e)}")
                if self.tracker:
                    self.tracker.finish_execution(success=False, error_message=str(e))
                raise

        print(f"\n{'='*60}")
        print(f"流水线执行完成！")
        print(f"{'='*60}\n")

        return current_data

    def save_result(self, result: gpd.GeoDataFrame, output_path: str, lineage_path: str = None):
        """保存结果和血缘图"""
        # 保存空间数据
        result.to_file(output_path)
        print(f"✓ 结果已保存: {output_path}")

        # 保存血缘图
        if self.tracker:
            if lineage_path is None:
                lineage_path = str(Path(output_path).with_suffix('.lineage.json'))
            self.tracker.save_lineage(lineage_path)

            # 打印血缘摘要
            print(f"\n血缘摘要:")
            print(f"  数据资产数: {len(self.tracker.graph.assets)}")
            print(f"  算子执行数: {len(self.tracker.graph.executions)}")
            print(f"  根资产数: {len(self.tracker.graph.root_assets)}")
            print(f"  叶资产数: {len(self.tracker.graph.leaf_assets)}")


# ============================================
# 使用示例
# ============================================

def example_with_lineage():
    """带血缘追踪的空间分析示例"""
    from shapely.geometry import Point

    # 模拟输入数据
    gdf = gpd.GeoDataFrame({
        'id': [1, 2, 3],
        'name': ['Park', 'School', 'Hospital'],
        'geometry': [Point(0, 0), Point(1, 1), Point(2, 2)]
    }, crs="EPSG:4326")

    # 定义算子
    def reproject(gdf, to_crs):
        return gdf.to_crs(to_crs)

    def buffer(gdf, distance):
        gdf = gdf.copy()
        gdf['geometry'] = gdf.geometry.buffer(distance)
        return gdf

    def filter_by_area(gdf, min_area):
        gdf = gdf.copy()
        return gdf[gdf.geometry.area >= min_area]

    # 构建流水线
    pipeline = (
        SpatialPipelineWithLineage("POI缓冲区分析", enable_lineage=True)
        .add_step(reproject, "投影转换", "reproject", to_crs="EPSG:3857")
        .add_step(buffer, "500米缓冲区", "buffer", distance=500)
        .add_step(filter_by_area, "面积过滤", "filter", min_area=100000)
    )

    # 执行
    result = pipeline.execute(gdf, input_name="POI数据")

    # 保存结果和血缘
    pipeline.save_result(
        result,
        "output/poi_buffer.geojson",
        lineage_path="output/poi_buffer.lineage.json"
    )

    # 可视化血缘图
    mermaid = pipeline.tracker.graph.export_mermaid()
    print("\n\nMermaid 流程图:")
    print(mermaid)


if __name__ == "__main__":
    example_with_lineage()
