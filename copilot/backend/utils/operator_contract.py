from typing import Any, Dict, List


def public_workflow_parameters(operator: Dict[str, Any]) -> List[Dict[str, Any]]:
    operator_name = operator.get("name") or operator.get("id") or "<unknown>"
    if "public_parameters" not in operator:
        raise ValueError(f"算子 {operator_name} 缺少 public_parameters 公开契约")

    parameters = operator["public_parameters"]
    if not isinstance(parameters, list):
        raise ValueError(f"算子 {operator_name} 的 public_parameters 必须是数组")

    result = []
    names = set()
    for parameter in parameters:
        if not isinstance(parameter, dict) or not isinstance(parameter.get("name"), str):
            raise ValueError(f"算子 {operator_name} 的 public_parameters 包含无效参数")
        if parameter.get("param_type") == "ui" or parameter.get("type") == "ui":
            continue

        name = parameter["name"]
        if name in names:
            raise ValueError(f"算子 {operator_name} 的 public_parameters 重复声明参数 {name}")
        names.add(name)
        result.append(parameter)

    return result
