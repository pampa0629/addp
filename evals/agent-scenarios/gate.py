"""统一聚合 ADDP Agent 场景契约和定向在线证据的门禁入口。

该入口只读取场景契约和仓库外在线证据，不复制证据内容，不调用 LLM 或 Tool。
"""

import argparse
import hashlib
import json
import os
import subprocess
import sys
import time
import uuid
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any

from evaluator import EvaluationFailure, evaluate_trace, load_scenario


REPORT_SCHEMA = "addp.agent-evaluation-gate/v2"
COMPARISON_SCHEMA = "addp.agent-evaluation-comparison/v1"
REPO_ROOT = Path(__file__).resolve().parents[2]
ONLINE_EVIDENCE_MAX_AGE = timedelta(hours=24)
ONLINE_EVIDENCE_FUTURE_SKEW = timedelta(minutes=5)
ONLINE_SCENARIOS = (
    "read-only-query",
    "approval-execution",
    "rejection-and-forbidden",
)
FORBIDDEN_EVIDENCE_FIELDS = {
    "authorization",
    "token",
    "access_token",
    "delegated_access_token",
    "workflow_definition",
    "engine_specific",
    "sample_rows",
}
FORBIDDEN_REPORT_FIELDS = FORBIDDEN_EVIDENCE_FIELDS | {
    "agent_run_id",
    "approval",
    "approval_id",
    "open_url",
    "request_fingerprint",
    "trace",
}


class GateFailure(ValueError):
    pass


class OnlineEvidenceStale(GateFailure):
    pass


def _utc_now(now: datetime | None = None) -> str:
    return (now or datetime.now(timezone.utc)).astimezone(timezone.utc).isoformat()


def _sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _is_git_revision(value: Any) -> bool:
    return (
        isinstance(value, str)
        and len(value) == 40
        and all(character in "0123456789abcdef" for character in value.lower())
    )


def _is_sha256(value: Any) -> bool:
    return (
        isinstance(value, str)
        and len(value) == 64
        and all(character in "0123456789abcdef" for character in value.lower())
    )


def _is_nonnegative_int(value: Any) -> bool:
    return isinstance(value, int) and not isinstance(value, bool) and value >= 0


def _source_identity() -> dict[str, Any]:
    try:
        revision = subprocess.run(
            ["git", "rev-parse", "HEAD"],
            cwd=REPO_ROOT,
            capture_output=True,
            text=True,
            timeout=10,
            check=True,
        ).stdout.strip()
        status = subprocess.run(
            ["git", "status", "--porcelain", "--untracked-files=normal"],
            cwd=REPO_ROOT,
            capture_output=True,
            text=True,
            timeout=10,
            check=True,
        ).stdout
    except (OSError, subprocess.SubprocessError) as exc:
        raise GateFailure("无法读取评测源码版本") from exc
    if not _is_git_revision(revision):
        raise GateFailure("评测源码版本不是有效 Git revision")
    return {"revision": revision, "worktree_dirty": bool(status.strip())}


def _external_path(value: str, *, must_exist: bool = False) -> Path:
    path = Path(value).expanduser().resolve()
    if path == REPO_ROOT or REPO_ROOT in path.parents:
        raise GateFailure("门禁报告和在线证据必须位于 ADDP 仓库外")
    if must_exist and not path.is_file():
        raise GateFailure(f"仓库外评测文件不存在: {path}")
    return path


def _scan_forbidden(
    value: Any,
    forbidden_fields: set[str] = FORBIDDEN_REPORT_FIELDS,
    location: str = "report",
) -> list[str]:
    found: list[str] = []
    if isinstance(value, dict):
        for key, child in value.items():
            if str(key).lower() in forbidden_fields:
                found.append(f"{location}.{key}")
            found.extend(_scan_forbidden(child, forbidden_fields, f"{location}.{key}"))
    elif isinstance(value, list):
        for index, child in enumerate(value):
            found.extend(_scan_forbidden(child, forbidden_fields, f"{location}[{index}]"))
    return found


def _parse_evidence_time(value: Any, scenario_name: str, now: datetime) -> datetime:
    if not isinstance(value, str) or not value:
        raise GateFailure(f"{scenario_name}: 在线证据创建时间无效")
    try:
        created_at = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as exc:
        raise GateFailure(f"{scenario_name}: 在线证据创建时间无法解析") from exc
    if created_at.utcoffset() is None:
        raise GateFailure(f"{scenario_name}: 在线证据创建时间必须包含时区")
    created_at = created_at.astimezone(timezone.utc)
    if created_at - now > ONLINE_EVIDENCE_FUTURE_SKEW:
        raise GateFailure(f"{scenario_name}: 在线证据创建时间超出未来时钟偏差")
    if now - created_at > ONLINE_EVIDENCE_MAX_AGE:
        raise OnlineEvidenceStale(f"{scenario_name}: 在线证据已超过 24 小时")
    return created_at


def _load_online(
    path: Path,
    scenario_name: str,
    expected_skill: str,
    now: datetime,
) -> tuple[dict[str, Any], str]:
    try:
        raw_evidence = path.read_bytes()
        evidence = json.loads(raw_evidence.decode("utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as exc:
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
    if evidence["skill"] != expected_skill:
        raise GateFailure(f"{scenario_name}: 在线证据 Skill 无效")
    _parse_evidence_time(evidence["created_at"], scenario_name, now)
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
    leaked = _scan_forbidden(evidence, FORBIDDEN_EVIDENCE_FIELDS, "evidence")
    if leaked:
        raise GateFailure(f"{scenario_name}: 在线证据包含禁止字段 {leaked}")
    return evidence, hashlib.sha256(raw_evidence).hexdigest()


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
    started_at = time.monotonic()
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
        return {
            "name": name,
            "status": "failed",
            "exit_code": None,
            "duration_ms": max(0, round((time.monotonic() - started_at) * 1000)),
        }
    if completed.returncode != 0:
        print(completed.stdout, file=sys.stderr, end="")
        print(completed.stderr, file=sys.stderr, end="")
    return {
        "name": name,
        "status": "passed" if completed.returncode == 0 else "failed",
        "exit_code": completed.returncode,
        "duration_ms": max(0, round((time.monotonic() - started_at) * 1000)),
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
                "agent.backend.tests.test_agent_evaluation_comparison",
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
    now: datetime | None = None,
    source: dict[str, Any] | None = None,
) -> dict[str, Any]:
    unknown_online = sorted(set(online or {}) - set(ONLINE_SCENARIOS))
    if unknown_online:
        raise GateFailure(f"不支持的在线场景: {unknown_online}")
    online = {name: _external_path(str(path), must_exist=True) for name, path in (online or {}).items()}
    checks = checks or []
    now = now or datetime.now(timezone.utc)
    if now.utcoffset() is None:
        raise GateFailure("门禁当前时间必须包含时区")
    now = now.astimezone(timezone.utc)
    if source is None:
        source = _source_identity()
    if (
        not isinstance(source, dict)
        or set(source) != {"revision", "worktree_dirty"}
        or not _is_git_revision(source["revision"])
        or not isinstance(source["worktree_dirty"], bool)
    ):
        raise GateFailure("评测源码身份无效")
    scenario_dirs = sorted(path.parent for path in scenario_root.glob("*/scenario.yaml"))
    if not scenario_dirs:
        raise GateFailure(f"没有找到场景契约: {scenario_root}")
    entries: list[dict[str, Any]] = []
    failures: list[str] = []
    contract_duration_seconds = 0.0
    for directory in scenario_dirs:
        scenario_name = directory.name
        scenario: dict[str, Any] | None = None
        contract_sha256: str | None = None
        contract_started_at = time.monotonic()
        try:
            contract_path = directory / "scenario.yaml"
            contract_sha256 = _sha256_file(contract_path)
            scenario = load_scenario(directory)
            if contract_sha256 != _sha256_file(contract_path):
                raise GateFailure(f"{scenario_name}: 场景契约在评测期间发生变化")
            offline_status = "passed"
            skill = scenario["skill"]
        except Exception as exc:
            offline_status = "failed"
            skill = None
            failures.append(f"{scenario_name}.offline: {exc}")
        finally:
            contract_duration_seconds += time.monotonic() - contract_started_at
        online_status = "not_provided"
        online_evidence: dict[str, str] | None = None
        if scenario_name in online and scenario is not None:
            try:
                evidence, evidence_sha256 = _load_online(
                    online[scenario_name], scenario_name, scenario["skill"], now
                )
                online_evidence = {
                    "created_at": _parse_evidence_time(evidence["created_at"], scenario_name, now).isoformat(),
                    "sha256": evidence_sha256,
                }
                evaluate_trace(scenario, evidence["trace"])
                online_status = "passed"
            except OnlineEvidenceStale as exc:
                online_status = "stale"
                failures.append(f"{scenario_name}.online: {exc}")
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
                "contract_sha256": contract_sha256,
                "offline": offline_status,
                "online": online_status,
                "online_evidence": online_evidence,
            }
        )
    if require_online:
        discovered = {entry["name"] for entry in entries}
        for missing in sorted(set(ONLINE_SCENARIOS) - discovered):
            failures.append(f"{missing}.offline: 缺少黄金场景契约")
    for check in checks:
        if check.get("status") != "passed":
            failures.append(f"{check.get('name')}: 离线门禁失败")
    report = {
        "schema": REPORT_SCHEMA,
        "created_at": _utc_now(now),
        "run_id": str(uuid.uuid4()),
        "mode": "online_required" if require_online else "offline",
        "source": source,
        "checks": [
            {
                "name": "scenario_contracts",
                "status": "passed" if all(entry["offline"] == "passed" for entry in entries) else "failed",
                "count": len(entries),
                "duration_ms": max(0, round(contract_duration_seconds * 1000)),
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
    return _validate_gate_report(report)


def _parse_report_time(value: Any, location: str) -> datetime:
    if not isinstance(value, str) or not value:
        raise GateFailure(f"{location}: 时间无效")
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as exc:
        raise GateFailure(f"{location}: 时间无法解析") from exc
    if parsed.utcoffset() is None:
        raise GateFailure(f"{location}: 时间必须包含时区")
    return parsed.astimezone(timezone.utc)


def _require_exact_keys(value: Any, expected: set[str], location: str) -> dict[str, Any]:
    if not isinstance(value, dict) or set(value) != expected:
        raise GateFailure(f"{location}: 字段不符合 {REPORT_SCHEMA}")
    return value


def _validate_gate_report(report: Any) -> dict[str, Any]:
    report = _require_exact_keys(
        report,
        {
            "schema",
            "created_at",
            "run_id",
            "mode",
            "source",
            "checks",
            "scenarios",
            "status",
            "failures",
        },
        "report",
    )
    if report["schema"] != REPORT_SCHEMA:
        raise GateFailure(f"report: 只支持 {REPORT_SCHEMA}")
    _parse_report_time(report["created_at"], "report.created_at")
    try:
        uuid.UUID(str(report["run_id"]))
    except (ValueError, TypeError, AttributeError) as exc:
        raise GateFailure("report.run_id: UUID 无效") from exc
    if report["mode"] not in {"offline", "online_required"}:
        raise GateFailure("report.mode: 模式无效")
    source = _require_exact_keys(report["source"], {"revision", "worktree_dirty"}, "report.source")
    if not _is_git_revision(source["revision"]) or not isinstance(source["worktree_dirty"], bool):
        raise GateFailure("report.source: 源码身份无效")
    if report["status"] not in {"passed", "failed"}:
        raise GateFailure("report.status: 状态无效")
    if not isinstance(report["failures"], list) or any(
        not isinstance(failure, str) or not failure for failure in report["failures"]
    ):
        raise GateFailure("report.failures: 失败原因无效")

    checks = report["checks"]
    if not isinstance(checks, list) or not checks:
        raise GateFailure("report.checks: 检查项无效")
    check_names: set[str] = set()
    for index, raw_check in enumerate(checks):
        location = f"report.checks[{index}]"
        if not isinstance(raw_check, dict) or not isinstance(raw_check.get("name"), str) or not raw_check["name"]:
            raise GateFailure(f"{location}: 检查项名称无效")
        name = raw_check["name"]
        expected = {"name", "status", "duration_ms", "count"} if name == "scenario_contracts" else {
            "name",
            "status",
            "duration_ms",
            "exit_code",
        }
        check = _require_exact_keys(raw_check, expected, location)
        if name in check_names:
            raise GateFailure(f"{location}: 检查项名称重复")
        check_names.add(name)
        if check["status"] not in {"passed", "failed"} or not _is_nonnegative_int(check["duration_ms"]):
            raise GateFailure(f"{location}: 状态或耗时无效")
        if name == "scenario_contracts":
            if not _is_nonnegative_int(check["count"]):
                raise GateFailure(f"{location}: 场景计数无效")
        elif check["exit_code"] is not None and (
            not isinstance(check["exit_code"], int) or isinstance(check["exit_code"], bool)
        ):
            raise GateFailure(f"{location}: 退出码无效")

    scenarios = report["scenarios"]
    if not isinstance(scenarios, list) or not scenarios:
        raise GateFailure("report.scenarios: 场景无效")
    scenario_names: set[str] = set()
    for index, raw_scenario in enumerate(scenarios):
        location = f"report.scenarios[{index}]"
        scenario = _require_exact_keys(
            raw_scenario,
            {"name", "skill", "contract_sha256", "offline", "online", "online_evidence"},
            location,
        )
        if not isinstance(scenario["name"], str) or not scenario["name"] or scenario["name"] in scenario_names:
            raise GateFailure(f"{location}: 场景名称无效或重复")
        scenario_names.add(scenario["name"])
        if scenario["offline"] not in {"passed", "failed"} or scenario["online"] not in {
            "not_provided",
            "passed",
            "failed",
            "stale",
            "missing",
        }:
            raise GateFailure(f"{location}: 场景状态无效")
        if scenario["offline"] == "passed":
            if not isinstance(scenario["skill"], str) or not scenario["skill"] or not _is_sha256(
                scenario["contract_sha256"]
            ):
                raise GateFailure(f"{location}: Skill 或契约摘要无效")
        else:
            if scenario["skill"] is not None and (
                not isinstance(scenario["skill"], str) or not scenario["skill"]
            ):
                raise GateFailure(f"{location}: Skill 无效")
            if scenario["contract_sha256"] is not None and not _is_sha256(scenario["contract_sha256"]):
                raise GateFailure(f"{location}: 契约摘要无效")
        evidence = scenario["online_evidence"]
        if evidence is not None:
            evidence = _require_exact_keys(evidence, {"created_at", "sha256"}, f"{location}.online_evidence")
            _parse_report_time(evidence["created_at"], f"{location}.online_evidence.created_at")
            if not _is_sha256(evidence["sha256"]):
                raise GateFailure(f"{location}.online_evidence: 摘要无效")
        if scenario["online"] == "passed" and evidence is None:
            raise GateFailure(f"{location}: 已通过在线场景缺少证据审计元数据")
        if scenario["online"] in {"not_provided", "missing", "stale"} and evidence is not None:
            raise GateFailure(f"{location}: 当前在线状态不得包含证据审计元数据")

    scenario_contract_check = next((check for check in checks if check["name"] == "scenario_contracts"), None)
    if scenario_contract_check is None or scenario_contract_check["count"] != len(scenarios):
        raise GateFailure("report.checks: scenario_contracts 与场景数量不一致")
    expected_passed = (
        not report["failures"]
        and all(check["status"] == "passed" for check in checks)
        and all(scenario["offline"] == "passed" for scenario in scenarios)
        and (
            report["mode"] == "offline"
            or (
                set(ONLINE_SCENARIOS).issubset(scenario_names)
                and all(
                    scenario["online"] == "passed"
                    for scenario in scenarios
                    if scenario["name"] in ONLINE_SCENARIOS
                )
            )
        )
    )
    if report["status"] == "failed" and not report["failures"]:
        raise GateFailure("report.failures: failed 报告必须包含失败原因")
    if (report["status"] == "passed") != expected_passed:
        raise GateFailure("report.status: 与检查项、场景或失败原因不一致")
    leaked = _scan_forbidden(report)
    if leaked:
        raise GateFailure(f"report: 包含禁止字段 {leaked}")
    return report


def load_gate_report(path: Path) -> dict[str, Any]:
    external = _external_path(str(path), must_exist=True)
    try:
        report = json.loads(external.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise GateFailure("评测报告不是有效 JSON") from exc
    return _validate_gate_report(report)


def compare_reports(
    baseline: dict[str, Any],
    current: dict[str, Any],
    *,
    now: datetime | None = None,
    require_release_ready: bool = False,
) -> dict[str, Any]:
    baseline = _validate_gate_report(baseline)
    current = _validate_gate_report(current)
    if baseline["mode"] != current["mode"]:
        raise GateFailure("基线与当前评测报告模式必须一致")
    now = now or datetime.now(timezone.utc)
    if now.utcoffset() is None:
        raise GateFailure("比较当前时间必须包含时区")
    now = now.astimezone(timezone.utc)

    regressions: list[str] = []
    baseline_scenarios = {scenario["name"]: scenario for scenario in baseline["scenarios"]}
    current_scenarios = {scenario["name"]: scenario for scenario in current["scenarios"]}
    added_scenarios = sorted(set(current_scenarios) - set(baseline_scenarios))
    removed_scenarios = sorted(set(baseline_scenarios) - set(current_scenarios))
    changed_contracts: list[str] = []
    scenario_comparisons: list[dict[str, Any]] = []
    for name in sorted(set(baseline_scenarios) | set(current_scenarios)):
        before = baseline_scenarios.get(name)
        after = current_scenarios.get(name)
        if before is None or after is None:
            contract_changed = None
            evidence_changed = None
        else:
            contract_changed = before["contract_sha256"] != after["contract_sha256"]
            evidence_changed = before["online_evidence"] != after["online_evidence"]
            if contract_changed:
                changed_contracts.append(name)
        if before is not None and after is None:
            regressions.append(f"scenario.{name}: removed")
        elif before is not None and after is not None:
            if before["offline"] == "passed" and after["offline"] != "passed":
                regressions.append(f"scenario.{name}.offline: passed -> {after['offline']}")
            if (
                baseline["mode"] == "online_required"
                and before["online"] == "passed"
                and after["online"] != "passed"
            ):
                regressions.append(f"scenario.{name}.online: passed -> {after['online']}")
        scenario_comparisons.append(
            {
                "name": name,
                "baseline_present": before is not None,
                "current_present": after is not None,
                "contract_changed": contract_changed,
                "offline": {
                    "baseline": before["offline"] if before is not None else None,
                    "current": after["offline"] if after is not None else None,
                },
                "online": {
                    "baseline": before["online"] if before is not None else None,
                    "current": after["online"] if after is not None else None,
                },
                "evidence_changed": evidence_changed,
            }
        )

    baseline_checks = {check["name"]: check for check in baseline["checks"]}
    current_checks = {check["name"]: check for check in current["checks"]}
    added_checks = sorted(set(current_checks) - set(baseline_checks))
    removed_checks = sorted(set(baseline_checks) - set(current_checks))
    check_comparisons: list[dict[str, Any]] = []
    for name in sorted(set(baseline_checks) | set(current_checks)):
        before = baseline_checks.get(name)
        after = current_checks.get(name)
        if before is not None and after is None:
            regressions.append(f"check.{name}: removed")
        elif before is not None and after is not None and before["status"] == "passed" and after["status"] != "passed":
            regressions.append(f"check.{name}: passed -> {after['status']}")
        baseline_duration = before["duration_ms"] if before is not None else None
        current_duration = after["duration_ms"] if after is not None else None
        check_comparisons.append(
            {
                "name": name,
                "baseline_present": before is not None,
                "current_present": after is not None,
                "status": {
                    "baseline": before["status"] if before is not None else None,
                    "current": after["status"] if after is not None else None,
                },
                "duration_ms": {
                    "baseline": baseline_duration,
                    "current": current_duration,
                    "delta": (
                        current_duration - baseline_duration
                        if baseline_duration is not None and current_duration is not None
                        else None
                    ),
                },
            }
        )
    if current["status"] == "failed":
        regressions.append("report.current: failed")
    regressions = sorted(set(regressions))
    release_blockers: list[str] = []
    if current["mode"] != "online_required":
        release_blockers.append("mode_not_online_required")
    if baseline["status"] != "passed":
        release_blockers.append("baseline_status_not_passed")
    if current["status"] != "passed":
        release_blockers.append("current_status_not_passed")
    if baseline["source"]["worktree_dirty"]:
        release_blockers.append("baseline_worktree_dirty")
    if current["source"]["worktree_dirty"]:
        release_blockers.append("current_worktree_dirty")
    if regressions:
        release_blockers.append("comparison_regressed")
    release_blockers = sorted(set(release_blockers))
    if regressions:
        comparison_status = "regressed"
    elif require_release_ready and release_blockers:
        comparison_status = "blocked"
    else:
        comparison_status = "passed"
    comparison = {
        "schema": COMPARISON_SCHEMA,
        "created_at": _utc_now(now),
        "comparison_id": str(uuid.uuid4()),
        "mode": current["mode"],
        "policy": "release" if require_release_ready else "comparison",
        "baseline": {
            "run_id": baseline["run_id"],
            "created_at": baseline["created_at"],
            "revision": baseline["source"]["revision"],
            "worktree_dirty": baseline["source"]["worktree_dirty"],
        },
        "current": {
            "run_id": current["run_id"],
            "created_at": current["created_at"],
            "revision": current["source"]["revision"],
            "worktree_dirty": current["source"]["worktree_dirty"],
        },
        "summary": {
            "added_scenarios": added_scenarios,
            "removed_scenarios": removed_scenarios,
            "changed_contracts": sorted(changed_contracts),
            "added_checks": added_checks,
            "removed_checks": removed_checks,
        },
        "scenarios": scenario_comparisons,
        "checks": check_comparisons,
        "release_readiness": {
            "eligible": not release_blockers,
            "blockers": release_blockers,
        },
        "status": comparison_status,
        "regressions": regressions,
    }
    leaked = _scan_forbidden(comparison)
    if leaked:
        raise GateFailure(f"比较报告包含禁止字段 {leaked}")
    return comparison


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
    parser.add_argument("--compare", nargs=2, metavar=("BASELINE", "CURRENT"))
    parser.add_argument("--require-release-ready", action="store_true")
    parser.add_argument("--output", help="仓库外评测输出路径；省略时只输出到 stdout")
    args = parser.parse_args()
    output_schema = COMPARISON_SCHEMA if args.compare else REPORT_SCHEMA
    try:
        if args.compare:
            if args.online or args.require_online:
                raise GateFailure("--compare 不能与 --online 或 --require-online 同时使用")
            report = compare_reports(
                load_gate_report(Path(args.compare[0])),
                load_gate_report(Path(args.compare[1])),
                require_release_ready=args.require_release_ready,
            )
        else:
            if args.require_release_ready:
                raise GateFailure("--require-release-ready 只能与 --compare 同时使用")
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
        print(json.dumps({"schema": output_schema, "status": "failed", "error": str(exc)}, ensure_ascii=False))
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
