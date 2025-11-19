#!/usr/bin/env python3
"""
Example 2: Overlay Analysis Workflow
示例2: 空间叠加工作流

功能:
- 两个图层的空间叠加
- 支持 intersection/union/difference
- 自动坐标系转换
- 可选质心计算

使用方法:
    python run_overlay_workflow.py \
      --input data/parcels.geojson \
      --clip data/boundary.geojson \
      --output output/parcels_clipped.geojson \
      --type intersection \
      --add-centroid
"""

import argparse
import asyncio
import logging
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent.parent))

from client import overlay_analysis

logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)


async def main():
    parser = argparse.ArgumentParser(description="空间叠加工作流")
    parser.add_argument("--input", required=True, help="输入图层路径")
    parser.add_argument("--clip", required=True, help="裁剪图层路径")
    parser.add_argument("--output", required=True, help="输出文件路径")
    parser.add_argument(
        "--type",
        default="intersection",
        choices=["intersection", "union", "difference", "identity"],
        help="叠加类型"
    )
    parser.add_argument("--add-centroid", action="store_true", help="添加质心坐标")

    args = parser.parse_args()

    # 验证输入文件
    for file_path in [args.input, args.clip]:
        if not Path(file_path).exists():
            logger.error(f"❌ 文件不存在: {file_path}")
            sys.exit(1)

    Path(args.output).parent.mkdir(parents=True, exist_ok=True)

    logger.info("=" * 70)
    logger.info("🚀 启动空间叠加 Workflow")
    logger.info("=" * 70)
    logger.info(f"📂 输入图层: {args.input}")
    logger.info(f"✂️  裁剪图层: {args.clip}")
    logger.info(f"💾 输出文件: {args.output}")
    logger.info(f"🔀 叠加类型: {args.type}")
    logger.info(f"📍 添加质心: {'是' if args.add_centroid else '否'}")
    logger.info("=" * 70)

    try:
        result = await overlay_analysis(
            input_file=args.input,
            clip_layer=args.clip,
            output_file=args.output,
            overlay_type=args.type
        )

        if result['success']:
            logger.info("\n" + "=" * 70)
            logger.info("🎉 工作流执行成功！")
            logger.info("=" * 70)
            logger.info(f"📊 输入记录数: {result['metadata']['input_count']}")
            logger.info(f"📊 裁剪记录数: {result['metadata']['clip_count']}")
            logger.info(f"📊 结果记录数: {result['metadata']['result_count']}")
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
