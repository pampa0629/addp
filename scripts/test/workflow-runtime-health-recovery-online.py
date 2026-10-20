#!/usr/bin/env python3
"""Three real native Runtime faults on the dedicated macOS Online deployment.

Usage: make test-online ONLINE_SUITE=workflow-runtime-health-recovery
       python3 scripts/test/workflow-runtime-health-recovery-online.py --restore-only
No development environment configuration or lifecycle is owned by this script.
"""

from __future__ import annotations

import argparse
import hashlib
import importlib.util
import json
import os
import signal
import stat
import subprocess
import sys
import time
from contextlib import contextmanager
from pathlib import Path
from urllib.parse import urlsplit


ROOT = Path(__file__).resolve().parents[2]
SUITE = "workflow-runtime-health-recovery"
CASES = (
    ("pointcloud", "pdal", "POINTCLOUD_PDAL_BIN"),
    ("model3d", "ifcconvert", "MODEL3D_IFC_CONVERTER_BIN"),
    ("document", "libreoffice", "DOCUMENT_LIBREOFFICE_BIN"),
)


class SuiteError(RuntimeError):
    pass


def require(condition, message):
    if not condition:
        raise SuiteError(message)


def external(path):
    resolved = Path(path).resolve()
    require(not resolved.is_relative_to(ROOT), "Online paths must be outside this checkout")
    return resolved


def isolation():
    require(os.environ.get("ADDP_ONLINE_HOST") == "1", "dedicated Online host is required")
    require(sys.platform == "darwin", "native recovery requires the dedicated macOS Runner")
    require(not (ROOT / ".env").exists(), "repository .env is forbidden")
    env_file = external(os.environ["ADDP_ONLINE_ENV_FILE"])
    require(env_file.is_file(), "external Online environment file is required")
    spec = importlib.util.spec_from_file_location("runtime_health_preflight", ROOT / "scripts/test/online-preflight.py")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    module.validate_online_environment(require_run_id=False)
    for name in ("SYSTEM_URL", "GATEWAY_URL"):
        module.parse_service(name.lower() + "=" + os.environ[name])
    return external(os.environ["ADDP_ONLINE_ARTIFACT_DIR"])


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def file_identity(path):
    require(not path.is_symlink(), "tool must be a regular owned file")
    info = path.stat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == os.getuid(), "tool ownership mismatch")
    return {"device": info.st_dev, "inode": info.st_ino,
            "mode": stat.S_IMODE(info.st_mode), "sha256": digest(path)}


def validate_tool(path):
    require(path == path.resolve(), "tool path must not traverse symlinks")
    relative = path.relative_to(ROOT).as_posix()
    parts = Path(relative).parts
    allowed = (
        relative == "engines/pointcloud-workflow/venv/bin/pdal"
        or (len(parts) == 5 and Path(relative).match(".dev-state/model3d-native/*/bin/IfcConvert"))
        or (len(parts) == 7 and Path(relative).match(".dev-state/document-native/*/LibreOffice.app/Contents/MacOS/soffice"))
    )
    require(allowed, "fault target is not a checkout-private Runtime tool")


class ToolFault:
    """Write ahead journal, exact inode restore, and refusal to overwrite replacements."""

    def __init__(self, journal):
        self.journal = journal

    def restore(self):
        if not self.journal.exists():
            return
        require(not self.journal.is_symlink(), "unsafe recovery journal")
        record = json.loads(self.journal.read_text())
        path = Path(record["path"])
        validate_tool(path)
        backup = path.with_name(path.name + ".addp-health-recovery")
        if backup.exists():
            require(file_identity(backup) == record["identity"], "backup tool identity changed")
            require(not path.exists() and not path.is_symlink(), "tool replaced concurrently; refusing overwrite")
            backup.rename(path)
        require(file_identity(path) == record["identity"], "restored tool identity mismatch")
        self.journal.unlink()

    @contextmanager
    def missing(self, path):
        validate_tool(path)
        identity = file_identity(path)
        backup = path.with_name(path.name + ".addp-health-recovery")
        require(not backup.exists() and not backup.is_symlink(), "stale tool backup exists")
        running = subprocess.check_output(["ps", "-axo", "comm="], text=True)
        require(str(path) not in running.splitlines(), "tool is executing; fault refused")
        with self.journal.open("x") as stream:
            json.dump({"path": str(path), "identity": identity}, stream)
            stream.flush()
            os.fsync(stream.fileno())
        try:
            path.rename(backup)
            yield
        finally:
            self.restore()


def native_command(name, code, environment=None):
    runtime = ROOT / f"engines/{name}-workflow"
    # Only source the existing read-only environment projection, never prepare,
    # launch, restart or keepalive. Function names come from the closed CASES set.
    require(name in {case[0] for case in CASES}, "unknown native Runtime")
    command = ["bash", "-c", "set -e; ROOT_DIR=$1; name=$2; code=$3; "
               'source "$ROOT_DIR/scripts/dev/$name-workflow.sh"; '
               '"addp_${name}_native_environment"; '
               'exec "$ROOT_DIR/engines/$name-workflow/venv/bin/python" -c "$code"',
               "_", str(ROOT), name, code]
    environment = dict(os.environ if environment is None else environment)
    environment.pop("RUNTIME_PUBLIC_PORT", None)
    result = subprocess.run(command, cwd=runtime, env=environment, capture_output=True, text=True, timeout=40)
    require(result.returncode == 0, f"{name} native helper failed (output withheld)")
    return json.loads(result.stdout.strip().splitlines()[-1])


def process_snapshot(names):
    result = {}
    for name in names:
        pid = int((ROOT / f".dev-pids/{name}.pid").read_text())
        os.kill(pid, 0)
        started = subprocess.check_output(["ps", "-p", str(pid), "-o", "lstart="], text=True).strip()
        cwd = subprocess.check_output(["lsof", "-a", "-p", str(pid), "-d", "cwd", "-Fn"], text=True)
        expected = ROOT / f"engines/{name.removesuffix('-workflow-engine')}-workflow" if name.endswith("-workflow-engine") else ROOT
        require("n" + str(expected) in cwd.splitlines(), f"{name} PID belongs to another checkout")
        require(bool(started), f"{name} process start time missing")
        result[name] = {"pid": pid, "started_at": started}
    return result


def validate_health(status, body, missing=None):
    require(isinstance(body, dict), "health must be an object")
    require(isinstance(body.get("service"), str) and bool(body["service"])
            and isinstance(body.get("version"), str) and bool(body["version"]), "health identity missing")
    require("conversion_ready" not in body, "obsolete health readiness flag")
    require(status == (503 if missing else 200) and body.get("status") == ("unhealthy" if missing else "healthy"),
            "health HTTP/status mismatch")
    facts = body.get("dependencies")
    require(isinstance(facts, dict) and bool(facts), "dependency facts missing")
    for fact in facts.values():
        require(isinstance(fact, dict) and set(fact) <= {"available", "required", "version", "details"},
                "invalid dependency facts")
        require(type(fact.get("available")) is bool and type(fact.get("required")) is bool,
                "dependency facts must be booleans")
        for field in ("version", "details"):
            require(field not in fact or isinstance(fact[field], str), "invalid dependency diagnostics")
    unavailable = {key for key, fact in facts.items() if fact["required"] and not fact["available"]}
    require(unavailable == ({missing} if missing else set()), "unexpected required dependency failures")
    return {"http_status": status, "status": body["status"], "service": body["service"],
            "version": body["version"], "dependencies": facts}


class Observer:
    def __init__(self):
        import httpx
        from addp_common.client.service_token import SyncOAuthServiceTokenSource
        self.tenant = int(os.environ["ADDP_ONLINE_TEST_TENANT_ID"])
        self.system = os.environ["SYSTEM_URL"].rstrip("/")
        self.client = httpx.Client(timeout=10, trust_env=False, follow_redirects=False)
        self.source = SyncOAuthServiceTokenSource(self.system, "addp-develop", os.environ["DEVELOP_SERVICE_CLIENT_SECRET"])
        self.names = ["system", "gateway"] + [f"{name}-workflow-engine" for name, _, _ in CASES]
        self.processes = process_snapshot(self.names)
        self.runtimes = {}
        for name, dep, variable in CASES:
            paths = native_command(name, f'import os,json; print(json.dumps({{"tool":os.environ[{variable!r}]}}))')
            tool = Path(paths["tool"])
            validate_tool(tool)
            self.runtimes[name] = {"tool": tool, "dep": dep}

    def close(self):
        self.source.close()
        self.client.close()

    def stable(self):
        require(process_snapshot(self.names) == self.processes, "managed process identity changed")

    def descriptors(self):
        response = self.client.get(self.system + "/api/v1/system/runtime/engine-descriptors?page_size=100",
                                   headers={"Authorization": "Bearer " + self.source.token(self.tenant)})
        require(response.status_code == 200, "descriptor observation failed")
        body = response.json()
        require(type(body.get("total")) is int and body["total"] <= 100
                and isinstance(body.get("data"), list) and len(body["data"]) == body["total"],
                "descriptor page is incomplete")
        return body["data"]

    def bind(self):
        descriptors = self.descriptors()
        for name, info in self.runtimes.items():
            matches = [item for item in descriptors if item["engine_type"] == name + "_workflow"]
            require(len(matches) == 1, f"{name} automatic registration missing or ambiguous")
            info["id"] = matches[0]["id"]
            endpoint = matches[0].get("runtime_endpoint")
            require(isinstance(endpoint, dict) and endpoint.get("protocol") == "http"
                    and endpoint.get("host") in {"localhost", "127.0.0.1"}
                    and type(endpoint.get("port")) is int and 0 < endpoint["port"] <= 65535,
                    "invalid native Runtime endpoint facts")
            info["url"] = f'http://{endpoint["host"]}:{endpoint["port"]}'
            parsed = urlsplit(info["url"])
            require(parsed.scheme == "http" and parsed.hostname in {"127.0.0.1", "localhost"}
                    and parsed.port and not parsed.username and not parsed.password
                    and parsed.path in {"", "/"} and not parsed.query and not parsed.fragment,
                    "Runtime endpoint must be loopback HTTP")
            pid = self.processes[name + "-workflow-engine"]["pid"]
            listeners = subprocess.check_output(["lsof", "-ti", f"TCP:{parsed.port}", "-sTCP:LISTEN"], text=True)
            require(set(listeners.split()) == {str(pid)}, "Runtime endpoint listener ownership mismatch")
            log = ROOT / f"logs/{name}-workflow-engine.log"
            require(f"{name}_workflow registered to System" in log.read_text(), "startup auto registration evidence missing")
        self.initial = {item["id"]: (item["engine_type"], item.get("runtime_endpoint"), item["lifecycle_state"])
                        for item in descriptors}

    def state(self, name, expected):
        self.stable()
        descriptors = self.descriptors()
        require({item["id"]: (item["engine_type"], item.get("runtime_endpoint"), item["lifecycle_state"])
                 for item in descriptors} == self.initial, "Engine identity or lifecycle changed")
        for runtime, info in self.runtimes.items():
            item = next(item for item in descriptors if item["id"] == info["id"])
            require(item["lifecycle_state"] == "active", "Runtime lifecycle must remain active")
            if item["connection_status"] != (expected if runtime == name else "online"):
                return False
        return True

    def health(self, name, missing=None):
        response = self.client.get(self.runtimes[name]["url"] + "/health")
        body = response.json()
        require(body.get("service") == name + "-workflow-engine", "health service identity mismatch")
        return validate_health(response.status_code, body, missing)

    def register_unhealthy(self, name):
        info = self.runtimes[name]
        env = dict(os.environ, PORT=str(urlsplit(info["url"]).port), RUNTIME_HOST=urlsplit(info["url"]).hostname,
                   PROTOCOL="http")
        # Observe the real client's result in this separate caller process only;
        # no live Runtime monkeypatching or simulated HTTP responses.
        return native_command(name, '''import json,os
import addp_common.client as client
from addp_common.client.runtime_registration import register_after_listener
original=client.register_runtime_engine
results=[]
def observed(*args,**kwargs):
    status,raw=original(*args,**kwargs)
    body=json.loads(raw)
    results.append({"http_status":status,"engine_id":body.get("engine_id"),"success":body.get("success")})
    return status,raw
client.register_runtime_engine=observed
import api_server
register_after_listener(api_server.register_to_system,int(os.environ["PORT"]))
assert len(results)==1
print(json.dumps(results[0]))
''', env)

    def log_offset(self, name):
        return (ROOT / f"logs/{name}-workflow-engine.log").stat().st_size

    def registration_probe_finished(self, name, offset):
        with (ROOT / f"logs/{name}-workflow-engine.log").open("rb") as log:
            log.seek(offset)
            # One listener check from register_after_listener and one real System
            # health request after registration; hold the fault for both.
            return log.read().count(b'"GET /health HTTP/1.1" 503') >= 2


def wait_for(check, message, timeout=90):
    start = time.monotonic()
    while time.monotonic() - start < timeout:
        if check():
            return round(time.monotonic() - start, 3)
        time.sleep(1)
    raise SuiteError(message)


def run_cycle(observer, fault, name):
    info = observer.runtimes[name]
    before = observer.health(name)
    require(observer.state(name, "online"), "initial Runtime observation is not online")
    with fault.missing(info["tool"]):
        missing = observer.health(name, info["dep"])
        require(set(missing["dependencies"]) == set(before["dependencies"]), "dependency catalog changed")
        for key, fact in before["dependencies"].items():
            if key != info["dep"]:
                require(missing["dependencies"].get(key) == fact, "unrelated dependency fact changed")
        offline = wait_for(lambda: observer.state(name, "offline"), "periodic offline observation timed out")
        offset = observer.log_offset(name)
        registration = observer.register_unhealthy(name)
        require(registration == {"http_status": 202, "engine_id": info["id"], "success": True},
                "unhealthy self registration must preserve Engine ID and return 202")
        wait_for(lambda: observer.registration_probe_finished(name, offset), "registration health probe did not finish")
        require(observer.state(name, "offline"), "unhealthy registration made Runtime online")
    restored = observer.health(name)
    require(restored == before, "health facts did not recover exactly")
    online = wait_for(lambda: observer.state(name, "online"), "periodic recovery observation timed out")
    observer.stable()
    return {"runtime": name, "engine_id": info["id"], "missing_dependency": info["dep"],
            "before": before, "missing": missing, "restored": restored,
            "unhealthy_registration": registration, "offline_seconds": offline, "online_seconds": online,
            "file_restored_exactly": True, "processes_unchanged": True, "result": "passed"}


def interrupted(_signal, _frame):
    raise SuiteError("acceptance interrupted")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--restore-only", action="store_true")
    args = parser.parse_args()
    observer = None
    try:
        artifact = isolation()
        fault = ToolFault(artifact / "workflow-runtime-health-fault.json")
        if args.restore_only:
            fault.restore()
            return 0
        require(not fault.journal.exists(), "unfinished fault requires --restore-only before startup")
        signal.signal(signal.SIGINT, interrupted)
        signal.signal(signal.SIGTERM, interrupted)
        observer = Observer()
        wait_for(lambda: all(any(item["engine_type"] == name + "_workflow" for item in observer.descriptors())
                             for name, _, _ in CASES), "startup auto registration timed out")
        observer.bind()
        for name, _, _ in CASES:
            wait_for(lambda: observer.state(name, "online"), "initial online observation timed out")
        report = {"schema_version": "addp.online-suite/v1", "suite": SUITE,
                  "run_id": os.environ["ADDP_ONLINE_TEST_RUN_ID"], "cycles": [], "cleanup": "passed"}
        for name, _, _ in CASES:
            report["cycles"].append(run_cycle(observer, fault, name))
        print(json.dumps(report, ensure_ascii=False, sort_keys=True))
        return 0
    except Exception as error:
        # Library/HTTP errors can contain tokens, URLs or secrets; only our fixed
        # assertions are suitable for archived diagnostics.
        message = str(error) if isinstance(error, SuiteError) else type(error).__name__
        print(f"Workflow Runtime health recovery failed: {message}", file=sys.stderr)
        return 1
    finally:
        if observer is not None:
            observer.close()


if __name__ == "__main__":
    raise SystemExit(main())
