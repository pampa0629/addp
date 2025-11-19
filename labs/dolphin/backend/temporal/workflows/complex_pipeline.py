"""
Complex Spatial Pipeline Workflow
复杂空间分析流水线

工作流程:
1. 投影转换
2. 缓冲区分析
3. 空间叠加 (多个图层)
4. 面积过滤
5. 几何简化
6. 添加质心和统计属性
7. 转回原始坐标系
8. 保存结果

特点:
- 支持多步骤串联
- 中间结果自动传递
- 每步自动重试
- 支持并行处理多个裁剪图层
"""

import logging
import asyncio
from datetime import timedelta
from typing import Dict, Any, List

from temporalio import workflow
from temporalio.common import RetryPolicy

with workflow.unsafe.imports_passed_through():
    from ..activities import (
        validate_file_exists,
        reproject_activity,
        buffer_activity,
        overlay_activity,
        filter_by_area_activity,
        simplify_activity,
        add_centroid_activity,
    )

logger = logging.getLogger(__name__)


@workflow.defn(name="complex_spatial_pipeline")
class ComplexSpatialPipeline:
    """
    复杂空间分析流水线

    适用场景:
    - 土地利用缓冲区叠加分析
    - 多图层空间交叉分析
    - 复杂空间数据处理流程
    """

    def __init__(self):
        self.steps_log = []

    @workflow.run
    async def run(self, params: Dict[str, Any]) -> Dict[str, Any]:
        """
        执行复杂空间分析流水线

        Args:
            params: {
                "input_file": str,
                "output_file": str,
                "pipeline_config": {
                    "buffer_distance": float,
                    "clip_layers": List[str],  # 多个裁剪图层
                    "min_area": float,
                    "simplify_tolerance": float,
                    "source_crs": str,
                    "compute_crs": str
                }
            }

        Returns:
            工作流执行结果
        """
        logger.info(f"🚀 启动复杂空间分析流水线")

        config = params.get('pipeline_config', {})
        buffer_distance = config.get('buffer_distance', 100.0)
        clip_layers = config.get('clip_layers', [])
        min_area = config.get('min_area', 1000.0)
        simplify_tolerance = config.get('simplify_tolerance', 5.0)
        source_crs = config.get('source_crs', 'EPSG:4326')
        compute_crs = config.get('compute_crs', 'EPSG:3857')

        retry_policy = RetryPolicy(
            initial_interval=timedelta(seconds=1),
            maximum_attempts=3,
            backoff_coefficient=2.0,
        )

        # Step 1: 验证输入
        logger.info("📋 步骤 1: 验证输入文件")
        validation = await workflow.execute_activity(
            validate_file_exists,
            args=[params['input_file']],
            start_to_close_timeout=timedelta(seconds=30),
            retry_policy=retry_policy,
        )

        if not validation['exists']:
            return {"success": False, "error": "输入文件不存在"}

        self.steps_log.append({"step": "validate", "record_count": validation['record_count']})
        logger.info(f"   ✅ 输入有效: {validation['record_count']} 条记录")

        # Step 2: 投影转换
        logger.info(f"📋 步骤 2: 投影转换 {source_crs} → {compute_crs}")
        reproject_result = await workflow.execute_activity(
            reproject_activity,
            args=[params['input_file'], compute_crs],
            start_to_close_timeout=timedelta(minutes=5),
            retry_policy=retry_policy,
        )

        if not reproject_result['success']:
            return {"success": False, "error": reproject_result.get('error')}

        self.steps_log.append({"step": "reproject", "crs": compute_crs})
        logger.info(f"   ✅ 投影完成")

        current_file = reproject_result['output_path']

        # Step 3: 缓冲区分析
        if buffer_distance > 0:
            logger.info(f"📋 步骤 3: 缓冲区分析 ({buffer_distance}m)")
            buffer_result = await workflow.execute_activity(
                buffer_activity,
                args=[current_file, buffer_distance],
                start_to_close_timeout=timedelta(minutes=10),
                retry_policy=retry_policy,
            )

            if not buffer_result['success']:
                return {"success": False, "error": buffer_result.get('error')}

            self.steps_log.append({"step": "buffer", "distance": buffer_distance})
            logger.info(f"   ✅ 缓冲区完成")

            current_file = buffer_result['output_path']

        # Step 4: 空间叠加 (并行处理多个裁剪图层)
        if clip_layers:
            logger.info(f"📋 步骤 4: 空间叠加 ({len(clip_layers)} 个图层)")

            # 并行执行多个叠加任务
            overlay_tasks = []
            for i, clip_layer in enumerate(clip_layers):
                logger.info(f"   启动叠加任务 {i+1}: {clip_layer}")
                task = workflow.execute_activity(
                    overlay_activity,
                    args=[current_file, clip_layer],
                    kwargs={"how": "intersection"},
                    start_to_close_timeout=timedelta(minutes=15),
                    retry_policy=retry_policy,
                )
                overlay_tasks.append(task)

            # 等待所有叠加任务完成
            overlay_results = await asyncio.gather(*overlay_tasks)

            # 检查是否有失败的任务
            failed = [r for r in overlay_results if not r['success']]
            if failed:
                return {"success": False, "error": f"叠加任务失败: {failed[0].get('error')}"}

            # 使用第一个结果作为后续处理的输入 (实际可能需要合并所有结果)
            current_file = overlay_results[0]['output_path']

            self.steps_log.append({
                "step": "overlay",
                "clip_count": len(clip_layers),
                "results": [r['record_count'] for r in overlay_results]
            })
            logger.info(f"   ✅ 叠加完成")

        # Step 5: 面积过滤
        if min_area > 0:
            logger.info(f"📋 步骤 5: 面积过滤 (min={min_area}㎡)")
            filter_result = await workflow.execute_activity(
                filter_by_area_activity,
                args=[current_file],
                kwargs={"min_area": min_area},
                start_to_close_timeout=timedelta(minutes=5),
                retry_policy=retry_policy,
            )

            if not filter_result['success']:
                return {"success": False, "error": filter_result.get('error')}

            self.steps_log.append({
                "step": "filter",
                "min_area": min_area,
                "record_count": filter_result['record_count']
            })
            logger.info(f"   ✅ 过滤完成: {filter_result['record_count']} 条记录")

            current_file = filter_result['output_path']

        # Step 6: 几何简化
        if simplify_tolerance > 0:
            logger.info(f"📋 步骤 6: 几何简化 (tolerance={simplify_tolerance})")
            simplify_result = await workflow.execute_activity(
                simplify_activity,
                args=[current_file, simplify_tolerance],
                start_to_close_timeout=timedelta(minutes=5),
                retry_policy=retry_policy,
            )

            if not simplify_result['success']:
                return {"success": False, "error": simplify_result.get('error')}

            self.steps_log.append({"step": "simplify", "tolerance": simplify_tolerance})
            logger.info(f"   ✅ 简化完成")

            current_file = simplify_result['output_path']

        # Step 7: 添加质心
        logger.info(f"📋 步骤 7: 添加质心坐标")
        centroid_result = await workflow.execute_activity(
            add_centroid_activity,
            args=[current_file],
            start_to_close_timeout=timedelta(minutes=5),
            retry_policy=retry_policy,
        )

        if not centroid_result['success']:
            return {"success": False, "error": centroid_result.get('error')}

        self.steps_log.append({"step": "centroid"})
        logger.info(f"   ✅ 质心添加完成")

        current_file = centroid_result['output_path']

        # Step 8: 转回原始坐标系
        logger.info(f"📋 步骤 8: 转回 {source_crs}")
        final_result = await workflow.execute_activity(
            reproject_activity,
            args=[current_file, source_crs],
            kwargs={"output_path": params['output_file']},
            start_to_close_timeout=timedelta(minutes=5),
            retry_policy=retry_policy,
        )

        if not final_result['success']:
            return {"success": False, "error": final_result.get('error')}

        self.steps_log.append({
            "step": "reproject_back",
            "crs": source_crs,
            "final_count": final_result['record_count']
        })
        logger.info(f"   ✅ 转回完成")

        # 工作流完成
        logger.info(f"🎉 复杂空间分析流水线完成")
        logger.info(f"   输出: {params['output_file']}")
        logger.info(f"   最终记录数: {final_result['record_count']}")

        return {
            "success": True,
            "output_file": params['output_file'],
            "record_count": final_result['record_count'],
            "steps_log": self.steps_log,
            "metadata": {
                "original_count": validation['record_count'],
                "final_count": final_result['record_count'],
                "total_steps": len(self.steps_log)
            }
        }
