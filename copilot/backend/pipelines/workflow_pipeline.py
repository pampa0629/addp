"""
工作流生成主流水线

协调 5 个阶段：数据源理解、算子筛选、工作流生成、验证、多轮对话改进
"""
from typing import Dict, Optional, Any

from agents.data_source_agent import DataSourceAgent
from chains.operator_selection_chain import OperatorSelectionChain
from chains.workflow_generation_chain import WorkflowGenerationChain
from chains.workflow_validation_chain import WorkflowValidationChain
from chains.workflow_auto_fix import WorkflowAutoFixer
from chains.workflow_modification_chain import WorkflowModificationChain

from models.workflow_models import Workflow, DataSourceContext, ValidationResult

from tools.develop_tools import (
    EngineTool,
    SchemaTableTool,
    ObjectStorageTool,
    OperatorDiscoveryTool,
    OperatorDetailTool
)
from tools.meta_tools import MetadataSearchTool


class WorkflowPipeline:
    """
    工作流生成主流水线

    协调 5 个阶段：
    1. 数据源理解（DataSourceAgent）
    2. 算子筛选（OperatorSelectionChain）
    3. 工作流生成（WorkflowGenerationChain）
    4. 工作流验证 + 自动修复（WorkflowValidationChain + AutoFixer）
    5. 多轮对话改进（WorkflowModificationChain，可选）
    """

    def __init__(self, llm, enable_data_source_understanding: bool = True):
        """
        初始化工作流生成流水线

        Args:
            llm: LangChain LLM 实例
            enable_data_source_understanding: 是否启用数据源理解阶段
        """
        self.llm = llm
        self.enable_data_source_understanding = enable_data_source_understanding

        # 初始化所有 Tools
        print("[WorkflowPipeline] 初始化 Tools")
        self.engine_tool = EngineTool()
        self.schema_table_tool = SchemaTableTool()
        self.object_storage_tool = ObjectStorageTool()
        self.operator_discovery_tool = OperatorDiscoveryTool()
        self.operator_detail_tool = OperatorDetailTool()
        self.metadata_search_tool = MetadataSearchTool()

        # 初始化各阶段
        print("[WorkflowPipeline] 初始化各阶段 Chains/Agents")

        # 阶段 1: 数据源理解 Agent
        if self.enable_data_source_understanding:
            self.data_source_agent = DataSourceAgent(
                llm=self.llm,
                engine_tool=self.engine_tool,
                schema_table_tool=self.schema_table_tool,
                object_storage_tool=self.object_storage_tool,
                metadata_search_tool=self.metadata_search_tool
            )
        else:
            self.data_source_agent = None

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
        engine_type: str = "python"  # 工作流引擎类型：python, spark, math
    ) -> Dict[str, Any]:
        """
        执行完整的工作流生成流程

        Args:
            query: 用户查询
            tenant_id: 租户 ID
            engine_type: 工作流引擎类型（python/spark/math），用于筛选算子

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
        print(f"  引擎类型: {engine_type}")
        print("=" * 60)

        try:
            # ========== 阶段 1: 数据源理解 ==========
            data_source: Optional[DataSourceContext] = None

            if self.enable_data_source_understanding:
                print("\n[WorkflowPipeline] ▶ 阶段 1: 数据源理解")
                data_source = await self.data_source_agent.understand(query, tenant_id)

                print(f"\n[WorkflowPipeline] ===== 数据源理解结果 =====")
                print(f"  引擎 ID: {data_source.engine_id}")
                print(f"  引擎名称: {data_source.engine_name}")
                print(f"  引擎类型: {data_source.engine_type}")
                print(f"  位置信息: {data_source.location}")
                print(f"  置信度: {data_source.confidence:.2f}")
                print(f"  候选项数量: {len(data_source.alternatives)}")
                print(f"[WorkflowPipeline] ===== 结果结束 =====\n")

                # 如果置信度低，返回候选让用户选择
                if data_source.confidence < 0.8:
                    print(f"[WorkflowPipeline] ⚠️ 数据源置信度低 ({data_source.confidence:.2f})")

                    if data_source.alternatives:
                        return {
                            "status": "need_clarification",
                            "data_source_candidates": data_source.alternatives,
                            "message": f"找到 {len(data_source.alternatives)} 个匹配的数据源，请选择一个"
                        }

                print(f"[WorkflowPipeline] ✅ 数据源理解完成")
                print(f"  引擎: {data_source.engine_name} ({data_source.engine_type})")
                print(f"  位置: {data_source.location}")
                print(f"  置信度: {data_source.confidence:.2f}")

            # ========== 阶段 2: 算子筛选 ==========
            print("\n[WorkflowPipeline] ▶ 阶段 2: 算子筛选")
            data_source_info = self._format_data_source_for_selection(data_source) if data_source else None
            selected_operators = await self.operator_selection_chain.select(
                query, data_source_info, engine_type=engine_type
            )

            print(f"[WorkflowPipeline] ✅ 算子筛选完成：{len(selected_operators)} 个算子")
            print(f"  算子列表: {', '.join(selected_operators)}")

            # ========== 阶段 3: 工作流生成 ==========
            print("\n[WorkflowPipeline] ▶ 阶段 3: 工作流生成")
            workflow = await self.workflow_generation_chain.generate(
                query=query,
                data_source=data_source,
                selected_operators=selected_operators,
                engine_type=engine_type  # 传递引擎类型
            )

            print(f"[WorkflowPipeline] ✅ 工作流生成完成：{len(workflow.tasks)} 个任务")

            # ========== 阶段 4: 工作流验证 + 自动修复 ==========
            print("\n[WorkflowPipeline] ▶ 阶段 4: 工作流验证")
            validation_result = await self.workflow_validation_chain.validate(workflow, engine_type=engine_type)

            if not validation_result.is_valid:
                print(f"[WorkflowPipeline] ⚠️ 验证失败，尝试自动修复")
                print(f"  错误数量: {len(validation_result.errors)}")

                # 自动修复
                workflow, validation_result = await self.workflow_auto_fixer.auto_fix(
                    workflow, validation_result
                )

                # 重新检查验证结果
                if not validation_result.is_valid:
                    print(f"[WorkflowPipeline] ❌ 自动修复失败")
                    # 修复失败，返回错误但仍返回工作流供用户预览
                    return {
                        "status": "validation_failed",
                        "workflow": workflow.dict(),
                        "errors": validation_result.errors,
                        "warnings": validation_result.warnings,
                        "suggestions": validation_result.suggestions,
                        "message": "工作流生成但存在问题，请预览后决定是否使用",
                        "data_source": data_source.dict() if data_source else None,
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
                "explanation": self._generate_explanation(workflow, data_source, selected_operators),
                "data_source": data_source.dict() if data_source else None,
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
        current_workflow: Workflow
    ) -> Dict[str, Any]:
        """
        修改现有工作流（多轮对话）

        Args:
            user_input: 用户的修改请求
            current_workflow: 当前工作流

        Returns:
            修改结果字典
        """
        print("\n" + "=" * 60)
        print(f"[WorkflowPipeline] 修改工作流")
        print(f"  用户输入: {user_input}")
        print("=" * 60)

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
            validation_result = await self.workflow_validation_chain.validate(modified_workflow)

            if not validation_result.is_valid:
                print(f"[WorkflowPipeline] ⚠️ 修改后的工作流验证失败，尝试自动修复")
                modified_workflow, validation_result = await self.workflow_auto_fixer.auto_fix(
                    modified_workflow, validation_result
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

    def _format_data_source_for_selection(self, data_source: Optional[DataSourceContext]) -> Optional[str]:
        """
        格式化数据源信息为文本（用于算子筛选）

        Args:
            data_source: 数据源上下文

        Returns:
            格式化的文本
        """
        if not data_source:
            return None

        parts = [
            f"引擎类型: {data_source.engine_type}",
            f"引擎名称: {data_source.engine_name}"
        ]

        location = data_source.location
        if location.schema and location.table:
            parts.append(f"数据位置: {location.schema}.{location.table}")
        elif location.bucket and location.path:
            parts.append(f"数据位置: {location.bucket}/{location.path}")

        return ", ".join(parts)

    def _generate_explanation(
        self,
        workflow: Workflow,
        data_source: Optional[DataSourceContext],
        selected_operators: list
    ) -> str:
        """
        生成工作流的解释文本

        Args:
            workflow: 生成的工作流
            data_source: 数据源上下文
            selected_operators: 选定的算子列表

        Returns:
            解释文本
        """
        lines = []

        lines.append(f"已生成包含 {len(workflow.tasks)} 个步骤的工作流：")

        # 列出任务
        for i, task in enumerate(workflow.tasks, 1):
            lines.append(f"{i}. {task.operator}")

        # 数据源信息
        if data_source:
            lines.append(f"\n数据源：{data_source.engine_name} ({data_source.engine_type})")

        return "\n".join(lines)


# 便捷函数：创建 WorkflowPipeline 实例
def create_workflow_pipeline(llm, enable_data_source_understanding: bool = True):
    """创建工作流生成流水线实例"""
    return WorkflowPipeline(llm, enable_data_source_understanding)
