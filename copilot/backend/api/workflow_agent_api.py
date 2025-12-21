"""
工作流生成 API 路由
"""
from fastapi import APIRouter, HTTPException
from pydantic import BaseModel
from typing import Optional, Dict

from agents.workflow_agent import workflow_agent
from services.memory_service import memory_service

router = APIRouter()


class WorkflowGenerationRequest(BaseModel):
    """工作流生成请求"""
    query: str
    conversation_id: Optional[int] = None
    tenant_id: int
    user_id: int


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
    try:
        # 1. 获取对话记忆（如果有）
        memory = None
        if request.conversation_id:
            memory = await memory_service.get_memory(request.conversation_id)

        # 2. 调用工作流 Agent 生成
        result = await workflow_agent.generate(
            query=request.query,
            memory=memory,
            tenant_id=request.tenant_id
        )

        # 3. 保存对话历史
        conversation_id = await memory_service.save_message(
            conversation_id=request.conversation_id,
            tenant_id=request.tenant_id,
            user_id=request.user_id,
            user_message=request.query,
            assistant_message=result.get("explanation", "工作流生成成功"),
            metadata={"workflow": result["workflow"]},
            context_type='workflow'
        )

        return WorkflowGenerationResponse(
            workflow=result["workflow"],
            explanation=result.get("explanation", "工作流生成成功"),
            conversation_id=conversation_id
        )

    except Exception as e:
        raise HTTPException(status_code=500, detail=f"工作流生成失败: {str(e)}")


@router.get("/workflow/conversations/{conversation_id}")
async def get_conversation_history(conversation_id: int):
    """获取对话历史"""
    try:
        history = await memory_service.get_conversation_history(conversation_id)
        return {"conversation_id": conversation_id, "messages": history}
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"获取对话历史失败: {str(e)}")
