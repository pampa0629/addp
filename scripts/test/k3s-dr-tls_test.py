import json
import os
from pathlib import Path
import shutil
import ssl
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
import importlib.util
import time

PROD = Path(__file__).resolve().parents[1] / "prod"
SCRIPT = PROD / "k3s-dr-tls.py"
SITE = "standby.example"
spec = importlib.util.spec_from_file_location("standby_tls", SCRIPT)
tls = importlib.util.module_from_spec(spec)
spec.loader.exec_module(tls)


class RenewalTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.root = Path(tmp.name)
        self.material = self.root / "tls"
        self.material.mkdir()
        for name in ("k3s-dr-recover.py", "k3s_dr_capture.py"):
            (self.root / name).symlink_to(PROD / name)
        self.env = dict(os.environ, OPENSSL_CONF=os.devnull, SSH_ORIGINAL_COMMAND="issue")
        self.openssl("req", "-x509", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256",
                     "-nodes", "-subj", "/CN=site authority", "-addext", "basicConstraints=critical,CA:TRUE",
                     "-days", "8", "-keyout", "intermediate.key", "-out", "intermediate.crt")
        shutil.copy(self.root / "intermediate.crt", self.material / "ca.pem")
        self.csr = self.openssl("req", "-new", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256",
                                "-nodes", "-subj", "/CN=" + SITE, "-keyout", "tls/leaf.key",
                                "-addext", "subjectAltName=DNS:other.example", "-addext", "basicConstraints=CA:TRUE")
        (self.root / "leaf.ext").write_text("subjectAltName=DNS:" + SITE + "\nbasicConstraints=CA:FALSE\n")
        self.material.joinpath("leaf.pem").write_bytes(self.openssl("x509", "-req", "-CA", "intermediate.crt",
                "-CAkey", "intermediate.key", "-set_serial", "6", "-days", "1", "-extfile", "leaf.ext", data=self.csr))
        # Use an empty Docker inventory for the unit-level standby.
        docker = self.root / "docker"
        docker.write_text('#!/bin/sh\ntest "$1" = ps\n')
        docker.chmod(0o700)
        self.env["PATH"] = str(self.root) + os.pathsep + self.env["PATH"]

    def openssl(self, *args, data=None):
        return subprocess.run(["openssl", *args], input=data, cwd=self.root, env=self.env,
                              capture_output=True, check=True).stdout

    def issue(self, csr, **env):
        return subprocess.run([sys.executable, SCRIPT, "issue", "--ca-dir", self.root,
                               "--hostname", SITE, "--openssl", "openssl"], input=csr,
                              env=dict(self.env, **env), capture_output=True)

    def test_signer_fixes_subject_permissions_and_lifetime(self):
        p = self.issue(self.csr)
        self.assertEqual(p.returncode, 0, p.stderr)
        cert = self.openssl("x509", "-noout", "-text", data=p.stdout).decode()
        self.assertIn("DNS:" + SITE, cert)
        self.assertNotIn("other.example", cert)
        self.assertIn("CA:FALSE", cert)
        self.assertIn("TLS Web Server Authentication", cert)
        expiry = self.openssl("x509", "-noout", "-enddate", data=p.stdout).decode().strip().split("=", 1)[1]
        self.assertLessEqual(abs(ssl.cert_time_to_seconds(expiry) - time.time() - 72 * 3600), 5)
        self.assertNotEqual(self.issue(self.csr, SSH_ORIGINAL_COMMAND="other").returncode, 0)
        other = self.openssl("req", "-new", "-subj", "/CN=other.example", "-key", "tls/leaf.key")
        self.assertNotEqual(self.issue(other).returncode, 0)

    def test_due_rotation_and_skip_without_issuer(self):
        cmd = [sys.executable, SCRIPT, "guest", "--kit", self.root, "--hostname", SITE]
        previous = (self.material / "leaf.pem").read_bytes()
        fail = subprocess.run(cmd, input=b"untrusted", capture_output=True, env=self.env)
        self.assertNotEqual(fail.returncode, 0)
        self.assertEqual((self.material / "leaf.pem").read_bytes(), previous)
        with subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=self.env) as p:
            csr = json.loads(p.stdout.readline())["csr"]
            response, err = p.communicate(self.issue(csr.encode()).stdout, timeout=15)
            self.assertEqual(p.returncode, 0, err)
            self.assertEqual(json.loads(response)["status"], "renewed")
        skip = subprocess.run(cmd, input=b"", capture_output=True, env=self.env, check=True)
        self.assertEqual(json.loads(skip.stdout)["status"], "skipped")

    def test_running_edge_failure_restores_previous_chain(self):
        from types import SimpleNamespace
        from io import BytesIO, StringIO
        previous = (self.material / "leaf.pem").read_bytes()
        response = self.issue(self.csr).stdout
        with patch.dict(os.environ, self.env), patch.object(sys, "stdout", StringIO()), \
                patch.object(sys, "stdin", SimpleNamespace(buffer=BytesIO(response))), \
                patch.object(tls, "live_edges", return_value=[("edge", "127.0.0.1")]), \
                patch.object(tls, "reload_edges", side_effect=[RuntimeError("reload failed"), None]) as reload:
            with self.assertRaisesRegex(RuntimeError, "reload failed"):
                tls.guest(SimpleNamespace(kit=self.root, hostname=SITE, force=False))
        self.assertEqual(reload.call_count, 2)
        self.assertEqual((self.material / "leaf.pem").read_bytes(), previous)


if __name__ == "__main__":
    unittest.main()
