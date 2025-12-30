"""
工作流生成 Agent：基于 LangChain 的 GIS 工作流生成（支持两阶段模式）
"""
from typing import List, Dict, Optional
import json
import re
import httpx
from langchain_core.prompts import ChatPromptTemplate
from langchain_core.messages import HumanMessage, AIMessage, SystemMessage

from services.llm_service import llm_service
from config import settings
from .operator_selection_agent import operator_selector


class WorkflowGenerationAgent:
    """
    工作流生成 Agent

    功能：
    - 理解用户的自然语言需求
    - 调用 Develop API 获取 Python Workflow 算子列表
    - 生成工作流 DAG JSON
    - 验证生成的工作流

    两阶段模式（新增）：
    - 第一阶段：使用 OperatorSelectionAgent 选择相关算子（基于简要描述）
    - 第二阶段：获取选中算子的详细信息，生成工作流
    """

    def __init__(self):
        self.llm_service = llm_service
        self.develop_api_url = settings.develop_service_url
        self.system_prompt = self._load_system_prompt()
        self.operator_selector = operator_selector  # 新增：算子选择器

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
        tenant_id: int = 1,
        use_two_stage: bool = True
    ) -> Dict:
        """
        生成工作流

        Args:
            query: 用户需求描述
            memory: 对话记忆（LangChain Messages 列表，可选）
            tenant_id: 租户 ID
            use_two_stage: 是否使用两阶段模式（默认 True）

        Returns:
            包含 workflow 和 explanation 的字典
        """
        print(f"\n{'='*60}")
        print(f"[Workflow Agent] 开始生成工作流")
        print(f"[Workflow Agent] 用户查询: {query}")
        print(f"[Workflow Agent] 租户 ID: {tenant_id}")
        print(f"[Workflow Agent] 对话历史: {'有' if memory else '无'}")
        print(f"[Workflow Agent] 工作流生成模式: {'两阶段' if use_two_stage else '单阶段'}")
        print(f"{'='*60}\n")

        try:
            if use_two_stage:
                # 两阶段模式
                print(f"[Workflow Agent] 使用两阶段工作流生成模式")

                # 第一阶段：选择算子
                print(f"\n[Workflow Agent] ---- 第一阶段：算子选择 ----")
                selected_operators = await self.operator_selector.select_operators(query, tenant_id)
                print(f"[Workflow Agent] ✅ 第一阶段完成，选中 {len(selected_operators)} 个算子: {selected_operators}")

                # 第二阶段：获取详细信息并生成工作流
                print(f"\n[Workflow Agent] ---- 第二阶段：工作流生成 ----")
                detailed_operators = await self._fetch_detailed_operators(selected_operators)
                print(f"[Workflow Agent] ✅ 获取到 {len(detailed_operators)} 个算子的详细信息")

                workflow = await self._generate_workflow_with_operators(query, detailed_operators, memory, tenant_id)
                print(f"[Workflow Agent] ✅ 工作流生成完成")

                result = {
                    "workflow": workflow,
                    "explanation": f"生成了包含 {len(workflow.get('tasks', []))} 个步骤的工作流（两阶段模式）"
                }
                print(f"[Workflow Agent] ✅ 两阶段工作流生成完成\n")
                return result
            else:
                # 单阶段模式（向后兼容）
                print(f"[Workflow Agent] 使用单阶段工作流生成模式（向后兼容）")
                return await self._generate_workflow_legacy(query, memory, tenant_id)

        except Exception as e:
            print(f"[Workflow Agent] ❌ 工作流生成失败: {type(e).__name__}: {str(e)}")
            print(f"[Workflow Agent] 异常详情:")
            import traceback
            import sys
            traceback.print_exc(file=sys.stdout)

            # 检查是否是网络错误
            if "Connection" in str(e) or "connection" in str(e):
                print(f"[Workflow Agent] 💡 这是网络连接错误，可能的原因：")
                print(f"[Workflow Agent]    1. 防火墙或代理阻止了连接")
                print(f"[Workflow Agent]    2. Dashscope API 服务暂时不可用")
                print(f"[Workflow Agent]    3. API Key 无效或过期")
                print(f"[Workflow Agent]    4. 网络环境无法访问外部 API")

            raise

    async def _generate_workflow_legacy(
        self,
        query: str,
        memory=None,
        tenant_id: int = 1
    ) -> Dict:
        """
        单阶段工作流生成（向后兼容）

        Args:
            query: 用户需求描述
            memory: 对话记忆
            tenant_id: 租户 ID

        Returns:
            工作流字典
        """
        print(f"[Workflow Agent] 执行单阶段工作流生成")

        # 1. 获取 LLM 实例
        try:
            llm = self.llm_service.get_llm(tenant_id=tenant_id)
            print(f"[Workflow Agent] ✅ LLM 实例创建成功")
        except Exception as e:
            print(f"[Workflow Agent] ❌ LLM 实例创建失败: {e}")
            raise

        # 2. 构建消息
        messages = [
            SystemMessage(content=self.system_prompt),
        ]

        # 添加历史对话（如果有）
        if memory:
            messages.extend(memory)
            print(f"[Workflow Agent] 已添加 {len(memory)} 条历史消息")

        # 添加用户查询
        messages.append(HumanMessage(content=query))
        print(f"[Workflow Agent] 消息总数: {len(messages)}")

        # 3. 调用 LLM
        try:
            print(f"[Workflow Agent] 正在调用 LLM...")
            print(f"[Workflow Agent] System Prompt 长度: {len(self.system_prompt)} 字符")
            print(f"[Workflow Agent] 用户消息: {query[:100]}...")

            # 使用新的适配器接口
            output = await llm.ainvoke(messages, temperature=0.7, max_tokens=2000)
            print(f"[Workflow Agent] ✅ LLM 调用成功")
            print(f"[Workflow Agent] LLM 响应长度: {len(output)} 字符")
            print(f"[Workflow Agent] LLM 响应内容:\n{'-'*60}\n{output}\n{'-'*60}")

            # 4. 提取工作流 JSON
            print(f"[Workflow Agent] 开始提取工作流 JSON...")
            workflow = self._extract_workflow(output)
            print(f"[Workflow Agent] ✅ 工作流 JSON 提取成功")
            print(f"[Workflow Agent] 工作流内容: {json.dumps(workflow, indent=2, ensure_ascii=False)}")

            # 5. 验证工作流
            print(f"[Workflow Agent] 开始验证工作流...")
            if not self._validate_workflow(workflow):
                print(f"[Workflow Agent] ❌ 工作流验证失败")
                raise ValueError("Generated workflow is invalid")
            print(f"[Workflow Agent] ✅ 工作流验证通过")

            return workflow

        except Exception as e:
            print(f"[Workflow Agent] ❌ 单阶段工作流生成失败: {type(e).__name__}: {str(e)}")
            raise

    async def _fetch_detailed_operators(self, operator_names: List[str]) -> List[Dict]:
        """
        从 Develop API 获取指定算子的详细信息

        Args:
            operator_names: 算子名称列表

        Returns:
            算子详细信息列表
        """
        print(f"[Workflow Agent] 获取 {len(operator_names)} 个算子的详细信息")
        detailed_operators = []

        # 构建认证头
        from config import settings
        headers = {}
        if settings.internal_api_key:
            headers["X-Internal-API-Key"] = settings.internal_api_key

        for op_name in operator_names:
            try:
                print(f"[Workflow Agent] 正在获取算子 '{op_name}' 的详细信息...")
                # 禁用系统代理，直接访问本地服务
                async with httpx.AsyncClient(timeout=10.0, trust_env=False) as client:
                    response = await client.get(
                        f"{self.develop_api_url}/api/develop/operators/{op_name}",
                        headers=headers
                    )
                    response.raise_for_status()
                    data = response.json()

                    if data.get("status") == "success":
                        operator = data.get("operator", {})
                        detailed_operators.append(operator)
                        print(f"[Workflow Agent] ✅ 成功获取算子 '{op_name}' 的详细信息")
                        # 打印关键信息
                        print(f"[Workflow Agent]    - 名称: {operator.get('name')}")
                        print(f"[Workflow Agent]    - 描述: {operator.get('description', '')[:60]}...")
                        if operator.get('workflow_example'):
                            print(f"[Workflow Agent]    - 包含工作流示例")
                    else:
                        print(f"[Workflow Agent] ⚠️ 算子 '{op_name}' 获取失败: {data.get('error', 'Unknown error')}")
            except httpx.HTTPError as e:
                print(f"[Workflow Agent] ⚠️ HTTP 请求失败 ('{op_name}'): {e}")
            except Exception as e:
                print(f"[Workflow Agent] ⚠️ 获取算子详情失败 ('{op_name}'): {type(e).__name__}: {e}")

        print(f"[Workflow Agent] ✅ 成功获取 {len(detailed_operators)}/{len(operator_names)} 个算子的详细信息")
        return detailed_operators

    async def _generate_workflow_with_operators(
        self,
        query: str,
        operators: List[Dict],
        memory=None,
        tenant_id: int = 1
    ) -> Dict:
        """
        使用选中算子的详细信息生成工作流

        Args:
            query: 用户需求描述
            operators: 算子详细信息列表
            memory: 对话记忆
            tenant_id: 租户 ID

        Returns:
            工作流字典
        """
        print(f"[Workflow Agent] 使用 {len(operators)} 个算子生成工作流")

        # 1. 获取 LLM 实例
        try:
            llm = self.llm_service.get_llm(tenant_id=tenant_id)
            print(f"[Workflow Agent] ✅ LLM 实例创建成功")
        except Exception as e:
            print(f"[Workflow Agent] ❌ LLM 实例创建失败: {e}")
            raise

        # 2. 构建详细 Prompt
        try:
            detailed_prompt = self._build_detailed_prompt(query, operators)
            print(f"[Workflow Agent] ✅ 详细 Prompt 构建成功，长度: {len(detailed_prompt)} 字符")
        except Exception as e:
            print(f"[Workflow Agent] ❌ Prompt 构建失败: {e}")
            raise

        # 3. 构建消息
        messages = [
            SystemMessage(content=detailed_prompt),
        ]

        # 添加历史对话（如果有）
        if memory:
            messages.extend(memory)
            print(f"[Workflow Agent] 已添加 {len(memory)} 条历史消息")

        # 添加用户查询
        messages.append(HumanMessage(content=query))
        print(f"[Workflow Agent] 消息总数: {len(messages)}")

        # 4. 调用 LLM
        try:
            print(f"[Workflow Agent] 正在调用 LLM（详细 Prompt）...")
            print(f"[Workflow Agent] Prompt 长度: {len(detailed_prompt)} 字符")
            print(f"[Workflow Agent] 用户消息: {query[:100]}...")

            output = await llm.ainvoke(messages, temperature=0.7, max_tokens=3000)
            print(f"[Workflow Agent] ✅ LLM 调用成功")
            print(f"[Workflow Agent] LLM 响应长度: {len(output)} 字符")
            print(f"[Workflow Agent] LLM 响应内容:\n{'-'*60}\n{output}\n{'-'*60}")

            # 5. 提取工作流 JSON
            print(f"[Workflow Agent] 开始提取工作流 JSON...")
            workflow = self._extract_workflow(output)
            print(f"[Workflow Agent] ✅ 工作流 JSON 提取成功")
            print(f"[Workflow Agent] 工作流内容: {json.dumps(workflow, indent=2, ensure_ascii=False)}")

            # 6. 验证工作流
            print(f"[Workflow Agent] 开始验证工作流...")
            if not self._validate_workflow(workflow):
                print(f"[Workflow Agent] ❌ 工作流验证失败")
                raise ValueError("Generated workflow is invalid")
            print(f"[Workflow Agent] ✅ 工作流验证通过")

            return workflow

        except Exception as e:
            print(f"[Workflow Agent] ❌ 详细工作流生成失败: {type(e).__name__}: {str(e)}")
            raise

    def _build_detailed_prompt(self, query: str, operators: List[Dict]) -> str:
        """
        基于算子详细信息构建 Prompt

        Args:
            query: 用户需求描述
            operators: 算子详细信息列表

        Returns:
            完整的 Prompt 文本
        """
        print(f"[Workflow Agent] 构建详细 Prompt（包含 {len(operators)} 个算子）")

        # 构建算子详细信息部分
        operators_section = "## 可用的算子（详细信息）\n\n"
        for op in operators:
            op_name = op.get("name", "Unknown")
            op_desc = op.get("description", "No description")
            op_brief = op.get("brief_description", op_desc)

            operators_section += f"### {op_name}\n"
            operators_section += f"**简要说明**: {op_brief}\n"
            operators_section += f"**详细说明**: {op_desc}\n"

            # 添加参数信息
            params = op.get("parameters", [])
            if params:
                operators_section += f"**参数**: \n"
                for param in params:
                    param_name = param.get("name", "unknown")
                    param_type = param.get("type", "unknown")
                    param_desc = param.get("description", "")
                    param_required = param.get("required", False)
                    required_str = "必需" if param_required else "可选"
                    operators_section += f"  - `{param_name}` ({param_type}, {required_str}): {param_desc}\n"

            # 添加工作流示例（重点）
            workflow_example = op.get("workflow_example")
            if workflow_example:
                operators_section += f"**工作流示例**:\n```json\n"
                if isinstance(workflow_example, str):
                    operators_section += workflow_example
                else:
                    operators_section += json.dumps(workflow_example, indent=2, ensure_ascii=False)
                operators_section += f"\n```\n"

            operators_section += "\n"

        print(f"[Workflow Agent] ✅ 算子部分长度: {len(operators_section)} 字符")

        # 构建完整 Prompt
        prompt = f"""你是一个 GIS 工作流生成助手。根据用户需求和提供的算子信息，生成一个由算子组成的工作流 DAG JSON。

{operators_section}

## 用户需求
{query}

## 任务要求

1. **分析需求**: 理解用户的 GIS 操作需求
2. **选择算子**: 从上述可用算子中选择合适的算子
3. **构建 DAG**: 按照逻辑顺序组织算子，形成有向无环图
4. **返回 JSON**: 只返回 JSON 格式的工作流，不要有其他内容

## JSON 格式规范

返回的工作流必须是以下格式：

```json
{{
  "tasks": [
    {{
      "id": "task1",
      "operator": "算子名称",
      "params": {{
        "param1": "value1",
        "param2": "value2"
      }},
      "dependencies": []
    }},
    {{
      "id": "task2",
      "operator": "算子名称",
      "params": {{
        "input_data": "task1"
      }},
      "dependencies": ["task1"]
    }}
  ]
}}
```

## 关键规则

1. **任务 ID**: 使用 `task1`, `task2`, ... 作为任务ID
2. **依赖关系**: 在 `dependencies` 中指定依赖的前驱任务ID
3. **参数传递**: 如果任务依赖前一个任务，使用前一个任务的 ID 作为参数值
4. **工作流示例**: 参考上述各个算子的 "工作流示例" 部分学习正确的 DAG JSON 格式
5. **参数有效性**: 确保所有参数值都是有效的，参数名称必须与算子定义一致
6. **逻辑连贯**: 确保任务之间有逻辑关联，依赖关系正确

## 重要提示

- **只返回 JSON**，不要有任何其他内容或解释
- **算子名称必须与上述列表完全一致**（区分大小写）
- **参数值类型必须匹配算子定义**
- **参考工作流示例的 JSON 结构**，学习正确的格式
"""
        print(f"[Workflow Agent] ✅ 详细 Prompt 构建完成，总长度: {len(prompt)} 字符")
        return prompt

    async def _fetch_operators(self) -> List[Dict]:
        """
        从 Develop API 获取算子列表

        Returns:
            算子列表
        """
        print(f"[Workflow Agent] 从 Develop API 获取算子列表...")
        print(f"[Workflow Agent] Develop API URL: {self.develop_api_url}/api/develop/operators")

        try:
            # 禁用系统代理，直接访问本地服务
            async with httpx.AsyncClient(timeout=10.0, trust_env=False) as client:
                response = await client.get(f"{self.develop_api_url}/api/develop/operators")
                response.raise_for_status()
                data = response.json()
                operators = data.get("operators", [])
                print(f"[Workflow Agent] ✅ 成功获取 {len(operators)} 个算子")
                for op in operators[:5]:  # 只打印前5个作为示例
                    print(f"[Workflow Agent]   - {op.get('name', 'Unknown')}: {op.get('description', '')[:50]}...")
                return operators
        except httpx.HTTPError as e:
            print(f"[Workflow Agent] ❌ HTTP 请求失败: {e}")
            return []
        except Exception as e:
            print(f"[Workflow Agent] ❌ 获取算子失败: {type(e).__name__}: {e}")
            return []

    def _extract_workflow(self, output: str) -> Dict:
        """
        从 LLM 输出中提取工作流 JSON

        Args:
            output: LLM 输出文本

        Returns:
            工作流字典
        """
        print(f"[Workflow Agent] 提取 JSON - 原始输出长度: {len(output)} 字符")

        # 尝试提取 JSON 代码块
        json_match = re.search(r'```json\s*\n(.*?)\n```', output, re.DOTALL)
        if json_match:
            json_str = json_match.group(1).strip()
            print(f"[Workflow Agent] 从 ```json 代码块中提取 JSON")
        else:
            # 尝试提取 {} 包裹的 JSON
            json_match = re.search(r'\{.*"tasks".*\}', output, re.DOTALL)
            if json_match:
                json_str = json_match.group(0).strip()
                print(f"[Workflow Agent] 从文本中提取 JSON 对象")
            else:
                # 假设整个输出就是 JSON
                json_str = output.strip()
                print(f"[Workflow Agent] 将整个输出作为 JSON 处理")

        print(f"[Workflow Agent] 提取的 JSON 字符串长度: {len(json_str)} 字符")
        print(f"[Workflow Agent] JSON 内容预览: {json_str[:200]}...")

        try:
            workflow = json.loads(json_str)
            print(f"[Workflow Agent] ✅ JSON 解析成功")
            return workflow
        except json.JSONDecodeError as e:
            print(f"[Workflow Agent] ❌ JSON 解析失败: {e}")
            print(f"[Workflow Agent] 失败位置: line {e.lineno}, column {e.colno}")
            print(f"[Workflow Agent] 完整 JSON 字符串:\n{json_str}")
            raise ValueError(f"Invalid JSON output from LLM: {e}")

    def _validate_workflow(self, workflow: Dict) -> bool:
        """
        验证工作流 DAG 结构

        Args:
            workflow: 工作流字典

        Returns:
            是否有效
        """
        print(f"[Workflow Agent] 验证工作流结构...")

        # 检查必需的字段
        if not isinstance(workflow, dict):
            print("[Workflow Agent] ❌ 工作流必须是字典类型")
            return False

        if "tasks" not in workflow:
            print("[Workflow Agent] ❌ 工作流缺少 'tasks' 字段")
            return False

        tasks = workflow["tasks"]
        if not isinstance(tasks, list):
            print("[Workflow Agent] ❌ 'tasks' 必须是列表类型")
            return False

        if len(tasks) == 0:
            print("[Workflow Agent] ❌ 工作流至少需要一个任务")
            return False

        print(f"[Workflow Agent] 工作流包含 {len(tasks)} 个任务")

        # 检查每个任务
        task_ids = set()
        for i, task in enumerate(tasks):
            print(f"[Workflow Agent] 验证任务 {i+1}/{len(tasks)}: {task.get('id', 'UNKNOWN')}")

            if not isinstance(task, dict):
                print(f"[Workflow Agent] ❌ 任务 {i+1} 不是字典类型")
                return False

            # 检查必需字段
            if "id" not in task or "operator" not in task or "params" not in task:
                print(f"[Workflow Agent] ❌ 任务 {i+1} 缺少必需字段 (id, operator, params)")
                print(f"[Workflow Agent] 任务内容: {task}")
                return False

            # 检查任务 ID 唯一性
            if task["id"] in task_ids:
                print(f"[Workflow Agent] ❌ 重复的任务 ID: {task['id']}")
                return False
            task_ids.add(task["id"])

            print(f"[Workflow Agent] ✅ 任务 {i+1} 验证通过 - ID: {task['id']}, 算子: {task['operator']}")

        print(f"[Workflow Agent] ✅ 所有任务验证通过")
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
