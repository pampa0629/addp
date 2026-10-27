"""Adapt official Compose services to one disposable, loopback-only deployment."""
import copy
import re

def require(value, message):
    if not value:
        raise ValueError(message)

def isolate_compose(document, selected, run_id, images, *, label):
    """Adapt ownership only. Retain the official service environment/entry/health."""
    result = {"name": run_id, "services": {}, "networks": {"isolated": {
        "name": run_id, "internal": True, "labels": {label: run_id}}, "ingress": {
        "name": run_id + "-ingress", "labels": {label: run_id}}}, "volumes": {}}
    for name in selected:
        service = copy.deepcopy(document["services"][name])
        require("network_mode" not in service and not service.get("privileged"), "unexpected product network/privilege contract")
        service["container_name"] = run_id + "-" + name
        service["labels"] = {**service.get("labels", {}), label: run_id}
        service["networks"] = {"isolated": {"aliases": [name]}}
        service["restart"] = "no"
        service["image"] = images.get(name, service.get("image"))
        service.pop("build", None)
        service.pop("depends_on", None)  # The caller starts each dependency in order.
        service.pop("ports", None)
        for mount in service.get("volumes", []):
            # No source, SDK, logs or data from the personal deployment is mounted.
            key = name + "-" + re.sub(r"[^a-zA-Z0-9]", "-", mount["target"]).strip("-")
            result["volumes"][key] = {"name": run_id + "-" + key, "labels": {label: run_id}}
        # Replace mounts without relying on host paths or retaining read-only empty volumes.
        service["volumes"] = [{"type": "volume", "source": name + "-" + re.sub(r"[^a-zA-Z0-9]", "-", mount["target"]).strip("-"),
                               "target": mount["target"]} for mount in document["services"][name].get("volumes", [])]
        result["services"][name] = service
    for name, port in (("postgres", 5432), ("system-backend", 8180), ("gateway", 8000), ("nginx", 80)):
        if name in result["services"]:
            result["services"][name]["networks"]["ingress"] = {}
            result["services"][name]["ports"] = [{"target": port, "host_ip": "127.0.0.1", "published": "0", "protocol": "tcp"}]
    return result
