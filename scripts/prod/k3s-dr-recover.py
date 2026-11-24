#!/usr/bin/env python3
"""Recover a K3s site's login and Catalog paths on an isolated Docker host."""
import argparse
from datetime import datetime, timezone
import hashlib
import ipaddress
import platform
import json
import os
from pathlib import Path
import re
import secrets
import ssl
import subprocess
import time

from k3s_dr_capture import DATABASE_FORMAT, MC_EXPORT, MC_SETUP, object_witness, search_witness, table_query


def run(argv, **kwargs):
    return subprocess.run(argv, check=True, **kwargs)


def verify_files(root):
    files = {}
    for line in (root / "SHA256SUMS").read_text().splitlines():
        digest, name = line.split("  ", 1)
        if not re.fullmatch(r"[a-zA-Z0-9_.-]+", name) or name in files:
            raise ValueError("invalid snapshot inventory")
        path = root / name
        if path.is_symlink() or not path.is_file():
            raise ValueError("snapshot member must be a regular file: " + name)
        with path.open("rb") as f:
            if hashlib.file_digest(f, "sha256").hexdigest() != digest:
                raise ValueError("snapshot checksum mismatch: " + name)
        files[name] = digest
    if set(files) != {p.name for p in root.iterdir() if p.name != "SHA256SUMS"}:
        raise ValueError("incomplete snapshot inventory")


def verify_tls(root, hostname):
    tls = root / "tls"
    leaf = tls / "leaf.pem"
    # Supplying an empty password keeps unattended validation from prompting on encrypted keys.
    ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER).load_cert_chain(leaf, tls / "leaf.key", password=b"")
    run(["openssl", "verify", "-trusted", str(tls / "ca.pem"), "-untrusted", str(leaf),
         "-purpose", "sslserver", "-verify_hostname", hostname, str(leaf)])


def recover(args):
    os.umask(0o077)
    start = time.time()
    clock = time.monotonic()
    root = args.kit.resolve()
    snap = root / "snapshot"
    work = args.run_dir.resolve()
    work.mkdir(mode=0o700)
    proof = {"fault_started": datetime.fromtimestamp(start, timezone.utc).isoformat(), "stages_seconds": {}}

    def stage(name):
        proof["stages_seconds"][name] = round(time.monotonic() - clock, 3)
        (work / "result.json").write_text(json.dumps(proof, indent=2) + "\n")

    stage("started")
    if not re.fullmatch(r"[a-z0-9]+(?:[.-][a-z0-9]+)*", args.hostname):
        raise ValueError("invalid recovery hostname")
    verify_tls(root, args.hostname)
    verify_files(snap)
    meta = json.loads((snap / "snapshot.json").read_text())
    images = json.loads((root / "images.json").read_text())
    target_arch = {"x86_64": "amd64", "aarch64": "arm64"}[platform.machine()]
    if set(images) != IMAGE_NAMES:
        raise ValueError("incomplete image lock")
    for ref in set(images.values()):
        if not re.fullmatch(r"sha256:[a-f0-9]{64}", ref):
            raise ValueError("images.json requires local immutable image IDs")
        image = json.loads(subprocess.check_output(["docker", "image", "inspect", ref]))[0]
        if image["Architecture"] != target_arch or image["Os"] != "linux":
            raise ValueError("image does not match recovery host: " + ref)
    project = "addp-dr-" + secrets.token_hex(5)
    proof["project"] = project
    (work / "project").write_text(project + "\n")
    stage("validated_kit")
    c = ["docker", "compose", "--project-name", project, "-f", str(work / "compose.json")]

    def call(*argv, data=None):
        return subprocess.check_output(c + list(argv), input=data)

    def wait(fn, label):
        until = time.monotonic() + args.timeout
        while True:
            try:
                return fn()
            except (subprocess.CalledProcessError, OSError) as e:
                if time.monotonic() >= until:
                    raise RuntimeError("timeout waiting for " + label) from e
                time.sleep(1)

    def env(name):
        return json.loads((snap / (name + ".json")).read_text())

    system = env("system-backend")
    redis = system["REDIS_PASSWORD"]
    services = {}

    def service(name, image=None, **fields):
        services[name] = dict(image=images[image or name], pull_policy="never", **fields)

    # Fresh volumes belong exclusively to this Compose project.
    admin = "dr_" + secrets.token_hex(8)
    volumes = {}
    for name in ["postgres", "business-postgres"]:
        volumes[name] = {}
        service(name, "postgres", environment={"POSTGRES_USER": admin, "POSTGRES_DB": "postgres", "POSTGRES_PASSWORD": secrets.token_urlsafe()},
                tmpfs=["/docker-entrypoint-initdb.d"], volumes=[name + ":/var/lib/postgresql/data"])
    service("redis", command=["redis-server", "--requirepass", redis])
    service("minio", entrypoint=["minio"], command=["server", "/data"],
            environment={k: system[k] for k in ("MINIO_ROOT_USER", "MINIO_ROOT_PASSWORD")})
    search_env = env("meilisearch-recovered")
    service("meilisearch", command=["/bin/meilisearch", "--import-dump", "/restore/search.dump"],
            environment=search_env, volumes=[str(snap / "search.dump") + ":/restore/search.dump:ro"])
    for name in ["system-backend", "catalog-backend", "gateway"]:
        app = env(name)
        for key in list(app):
            if key.startswith(("ADDP_TELEMETRY_", "ADDP_METRICS_", "GATEWAY_METRICS_CONTROL_",
                               "ADDP_TRACE_", "ADDP_PROCESS_METRICS_", "ADDP_OBSERVABILITY_",
                               "ADDP_HOST_NODE_", "ADDP_DEPLOYMENT_DISCOVERY_")):
                del app[key]
        app.update(SERVICE_HOST=name, SYSTEM_URL="http://system-backend:8180",
                   POSTGRES_HOST="postgres", REDIS_HOST="redis",
                   MEILISEARCH_URL="http://meilisearch:7700", MINIO_ENDPOINT="minio:9000")
        service(name, environment=app)
    service("probe", "edge", entrypoint=["sleep", "infinity"],
            volumes=[str(root / "tls" / "ca.pem") + ":/tls/ca.pem:ro"])
    service("console")
    service("catalog-frontend")
    nginx = work / "nginx.conf"
    nginx.write_text("""
events {}
http {
 server {
  listen 443 ssl;
  server_name HOSTNAME;
  ssl_certificate /tls/leaf.pem;
  ssl_certificate_key /tls/leaf.key;
  proxy_set_header Host $host;
  proxy_set_header X-Forwarded-Proto https;
  location /api/ { proxy_pass http://gateway:8000; }
  location /module-ui/catalog/ { proxy_pass http://catalog-frontend:80/; }
  location / { proxy_pass http://console:80; }
 }
}
""".replace("HOSTNAME", args.hostname))
    service("edge", networks={"default": {"aliases": [args.hostname]}},
            volumes=[str(nginx) + ":/etc/nginx/nginx.conf:ro", str(root / "tls") + ":/tls:ro"])
    spec = {"services": services, "networks": {"default": {"internal": True}}, "volumes": volumes}
    (work / "compose.json").write_text(json.dumps(spec).replace("$", "$$"))
    run(c + ["up", "-d", "--pull", "never", "--no-build", "probe"])
    net = json.loads(subprocess.check_output(["docker", "network", "inspect", project + "_default"]))[0]
    if not net["Internal"]:
        raise RuntimeError("recovery network must be internal")
    proof["network_internal"] = True
    addr = str(ipaddress.IPv4Address(args.primary_ip))
    isolated = subprocess.run(c + ["exec", "-T", "probe", "curl", "--noproxy", "*", "-sS",
        "--max-time", "3", "--cacert", "/tls/ca.pem", "--resolve", f"{args.hostname}:443:{addr}",
        "https://" + args.hostname + "/"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    if isolated.returncode not in (7, 28):
        raise RuntimeError("primary isolation unproven: curl exit " + str(isolated.returncode))
    proof["primary_probe"] = {"ip": addr, "curl_exit": isolated.returncode}
    stage("primary_unreachable")
    run(c + ["up", "-d", "--pull", "never", "--no-build", "postgres", "business-postgres", "meilisearch", "redis", "minio"])
    stage("infra_started")
    buckets = meta["minio"]["buckets"]
    archive = snap / "objects.tar"
    if object_witness(archive) != meta["minio"]["objects"]:
        raise RuntimeError("object archive differs from source inventory")
    wait(lambda: call("exec", "-T", "minio", "curl", "-fs", "http://localhost:9000/minio/health/ready"), "MinIO")
    restore = MC_SETUP + """
tar -C "$tmp/objects" -xf -
for bucket do
    mc mb "dr/$bucket" >/dev/null
    mc mirror "$tmp/objects/$bucket" "dr/$bucket" >&2
done
"""
    with archive.open("rb") as f:
        run(c + ["exec", "-T", "minio", "sh", "-ec", restore, "restore", *buckets], stdin=f)
    readback = work / "readback.tar"
    with readback.open("wb") as out:
        run(c + ["exec", "-T", "minio", "sh", "-ec", MC_EXPORT, "verify", *buckets], stdout=out)
    if object_witness(readback) != meta["minio"]["objects"]:
        raise RuntimeError("restored business objects differ")
    readback.unlink()
    proof["minio"] = {"buckets": buckets, "object_count": len(meta["minio"]["objects"])}
    stage("objects_verified")
    for dep, db in meta["databases"].items():
        wait(lambda: run(c + ["exec", "-T", dep, "pg_isready", "-h", "127.0.0.1", "-U", admin, "-d", "postgres"],
                         stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL), dep)
        psql = ["exec", "-T", dep, "psql", "-Xq", "-v", "ON_ERROR_STOP=1", "-U", admin]
        source = db["format"]
        available = json.loads(call(*psql, "-At", "-d", "postgres", "-c",
            "SELECT json_object_agg(name,default_version) FROM pg_available_extensions"))
        missing = [name for name, version in source["extensions"].items() if available.get(name) != version]
        if missing:
            raise ValueError("required extension versions unavailable on target: " + ", ".join(missing))
        with (snap / (dep + ".roles.sql")).open("rb") as f:
            run(c + psql + ["-d", "postgres"], stdin=f, stdout=subprocess.DEVNULL)
        with (snap / (dep + ".dump")).open("rb") as f:
            run(c + ["exec", "-T", dep, "pg_restore", "--exit-on-error", "--create", "-U", admin, "-d", "postgres"], stdin=f)
        actual_format = json.loads(call(*psql, "-At", "-d", db["database"], "-c", DATABASE_FORMAT))
        if (source["extensions"] != actual_format["extensions"]
                or actual_format["version"] // 10000 != source["version"] // 10000
                or actual_format["version"] < source["version"]):
            raise ValueError("database version or installed extensions differ: " + dep)
        witness = json.loads(call(*psql, "-At", "-d", db["database"], "-c", table_query(db["table"])))
        if db["witness"] != witness:
            raise RuntimeError(dep + " verification table differs from exported snapshot")
        proof.setdefault("databases", {})[dep] = dict(db, age_at_fault_seconds=round(
            start - datetime.fromisoformat(db["lower"]).timestamp(), 3))
        stage(dep + "_verified")

    def search_get(path):
        out = call("exec", "-T", "meilisearch", "sh", "-ec",
                   'exec curl -fs -H "Authorization: Bearer $MEILI_MASTER_KEY" "http://localhost:7700$1"', "get", path)
        return json.loads(out)

    actual = wait(lambda: search_witness(search_get), "search import")
    if actual != meta["search"]:
        raise RuntimeError("restored search indexes differ from captured source")
    proof["search"] = actual
    stage("search_verified")
    def ready(name, port):
        wait(lambda: call("exec", "-T", "probe", "curl", "-fs", "--max-time", "4",
                          f"http://{name}:{port}/health/ready"), name + " readiness")

    run(c + ["up", "-d", "--pull", "never", "--no-build", "system-backend"])
    ready("system-backend", 8180)
    run(c + ["up", "-d", "--pull", "never", "--no-build", "catalog-backend", "gateway",
             "console", "catalog-frontend", "edge"])
    edge = ["exec", "-T", "probe", "curl", "-fsS", "--noproxy", "*", "--max-time", "5", "--cacert", "/tls/ca.pem"]
    url = "https://" + args.hostname
    def http(path, body=None, token=None):
        # Send credentials via stdin, never in container command arguments or evidence.
        req = edge + ["-K", "-", url + path]
        lines = []
        if body is not None:
            lines.extend(['header = "Content-Type: application/json"',
                          "data = " + json.dumps(json.dumps(body, ensure_ascii=True))])
        if token:
            lines.append("header = " + json.dumps("Authorization: Bearer " + token))
        return call(*req, data=("\n".join(lines) + "\n").encode())

    ready("catalog-backend", 8192)
    ready("gateway", 8000)
    for path in ["/", "/module-ui/catalog/"]:
        if b"<html" not in http(path).lower():
            raise RuntimeError("missing frontend HTML: " + path)
    stage("https_ready")
    a = json.loads((root / "login.json").read_text())
    b = json.loads(http("/api/v1/system/login", {k: a[k] for k in ("username", "password")}))
    if "selection" in b:
        sel = b["selection"]
        opts = [t for t in sel["contexts"] if t["type"] == "tenant" and (
            "tenant_membership_id" not in a or t["tenant_membership_id"] == a["tenant_membership_id"])]
        if len(opts) != 1:
            raise ValueError("specify the verification tenant_membership_id")
        b = json.loads(http("/api/v1/system/auth/context-selections", {
            "context_type": "tenant", "selection_ticket": sel["selection_ticket"],
            "tenant_membership_id": opts[0]["tenant_membership_id"]}))
    token = b["session"]["access_token"]
    if not token:
        raise RuntimeError("login did not return an access token")
    stage("login_verified")
    result = wait(lambda: json.loads(http("/api/v1/catalog/entries?page=1&page_size=1", token=token)), "Catalog route")
    if not isinstance(result.get("data"), list) or type(result.get("total")) is not int:
        raise RuntimeError("invalid Catalog response")
    proof["catalog_total"] = result["total"]
    stage("catalog_verified")
    print(work / "result.json")


IMAGE_NAMES = {"edge", "postgres", "redis", "minio", "meilisearch", "system-backend",
               "catalog-backend", "gateway", "console", "catalog-frontend"}


def lock_images(args):
    refs = json.loads(args.references.read_text())
    if set(refs) != IMAGE_NAMES:
        raise ValueError("expected image names: " + ", ".join(sorted(IMAGE_NAMES)))
    locked = {}
    for name, ref in refs.items():
        item = json.loads(subprocess.check_output(["docker", "image", "inspect", ref]))[0]
        locked[name] = item["Id"]
    with args.out.open("x") as f:
        json.dump(locked, f, indent=2)


if __name__ == "__main__":
    p = argparse.ArgumentParser(description=__doc__)
    commands = p.add_subparsers(dest="action", required=True)
    lock = commands.add_parser("lock-images", help="freeze locally available images before an outage")
    lock.add_argument("--references", type=Path, required=True)
    lock.add_argument("--out", type=Path, required=True)
    check = commands.add_parser("check-tls", help="verify recovery TLS before an outage")
    check.add_argument("--kit", type=Path, required=True)
    check.add_argument("--hostname", required=True)
    restore = commands.add_parser("restore", help="run with the primary unreachable and no image pulls")
    restore.add_argument("--kit", type=Path, required=True)
    restore.add_argument("--run-dir", type=Path, required=True, help="new private directory for this attempt")
    restore.add_argument("--hostname", required=True)
    restore.add_argument("--primary-ip", required=True, help="source IPv4 address for the isolated negative probe")
    restore.add_argument("--timeout", type=int, default=180)
    args = p.parse_args()
    if args.action == "lock-images":
        lock_images(args)
    elif args.action == "check-tls":
        verify_tls(args.kit, args.hostname)
    else:
        recover(args)
