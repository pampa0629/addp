import hashlib
import importlib.util
import io
import json
import os
import re
import shutil
import struct
import subprocess
import sys
import tempfile
import unittest
from types import SimpleNamespace
from unittest import mock
import xml.etree.ElementTree as ET
import zlib
from pathlib import Path


SCRIPT = Path(__file__).parents[2] / "business/scripts/online-manager-minio-fixture.sh"


class ManagerPDFPhysicalFixtureTest(unittest.TestCase):
    def test_pdf_observer_requires_real_object_absence_and_preserves_source(self):
        path = SCRIPT.with_name('online-raster-minio-fixture.py')
        spec = importlib.util.spec_from_file_location('manager_pdf_fixture', path)
        module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
        class S3Error(Exception):
            code = 'NoSuchKey'
        class Stream(io.BytesIO):
            def release_conn(self): pass
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            config = {'pptx': {'endpoint': 'source', 'access_key': 'test', 'secret_key': 'test',
                'bucket': 'business', 'object': 'source.pptx', 'sha256': hashlib.sha256(b'source').hexdigest()},
                'infra': {'endpoint': 'infra', 'access_key': 'test', 'secret_key': 'test'}}
            (root / 'config.json').write_text(json.dumps(config))
            request = {'locator': 'addp-infra://minio/manager/tenant_7/document-preview/fp/slides.pdf?type=object',
                'tenant_id': 7, 'fingerprint': 'fp', 'size_bytes': 4}
            (root / 'manager-raster-request.json').write_text(json.dumps(request))
            present, source, residual = True, b'source', []
            def stat_object(bucket, key):
                if not present: raise S3Error()
                return SimpleNamespace(size=4)
            source_client = SimpleNamespace(get_object=lambda *args: Stream(source))
            infra_client = SimpleNamespace(stat_object=stat_object,
                get_object=lambda *args: Stream(b'%PDF'), list_objects=lambda *args, **kwargs: residual)
            sdk = SimpleNamespace(Minio=lambda endpoint, **kwargs: source_client if endpoint == 'source' else infra_client)
            with mock.patch.dict(sys.modules, {'minio': sdk, 'minio.error': SimpleNamespace(S3Error=S3Error)}):
                verified = module.manager_pdf_worker('pdf-verify', root / 'config.json')
                self.assertTrue(verified['object_present'])
                with self.assertRaisesRegex(module.FixtureError, 'still exists physically'):
                    module.manager_pdf_worker('pdf-deleted', root / 'config.json')
                present = False
                self.assertEqual(module.manager_pdf_worker('pdf-deleted', root / 'config.json'),
                    {'object_deleted': True, 'source_unchanged': True, 'residual_objects': 0})
                residual = [SimpleNamespace(object_name='unexpected')]
                with self.assertRaisesRegex(module.FixtureError, 'residual objects'):
                    module.manager_pdf_worker('pdf-deleted', root / 'config.json')
                source = b'changed'
                with self.assertRaisesRegex(module.FixtureError, 'source PPTX changed'):
                    module.manager_pdf_worker('pdf-deleted', root / 'config.json')
                request['tenant_id'] = 8
                (root / 'manager-raster-request.json').write_text(json.dumps(request))
                with self.assertRaisesRegex(module.FixtureError, 'not owned'):
                    module.manager_pdf_worker('pdf-deleted', root / 'config.json')


class OnlineManagerMinIOFixtureTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-online-manager-minio-")
        self.root = Path(self.temporary.name)
        self.business = self.root / "business"
        self.bin = self.root / "bin"
        self.state = self.root / "running"
        self.image = self.root / "image-built"
        self.log = self.root / "docker.log"
        (self.business / "scripts").mkdir(parents=True)
        (self.business / "nfs/data/点云").mkdir(parents=True)
        (self.business / "nfs/data/3d/stl/Print Light Gun/images").mkdir(parents=True)
        (self.business / "fixtures/manager").mkdir(parents=True)
        self.bin.mkdir()
        shutil.copy2(SCRIPT, self.business / "scripts/online-manager-minio-fixture.sh")
        (self.business / "docker-compose.yml").write_text("services: {}\n", encoding="utf-8")
        (self.business / "nfs/data/点云/pdal_las12_format0.las").write_bytes(b"LAS fixture")
        (self.business / "fixtures/manager/addp_online_preview_fixture.pptx").write_bytes(b"PPTX fixture")
        (self.business / "nfs/data/3d/stl/Print Light Gun/images/Autocop_4X3.jpg").write_bytes(b"JPEG fixture")
        self._executable("uname", '#!/bin/bash\nif [ "$1" = -m ]; then echo x86_64; else echo "${ADDP_TEST_OS:-Darwin}"; fi\n')
        self._executable(
            "curl",
            "#!/bin/bash\n[ -f \"$ADDP_TEST_CONTAINER_STATE\" ]\n",
        )
        self._executable(
            "docker",
            """#!/bin/bash
printf '%s|%s|%s|%s\n' "$*" "$MINIO_API_PORT" "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >> "$ADDP_TEST_DOCKER_LOG"
case "$1" in
  compose)
    case " $* " in
      *" build minio "*) touch "$ADDP_TEST_IMAGE_STATE" ;;
      *" up -d minio "*) touch "$ADDP_TEST_CONTAINER_STATE" ;;
      *" down --volumes --remove-orphans "*) rm -f "$ADDP_TEST_CONTAINER_STATE" ;;
      *" rm -sf minio "*) rm -f "$ADDP_TEST_CONTAINER_STATE" ;;
    esac
    ;;
  image)
    [ -f "$ADDP_TEST_IMAGE_STATE" ] || exit 1
    ;;
  inspect)
    [ -f "$ADDP_TEST_CONTAINER_STATE" ] || exit 1
    case " $* " in
      *"com.docker.compose.project"*) echo "${ADDP_TEST_CONTAINER_OWNERSHIP:-business/minio}" ;;
      *"NetworkSettings.Networks"*) echo business_default ;;
      *) echo true ;;
    esac
    ;;
  ps|network|volume) exit 0 ;;
  run)
    [ -f "$ADDP_TEST_CONTAINER_STATE" ] || exit 1
    case " $* " in
      *" cp --quiet /fixture/source.las "*)
        for argument in "$@"; do
          case "$argument" in
            *:/fixture/source.las:ro)
              mkdir -p "$ADDP_TEST_MODEL_FIXTURE_CAPTURE_DIR"
              cp "${argument%:/fixture/source.las:ro}" "$ADDP_TEST_MODEL_FIXTURE_CAPTURE_DIR/pdal_las12_format0.las" ;;
          esac
        done
        ;;
      *" cp --quiet /fixture/dae/"*|*" cp --quiet /fixture/3ds/"*)
        mount=""
        source=""
        for argument in "$@"; do
          case "$argument" in
            *:/fixture:ro) mount="${argument%:/fixture:ro}" ;;
            /fixture/dae/*|/fixture/3ds/*) source="${argument#/fixture/}" ;;
          esac
        done
        mkdir -p "$ADDP_TEST_MODEL_FIXTURE_CAPTURE_DIR/$(dirname "$source")"
        cp "$mount/$source" "$ADDP_TEST_MODEL_FIXTURE_CAPTURE_DIR/$source" || exit 1
        ;;
    esac
    ;;
esac
""",
        )
        self.environment = dict(os.environ)
        self.environment.update(
            {
                "PATH": str(self.bin) + os.pathsep + self.environment["PATH"],
                "ADDP_ONLINE_HOST": "1",
                "ADDP_ONLINE_MANAGER_MINIO_PORT": "59002",
                "ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY": "online-manager",
                "ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY": "manager-secret-1234",
                "ADDP_ONLINE_MANAGER_MINIO_BUCKET": "addp-online",
                "ADDP_ONLINE_MANAGER_MINIO_POINTCLOUD_OBJECT": "pointcloud/pdal_las12_format0.las",
                "ADDP_ONLINE_MANAGER_MINIO_PPTX_OBJECT": "document/addp_online_preview_fixture.pptx",
                "ADDP_ONLINE_MANAGER_MINIO_HYBRID_SEARCH_IMAGE_OBJECT": "hybrid-search/purple-gaming-light-gun.jpg",
                "ADDP_TEST_CONTAINER_STATE": str(self.state),
                "ADDP_TEST_IMAGE_STATE": str(self.image),
                "ADDP_TEST_DOCKER_LOG": str(self.log),
                "ADDP_TEST_MODEL_FIXTURE_CAPTURE_DIR": str(self.root / "model-fixtures"),
                "MINIO_API_PORT": "9002",
                "MINIO_ROOT_USER": "personal",
                "MINIO_ROOT_PASSWORD": "personal-secret",
            }
        )

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def _executable(self, name: str, content: str) -> None:
        path = self.bin / name
        path.write_text(content, encoding="utf-8")
        path.chmod(0o755)

    def run_fixture(self, action: str, **overrides: str) -> subprocess.CompletedProcess[str]:
        environment = dict(self.environment)
        environment.update(overrides)
        return subprocess.run(
            ["bash", "business/scripts/online-manager-minio-fixture.sh", action],
            cwd=self.root,
            env=environment,
            capture_output=True,
            text=True,
        )

    def test_starts_seeds_validates_and_stops_dedicated_minio(self) -> None:
        started = self.run_fixture("start")
        checked = self.run_fixture("status")
        stopped = self.run_fixture("stop")

        self.assertEqual(started.returncode, 0, started.stderr)
        self.assertEqual(checked.returncode, 0, checked.stderr)
        self.assertEqual(stopped.returncode, 0, stopped.stderr)
        commands = self.log.read_text(encoding="utf-8")
        self.assertIn("--env-file /dev/null", commands)
        self.assertIn("up -d minio", commands)
        self.assertIn("build minio", commands)
        self.assertIn("--entrypoint mc --network business_default", commands)
        self.assertIn("addp-minio:RELEASE.2025-10-15T17-29-55Z mb --ignore-existing fixture/addp-online", commands)
        self.assertIn("addp-minio:RELEASE.2025-10-15T17-29-55Z cp --quiet /fixture/source.las fixture/addp-online/pointcloud/pdal_las12_format0.las", commands)
        self.assertIn("addp-minio:RELEASE.2025-10-15T17-29-55Z cp --quiet /fixture/source.pptx fixture/addp-online/document/addp_online_preview_fixture.pptx", commands)
        self.assertIn("addp-minio:RELEASE.2025-10-15T17-29-55Z cp --quiet /fixture/source.jpg fixture/addp-online/hybrid-search/purple-gaming-light-gun.jpg", commands)
        for model_object in ("dae/model.dae", "dae/texture.png", "3ds/model.3ds", "3ds/texture.png"):
            self.assertIn(f"cp --quiet /fixture/{model_object} fixture/addp-online/model3d/{model_object}", commands)
            self.assertIn(f"stat fixture/addp-online/model3d/{model_object}", commands)
        for directory in re.findall(r"-v (\S+):/fixture:ro", commands):
            self.assertFalse(Path(directory).exists(), "temporary generated source directory must be removed")
        self.assertIn("|59002|online-manager|manager-secret-1234", commands)
        self.assertNotIn("|9002|personal|personal-secret", commands)
        self.assertFalse((self.business / ".env").exists())
        self.assertFalse(self.state.exists())

    def test_generated_models_and_png_are_deterministic_valid_source_files(self) -> None:
        first = self.run_fixture("start")
        self.assertEqual(first.returncode, 0, first.stderr)
        root = self.root / "model-fixtures"
        baseline = {str(path.relative_to(root)): path.read_bytes() for path in root.rglob("*") if path.is_file()}
        las = baseline["pdal_las12_format0.las"]
        self.assertEqual(las[:4], b"LASF")
        self.assertEqual(las[24:26], bytes((1, 2)))
        offset = struct.unpack_from("<I", las, 96)[0]
        self.assertEqual(struct.unpack_from("<BHI", las, 104), (0, 20, 3))
        self.assertEqual(len(las), offset + 3 * 20)
        self.assertEqual(struct.unpack_from("<iii", las, offset + 20), (100, 100, 50))
        self.assertEqual(struct.unpack_from("<16H", las, 281)[11], 3857)
        dae = ET.fromstring(baseline["dae/model.dae"])
        ns = "{http://www.collada.org/2005/11/COLLADASchema}"
        self.assertEqual(dae.tag, ns + "COLLADA")
        self.assertEqual(dae.get("version"), "1.4.1")
        self.assertEqual(dae.find(ns + "asset/" + ns + "unit").get("meter"), "0.01")
        self.assertEqual(dae.find(ns + "library_images/" + ns + "image/" + ns + "init_from").text, "texture.png")
        model = baseline["3ds/model.3ds"]
        self.assertEqual(struct.unpack_from("<HI", model), (0x4D4D, len(model)))
        self.assertIn(b"texture.png\0", model)
        png = baseline["dae/texture.png"]
        self.assertEqual(png, baseline["3ds/texture.png"])
        self.assertEqual(png[:8], b"\x89PNG\r\n\x1a\n")
        offset = 8
        while offset < len(png):
            size, kind = struct.unpack_from(">I4s", png, offset)
            data = png[offset + 8:offset + 8 + size]
            crc = struct.unpack_from(">I", png, offset + 8 + size)[0]
            self.assertEqual(crc, zlib.crc32(kind + data))
            if kind == b"IDAT":
                self.assertEqual(len(zlib.decompress(data)), 14)
            offset += 12 + size
        second = self.run_fixture("start")
        self.assertEqual(second.returncode, 0, second.stderr)
        current = {str(path.relative_to(root)): path.read_bytes() for path in root.rglob("*") if path.is_file()}
        self.assertEqual(current, baseline)

    def test_hosted_profile_creates_owner_only_descriptor_and_removes_volumes(self) -> None:
        descriptor = self.root / "manager-engine.json"
        overrides = dict(ADDP_TEST_OS="Linux", ADDP_ONLINE_HOSTED="1", GITHUB_ACTIONS="true",
                         RUNNER_OS="Linux", ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE=str(descriptor))
        shutil.rmtree(self.business / "nfs")
        result = self.run_fixture("start", **overrides)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(descriptor.stat().st_mode & 0o777, 0o600)
        data = json.loads(descriptor.read_text())
        self.assertEqual(data["engine_type"], "minio")
        self.assertEqual(data["connection_info"]["endpoint"], "127.0.0.1:59002")
        self.assertEqual(data["connection_info"]["secret_key"], self.environment["ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY"])
        stopped = self.run_fixture("stop", **overrides)
        self.assertEqual(stopped.returncode, 0, stopped.stderr)
        self.assertIn("down --volumes --remove-orphans", self.log.read_text())
        self.assertFalse(self.state.exists())

    def test_hosted_profile_rejects_non_github_environment_before_docker(self) -> None:
        result = self.run_fixture("start", ADDP_TEST_OS="Linux", ADDP_ONLINE_HOSTED="1", GITHUB_ACTIONS="false")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.log.exists())

    def test_rejects_unsafe_secret_before_docker(self) -> None:
        result = self.run_fixture("start", ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY="bad secret")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("URL-safe", result.stderr)
        self.assertFalse(self.log.exists())

    def test_rejects_object_key_with_dot_segments_before_docker(self) -> None:
        result = self.run_fixture("start", ADDP_ONLINE_MANAGER_MINIO_PPTX_OBJECT="document/../source.pptx")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("dot segments", result.stderr)
        self.assertFalse(self.log.exists())

    def test_rejects_non_las_object_key_before_docker(self) -> None:
        result = self.run_fixture(
            "start", ADDP_ONLINE_MANAGER_MINIO_POINTCLOUD_OBJECT="pointcloud/source.csv"
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("must end with .las", result.stderr)
        self.assertFalse(self.log.exists())

    def test_rejects_non_pptx_object_key_before_docker(self) -> None:
        result = self.run_fixture(
            "start", ADDP_ONLINE_MANAGER_MINIO_PPTX_OBJECT="document/source.pdf"
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("must end with .pptx", result.stderr)
        self.assertFalse(self.log.exists())

    def test_rejects_non_jpg_hybrid_search_object_key_before_docker(self) -> None:
        result = self.run_fixture(
            "start",
            ADDP_ONLINE_MANAGER_MINIO_HYBRID_SEARCH_IMAGE_OBJECT="hybrid-search/source.png",
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("must end with .jpg", result.stderr)
        self.assertFalse(self.log.exists())

    def test_refuses_container_owned_by_another_compose_service(self) -> None:
        self.state.touch()

        result = self.run_fixture("stop", ADDP_TEST_CONTAINER_OWNERSHIP="personal/minio")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("not owned", result.stderr)
        self.assertTrue(self.state.exists())


if __name__ == "__main__":
    unittest.main()
