#!/usr/bin/env python3

from __future__ import annotations

import importlib.util
import subprocess
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("check-release-eligibility.py")
SPEC = importlib.util.spec_from_file_location("release_eligibility", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(MODULE)


class ReleaseEligibilityTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary_directory = tempfile.TemporaryDirectory()
        self.repository = Path(self.temporary_directory.name)
        subprocess.run(["git", "init", "-q", "-b", "main"], cwd=self.repository, check=True)
        subprocess.run(["git", "config", "user.email", "ci@example.invalid"], cwd=self.repository, check=True)
        subprocess.run(["git", "config", "user.name", "CI"], cwd=self.repository, check=True)
        version_file = self.repository / "common-python/addp_common/__init__.py"
        version_file.parent.mkdir(parents=True)
        version_file.write_text('__version__ = "1.2.3"\n', encoding="utf-8")
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        subprocess.run(["git", "commit", "-qm", "release"], cwd=self.repository, check=True)
        self.sha = MODULE.git(self.repository, "rev-parse", "HEAD")
        subprocess.run(["git", "tag", "v1.2.3"], cwd=self.repository, check=True)
        subprocess.run(
            ["git", "update-ref", "refs/remotes/origin/main", self.sha],
            cwd=self.repository,
            check=True,
        )

    def tearDown(self) -> None:
        self.temporary_directory.cleanup()

    def test_accepts_matching_tag_on_origin_main(self) -> None:
        MODULE.validate_release_source(self.repository, "v1.2.3", self.sha, False)

    def test_rejects_tag_that_does_not_match_package_version(self) -> None:
        with self.assertRaisesRegex(RuntimeError, "must equal package tag"):
            MODULE.validate_release_source(self.repository, "v1.2.4", self.sha, False)

    def test_accepts_pre_tag_only_at_origin_main_tip_without_existing_tag(self) -> None:
        subprocess.run(
            ["git", "tag", "-d", "v1.2.3"],
            cwd=self.repository,
            check=True,
            stdout=subprocess.DEVNULL,
        )
        MODULE.validate_release_source(self.repository, "v1.2.3", self.sha, True)

    def test_rejects_pre_tag_when_tag_already_exists(self) -> None:
        with self.assertRaisesRegex(RuntimeError, "already exists locally"):
            MODULE.validate_release_source(self.repository, "v1.2.3", self.sha, True)

    def test_rejects_commit_outside_origin_main(self) -> None:
        subprocess.run(["git", "checkout", "-qb", "detached-release"], cwd=self.repository, check=True)
        (self.repository / "release.txt").write_text("release\n", encoding="utf-8")
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        subprocess.run(["git", "commit", "-qm", "outside main"], cwd=self.repository, check=True)
        outside_sha = MODULE.git(self.repository, "rev-parse", "HEAD")
        subprocess.run(
            ["git", "tag", "-f", "v1.2.3", outside_sha],
            cwd=self.repository,
            check=True,
            stdout=subprocess.DEVNULL,
        )
        with self.assertRaises(subprocess.CalledProcessError):
            MODULE.validate_release_source(self.repository, "v1.2.3", outside_sha, False)

    def test_platform_ci_state_requires_successful_push_run_for_sha(self) -> None:
        payload = {
            "workflow_runs": [
                {"name": "Platform CI", "event": "push", "head_sha": "other", "status": "completed", "conclusion": "success"},
                {"name": "Platform CI", "event": "pull_request", "head_sha": self.sha, "status": "completed", "conclusion": "success"},
                {"name": "Platform CI", "event": "push", "head_sha": self.sha, "status": "completed", "conclusion": "failure"},
            ]
        }
        self.assertEqual("failure", MODULE.platform_ci_state(payload, self.sha)[0])
        payload["workflow_runs"][-1]["conclusion"] = "success"
        self.assertEqual(("success", "Platform CI succeeded"), MODULE.platform_ci_state(payload, self.sha))


if __name__ == "__main__":
    unittest.main()
