import importlib.util
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


SCRIPT = Path(__file__).with_name("release-gate.py")
SPEC = importlib.util.spec_from_file_location("release_gate", SCRIPT)
RELEASE_GATE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = RELEASE_GATE
SPEC.loader.exec_module(RELEASE_GATE)


class DeploymentPackageTest(unittest.TestCase):
    def test_registry_package_preserves_release_and_rejects_missing_image_before_infra(self) -> None:
        repository = SCRIPT.parents[2]
        inputs = subprocess.check_output(
            ['git', 'ls-files', '-z', '--', '.env.example', 'docker-compose.yml',
             'docker-compose.infra.yml', 'docker-compose.runtimes.yml', 'scripts/build/package.sh',
             'scripts/infra', 'scripts/prod', 'scripts/dev', 'scripts/utils', 'common', 'nginx/nginx.conf',
             'business/.env.example', 'business/docker-compose.yml', 'gateway/go.mod',
             ':(glob)*/backend/go.mod', 'monitor/backend/go.sum', 'monitor/backend/cmd/minio-metrics-source',
             'monitor/backend/cmd/redis-metrics-source', 'monitor/backend/cmd/cdc-metrics-source',
             'monitor/backend/internal/cdcsource', 'monitor/backend/internal/miniosource',
             'monitor/backend/internal/redissource', 'monitor/backend/internal/sourceserver'], cwd=repository,
        )
        with tempfile.TemporaryDirectory(prefix='addp-package-') as directory:
            root = Path(directory)
            names = os.fsdecode(inputs).split('\0') + ['scripts/prod/release-images.py']
            for name in names:
                # The working tree may delete tracked sources before publication.
                # Preserve dangling-link errors while excluding actual deletions.
                if not name or not os.path.lexists(repository / name):
                    continue
                target = root / name
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(repository / name, target)
            env = {'PATH': os.environ['PATH']}
            spec = importlib.util.spec_from_file_location('package_release_images', root / 'scripts/prod/release-images.py')
            release = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(release)
            prepared = root / 'dist/compose-release'
            prepared.mkdir(parents=True)
            reference = 'localhost:5001/addp-fixture@sha256:' + 'a' * 64
            projects = {name: {name + '-service': reference} for name in release.COMPOSE_FILES}
            manifest = {'schema': 'addp.compose-release/v1', 'source_commit': 'b' * 40,
                        'build_root': str(root), 'platform': 'linux/arm64', 'version': 'fixture',
                        'registry': 'localhost:5001', 'selection': release.selection({}),
                        'inputs': release.compose_inputs(), 'projects': projects, 'sources': {}, 'image_ids': {reference: 'sha256:' + 'c' * 64}}
            (prepared / 'manifest.json').write_text(json.dumps(manifest))
            for project, services in projects.items():
                release.write_override(prepared / (project + '-images.yml'), {name: manifest['image_ids'][ref] for name, ref in services.items()})
            package = subprocess.run(['bash', 'scripts/build/package.sh', '--mode', 'registry'], cwd=root,
                                     env=env, capture_output=True, text=True, timeout=60)
            self.assertEqual(package.returncode, 0, package.stderr)
            output, = (root / 'dist').glob('package-registry-*')
            self.assertFalse((output / '.env').exists())
            self.assertEqual(json.loads((output / 'dist/compose-release/manifest.json').read_text()), manifest)
            host_entry = output / 'scripts/infra/host-metrics.sh'
            result = subprocess.run([host_entry, 'check'], cwd=root, env=env,
                                    capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 1, result.stderr)
            self.assertIn('配置读取失败', result.stdout)

            # Missing immutable images stop before the Infra startup boundary.
            # No command from this package can reach the host Docker daemon.
            stub = root / 'bin' / 'docker'
            stub.parent.mkdir()
            stub.write_text('''#!/bin/sh
printf '%s\n' "$*" >> "$PACKAGE_TEST_BOUNDARY"
if [ "$1" = info ]; then
    printf '{"OSType":"linux","Architecture":"aarch64"}\n'
    exit 0
fi
exit 1
''')
            stub.chmod(0o755)
            boundary = root / 'infra-docker-boundary'
            result = subprocess.run(['bash', 'scripts/prod/start.sh'], cwd=output,
                                    env={'PATH': f"{stub.parent}{os.pathsep}{env['PATH']}",
                                         'PACKAGE_TEST_BOUNDARY': str(boundary)},
                                    capture_output=True, text=True, timeout=30)
            self.assertTrue(boundary.is_file(), result.stdout + result.stderr)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('pull ' + reference, boundary.read_text())
            self.assertNotIn('compose up', boundary.read_text())
            self.assertNotIn('build', boundary.read_text())
            self.assertEqual((output / '.env').stat().st_mode & 0o777, 0o600)
            self.assertFalse((output / '.dev-state/lifecycle.lock').exists())
            result = subprocess.run([host_entry, 'check'], cwd=root, env=env,
                                    capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn('共 1 项，失败 0 项', result.stdout)

            for entry in ('scripts/utils/deployment-inventory.py', 'scripts/infra/node-metrics.py',
                          'scripts/infra/redis-metrics.py', 'scripts/infra/minio-observation.py'):
                result = subprocess.run([sys.executable, entry, '--help'], cwd=output,
                                        env=env, capture_output=True, text=True, timeout=10)
                self.assertEqual(result.returncode, 0, result.stderr)
            for source in ('common/cmd/runtime-log/main.go', 'gateway/go.mod', 'system/backend/go.mod',
                           'monitor/backend/cmd/minio-metrics-source/main.go',
                           'monitor/backend/cmd/redis-metrics-source/main.go'):
                self.assertTrue((output / source).is_file(), source)


class ReleaseGateTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-release-gate-")
        self.root = Path(self.temporary.name)
        self.repository = self.root / "repository"
        self.repository.mkdir()
        self.artifacts = self.root / "artifacts"
        self.environment = {
            "ADDP_RELEASE_ARTIFACT_DIR": str(self.artifacts),
            "ADDP_RELEASE_TEST_SECRET": "never-persist-this-secret",
        }
        self.suites = {
            "sample-product": RELEASE_GATE.Suite(
                target="test-sample-release",
                artifact_environment=(("SAMPLE_REPORT", "sample.json"),),
                owner_report="sample.json",
            )
        }

    def tearDown(self) -> None:
        self.temporary.cleanup()

    @patch.object(RELEASE_GATE.time, "monotonic", side_effect=(10.0, 12.5))
    @patch.object(RELEASE_GATE.subprocess, "run")
    def test_dispatches_owner_target_and_writes_report(self, run, _monotonic) -> None:
        def execute(command, *, cwd, env, check):
            self.assertEqual(command, ("make", "--no-print-directory", "test-sample-release"))
            self.assertEqual(cwd, self.repository)
            self.assertFalse(check)
            Path(env["SAMPLE_REPORT"]).write_text('{"result":"passed"}\n', encoding="utf-8")
            self.assertEqual(env["ADDP_RELEASE_SUITE"], "sample-product")
            return subprocess.CompletedProcess(command, 0)

        run.side_effect = execute
        report = RELEASE_GATE.run_release(
            "sample-product", self.repository, self.environment, self.suites
        )

        self.assertEqual(report["result"], "passed")
        self.assertEqual(report["duration_ms"], 2500)
        self.assertEqual(report["artifacts"], ["sample.json"])
        persisted = json.loads(
            (self.artifacts / "release-report.json").read_text(encoding="utf-8")
        )
        persisted_text = (self.artifacts / "release-report.json").read_text(
            encoding="utf-8"
        )
        self.assertEqual(persisted["schema_version"], "addp.release-gate/v1")
        self.assertEqual(persisted["owner_report"], "sample.json")
        self.assertNotIn("never-persist-this-secret", persisted_text)
        summary = (self.artifacts / "release-summary.md").read_text(encoding="utf-8")
        self.assertIn("| sample-product | `test-sample-release` | passed |", summary)

    @patch.object(RELEASE_GATE.time, "monotonic", side_effect=(1.0, 2.0))
    @patch.object(
        RELEASE_GATE.subprocess,
        "run",
        return_value=subprocess.CompletedProcess(("make",), 7),
    )
    def test_owner_failure_still_writes_failed_report(self, _run, _monotonic) -> None:
        with self.assertRaisesRegex(RELEASE_GATE.ReleaseOwnerGateError, "status 7"):
            RELEASE_GATE.run_release(
                "sample-product", self.repository, self.environment, self.suites
            )
        report = json.loads(
            (self.artifacts / "release-report.json").read_text(encoding="utf-8")
        )
        self.assertEqual(report["result"], "failed")
        self.assertEqual(report["error_code"], "release_owner_gate_failed")

    @patch.object(RELEASE_GATE.time, "monotonic", side_effect=(1.0, 2.0))
    @patch.object(
        RELEASE_GATE.subprocess,
        "run",
        return_value=subprocess.CompletedProcess(("make",), 0),
    )
    def test_missing_owner_report_fails_release_suite(self, _run, _monotonic) -> None:
        with self.assertRaisesRegex(
            RELEASE_GATE.ReleaseOwnerEvidenceError,
            "did not produce owner report",
        ):
            RELEASE_GATE.run_release(
                "sample-product", self.repository, self.environment, self.suites
            )
        report = json.loads(
            (self.artifacts / "release-report.json").read_text(encoding="utf-8")
        )
        self.assertEqual(report["result"], "failed")
        self.assertEqual(report["error_code"], "release_owner_evidence_missing")

    def test_registers_only_current_t5_suites(self) -> None:
        self.assertEqual(
            set(RELEASE_GATE.SUITES),
            {
                "agent-evaluation",
                "common-python-cli",
                "dameng-official-media",
                "kingbase-official-media",
                "opengauss-official-media",
                "gaussdb-official-media",
            },
        )
        self.assertEqual(
            RELEASE_GATE.SUITES["agent-evaluation"].target,
            "test-agent-eval-release",
        )
        self.assertEqual(
            RELEASE_GATE.SUITES["common-python-cli"].target,
            "test-common-python-cli-release",
        )
        self.assertEqual(
            RELEASE_GATE.SUITES["dameng-official-media"].target,
            "test-dameng-official-media-release",
        )
        self.assertIsNone(
            RELEASE_GATE.SUITES["dameng-official-media"].workflow_job,
        )
        self.assertEqual(
            RELEASE_GATE.SUITES["opengauss-official-media"].target,
            "test-opengauss-official-media-release",
        )
        self.assertEqual(
            RELEASE_GATE.SUITES["opengauss-official-media"].workflow_job,
            "opengauss-official-media-certification",
        )
        self.assertEqual(
            RELEASE_GATE.SUITES["kingbase-official-media"].target,
            "test-kingbase-official-media-release",
        )
        self.assertIsNone(RELEASE_GATE.SUITES["kingbase-official-media"].workflow_job)

    def test_rejects_unknown_suite(self) -> None:
        with self.assertRaisesRegex(RELEASE_GATE.ReleaseGateError, "registered suites"):
            RELEASE_GATE.run_release(
                "missing", self.repository, self.environment, self.suites
            )

    def test_rejects_relative_artifact_directory(self) -> None:
        self.environment["ADDP_RELEASE_ARTIFACT_DIR"] = "artifacts"
        with self.assertRaisesRegex(RELEASE_GATE.ReleaseGateError, "absolute"):
            RELEASE_GATE.run_release(
                "sample-product", self.repository, self.environment, self.suites
            )

    def test_rejects_artifact_directory_inside_repository(self) -> None:
        self.environment["ADDP_RELEASE_ARTIFACT_DIR"] = str(
            self.repository / "artifacts"
        )
        with self.assertRaisesRegex(RELEASE_GATE.ReleaseGateError, "outside"):
            RELEASE_GATE.run_release(
                "sample-product", self.repository, self.environment, self.suites
            )

    def test_rejects_non_empty_artifact_directory(self) -> None:
        self.artifacts.mkdir()
        (self.artifacts / "stale.whl").write_text("stale", encoding="utf-8")
        with self.assertRaisesRegex(RELEASE_GATE.ReleaseGateError, "empty"):
            RELEASE_GATE.run_release(
                "sample-product", self.repository, self.environment, self.suites
            )


if __name__ == "__main__":
    unittest.main()
