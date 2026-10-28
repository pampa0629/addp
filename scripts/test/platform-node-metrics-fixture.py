"""Owned TLS transport and center deployment for the real Hosted metrics suite."""
from __future__ import annotations

import argparse
import hashlib
from importlib.util import module_from_spec, spec_from_file_location
import ipaddress
import json
import os
import platform
import secrets
import signal
import shlex
import socket
import subprocess
import sys
import time
import re
from contextlib import contextmanager
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SOURCE_SPEC = spec_from_file_location("node_metrics_deployment_source", ROOT / "scripts/utils/deployment-inventory.py")
SOURCE = module_from_spec(SOURCE_SPEC)
sys.modules[SOURCE_SPEC.name] = SOURCE
SOURCE_SPEC.loader.exec_module(SOURCE)
PROJECT = "addp-metrics-online"
PROCESS_PORTS = {"system": 19101, "monitor": 19102}
WORKER_PROFILES = {
    "quality-workers": {"module": "quality", "ports": {"quality": 19103, "quality-worker-1": 19104, "quality-worker-2": 19105},
                        "replace": ["quality-worker-1"],
                        "env": {"ADDP_DEV_QUALITY_WORKER_REPLICAS": "2", "QUALITY_WORKER_CONCURRENCY": "1"}},
    "meta-workers": {"module": "meta", "ports": {"meta": 19103, "meta-worker-1": 19104, "meta-worker-2": 19105},
                     "replace": ["meta-worker-1"],
                     "env": {"ADDP_DEV_META_WORKER_REPLICAS": "2", "META_WORKER_CONCURRENCY": "1"}},
    "transfer-workers": {"module": "transfer", "ports": {"transfer": 19103, "transfer-bounded-worker-1": 19104,
                         "transfer-bounded-worker-2": 19105, "transfer-continuous-worker-1": 19106,
                         "transfer-continuous-worker-2": 19107},
                         "replace": ["transfer-bounded-worker-1", "transfer-continuous-worker-1"],
                         "env": {"ADDP_DEV_TRANSFER_BOUNDED_WORKER_REPLICAS": "2",
                                 "ADDP_DEV_TRANSFER_CONTINUOUS_WORKER_REPLICAS": "2",
                                 "TRANSFER_BOUNDED_WORKER_CONCURRENCY": "1", "TRANSFER_CONTINUOUS_WORKER_CAPACITY": "1",
                                 "TRANSFER_CONTINUOUS_WORKER_INSTANCE_ID": ""}},
}

WORKER_PROFILES["service-availability"] = WORKER_PROFILES["meta-workers"]


def process_units():
    result = {name: (name, "backend", port) for name, port in PROCESS_PORTS.items()}
    profile = WORKER_PROFILES.get(os.environ.get("ADDP_ONLINE_NODE_METRICS_CASE"))
    if profile:
        module = profile["module"]
        result.update({unit: (module, "backend" if unit == module else "worker", port)
                       for unit, port in profile["ports"].items()})
    return result


POSTGRES_PORTS = {"infra": 19401, "business": 19402}


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


def command(args, *, timeout=120):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=timeout).stdout


def native_node_source_proof():
    """Verify the actual owned exporter, not only a rendered Compose declaration."""
    boundary()
    if platform.system() != "Linux":
        raise ValueError("native node evidence requires Linux")
    info = json.loads(command(["docker", "info", "--format", "{{json .}}"], timeout=5))
    if (info.get("OSType") != "linux" or info.get("KernelVersion") != platform.release()
            or "Docker Desktop" in info.get("OperatingSystem", "")):
        raise ValueError("native node evidence requires the current host kernel")
    ids = command(["docker", "ps", "-aq", "--no-trunc", "--filter",
                   "label=com.docker.compose.project=addp-node-metrics", "--filter",
                   "label=com.docker.compose.service=node-exporter"], timeout=5).split()
    if len(ids) != 1 or not re.fullmatch(r"[0-9a-f]{64}", ids[0]):
        raise ValueError("native node source ownership is ambiguous")
    rows = json.loads(command(["docker", "inspect", ids[0]], timeout=5))
    if len(rows) != 1:
        raise ValueError("native node source inspection is incomplete")
    row = rows[0]
    labels = row.get("Config", {}).get("Labels", {})
    expected = {"com.docker.compose.project": "addp-node-metrics",
                "com.docker.compose.service": "node-exporter", "io.addp.node-metrics.owner": str(ROOT)}
    if (row.get("Id") != ids[0] or any(labels.get(k) != v for k, v in expected.items())
            or row.get("State", {}).get("Running") is not True):
        raise ValueError("native node source is not the current owned exporter")
    host, config = row.get("HostConfig", {}), row.get("Config", {})
    if (host.get("NetworkMode") != "host" or host.get("PidMode") != "host"
            or host.get("ReadonlyRootfs") is not True or host.get("Privileged") is not False
            or host.get("CapDrop") != ["ALL"] or config.get("User") != "65534:65534"):
        raise ValueError("native node source isolation does not match the standard deployment")
    mounts = [m for m in row.get("Mounts", []) if m.get("Destination") == "/host"]
    if (len(mounts) != 1 or mounts[0].get("Type") != "bind" or mounts[0].get("Source") != "/"
            or mounts[0].get("RW") is not False or mounts[0].get("Propagation") != "rslave"
            or any(m.get("Destination", "").startswith("/host/") for m in row.get("Mounts", []))):
        raise ValueError("native node source lacks the read-only host root")
    args = config.get("Cmd", [])
    if not isinstance(args, list) or not all(isinstance(arg, str) for arg in args):
        raise ValueError("native node collector arguments are invalid")
    for key, path in (("rootfs", "/host"), ("procfs", "/host/proc"), ("sysfs", "/host/sys")):
        option = "--path."+key
        if [arg for arg in args if arg == option or arg.startswith(option+"=")] != [option+"="+path]:
            raise ValueError("native node collector paths do not reference the host")
    for option in ("--collector.filesystem", "--collector.disable-defaults"):
        negative = option.replace("--", "--no-", 1)
        if [arg for arg in args if arg in (option, negative) or arg.startswith((option+"=", negative+"="))] != [option]:
            raise ValueError("native filesystem collector is not explicitly enabled")
    if any(arg.startswith("--collector.textfile") for arg in args):
        raise ValueError("native source cannot use a textfile substitute")
    return {"schema_version": "addp.native-node-source/v1", "native_linux": True,
            "source_id_sha256": hashlib.sha256(ids[0].encode()).hexdigest(),
            "host_network_pid": True, "readonly_host_root": True, "host_collector_paths": True,
            "filesystem_collector": True, "nonprivileged_exporter": True}


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
    engine_case = os.environ.get("ADDP_ONLINE_NODE_METRICS_CASE") == "engine-admission"
    units = process_units()
    for port in (9444, 19100, *(entry[2] for entry in units.values()), *(POSTGRES_PORTS.values() if engine_case else ())):
        with socket.socket() as sock:
            sock.bind((address, port))
    for name in ("center-tls", "node-tls", "deployment", "control", "admission", *("process-"+unit for unit in units)):
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
    for folder, issuer in (("center-tls", center), ("node-tls", source), *(("process-"+unit, source) for unit in units)):
        (directory / folder / "ca.crt").write_bytes(Path(str(issuer)+".crt").read_bytes())
        certificate(issuer, directory / folder, "server", True)
    if engine_case:
        for scope in POSTGRES_PORTS:
            folder = directory / ("postgres-" + scope)
            folder.mkdir(mode=0o755)
            (folder / "ca.crt").write_bytes(Path(str(source)+".crt").read_bytes())
            certificate(source, folder, "server", True)
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
        "MONITOR_METRICS_ALLOWED_CIDRS": address+"/32", "MONITOR_METRICS_ALLOWED_PORTS": ",".join(map(str, (19100, *(entry[2] for entry in units.values())))),
        "ADDP_DEV_PROCESS_METRICS_PLAN": json.dumps({
            unit: {"module_name": module, "role": role, "listen": address+":"+str(port),
             "endpoint": "https://"+address+":"+str(port)+"/metrics", "tls_dir": str(directory / ("process-"+unit))}
            for unit, (module, role, port) in units.items()}, separators=(",", ":")),
        "MONITOR_METRICS_CA_FILE": str(directory / "admission/ca.crt"),
        "MONITOR_METRICS_CLIENT_CERT_FILE": str(directory / "admission/client.crt"),
        "MONITOR_METRICS_CLIENT_KEY_FILE": str(directory / "admission/client.key"),
    }
    profile = WORKER_PROFILES.get(os.environ.get("ADDP_ONLINE_NODE_METRICS_CASE"))
    if profile:
        values.update(profile["env"])
    if engine_case:
        values["MONITOR_METRICS_ALLOWED_PORTS"] += "," + ",".join(map(str, POSTGRES_PORTS.values()))
        values["ADDP_POSTGRES_METRICS_STATE_DIR"] = str(directory / "postgres-sources")
        values["ADDP_POSTGRES_AUTH_STATE_DIR"] = str(directory / "postgres-auth")
    os.environ.update(values)
    command(["python3", str(ROOT / "scripts/infra/generate-metrics-config.py")])
    # The center belongs to the standard Infra lifecycle; this fixture only
    # supplies protected transport to the real Gateway.
    # Read the production transport directly. A TLS-only fixture must not
    # interpolate the whole T2 stack or require unrelated SQL source secrets.
    transport = json.loads(command(["docker", "compose", "--env-file", "/dev/null", "-f",
                                   str(ROOT / "scripts/infra/metrics-control.yml"),
                                   "config", "--format", "json"]))["services"]["metrics-control"]
    transport.pop("ports", None)
    transport.pop("networks", None)
    transport.pop("restart", None)
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
    return subprocess.run(["docker", "compose", "--project-directory", str(ROOT/"business"), "-p", "business",
                           "--env-file", str(ROOT/"business/.env.example"), "--env-file", str(directory/"engine-source.env"),
                           "-f", str(ROOT/"business/docker-compose.yml"), *args],
                          env=SOURCE.observer_environment(), check=True, capture_output=True, text=True, timeout=150).stdout


def engine_metrics_action(directory, action):
    """Use the production deployment owner; never install via SQL test hooks."""
    require_state = str(directory / "postgres-sources")
    if os.environ.get("ADDP_POSTGRES_METRICS_STATE_DIR") != require_state:
        raise ValueError("PostgreSQL source state must belong to this Hosted fixture")
    operation = "down" if action in ("engine-down", "engine-metrics-pause") else "up"
    scopes = ("business",) if action in ("engine-metrics-pause", "engine-metrics-resume") else ("business", "infra")
    failed = False
    for scope in scopes:
        env = dict(SOURCE.observer_environment(), POSTGRES_USER="business" if scope == "business" else os.environ["POSTGRES_USER"],
                   ADDP_POSTGRES_METRICS_ENABLED="true", ADDP_POSTGRES_METRICS_STATE_DIR=require_state,
                   ADDP_POSTGRES_AUTH_STATE_DIR=str(directory / "postgres-auth"),
                   ADDP_POSTGRES_METRICS_TLS_DIR=str(directory / ("postgres-" + scope)),
                   ADDP_POSTGRES_METRICS_LISTEN=os.environ["ADDP_ONLINE_METRICS_NODE_IP"]+":"+str(POSTGRES_PORTS[scope]))
        try:
            subprocess.run([sys.executable, str(ROOT / "scripts/infra/postgres-observation/lifecycle.py"), operation, "--scope", scope],
                           env=env, check=True, capture_output=True, text=True, timeout=210)
        except (OSError, subprocess.SubprocessError):
            if action != "engine-down": raise ValueError("Owned PostgreSQL source lifecycle failed") from None
            failed = True
        if action != "engine-down":
            subprocess.run([sys.executable, str(ROOT / "scripts/utils/deployment-inventory.py"), "refresh", "--scope", scope,
                            "--directory", os.environ.get("ADDP_DEPLOYMENT_DISCOVERY_DIR") or str(ROOT / ".dev-state/deployment-discovery")],
                           env=SOURCE.observer_environment(), check=True, capture_output=True, text=True, timeout=40)
    if failed:
        raise ValueError("Owned PostgreSQL source cleanup failed")


def engine_source_action(directory, action):
    if os.environ.get("ADDP_ONLINE_NODE_METRICS_CASE") != "engine-admission":
        raise ValueError("Business fixture requires the engine-admission case")
    print("Engine source " + action + ": validate workflow ownership", flush=True)
    if any(re.fullmatch(r"[1-9][0-9]{0,19}", os.environ.get(key, "")) is None for key in ("GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT")):
        raise ValueError("engine source requires its Hosted workflow run and attempt")
    expected = {"github_run_id": os.environ["GITHUB_RUN_ID"], "github_run_attempt": os.environ["GITHUB_RUN_ATTEMPT"],
                "repository": str(ROOT), "project": "business"}
    print("Engine source " + action + ": validate local Docker", flush=True)
    SOURCE.local_docker(env=SOURCE.observer_environment())
    owner = directory/"engine-source-owner.json"
    if action == "engine-up":
        if owner.exists() or (directory/"engine-source.env").exists():
            raise ValueError("Business source is already owned")
        for args in (["ps","-aq"],["network","ls","-q"],["volume","ls","-q"]):
            if command(["docker",*args,"--filter","label=com.docker.compose.project=business"]).strip():
                raise ValueError("refusing an existing Business source")
        # Normal Business engine initialization only; no application queries or sample generators.
        envfile=directory/"engine-source.env"
        auth = subprocess.run([sys.executable, str(ROOT / "scripts/infra/postgres-observation/authentication.py"), "prepare-business"],
                              env=dict(SOURCE.observer_environment(), ADDP_POSTGRES_AUTH_STATE_DIR=str(directory / "postgres-auth")),
                              check=True, capture_output=True, text=True, timeout=15).stdout.strip()
        if auth != str(directory / "postgres-auth/runtime"):
            raise ValueError("Business authentication directory escaped the owned fixture")
        owner.write_text(json.dumps(expected)); owner.chmod(0o600)
        envfile.write_text("POSTGRES_USER=business\nPOSTGRES_DB=business\nPOSTGRES_PORT=5433\nPOSTGRES_IMAGE=postgis/postgis:15-3.4\nPOSTGRES_PASSWORD="+secrets.token_urlsafe(32)+"\nADDP_BUSINESS_POSTGRES_AUTH_DIR="+auth+"\n")
        envfile.chmod(0o600)
        print("Engine source engine-up: start owned PostgreSQL", flush=True)
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
    if action in ("engine-metrics-up", "engine-metrics-pause", "engine-metrics-resume"):
        engine_metrics_action(directory, action)
        return
    if action=="engine-restart":
        if len(ids)!=1: raise ValueError("Business PostgreSQL identity is not unique")
        print("Engine source engine-restart: restart owned PostgreSQL and refresh source", flush=True)
        engine_compose(directory,"restart","--timeout","10","postgres")
        engine_compose(directory,"up","-d","--wait","--wait-timeout","120","postgres")
        subprocess.run([sys.executable,str(ROOT/"scripts/utils/deployment-inventory.py"),"refresh","--scope","business","--directory",os.environ.get("ADDP_DEPLOYMENT_DISCOVERY_DIR") or str(ROOT/".dev-state/deployment-discovery")],
                       env=SOURCE.observer_environment(), check=True, capture_output=True, text=True, timeout=40)
        return
    print("Engine source engine-down: remove owned PostgreSQL and verify zero residuals", flush=True)
    metrics_failed = False
    try:
        engine_metrics_action(directory, action)
    except (ValueError, OSError, subprocess.SubprocessError):
        metrics_failed = True
    engine_compose(directory,"down","--volumes","--remove-orphans")
    for args in (["ps","-aq"],["network","ls","-q"],["volume","ls","-q"]):
        if command(["docker",*args,"--filter","label=com.docker.compose.project=business"]).strip():
            raise ValueError("Business source has residual resources")
    owner.unlink(); (directory/"engine-source.env").unlink(missing_ok=True)
    if metrics_failed:
        raise ValueError("PostgreSQL source cleanup failed; core Business destruction was still attempted")


# Fault injection is a private fixture action, never a product lifecycle option.
PAUSED_WORKERS = 'availability-paused-workers.json'
AVAILABILITY_UNITS = ('meta-worker-1', 'meta-worker-2')


def worker_process_record(unit):
    if unit not in AVAILABILITY_UNITS:
        raise ValueError('invalid availability Worker unit')
    path = ROOT / '.dev-pids' / (unit+'.pid')
    if path.is_symlink():
        raise ValueError('Worker PID record cannot be a symlink')
    raw = path.read_text().strip()
    if re.fullmatch(r'[1-9][0-9]*', raw) is None or int(raw) <= 1:
        raise ValueError('invalid Worker PID')
    pid = int(raw)
    proc = Path('/proc') / str(pid)
    binary = ROOT / '.dev-bins/addp-meta-worker'
    if proc.joinpath('cwd').resolve(strict=True) != ROOT or proc.joinpath('exe').resolve(strict=True) != binary.resolve(strict=True):
        raise ValueError('Worker belongs to another checkout or binary')
    stat = proc.joinpath('stat').read_text().rsplit(')', 1)[1].split()
    if stat[0] in {'Z', 'X'}:
        raise ValueError('Worker is not live')
    return {'unit': unit, 'pid': pid, 'start_ticks': stat[19]}


def availability_worker_boundary(directory):
    if boundary() != directory or os.environ.get('ADDP_ONLINE_NODE_METRICS_CASE') != 'service-availability':
        raise ValueError('Worker pause requires the dedicated Hosted case')
    if not hasattr(os, 'pidfd_open') or not hasattr(signal, 'pidfd_send_signal'):
        raise ValueError('Worker pause requires Linux pidfd support')


def resume_paused_workers(directory):
    availability_worker_boundary(directory)
    path = directory / PAUSED_WORKERS
    if not path.exists() and not path.is_symlink():
        return
    if path.is_symlink() or path.stat().st_mode & 0o777 != 0o600 or path.stat().st_size > 2048:
        raise ValueError('invalid private Worker recovery record')
    value = json.loads(path.read_text())
    if (not isinstance(value, dict) or set(value) != {'root', 'workers'} or value['root'] != str(ROOT)
            or not isinstance(value['workers'], list) or not 1 <= len(value['workers']) <= 2):
        raise ValueError('invalid Worker recovery scope')
    seen, failure = set(), False
    for row in value['workers']:
        try:
            if (not isinstance(row, dict) or set(row) != {'unit', 'pid', 'start_ticks'} or row['unit'] not in AVAILABILITY_UNITS
                    or type(row['pid']) is not int or row['pid'] <= 1 or row['unit'] in seen):
                raise ValueError('invalid Worker recovery identity')
            seen.add(row['unit'])
            # A dead process is already restored for cleanup purposes; never signal a reused PID.
            if not Path('/proc', str(row['pid'])).exists():
                continue
            fd = os.pidfd_open(row['pid'])
            try:
                if worker_process_record(row['unit']) != row:
                    raise ValueError('Worker recovery identity changed')
                signal.pidfd_send_signal(fd, signal.SIGCONT)
            finally:
                os.close(fd)
        except (OSError, ValueError, KeyError, IndexError):
            failure = True
    if failure:
        raise ValueError('Worker recovery incomplete; record retained')
    path.unlink()


@contextmanager
def availability_pauses(directory):
    availability_worker_boundary(directory)
    path = directory / PAUSED_WORKERS
    if path.exists() or path.is_symlink():
        raise ValueError('Worker recovery record already exists')
    records, fds, paused = {}, {}, []
    def act(unit, paused_state):
        if unit not in records or type(paused_state) is not bool:
            raise ValueError('invalid Worker pause request')
        if worker_process_record(unit) != records[unit]:
            raise ValueError('Worker identity changed before fault injection')
        if paused_state:
            if unit in paused:
                raise ValueError('Worker is already paused')
            # Persist before SIGSTOP; interruption cannot leave an unrecorded paused process.
            paused.append(unit)
        elif unit not in paused:
            raise ValueError('Worker is not paused')
        data = {'root': str(ROOT), 'workers': [records[u] for u in paused]}
        temporary = path.with_suffix('.tmp')
        try:
            with open(temporary, 'x', opener=lambda name, flags: os.open(name, flags, 0o600)) as out:
                json.dump(data, out); out.flush(); os.fsync(out.fileno())
            temporary.replace(path)
        finally:
            temporary.unlink(missing_ok=True)
        signal.pidfd_send_signal(fds[unit], signal.SIGSTOP if paused_state else signal.SIGCONT)
        # Retain resumed records too: outer recovery can safely send SIGCONT again.
    try:
        for unit in AVAILABILITY_UNITS:
            row = worker_process_record(unit)
            fd = os.pidfd_open(row['pid'])
            fds[unit] = fd
            if worker_process_record(unit) != row or row['pid'] in [r['pid'] for r in records.values()]:
                raise ValueError('Worker identity raced or is duplicated')
            records[unit] = row
        yield act
    finally:
        try:
            # Attempt every pinned original process even if one has exited.
            failed = False
            for unit in paused:
                try:
                    signal.pidfd_send_signal(fds[unit], signal.SIGCONT)
                except ProcessLookupError:
                    pass
                except OSError:
                    failed = True
            if failed:
                raise ValueError('pinned Worker recovery incomplete')
            if path.exists():
                resume_paused_workers(directory)
        finally:
            for fd in fds.values():
                os.close(fd)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=("prepare", "query", "up", "down", "engine-up", "engine-restart", "engine-down", "engine-metrics-up", "engine-metrics-pause", "engine-metrics-resume", "resume-workers"))
    args = parser.parse_args()
    directory = boundary()
    if args.action == "resume-workers":
        resume_paused_workers(directory)
    elif args.action.startswith("engine-"):
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
