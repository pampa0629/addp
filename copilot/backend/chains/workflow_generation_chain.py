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

from models.workflow_models import Workflow, DataSourceContext
from tools.develop_tools import OperatorDetailTool


class WorkflowGenerationChain:
    """
    工作流生成 Chain

    功能：
    1. 批量获取算子详情（而不是逐个获取）
    2. 使用 Pydantic 输出解析器确保输出格式
    3. 强调 workflow_example 的重要性
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
        data_source: Optional[DataSourceContext],
        selected_operators: List[str],
        engine_type: str = "python_workflow"  # 工作流引擎类型
    ) -> Workflow:
        """
        生成工作流

        Args:
            query: 用户查询
            data_source: 数据源上下文（可选，如果数据源理解失败可为 None）
            selected_operators: 选定的算子名称列表
            engine_type: 工作流引擎类型（python_workflow/spark_workflow/math_workflow），用于获取正确的算子详情

        Returns:
            Workflow: 生成的工作流

        Raises:
            ValueError: 如果生成失败或输出解析失败
        """
        print(f"[WorkflowGenerationChain] 开始生成工作流")
        print(f"  查询: {query}")
        print(f"  选定算子: {selected_operators}")
        print(f"  引擎类型: {engine_type}")

        # 1. 批量获取算子详情（传递 engine_type）
        print(f"[WorkflowGenerationChain] 批量获取 {len(selected_operators)} 个算子的详情（引擎: {engine_type}）")
        operator_details = await self._fetch_operator_details(selected_operators, engine_type)

        if not operator_details:
            raise ValueError("无法获取算子详情，无法生成工作流")

        # 2. 格式化算子详情（强调 workflow_example）
        operators_detail_text = self._format_operator_details(operator_details)

        # 3. 格式化数据源信息
        data_source_info = self._format_data_source_info(data_source)

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

    async def _fetch_operator_details(self, operator_names: List[str], engine_type: str = "python_workflow") -> List[Dict]:
        """
        批量获取算子详情

        Args:
            operator_names: 算子名称列表
            engine_type: 工作流引擎类型（python_workflow/spark_workflow/math_workflow）

        Returns:
            算子详情列表
        """
        # 使用 asyncio.gather 并行获取所有算子详情
        tasks = [
            self.operator_detail_tool._arun(operator_name=name, engine_type=engine_type)
            for name in operator_names
        ]

        results = await asyncio.gather(*tasks, return_exceptions=True)

        # 过滤掉失败的结果
        operator_details = []
        for name, result in zip(operator_names, results):
            if isinstance(result, Exception):
                print(f"[WorkflowGenerationChain] ⚠️ 获取算子 '{name}' 详情失败: {result}")
                continue

            if result is None:
                print(f"[WorkflowGenerationChain] ⚠️ 算子 '{name}' 不存在")
                continue

            operator_details.append(result)

        print(f"[WorkflowGenerationChain] 成功获取 {len(operator_details)}/{len(operator_names)} 个算子详情")
        return operator_details

    def _format_operator_details(self, operator_details: List[Dict]) -> str:
        """
        格式化算子详情为文本（强调 workflow_example）

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

            # 算子级别的注意事项（重要！包含如"必须先 dissolve"等关键提示）
            op_notes = detailed.get("notes", [])
            if op_notes:
                lines.append("\n**⚠️ 注意事项**:")
                for note in op_notes:
                    lines.append(f"  - {note}")

            # 参数定义
            if op.get("parameters"):
                lines.append("\n**参数**:")
                for param in op["parameters"]:
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

            # ⭐ 重点：workflow_example（从 detailed_description 中取）
            workflow_example = detailed.get("workflow_example")
            if workflow_example:
                lines.append("\n**⭐ 工作流示例（重要参考）**:")
                lines.append("```json")

                import json
                if isinstance(workflow_example, dict):
                    example_json = json.dumps(workflow_example, ensure_ascii=False, indent=2)
                else:
                    example_json = workflow_example

                lines.append(example_json)
                lines.append("```")

            # 输出定义
            if op.get("outputs"):
                lines.append("\n**输出**:")
                for output in op["outputs"]:
                    lines.append(
                        f"  - `{output['name']}` ({output.get('type', 'unknown')}): "
                        f"{output.get('description', '')}"
                    )

            lines.append("\n---\n")

        return "\n".join(lines)

    def _format_data_source_info(self, data_source: Optional[DataSourceContext]) -> str:
        """
        格式化数据源信息为文本

        Args:
            data_source: 数据源上下文

        Returns:
            格式化的文本
        """
        if not data_source:
            return "数据源信息未提供（用户可能直接指定了参数）"

        lines = [
            f"**引擎 ID**: {data_source.engine_id}",
            f"**引擎名称**: {data_source.engine_name}",
            f"**引擎类型**: {data_source.engine_type}",
            f"**置信度**: {data_source.confidence:.2f}",
        ]

        # 根据引擎类型显示不同的位置信息
        location = data_source.location
        if location.schema and location.table:
            lines.append(f"**Schema**: {location.schema}")
            lines.append(f"**Table**: {location.table}")
        elif location.bucket and location.path:
            lines.append(f"**Bucket**: {location.bucket}")
            lines.append(f"**Path**: {location.path}")
        elif location.resource_identifier:
            lines.append(f"**资源标识**: {location.resource_identifier}")

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
