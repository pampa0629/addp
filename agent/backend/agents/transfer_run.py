"""Transfer owner review references and deterministic resume, never approval."""
import copy

START_TOOL = "transfer.task.start"
GET_TOOL = "transfer.execution.get"


def platform_identity(checkpoint):
    context = checkpoint["observed"]["platform_capabilities"].get(START_TOOL)
    if not isinstance(context, dict) or context.get("capability") != START_TOOL:
        raise ValueError("platform_condition_unsatisfied:platform_context")
    operation = context.get("operation", {})
    if operation.get("owner") != "transfer" or operation.get("tool") != START_TOOL:
        raise ValueError("platform_operation_mismatch")
    supported = {"saved_supported_task", "owner_run_approval", "current_execution_authorization"}
    if set(operation.get("inputs_required", [])) != supported:
        raise ValueError("platform_condition_unsatisfied:unknown_run_condition")
    return {"revision": context["revision"], "digest": context["digest"]}


def capture_run_review(result, checkpoint):
    if not isinstance(result, dict) or result.get("status") != "approval_required":
        return
    summary = result.get("request_summary", {})
    task_id = summary.get("task_id")
    if not isinstance(task_id, int) or isinstance(task_id, bool) or task_id < 1:
        raise ValueError("invalid_run_approval_task")
    checkpoint["observed"]["run_approval"] = {
        "task_id": task_id, "approval_id": result["interaction_id"],
        "request_fingerprint": result["request_fingerprint"],
        "capability": platform_identity(checkpoint),
    }
    checkpoint["platform_capability"] = START_TOOL


def confirm_run_review(checkpoint, interaction):
    observed = checkpoint["observed"].get("run_approval")
    answer = interaction.answer
    if (not isinstance(observed, dict) or not isinstance(answer, dict)
            or interaction.owner_interaction_id != observed["approval_id"]
            or answer.get("approval_id") != observed["approval_id"]
            or answer.get("request_fingerprint") != observed["request_fingerprint"]
            or answer.get("status") not in {"approved", "consumed", "rejected"}):
        raise ValueError("owner_run_approval_not_observed")
    checkpoint["confirmed"]["run_approval"] = {
        **copy.deepcopy(observed), "status": answer["status"],
        **({"execution_id": answer["execution_id"]} if answer.get("execution_id") else {}),
    }


def validate_start(arguments, checkpoint):
    current = platform_identity(checkpoint)
    if set(arguments) == {"task_id"}:
        return  # The owner may only request a pending review, never run.
    confirmed = checkpoint["confirmed"].get("run_approval")
    if not isinstance(confirmed, dict) or confirmed.get("status") != "approved":
        raise ValueError("platform_condition_unsatisfied:owner_run_approval")
    expected = {key: confirmed[key] for key in ("task_id", "approval_id", "request_fingerprint")}
    if arguments != expected or current != confirmed["capability"]:
        raise ValueError("platform_condition_unsatisfied:run_review_changed")
