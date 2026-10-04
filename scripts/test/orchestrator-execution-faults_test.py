import concurrent.futures
import http.client
import importlib
import json
import os
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from unittest.mock import patch

FAULTS = importlib.import_module("scripts.test.orchestrator-execution-faults")
ONLINE = importlib.import_module("scripts.test.orchestrator-execution-online")
PARENT = "00000000-0000-0000-0000-000000000001"
CHILD = "00000000-0000-0000-0000-000000000002"


class OwnerHandler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_POST(self):
        self.rfile.read(int(self.headers.get("Content-Length", "0")))
        self.server.posts += 1
        self.respond(202, {"execution_id": CHILD})

    def do_GET(self):
        self.server.gets += 1
        self.respond(200, {"execution_id": CHILD, "status": "success"})

    def respond(self, status, value):
        raw = json.dumps(value).encode()
        self.send_response(status)
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)


class FaultProxyTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-orchestrator-proxy-")
        self.addCleanup(self.temporary.cleanup)
        self.directory = Path(self.temporary.name)
        self.owner = ThreadingHTTPServer(("127.0.0.1", 0), OwnerHandler)
        self.owner.posts = 0
        self.owner.gets = 0
        self.proxy = FAULTS.FaultProxy(self.directory, self.owner.server_port)
        for server in (self.owner, self.proxy):
            threading.Thread(target=server.serve_forever, daemon=True).start()
            self.addCleanup(server.server_close)
            self.addCleanup(server.shutdown)
        self.secret = "never-record-this-bearer"

    def arm(self, mode):
        FAULTS.write_json(self.directory / "command.json", {"case_id": "case_one", "task_id": 7, "mode": mode})

    def request(self, method="POST", path="/api/v1/meta/task-provider/tasks/scan/7/execute", host=None):
        connection = http.client.HTTPConnection("127.0.0.1", self.proxy.server_port, timeout=5)
        try:
            target = f"http://{host or FAULTS.HOST}:{self.owner.server_port}{path}"
            connection.request(method, target, json.dumps({"parent_execution_id": PARENT}),
                               {"Authorization": "Bearer " + self.secret, "Content-Type": "application/json"})
            response = connection.getresponse()
            return response.status, response.read()
        finally:
            connection.close()

    def test_response_is_lost_only_after_real_http_acceptance_without_secret_evidence(self):
        self.arm("lose_response")
        with self.assertRaises(http.client.RemoteDisconnected):
            self.request()
        self.assertEqual(self.owner.posts, 1)
        witness = json.loads((self.directory / "witness.json").read_text())
        self.assertEqual(witness["posts"], 1)
        self.assertEqual(witness["parent_execution_id"], PARENT)
        self.assertEqual(witness["child_execution_ids"], [CHILD])
        self.assertNotIn("error", witness)
        self.assertNotIn(self.secret, "".join(p.read_text() for p in self.directory.iterdir()))

    def test_status_barrier_releases_without_blocking_other_owner_reads(self):
        self.arm("hold_status")
        self.assertEqual(self.request()[0], 202)
        with concurrent.futures.ThreadPoolExecutor() as executor:
            waiting = executor.submit(self.request, "GET", "/api/v1/meta/task-provider/executions/" + CHILD)
            for _ in range(50):
                if self.proxy.witness.get("held_status_requests"):
                    break
                time.sleep(0.02)
            self.assertEqual(self.proxy.witness.get("held_status_requests"), 1)
            self.assertFalse(waiting.done())
            self.assertEqual(self.request("GET", "/api/v1/meta/execution-read-scope")[0], 200)
            self.arm("pass")
            self.assertEqual(waiting.result(timeout=2)[0], 200)
        self.assertEqual(self.owner.posts, 1)

    def test_other_task_and_unarmed_requests_are_transparent_but_replay_is_counted(self):
        self.assertEqual(self.request()[0], 202)
        self.arm("lose_response")
        self.assertEqual(self.request(path="/api/v1/meta/task-provider/tasks/scan/8/execute")[0], 202)
        for _ in range(2):
            with self.assertRaises(http.client.RemoteDisconnected):
                self.request()
        self.assertEqual(self.proxy.witness["posts"], 2)
        self.assertEqual(len(self.proxy.witness["child_execution_ids"]), 2)

    def test_renewal_barrier_proves_client_cancellation_without_forwarding_status(self):
        self.arm("hold_renewal")
        self.assertEqual(self.request()[0], 202)
        connection = http.client.HTTPConnection("127.0.0.1", self.proxy.server_port, timeout=2)
        connection.request("GET", f"http://{FAULTS.HOST}:{self.owner.server_port}/api/v1/meta/task-provider/executions/{CHILD}")
        for _ in range(100):
            if self.proxy.witness.get("held_status_requests"):
                break
            time.sleep(0.01)
        self.assertEqual(self.proxy.witness["held_status_requests"], 1)
        connection.close()
        for _ in range(100):
            if self.proxy.witness.get("cancelled_status_requests"):
                break
            time.sleep(0.01)
        self.assertEqual(self.proxy.witness["cancelled_status_requests"], 1)
        self.assertEqual(self.owner.gets, 0)

    def test_proxy_refuses_foreign_target_before_forwarding_authorization(self):
        self.assertEqual(self.request(host="unowned.example")[0], 403)
        self.assertEqual(self.owner.posts, 0)

    def test_scope_connection_fault_does_not_forge_response_or_interrupt_mutations(self):
        path = "/api/v1/meta/execution-read-scope"
        self.assertEqual(self.request("GET", path)[0], 200)
        self.arm("drop_scope")
        for _ in range(3):
            with self.assertRaises(http.client.RemoteDisconnected):
                self.request("GET", path)
        self.assertEqual(self.owner.gets, 1)
        self.assertEqual(self.proxy.witness["scope_drops"], 3)
        self.assertEqual(self.proxy.witness["posts"], 0)
        self.assertEqual(self.proxy.witness["child_execution_ids"], [])
        self.assertNotIn("error", self.proxy.witness)
        self.assertEqual(self.request("GET", "/api/v1/meta/task-provider/executions/" + CHILD)[0], 200)
        self.assertEqual(self.request()[0], 202)
        self.assertEqual(self.owner.posts, 1)
        self.arm("pass")
        self.assertEqual(self.request("GET", path)[0], 200)
        self.assertNotIn(self.secret, (self.directory / "witness.json").read_text())

    def test_invalid_parent_does_not_send_the_mutation(self):
        self.arm("lose_response")
        with patch.object(FAULTS, "UUID", new=type("RejectUUID", (), {"fullmatch": lambda *_: None})()):
            with self.assertRaises(http.client.RemoteDisconnected):
                self.request()
        self.assertEqual(self.owner.posts, 0)


class RenewalDatabaseOwnershipTest(unittest.TestCase):
    def setUp(self):
        self.faults = object.__new__(FAULTS.HostedFaults)
        self.faults.root, self.faults.secret = Path("/owned/repo"), Path("/owned/secret")
        self.faults.renewal_schema = None
        self.faults.renewal_process = None
        self.env = {"POSTGRES_USER": "fixture", "POSTGRES_PASSWORD": "private-password",
                    "ADDP_ONLINE_ORCHESTRATOR_POSTGRES_ID": "owned-id"}
        self.container = {"Id": "owned-id", "State": {"Running": True}, "Config": {
            "Labels": {"com.docker.compose.service": "postgres", "com.docker.compose.project.working_dir": "/owned/repo"},
            "Env": ["POSTGRES_DB=addp_online", "POSTGRES_USER=fixture", "POSTGRES_PASSWORD=private-password"]}}

    def test_personal_environment_or_foreign_container_cannot_execute_sql(self):
        with patch.object(FAULTS, "deployment_secret", side_effect=ValueError("personal")), \
                patch.object(FAULTS.subprocess, "run") as run:
            with self.assertRaises(ValueError):
                self.faults.postgres("SELECT 1")
            run.assert_not_called()
        for mutation in ("id", "database", "root", "service", "password", "stopped"):
            import copy
            container = copy.deepcopy(self.container)
            if mutation == "id": container["Id"] = "foreign"
            if mutation == "database": container["Config"]["Env"][0] = "POSTGRES_DB=addp"
            if mutation == "root": container["Config"]["Labels"]["com.docker.compose.project.working_dir"] = "/foreign"
            if mutation == "service": container["Config"]["Labels"]["com.docker.compose.service"] = "business"
            if mutation == "password": container["Config"]["Env"][2] = "POSTGRES_PASSWORD=foreign"
            if mutation == "stopped": container["State"]["Running"] = False
            with self.subTest(mutation=mutation), patch.dict(os.environ, self.env, clear=True), \
                    patch.object(FAULTS, "deployment_secret", return_value=self.faults.secret), \
                    patch.object(FAULTS.subprocess, "run", return_value=type("Result", (), {"stdout": json.dumps([container])})()) as run:
                with self.assertRaises(ValueError): self.faults.postgres("SELECT 1")
                self.assertEqual(run.call_count, 1)  # inspect only, never docker exec

    def test_sql_uses_verified_container_id_and_stdin_without_credential_arguments(self):
        results = [type("Result", (), {"stdout": json.dumps([self.container])})(), type("Result", (), {"stdout": "1\n"})()]
        with patch.dict(os.environ, self.env, clear=True), \
                patch.object(FAULTS, "deployment_secret", return_value=self.faults.secret), \
                patch.object(FAULTS.subprocess, "run", side_effect=results) as run:
            self.assertEqual(self.faults.postgres("SELECT 1;"), "1")
            command = run.call_args.args[0]
            self.assertEqual(command[:4], ["docker", "exec", "-i", "owned-id"])
            self.assertEqual(run.call_args.kwargs["input"], "SELECT 1;")
            self.assertNotIn("private-password", str(run.call_args))

    def test_invalid_target_is_rejected_and_cleanup_failure_remains_pending(self):
        with patch.object(self.faults, "postgres") as execute, \
                patch.object(self.faults, "pin_process", return_value=(1234, 71)), patch.object(FAULTS.os, "close"):
            for parent, tenant, task in (("bad", 42, 7), (PARENT, 1, 7), (PARENT, 42, 0)):
                with self.assertRaises(ValueError): self.faults.reject_renewal(parent, tenant, task)
            execute.assert_not_called()
            self.faults.reject_renewal(PARENT, 42, 7)
            schema = self.faults.renewal_schema
            execute.side_effect = ["", "1"]
            with self.assertRaises(ValueError): self.faults.clear_renewal()
            self.assertEqual(self.faults.renewal_schema, schema)
            execute.side_effect = ["", "0"]
            self.faults.clear_renewal()
            self.assertIsNone(self.faults.renewal_schema)

    def test_renewal_observes_pinned_process_exit_without_sending_a_signal(self):
        for stopped in (True, False):
            self.faults.renewal_process, self.faults.exited_pid = (1234, 71), None
            with self.subTest(stopped=stopped), patch.object(FAULTS.select, "select", return_value=([71] if stopped else [], [], [])), \
                    patch.object(FAULTS.os, "close") as close, \
                    patch.object(FAULTS.signal, "pidfd_send_signal", create=True) as send:
                if stopped:
                    self.faults.await_renewal_exit()
                    self.assertEqual(self.faults.exited_pid, 1234)
                else:
                    with self.assertRaises(ValueError): self.faults.await_renewal_exit()
                    self.assertIsNone(self.faults.exited_pid)
                send.assert_not_called()
                close.assert_called_once_with(71)
                self.assertIsNone(self.faults.renewal_process)


class FaultOwnershipTest(unittest.TestCase):
    def test_personal_environment_is_rejected_before_any_mutation(self):
        with patch.dict(os.environ, {}, clear=True), patch.object(FAULTS.subprocess, "run") as run:
            with self.assertRaises(ValueError):
                FAULTS.deployment_secret()
            run.assert_not_called()

    def test_alias_cleanup_preserves_unrelated_entries_and_cannot_remove_foreign_alias(self):
        with tempfile.TemporaryDirectory() as temporary:
            hosts = Path(temporary) / "hosts"
            original = "127.0.0.1 localhost\n192.0.2.2 existing.test\n"
            hosts.write_text(original)
            actual_run = subprocess.run

            def isolated_run(command, **kwargs):
                self.assertEqual(command[:4], ["sudo", "-n", "python3", "-c"])
                program = command[4].replace("'/etc/hosts'", repr(str(hosts)))
                return actual_run([sys.executable, "-c", program, *command[5:]], **kwargs)

            with patch.object(FAULTS.subprocess, "run", side_effect=isolated_run):
                secret = Path(temporary) / "addp-online-secret-this-run"
                FAULTS.network_alias(secret, "add")
                self.assertIn(FAULTS.HOST, hosts.read_text())
                FAULTS.network_alias(secret, "remove")
                self.assertEqual(hosts.read_text(), original)
                foreign = original + "127.0.0.1 " + FAULTS.HOST + " # other-owner\n"
                hosts.write_text(foreign)
                with self.assertRaises(subprocess.CalledProcessError):
                    FAULTS.network_alias(secret, "add")
                FAULTS.network_alias(secret, "remove")
                self.assertEqual(hosts.read_text(), foreign)

    def test_process_proof_requires_exact_executable_uid_and_deployment(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            executable = root / ".dev-bins/addp-orchestrator"
            executable.parent.mkdir()
            executable.touch()
            process = root / "process"
            process.mkdir()
            (process / "exe").symlink_to(executable)
            secret = root / "addp-online-secret-owned"
            values = {"ADDP_ONLINE_SECRET_DIR": str(secret), "POSTGRES_DB": "addp_online", "ADDP_ONLINE_HOSTED": "1"}

            def environment(value):
                (process / "environ").write_bytes("\0".join(f"{k}={v}" for k, v in value.items()).encode())

            environment(values)
            FAULTS.validate_process(process, root, secret)
            for key, wrong in (("ADDP_ONLINE_SECRET_DIR", "/different-run"), ("POSTGRES_DB", "addp"), ("ADDP_ONLINE_HOSTED", "0")):
                environment({**values, key: wrong})
                with self.assertRaises(ValueError):
                    FAULTS.validate_process(process, root, secret)
            environment(values)
            with patch.object(FAULTS.os, "getuid", return_value=os.getuid() + 1):
                with self.assertRaises(ValueError):
                    FAULTS.validate_process(process, root, secret)
            (process / "exe").unlink()
            (process / "exe").symlink_to(sys.executable)
            with self.assertRaises(ValueError):
                FAULTS.validate_process(process, root, secret)

    def test_signal_uses_pinned_pidfd_and_closes_it_when_ownership_is_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / ".dev-pids").mkdir()
            (root / ".dev-pids/orchestrator.pid").write_text("1234")
            faults = object.__new__(FAULTS.HostedFaults)
            faults.root, faults.secret = root, root / "secret"
            with patch.object(FAULTS.os, "pidfd_open", return_value=71, create=True), \
                    patch.object(FAULTS.os, "close") as close, \
                    patch.object(FAULTS.signal, "pidfd_send_signal", create=True) as send, \
                    patch.object(FAULTS, "validate_process", side_effect=ValueError("unowned")):
                with self.assertRaises(ValueError):
                    faults.crash()
                send.assert_not_called()
                close.assert_called_once_with(71)
            with patch.object(FAULTS.os, "pidfd_open", return_value=72, create=True), \
                    patch.object(FAULTS.os, "close") as close, \
                    patch.object(FAULTS.signal, "pidfd_send_signal", create=True) as send, \
                    patch.object(FAULTS, "validate_process"), \
                    patch.object(FAULTS.select, "select", return_value=([72], [], [])):
                faults.crash()
                send.assert_called_once_with(72, FAULTS.signal.SIGKILL)
                close.assert_called_once_with(72)
                self.assertEqual(faults.exited_pid, 1234)

    def test_replacement_requires_proven_crash_and_a_different_owned_process(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / ".dev-pids").mkdir()
            (root / ".dev-pids/orchestrator.pid").write_text("1234")
            faults = object.__new__(FAULTS.HostedFaults)
            faults.root, faults.secret, faults.exited_pid = root, root / "secret", None
            with patch.object(FAULTS.subprocess, "run") as run:
                with self.assertRaises(ValueError):
                    faults.restart()
                run.assert_not_called()
                faults.exited_pid = 1234
                with patch.dict(os.environ, {"ADDP_ONLINE_ORCHESTRATOR_PROXY_URL": "http://127.0.0.1:18882",
                                            "ADDP_ONLINE_ARTIFACT_DIR": str(root)}):
                    with self.assertRaises(ValueError):
                        faults.restart()
                    self.assertEqual(run.call_args.args[0], ["bash", "scripts/dev/start.sh", "-orchestrator"])
                    (root / ".dev-pids/orchestrator.pid").write_text("1235")
                    with patch.object(FAULTS, "validate_process") as validate:
                        faults.restart()
                        validate.assert_called_once_with(Path("/proc/1235"), root, root / "secret")


class FaultScenarioTest(unittest.TestCase):
    def scenario(self, mode, corrupt=None):
        class Controller:
            crashed = restarted = released = injected = False

            def arm(self, *args):
                pass

            def witness(self):
                return {"posts": 2 if corrupt == "replay" else 1, "parent_execution_id": PARENT, "child_execution_ids": [CHILD], "held_status_requests": 1,
                        "cancelled_status_requests": 0 if corrupt == "request_alive" else 1}

            def reject_renewal(self, *args):
                self.injected = True

            def await_renewal_exit(self):
                pass

            def renewal_rejections(self):
                return 0 if corrupt == "not_rejected" else 1

            def crash(self):
                self.crashed = True

            def restart(self):
                self.restarted = True

            def release(self):
                self.released = True

        controller = Controller()
        code = {"hold_status": "orchestrator.execution.lease_expired", "hold_renewal": "orchestrator.execution.coordinator_stopped", "lose_response": "orchestrator.execution.dispatch_uncertain"}[mode]
        # An unknown dispatch outcome leaves Go StepResult.Result nil, serialized as JSON null.
        steps = {"probe": {"status": "running" if mode in {"hold_status", "hold_renewal"} else "failed", "phase": "waiting" if mode in {"hold_status", "hold_renewal"} else "terminal", "result": None}}
        if mode in {"hold_status", "hold_renewal"} or corrupt == "invented_child":
            steps["probe"]["result"] = {"execution_id": CHILD}
        if corrupt == "malformed_result":
            steps["probe"]["result"] = []
        terminal = {"execution_id": PARENT, "status": "failed", "progress": 0, "attempt": 1,
                    "error_details": {"code": code}, "metadata": {"step_results": steps}}
        safe_step = {"id": "probe", "status": steps["probe"]["status"], "phase": steps["probe"]["phase"]}
        parent = {"execution_id": PARENT, "status": "failed", "progress": 0, "error_details": {"code": code}, "steps": [safe_step]}
        if corrupt == "stale_parent":
            parent["status"] = "running"
        child = {"execution_id": CHILD, "parent_execution_id": PARENT, "module": "meta", "status": "success"}
        if corrupt == "fake_cancel":
            child["status"] = "cancelled"
        if corrupt == "private":
            child["execution_config"] = {"secret": "never-report"}
        tree = {"execution": parent, "truncated": False, "children": [{"execution": child, "truncated": False, "children": []}]}

        class Client:
            def request(self, method, path, expected):
                if path.endswith("/tree"):
                    return ONLINE.API.Response(200, tree)
                if mode == "hold_renewal" and controller.injected:
                    return ONLINE.API.Response(200, {**terminal, "progress": 100} if corrupt == "late_write" else terminal)
                return ONLINE.API.Response(200, {**terminal, "status": "running"})

        report = {"checks": [], "tenant_id": "42"}
        with patch.object(ONLINE, "wait_execution", return_value=terminal), \
                patch.object(ONLINE, "assert_events", return_value=3), \
                patch.object(ONLINE, "read_events", return_value=[]), \
                patch.object(ONLINE.time, "sleep"), \
                patch.object(ONLINE.time, "monotonic", side_effect=range(1000)):
            ONLINE.run_fault_case(Client(), lambda *_args, **_kwargs: (PARENT, {}), 8, 7,
                                  "renewal_failure" if mode == "hold_renewal" else "process_crash" if mode == "hold_status" else "response_lost", mode, controller, report)
        self.assertTrue(controller.released)
        return controller, report

    def test_response_loss_and_crash_require_real_child_and_never_invent_step_state(self):
        for mode in ("lose_response", "hold_status"):
            with self.subTest(mode=mode):
                controller, report = self.scenario(mode)
                self.assertEqual(controller.crashed, mode == "hold_status")
                self.assertEqual(controller.restarted, mode == "hold_status")
                self.assertEqual(report["faults"][0]["child_execution_id"], CHILD)
                self.assertEqual(report["faults"][0]["posts"], 1)

    def test_renewal_proves_database_rejection_cancellation_and_stable_terminal(self):
        controller, report = self.scenario("hold_renewal")
        self.assertTrue(controller.injected)
        self.assertFalse(controller.crashed)
        self.assertTrue(controller.restarted)
        self.assertEqual(report["faults"][0]["renewal_rejections"], 1)
        self.assertEqual(report["faults"][0]["fault_cleanup"], "passed")
        self.assertEqual(len(report["checks"]), 4)

    def test_renewal_rejects_unproven_fault_live_request_late_write_and_replay(self):
        for corrupt in ("not_rejected", "request_alive", "late_write", "replay", "private", "fake_cancel", "stale_parent"):
            with self.subTest(corrupt=corrupt), self.assertRaises(ONLINE.SuiteError):
                self.scenario("hold_renewal", corrupt)

    def test_fault_checks_reject_replay_private_data_fake_cancellation_and_invented_identity(self):
        for corrupt in ("replay", "private", "fake_cancel", "invented_child", "stale_parent", "malformed_result"):
            with self.subTest(corrupt=corrupt):
                with self.assertRaises(ONLINE.SuiteError):
                    self.scenario("lose_response", corrupt)


if __name__ == "__main__":
    unittest.main()
