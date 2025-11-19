#!/usr/bin/env python3
"""
Example 1: Buffer Analysis Workflow
示例1: 缓冲区分析工作流

功能:
- 读取点/线/面数据
- 执行缓冲区分析
- 过滤小面积
- 添加质心坐标
- 输出 GeoJSON

使用方法:
    python run_buffer_workflow.py \
      --input data/roads.geojson \
      --output output/roads_buffer.geojson \
      --distance 100 \
      --min-area 1000
"""

import argparse
import asyncio
import logging
import sys
from pathlib import Path

# 添加父目录到路径
sys.path.insert(0, str(Path(__file__).parent.parent))

from client import buffer_analysis

# 配置日志
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)


async def main():
    parser = argparse.ArgumentParser(description="缓冲区分析工作流")
    parser.add_argument("--input", required=True, help="输入文件路径 (GeoJSON/Shapefile)")
    parser.add_argument("--output", required=True, help="输出文件路径 (GeoJSON)")
    parser.add_argument("--distance", type=float, default=100.0, help="缓冲距离（米）")
    parser.add_argument("--min-area", type=float, default=1000.0, help="最小面积过滤（平方米）")

    args = parser.parse_args()

    # 验证输入文件
    if not Path(args.input).exists():
        logger.error(f"❌ 输入文件不存在: {args.input}")
        sys.exit(1)

    # 创建输出目录
    Path(args.output).parent.mkdir(parents=True, exist_ok=True)

    logger.info("=" * 70)
    logger.info("🚀 启动缓冲区分析 Workflow")
    logger.info("=" * 70)
    logger.info(f"📂 输入文件: {args.input}")
    logger.info(f"💾 输出文件: {args.output}")
    logger.info(f"📏 缓冲距离: {args.distance} 米")
    logger.info(f"📐 最小面积: {args.min_area} 平方米")
    logger.info("=" * 70)

    try:
        # 执行工作流
        result = await buffer_analysis(
            input_file=args.input,
            output_file=args.output,
            buffer_distance=args.distance,
            min_area=args.min_area
        )

        # 打印结果
        if result['success']:
            logger.info("\n" + "=" * 70)
            logger.info("🎉 工作流执行成功！")
            logger.info("=" * 70)
            logger.info(f"📊 原始记录数: {result['metadata']['original_count']}")
            logger.info(f"📊 最终记录数: {result['metadata']['final_count']}")
            logger.info(f"📝 完成步骤: {', '.join(result['steps_completed'])}")
            logger.info(f"💾 输出文件: {result['output_file']}")
            logger.info("=" * 70)
        else:
            logger.error(f"\n❌ 工作流执行失败: {result.get('error')}")
            sys.exit(1)

    except Exception as e:
        logger.error(f"\n❌ 执行出错: {e}")
        import traceback
        traceback.print_exc()
        sys.exit(1)


if __name__ == "__main__":
    asyncio.run(main())
