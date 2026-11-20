"""Deployment-owned Docker control. The only caller is the API over a private Unix socket."""
import json
import os
import re
import signal
import socket
import threading
import time
from http.server import BaseHTTPRequestHandler
from socketserver import UnixStreamServer

import httpx

SOCKET = "/run/notebook-launcher/control.sock"
JOBS = "/workspace/notebooks"
OWNER = "addp.jupyter.launcher"
JOB = "addp.jupyter.job"
DEADLINE = "addp.jupyter.deadline"


class DockerJobs:
    def __init__(self):
        self.http = httpx.Client(transport=httpx.HTTPTransport(uds="/var/run/docker.sock"),
                                 base_url="http://docker", timeout=30)
        if int(self.call("GET", "/version")["Version"].split(".", 1)[0]) < 28:
            raise RuntimeError("Notebook internal isolated bridges require Docker Engine 28 or later")
        deployed = self.call("GET", f"/containers/{socket.gethostname()}/json")
        self.image = deployed["Image"]
        self.project = deployed["Config"]["Labels"]["com.docker.compose.project"]
        self.lock = threading.Lock()
        self.max_ttl = int(os.environ.get("JUPYTER_SESSION_TTL_SECONDS", "3600"))

    def call(self, method, path, **kwargs):
        r = self.http.request(method, path, **kwargs)
        r.raise_for_status()
        return r.json() if r.content else None

    def resources(self, kind, job=None):
        labels = [f"{OWNER}={self.project}"]
        if job:
            labels.append(f"{JOB}={job}")
        params = {"filters": json.dumps({"label": labels})}
        if kind == "containers":
            params["all"] = "1"
        return self.call("GET", f"/{kind}/json" if kind == "containers" else "/networks", params=params)

    def remove(self, job):
        for c in self.resources("containers", job):
            self.call("DELETE", f"/containers/{c['Id']}", params={"force": "1", "v": "1"})
        for net in self.resources("networks", job):
            details = self.call("GET", f"/networks/{net['Id']}")
            for cid in details["Containers"] or {}:
                labels = self.call("GET", f"/containers/{cid}/json")["Config"]["Labels"]
                if (labels.get("com.docker.compose.project") != self.project
                        or labels.get("com.docker.compose.service") != "jupyter-engine"):
                    raise RuntimeError("unexpected member in Notebook network")
                self.call("POST", f"/networks/{net['Id']}/disconnect", json={"Container": cid, "Force": True})
            self.call("DELETE", f"/networks/{net['Id']}")

    def control_api(self):
        filters = {"label": [f"com.docker.compose.project={self.project}",
                             "com.docker.compose.service=jupyter-engine"]}
        api, = self.call("GET", "/containers/json", params={"filters": json.dumps(filters)})
        if api["ImageID"] != self.image:
            raise RuntimeError("Notebook API and launcher image IDs differ")
        volume, = [mount["Name"] for mount in api["Mounts"]
                   if mount["Destination"] == JOBS and mount["Type"] == "volume"]
        return api["Id"], volume

    def create(self, job, mode, ttl):
        if mode not in ("lab", "batch") or type(ttl) is not int or not 1 <= ttl <= (
                self.max_ttl if mode == "lab" else 7200):
            raise ValueError("invalid execution scope")
        if self.resources("containers", job) or self.resources("networks", job):
            raise FileExistsError("job already allocated")
        labels = {OWNER: self.project, JOB: job, DEADLINE: str(time.time() + ttl + 90)}
        network = "none"
        try:
            api, volume = self.control_api()
            if mode == "lab":
                network = self.call("POST", "/networks/create", json={
                    "Name": f"{self.project}-notebook-{job}", "Driver": "bridge", "Internal": True, "EnableIPv6": False,
                    "Options": {"com.docker.network.bridge.gateway_mode_ipv4": "isolated"}, "Labels": labels,
                })["Id"]
                self.call("POST", f"/networks/{network}/connect", json={
                    "Container": api, "EndpointConfig": {"Aliases": ["nb-control"]}})
            name = f"nb-{job}"
            host = {"NetworkMode": network, "ReadonlyRootfs": True, "CapDrop": ["ALL"],
                    "SecurityOpt": ["no-new-privileges"], "PidsLimit": 256, "Memory": 2147483648,
                    "NanoCpus": 2000000000, "LogConfig": {"Type": "none"}, "Tmpfs": {"/tmp": "rw,nosuid,nodev,mode=1777"},
                    "Mounts": [{"Type": "volume", "Source": volume, "Target": "/job",
                                "VolumeOptions": {"NoCopy": True, "Subpath": job}}]}
            config = {"Image": self.image, "User": "10001:10001", "WorkingDir": "/job",
                      "Entrypoint": ["python", "-I", "/app/worker.py"], "Cmd": [mode],
                      "Env": ["HOME=/job", "PYTHONNOUSERSITE=1"], "Labels": labels, "HostConfig": host,
                      "Healthcheck": {"Test": ["NONE"]}}
            if mode == "lab":
                config["NetworkingConfig"] = {"EndpointsConfig": {network: {"Aliases": [name]}}}
            cid = self.call("POST", "/containers/create", json=config)["Id"]
            self.call("POST", f"/containers/{cid}/start")
        except Exception:
            self.remove(job)
            raise

    def expired(self, all_jobs=False):
        with self.lock:
            for item in self.resources("containers") + self.resources("networks"):
                labels = item["Labels"]
                if all_jobs or float(labels[DEADLINE]) <= time.time():
                    self.remove(labels[JOB])


class Handler(BaseHTTPRequestHandler):
    def dispatch(self):
        if self.command == "GET" and self.path == "/ready":
            try:
                self.server.jobs.control_api()
            except Exception:
                self.send_error(503)
                return
            self.send_response(204)
            self.end_headers()
            return
        match = re.fullmatch(r"/jobs/([a-f0-9][a-f0-9-]{0,59})", self.path)
        if not match:
            self.send_error(404)
            return
        key = match[1]
        try:
            with self.server.jobs.lock:
                if self.command == "PUT":
                    size = int(self.headers.get("Content-Length", "0"))
                    if not 0 < size <= 128:
                        raise ValueError("invalid request size")
                    data = json.loads(self.rfile.read(size))
                    if not isinstance(data, dict) or set(data) != {"mode", "ttl"}:
                        raise ValueError("invalid launch request")
                    self.server.jobs.create(key, data["mode"], data["ttl"])
                elif self.command == "DELETE":
                    self.server.jobs.remove(key)
                else:
                    found = self.server.jobs.resources("containers", key)
                    if not found:
                        self.send_error(404)
                        return
                    state = self.server.jobs.call("GET", f"/containers/{found[0]['Id']}/json")["State"]
                    body = json.dumps({"running": state["Running"], "exit_code": state["ExitCode"]}).encode()
                    self.send_response(200)
                    self.send_header("Content-Length", str(len(body)))
                    self.end_headers()
                    self.wfile.write(body)
                    return
            self.send_response(204)
            self.end_headers()
        except FileExistsError:
            self.send_error(409)
        except (ValueError, KeyError):
            self.send_error(400)
        except Exception as exc:
            print("Notebook container operation failed:", type(exc).__name__, flush=True)
            self.send_error(503)

    do_PUT = do_GET = do_DELETE = dispatch

    def log_message(self, *args):
        pass


if __name__ == "__main__":
    jobs = DockerJobs()
    os.makedirs(os.path.dirname(SOCKET), exist_ok=True)
    if os.path.exists(SOCKET):
        os.unlink(SOCKET)
    stop = threading.Event()
    signal.signal(signal.SIGTERM, lambda *_: stop.set())
    with UnixStreamServer(SOCKET, Handler) as server:
        server.jobs = jobs
        os.chown(SOCKET, 10001, 10001)
        os.chmod(SOCKET, 0o600)
        server.timeout = 1
        sweep = 0
        try:
            while not stop.is_set():
                server.handle_request()
                if time.monotonic() >= sweep:
                    try:
                        jobs.expired()
                    except (OSError, httpx.HTTPError, RuntimeError) as exc:
                        print("Notebook expiry check failed:", type(exc).__name__, flush=True)
                    sweep = time.monotonic() + 30
        finally:
            jobs.expired(all_jobs=True)
