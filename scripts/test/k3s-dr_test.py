import hashlib
import importlib.util
import io
from pathlib import Path
import ssl
import subprocess
import sys
import tarfile
import tempfile
import unittest

PROD = Path(__file__).resolve().parents[1] / "prod"
sys.path.insert(0, str(PROD))
import k3s_dr_capture as capture

spec = importlib.util.spec_from_file_location("dr_recover", PROD / "k3s-dr-recover.py")
recover = importlib.util.module_from_spec(spec)
spec.loader.exec_module(recover)


class RecoveryKitTest(unittest.TestCase):
    def test_tls_uses_kit_trust_original_hostname_and_matching_key(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            tls = root / "tls"
            tls.mkdir()
            for name in ("site", "other"):
                subprocess.run(["openssl", "req", "-config", "/dev/null", "-x509", "-newkey", "rsa:2048", "-nodes",
                                "-subj", "/CN=" + name, "-addext", "subjectAltName=DNS:" + name,
                                "-days", "1", "-keyout", str(tls / (name + ".key")),
                                "-out", str(tls / (name + ".pem"))], check=True, capture_output=True)
            (tls / "leaf.pem").write_bytes((tls / "site.pem").read_bytes())
            (tls / "leaf.key").write_bytes((tls / "site.key").read_bytes())
            (tls / "ca.pem").write_bytes((tls / "site.pem").read_bytes())
            recover.verify_tls(root, "site")
            with self.assertRaises(subprocess.CalledProcessError):
                recover.verify_tls(root, "other")
            (tls / "ca.pem").write_bytes((tls / "other.pem").read_bytes())
            with self.assertRaises(subprocess.CalledProcessError):
                recover.verify_tls(root, "site")
            (tls / "leaf.key").write_bytes((tls / "other.key").read_bytes())
            with self.assertRaises(ssl.SSLError):
                recover.verify_tls(root, "site")

    def test_snapshot_copy_must_match_inventory(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            data = root / "postgres.dump"
            data.write_bytes(b"backup")
            (root / "SHA256SUMS").write_text(hashlib.sha256(b"backup").hexdigest() + "  postgres.dump\n")
            recover.verify_files(root)
            data.write_bytes(b"partial")
            with self.assertRaisesRegex(ValueError, "checksum"):
                recover.verify_files(root)
            data.write_bytes(b"backup")
            (root / "unexpected").touch()
            with self.assertRaisesRegex(ValueError, "incomplete"):
                recover.verify_files(root)

    def test_search_matches_contents_even_when_counts_match(self):
        def witness(value):
            api = {"/indexes?limit=100&offset=0": {"results": [{"uid": "entries", "primaryKey": "id"}]},
                   "/indexes/entries/documents?limit=500&offset=0": {"results": [{"id": 1, "value": value}]},
                   "/indexes/entries/settings": {"searchableAttributes": ["value"]}}
            return capture.search_witness(api.__getitem__)
        a, b = witness("old"), witness("new")
        self.assertEqual(a["entries"]["count"], b["entries"]["count"])
        self.assertNotEqual(a["entries"]["documents"], b["entries"]["documents"])

    def test_object_bytes_and_unsafe_members(self):
        with tempfile.TemporaryDirectory() as tmp:
            archive = Path(tmp) / "objects.tar"
            with tarfile.open(archive, "w") as out:
                item = tarfile.TarInfo("bucket/file")
                item.size = 4
                out.addfile(item, io.BytesIO(b"data"))
            self.assertEqual(capture.object_witness(archive),
                             {"bucket/file": hashlib.sha256(b"data").hexdigest()})
            with tarfile.open(archive, "a") as out:
                item = tarfile.TarInfo("../foreign")
                item.type = tarfile.DIRTYPE
                out.addfile(item)
            with self.assertRaisesRegex(ValueError, "unexpected"):
                capture.object_witness(archive)

    def test_business_receipt_uses_qualified_table(self):
        with self.assertRaises(ValueError):
            capture.table_query("public.t; select 1")
        sql = capture.table_query("sales.orders")
        self.assertIn("FROM sales.orders r", sql)
        self.assertTrue(sql.startswith("SET TIME ZONE 'UTC';"))


if __name__ == "__main__":
    unittest.main()
