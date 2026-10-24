#!/usr/bin/env python3
"""Inject one development launch unit's metrics input, then exec its receiver.

The SDK owns address, certificate and listener validation. This adapter owns
only the development plan and the current launch unit's deployment binding.
"""

import json
import os
import re
import sys

PLAN_ENV = "ADDP_DEV_PROCESS_METRICS_PLAN"
SDK_ENV = "ADDP_PROCESS_METRICS_DEPLOYMENTS"
INVALID = "dev_process_metrics_plan_invalid"
FIELDS = {"module_name", "role", "listen", "endpoint", "tls_dir"}


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(INVALID)
        result[key] = value
    return result


def select_deployment(raw, unit, module, role):
    if raw == "":
        return ""
    if len(raw.encode("utf-8")) > 256 << 10:
        raise ValueError(INVALID)
    plan = json.loads(raw, object_pairs_hook=unique_object)
    if not isinstance(plan, dict) or len(plan) > 1000:
        raise ValueError(INVALID)
    seen = {name: set() for name in ("listen", "endpoint", "tls_dir")}
    for name, deployment in plan.items():
        if not re.fullmatch(r"[a-z0-9][a-z0-9_-]{0,99}", name):
            raise ValueError(INVALID)
        if not isinstance(deployment, dict) or not FIELDS <= set(deployment) or set(deployment) - FIELDS - {"cgroup_v2"}:
            raise ValueError(INVALID)
        if any(not isinstance(value, str) or not value or len(value) > 1024
               for key, value in deployment.items() if key in FIELDS):
            raise ValueError(INVALID)
        if "cgroup_v2" in deployment and type(deployment["cgroup_v2"]) is not bool:
            raise ValueError(INVALID)
        if not re.fullmatch(r"[a-z0-9_-]{1,50}", deployment["module_name"]):
            raise ValueError(INVALID)
        if deployment["role"] not in {"backend", "worker", "scheduler", "ingress"}:
            raise ValueError(INVALID)
        for field, values in seen.items():
            if deployment[field] in values:
                raise ValueError(INVALID)
            values.add(deployment[field])
    selected = plan.get(unit)
    if selected is None:
        return ""
    if selected["module_name"] != module or selected["role"] != role:
        raise ValueError(INVALID)
    return json.dumps([selected], ensure_ascii=False, separators=(",", ":"))


def launch(argv, environment):
    if len(argv) < 5:
        print("Usage: process-metrics-launch.py UNIT MODULE ROLE RECEIVER PROGRAM [ARGS...]", file=sys.stderr)
        return 2
    unit, module, role, receiver, *program = argv
    child = dict(environment)
    try:
        # A development root SDK input is forbidden, even without a new plan.
        # Never let an inherited module/role array select another launch unit.
        if child.get(SDK_ENV, ""):
            raise ValueError(INVALID)
        deployment = select_deployment(child.get(PLAN_ENV, ""), unit, module, role)
    except (ValueError, RecursionError, UnicodeError):
        print(INVALID, file=sys.stderr)
        deployment = ""
    child.pop(PLAN_ENV, None)
    child[SDK_ENV] = deployment
    os.execvpe(receiver, [receiver, "launch", "--module", module, "--role", role, "--", *program], child)


if __name__ == "__main__":
    sys.exit(launch(sys.argv[1:], os.environ))
