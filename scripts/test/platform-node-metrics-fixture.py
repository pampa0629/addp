"""Owned TLS transport and center deployment for the real Hosted metrics suite."""
from __future__ import annotations

import argparse
from importlib import import_module
import ipaddress
import json
import os
import platform
import secrets
import shlex
import socket
import subprocess
import sys
import time
import re
from contextlib import contextmanager
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PROJECT = "addp-metrics-online"
PROCESS_PORTS = {"system": 19101, "monitor": 19102}


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
    for port in (9444, 19100, *PROCESS_PORTS.values()):
        with socket.socket() as sock:
            sock.bind((address, port))
    for name in ("center-tls", "node-tls", "deployment", "control", "admission", "process-system", "process-monitor"):
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

    (directory / "query").mkdir(mode=0o700)
    center, control, source = ca("center-ca"), ca("control-ca"), ca("source-ca")
    for folder, issuer in (("center-tls", center), ("node-tls", source), ("process-system", source), ("process-monitor", source)):
        (directory / folder / "ca.crt").write_bytes(Path(str(issuer)+".crt").read_bytes())
        certificate(issuer, directory / folder, "server", True)
    certificate(center, directory / "center-tls", "health")
    certificate(center, directory / "query", "client")
    (directory / "query/client.key").chmod(0o600)
    (directory / "query/ca.crt").write_bytes(Path(str(center)+".crt").read_bytes())
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
        "MONITOR_WEBHOOK_ALLOW_PRIVATE_NETWORKS": "true", "SSL_CERT_FILE": str(directory / "center-tls/ca.crt"),
        "ADDP_OBSERVABILITY_METRICS_ENABLED": "true", "ADDP_OBSERVABILITY_LOGS_ENABLED": "false",
        "ADDP_METRICS_TLS_DIR": str(directory / "center-tls"), "ADDP_METRICS_DEPLOYMENT_DIR": str(deployment),
        "PROMETHEUS_SERVICE_CLIENT_SECRET": secret,
        "PROMETHEUS_SYSTEM_URL": "https://"+address+":9444", "PROMETHEUS_MONITOR_URL": "https://"+address+":9444",
        "ADDP_ONLINE_METRICS_NODE_IP": address,
        "ADDP_NODE_METRICS_ENABLED": "true", "ADDP_NODE_METRICS_LISTEN": address+":19100",
        "ADDP_NODE_METRICS_TLS_DIR": str(directory / "node-tls"),
        "MONITOR_PROMETHEUS_CA_FILE": str(directory / "query/ca.crt"),
        "MONITOR_PROMETHEUS_CLIENT_CERT_FILE": str(directory / "query/client.crt"),
        "MONITOR_PROMETHEUS_CLIENT_KEY_FILE": str(directory / "query/client.key"),
        "MONITOR_METRICS_ALLOWED_CIDRS": address+"/32", "MONITOR_METRICS_ALLOWED_PORTS": ",".join(map(str, (19100, *PROCESS_PORTS.values()))),
        "ADDP_DEV_PROCESS_METRICS_PLAN": json.dumps({
            module: {"module_name": module, "role": "backend", "listen": address+":"+str(port),
             "endpoint": "https://"+address+":"+str(port)+"/metrics", "tls_dir": str(directory / ("process-"+module))}
            for module, port in PROCESS_PORTS.items()}, separators=(",", ":")),
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


def center_identity():
    ids = command(["docker", "ps", "-aq", "--filter", "label=com.docker.compose.project=addp-infra",
                   "--filter", "label=com.docker.compose.service=prometheus"]).split()
    if len(ids) != 1:
        raise ValueError("expected exactly one standard Infra metrics center")
    rows = json.loads(command(["docker", "inspect", ids[0]]))
    labels = rows[0]["Config"].get("Labels", {}) if len(rows) == 1 else {}
    expected = {"com.docker.compose.project": "addp-infra", "com.docker.compose.service": "prometheus",
                "com.docker.compose.project.working_dir": str(ROOT)}
    if any(labels.get(key) != value for key, value in expected.items()):
        raise ValueError("refusing a foreign Infra metrics center")
    return ids[0]


def center_origin():
    address = command(["docker", "port", center_identity(), "9090/tcp"]).strip()
    host, separator, port = address.partition(":")
    if host != "127.0.0.1" or separator != ":" or not port.isdecimal() or not 1 <= int(port) <= 65535 or port != str(int(port)):
        raise ValueError("center must publish one canonical owned loopback port")
    return "https://" + address


def configure_query(directory):
    origin = center_origin()
    marker = directory / "query/connected"
    if marker.exists():
        raise ValueError("query fixture is already configured")
    text = "export MONITOR_PROMETHEUS_URL=" + shlex.quote(origin) + "\n"
    for name in ("metrics.env", "runtime.env"):
        with (directory / name).open("a") as file:
            file.write(text)
        (directory / name).chmod(0o600)
    marker.write_text("configured\n")


def cpu_pressure_container(row):
    labels = row.get("Config", {}).get("Labels", {})
    owner = labels.get("io.addp.cpu-pressure.owner", "")
    if (not re.fullmatch(r"[0-9a-f]{32}", owner) or row.get("Name") != "/addp-online-cpu-"+owner
            or not re.fullmatch(r"[0-9a-f]{64}", row.get("Id", ""))
            or labels.get("com.docker.compose.project") != PROJECT
            or labels.get("com.docker.compose.service") != "cpu-pressure"
            or labels.get("com.addp.online-runtime") != "platform-node-metrics"):
        raise ValueError("CPU pressure container ownership changed")
    return row


def cleanup_cpu_pressure(directory):
    if boundary() != directory:raise ValueError("CPU cleanup requires the owned Hosted deployment")
    query = ["docker", "ps", "-aq", "--filter", "label=com.docker.compose.project="+PROJECT,
             "--filter", "label=com.docker.compose.service=cpu-pressure"]
    ids = command(query).split()
    if not ids:return
    rows = json.loads(command(["docker", "inspect", *ids]))
    if len(rows) != len(ids) or {cpu_pressure_container(row)["Id"] for row in rows} != set(ids):
        raise ValueError("CPU cleanup receipt differs from its owned container set")
    command(["docker", "rm", "-f", "-v", *ids])
    if command(query).strip():raise ValueError("CPU pressure has a residual container")


@contextmanager
def cpu_pressure(directory, plan, detail):
    if boundary() != directory:
        raise ValueError("CPU pressure requires the owned Hosted deployment")
    cores, quota = plan.get("logical_cores"), plan.get("quota_nanos")
    if (type(cores) is not int or cores <= 0 or type(quota) is not int
            or quota != min(1_000_000_000, cores*250_000_000) or plan.get("max_seconds") != 300
            or os.cpu_count() != cores):
        raise ValueError("CPU pressure has an invalid host quota")
    docker = json.loads(command(["docker", "info", "--format", "{{json .}}"]))
    if type(docker.get("NCPU")) is not int or docker["NCPU"] != cores:
        raise ValueError("CPU pressure Docker host differs from the observed VM")
    source = compose(directory, "ps", "-q", "control-tls").split()
    if len(source) != 1:
        raise ValueError("CPU pressure requires its existing transport image")
    transport = json.loads(command(["docker", "inspect", source[0]]))
    labels = {"com.docker.compose.project": PROJECT, "com.addp.online-runtime": "platform-node-metrics"}
    if (len(transport) != 1 or not transport[0].get("State", {}).get("Running")
            or any(transport[0].get("Config", {}).get("Labels", {}).get(k) != v for k,v in labels.items())
            or not re.fullmatch(r"sha256:[0-9a-f]{64}", transport[0].get("Image", ""))):
        raise ValueError("CPU pressure transport is not owned")
    image = transport[0]["Image"]
    nonce = secrets.token_hex(16)
    name = "addp-online-cpu-"+nonce
    labels.update({"com.docker.compose.service": "cpu-pressure", "io.addp.cpu-pressure.owner": nonce})
    identifier, attempted = None, False
    detail["cpu_released"] = False

    def listed():
        return command(["docker", "ps", "-aq", "--filter", "name=^/"+name+"$"]).split()

    def owned():
        rows = json.loads(command(["docker", "inspect", identifier or name]))
        if (len(rows) != 1 or rows[0].get("Name") != "/"+name
                or (identifier and rows[0].get("Id") != identifier)
                or any(rows[0].get("Config", {}).get("Labels", {}).get(k) != v for k,v in labels.items())):
            raise ValueError("CPU pressure container ownership changed")
        return cpu_pressure_container(rows[0])

    def usage():
        values = dict(line.split() for line in command(["docker", "exec", identifier, "cat", "/sys/fs/cgroup/cpu.stat"]).splitlines())
        value = values.get("usage_usec", "")
        if not value.isdecimal(): raise ValueError("CPU pressure kernel usage is unavailable")
        return int(value)

    if listed(): raise ValueError("CPU pressure container name already exists")
    try:
        args = ["docker", "create", "--name", name, "--cpus", str(quota/1_000_000_000),
                "--memory", "64m", "--pids-limit", "16", "--network", "none", "--read-only",
                "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--user", "65534:65534", "--init"]
        for key,value in labels.items():args += ["--label", key+"="+value]
        # Start idle under an independent wall-clock fuse. Admit the real load only after kernel quota verification.
        args += [image, "/bin/sh", "-c", "exec timeout -s KILL 300 /bin/sh -c 'while :; do sleep 1; done'"]
        attempted = True
        receipt = command(args).strip()
        if not re.fullmatch(r"[0-9a-f]{64}", receipt):raise ValueError("CPU pressure create receipt is invalid")
        identifier = receipt
        current=owned();host=current.get("HostConfig", {})
        if (current.get("Image") != image or current.get("Mounts") != [] or current.get("Config", {}).get("User") != "65534:65534"
                or host.get("NanoCpus") != quota or host.get("Memory") != 64*1024**2 or host.get("PidsLimit") != 16
                or host.get("NetworkMode") != "none" or host.get("ReadonlyRootfs") is not True
                or host.get("Privileged") is not False or host.get("CapDrop") != ["ALL"]):
            raise ValueError("CPU pressure container limits differ from the authorized plan")
        started=time.monotonic()
        command(["docker", "start", identifier])
        maximum=command(["docker", "exec", identifier, "cat", "/sys/fs/cgroup/cpu.max"]).split()
        if (len(maximum) != 2 or not all(v.isdecimal() and int(v)>0 for v in maximum)
                or int(maximum[0])*1_000_000_000 != quota*int(maximum[1])):
            raise ValueError("CPU pressure kernel quota differs from the authorized plan")
        before=usage()
        detail["cpu_limits"]={"logical_cores":cores, "quota_nanos":quota, "kernel_cpu_max":" ".join(maximum), "max_seconds":300, "image":image}
        detail["pressure_started_at"]=time.time()
        command(["docker", "exec", "-d", identifier, "/bin/sh", "-c", "while :; do :; done"])
        yield
        if not owned().get("State", {}).get("Running") or time.monotonic()-started >= 300:
            raise ValueError("CPU pressure exceeded its hard wall-clock deadline")
        detail["cpu_usage_usec"]=usage()-before
        if detail["cpu_usage_usec"] < 1_000_000:raise ValueError("CPU pressure has no material kernel usage")
        detail["cpu_duration_seconds"]=time.monotonic()-started
    finally:
        failing=sys.exc_info()[0] is not None
        if attempted:
            try:
                ids=listed()
                if ids:
                    current=owned()
                    command(["docker", "rm", "-f", "-v", current["Id"]])
                if listed():raise ValueError("CPU pressure has a residual container")
                detail["cpu_released"]=True
                detail["pressure_released_at"]=time.time()
            except (OSError, ValueError, subprocess.SubprocessError):
                detail["cpu_cleanup_incomplete"]=True
                if not failing:raise


def assert_owned(directory):
    ids = command(["docker", "ps", "-aq", "--filter", "label=com.docker.compose.project="+PROJECT]).split()
    if ids:
        for container in json.loads(command(["docker", "inspect", *ids])):
            if container["Config"].get("Labels", {}).get("com.addp.online-runtime") != "platform-node-metrics":
                raise ValueError("refusing an unowned metrics container")


def assert_process_listeners_closed():
    # Application shutdown precedes fixture cleanup. Inspect, never kill or
    # adopt a foreign listener that may have taken a test port.
    address = str(ipaddress.ip_address(os.environ["ADDP_ONLINE_METRICS_NODE_IP"]))
    for port in PROCESS_PORTS.values():
        with socket.socket() as sock:
            sock.settimeout(1)
            if sock.connect_ex((address, port)) == 0:
                raise ValueError("process metrics has a residual listener")



def engine_compose(directory, *args):
    source = import_module("scripts.utils.deployment-inventory")
    return subprocess.run(["docker", "compose", "--project-directory", str(ROOT/"business"), "-p", "business",
                           "--env-file", str(ROOT/"business/.env.example"), "--env-file", str(directory/"engine-source.env"),
                           "-f", str(ROOT/"business/docker-compose.yml"), *args],
                          env=source.observer_environment(), check=True, capture_output=True, text=True, timeout=150).stdout


def engine_source_action(directory, action):
    if os.environ.get("ADDP_ONLINE_NODE_METRICS_CASE") != "engine-admission":
        raise ValueError("Business fixture requires the engine-admission case")
    source=import_module("scripts.utils.deployment-inventory")
    source.local_docker(env=source.observer_environment())
    owner = directory/"engine-source-owner.json"
    expected = {"run_id": os.environ["ADDP_ONLINE_TEST_RUN_ID"], "repository": str(ROOT), "project": "business"}
    if action == "engine-up":
        if owner.exists() or (directory/"engine-source.env").exists():
            raise ValueError("Business source is already owned")
        for args in (["ps","-aq"],["network","ls","-q"],["volume","ls","-q"]):
            if command(["docker",*args,"--filter","label=com.docker.compose.project=business"]).strip():
                raise ValueError("refusing an existing Business source")
        owner.write_text(json.dumps(expected)); owner.chmod(0o600)
        # Normal Business engine initialization only; no application queries or sample generators.
        envfile=directory/"engine-source.env"
        envfile.write_text("POSTGRES_USER=business\nPOSTGRES_DB=business\nPOSTGRES_PORT=5433\nPOSTGRES_IMAGE=postgis/postgis:15-3.4\nPOSTGRES_PASSWORD="+secrets.token_urlsafe(32)+"\n")
        envfile.chmod(0o600)
        engine_compose(directory,"up","-d","--wait","--wait-timeout","120","postgres")
        return
    if not owner.exists():
        for args in (["ps","-aq"],["network","ls","-q"],["volume","ls","-q"]):
            if command(["docker",*args,"--filter","label=com.docker.compose.project=business"]).strip():
                raise ValueError("Business ownership is missing")
        if action=="engine-down": return
        raise ValueError("Business source was not started")
    if json.loads(owner.read_text()) != expected or owner.stat().st_mode & 0o777 != 0o600:
        raise ValueError("Business source owner differs")
    ids=command(["docker","ps","-aq","--no-trunc","--filter","label=com.docker.compose.project=business"]).split()
    for container in ids:
        labels=json.loads(command(["docker","inspect","--format",'{{json .Config.Labels}}',container]))
        if (labels.get("com.docker.compose.project.working_dir")!=str(ROOT/"business")
                or labels.get("com.docker.compose.service")!="postgres"):
            raise ValueError("refusing a foreign Business component")
    if action=="engine-restart":
        if len(ids)!=1: raise ValueError("Business PostgreSQL identity is not unique")
        engine_compose(directory,"restart","--timeout","10","postgres")
        engine_compose(directory,"up","-d","--wait","--wait-timeout","120","postgres")
        source=import_module("scripts.utils.deployment-inventory")
        subprocess.run([sys.executable,str(ROOT/"scripts/utils/deployment-inventory.py"),"refresh","--scope","business","--directory",os.environ.get("ADDP_DEPLOYMENT_DISCOVERY_DIR") or str(ROOT/".dev-state/deployment-discovery")],
                       env=source.observer_environment(), check=True, capture_output=True, text=True, timeout=40)
        return
    engine_compose(directory,"down","--volumes","--remove-orphans")
    for args in (["ps","-aq"],["network","ls","-q"],["volume","ls","-q"]):
        if command(["docker",*args,"--filter","label=com.docker.compose.project=business"]).strip():
            raise ValueError("Business source has residual resources")
    owner.unlink(); (directory/"engine-source.env").unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=("prepare", "query", "up", "down", "engine-up", "engine-restart", "engine-down"))
    args = parser.parse_args()
    directory = boundary()
    if args.action.startswith("engine-"):
        engine_source_action(directory,args.action)
    elif args.action == "prepare":
        prepare(directory)
    elif args.action == "query":
        configure_query(directory)
    elif (directory / "metrics-compose.json").exists():
        assert_owned(directory)
        if args.action == "up":
            compose(directory, "up", "-d", "--wait", "--wait-timeout", "60")
        else:
            cleanup_cpu_pressure(directory)
            compose(directory, "down", "--volumes", "--remove-orphans")
            for kind, cmd in (("container", ["ps", "-aq"]), ("network", ["network", "ls", "-q"]), ("volume", ["volume", "ls", "-q"])):
                if command(["docker", *cmd, "--filter", "label=com.docker.compose.project="+PROJECT]).strip():
                    raise ValueError("metrics fixture has residual " + kind)
            assert_process_listeners_closed()
    elif args.action == "up":
        raise ValueError("metrics fixture has not been prepared")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, subprocess.SubprocessError):
        raise SystemExit("Hosted metrics fixture failed; no credential details are printed")
