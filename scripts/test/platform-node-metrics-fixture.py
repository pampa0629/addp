"""Owned TLS transport and center deployment for the real Hosted metrics suite."""
from __future__ import annotations

import argparse
import ipaddress
import json
import os
import platform
import secrets
import shlex
import socket
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PROJECT = "addp-metrics-online"


def boundary():
    if (os.environ.get("GITHUB_ACTIONS") != "true" or os.environ.get("RUNNER_OS") != "Linux"
            or platform.system() != "Linux" or platform.machine() != "x86_64"
            or os.environ.get("ADDP_ONLINE_HOSTED") != "1" or os.environ.get("ADDP_ONLINE_HOST") != "1"
            or os.environ.get("ONLINE_SUITE") != "platform-node-metrics"
            or os.environ.get("POSTGRES_DB") != "addp_online" or (ROOT / ".env").exists()):
        raise ValueError("metrics fixture requires its isolated Hosted Linux profile")
    directory = Path(os.environ["ADDP_ONLINE_SECRET_DIR"])
    artifacts = Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"]).resolve()
    if (not directory.is_absolute() or directory.is_symlink() or not directory.is_dir()
            or directory.resolve().is_relative_to(ROOT) or directory.resolve().is_relative_to(artifacts)
            or directory.stat().st_mode & 0o777 != 0o700):
        raise ValueError("metrics fixture requires an external private secret directory")
    return directory


def command(args):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=120).stdout


def compose(directory, *args):
    return command(["docker", "compose", "--env-file", "/dev/null", "-p", PROJECT,
                    "-f", str(directory / "metrics-compose.json"), *args])


def openssl(*args):
    command(["openssl", *args])


def prepare(directory):
    # Refuse reuse before generating certificates or touching Docker resources.
    if (directory / "metrics.env").exists() or (directory / "metrics-compose.json").exists():
        raise ValueError("metrics fixture is already prepared")
    bridge = json.loads(command(["docker", "network", "inspect", "bridge"]))
    address = bridge[0]["IPAM"]["Config"][0]["Gateway"]
    ip = ipaddress.ip_address(address)
    if ip.version != 4 or not ip.is_private or ip.is_loopback or ip.is_link_local:
        raise ValueError("Hosted metrics requires its native private Docker bridge gateway")
    for port in (9444, 19100):
        with socket.socket() as sock:
            sock.bind((address, port))
    for name in ("center-tls", "node-tls", "deployment", "control", "admission"):
        (directory / name).mkdir(mode=0o755)

    def ca(name):
        path = directory / name
        openssl("req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1", "-subj", "/CN="+name,
                "-addext", "basicConstraints=critical,CA:TRUE", "-addext", "keyUsage=critical,keyCertSign,cRLSign",
                "-keyout", str(path)+".key", "-out", str(path)+".crt")
        return path

    def certificate(issuer, folder, name, server=False):
        key, cert = folder / (name+".key"), folder / (name+".crt")
        csr = directory / (folder.name+"-"+name+".csr")
        extensions = directory / "extensions"
        extensions.write_text("extendedKeyUsage=" + ("serverAuth\nsubjectAltName=IP:127.0.0.1,IP:"+address+",DNS:localhost\n" if server else "clientAuth\n"))
        openssl("req", "-new", "-newkey", "rsa:2048", "-nodes", "-subj", "/CN="+name,
                "-keyout", str(key), "-out", str(csr))
        openssl("x509", "-req", "-in", str(csr), "-CA", str(issuer)+".crt", "-CAkey", str(issuer)+".key",
                "-CAcreateserial", "-days", "1", "-extfile", str(extensions), "-out", str(cert))
        # Only temporary fixture files under the private, unmounted parent.
        key.chmod(0o644)

    center, control, source = ca("center-ca"), ca("control-ca"), ca("source-ca")
    for folder, issuer in (("center-tls", center), ("node-tls", source)):
        (directory / folder / "ca.crt").write_bytes(Path(str(issuer)+".crt").read_bytes())
        certificate(issuer, directory / folder, "server", True)
    certificate(center, directory / "center-tls", "health")
    certificate(control, directory / "control", "server", True)
    certificate(source, directory / "deployment", "collector")
    certificate(source, directory / "admission", "client")
    deployment = directory / "deployment"
    for name, issuer in (("source-ca.crt", source), ("control-ca.crt", control)):
        (deployment / name).write_bytes(Path(str(issuer)+".crt").read_bytes())
    (directory / "admission/ca.crt").write_bytes(Path(str(source)+".crt").read_bytes())
    secret = secrets.token_hex(32)
    (deployment / "prometheus-client-secret").write_text(secret)
    (deployment / "prometheus-client-secret").chmod(0o644)
    values = {
        "ADDP_OBSERVABILITY_METRICS_ENABLED": "true", "ADDP_OBSERVABILITY_LOGS_ENABLED": "false",
        "ADDP_METRICS_TLS_DIR": str(directory / "center-tls"), "ADDP_METRICS_DEPLOYMENT_DIR": str(deployment),
        "PROMETHEUS_SERVICE_CLIENT_SECRET": secret,
        "PROMETHEUS_SYSTEM_URL": "https://"+address+":9444", "PROMETHEUS_MONITOR_URL": "https://"+address+":9444",
        "ADDP_ONLINE_METRICS_NODE_IP": address,
        "ADDP_NODE_METRICS_ENABLED": "true", "ADDP_NODE_METRICS_LISTEN": address+":19100",
        "ADDP_NODE_METRICS_TLS_DIR": str(directory / "node-tls"),
        "MONITOR_METRICS_ALLOWED_CIDRS": address+"/32", "MONITOR_METRICS_ALLOWED_PORTS": "19100",
        "MONITOR_METRICS_CA_FILE": str(directory / "admission/ca.crt"),
        "MONITOR_METRICS_CLIENT_CERT_FILE": str(directory / "admission/client.crt"),
        "MONITOR_METRICS_CLIENT_KEY_FILE": str(directory / "admission/client.key"),
    }
    os.environ.update(values)
    command(["python3", str(ROOT / "scripts/infra/generate-metrics-config.py")])
    # The center belongs to the standard Infra lifecycle; this fixture only
    # supplies protected transport to the real Gateway.
    # Use the same pinned TLS transport image and resource bounds as the T2 owner.
    os.environ["METRICS_T2_WORK"] = str(directory)
    transport = json.loads(command(["docker", "compose", "--env-file", "/dev/null", "-f",
                                   str(ROOT / "scripts/test/docker-compose.monitor-metrics-t2.yml"),
                                   "config", "--format", "json"]))["services"]["metrics-source"]
    transport.pop("ports", None)
    transport.pop("networks", None)
    transport["network_mode"] = "host"
    transport["volumes"] = [{"type": "bind", "source": str(directory / "control"), "target": "/control", "read_only": True},
                            {"type": "bind", "source": str(directory / "nginx.conf"), "target": "/etc/nginx/nginx.conf", "read_only": True}]
    transport["labels"] = {"com.addp.online-runtime": "platform-node-metrics"}
    spec = {"services": {"control-tls": transport}}
    (directory / "metrics-compose.json").write_text(json.dumps(spec))
    (directory / "nginx.conf").write_text('''worker_processes 1;
pid /tmp/nginx.pid;
error_log /dev/stderr warn;
events { worker_connections 128; }
http {
  access_log off;
  client_body_temp_path /tmp/client;
  proxy_temp_path /tmp/proxy;
  fastcgi_temp_path /tmp/fastcgi;
  uwsgi_temp_path /tmp/uwsgi;
  scgi_temp_path /tmp/scgi;
  server {
    listen @@IP@@:9444 ssl;
    ssl_certificate /control/server.crt;
    ssl_certificate_key /control/server.key;
    ssl_protocols TLSv1.2 TLSv1.3;
    location = /api/v1/system/oauth/token { proxy_pass http://127.0.0.1:8000; }
    location = /api/v1/monitor/platform/metrics_discovery { proxy_pass http://127.0.0.1:8000; }
    location / { return 404; }
  }
}
'''.replace("@@IP@@", address))
    text = "".join("export " + key + "=" + shlex.quote(value) + "\n" for key, value in sorted(values.items()))
    (directory / "metrics.env").write_text(text)
    (directory / "metrics.env").chmod(0o600)
    # The standard lifecycle reloads this file, so it must contain current inputs.
    runtime = directory / "runtime.env"
    with runtime.open("a") as file:
        file.write("\n"+text)
    runtime.chmod(0o600)


def assert_owned(directory):
    ids = command(["docker", "ps", "-aq", "--filter", "label=com.docker.compose.project="+PROJECT]).split()
    if ids:
        for container in json.loads(command(["docker", "inspect", *ids])):
            if container["Config"].get("Labels", {}).get("com.addp.online-runtime") != "platform-node-metrics":
                raise ValueError("refusing an unowned metrics container")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=("prepare", "up", "down"))
    args = parser.parse_args()
    directory = boundary()
    if args.action == "prepare":
        prepare(directory)
    elif (directory / "metrics-compose.json").exists():
        assert_owned(directory)
        if args.action == "up":
            compose(directory, "up", "-d", "--wait", "--wait-timeout", "60")
        else:
            compose(directory, "down", "--volumes", "--remove-orphans")
            for kind, cmd in (("container", ["ps", "-aq"]), ("network", ["network", "ls", "-q"]), ("volume", ["volume", "ls", "-q"])):
                if command(["docker", *cmd, "--filter", "label=com.docker.compose.project="+PROJECT]).strip():
                    raise ValueError("metrics fixture has residual " + kind)
    elif args.action == "up":
        raise ValueError("metrics fixture has not been prepared")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, subprocess.SubprocessError):
        raise SystemExit("Hosted metrics fixture failed; no credential details are printed")
