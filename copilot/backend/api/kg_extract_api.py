"""
KG 图谱构建 API
提供单 chunk 实体/关系抽取端点，供 Graph 后端调用
"""
from fastapi import APIRouter, Depends, HTTPException

from dependencies.auth import require_internal_api_key
from models.kg_models import KGExtractRequest, KGExtractResponse
from pipelines.kg_build_pipeline import KGBuildPipeline

router = APIRouter()


@router.post(
    "/kg-build/extract",
    response_model=KGExtractResponse,
    summary="抽取实体关系 | Extract From Chunk",
    dependencies=[Depends(require_internal_api_key)],
    openapi_extra={"x-addp-auth-mode": "internal"},
)
async def extract_from_chunk(request: KGExtractRequest):
    """
    从单个文本 chunk 抽取实体和关系

    - 文本分块由 Graph 后端负责（见 graph/backend/internal/service/build_service.go）
    - 此端点只处理单个 chunk（约 1000 字符）
    - 抽取结果的置信度分类和写入 Neo4j 由 Graph 后端负责
    """
    if not request.text or not request.text.strip():
        raise HTTPException(status_code=400, detail="text 不能为空")

    if not request.ontology.entity_types:
        raise HTTPException(status_code=400, detail="ontology.entity_types 不能为空")

    try:
        pipeline = KGBuildPipeline()
        return await pipeline.run(request)
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"抽取失败: {str(e)}")
