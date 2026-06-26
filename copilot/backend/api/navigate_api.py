"""
导航引导 API 路由

接收用户意图，返回平台模块/页面的导航建议
"""
from fastapi import APIRouter
from pydantic import BaseModel
from typing import Optional, List
import json

from services.llm_service import llm_service

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
    query: str
    tenant_id: int = 1
    user_id: int = 1


class NavigateAction(BaseModel):
    label: str
    route: str


class NavigateResponse(BaseModel):
    text: str
    actions: List[NavigateAction]


@router.post("/navigate/guide", response_model=NavigateResponse, summary="导航引导 | Navigation Guide")
async def navigate_guide(request: NavigateRequest):
    """
    根据用户意图返回平台导航建议

    接收自然语言描述，返回相关模块/页面的链接列表
    """
    try:
        llm = llm_service.get_llm()
        messages = [
            {"role": "system", "content": SYSTEM_PROMPT},
            {"role": "user", "content": request.query}
        ]
        raw = llm.invoke(messages, temperature=0.3, max_tokens=500)

        # 解析 JSON
        # LangChain ChatModel 返回 AIMessage 对象，需要提取 content
        text = raw.content if hasattr(raw, 'content') else str(raw)
        # 去掉可能的 markdown 代码块
        text = text.strip()
        if text.startswith("```"):
            lines = text.split("\n")
            text = "\n".join(lines[1:-1])

        data = json.loads(text)
        actions = [NavigateAction(**a) for a in data.get("actions", [])]
        return NavigateResponse(text=data.get("text", ""), actions=actions)

    except Exception as e:
        print(f"[NavigateAPI] 导航引导失败: {e}")
        return NavigateResponse(
            text="抱歉，暂时无法处理您的请求，请直接浏览左侧菜单。",
            actions=[]
        )
