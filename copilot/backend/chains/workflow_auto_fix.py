"""
工作流自动修复

基于验证错误，使用 LLM 自动修复工作流
"""
from typing import Optional

from langchain_core.messages import HumanMessage, SystemMessage
from langchain_core.output_parsers import PydanticOutputParser
from langchain_core.prompts import PromptTemplate

from models.workflow_models import Workflow, ValidationResult
from chains.workflow_validation_chain import WorkflowValidationChain


class WorkflowAutoFixer:
    """
    工作流自动修复器

    功能：
    1. 基于验证错误生成修复 Prompt
    2. 调用 LLM 修复工作流
    3. 重新验证修复后的工作流
    4. 支持最多 N 次重试
    """

    def __init__(self, llm, validator: WorkflowValidationChain, max_retries: int = 2):
        """
        初始化自动修复器

        Args:
            llm: LangChain LLM 实例
            validator: 工作流验证器
            max_retries: 最大重试次数
        """
        self.llm = llm
        self.validator = validator
        self.max_retries = max_retries

        # 创建输出解析器
        self.output_parser = PydanticOutputParser(pydantic_object=Workflow)

        # 创建修复 Prompt 模板
        self.fix_prompt_template = self._create_fix_prompt_template()

    def _create_fix_prompt_template(self) -> PromptTemplate:
        """创建修复 Prompt 模板"""
        template = """你是一个 GIS 工作流修复专家。当前工作流验证失败，请根据错误信息修复工作流。

## 当前工作流
```json
{workflow_json}
```

## 验证错误
{errors}

## 验证警告
{warnings}

## 修复建议
{suggestions}

## 任务要求

请仔细分析上述错误和建议，修复工作流中的问题：

1. **必需参数缺失**：补充所有必需参数（参考算子定义）
2. **参数名称错误**：修正拼写错误，确保与算子定义完全一致
3. **循环依赖**：调整 depends_on 关系，确保形成有向无环图
4. **依赖不存在或缺少依赖字段**：删除无效的依赖引用；每个任务都必须显式包含 `depends_on`，没有依赖时写空数组 `[]`
5. **任务 ID 重复**：为重复的任务分配唯一 ID
6. **未知算子**：替换为正确的算子名称
7. **资源参数错误**：
   - load 任务读取已有表、文件或对象时必须使用 `locator`
   - save 任务创建新目标时必须使用 `target_parent_locator + target_name`
   - 不要在算子 params 中填写 `engine_id`、`connection_info`、`schema`、`table`、`path`

## 修复原则

- **最小改动**：只修复错误，不要改变工作流的整体逻辑
- **保持一致**：确保修改后的工作流仍然符合用户需求
- **验证合法**：修复后的工作流必须通过所有验证
- **不编造资源**：如果当前工作流或错误建议中没有可用 locator，不要自行拼接 `addp://...`；保留最小可修复结构，让验证结果提示用户补充资源选择

## 输出格式

{format_instructions}

**重要提示**：只返回修复后的工作流 JSON，不要有任何其他内容或解释。
"""

        return PromptTemplate(
            template=template,
            input_variables=["workflow_json", "errors", "warnings", "suggestions"],
            partial_variables={
                "format_instructions": self.output_parser.get_format_instructions()
            }
        )

    async def auto_fix(
        self,
        workflow: Workflow,
        validation_result: ValidationResult,
        workflow_engine_id: Optional[int] = None,
        tenant_id: int = 0,
    ) -> tuple[Workflow, ValidationResult]:
        """
        自动修复工作流

        Args:
            workflow: 待修复的工作流
            validation_result: 验证结果（包含错误信息）
            workflow_engine_id: 工作流引擎实例 ID，用于重新验证算子定义

        Returns:
            (修复后的工作流, 最终验证结果)
        """
        current_workflow = workflow
        current_validation = validation_result

        for _ in range(self.max_retries):
            # 如果已经验证通过，直接返回
            if current_validation.is_valid:
                return current_workflow, current_validation

            # 调用 LLM 修复
            try:
                fixed_workflow = await self._fix_once(current_workflow, current_validation)

                # 重新验证
                new_validation = await self.validator.validate(
                    fixed_workflow,
                    workflow_engine_id=workflow_engine_id,
                    tenant_id=tenant_id,
                )

                # 更新当前状态
                current_workflow = fixed_workflow
                current_validation = new_validation

                # 如果验证通过，提前返回
                if new_validation.is_valid:
                    return current_workflow, current_validation

            except Exception:
                continue

        return current_workflow, current_validation

    async def _fix_once(
        self,
        workflow: Workflow,
        validation_result: ValidationResult
    ) -> Workflow:
        """
        执行一次修复

        Args:
            workflow: 待修复的工作流
            validation_result: 验证结果

        Returns:
            修复后的工作流

        Raises:
            ValueError: 如果修复失败
        """
        # 准备输入
        workflow_json = workflow.model_dump_json(indent=2)
        errors_text = "\n".join(f"- {e}" for e in validation_result.errors)
        warnings_text = "\n".join(f"- {w}" for w in validation_result.warnings) if validation_result.warnings else "无"
        suggestions_text = "\n".join(f"- {s}" for s in validation_result.suggestions) if validation_result.suggestions else "无"

        prompt = self.fix_prompt_template.format(
            workflow_json=workflow_json,
            errors=errors_text,
            warnings=warnings_text,
            suggestions=suggestions_text,
        )
        response = await self.llm.ainvoke([
            SystemMessage(content="你是 ADDP 工作流修复器，只能返回符合给定契约的工作流。"),
            HumanMessage(content=prompt),
        ])
        try:
            return self.output_parser.parse(str(getattr(response, "content", response)))
        except Exception as error:
            raise ValueError("工作流修复结果不符合结构化契约") from error
