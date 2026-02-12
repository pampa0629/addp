"""
工作流生成 API 路由

基于 WorkflowPipeline 的多阶段工作流生成
"""
from fastapi import APIRouter, HTTPException, Request
from pydantic import BaseModel
from typing import Optional, Dict, Any

from pipelines.workflow_pipeline import WorkflowPipeline
from services.llm_service import llm_service

# TODO: Copilot 暂时不需要保存对话历史，注释掉以避免数据库依赖
# from services.memory_service import memory_service

router = APIRouter()

# 全局 Pipeline 实例（延迟初始化）
_workflow_pipeline: Optional[WorkflowPipeline] = None


def get_workflow_pipeline() -> WorkflowPipeline:
    """获取或创建 WorkflowPipeline 实例"""
    global _workflow_pipeline

    if _workflow_pipeline is None:
        print("[API] 初始化 WorkflowPipeline...")
        llm = llm_service.get_llm()
        _workflow_pipeline = WorkflowPipeline(
            llm=llm,
            enable_data_source_understanding=True  # 启用数据源理解
        )
        print("[API] ✅ WorkflowPipeline 初始化完成")

    return _workflow_pipeline


class WorkflowGenerationRequest(BaseModel):
    """工作流生成请求"""
    query: str
    conversation_id: Optional[int] = None
    tenant_id: int = 1
    user_id: int = 1
    skip_data_source: bool = False  # 是否跳过数据源理解阶段
    engine_type: str = "python"  # 工作流引擎类型：python, spark, math（默认 python）
    workflow_engine_id: Optional[int] = None  # 工作流引擎 ID（由前端传递，对应 system.engines 表中的 ID）


class WorkflowGenerationResponse(BaseModel):
    """工作流生成响应"""
    status: str  # success, need_clarification, validation_failed, error
    workflow: Optional[Dict] = None
    explanation: Optional[str] = None
    conversation_id: Optional[int] = None

    # 数据源候选（当 status=need_clarification 时）
    data_source_candidates: Optional[list] = None
    message: Optional[str] = None

    # 验证结果（当 status=validation_failed 时）
    errors: Optional[list] = None
    warnings: Optional[list] = None
    suggestions: Optional[list] = None

    # 额外信息
    data_source: Optional[Dict] = None
    selected_operators: Optional[list] = None
    validation_result: Optional[Dict] = None

    # 推荐的执行配置（用于前端创建 dev_item）
    recommended_execution_config: Optional[Dict] = None


@router.post("/workflow/generate", response_model=WorkflowGenerationResponse)
async def generate_workflow(request: WorkflowGenerationRequest):
    """
    生成工作流（基于 WorkflowPipeline）

    根据用户的自然语言描述生成 GIS 工作流 DAG

    流程：
    1. 数据源理解（可选，支持跳过）
    2. 算子筛选
    3. 工作流生成
    4. 工作流验证 + 自动修复
    """
    print(f"\n{'='*80}")
    print(f"[API] 收到工作流生成请求")
    print(f"[API] 用户查询: {request.query}")
    print(f"[API] 租户 ID: {request.tenant_id}")
    print(f"[API] 用户 ID: {request.user_id}")
    print(f"[API] 对话 ID: {request.conversation_id}")
    print(f"[API] 跳过数据源理解: {request.skip_data_source}")
    print(f"[API] 工作流引擎类型: {request.engine_type}")
    print(f"{'='*80}\n")

    try:
        # 获取 Pipeline 实例
        pipeline = get_workflow_pipeline()

        # 调用 Pipeline 生成工作流
        print(f"[API] 调用 WorkflowPipeline...")
        result = await pipeline.run(
            query=request.query,
            tenant_id=request.tenant_id,
            skip_data_source=request.skip_data_source,
            engine_type=request.engine_type  # 传递引擎类型
        )
        print(f"[API] ✅ WorkflowPipeline 返回结果")

        # 根据不同状态构造响应
        status = result.get("status")

        if status == "success":
            # 成功生成工作流
            # TODO: Copilot 暂时不需要保存对话历史
            conversation_id = 0  # 临时固定值

            # 构造推荐的执行配置
            # 注意：
            # 1. 如果前端传递了 workflow_engine_id，直接使用（推荐方式）
            # 2. 如果没有传递，设置为 None，由前端/Develop Backend 根据 engine_type 查询默认引擎
            recommended_execution_config = {
                "type": "workflow",
                "engine_type": f"{request.engine_type}_workflow",  # python_workflow, spark_workflow, math_workflow
                "engine_id": request.workflow_engine_id  # 工作流引擎 ID（如果前端传递了）
            }

            response = WorkflowGenerationResponse(
                status="success",
                workflow=result["workflow"],
                explanation=result.get("explanation", "工作流生成成功"),
                conversation_id=conversation_id,
                data_source=result.get("data_source"),
                selected_operators=result.get("selected_operators"),
                validation_result=result.get("validation_result"),
                recommended_execution_config=recommended_execution_config  # 新增字段
            )

            print(f"[API] ✅ 工作流生成成功")
            print(f"[API] 任务数量: {len(result['workflow']['tasks'])}")
            print(f"[API] 推荐引擎: {recommended_execution_config['engine_type']} (ID: {recommended_execution_config['engine_id']})")
            print(f"[API] ✅ 请求处理完成\n")
            return response

        elif status == "need_clarification":
            # 数据源模糊，需要用户选择
            response = WorkflowGenerationResponse(
                status="need_clarification",
                data_source_candidates=result.get("data_source_candidates", []),
                message=result.get("message", "请选择数据源")
            )

            print(f"[API] ⚠️  数据源模糊，返回候选项")
            print(f"[API] 候选数量: {len(result.get('data_source_candidates', []))}")
            return response

        elif status == "validation_failed":
            # 工作流验证失败（自动修复也失败）
            response = WorkflowGenerationResponse(
                status="validation_failed",
                workflow=result.get("workflow"),
                errors=result.get("errors", []),
                warnings=result.get("warnings", []),
                suggestions=result.get("suggestions", []),
                message=result.get("message", "工作流生成但存在问题"),
                data_source=result.get("data_source"),
                selected_operators=result.get("selected_operators"),
                validation_result=result.get("validation_result")
            )

            print(f"[API] ⚠️  工作流验证失败")
            print(f"[API] 错误数量: {len(result.get('errors', []))}")
            return response

        else:
            # 未知状态或错误
            error_msg = result.get("message", "工作流生成失败")
            print(f"[API] ❌ 错误: {error_msg}")
            raise HTTPException(status_code=500, detail=error_msg)

    except HTTPException:
        # 重新抛出 HTTPException
        raise

    except Exception as e:
        # 其他异常返回 500
        print(f"[API] ❌ 工作流生成失败: {type(e).__name__}: {str(e)}")
        import traceback
        traceback.print_exc()

        # 返回错误响应
        return WorkflowGenerationResponse(
            status="error",
            message=f"服务内部错误: {str(e)}"
        )


# TODO: Copilot 暂时不需要对话历史功能
# @router.get("/workflow/conversations/{conversation_id}")
# async def get_conversation_history(conversation_id: int):
#     """获取对话历史"""
#     try:
#         history = await memory_service.get_conversation_history(conversation_id)
#         return {"conversation_id": conversation_id, "messages": history}
#     except Exception as e:
#         raise HTTPException(status_code=500, detail=f"获取对话历史失败: {str(e)}")
