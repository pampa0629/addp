"""
IO Activities for Geospatial Data
文件读写和验证 Activities
"""

import logging
from typing import Dict, Any
from pathlib import Path
from dataclasses import dataclass

from temporalio import activity
import geopandas as gpd

logger = logging.getLogger(__name__)


@dataclass
class FileInfo:
    """文件信息"""
    exists: bool
    path: str
    size_bytes: int = 0
    record_count: int = 0
    crs: str = None
    bounds: Dict[str, float] = None
    error: str = None


@activity.defn(name="validate_file")
async def validate_file_exists(file_path: str) -> Dict[str, Any]:
    """
    验证文件是否存在并可读

    Args:
        file_path: 文件路径

    Returns:
        FileInfo 字典
    """
    logger.info(f"🔍 验证文件: {file_path}")

    path = Path(file_path)

    if not path.exists():
        logger.warning(f"  ⚠️ 文件不存在")
        return FileInfo(exists=False, path=file_path, error="File not found").__dict__

    try:
        # 尝试读取文件获取元信息
        gdf = gpd.read_file(file_path)

        bounds = gdf.total_bounds
        info = FileInfo(
            exists=True,
            path=file_path,
            size_bytes=path.stat().st_size,
            record_count=len(gdf),
            crs=str(gdf.crs) if gdf.crs else None,
            bounds={
                "minx": float(bounds[0]),
                "miny": float(bounds[1]),
                "maxx": float(bounds[2]),
                "maxy": float(bounds[3])
            }
        )

        logger.info(f"  ✅ 文件有效: {len(gdf)} 条记录, CRS={gdf.crs}")
        return info.__dict__

    except Exception as e:
        logger.error(f"  ❌ 文件读取失败: {str(e)}")
        return FileInfo(
            exists=True,
            path=file_path,
            size_bytes=path.stat().st_size,
            error=str(e)
        ).__dict__


@activity.defn(name="read_file")
async def read_geospatial_file(file_path: str) -> Dict[str, Any]:
    """
    读取地理空间文件

    Args:
        file_path: 文件路径

    Returns:
        包含文件信息的字典
    """
    logger.info(f"📂 读取文件: {file_path}")

    try:
        gdf = gpd.read_file(file_path)

        result = {
            "success": True,
            "path": file_path,
            "record_count": len(gdf),
            "crs": str(gdf.crs),
            "columns": list(gdf.columns),
            "geometry_types": gdf.geometry.geom_type.unique().tolist()
        }

        logger.info(f"  ✅ 读取成功: {len(gdf)} 条记录")
        return result

    except Exception as e:
        logger.error(f"  ❌ 读取失败: {str(e)}")
        return {
            "success": False,
            "path": file_path,
            "error": str(e)
        }


@activity.defn(name="write_file")
async def write_geospatial_file(
    input_path: str,
    output_path: str,
    driver: str = "GeoJSON"
) -> Dict[str, Any]:
    """
    写入地理空间文件

    Args:
        input_path: 输入文件路径（内存中的临时文件）
        output_path: 输出文件路径
        driver: 输出格式驱动 (GeoJSON/ESRI Shapefile/GPKG)

    Returns:
        写入结果字典
    """
    logger.info(f"💾 写入文件: {output_path} (driver={driver})")

    try:
        # 读取输入数据
        gdf = gpd.read_file(input_path)

        # 创建输出目录
        Path(output_path).parent.mkdir(parents=True, exist_ok=True)

        # 写入文件
        gdf.to_file(output_path, driver=driver)

        file_size = Path(output_path).stat().st_size / 1024 / 1024  # MB

        result = {
            "success": True,
            "output_path": output_path,
            "record_count": len(gdf),
            "driver": driver,
            "size_mb": round(file_size, 2)
        }

        logger.info(f"  ✅ 写入成功: {file_size:.2f} MB")
        return result

    except Exception as e:
        logger.error(f"  ❌ 写入失败: {str(e)}")
        return {
            "success": False,
            "output_path": output_path,
            "error": str(e)
        }
