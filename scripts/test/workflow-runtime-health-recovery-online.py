#!/usr/bin/env python3
"""Accept three real product Runtime faults on disposable GitHub Hosted Linux.

Usage: make test-online ONLINE_SUITE=workflow-runtime-health-recovery
       python3 scripts/test/workflow-runtime-health-recovery-online.py --restore-only
Only containers labelled for this suite and run can receive fault commands.
"""
from __future__ import annotations

import argparse
from contextlib import contextmanager
import importlib.util
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[2]
SUITE = "workflow-runtime-health-recovery"
CASES = (("pointcloud", "pdal", "POINTCLOUD_PDAL_BIN"),
         ("model3d", "ifcconvert", "MODEL3D_IFC_CONVERTER_BIN"),
         ("document", "libreoffice", "DOCUMENT_LIBREOFFICE_BIN"))


class SuiteError(RuntimeError):
    pass


def require(condition, message):
    if not condition:
        raise SuiteError(message)


def external(path):
    resolved = Path(path).resolve()
    require(not resolved.is_relative_to(ROOT), "Online paths must be outside this checkout")
    return resolved


def isolation(restoring=False):
    require(os.environ.get("GITHUB_ACTIONS") == "true" and os.environ.get("RUNNER_OS") == "Linux"
            and os.environ.get("ADDP_ONLINE_HOSTED") == "1" and os.environ.get("ADDP_ONLINE_HOST") == "1",
            "disposable GitHub Hosted Linux profile is required")
    require(not (ROOT / ".env").exists(), "repository .env is forbidden")
    require(bool(os.environ.get("ADDP_ONLINE_TEST_RUN_ID")), "owned run ID is required")
    require(os.environ.get("POSTGRES_DB") == "addp_online", "isolated Online database is required")
    require(external(os.environ["ADDP_ONLINE_SECRET_DIR"]).is_dir(), "external secret directory is required")
    if not restoring:
        spec = importlib.util.spec_from_file_location("runtime_health_preflight", ROOT / "scripts/test/online-preflight.py")
        module = importlib.util.module_from_spec(spec)
        sys.modules[spec.name] = module
        spec.loader.exec_module(module)
        module.validate_online_environment(require_run_id=True)
        for name in ("SYSTEM_URL", "GATEWAY_URL"):
            module.parse_service(name.lower() + "=" + os.environ[name])
    return external(os.environ["ADDP_ONLINE_ARTIFACT_DIR"])


def docker(*args, input=None, absent=False):
    result = subprocess.run(["docker", *args], input=input, capture_output=True, text=True, timeout=45)
    require(result.returncode == 0, "owned container command failed (output withheld)")
    return result.stdout


class Runtime:
    def __init__(self, name):
        require(name in {case[0] for case in CASES}, "unknown product Runtime")
        self.name, self.container = name, "addp-health-" + name
        self.identity = self.inspect()
        require(self.identity is not None, "required product container is missing")

    def inspect(self):
        identifiers = docker("ps", "-aq", "--filter", "name=^/" + self.container + "$").split()
        if not identifiers:
            return None
        require(len(identifiers) == 1, "ambiguous product container")
        info = json.loads(docker("inspect", identifiers[0]))[0]
        labels = info["Config"].get("Labels") or {}
        require(labels.get("com.addp.online-runtime") == SUITE
                and labels.get("com.addp.online-run-id") == os.environ["ADDP_ONLINE_TEST_RUN_ID"],
                "container ownership mismatch")
        require(info["Config"]["Cmd"] == ["python", "api_server.py"]
                and info["HostConfig"]["NetworkMode"] == "host", "product entry or network mismatch")
        require(info["State"]["Running"], "product container is not running")
        environment = dict(entry.split("=", 1) for entry in info["Config"].get("Env", []) if "=" in entry)
        port = int(environment.get("PORT", "0"))
        require(0 < port <= 65535, "product listen port is missing")
        return {"id": info["Id"], "image": info["Image"], "pid": info["State"]["Pid"],
                "started_at": info["State"]["StartedAt"], "restart_count": info["RestartCount"], "port": port}

    def stable(self):
        require(self.inspect() == self.identity, "Runtime container identity changed")

    def agent(self, action):
        self.stable()
        variable = next(variable for name, _, variable in CASES if name == self.name)
        source = (ROOT / "scripts/test/workflow-runtime-health-tool.py").read_text()
        return json.loads(docker("exec", "-i", "--user", "0", self.identity["id"], "python", "-", action, variable, input=source))

    def logs(self):
        self.stable()
        result = subprocess.run(["docker", "logs", self.identity["id"]], stdout=subprocess.PIPE,
                                stderr=subprocess.STDOUT, text=True, timeout=15)
        require(result.returncode == 0, "Runtime log observation failed")
        return result.stdout

    def register(self):
        self.stable()
        code = '''import json,os
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
'''
        return json.loads(docker("exec", self.identity["id"], "python", "-c", code).strip().splitlines()[-1])


class ToolFault:
    def __init__(self, runtimes):
        self.runtimes = runtimes
        self.baselines = {name: runtime.agent("prepare") for name, runtime in runtimes.items()}

    @contextmanager
    def missing(self, name):
        runtime = self.runtimes[name]
        try:
            require(runtime.agent("missing") == self.baselines[name], "tool baseline changed")
            yield
        finally:
            result = runtime.agent("restore")
            require(result.get("identity") == self.baselines[name], "tool was not restored exactly")


def restore_all():
    results = {}
    for name, _, _ in CASES:
        # A failed build may never have created this container. Check through
        # docker ps (daemon errors fail closed), then admit its labels normally.
        container = "addp-health-" + name
        if not docker("ps", "-aq", "--filter", "name=^/" + container + "$").strip():
            results[name] = "not_created"
            continue
        results[name] = Runtime(name).agent("restore")["result"]
    return results


def process_snapshot(names):
    result = {}
    for name in names:
        pid = int((ROOT / f".dev-pids/{name}.pid").read_text())
        os.kill(pid, 0)
        started = subprocess.check_output(["ps", "-p", str(pid), "-o", "lstart="], text=True).strip()
        cwd = subprocess.check_output(["lsof", "-a", "-p", str(pid), "-d", "cwd", "-Fn"], text=True)
        require("n" + str(ROOT) in cwd.splitlines() and bool(started), "backend PID ownership mismatch")
        result[name] = {"pid": pid, "started_at": started}
    return result


class Observer:
    def __init__(self):
        import httpx
        from addp_common.client.service_token import SyncOAuthServiceTokenSource
        self.tenant = int(os.environ["ADDP_ONLINE_TEST_TENANT_ID"])
        self.system = os.environ["SYSTEM_URL"].rstrip("/")
        self.client = httpx.Client(timeout=10, trust_env=False, follow_redirects=False)
        self.source = SyncOAuthServiceTokenSource(self.system, "addp-develop", os.environ["DEVELOP_SERVICE_CLIENT_SECRET"])
        self.names = ["system", "gateway"]
        self.processes = process_snapshot(self.names)
        self.products = {name: Runtime(name) for name, _, _ in CASES}
        self.runtimes = {name: {"dep": dep} for name, dep, _ in CASES}

    def close(self):
        self.source.close()
        self.client.close()

    def stable(self):
        require(process_snapshot(self.names) == self.processes, "backend process identity changed")
        for runtime in self.products.values():
            runtime.stable()

    def descriptors(self):
        response = self.client.get(self.system + "/api/v1/system/runtime/engine-descriptors?page_size=100",
                                   headers={"Authorization": "Bearer " + self.source.token(self.tenant)})
        require(response.status_code == 200, "descriptor observation failed")
        body = response.json()
        require(type(body.get("total")) is int and body["total"] <= 100
                and isinstance(body.get("data"), list) and len(body["data"]) == body["total"], "descriptor page is incomplete")
        return body["data"]

    def bind(self):
        descriptors = self.descriptors()
        for name, info in self.runtimes.items():
            matches = [item for item in descriptors if item["engine_type"] == name + "_workflow"]
            require(len(matches) == 1, "automatic registration missing or ambiguous")
            endpoint = matches[0].get("runtime_endpoint")
            require(isinstance(endpoint, dict) and endpoint.get("protocol") == "http"
                    and endpoint.get("host") == "127.0.0.1" and type(endpoint.get("port")) is int
                    and endpoint["port"] == self.products[name].identity["port"], "invalid product Runtime endpoint")
            info.update(id=matches[0]["id"], url=f'http://127.0.0.1:{endpoint["port"]}')
            require(f"{name}_workflow registered to System" in self.products[name].logs(), "startup registration evidence missing")
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
        return self.products[name].register()

    def log_offset(self, name):
        return self.products[name].logs().count('"GET /health HTTP/1.1" 503')

    def registration_probe_finished(self, name, offset):
        return self.log_offset(name) - offset >= 2
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
    with fault.missing(name):
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
    admitted = False
    try:
        isolation(restoring=args.restore_only)
        admitted = True
        if args.restore_only:
            print(json.dumps({"restored": restore_all()}))
            return 0
        signal.signal(signal.SIGINT, interrupted)
        signal.signal(signal.SIGTERM, interrupted)
        observer = Observer()
        wait_for(lambda: all(any(item["engine_type"] == name + "_workflow" for item in observer.descriptors())
                             for name, _, _ in CASES), "startup auto registration timed out")
        observer.bind()
        wait_for(lambda: observer.state("pointcloud", "online"), "initial online observation timed out")
        fault = ToolFault(observer.products)
        report = {"schema_version": "addp.online-suite/v1", "suite": SUITE,
                  "run_id": os.environ["ADDP_ONLINE_TEST_RUN_ID"], "cycles": [], "cleanup": "passed"}
        for name, _, _ in CASES:
            cycle = run_cycle(observer, fault, name)
            cycle["file_identity"] = fault.baselines[name]
            report["cycles"].append(cycle)
        report["runtime_identities"] = {name: runtime.identity for name, runtime in observer.products.items()}
        print(json.dumps(report, ensure_ascii=False, sort_keys=True))
        return 0
    except Exception as error:
        message = str(error) if isinstance(error, SuiteError) else type(error).__name__
        print(f"Workflow Runtime health recovery failed: {message}", file=sys.stderr)
        return 1
    finally:
        if admitted and not args.restore_only:
            restore_all()
        if observer is not None:
            observer.close()


if __name__ == "__main__":
    raise SystemExit(main())
