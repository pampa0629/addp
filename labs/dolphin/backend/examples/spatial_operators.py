#!/usr/bin/env python3
"""
空间算子编排示例
演示中间数据在内存中流转，最终结果落盘的完整流程
"""

import argparse
import json
from datetime import datetime
from pathlib import Path
from typing import Any, Callable, Dict, List

import geopandas as gpd
from shapely.geometry import shape, mapping
from shapely.ops import transform
import pyproj


class SpatialPipeline:
    """
    空间算子编排引擎

    特点:
    1. 中间数据在内存中流转（GeoDataFrame 对象）
    2. 算子链式调用
    3. 最终结果一次性落盘
    4. 支持多种输出格式
    """

    def __init__(self, name: str = "Unnamed Pipeline"):
        self.name = name
        self.steps: List[tuple] = []
        self.metadata: Dict[str, Any] = {
            "created_at": datetime.now().isoformat(),
            "total_steps": 0,
            "execution_log": []
        }

    def add_step(self, operator: Callable, name: str, **kwargs) -> 'SpatialPipeline':
        """
        添加算子到流水线

        Args:
            operator: 算子函数
            name: 算子名称（用于日志）
            **kwargs: 算子参数

        Returns:
            self: 支持链式调用
        """
        self.steps.append((operator, name, kwargs))
        self.metadata["total_steps"] += 1
        return self

    def execute(self, input_data: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
        """
        执行流水线（数据在内存中流转）

        Args:
            input_data: 输入的 GeoDataFrame

        Returns:
            处理后的 GeoDataFrame（仍在内存中）
        """
        print(f"\n{'='*60}")
        print(f"执行流水线: {self.name}")
        print(f"总步骤数: {self.metadata['total_steps']}")
        print(f"{'='*60}\n")

        data = input_data.copy()
        print(f"输入数据: {len(data)} 条记录")

        for i, (operator, name, kwargs) in enumerate(self.steps, 1):
            start_time = datetime.now()
            print(f"\n[步骤 {i}/{self.metadata['total_steps']}] 执行算子: {name}")
            print(f"  参数: {kwargs}")

            # 执行算子（数据在内存中）
            data = operator(data, **kwargs)

            elapsed = (datetime.now() - start_time).total_seconds()
            print(f"  ✓ 完成，耗时 {elapsed:.3f}s，当前记录数: {len(data)}")

            # 记录执行日志
            self.metadata["execution_log"].append({
                "step": i,
                "operator": name,
                "elapsed_seconds": elapsed,
                "result_count": len(data)
            })

        print(f"\n{'='*60}")
        print(f"流水线执行完成！最终记录数: {len(data)}")
        print(f"{'='*60}\n")

        return data

    def save_result(
        self,
        result: gpd.GeoDataFrame,
        output_path: str,
        format: str = "auto",
        metadata_path: str = None
    ):
        """
        保存最终结果到磁盘（一次性落盘）

        Args:
            result: 流水线执行结果（内存中的 GeoDataFrame）
            output_path: 输出文件路径
            format: 输出格式 (auto/shapefile/geojson/gpkg/postgis)
            metadata_path: 元数据保存路径（可选）
        """
        print(f"\n{'='*60}")
        print(f"保存结果到磁盘")
        print(f"{'='*60}\n")

        output_file = Path(output_path)
        output_file.parent.mkdir(parents=True, exist_ok=True)

        # 自动检测格式
        if format == "auto":
            ext = output_file.suffix.lower()
            format_map = {
                ".shp": "shapefile",
                ".geojson": "geojson",
                ".json": "geojson",
                ".gpkg": "gpkg",
                ".geoparquet": "parquet"
            }
            format = format_map.get(ext, "geojson")

        start_time = datetime.now()

        # 根据格式保存
        if format == "shapefile":
            print(f"  格式: Shapefile")
            result.to_file(output_path, driver="ESRI Shapefile")

        elif format == "geojson":
            print(f"  格式: GeoJSON")
            result.to_file(output_path, driver="GeoJSON")

        elif format == "gpkg":
            print(f"  格式: GeoPackage")
            result.to_file(output_path, driver="GPKG")

        elif format == "parquet":
            print(f"  格式: GeoParquet")
            result.to_parquet(output_path)

        elif format == "postgis":
            # 示例：保存到 PostGIS（需要数据库连接）
            print(f"  格式: PostGIS")
            # from sqlalchemy import create_engine
            # engine = create_engine('postgresql://user:pass@localhost:5432/db')
            # result.to_postgis('table_name', engine, if_exists='replace')
            raise NotImplementedError("PostGIS 保存需要配置数据库连接")

        else:
            raise ValueError(f"不支持的格式: {format}")

        elapsed = (datetime.now() - start_time).total_seconds()
        file_size = output_file.stat().st_size / 1024 / 1024  # MB

        print(f"  ✓ 保存完成")
        print(f"  文件路径: {output_path}")
        print(f"  文件大小: {file_size:.2f} MB")
        print(f"  写入耗时: {elapsed:.3f}s")

        # 保存元数据
        if metadata_path:
            self.metadata["output_file"] = str(output_path)
            self.metadata["output_format"] = format
            self.metadata["output_size_mb"] = file_size
            self.metadata["final_record_count"] = len(result)
            self.metadata["saved_at"] = datetime.now().isoformat()

            with open(metadata_path, 'w', encoding='utf-8') as f:
                json.dump(self.metadata, f, indent=2, ensure_ascii=False)

            print(f"  元数据已保存: {metadata_path}")

        print(f"\n{'='*60}\n")


# ============================================
# 空间算子实现（操作内存中的 GeoDataFrame）
# ============================================

def reproject(gdf: gpd.GeoDataFrame, from_crs: str = None, to_crs: str = "EPSG:3857") -> gpd.GeoDataFrame:
    """投影转换算子"""
    if from_crs:
        gdf = gdf.set_crs(from_crs, allow_override=True)
    return gdf.to_crs(to_crs)


def buffer(gdf: gpd.GeoDataFrame, distance: float = 100) -> gpd.GeoDataFrame:
    """缓冲区算子"""
    gdf = gdf.copy()
    gdf['geometry'] = gdf.geometry.buffer(distance)
    return gdf


def intersect(gdf: gpd.GeoDataFrame, clip_layer_path: str) -> gpd.GeoDataFrame:
    """空间叠加算子（裁剪）"""
    clip_layer = gpd.read_file(clip_layer_path)
    return gpd.overlay(gdf, clip_layer, how='intersection')


def filter_by_area(gdf: gpd.GeoDataFrame, min_area: float = 0, max_area: float = float('inf')) -> gpd.GeoDataFrame:
    """面积过滤算子"""
    gdf = gdf.copy()
    gdf['_area'] = gdf.geometry.area
    mask = (gdf['_area'] >= min_area) & (gdf['_area'] <= max_area)
    result = gdf[mask].copy()
    result = result.drop(columns=['_area'])
    return result


def add_centroid(gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """添加质心坐标算子"""
    gdf = gdf.copy()
    gdf['centroid_x'] = gdf.geometry.centroid.x
    gdf['centroid_y'] = gdf.geometry.centroid.y
    return gdf


def simplify(gdf: gpd.GeoDataFrame, tolerance: float = 10) -> gpd.GeoDataFrame:
    """几何简化算子"""
    gdf = gdf.copy()
    gdf['geometry'] = gdf.geometry.simplify(tolerance)
    return gdf


# ============================================
# 示例工作流
# ============================================

def example_workflow_1():
    """
    示例 1: 土地利用缓冲区分析

    数据流:
    Shapefile (磁盘)
      → 读入内存 (GeoDataFrame)
      → 投影转换 (内存)
      → 缓冲区 (内存)
      → 面积过滤 (内存)
      → 添加质心 (内存)
      → 保存结果 (落盘 GeoJSON)
    """
    print("\n" + "="*70)
    print("示例 1: 土地利用缓冲区分析")
    print("="*70)

    # 1. 读取输入数据（从磁盘到内存）
    print("\n[读取输入数据]")
    input_file = "data/landuse.shp"
    print(f"  读取文件: {input_file}")

    # 模拟数据（实际使用时替换为真实文件）
    from shapely.geometry import Point
    gdf = gpd.GeoDataFrame({
        'id': [1, 2, 3],
        'name': ['Park', 'School', 'Hospital'],
        'geometry': [Point(0, 0), Point(1, 1), Point(2, 2)]
    }, crs="EPSG:4326")
    print(f"  ✓ 读取完成，共 {len(gdf)} 条记录")

    # 2. 构建流水线（算子链）
    pipeline = (
        SpatialPipeline("土地利用缓冲区分析")
        .add_step(reproject, "投影转换", from_crs="EPSG:4326", to_crs="EPSG:3857")
        .add_step(buffer, "缓冲区分析", distance=500)  # 500米缓冲区
        .add_step(filter_by_area, "面积过滤", min_area=100000)  # 过滤小于 0.1 平方公里
        .add_step(add_centroid, "添加质心坐标")
        .add_step(simplify, "几何简化", tolerance=10)
    )

    # 3. 执行流水线（数据在内存中流转）
    result = pipeline.execute(gdf)

    # 4. 保存结果（一次性落盘）
    output_file = "output/landuse_buffer_result.geojson"
    metadata_file = "output/landuse_buffer_metadata.json"
    pipeline.save_result(result, output_file, format="geojson", metadata_path=metadata_file)

    return result


def example_workflow_2():
    """
    示例 2: 多步骤空间分析（适合 DolphinScheduler 调度）
    """
    print("\n" + "="*70)
    print("示例 2: 复杂空间分析流水线")
    print("="*70)

    # 模拟输入数据
    from shapely.geometry import Polygon
    gdf = gpd.GeoDataFrame({
        'id': [1, 2, 3, 4, 5],
        'type': ['residential', 'commercial', 'industrial', 'park', 'school'],
        'geometry': [
            Polygon([(i, i), (i+1, i), (i+1, i+1), (i, i+1)])
            for i in range(5)
        ]
    }, crs="EPSG:4326")

    # 复杂流水线
    pipeline = (
        SpatialPipeline("复杂空间分析")
        .add_step(reproject, "WGS84转Web墨卡托", to_crs="EPSG:3857")
        .add_step(buffer, "100米缓冲区", distance=100)
        .add_step(filter_by_area, "过滤小面积", min_area=5000)
        .add_step(add_centroid, "计算质心")
        .add_step(reproject, "转回WGS84", to_crs="EPSG:4326")
    )

    result = pipeline.execute(gdf)

    # 保存为 GeoPackage（更现代的格式）
    pipeline.save_result(
        result,
        "output/complex_analysis.gpkg",
        format="gpkg",
        metadata_path="output/complex_analysis_metadata.json"
    )

    return result


# ============================================
# 命令行接口（供 DolphinScheduler 调用）
# ============================================

def main():
    """
    命令行入口

    DolphinScheduler Shell 任务示例:
    python spatial_operators.py \
      --input data/input.shp \
      --output output/result.geojson \
      --buffer-distance 500 \
      --min-area 100000 \
      --format geojson
    """
    parser = argparse.ArgumentParser(description="空间算子流水线")
    parser.add_argument("--input", required=True, help="输入文件路径")
    parser.add_argument("--output", required=True, help="输出文件路径")
    parser.add_argument("--format", default="auto", help="输出格式")
    parser.add_argument("--buffer-distance", type=float, default=100, help="缓冲区距离（米）")
    parser.add_argument("--min-area", type=float, default=0, help="最小面积过滤")
    parser.add_argument("--simplify-tolerance", type=float, default=10, help="简化容差")
    parser.add_argument("--metadata", help="元数据输出路径")

    args = parser.parse_args()

    # 读取输入
    print(f"读取输入文件: {args.input}")
    gdf = gpd.read_file(args.input)

    # 构建流水线
    pipeline = (
        SpatialPipeline(f"Processing {Path(args.input).name}")
        .add_step(buffer, "缓冲区分析", distance=args.buffer_distance)
        .add_step(filter_by_area, "面积过滤", min_area=args.min_area)
        .add_step(simplify, "几何简化", tolerance=args.simplify_tolerance)
        .add_step(add_centroid, "添加质心")
    )

    # 执行（内存中）
    result = pipeline.execute(gdf)

    # 落盘
    pipeline.save_result(result, args.output, format=args.format, metadata_path=args.metadata)

    print("\n✓ 处理完成！")


if __name__ == "__main__":
    # 运行示例
    # example_workflow_1()
    # example_workflow_2()

    # 或者作为命令行工具
    main()
