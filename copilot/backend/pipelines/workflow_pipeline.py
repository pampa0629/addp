"""
工作流生成主流水线

协调 5 个阶段：数据源理解、算子筛选、工作流生成、验证、多轮对话改进
"""
from typing import Dict, Optional, Any

from chains.operator_selection_chain import OperatorSelectionChain
from chains.workflow_generation_chain import WorkflowGenerationChain
from chains.workflow_validation_chain import WorkflowValidationChain
from chains.workflow_auto_fix import WorkflowAutoFixer
from chains.workflow_modification_chain import WorkflowModificationChain

from models.workflow_models import Workflow, WorkflowResourceFact, ValidationResult

from tools.develop_tools import OperatorDiscoveryTool, OperatorDetailTool


class WorkflowPipeline:
    """
    工作流生成主流水线

    协调 5 个阶段：
    1. 消费调用方已验证的多资源事实
    2. 算子筛选（OperatorSelectionChain）
    3. 工作流生成（WorkflowGenerationChain）
    4. 工作流验证 + 自动修复（WorkflowValidationChain + AutoFixer）
    5. 多轮对话改进（WorkflowModificationChain，可选）
    """

    def __init__(self, llm):
        """
        初始化工作流生成流水线

        Args:
            llm: LangChain LLM 实例
        """
        self.llm = llm
        # 初始化所有 Tools
        print("[WorkflowPipeline] 初始化 Tools")
        self.operator_discovery_tool = OperatorDiscoveryTool()
        self.operator_detail_tool = OperatorDetailTool()

        # 初始化各阶段
        print("[WorkflowPipeline] 初始化各阶段 Chains/Agents")

        # 阶段 2: 算子筛选 Chain
        self.operator_selection_chain = OperatorSelectionChain(
            llm=self.llm,
            operator_tool=self.operator_discovery_tool
        )

        # 阶段 3: 工作流生成 Chain
        self.workflow_generation_chain = WorkflowGenerationChain(
            llm=self.llm,
            operator_detail_tool=self.operator_detail_tool
        )

        # 阶段 4: 工作流验证 Chain
        self.workflow_validation_chain = WorkflowValidationChain(
            operator_detail_tool=self.operator_detail_tool
        )

        # 阶段 4: 自动修复器
        self.workflow_auto_fixer = WorkflowAutoFixer(
            llm=self.llm,
            validator=self.workflow_validation_chain,
            max_retries=2
        )

        # 阶段 5: 工作流修改 Chain（延迟初始化，避免初始化错误）
        self.workflow_modification_chain = None

        print("[WorkflowPipeline] ✅ 初始化完成")

    async def run(
        self,
        query: str,
        tenant_id: int = 1,
        workflow_engine_id: Optional[int] = None,
        resources: list[WorkflowResourceFact] | None = None,
    ) -> Dict[str, Any]:
        """
        执行完整的工作流生成流程

        Args:
            query: 用户查询
            tenant_id: 租户 ID
            workflow_engine_id: 工作流引擎实例 ID，用于算子发现、详情获取和验证

        Returns:
            生成结果字典，包含：
            - status: 状态（success、need_clarification、validation_failed）
            - workflow: 生成的工作流
            - explanation: 解释文本
            - data_source: 数据源上下文（如果有）
            - selected_operators: 选定的算子列表
            - validation_result: 验证结果
            - errors: 错误列表（如果有）
        """
        print("=" * 60)
        print(f"[WorkflowPipeline] 开始生成工作流")
        print(f"  查询: {query}")
        print(f"  租户 ID: {tenant_id}")
        print(f"  工作流引擎 ID: {workflow_engine_id}")
        print("=" * 60)

        try:
            if not workflow_engine_id:
                raise ValueError("workflow_engine_id 是 Copilot 工作流生成必需上下文")

            resources = resources or []
            if not resources:
                return {
                    "status": "need_clarification",
                    "clarification_reason": "resource_facts_required",
                    "message": "请先通过 ADDP 数据发现与预览确认工作流输入资源",
                }
            missing_crs_roles = [
                resource.role
                for resource in resources
                if resource.geometry_column and not resource.crs
            ]
            if missing_crs_roles:
                return {
                    "status": "need_clarification",
                    "clarification_reason": "resource_crs_required",
                    "message": "以下空间资源缺少 CRS：" + "、".join(missing_crs_roles),
                }

            # ========== 阶段 2: 算子筛选 ==========
            print("\n[WorkflowPipeline] ▶ 阶段 2: 算子筛选")
            resource_info = self._format_resources(resources)
            selected_operators = await self.operator_selection_chain.select(
                query,
                resource_info,
                workflow_engine_id=workflow_engine_id,
                tenant_id=tenant_id,
            )

            print(f"[WorkflowPipeline] ✅ 算子筛选完成：{len(selected_operators)} 个算子")
            print(f"  算子列表: {', '.join(selected_operators)}")

            # ========== 阶段 3: 工作流生成 ==========
            print("\n[WorkflowPipeline] ▶ 阶段 3: 工作流生成")
            workflow = await self.workflow_generation_chain.generate(
                query=query,
                resources=resources,
                selected_operators=selected_operators,
                workflow_engine_id=workflow_engine_id,
                tenant_id=tenant_id,
            )

            print(f"[WorkflowPipeline] ✅ 工作流生成完成：{len(workflow.tasks)} 个任务")

            if not self._workflow_resource_facts_are_verified(workflow, resources):
                print("[WorkflowPipeline] ⚠️ 生成结果包含未验证的资源 locator")
                return {
                    "status": "need_clarification",
                    "clarification_reason": "resource_facts_unverified",
                    "message": "候选工作流引用了 resources 之外的 locator",
                }

            # ========== 阶段 4: 工作流验证 + 自动修复 ==========
            print("\n[WorkflowPipeline] ▶ 阶段 4: 工作流验证")
            validation_result = await self.workflow_validation_chain.validate(
                workflow,
                workflow_engine_id=workflow_engine_id,
                tenant_id=tenant_id,
            )

            if not validation_result.is_valid:
                print(f"[WorkflowPipeline] ⚠️ 验证失败，尝试自动修复")
                print(f"  错误数量: {len(validation_result.errors)}")

                # 自动修复
                workflow, validation_result = await self.workflow_auto_fixer.auto_fix(
                    workflow,
                    validation_result,
                    workflow_engine_id=workflow_engine_id,
                    tenant_id=tenant_id,
                )

                # 重新检查验证结果
                if not validation_result.is_valid:
                    print(f"[WorkflowPipeline] ❌ 自动修复失败")
                    has_previewable_workflow = bool(workflow.tasks)
                    return {
                        "status": "validation_failed",
                        "workflow": workflow.dict(),
                        "errors": validation_result.errors,
                        "warnings": validation_result.warnings,
                        "suggestions": validation_result.suggestions,
                        "message": (
                            "工作流生成但存在问题，请预览后决定是否使用"
                            if has_previewable_workflow
                            else "工作流生成失败，未形成可预览的任务"
                        ),
                        "resources": [resource.model_dump(exclude_none=True) for resource in resources],
                        "selected_operators": selected_operators,
                        "validation_result": validation_result.dict()
                    }
                else:
                    print(f"[WorkflowPipeline] ✅ 自动修复成功")

            print(f"[WorkflowPipeline] ✅ 工作流验证通过")
            if validation_result.warnings:
                print(f"  警告数量: {len(validation_result.warnings)}")

            # ========== 成功 ==========
            print("\n" + "=" * 60)
            print(f"[WorkflowPipeline] ✅ 工作流生成成功！")
            print(f"  任务数量: {len(workflow.tasks)}")
            print(f"  算子列表: {', '.join([t.operator for t in workflow.tasks])}")
            print("=" * 60)

            return {
                "status": "success",
                "workflow": workflow.dict(),
                "explanation": self._generate_explanation(workflow, resources, selected_operators),
                "resources": [resource.model_dump(exclude_none=True) for resource in resources],
                "selected_operators": selected_operators,
                "validation_result": validation_result.dict()
            }

        except Exception as e:
            print(f"\n[WorkflowPipeline] ❌ 错误: {type(e).__name__}: {str(e)}")
            import traceback
            traceback.print_exc()

            return {
                "status": "error",
                "message": f"工作流生成失败: {str(e)}",
                "error_type": type(e).__name__
            }

    async def modify(
        self,
        user_input: str,
        current_workflow: Workflow,
        workflow_engine_id: Optional[int] = None,
        tenant_id: int = 0,
    ) -> Dict[str, Any]:
        """
        修改现有工作流（多轮对话）

        Args:
            user_input: 用户的修改请求
            current_workflow: 当前工作流
            workflow_engine_id: 工作流引擎实例 ID

        Returns:
            修改结果字典
        """
        print("\n" + "=" * 60)
        print(f"[WorkflowPipeline] 修改工作流")
        print(f"  用户输入: {user_input}")
        print(f"  工作流引擎 ID: {workflow_engine_id}")
        print("=" * 60)

        if not workflow_engine_id:
            return {
                "status": "error",
                "message": "workflow_engine_id 是工作流修改必需上下文"
            }

        # 延迟初始化 WorkflowModificationChain
        if self.workflow_modification_chain is None:
            print("[WorkflowPipeline] 初始化 WorkflowModificationChain...")
            self.workflow_modification_chain = WorkflowModificationChain(self.llm)

        try:
            # 调用修改 Chain
            modified_workflow = await self.workflow_modification_chain.modify(
                user_input=user_input,
                current_workflow=current_workflow
            )

            # 验证修改后的工作流
            print(f"[WorkflowPipeline] 验证修改后的工作流")
            validation_result = await self.workflow_validation_chain.validate(
                modified_workflow,
                workflow_engine_id=workflow_engine_id,
                tenant_id=tenant_id,
            )

            if not validation_result.is_valid:
                print(f"[WorkflowPipeline] ⚠️ 修改后的工作流验证失败，尝试自动修复")
                modified_workflow, validation_result = await self.workflow_auto_fixer.auto_fix(
                    modified_workflow,
                    validation_result,
                    workflow_engine_id=workflow_engine_id,
                    tenant_id=tenant_id,
                )

            return {
                "status": "success" if validation_result.is_valid else "validation_failed",
                "workflow": modified_workflow.dict(),
                "explanation": "已根据你的反馈更新工作流",
                "validation_result": validation_result.dict()
            }

        except Exception as e:
            print(f"[WorkflowPipeline] ❌ 修改失败: {type(e).__name__}: {str(e)}")
            import traceback
            traceback.print_exc()

            return {
                "status": "error",
                "message": f"工作流修改失败: {str(e)}",
                "error_type": type(e).__name__
            }

    @staticmethod
    def _format_resources(resources: list[WorkflowResourceFact]) -> str:
        return "\n".join(
            f"- role={resource.role}; locator={resource.locator}; "
            f"geometry_column={resource.geometry_column or '无'}; crs={resource.crs or '未知'}"
            for resource in resources
        )

    @staticmethod
    def _workflow_resource_facts_are_verified(
        workflow: Workflow,
        resources: list[WorkflowResourceFact],
    ) -> bool:
        source_locators = {resource.locator for resource in resources}
        for task in workflow.tasks:
            if task.operator == "load" and task.params.get("locator") not in source_locators:
                return False
        return True

    def _generate_explanation(
        self,
        workflow: Workflow,
        resources: list[WorkflowResourceFact],
        selected_operators: list
    ) -> str:
        """
        生成工作流的解释文本

        Args:
            workflow: 生成的工作流
            resources: 已验证的多资源事实
            selected_operators: 选定的算子列表

        Returns:
            解释文本
        """
        lines = []

        lines.append(f"已生成包含 {len(workflow.tasks)} 个步骤的工作流：")

        # 列出任务
        for i, task in enumerate(workflow.tasks, 1):
            lines.append(f"{i}. {task.operator}")

        lines.append("\n输入资源：" + "、".join(resource.role for resource in resources))

        return "\n".join(lines)


# 便捷函数：创建 WorkflowPipeline 实例
def create_workflow_pipeline(llm):
    """创建工作流生成流水线实例"""
    return WorkflowPipeline(llm)
