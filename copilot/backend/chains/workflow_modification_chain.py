"""
工作流修改 Chain

支持多轮对话的工作流修改
"""
from pathlib import Path
from typing import Optional

from langchain.chains import ConversationChain
from langchain.memory import ConversationBufferWindowMemory
from langchain.prompts import PromptTemplate
from langchain_core.output_parsers import PydanticOutputParser
from langchain_core.exceptions import OutputParserException

from models.workflow_models import Workflow


class WorkflowModificationChain:
    """
    工作流修改 Chain

    功能：
    1. 使用 ConversationChain 支持多轮对话
    2. 保留最近 5 轮对话历史
    3. 理解用户修改意图，局部更新工作流
    4. 使用 Pydantic 输出解析器确保格式正确
    """

    def __init__(self, llm):
        """
        初始化工作流修改 Chain

        Args:
            llm: LangChain LLM 实例
        """
        self.llm = llm

        # 创建 Memory（保留最近 5 轮对话）
        self.memory = ConversationBufferWindowMemory(
            k=5,  # 保留最近 5 轮
            return_messages=True,
            memory_key="chat_history"
        )

        # 创建输出解析器
        self.output_parser = PydanticOutputParser(pydantic_object=Workflow)

        # 加载 Prompt 模板
        self.prompt_template = self._load_prompt_template()

        # 创建 ConversationChain
        self.chain = ConversationChain(
            llm=self.llm,
            memory=self.memory,
            prompt=self.prompt_template,
            verbose=True
        )

    def _load_prompt_template(self) -> PromptTemplate:
        """加载 Prompt 模板"""
        prompt_file = Path(__file__).parent.parent / "prompts" / "workflow_modification.txt"

        if not prompt_file.exists():
            raise FileNotFoundError(f"Prompt 模板文件不存在: {prompt_file}")

        prompt_content = prompt_file.read_text(encoding="utf-8")

        # 创建 PromptTemplate
        return PromptTemplate(
            template=prompt_content,
            input_variables=["chat_history", "current_workflow", "user_input"],
            partial_variables={
                "format_instructions": self.output_parser.get_format_instructions()
            }
        )

    async def modify(
        self,
        user_input: str,
        current_workflow: Workflow
    ) -> Workflow:
        """
        基于用户反馈修改工作流

        Args:
            user_input: 用户的修改请求
            current_workflow: 当前工作流

        Returns:
            修改后的工作流

        Raises:
            ValueError: 如果修改失败或输出解析失败
        """
        print(f"[WorkflowModificationChain] 开始修改工作流")
        print(f"  用户输入: {user_input}")

        # 准备当前工作流的 JSON 表示
        current_workflow_json = current_workflow.json(indent=2, ensure_ascii=False)

        try:
            # 调用 ConversationChain
            result = await self.chain.ainvoke({
                "current_workflow": current_workflow_json,
                "user_input": user_input
            })

            llm_output = result.get("response", "")
            print(f"[WorkflowModificationChain] LLM 输出长度: {len(llm_output)} 字符")

            # 解析输出
            modified_workflow = self.output_parser.parse(llm_output)

            print(f"[WorkflowModificationChain] ✅ 成功修改工作流：{len(modified_workflow.tasks)} 个任务")
            return modified_workflow

        except OutputParserException as e:
            print(f"[WorkflowModificationChain] ❌ 输出解析失败: {e}")

            # 尝试清理输出
            cleaned_output = self._clean_llm_output(result.get("response", ""))
            if cleaned_output:
                try:
                    modified_workflow = self.output_parser.parse(cleaned_output)
                    print(f"[WorkflowModificationChain] ✅ 清理后解析成功")
                    return modified_workflow
                except Exception as retry_error:
                    print(f"[WorkflowModificationChain] ❌ 清理后仍然解析失败: {retry_error}")

            raise ValueError(f"工作流修改 JSON 解析失败: {e}")

        except Exception as e:
            print(f"[WorkflowModificationChain] ❌ 修改工作流失败: {type(e).__name__}: {e}")
            raise

    def clear_history(self):
        """清空对话历史"""
        self.memory.clear()
        print(f"[WorkflowModificationChain] 对话历史已清空")

    def get_history(self) -> list:
        """
        获取对话历史

        Returns:
            对话历史列表
        """
        return self.memory.buffer

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


# 便捷函数：创建 WorkflowModificationChain 实例
def create_workflow_modification_chain(llm):
    """创建工作流修改 Chain 实例"""
    return WorkflowModificationChain(llm)
