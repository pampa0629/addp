#!/usr/bin/env python3
"""Issue a scoped standby TLS certificate, or renew one in a Linux recovery kit."""
import argparse
import fcntl
import ipaddress
import json
import os
from pathlib import Path
import re
import secrets
import socket
import ssl
import subprocess
import sys
import tempfile
import time
from datetime import datetime, timezone


def output(argv, **kwargs):
    return subprocess.check_output(argv, **kwargs)


def expires(openssl, path):
    line = output([openssl, "x509", "-in", path, "-enddate", "-noout"], text=True)
    return int(ssl.cert_time_to_seconds(line.strip().split("=", 1)[1]))


def subject_name(hostname):
    if not re.fullmatch(r"[a-z0-9]+(?:[.-][a-z0-9]+)*", hostname):
        raise ValueError("invalid site hostname")
    return "/CN=" + hostname


def issue(args):
    if os.environ.get("SSH_ORIGINAL_COMMAND") != "issue":
        raise ValueError("only the issue command is allowed")
    expected = subject_name(args.hostname)
    csr = sys.stdin.buffer.read(16385)
    if len(csr) > 16384 or not csr:
        raise ValueError("invalid CSR size")
    req = [args.openssl, "req", "-config", "/dev/null", "-noout"]
    verified = subprocess.run(req + ["-verify"], input=csr, capture_output=True)
    if verified.returncode or b"verify OK" not in verified.stdout + verified.stderr:
        raise ValueError("invalid CSR signature")
    if output(req + ["-subject", "-nameopt", "RFC2253"], input=csr).decode().strip() != "subject=" + expected[1:]:
        raise ValueError("CSR subject outside the authorized site")

    ca = args.ca_dir / "intermediate.crt"
    now = int(time.time())
    days = min(3, (expires(args.openssl, ca) - now - 60) // 86400)
    if days < 1:
        raise ValueError("issuing intermediate has less than one day left")
    with tempfile.TemporaryDirectory() as tmp:
        ext = Path(tmp) / "leaf.ext"
        ext.write_text("basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature\n"
                       "extendedKeyUsage=serverAuth\nsubjectAltName=DNS:" + args.hostname + "\n")
        leaf = output([args.openssl, "x509", "-req", "-CA", ca, "-CAkey", args.ca_dir / "intermediate.key",
                       "-set_serial", "0x" + secrets.token_hex(16), "-sha256", "-copy_extensions", "none",
                       "-days", str(days),
                       "-extfile", ext], input=csr)
        sys.stdout.buffer.write(leaf + ca.read_bytes())


def verified(kit, candidate, hostname):
    subprocess.run([sys.executable, kit / "k3s-dr-recover.py", "check-tls", "--kit", candidate,
                    "--hostname", hostname], check=True, stdout=subprocess.DEVNULL)


def live_edges(tls):
    ids = output(["docker", "ps", "-q", "--filter", "label=com.docker.compose.service=edge"], text=True).split()
    if not ids:
        return []
    edges = []
    for item in json.loads(output(["docker", "inspect", *ids])):
        project = item["Config"]["Labels"].get("com.docker.compose.project", "")
        if re.fullmatch("addp-dr-[0-9a-f]{10}", project) and any(
            m["Destination"] == "/tls" and m["Source"] == str(tls) and not m["RW"] for m in item["Mounts"]
        ):
            ip = item["NetworkSettings"]["Networks"][project + "_default"]["IPAddress"]
            edges.append((item["Id"], str(ipaddress.IPv4Address(ip))))
    return edges


def reload_edges(edges, leaf, hostname):
    pem = leaf.read_text().split("-----END CERTIFICATE-----", 1)[0] + "-----END CERTIFICATE-----"
    expected = ssl.PEM_cert_to_DER_cert(pem)
    ctx = ssl.create_default_context(cafile=str(leaf.parent / "ca.pem"))
    for cid, ip in edges:
        subprocess.run(["docker", "exec", cid, "nginx", "-s", "reload"], check=True, stdout=subprocess.DEVNULL)
        until = time.monotonic() + 12
        while time.monotonic() < until:
            with socket.create_connection((ip, 443), timeout=2) as sock:
                with ctx.wrap_socket(sock, server_hostname=hostname) as client:
                    if client.getpeercert(binary_form=True) == expected:
                        break
            time.sleep(0.2)
        else:
            raise RuntimeError("edge did not adopt the new certificate: " + ip)


def guest(args):
    kit = args.kit.resolve()
    tls = kit / "tls"
    leaf = tls / "leaf.pem"
    with (kit / ".dr-tls.lock").open("a") as lock, tempfile.TemporaryDirectory(dir=tls) as tmp:
        fcntl.flock(lock, fcntl.LOCK_EX)
        current_end = expires("openssl", leaf)
        check = ["openssl", "verify", "-trusted", tls / "ca.pem", "-untrusted", leaf, "-purpose", "sslserver",
                 "-verify_hostname", args.hostname, "-attime", str(int(time.time()) + 48 * 3600), leaf]
        if not args.force and current_end > time.time() + 48 * 3600 and subprocess.run(check, capture_output=True).returncode == 0:
            verified(kit, kit, args.hostname)
            print(json.dumps({"status": "skipped", "expires": current_end}), flush=True)
            return

        csr = output(["openssl", "req", "-new", "-key", tls / "leaf.key", "-subj", subject_name(args.hostname)])
        print(json.dumps({"csr": csr.decode("ascii")}), flush=True)
        chain = sys.stdin.buffer.read(65537)
        if not chain or len(chain) > 65536:
            raise ValueError("no valid response from issuer")
        candidate = Path(tmp) / "tls"
        candidate.mkdir()
        for name in ("leaf.key", "ca.pem"):
            (candidate / name).symlink_to(tls / name)
        fresh = candidate / "leaf.pem"
        fresh.write_bytes(chain)
        verified(kit, candidate.parent, args.hostname)
        new_end = expires("openssl", fresh)
        if new_end <= current_end or new_end > time.time() + 72 * 3600 + 1:
            raise ValueError("new certificate must extend expiry within 72 hours")
        edges = live_edges(tls)
        saved = Path(tmp) / "previous.pem"
        os.link(leaf, saved)
        fresh.replace(leaf)
        try:
            reload_edges(edges, leaf, args.hostname)
        except Exception:
            saved.replace(leaf)
            reload_edges(edges, leaf, args.hostname)
            raise
        print(json.dumps({"status": "renewed", "expires": new_end, "edges": len(edges),
                          "sha256": output(["openssl", "x509", "-in", leaf, "-fingerprint", "-sha256", "-noout"],
                                           text=True).strip().split("=")[1]}))


def renew(args):
    vm = ["colima", "ssh", "--profile", args.colima_profile, "--"] if args.colima_profile else []
    cmd = vm + ["python3", str(args.kit / "k3s-dr-tls.py"), "guest", "--kit", str(args.kit),
                "--hostname", args.hostname]
    with subprocess.Popen(cmd + (["--force"] if args.force else []), stdin=subprocess.PIPE,
                          stdout=subprocess.PIPE) as child:
        try:
            request = json.loads(child.stdout.readline())
            chain = b""
            if "csr" in request:
                chain = output(["ssh", "-F", args.ssh_config, "-o", "BatchMode=yes", "-o",
                                "StrictHostKeyChecking=yes", args.issuer, "issue"],
                               input=request["csr"].encode(), timeout=30)
            response, _ = child.communicate(chain)
            if child.returncode:
                raise RuntimeError("recovery VM failed to verify or install the certificate")
            result = json.loads(response) if chain else request
            print(json.dumps(result), flush=True)
        finally:
            if child.poll() is None:
                child.communicate(b"")


if __name__ == "__main__":
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    subs = parser.add_subparsers(dest="action", required=True)
    signer = subs.add_parser("issue")
    signer.add_argument("--ca-dir", type=Path, required=True)
    signer.add_argument("--openssl", required=True)
    signer.add_argument("--hostname", required=True)
    for action in ("guest", "renew"):
        p = subs.add_parser(action)
        p.add_argument("--kit", type=Path, required=True)
        p.add_argument("--hostname", required=True)
        p.add_argument("--force", action="store_true")
        if action == "renew":
            p.add_argument("--issuer", required=True)
            p.add_argument("--ssh-config", required=True)
            p.add_argument("--colima-profile")
            p.add_argument("--alert-file", required=True, type=Path)
    a = parser.parse_args()
    try:
        {"issue": issue, "guest": guest, "renew": renew}[a.action](a)
    except Exception as exc:
        if a.action == "renew":
            details = {"site": a.hostname, "time": datetime.now(timezone.utc).isoformat(),
                       "error": str(exc)}
            with tempfile.NamedTemporaryFile(mode="w", dir=a.alert_file.parent, delete=False) as f:
                json.dump(details, f)
            os.replace(f.name, a.alert_file)
            subprocess.run(["logger", "-p", "user.err", "-t", "addp-dr-tls", json.dumps(details)])
            if sys.platform == "darwin":
                subprocess.run(["osascript", "-e", 'display notification "备用 HTTPS 证书续签失败，请查看 ALERT.json" with title "ADDP 灾备"'])
        sys.exit(str(exc))
    if a.action == "renew":
        a.alert_file.unlink(missing_ok=True)
