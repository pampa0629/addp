"""
算子筛选 Chain

使用 LangChain SequentialChain 实现算子筛选逻辑，替换现有的 OperatorSelectionAgent
"""
import json
from typing import List, Dict, Optional
from langchain.chains import LLMChain
from langchain.prompts import PromptTemplate
from langchain.output_parsers import PydanticOutputParser, OutputFixingParser
from pydantic import BaseModel, Field

from tools.develop_tools import OperatorDiscoveryTool


class OperatorListOutput(BaseModel):
    """算子列表输出模型"""
    operators: List[str] = Field(description="选中的算子名称列表")
    reasoning: Optional[str] = Field(None, description="选择理由")


class OperatorSelectionChain:
    """
    算子筛选 Chain

    功能：
    - 从所有算子中选择与用户查询最相关的 3-8 个算子
    - 使用 JsonOutputParser 自动解析 LLM 输出
    - 支持按类别分组展示算子
    """

    def __init__(self, llm, operator_tool: Optional[OperatorDiscoveryTool] = None):
        """
        初始化算子筛选 Chain

        Args:
            llm: LLM 实例（LangChain 兼容的 LLM）
            operator_tool: 算子发现 Tool（可选，默认创建新实例）
        """
        self.llm = llm
        self.operator_tool = operator_tool or OperatorDiscoveryTool()

        # 创建 Pydantic 输出解析器
        self.output_parser = PydanticOutputParser(pydantic_object=OperatorListOutput)

        # 创建修复解析器（自动修复 JSON 格式错误）
        self.fixing_parser = OutputFixingParser.from_llm(
            parser=self.output_parser,
            llm=self.llm
        )

        # 加载 Prompt 模板
        self.prompt_template = self._load_prompt_template()

        # 创建 LLMChain（不设置 output_parser，手动解析）
        self.chain = LLMChain(
            llm=self.llm,
            prompt=self.prompt_template,
            verbose=True
        )

    def _load_prompt_template(self) -> PromptTemplate:
        """加载 Prompt 模板"""
        # 尝试从文件加载
        try:
            with open("prompts/operator_selection.txt", "r", encoding="utf-8") as f:
                template = f.read()
        except FileNotFoundError:
            # 如果文件不存在，使用内置模板
            template = self._get_builtin_template()

        return PromptTemplate(
            template=template,
            input_variables=["query", "operators_by_category", "data_source_info"],
            partial_variables={
                "format_instructions": self.output_parser.get_format_instructions()
            }
        )

    def _get_builtin_template(self) -> str:
        """获取内置 Prompt 模板"""
        return """你是一个 GIS 算子选择专家。根据用户需求和数据源信息，从以下算子中选择最相关的算子。

## 用户需求
{query}

## 数据源信息
{data_source_info}

## 可用算子（按类别分组）
{operators_by_category}

## 任务要求

请选择 **3-8 个最相关的算子**，遵循以下原则：

1. **必须包含数据输入算子**：如 `load`（从数据库/文件加载）
2. **必须包含数据输出算子**：如 `save`（保存到数据库/文件）
3. **选择核心处理算子**：根据需求选择，如缓冲、相交、融合等
4. **避免功能重复**：不要选择功能相似的算子
5. **优先简单直接**：能用一个算子完成就不用多个

## 输出格式

{format_instructions}

**重要**: 只返回 JSON 格式，不要有任何其他内容或解释。
"""

    def _format_operators_by_category(self, operators: List[Dict]) -> str:
        """按类别格式化算子列表"""
        # 按类别分组
        by_category = {}
        for op in operators:
            cat = op.get("category", "其他")
            if cat not in by_category:
                by_category[cat] = []
            by_category[cat].append(op)

        # 格式化输出
        result = ""
        for cat, ops in sorted(by_category.items()):
            result += f"\n### {cat}\n"
            for op in ops:
                result += f"- **{op['name']}**: {op['brief']}\n"

        return result

    async def select(
        self,
        query: str,
        data_source_info: Optional[str] = None,
        engine_type: str = "python"  # 工作流引擎类型
    ) -> List[str]:
        """
        选择相关算子

        Args:
            query: 用户查询
            data_source_info: 数据源信息（可选）
            engine_type: 工作流引擎类型（python/spark/math），只获取该引擎的算子

        Returns:
            选中的算子名称列表
        """
        print(f"\n{'='*60}")
        print(f"[OperatorSelectionChain] 开始选择算子")
        print(f"[OperatorSelectionChain] 用户查询: {query}")
        print(f"[OperatorSelectionChain] 引擎类型: {engine_type}")
        print(f"{'='*60}\n")

        try:
            # 1. 获取指定引擎的算子（使用缓存）
            all_operators = await self.operator_tool._arun(engine_type=engine_type)
            print(f"[OperatorSelectionChain] 获取到 {len(all_operators)} 个算子（引擎: {engine_type}）")

            if not all_operators:
                print(f"[OperatorSelectionChain] ⚠️ 没有可用的算子")
                return []

            # 2. 格式化算子列表
            operators_by_category = self._format_operators_by_category(all_operators)

            # 3. 构建数据源信息
            data_source_str = data_source_info or "未提供数据源信息"

            # 4. 调用 LLM
            print(f"[OperatorSelectionChain] 正在调用 LLM...")
            result = await self.chain.ainvoke({
                "query": query,
                "operators_by_category": operators_by_category,
                "data_source_info": data_source_str
            })

            # 5. 解析结果（LLM 返回字符串）
            llm_output = result.get("text", "") if isinstance(result, dict) else str(result)
            print(f"[OperatorSelectionChain] LLM 输出长度: {len(llm_output)} 字符")

            # 使用 fixing_parser 自动修复并解析
            try:
                output = self.fixing_parser.parse(llm_output)
            except Exception as e:
                print(f"[OperatorSelectionChain] ⚠️ 解析失败，尝试普通 parser: {e}")
                output = self.output_parser.parse(llm_output)

            selected = output.operators[:8]  # 限制最多 8 个
            print(f"[OperatorSelectionChain] ✅ 选中 {len(selected)} 个算子: {selected}")

            if output.reasoning:
                print(f"[OperatorSelectionChain] 选择理由: {output.reasoning}")

            return selected

        except json.JSONDecodeError as e:
            print(f"[OperatorSelectionChain] ❌ JSON 解析失败: {e}")
            # 回退：尝试从文本中提取算子名称
            return self._fallback_parse(str(result))
        except Exception as e:
            print(f"[OperatorSelectionChain] ❌ 算子选择失败: {type(e).__name__}: {e}")
            import traceback
            traceback.print_exc()
            return []

    def _fallback_parse(self, text: str) -> List[str]:
        """回退方案：从文本中提取算子名称"""
        print(f"[OperatorSelectionChain] 使用回退方案解析")
        import re

        operators = []
        # 尝试提取 JSON 数组
        json_match = re.search(r'\[(.*?)\]', text, re.DOTALL)
        if json_match:
            try:
                operators = json.loads('[' + json_match.group(1) + ']')
                if isinstance(operators, list) and all(isinstance(op, str) for op in operators):
                    return operators[:8]
            except:
                pass

        # 逐行提取
        for line in text.split('\n'):
            line = line.strip(' -"\'[]')
            if line and not line.startswith('#'):
                match = re.match(r'^([a-z_][a-z0-9_]*)', line, re.IGNORECASE)
                if match:
                    operators.append(match.group(1))

        return operators[:8]


# 创建工厂函数
def create_operator_selection_chain(llm, operator_tool: Optional[OperatorDiscoveryTool] = None):
    """创建算子筛选 Chain"""
    return OperatorSelectionChain(llm, operator_tool)
