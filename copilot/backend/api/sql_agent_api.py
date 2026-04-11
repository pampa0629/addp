"""
SQL 生成 API 路由
"""
from fastapi import APIRouter, HTTPException
from pydantic import BaseModel
from typing import Optional, List, Dict

from services.sql_service import sql_service
# TODO: Copilot 暂时不需要保存对话历史，注释掉以避免数据库依赖
# from services.memory_service import memory_service
from services.metadata_matcher import metadata_matcher

router = APIRouter()


class DataSourceCandidate(BaseModel):
    """数据源候选"""
    engine_id: Optional[int] = None
    engine_name: Optional[str] = None
    schema_name: Optional[str] = None
    table_name: Optional[str] = None
    score: float
    reason: str


class SQLGenerationRequest(BaseModel):
    """SQL 生成请求"""
    query: str
    conversation_id: Optional[int] = None
    engine_id: Optional[int] = None
    tenant_id: int
    user_id: int


class SQLGenerationResponse(BaseModel):
    """SQL 生成响应"""
    sql: str
    explanation: str
    auto_selected: bool
    candidates: List[DataSourceCandidate]
    conversation_id: int


@router.post("/sql/generate", response_model=SQLGenerationResponse, summary="生成 SQL | Generate SQL")
async def generate_sql(request: SQLGenerationRequest):
    """
    生成 SQL 语句

    根据用户的自然语言描述生成 SQL 查询
    """
    try:
        # 1. 匹配数据源
        match_result = await metadata_matcher.match_datasources(
            query=request.query,
            tenant_id=request.tenant_id,
            engine_id=request.engine_id
        )

        # 2. 获取对话记忆（如果有）
        # TODO: Copilot 暂时不需要对话历史，直接返回结果
        # memory = None
        # if request.conversation_id:
        #     memory = await memory_service.get_memory(request.conversation_id)

        # 3. 调用 SQL Agent 生成（不使用 memory）
        result = await sql_service.generate(
            query=request.query,
            datasources=match_result.candidates,
            memory=None,  # 不使用对话历史
            tenant_id=request.tenant_id
        )

        # 4. 保存对话历史
        # TODO: Copilot 暂时不需要保存对话历史
        # conversation_id = await memory_service.save_message(
        #     conversation_id=request.conversation_id,
        #     tenant_id=request.tenant_id,
        #     user_id=request.user_id,
        #     user_message=request.query,
        #     assistant_message=result["sql"],
        #     metadata={
        #         "selected_datasource": match_result.selected,
        #         "candidates": match_result.candidates
        #     },
        #     context_type='sql'
        # )
        conversation_id = 0  # 临时固定值，不保存对话历史

        # 5. 转换候选列表
        candidates = [
            DataSourceCandidate(
                engine_id=c.get("engine_id"),
                engine_name=c.get("engine_name"),
                schema_name=c.get("schema_name"),
                table_name=c.get("table_name"),
                score=c.get("score", 0.0),
                reason=c.get("reason", "")
            )
            for c in match_result.candidates
        ]

        return SQLGenerationResponse(
            sql=result["sql"],
            explanation=result.get("explanation", ""),
            auto_selected=match_result.auto_selected,
            candidates=candidates,
            conversation_id=conversation_id
        )

    except Exception as e:
        raise HTTPException(status_code=500, detail=f"SQL 生成失败: {str(e)}")


# TODO: Copilot 暂时不需要对话历史功能
# @router.get("/sql/conversations/{conversation_id}")
# async def get_conversation_history(conversation_id: int):
#     """获取对话历史"""
#     try:
#         history = await memory_service.get_conversation_history(conversation_id)
#         return {"conversation_id": conversation_id, "messages": history}
#     except Exception as e:
#         raise HTTPException(status_code=500, detail=f"获取对话历史失败: {str(e)}")
