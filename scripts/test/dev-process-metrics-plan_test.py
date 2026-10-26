import importlib
import json
import os
from pathlib import Path
import shlex
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

LAUNCH = importlib.import_module("scripts.dev.process-metrics-launch")
ROOT = Path(__file__).resolve().parents[2]


def deployment(module, role, port):
    return {"module_name": module, "role": role, "listen": f"127.0.0.1:{port}",
            "endpoint": f"https://127.0.0.1:{port}/metrics", "tls_dir": f"/external/source-{port}"}


class ProcessMetricsPlanTest(unittest.TestCase):
    def test_cgroup_observation_is_an_explicit_per_unit_boolean(self):
        for value in (True, False):
            plan = {"meta-worker-1": dict(deployment("meta", "worker", 19106), cgroup_v2=value)}
            result = LAUNCH.select_deployment(json.dumps(plan), "meta-worker-1", "meta", "worker")
            self.assertIs(json.loads(result)[0]["cgroup_v2"], value)
        for value in (None, "true", 1, [], {}):
            plan = {"meta-worker-1": dict(deployment("meta", "worker", 19106), cgroup_v2=value)}
            with self.assertRaises(ValueError):
                LAUNCH.select_deployment(json.dumps(plan), "meta-worker-1", "meta", "worker")

    def setUp(self):
        self.plan = {
            "meta": deployment("meta", "backend", 19104),
            "meta-worker-1": deployment("meta", "worker", 19106),
            "transfer-bounded-worker-1": deployment("transfer", "worker", 19107),
            "transfer-continuous-worker-1": deployment("transfer", "worker", 19108),
        }
        self.raw = json.dumps(self.plan)

    def test_each_launch_unit_receives_only_its_own_sdk_deployment(self):
        for unit, entry in self.plan.items():
            result = LAUNCH.select_deployment(self.raw, unit, entry["module_name"], entry["role"])
            self.assertEqual(json.loads(result), [entry])
        self.assertEqual(LAUNCH.select_deployment(self.raw, "monitor", "monitor", "backend"), "")
        self.assertEqual(LAUNCH.select_deployment("", "meta-worker-1", "meta", "worker"), "")
        self.assertEqual(LAUNCH.select_deployment("{}", "meta-worker-1", "meta", "worker"), "")

    def test_future_same_type_units_do_not_share_a_deployment(self):
        # Selection can isolate units; this does not add replica startup.
        plan = {"meta-worker-1": deployment("meta", "worker", 19106),
                "meta-worker-2": deployment("meta", "worker", 19109)}
        for unit, entry in plan.items():
            self.assertEqual(json.loads(LAUNCH.select_deployment(json.dumps(plan), unit, "meta", "worker")), [entry])

    def test_binding_must_match_the_current_formal_module_and_role(self):
        for module, role in (("meta", "worker"), ("transfer", "backend")):
            with self.subTest(module=module, role=role), self.assertRaises(ValueError):
                LAUNCH.select_deployment(self.raw, "meta", module, role)

    def test_duplicate_objects_and_trailing_content_are_rejected(self):
        entry = json.dumps(self.plan["meta"])
        for raw in ('{"meta":'+entry+',"meta":'+entry+'}',
                    '{"meta":'+entry[:-1]+',"role":"backend"}}',
                    self.raw+' {}', '[]', 'null', 'true', '{', '{"meta":null}'):
            with self.subTest(raw=raw), self.assertRaises(ValueError):
                LAUNCH.select_deployment(raw, "meta", "meta", "backend")

    def test_invalid_structure_anywhere_rejects_the_complete_plan(self):
        bad_entries = [dict(self.plan["meta"], extra="unused"),
                       dict(self.plan["meta"], tls_dir=""),
                       dict(self.plan["meta"], listen=None),
                       dict(self.plan["meta"], module_name="Bad Module"),
                       dict(self.plan["meta"], role="continuous-worker"),
                       dict(self.plan["meta"], tls_dir="a" * 1025),
                       {k: v for k, v in self.plan["meta"].items() if k != "role"}]
        for entry in bad_entries:
            with self.subTest(entry=entry), self.assertRaises(ValueError):
                LAUNCH.select_deployment(json.dumps({"other": entry}), "meta", "meta", "backend")
        for key in ("", "../meta", "meta worker", "a" * 101):
            with self.subTest(key=key), self.assertRaises(ValueError):
                LAUNCH.select_deployment(json.dumps({key: self.plan["meta"]}), "meta", "meta", "backend")

    def test_independent_sources_cannot_repeat_addresses_or_tls_directories(self):
        for field in ("listen", "endpoint", "tls_dir"):
            plan = json.loads(self.raw)
            plan["transfer-continuous-worker-1"][field] = plan["transfer-bounded-worker-1"][field]
            with self.subTest(field=field), self.assertRaises(ValueError):
                LAUNCH.select_deployment(json.dumps(plan), "meta", "meta", "backend")

    def test_plan_size_and_unit_count_are_bounded(self):
        large = {f"unit-{i}": deployment("meta", "worker", 20000+i) for i in range(1001)}
        with self.assertRaises(ValueError):
            LAUNCH.select_deployment(json.dumps(large), "meta", "meta", "backend")
        with self.assertRaises(ValueError):
            LAUNCH.select_deployment(' ' * (256 << 10) + '{}', "meta", "meta", "backend")
        with self.assertRaises(ValueError):
            LAUNCH.select_deployment('界' * (100 << 10), "meta", "meta", "backend")

    def run_receiver(self, unit, module, role, plan, inherited=""):
        with tempfile.TemporaryDirectory(prefix="addp-dev-process-metrics-") as folder:
            directory = Path(folder)
            output = directory / "received.json"
            receiver = directory / "receiver.py"
            receiver.write_text("""import json, os, sys
from pathlib import Path
Path(os.environ['TEST_OUTPUT']).write_text(json.dumps({
    'args': sys.argv[1:], 'pid': os.getpid(),
    'has_sdk': 'ADDP_PROCESS_METRICS_DEPLOYMENTS' in os.environ,
    'sdk': os.environ.get('ADDP_PROCESS_METRICS_DEPLOYMENTS'),
    'has_plan': 'ADDP_DEV_PROCESS_METRICS_PLAN' in os.environ,
    'business': os.environ.get('TEST_BUSINESS_VALUE')}))
""")
            # Execute the existing shell/interpreter, not a fresh executable
            # shebang file whose startup can consume the 10s deadline. The
            # shell reads the real action argument as its script name; $0
            # forwards that same argument so the exact argv assertion remains.
            (directory / "launch").write_text(
                "exec " + shlex.quote(sys.executable) + " "
                + shlex.quote(str(receiver)) + ' "$0" "$@"\n')
            env = dict(os.environ, ADDP_DEV_PROCESS_METRICS_PLAN=plan,
                       ADDP_PROCESS_METRICS_DEPLOYMENTS=inherited,
                       TEST_OUTPUT=str(output), TEST_BUSINESS_VALUE="unchanged")
            with subprocess.Popen([sys.executable, str(ROOT / "scripts/dev/process-metrics-launch.py"),
                                   unit, module, role, "/bin/sh", "/owned/program", "argument with spaces"],
                                  cwd=directory, env=env, stdout=subprocess.PIPE,
                                  stderr=subprocess.PIPE, text=True) as process:
                try:
                    stdout, stderr = process.communicate(timeout=10)
                finally:
                    if process.poll() is None:
                        process.kill()
                    process.communicate()
            self.assertEqual(process.returncode, 0, stderr)
            self.assertEqual(stdout, "")
            result = json.loads(output.read_text())
            self.assertEqual(result["pid"], process.pid)
            self.assertEqual(result["args"], ["launch", "--module", module, "--role", role,
                                             "--", "/owned/program", "argument with spaces"])
            self.assertTrue(result["has_sdk"])
            self.assertFalse(result["has_plan"])
            self.assertEqual(result["business"], "unchanged")
            return result, stderr

    def test_receiver_timeout_reaps_owned_process_and_closes_pipes(self):
        communicate = subprocess.Popen.communicate
        processes = []

        def timeout_once(process, *args, **kwargs):
            if not processes:
                processes.append(process)
                self.assertEqual(kwargs["timeout"], 10)
                raise subprocess.TimeoutExpired(process.args, kwargs["timeout"])
            return communicate(process, *args, **kwargs)

        try:
            with mock.patch.object(subprocess.Popen, "communicate", timeout_once):
                with self.assertRaises(subprocess.TimeoutExpired):
                    self.run_receiver("meta", "meta", "backend", self.raw)
            self.assertEqual(len(processes), 1)
            self.assertIsNotNone(processes[0].poll())
            self.assertTrue(processes[0].stdout.closed)
            self.assertTrue(processes[0].stderr.closed)
        finally:
            # Keep the regression itself isolated even if cleanup regresses.
            for process in processes:
                if process.poll() is None:
                    process.kill()
                communicate(process)

    def test_real_exec_preserves_receiver_pid_arguments_and_business_environment(self):
        for unit, entry in self.plan.items():
            result, stderr = self.run_receiver(unit, entry["module_name"], entry["role"], self.raw)
            self.assertEqual(json.loads(result["sdk"]), [entry])
            self.assertEqual(stderr, "")

    def test_unselected_and_disabled_sources_receive_explicit_empty_sdk_input(self):
        for plan in ("", "{}", self.raw):
            result, stderr = self.run_receiver("monitor", "monitor", "backend", plan)
            self.assertEqual(result["sdk"], "")
            self.assertEqual(stderr, "")

    def test_legacy_input_never_falls_back_and_business_still_executes(self):
        for plan in ("", self.raw):
            result, stderr = self.run_receiver("meta", "meta", "backend", plan, self.raw)
            self.assertEqual(result["sdk"], "")
            self.assertEqual(stderr.strip(), LAUNCH.INVALID)
            self.assertNotIn("/external", stderr)

    def test_bad_plan_and_binding_clear_metrics_but_keep_business_running(self):
        for raw, module, role in (("bad json with private data", "meta", "backend"),
                                  (self.raw, "transfer", "backend"),
                                  ("[" * 2000, "meta", "backend")):
            result, stderr = self.run_receiver("meta", module, role, raw)
            self.assertEqual(result["sdk"], "")
            self.assertEqual(stderr.strip(), LAUNCH.INVALID)

    def test_standard_entry_has_no_direct_go_receiver_launch_remaining(self):
        source = (ROOT / "scripts/dev/start.sh").read_text()
        self.assertNotRegex(source, r'addp-runtime-log" launch .* -- "\$\{PROJECT_ROOT\}/\.dev-bins/addp-')
        self.assertIn('process-metrics-launch.py" "$worker" "$module" worker', source)
        for unit, role in (("system", "backend"), ("meta", "backend"),
                           ("transfer", "backend"), ("monitor", "backend"), ("gateway", "ingress")):
            self.assertIn(f'process-metrics-launch.py" {unit} {unit} {role}', source)
        example = (ROOT / ".env.example").read_text()
        self.assertIn("ADDP_DEV_PROCESS_METRICS_PLAN=", example)
        self.assertNotRegex(example, r"(?m)^ADDP_PROCESS_METRICS_DEPLOYMENTS=")


if __name__ == "__main__":
    unittest.main()


class PythonSourcePlanTest(unittest.TestCase):
    def test_python_backends_use_the_same_launch_adapter(self):
        source=(ROOT/'scripts/dev/start.sh').read_text()
        for module in ('agent','copilot'):
            self.assertIn('process-metrics-launch.py" '+module+' '+module+' backend',source)
            entry=deployment(module,'backend',19444 if module=='agent' else 19445)
            self.assertEqual(json.loads(LAUNCH.select_deployment(json.dumps({module:entry}),module,module,'backend')),[entry])
