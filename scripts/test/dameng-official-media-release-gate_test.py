import hashlib
import subprocess
import tempfile
import unittest
import zipfile
from pathlib import Path


HELPER = Path(__file__).parents[1] / "lib/dameng-official-media.sh"
GATE = Path(__file__).with_name("dameng-official-media-release-gate.sh")
CONTAINERFILE = Path(__file__).parents[2] / "business/dameng/Containerfile"


class DamengOfficialMediaReleaseGateTest(unittest.TestCase):
    def test_pins_single_arm64_route_and_zero_residue(self) -> None:
        helper = HELPER.read_text(encoding="utf-8")
        gate = GATE.read_text(encoding="utf-8")
        containerfile = CONTAINERFILE.read_text(encoding="utf-8")

        for expected in (
            "dm8_20260708_HWarm920_kylin10_sp1_64.zip",
            "d6871147cd4a04e1595d9dedf9d05245c55b17bff2ae738dded37fe568df9824",
            "2a8a4844527e901718a88b4c747d7fd44460bcb2a862956bba98146760b8423e",
            "5cd15db0f0f19cf985565615114cf6242c0257b2df566e5b81eb9cb62832e921",
            "f80e1472517f148f4aa7f7c0e9feb22c939349d1c464079481e869700e77f873",
            "09e911c8cb5015f3c17cd1e834cdddc12b994c4bf417d77b87b7440e2296ff3b",
            "addp/dameng:dm8-20260708-arm64",
        ):
            self.assertIn(expected, helper)
        self.assertIn("ubuntu:22.04@sha256:829f6df", containerfile)
        self.assertIn("COPY --chown=dmdba:dinstall --from=dm-media", containerfile)
        self.assertIn("linux/arm64", gate)
        self.assertIn("libdodbc.so", gate)
        self.assertIn("CHARACTER_CODE=PG_UTF8", gate)
        self.assertIn("SELECT BUILD_VERSION FROM V\\$INSTANCE", gate)
        self.assertNotIn("BANNER, ID", gate)
        self.assertIn('= "$DAMENG_LIBDODBC_SHA256"', gate)
        self.assertIn("timeout 30", helper)
        self.assertIn('\\"$DAMENG_DATABASE_PASSWORD\\"', helper)
        self.assertIn("docker rm --force", gate)
        self.assertIn("docker network rm", gate)
        self.assertIn("docker image rm", gate)
        self.assertIn('"zero_residue": True', gate)
        self.assertNotIn("engine_type=dameng", gate)

    def test_verifies_both_zip_and_inner_iso_hashes(self) -> None:
        with tempfile.TemporaryDirectory(prefix="addp-dameng-media-") as temporary:
            media = Path(temporary) / "media.zip"
            iso = b"pinned-dm8-iso"
            with zipfile.ZipFile(media, "w") as archive:
                archive.writestr("dm8.iso", iso)
            media_sha256 = hashlib.sha256(media.read_bytes()).hexdigest()
            iso_sha256 = hashlib.sha256(iso).hexdigest()
            result = subprocess.run(
                [
                    "bash",
                    "-c",
                    'source "$1"; DAMENG_OFFICIAL_MEDIA_SHA256=$2; DAMENG_OFFICIAL_ISO_SHA256=$3; dameng_verify_official_media "$4"',
                    "_",
                    str(HELPER),
                    media_sha256,
                    iso_sha256,
                    str(media),
                ],
                capture_output=True,
                text=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)

            invalid = subprocess.run(
                [
                    "bash",
                    "-c",
                    'source "$1"; DAMENG_OFFICIAL_MEDIA_SHA256=$2; DAMENG_OFFICIAL_ISO_SHA256=$3; dameng_verify_official_media "$4"',
                    "_",
                    str(HELPER),
                    media_sha256,
                    "0" * 64,
                    str(media),
                ],
                capture_output=True,
                text=True,
            )
            self.assertNotEqual(invalid.returncode, 0)
            self.assertIn("ISO SHA-256 mismatch", invalid.stderr)


if __name__ == "__main__":
    unittest.main()
