"""Reusable checks for ADDP workflow operator metadata."""

from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any, Iterable, Sequence


REQUIRED_OPERATOR_FIELDS = {
    "id",
    "name",
    "display_name",
    "engine_type",
    "category",
    "category_path",
    "description",
    "execution_modes",
    "parameters",
    "output_ports",
}

ALLOWED_EXECUTION_MODES = {"workflow", "direct"}
ALLOWED_PARAM_TYPES = {"input", "output", "param"}
PUBLIC_RESOURCE_PARAMETER_NAMES = {"locator", "target_parent_locator", "target_name"}


def validate_operator_metadata_contract(
    operators: Iterable[dict[str, Any]],
    *,
    expected_engine_type: str | None = None,
) -> list[str]:
    """Return all addp.workflow/v1 operator metadata contract violations."""

    errors: list[str] = []
    operator_list = list(operators)
    if not operator_list:
        return ["operator list must not be empty"]

    seen_ids: set[str] = set()
    seen_names: set[str] = set()
    for index, operator in enumerate(operator_list):
        label = str(operator.get("name") or operator.get("id") or f"#{index}")
        _validate_operator(operator, label, expected_engine_type, seen_ids, seen_names, errors)

    return errors


def assert_operator_metadata_contract(
    operators: Iterable[dict[str, Any]],
    *,
    expected_engine_type: str | None = None,
) -> None:
    errors = validate_operator_metadata_contract(
        operators,
        expected_engine_type=expected_engine_type,
    )
    assert not errors, "operator metadata contract violations:\n" + "\n".join(f"- {err}" for err in errors)


def _validate_operator(
    operator: dict[str, Any],
    label: str,
    expected_engine_type: str | None,
    seen_ids: set[str],
    seen_names: set[str],
    errors: list[str],
) -> None:
    missing = sorted(field for field in REQUIRED_OPERATOR_FIELDS if field not in operator)
    for field in missing:
        errors.append(f"{label}: missing required field {field}")

    if "module" in operator:
        errors.append(f"{label}: module is not allowed in operator metadata")
    if "outputs" in operator:
        errors.append(f"{label}: outputs is not allowed in operator metadata; use output_ports")

    operator_id = _text(operator.get("id"))
    operator_name = _text(operator.get("name"))
    if operator_id:
        if operator_id in seen_ids:
            errors.append(f"{label}: duplicate id {operator_id}")
        seen_ids.add(operator_id)
    if operator_name:
        if operator_name in seen_names:
            errors.append(f"{label}: duplicate name {operator_name}")
        seen_names.add(operator_name)

    for field in ("id", "name", "display_name", "engine_type", "category", "description"):
        if field in operator and not _text(operator.get(field)):
            errors.append(f"{label}: {field} must be a non-empty string")

    if expected_engine_type and operator.get("engine_type") != expected_engine_type:
        errors.append(
            f"{label}: engine_type={operator.get('engine_type')} does not match {expected_engine_type}"
        )

    category_path = operator.get("category_path")
    if "category_path" in operator:
        if not isinstance(category_path, list) or not category_path:
            errors.append(f"{label}: category_path must be a non-empty array")
        elif any(not _text(item) for item in category_path):
            errors.append(f"{label}: category_path must contain only non-empty strings")

    execution_modes = operator.get("execution_modes")
    if "execution_modes" in operator:
        if not isinstance(execution_modes, list) or not execution_modes:
            errors.append(f"{label}: execution_modes must be a non-empty array")
        else:
            unsupported = sorted(set(execution_modes) - ALLOWED_EXECUTION_MODES)
            if unsupported:
                errors.append(f"{label}: unsupported execution_modes {unsupported}")

    parameters = operator.get("parameters")
    if "parameters" in operator:
        if not isinstance(parameters, list):
            errors.append(f"{label}: parameters must be an array")
        else:
            _validate_parameters(label, parameters, errors)

    output_ports = operator.get("output_ports")
    if "output_ports" in operator:
        if not isinstance(output_ports, list) or not output_ports:
            errors.append(f"{label}: output_ports must be a non-empty array")
        else:
            _validate_output_ports(label, output_ports, errors)


def _validate_parameters(label: str, parameters: list[Any], errors: list[str]) -> None:
    seen_names: set[str] = set()
    for index, parameter in enumerate(parameters):
        prefix = f"{label}: parameters[{index}]"
        if not isinstance(parameter, dict):
            errors.append(f"{prefix} must be an object")
            continue

        for field in ("name", "type", "description"):
            if not _text(parameter.get(field)):
                errors.append(f"{prefix}.{field} must be a non-empty string")
        parameter_name = _text(parameter.get("name"))
        if parameter_name:
            if parameter_name in seen_names:
                errors.append(f"{prefix}: duplicate parameter name {parameter_name}")
            seen_names.add(parameter_name)
        if parameter.get("name") in PUBLIC_RESOURCE_PARAMETER_NAMES:
            errors.append(f"{prefix}: public resource parameter is not allowed in Runtime Operator Spec")
        if parameter.get("ui_type") == "resource_tree_picker":
            errors.append(f"{prefix}: resource_tree_picker is not allowed in Runtime Operator Spec")
        if "required" not in parameter or not isinstance(parameter.get("required"), bool):
            errors.append(f"{prefix}.required must be boolean")
        if "param_type" in parameter:
            param_type = parameter.get("param_type")
            if param_type not in ALLOWED_PARAM_TYPES:
                errors.append(f"{prefix}.param_type must be one of {sorted(ALLOWED_PARAM_TYPES)}")


def _validate_output_ports(label: str, output_ports: list[Any], errors: list[str]) -> None:
    default_count = 0
    for index, output_port in enumerate(output_ports):
        prefix = f"{label}: output_ports[{index}]"
        if not isinstance(output_port, dict):
            errors.append(f"{prefix} must be an object")
            continue

        for field in ("name", "type"):
            if not _text(output_port.get(field)):
                errors.append(f"{prefix}.{field} must be a non-empty string")
        if "is_default" not in output_port or not isinstance(output_port.get("is_default"), bool):
            errors.append(f"{prefix}.is_default must be boolean")
        if output_port.get("is_default") is True:
            default_count += 1

    if len(output_ports) == 1 and default_count != 1:
        errors.append(f"{label}: single output operator must declare its only output port as default")
    if default_count > 1:
        errors.append(f"{label}: output_ports must not declare multiple default ports")


def _text(value: Any) -> str:
    return value.strip() if isinstance(value, str) else ""


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Validate ADDP addp.workflow/v1 operator metadata JSON.",
    )
    parser.add_argument(
        "json_file",
        help="Path to an /api/operators response JSON file or a raw operators array.",
    )
    parser.add_argument(
        "--engine-type",
        help="Expected operator engine_type, such as geopython_workflow or spark_workflow.",
    )
    args = parser.parse_args(argv)

    operators = _load_operators(Path(args.json_file))
    errors = validate_operator_metadata_contract(
        operators,
        expected_engine_type=args.engine_type,
    )
    if errors:
        print("operator metadata contract violations:")
        for error in errors:
            print(f"- {error}")
        return 1

    print(f"operator metadata contract OK: {len(operators)} operators")
    return 0


def _load_operators(path: Path) -> list[dict[str, Any]]:
    with path.open(encoding="utf-8") as f:
        payload = json.load(f)

    if isinstance(payload, list):
        return payload
    if isinstance(payload, dict) and isinstance(payload.get("operators"), list):
        return payload["operators"]
    raise ValueError("JSON must be an operators array or an object with operators array")


if __name__ == "__main__":
    raise SystemExit(main())
