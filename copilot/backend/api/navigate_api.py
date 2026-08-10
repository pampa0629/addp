"""
导航引导 API 路由

接收用户意图，返回平台模块/页面的导航建议
"""
from fastapi import APIRouter, Depends, HTTPException, status
from pydantic import BaseModel, ConfigDict
from typing import Optional, List
import json

from addp_common.auth import AuthorizationContext
from database import get_db
from dependencies.auth import require_tenant_user
from langchain_core.messages import HumanMessage, SystemMessage
from services.inference_service import CopilotInferenceService
from sqlalchemy.orm import Session

router = APIRouter()

# 平台模块/页面地图（供 LLM 参考）
PLATFORM_MAP = """
ADDP 数据平台模块说明：

【数据准备】
- /transfer/tasks - 数据传输任务（配置和管理数据同步任务）
- /transfer/executions - 传输执行记录
- /meta/scan - 元数据扫描（扫描数据库表结构）
- /meta/tasks - 元数据扫描任务
- /manager/data-explorer - 数据探索（浏览和查询数据）
- /manager/data-retrieval - 数据检索
- /manager/vectorization-tasks - 向量化任务

【数据治理】
- /standard/domains - 数据标准域
- /standard/glossaries - 业务术语表
- /standard/elements - 数据元素
- /standard/code-sets - 代码集
- /standard/units - 计量单位
- /standard/classifications - 数据分类
- /standard/metrics - 数据指标
- /standard/documents - 标准文档
- /standard/dimension-hierarchies - 维度层次
- /modeling/dw-layers - 数仓分层
- /modeling/entities - 实体定义
- /modeling/logical-tables - 逻辑表
- /modeling/er-diagram - ER 图
- /modeling/star-schema - 星型模型
- /quality/check-tasks - 质量检查任务
- /quality/rule-applications - 质量规则应用
- /quality/executions - 质量检查执行记录
- /quality/issues - 质量问题列表

【开发与监控】
- /develop/sql - SQL 工作台（在线 SQL 查询）
- /develop/notebook - Jupyter Notebook（Python 数据分析）
- /develop/gis-workflow - GIS 工作流编辑器（可视化空间数据处理）
- /develop/gis-tasks - GIS 任务列表
- /develop/gis-executions - GIS 执行历史
- /service/query-services - 查询服务（发布数据 API）
- /service/tile - 地图瓦片服务
- /service/services - 服务管理
- /service/catalog - 服务目录
- /orchestrator/orchestrations - 工作流编排（调度多个任务）
- /orchestrator/executions - 编排执行记录
- /monitor/dashboard - 监控仪表盘
- /monitor/executions - 执行监控

【数据资产】
- /asset/assets - 数据资产列表
- /asset/type-definitions - 资产类型定义
- /asset/categories - 资产分类
- /asset/applications - 资产应用
- /asset/dashboard - 资产仪表盘

【其他】
- /agent - AI 智能助手（对话式 AI）
- /graph/ontologies - 知识图谱本体
- /graph/graphs - 图谱管理
- /graph/analysis - 图谱分析
- /graph/knowledge-service - 知识服务
- /system/users - 用户管理
- /system/engines - 引擎管理（配置通用引擎、查看扩展运行时）
- /system/applications - 应用与 API Key 管理
- /system/logs - 系统日志
- /system/cleanup - 数据清理
"""

SYSTEM_PROMPT = f"""你是 ADDP 数据平台的导航助手。根据用户的需求，推荐最相关的平台页面。

{PLATFORM_MAP}

请根据用户的问题，返回 JSON 格式的导航建议：
{{
  "text": "简短的引导说明（1-2句话）",
  "actions": [
    {{"label": "页面名称", "route": "/module/page"}},
    ...
  ]
}}

规则：
- actions 最多返回 4 个
- route 必须是上面列出的路径之一
- text 用中文，简洁友好
- 只返回 JSON，不要其他内容
"""


class NavigateRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    query: str


class NavigateAction(BaseModel):
    label: str
    route: str


class NavigateResponse(BaseModel):
    text: str
    actions: List[NavigateAction]


@router.post(
    "/navigate/guide",
    response_model=NavigateResponse,
    summary="导航引导 | Navigation Guide",
    openapi_extra={"x-addp-auth-mode": "authenticated"},
)
async def navigate_guide(
    request: NavigateRequest,
    user: AuthorizationContext = Depends(require_tenant_user),
    db: Session = Depends(get_db),
):
    """
    根据用户意图返回平台导航建议

    接收自然语言描述，返回相关模块/页面的链接列表
    """
    try:
        llm = CopilotInferenceService.chat_model(
            db,
            tenant_id=user.tenant_id,
            scenario_code="navigation_guide",
            temperature=0.3,
            max_output_tokens=500,
        )
        messages = [
            SystemMessage(content=SYSTEM_PROMPT),
            HumanMessage(content=request.query),
        ]
        raw = await llm.ainvoke(messages)

        text = raw.content if hasattr(raw, 'content') else str(raw)
        data = json.loads(text.strip())
        actions = [NavigateAction(**a) for a in data.get("actions", [])]
        return NavigateResponse(text=data.get("text", ""), actions=actions)

    except ValueError as error:
        raise HTTPException(status_code=400, detail="导航请求结果不符合结构化契约") from error
    except Exception as error:
        raise HTTPException(
            status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
            detail="导航推理暂不可用",
        ) from error
