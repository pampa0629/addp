"""Deterministic safety and false-positive checks; no developer services or tools."""

import copy
import importlib.util
import json
import os
import re
import signal
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


SCRIPT = Path(__file__).with_name("workflow-runtime-health-recovery-online.py")
SPEC = importlib.util.spec_from_file_location("workflow_runtime_health_recovery", SCRIPT)
SUITE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SUITE)


class FakeObserver:
    def __init__(self, tool, name, dep):
        self.runtimes = {name: {"tool": tool, "dep": dep, "id": 42}}
        self.events = []
        self.registration = {"http_status": 202, "engine_id": 42, "success": True}
        self.changed_dependency = False
        self.fail_offline = False

    def health(self, name, missing=None):
        self.events.append("missing" if missing else "healthy")
        facts = {self.runtimes[name]["dep"]: {"required": True, "available": missing is None},
                 "other": {"required": True, "available": not (missing and self.changed_dependency)}}
        return {"status": "unhealthy" if missing else "healthy", "dependencies": facts}

    def state(self, name, expected):
        self.events.append(expected)
        if expected == "offline" and self.fail_offline:
            raise SUITE.SuiteError("peer or lifecycle changed")
        return True

    def log_offset(self, name):
        return 0

    def register_unhealthy(self, name):
        self.events.append("register")
        assert not self.runtimes[name]["tool"].exists()
        return self.registration

    def registration_probe_finished(self, name, offset):
        self.events.append("registration_probe_finished")
        assert not self.runtimes[name]["tool"].exists()
        return True

    def stable(self):
        self.events.append("stable")


class RuntimeHealthRecoveryTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-runtime-health-unit-")
        self.root = Path(self.temporary.name).resolve() / "checkout"
        self.root.mkdir()
        self.artifacts = Path(self.temporary.name) / "artifacts"
        self.artifacts.mkdir()
        self.root_patch = patch.object(SUITE, "ROOT", self.root)
        self.root_patch.start()
        self.addCleanup(self.root_patch.stop)
        self.addCleanup(self.temporary.cleanup)
        self.fault = SUITE.ToolFault(self.artifacts / "journal.json")

    def tool(self, name="pointcloud"):
        relative = {
            "pointcloud": "engines/pointcloud-workflow/venv/bin/pdal",
            "model3d": ".dev-state/model3d-native/hash/bin/IfcConvert",
            "document": ".dev-state/document-native/hash/LibreOffice.app/Contents/MacOS/soffice",
        }[name]
        tool = self.root / relative
        tool.parent.mkdir(parents=True, exist_ok=True)
        tool.write_bytes(b"owned native executable fixture")
        tool.chmod(0o755)
        return tool

    def test_three_cycles_hold_fault_through_unhealthy_registration_and_restore_before_periodic_online(self):
        for name, dep, _ in SUITE.CASES:
            with self.subTest(name=name):
                tool = self.tool(name)
                original = SUITE.file_identity(tool)
                observer = FakeObserver(tool, name, dep)
                report = SUITE.run_cycle(observer, self.fault, name)
                self.assertEqual(report["result"], "passed")
                self.assertEqual(observer.events, ["healthy", "online", "missing", "offline", "register",
                                                  "registration_probe_finished", "offline", "healthy", "online", "stable"])
                self.assertEqual(SUITE.file_identity(tool), original)
                self.assertFalse(self.fault.journal.exists())

    def test_assertion_failures_restore_tools_and_never_report_online_recovery(self):
        for failure in ("wrong_id", "registration_rejected", "dependency_changed", "peer_changed", "probe_timeout"):
            with self.subTest(failure=failure):
                tool = self.tool()
                original = SUITE.file_identity(tool)
                observer = FakeObserver(tool, "pointcloud", "pdal")
                if failure == "wrong_id":
                    observer.registration["engine_id"] = 43
                elif failure == "registration_rejected":
                    observer.registration["http_status"] = 503
                elif failure == "dependency_changed":
                    observer.changed_dependency = True
                elif failure == "peer_changed":
                    observer.fail_offline = True
                else:
                    observer.registration_probe_finished = lambda *_: (_ for _ in ()).throw(SUITE.SuiteError("probe timeout"))
                with self.assertRaises(SUITE.SuiteError):
                    SUITE.run_cycle(observer, self.fault, "pointcloud")
                self.assertEqual(SUITE.file_identity(tool), original)
                self.assertFalse(self.fault.journal.exists())
                self.assertEqual(observer.events.count("online"), 1)

    def test_interrupt_restores_exact_file_without_chmod_of_hardlinked_cache(self):
        tool = self.tool()
        cache = self.artifacts / "conda-cache"
        os.link(tool, cache)
        original = SUITE.file_identity(cache)
        with self.assertRaises(SUITE.SuiteError):
            with self.fault.missing(tool):
                self.assertFalse(tool.exists())
                SUITE.interrupted(signal.SIGTERM, None)
        self.assertEqual(SUITE.file_identity(tool), original)
        self.assertEqual(SUITE.file_identity(cache), original)

    def test_restore_only_handles_killed_process_and_is_idempotent(self):
        tool = self.tool()
        original = SUITE.file_identity(tool)
        program = f'''import importlib.util, pathlib
s=importlib.util.spec_from_file_location("fault",{str(SCRIPT)!r})
m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
m.ROOT=pathlib.Path({str(self.root)!r})
with m.ToolFault(pathlib.Path({str(self.fault.journal)!r})).missing(pathlib.Path({str(tool)!r})):
    import os,signal; os.kill(os.getpid(),signal.SIGKILL)
'''
        result = subprocess.run([sys.executable, "-c", program], capture_output=True, text=True)
        self.assertEqual(result.returncode, -signal.SIGKILL, result.stderr)
        self.assertFalse(tool.exists())
        self.fault.restore()
        self.fault.restore()
        self.assertEqual(SUITE.file_identity(tool), original)
        self.assertFalse(list(tool.parent.glob("*.addp-health-recovery")))

    def test_recovery_refuses_concurrent_replacement_and_preserves_journal(self):
        tool = self.tool()
        original = SUITE.file_identity(tool)
        with self.assertRaisesRegex(SUITE.SuiteError, "refusing overwrite"):
            with self.fault.missing(tool):
                tool.write_bytes(b"concurrent replacement")
        self.assertTrue(self.fault.journal.exists())
        self.assertEqual(tool.read_bytes(), b"concurrent replacement")
        tool.unlink()
        self.fault.restore()
        self.assertEqual(SUITE.file_identity(tool), original)

    def test_fault_refuses_external_symlink_and_unowned_paths(self):
        outside = self.artifacts / "tool"
        outside.write_bytes(b"external")
        for tool in (outside, self.root / "system-tool"):
            with self.subTest(tool=tool), self.assertRaises((SUITE.SuiteError, ValueError)):
                with self.fault.missing(tool):
                    self.fail("unsafe fault was allowed")
        tool = self.tool()
        tool.unlink()
        tool.symlink_to(outside)
        with self.assertRaises(SUITE.SuiteError):
            with self.fault.missing(tool):
                self.fail("symlink was allowed")
        self.assertEqual(outside.read_bytes(), b"external")

    def test_health_contract_rejects_false_positive_and_legacy_shapes(self):
        body = {"status": "healthy", "service": "pointcloud-workflow", "version": "1.0",
                "dependencies": {"pdal": {"required": True, "available": True}}}
        SUITE.validate_health(200, body)
        for modify, status in ((lambda b: b.update(status="degraded"), 200),
                               (lambda b: b.update(conversion_ready=True), 200),
                               (lambda b: b["dependencies"]["pdal"].update(available=False), 200),
                               (lambda b: b["dependencies"]["pdal"].update(required="true"), 200),
                               (lambda b: b["dependencies"]["pdal"].update(path="/private"), 200),
                               (lambda b: b.update(service=""), 200), (lambda b: None, 503)):
            invalid = copy.deepcopy(body)
            modify(invalid)
            with self.subTest(body=invalid), self.assertRaises(SUITE.SuiteError):
                SUITE.validate_health(status, invalid)
        body["status"] = "unhealthy"
        body["dependencies"]["pdal"]["available"] = False
        SUITE.validate_health(503, body, "pdal")

    def test_personal_environment_is_rejected_before_native_commands(self):
        (self.root / ".env").touch()
        with patch.dict(os.environ, {"ADDP_ONLINE_HOST": "1"}), patch.object(sys, "platform", "darwin"):
            with self.assertRaisesRegex(SUITE.SuiteError, "repository .env"):
                SUITE.isolation()

    def test_exact_process_selects_only_requested_runtime_using_production_flags(self):
        source = (SCRIPT.parents[1] / "dev/start.sh").read_text()
        body = re.search(r'if \[ "\$EXACT_PROCESS" = true \]; then\n    case "\$SELECTED_MODULE" in\n(.*?)    esac', source, re.S).group(1)
        for name, _, _ in SUITE.CASES:
            shell = 'SELECTED_MODULE=$1; case "$SELECTED_MODULE" in\n' + body + '\nesac\ncompgen -A variable START_ | while read -r flag; do printf "%s=%s\\n" "$flag" "${!flag}"; done'
            result = subprocess.run(["bash", "-c", shell, "_", name + "-workflow"], capture_output=True, text=True, check=True)
            self.assertEqual(result.stdout.strip(), f"START_{name.upper()}_WORKFLOW=true")

    def test_descriptor_endpoint_facts_bind_to_owned_listener_and_identity_changes_fail(self):
        descriptors = []
        observer = SUITE.Observer.__new__(SUITE.Observer)
        observer.runtimes = {}
        observer.processes = {}
        observer.stable = lambda: None
        for number, (name, dep, _) in enumerate(SUITE.CASES, 42):
            observer.runtimes[name] = {"dep": dep}
            observer.processes[name + "-workflow-engine"] = {"pid": number}
            descriptors.append({"id": number, "engine_type": name + "_workflow", "lifecycle_state": "active",
                                "connection_status": "online", "runtime_endpoint":
                                {"protocol": "http", "host": "localhost", "port": 8100 + number}})
            log = self.root / f"logs/{name}-workflow-engine.log"
            log.parent.mkdir(exist_ok=True)
            log.write_text(f"{name}_workflow registered to System\n")
        descriptors.append({"id": 99, "engine_type": "postgresql", "lifecycle_state": "active"})
        observer.descriptors = lambda: copy.deepcopy(descriptors)
        with patch.object(SUITE.subprocess, "check_output", side_effect=lambda args, **_: str(int(args[2][4:]) - 8100) + "\n"):
            observer.bind()
        self.assertEqual(observer.runtimes["pointcloud"]["url"], "http://localhost:8142")
        self.assertTrue(observer.state("pointcloud", "online"))
        descriptors[0]["connection_status"] = "offline"
        self.assertTrue(observer.state("pointcloud", "offline"))
        descriptors[1]["connection_status"] = "offline"
        self.assertFalse(observer.state("pointcloud", "offline"))
        for field, value in (("id", 111), ("lifecycle_state", "retired"),
                             ("runtime_endpoint", {"host": "other-checkout"})):
            original = descriptors[0][field]
            descriptors[0][field] = value
            with self.subTest(field=field), self.assertRaises(SUITE.SuiteError):
                observer.state("pointcloud", "offline")
            descriptors[0][field] = original


if __name__ == "__main__":
    unittest.main()
