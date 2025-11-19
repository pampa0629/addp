"""
Temporal Client
客户端 - 启动 Workflows

提供便捷的 API 调用 Temporal Workflows
"""

import asyncio
import logging
from typing import Dict, Any
from datetime import datetime

from temporalio.client import Client

from config import config
from workflows import (
    BufferAnalysisWorkflow,
    OverlayAnalysisWorkflow,
    ComplexSpatialPipeline,
)

logger = logging.getLogger(__name__)


class TemporalSpatialClient:
    """
    Temporal 空间分析客户端

    用法:
        client = TemporalSpatialClient()
        await client.connect()

        result = await client.run_buffer_analysis(
            input_file="data/roads.geojson",
            output_file="output/roads_buffer.geojson",
            buffer_distance=100
        )
    """

    def __init__(self, host: str = None, namespace: str = None):
        self.host = host or config.temporal_host
        self.namespace = namespace or config.temporal_namespace
        self.task_queue = config.task_queue
        self.client = None

    async def connect(self):
        """连接到 Temporal Server"""
        try:
            self.client = await Client.connect(self.host, namespace=self.namespace)
            logger.info(f"✅ 连接到 Temporal Server: {self.host}")
        except Exception as e:
            logger.error(f"❌ 连接失败: {e}")
            raise

    async def run_buffer_analysis(
        self,
        input_file: str,
        output_file: str,
        buffer_distance: float = 100.0,
        min_area: float = 1000.0,
        **kwargs
    ) -> Dict[str, Any]:
        """
        执行缓冲区分析 Workflow

        Args:
            input_file: 输入文件路径
            output_file: 输出文件路径
            buffer_distance: 缓冲距离（米）
            min_area: 最小面积过滤（平方米）
            **kwargs: 其他参数

        Returns:
            Workflow 执行结果
        """
        if not self.client:
            await self.connect()

        workflow_id = f"buffer-analysis-{datetime.now().timestamp()}"

        logger.info(f"🚀 启动缓冲区分析 Workflow")
        logger.info(f"   Workflow ID: {workflow_id}")
        logger.info(f"   输入: {input_file}")
        logger.info(f"   缓冲距离: {buffer_distance}m")

        params = {
            "input_file": input_file,
            "output_file": output_file,
            "buffer_distance": buffer_distance,
            "min_area": min_area,
            **kwargs
        }

        result = await self.client.execute_workflow(
            BufferAnalysisWorkflow.run,
            params,
            id=workflow_id,
            task_queue=self.task_queue,
        )

        logger.info(f"✅ Workflow 完成")
        return result

    async def run_overlay_analysis(
        self,
        input_file: str,
        clip_layer: str,
        output_file: str,
        overlay_type: str = "intersection",
        add_centroid: bool = False
    ) -> Dict[str, Any]:
        """
        执行空间叠加 Workflow

        Args:
            input_file: 输入图层路径
            clip_layer: 裁剪图层路径
            output_file: 输出文件路径
            overlay_type: 叠加类型 (intersection/union/difference)
            add_centroid: 是否添加质心

        Returns:
            Workflow 执行结果
        """
        if not self.client:
            await self.connect()

        workflow_id = f"overlay-analysis-{datetime.now().timestamp()}"

        logger.info(f"🚀 启动空间叠加 Workflow")
        logger.info(f"   Workflow ID: {workflow_id}")
        logger.info(f"   叠加类型: {overlay_type}")

        params = {
            "input_file": input_file,
            "clip_layer": clip_layer,
            "output_file": output_file,
            "overlay_type": overlay_type,
            "add_centroid": add_centroid
        }

        result = await self.client.execute_workflow(
            OverlayAnalysisWorkflow.run,
            params,
            id=workflow_id,
            task_queue=self.task_queue,
        )

        logger.info(f"✅ Workflow 完成")
        return result

    async def run_complex_pipeline(
        self,
        input_file: str,
        output_file: str,
        pipeline_config: Dict[str, Any]
    ) -> Dict[str, Any]:
        """
        执行复杂空间分析流水线

        Args:
            input_file: 输入文件路径
            output_file: 输出文件路径
            pipeline_config: 流水线配置 {
                "buffer_distance": float,
                "clip_layers": List[str],
                "min_area": float,
                "simplify_tolerance": float,
                ...
            }

        Returns:
            Workflow 执行结果
        """
        if not self.client:
            await self.connect()

        workflow_id = f"complex-pipeline-{datetime.now().timestamp()}"

        logger.info(f"🚀 启动复杂空间分析流水线")
        logger.info(f"   Workflow ID: {workflow_id}")

        params = {
            "input_file": input_file,
            "output_file": output_file,
            "pipeline_config": pipeline_config
        }

        result = await self.client.execute_workflow(
            ComplexSpatialPipeline.run,
            params,
            id=workflow_id,
            task_queue=self.task_queue,
        )

        logger.info(f"✅ Workflow 完成")
        return result


# ===========================
# 便捷函数（用于命令行调用）
# ===========================

async def buffer_analysis(
    input_file: str,
    output_file: str,
    buffer_distance: float = 100.0,
    min_area: float = 1000.0
):
    """便捷函数: 缓冲区分析"""
    client = TemporalSpatialClient()
    return await client.run_buffer_analysis(
        input_file, output_file, buffer_distance, min_area
    )


async def overlay_analysis(
    input_file: str,
    clip_layer: str,
    output_file: str,
    overlay_type: str = "intersection"
):
    """便捷函数: 空间叠加"""
    client = TemporalSpatialClient()
    return await client.run_overlay_analysis(
        input_file, clip_layer, output_file, overlay_type
    )


async def complex_pipeline(
    input_file: str,
    output_file: str,
    pipeline_config: Dict[str, Any]
):
    """便捷函数: 复杂流水线"""
    client = TemporalSpatialClient()
    return await client.run_complex_pipeline(
        input_file, output_file, pipeline_config
    )
