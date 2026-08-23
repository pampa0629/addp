#!/usr/bin/env python3
"""Reject CLI release tags that are not backed by a successful Platform CI run."""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import time
import urllib.parse
import urllib.request
from pathlib import Path


def git(repository: Path, *arguments: str) -> str:
    return subprocess.check_output(
        ["git", *arguments], cwd=repository, text=True
    ).strip()


def package_version(repository: Path) -> str:
    source = (repository / "common-python/addp_common/__init__.py").read_text(
        encoding="utf-8"
    )
    match = re.search(r'(?m)^__version__ = "([^"]+)"$', source)
    if not match:
        raise RuntimeError("common-python package version is missing")
    return match.group(1)


def validate_tag(repository: Path, tag: str, sha: str) -> None:
    expected_tag = f"v{package_version(repository)}"
    if tag != expected_tag:
        raise RuntimeError(f"release tag {tag!r} must equal package tag {expected_tag!r}")
    tag_sha = git(repository, "rev-parse", f"{tag}^{{commit}}")
    if tag_sha != sha:
        raise RuntimeError(f"release tag resolves to {tag_sha}, expected {sha}")
    subprocess.run(
        ["git", "merge-base", "--is-ancestor", sha, "refs/remotes/origin/main"],
        cwd=repository,
        check=True,
    )


def platform_ci_state(payload: dict, sha: str) -> tuple[str, str]:
    runs = [
        run
        for run in payload.get("workflow_runs", [])
        if run.get("name") == "Platform CI"
        and run.get("event") == "push"
        and run.get("head_sha") == sha
    ]
    if any(run.get("status") == "completed" and run.get("conclusion") == "success" for run in runs):
        return "success", "Platform CI succeeded"
    if any(run.get("status") != "completed" for run in runs):
        return "pending", "Platform CI is still running"
    if runs:
        conclusions = sorted({str(run.get("conclusion")) for run in runs})
        return "failure", f"Platform CI completed without success: {', '.join(conclusions)}"
    return "pending", "Platform CI push run is not registered yet"


def fetch_runs(repository_name: str, sha: str, token: str) -> dict:
    query = urllib.parse.urlencode({"head_sha": sha, "event": "push", "per_page": 100})
    request = urllib.request.Request(
        f"https://api.github.com/repos/{repository_name}/actions/runs?{query}",
        headers={
            "Accept": "application/vnd.github+json",
            "Authorization": f"Bearer {token}",
            "X-GitHub-Api-Version": "2022-11-28",
        },
    )
    with urllib.request.urlopen(request, timeout=30) as response:
        return json.load(response)


def wait_for_platform_ci(
    repository_name: str,
    sha: str,
    token: str,
    max_wait_seconds: int,
    poll_seconds: int,
) -> None:
    deadline = time.monotonic() + max_wait_seconds
    while True:
        state, detail = platform_ci_state(fetch_runs(repository_name, sha, token), sha)
        print(detail)
        if state == "success":
            return
        if state == "failure":
            raise RuntimeError(detail)
        if time.monotonic() >= deadline:
            raise RuntimeError(f"timed out waiting for Platform CI: {detail}")
        time.sleep(poll_seconds)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repository", type=Path, default=Path.cwd())
    parser.add_argument("--tag", required=True)
    parser.add_argument("--sha", required=True)
    parser.add_argument("--github-repository", required=True)
    parser.add_argument("--max-wait-seconds", type=int, default=600)
    parser.add_argument("--poll-seconds", type=int, default=15)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    token = os.environ.get("GITHUB_TOKEN", "")
    if not token:
        print("GITHUB_TOKEN is required", file=sys.stderr)
        return 2
    try:
        validate_tag(args.repository.resolve(), args.tag, args.sha)
        wait_for_platform_ci(
            args.github_repository,
            args.sha,
            token,
            args.max_wait_seconds,
            args.poll_seconds,
        )
    except (RuntimeError, subprocess.CalledProcessError, OSError) as error:
        print(f"CLI release eligibility failed: {error}", file=sys.stderr)
        return 1
    print(f"CLI release eligibility passed for {args.tag} at {args.sha}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
