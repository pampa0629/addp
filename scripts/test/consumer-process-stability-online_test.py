import importlib.util
import sys
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("consumer-process-stability-online.py")
SPEC = importlib.util.spec_from_file_location("consumer_process_stability_online", SCRIPT)
STABILITY = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = STABILITY
SPEC.loader.exec_module(STABILITY)


class ConsumerProcessStabilityOnlineTest(unittest.TestCase):
    @staticmethod
    def processes() -> dict[str, dict[str, object]]:
        return {
            name: {"pid": index + 100, "command": f"addp-{name}"}
            for index, name in enumerate(STABILITY.PROCESS_NAMES)
        }

    def test_accepts_the_same_managed_processes(self) -> None:
        processes = self.processes()
        STABILITY.verify_processes(processes, processes)

    def test_rejects_pid_or_command_replacement(self) -> None:
        expected = self.processes()
        current = self.processes()
        current["manager"] = {"pid": 999, "command": "addp-manager"}

        with self.assertRaisesRegex(STABILITY.StabilityError, "manager"):
            STABILITY.verify_processes(expected, current)

        current = self.processes()
        current["service-frontend"] = {
            "pid": expected["service-frontend"]["pid"],
            "command": "replacement-process",
        }
        with self.assertRaisesRegex(STABILITY.StabilityError, "service-frontend"):
            STABILITY.verify_processes(expected, current)


if __name__ == "__main__":
    unittest.main()
