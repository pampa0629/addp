"""
KG 图谱抽取应用服务
处理单个文本 chunk 的实体和关系抽取
注意：文本分块由 Graph 后端（Go）负责，此 pipeline 只处理单个 chunk
"""
import time
from typing import Optional

from models.kg_models import KGExtractRequest, KGExtractResponse
from chains.entity_extraction_chain import EntityExtractionChain
from chains.relation_extraction_chain import RelationExtractionChain


class KGExtractionService:
    """
    KG 构建 Pipeline（单 chunk 抽取）

    职责：
    1. 调用 EntityExtractionChain 抽取实体
    2. 调用 RelationExtractionChain 抽取关系
    3. 返回抽取结果（不做分块，不做去重，由 Graph 后端处理）

    设计原则：
    - 无状态：每次调用独立，不依赖前次调用
    - 无超时风险：单 chunk 约 1000 字符，LLM 处理几秒内完成
    - 易于水平扩展：可以并行处理多个 chunk 请求
    """

    def __init__(self, llm):
        if llm is None:
            raise ValueError("knowledge graph extraction requires an Inference ChatModel")
        self.llm = llm
        self.entity_chain = EntityExtractionChain(llm)
        self.relation_chain = RelationExtractionChain(llm)

    async def run(self, request: KGExtractRequest) -> KGExtractResponse:
        """
        处理单个文本 chunk 的抽取

        Args:
            request: 包含文本、本体定义、文档上下文的请求

        Returns:
            抽取到的实体和关系列表
        """
        start_time = time.time()

        # 1. 实体抽取
        entities = await self.entity_chain.extract(
            text=request.text,
            entity_types=request.ontology.entity_types,
            doc_context=request.doc_context
        )

        # 2. 关系抽取（基于已识别的实体）
        relations = []
        if entities and request.ontology.relation_types:
            relations = await self.relation_chain.extract(
                text=request.text,
                relation_types=request.ontology.relation_types,
                entities=entities,
                doc_context=request.doc_context
            )

        processing_time = time.time() - start_time
        return KGExtractResponse(
            entities=entities,
            relations=relations,
            processing_time=processing_time
        )
