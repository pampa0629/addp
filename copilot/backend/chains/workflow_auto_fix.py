"""
工作流自动修复

基于验证错误，使用 LLM 自动修复工作流
"""
from typing import Optional

from langchain.chains import LLMChain
from langchain.prompts import PromptTemplate
from langchain_core.output_parsers import PydanticOutputParser
from langchain_core.exceptions import OutputParserException

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

        # 创建 LLMChain
        self.fix_chain = LLMChain(
            llm=self.llm,
            prompt=self.fix_prompt_template,
            verbose=True
        )

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
        workflow_engine_id: Optional[int] = None
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
        print(f"[WorkflowAutoFixer] 开始自动修复工作流")
        print(f"  错误数量: {len(validation_result.errors)}")
        print(f"  警告数量: {len(validation_result.warnings)}")

        current_workflow = workflow
        current_validation = validation_result

        for attempt in range(self.max_retries):
            # 如果已经验证通过，直接返回
            if current_validation.is_valid:
                print(f"[WorkflowAutoFixer] ✅ 工作流已验证通过")
                return current_workflow, current_validation

            print(f"[WorkflowAutoFixer] 第 {attempt + 1}/{self.max_retries} 次修复尝试")

            # 调用 LLM 修复
            try:
                fixed_workflow = await self._fix_once(current_workflow, current_validation)

                # 重新验证
                print(f"[WorkflowAutoFixer] 重新验证修复后的工作流")
                new_validation = await self.validator.validate(
                    fixed_workflow,
                    workflow_engine_id=workflow_engine_id
                )

                # 更新当前状态
                current_workflow = fixed_workflow
                current_validation = new_validation

                # 如果验证通过，提前返回
                if new_validation.is_valid:
                    print(f"[WorkflowAutoFixer] ✅ 修复成功！（第 {attempt + 1} 次尝试）")
                    return current_workflow, current_validation

                # 如果错误数量减少，说明有进展
                if len(new_validation.errors) < len(validation_result.errors):
                    print(f"[WorkflowAutoFixer] 修复有进展：错误从 {len(validation_result.errors)} 减少到 {len(new_validation.errors)}")
                else:
                    print(f"[WorkflowAutoFixer] ⚠️ 修复未减少错误数量")

            except Exception as e:
                print(f"[WorkflowAutoFixer] ❌ 第 {attempt + 1} 次修复失败: {type(e).__name__}: {e}")
                # 继续尝试

        # 所有尝试都失败
        print(f"[WorkflowAutoFixer] ❌ 自动修复失败（{self.max_retries} 次尝试后仍有错误）")
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
        workflow_json = workflow.json(indent=2, ensure_ascii=False)
        errors_text = "\n".join(f"- {e}" for e in validation_result.errors)
        warnings_text = "\n".join(f"- {w}" for w in validation_result.warnings) if validation_result.warnings else "无"
        suggestions_text = "\n".join(f"- {s}" for s in validation_result.suggestions) if validation_result.suggestions else "无"

        # 调用 LLM
        result = await self.fix_chain.ainvoke({
            "workflow_json": workflow_json,
            "errors": errors_text,
            "warnings": warnings_text,
            "suggestions": suggestions_text
        })

        llm_output = result["text"]

        # 解析输出
        try:
            fixed_workflow = self.output_parser.parse(llm_output)
            return fixed_workflow

        except OutputParserException as e:
            print(f"[WorkflowAutoFixer] ❌ 输出解析失败，尝试清理: {e}")

            # 尝试清理输出
            cleaned_output = self._clean_llm_output(llm_output)
            if cleaned_output:
                try:
                    fixed_workflow = self.output_parser.parse(cleaned_output)
                    return fixed_workflow
                except Exception as retry_error:
                    print(f"[WorkflowAutoFixer] ❌ 清理后仍然解析失败: {retry_error}")

            raise ValueError(f"无法解析修复后的工作流 JSON: {e}")

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
            output = output[7:]
        elif output.startswith("```"):
            output = output[3:]

        if output.endswith("```"):
            output = output[:-3]

        return output.strip()


# 便捷函数：自动修复工作流
async def auto_fix_workflow(
    workflow: Workflow,
    validation_result: ValidationResult,
    llm,
    validator: WorkflowValidationChain,
    max_retries: int = 2,
    workflow_engine_id: Optional[int] = None
) -> tuple[Workflow, ValidationResult]:
    """
    自动修复工作流（便捷函数）

    Args:
        workflow: 待修复的工作流
        validation_result: 验证结果
        llm: LLM 实例
        validator: 验证器
        max_retries: 最大重试次数
        workflow_engine_id: 工作流引擎实例 ID

    Returns:
        (修复后的工作流, 最终验证结果)
    """
    fixer = WorkflowAutoFixer(llm, validator, max_retries)
    return await fixer.auto_fix(
        workflow,
        validation_result,
        workflow_engine_id=workflow_engine_id
    )
