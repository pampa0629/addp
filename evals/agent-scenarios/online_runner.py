import argparse
import asyncio
import json
import os
import sys
import uuid
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


REPO_ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(REPO_ROOT / "common-python"))
sys.path.insert(0, str(REPO_ROOT / "agent" / "backend"))

from addp_common.oauth import OAuthLoginError, refresh_access_token
from addp_common.client import DevelopClient
from addp_common.tools import ToolExecutionError, ToolExecutor
from agents.events import AgentEvent
from agents.result_refs import build_result_ref
from evaluator import EvaluationFailure, evaluate_trace, load_scenario, phase_from_events
from protocol.a2ui import preview_presentations


EVIDENCE_SCHEMA = "addp.agent-online-evidence/v1"
SKILL_NAME = "workflow-analysis"
FORBIDDEN_EVIDENCE_FIELDS = {
    "authorization",
    "token",
    "access_token",
    "delegated_access_token",
    "workflow_definition",
    "engine_specific",
    "sample_rows",
}


class OnlineEvaluationError(ValueError):
    def __init__(self, code: str, message: str):
        super().__init__(message)
        self.code = code


def _utc_now() -> str:
    return datetime.now(timezone.utc).isoformat()


def _require_external_path(value: str, *, must_exist: bool = False) -> Path:
    path = Path(value).expanduser().resolve()
    if path == REPO_ROOT or REPO_ROOT in path.parents:
        raise ValueError("在线评测输入和证据必须位于 ADDP 仓库外")
    if must_exist and not path.is_file():
        raise ValueError(f"文件不存在: {path}")
    return path


def _write_evidence(path: Path, evidence: dict[str, Any]) -> None:
    _validate_evidence_content(evidence)
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(f".{path.name}.{uuid.uuid4().hex}.tmp")
    temporary.write_text(
        json.dumps(evidence, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )
    temporary.replace(path)


def _load_evidence(path: Path, expected_scenario: str) -> dict[str, Any]:
    evidence = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(evidence, dict) or set(evidence) != {
        "schema",
        "scenario",
        "skill",
        "created_at",
        "approval",
        "trace",
    }:
        raise ValueError("在线评测证据字段不符合唯一契约")
    if evidence["schema"] != EVIDENCE_SCHEMA or evidence["scenario"] != expected_scenario:
        raise ValueError("在线评测证据 Schema 或场景不匹配")
    if evidence["skill"] != SKILL_NAME or not isinstance(evidence["trace"], dict):
        raise ValueError("在线评测证据 Skill 或 trace 无效")
    _validate_evidence_content(evidence)
    return evidence


def _validate_evidence_content(value: Any, location: str = "evidence") -> None:
    if isinstance(value, dict):
        for key, child in value.items():
            if str(key).lower() in FORBIDDEN_EVIDENCE_FIELDS:
                raise ValueError(f"在线评测证据禁止字段: {location}.{key}")
            _validate_evidence_content(child, f"{location}.{key}")
    elif isinstance(value, list):
        for index, child in enumerate(value):
            _validate_evidence_content(child, f"{location}[{index}]")


def _evidence(scenario: str, trace: dict[str, Any], approval: dict[str, Any] | None = None) -> dict[str, Any]:
    return {
        "schema": EVIDENCE_SCHEMA,
        "scenario": scenario,
        "skill": SKILL_NAME,
        "created_at": _utc_now(),
        "approval": approval,
        "trace": trace,
    }


def _walk_objects(value: Any):
    if isinstance(value, dict):
        yield value
        for child in value.values():
            yield from _walk_objects(child)
    elif isinstance(value, list):
        for child in value:
            yield from _walk_objects(child)


def _observed_locators(value: Any) -> set[str]:
    locators: set[str] = set()
    for candidate in _walk_objects(value):
        locator = candidate.get("locator")
        if isinstance(locator, str) and locator.startswith("addp://"):
            locators.add(locator)
        location = candidate.get("location")
        if isinstance(location, dict):
            locator = location.get("locator")
            if isinstance(locator, str) and locator.startswith("addp://"):
                locators.add(locator)
    return locators


async def _call_tool(
    executor: ToolExecutor,
    *,
    name: str,
    arguments: dict[str, Any],
    agent_run_id: str,
    tool_call_id: str,
) -> tuple[Any | None, list[AgentEvent]]:
    events = [
        AgentEvent(
            kind="tool_start",
            payload={"tool_call_id": tool_call_id, "tool_name": name, "args": arguments},
        )
    ]
    try:
        result = await executor.call(
            name,
            arguments,
            agent_run_id=agent_run_id,
            tool_call_id=tool_call_id,
        )
    except ToolExecutionError as exc:
        events.append(
            AgentEvent(
                kind="tool_result",
                payload={
                    "tool_call_id": tool_call_id,
                    "tool_name": name,
                    "is_error": True,
                    "error_code": exc.code,
                },
            )
        )
        return None, events
    events.append(
        AgentEvent(
            kind="tool_result",
            payload={"tool_call_id": tool_call_id, "tool_name": name, "is_error": False},
        )
    )
    return result, events


def _require_tool_result(name: str, result: Any | None, events: list[AgentEvent]) -> Any:
    if result is not None:
        return result
    error_code = next(
        (
            str(event.payload.get("error_code"))
            for event in reversed(events)
            if event.kind == "tool_result" and event.payload.get("error_code")
        ),
        "online_tool_failed",
    )
    raise OnlineEvaluationError(error_code, f"{name} 在线调用失败")


async def _source_token(base_url: str) -> str:
    try:
        return await refresh_access_token(base_url)
    except OAuthLoginError as exc:
        raise ValueError("现有 OAuth 登录无法刷新，请先执行 addp auth login") from exc


async def _read_only(args: argparse.Namespace, executor: ToolExecutor) -> tuple[dict[str, Any], str]:
    scenario = load_scenario(Path(__file__).parent / "read-only-query")
    agent_run_id = str(uuid.uuid4())
    search_result, search_events = await _call_tool(
        executor,
        name="data.search",
        arguments={"query": args.query, "limit": args.limit},
        agent_run_id=agent_run_id,
        tool_call_id=str(uuid.uuid4()),
    )
    search_result = _require_tool_result("data.search", search_result, search_events)
    if args.locator not in _observed_locators(search_result):
        raise ValueError("显式 locator 未出现在本次 data.search 结果中")

    preview_result, preview_events = await _call_tool(
        executor,
        name="data.preview",
        arguments={"locator": args.locator, "limit": args.limit},
        agent_run_id=agent_run_id,
        tool_call_id=str(uuid.uuid4()),
    )
    preview_result = _require_tool_result("data.preview", preview_result, preview_events)
    presentation_events = [
        AgentEvent(kind="presentation", payload=presentation)
        for presentation in preview_presentations(preview_result)
    ]
    phase = phase_from_events(
        "query",
        agent_run_id,
        "completed",
        [*search_events, *preview_events, *presentation_events],
        owner_effects={"approvals_created": 0, "executions_created": 0},
        persisted_state={},
    )
    trace = {"skill": SKILL_NAME, "phases": [phase]}
    evaluate_trace(scenario, trace)
    return _evidence(scenario["name"], trace), "passed"


def _load_workflow(path: Path) -> dict[str, Any]:
    workflow = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(workflow, dict):
        raise ValueError("workflow 文件根节点必须是对象")
    return workflow


async def _approval_request(args: argparse.Namespace, executor: ToolExecutor) -> tuple[dict[str, Any], str]:
    scenario = load_scenario(Path(__file__).parent / args.scenario)
    agent_run_id = str(uuid.uuid4())
    result, events = await _call_tool(
        executor,
        name="workflow.run",
        arguments={
            "workflow_engine_id": args.workflow_engine_id,
            "workflow_definition": _load_workflow(_require_external_path(args.workflow_file, must_exist=True)),
        },
        agent_run_id=agent_run_id,
        tool_call_id=str(uuid.uuid4()),
    )
    result = _require_tool_result("workflow.run", result, events)
    if not isinstance(result, dict) or result.get("status") != "approval_required":
        raise ValueError("workflow.run 未返回 approval_required")
    approval = {
        "agent_run_id": agent_run_id,
        "approval_id": result["interaction_id"],
        "request_fingerprint": result["request_fingerprint"],
        "open_url": result["open_url"],
    }
    trace: dict[str, Any] = {"skill": SKILL_NAME, "phases": []}
    if args.scenario == "approval-execution":
        events.append(
            AgentEvent(
                kind="interaction_required",
                payload={"interaction_kind": "owner_approval", "owner": "develop"},
            )
        )
        trace["phases"].append(
            phase_from_events(
                "request",
                agent_run_id,
                "waiting",
                events,
                owner_effects={"approvals_created": 1, "executions_created": 0},
                persisted_state={
                    "interaction": {
                        "owner_interaction_id": approval["approval_id"],
                        "request_fingerprint": approval["request_fingerprint"],
                    }
                },
            )
        )
    return _evidence(scenario["name"], trace, approval), "awaiting_owner"


def _approval_context(evidence: dict[str, Any]) -> dict[str, str]:
    approval = evidence.get("approval")
    if not isinstance(approval, dict) or set(approval) != {
        "agent_run_id",
        "approval_id",
        "request_fingerprint",
        "open_url",
    }:
        raise ValueError("在线证据缺少 approval 恢复上下文")
    if any(not isinstance(value, str) or not value for value in approval.values()):
        raise ValueError("在线证据 approval 恢复上下文无效")
    return approval


async def _owner_approval_status(executor: ToolExecutor, approval_id: str) -> str:
    try:
        async with DevelopClient(base_url=executor.base_url, user_token=executor.source_token) as client:
            approval = await client.get_tool_approval(approval_id)
    except Exception as exc:
        raise OnlineEvaluationError("approval_status_unavailable", "无法读取 Develop 审批状态") from exc
    status = approval.get("status") if isinstance(approval, dict) else None
    if not isinstance(status, str) or not status:
        raise OnlineEvaluationError("invalid_owner_response", "Develop 审批状态响应无效")
    return status


async def _approval_resume(args: argparse.Namespace, executor: ToolExecutor) -> tuple[dict[str, Any], str]:
    scenario = load_scenario(Path(__file__).parent / "approval-execution")
    evidence = _load_evidence(_require_external_path(args.evidence, must_exist=True), scenario["name"])
    approval = _approval_context(evidence)
    result, events = await _call_tool(
        executor,
        name="workflow.run",
        arguments={
            "approval_id": approval["approval_id"],
            "request_fingerprint": approval["request_fingerprint"],
        },
        agent_run_id=approval["agent_run_id"],
        tool_call_id=str(uuid.uuid4()),
    )
    result = _require_tool_result("workflow.run", result, events)
    if not isinstance(result, dict):
        raise ValueError("批准恢复未创建 execution")
    result_ref = build_result_ref("workflow.run", result)
    if result_ref is not None:
        events.append(AgentEvent(kind="result_ref", payload={"result_ref": result_ref}))
    phase = phase_from_events(
        "resume",
        approval["agent_run_id"],
        "completed",
        events,
        owner_effects={"approvals_created": 0, "executions_created": 1},
        persisted_state={
            "step": {
                "approval_id": approval["approval_id"],
                "request_fingerprint": approval["request_fingerprint"],
            }
        },
    )
    trace = json.loads(json.dumps(evidence["trace"], ensure_ascii=False))
    trace["phases"].append(phase)
    evaluate_trace(scenario, trace)
    return _evidence(scenario["name"], trace, approval), "passed"


async def _approval_rejection(args: argparse.Namespace, executor: ToolExecutor) -> tuple[dict[str, Any], str]:
    scenario = load_scenario(Path(__file__).parent / "rejection-and-forbidden")
    evidence = _load_evidence(_require_external_path(args.evidence, must_exist=True), scenario["name"])
    approval = _approval_context(evidence)
    approval_status = await _owner_approval_status(executor, approval["approval_id"])
    if approval_status != "rejected":
        raise OnlineEvaluationError(
            "approval_not_rejected",
            f"拒绝评测要求 Owner 状态为 rejected，当前为 {approval_status}",
        )
    arguments = {
        "approval_id": approval["approval_id"],
        "request_fingerprint": approval["request_fingerprint"],
    }
    _result, rejected_events = await _call_tool(
        executor,
        name="workflow.run",
        arguments=arguments,
        agent_run_id=approval["agent_run_id"],
        tool_call_id=str(uuid.uuid4()),
    )
    replay_run_id = str(uuid.uuid4())
    _result, replay_events = await _call_tool(
        executor,
        name="workflow.run",
        arguments=arguments,
        agent_run_id=replay_run_id,
        tool_call_id=str(uuid.uuid4()),
    )
    phases = [
        phase_from_events(
            "rejected",
            approval["agent_run_id"],
            "completed",
            rejected_events,
            owner_effects={"approvals_created": 0, "executions_created": 0},
            persisted_state={"step": {"error_code": "approval_rejected"}},
        ),
        phase_from_events(
            "replay",
            replay_run_id,
            "completed",
            replay_events,
            owner_effects={"approvals_created": 0, "executions_created": 0},
            persisted_state={"step": {"error_code": "approval_forbidden"}},
        ),
    ]
    trace = {"skill": SKILL_NAME, "phases": phases}
    evaluate_trace(scenario, trace)
    return _evidence(scenario["name"], trace, approval), "passed"


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="ADDP Agent 定向在线评测")
    parser.add_argument("--base-url", default=os.getenv("ADDP_BASE_URL", "http://localhost:8000"))
    parser.add_argument("--output", required=True, help="仓库外在线证据 JSON 路径")
    commands = parser.add_subparsers(dest="command", required=True)

    read_only = commands.add_parser("read-only")
    read_only.add_argument("--query", required=True)
    read_only.add_argument("--locator", required=True)
    read_only.add_argument("--limit", type=int, choices=range(1, 101), default=10)

    request = commands.add_parser("approval-request")
    request.add_argument("--scenario", choices=["approval-execution", "rejection-and-forbidden"], required=True)
    request.add_argument("--workflow-file", required=True)
    request.add_argument("--workflow-engine-id", type=int, required=True)

    resume = commands.add_parser("approval-resume")
    resume.add_argument("--evidence", required=True)

    rejection = commands.add_parser("approval-rejection")
    rejection.add_argument("--evidence", required=True)
    return parser


async def _run(args: argparse.Namespace) -> dict[str, Any]:
    output = _require_external_path(args.output)
    token = await _source_token(args.base_url)
    executor = ToolExecutor(args.base_url, token)
    handlers = {
        "read-only": _read_only,
        "approval-request": _approval_request,
        "approval-resume": _approval_resume,
        "approval-rejection": _approval_rejection,
    }
    evidence, status = await handlers[args.command](args, executor)
    _write_evidence(output, evidence)
    return {"schema": EVIDENCE_SCHEMA, "scenario": evidence["scenario"], "status": status, "output": str(output)}


def main() -> int:
    try:
        result = asyncio.run(_run(_parser().parse_args()))
    except OnlineEvaluationError as exc:
        print(json.dumps({"error": {"code": exc.code, "message": str(exc)}}, ensure_ascii=False))
        return 1
    except (EvaluationFailure, OSError, ValueError, json.JSONDecodeError) as exc:
        print(json.dumps({"error": {"code": "online_evaluation_failed", "message": str(exc)}}, ensure_ascii=False))
        return 1
    print(json.dumps(result, ensure_ascii=False, separators=(",", ":")))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
