#!/usr/bin/env python3
"""
Example 3: Complex Spatial Pipeline
示例3: 复杂空间分析流水线

功能:
- 多步骤空间分析
- 缓冲区 + 叠加 + 过滤 + 简化
- 支持并行处理多个裁剪图层
- 完整的数据处理流程

使用方法:
    python run_complex_workflow.py \
      --config examples/pipeline_config.json

或者直接指定参数:
    python run_complex_workflow.py \
      --input data/buildings.geojson \
      --output output/result.geojson \
      --buffer-distance 50 \
      --min-area 500 \
      --simplify-tolerance 5
"""

import argparse
import asyncio
import json
import logging
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent.parent))

from client import complex_pipeline

logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)


async def main():
    parser = argparse.ArgumentParser(description="复杂空间分析流水线")
    parser.add_argument("--config", help="配置文件路径 (JSON)")
    parser.add_argument("--input", help="输入文件路径")
    parser.add_argument("--output", help="输出文件路径")
    parser.add_argument("--buffer-distance", type=float, default=100.0, help="缓冲距离（米）")
    parser.add_argument("--min-area", type=float, default=1000.0, help="最小面积（平方米）")
    parser.add_argument("--simplify-tolerance", type=float, default=5.0, help="简化容差")
    parser.add_argument("--clip-layers", nargs="*", help="裁剪图层路径列表")

    args = parser.parse_args()

    # 从配置文件加载或使用命令行参数
    if args.config:
        with open(args.config) as f:
            config_data = json.load(f)
        input_file = config_data['input_file']
        output_file = config_data['output_file']
        pipeline_config = config_data['pipeline_config']
    else:
        if not args.input or not args.output:
            logger.error("❌ 必须指定 --config 或 --input/--output")
            sys.exit(1)

        input_file = args.input
        output_file = args.output
        pipeline_config = {
            "buffer_distance": args.buffer_distance,
            "min_area": args.min_area,
            "simplify_tolerance": args.simplify_tolerance,
            "clip_layers": args.clip_layers or [],
            "source_crs": "EPSG:4326",
            "compute_crs": "EPSG:3857"
        }

    # 验证输入文件
    if not Path(input_file).exists():
        logger.error(f"❌ 输入文件不存在: {input_file}")
        sys.exit(1)

    Path(output_file).parent.mkdir(parents=True, exist_ok=True)

    logger.info("=" * 70)
    logger.info("🚀 启动复杂空间分析流水线")
    logger.info("=" * 70)
    logger.info(f"📂 输入文件: {input_file}")
    logger.info(f"💾 输出文件: {output_file}")
    logger.info(f"⚙️  流水线配置:")
    logger.info(f"   - 缓冲距离: {pipeline_config.get('buffer_distance', 0)} 米")
    logger.info(f"   - 最小面积: {pipeline_config.get('min_area', 0)} 平方米")
    logger.info(f"   - 简化容差: {pipeline_config.get('simplify_tolerance', 0)}")
    logger.info(f"   - 裁剪图层数: {len(pipeline_config.get('clip_layers', []))}")
    logger.info("=" * 70)

    try:
        result = await complex_pipeline(
            input_file=input_file,
            output_file=output_file,
            pipeline_config=pipeline_config
        )

        if result['success']:
            logger.info("\n" + "=" * 70)
            logger.info("🎉 流水线执行成功！")
            logger.info("=" * 70)
            logger.info(f"📊 原始记录数: {result['metadata']['original_count']}")
            logger.info(f"📊 最终记录数: {result['metadata']['final_count']}")
            logger.info(f"📝 执行步骤数: {result['metadata']['total_steps']}")
            logger.info(f"💾 输出文件: {result['output_file']}")

            logger.info("\n📋 步骤详情:")
            for i, step in enumerate(result['steps_log'], 1):
                logger.info(f"   {i}. {step}")

            logger.info("=" * 70)
        else:
            logger.error(f"\n❌ 流水线执行失败: {result.get('error')}")
            sys.exit(1)

    except Exception as e:
        logger.error(f"\n❌ 执行出错: {e}")
        import traceback
        traceback.print_exc()
        sys.exit(1)


if __name__ == "__main__":
    asyncio.run(main())
