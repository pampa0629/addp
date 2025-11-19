"""
Overlay Analysis Workflow
空间叠加分析工作流

工作流程:
1. 验证输入文件和裁剪图层
2. 确保坐标系一致
3. 执行空间叠加 (intersection/union/difference)
4. 添加统计属性
5. 保存结果
"""

import logging
from datetime import timedelta
from typing import Dict, Any

from temporalio import workflow
from temporalio.common import RetryPolicy

with workflow.unsafe.imports_passed_through():
    from ..activities import (
        validate_file_exists,
        overlay_activity,
        reproject_activity,
        add_centroid_activity,
    )

logger = logging.getLogger(__name__)


@workflow.defn(name="overlay_analysis")
class OverlayAnalysisWorkflow:
    """
    空间叠加分析工作流

    支持多种叠加类型:
    - intersection: 交集
    - union: 并集
    - difference: 差集
    - identity: 保留输入图层所有要素
    """

    @workflow.run
    async def run(self, params: Dict[str, Any]) -> Dict[str, Any]:
        """
        执行空间叠加工作流

        Args:
            params: {
                "input_file": str,
                "clip_layer": str,
                "output_file": str,
                "overlay_type": str,  # intersection/union/difference
                "add_centroid": bool
            }

        Returns:
            工作流执行结果
        """
        logger.info(f"🚀 启动空间叠加工作流")
        logger.info(f"   输入: {params['input_file']}")
        logger.info(f"   裁剪图层: {params['clip_layer']}")
        logger.info(f"   叠加类型: {params.get('overlay_type', 'intersection')}")

        overlay_type = params.get('overlay_type', 'intersection')
        add_centroid = params.get('add_centroid', False)

        retry_policy = RetryPolicy(
            initial_interval=timedelta(seconds=1),
            maximum_attempts=3,
            backoff_coefficient=2.0,
        )

        # Step 1: 验证输入文件
        logger.info("📋 步骤 1: 验证输入文件")
        input_validation = await workflow.execute_activity(
            validate_file_exists,
            args=[params['input_file']],
            start_to_close_timeout=timedelta(seconds=30),
            retry_policy=retry_policy,
        )

        if not input_validation['exists']:
            return {"success": False, "error": f"输入文件不存在: {params['input_file']}"}

        logger.info(f"   ✅ 输入图层: {input_validation['record_count']} 条记录")

        # Step 2: 验证裁剪图层
        logger.info("📋 步骤 2: 验证裁剪图层")
        clip_validation = await workflow.execute_activity(
            validate_file_exists,
            args=[params['clip_layer']],
            start_to_close_timeout=timedelta(seconds=30),
            retry_policy=retry_policy,
        )

        if not clip_validation['exists']:
            return {"success": False, "error": f"裁剪图层不存在: {params['clip_layer']}"}

        logger.info(f"   ✅ 裁剪图层: {clip_validation['record_count']} 条记录")

        # Step 3: 执行空间叠加
        logger.info(f"📋 步骤 3: 执行空间叠加 ({overlay_type})")
        overlay_result = await workflow.execute_activity(
            overlay_activity,
            args=[params['input_file'], params['clip_layer']],
            kwargs={"output_path": params['output_file'], "how": overlay_type},
            start_to_close_timeout=timedelta(minutes=15),
            retry_policy=retry_policy,
        )

        if not overlay_result['success']:
            return {"success": False, "error": overlay_result.get('error')}

        logger.info(f"   ✅ 叠加完成: {overlay_result['record_count']} 条记录")

        # Step 4 (可选): 添加质心
        if add_centroid:
            logger.info(f"📋 步骤 4: 添加质心坐标")
            centroid_result = await workflow.execute_activity(
                add_centroid_activity,
                args=[overlay_result['output_path']],
                kwargs={"output_path": params['output_file']},
                start_to_close_timeout=timedelta(minutes=5),
                retry_policy=retry_policy,
            )

            if not centroid_result['success']:
                return {"success": False, "error": centroid_result.get('error')}

            logger.info(f"   ✅ 质心添加完成")
            final_count = centroid_result['record_count']
        else:
            final_count = overlay_result['record_count']

        # 工作流完成
        logger.info(f"🎉 空间叠加工作流完成")
        logger.info(f"   输出: {params['output_file']}")
        logger.info(f"   最终记录数: {final_count}")

        return {
            "success": True,
            "output_file": params['output_file'],
            "record_count": final_count,
            "metadata": {
                "overlay_type": overlay_type,
                "input_count": input_validation['record_count'],
                "clip_count": clip_validation['record_count'],
                "result_count": final_count
            }
        }
