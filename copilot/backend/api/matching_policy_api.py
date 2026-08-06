from addp_common.auth import AuthorizationContext
from fastapi import APIRouter, Depends, HTTPException, status
from pydantic import BaseModel, ConfigDict, Field
from sqlalchemy import select, update
from sqlalchemy.orm import Session

from authorization_permissions_generated import COPILOT_CONFIGURATION_READ, COPILOT_CONFIGURATION_UPDATE
from database import get_db
from dependencies.auth import require_permissions
from models.matching_policy import MatchingPolicy
from services.metadata_matcher import metadata_matcher

router = APIRouter()


class MatchingPolicyUpdate(BaseModel):
    model_config = ConfigDict(extra="forbid")
    version: int = Field(ge=0)
    score_threshold: float = Field(gt=0, le=1)
    max_candidates: int = Field(ge=1, le=100)


class MatchingPolicyResponse(BaseModel):
    scope_type: str
    tenant_id: int | None = None
    score_threshold: float
    max_candidates: int
    version: int
    inherited: bool = False


def _scope(context: AuthorizationContext):
    if context.context_type == "platform": return "platform", None
    if context.context_type == "tenant" and context.tenant_id is not None: return "tenant", context.tenant_id
    raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="Unsupported authorization context")


def _default(scope: str, tenant_id: int | None, inherited: bool = False):
    return MatchingPolicyResponse(scope_type=scope, tenant_id=tenant_id, score_threshold=0.15, max_candidates=10, version=0, inherited=inherited)


@router.get("/settings/matching-policy", response_model=MatchingPolicyResponse, openapi_extra={"x-addp-auth-mode": "permission", "x-addp-required-permissions": [COPILOT_CONFIGURATION_READ]})
def get_matching_policy(context: AuthorizationContext = Depends(require_permissions(COPILOT_CONFIGURATION_READ)), db: Session = Depends(get_db)):
    scope, tenant_id = _scope(context)
    row = db.scalar(select(MatchingPolicy).where(MatchingPolicy.scope_type == scope, MatchingPolicy.tenant_id == tenant_id))
    inherited = False
    if row is None and scope == "tenant":
        row = db.scalar(select(MatchingPolicy).where(MatchingPolicy.scope_type == "platform", MatchingPolicy.tenant_id.is_(None)))
        inherited = row is not None
    if row is None: return _default(scope, tenant_id, scope == "tenant")
    return MatchingPolicyResponse(scope_type=scope, tenant_id=tenant_id, score_threshold=float(row.score_threshold), max_candidates=row.max_candidates, version=row.version, inherited=inherited)


@router.put("/settings/matching-policy", response_model=MatchingPolicyResponse, openapi_extra={"x-addp-auth-mode": "permission", "x-addp-required-permissions": [COPILOT_CONFIGURATION_UPDATE]})
def update_matching_policy(request: MatchingPolicyUpdate, context: AuthorizationContext = Depends(require_permissions(COPILOT_CONFIGURATION_UPDATE)), db: Session = Depends(get_db)):
    scope, tenant_id = _scope(context)
    row = db.scalar(select(MatchingPolicy).where(MatchingPolicy.scope_type == scope, MatchingPolicy.tenant_id == tenant_id))
    if row is None:
        if request.version != 0: raise HTTPException(status_code=409, detail="matching_policy_version_conflict")
        row = MatchingPolicy(scope_type=scope, tenant_id=tenant_id, score_threshold=request.score_threshold, max_candidates=request.max_candidates, version=1, updated_by=context.principal_id)
        db.add(row)
    else:
        if row.version != request.version: raise HTTPException(status_code=409, detail="matching_policy_version_conflict")
        row.score_threshold, row.max_candidates, row.version, row.updated_by = request.score_threshold, request.max_candidates, row.version + 1, context.principal_id
    db.commit(); db.refresh(row)
    metadata_matcher.update_policy(float(row.score_threshold), row.max_candidates, tenant_id)
    return MatchingPolicyResponse(scope_type=scope, tenant_id=tenant_id, score_threshold=float(row.score_threshold), max_candidates=row.max_candidates, version=row.version)
