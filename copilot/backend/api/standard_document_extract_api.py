"""供 Standard 后端调用的文档候选提炼端点。"""

from fastapi import APIRouter, Depends, HTTPException
from sqlalchemy.orm import Session

from addp_common.auth import AuthorizationContext
from authorization_permissions_generated import COPILOT_STANDARD_DOCUMENT_EXECUTE
from database import get_db
from dependencies.auth import require_tenant_service
from models.standard_document_models import (
    StandardDocumentExtractRequest,
    StandardDocumentExtractResponse,
)
from services.inference_service import CopilotInferenceService
from services.standard_document_extraction_service import StandardDocumentExtractionService

router = APIRouter()
require_standard_service = require_tenant_service(
    "addp-standard", COPILOT_STANDARD_DOCUMENT_EXECUTE
)


@router.post(
    "/standard-documents/extract",
    response_model=StandardDocumentExtractResponse,
    summary="提炼数据标准候选 | Extract data standard candidates",
    openapi_extra={
        "x-addp-auth-mode": "permission",
        "x-addp-required-permissions": [COPILOT_STANDARD_DOCUMENT_EXECUTE],
    },
)
async def extract_standard_document(
    request: StandardDocumentExtractRequest,
    service_context: AuthorizationContext = Depends(require_standard_service),
    db: Session = Depends(get_db),
):
    try:
        llm = CopilotInferenceService.chat_model(
            db,
            tenant_id=service_context.tenant_id,
            scenario_code="standard_document_extraction",
            temperature=0.0,
            max_output_tokens=8000,
        )
        return await StandardDocumentExtractionService(llm).run(request)
    except ValueError as error:
        raise HTTPException(status_code=400, detail=str(error)) from error
    except Exception as error:
        raise HTTPException(status_code=502, detail="数据标准候选提炼失败") from error
