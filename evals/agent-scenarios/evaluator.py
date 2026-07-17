import argparse
import json
from pathlib import Path
from typing import Any, Iterable

import yaml


SCHEMA = "addp.agent-scenario/v1"
SCENARIO_CATEGORIES = {
    "read_only_query",
    "approval_execution",
    "rejection_forbidden",
    "business_capability",
}
PHASE_STATUSES = {"completed", "waiting", "failed"}


class ScenarioContractError(ValueError):
    pass


class EvaluationFailure(AssertionError):
    def __init__(self, failures: list[str]):
        super().__init__("\n".join(failures))
        self.failures = failures


def _require_exact_keys(value: dict[str, Any], required: set[str], optional: set[str], location: str) -> None:
    missing = sorted(required - value.keys())
    extra = sorted(value.keys() - required - optional)
    if missing or extra:
        details = []
        if missing:
            details.append(f"缺少 {missing}")
        if extra:
            details.append(f"存在未声明字段 {extra}")
        raise ScenarioContractError(f"{location}: {'；'.join(details)}")


def _require_string_list(value: Any, location: str) -> list[str]:
    if not isinstance(value, list) or any(not isinstance(item, str) or not item for item in value):
        raise ScenarioContractError(f"{location}: 必须是字符串数组")
    return value


def load_scenario(path: str | Path) -> dict[str, Any]:
    scenario_path = Path(path)
    if scenario_path.is_dir():
        scenario_path = scenario_path / "scenario.yaml"
    scenario = yaml.safe_load(scenario_path.read_text(encoding="utf-8"))
    if not isinstance(scenario, dict):
        raise ScenarioContractError(f"{scenario_path}: 根节点必须是对象")
    _require_exact_keys(
        scenario,
        {"schema", "name", "category", "skill", "prompt", "expectations"},
        set(),
        str(scenario_path),
    )
    if scenario["schema"] != SCHEMA:
        raise ScenarioContractError(f"{scenario_path}: schema 必须为 {SCHEMA}")
    if scenario["category"] not in SCENARIO_CATEGORIES:
        raise ScenarioContractError(f"{scenario_path}: 未知 category {scenario['category']}")
    if scenario_path.parent.name != scenario["name"]:
        raise ScenarioContractError(f"{scenario_path}: name 必须与目录名一致")
    if not all(isinstance(scenario[key], str) and scenario[key] for key in ("name", "skill", "prompt")):
        raise ScenarioContractError(f"{scenario_path}: name、skill、prompt 必须是非空字符串")
    _validate_expectations(scenario["expectations"], str(scenario_path))
    return scenario


def _validate_expectations(expectations: Any, location: str) -> None:
    if not isinstance(expectations, dict):
        raise ScenarioContractError(f"{location}.expectations: 必须是对象")
    _require_exact_keys(
        expectations,
        {"phases", "forbidden_persisted_fields"},
        {"same_agent_run", "different_agent_run", "conditional_interactions", "forbidden_assumptions"},
        f"{location}.expectations",
    )
    phases = expectations["phases"]
    if not isinstance(phases, dict) or not phases:
        raise ScenarioContractError(f"{location}.expectations.phases: 必须是非空对象")
    for name, phase in phases.items():
        if not isinstance(name, str) or not name or not isinstance(phase, dict):
            raise ScenarioContractError(f"{location}.expectations.phases: phase 必须使用非空名称和对象")
        _require_exact_keys(
            phase,
            {
                "status",
                "required_tools",
                "forbidden_tools",
                "required_errors",
                "interactions",
                "presentations",
                "result_refs",
                "owner_effects",
            },
            {"allowed_tool_risks", "tool_input_fields"},
            f"{location}.expectations.phases.{name}",
        )
        if phase["status"] not in PHASE_STATUSES:
            raise ScenarioContractError(f"{location}.expectations.phases.{name}.status: 未知状态")
        for key in ("required_tools", "forbidden_tools", "required_errors"):
            _require_string_list(phase[key], f"{location}.expectations.phases.{name}.{key}")
        if "allowed_tool_risks" in phase:
            risks = _require_string_list(
                phase["allowed_tool_risks"],
                f"{location}.expectations.phases.{name}.allowed_tool_risks",
            )
            if any(risk not in {"read", "propose", "write"} for risk in risks):
                raise ScenarioContractError(f"{location}.expectations.phases.{name}: 未知 Tool risk")
        tool_input_fields = phase.get("tool_input_fields", {})
        if not isinstance(tool_input_fields, dict):
            raise ScenarioContractError(f"{location}.expectations.phases.{name}.tool_input_fields: 必须是对象")
        for tool_name, fields in tool_input_fields.items():
            if not isinstance(tool_name, str) or not tool_name:
                raise ScenarioContractError(f"{location}.expectations.phases.{name}.tool_input_fields: Tool 名不能为空")
            _require_string_list(fields, f"{location}.expectations.phases.{name}.tool_input_fields.{tool_name}")
        interactions = phase["interactions"]
        if not isinstance(interactions, list):
            raise ScenarioContractError(f"{location}.expectations.phases.{name}.interactions: 必须是数组")
        for index, interaction in enumerate(interactions):
            if not isinstance(interaction, dict):
                raise ScenarioContractError(f"{location}.expectations.phases.{name}.interactions[{index}]: 必须是对象")
            _require_exact_keys(
                interaction,
                {"kind", "owner", "status"},
                set(),
                f"{location}.expectations.phases.{name}.interactions[{index}]",
            )
        presentations = _require_string_list(
            phase["presentations"],
            f"{location}.expectations.phases.{name}.presentations",
        )
        if any(component not in {"WorkflowDag", "MapView", "TablePreview", "ResourcePicker"} for component in presentations):
            raise ScenarioContractError(f"{location}.expectations.phases.{name}.presentations: 未知组件")
        result_refs = phase["result_refs"]
        if not isinstance(result_refs, dict):
            raise ScenarioContractError(f"{location}.expectations.phases.{name}.result_refs: 必须是对象")
        _require_exact_keys(result_refs, {"count", "kinds"}, set(), f"{location}.expectations.phases.{name}.result_refs")
        if not isinstance(result_refs["count"], int) or result_refs["count"] < 0:
            raise ScenarioContractError(f"{location}.expectations.phases.{name}.result_refs.count: 必须是非负整数")
        _require_string_list(result_refs["kinds"], f"{location}.expectations.phases.{name}.result_refs.kinds")
        owner_effects = phase["owner_effects"]
        if not isinstance(owner_effects, dict) or any(
            not isinstance(key, str) or not isinstance(value, int) or value < 0
            for key, value in owner_effects.items()
        ):
            raise ScenarioContractError(f"{location}.expectations.phases.{name}.owner_effects: 必须是非负整数对象")
    phase_names = set(phases)
    for relation in ("same_agent_run", "different_agent_run"):
        pairs = expectations.get(relation, [])
        if not isinstance(pairs, list):
            raise ScenarioContractError(f"{location}.expectations.{relation}: 必须是 phase 对数组")
        for pair in pairs:
            if not isinstance(pair, list) or len(pair) != 2 or any(name not in phase_names for name in pair):
                raise ScenarioContractError(f"{location}.expectations.{relation}: 必须引用两个已声明 phase")
    _require_string_list(expectations["forbidden_persisted_fields"], f"{location}.expectations.forbidden_persisted_fields")
    conditional_interactions = expectations.get("conditional_interactions", [])
    if not isinstance(conditional_interactions, list):
        raise ScenarioContractError(f"{location}.expectations.conditional_interactions: 必须是数组")
    for index, interaction in enumerate(conditional_interactions):
        if not isinstance(interaction, dict):
            raise ScenarioContractError(f"{location}.expectations.conditional_interactions[{index}]: 必须是对象")
        _require_exact_keys(
            interaction,
            {"when", "phase", "kind", "owner", "component"},
            set(),
            f"{location}.expectations.conditional_interactions[{index}]",
        )
        if any(
            not isinstance(interaction[key], str) or not interaction[key]
            for key in ("when", "phase", "kind", "owner", "component")
        ):
            raise ScenarioContractError(f"{location}.expectations.conditional_interactions[{index}]: 字段不能为空")
        if interaction["component"] not in {"ClarificationChoice", "ResourcePicker"}:
            raise ScenarioContractError(
                f"{location}.expectations.conditional_interactions[{index}].component: 未知组件"
            )
        if interaction["phase"] not in phase_names:
            raise ScenarioContractError(
                f"{location}.expectations.conditional_interactions[{index}].phase: 未知 phase"
            )
    _require_string_list(
        expectations.get("forbidden_assumptions", []),
        f"{location}.expectations.forbidden_assumptions",
    )


def load_tool_risks(repo_root: str | Path | None = None) -> dict[str, str]:
    root = Path(repo_root) if repo_root else Path(__file__).resolve().parents[2]
    manifest_path = root / "common-python" / "addp_common" / "tools" / "manifest.json"
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    return {tool["name"]: tool["risk"] for tool in manifest["tools"]}


def phase_from_events(
    name: str,
    agent_run_id: str,
    status: str,
    events: Iterable[Any],
    *,
    owner_effects: dict[str, int],
    persisted_state: Any,
) -> dict[str, Any]:
    tools: list[dict[str, Any]] = []
    tool_indexes: dict[str, int] = {}
    interactions: list[dict[str, str]] = []
    presentations: list[str] = []
    result_refs: list[dict[str, Any]] = []
    for event in events:
        kind = event.kind if hasattr(event, "kind") else event["kind"]
        payload = event.payload if hasattr(event, "payload") else event["payload"]
        if kind == "tool_start":
            tool_indexes[str(payload["tool_call_id"])] = len(tools)
            arguments = payload.get("args")
            tools.append(
                {
                    "name": str(payload["tool_name"]),
                    "status": "started",
                    "error_code": None,
                    "input_fields": sorted(arguments) if isinstance(arguments, dict) else [],
                }
            )
        elif kind == "tool_result":
            index = tool_indexes.get(str(payload["tool_call_id"]))
            if index is not None:
                tools[index]["status"] = "failed" if payload.get("is_error") else "succeeded"
                tools[index]["error_code"] = payload.get("error_code")
        elif kind == "interaction_required":
            interaction_kind = str(payload.get("interaction_kind") or "clarification")
            interactions.append(
                {
                    "kind": interaction_kind,
                    "owner": str(payload.get("owner") or "agent"),
                    "status": "pending",
                }
            )
            reason = str(payload.get("reason") or "")
            candidates = payload.get("candidates") or []
            if (
                interaction_kind == "clarification"
                and reason == "data_source_ambiguous"
                and candidates
                and all(
                    isinstance(candidate.get("value"), str)
                    and candidate["value"].startswith("addp://")
                    and isinstance(candidate.get("candidate"), dict)
                    and candidate["candidate"].get("locator") == candidate["value"]
                    for candidate in candidates
                )
            ):
                presentations.append("ResourcePicker")
            elif interaction_kind == "clarification":
                presentations.append("ClarificationChoice")
        elif kind == "presentation":
            component = {
                "workflow_dag": "WorkflowDag",
                "map_view": "MapView",
                "table_preview": "TablePreview",
            }.get(payload.get("kind"))
            if component:
                presentations.append(component)
        elif kind == "result_ref":
            result_refs.append(payload["result_ref"])
    return {
        "name": name,
        "agent_run_id": agent_run_id,
        "status": status,
        "tools": tools,
        "interactions": interactions,
        "presentations": presentations,
        "result_refs": result_refs,
        "owner_effects": owner_effects,
        "persisted_state": persisted_state,
    }


def _find_forbidden_fields(value: Any, forbidden: set[str], location: str = "persisted_state") -> list[str]:
    found: list[str] = []
    if isinstance(value, dict):
        for key, child in value.items():
            child_location = f"{location}.{key}"
            if str(key).lower() in forbidden:
                found.append(child_location)
            found.extend(_find_forbidden_fields(child, forbidden, child_location))
    elif isinstance(value, list):
        for index, child in enumerate(value):
            found.extend(_find_forbidden_fields(child, forbidden, f"{location}[{index}]"))
    return found


def evaluate_trace(
    scenario: dict[str, Any],
    trace: dict[str, Any],
    *,
    tool_risks: dict[str, str] | None = None,
) -> None:
    failures: list[str] = []
    if trace.get("skill") != scenario["skill"]:
        failures.append(f"skill: 期望 {scenario['skill']}，实际 {trace.get('skill')}")
    actual_phases = {phase.get("name"): phase for phase in trace.get("phases", []) if isinstance(phase, dict)}
    expected_phases = scenario["expectations"]["phases"]
    if set(actual_phases) != set(expected_phases):
        failures.append(f"phases: 期望 {sorted(expected_phases)}，实际 {sorted(actual_phases)}")
    risks = tool_risks or load_tool_risks()
    active_conditions = set(trace.get("conditions", []))
    forbidden_fields = {field.lower() for field in scenario["expectations"]["forbidden_persisted_fields"]}
    for phase_name, expected in expected_phases.items():
        actual = actual_phases.get(phase_name)
        if actual is None:
            continue
        if actual.get("status") != expected["status"]:
            failures.append(f"{phase_name}.status: 期望 {expected['status']}，实际 {actual.get('status')}")
        tools = actual.get("tools", [])
        tool_names = [tool.get("name") for tool in tools]
        for required in expected["required_tools"]:
            if required not in tool_names:
                failures.append(f"{phase_name}.tools: 缺少 {required}")
        for forbidden in expected["forbidden_tools"]:
            if forbidden in tool_names:
                failures.append(f"{phase_name}.tools: 禁止调用 {forbidden}")
        actual_errors = [tool.get("error_code") for tool in tools if tool.get("error_code")]
        if actual_errors != expected["required_errors"]:
            failures.append(f"{phase_name}.errors: 期望 {expected['required_errors']}，实际 {actual_errors}")
        for tool in tools:
            if tool.get("status") == "started":
                failures.append(f"{phase_name}.tools: {tool.get('name')} 缺少结束结果")
        for tool_name, expected_fields in expected.get("tool_input_fields", {}).items():
            matching_tools = [tool for tool in tools if tool.get("name") == tool_name]
            if not matching_tools:
                continue
            for tool in matching_tools:
                if tool.get("input_fields") != sorted(expected_fields):
                    failures.append(
                        f"{phase_name}.tool_input_fields.{tool_name}: "
                        f"期望 {sorted(expected_fields)}，实际 {tool.get('input_fields')}"
                    )
        if "allowed_tool_risks" in expected:
            allowed_risks = set(expected["allowed_tool_risks"])
            for tool_name in tool_names:
                risk = risks.get(str(tool_name))
                if risk is None:
                    failures.append(f"{phase_name}.tools: Manifest 不存在 {tool_name}")
                elif risk not in allowed_risks:
                    failures.append(f"{phase_name}.tools: {tool_name} risk={risk} 不在 {sorted(allowed_risks)}")
        actual_interactions = actual.get("interactions", [])
        if actual_interactions != expected["interactions"]:
            failures.append(f"{phase_name}.interactions: 期望 {expected['interactions']}，实际 {actual_interactions}")
        expected_presentations = [
            *expected["presentations"],
            *[
                conditional["component"]
                for conditional in scenario["expectations"].get("conditional_interactions", [])
                if conditional["when"] in active_conditions and conditional["phase"] == phase_name
            ],
        ]
        if actual.get("presentations", []) != expected_presentations:
            failures.append(
                f"{phase_name}.presentations: 期望 {expected_presentations}，实际 {actual.get('presentations', [])}"
            )
        actual_refs = actual.get("result_refs", [])
        actual_kinds = [ref.get("kind") for ref in actual_refs]
        if len(actual_refs) != expected["result_refs"]["count"]:
            failures.append(
                f"{phase_name}.result_refs.count: 期望 {expected['result_refs']['count']}，实际 {len(actual_refs)}"
            )
        if actual_kinds != expected["result_refs"]["kinds"]:
            failures.append(f"{phase_name}.result_refs.kinds: 期望 {expected['result_refs']['kinds']}，实际 {actual_kinds}")
        if actual.get("owner_effects") != expected["owner_effects"]:
            failures.append(f"{phase_name}.owner_effects: 期望 {expected['owner_effects']}，实际 {actual.get('owner_effects')}")
        leaked = _find_forbidden_fields(actual.get("persisted_state"), forbidden_fields)
        if leaked:
            failures.append(f"{phase_name}.persisted_state: 禁止字段 {leaked}")
    for left, right in scenario["expectations"].get("same_agent_run", []):
        if left in actual_phases and right in actual_phases:
            if actual_phases[left].get("agent_run_id") != actual_phases[right].get("agent_run_id"):
                failures.append(f"agent_run: {left} 与 {right} 必须相同")
    for left, right in scenario["expectations"].get("different_agent_run", []):
        if left in actual_phases and right in actual_phases:
            if actual_phases[left].get("agent_run_id") == actual_phases[right].get("agent_run_id"):
                failures.append(f"agent_run: {left} 与 {right} 必须不同")
    all_interactions = [
        interaction
        for phase in actual_phases.values()
        for interaction in phase.get("interactions", [])
    ]
    for conditional in scenario["expectations"].get("conditional_interactions", []):
        if conditional["when"] not in active_conditions:
            continue
        expected_interaction = {"kind": conditional["kind"], "owner": conditional["owner"]}
        if not any(
            all(interaction.get(key) == value for key, value in expected_interaction.items())
            for interaction in all_interactions
        ):
            failures.append(f"conditional_interactions: {conditional['when']} 必须创建 {expected_interaction}")
        expected_component = conditional["component"]
        if expected_component not in [
            component
            for phase in actual_phases.values()
            for component in phase.get("presentations", [])
        ]:
            failures.append(
                f"conditional_interactions: {conditional['when']} 必须使用 {expected_component}"
            )
    forbidden_assumptions = set(scenario["expectations"].get("forbidden_assumptions", []))
    actual_assumptions = set(trace.get("assumptions", []))
    violated_assumptions = sorted(forbidden_assumptions & actual_assumptions)
    if violated_assumptions:
        failures.append(f"assumptions: 禁止假设 {violated_assumptions}")
    if failures:
        raise EvaluationFailure(failures)


def main() -> int:
    parser = argparse.ArgumentParser(description="验证 ADDP Agent 评测轨迹")
    parser.add_argument("scenario", help="场景目录或 scenario.yaml")
    parser.add_argument("trace", help="归一化轨迹 JSON")
    args = parser.parse_args()
    scenario = load_scenario(args.scenario)
    trace = json.loads(Path(args.trace).read_text(encoding="utf-8"))
    evaluate_trace(scenario, trace)
    print(json.dumps({"scenario": scenario["name"], "status": "passed"}, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
