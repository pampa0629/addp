import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "business/scripts/k3s-kingbase-trial.sh"


class KingbaseK3sTrialTest(unittest.TestCase):
    def run_script(self, *args, env=None):
        return subprocess.run(["bash", str(SCRIPT), *args], capture_output=True, text=True, env=env, timeout=10)

    def test_render_preserves_the_live_storage_and_shutdown_contract(self):
        result = self.run_script("render", "test-business", "native-amd64.site", "private-kingbase")
        self.assertEqual(result.returncode, 0, result.stderr)
        manifest = result.stdout
        self.assertIn("kubernetes.io/arch: amd64", manifest)
        self.assertIn('kubernetes.io/hostname: "native-amd64.site"', manifest)
        self.assertIn("imagePullPolicy: Never", manifest)
        self.assertIn("type: Recreate", manifest)
        self.assertIn("mountPath: /home/kingbase/userdata", manifest)
        self.assertIn('export KINGBASE_PASSWORD="$DB_PASSWORD"', manifest)
        self.assertIn('name: "private-kingbase"', manifest)
        self.assertIn('"-m", "fast", "-w", "-t", "45", "stop"', manifest)
        self.assertIn("terminationGracePeriodSeconds: 60", manifest)
        self.assertNotIn("NodePort", manifest)

    def test_render_rejects_invalid_names(self):
        self.assertNotEqual(self.run_script("render", "test-business", "node\nkind: Secret", "auth").returncode, 0)

    def test_verify_is_read_only_and_checks_deployment_ownership(self):
        with tempfile.TemporaryDirectory() as directory:
            kubectl = Path(directory) / "kubectl"
            kubectl.write_text(
                '#!/bin/bash\ncase "$3" in\n'
                'get) printf "%s" "$TEST_PURPOSE" ;;\n'
                'rollout) exit 0 ;;\n'
                'exec) cat ;;\n'
                '*) exit 1 ;;\nesac\n'
            )
            kubectl.chmod(0o700)
            env = dict(os.environ, PATH=f"{directory}:{os.environ['PATH']}", TEST_PURPOSE="unknown")
            wrong = self.run_script("verify", "test-business", env=env)
            self.assertNotEqual(wrong.returncode, 0)
            self.assertNotIn("get_license_validdays", wrong.stdout)
            env["TEST_PURPOSE"] = "vendor-trial-technical-evaluation"
            verified = self.run_script("verify", "test-business", env=env)
            self.assertEqual(verified.returncode, 0, verified.stderr)
            self.assertIn("get_license_validdays", verified.stdout)
            self.assertNotIn("CREATE DATABASE", verified.stdout)
            self.assertNotIn("GRANT ", verified.stdout)
            initialized = self.run_script("initialize", "test-business", env=env)
            self.assertEqual(initialized.returncode, 0, initialized.stderr)
            self.assertIn("ON CONFLICT", initialized.stdout)
            self.assertIn("to_regclass", initialized.stdout)
            self.assertIn("admin -d business -1 -f -", initialized.stdout)


if __name__ == "__main__":
    unittest.main()
