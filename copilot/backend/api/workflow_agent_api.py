"""
工作流生成 API 路由

基于 WorkflowPipeline 的多阶段工作流生成
"""
from fastapi import APIRouter, Depends, HTTPException
from pydantic import BaseModel, ConfigDict, Field
from typing import Optional, Dict, Any

from pipelines.workflow_pipeline import WorkflowPipeline
from services.llm_service import llm_service
from addp_common.auth import AuthorizationContext
from dependencies.auth import require_tool_user
from models.workflow_models import WorkflowResourceFact

# TODO: Copilot 暂时不需要保存对话历史，注释掉以避免数据库依赖
# from services.memory_service import memory_service

router = APIRouter()
require_workflow_draft_tool = require_tool_user("copilot", "workflow.draft.generate")

# 全局 Pipeline 实例（延迟初始化）
_workflow_pipeline: Optional[WorkflowPipeline] = None


def get_workflow_pipeline() -> WorkflowPipeline:
    """获取或创建 WorkflowPipeline 实例"""
    global _workflow_pipeline

    if _workflow_pipeline is None:
        print("[API] 初始化 WorkflowPipeline...")
        llm = llm_service.get_llm()
        _workflow_pipeline = WorkflowPipeline(llm=llm)
        print("[API] ✅ WorkflowPipeline 初始化完成")

    return _workflow_pipeline


class WorkflowGenerationRequest(BaseModel):
    """工作流生成请求"""
    model_config = ConfigDict(extra="forbid")

    query: str
    conversation_id: Optional[int] = None
    workflow_engine_id: int  # 工作流引擎实例 ID；算子发现、详情和验证均以该实例为准
    resources: list[WorkflowResourceFact] = Field(
        description="由 owner Tool 验证的全部输入资源事实；不允许 Copilot 自行搜索或拼接 locator"
    )


class WorkflowGenerationResponse(BaseModel):
    """工作流生成响应"""
    status: str  # success, need_clarification, validation_failed, error
    workflow: Optional[Dict] = None
    explanation: Optional[str] = None
    conversation_id: Optional[int] = None

    clarification_reason: Optional[str] = None
    message: Optional[str] = None

    # 验证结果（当 status=validation_failed 时）
    errors: Optional[list] = None
    warnings: Optional[list] = None
    suggestions: Optional[list] = None

    # 额外信息
    resources: Optional[list[Dict[str, Any]]] = None
    selected_operators: Optional[list] = None
    validation_result: Optional[Dict] = None


@router.post(
    "/workflow/generate",
    response_model=WorkflowGenerationResponse,
    summary="生成工作流 | Generate Workflow",
    responses={500: {"description": "工作流生成失败 | Workflow generation failed"}},
)
async def generate_workflow(
    request: WorkflowGenerationRequest,
    user: AuthorizationContext = Depends(require_workflow_draft_tool),
):
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
    print(f"[API] 租户 ID: {user.tenant_id}")
    print(f"[API] 用户 ID: {user.user_id}")
    print(f"[API] 对话 ID: {request.conversation_id}")
    print(f"[API] 工作流引擎 ID: {request.workflow_engine_id}")
    print(f"{'='*80}\n")

    try:
        # 获取 Pipeline 实例
        pipeline = get_workflow_pipeline()

        # 调用 Pipeline 生成工作流
        print(f"[API] 调用 WorkflowPipeline...")
        result = await pipeline.run(
            query=request.query,
            tenant_id=user.tenant_id,
            workflow_engine_id=request.workflow_engine_id,
            resources=request.resources,
        )
        print(f"[API] ✅ WorkflowPipeline 返回结果")

        # 根据不同状态构造响应
        status = result.get("status")

        if status == "success":
            # 成功生成工作流
            # TODO: Copilot 暂时不需要保存对话历史
            conversation_id = 0  # 临时固定值

            response = WorkflowGenerationResponse(
                status="success",
                workflow=result["workflow"],
                explanation=result.get("explanation", "工作流生成成功"),
                conversation_id=conversation_id,
                resources=result.get("resources"),
                selected_operators=result.get("selected_operators"),
                validation_result=result.get("validation_result")
            )

            print(f"[API] ✅ 工作流生成成功")
            print(f"[API] 任务数量: {len(result['workflow']['tasks'])}")
            print(f"[API] ✅ 请求处理完成\n")
            return response

        elif status == "need_clarification":
            response = WorkflowGenerationResponse(
                status="need_clarification",
                clarification_reason=result.get("clarification_reason"),
                message=result.get("message", "请补充已验证的资源事实")
            )

            print(f"[API] ⚠️  资源事实需要澄清")
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
                resources=result.get("resources"),
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

        raise HTTPException(status_code=500, detail=f"工作流生成失败: {str(e)}") from e


# TODO: Copilot 暂时不需要对话历史功能
# @router.get("/workflow/conversations/{conversation_id}")
# async def get_conversation_history(conversation_id: int):
#     """获取对话历史"""
#     try:
#         history = await memory_service.get_conversation_history(conversation_id)
#         return {"conversation_id": conversation_id, "messages": history}
#     except Exception as e:
#         raise HTTPException(status_code=500, detail=f"获取对话历史失败: {str(e)}")
