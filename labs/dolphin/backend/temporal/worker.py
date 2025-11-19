"""
Temporal Worker
工作进程 - 执行 Activities 和 Workflows

启动方式:
    python worker.py
"""

import asyncio
import logging
import sys
from pathlib import Path

# 添加父目录到路径
sys.path.insert(0, str(Path(__file__).parent.parent))

from temporalio.client import Client
from temporalio.worker import Worker

from config import config
from workflows import (
    BufferAnalysisWorkflow,
    OverlayAnalysisWorkflow,
    ComplexSpatialPipeline,
)
from activities import (
    # Spatial operations
    buffer_activity,
    reproject_activity,
    overlay_activity,
    filter_by_area_activity,
    add_centroid_activity,
    simplify_activity,
    union_activity,
    # IO operations
    read_geospatial_file,
    write_geospatial_file,
    validate_file_exists,
)

# 配置日志
logging.basicConfig(
    level=getattr(logging, config.log_level),
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)


async def main():
    """启动 Temporal Worker"""

    logger.info("=" * 70)
    logger.info("🚀 启动 Temporal Worker")
    logger.info("=" * 70)
    logger.info(f"📡 Temporal Server: {config.temporal_host}")
    logger.info(f"📦 Namespace: {config.temporal_namespace}")
    logger.info(f"📋 Task Queue: {config.task_queue}")
    logger.info(f"⚙️  Max Concurrent Activities: {config.max_concurrent_activities}")
    logger.info(f"⚙️  Max Concurrent Workflows: {config.max_concurrent_workflows}")
    logger.info("=" * 70)

    # 连接到 Temporal Server
    try:
        client = await Client.connect(
            config.temporal_host,
            namespace=config.temporal_namespace
        )
        logger.info("✅ 成功连接到 Temporal Server")
    except Exception as e:
        logger.error(f"❌ 连接 Temporal Server 失败: {e}")
        logger.error("请确保 Temporal Server 已启动:")
        logger.error("  docker-compose -f docker-compose-temporal.yml up -d")
        sys.exit(1)

    # 注册的 Workflows
    workflows = [
        BufferAnalysisWorkflow,
        OverlayAnalysisWorkflow,
        ComplexSpatialPipeline,
    ]

    # 注册的 Activities
    activities = [
        # Spatial
        buffer_activity,
        reproject_activity,
        overlay_activity,
        filter_by_area_activity,
        add_centroid_activity,
        simplify_activity,
        union_activity,
        # IO
        read_geospatial_file,
        write_geospatial_file,
        validate_file_exists,
    ]

    logger.info(f"📝 注册 Workflows: {len(workflows)} 个")
    for wf in workflows:
        logger.info(f"   - {wf.__name__}")

    logger.info(f"📝 注册 Activities: {len(activities)} 个")
    for act in activities:
        logger.info(f"   - {act.__name__ if hasattr(act, '__name__') else str(act)}")

    logger.info("=" * 70)

    # 创建 Worker
    worker = Worker(
        client,
        task_queue=config.task_queue,
        workflows=workflows,
        activities=activities,
        max_concurrent_activities=config.max_concurrent_activities,
        max_concurrent_workflow_tasks=config.max_concurrent_workflows,
    )

    logger.info("🎯 Worker 已启动，等待任务...")
    logger.info("   按 Ctrl+C 停止")
    logger.info("=" * 70)

    # 运行 Worker (阻塞直到收到停止信号)
    try:
        await worker.run()
    except KeyboardInterrupt:
        logger.info("\n⏹️  收到停止信号，正在关闭 Worker...")
    except Exception as e:
        logger.error(f"❌ Worker 运行错误: {e}")
        raise


if __name__ == "__main__":
    asyncio.run(main())
