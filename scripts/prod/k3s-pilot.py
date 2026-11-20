#!/usr/bin/env python3
"""Install and check the ADDP System/Gateway/Catalog pilot on an existing K3s cluster."""

import argparse
import base64
import json
import ipaddress
import os
from pathlib import Path
import re
import secrets
import subprocess
from urllib import request

MODULES = ("system", "gateway", "catalog")
INFRA = ("postgres", "redis", "minio", "meilisearch")
PORTS = {"system": 8180, "gateway": 8000, "catalog": 8192, "postgres": 5432, "redis": 6379,
         "minio": 9000, "meilisearch": 7700}
NAMES = {"system": "system-backend", "gateway": "gateway", "catalog": "catalog-backend"}
KEYS = ("ENCRYPTION_KEY", "IAM_MFA_ENCRYPTION_KEY", "OAUTH_USER_CODE_PEPPER")
PASSWORDS = ("POSTGRES_PASSWORD", "REDIS_PASSWORD", "MINIO_ROOT_PASSWORD", "MEILISEARCH_MASTER_KEY")
ORIGIN = "http://127.0.0.1:18082"


def read_bundle(path, arch):
    manifest = json.loads((path / "release.json").read_text())
    expected = set(MODULES) | (set(INFRA) if arch == "arm64" else set())
    if (manifest.get("schema") != "addp.k3s-pilot/v1" or manifest["arch"] != arch
            or not re.fullmatch("[a-f0-9]{40}", manifest["commit"]) or set(manifest["images"]) != expected):
        raise ValueError(f"Invalid {arch} node bundle")
    subprocess.run(["sha256sum", "--check", "--quiet", "SHA256SUMS"], cwd=path, check=True)
    return manifest


def private_text(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_mode & 0o077:
        raise ValueError("The credential or token file must be a private regular file (0600)")
    return path.read_text()


def read_private(path):
    return json.loads(private_text(path))


def create_credentials(manifest, path):
    values = {key: base64.b64encode(secrets.token_bytes(32)).decode() for key in KEYS}
    values.update({key: secrets.token_hex(24) for key in (*PASSWORDS, *manifest["service_secret_keys"])})
    data = {"runtime": values, "publishers": {name: secrets.token_hex(24) for name in MODULES}}
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w") as file:
        json.dump(data, file)
        file.write("\n")


def load_node(path, manifest):
    local = {"aarch64": "arm64", "x86_64": "amd64"}.get(os.uname().machine)
    if local != manifest["arch"] or os.geteuid() != 0:
        raise ValueError("Run load-node as root on a K3s Linux node with the matching architecture")
    subprocess.run(["k3s", "ctr", "-n", "k8s.io", "images", "import", str(path / "images.tar")], check=True)
    for name, image in manifest["images"].items():
        raw = subprocess.check_output(["k3s", "crictl", "inspecti", image["image"]])
        if json.loads(raw)["status"]["id"] != image["config_digest"]:
            raise ValueError(f"Imported image identity differs: {name}")
    print(f"Verified {len(manifest['images'])} native images for {manifest['commit']}")


def workload(name, image, node, arch, env, *, credentials=(), readiness, args=None, command=None, volumes=()):
    labels = {"app.kubernetes.io/name": name}
    container = {"name": name, "image": image, "imagePullPolicy": "Never",
                 "securityContext": {"allowPrivilegeEscalation": False},
                 "envFrom": [{"secretRef": {"name": secret}} for secret in credentials],
                 "env": [{"name": key, "value": str(value)} for key, value in env.items()],
                 "readinessProbe": {**readiness, "periodSeconds": 3, "timeoutSeconds": 2}}
    pod = {"nodeSelector": {"kubernetes.io/arch": arch, "kubernetes.io/hostname": node},
           "automountServiceAccountToken": False, "enableServiceLinks": False,
           "securityContext": {"seccompProfile": {"type": "RuntimeDefault"}},
           "containers": [container]}
    if args:
        container["args"] = args
    if command:
        container["command"] = command
    if volumes:
        pod["volumes"] = [{"name": key, **source} for key, _, source in volumes]
        container["volumeMounts"] = [{"name": key, "mountPath": mount} for key, mount, _ in volumes]
    return object_("apps/v1", "Deployment", name, spec={
        "replicas": 1, "strategy": {"type": "Recreate"}, "selector": {"matchLabels": labels},
        "template": {"metadata": {"labels": labels}, "spec": pod},
    })


def object_(api, kind, name, **fields):
    return {"apiVersion": api, "kind": kind, "metadata": {"name": name}, **fields}


def http_probe(port, path):
    return {"httpGet": {"port": port, "path": path}}


def service(name, port):
    return object_("v1", "Service", name, spec={
        "selector": {"app.kubernetes.io/name": name}, "ports": [{"port": port, "targetPort": port}]})


def infra_objects(bundle, images, node, storage):
    specifications = {
        "postgres": ("/var/lib/postgresql/data", "10Gi", {"POSTGRES_USER": "addp", "POSTGRES_DB": "addp",
                     "PGDATA": "/var/lib/postgresql/data/pgdata"}, "db"),
        "redis": ("/data", "2Gi", {}, "redis"),
        "minio": ("/data", "10Gi", {"MINIO_ROOT_USER": "minioadmin"}, "minio"),
        "meilisearch": ("/meili_data", "4Gi", {"MEILI_ENV": "production"}, "meili"),
    }
    yield object_("v1", "ConfigMap", "redis-start", data={
        "redis-authentication.sh": (bundle / "redis-authentication.sh").read_text()})
    for name, (mount, size, env, secret) in specifications.items():
        yield object_("v1", "PersistentVolumeClaim", name, spec={
            "accessModes": ["ReadWriteOnce"], "storageClassName": storage,
            "resources": {"requests": {"storage": size}}})
        yield service(name, PORTS[name])
        volumes = [("data", mount, {"persistentVolumeClaim": {"claimName": name}})]
        extra = {}
        if name == "postgres":
            probe = {"exec": {"command": ["pg_isready", "-h", "127.0.0.1", "-U", "addp", "-d", "addp"]}}
        elif name == "redis":
            volumes.append(("start", "/addp", {"configMap": {"name": "redis-start"}}))
            extra = {"command": ["sh", "/addp/redis-authentication.sh"], "args": ["infra"]}
            probe = {"exec": {"command": ["sh", "-ec", 'REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli ping | grep -qx PONG']}}
        elif name == "minio":
            extra = {"args": ["server", "/data"]}
            probe = http_probe(9000, "/minio/health/ready")
        else:
            probe = http_probe(7700, "/health")
        yield workload(name, images[name]["image"], node, "arm64", env, credentials=[secret],
                       readiness=probe, volumes=volumes, **extra)


def platform_database_options():
    return {
        "ADDP_INFRA_DATABASE_PROVIDER": os.environ.get("ADDP_INFRA_DATABASE_PROVIDER") or "postgres",
        "TZ": os.environ.get("TZ") or "Asia/Shanghai",
    }


def product_objects(module, images, node, arch):
    name = NAMES[module]
    env = {**platform_database_options(), "ENV": "production", "GIN_MODE": "release", "SYSTEM_URL": "http://system-backend:8180",
           "ADDP_RUNTIME_LOG_ROOT": "/var/log/addp",
           "POSTGRES_HOST": "postgres", "POSTGRES_PORT": 5432, "POSTGRES_USER": "addp", "POSTGRES_DB": "addp"}
    credentials = ["db"]
    if module in ("system", "gateway"):
        env.update(REDIS_HOST="redis", REDIS_PORT=6379)
        credentials.append("redis")
    if module == "system":
        env.update(PUBLIC_API_URL=ORIGIN, CONSOLE_URL=ORIGIN, ALLOWED_ORIGINS=ORIGIN,
                   CATALOG_URL="http://catalog-backend:8192", MINIO_ENDPOINT="minio:9000",
                   MINIO_ROOT_USER="minioadmin", ADDP_OBSERVABILITY_LOGS_ENABLED="false",
                   ADDP_OBSERVABILITY_METRICS_ENABLED="false")
        credentials.extend(("minio", "system"))
    elif module == "catalog":
        env["MEILISEARCH_URL"] = "http://meilisearch:7700"
        credentials.extend(("search", "catalog"))
    else:
        credentials.append("gateway")
    deploy = workload(name, images[module]["image"], node, arch, env, credentials=credentials,
                      readiness=http_probe(PORTS[module], "/health/ready"),
                      volumes=[("logs", "/var/log/addp", {"emptyDir": {}})])
    container = deploy["spec"]["template"]["spec"]["containers"][0]
    container["env"].append({
        "name": "SERVICE_HOST", "valueFrom": {"fieldRef": {"fieldPath": "status.podIP"}}})
    endpoint = service(name, PORTS[module])
    if module == "catalog":
        endpoint["spec"]["ports"][0].update(name="http", protocol="TCP")
        container["startupProbe"] = {**http_probe(PORTS[module], "/health/live"),
                                     "periodSeconds": 5, "timeoutSeconds": 2, "failureThreshold": 48}
        container["livenessProbe"] = {**http_probe(PORTS[module], "/health/live"),
                                      "periodSeconds": 10, "timeoutSeconds": 2, "failureThreshold": 3}
    return endpoint, deploy


def secret_objects(manifest, data):
    env = data["runtime"]
    partitions = {"db": ["POSTGRES_PASSWORD"], "redis": ["REDIS_PASSWORD"], "minio": ["MINIO_ROOT_PASSWORD"],
                  "search": ["MEILISEARCH_MASTER_KEY"], "system": [*KEYS, *manifest["service_secret_keys"]],
                  "gateway": ["GATEWAY_SERVICE_CLIENT_SECRET"], "catalog": ["CATALOG_SERVICE_CLIENT_SECRET"]}
    return [object_("v1", "Secret", name, type="Opaque", stringData={k: env[k] for k in keys})
            for name, keys in partitions.items()] + [
        object_("v1", "Secret", "meili", type="Opaque", stringData={"MEILI_MASTER_KEY": env["MEILISEARCH_MASTER_KEY"]})]


class Cluster:
    def __init__(self, context, namespace):
        self.command = ["kubectl", "--context", context, "--namespace", namespace]

    def run(self, *args, data=None, capture=False):
        return subprocess.run([*self.command, *args], input=None if data is None else json.dumps(data).encode(),
                              stdout=subprocess.PIPE if capture else None, check=True).stdout

    def create(self, objects):
        self.run("create", "-f", "-", data={"apiVersion": "v1", "kind": "List", "items": list(objects)})

    def ready(self, *names):
        for name in names:
            self.run("rollout", "status", f"deployment/{name}", "--timeout=240s")

    def node_arch(self, node):
        result = json.loads(self.run("get", "node", node, "-o", "json", capture=True))
        if not any(c["type"] == "Ready" and c["status"] == "True" for c in result["status"]["conditions"]):
            raise ValueError(f"Node is not Ready: {node}")
        return result["metadata"]["labels"]["kubernetes.io/arch"]


def install(args):
    arm = read_bundle(args.arm64, "arm64")
    amd = read_bundle(args.amd64, "amd64")
    if arm["commit"] != amd["commit"]:
        raise ValueError("Both node bundles must have the same source commit")
    if args.namespace in ("default", "kube-system", "kube-public", "kube-node-lease"):
        raise ValueError("Use a new namespace dedicated to the pilot")
    data = read_private(args.credentials)
    cluster = Cluster(args.context, args.namespace)
    if cluster.node_arch(args.arm_node) != "arm64" or cluster.node_arch(args.amd_node) != "amd64":
        raise ValueError("The requested nodes do not match the required architectures")
    cluster.run("get", "storageclass", args.storage_class)
    # Native declaration identity comes from the image selected to run each module.
    declarations = {module: json.loads(((args.amd64 if module == "catalog" else args.arm64) /
                                        f"{module}.json").read_text()) for module in MODULES}
    objects = secret_objects(arm, data)
    cluster.run("create", "namespace", args.namespace)
    cluster.create(objects)
    cluster.create(infra_objects(args.arm64, arm["images"], args.arm_node, args.storage_class))
    cluster.ready(*INFRA)
    empty = cluster.run("exec", "deploy/postgres", "--", "psql", "-U", "addp", "-d", "addp", "-Atc",
                        "SELECT to_regnamespace('system') IS NULL AND to_regnamespace('catalog') IS NULL",
                        capture=True)
    if empty.strip() != b"t":
        raise ValueError("Expected an empty application database before System starts")
    print("Empty application schemas verified", flush=True)
    cluster.create(product_objects("system", arm["images"], args.arm_node, "arm64"))
    cluster.ready("system-backend")
    print("System is Ready before declaration publication", flush=True)
    for module in MODULES:
        credentials = {"secret": data["publishers"][module]}
        command = ("exec", "-i", "deploy/system-backend", "--", "/app/server")
        cluster.run(*command, "provision-module-publisher", module, data=credentials)
        cluster.run(*command, "publish-module-declaration", module,
                    data={**credentials, "declaration": declarations[module]})
        print(f"Published {module} declaration", flush=True)
    cluster.create(product_objects("gateway", arm["images"], args.arm_node, "arm64"))
    cluster.create(product_objects("catalog", amd["images"], args.amd_node, "amd64"))
    cluster.ready("gateway", "catalog-backend")
    print(f"Installed pilot {arm['commit']} in {args.namespace}", flush=True)


def bootstrap(args):
    values = read_private(args.credentials)["runtime"]
    binary = args.bundle / "addp-iam-bootstrap"
    env = {**os.environ, **platform_database_options(), **{k: values[k] for k in (*KEYS, "POSTGRES_PASSWORD")},
           "ENV": "production", "POSTGRES_HOST": "127.0.0.1", "POSTGRES_PORT": str(args.postgres_port),
           "POSTGRES_DB": "addp", "POSTGRES_USER": "addp"}
    command = [str(binary.resolve()), args.phase]
    if args.phase == "apply":
        if not args.administrators:
            raise ValueError("The apply phase requires an administrator manifest")
        command.extend(("--manifest", str(args.administrators)))
    subprocess.run(command, env=env, check=True)


def catalog_endpoints(namespace, uid, service, slices):
    name = NAMES["catalog"]
    meta, spec = service["metadata"], service["spec"]
    if (not uid or meta["uid"] != uid or meta["namespace"] != namespace or meta["name"] != name
            or meta.get("deletionTimestamp") or not spec.get("selector") or spec.get("publishNotReadyAddresses")):
        raise ValueError("Catalog Service binding mismatch")
    if not any(port.get("name") == "http" and port.get("protocol") == "TCP" for port in spec["ports"]):
        raise ValueError("Catalog Service has no named HTTP port")
    endpoints = set()
    for item in slices:
        meta = item["metadata"]
        labels = meta.get("labels", {})
        owners = meta.get("ownerReferences", [])
        if (meta["namespace"] != namespace or meta.get("deletionTimestamp")
                or labels.get("kubernetes.io/service-name") != name
                or labels.get("endpointslice.kubernetes.io/managed-by") != "endpointslice-controller.k8s.io"
                or not any(o.get("apiVersion") == "v1" and o.get("kind") == "Service"
                           and o.get("name") == name and o.get("uid") == uid and o.get("controller") is True
                           for o in owners)):
            continue
        ports = [p["port"] for p in item.get("ports", []) if p.get("name") == "http"
                 and p.get("protocol") == "TCP" and isinstance(p.get("port"), int) and 1 <= p["port"] <= 65535]
        if len(ports) != 1 or item["addressType"] not in ("IPv4", "IPv6"):
            continue
        for endpoint in item["endpoints"]:
            target = endpoint.get("targetRef", {})
            conditions = endpoint.get("conditions", {})
            if (conditions.get("ready") is not True or conditions.get("terminating")
                    or target.get("kind") != "Pod" or target.get("namespace") != namespace or not target.get("uid")):
                continue
            for addr in endpoint["addresses"]:
                ip = ipaddress.ip_address(addr)
                if item["addressType"] == f"IPv{ip.version}":
                    endpoints.add((target["uid"], str(ip), ports[0]))
    return [{"pod_uid": uid, "address": addr, "port": port} for uid, addr, port in sorted(endpoints)]


def inspect_catalog(args):
    cluster = Cluster(args.context, args.namespace)
    svc_args = ("get", "service", NAMES["catalog"], "-o", "json")
    service = json.loads(cluster.run(*svc_args, capture=True))
    slices = json.loads(cluster.run("get", "endpointslices.discovery.k8s.io", "-l",
                                    "kubernetes.io/service-name=catalog-backend", "-o", "json", capture=True))
    confirmed = json.loads(cluster.run(*svc_args, capture=True))
    if confirmed["metadata"]["resourceVersion"] != service["metadata"]["resourceVersion"]:
        raise ValueError("Catalog Service changed during inspection")
    endpoints = catalog_endpoints(args.namespace, args.service_uid, confirmed, slices["items"])
    print(json.dumps({"namespace": args.namespace, "service_uid": args.service_uid, "endpoints": endpoints}))


def verify(args):
    arm = read_bundle(args.arm64, "arm64")
    amd = read_bundle(args.amd64, "amd64")
    if arm["commit"] != amd["commit"]:
        raise ValueError("Both node bundles must have the same source commit")
    cluster = Cluster(args.context, args.namespace)
    pods = {}
    for module in MODULES:
        name = NAMES[module]
        items = json.loads(cluster.run("get", "pods", "-l", f"app.kubernetes.io/name={name}", "-o", "json",
                                       capture=True))["items"]
        pod, = [item for item in items if not item["metadata"].get("deletionTimestamp")]
        phase = pod["status"]
        node = pod["spec"]["nodeName"]
        arch = cluster.node_arch(node)
        selected = amd if module == "catalog" else arm
        container = phase["containerStatuses"][0]
        if arch != selected["arch"] or not container["ready"] or container["image"] != selected["images"][module]["image"]:
            raise ValueError(f"Pod image, architecture or readiness mismatch: {module}")
        pods[module] = {"node": node, "arch": arch, "pod_ip": phase["podIP"],
                        "image": phase["containerStatuses"][0]["image"], "image_id": phase["containerStatuses"][0]["imageID"]}
    opener = request.build_opener(request.ProxyHandler({}))
    with opener.open(ORIGIN + "/", timeout=10) as response:
        route = json.load(response)["modules"]["catalog"]
    if route != f"http://{pods['catalog']['pod_ip']}:8192":
        raise ValueError("Gateway has not selected the actual Catalog Pod")
    token = private_text(args.reader_token_file).strip()
    req = request.Request(ORIGIN + "/api/v1/catalog/entries?view=governance&page=1&page_size=1",
                          headers={"Authorization": "Bearer " + token})
    with opener.open(req, timeout=10) as response:
        body = json.load(response)
        status = response.status
    if status != 200 or not isinstance(body["data"], list) or type(body["total"]) is not int:
        raise ValueError("Catalog returned an invalid list response")
    print(json.dumps({"commit": arm["commit"], "pods": pods, "catalog_route": route,
                      "catalog_http": status, "total": body["total"]}, indent=2))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    actions = parser.add_subparsers(dest="action", required=True)
    for command in ("load-node", "init-credentials"):
        action = actions.add_parser(command)
        action.add_argument("--bundle", type=Path, required=True)
        if command == "load-node":
            action.add_argument("--arch", choices=("arm64", "amd64"), required=True)
        else:
            action.add_argument("--credentials", type=Path, required=True)
    for command in ("install", "verify"):
        action = actions.add_parser(command)
        action.add_argument("--context", required=True)
        action.add_argument("--namespace", required=True)
        action.add_argument("--arm64", type=Path, required=True)
        action.add_argument("--amd64", type=Path, required=True)
        if command == "install":
            action.add_argument("--credentials", type=Path, required=True)
            action.add_argument("--arm-node", required=True)
            action.add_argument("--amd-node", required=True)
            action.add_argument("--storage-class", required=True)
        else:
            action.add_argument("--reader-token-file", type=Path, required=True)
    check = actions.add_parser("inspect-catalog")
    check.add_argument("--context", required=True)
    check.add_argument("--namespace", required=True)
    check.add_argument("--service-uid", required=True)
    action = actions.add_parser("bootstrap")
    action.add_argument("phase", choices=("prepare", "apply"))
    action.add_argument("--bundle", type=Path, required=True)
    action.add_argument("--credentials", type=Path, required=True)
    action.add_argument("--administrators", type=Path)
    action.add_argument("--postgres-port", type=int, default=15433)
    args = parser.parse_args()
    try:
        if args.action == "load-node":
            load_node(args.bundle, read_bundle(args.bundle, args.arch))
        elif args.action == "init-credentials":
            create_credentials(read_bundle(args.bundle, "arm64"), args.credentials)
        elif args.action == "install":
            install(args)
        elif args.action == "inspect-catalog":
            inspect_catalog(args)
        elif args.action == "bootstrap":
            bootstrap(args)
        else:
            verify(args)
    except (OSError, ValueError, KeyError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"K3s pilot {args.action} failed: {error}\n")


if __name__ == "__main__":
    main()
