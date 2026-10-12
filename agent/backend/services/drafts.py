"""Read-only references to persisted, formally checked task arguments.

There is no editable draft store, business API, approval or checkpoint reuse.
"""
import copy
import json
import re
from uuid import UUID

from jsonschema import Draft202012Validator
from sqlalchemy import select

from addp_common.tools import get_tool
from agents.platform_conditions import CHECK_TOOL, config_arguments_digest
from models.run import AgentRun
from models.run_step import AgentRunStep
from protocol.a2ui import transfer_config_check_presentation

DRAFT_CATALOG_LIMIT = 8
DRAFT_SNAPSHOT_MAX_BYTES = 128 * 1024
DRAFT_PRESENTATION_MAX_BYTES = 256 * 1024
DRAFT_INSPECT = "draft.inspect"
_REF = re.compile(r"draft:([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}):([0-9a-f]{64})\Z")


class DraftReferenceError(ValueError):
    pass


def draft_snapshot(step) -> dict:
    """Restore exact recorded arguments, never infer them from an owner schema."""
    if step.tool_name != CHECK_TOOL or step.status != "completed":
        raise DraftReferenceError("draft_snapshot_invalid")
    evidence = step.facts.get("config_checks") if isinstance(step.facts, dict) else None
    if not isinstance(evidence, list) or len(evidence) != 1:
        raise DraftReferenceError("draft_snapshot_invalid")
    evidence = evidence[0]
    arguments = step.input_data
    report = evidence.get("report") if isinstance(evidence, dict) else None
    tool = get_tool(CHECK_TOOL)
    if (not isinstance(arguments, dict) or not isinstance(report, dict)
            or list(Draft202012Validator(tool.input_schema).iter_errors(arguments))
            or list(Draft202012Validator(tool.output_schema).iter_errors(report))):
        raise DraftReferenceError("draft_snapshot_invalid")
    digest = config_arguments_digest(arguments)
    if evidence.get("arguments_digest") != digest:
        raise DraftReferenceError("draft_snapshot_invalid")
    snapshot = {
        "reference": f"draft:{UUID(str(step.id))}:{digest}",
        "name": arguments["name"],
        "arguments": copy.deepcopy(arguments),
        "report": transfer_config_check_presentation(report)["report"],
    }
    if len(json.dumps(snapshot, ensure_ascii=False, separators=(",", ":")).encode()) > DRAFT_SNAPSHOT_MAX_BYTES:
        raise DraftReferenceError("draft_snapshot_too_large")
    return snapshot


def draft_presentation(snapshot: dict) -> dict:
    mappings = [
        {key: field.get(key) for key in ("source", "target", "target_type", "nullable", "precision", "scale")}
        for transform in snapshot["arguments"]["config"].get("transforms", [])
        for field in transform["fields"]
    ]
    presentation = {"kind": "transfer_draft_snapshot", "reference": snapshot["reference"], "name": snapshot["name"],
                    "parameters": json.dumps(snapshot["arguments"], ensure_ascii=False, indent=2), "mappings": mappings}
    if (len(presentation["parameters"].encode()) > DRAFT_SNAPSHOT_MAX_BYTES
            or len(json.dumps(presentation, ensure_ascii=False, separators=(",", ":")).encode()) > DRAFT_PRESENTATION_MAX_BYTES):
        raise DraftReferenceError("draft_snapshot_too_large")
    return presentation


def _owned_steps(session_id, user_id, tenant_id):
    return select(AgentRunStep).join(AgentRun, AgentRun.id == AgentRunStep.agent_run_id).where(
        AgentRun.session_id == session_id, AgentRun.user_id == user_id, AgentRun.tenant_id == tenant_id,
        AgentRunStep.tool_name == CHECK_TOOL, AgentRunStep.status == "completed",
    )


async def list_session_drafts(db, *, session_id: int, user_id: int, tenant_id: int) -> list[dict]:
    # A bounded recent window, not a claim to enumerate every historical draft.
    result = await db.execute(_owned_steps(session_id, user_id, tenant_id)
                              .where(AgentRun.status == "completed")
                              .order_by(AgentRunStep.completed_at.desc(), AgentRunStep.id.desc())
                              .limit(DRAFT_CATALOG_LIMIT))
    catalog = []
    for step in result.scalars().all():
        # The directory is not a validation/availability proof. Restore only
        # the selected snapshot, so unrelated historical schema changes do not
        # prevent ordinary chat. Steps without evidence never minted a ref.
        checks = step.facts.get("config_checks", []) if isinstance(step.facts, dict) else []
        if not isinstance(checks, list) or len(checks) != 1 or not isinstance(checks[0], dict):
            continue
        digest = checks[0].get("arguments_digest")
        name = step.input_data.get("name") if isinstance(step.input_data, dict) else None
        report = checks[0].get("report", {})
        if not isinstance(digest, str) or not re.fullmatch(r"[0-9a-f]{64}", digest) or not isinstance(name, str) or not 1 <= len(name) <= 200:
            continue
        observed_at = report.get("observed_at") if isinstance(report, dict) else None
        catalog.append({"reference": f"draft:{step.id}:{digest}", "name": name,
                        "observed_at": observed_at if isinstance(observed_at, str) and len(observed_at) <= 128 else ""})
    return catalog


async def read_checked_draft_step(db, step_id: UUID, *, session_id: int, user_id: int, tenant_id: int) -> dict:
    result = await db.execute(_owned_steps(session_id, user_id, tenant_id).where(AgentRunStep.id == step_id))
    step = result.scalar_one_or_none()
    if step is None:
        raise DraftReferenceError("draft_reference_unavailable")
    return draft_snapshot(step)


async def read_session_draft(db, reference: str, *, session_id: int, user_id: int, tenant_id: int) -> dict:
    match = _REF.fullmatch(reference)
    if not match:
        raise DraftReferenceError("draft_reference_unavailable")
    snapshot = await read_checked_draft_step(db, UUID(match[1]), session_id=session_id, user_id=user_id, tenant_id=tenant_id)
    if snapshot["reference"] != reference:
        raise DraftReferenceError("draft_reference_unavailable")
    return snapshot
