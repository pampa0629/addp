"""Exercise the deployment API during the owned Hosted module lifecycle."""

import argparse
import base64
import importlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time
from urllib.parse import urlencode

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
online = importlib.import_module("scripts.test.module-registry-recovery-online")
collection = "/api/v1/system/deployment/module_declarations"
platform = "/api/v1/system/platform"


def require(condition, message):
    if not condition:
        raise online.SuiteError(message)


def observe(phase, module):
    client = online.JSONClient(os.environ["SYSTEM_URL"], 5, "SYSTEM_URL")
    observer = {"Authorization": "Bearer " + os.environ["ADDP_ONLINE_TEST_PLATFORM_ACCESS_TOKEN"]}
    secret_dir = Path(os.environ["ADDP_ONLINE_SECRET_DIR"]) / "publishers"
    artifacts = Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"])
    path = collection + "/" + module
    artifact = json.loads((Path(module) / "module-declaration.json").read_text())
    body = {k: v for k, v in artifact.items() if k != "module_name"}

    def publish():
        subprocess.run([sys.executable, "scripts/prod/publish-module-declarations.py",
                        "--credential-dir", str(secret_dir), module], check=True)

    def publisher_headers():
        secret = (secret_dir / module).read_text()
        basic = base64.b64encode(f"addp-{module}-publisher:{secret}".encode()).decode()
        response = client.request("POST", "/api/v1/system/oauth/token", (200,),
                                  form={"grant_type": "client_credentials", "scope": "addp.api",
                                        "audience": "addp.api", "context_type": "platform"},
                                  headers={"Authorization": "Basic " + basic})
        return {"Authorization": "Bearer " + response.payload["access_token"]}

    def instances():
        return client.request("GET", platform + "/module-instances?" + urlencode(
            {"module_name": module, "page": 1, "page_size": 100}), (200,), headers=observer).payload["data"]

    def definition():
        return client.request("GET", platform + "/modules/" + module, (200,), headers=observer).payload

    def until(read, message):
        deadline = time.monotonic() + 65
        while True:
            value = read()
            if value:
                return value
            if time.monotonic() >= deadline:
                raise online.SuiteError(message)
            time.sleep(0.25)

    if phase == "system-cold":
        live = client.request("GET", "/health/live", (200,)).payload
        require(client.request("GET", "/health/ready", (200,)).payload["status"] == "ready", "System not Ready")
        require(not instances(), "System already registered before publication")
        client.request("GET", path, (404,), headers=publisher_headers())
        client.request("GET", platform + "/modules/system", (404,), headers=observer)
        publish()
        first = until(lambda: next((i for i in instances() if i["status"] == "up"), None), "System registration timed out")
        renewed = until(lambda: next((i for i in instances() if i["instance_id"] == first["instance_id"]
                            and i["lease_expires_at"] > first["lease_expires_at"]), None), "System lease did not advance")
        require(client.request("GET", "/health/live", (200,)).payload["started_at"] == live["started_at"], "System restarted")
        require(client.request("GET", "/health/ready", (200,)).payload["status"] == "ready", "System lost Ready")
        result = {"ready_before_publication": True, "instances_before": 0, "initial": first, "renewed": renewed,
                  "published": client.request("GET", path, (200,), headers=publisher_headers()).payload}
    elif phase == "catalog-prebuilt":
        original = client.request("POST", platform + "/modules", (201,),
                                  body={"module_name": module, "enabled": False}, headers=observer).payload
        require(original["version"] == 1 and not original["enabled"] and not instances(), "unexpected Catalog prebuild")
        headers = publisher_headers()
        baseline = client.request("GET", path, (200,), headers=headers).payload
        require(baseline["version"] == 1 and baseline["declaration_published_at"] is None, "Catalog was already published")
        publish()
        published = client.request("GET", path, (200,), headers=headers).payload
        require(published["version"] == 2 and published["declaration_published_at"], "first Catalog publication failed")
        repeated = client.request("PUT", path, (200,), body={**body, "version": 2}, headers=headers).payload
        require(repeated == published, "idempotent Catalog publication changed resource")
        conflict = client.request("PUT", path, (409,), body={**body, "version": 1}, headers=headers).payload
        require(conflict["error_code"] == "resource_version_conflict", "stale publication did not conflict")
        current = definition()
        require(current["id"] == original["id"] and not current["enabled"] and current["version"] == 2
                and not instances(), "Catalog intent or instances changed")
        result = {"original": original, "baseline": baseline, "published": published, "conflict": conflict["error_code"], "idempotent": True}
    elif phase == "catalog-running":
        instance = until(lambda: next((i for i in instances() if i["status"] == "up"), None), "Catalog failed to register")
        gateway = online.JSONClient(os.environ["GATEWAY_URL"], 5, "GATEWAY_URL")
        require("catalog" not in gateway.request("GET", "/", (200,)).payload["modules"], "disabled Catalog routed")
        gateway.request("GET", "/api/v1/catalog/health", (503,))
        require(not definition()["enabled"], "Catalog registration changed intent")
        result = {"instance": instance, "route_present": False}
    elif phase == "waiting":
        backend_url = os.environ["MANAGER_URL"] if module == "manager" else "http://127.0.0.1:" + os.environ["COPILOT_BACKEND_PORT"]
        ready = online.JSONClient(backend_url, 5, "backend").request("GET", "/health/ready", (200,)).payload
        require(ready["registration_state"] == "starting" and not instances(), "backend did not wait for declaration")
        client.request("GET", platform + "/modules/" + module, (404,), headers=observer)
        publish()
        result = {"ready_before_publication": ready, "instance_count_before": 0}
    elif phase == "manager-upgrade":
        headers = publisher_headers()
        old = client.request("GET", path, (200,), headers=headers).payload
        body["task_provider"]["description"] += " (Hosted deployment)"
        updated = client.request("PUT", path, (200,), body={**body, "version": old["version"]}, headers=headers).payload
        require(updated["version"] == old["version"] + 1, "Manager version did not advance")
        result = {"published": updated, "enabled": definition()["enabled"]}
    else:
        expected = json.loads((artifacts / "declaration-manager-upgrade.json").read_text())
        current = client.request("GET", path, (200,), headers=publisher_headers()).payload
        require(current == expected["published"] and definition()["enabled"] == expected["enabled"],
                "Manager reconnection changed published declaration")
        runtime = online.RegistryClient(os.environ["SYSTEM_URL"], os.environ["MANAGER_SERVICE_CLIENT_SECRET"], 5)
        rejected = client.request("POST", "/api/v1/system/runtime/modules", (400,),
                                  body={"module_name": "manager", "instance_id": "old-contract", "role": "backend",
                                        "module_url": os.environ["MANAGER_URL"], "route_prefix": "/manager",
                                        "process_started_at": online.PROCESS_STARTED_AT}, headers=runtime.authorization)
        require(rejected.payload["error_code"] == "module_registration_invalid", "mixed registration accepted")
        require(client.request("GET", path, (200,), headers=publisher_headers()).payload == current, "rejection changed definition")
        result = {"published": current, "enabled": expected["enabled"], "mixed_request_status": 400}
    (artifacts / f"declaration-{phase}-{module}.json" if phase == "waiting" else
     artifacts / f"declaration-{phase}.json").write_text(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("phase", choices=("system-cold", "catalog-prebuilt", "catalog-running", "waiting", "manager-upgrade", "manager-reconnected"))
    parser.add_argument("module", choices=("system", "catalog", "manager", "copilot"))
    args = parser.parse_args()
    observe(args.phase, args.module)
