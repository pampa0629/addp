"""
Spatial Analysis Activities for Temporal Workflows

包装现有的 GeoPandas 空间算子为 Temporal Activities
复用 backend/spatial/ 中的算子实现
"""

import logging
from typing import Dict, Any, List
from dataclasses import dataclass
from pathlib import Path

from temporalio import activity
import geopandas as gpd
from shapely.geometry import shape, mapping

logger = logging.getLogger(__name__)


@dataclass
class ActivityResult:
    """Activity 执行结果"""
    success: bool
    output_path: str = None
    record_count: int = 0
    error: str = None
    metadata: Dict[str, Any] = None


@activity.defn(name="buffer")
async def buffer_activity(
    input_path: str,
    distance: float,
    output_path: str = None,
    segments: int = 16
) -> Dict[str, Any]:
    """
    缓冲区分析 Activity

    Args:
        input_path: 输入 GeoJSON/Shapefile 路径
        distance: 缓冲距离（单位取决于坐标系）
        output_path: 输出路径（可选，默认生成临时文件）
        segments: 圆弧段数

    Returns:
        ActivityResult 字典
    """
    logger.info(f"🔵 执行缓冲区分析: input={input_path}, distance={distance}")

    try:
        # 读取数据
        gdf = gpd.read_file(input_path)
        logger.info(f"  读取 {len(gdf)} 条记录")

        # 执行缓冲区
        gdf['geometry'] = gdf.geometry.buffer(distance, resolution=segments)

        # 生成输出路径
        if output_path is None:
            output_dir = Path(input_path).parent / "temp"
            output_dir.mkdir(exist_ok=True)
            output_path = str(output_dir / f"{Path(input_path).stem}_buffer.geojson")

        # 保存结果
        Path(output_path).parent.mkdir(parents=True, exist_ok=True)
        gdf.to_file(output_path, driver='GeoJSON')

        result = ActivityResult(
            success=True,
            output_path=output_path,
            record_count=len(gdf),
            metadata={"distance": distance, "segments": segments}
        )

        logger.info(f"  ✅ 完成，输出: {output_path}")
        return result.__dict__

    except Exception as e:
        logger.error(f"  ❌ 失败: {str(e)}")
        return ActivityResult(success=False, error=str(e)).__dict__


@activity.defn(name="reproject")
async def reproject_activity(
    input_path: str,
    target_crs: str,
    output_path: str = None,
    source_crs: str = None
) -> Dict[str, Any]:
    """
    投影转换 Activity

    Args:
        input_path: 输入文件路径
        target_crs: 目标坐标系 (e.g., "EPSG:3857")
        output_path: 输出路径
        source_crs: 源坐标系（可选，自动检测）

    Returns:
        ActivityResult 字典
    """
    logger.info(f"🔵 执行投影转换: {source_crs or 'auto'} → {target_crs}")

    try:
        gdf = gpd.read_file(input_path)

        # 设置源坐标系（如果指定）
        if source_crs:
            gdf = gdf.set_crs(source_crs, allow_override=True)

        original_crs = str(gdf.crs)

        # 转换投影
        gdf = gdf.to_crs(target_crs)

        # 生成输出路径
        if output_path is None:
            output_dir = Path(input_path).parent / "temp"
            output_dir.mkdir(exist_ok=True)
            output_path = str(output_dir / f"{Path(input_path).stem}_reprojected.geojson")

        Path(output_path).parent.mkdir(parents=True, exist_ok=True)
        gdf.to_file(output_path, driver='GeoJSON')

        result = ActivityResult(
            success=True,
            output_path=output_path,
            record_count=len(gdf),
            metadata={"original_crs": original_crs, "target_crs": target_crs}
        )

        logger.info(f"  ✅ 完成: {original_crs} → {target_crs}")
        return result.__dict__

    except Exception as e:
        logger.error(f"  ❌ 失败: {str(e)}")
        return ActivityResult(success=False, error=str(e)).__dict__


@activity.defn(name="overlay")
async def overlay_activity(
    input_path: str,
    clip_layer_path: str,
    output_path: str = None,
    how: str = "intersection"
) -> Dict[str, Any]:
    """
    空间叠加 Activity

    Args:
        input_path: 输入图层路径
        clip_layer_path: 裁剪图层路径
        output_path: 输出路径
        how: 叠加方式 (intersection/union/difference/identity)

    Returns:
        ActivityResult 字典
    """
    logger.info(f"🔵 执行空间叠加: how={how}")

    try:
        gdf1 = gpd.read_file(input_path)
        gdf2 = gpd.read_file(clip_layer_path)

        logger.info(f"  输入图层: {len(gdf1)} 条记录")
        logger.info(f"  裁剪图层: {len(gdf2)} 条记录")

        # 确保坐标系一致
        if gdf1.crs != gdf2.crs:
            logger.info(f"  坐标系不一致，转换中...")
            gdf2 = gdf2.to_crs(gdf1.crs)

        # 执行叠加
        result_gdf = gpd.overlay(gdf1, gdf2, how=how)

        # 生成输出路径
        if output_path is None:
            output_dir = Path(input_path).parent / "temp"
            output_dir.mkdir(exist_ok=True)
            output_path = str(output_dir / f"{Path(input_path).stem}_overlay.geojson")

        Path(output_path).parent.mkdir(parents=True, exist_ok=True)
        result_gdf.to_file(output_path, driver='GeoJSON')

        result = ActivityResult(
            success=True,
            output_path=output_path,
            record_count=len(result_gdf),
            metadata={"overlay_type": how, "input_records": len(gdf1), "clip_records": len(gdf2)}
        )

        logger.info(f"  ✅ 完成，结果: {len(result_gdf)} 条记录")
        return result.__dict__

    except Exception as e:
        logger.error(f"  ❌ 失败: {str(e)}")
        return ActivityResult(success=False, error=str(e)).__dict__


@activity.defn(name="filter_by_area")
async def filter_by_area_activity(
    input_path: str,
    min_area: float = 0,
    max_area: float = float('inf'),
    output_path: str = None
) -> Dict[str, Any]:
    """
    面积过滤 Activity

    Args:
        input_path: 输入文件路径
        min_area: 最小面积
        max_area: 最大面积
        output_path: 输出路径

    Returns:
        ActivityResult 字典
    """
    logger.info(f"🔵 执行面积过滤: {min_area} <= area <= {max_area}")

    try:
        gdf = gpd.read_file(input_path)
        original_count = len(gdf)

        # 计算面积
        gdf['_area'] = gdf.geometry.area

        # 过滤
        mask = (gdf['_area'] >= min_area) & (gdf['_area'] <= max_area)
        result_gdf = gdf[mask].copy()
        result_gdf = result_gdf.drop(columns=['_area'])

        # 生成输出路径
        if output_path is None:
            output_dir = Path(input_path).parent / "temp"
            output_dir.mkdir(exist_ok=True)
            output_path = str(output_dir / f"{Path(input_path).stem}_filtered.geojson")

        Path(output_path).parent.mkdir(parents=True, exist_ok=True)
        result_gdf.to_file(output_path, driver='GeoJSON')

        result = ActivityResult(
            success=True,
            output_path=output_path,
            record_count=len(result_gdf),
            metadata={
                "min_area": min_area,
                "max_area": max_area,
                "original_count": original_count,
                "filtered_count": len(result_gdf)
            }
        )

        logger.info(f"  ✅ 完成: {original_count} → {len(result_gdf)} 条记录")
        return result.__dict__

    except Exception as e:
        logger.error(f"  ❌ 失败: {str(e)}")
        return ActivityResult(success=False, error=str(e)).__dict__


@activity.defn(name="add_centroid")
async def add_centroid_activity(
    input_path: str,
    output_path: str = None
) -> Dict[str, Any]:
    """
    添加质心坐标 Activity

    Args:
        input_path: 输入文件路径
        output_path: 输出路径

    Returns:
        ActivityResult 字典
    """
    logger.info(f"🔵 添加质心坐标")

    try:
        gdf = gpd.read_file(input_path)

        # 计算质心
        gdf['centroid_x'] = gdf.geometry.centroid.x
        gdf['centroid_y'] = gdf.geometry.centroid.y

        # 生成输出路径
        if output_path is None:
            output_dir = Path(input_path).parent / "temp"
            output_dir.mkdir(exist_ok=True)
            output_path = str(output_dir / f"{Path(input_path).stem}_centroids.geojson")

        Path(output_path).parent.mkdir(parents=True, exist_ok=True)
        gdf.to_file(output_path, driver='GeoJSON')

        result = ActivityResult(
            success=True,
            output_path=output_path,
            record_count=len(gdf),
            metadata={"added_fields": ["centroid_x", "centroid_y"]}
        )

        logger.info(f"  ✅ 完成")
        return result.__dict__

    except Exception as e:
        logger.error(f"  ❌ 失败: {str(e)}")
        return ActivityResult(success=False, error=str(e)).__dict__


@activity.defn(name="simplify")
async def simplify_activity(
    input_path: str,
    tolerance: float,
    output_path: str = None
) -> Dict[str, Any]:
    """
    几何简化 Activity

    Args:
        input_path: 输入文件路径
        tolerance: 简化容差
        output_path: 输出路径

    Returns:
        ActivityResult 字典
    """
    logger.info(f"🔵 执行几何简化: tolerance={tolerance}")

    try:
        gdf = gpd.read_file(input_path)

        # 简化几何
        gdf['geometry'] = gdf.geometry.simplify(tolerance)

        # 生成输出路径
        if output_path is None:
            output_dir = Path(input_path).parent / "temp"
            output_dir.mkdir(exist_ok=True)
            output_path = str(output_dir / f"{Path(input_path).stem}_simplified.geojson")

        Path(output_path).parent.mkdir(parents=True, exist_ok=True)
        gdf.to_file(output_path, driver='GeoJSON')

        result = ActivityResult(
            success=True,
            output_path=output_path,
            record_count=len(gdf),
            metadata={"tolerance": tolerance}
        )

        logger.info(f"  ✅ 完成")
        return result.__dict__

    except Exception as e:
        logger.error(f"  ❌ 失败: {str(e)}")
        return ActivityResult(success=False, error=str(e)).__dict__


@activity.defn(name="union")
async def union_activity(
    input_paths: List[str],
    output_path: str
) -> Dict[str, Any]:
    """
    几何合并 Activity

    Args:
        input_paths: 输入文件路径列表
        output_path: 输出路径

    Returns:
        ActivityResult 字典
    """
    logger.info(f"🔵 执行几何合并: {len(input_paths)} 个图层")

    try:
        # 读取所有图层
        gdfs = [gpd.read_file(path) for path in input_paths]

        # 确保坐标系一致
        target_crs = gdfs[0].crs
        for i, gdf in enumerate(gdfs[1:], 1):
            if gdf.crs != target_crs:
                logger.info(f"  转换图层 {i} 的坐标系")
                gdfs[i] = gdf.to_crs(target_crs)

        # 合并所有图层
        result_gdf = gpd.GeoDataFrame(pd.concat(gdfs, ignore_index=True), crs=target_crs)

        # 执行几何合并（dissolve）
        result_gdf = result_gdf.dissolve()

        Path(output_path).parent.mkdir(parents=True, exist_ok=True)
        result_gdf.to_file(output_path, driver='GeoJSON')

        result = ActivityResult(
            success=True,
            output_path=output_path,
            record_count=len(result_gdf),
            metadata={"input_count": len(input_paths)}
        )

        logger.info(f"  ✅ 完成")
        return result.__dict__

    except Exception as e:
        logger.error(f"  ❌ 失败: {str(e)}")
        return ActivityResult(success=False, error=str(e)).__dict__
