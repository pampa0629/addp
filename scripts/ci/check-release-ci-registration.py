#!/usr/bin/env python3
"""Check the single ADDP T5 dispatch and reporting contract."""

from __future__ import annotations

import argparse
import ast
import re
from pathlib import Path


class RegistrationError(RuntimeError):
    pass


def make_recipe(makefile: str, target: str) -> str | None:
    match = re.search(
        rf"(?ms)^{re.escape(target)}:[^\n]*\n(?P<recipe>(?:\t[^\n]*\n)+)",
        makefile,
    )
    return match.group("recipe") if match else None


def make_declaration(makefile: str, target: str) -> str | None:
    match = re.search(rf"(?m)^{re.escape(target)}:[^\n]*$", makefile)
    return match.group(0) if match else None


def registered_suites(dispatcher: Path) -> dict[str, str]:
    tree = ast.parse(dispatcher.read_text(encoding="utf-8"), filename=str(dispatcher))
    suite_node: ast.Dict | None = None
    for node in tree.body:
        if isinstance(node, ast.AnnAssign) and isinstance(node.target, ast.Name):
            if node.target.id == "SUITES" and isinstance(node.value, ast.Dict):
                suite_node = node.value
                break
        if isinstance(node, ast.Assign) and any(
            isinstance(target, ast.Name) and target.id == "SUITES"
            for target in node.targets
        ):
            if isinstance(node.value, ast.Dict):
                suite_node = node.value
                break
    if suite_node is None:
        raise RegistrationError("scripts/test/release-gate.py SUITES registry is missing")

    suites: dict[str, str] = {}
    for key, value in zip(suite_node.keys, suite_node.values, strict=True):
        if not isinstance(key, ast.Constant) or not isinstance(key.value, str):
            raise RegistrationError("release suite names must be string literals")
        if not isinstance(value, ast.Call):
            raise RegistrationError(f"release suite {key.value}: Suite declaration is invalid")
        target = next(
            (
                keyword.value.value
                for keyword in value.keywords
                if keyword.arg == "target"
                and isinstance(keyword.value, ast.Constant)
                and isinstance(keyword.value.value, str)
            ),
            None,
        )
        if target is None:
            raise RegistrationError(f"release suite {key.value}: owner target is missing")
        suites[key.value] = target
    return suites


def workflow_job(workflow: str, job_name: str) -> str:
    match = re.search(
        rf"(?ms)^  {re.escape(job_name)}:\s*\n(?P<job>.*?)(?=^  [A-Za-z0-9_-]+:\s*$|\Z)",
        workflow,
    )
    return match.group("job") if match else ""


def validate_registration(repository: Path) -> list[str]:
    errors: list[str] = []
    dispatcher = repository / "scripts/test/release-gate.py"
    if not dispatcher.is_file():
        return ["scripts/test/release-gate.py is missing"]
    try:
        suites = registered_suites(dispatcher)
    except (RegistrationError, SyntaxError) as error:
        return [str(error)]
    if not suites:
        errors.append("release suite registry must not be empty")

    makefile_path = repository / "Makefile"
    makefile = makefile_path.read_text(encoding="utf-8") if makefile_path.is_file() else ""
    aggregate_recipe = make_recipe(makefile, "test-release")
    if aggregate_recipe is None:
        errors.append("Makefile target test-release is missing")
    elif (
        "scripts/test/release-gate.py" not in aggregate_recipe
        or '--suite "$(RELEASE_SUITE)"' not in aggregate_recipe
    ):
        errors.append("Makefile test-release must dispatch RELEASE_SUITE through release-gate.py")

    for suite, target in sorted(suites.items()):
        if make_recipe(makefile, target) is None:
            errors.append(f"release suite {suite}: Makefile owner target {target} is missing")
        elif "##" in (make_declaration(makefile, target) or ""):
            errors.append(
                f"release suite {suite}: Makefile owner target {target} "
                "must remain internal to test-release"
            )

    platform_recipe = make_recipe(makefile, "test-platform") or ""
    runner_recipe = make_recipe(makefile, "test-release-runner")
    if runner_recipe is None:
        errors.append("Makefile target test-release-runner is missing")
        runner_recipe = ""
    for required in (
        "scripts/test/release-gate_test.py",
        "scripts/ci/check-release-ci-registration_test.py",
        "scripts/ci/check-release-ci-registration.py",
    ):
        if required not in runner_recipe:
            errors.append(f"Makefile test-release-runner must run {required}")
    if "test-release-runner" not in platform_recipe:
        errors.append("Makefile test-platform must run test-release-runner")

    workflow_path = repository / ".github/workflows/release-and-t2-gates.yml"
    workflow = workflow_path.read_text(encoding="utf-8") if workflow_path.is_file() else ""
    cli_job = workflow_job(workflow, "cli-product-macos-verification")
    if not cli_job:
        errors.append("CLI T5 verification job is missing")
    else:
        if "make test-release RELEASE_SUITE=common-python-cli" not in cli_job:
            errors.append("CLI T5 workflow must call the shared test-release entry")
        if re.search(r"\bmake\s+test-common-python-cli-release\b", cli_job):
            errors.append("CLI T5 workflow must not bypass the shared test-release entry")
        if "ADDP_RELEASE_ARTIFACT_DIR:" not in cli_job:
            errors.append("CLI T5 workflow must configure the shared release artifact directory")
        if "release-summary.md" not in cli_job or "details-file:" not in cli_job:
            errors.append("CLI T5 workflow must attach the shared release summary")

    selection_job = workflow_job(workflow, "selection")
    if "scripts/test/release-gate.py" not in selection_job:
        errors.append("CLI T5 path selection must include the shared release dispatcher")
    return errors


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repository", type=Path, required=True)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    errors = validate_registration(args.repository.resolve())
    if errors:
        for error in errors:
            print(f"release CI registration error: {error}")
        return 1
    print("Release CI registration is complete.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
