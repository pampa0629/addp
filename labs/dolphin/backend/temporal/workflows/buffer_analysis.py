"""
Buffer Analysis Workflow
缓冲区分析工作流

工作流程:
1. 验证输入文件
2. 投影转换 (WGS84 → Web Mercator)
3. 执行缓冲区分析
4. 过滤小面积图斑
5. 添加质心坐标
6. 转回 WGS84
7. 保存最终结果
"""

import logging
from dataclasses import dataclass
from datetime import timedelta
from typing import Dict, Any

from temporalio import workflow
from temporalio.common import RetryPolicy

# 导入 Activities (类型检查用)
with workflow.unsafe.imports_passed_through():
    from ..activities import (
        validate_file_exists,
        buffer_activity,
        reproject_activity,
        filter_by_area_activity,
        add_centroid_activity,
        write_geospatial_file,
    )

logger = logging.getLogger(__name__)


@dataclass
class BufferAnalysisInput:
    """缓冲区分析输入参数"""
    input_file: str
    output_file: str
    buffer_distance: float = 100.0  # 米
    min_area: float = 1000.0  # 平方米
    buffer_segments: int = 16
    source_crs: str = "EPSG:4326"
    compute_crs: str = "EPSG:3857"  # 计算用投影坐标系


@workflow.defn(name="buffer_analysis")
class BufferAnalysisWorkflow:
    """
    缓冲区分析工作流

    特点:
    - 自动投影转换 (度 → 米)
    - 面积过滤
    - 质心计算
    - 自动重试机制
    """

    def __init__(self):
        self.workflow_result = {
            "steps_completed": [],
            "final_record_count": 0,
            "errors": []
        }

    @workflow.run
    async def run(self, params: Dict[str, Any]) -> Dict[str, Any]:
        """
        执行缓冲区分析工作流

        Args:
            params: BufferAnalysisInput 字典

        Returns:
            工作流执行结果
        """
        logger.info(f"🚀 启动缓冲区分析工作流")
        logger.info(f"   输入: {params['input_file']}")
        logger.info(f"   缓冲距离: {params.get('buffer_distance', 100)} 米")

        # 默认参数
        buffer_distance = params.get('buffer_distance', 100.0)
        min_area = params.get('min_area', 1000.0)
        buffer_segments = params.get('buffer_segments', 16)
        source_crs = params.get('source_crs', 'EPSG:4326')
        compute_crs = params.get('compute_crs', 'EPSG:3857')

        # 重试策略
        retry_policy = RetryPolicy(
            initial_interval=timedelta(seconds=1),
            maximum_attempts=3,
            backoff_coefficient=2.0,
        )

        # Step 1: 验证输入文件
        logger.info("📋 步骤 1: 验证输入文件")
        validation_result = await workflow.execute_activity(
            validate_file_exists,
            args=[params['input_file']],
            start_to_close_timeout=timedelta(seconds=30),
            retry_policy=retry_policy,
        )

        if not validation_result['exists']:
            error_msg = f"输入文件不存在: {params['input_file']}"
            logger.error(f"❌ {error_msg}")
            return {"success": False, "error": error_msg}

        self.workflow_result['steps_completed'].append("validate_input")
        logger.info(f"   ✅ 文件有效: {validation_result['record_count']} 条记录")

        # Step 2: 投影转换 (WGS84 → Web Mercator)
        logger.info(f"📋 步骤 2: 投影转换 {source_crs} → {compute_crs}")
        reproject_result = await workflow.execute_activity(
            reproject_activity,
            args=[params['input_file'], compute_crs],
            start_to_close_timeout=timedelta(minutes=5),
            retry_policy=retry_policy,
        )

        if not reproject_result['success']:
            logger.error(f"❌ 投影转换失败: {reproject_result.get('error')}")
            return {"success": False, "error": reproject_result.get('error')}

        self.workflow_result['steps_completed'].append("reproject_to_metric")
        logger.info(f"   ✅ 投影完成")

        # Step 3: 缓冲区分析
        logger.info(f"📋 步骤 3: 缓冲区分析 (距离={buffer_distance}m)")
        buffer_result = await workflow.execute_activity(
            buffer_activity,
            args=[reproject_result['output_path'], buffer_distance],
            kwargs={"segments": buffer_segments},
            start_to_close_timeout=timedelta(minutes=10),
            retry_policy=retry_policy,
        )

        if not buffer_result['success']:
            logger.error(f"❌ 缓冲区分析失败: {buffer_result.get('error')}")
            return {"success": False, "error": buffer_result.get('error')}

        self.workflow_result['steps_completed'].append("buffer")
        logger.info(f"   ✅ 缓冲区完成")

        # Step 4: 面积过滤
        logger.info(f"📋 步骤 4: 面积过滤 (min={min_area}㎡)")
        filter_result = await workflow.execute_activity(
            filter_by_area_activity,
            args=[buffer_result['output_path']],
            kwargs={"min_area": min_area},
            start_to_close_timeout=timedelta(minutes=5),
            retry_policy=retry_policy,
        )

        if not filter_result['success']:
            logger.error(f"❌ 面积过滤失败: {filter_result.get('error')}")
            return {"success": False, "error": filter_result.get('error')}

        self.workflow_result['steps_completed'].append("filter_by_area")
        logger.info(f"   ✅ 过滤完成: {filter_result['record_count']} 条记录")

        # Step 5: 添加质心坐标
        logger.info(f"📋 步骤 5: 添加质心坐标")
        centroid_result = await workflow.execute_activity(
            add_centroid_activity,
            args=[filter_result['output_path']],
            start_to_close_timeout=timedelta(minutes=5),
            retry_policy=retry_policy,
        )

        if not centroid_result['success']:
            logger.error(f"❌ 质心计算失败: {centroid_result.get('error')}")
            return {"success": False, "error": centroid_result.get('error')}

        self.workflow_result['steps_completed'].append("add_centroid")
        logger.info(f"   ✅ 质心添加完成")

        # Step 6: 转回 WGS84
        logger.info(f"📋 步骤 6: 转回 {source_crs}")
        final_reproject_result = await workflow.execute_activity(
            reproject_activity,
            args=[centroid_result['output_path'], source_crs],
            kwargs={"output_path": params['output_file']},
            start_to_close_timeout=timedelta(minutes=5),
            retry_policy=retry_policy,
        )

        if not final_reproject_result['success']:
            logger.error(f"❌ 最终投影失败: {final_reproject_result.get('error')}")
            return {"success": False, "error": final_reproject_result.get('error')}

        self.workflow_result['steps_completed'].append("reproject_back")
        self.workflow_result['final_record_count'] = final_reproject_result['record_count']
        logger.info(f"   ✅ 转回完成")

        # 工作流成功完成
        logger.info(f"🎉 缓冲区分析工作流完成")
        logger.info(f"   输出: {params['output_file']}")
        logger.info(f"   最终记录数: {final_reproject_result['record_count']}")

        return {
            "success": True,
            "output_file": params['output_file'],
            "record_count": final_reproject_result['record_count'],
            "steps_completed": self.workflow_result['steps_completed'],
            "metadata": {
                "buffer_distance": buffer_distance,
                "min_area": min_area,
                "original_count": validation_result['record_count'],
                "final_count": final_reproject_result['record_count']
            }
        }
