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


def validate_release_source(repository: Path, tag: str, sha: str, pre_tag: bool) -> None:
    expected_tag = f"v{package_version(repository)}"
    if tag != expected_tag:
        raise RuntimeError(f"release tag {tag!r} must equal package tag {expected_tag!r}")
    if pre_tag:
        head_sha = git(repository, "rev-parse", "HEAD")
        main_sha = git(repository, "rev-parse", "refs/remotes/origin/main")
        if sha != head_sha or sha != main_sha:
            raise RuntimeError(
                f"pre-tag release commit must equal HEAD and origin/main: "
                f"release={sha}, HEAD={head_sha}, origin/main={main_sha}"
            )
        tag_exists = subprocess.run(
            ["git", "show-ref", "--verify", "--quiet", f"refs/tags/{tag}"],
            cwd=repository,
        ).returncode == 0
        if tag_exists:
            raise RuntimeError(f"release tag already exists locally: {tag}")
    else:
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
    headers = {
        "Accept": "application/vnd.github+json",
        "X-GitHub-Api-Version": "2022-11-28",
    }
    if token:
        headers["Authorization"] = f"Bearer {token}"
    request = urllib.request.Request(
        f"https://api.github.com/repos/{repository_name}/actions/runs?{query}",
        headers=headers,
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
    parser.add_argument("--github-repository")
    parser.add_argument("--pre-tag", action="store_true")
    parser.add_argument("--max-wait-seconds", type=int, default=600)
    parser.add_argument("--poll-seconds", type=int, default=15)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    token = os.environ.get("GITHUB_TOKEN", "")
    try:
        repository = args.repository.resolve()
        repository_name = args.github_repository
        if not repository_name:
            origin = git(repository, "remote", "get-url", "origin")
            match = re.search(r"github\.com[/:]([^/]+/[^/]+?)(?:\.git)?$", origin)
            if not match:
                raise RuntimeError(f"cannot derive GitHub repository from origin: {origin!r}")
            repository_name = match.group(1)
        validate_release_source(repository, args.tag, args.sha, args.pre_tag)
        wait_for_platform_ci(
            repository_name,
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
