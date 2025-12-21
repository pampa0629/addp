"""
工作流生成 Agent：基于 LangChain 的 GIS 工作流生成
"""
from typing import List, Dict, Optional
import json
import re
import httpx
from langchain_core.prompts import ChatPromptTemplate
from langchain_core.messages import HumanMessage, AIMessage, SystemMessage

from services.llm_service import llm_service
from config import settings


class WorkflowGenerationAgent:
    """
    工作流生成 Agent

    功能：
    - 理解用户的自然语言需求
    - 调用 Develop API 获取 GeoPandas 算子列表
    - 生成工作流 DAG JSON
    - 验证生成的工作流
    """

    def __init__(self):
        self.llm_service = llm_service
        self.develop_api_url = settings.develop_service_url
        self.system_prompt = self._load_system_prompt()

    def _load_system_prompt(self) -> str:
        """加载工作流生成 Prompt"""
        try:
            with open("prompts/workflow_generation.txt", "r", encoding="utf-8") as f:
                return f.read()
        except FileNotFoundError:
            # 如果文件不存在，使用简化版本
            return """你是一个 GIS 工作流生成助手。
根据用户需求，生成一个由算子组成的工作流 JSON。
只返回 JSON 格式的工作流，不要有其他内容。"""

    async def generate(
        self,
        query: str,
        memory=None,
        tenant_id: int = 1
    ) -> Dict:
        """
        生成工作流

        Args:
            query: 用户需求描述
            memory: 对话记忆（LangChain Messages 列表，可选）
            tenant_id: 租户 ID

        Returns:
            包含 workflow 和 explanation 的字典
        """
        # 1. 获取 LLM 实例
        llm = self.llm_service.get_llm(tenant_id=tenant_id)

        # 2. 构建消息
        messages = [
            SystemMessage(content=self.system_prompt),
        ]

        # 添加历史对话（如果有）
        if memory:
            messages.extend(memory)

        # 添加用户查询
        messages.append(HumanMessage(content=query))

        # 3. 调用 LLM
        try:
            response = await llm.ainvoke(messages)
            output = response.content

            # 4. 提取工作流 JSON
            workflow = self._extract_workflow(output)

            # 5. 验证工作流
            if not self._validate_workflow(workflow):
                raise ValueError("Generated workflow is invalid")

            return {
                "workflow": workflow,
                "explanation": f"生成了包含 {len(workflow.get('tasks', []))} 个步骤的工作流"
            }

        except Exception as e:
            print(f"Error generating workflow: {e}")
            raise

    async def _fetch_operators(self) -> List[Dict]:
        """
        从 Develop API 获取算子列表

        Returns:
            算子列表
        """
        try:
            async with httpx.AsyncClient(timeout=10.0) as client:
                response = await client.get(f"{self.develop_api_url}/api/develop/operators")
                response.raise_for_status()
                data = response.json()
                return data.get("operators", [])
        except Exception as e:
            print(f"Error fetching operators: {e}")
            return []

    def _extract_workflow(self, output: str) -> Dict:
        """
        从 LLM 输出中提取工作流 JSON

        Args:
            output: LLM 输出文本

        Returns:
            工作流字典
        """
        # 尝试提取 JSON 代码块
        json_match = re.search(r'```json\s*\n(.*?)\n```', output, re.DOTALL)
        if json_match:
            json_str = json_match.group(1).strip()
        else:
            # 尝试提取 {} 包裹的 JSON
            json_match = re.search(r'\{.*"tasks".*\}', output, re.DOTALL)
            if json_match:
                json_str = json_match.group(0).strip()
            else:
                # 假设整个输出就是 JSON
                json_str = output.strip()

        try:
            workflow = json.loads(json_str)
            return workflow
        except json.JSONDecodeError as e:
            print(f"Failed to parse JSON: {e}")
            print(f"Output: {output}")
            raise ValueError(f"Invalid JSON output from LLM: {e}")

    def _validate_workflow(self, workflow: Dict) -> bool:
        """
        验证工作流 DAG 结构

        Args:
            workflow: 工作流字典

        Returns:
            是否有效
        """
        # 检查必需的字段
        if not isinstance(workflow, dict):
            print("Workflow must be a dictionary")
            return False

        if "tasks" not in workflow:
            print("Workflow must have 'tasks' field")
            return False

        tasks = workflow["tasks"]
        if not isinstance(tasks, list):
            print("'tasks' must be a list")
            return False

        if len(tasks) == 0:
            print("Workflow must have at least one task")
            return False

        # 检查每个任务
        task_ids = set()
        for task in tasks:
            if not isinstance(task, dict):
                print(f"Invalid task: {task}")
                return False

            # 检查必需字段
            if "id" not in task or "operator" not in task or "params" not in task:
                print(f"Task missing required fields: {task}")
                return False

            # 检查任务 ID 唯一性
            if task["id"] in task_ids:
                print(f"Duplicate task ID: {task['id']}")
                return False
            task_ids.add(task["id"])

        return True

    def _build_operator_context(self, operators: List[Dict]) -> str:
        """
        构建算子上下文（用于 Prompt）

        Args:
            operators: 算子列表

        Returns:
            算子上下文字符串
        """
        context = "可用的算子：\n\n"
        for op in operators:
            context += f"- {op['name']}: {op.get('description', '')}\n"
            if op.get('parameters'):
                context += f"  参数: {', '.join([p['name'] for p in op['parameters']])}\n"
        return context


# 全局单例
workflow_agent = WorkflowGenerationAgent()
