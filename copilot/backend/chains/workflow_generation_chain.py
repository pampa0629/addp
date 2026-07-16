"""
工作流生成 Chain

使用 LangChain LLMChain + Pydantic 输出解析器生成工作流 JSON
"""
import asyncio
from typing import List, Dict, Optional
from pathlib import Path

from langchain.chains import LLMChain
from langchain.prompts import PromptTemplate
from langchain_core.output_parsers import PydanticOutputParser
from langchain_core.exceptions import OutputParserException

from models.workflow_models import Workflow, WorkflowResourceFact
from tools.develop_tools import OperatorDetailTool
from utils.operator_contract import public_workflow_parameters


class WorkflowGenerationChain:
    """
    工作流生成 Chain

    功能：
    1. 批量获取算子详情（而不是逐个获取）
    2. 使用 Pydantic 输出解析器确保输出格式
    3. 只向 LLM 暴露 Public Operator Spec
    4. 处理 LLM 输出解析错误
    """

    def __init__(self, llm, operator_detail_tool: OperatorDetailTool):
        """
        初始化工作流生成 Chain

        Args:
            llm: LangChain LLM 实例
            operator_detail_tool: 算子详情 Tool
        """
        self.llm = llm
        self.operator_detail_tool = operator_detail_tool

        # 1. 创建 Pydantic 输出解析器
        self.output_parser = PydanticOutputParser(pydantic_object=Workflow)

        # 2. 加载 Prompt 模板
        self.prompt_template = self._load_prompt_template()

        # 3. 创建 LLMChain
        self.chain = LLMChain(
            llm=self.llm,
            prompt=self.prompt_template,
            verbose=True
        )

    def _load_prompt_template(self) -> PromptTemplate:
        """加载 Prompt 模板"""
        prompt_file = Path(__file__).parent.parent / "prompts" / "workflow_generation.txt"

        if not prompt_file.exists():
            raise FileNotFoundError(f"Prompt 模板文件不存在: {prompt_file}")

        prompt_content = prompt_file.read_text(encoding="utf-8")

        # 创建 PromptTemplate，注入 format_instructions
        return PromptTemplate(
            template=prompt_content,
            input_variables=["query", "data_source_info", "operators_detail"],
            partial_variables={
                "format_instructions": self.output_parser.get_format_instructions()
            }
        )

    async def generate(
        self,
        query: str,
        resources: List[WorkflowResourceFact],
        selected_operators: List[str],
        workflow_engine_id: Optional[int] = None,
        tenant_id: int = 0,
    ) -> Workflow:
        """
        生成工作流

        Args:
            query: 用户查询
            resources: 已由 owner Tool 验证的多资源事实
            selected_operators: 选定的算子名称列表
            workflow_engine_id: 工作流引擎实例 ID，用于获取正确的算子详情

        Returns:
            Workflow: 生成的工作流

        Raises:
            ValueError: 如果生成失败或输出解析失败
        """
        print(f"[WorkflowGenerationChain] 开始生成工作流")
        print(f"  查询: {query}")
        print(f"  选定算子: {selected_operators}")
        print(f"  工作流引擎 ID: {workflow_engine_id}")

        if not workflow_engine_id:
            raise ValueError("workflow_engine_id 是算子详情获取必需上下文")

        # 1. 批量获取算子详情（按工作流引擎实例）
        print(f"[WorkflowGenerationChain] 批量获取 {len(selected_operators)} 个算子的详情（工作流引擎 ID: {workflow_engine_id}）")
        operator_details = await self._fetch_operator_details(
            selected_operators,
            workflow_engine_id,
            tenant_id,
        )

        if not operator_details:
            raise ValueError("无法获取算子详情，无法生成工作流")

        # 2. 格式化算子的公开契约
        operators_detail_text = self._format_operator_details(operator_details)

        # 3. 格式化数据源信息
        data_source_info = self._format_resource_info(resources)

        # 📝 记录完整的算子详情（用于调试）
        print("\n" + "="*80)
        print("[WorkflowGenerationChain] 传递给 LLM 的算子详情:")
        print("="*80)
        print(operators_detail_text)
        print("="*80 + "\n")

        # 4. 调用 LLMChain
        print(f"[WorkflowGenerationChain] 调用 LLM 生成工作流")
        try:
            result = await self.chain.ainvoke({
                "query": query,
                "data_source_info": data_source_info,
                "operators_detail": operators_detail_text
            })

            # 5. 解析 LLM 输出
            llm_output = result["text"]
            print(f"[WorkflowGenerationChain] LLM 输出长度: {len(llm_output)} 字符")

            # 📝 记录完整的 LLM 输出（用于调试）
            print("\n" + "="*80)
            print("[WorkflowGenerationChain] LLM 原始输出:")
            print("="*80)
            print(llm_output)
            print("="*80 + "\n")

            workflow = self.output_parser.parse(llm_output)

            print(f"[WorkflowGenerationChain] ✅ 成功生成工作流：{len(workflow.tasks)} 个任务")
            return workflow

        except OutputParserException as e:
            print(f"[WorkflowGenerationChain] ❌ 输出解析失败: {e}")

            # 尝试手动清理 JSON（去除 markdown 代码块标记）
            cleaned_output = self._clean_llm_output(result.get("text", ""))
            if cleaned_output:
                try:
                    workflow = self.output_parser.parse(cleaned_output)
                    print(f"[WorkflowGenerationChain] ✅ 清理后解析成功")
                    return workflow
                except Exception as retry_error:
                    print(f"[WorkflowGenerationChain] ❌ 清理后仍然解析失败: {retry_error}")

            raise ValueError(f"工作流 JSON 解析失败: {e}")

        except Exception as e:
            print(f"[WorkflowGenerationChain] ❌ 生成工作流失败: {type(e).__name__}: {e}")
            raise

    async def _fetch_operator_details(
        self,
        operator_names: List[str],
        workflow_engine_id: int,
        tenant_id: int,
    ) -> List[Dict]:
        """
        批量获取算子详情

        Args:
            operator_names: 算子名称列表
            workflow_engine_id: 工作流引擎实例 ID

        Returns:
            算子详情列表
        """
        # 使用 asyncio.gather 并行获取所有算子详情
        tasks = [
            self.operator_detail_tool._arun(
                operator_name=name,
                workflow_engine_id=workflow_engine_id,
                tenant_id=tenant_id,
            )
            for name in operator_names
        ]

        results = await asyncio.gather(*tasks)

        operator_details = []
        for name, result in zip(operator_names, results):
            if result is None:
                raise ValueError(f"工作流引擎 {workflow_engine_id} 不存在算子 {name}")

            operator_details.append(result)

        print(f"[WorkflowGenerationChain] 成功获取 {len(operator_details)}/{len(operator_names)} 个算子详情")
        return operator_details

    def _format_operator_details(self, operator_details: List[Dict]) -> str:
        """
        格式化算子公开契约为文本

        Args:
            operator_details: 算子详情列表

        Returns:
            格式化的文本
        """
        lines = []

        for i, op in enumerate(operator_details, 1):
            lines.append(f"### {i}. {op['name']} - {op.get('brief_description', '')}")
            lines.append(f"**描述**: {op.get('description', '')}")
            lines.append(f"**分类**: {op.get('category', '未分类')}")

            # 从 detailed_description 中提取详细信息
            detailed = op.get("detailed_description", {})
            if not isinstance(detailed, dict):
                detailed = {}

            # 算子级别的注意事项（重要！包含如"必须先 dissolve"等关键提示）
            op_notes = detailed.get("notes", [])
            if op_notes:
                lines.append("\n**⚠️ 注意事项**:")
                for note in op_notes:
                    lines.append(f"  - {note}")

            parameters = public_workflow_parameters(op)
            if parameters:
                lines.append("\n**参数**:")
                for param in parameters:
                    required = "必需" if param.get("required") else "可选"
                    param_type = param.get("type", "未知")
                    default = param.get("default", "无")

                    # 构建参数行
                    param_line = f"  - `{param['name']}` ({param_type}, {required}): {param.get('description', '')}"

                    # ⭐ 重要：添加 enum 约束（如果存在）
                    if param.get("enum"):
                        enum_values = ", ".join([f'"{v}"' for v in param["enum"]])
                        param_line += f" **[可选值: {enum_values}]**"

                    param_line += f" [默认: {default}]"

                    # 添加 notes（如果存在）
                    if param.get("notes"):
                        param_line += f"\n    说明: {param['notes']}"

                    lines.append(param_line)

            # 输出定义
            if op.get("output_ports"):
                lines.append("\n**输出**:")
                for output in op["output_ports"]:
                    lines.append(
                        f"  - `{output['name']}` ({output.get('type', 'unknown')}): "
                        f"{output.get('description', '')}"
                    )

            lines.append("\n---\n")

        return "\n".join(lines)

    def _format_resource_info(self, resources: List[WorkflowResourceFact]) -> str:
        lines = []
        for resource in resources:
            lines.extend([
                f"### {resource.role}",
                f"- locator: `{resource.locator}`",
                f"- data_type: {resource.data_type or '未知'}",
                f"- geometry_column: {resource.geometry_column or '无'}",
                f"- geometry_type: {resource.geometry_type or '未知'}",
                f"- crs: {resource.crs or '未知'}",
            ])
            if resource.fields:
                lines.append(f"- fields: {resource.fields}")
        return "\n".join(lines)

    def _clean_llm_output(self, output: str) -> str:
        """
        清理 LLM 输出（去除 markdown 代码块标记等）

        Args:
            output: LLM 原始输出

        Returns:
            清理后的 JSON 字符串
        """
        output = output.strip()

        # 去除 markdown 代码块标记
        if output.startswith("```json"):
            output = output[7:]  # 去除 ```json
        elif output.startswith("```"):
            output = output[3:]  # 去除 ```

        if output.endswith("```"):
            output = output[:-3]  # 去除尾部 ```

        return output.strip()


# 便捷函数：创建 WorkflowGenerationChain 实例
def create_workflow_generation_chain(llm, operator_detail_tool: OperatorDetailTool):
    """创建工作流生成 Chain 实例"""
    return WorkflowGenerationChain(llm, operator_detail_tool)
