"""统一聚合 ADDP Agent 场景契约和定向在线证据的门禁入口。

该入口只读取场景契约和仓库外在线证据，不复制证据内容，不调用 LLM 或 Tool。
"""

import argparse
import json
import os
import subprocess
import sys
import uuid
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from evaluator import EvaluationFailure, evaluate_trace, load_scenario


REPORT_SCHEMA = "addp.agent-evaluation-gate/v1"
REPO_ROOT = Path(__file__).resolve().parents[2]
ONLINE_SCENARIOS = (
    "read-only-query",
    "approval-execution",
    "rejection-and-forbidden",
)
FORBIDDEN_REPORT_FIELDS = {
    "authorization",
    "token",
    "access_token",
    "delegated_access_token",
    "workflow_definition",
    "engine_specific",
    "sample_rows",
}


class GateFailure(ValueError):
    pass


def _utc_now() -> str:
    return datetime.now(timezone.utc).isoformat()


def _external_path(value: str, *, must_exist: bool = False) -> Path:
    path = Path(value).expanduser().resolve()
    if path == REPO_ROOT or REPO_ROOT in path.parents:
        raise GateFailure("门禁报告和在线证据必须位于 ADDP 仓库外")
    if must_exist and not path.is_file():
        raise GateFailure(f"在线证据不存在: {path}")
    return path


def _scan_forbidden(value: Any, location: str = "report") -> list[str]:
    found: list[str] = []
    if isinstance(value, dict):
        for key, child in value.items():
            if str(key).lower() in FORBIDDEN_REPORT_FIELDS:
                found.append(f"{location}.{key}")
            found.extend(_scan_forbidden(child, f"{location}.{key}"))
    elif isinstance(value, list):
        for index, child in enumerate(value):
            found.extend(_scan_forbidden(child, f"{location}[{index}]"))
    return found


def _load_online(path: Path, scenario_name: str, expected_skill: str) -> dict[str, Any]:
    try:
        evidence = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise GateFailure(f"{scenario_name}: 在线证据不是有效 JSON") from exc
    if not isinstance(evidence, dict) or set(evidence) != {
        "schema",
        "scenario",
        "skill",
        "created_at",
        "approval",
        "trace",
    }:
        raise GateFailure(f"{scenario_name}: 在线证据字段不符合 addp.agent-online-evidence/v1")
    if evidence["schema"] != "addp.agent-online-evidence/v1" or evidence["scenario"] != scenario_name:
        raise GateFailure(f"{scenario_name}: 在线证据 Schema 或场景不匹配")
    if evidence["skill"] != expected_skill or not isinstance(evidence["created_at"], str) or not evidence["created_at"]:
        raise GateFailure(f"{scenario_name}: 在线证据 Skill 或创建时间无效")
    if not isinstance(evidence["trace"], dict):
        raise GateFailure(f"{scenario_name}: 在线证据 trace 无效")
    approval = evidence["approval"]
    if scenario_name == "read-only-query":
        if approval is not None:
            raise GateFailure(f"{scenario_name}: 只读证据不得包含 approval")
    elif not isinstance(approval, dict) or set(approval) != {
        "agent_run_id",
        "approval_id",
        "request_fingerprint",
        "open_url",
    } or any(not isinstance(value, str) or not value for value in approval.values()):
        raise GateFailure(f"{scenario_name}: 在线证据 approval 上下文无效")
    leaked = _scan_forbidden(evidence, "evidence")
    if leaked:
        raise GateFailure(f"{scenario_name}: 在线证据包含禁止字段 {leaked}")
    return evidence


def _parse_online(values: list[str]) -> dict[str, Path]:
    result: dict[str, Path] = {}
    for value in values:
        if "=" not in value:
            raise GateFailure(f"--online 必须使用 scenario=path: {value}")
        name, raw_path = value.split("=", 1)
        if name not in ONLINE_SCENARIOS or not raw_path:
            raise GateFailure(f"--online 不支持的场景或路径: {value}")
        if name in result:
            raise GateFailure(f"--online 重复场景: {name}")
        result[name] = _external_path(raw_path, must_exist=True)
    return result


def _run_check(name: str, command: list[str], cwd: Path, env: dict[str, str] | None = None) -> dict[str, Any]:
    try:
        completed = subprocess.run(
            command,
            cwd=cwd,
            env=env,
            capture_output=True,
            text=True,
            timeout=180,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        print(f"{name}: {exc}", file=sys.stderr)
        return {"name": name, "status": "failed", "exit_code": None}
    if completed.returncode != 0:
        print(completed.stdout, file=sys.stderr, end="")
        print(completed.stderr, file=sys.stderr, end="")
    return {
        "name": name,
        "status": "passed" if completed.returncode == 0 else "failed",
        "exit_code": completed.returncode,
    }


def run_offline_checks() -> list[dict[str, Any]]:
    python = REPO_ROOT / "agent" / "backend" / "venv" / "bin" / "python"
    python_path = os.pathsep.join(
        [
            str(REPO_ROOT / "agent" / "backend"),
            str(REPO_ROOT / "common-python"),
            str(Path(__file__).parent),
        ]
    )
    agent_env = dict(os.environ, PYTHONPATH=python_path)
    common_env = dict(agent_env, PYTEST_DISABLE_PLUGIN_AUTOLOAD="1")
    return [
        _run_check(
            "agent_evaluation_and_persistence",
            [
                str(python),
                "-m",
                "unittest",
                "-q",
                "agent.backend.tests.test_agent_evaluation_baseline",
                "agent.backend.tests.test_agent_evaluation_gate",
                "agent.backend.tests.test_agent_online_runner",
                "agent.backend.tests.test_ag_ui_protocol",
                "agent.backend.tests.test_checkpoints",
                "agent.backend.tests.test_messages",
                "agent.backend.tests.test_run_events",
                "agent.backend.tests.test_runs",
            ],
            REPO_ROOT,
            agent_env,
        ),
        _run_check(
            "common_python",
            [str(python), "-m", "pytest", "-p", "pytest_asyncio.plugin", "-q"],
            REPO_ROOT / "common-python",
            common_env,
        ),
        _run_check(
            "agent_frontend",
            ["npm", "test"],
            REPO_ROOT / "agent" / "frontend",
        ),
    ]


def build_report(
    scenario_root: Path,
    online: dict[str, Path] | None = None,
    *,
    require_online: bool = False,
    checks: list[dict[str, Any]] | None = None,
) -> dict[str, Any]:
    unknown_online = sorted(set(online or {}) - set(ONLINE_SCENARIOS))
    if unknown_online:
        raise GateFailure(f"不支持的在线场景: {unknown_online}")
    online = {name: _external_path(str(path), must_exist=True) for name, path in (online or {}).items()}
    checks = checks or []
    scenario_dirs = sorted(path.parent for path in scenario_root.glob("*/scenario.yaml"))
    if not scenario_dirs:
        raise GateFailure(f"没有找到场景契约: {scenario_root}")
    entries: list[dict[str, Any]] = []
    failures: list[str] = []
    for directory in scenario_dirs:
        scenario_name = directory.name
        scenario: dict[str, Any] | None = None
        try:
            scenario = load_scenario(directory)
            offline_status = "passed"
            skill = scenario["skill"]
        except Exception as exc:
            offline_status = "failed"
            skill = None
            failures.append(f"{scenario_name}.offline: {exc}")
        online_status = "not_provided"
        if scenario_name in online and scenario is not None:
            try:
                evidence = _load_online(online[scenario_name], scenario_name, scenario["skill"])
                evaluate_trace(scenario, evidence["trace"])
                online_status = "passed"
            except (GateFailure, EvaluationFailure, KeyError) as exc:
                online_status = "failed"
                failures.append(f"{scenario_name}.online: {exc}")
        elif require_online and scenario_name in ONLINE_SCENARIOS:
            online_status = "missing"
            failures.append(f"{scenario_name}.online: 缺少在线证据")
        entries.append(
            {
                "name": scenario_name,
                "skill": skill,
                "offline": offline_status,
                "online": online_status,
            }
        )
    for check in checks:
        if check.get("status") != "passed":
            failures.append(f"{check.get('name')}: 离线门禁失败")
    report = {
        "schema": REPORT_SCHEMA,
        "created_at": _utc_now(),
        "run_id": str(uuid.uuid4()),
        "mode": "online_required" if require_online else "offline",
        "checks": [
            {
                "name": "scenario_contracts",
                "status": "passed" if all(entry["offline"] == "passed" for entry in entries) else "failed",
                "count": len(entries),
            },
            *checks,
        ],
        "scenarios": entries,
        "status": "failed" if failures else "passed",
        "failures": failures,
    }
    leaked = _scan_forbidden(report)
    if leaked:
        raise GateFailure(f"门禁报告包含禁止字段 {leaked}")
    return report


def _write_report(path: Path, report: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(f".{path.name}.{uuid.uuid4().hex}.tmp")
    temporary.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    temporary.replace(path)


def main() -> int:
    parser = argparse.ArgumentParser(description="ADDP Agent 评测统一门禁")
    parser.add_argument("--scenario-root", default=str(Path(__file__).parent))
    parser.add_argument("--online", action="append", default=[], metavar="SCENARIO=EVIDENCE")
    parser.add_argument("--require-online", action="store_true")
    parser.add_argument("--output", help="仓库外门禁报告路径；省略时输出到 stdout")
    args = parser.parse_args()
    try:
        report = build_report(
            Path(args.scenario_root).resolve(),
            _parse_online(args.online),
            require_online=args.require_online,
            checks=run_offline_checks(),
        )
        if args.output:
            _write_report(_external_path(args.output), report)
        print(json.dumps(report, ensure_ascii=False, separators=(",", ":")))
        return 0 if report["status"] == "passed" else 1
    except (GateFailure, OSError, ValueError) as exc:
        print(json.dumps({"schema": REPORT_SCHEMA, "status": "failed", "error": str(exc)}, ensure_ascii=False))
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
