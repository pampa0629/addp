"""
工作流生成 API 路由
"""
from fastapi import APIRouter, HTTPException
from pydantic import BaseModel
from typing import Optional, Dict

from agents.workflow_agent import workflow_agent
# TODO: Copilot 暂时不需要保存对话历史，注释掉以避免数据库依赖
# from services.memory_service import memory_service

router = APIRouter()


class WorkflowGenerationRequest(BaseModel):
    """工作流生成请求"""
    query: str
    conversation_id: Optional[int] = None
    tenant_id: int
    user_id: int
    use_two_stage: bool = True  # 新增：是否使用两阶段模式（默认 True）


class WorkflowGenerationResponse(BaseModel):
    """工作流生成响应"""
    workflow: Dict
    explanation: str
    conversation_id: int


@router.post("/workflow/generate", response_model=WorkflowGenerationResponse)
async def generate_workflow(request: WorkflowGenerationRequest):
    """
    生成工作流

    根据用户的自然语言描述生成 GIS 工作流 DAG
    """
    print(f"\n{'='*80}")
    print(f"[API] 收到工作流生成请求")
    print(f"[API] 用户查询: {request.query}")
    print(f"[API] 租户 ID: {request.tenant_id}")
    print(f"[API] 用户 ID: {request.user_id}")
    print(f"[API] 对话 ID: {request.conversation_id}")
    print(f"{'='*80}\n")

    try:
        # 1. 获取对话记忆（如果有）
        # TODO: Copilot 暂时不需要对话历史，直接返回结果
        # memory = None
        # if request.conversation_id:
        #     print(f"[API] 加载对话历史: {request.conversation_id}")
        #     memory = await memory_service.get_memory(request.conversation_id)
        #     print(f"[API] 对话历史加载完成")

        # 2. 调用工作流 Agent 生成（不使用 memory）
        print(f"[API] 调用 Workflow Agent...")
        result = await workflow_agent.generate(
            query=request.query,
            memory=None,  # 不使用对话历史
            tenant_id=request.tenant_id,
            use_two_stage=request.use_two_stage
        )
        print(f"[API] ✅ Workflow Agent 返回结果")

        # 3. 保存对话历史
        # TODO: Copilot 暂时不需要保存对话历史
        # print(f"[API] 保存对话历史...")
        # conversation_id = await memory_service.save_message(
        #     conversation_id=request.conversation_id,
        #     tenant_id=request.tenant_id,
        #     user_id=request.user_id,
        #     user_message=request.query,
        #     assistant_message=result.get("explanation", "工作流生成成功"),
        #     metadata={"workflow": result["workflow"]},
        #     context_type='workflow'
        # )
        # print(f"[API] 对话历史已保存: {conversation_id}")
        conversation_id = 0  # 临时固定值，不保存对话历史

        response = WorkflowGenerationResponse(
            workflow=result["workflow"],
            explanation=result.get("explanation", "工作流生成成功"),
            conversation_id=conversation_id
        )
        print(f"[API] ✅ 请求处理完成\n")
        return response

    except ValueError as e:
        # 验证失败（如空工作流）返回 422
        error_msg = str(e)
        print(f"[API] ⚠️  工作流生成验证失败: {error_msg}")

        # 提供更友好的错误消息
        if "invalid" in error_msg.lower():
            detail = "无法根据您的描述生成有效的工作流。请提供更详细的需求描述，例如：'加载数据，计算缓冲区，然后保存结果'。"
        else:
            detail = f"工作流生成失败: {error_msg}"

        raise HTTPException(status_code=422, detail=detail)

    except Exception as e:
        # 其他异常返回 500
        print(f"[API] ❌ 工作流生成失败: {type(e).__name__}: {str(e)}")
        import traceback
        traceback.print_exc()
        raise HTTPException(status_code=500, detail=f"服务内部错误: {str(e)}")


# TODO: Copilot 暂时不需要对话历史功能
# @router.get("/workflow/conversations/{conversation_id}")
# async def get_conversation_history(conversation_id: int):
#     """获取对话历史"""
#     try:
#         history = await memory_service.get_conversation_history(conversation_id)
#         return {"conversation_id": conversation_id, "messages": history}
#     except Exception as e:
#         raise HTTPException(status_code=500, detail=f"获取对话历史失败: {str(e)}")
