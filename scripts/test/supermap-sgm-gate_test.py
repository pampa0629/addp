"""Deterministic isolation/failure contracts; no Docker, SDK, license or network."""
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location("sgm_t2_gate", Path(__file__).with_name("supermap-sgm-gate.py"))
GATE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(GATE)


class IsolationContract(unittest.TestCase):
    def test_wrong_architecture_rejected_before_start(self):
        for os_name, arch in (("linux", "amd64"), ("darwin", "arm64")):
            with self.subTest(os=os_name, arch=arch), patch.object(GATE, "command", return_value=json.dumps([
                {"Os": os_name, "Architecture": arch, "Id": "sha256:" + "a" * 64}])):
                with self.assertRaises(GATE.GateError):
                    GATE.image_identity("private")

    def test_report_cannot_overwrite_or_enter_checkout_through_symlink(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            existing = root / "existing.json"
            existing.write_text("previous evidence")
            link = root / "checkout"
            link.symlink_to(GATE.ROOT, target_is_directory=True)
            for value in ("relative.json", str(existing), str(link / "report.json")):
                with self.subTest(value=value), self.assertRaises(GATE.GateError):
                    GATE.report_path(value)
            self.assertEqual(existing.read_text(), "previous evidence")

    def test_private_sdk_directory_cannot_follow_tmpdir_into_checkout(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            work = root / "private"
            work.mkdir()
            with patch.object(GATE, "ROOT", root), patch.object(GATE.tempfile, "mkdtemp", return_value=str(work)):
                with self.assertRaisesRegex(GATE.GateError, "outside"):
                    GATE.Lifecycle()
            self.assertFalse(work.exists())

    def test_unknown_create_outcome_cleanup_uses_only_run_labels(self):
        life = GATE.Lifecycle()
        calls = []
        resources = {"container": ["owned-container"], "network": ["owned-network"], "volume": ["owned-volume"]}

        def docker(*args, **_kwargs):
            calls.append(args)
            kind = args[0]
            if args[1] == "ls":
                self.assertEqual(args[-1], f"label={GATE.LABEL}={life.run_id}")
                return "\n".join(resources[kind])
            self.assertIn(args[-1], resources[kind])
            resources[kind].clear()
            return ""

        with patch.object(life, "docker", side_effect=docker):
            self.assertEqual(life.cleanup(), [])
        self.assertFalse(life.work.exists())
        self.assertEqual([call[0] for call in calls if "rm" in call], ["container", "volume", "network"])

    def test_stopped_and_never_started_containers_are_included_in_cleanup(self):
        life = GATE.Lifecycle()
        try:
            with patch.object(life, "docker", return_value="stopped\ncreated\n") as docker:
                self.assertEqual(life.owned("container"), ["stopped", "created"])
                self.assertIn("--all", docker.call_args.args)
        finally:
            GATE.shutil.rmtree(life.work)

    def test_cleanup_continues_after_container_failure_and_deletes_private_data(self):
        life = GATE.Lifecycle()
        private = life.private_json("secret.json", {"secret": "must disappear"})
        self.assertEqual(private.stat().st_mode & 0o777, 0o600)
        visited = []

        def owned(kind):
            visited.append(kind)
            if kind == "container":
                raise GATE.GateError("daemon failure")
            return []

        with patch.object(life, "owned", side_effect=owned):
            self.assertEqual(life.cleanup(), ["container"])
        self.assertIn("network", visited)
        self.assertIn("volume", visited)
        self.assertFalse(private.exists())

    def test_failure_report_is_private_and_cannot_claim_pass_or_expose_exception(self):
        with tempfile.TemporaryDirectory() as directory:
            report = Path(directory) / "report.json"
            with patch.dict(GATE.os.environ, {"SGM_T2_REPORT": str(report)}, clear=True), \
                    patch.object(GATE, "build_images", side_effect=GATE.GateError("forced preflight failure")), \
                    patch.object(GATE.Lifecycle, "owned", return_value=[]), \
                    contextlib.redirect_stdout(io.StringIO()) as output, contextlib.redirect_stderr(output):
                self.assertEqual(GATE.main(), 1)
            data = json.loads(report.read_text())
            self.assertEqual(data["result"], "failed")
            self.assertTrue(data["zero_residue"])
            self.assertEqual(report.stat().st_mode & 0o777, 0o600)
            self.assertNotIn("SECRET_LICENSE", report.read_text())

    def test_trace_drops_headers_and_rejects_directory_listing(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "trace"
            event = {"api": {"name": "GetObject"}, "request": {
                "method": "GET", "path": "/sgm-t2/source/denied/compass.png", "headers": {"Authorization": "Credential=runtime-123/SECRET"}},
                "response": {"statusCode": 403}}
            path.write_text(json.dumps(event) + "\n")
            self.assertNotIn("SECRET", json.dumps(GATE.audit_trace(path)))
            path.write_text(json.dumps({"api": {"name": "ListObjectsV2"}}) + "\n")
            with self.assertRaisesRegex(GATE.GateError, "listed"):
                GATE.audit_trace(path)

    def test_extra_fallback_after_denial_is_rejected(self):
        with self.assertRaisesRegex(GATE.GateError, "candidate"):
            GATE.verify_candidate_requests([
                {"path": "/sgm-t2/source/denied/compass.png", "status": 403},
                {"path": "/sgm-t2/source/denied/Textures/compass.png", "status": 200},
            ])

    def test_command_failure_never_exposes_output_or_credential_arguments(self):
        with patch.object(GATE.subprocess, "Popen") as factory:
            factory.return_value.communicate.return_value = (b"PRIVATE_SDK_DATA", b"SECRET_PASSWORD")
            factory.return_value.returncode = 1
            with self.assertRaises(GATE.GateError) as raised:
                GATE.command(["docker", "password=SECRET_PASSWORD"])
        self.assertNotIn("PRIVATE", str(raised.exception))
        self.assertNotIn("SECRET", str(raised.exception))

    def test_licensed_entry_is_manual_and_deterministic_tests_use_existing_ci(self):
        make = (GATE.ROOT / "Makefile").read_text()
        self.assertRegex(make, r"test-engines-supermap-sgm:[^\n]*\n\t@bash scripts/test/engines-supermap-sgm-gate.sh")
        self.assertIn("@python3 -m unittest scripts/test/supermap-sgm-gate_test.py", make)
        spec = importlib.util.spec_from_file_location("sgm_t2_registration", GATE.ROOT / "scripts/ci/check-t2-ci-registration.py")
        registration = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(registration)
        self.assertIn(("scripts/test/engines-supermap-sgm-gate.sh", "test-engines-supermap-sgm", "engines", "supermap"),
                      registration.discover_owner_managed_gates(GATE.ROOT))
        for workflow in (GATE.ROOT / ".github/workflows").glob("*.yml"):
            self.assertNotIn("make test-engines-supermap-sgm", workflow.read_text())
        changed_spec = importlib.util.spec_from_file_location("sgm_changed_gate", GATE.ROOT / "scripts/test/changed-gate.py")
        changed = importlib.util.module_from_spec(changed_spec)
        changed_spec.loader.exec_module(changed)
        for path in ("scripts/test/engines-supermap-sgm-gate.sh", "scripts/test/supermap-sgm-gate.py",
                     "scripts/test/supermap-sgm-probe.py", "scripts/test/supermap-sgm-gate_test.py"):
            self.assertEqual(changed.affected_modules(GATE.ROOT, [path]), ["engines"])


if __name__ == "__main__":
    unittest.main()
