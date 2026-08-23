#!/usr/bin/env python3

from __future__ import annotations

import importlib.util
import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


SCRIPT = Path(__file__).with_name("select-image-services.py")
SPEC = importlib.util.spec_from_file_location("select_image_services", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(MODULE)


class SelectImageServicesTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary_directory = tempfile.TemporaryDirectory()
        self.repository = Path(self.temporary_directory.name)
        self.registrations = [
            ("system-backend", "system/backend"),
            ("agent-backend", "agent/backend"),
            ("meta-backend", "meta/backend"),
            ("meta-worker", "meta/backend"),
            ("gateway", "gateway"),
            ("geopython-workflow-engine", "engines/geopython-workflow"),
            ("model3d-workflow-engine", "engines/model3d-workflow"),
            ("console", "console/frontend"),
            ("meta-frontend", "meta/frontend"),
            ("nginx", "nginx"),
        ]
        self._write(
            "agent/backend/Dockerfile",
            "FROM scratch\nCOPY common-python /common-python\n",
        )
        self._write(
            "engines/geopython-workflow/Dockerfile",
            "FROM scratch\nCOPY common-python /common-python\n",
        )
        self._write(
            "engines/model3d-workflow/Dockerfile",
            "FROM scratch\nCOPY common-python /common-python\n",
        )

    def tearDown(self) -> None:
        self.temporary_directory.cleanup()

    def _write(self, relative_path: str, content: str) -> None:
        path = self.repository / relative_path
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")

    def _select(self, *paths: str) -> list[str]:
        return MODULE.select_services(
            self.repository, self.registrations, set(paths)
        )

    def test_keeps_representative_baseline_for_unrelated_change(self) -> None:
        self.assertEqual(
            ["system-backend", "agent-backend", "console", "nginx"],
            self._select("docs/README.md"),
        )

    def test_module_change_selects_all_registered_images_in_directory(self) -> None:
        selected = self._select("meta/backend/internal/service/example.go")
        self.assertIn("meta-backend", selected)
        self.assertIn("meta-worker", selected)

    def test_shared_frontend_change_selects_all_frontends(self) -> None:
        selected = self._select("common-frontend/basic/src/index.js")
        self.assertIn("console", selected)
        self.assertIn("meta-frontend", selected)

    def test_shared_python_change_selects_real_consumers(self) -> None:
        selected = self._select("common-python/addp_common/client.py")
        self.assertIn("agent-backend", selected)
        self.assertIn("geopython-workflow-engine", selected)
        self.assertNotIn("model3d-workflow-engine", selected)

    def test_shared_go_change_selects_go_product_images(self) -> None:
        selected = self._select("common/client/system.go")
        self.assertIn("system-backend", selected)
        self.assertIn("meta-backend", selected)
        self.assertIn("meta-worker", selected)
        self.assertIn("gateway", selected)
        self.assertNotIn("meta-frontend", selected)

    def test_dockerignore_change_selects_all_hosted_images(self) -> None:
        selected = self._select(".dockerignore")
        self.assertEqual(
            {name for name, _ in self.registrations}
            - MODULE.HOSTED_RUNNER_EXCLUSIONS,
            set(selected),
        )

    def test_push_event_reads_exact_changed_paths(self) -> None:
        subprocess.run(["git", "init", "-q"], cwd=self.repository, check=True)
        subprocess.run(
            ["git", "config", "user.email", "ci@example.invalid"],
            cwd=self.repository,
            check=True,
        )
        subprocess.run(
            ["git", "config", "user.name", "CI"],
            cwd=self.repository,
            check=True,
        )
        self._write("docs/README.md", "before\n")
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        subprocess.run(["git", "commit", "-qm", "before"], cwd=self.repository, check=True)
        before = subprocess.check_output(
            ["git", "rev-parse", "HEAD"], cwd=self.repository, text=True
        ).strip()
        self._write("meta/backend/Dockerfile", "FROM scratch\n")
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        subprocess.run(["git", "commit", "-qm", "after"], cwd=self.repository, check=True)
        head = subprocess.check_output(
            ["git", "rev-parse", "HEAD"], cwd=self.repository, text=True
        ).strip()

        with patch.dict(
            os.environ,
            {
                "ADDP_CI_EVENT": "push",
                "ADDP_CI_BEFORE": before,
                "ADDP_CI_HEAD": head,
            },
            clear=False,
        ):
            self.assertEqual(
                {"meta/backend/Dockerfile"},
                MODULE.changed_paths_from_git(self.repository),
            )


if __name__ == "__main__":
    unittest.main()
