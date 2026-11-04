"""Exact Transfer parameter provenance, independent of model prose and approvals."""
import copy
import json
import re

from jsonschema import Draft202012Validator
from addp_common.tools import get_tool
from agents.platform_conditions import CREATE_TOOL, config_arguments_digest

BUILD_TOOL = "transfer.task.draft.build"
MAX_PARAMETER_BYTES = 128 * 1024


def _unique_object(pairs):
    value = {}
    for key, item in pairs:
        if key in value:
            raise ValueError("duplicate_json_key")
        value[key] = item
    return value


def explicit_user_arguments(text: str) -> dict | None:
    """Accept exactly one complete JSON request supplied by the user.

    Decode complete objects in order, never pull a nested config out of another
    object or infer missing properties. Markdown and a short prose prefix work.
    """
    if not isinstance(text, str) or len(text.encode()) > MAX_PARAMETER_BYTES:
        return None
    decoder = json.JSONDecoder(object_pairs_hook=_unique_object)
    validator = Draft202012Validator(get_tool(CREATE_TOOL).input_schema)
    arguments, cursor = None, 0
    while match := re.search(r"[\[\{]", text[cursor:]):
        start = cursor + match.start()
        try:
            value, end = decoder.raw_decode(text, start)
        except ValueError:
            # A broken outer object must never expose a valid inner object.
            return None
        if arguments is not None or list(validator.iter_errors(value)):
            return None
        arguments, cursor = value, end
    return arguments


def validate_parameter_reference(reference):
    if reference is None:
        return
    if (not isinstance(reference, dict) or set(reference) != {"kind", "id", "digest"}
            or reference["kind"] not in {"build", "user"}
            or not isinstance(reference["id"], str) or not re.fullmatch(r"[^\x00-\x1f]{1,140}", reference["id"])
            or not isinstance(reference["digest"], str) or not re.fullmatch(r"[0-9a-f]{64}", reference["digest"])):
        raise ValueError("transfer_parameter_source_invalid")


def parameter_reference(kind: str, identity: str, arguments: dict) -> dict:
    reference = {"kind": kind, "id": identity, "digest": config_arguments_digest(arguments)}
    validate_parameter_reference(reference)
    return reference


def validate_parameter_arguments(reference: dict, arguments: dict) -> None:
    validate_parameter_reference(reference)
    if (not isinstance(arguments, dict)
            or list(Draft202012Validator(get_tool(CREATE_TOOL).input_schema).iter_errors(arguments))
            or reference is None or reference["digest"] != config_arguments_digest(arguments)
            or len(json.dumps(arguments, ensure_ascii=False).encode()) > MAX_PARAMETER_BYTES):
        raise ValueError("transfer_parameter_source_invalid")


def build_parameter_evidence(arguments: dict, call_id: str) -> tuple[dict, dict]:
    reference = parameter_reference("build", call_id, arguments)
    validate_parameter_arguments(reference, arguments)
    return reference, {"reference": reference, "arguments": copy.deepcopy(arguments)}
