"""Finite Agent condition adapters; semantic declarations remain Ontology-owned.

This validates a reviewed draft, not engine query execution or IAM authority.
No platform definitions are loaded from files at runtime.
"""

import copy
import hashlib
import json
from typing import Any

from jsonschema import Draft202012Validator

from addp_common.tools import get_tool


REVIEW_REASON = "transfer_create_review"
CREATE_TOOL = "transfer.task.create"


def _fail(condition: str) -> None:
    raise ValueError(f"platform_condition_unsatisfied:{condition}")


def _json(value: Any) -> str:
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"), allow_nan=False)


def _source(arguments: dict, checkpoint: dict) -> dict:
    locator = arguments["config"]["source"]["locator"]
    fact = checkpoint["observed"]["resources"].get(locator, {})
    if fact.get("fact_tool") != "resource.facts.get" or not fact.get("fields") or not fact.get("source_engine_type") or not fact.get("query_names"):
        _fail("confirmed_source")
    return fact


def _target(arguments: dict, checkpoint: dict) -> dict:
    target = arguments["config"]["target"]
    fact = checkpoint["observed"]["resources"].get(target["parent_locator"], {})
    if fact.get("item_type") not in {"schema", "database"} or not target["name"].strip():
        _fail("confirmed_target_parent_and_name")
    return fact


def _native_fields(fact: dict) -> dict[str, dict]:
    return {field["name"]: field for field in fact["fields"] if isinstance(field.get("name"), str)}


def _unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            _fail("confirmed_row_grain_and_fields")
        result[key] = value
    return result


def _query_outputs(source: dict, fact: dict) -> set[str]:
    fields = _native_fields(fact)
    query = source.get("query")
    if not query:
        return set(fields)
    # Only the Skill's explicit direct projection slice is admitted here. The
    # Provider remains responsible for full syntax, permissions and read-set.
    if query.get("language") != "mql" or fact.get("source_engine_type") != "mongodb":
        _fail("confirmed_row_grain_and_fields")
    try:
        command = json.loads(query["statement"], object_pairs_hook=_unique_object)
    except (ValueError, TypeError, KeyError, RecursionError):
        _fail("confirmed_row_grain_and_fields")
    if not isinstance(command, dict) or set(command) != {"aggregate", "pipeline"} or command["aggregate"] != fact["query_names"].get("mql"):
        _fail("confirmed_row_grain_and_fields")
    pipeline = command["pipeline"]
    if not isinstance(pipeline, list) or len(pipeline) not in {1, 2}:
        _fail("confirmed_row_grain_and_fields")
    unwind = None
    if len(pipeline) == 2:
        stage = pipeline[0]
        if not isinstance(stage, dict) or set(stage) != {"$unwind"} or not isinstance(stage["$unwind"], str):
            _fail("confirmed_row_grain_and_fields")
        unwind = stage["$unwind"].removeprefix("$")
        if stage["$unwind"] != "$" + unwind or fields.get(unwind, {}).get("type") != "array":
            _fail("confirmed_row_grain_and_fields")
    last = pipeline[-1]
    if not isinstance(last, dict) or set(last) != {"$project"} or not isinstance(last["$project"], dict) or not last["$project"]:
        _fail("confirmed_row_grain_and_fields")
    outputs = set()
    for output, expression in last["$project"].items():
        if output == "_id" and type(expression) is int and expression == 0:
            continue
        path = output if type(expression) is int and expression == 1 else None
        if isinstance(expression, str) and expression.startswith("$") and not expression.startswith("$$"):
            path = expression[1:]
        if not path or path not in fields:
            _fail("confirmed_row_grain_and_fields")
        array_ancestors = [name for name, field in fields.items() if field.get("type") == "array" and path.startswith(name + ".")]
        if any(name != unwind for name in array_ancestors):
            _fail("confirmed_row_grain_and_fields")
        outputs.add(output)
    if "_id" in fields and last["$project"].get("_id") != 0:
        outputs.add("_id")
    return outputs


def _mapping(arguments: dict, checkpoint: dict) -> list[dict]:
    outputs = _query_outputs(arguments["config"]["source"], _source(arguments, checkpoint))
    transforms = arguments["config"].get("transforms", [])
    if len(transforms) != 1 or transforms[0].get("type") != "field_mapping":
        _fail("confirmed_row_grain_and_fields")
    fields = transforms[0].get("fields", [])
    if not fields or any(field.get("source") not in outputs for field in fields):
        _fail("confirmed_row_grain_and_fields")
    targets = [field["target"] for field in fields]
    if len(targets) != len(set(targets)):
        _fail("confirmed_row_grain_and_fields")
    return fields


def review_identity(arguments: dict, checkpoint: dict) -> dict:
    context = checkpoint["observed"]["platform_capabilities"].get(CREATE_TOOL)
    if not isinstance(context, dict):
        _fail("platform_context")
    identity = {"tool": CREATE_TOOL, "revision": context["revision"], "digest": context["digest"]}
    evidence = {"arguments": arguments, "capability": identity, "source": _source(arguments, checkpoint), "target": _target(arguments, checkpoint)}
    identity["fingerprint"] = hashlib.sha256(_json(evidence).encode("utf-8")).hexdigest()
    return identity


def validate_create(arguments: dict, checkpoint: dict, *, require_review: bool = True) -> dict:
    if list(Draft202012Validator(get_tool(CREATE_TOOL).input_schema).iter_errors(arguments)):
        _fail("arguments")
    context = checkpoint["observed"]["platform_capabilities"].get(CREATE_TOOL)
    if not isinstance(context, dict):
        _fail("platform_context")
    operation = context["operation"]
    if operation["tool"] != CREATE_TOOL or operation["owner"] != get_tool(CREATE_TOOL).owner:
        _fail("platform_context")
    config = arguments["config"]
    for condition in operation["inputs_required"]:
        if condition == "confirmed_source":
            _source(arguments, checkpoint)
        elif condition == "confirmed_target_parent_and_name":
            _target(arguments, checkpoint)
        elif condition == "bounded":
            if config["runtime"]["boundary"] != "bounded":
                _fail(condition)
        elif condition == "snapshot":
            if config["load"]["mode"] != "snapshot":
                _fail(condition)
        elif condition == "explicit_apply_policy":
            policy = config["target"]["policy"]
            if policy["apply_mode"] == "upsert":
                targets = {field["target"] for field in _mapping(arguments, checkpoint)}
                keys = policy.get("keys", [])
                if not keys or len(keys) != len(set(keys)) or not set(keys) <= targets:
                    _fail(condition)
        elif condition == "confirmed_row_grain_and_fields":
            _mapping(arguments, checkpoint)
        elif condition == "user_review":
            pass  # Checked below against the full arguments and current facts.
        else:
            _fail(f"unsupported:{condition}")
    identity = review_identity(arguments, checkpoint)
    if require_review and checkpoint["confirmed"]["operation_reviews"].get(CREATE_TOOL) != identity:
        _fail("user_review")
    return identity


def prepare_review(arguments: dict, checkpoint: dict) -> dict:
    identity = validate_create(arguments, checkpoint, require_review=False)
    checkpoint["observed"]["operation_reviews"][CREATE_TOOL] = copy.deepcopy(identity)
    checkpoint["confirmed"]["operation_reviews"].pop(CREATE_TOOL, None)
    return identity


def review_json(arguments: dict) -> str:
    return "\n```json\n" + _json(arguments) + "\n```"
